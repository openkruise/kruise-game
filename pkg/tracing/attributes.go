/*
Copyright 2024 The Kruise Authors.

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

package tracing

import (
	"strings"

	corev1 "k8s.io/api/core/v1"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/pkg/telemetryfields"
	"go.opentelemetry.io/otel/attribute"
)

var (
	gameServerSetNameKey      = attribute.Key(telemetryfields.FieldGameServerSetName)
	gameServerSetNamespaceKey = attribute.Key(telemetryfields.FieldGameServerSetNamespace)
	gameServerNameKey         = attribute.Key(telemetryfields.FieldGameServerName)
	gameServerNamespaceKey    = attribute.Key(telemetryfields.FieldGameServerNamespace)
	networkPluginKey          = attribute.Key(telemetryfields.FieldNetworkPluginName)
	networkStatusKey          = attribute.Key(telemetryfields.FieldNetworkStatus)
	networkResourceIDKey      = attribute.Key(telemetryfields.FieldNetworkResourceID)
	networkPortRangeKey       = attribute.Key(telemetryfields.FieldNetworkPortRange)
	componentKey              = attribute.Key(telemetryfields.FieldComponent)
	webhookHandlerKey         = attribute.Key(telemetryfields.FieldWebhookHandler)
	errorTypeKey              = attribute.Key(telemetryfields.FieldErrorType)
	cloudProviderKey          = attribute.Key(telemetryfields.FieldProvider)
	admissionRequestUIDKey    = attribute.Key(telemetryfields.FieldAdmissionRequestUID)
	reconcileTriggerKey       = attribute.Key(telemetryfields.FieldReconcileTrigger)
	reconcileActionKey        = attribute.Key(telemetryfields.FieldReconcileAction)
	linkReasonKey             = attribute.Key(telemetryfields.FieldLinkReason)
	k8sNamespaceKey           = attribute.Key(telemetryfields.FieldK8sNamespaceName)
	reconcileRequeueKey       = attribute.Key(telemetryfields.FieldReconcileRequeue)
)

// CloudProvider represents the canonical OpenTelemetry enumeration values for cloud providers.
type CloudProvider string

const (
	CloudProviderAWS          CloudProvider = "aws"
	CloudProviderAlibabaCloud CloudProvider = "alibaba_cloud"
	CloudProviderTencentCloud CloudProvider = "tencent_cloud"
	CloudProviderVolcengine   CloudProvider = "volcengine"
	CloudProviderJDCloud      CloudProvider = "jdcloud"
	CloudProviderHwCloud      CloudProvider = "hwcloud"
	CloudProviderKubernetes   CloudProvider = "kubernetes"
	CloudProviderUnknown      CloudProvider = "unknown"
)

// AttrGameServerSetName returns a span attribute representing the GameServerSet name.
func AttrGameServerSetName(name string) attribute.KeyValue {
	return gameServerSetNameKey.String(name)
}

// AttrGameServerName returns a span attribute representing the GameServer/GameServer Pod name.
func AttrGameServerName(name string) attribute.KeyValue {
	return gameServerNameKey.String(name)
}

// AttrGameServerSetNamespace returns a span attribute representing the GameServerSet namespace.
func AttrGameServerSetNamespace(namespace string) attribute.KeyValue {
	return gameServerSetNamespaceKey.String(namespace)
}

// AttrGameServerNamespace returns a span attribute representing the GameServer namespace.
func AttrGameServerNamespace(namespace string) attribute.KeyValue {
	return gameServerNamespaceKey.String(namespace)
}

// AttrNetworkPlugin returns a span attribute representing the network plugin name.
func AttrNetworkPlugin(name string) attribute.KeyValue {
	return networkPluginKey.String(normalizeDimensionValue(name))
}

// AttrComponent returns a span attribute representing which OpenKruiseGame component emits the span.
func AttrComponent(component string) attribute.KeyValue {
	return componentKey.String(component)
}

// AttrWebhookHandler returns a span attribute representing which webhook handler processed the request.
func AttrWebhookHandler(handler string) attribute.KeyValue {
	return webhookHandlerKey.String(handler)
}

// AttrNetworkStatus returns a span attribute representing the network status (ready/not_ready/error).
func AttrNetworkStatus(status string) attribute.KeyValue {
	return networkStatusKey.String(status)
}

// EnsureNetworkStatusAttr appends a default network status attribute when missing.
// The returned slice always includes game.kruise.io.network.status so spanmetrics dimensions remain stable.
func EnsureNetworkStatusAttr(attrs []attribute.KeyValue, defaultStatus string) []attribute.KeyValue {
	for _, attr := range attrs {
		if attr.Key == networkStatusKey {
			return attrs
		}
	}
	if defaultStatus == "" {
		defaultStatus = "unknown"
	}
	return append(attrs, AttrNetworkStatus(defaultStatus))
}

// AttrNetworkResourceID returns a span attribute representing the related cloud resource identifier.
func AttrNetworkResourceID(id string) attribute.KeyValue {
	return networkResourceIDKey.String(id)
}

// AttrNetworkPortRange returns a span attribute representing the host/network port range.
func AttrNetworkPortRange(portRange string) attribute.KeyValue {
	return networkPortRangeKey.String(portRange)
}

// AttrCloudProvider returns a span attribute representing the cloud provider enumeration value.
func AttrCloudProvider(provider CloudProvider) attribute.KeyValue {
	return cloudProviderKey.String(string(provider))
}

// AttrReconcileAction returns a span attribute representing the current reconcile action (create_statefulset/kill_gameservers/scale_gameservers/update_workload/etc.).
func AttrReconcileAction(action string) attribute.KeyValue {
	return reconcileActionKey.String(action)
}

// AttrLinkReason returns an attribute describing the reason a trace Link was added
func AttrLinkReason(reason string) attribute.KeyValue {
	return linkReasonKey.String(reason)
}

// AttrReconcileRequeue returns a bool attribute to indicate whether reconcile will requeue.
func AttrReconcileRequeue(requeue bool) attribute.KeyValue {
	return reconcileRequeueKey.Bool(requeue)
}

// CloudProviderFromNetworkType maps a GameServer network-type annotation (e.g. "Kubernetes-HostPort")
// to the canonical OpenTelemetry cloud.provider enumeration used in spanmetrics dimensions.
func CloudProviderFromNetworkType(networkType string) (CloudProvider, bool) {
	trimmed := strings.TrimSpace(networkType)
	if trimmed == "" {
		return CloudProviderUnknown, false
	}
	lower := strings.ToLower(trimmed)
	switch {
	case strings.HasPrefix(lower, "kubernetes-"):
		return CloudProviderKubernetes, true
	case strings.HasPrefix(lower, "alibabacloud-"):
		return CloudProviderAlibabaCloud, true
	case strings.HasPrefix(lower, "amazonwebservices-"):
		return CloudProviderAWS, true
	case strings.HasPrefix(lower, "tencentcloud-"):
		return CloudProviderTencentCloud, true
	case strings.HasPrefix(lower, "volcengine-"):
		return CloudProviderVolcengine, true
	case strings.HasPrefix(lower, "jdcloud-"):
		return CloudProviderJDCloud, true
	case strings.HasPrefix(lower, "hwcloud-"):
		return CloudProviderHwCloud, true
	default:
		return CloudProviderUnknown, false
	}
}

// AttrErrorType returns a span attribute representing the classified error type (ParameterError, ApiCallError, etc.).
func AttrErrorType(errType string) attribute.KeyValue {
	return errorTypeKey.String(errType)
}

// AttrAdmissionRequestUID returns a span attribute representing the admission request UID.
func AttrAdmissionRequestUID(uid string) attribute.KeyValue {
	return admissionRequestUIDKey.String(uid)
}

// AttrReconcileTrigger returns a span attribute representing the reconcile trigger type (create/update/delete/pod.updated/etc.).
func AttrReconcileTrigger(trigger string) attribute.KeyValue {
	return reconcileTriggerKey.String(trigger)
}

// AttrK8sPodName returns a span attribute for k8s.pod.name.
func AttrK8sPodName(podName string) attribute.KeyValue {
	return attribute.Key(telemetryfields.FieldK8sPodName).String(podName)
}

// AttrK8sNodeName returns a span attribute for k8s.node.name.
func AttrK8sNodeName(nodeName string) attribute.KeyValue {
	return attribute.Key(telemetryfields.FieldK8sNodeName).String(nodeName)
}

// AttrServiceName returns a span attribute for service.name.
func AttrServiceName(name string) attribute.KeyValue {
	return attribute.Key(telemetryfields.FieldServiceName).String(name)
}

// AttrServiceNamespace returns a span attribute for service.namespace.
func AttrServiceNamespace(ns string) attribute.KeyValue {
	return attribute.Key(telemetryfields.FieldServiceNamespace).String(ns)
}

// AttrK8sNamespaceName returns a span attribute for k8s.namespace.name.
func AttrK8sNamespaceName(namespace string) attribute.KeyValue {
	return k8sNamespaceKey.String(namespace)
}

// AttrsForGameServer returns a slice of attributes covering GameServerSet and GameServer names.
func AttrsForGameServer(gssName, gsName string) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, 2)
	if gssName != "" {
		attrs = append(attrs, AttrGameServerSetName(gssName))
	}
	if gsName != "" {
		attrs = append(attrs, AttrGameServerName(gsName))
	}
	return attrs
}

// BaseNetworkAttrs assembles the common attribute set required by network plugins.
func BaseNetworkAttrs(component, pluginName string, pod *corev1.Pod, extras ...attribute.KeyValue) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, 6+len(extras))
	if component != "" {
		attrs = append(attrs, AttrComponent(component))
	}
	if pluginName != "" {
		attrs = append(attrs, AttrNetworkPlugin(pluginName))
	}
	if pod != nil {
		if ns := pod.GetNamespace(); ns != "" {
			attrs = append(attrs,
				AttrK8sNamespaceName(ns),
				AttrGameServerSetNamespace(ns),
				AttrGameServerNamespace(ns),
			)
		}
		attrs = append(attrs, AttrsForGameServer(
			pod.GetLabels()[gamekruiseiov1alpha1.GameServerOwnerGssKey],
			pod.GetName(),
		)...)
	}
	attrs = append(attrs, extras...)
	return attrs
}

// normalizeDimensionValue converts human-friendly plugin names into lower snake/hyphen case strings
// so that metric dimensions remain stable (Grafana queries depend on exact matches).
func normalizeDimensionValue(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	lower := strings.ToLower(trimmed)
	if strings.ContainsAny(lower, " \t") {
		lower = strings.Join(strings.Fields(lower), "_")
	}
	return lower
}
