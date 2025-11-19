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
	"go.opentelemetry.io/otel/attribute"
)

// Standardized attribute/log field names for GameServer and networking metadata.
const (
	FieldGameServerSetName      = "game.kruise.io.game_server_set.name"
	FieldGameServerSetNamespace = "game.kruise.io.game_server_set.namespace"
	FieldGameServerName         = "game.kruise.io.game_server.name"
	FieldGameServerNamespace    = "game.kruise.io.game_server.namespace"
	FieldNetworkPluginName      = "game.kruise.io.network.plugin.name"
	FieldNetworkStatus          = "game.kruise.io.network.status"
	FieldNetworkResourceID      = "game.kruise.io.network.resource_id"
	FieldNetworkPortRange       = "game.kruise.io.network.port_range"
	FieldComponent              = "game.kruise.io.component"
	FieldWebhookHandler         = "game.kruise.io.webhook.handler"
	FieldAdmissionRequestUID    = "k8s.admission.request.uid"
)

// Standardized logger field names to avoid ad-hoc magic strings.
const (
	FieldAdmissionOperation           = "admission.operation"
	FieldAdmissionResource            = "k8s.admission.resource"
	FieldAllocatedPods                = "game.kruise.io.host_port.allocated_pods"
	FieldAllowNotReadyContainers      = "game.kruise.io.network.allow_not_ready_containers"
	FieldAnnotations                  = "k8s.annotations"
	FieldBlockPorts                   = "game.kruise.io.network.block_ports"
	FieldBody                         = "game.kruise.io.request.body"
	FieldCollector                    = "observability.collector"
	FieldConfigEntries                = "game.kruise.io.config.entries"
	FieldContext                      = "context"
	FieldCount                        = "game.kruise.io.count"
	FieldCurrent                      = "game.kruise.io.current"
	FieldCurrentHash                  = "game.kruise.io.hash.current"
	FieldCurrentReplicas              = "game.kruise.io.replicas.current"
	FieldDataDirectory                = "game.kruise.io.fs.data_directory"
	FieldDesired                      = "game.kruise.io.desired"
	FieldDesiredPorts                 = "game.kruise.io.network.desired_ports"
	FieldDirectory                    = "game.kruise.io.fs.directory"
	FieldError                        = "error"
	FieldEvent                        = "game.kruise.io.event"
	FieldExpectedHash                 = "game.kruise.io.hash.expected"
	FieldExpectedReplicas             = "game.kruise.io.replicas.expected"
	FieldExternalAddresses            = "game.kruise.io.network.external_addresses"
	FieldExternalPorts                = "game.kruise.io.network.external_ports"
	FieldFile                         = "game.kruise.io.fs.file"
	FieldGeneration                   = "game.kruise.io.generation"
	FieldHash                         = "game.kruise.io.hash"
	FieldHashNew                      = "game.kruise.io.hash.new"
	FieldHashOld                      = "game.kruise.io.hash.old"
	FieldHostPorts                    = "game.kruise.io.host_port.ports"
	FieldInternalAddresses            = "game.kruise.io.network.internal_addresses"
	FieldInternalPorts                = "game.kruise.io.network.internal_ports"
	FieldIteration                    = "game.kruise.io.test.iteration"
	FieldKeys                         = "game.kruise.io.keys"
	FieldK8sNamespaceName             = "k8s.namespace.name"
	FieldK8sPodName                   = "k8s.pod.name"
	FieldLBID                         = "game.kruise.io.network.lb_id"
	FieldManagedPods                  = "game.kruise.io.managed_pods"
	FieldMaxPort                      = "game.kruise.io.network.max_port"
	FieldMessage                      = "game.kruise.io.message"
	FieldMinPort                      = "game.kruise.io.network.min_port"
	FieldMode                         = "game.kruise.io.fs.mode"
	FieldNetworkDisabled              = "game.kruise.io.network.disabled"
	FieldNetworkState                 = "game.kruise.io.network.state"
	FieldNetworkType                  = "game.kruise.io.network.type"
	FieldNetworkTypeAnnotationPresent = "game.kruise.io.network.type_annotation_present"
	FieldNewManageIDs                 = "game.kruise.io.gameserver_set.manage_ids.new"
	FieldNewReserveIDs                = "game.kruise.io.gameserver_set.reserve_ids.new"
	FieldNewUID                       = "game.kruise.io.uid.new"
	FieldNodeIP                       = "k8s.node.ip"
	FieldNodeNameQualified            = "k8s.node.name"
	FieldObservedGeneration           = "game.kruise.io.observed_generation"
	FieldOldUID                       = "game.kruise.io.uid.old"
	FieldOperation                    = "game.kruise.io.operation"
	FieldPath                         = "game.kruise.io.fs.path"
	FieldPaths                        = "game.kruise.io.fs.paths"
	FieldPluginAlias                  = "game.kruise.io.plugin.alias"
	FieldPluginOperation              = "game.kruise.io.plugin.operation"
	FieldPlugins                      = "game.kruise.io.plugins"
	FieldPluginSlug                   = "game.kruise.io.plugin.slug"
	FieldPodAllocate                  = "game.kruise.io.network.pod_allocate_cache"
	FieldPodCount                     = "game.kruise.io.pod.count"
	FieldPodIP                        = "k8s.pod.ip"
	FieldPodKeyQualified              = "game.kruise.io.pod.key"
	FieldPodKeyLegacy                 = "game.kruise.io.pod.key_legacy"
	FieldPodTemplateRevision          = "game.kruise.io.pod_template.revision"
	FieldPort                         = "game.kruise.io.network.port"
	FieldPortCount                    = "game.kruise.io.network.port_count"
	FieldPortMax                      = "game.kruise.io.network.port.max"
	FieldPortMin                      = "game.kruise.io.network.port.min"
	FieldPorts                        = "game.kruise.io.network.ports"
	FieldPodProbeMarker               = "game.kruise.io.pod_probe_marker.name"
	FieldProvider                     = "cloud.provider"
	FieldReclaimPolicy                = "game.kruise.io.reclaim_policy"
	FieldRemaining                    = "game.kruise.io.remaining"
	FieldReplicas                     = "game.kruise.io.replicas"
	FieldRequestedPorts               = "game.kruise.io.network.requested_ports"
	FieldReserveIDsAnnotation         = "game.kruise.io.gameserver_set.reserve_ids.annotation"
	FieldReserveIDsImplicit           = "game.kruise.io.gameserver_set.reserve_ids.implicit"
	FieldReserveIDsSpec               = "game.kruise.io.gameserver_set.reserve_ids.spec"
	FieldSelectorAfter                = "game.kruise.io.nodeport.selector.after"
	FieldSelectorBefore               = "game.kruise.io.nodeport.selector.before"
	FieldService                      = "game.kruise.io.service.key"
	FieldServiceName                  = "service.name"
	FieldServiceNamespace             = "service.namespace"
	FieldServiceQualities             = "game.kruise.io.service_qualities.count"
	FieldServiceUID                   = "game.kruise.io.service.uid"
	FieldSpanID                       = "span_id"
	FieldSpanName                     = "span.name"
	FieldState                        = "game.kruise.io.state"
	FieldStrategyMaxUnavailable       = "game.kruise.io.strategy.max_unavailable"
	FieldStrategyScaleDown            = "game.kruise.io.strategy.scale_down"
	FieldTargetDirectory              = "game.kruise.io.fs.target_directory"
	FieldTraceID                      = "trace_id"
	FieldTraceparent                  = "game.kruise.io.traceparent"
	FieldTSDirectory                  = "game.kruise.io.fs.ts_directory"
)

var (
	gameServerSetNameKey      = attribute.Key(FieldGameServerSetName)
	gameServerSetNamespaceKey = attribute.Key(FieldGameServerSetNamespace)
	gameServerNameKey         = attribute.Key(FieldGameServerName)
	gameServerNamespaceKey    = attribute.Key(FieldGameServerNamespace)
	networkPluginKey          = attribute.Key(FieldNetworkPluginName)
	networkStatusKey          = attribute.Key(FieldNetworkStatus)
	networkResourceIDKey      = attribute.Key(FieldNetworkResourceID)
	networkPortRangeKey       = attribute.Key(FieldNetworkPortRange)
	componentKey              = attribute.Key(FieldComponent)
	webhookHandlerKey         = attribute.Key(FieldWebhookHandler)
	errorTypeKey              = attribute.Key("error.type")
	cloudProviderKey          = attribute.Key("cloud.provider")
	admissionRequestUIDKey    = attribute.Key(FieldAdmissionRequestUID)
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
				attribute.String("k8s.namespace.name", ns),
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
