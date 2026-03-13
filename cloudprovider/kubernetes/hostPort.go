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

	// Shard configuration constants
	DefaultShardCount = 1 // Default to 1 for backward compatibility
	MinShardCount     = 1
	MaxShardCount     = 128
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
	hostPortAttrShardIDKey           = attribute.Key("game.kruise.io.network.plugin.kubernetes.hostport.shard_id")
)

type HostPortPlugin struct {
	// Global configuration (read-only after init)
	maxPort    int32
	minPort    int32
	shardCount int
	shardMask  int32 // shardCount - 1, for fast modulo using &

	// Sharded port allocation state
	shards []*PortShard
}

// PortShard represents an independent port allocation unit with its own lock
type PortShard struct {
	mutex sync.RWMutex

	// Shard identifier
	id int

	// Port range for this shard
	minPort int32
	maxPort int32

	// Port allocation state
	podAllocated map[string]string // podKey -> "port1,port2,..."
	portAmount   map[int32]int     // port -> usage count
	amountStat   []int             // usage count statistics
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

// getShard returns the shard for a given pod using GameServerSet name as the sharding key
func (hpp *HostPortPlugin) getShard(pod *corev1.Pod) *PortShard {
	gssName := pod.GetLabels()[gamekruiseiov1alpha1.GameServerOwnerGssKey]
	if gssName == "" {
		// Fallback to pod name if no GSS label
		gssName = pod.GetName()
	}
	return hpp.getShardByKey(gssName)
}

// getShardByKey returns the shard for a given sharding key using FNV-1a hash
func (hpp *HostPortPlugin) getShardByKey(key string) *PortShard {
	hash := fnv32a(key)
	shardIndex := int(hash & hpp.shardMask)
	return hpp.shards[shardIndex]
}

// fnv32a implements FNV-1a 32-bit hash for even distribution
func fnv32a(s string) int32 {
	h := uint32(2166136261)
	for _, c := range s {
		h ^= uint32(c)
		h *= 16777619
	}
	return int32(h)
}

// clamp returns value clamped to [min, max] range
func clamp(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// determineShardCount calculates the optimal shard count based on configuration
func determineShardCount(opts provideroptions.HostPortOptions) int {
	// From config
	if opts.ShardCount > 0 {
		return clamp(opts.ShardCount, MinShardCount, MaxShardCount)
	}
	// Default to 1 for backward compatibility
	return DefaultShardCount
}

// newPortShard creates a new PortShard with the given port range
func newPortShard(id int, minPort, maxPort int32) *PortShard {
	shard := &PortShard{
		id:           id,
		minPort:      minPort,
		maxPort:      maxPort,
		podAllocated: make(map[string]string),
		portAmount:   make(map[int32]int),
	}

	// Initialize port amount map
	for port := minPort; port <= maxPort; port++ {
		shard.portAmount[port] = 0
	}
	shard.amountStat = make([]int, 1)
	shard.amountStat[0] = int(maxPort - minPort + 1)

	return shard
}

// allocate assigns ports from the shard
func (s *PortShard) allocate(num int, podKey string) []int32 {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	hostPorts, index := selectPorts(s.amountStat, s.portAmount, num)
	for _, hostPort := range hostPorts {
		s.portAmount[hostPort]++
		s.amountStat[index]--
		if index+1 >= len(s.amountStat) {
			s.amountStat = append(s.amountStat, 0)
		}
		s.amountStat[index+1]++
	}

	s.podAllocated[podKey] = util.Int32SliceToString(hostPorts, ",")
	return hostPorts
}

// getAllocatedPorts returns previously allocated ports for a pod (read-only)
func (s *PortShard) getAllocatedPorts(podKey string) ([]int32, bool) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	if str, ok := s.podAllocated[podKey]; ok {
		return util.StringToInt32Slice(str, ","), true
	}
	return nil, false
}

// deallocate releases ports from the shard
func (s *PortShard) deallocate(hostPorts []int32, podKey string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	for _, hostPort := range hostPorts {
		if hostPort < s.minPort || hostPort > s.maxPort {
			continue
		}
		amount := s.portAmount[hostPort]
		s.portAmount[hostPort]--
		if amount < len(s.amountStat) {
			s.amountStat[amount]--
		}
		if amount > 0 && amount-1 < len(s.amountStat) {
			s.amountStat[amount-1]++
		}
	}

	delete(s.podAllocated, podKey)
}

// hasAllocation checks if the shard has an allocation for the given pod key
func (s *PortShard) hasAllocation(podKey string) bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	_, ok := s.podAllocated[podKey]
	return ok
}

