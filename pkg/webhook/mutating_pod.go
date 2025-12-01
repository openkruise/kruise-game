/*
Copyright 2022 The Kruise Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-logr/logr"
	gameKruiseV1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/cloudprovider/errors"
	"github.com/openkruise/kruise-game/cloudprovider/manager"
	"github.com/openkruise/kruise-game/pkg/logging"
	"github.com/openkruise/kruise-game/pkg/telemetryfields"
	"github.com/openkruise/kruise-game/pkg/tracing"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	podMutatingTimeout        = 8 * time.Second
	mutatingTimeoutReason     = "MutatingTimeout"
	defaultNetworkStatusLabel = telemetryfields.NetworkStatusWaiting
	webhookHandlerMutatingPod = "mutating-pod"
)

type patchResult struct {
	pod *corev1.Pod
	err errors.PluginError
}

type PodMutatingHandler struct {
	Client               client.Client
	decoder              admission.Decoder
	CloudProviderManager *manager.ProviderManager
	eventRecorder        record.EventRecorder
}

func (pmh *PodMutatingHandler) Handle(ctx context.Context, req admission.Request) admission.Response {
	// Create root span for Webhook (SERVER span kind)
	tracer := otel.Tracer("okg-controller-manager")
	ctx, span := tracer.Start(ctx, tracing.SpanAdmissionMutatePod,
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(
			tracing.AttrK8sNamespaceName(req.Namespace),
			tracing.AttrGameServerName(req.Name),
			attribute.String(telemetryfields.FieldAdmissionOperation, string(req.Operation)),
			tracing.AttrComponent("webhook"),
			tracing.AttrWebhookHandler(webhookHandlerMutatingPod),
		))
	defer span.End()

	op := string(req.Operation)
	reqUID := string(req.UID)
	gvk := fmt.Sprintf("%s/%s/%s", req.Kind.Group, req.Kind.Version, req.Kind.Kind)
	logger := logging.FromContextWithTrace(ctx).WithValues(
		telemetryfields.FieldComponent, "webhook",
		telemetryfields.FieldWebhookHandler, webhookHandlerMutatingPod,
		telemetryfields.FieldAdmissionOperation, op,
		telemetryfields.FieldAdmissionOperation, op,
		telemetryfields.FieldK8sNamespaceName, req.Namespace,
		telemetryfields.FieldAdmissionRequestUID, reqUID,
		telemetryfields.FieldAdmissionResource, gvk,
	)
	span.SetAttributes(tracing.AttrAdmissionRequestUID(reqUID))
	ctx = logr.NewContext(ctx, logger)

	// decode request & get pod
	pod, err := getPodFromRequest(req, pmh.decoder)
	if err != nil {
		logger.Error(err, "Failed to decode pod from admission request")
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to decode pod from request")
		return admission.Errored(http.StatusInternalServerError, err)
	}

	if pod != nil {
		logger = logger.WithValues(
			telemetryfields.FieldGameServerNamespace, pod.GetNamespace(),
			telemetryfields.FieldGameServerName, pod.GetName(),
		)
		span.SetAttributes(tracing.AttrGameServerName(pod.GetName()))
		span.SetAttributes(tracing.AttrGameServerNamespace(pod.GetNamespace()))
		if gssName := pod.GetLabels()[gameKruiseV1alpha1.GameServerOwnerGssKey]; gssName != "" {
			logger = logger.WithValues(
				telemetryfields.FieldGameServerSetNamespace, pod.GetNamespace(),
				telemetryfields.FieldGameServerSetName, gssName,
			)
			span.SetAttributes(
				tracing.AttrGameServerSetName(gssName),
				tracing.AttrGameServerSetNamespace(pod.GetNamespace()),
			)
		}
		span.SetAttributes(tracing.AttrK8sNamespaceName(pod.GetNamespace()))
	}

	initialNetworkStatus := deriveNetworkStatusLabel(ctx, pod)
	if initialNetworkStatus == "" {
		initialNetworkStatus = defaultNetworkStatusLabel
	}
	span.SetAttributes(tracing.AttrNetworkStatus(initialNetworkStatus))

	// Parse traceparent from Pod annotation and add Link to Reconcile span
	if traceparent, ok := pod.Annotations[telemetryfields.AnnotationTraceparent]; ok {
		remoteSpanContext, err := tracing.ParseTraceparent(traceparent)
		if err != nil {
			logger.Error(err, "Failed to parse traceparent from pod annotation", telemetryfields.FieldTraceparent, traceparent)
		} else {
			span.AddLink(trace.Link{
				SpanContext: remoteSpanContext,
				Attributes: []attribute.KeyValue{
					tracing.AttrLinkReason("triggered_by_reconcile"),
				},
			})
			logger.Info("Linked webhook span to reconcile trace", telemetryfields.FieldTraceparent, traceparent)
		}
	}

	if req.Operation == admissionv1.Create {
		pod, err = patchContainers(pmh.Client, pod, ctx)
		if err != nil {
			msg := fmt.Sprintf("Pod %s/%s patchContainers failed, because of %s", pod.Namespace, pod.Name, err.Error())
			logger.Error(err, "Patch containers failed")
			span.RecordError(err)
			span.SetStatus(codes.Error, "patchContainers failed")
			return admission.Denied(msg)
		}
	}

	// get the plugin according to pod
	logger.V(4).Info("Processing webhook request", telemetryfields.FieldAnnotations, pod.Annotations)

	// List all available plugins for debugging
	availablePlugins := []string{}
	for _, cp := range pmh.CloudProviderManager.CloudProviders {
		plugins, err := cp.ListPlugins()
		if err != nil {
			logger.Error(err, "Cloud provider failed to list plugins", telemetryfields.FieldProvider, cp.Name())
			continue
		}
		for _, p := range plugins {
			availablePlugins = append(availablePlugins, p.Name())
		}
	}
	logger.V(4).Info("Available plugins", telemetryfields.FieldPlugins, availablePlugins)

	plugin, ok := pmh.CloudProviderManager.FindAvailablePlugins(pod)
	if !ok {
		networkType, hasNetworkType := pod.Annotations[gameKruiseV1alpha1.GameServerNetworkType]
		msg := fmt.Sprintf("Pod %s/%s has no available plugin (network-type annotation: %s=%v, available plugins: %v)",
			pod.Namespace, pod.Name, networkType, hasNetworkType, availablePlugins)
		logger.Info("No available network plugin", telemetryfields.FieldMessage, msg, telemetryfields.FieldNetworkType, networkType, telemetryfields.FieldNetworkTypeAnnotationPresent, hasNetworkType)
		span.SetAttributes(attribute.Bool(telemetryfields.FieldPluginAvailable, false))
		span.SetStatus(codes.Ok, "no plugin needed")
		return getAdmissionResponse(ctx, req, patchResult{pod: pod, err: nil})
	}

	pluginName := plugin.Name()
	spanAttrs := []attribute.KeyValue{
		tracing.AttrNetworkPlugin(pluginName),
		attribute.String(telemetryfields.FieldPluginAlias, plugin.Alias()),
	}
	if provider, ok := tracing.CloudProviderFromNetworkType(pluginName); ok {
		spanAttrs = append(spanAttrs, tracing.AttrCloudProvider(provider))
	} else {
		spanAttrs = append(spanAttrs, tracing.AttrCloudProvider(tracing.CloudProviderUnknown))
	}
	span.SetAttributes(spanAttrs...)
	logger = logger.WithValues(
		telemetryfields.FieldNetworkPluginName, pluginName,
		telemetryfields.FieldPluginAlias, plugin.Alias(),
	)

	// define context with timeout
	// CRITICAL: Use ctx from webhook request to preserve trace context propagation
	// DO NOT use context.Background() as it would lose the trace parent from HTTP headers
	ctx, cancel := context.WithTimeout(ctx, podMutatingTimeout)
	defer cancel()

	// cloud provider plugin patches pod
	resultCh := make(chan patchResult, 1)
	go func(ctx context.Context) {
		var newPod *corev1.Pod
		var pluginError errors.PluginError
		operation := strings.ToLower(string(req.Operation))
		pluginLogger := logger.WithValues(telemetryfields.FieldPluginOperation, operation)
		pluginSpanAttrs := buildPluginSpanAttributes(spanAttrs, operation, initialNetworkStatus)
		pluginCtx, pluginSpan := tracer.Start(ctx,
			tracing.SpanExecuteNetworkPlugin,
			trace.WithSpanKind(trace.SpanKindInternal),
			trace.WithAttributes(pluginSpanAttrs...),
		)
		defer pluginSpan.End()
		pluginLogger.V(4).Info("Invoking network plugin")
		switch req.Operation {
		case admissionv1.Create:
			newPod, pluginError = plugin.OnPodAdded(pmh.Client, pod, pluginCtx)
		case admissionv1.Update:
			newPod, pluginError = plugin.OnPodUpdated(pmh.Client, pod, pluginCtx)
		case admissionv1.Delete:
			pluginError = plugin.OnPodDeleted(pmh.Client, pod, pluginCtx)
		}
		if pluginError != nil {
			msg := fmt.Sprintf("Failed to %s pod %s/%s ,because of %s", req.Operation, pod.Namespace, pod.Name, pluginError.Error())
			pluginLogger.Error(pluginError, "Plugin execution failed")
			pmh.eventRecorder.Eventf(pod, corev1.EventTypeWarning, string(pluginError.Type()), msg)
			pluginSpan.RecordError(pluginError)
			pluginSpan.SetStatus(codes.Error, pluginError.Error())
			pluginSpan.SetAttributes(tracing.AttrErrorType(string(pluginError.Type())))
			newPod = pod.DeepCopy()
		} else {
			pluginLogger.Info("Plugin execution succeeded")
			pluginSpan.SetStatus(codes.Ok, "plugin execution succeeded")
		}

		finalPluginNetworkStatus := resolveNetworkStatusLabel(pluginCtx, newPod, initialNetworkStatus, pluginError)
		pluginSpan.SetAttributes(tracing.AttrNetworkStatus(finalPluginNetworkStatus))
		resultCh <- patchResult{
			pod: newPod,
			err: pluginError,
		}
	}(ctx)

	select {
	// timeout
	case <-ctx.Done():
		msg := fmt.Sprintf("Failed to %s pod %s/%s, because plugin %s exec timed out", req.Operation, pod.Namespace, pod.Name, plugin.Name())
		logger.Error(ctx.Err(), "Plugin execution timed out")
		pmh.eventRecorder.Eventf(pod, corev1.EventTypeWarning, mutatingTimeoutReason, msg)
		span.SetStatus(codes.Error, "plugin execution timeout")
		span.SetAttributes(attribute.Bool(telemetryfields.FieldPluginTimeout, true))
		span.SetAttributes(tracing.AttrNetworkStatus(telemetryfields.NetworkStatusError))
		return admission.Allowed(msg)
	// completed before timeout
	case result := <-resultCh:
		if result.err != nil {
			span.RecordError(result.err)
			span.SetStatus(codes.Error, fmt.Sprintf("plugin error: %s", result.err.Error()))
		} else {
			span.SetStatus(codes.Ok, "admission completed successfully")
		}

		finalNetworkStatus := resolveNetworkStatusLabel(ctx, result.pod, initialNetworkStatus, result.err)
		span.SetAttributes(tracing.AttrNetworkStatus(finalNetworkStatus))
		return getAdmissionResponse(ctx, req, result)
	}
}

func getPodFromRequest(req admission.Request, decoder admission.Decoder) (*corev1.Pod, error) {
	pod := &corev1.Pod{}
	if req.Operation == admissionv1.Delete {
		err := decoder.DecodeRaw(req.OldObject, pod)
		return pod, err
	}
	err := decoder.Decode(req, pod)
	return pod, err
}

func getAdmissionResponse(ctx context.Context, req admission.Request, result patchResult) admission.Response {
	if result.err != nil {
		return admission.Denied(result.err.Error())
	}
	if req.Operation == admissionv1.Delete {
		return admission.Allowed("delete successfully")
	}

	// Remove traceparent annotation from the target Pod object BEFORE generating patch
	// This way, PatchResponseFromRaw will automatically generate a "remove" operation in the JSONPatch
	//
	// Why this works:
	// - controller-runtime's admission.Response uses resp.Patches (operation slice) internally
	// - resp.Patch (byte array) gets OVERWRITTEN by resp.Patches during serialization
	// - Manually appending to resp.Patch doesn't work because it gets discarded
	// - The correct approach: modify the target object, then let PatchResponseFromRaw calculate the diff
	//
	// References:
	// - https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/webhook/admission
	// - "Patches set here will override any patches in the response"
	pod := result.pod.DeepCopy()
	if pod.Annotations != nil {
		if _, exists := pod.Annotations[telemetryfields.AnnotationTraceparent]; exists {
			delete(pod.Annotations, telemetryfields.AnnotationTraceparent)
			if ctx != nil {
				logging.FromContextWithTrace(ctx).V(4).Info("Removed traceparent annotation before generating patch",
					telemetryfields.FieldK8sNamespaceName, req.Namespace,
					telemetryfields.FieldK8sPodName, req.Name,
				)
			}

			// Optional: clean up empty annotations map for cleaner diff
			if len(pod.Annotations) == 0 {
				pod.Annotations = nil
			}
		}
	}

	// Generate JSONPatch by comparing req.Object.Raw with the modified pod
	// The framework will automatically include a "remove" operation for the deleted annotation
	marshaledPod, err := json.Marshal(pod)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	// Let controller-runtime handle the patch generation
	// DO NOT manually modify resp.Patch or resp.Patches after this
	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
}

func NewPodMutatingHandler(client client.Client, decoder admission.Decoder, cpm *manager.ProviderManager, recorder record.EventRecorder) *PodMutatingHandler {
	return &PodMutatingHandler{
		Client:               client,
		decoder:              decoder,
		CloudProviderManager: cpm,
		eventRecorder:        recorder,
	}
}

func patchContainers(client client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, error) {
	if _, ok := pod.GetLabels()[gameKruiseV1alpha1.GameServerOwnerGssKey]; !ok {
		return pod, nil
	}
	gs := &gameKruiseV1alpha1.GameServer{}
	err := client.Get(ctx, types.NamespacedName{
		Namespace: pod.GetNamespace(),
		Name:      pod.GetName(),
	}, gs)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return pod, nil
		}
		return pod, err
	}
	if gs.Spec.Containers != nil {
		var containers []corev1.Container
		for _, podContainer := range pod.Spec.Containers {
			container := podContainer
			for _, gsContainer := range gs.Spec.Containers {
				if gsContainer.Name == podContainer.Name {
					// patch Image
					if gsContainer.Image != podContainer.Image && gsContainer.Image != "" {
						container.Image = gsContainer.Image
					}

					// patch Resources
					if limitCPU, ok := gsContainer.Resources.Limits[corev1.ResourceCPU]; ok {
						if container.Resources.Limits == nil {
							container.Resources.Limits = make(map[corev1.ResourceName]resource.Quantity)
						}
						container.Resources.Limits[corev1.ResourceCPU] = limitCPU
					}
					if limitMemory, ok := gsContainer.Resources.Limits[corev1.ResourceMemory]; ok {
						if container.Resources.Limits == nil {
							container.Resources.Limits = make(map[corev1.ResourceName]resource.Quantity)
						}
						container.Resources.Limits[corev1.ResourceMemory] = limitMemory
					}
					if requestCPU, ok := gsContainer.Resources.Requests[corev1.ResourceCPU]; ok {
						if container.Resources.Requests == nil {
							container.Resources.Requests = make(map[corev1.ResourceName]resource.Quantity)
						}
						container.Resources.Requests[corev1.ResourceCPU] = requestCPU
					}
					if requestMemory, ok := gsContainer.Resources.Requests[corev1.ResourceMemory]; ok {
						if container.Resources.Requests == nil {
							container.Resources.Requests = make(map[corev1.ResourceName]resource.Quantity)
						}
						container.Resources.Requests[corev1.ResourceMemory] = requestMemory
					}
				}
			}
			containers = append(containers, container)
		}
		pod.Spec.Containers = containers
	}
	return pod, nil
}

func deriveNetworkStatusLabel(ctx context.Context, pod *corev1.Pod) string {
	if pod == nil || pod.Annotations == nil {
		return ""
	}
	rawStatus, ok := pod.Annotations[gameKruiseV1alpha1.GameServerNetworkStatus]
	if !ok || strings.TrimSpace(rawStatus) == "" {
		return ""
	}
	var status gameKruiseV1alpha1.NetworkStatus
	if err := json.Unmarshal([]byte(rawStatus), &status); err != nil {
		if ctx != nil {
			logging.FromContextWithTrace(ctx).V(4).Info("Pod has invalid network status annotation", telemetryfields.FieldExceptionMessage, err)
		}
		return ""
	}
	return normalizeNetworkStateValue(status.CurrentNetworkState)
}

func normalizeNetworkStateValue(state gameKruiseV1alpha1.NetworkState) string {
	switch state {
	case gameKruiseV1alpha1.NetworkReady:
		return telemetryfields.NetworkStatusReady
	case gameKruiseV1alpha1.NetworkNotReady:
		return telemetryfields.NetworkStatusNotReady
	case gameKruiseV1alpha1.NetworkWaiting:
		return telemetryfields.NetworkStatusWaiting
	}
	if state == "" {
		return ""
	}
	return strings.ToLower(string(state))
}

func buildPluginSpanAttributes(base []attribute.KeyValue, operation, defaultStatus string) []attribute.KeyValue {
	attrs := append([]attribute.KeyValue{}, base...)
	attrs = append(attrs,
		attribute.String(telemetryfields.FieldPluginOperation, operation),
		tracing.AttrComponent("webhook"),
	)
	return tracing.EnsureNetworkStatusAttr(attrs, defaultStatus)
}

func resolveNetworkStatusLabel(ctx context.Context, pod *corev1.Pod, fallback string, pluginErr errors.PluginError) string {
	if status := deriveNetworkStatusLabel(ctx, pod); status != "" {
		return status
	}
	if pluginErr != nil {
		return telemetryfields.NetworkStatusError
	}
	if fallback == "" {
		return defaultNetworkStatusLabel
	}
	return fallback
}
