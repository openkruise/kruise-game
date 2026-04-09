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

package kubernetes

import (
	"context"
	"fmt"
	"math"
	"net"
	"strconv"
	"strings"
	"sync"

	"github.com/go-logr/logr"
	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/cloudprovider"
	"github.com/openkruise/kruise-game/cloudprovider/errors"
	provideroptions "github.com/openkruise/kruise-game/cloudprovider/options"
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
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	HostPortNetwork = "Kubernetes-HostPort"
	// ContainerPortsKey represents the configuration key when using hostPort.
	// Its corresponding value format is as follows, containerName:port1/protocol1,port2/protocol2,... e.g. game-server:25565/TCP
	// When no protocol is specified, TCP is used by default
	ContainerPortsKey = "ContainerPorts"
	PortSameAsHost    = "SameAsHost"
	ProtocolTCPUDP    = "TCPUDP"

	hostPortComponentName = "okg-controller-manager"
	hostPortPluginSlug    = telemetryfields.NetworkPluginKubernetesHostPort
)

var (
	hostPortAttrPortsReusedKey       = attribute.Key("game.kruise.io.network.plugin.kubernetes.hostport.ports_reused")
	hostPortAttrAllocatedPortsKey    = attribute.Key("game.kruise.io.network.plugin.kubernetes.hostport.allocated_ports")
	hostPortAttrAllocatedCountKey    = attribute.Key("game.kruise.io.network.plugin.kubernetes.hostport.ports_allocated_count")
	hostPortAttrPortsRequestedKey    = attribute.Key("game.kruise.io.network.plugin.kubernetes.hostport.ports_requested")
	hostPortAttrContainersPatchedKey = attribute.Key("game.kruise.io.network.plugin.kubernetes.hostport.containers_patched")
	hostPortAttrPodKey               = attribute.Key("game.kruise.io.network.plugin.kubernetes.hostport.pod_key")
	hostPortAttrNodeIPKey            = attribute.Key("game.kruise.io.network.plugin.kubernetes.hostport.node_ip")
	hostPortAttrInternalPortCountKey = attribute.Key("game.kruise.io.network.plugin.kubernetes.hostport.internal_port_count")
	hostPortAttrExternalPortCountKey = attribute.Key("game.kruise.io.network.plugin.kubernetes.hostport.external_port_count")
	hostPortAttrReleasedPortsKey     = attribute.Key("game.kruise.io.network.plugin.kubernetes.hostport.released_ports")
	hostPortAttrReleasedCountKey     = attribute.Key("game.kruise.io.network.plugin.kubernetes.hostport.released_ports_count")
)

// HostPortPlugin implements the HostPort network plugin with port reuse support.
// Key design:
// 1. Ports are per-node resources in Kubernetes - the same port number can be used on different nodes
// 2. The Kubernetes scheduler handles per-node hostPort conflict detection
// 3. This plugin assigns port numbers using a least-used strategy for even distribution
// 4. A single mutex protects all state - the critical section is ~3μs (scan ports + update counters)
type HostPortPlugin struct {
	maxPort int32
	minPort int32

	mu        sync.Mutex
	portUsage []int32            // index: port-minPort, value: number of pods using this port
	podPorts  map[string][]int32 // podKey -> allocated ports
}

func init() {
	hostPortPlugin := &HostPortPlugin{}
	kubernetesProvider.registerPlugin(hostPortPlugin)
}

func (hpp *HostPortPlugin) Name() string {
	return HostPortNetwork
}

func (hpp *HostPortPlugin) Alias() string {
	return ""
}

