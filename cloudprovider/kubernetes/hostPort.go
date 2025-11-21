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
	hostPortPluginSlug    = "kubernetes-hostport"
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

type HostPortPlugin struct {
	maxPort      int32
	minPort      int32
	podAllocated map[string]string
	portAmount   map[int32]int
	amountStat   []int
	mutex        sync.RWMutex
}

func init() {
	hostPortPlugin := HostPortPlugin{
		mutex:        sync.RWMutex{},
		podAllocated: make(map[string]string),
	}
	kubernetesProvider.registerPlugin(&hostPortPlugin)
}

func (hpp *HostPortPlugin) Name() string {
	return HostPortNetwork
}

func (hpp *HostPortPlugin) Alias() string {
	return ""
}

func (hpp *HostPortPlugin) OnPodAdded(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, errors.PluginError) {
	// Create root span for HostPort OnPodAdded
	tracer := otel.Tracer("okg-controller-manager")
	ctx, span := startHostPortSpan(ctx, tracer, tracing.SpanPrepareHostPortPod, pod)
	defer span.End()
	podKey := pod.GetNamespace() + "/" + pod.GetName()
	span.SetAttributes(
		tracing.AttrNetworkStatus("waiting"),
		hostPortAttrPodKey.String(podKey),
		hostPortAttrPortsReusedKey.Bool(false),
	)

	logger := hostPortLogger(ctx, pod).WithValues(tracing.FieldOperation, "add")
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
			tracing.AttrErrorType("ParameterError"),
		)
		return pod, errors.NewPluginError(errors.InternalError, "There is a pod with same ns/name exists in cluster")
	}
	if !k8serrors.IsNotFound(err) {
		logger.Error(err, "Failed to check existing pod before hostport allocation")
		span.RecordError(err)
		span.SetAttributes(tracing.AttrErrorType("ApiCallError"))
		span.SetStatus(codes.Error, "failed to check for existing pod")
		return pod, errors.NewPluginError(errors.ApiCallError, err.Error())
	}

	networkManager := utils.NewNetworkManager(pod, c)
	conf := networkManager.GetNetworkConfig()
	containerPortsMap, containerProtocolsMap, numToAlloc := parseConfig(conf, pod)
	requestedPorts := numToAlloc

	var hostPorts []int32
	if str, ok := hpp.podAllocated[podKey]; ok {
		hostPorts = util.StringToInt32Slice(str, ",")
		logger.Info("Reusing previously allocated hostPorts", tracing.FieldHostPorts, hostPorts, tracing.FieldRequestedPorts, requestedPorts)
		span.SetAttributes(
			hostPortAttrPortsReusedKey.Bool(true),
			hostPortAttrAllocatedPortsKey.String(util.Int32SliceToString(hostPorts, ",")),
			hostPortAttrAllocatedCountKey.Int64(int64(len(hostPorts))),
		)
	} else {
		// Create child span for port allocation
		_, allocSpan := startHostPortSpan(ctx, tracer, tracing.SpanAllocateHostPort, pod,
			tracing.AttrNetworkStatus("waiting"),
			hostPortAttrPortsRequestedKey.Int64(int64(requestedPorts)),
			hostPortAttrPodKey.String(podKey),
		)
		hostPorts = hpp.allocate(numToAlloc, podKey)
		logger.Info("Allocated hostPorts for pod", tracing.FieldHostPorts, hostPorts, tracing.FieldRequestedPorts, requestedPorts)
		allocSpan.SetAttributes(
			hostPortAttrAllocatedPortsKey.String(util.Int32SliceToString(hostPorts, ",")),
			hostPortAttrAllocatedCountKey.Int64(int64(len(hostPorts))),
		)
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
	span.SetStatus(codes.Ok, "pod configured with host ports successfully")

	return pod, nil
}

