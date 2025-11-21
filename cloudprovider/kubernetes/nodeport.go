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

package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/cloudprovider"
	cperrors "github.com/openkruise/kruise-game/cloudprovider/errors"
	"github.com/openkruise/kruise-game/cloudprovider/utils"
	"github.com/openkruise/kruise-game/pkg/logging"
	"github.com/openkruise/kruise-game/pkg/telemetryfields"
	"github.com/openkruise/kruise-game/pkg/tracing"
	"github.com/openkruise/kruise-game/pkg/util"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	NodePortNetwork = "Kubernetes-NodePort"

	PortProtocolsConfigName = "PortProtocols"

	SvcSelectorDisabledKey = "game.kruise.io/svc-selector-disabled"

	nodePortComponentName    = "okg-controller-manager"
	nodePortPluginSlug       = telemetryfields.NetworkPluginKubernetesNodePort
	nodePortReconcileTrigger = "pod.updated"
	nodePortSelectorBefore   = "game.kruise.io.nodeport.selector.before"
	nodePortSelectorAfter    = "game.kruise.io.nodeport.selector.after"
)

var (
	nodePortAttrNetworkDisabledKey = attribute.Key("game.kruise.io.network.plugin.kubernetes.nodeport.network_disabled")
	nodePortAttrHashMismatchKey    = attribute.Key("game.kruise.io.network.plugin.kubernetes.nodeport.hash_mismatch")
	nodePortAttrServicePortsKey    = attribute.Key("game.kruise.io.network.plugin.kubernetes.nodeport.service_ports")
	nodePortAttrServicePortCount   = attribute.Key("game.kruise.io.network.plugin.kubernetes.nodeport.service_port_count")
	nodePortAttrSelectorKey        = attribute.Key("game.kruise.io.network.plugin.kubernetes.nodeport.selector")
	nodePortAttrAllowNotReadyKey   = attribute.Key("game.kruise.io.network.plugin.kubernetes.nodeport.allow_not_ready")
	nodePortAttrAddressListKey     = attribute.Key("game.kruise.io.network.plugin.kubernetes.nodeport.address_list")
	nodePortAttrSelectorBeforeKey  = attribute.Key("game.kruise.io.network.plugin.kubernetes.nodeport.selector_before")
	nodePortAttrSelectorAfterKey   = attribute.Key("game.kruise.io.network.plugin.kubernetes.nodeport.selector_after")
)

type NodePortPlugin struct {
}

func (n *NodePortPlugin) Name() string {
	return NodePortNetwork
}

func (n *NodePortPlugin) Alias() string {
	return ""
}

func (n *NodePortPlugin) Init(client client.Client, options cloudprovider.CloudProviderOptions, ctx context.Context) error {
	return nil
}

func (n *NodePortPlugin) OnPodAdded(client client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	return pod, nil
}