// allocatePorts selects the least-used ports from the global port range.
// The same port number can be assigned to multiple pods - the Kubernetes scheduler
// ensures they land on different nodes (hostPort is a per-node resource).
func (hpp *HostPortPlugin) allocatePorts(num int, podKey string) ([]int32, error) {
	hpp.mu.Lock()
	defer hpp.mu.Unlock()

	// Idempotency: return existing allocation if present
	if existing, ok := hpp.podPorts[podKey]; ok {
		return existing, nil
	}

	totalPorts := int(hpp.maxPort - hpp.minPort + 1)
	if totalPorts < num {
		return nil, fmt.Errorf("insufficient port range: need %d ports but range only has %d", num, totalPorts)
	}

	// Select 'num' ports with the lowest usage count (least-used strategy)
	result := make([]int32, 0, num)
	selected := make(map[int32]bool, num)
	for len(result) < num {
		minUsage := int32(math.MaxInt32)
		bestPort := hpp.minPort
		for p := hpp.minPort; p <= hpp.maxPort; p++ {
			usage := hpp.portUsage[p-hpp.minPort]
			if !selected[p] && usage < minUsage {
				minUsage = usage
				bestPort = p
			}
		}
		result = append(result, bestPort)
		selected[bestPort] = true
		hpp.portUsage[bestPort-hpp.minPort]++
	}

	hpp.podPorts[podKey] = result
	return result, nil
}

// deallocatePorts releases the ports allocated to a pod, decrementing usage counters.
func (hpp *HostPortPlugin) deallocatePorts(podKey string) []int32 {
	hpp.mu.Lock()
	defer hpp.mu.Unlock()

	ports, ok := hpp.podPorts[podKey]
	if !ok {
		return nil
	}
	for _, port := range ports {
		idx := port - hpp.minPort
		if idx >= 0 && idx < int32(len(hpp.portUsage)) && hpp.portUsage[idx] > 0 {
			hpp.portUsage[idx]--
		}
	}
	delete(hpp.podPorts, podKey)
	return ports
}

