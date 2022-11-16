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

package alibabacloud

import (
	"context"
	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/cloudprovider/alibabacloud/apis/v1"
	"github.com/openkruise/kruise-game/cloudprovider/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
)

const (
	NatGwNetwork       = "Ali-NatGw"
	AliasNawGw         = "NatGwNetwork"
	FixedConfigName    = "Fixed"
	PortsConfigName    = "Ports"
	ProtocolConfigName = "Protocol"
	DnatAnsKey         = "k8s.aliyun.com/pod-dnat"
	PortsAnsKey        = "k8s.aliyun.com/pod-dnat-expose-port"
	ProtocolAnsKey     = "k8s.aliyun.com/pod-dnat-expose-protocol"
	FixedAnsKey        = "k8s.aliyun.com/pod-dnat-fixed"
)

type NatGwPlugin struct {
}

func (n NatGwPlugin) Name() string {
	return NatGwNetwork
}

func (n NatGwPlugin) Alias() string {
	return AliasNawGw
}

func (n NatGwPlugin) Init(c client.Client) error {
	return nil
}

func (n NatGwPlugin) OnPodAdded(c client.Client, pod *corev1.Pod) (*corev1.Pod, error) {
	networkManager := utils.NewNetworkManager(pod, c)
	conf := networkManager.GetNetworkConfig()
	ports, protocol, fixed := parseConfig(conf)
	pod.Annotations[DnatAnsKey] = "true"
	pod.Annotations[PortsAnsKey] = ports
	if protocol != "" {
		pod.Annotations[ProtocolAnsKey] = protocol
	}
	if fixed != "" {
		pod.Annotations[FixedAnsKey] = fixed
	}
	return pod, nil
}

func (n NatGwPlugin) OnPodUpdated(c client.Client, pod *corev1.Pod) (*corev1.Pod, error) {
	networkManager := utils.NewNetworkManager(pod, c)

	networkStatus, _ := networkManager.GetNetworkStatus()
	if networkStatus == nil {
		return networkManager.UpdateNetworkStatus(gamekruiseiov1alpha1.NetworkStatus{
			CurrentNetworkState: gamekruiseiov1alpha1.NetworkWaiting,
		}, pod)
	}

	podDNat := &v1.PodDNAT{}
	err := c.Get(context.Background(), types.NamespacedName{
		Name:      pod.GetName(),
		Namespace: pod.GetNamespace(),
	}, podDNat)
	if err != nil || podDNat.Status.Entries == nil {
		return pod, nil
	}

	internalAddresses := make([]gamekruiseiov1alpha1.NetworkAddress, 0)
	externalAddresses := make([]gamekruiseiov1alpha1.NetworkAddress, 0)
	for _, entry := range podDNat.Status.Entries {
		instrIPort := intstr.FromString(entry.InternalPort)
		instrEPort := intstr.FromString(entry.ExternalPort)
		internalAddress := gamekruiseiov1alpha1.NetworkAddress{
			IP: entry.InternalIP,
			Ports: []gamekruiseiov1alpha1.NetworkPort{
				{
					Name:     entry.InternalPort,
					Port:     &instrIPort,
					Protocol: corev1.Protocol(strings.ToUpper(entry.IPProtocol)),
				},
			},
		}
		externalAddress := gamekruiseiov1alpha1.NetworkAddress{
			IP: entry.ExternalIP,
			Ports: []gamekruiseiov1alpha1.NetworkPort{
				{
					Name:     entry.InternalPort,
					Port:     &instrEPort,
					Protocol: corev1.Protocol(strings.ToUpper(entry.IPProtocol)),
				},
			},
		}
		internalAddresses = append(internalAddresses, internalAddress)
		externalAddresses = append(externalAddresses, externalAddress)
	}
	networkStatus.InternalAddresses = internalAddresses
	networkStatus.ExternalAddresses = externalAddresses
	networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkReady
	return networkManager.UpdateNetworkStatus(*networkStatus, pod)
}

func (n NatGwPlugin) OnPodDeleted(c client.Client, pod *corev1.Pod) error {
	return nil
}

func init() {
	alibabaCloudProvider.registerPlugin(&NatGwPlugin{})
}

func parseConfig(conf []gamekruiseiov1alpha1.NetworkConfParams) (string, string, string) {
	var ports string
	var protocol string
	var fixed string
	for _, c := range conf {
		switch c.Name {
		case PortsConfigName:
			ports = c.Value
		case ProtocolConfigName:
			protocol = c.Value
		case FixedConfigName:
			fixed = c.Value
		}
	}
	return ports, protocol, fixed
}