func (n *NodePortPlugin) OnPodUpdated(client client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	// Create root span for NodePort OnPodUpdated
	tracer := otel.Tracer("okg-controller-manager")
	ctx, span := startNodePortSpan(ctx, tracer, tracing.SpanProcessNodePortPod, pod)
	defer span.End()
	logger := nodePortLogger(ctx, pod).WithValues(telemetryfields.FieldOperation, "update")
	serviceKey := fmt.Sprintf("%s/%s", pod.GetNamespace(), pod.GetName())

	networkManager := utils.NewNetworkManager(pod, client)

	networkStatus, _ := networkManager.GetNetworkStatus()
	networkConfig := networkManager.GetNetworkConfig()
	currentState := normalizeNetworkState(networkStatus)
	span.SetAttributes(
		tracing.AttrReconcileTrigger(nodePortReconcileTrigger),
		nodePortAttrNetworkDisabledKey.Bool(networkManager.GetNetworkDisabled()),
		nodePortAttrAllowNotReadyKey.Bool(util.IsAllowNotReadyContainers(networkConfig)),
		nodePortAttrHashMismatchKey.Bool(false),
		tracing.AttrNetworkStatus(currentState),
	)
	logger.Info("Processing NodePort pod update",
		telemetryfields.FieldNetworkDisabled, networkManager.GetNetworkDisabled(),
		telemetryfields.FieldAllowNotReadyContainers, util.IsAllowNotReadyContainers(networkConfig),
		telemetryfields.FieldNetworkState, currentState,
	)
	npc, err := parseNodePortConfig(networkConfig)
	if err != nil {
		logger.Error(err, "Failed to parse NodePort network config", telemetryfields.FieldConfigEntries, len(networkConfig))
		span.RecordError(err)
		span.SetAttributes(tracing.AttrErrorType(telemetryfields.ErrorTypeParameter))
		span.SetStatus(codes.Error, "failed to parse NodePort config")
		return pod, cperrors.NewPluginError(cperrors.ParameterError, err.Error())
	}

	if networkStatus == nil {
		logger.Info("Network status missing, marking pod as not_ready")
		pod, err := networkManager.UpdateNetworkStatus(gamekruiseiov1alpha1.NetworkStatus{
			CurrentNetworkState: gamekruiseiov1alpha1.NetworkNotReady,
		}, pod)
		return pod, cperrors.ToPluginError(err, cperrors.InternalError)
	}

	// get svc
	svc := &corev1.Service{}
	err = client.Get(ctx, types.NamespacedName{
		Name:      pod.GetName(),
		Namespace: pod.GetNamespace(),
	}, svc)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("NodePort service missing, creating new service", telemetryfields.FieldService, serviceKey, telemetryfields.FieldPortCount, len(npc.ports))
			_, createSpan := startNodePortSpan(ctx, tracer, tracing.SpanCreateNodePortService, pod,
				tracing.AttrNetworkStatus(telemetryfields.NetworkStatusNotReady),
				tracing.AttrServiceName(pod.GetName()),
				tracing.AttrServiceNamespace(pod.GetNamespace()),
				nodePortAttrServicePortCount.Int(len(npc.ports)),
			)
			defer createSpan.End()
			svcToCreate := consNodePortSvc(npc, pod, client, ctx)
			createSpan.SetAttributes(
				attribute.String("service.type", string(corev1.ServiceTypeNodePort)),
				nodePortAttrServicePortsKey.String(formatNodePortServicePorts(svcToCreate.Spec.Ports)),
				nodePortAttrSelectorKey.String(formatNodePortSelector(svcToCreate.Spec.Selector)),
			)
			err := client.Create(ctx, svcToCreate)
			if err != nil {
				logger.Error(err, "Failed to create NodePort service", telemetryfields.FieldService, serviceKey)
				createSpan.RecordError(err)
				createSpan.SetAttributes(tracing.AttrErrorType(telemetryfields.ErrorTypeAPICall))
				createSpan.SetStatus(codes.Error, "failed to create service")
				return pod, cperrors.ToPluginError(err, cperrors.ApiCallError)
			}
			createSpan.SetAttributes(
				tracing.AttrNetworkResourceID(string(svcToCreate.GetUID())),
			)
			createSpan.SetStatus(codes.Ok, "service created successfully")
			logger.Info("NodePort service created", telemetryfields.FieldService, serviceKey, telemetryfields.FieldServiceUID, svcToCreate.GetUID(), telemetryfields.FieldPortCount, len(svcToCreate.Spec.Ports))
			return pod, nil
		}
		logger.Error(err, "Failed to get NodePort service", telemetryfields.FieldService, serviceKey)
		span.RecordError(err)
		span.SetAttributes(tracing.AttrErrorType(telemetryfields.ErrorTypeAPICall))
		span.SetStatus(codes.Error, "failed to get service")
		return pod, cperrors.NewPluginError(cperrors.ApiCallError, err.Error())
	}

	// update svc
	if util.GetHash(npc) != svc.GetAnnotations()[ServiceHashKey] {
		logger.Info("NodePort service hash mismatch detected, updating service", telemetryfields.FieldService, serviceKey,
			telemetryfields.FieldCurrentHash, svc.GetAnnotations()[ServiceHashKey],
			telemetryfields.FieldExpectedHash, util.GetHash(npc))
		span.SetAttributes(
			nodePortAttrHashMismatchKey.Bool(true),
			tracing.AttrNetworkStatus(telemetryfields.NetworkStatusNotReady),
		)
		networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkNotReady
		pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
		if err != nil {
			return pod, cperrors.NewPluginError(cperrors.InternalError, err.Error())
		}
		return pod, cperrors.ToPluginError(client.Update(ctx, consNodePortSvc(npc, pod, client, ctx)), cperrors.ApiCallError)
	}

	// disable network
	if networkManager.GetNetworkDisabled() && svc.Spec.Selector[SvcSelectorKey] == pod.GetName() {
		selectorBefore := copyStringMap(svc.Spec.Selector)
		selectorAfter := copyStringMap(svc.Spec.Selector)
		selectorAfter[SvcSelectorDisabledKey] = pod.GetName()
		delete(selectorAfter, SvcSelectorKey)
		logger.Info("Disabling NodePort selector for pod", telemetryfields.FieldService, serviceKey,
			nodePortSelectorBefore, formatNodePortSelector(selectorBefore),
			nodePortSelectorAfter, formatNodePortSelector(selectorAfter))
		_, toggleSpan := startNodePortSpan(ctx, tracer, tracing.SpanToggleNodePortSelector, pod,
			attribute.String("selector.action", "disable"),
			tracing.AttrServiceName(svc.GetName()),
			nodePortAttrSelectorBeforeKey.String(formatNodePortSelector(selectorBefore)),
			nodePortAttrSelectorAfterKey.String(formatNodePortSelector(selectorAfter)),
			nodePortAttrAllowNotReadyKey.Bool(util.IsAllowNotReadyContainers(networkConfig)),
			tracing.AttrNetworkStatus(telemetryfields.NetworkStatusNotReady),
		)
		defer toggleSpan.End()
		svc.Spec.Selector = selectorAfter
		err = client.Update(ctx, svc)
		if err != nil {
			logger.Error(err, "Failed to disable NodePort selector", telemetryfields.FieldService, serviceKey)
			toggleSpan.RecordError(err)
			toggleSpan.SetAttributes(tracing.AttrErrorType(telemetryfields.ErrorTypeAPICall))
			toggleSpan.SetStatus(codes.Error, "failed to disable nodeport selector")
		} else {
			logger.Info("Disabled NodePort selector", telemetryfields.FieldService, serviceKey)
			toggleSpan.SetStatus(codes.Ok, "nodeport selector disabled")
		}
		return pod, cperrors.ToPluginError(err, cperrors.ApiCallError)
	}

	// enable network
	if !networkManager.GetNetworkDisabled() && svc.Spec.Selector[SvcSelectorDisabledKey] == pod.GetName() {
		selectorBefore := copyStringMap(svc.Spec.Selector)
		selectorAfter := copyStringMap(svc.Spec.Selector)
		selectorAfter[SvcSelectorKey] = pod.GetName()
		delete(selectorAfter, SvcSelectorDisabledKey)
		logger.Info("Enabling NodePort selector for pod", telemetryfields.FieldService, serviceKey,
			nodePortSelectorBefore, formatNodePortSelector(selectorBefore),
			nodePortSelectorAfter, formatNodePortSelector(selectorAfter))
		_, toggleSpan := startNodePortSpan(ctx, tracer, tracing.SpanToggleNodePortSelector, pod,
			attribute.String("selector.action", "enable"),
			tracing.AttrServiceName(svc.GetName()),
			nodePortAttrSelectorBeforeKey.String(formatNodePortSelector(selectorBefore)),
			nodePortAttrSelectorAfterKey.String(formatNodePortSelector(selectorAfter)),
			nodePortAttrAllowNotReadyKey.Bool(util.IsAllowNotReadyContainers(networkConfig)),
			tracing.AttrNetworkStatus(telemetryfields.NetworkStatusReady),
		)
		defer toggleSpan.End()
		svc.Spec.Selector = selectorAfter
		err = client.Update(ctx, svc)
		if err != nil {
			logger.Error(err, "Failed to enable NodePort selector", telemetryfields.FieldService, serviceKey)
			toggleSpan.RecordError(err)
			toggleSpan.SetAttributes(tracing.AttrErrorType(telemetryfields.ErrorTypeAPICall))
			toggleSpan.SetStatus(codes.Error, "failed to enable nodeport selector")
		} else {
			logger.Info("Enabled NodePort selector", telemetryfields.FieldService, serviceKey)
			toggleSpan.SetStatus(codes.Ok, "nodeport selector enabled")
		}
		return pod, cperrors.ToPluginError(err, cperrors.ApiCallError)
	}

	// allow not ready containers
	if util.IsAllowNotReadyContainers(networkManager.GetNetworkConfig()) {
		toUpDateSvc, err := utils.AllowNotReadyContainers(client, ctx, pod, svc, false)
		if err != nil {
			logger.Error(err, "Failed to evaluate allow-not-ready containers", telemetryfields.FieldService, serviceKey)
			return pod, err
		}

		if toUpDateSvc {
			logger.Info("Updating NodePort service to allow not-ready containers", telemetryfields.FieldService, serviceKey)
			err := client.Update(ctx, svc)
			if err != nil {
				logger.Error(err, "Failed to update service for allow-not-ready containers", telemetryfields.FieldService, serviceKey)
				return pod, cperrors.ToPluginError(err, cperrors.ApiCallError)
			}
		}
	}

	// network ready
	node := &corev1.Node{}
	err = client.Get(ctx, types.NamespacedName{
		Name: pod.Spec.NodeName,
	}, node)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Error(err, "Node not found for NodePort pod", telemetryfields.FieldK8sNodeName, pod.Spec.NodeName)
			span.RecordError(err)
			span.SetAttributes(
				tracing.AttrNetworkStatus(telemetryfields.NetworkStatusNotReady),
				tracing.AttrErrorType(telemetryfields.ErrorTypeResourceNotReady),
			)
			span.SetStatus(codes.Error, "node not scheduled yet")
			return pod, nil
		}
		logger.Error(err, "Failed to get node for NodePort pod", telemetryfields.FieldK8sNodeName, pod.Spec.NodeName)
		span.RecordError(err)
		span.SetAttributes(tracing.AttrErrorType(telemetryfields.ErrorTypeAPICall))
		span.SetStatus(codes.Error, "failed to get node")
		return pod, cperrors.NewPluginError(cperrors.ApiCallError, err.Error())
	}

	if pod.Status.PodIP == "" {
		// Pod IP not exist, Network NotReady
		errPodIPNotReady := fmt.Errorf("pod IP not assigned")
		logger.Error(errPodIPNotReady, "Pod IP not ready for NodePort", telemetryfields.FieldService, serviceKey)
		span.SetAttributes(
			tracing.AttrNetworkStatus(telemetryfields.NetworkStatusNotReady),
			tracing.AttrErrorType(telemetryfields.ErrorTypeResourceNotReady),
		)
		span.RecordError(errPodIPNotReady)
		span.SetStatus(codes.Error, "pod IP not ready")
		networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkNotReady
		pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
		return pod, cperrors.ToPluginError(err, cperrors.InternalError)
	}

	internalAddresses := make([]gamekruiseiov1alpha1.NetworkAddress, 0)
	externalAddresses := make([]gamekruiseiov1alpha1.NetworkAddress, 0)
	nodeIP := getAddress(node)
	for _, port := range svc.Spec.Ports {
		instrIPort := port.TargetPort
		if port.NodePort == 0 {
			errNodePortNotReady := fmt.Errorf("nodeport %s not assigned yet", port.Name)
			logger.Error(errNodePortNotReady, "NodePort not allocated", telemetryfields.FieldService, serviceKey, telemetryfields.FieldPort, port.Name)
			span.SetAttributes(
				tracing.AttrNetworkStatus(telemetryfields.NetworkStatusNotReady),
				tracing.AttrErrorType(telemetryfields.ErrorTypeResourceNotReady),
				attribute.String("port.name", port.Name),
			)
			span.RecordError(errNodePortNotReady)
			span.SetStatus(codes.Error, "NodePort not allocated")
			networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkNotReady
			pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
			return pod, cperrors.ToPluginError(err, cperrors.InternalError)
		}
		instrEPort := intstr.FromInt(int(port.NodePort))
		internalAddress := gamekruiseiov1alpha1.NetworkAddress{
			IP: pod.Status.PodIP,
			Ports: []gamekruiseiov1alpha1.NetworkPort{
				{
					Name:     instrIPort.String(),
					Port:     &instrIPort,
					Protocol: port.Protocol,
				},
			},
		}
		externalAddress := gamekruiseiov1alpha1.NetworkAddress{
			IP: nodeIP,
			Ports: []gamekruiseiov1alpha1.NetworkPort{
				{
					Name:     instrIPort.String(),
					Port:     &instrEPort,
					Protocol: port.Protocol,
				},
			},
		}
		internalAddresses = append(internalAddresses, internalAddress)
		externalAddresses = append(externalAddresses, externalAddress)
	}
	networkStatus.InternalAddresses = internalAddresses
	networkStatus.ExternalAddresses = externalAddresses
	networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkReady

	_, publishSpan := startNodePortSpan(ctx, tracer, tracing.SpanPublishNodePortStatus, pod,
		tracing.AttrNetworkStatus(telemetryfields.NetworkStatusReady),
		attribute.String(telemetryfields.FieldNodeIP, nodeIP),
		attribute.Int(telemetryfields.FieldInternalAddresses, len(internalAddresses)),
		attribute.Int(telemetryfields.FieldExternalAddresses, len(externalAddresses)),
		nodePortAttrAddressListKey.String(formatNodePortAddresses(internalAddresses, externalAddresses)),
	)
	publishSpan.SetStatus(codes.Ok, "nodeport addresses published")
	publishSpan.End()
	logger.Info("Published NodePort status",
		telemetryfields.FieldService, serviceKey,
		telemetryfields.FieldNodeIP, nodeIP,
		telemetryfields.FieldInternalAddresses, len(internalAddresses),
		telemetryfields.FieldExternalAddresses, len(externalAddresses),
	)

	// Record success
	span.SetAttributes(tracing.AttrNetworkStatus(telemetryfields.NetworkStatusReady))
	span.SetStatus(codes.Ok, "nodeport pod processed")

	pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
	return pod, cperrors.ToPluginError(err, cperrors.InternalError)
}