func (hpp *HostPortPlugin) OnPodAdded(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, errors.PluginError) {
	tracer := otel.Tracer("okg-controller-manager")
	ctx, span := startHostPortSpan(ctx, tracer, tracing.SpanPrepareHostPortPod, pod)
	finalNetworkStatus := telemetryfields.NetworkStatusWaiting
	var finalErr error
	var finalErrorType string
	finalStatus := codes.Ok
	finalMessage := "pod configured with host ports successfully"
	defer func() {
		if r := recover(); r != nil {
			finalErr = fmt.Errorf("panic: %v", r)
			finalNetworkStatus = telemetryfields.NetworkStatusError
			finalErrorType = telemetryfields.ErrorTypeInternal
			finalStatus = codes.Error
			finalMessage = fmt.Sprintf("panic: %v", r)
		}
		if finalErr != nil {
			span.RecordError(finalErr)
			finalStatus = codes.Error
			if finalMessage == "pod configured with host ports successfully" {
				finalMessage = finalErr.Error()
			}
			if finalErrorType != "" {
				span.SetAttributes(tracing.AttrErrorType(finalErrorType))
			}
		}
		span.SetAttributes(tracing.AttrNetworkStatus(finalNetworkStatus))
		span.SetStatus(finalStatus, finalMessage)
		span.End()
	}()
	podKey := pod.GetNamespace() + "/" + pod.GetName()

	span.SetAttributes(
		tracing.AttrNetworkStatus(telemetryfields.NetworkStatusWaiting),
		hostPortAttrPodKey.String(podKey),
		hostPortAttrPortsReusedKey.Bool(false),
	)

	logger := hostPortLogger(ctx, pod).WithValues(telemetryfields.FieldOperation, "add")
	logger.Info("Handling hostport pod ADD operation")
	podNow := &corev1.Pod{}
	err := c.Get(ctx, types.NamespacedName{
		Namespace: pod.GetNamespace(),
		Name:      pod.GetName(),
	}, podNow)
	if err == nil {
		dupErr := fmt.Errorf("pod %s/%s already exists", pod.GetNamespace(), pod.GetName())
		logger.Error(dupErr, "Pod already exists, skipping hostport allocation")
		span.RecordError(dupErr)
		span.SetStatus(codes.Error, "pod with same name already exists")
		span.SetAttributes(tracing.AttrErrorType(telemetryfields.ErrorTypeParameter))
		finalErr = dupErr
		finalErrorType = telemetryfields.ErrorTypeParameter
		return pod, errors.NewPluginError(errors.InternalError, "There is a pod with same ns/name exists in cluster")
	}
	if !k8serrors.IsNotFound(err) {
		logger.Error(err, "Failed to check existing pod before hostport allocation")
		span.RecordError(err)
		span.SetAttributes(tracing.AttrErrorType(telemetryfields.ErrorTypeAPICall))
		span.SetStatus(codes.Error, "failed to check for existing pod")
		finalErr = err
		finalErrorType = telemetryfields.ErrorTypeAPICall
		finalNetworkStatus = telemetryfields.NetworkStatusError
		return pod, errors.NewPluginError(errors.ApiCallError, err.Error())
	}

	networkManager := utils.NewNetworkManager(pod, c)
	conf := networkManager.GetNetworkConfig()
	containerPortsMap, containerProtocolsMap, numToAlloc := parseConfig(conf, pod)
	span.AddEvent(tracing.EventNetworkHostPortConfigParsed)
	requestedPorts := numToAlloc

	var hostPorts []int32
	// Check for existing allocation (idempotency) - handled inside allocatePorts

	if len(hostPorts) == 0 {
		_, allocSpan := startHostPortSpan(ctx, tracer, tracing.SpanAllocateHostPort, pod,
			hostPortAttrPortsRequestedKey.Int64(int64(requestedPorts)),
			hostPortAttrPodKey.String(podKey),
		)
		var allocErr error
		hostPorts, allocErr = hpp.allocatePorts(numToAlloc, podKey)
		if allocErr != nil {
			logger.Error(allocErr, "Failed to allocate hostPorts")
			span.RecordError(allocErr)
			span.SetAttributes(tracing.AttrErrorType(telemetryfields.ErrorTypePortExhausted))
			span.SetStatus(codes.Error, "failed to allocate host ports")
			finalErr = allocErr
			finalErrorType = telemetryfields.ErrorTypePortExhausted
			finalNetworkStatus = telemetryfields.NetworkStatusError
			allocSpan.SetStatus(codes.Error, allocErr.Error())
			allocSpan.End()
			return pod, errors.NewPluginError(errors.InternalError, allocErr.Error())
		}
		logger.Info("Allocated hostPorts for pod", telemetryfields.FieldHostPorts, hostPorts, telemetryfields.FieldRequestedPorts, requestedPorts)
		allocSpan.SetAttributes(
			hostPortAttrAllocatedPortsKey.String(util.Int32SliceToString(hostPorts, ",")),
			hostPortAttrAllocatedCountKey.Int64(int64(len(hostPorts))),
		)
		span.AddEvent(tracing.EventNetworkHostPortPortsAllocated, trace.WithAttributes(hostPortAttrAllocatedPortsKey.String(util.Int32SliceToString(hostPorts, ",")), hostPortAttrAllocatedCountKey.Int64(int64(len(hostPorts)))))
		allocSpan.SetStatus(codes.Ok, "ports allocated successfully")
		allocSpan.End()
	}

	containers := pod.Spec.Containers
	for cIndex, container := range pod.Spec.Containers {
		if ports, ok := containerPortsMap[container.Name]; ok {
			containerPorts := container.Ports
			for i, port := range ports {
				if port == -1 {
					port = hostPorts[numToAlloc-1]
				}
				protocol := containerProtocolsMap[container.Name][i]
				hostPort := hostPorts[numToAlloc-1]
				if protocol == ProtocolTCPUDP {
					containerPorts = append(containerPorts,
						corev1.ContainerPort{
							ContainerPort: port,
							HostPort:      hostPort,
							Protocol:      corev1.ProtocolTCP,
						}, corev1.ContainerPort{
							ContainerPort: port,
							HostPort:      hostPort,
							Protocol:      corev1.ProtocolUDP,
						})
				} else {
					containerPorts = append(containerPorts, corev1.ContainerPort{
						ContainerPort: port,
						HostPort:      hostPort,
						Protocol:      protocol,
					})
				}
				numToAlloc--
			}
			containers[cIndex].Ports = containerPorts
		}
	}
	pod.Spec.Containers = containers

	span.SetAttributes(
		hostPortAttrContainersPatchedKey.Int64(int64(len(containerPortsMap))),
		hostPortAttrAllocatedPortsKey.String(util.Int32SliceToString(hostPorts, ",")),
		hostPortAttrAllocatedCountKey.Int64(int64(len(hostPorts))),
	)
	span.AddEvent(tracing.EventNetworkHostPortContainersPatched, trace.WithAttributes(hostPortAttrContainersPatchedKey.Int64(int64(len(containerPortsMap))), hostPortAttrAllocatedCountKey.Int64(int64(len(hostPorts)))))
	finalNetworkStatus = telemetryfields.NetworkStatusNotReady

	return pod, nil
}

