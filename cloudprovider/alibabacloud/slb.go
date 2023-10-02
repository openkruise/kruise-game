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
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strconv"
	"strings"
	"sync"
)

const (
	SlbNetwork              = "AlibabaCloud-SLB"
	AliasSLB                = "LB-Network"
	SlbIdsConfigName        = "SlbIds"
	PortProtocolsConfigName = "PortProtocols"
	SlbListenerOverrideKey  = "service.beta.kubernetes.io/alibaba-cloud-loadbalancer-force-override-listeners"
	SlbIdAnnotationKey      = "service.beta.kubernetes.io/alibaba-cloud-loadbalancer-id"
	SlbIdLabelKey           = "service.k8s.alibaba/loadbalancer-id"
	SvcSelectorKey          = "statefulset.kubernetes.io/pod-name"
	SlbConfigHashKey        = "game.kruise.io/network-config-hash"
)

type portAllocated map[int32]bool

type SlbPlugin struct {
	maxPort     int32
	minPort     int32
	cache       map[string]portAllocated
	podAllocate map[string]string
	mutex       sync.RWMutex
}

type slbConfig struct {
	lbIds       []string
	targetPorts []int
	protocols   []corev1.Protocol
	isFixed     bool
}

func (s *SlbPlugin) Name() string {
	return SlbNetwork
}

func (s *SlbPlugin) Alias() string {
	return AliasSLB
}

func (s *SlbPlugin) Init(c client.Client, options cloudprovider.CloudProviderOptions, ctx context.Context) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	slbOptions := options.(provideroptions.AlibabaCloudOptions).SLBOptions
	s.minPort = slbOptions.MinPort
	s.maxPort = slbOptions.MaxPort

	svcList := &corev1.ServiceList{}
	err := c.List(ctx, svcList)
	if err != nil {
		return err
	}

	s.cache, s.podAllocate = initLbCache(svcList.Items, s.minPort, s.maxPort)
	return nil
}

func initLbCache(svcList []corev1.Service, minPort, maxPort int32) (map[string]portAllocated, map[string]string) {
	newCache := make(map[string]portAllocated)
	newPodAllocate := make(map[string]string)
	for _, svc := range svcList {
		lbId := svc.Labels[SlbIdLabelKey]
		if lbId != "" && svc.Spec.Type == corev1.ServiceTypeLoadBalancer {
			if newCache[lbId] == nil {
				newCache[lbId] = make(portAllocated, maxPort-minPort)
				for i := minPort; i < maxPort; i++ {
					newCache[lbId][i] = false
				}
			}
			var ports []int32
			for _, port := range getPorts(svc.Spec.Ports) {
				if port <= maxPort && port >= minPort {
					newCache[lbId][port] = true
					ports = append(ports, port)
				}
			}
			if len(ports) != 0 {
				newPodAllocate[svc.GetNamespace()+"/"+svc.GetName()] = lbId + ":" + util.Int32SliceToString(ports, ",")
			}
		}
	}
	log.Infof("[%s] podAllocate cache complete initialization: %v", SlbNetwork, newPodAllocate)
	return newCache, newPodAllocate
}

func (s *SlbPlugin) OnPodAdded(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	networkManager := utils.NewNetworkManager(pod, c)
	networkConfig := networkManager.GetNetworkConfig()
	sc := parseLbConfig(networkConfig)
	err := c.Create(ctx, s.consSvc(sc, pod, c, ctx))
	return pod, cperrors.ToPluginError(err, cperrors.ApiCallError)
}

func (s *SlbPlugin) OnPodUpdated(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	networkManager := utils.NewNetworkManager(pod, c)

	networkStatus, _ := networkManager.GetNetworkStatus()
	networkConfig := networkManager.GetNetworkConfig()
	sc := parseLbConfig(networkConfig)
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
			return pod, cperrors.ToPluginError(c.Create(ctx, s.consSvc(sc, pod, c, ctx)), cperrors.ApiCallError)
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
		return pod, cperrors.ToPluginError(c.Update(ctx, s.consSvc(sc, pod, c, ctx)), cperrors.ApiCallError)
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
			IP: svc.Status.LoadBalancer.Ingress[0].IP,
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

func (s *SlbPlugin) OnPodDeleted(c client.Client, pod *corev1.Pod, ctx context.Context) cperrors.PluginError {
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
		for key := range s.podAllocate {
			gssName := pod.GetLabels()[gamekruiseiov1alpha1.GameServerOwnerGssKey]
			if strings.Contains(key, pod.GetNamespace()+"/"+gssName) {
				podKeys = append(podKeys, key)
			}
		}
	} else {
		podKeys = append(podKeys, pod.GetNamespace()+"/"+pod.GetName())
	}

	for _, podKey := range podKeys {
		s.deAllocate(podKey)
	}

	return nil
}