func (hpp *HostPortPlugin) OnPodUpdated(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, errors.PluginError) {
	// Create root span for HostPort OnPodUpdated
	tracer := otel.Tracer("okg-controller-manager")
	ctx, span := startHostPortSpan(ctx, tracer, tracing.SpanProcessHostPortUpdate, pod)
	defer span.End()
	span.SetAttributes(hostPortAttrPodKey.String(pod.GetNamespace() + "/" + pod.GetName()))

	logger := hostPortLogger(ctx, pod).WithValues(tracing.FieldOperation, "update")
	logger.Info("Processing hostport pod UPDATE operation")
	node := &corev1.Node{}
	err := c.Get(ctx, types.NamespacedName{
		Name: pod.Spec.NodeName,
	}, node)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Error(err, "Node not found for hostport pod", tracing.FieldNodeNameQualified, pod.Spec.NodeName)
			span.RecordError(err)
			span.SetStatus(codes.Error, "node not found")
			span.SetAttributes(
				tracing.AttrNetworkStatus("not_ready"),
				tracing.AttrErrorType("ResourceNotReady"),
			)
			return pod, nil
		}
		logger.Error(err, "Failed to fetch node for hostport pod", tracing.FieldNodeNameQualified, pod.Spec.NodeName)
		span.RecordError(err)
		span.SetAttributes(tracing.AttrErrorType("ApiCallError"))
		span.SetStatus(codes.Error, "failed to get node")
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
		logger.Error(errNetworkNotReady, "HostPort network not ready", tracing.FieldInternalPorts, len(iNetworkPorts), tracing.FieldExternalPorts, len(eNetworkPorts), tracing.FieldPodIP, pod.Status.PodIP)
		span.SetAttributes(
			tracing.AttrNetworkStatus("not_ready"),
			attribute.Int("game.kruise.io.network.internal_ports", len(iNetworkPorts)),
			attribute.Int("game.kruise.io.network.external_ports", len(eNetworkPorts)),
			hostPortAttrInternalPortCountKey.Int64(int64(len(iNetworkPorts))),
			hostPortAttrExternalPortCountKey.Int64(int64(len(eNetworkPorts))),
			attribute.String("pod.ip", pod.Status.PodIP),
			tracing.AttrErrorType("ResourceNotReady"),
		)
		span.RecordError(errNetworkNotReady)
		span.SetStatus(codes.Error, "network not ready")
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
		tracing.AttrNetworkStatus("ready"),
		attribute.String("node.ip", nodeIp),
		hostPortAttrNodeIPKey.String(nodeIp),
		attribute.Int("game.kruise.io.network.internal_ports", len(iNetworkPorts)),
		attribute.Int("game.kruise.io.network.external_ports", len(eNetworkPorts)),
		hostPortAttrInternalPortCountKey.Int64(int64(len(iNetworkPorts))),
		hostPortAttrExternalPortCountKey.Int64(int64(len(eNetworkPorts))),
	)
	span.SetStatus(codes.Ok, "network ready")
	logger.Info("Updated hostport network status", tracing.FieldNodeIP, nodeIp, tracing.FieldInternalPorts, len(iNetworkPorts), tracing.FieldExternalPorts, len(eNetworkPorts))

	pod, err = networkManager.UpdateNetworkStatus(networkStatus, pod)
	return pod, errors.ToPluginError(err, errors.InternalError)
}