func (hpp *HostPortPlugin) OnPodUpdated(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, errors.PluginError) {
	tracer := otel.Tracer("okg-controller-manager")
	ctx, span := startHostPortSpan(ctx, tracer, tracing.SpanProcessHostPortUpdate, pod)
	finalNetworkStatus := telemetryfields.NetworkStatusWaiting
	var finalErr error
	var finalErrorType string
	finalStatus := codes.Ok
	finalMessage := "hostport pod processed"
	defer func() {
		if r := recover(); r != nil {
			finalErr = fmt.Errorf("panic: %v", r)
			finalNetworkStatus = telemetryfields.NetworkStatusError
			finalErrorType = telemetryfields.ErrorTypeInternal
			finalStatus = codes.Error
			finalMessage = fmt.Sprintf("panic: %v", r)
		}
		if finalErr != nil {
			span.RecordError(finalErr)
			finalStatus = codes.Error
			if finalMessage == "hostport pod processed" {
				finalMessage = finalErr.Error()
			}
			if finalErrorType != "" {
				span.SetAttributes(tracing.AttrErrorType(finalErrorType))
			}
		}
		span.SetAttributes(tracing.AttrNetworkStatus(finalNetworkStatus))
		span.SetStatus(finalStatus, finalMessage)
		span.End()
	}()
	span.SetAttributes(hostPortAttrPodKey.String(pod.GetNamespace() + "/" + pod.GetName()))

	logger := hostPortLogger(ctx, pod).WithValues(telemetryfields.FieldOperation, "update")
	logger.Info("Processing hostport pod UPDATE operation")
	node := &corev1.Node{}
	err := c.Get(ctx, types.NamespacedName{
		Name: pod.Spec.NodeName,
	}, node)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Error(err, "Node not found for hostport pod", telemetryfields.FieldK8sNodeName, pod.Spec.NodeName)
			span.RecordError(err)
			finalNetworkStatus = telemetryfields.NetworkStatusNotReady
			finalErrorType = telemetryfields.ErrorTypeResourceNotReady
			finalErr = err
			span.SetAttributes(
				tracing.AttrNetworkStatus(finalNetworkStatus),
				tracing.AttrErrorType(finalErrorType),
			)
			return pod, nil
		}
		logger.Error(err, "Failed to fetch node for hostport pod", telemetryfields.FieldK8sNodeName, pod.Spec.NodeName)
		span.RecordError(err)
		finalErr = err
		finalNetworkStatus = telemetryfields.NetworkStatusError
		finalErrorType = telemetryfields.ErrorTypeAPICall
		span.SetAttributes(tracing.AttrErrorType(finalErrorType))
		return pod, errors.NewPluginError(errors.ApiCallError, err.Error())
	}
	nodeIp := getAddress(node)
	span.SetAttributes(hostPortAttrNodeIPKey.String(nodeIp))

	networkManager := utils.NewNetworkManager(pod, c)

	iNetworkPorts := make([]gamekruiseiov1alpha1.NetworkPort, 0)
	eNetworkPorts := make([]gamekruiseiov1alpha1.NetworkPort, 0)
	for _, container := range pod.Spec.Containers {
		for _, port := range container.Ports {
			if port.HostPort >= hpp.minPort && port.HostPort <= hpp.maxPort {
				containerPortIs := intstr.FromInt(int(port.ContainerPort))
				hostPortIs := intstr.FromInt(int(port.HostPort))
				iNetworkPorts = append(iNetworkPorts, gamekruiseiov1alpha1.NetworkPort{
					Name:     container.Name + "-" + containerPortIs.String(),
					Port:     &containerPortIs,
					Protocol: port.Protocol,
				})
				eNetworkPorts = append(eNetworkPorts, gamekruiseiov1alpha1.NetworkPort{
					Name:     container.Name + "-" + containerPortIs.String(),
					Port:     &hostPortIs,
					Protocol: port.Protocol,
				})
			}
		}
	}

	if len(iNetworkPorts) == 0 || len(eNetworkPorts) == 0 || pod.Status.PodIP == "" {
		errNetworkNotReady := fmt.Errorf("pod ip or hostports missing")
		logger.Error(errNetworkNotReady, "HostPort network not ready", telemetryfields.FieldInternalPorts, len(iNetworkPorts), telemetryfields.FieldExternalPorts, len(eNetworkPorts), telemetryfields.FieldPodIP, pod.Status.PodIP)
		finalNetworkStatus = telemetryfields.NetworkStatusNotReady
		finalErr = errNetworkNotReady
		finalErrorType = telemetryfields.ErrorTypeResourceNotReady
		span.SetAttributes(
			tracing.AttrNetworkStatus(finalNetworkStatus),
			attribute.Int(telemetryfields.FieldInternalPorts, len(iNetworkPorts)),
			attribute.Int(telemetryfields.FieldExternalPorts, len(eNetworkPorts)),
			hostPortAttrInternalPortCountKey.Int64(int64(len(iNetworkPorts))),
			hostPortAttrExternalPortCountKey.Int64(int64(len(eNetworkPorts))),
			attribute.String(telemetryfields.FieldPodIP, pod.Status.PodIP),
			tracing.AttrErrorType(finalErrorType),
		)
		span.RecordError(errNetworkNotReady)
		finalStatus = codes.Error
		finalMessage = "network not ready"
		pod, err := networkManager.UpdateNetworkStatus(gamekruiseiov1alpha1.NetworkStatus{
			CurrentNetworkState: gamekruiseiov1alpha1.NetworkNotReady,
		}, pod)
		return pod, errors.ToPluginError(err, errors.InternalError)
	}

	networkStatus := gamekruiseiov1alpha1.NetworkStatus{
		InternalAddresses: []gamekruiseiov1alpha1.NetworkAddress{
			{
				IP:    pod.Status.PodIP,
				Ports: iNetworkPorts,
			},
		},
		ExternalAddresses: []gamekruiseiov1alpha1.NetworkAddress{
			{
				IP:    nodeIp,
				Ports: eNetworkPorts,
			},
		},
		CurrentNetworkState: gamekruiseiov1alpha1.NetworkReady,
	}

	span.SetAttributes(
		tracing.AttrNetworkStatus(telemetryfields.NetworkStatusReady),
		attribute.String(telemetryfields.FieldNodeIP, nodeIp),
		hostPortAttrNodeIPKey.String(nodeIp),
		attribute.Int(telemetryfields.FieldInternalPorts, len(iNetworkPorts)),
		attribute.Int(telemetryfields.FieldExternalPorts, len(eNetworkPorts)),
		hostPortAttrInternalPortCountKey.Int64(int64(len(iNetworkPorts))),
		hostPortAttrExternalPortCountKey.Int64(int64(len(eNetworkPorts))),
	)
	finalNetworkStatus = telemetryfields.NetworkStatusReady
	finalStatus = codes.Ok
	finalMessage = "network ready"
	span.AddEvent(tracing.EventNetworkHostPortStatusPublished, trace.WithAttributes(hostPortAttrNodeIPKey.String(nodeIp), hostPortAttrInternalPortCountKey.Int64(int64(len(iNetworkPorts))), hostPortAttrExternalPortCountKey.Int64(int64(len(eNetworkPorts)))))
	logger.Info("Updated hostport network status", telemetryfields.FieldNodeIP, nodeIp, telemetryfields.FieldInternalPorts, len(iNetworkPorts), telemetryfields.FieldExternalPorts, len(eNetworkPorts))

	pod, err = networkManager.UpdateNetworkStatus(networkStatus, pod)
	return pod, errors.ToPluginError(err, errors.InternalError)
}