func (s *SlbPlugin) allocate(lbIds []string, num int, nsName string) (string, []int32) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	var ports []int32
	var lbId string

	// find lb with adequate ports
	for _, slbId := range lbIds {
		sum := 0
		for i := s.minPort; i < s.maxPort; i++ {
			if !s.cache[slbId][i] {
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
		if s.cache[lbId] == nil {
			s.cache[lbId] = make(portAllocated, s.maxPort-s.minPort)
			for i := s.minPort; i < s.maxPort; i++ {
				s.cache[lbId][i] = false
			}
		}

		for p, allocated := range s.cache[lbId] {
			if !allocated {
				port = p
				break
			}
		}
		s.cache[lbId][port] = true
		ports = append(ports, port)
	}

	s.podAllocate[nsName] = lbId + ":" + util.Int32SliceToString(ports, ",")
	log.Infof("pod %s allocate slb %s ports %v", nsName, lbId, ports)
	return lbId, ports
}

func (s *SlbPlugin) deAllocate(nsName string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	allocatedPorts, exist := s.podAllocate[nsName]
	if !exist {
		return
	}

	slbPorts := strings.Split(allocatedPorts, ":")
	lbId := slbPorts[0]
	ports := util.StringToInt32Slice(slbPorts[1], ",")
	for _, port := range ports {
		s.cache[lbId][port] = false
	}

	delete(s.podAllocate, nsName)
	log.Infof("pod %s deallocate slb %s ports %v", nsName, lbId, ports)
}

func init() {
	slbPlugin := SlbPlugin{
		mutex: sync.RWMutex{},
	}
	alibabaCloudProvider.registerPlugin(&slbPlugin)
}

func parseLbConfig(conf []gamekruiseiov1alpha1.NetworkConfParams) *slbConfig {
	var lbIds []string
	ports := make([]int, 0)
	protocols := make([]corev1.Protocol, 0)
	isFixed := false
	for _, c := range conf {
		switch c.Name {
		case SlbIdsConfigName:
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
	return &slbConfig{
		lbIds:       lbIds,
		protocols:   protocols,
		targetPorts: ports,
		isFixed:     isFixed,
	}
}

func getPorts(ports []corev1.ServicePort) []int32 {
	var ret []int32
	for _, port := range ports {
		ret = append(ret, port.Port)
	}
	return ret
}

func (s *SlbPlugin) consSvc(sc *slbConfig, pod *corev1.Pod, c client.Client, ctx context.Context) *corev1.Service {
	var ports []int32
	var lbId string
	podKey := pod.GetNamespace() + "/" + pod.GetName()
	allocatedPorts, exist := s.podAllocate[podKey]
	if exist {
		slbPorts := strings.Split(allocatedPorts, ":")
		lbId = slbPorts[0]
		ports = util.StringToInt32Slice(slbPorts[1], ",")
	} else {
		lbId, ports = s.allocate(sc.lbIds, len(sc.targetPorts), podKey)
	}

	svcPorts := make([]corev1.ServicePort, 0)
	for i := 0; i < len(sc.targetPorts); i++ {
		svcPorts = append(svcPorts, corev1.ServicePort{
			Name:       strconv.Itoa(sc.targetPorts[i]),
			Port:       ports[i],
			Protocol:   sc.protocols[i],
			TargetPort: intstr.FromInt(sc.targetPorts[i]),
		})
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pod.GetName(),
			Namespace: pod.GetNamespace(),
			Annotations: map[string]string{
				SlbListenerOverrideKey: "true",
				SlbIdAnnotationKey:     lbId,
				SlbConfigHashKey:       util.GetHash(sc),
			},
			OwnerReferences: getSvcOwnerReference(c, ctx, pod, sc.isFixed),
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeLoadBalancer,
			Selector: map[string]string{
				SvcSelectorKey: pod.GetName(),
			},
			Ports: svcPorts,
		},
	}
	return svc
}

func getSvcOwnerReference(c client.Client, ctx context.Context, pod *corev1.Pod, isFixed bool) []metav1.OwnerReference {
	ownerReferences := []metav1.OwnerReference{
		{
			APIVersion:         pod.APIVersion,
			Kind:               pod.Kind,
			Name:               pod.GetName(),
			UID:                pod.GetUID(),
			Controller:         pointer.BoolPtr(true),
			BlockOwnerDeletion: pointer.BoolPtr(true),
		},
	}
	if isFixed {
		gss, err := util.GetGameServerSetOfPod(pod, c, ctx)
		if err == nil {
			ownerReferences = []metav1.OwnerReference{
				{
					APIVersion:         gss.APIVersion,
					Kind:               gss.Kind,
					Name:               gss.GetName(),
					UID:                gss.GetUID(),
					Controller:         pointer.BoolPtr(true),
					BlockOwnerDeletion: pointer.BoolPtr(true),
				},
			}
		}
	}
	return ownerReferences
}