// getPortRange returns the port range for this shard
func (s *PortShard) getPortRange() (int32, int32) {
	return s.minPort, s.maxPort
}

// getAllocatedPodCount returns the number of pods with allocations in this shard
func (s *PortShard) getAllocatedPodCount() int {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return len(s.podAllocated)
}

// recoverState recovers shard state from existing pods
func (s *PortShard) recoverState(pods []corev1.Pod) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	for _, pod := range pods {
		var hostPorts []int32
		for _, container := range pod.Spec.Containers {
			for _, port := range container.Ports {
				if port.HostPort >= s.minPort && port.HostPort <= s.maxPort {
					s.portAmount[port.HostPort]++
					hostPorts = append(hostPorts, port.HostPort)
				}
			}
		}
		if len(hostPorts) != 0 {
			podKey := pod.GetNamespace() + "/" + pod.GetName()
			s.podAllocated[podKey] = util.Int32SliceToString(hostPorts, ",")
		}
	}

	// Rebuild amountStat
	maxAmount := 0
	for _, amount := range s.portAmount {
		if amount > maxAmount {
			maxAmount = amount
		}
	}
	s.amountStat = make([]int, maxAmount+1)
	for _, amount := range s.portAmount {
		s.amountStat[amount]++
	}
}

func (hpp *HostPortPlugin) OnPodAdded(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, errors.PluginError) {
	// Create root span for HostPort OnPodAdded
	tracer := otel.Tracer("okg-controller-manager")
	ctx, span := startHostPortSpan(ctx, tracer, tracing.SpanPrepareHostPortPod, pod)
	// track final network status and error to ensure a single final attribute is set on the parent span
	finalNetworkStatus := telemetryfields.NetworkStatusWaiting
	var finalErr error
	var finalErrorType string
	finalStatus := codes.Ok
	finalMessage := "pod configured with host ports successfully"
	defer func() {
		// Handle panic first so the span records it before finalization.
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

	// Get the shard for this pod
	shard := hpp.getShard(pod)
	span.SetAttributes(
		tracing.AttrNetworkStatus(telemetryfields.NetworkStatusWaiting),
		hostPortAttrPodKey.String(podKey),
		hostPortAttrPortsReusedKey.Bool(false),
		hostPortAttrShardIDKey.Int(shard.id),
	)

	logger := hostPortLogger(ctx, pod).WithValues(telemetryfields.FieldOperation, "add", "shard_id", shard.id)
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
		span.SetAttributes(
			tracing.AttrErrorType(telemetryfields.ErrorTypeParameter),
		)
		finalErr = dupErr
		finalErrorType = telemetryfields.ErrorTypeParameter
		// parameter conflict (duplicate pod) is not an indication of network error -> keep parent neutral/waiting
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
	// add config parsed event
	span.AddEvent(tracing.EventNetworkHostPortConfigParsed)
	requestedPorts := numToAlloc

	var hostPorts []int32
	// Check for existing allocation in shard (idempotency)
	if ports, ok := shard.getAllocatedPorts(podKey); ok {
		hostPorts = ports
		logger.Info("Reusing previously allocated hostPorts", telemetryfields.FieldHostPorts, hostPorts, telemetryfields.FieldRequestedPorts, requestedPorts)
		span.SetAttributes(
			hostPortAttrPortsReusedKey.Bool(true),
			hostPortAttrAllocatedPortsKey.String(util.Int32SliceToString(hostPorts, ",")),
			hostPortAttrAllocatedCountKey.Int64(int64(len(hostPorts))),
		)
		// mark reused event
		span.AddEvent(tracing.EventNetworkHostPortPortsReused, trace.WithAttributes(hostPortAttrAllocatedPortsKey.String(util.Int32SliceToString(hostPorts, ",")), hostPortAttrAllocatedCountKey.Int64(int64(len(hostPorts)))))
	} else {
		// Create child span for port allocation
		_, allocSpan := startHostPortSpan(ctx, tracer, tracing.SpanAllocateHostPort, pod,
			hostPortAttrPortsRequestedKey.Int64(int64(requestedPorts)),
			hostPortAttrPodKey.String(podKey),
			hostPortAttrShardIDKey.Int(shard.id),
		)
		hostPorts = shard.allocate(numToAlloc, podKey)
		logger.Info("Allocated hostPorts for pod", telemetryfields.FieldHostPorts, hostPorts, telemetryfields.FieldRequestedPorts, requestedPorts)
		allocSpan.SetAttributes(
			hostPortAttrAllocatedPortsKey.String(util.Int32SliceToString(hostPorts, ",")),
			hostPortAttrAllocatedCountKey.Int64(int64(len(hostPorts))),
		)
		// add event on parent when allocation happened
		span.AddEvent(tracing.EventNetworkHostPortPortsAllocated, trace.WithAttributes(hostPortAttrAllocatedPortsKey.String(util.Int32SliceToString(hostPorts, ",")), hostPortAttrAllocatedCountKey.Int64(int64(len(hostPorts)))))
		allocSpan.SetStatus(codes.Ok, "ports allocated successfully")
		allocSpan.End()
	}

	// patch pod container ports
	containers := pod.Spec.Containers
	for cIndex, container := range pod.Spec.Containers {
		if ports, ok := containerPortsMap[container.Name]; ok {
			containerPorts := container.Ports
			for i, port := range ports {
				// -1 means same as host
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

	// Record success
	span.SetAttributes(
		hostPortAttrContainersPatchedKey.Int64(int64(len(containerPortsMap))),
		hostPortAttrAllocatedPortsKey.String(util.Int32SliceToString(hostPorts, ",")),
		hostPortAttrAllocatedCountKey.Int64(int64(len(hostPorts))),
	)
	// mark containers patched event
	span.AddEvent(tracing.EventNetworkHostPortContainersPatched, trace.WithAttributes(hostPortAttrContainersPatchedKey.Int64(int64(len(containerPortsMap))), hostPortAttrAllocatedCountKey.Int64(int64(len(hostPorts)))))
	// mark NotReady until OnPodUpdated verifies network; defer will set final attributes/status
	finalNetworkStatus = telemetryfields.NetworkStatusNotReady

	return pod, nil
}

func (hpp *HostPortPlugin) OnPodUpdated(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, errors.PluginError) {
	// Create root span for HostPort OnPodUpdated
	tracer := otel.Tracer("okg-controller-manager")
	ctx, span := startHostPortSpan(ctx, tracer, tracing.SpanProcessHostPortUpdate, pod)
	// track final network status and error to ensure a single final attribute is set on the parent span
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
			// mark final status as NotReady and record error type
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

	// network not ready
	if len(iNetworkPorts) == 0 || len(eNetworkPorts) == 0 || pod.Status.PodIP == "" {
		errNetworkNotReady := fmt.Errorf("pod ip or hostports missing")
		logger.Error(errNetworkNotReady, "HostPort network not ready", telemetryfields.FieldInternalPorts, len(iNetworkPorts), telemetryfields.FieldExternalPorts, len(eNetworkPorts), telemetryfields.FieldPodIP, pod.Status.PodIP)
		// update parent final status
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

	// Record success
	span.SetAttributes(
		tracing.AttrNetworkStatus(telemetryfields.NetworkStatusReady),
		attribute.String(telemetryfields.FieldNodeIP, nodeIp),
		hostPortAttrNodeIPKey.String(nodeIp),
		attribute.Int(telemetryfields.FieldInternalPorts, len(iNetworkPorts)),
		attribute.Int(telemetryfields.FieldExternalPorts, len(eNetworkPorts)),
		hostPortAttrInternalPortCountKey.Int64(int64(len(iNetworkPorts))),
		hostPortAttrExternalPortCountKey.Int64(int64(len(eNetworkPorts))),
	)
	// parent final network status set to Ready
	finalNetworkStatus = telemetryfields.NetworkStatusReady
	finalStatus = codes.Ok
	finalMessage = "network ready"
	// add a status published event
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

	// Get the shard for this pod
	shard := hpp.getShard(pod)
	span.SetAttributes(hostPortAttrShardIDKey.Int(shard.id))

	if !shard.hasAllocation(podKey) {
		logger.V(4).Info("No hostport allocation found for pod")
		finalStatus = codes.Ok
		finalMessage = "no hostport allocation found"
		return nil
	}

	hostPorts := make([]int32, 0)
	for _, container := range pod.Spec.Containers {
		for _, port := range container.Ports {
			if port.HostPort >= hpp.minPort && port.HostPort <= hpp.maxPort {
				hostPorts = append(hostPorts, port.HostPort)
			}
		}
	}

	shard.deallocate(hostPorts, podKey)
	logger.Info("Released hostPorts for pod", telemetryfields.FieldHostPorts, hostPorts)
	span.AddEvent(tracing.EventNetworkHostPortReleased, trace.WithAttributes(hostPortAttrReleasedCountKey.Int64(int64(len(hostPorts))), hostPortAttrReleasedPortsKey.String(util.Int32SliceToString(hostPorts, ","))))
	span.SetAttributes(
		hostPortAttrReleasedCountKey.Int64(int64(len(hostPorts))),
		hostPortAttrReleasedPortsKey.String(util.Int32SliceToString(hostPorts, ",")),
	)
	// mark final status OK for deleted, keep network_status as NotReady/cleanup done
	finalMessage = "hostport allocation cleaned up"
	finalStatus = codes.Ok
	return nil
}

func (hpp *HostPortPlugin) Init(c client.Client, options cloudprovider.CloudProviderOptions, ctx context.Context) error {
	hostPortOptions := options.(provideroptions.KubernetesOptions).HostPort
	hpp.maxPort = hostPortOptions.MaxPort
	hpp.minPort = hostPortOptions.MinPort
	hpp.shardCount = determineShardCount(hostPortOptions)
	hpp.shardMask = int32(hpp.shardCount - 1)

	// Initialize shards with partitioned port ranges
	hpp.shards = make([]*PortShard, hpp.shardCount)
	totalPorts := hpp.maxPort - hpp.minPort + 1
	portsPerShard := totalPorts / int32(hpp.shardCount)

	for i := 0; i < hpp.shardCount; i++ {
		shardMinPort := hpp.minPort + int32(i)*portsPerShard
		shardMaxPort := shardMinPort + portsPerShard - 1

		// Last shard gets remaining ports
		if i == hpp.shardCount-1 {
			shardMaxPort = hpp.maxPort
		}

		hpp.shards[i] = newPortShard(i, shardMinPort, shardMaxPort)
	}

	// Recover state from existing pods
	podList := &corev1.PodList{}
	err := c.List(ctx, podList)
	if err != nil {
		return err
	}

	// Group pods by shard for recovery
	shardPods := make(map[int][]corev1.Pod)
	for _, pod := range podList.Items {
		if pod.GetAnnotations()[gamekruiseiov1alpha1.GameServerNetworkType] != HostPortNetwork {
			continue
		}
		shard := hpp.getShard(&pod)
		shardPods[shard.id] = append(shardPods[shard.id], pod)
	}

	// Recover each shard
	for shardID, pods := range shardPods {
		hpp.shards[shardID].recoverState(pods)
	}

	// Calculate total allocated pods for logging
	totalAllocated := 0
	for _, shard := range hpp.shards {
		totalAllocated += shard.getAllocatedPodCount()
	}

	logger := hostPortLogger(ctx, nil).WithValues(
		telemetryfields.FieldOperation, "init",
		telemetryfields.FieldPortMin, hpp.minPort,
		telemetryfields.FieldPortMax, hpp.maxPort,
		"shard_count", hpp.shardCount,
	)
	logger.Info("Initialized hostport allocation state with sharding", telemetryfields.FieldAllocatedPods, totalAllocated)
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
					// handle port
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
					// handle protocol
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

func selectPorts(amountStat []int, portAmount map[int32]int, num int) ([]int32, int) {
	var index int
	for i, total := range amountStat {
		if total >= num {
			index = i
			break
		}
	}

	hostPorts := make([]int32, 0)
	for hostPort, amount := range portAmount {
		if amount == index {
			hostPorts = append(hostPorts, hostPort)
			num--
		}
		if num == 0 {
			break
		}
	}
	return hostPorts, index
}