func (hpp *HostPortPlugin) OnPodDeleted(c client.Client, pod *corev1.Pod, ctx context.Context) errors.PluginError {
	logger := hostPortLogger(ctx, pod).WithValues(telemetryfields.FieldOperation, "delete")
	logger.Info("Processing hostport pod DELETE operation")
	tracer := otel.Tracer("okg-controller-manager")
	_, span := startHostPortSpan(ctx, tracer, tracing.SpanCleanupHostPortAllocation, pod,
		tracing.AttrNetworkStatus(telemetryfields.NetworkStatusNotReady),
	)
	finalNetworkStatus := telemetryfields.NetworkStatusNotReady
	var finalErr error
	var finalErrorType string
	finalStatus := codes.Ok
	finalMessage := "hostport allocation cleaned up"
	defer func() {
		if r := recover(); r != nil {
			finalErr = fmt.Errorf("panic: %v", r)
			finalNetworkStatus = telemetryfields.NetworkStatusError
			finalErrorType = telemetryfields.ErrorTypeInternal
			finalStatus = codes.Error
			finalMessage = fmt.Sprintf("panic: %v", r)
		}
		if finalErr != nil {
			span.RecordError(finalErr)
			finalStatus = codes.Error
			if finalMessage == "hostport allocation cleaned up" {
				finalMessage = finalErr.Error()
			}
			if finalErrorType != "" {
				span.SetAttributes(tracing.AttrErrorType(finalErrorType))
			}
		}
		span.SetAttributes(tracing.AttrNetworkStatus(finalNetworkStatus))
		span.SetStatus(finalStatus, finalMessage)
		span.End()
	}()
	podKey := pod.GetNamespace() + "/" + pod.GetName()
	span.SetAttributes(hostPortAttrPodKey.String(podKey))

	// Check if we have allocation for this pod
	hpp.mu.Lock()
	_, hasAllocation := hpp.podPorts[podKey]
	hpp.mu.Unlock()

	if !hasAllocation {
		logger.V(4).Info("No hostport allocation found for pod")
		finalStatus = codes.Ok
		finalMessage = "no hostport allocation found"
		return nil
	}

	hostPorts := hpp.deallocatePorts(podKey)
	logger.Info("Released hostPorts for pod", telemetryfields.FieldHostPorts, hostPorts)
	span.AddEvent(tracing.EventNetworkHostPortReleased, trace.WithAttributes(hostPortAttrReleasedCountKey.Int64(int64(len(hostPorts))), hostPortAttrReleasedPortsKey.String(util.Int32SliceToString(hostPorts, ","))))
	span.SetAttributes(
		hostPortAttrReleasedCountKey.Int64(int64(len(hostPorts))),
		hostPortAttrReleasedPortsKey.String(util.Int32SliceToString(hostPorts, ",")),
	)
	finalMessage = "hostport allocation cleaned up"
	finalStatus = codes.Ok
	return nil
}