func (n *NodePortPlugin) OnPodDeleted(client client.Client, pod *corev1.Pod, ctx context.Context) cperrors.PluginError {
	return nil
}

func init() {
	kubernetesProvider.registerPlugin(&NodePortPlugin{})
}

type nodePortConfig struct {
	ports     []int
	protocols []corev1.Protocol
	isFixed   bool
}

func parseNodePortConfig(conf []gamekruiseiov1alpha1.NetworkConfParams) (*nodePortConfig, error) {
	var ports []int
	var protocols []corev1.Protocol
	isFixed := false

	for _, c := range conf {
		switch c.Name {
		case PortProtocolsConfigName:
			ports, protocols = parsePortProtocols(c.Value)

		case FixedKey:
			var err error
			isFixed, err = strconv.ParseBool(c.Value)
			if err != nil {
				return nil, err
			}
		}
	}
	return &nodePortConfig{
		ports:     ports,
		protocols: protocols,
		isFixed:   isFixed,
	}, nil
}

func parsePortProtocols(value string) ([]int, []corev1.Protocol) {
	ports := make([]int, 0)
	protocols := make([]corev1.Protocol, 0)
	for _, pp := range strings.Split(value, ",") {
		ppSlice := strings.Split(pp, "/")
		port, err := strconv.Atoi(ppSlice[0])
		if err != nil {
			continue
		}
		ports = append(ports, port)
		if len(ppSlice) != 2 {
			protocols = append(protocols, corev1.ProtocolTCP)
		} else {
			protocols = append(protocols, corev1.Protocol(ppSlice[1]))
		}
	}
	return ports, protocols
}

