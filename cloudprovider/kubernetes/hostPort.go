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
	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/cloudprovider"
	provideroptions "github.com/openkruise/kruise-game/cloudprovider/options"
	"github.com/openkruise/kruise-game/cloudprovider/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	log "k8s.io/klog/v2"
	"net"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strconv"
	"strings"
	"sync"
)

const (
	HostPortNetwork = "HostPortNetwork"
	//ContainerPortsKey represents the configuration key when using hostPort.
	//Its corresponding value format is as follows, containerName:port1/protocol1,port2/protocol2,... e.g. game-server:25565/TCP
	//When no protocol is specified, TCP is used by default
	ContainerPortsKey = "ContainerPorts"
)

type HostPortPlugin struct {
	maxPort     int32
	minPort     int32
	isAllocated map[string]bool
	portAmount  map[int32]int
	amountStat  []int
	mutex       sync.RWMutex
}

func init() {
	hostPortPlugin := HostPortPlugin{
		mutex:       sync.RWMutex{},
		isAllocated: make(map[string]bool),
	}
	kubernetesProvider.registerPlugin(&hostPortPlugin)
}

func (hpp *HostPortPlugin) Name() string {
	return HostPortNetwork
}

func (hpp *HostPortPlugin) Alias() string {
	return ""
}

func (hpp *HostPortPlugin) OnPodAdded(c client.Client, pod *corev1.Pod) (*corev1.Pod, error) {
	if _, ok := hpp.isAllocated[pod.GetNamespace()+"/"+pod.GetName()]; ok {
		return pod, nil
	}

	networkManager := utils.NewNetworkManager(pod, c)
	conf := networkManager.GetNetworkConfig()

	containerPortsMap, containerProtocolsMap, numToAlloc := parseConfig(conf, pod)
	hostPorts := hpp.allocate(numToAlloc, pod.GetNamespace()+"/"+pod.GetName())

	log.V(5).Infof("pod %s/%s allocated hostPorts %v", pod.GetNamespace(), pod.GetName(), hostPorts)

	// patch pod container ports
	containers := pod.Spec.Containers
	for cIndex, container := range pod.Spec.Containers {
		if ports, ok := containerPortsMap[container.Name]; ok {
			containerPorts := container.Ports
			for i, port := range ports {
				containerPort := corev1.ContainerPort{
					ContainerPort: port,
					HostPort:      hostPorts[numToAlloc-1],
					Protocol:      containerProtocolsMap[container.Name][i],
				}
				containerPorts = append(containerPorts, containerPort)
				numToAlloc--
			}
			containers[cIndex].Ports = containerPorts
		}
	}
	pod.Spec.Containers = containers
	return pod, nil
}

func (hpp *HostPortPlugin) OnPodUpdated(c client.Client, pod *corev1.Pod) (*corev1.Pod, error) {
	node := &corev1.Node{}
	err := c.Get(context.Background(), types.NamespacedName{
		Name: pod.Spec.NodeName,
	}, node)
	if err != nil {
		return pod, err
	}
	iip, eip := getAddress(node)

	networkManager := utils.NewNetworkManager(pod, c)
	status, _ := networkManager.GetNetworkStatus()
	if status != nil {
		return pod, nil
	}

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

	networkStatus := gamekruiseiov1alpha1.NetworkStatus{
		InternalAddresses: []gamekruiseiov1alpha1.NetworkAddress{
			{
				IP:    iip,
				Ports: iNetworkPorts,
			},
		},
		ExternalAddresses: []gamekruiseiov1alpha1.NetworkAddress{
			{
				IP:    eip,
				Ports: eNetworkPorts,
			},
		},
		CurrentNetworkState: gamekruiseiov1alpha1.NetworkReady,
	}

	return networkManager.UpdateNetworkStatus(networkStatus, pod)
}

func (hpp *HostPortPlugin) OnPodDeleted(c client.Client, pod *corev1.Pod) error {
	if _, ok := hpp.isAllocated[pod.GetNamespace()+"/"+pod.GetName()]; !ok {
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
	return nil
}

func (hpp *HostPortPlugin) Init(c client.Client, options cloudprovider.CloudProviderOptions) error {
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
	err := c.List(context.Background(), podList)
	if err != nil {
		return err
	}
	for _, pod := range podList.Items {
		if pod.GetAnnotations()[gamekruiseiov1alpha1.GameServerNetworkType] == HostPortNetwork {
			for _, container := range pod.Spec.Containers {
				for _, port := range container.Ports {
					if port.HostPort >= hpp.minPort && port.HostPort <= hpp.maxPort {
						newPortAmount[port.HostPort]++
						hpp.isAllocated[pod.GetNamespace()+"/"+pod.GetName()] = true
					}
				}
			}
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

	hpp.isAllocated[nsname] = true
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

	delete(hpp.isAllocated, nsname)
}

func verifyContainerName(containerName string, pod *corev1.Pod) bool {
	for _, container := range pod.Spec.Containers {
		if container.Name == containerName {
			return true
		}
	}
	return false
}

func getAddress(node *corev1.Node) (string, string) {
	var eip string
	var iip string

	for _, a := range node.Status.Addresses {
		if a.Type == corev1.NodeExternalIP && net.ParseIP(a.Address) != nil {
			eip = a.Address
		}
	}

	for _, a := range node.Status.Addresses {
		if a.Type == corev1.NodeExternalDNS {
			eip = a.Address
		}
	}

	for _, a := range node.Status.Addresses {
		if a.Type == corev1.NodeInternalIP && net.ParseIP(a.Address) != nil {
			iip = a.Address
		}
	}

	for _, a := range node.Status.Addresses {
		if a.Type == corev1.NodeInternalDNS {
			iip = a.Address
		}
	}

	return iip, eip
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
					port, err := strconv.ParseInt(ppSlice[0], 10, 32)
					if err != nil {
						continue
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