func (hpp *HostPortPlugin) Init(c client.Client, options cloudprovider.CloudProviderOptions, ctx context.Context) error {
	hostPortOptions := options.(provideroptions.KubernetesOptions).HostPort
	hpp.maxPort = hostPortOptions.MaxPort
	hpp.minPort = hostPortOptions.MinPort

	totalPorts := hpp.maxPort - hpp.minPort + 1

	// Initialize port usage counters (all zeros)
	hpp.portUsage = make([]int32, totalPorts)
	hpp.podPorts = make(map[string][]int32)

	// Recover state from existing pods
	podList := &corev1.PodList{}
	err := c.List(ctx, podList)
	if err != nil {
		return err
	}

	for _, pod := range podList.Items {
		if pod.GetAnnotations()[gamekruiseiov1alpha1.GameServerNetworkType] != HostPortNetwork {
			continue
		}
		var hostPorts []int32
		for _, container := range pod.Spec.Containers {
			for _, port := range container.Ports {
				if port.HostPort >= hpp.minPort && port.HostPort <= hpp.maxPort {
					idx := port.HostPort - hpp.minPort
					if idx >= 0 && idx < int32(len(hpp.portUsage)) {
						hpp.portUsage[idx]++
					}
					hostPorts = append(hostPorts, port.HostPort)
				}
			}
		}
		if len(hostPorts) != 0 {
			podKey := pod.GetNamespace() + "/" + pod.GetName()
			hpp.podPorts[podKey] = hostPorts
		}
	}

	logger := hostPortLogger(ctx, nil).WithValues(
		telemetryfields.FieldOperation, "init",
		telemetryfields.FieldPortMin, hpp.minPort,
		telemetryfields.FieldPortMax, hpp.maxPort,
	)
	logger.Info("Initialized hostport plugin with port-reuse support", telemetryfields.FieldAllocatedPods, len(hpp.podPorts))
	return nil
}