func consNodePortSvc(npc *nodePortConfig, pod *corev1.Pod, c client.Client, ctx context.Context) *corev1.Service {
	svcPorts := make([]corev1.ServicePort, 0)
	for i := 0; i < len(npc.ports); i++ {
		svcPorts = append(svcPorts, corev1.ServicePort{
			Name:       strconv.Itoa(npc.ports[i]),
			Port:       int32(npc.ports[i]),
			Protocol:   npc.protocols[i],
			TargetPort: intstr.FromInt(npc.ports[i]),
		})
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pod.GetName(),
			Namespace: pod.GetNamespace(),
			Annotations: map[string]string{
				ServiceHashKey: util.GetHash(npc),
			},
			OwnerReferences: consOwnerReference(c, ctx, pod, npc.isFixed),
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeNodePort,
			Selector: map[string]string{
				SvcSelectorKey: pod.GetName(),
			},
			Ports: svcPorts,
		},
	}
	return svc
}

func nodePortSpanAttrs(pod *corev1.Pod, extras ...attribute.KeyValue) []attribute.KeyValue {
	attrExtras := []attribute.KeyValue{
		tracing.AttrCloudProvider(tracing.CloudProviderKubernetes),
	}
	if pod != nil && pod.Spec.NodeName != "" {
		attrExtras = append(attrExtras, tracing.AttrK8sNodeName(pod.Spec.NodeName))
	}
	attrExtras = append(attrExtras, extras...)
	attrExtras = tracing.EnsureNetworkStatusAttr(attrExtras, telemetryfields.NetworkStatusWaiting)
	return tracing.BaseNetworkAttrs(nodePortComponentName, nodePortPluginSlug, pod, attrExtras...)
}

