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
	"net"
	"strconv"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	log "k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/cloudprovider"
	"github.com/openkruise/kruise-game/cloudprovider/errors"
	provideroptions "github.com/openkruise/kruise-game/cloudprovider/options"
	"github.com/openkruise/kruise-game/cloudprovider/utils"
	"github.com/openkruise/kruise-game/pkg/util"
)

const (
	HostPortNetwork = "Kubernetes-HostPort"
	// ContainerPortsKey represents the configuration key when using hostPort.
	// Its corresponding value format is as follows, containerName:port1/protocol1,port2/protocol2,... e.g. game-server:25565/TCP
	// When no protocol is specified, TCP is used by default
	ContainerPortsKey = "ContainerPorts"
	PortSameAsHost    = "SameAsHost"
	ProtocolTCPUDP    = "TCPUDP"
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
	log.Infof("Receiving pod %s/%s ADD Operation", pod.GetNamespace(), pod.GetName())
	podNow := &corev1.Pod{}
	err := c.Get(ctx, types.NamespacedName{
		Namespace: pod.GetNamespace(),
		Name:      pod.GetName(),
	}, podNow)
	if err == nil {
		log.Infof("There is a pod with same ns/name(%s/%s) exists in cluster, do not allocate", pod.GetNamespace(), pod.GetName())
		return pod, errors.NewPluginError(errors.InternalError, "There is a pod with same ns/name exists in cluster")
	}
	if !k8serrors.IsNotFound(err) {
		return pod, errors.NewPluginError(errors.ApiCallError, err.Error())
	}

	networkManager := utils.NewNetworkManager(pod, c)
	conf := networkManager.GetNetworkConfig()
	containerPortsMap, containerProtocolsMap, numToAlloc := parseConfig(conf, pod)

	var hostPorts []int32
	if str, ok := hpp.podAllocated[pod.GetNamespace()+"/"+pod.GetName()]; ok {
		hostPorts = util.StringToInt32Slice(str, ",")
		log.Infof("pod %s/%s use hostPorts %v , which are allocated before", pod.GetNamespace(), pod.GetName(), hostPorts)
	} else {
		hostPorts = hpp.allocate(numToAlloc, pod.GetNamespace()+"/"+pod.GetName())
		log.Infof("pod %s/%s allocated hostPorts %v", pod.GetNamespace(), pod.GetName(), hostPorts)
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
	return pod, nil
}

func (hpp *HostPortPlugin) OnPodUpdated(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, errors.PluginError) {
	log.Infof("Receiving pod %s/%s UPDATE Operation", pod.GetNamespace(), pod.GetName())
	node := &corev1.Node{}
	err := c.Get(ctx, types.NamespacedName{
		Name: pod.Spec.NodeName,
	}, node)
	if err != nil {
		return pod, errors.NewPluginError(errors.ApiCallError, err.Error())
	}
	nodeIp := getAddress(node)

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

	pod, err = networkManager.UpdateNetworkStatus(networkStatus, pod)
	return pod, errors.ToPluginError(err, errors.InternalError)
}

func (hpp *HostPortPlugin) OnPodDeleted(c client.Client, pod *corev1.Pod, ctx context.Context) errors.PluginError {
	log.Infof("Receiving pod %s/%s DELETE Operation", pod.GetNamespace(), pod.GetName())
	if _, ok := hpp.podAllocated[pod.GetNamespace()+"/"+pod.GetName()]; !ok {
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

	hpp.deAllocate(hostPorts, pod.GetNamespace()+"/"+pod.GetName())
	log.Infof("pod %s/%s deallocated hostPorts %v", pod.GetNamespace(), pod.GetName(), hostPorts)
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
	log.Infof("[Kubernetes-HostPort] podAllocated init: %v", hpp.podAllocated)
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
