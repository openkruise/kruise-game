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
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/cloudprovider"
	cperrors "github.com/openkruise/kruise-game/cloudprovider/errors"
	"github.com/openkruise/kruise-game/cloudprovider/utils"
	"github.com/openkruise/kruise-game/pkg/util"
)

const (
	NodePortNetwork = "Kubernetes-NodePort"

	PortProtocolsConfigName = "PortProtocols"

	SvcSelectorDisabledKey = "game.kruise.io/svc-selector-disabled"
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
	networkManager := utils.NewNetworkManager(pod, client)

	networkStatus, _ := networkManager.GetNetworkStatus()
	networkConfig := networkManager.GetNetworkConfig()
	npc, err := parseNodePortConfig(networkConfig)
	if err != nil {
		return pod, cperrors.NewPluginError(cperrors.ParameterError, err.Error())
	}

	if networkStatus == nil {
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
			return pod, cperrors.ToPluginError(client.Create(ctx, consNodePortSvc(npc, pod, client, ctx)), cperrors.ApiCallError)
		}
		return pod, cperrors.NewPluginError(cperrors.ApiCallError, err.Error())
	}

	// update svc
	if util.GetHash(npc) != svc.GetAnnotations()[ServiceHashKey] {
		networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkNotReady
		pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
		if err != nil {
			return pod, cperrors.NewPluginError(cperrors.InternalError, err.Error())
		}
		return pod, cperrors.ToPluginError(client.Update(ctx, consNodePortSvc(npc, pod, client, ctx)), cperrors.ApiCallError)
	}

	// disable network
	if networkManager.GetNetworkDisabled() && svc.Spec.Selector[SvcSelectorKey] == pod.GetName() {
		newSelector := svc.Spec.Selector
		newSelector[SvcSelectorDisabledKey] = pod.GetName()
		delete(svc.Spec.Selector, SvcSelectorKey)
		svc.Spec.Selector = newSelector
		return pod, cperrors.ToPluginError(client.Update(ctx, svc), cperrors.ApiCallError)
	}

	// enable network
	if !networkManager.GetNetworkDisabled() && svc.Spec.Selector[SvcSelectorDisabledKey] == pod.GetName() {
		newSelector := svc.Spec.Selector
		newSelector[SvcSelectorKey] = pod.GetName()
		delete(svc.Spec.Selector, SvcSelectorDisabledKey)
		svc.Spec.Selector = newSelector
		return pod, cperrors.ToPluginError(client.Update(ctx, svc), cperrors.ApiCallError)
	}

	// allow not ready containers
	if util.IsAllowNotReadyContainers(networkManager.GetNetworkConfig()) {
		toUpDateSvc, err := utils.AllowNotReadyContainers(client, ctx, pod, svc, false)
		if err != nil {
			return pod, err
		}

		if toUpDateSvc {
			err := client.Update(ctx, svc)
			if err != nil {
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
		return pod, cperrors.NewPluginError(cperrors.ApiCallError, err.Error())
	}

	if pod.Status.PodIP == "" {
		// Pod IP not exist, Network NotReady
		networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkNotReady
		pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
		return pod, cperrors.ToPluginError(err, cperrors.InternalError)
	}

	internalAddresses := make([]gamekruiseiov1alpha1.NetworkAddress, 0)
	externalAddresses := make([]gamekruiseiov1alpha1.NetworkAddress, 0)
	for _, port := range svc.Spec.Ports {
		instrIPort := port.TargetPort
		if port.NodePort == 0 {
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
			IP: getAddress(node),
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