func startNodePortSpan(ctx context.Context, tracer trace.Tracer, name string, pod *corev1.Pod, extras ...attribute.KeyValue) (context.Context, trace.Span) {
	return tracer.Start(ctx, name,
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(nodePortSpanAttrs(pod, extras...)...),
	)
}

func formatNodePortServicePorts(ports []corev1.ServicePort) string {
	type portSnapshot struct {
		Name       string `json:"name,omitempty"`
		Port       int32  `json:"port,omitempty"`
		NodePort   int32  `json:"nodePort,omitempty"`
		Protocol   string `json:"protocol,omitempty"`
		TargetPort string `json:"targetPort,omitempty"`
	}
	snapshot := make([]portSnapshot, 0, len(ports))
	for _, p := range ports {
		snapshot = append(snapshot, portSnapshot{
			Name:       p.Name,
			Port:       p.Port,
			NodePort:   p.NodePort,
			Protocol:   string(p.Protocol),
			TargetPort: p.TargetPort.String(),
		})
	}
	return marshalToJSONString(snapshot)
}

func formatNodePortSelector(selector map[string]string) string {
	if selector == nil {
		return "{}"
	}
	return marshalToJSONString(selector)
}

func formatNodePortAddresses(internal, external []gamekruiseiov1alpha1.NetworkAddress) string {
	type addressSnapshot struct {
		Internal []gamekruiseiov1alpha1.NetworkAddress `json:"internal"`
		External []gamekruiseiov1alpha1.NetworkAddress `json:"external"`
	}
	return marshalToJSONString(addressSnapshot{Internal: internal, External: external})
}

