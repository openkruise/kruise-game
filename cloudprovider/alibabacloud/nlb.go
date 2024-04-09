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

package alibabacloud

import (
	"context"
	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/cloudprovider"
	cperrors "github.com/openkruise/kruise-game/cloudprovider/errors"
	provideroptions "github.com/openkruise/kruise-game/cloudprovider/options"
	"github.com/openkruise/kruise-game/cloudprovider/utils"
	"github.com/openkruise/kruise-game/pkg/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	log "k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strconv"
	"strings"
	"sync"
)

const (
	NlbNetwork = "AlibabaCloud-NLB"
	AliasNLB   = "NLB-Network"
)

type NlbPlugin struct {
	maxPort     int32
	minPort     int32
	cache       map[string]portAllocated
	podAllocate map[string]string
	mutex       sync.RWMutex
}

type nlbConfig struct {
	lbIds       []string
	targetPorts []int
	protocols   []corev1.Protocol
	isFixed     bool
}

func (n *NlbPlugin) Name() string {
	return NlbNetwork
}

func (n *NlbPlugin) Alias() string {
	return AliasNLB
}

func (n *NlbPlugin) Init(c client.Client, options cloudprovider.CloudProviderOptions, ctx context.Context) error {
	n.mutex.Lock()
	defer n.mutex.Unlock()
	slbOptions := options.(provideroptions.AlibabaCloudOptions).NLBOptions
	n.minPort = slbOptions.MinPort
	n.maxPort = slbOptions.MaxPort

	svcList := &corev1.ServiceList{}
	err := c.List(ctx, svcList)
	if err != nil {
		return err
	}

	n.cache, n.podAllocate = initLbCache(svcList.Items, n.minPort, n.maxPort)
	log.Infof("[%s] podAllocate cache complete initialization: %v", NlbNetwork, n.podAllocate)
	return nil
}

func (n *NlbPlugin) OnPodAdded(client client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	return pod, nil
}

func (n *NlbPlugin) OnPodUpdated(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	networkManager := utils.NewNetworkManager(pod, c)

	networkStatus, _ := networkManager.GetNetworkStatus()
	networkConfig := networkManager.GetNetworkConfig()
	sc := parseNlbConfig(networkConfig)
	if networkStatus == nil {
		pod, err := networkManager.UpdateNetworkStatus(gamekruiseiov1alpha1.NetworkStatus{
			CurrentNetworkState: gamekruiseiov1alpha1.NetworkNotReady,
		}, pod)
		return pod, cperrors.ToPluginError(err, cperrors.InternalError)
	}

	// get svc
	svc := &corev1.Service{}
	err := c.Get(ctx, types.NamespacedName{
		Name:      pod.GetName(),
		Namespace: pod.GetNamespace(),
	}, svc)
	if err != nil {
		if errors.IsNotFound(err) {
			return pod, cperrors.ToPluginError(c.Create(ctx, n.consSvc(sc, pod, c, ctx)), cperrors.ApiCallError)
		}
		return pod, cperrors.NewPluginError(cperrors.ApiCallError, err.Error())
	}

	// update svc
	if util.GetHash(sc) != svc.GetAnnotations()[SlbConfigHashKey] {
		networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkNotReady
		pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
		if err != nil {
			return pod, cperrors.NewPluginError(cperrors.InternalError, err.Error())
		}
		return pod, cperrors.ToPluginError(c.Update(ctx, n.consSvc(sc, pod, c, ctx)), cperrors.ApiCallError)
	}

	// disable network
	if networkManager.GetNetworkDisabled() && svc.Spec.Type == corev1.ServiceTypeLoadBalancer {
		svc.Spec.Type = corev1.ServiceTypeClusterIP
		return pod, cperrors.ToPluginError(c.Update(ctx, svc), cperrors.ApiCallError)
	}

	// enable network
	if !networkManager.GetNetworkDisabled() && svc.Spec.Type == corev1.ServiceTypeClusterIP {
		svc.Spec.Type = corev1.ServiceTypeLoadBalancer
		return pod, cperrors.ToPluginError(c.Update(ctx, svc), cperrors.ApiCallError)
	}

	// network not ready
	if svc.Status.LoadBalancer.Ingress == nil {
		networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkNotReady
		pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
		return pod, cperrors.ToPluginError(err, cperrors.InternalError)
	}

	// allow not ready containers
	if util.IsAllowNotReadyContainers(networkManager.GetNetworkConfig()) {
		toUpDateSvc, err := utils.AllowNotReadyContainers(c, ctx, pod, svc, false)
		if err != nil {
			return pod, err
		}

		if toUpDateSvc {
			err := c.Update(ctx, svc)
			if err != nil {
				return pod, cperrors.ToPluginError(err, cperrors.ApiCallError)
			}
		}
	}

	// network ready
	internalAddresses := make([]gamekruiseiov1alpha1.NetworkAddress, 0)
	externalAddresses := make([]gamekruiseiov1alpha1.NetworkAddress, 0)
	for _, port := range svc.Spec.Ports {
		instrIPort := port.TargetPort
		instrEPort := intstr.FromInt(int(port.Port))
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
			EndPoint: svc.Status.LoadBalancer.Ingress[0].Hostname,
			IP:       svc.Status.LoadBalancer.Ingress[0].IP,
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

func (n *NlbPlugin) OnPodDeleted(c client.Client, pod *corev1.Pod, ctx context.Context) cperrors.PluginError {
	networkManager := utils.NewNetworkManager(pod, c)
	networkConfig := networkManager.GetNetworkConfig()
	sc := parseLbConfig(networkConfig)

	var podKeys []string
	if sc.isFixed {
		gss, err := util.GetGameServerSetOfPod(pod, c, ctx)
		if err != nil && !errors.IsNotFound(err) {
			return cperrors.ToPluginError(err, cperrors.ApiCallError)
		}
		// gss exists in cluster, do not deAllocate.
		if err == nil && gss.GetDeletionTimestamp() == nil {
			return nil
		}
		// gss not exists in cluster, deAllocate all the ports related to it.
		for key := range n.podAllocate {
			gssName := pod.GetLabels()[gamekruiseiov1alpha1.GameServerOwnerGssKey]
			if strings.Contains(key, pod.GetNamespace()+"/"+gssName) {
				podKeys = append(podKeys, key)
			}
		}
	} else {
		podKeys = append(podKeys, pod.GetNamespace()+"/"+pod.GetName())
	}

	for _, podKey := range podKeys {
		n.deAllocate(podKey)
	}

	return nil
}

func init() {
	nlbPlugin := NlbPlugin{
		mutex: sync.RWMutex{},
	}
	alibabaCloudProvider.registerPlugin(&nlbPlugin)
}

func (n *NlbPlugin) consSvc(nc *nlbConfig, pod *corev1.Pod, c client.Client, ctx context.Context) *corev1.Service {
	var ports []int32
	var lbId string
	podKey := pod.GetNamespace() + "/" + pod.GetName()
	allocatedPorts, exist := n.podAllocate[podKey]
	if exist {
		slbPorts := strings.Split(allocatedPorts, ":")
		lbId = slbPorts[0]
		ports = util.StringToInt32Slice(slbPorts[1], ",")
	} else {
		lbId, ports = n.allocate(nc.lbIds, len(nc.targetPorts), podKey)
	}

	svcPorts := make([]corev1.ServicePort, 0)
	for i := 0; i < len(nc.targetPorts); i++ {
		svcPorts = append(svcPorts, corev1.ServicePort{
			Name:       strconv.Itoa(nc.targetPorts[i]),
			Port:       ports[i],
			Protocol:   nc.protocols[i],
			TargetPort: intstr.FromInt(nc.targetPorts[i]),
		})
	}

	loadBalancerClass := "alibabacloud.com/nlb"

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pod.GetName(),
			Namespace: pod.GetNamespace(),
			Annotations: map[string]string{
				SlbListenerOverrideKey: "true",
				SlbIdAnnotationKey:     lbId,
				SlbConfigHashKey:       util.GetHash(nc),
			},
			OwnerReferences: getSvcOwnerReference(c, ctx, pod, nc.isFixed),
		},
		Spec: corev1.ServiceSpec{
			ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyTypeLocal,
			Type:                  corev1.ServiceTypeLoadBalancer,
			Selector: map[string]string{
				SvcSelectorKey: pod.GetName(),
			},
			Ports:             svcPorts,
			LoadBalancerClass: &loadBalancerClass,
		},
	}
	return svc
}