func hostPortLogger(ctx context.Context, pod *corev1.Pod) logr.Logger {
	logger := logging.FromContextWithTrace(ctx).WithValues(
		telemetryfields.FieldComponent, "cloudprovider",
		telemetryfields.FieldNetworkPluginName, hostPortPluginSlug,
		telemetryfields.FieldPluginSlug, hostPortPluginSlug,
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

func hostPortSpanAttrs(pod *corev1.Pod, extras ...attribute.KeyValue) []attribute.KeyValue {
	attrExtras := []attribute.KeyValue{
		tracing.AttrCloudProvider(tracing.CloudProviderKubernetes),
	}
	if pod != nil && pod.Spec.NodeName != "" {
		attrExtras = append(attrExtras, tracing.AttrK8sNodeName(pod.Spec.NodeName))
	}
	attrExtras = append(attrExtras, extras...)
	attrExtras = tracing.EnsureNetworkStatusAttr(attrExtras, telemetryfields.NetworkStatusWaiting)
	return tracing.BaseNetworkAttrs(hostPortComponentName, hostPortPluginSlug, pod, attrExtras...)
}

func startHostPortSpan(ctx context.Context, tracer trace.Tracer, name string, pod *corev1.Pod, extras ...attribute.KeyValue) (context.Context, trace.Span) {
	return tracer.Start(ctx, name,
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(hostPortSpanAttrs(pod, extras...)...),
	)
}

func verifyContainerName(containerName string, pod *corev1.Pod) bool {
	for _, container := range pod.Spec.Containers {
		if container.Name == containerName {
			return true
		}
	}
	return false
}

func getAddress(node *corev1.Node) string {
	nodeIp := ""

	for _, a := range node.Status.Addresses {
		if a.Type == corev1.NodeExternalIP && net.ParseIP(a.Address) != nil {
			nodeIp = a.Address
		}
	}

	for _, a := range node.Status.Addresses {
		if a.Type == corev1.NodeExternalDNS {
			nodeIp = a.Address
		}
	}

	if nodeIp == "" {
		for _, a := range node.Status.Addresses {
			if a.Type == corev1.NodeInternalIP && net.ParseIP(a.Address) != nil {
				nodeIp = a.Address
			}
		}

		for _, a := range node.Status.Addresses {
			if a.Type == corev1.NodeInternalDNS {
				nodeIp = a.Address
			}
		}
	}

	return nodeIp
}

func parseConfig(conf []gamekruiseiov1alpha1.NetworkConfParams, pod *corev1.Pod) (map[string][]int32, map[string][]corev1.Protocol, int) {
	numToAlloc := 0
	containerPortsMap := make(map[string][]int32)
	containerProtocolsMap := make(map[string][]corev1.Protocol)
	for _, c := range conf {
		if c.Name == ContainerPortsKey {
			cpSlice := strings.Split(c.Value, ":")
			containerName := cpSlice[0]
			if verifyContainerName(containerName, pod) && len(cpSlice) == 2 {
				ports := make([]int32, 0)
				protocols := make([]corev1.Protocol, 0)
				for _, portString := range strings.Split(cpSlice[1], ",") {
					ppSlice := strings.Split(portString, "/")
					var port int64
					var err error
					if ppSlice[0] == PortSameAsHost {
						port = -1
					} else {
						port, err = strconv.ParseInt(ppSlice[0], 10, 32)
						if err != nil {
							continue
						}
					}
					numToAlloc++
					ports = append(ports, int32(port))
					if len(ppSlice) == 2 {
						protocols = append(protocols, corev1.Protocol(ppSlice[1]))
					} else {
						protocols = append(protocols, corev1.ProtocolTCP)
					}
				}
				containerPortsMap[containerName] = ports
				containerProtocolsMap[containerName] = protocols
			}
		}
	}
	return containerPortsMap, containerProtocolsMap, numToAlloc
}