func marshalToJSONString(v interface{}) string {
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(data)
}

func copyStringMap(source map[string]string) map[string]string {
	if source == nil {
		return map[string]string{}
	}
	clone := make(map[string]string, len(source))
	for k, v := range source {
		clone[k] = v
	}
	return clone
}

func normalizeNetworkState(status *gamekruiseiov1alpha1.NetworkStatus) string {
	if status == nil || status.CurrentNetworkState == "" {
		return telemetryfields.NetworkStatusNotReady
	}
	switch status.CurrentNetworkState {
	case gamekruiseiov1alpha1.NetworkReady:
		return telemetryfields.NetworkStatusReady
	case gamekruiseiov1alpha1.NetworkNotReady:
		return telemetryfields.NetworkStatusNotReady
	case gamekruiseiov1alpha1.NetworkWaiting:
		return telemetryfields.NetworkStatusWaiting
	default:
		return strings.ToLower(string(status.CurrentNetworkState))
	}
}

func nodePortLogger(ctx context.Context, pod *corev1.Pod) logr.Logger {
	logger := logging.FromContextWithTrace(ctx).WithValues(
		telemetryfields.FieldComponent, "cloudprovider",
		telemetryfields.FieldNetworkPluginName, nodePortPluginSlug,
		telemetryfields.FieldPluginSlug, nodePortPluginSlug,
	)
	if pod != nil {
		logger = logger.WithValues(
			telemetryfields.FieldGameServerNamespace, pod.GetNamespace(),
			telemetryfields.FieldGameServerName, pod.GetName(),
		)
		if nodeName := pod.Spec.NodeName; nodeName != "" {
			logger = logger.WithValues(telemetryfields.FieldK8sNodeName, nodeName)
		}
		if gss := pod.GetLabels()[gamekruiseiov1alpha1.GameServerOwnerGssKey]; gss != "" {
			logger = logger.WithValues(
				telemetryfields.FieldGameServerSetNamespace, pod.GetNamespace(),
				telemetryfields.FieldGameServerSetName, gss,
			)
		}
	}
	return logger
}