func (n *NlbPlugin) allocate(lbIds []string, num int, nsName string) (string, []int32) {
	n.mutex.Lock()
	defer n.mutex.Unlock()

	var ports []int32
	var lbId string

	// find lb with adequate ports
	for _, slbId := range lbIds {
		sum := 0
		for i := n.minPort; i < n.maxPort; i++ {
			if !n.cache[slbId][i] {
				sum++
			}
			if sum >= num {
				lbId = slbId
				break
			}
		}
	}

	// select ports
	for i := 0; i < num; i++ {
		var port int32
		if n.cache[lbId] == nil {
			n.cache[lbId] = make(portAllocated, n.maxPort-n.minPort)
			for i := n.minPort; i < n.maxPort; i++ {
				n.cache[lbId][i] = false
			}
		}

		for p, allocated := range n.cache[lbId] {
			if !allocated {
				port = p
				break
			}
		}
		n.cache[lbId][port] = true
		ports = append(ports, port)
	}

	n.podAllocate[nsName] = lbId + ":" + util.Int32SliceToString(ports, ",")
	log.Infof("pod %s allocate nlb %s ports %v", nsName, lbId, ports)
	return lbId, ports
}

func (n *NlbPlugin) deAllocate(nsName string) {
	n.mutex.Lock()
	defer n.mutex.Unlock()

	allocatedPorts, exist := n.podAllocate[nsName]
	if !exist {
		return
	}

	slbPorts := strings.Split(allocatedPorts, ":")
	lbId := slbPorts[0]
	ports := util.StringToInt32Slice(slbPorts[1], ",")
	for _, port := range ports {
		n.cache[lbId][port] = false
	}

	delete(n.podAllocate, nsName)
	log.Infof("pod %s deallocate nlb %s ports %v", nsName, lbId, ports)
}

func parseNlbConfig(conf []gamekruiseiov1alpha1.NetworkConfParams) *nlbConfig {
	var lbIds []string
	ports := make([]int, 0)
	protocols := make([]corev1.Protocol, 0)
	isFixed := false
	for _, c := range conf {
		switch c.Name {
		case NlbIdsConfigName:
			for _, slbId := range strings.Split(c.Value, ",") {
				if slbId != "" {
					lbIds = append(lbIds, slbId)
				}
			}
		case PortProtocolsConfigName:
			for _, pp := range strings.Split(c.Value, ",") {
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
		case FixedConfigName:
			v, err := strconv.ParseBool(c.Value)
			if err != nil {
				continue
			}
			isFixed = v
		}
	}
	return &nlbConfig{
		lbIds:       lbIds,
		protocols:   protocols,
		targetPorts: ports,
		isFixed:     isFixed,
	}
}