func (hpp *HostPortPlugin) OnPodDeleted(c client.Client, pod *corev1.Pod, ctx context.Context) errors.PluginError {
	logger := hostPortLogger(ctx, pod).WithValues(tracing.FieldOperation, "delete")
	logger.Info("Processing hostport pod DELETE operation")
	tracer := otel.Tracer("okg-controller-manager")
	_, span := startHostPortSpan(ctx, tracer, tracing.SpanCleanupHostPortAllocation, pod,
		tracing.AttrNetworkStatus("not_ready"),
	)
	defer span.End()
	podKey := pod.GetNamespace() + "/" + pod.GetName()
	span.SetAttributes(hostPortAttrPodKey.String(podKey))
	if _, ok := hpp.podAllocated[podKey]; !ok {
		logger.V(4).Info("No hostport allocation found for pod")
		span.SetStatus(codes.Ok, "no hostport allocation found")
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

	hpp.deAllocate(hostPorts, podKey)
	logger.Info("Released hostPorts for pod", tracing.FieldHostPorts, hostPorts)
	span.SetAttributes(
		hostPortAttrReleasedCountKey.Int64(int64(len(hostPorts))),
		hostPortAttrReleasedPortsKey.String(util.Int32SliceToString(hostPorts, ",")),
	)
	span.SetStatus(codes.Ok, "hostport allocation cleaned up")
	return nil
}

func (hpp *HostPortPlugin) Init(c client.Client, options cloudprovider.CloudProviderOptions, ctx context.Context) error {
	hpp.mutex.Lock()
	defer hpp.mutex.Unlock()

	hostPortOptions := options.(provideroptions.KubernetesOptions).HostPort
	hpp.maxPort = hostPortOptions.MaxPort
	hpp.minPort = hostPortOptions.MinPort

	newPortAmount := make(map[int32]int, hpp.maxPort-hpp.minPort+1)
	for i := hpp.minPort; i <= hpp.maxPort; i++ {
		newPortAmount[i] = 0
	}
	podList := &corev1.PodList{}
	err := c.List(ctx, podList)
	if err != nil {
		return err
	}
	for _, pod := range podList.Items {
		var hostPorts []int32
		if pod.GetAnnotations()[gamekruiseiov1alpha1.GameServerNetworkType] == HostPortNetwork {
			for _, container := range pod.Spec.Containers {
				for _, port := range container.Ports {
					if port.HostPort >= hpp.minPort && port.HostPort <= hpp.maxPort {
						newPortAmount[port.HostPort]++
						hostPorts = append(hostPorts, port.HostPort)
					}
				}
			}
		}
		if len(hostPorts) != 0 {
			hpp.podAllocated[pod.GetNamespace()+"/"+pod.GetName()] = util.Int32SliceToString(hostPorts, ",")
		}
	}

	size := 0
	for _, amount := range newPortAmount {
		if amount > size {
			size = amount
		}
	}
	newAmountStat := make([]int, size+1)
	for _, amount := range newPortAmount {
		newAmountStat[amount]++
	}

	hpp.portAmount = newPortAmount
	hpp.amountStat = newAmountStat
	logger := hostPortLogger(ctx, nil).WithValues(
		tracing.FieldOperation, "init",
		tracing.FieldPortMin, hpp.minPort,
		tracing.FieldPortMax, hpp.maxPort,
	)
	logger.Info("Initialized hostport allocation state", tracing.FieldAllocatedPods, len(hpp.podAllocated))
	return nil
}

func (hpp *HostPortPlugin) allocate(num int, nsname string) []int32 {
	hpp.mutex.Lock()
	defer hpp.mutex.Unlock()

	hostPorts, index := selectPorts(hpp.amountStat, hpp.portAmount, num)
	for _, hostPort := range hostPorts {
		hpp.portAmount[hostPort]++
		hpp.amountStat[index]--
		if index+1 >= len(hpp.amountStat) {
			hpp.amountStat = append(hpp.amountStat, 0)
		}
		hpp.amountStat[index+1]++
	}

	hpp.podAllocated[nsname] = util.Int32SliceToString(hostPorts, ",")
	return hostPorts
}

func (hpp *HostPortPlugin) deAllocate(hostPorts []int32, nsname string) {
	hpp.mutex.Lock()
	defer hpp.mutex.Unlock()

	for _, hostPort := range hostPorts {
		amount := hpp.portAmount[hostPort]
		hpp.portAmount[hostPort]--
		hpp.amountStat[amount]--
		hpp.amountStat[amount-1]++
	}

	delete(hpp.podAllocated, nsname)
}

func hostPortLogger(ctx context.Context, pod *corev1.Pod) logr.Logger {
	logger := logging.FromContextWithTrace(ctx).WithValues(
		tracing.FieldComponent, "cloudprovider",
		tracing.FieldNetworkPluginName, hostPortPluginSlug,
		tracing.FieldPluginSlug, hostPortPluginSlug,
	)
	if pod != nil {
		logger = logger.WithValues(
			tracing.FieldGameServerNamespace, pod.GetNamespace(),
			tracing.FieldGameServerName, pod.GetName(),
		)
		if nodeName := pod.Spec.NodeName; nodeName != "" {
			logger = logger.WithValues(tracing.FieldNodeNameQualified, nodeName)
		}
		if gss := pod.GetLabels()[gamekruiseiov1alpha1.GameServerOwnerGssKey]; gss != "" {
			logger = logger.WithValues(
				tracing.FieldGameServerSetNamespace, pod.GetNamespace(),
				tracing.FieldGameServerSetName, gss,
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
		attrExtras = append(attrExtras, attribute.String("k8s.node.name", pod.Spec.NodeName))
	}
	attrExtras = append(attrExtras, extras...)
	attrExtras = tracing.EnsureNetworkStatusAttr(attrExtras, "waiting")
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
