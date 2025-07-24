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

package hwcloud

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	log "k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/cloudprovider"
	cperrors "github.com/openkruise/kruise-game/cloudprovider/errors"
	provideroptions "github.com/openkruise/kruise-game/cloudprovider/options"
	"github.com/openkruise/kruise-game/cloudprovider/utils"
	"github.com/openkruise/kruise-game/pkg/util"
)

const (
	ElbAutocreateAnnotationKey = "kubernetes.io/elb.autocreate"

	CCEElbNetwork = "HwCloud-CCE-ELB"
	AliasCCEELB   = "CCE-ELB-Network"
)

func init() {
	elbPlugin := CCEElbPlugin{
		mutex: sync.RWMutex{},
	}
	hwCloudProvider.registerPlugin(&elbPlugin)
}

type cceElbConfig struct {
	elbIds                    []string
	targetPorts               []int
	protocols                 []corev1.Protocol
	isFixed                   bool
	externalTrafficPolicyType corev1.ServiceExternalTrafficPolicyType
	hwOptions                 map[string]string
}

func (e cceElbConfig) isAutoCreateElb() bool {
	// auto create elb mode annotation
	jsonValue, ok := e.hwOptions[ElbAutocreateAnnotationKey]
	return ok && jsonValue != "" && len(e.elbIds) == 0
}

type ccePortAllocated map[int32]bool

type CCEElbPlugin struct {
	maxPort     int32
	minPort     int32
	blockPorts  []int32
	cache       map[string]ccePortAllocated
	podAllocate map[string]string
	mutex       sync.RWMutex
}

func (s *CCEElbPlugin) Name() string {
	return CCEElbNetwork
}

func (s *CCEElbPlugin) Alias() string {
	return AliasCCEELB
}

func (s *CCEElbPlugin) Init(c client.Client, options cloudprovider.CloudProviderOptions, ctx context.Context) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	elbOptions := options.(provideroptions.HwCloudOptions).CCEELBOptions
	s.minPort = elbOptions.ELBOptions.MinPort
	s.maxPort = elbOptions.ELBOptions.MaxPort
	s.blockPorts = elbOptions.ELBOptions.BlockPorts

	// get all service
	svcList := &corev1.ServiceList{}
	err := c.List(ctx, svcList)
	if err != nil {
		return err
	}

	s.cache, s.podAllocate = initCCELbCache(svcList.Items, s.minPort, s.maxPort, s.blockPorts)
	log.Infof("[%s] podAllocate cache complete initialization: %v", CCEElbNetwork, s.podAllocate)
	return nil
}

// fillCache: you need to add lock before calling this function
func (s *CCEElbPlugin) fillCache(lbId string, usedPorts []int32) {
	if s.cache == nil {
		s.cache = make(map[string]ccePortAllocated)
	}
	if s.cache[lbId] != nil {
		return
	}
	alloc := make(ccePortAllocated, s.maxPort-s.minPort+1)
	for port := s.minPort; port <= s.maxPort; port++ {
		alloc[port] = false
	}
	for _, port := range s.blockPorts {
		if port >= s.minPort && port <= s.maxPort {
			alloc[port] = true
		}
	}
	s.cache[lbId] = alloc
	for _, port := range usedPorts {
		s.cache[lbId][port] = true
	}
}

func (s *CCEElbPlugin) updateCachesAfterAutoCreateElb(c client.Client, name, namespace string,
	interval, totalTimeout time.Duration) {
	if interval > totalTimeout {
		panic("interval must be lesser than timeout")
	}
	log.Infof("Starting periodic cache update for %s/%s (interval: %s, timeout: %s)",
		namespace, name, interval, totalTimeout)
	timeoutCtx, cancel := context.WithTimeout(context.Background(), totalTimeout)
	defer cancel()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var (
		attempt   int
		lastError error
	)

	for {
		select {
		case <-timeoutCtx.Done():
			log.Warningf("Cache update failed for %s/%s after %d attempts. Last error: %v",
				namespace, name, attempt, lastError)
			return

		case <-ticker.C:
			attempt++
			log.Infof("Attempt #%d: updating cache for %s/%s", attempt, namespace, name)

			svc := &corev1.Service{}
			err := c.Get(timeoutCtx, types.NamespacedName{
				Name:      name,
				Namespace: namespace,
			}, svc)

			if err != nil {
				log.Errorf("failed to get Service: %s", err)
				continue
			}

			elbId := svc.Annotations[ElbIdAnnotationKey]
			usedPorts := getCCEPorts(svc.Spec.Ports)

			if elbId == "" || len(usedPorts) == 0 {
				continue
			}

			s.mutex.Lock()
			s.fillCache(elbId, usedPorts)
			if s.podAllocate == nil {
				s.podAllocate = make(map[string]string)
			}
			s.podAllocate[newPodAllocateKey(name, namespace)] = newPodAllocateValue(elbId, usedPorts)
			s.mutex.Unlock()

			log.Infof("Attempt #%d success: updated cache for %s/%s with ELB %s and %d ports",
				attempt, namespace, name, elbId, len(usedPorts))
			return
		}
	}
}

func (s *CCEElbPlugin) OnPodAdded(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	return pod, nil
}

func (s *CCEElbPlugin) OnPodUpdated(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	log.Infof("on update pod begin")
	networkManager := utils.NewNetworkManager(pod, c)
	if networkManager.GetNetworkType() != CCEElbNetwork {
		log.Infof("pod %s/%s network type is not %s, skipping", pod.Namespace, pod.Name, CCEElbNetwork)
		return pod, nil
	}
	networkStatus, _ := networkManager.GetNetworkStatus()
	if networkStatus == nil {
		log.Warningf("network status is nil")
		pod, err := networkManager.UpdateNetworkStatus(gamekruiseiov1alpha1.NetworkStatus{
			CurrentNetworkState: gamekruiseiov1alpha1.NetworkNotReady,
		}, pod)
		return pod, cperrors.ToPluginError(err, cperrors.InternalError)
	}
	networkConfig := networkManager.GetNetworkConfig()
	sc, err := parseCCELbConfig(networkConfig)
	if err != nil {
		log.Errorf("parse elb config failed: %s, network configuration: %#v", err, networkConfig)
		return pod, cperrors.ToPluginError(err, cperrors.ParameterError)
	}
	log.Infof("creating svc %s/%s", pod.GetNamespace(), pod.GetName())
	// get svc
	svc := &corev1.Service{}
	err = c.Get(ctx, types.NamespacedName{
		Name:      pod.GetName(),
		Namespace: pod.GetNamespace(),
	}, svc)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			log.Infof("svc %s/%s not found, will create it", pod.GetNamespace(), pod.GetName())
			service, err := s.consSvc(sc, pod, c, ctx)
			if err != nil {
				return pod, cperrors.ToPluginError(err, cperrors.ParameterError)
			}
			if err = c.Create(ctx, service); err != nil {
				log.Errorf("create svc %s/%s failed: %s", pod.GetNamespace(), pod.GetName(), err)
				return pod, cperrors.ToPluginError(err, cperrors.ApiCallError)
			}
			log.Infof("create svc %s/%s success", pod.GetNamespace(), pod.GetName())
			if sc.isAutoCreateElb() {
				go s.updateCachesAfterAutoCreateElb(c, pod.Name, pod.Namespace, 5*time.Second, 10*time.Minute)
			}
			return pod, cperrors.ToPluginError(nil, cperrors.ApiCallError)
		}
		log.Errorf("get svc %s/%s failed: %s", pod.GetNamespace(), pod.GetName(), err)
		return pod, cperrors.NewPluginError(cperrors.ApiCallError, err.Error())
	}

	// old svc remain
	if svc.OwnerReferences[0].Kind == "Pod" && svc.OwnerReferences[0].UID != pod.UID {
		log.Infof("[%s] waitting old svc %s/%s deleted. old owner pod uid is %s, but now is %s", CCEElbNetwork, svc.Namespace, svc.Name, svc.OwnerReferences[0].UID, pod.UID)
		return pod, nil
	}

	// update svc
	if util.GetHash(sc) != svc.GetAnnotations()[ElbConfigHashKey] {
		networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkNotReady
		pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
		if err != nil {
			return pod, cperrors.NewPluginError(cperrors.InternalError, err.Error())
		}
		service, err := s.consSvc(sc, pod, c, ctx)
		if err != nil {
			return pod, cperrors.NewPluginError(cperrors.ParameterError, err.Error())
		}
		return pod, cperrors.ToPluginError(c.Update(ctx, service), cperrors.ApiCallError)
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

func (s *CCEElbPlugin) OnPodDeleted(c client.Client, pod *corev1.Pod, ctx context.Context) cperrors.PluginError {
	networkManager := utils.NewNetworkManager(pod, c)
	networkConfig := networkManager.GetNetworkConfig()
	if networkManager.GetNetworkType() != CCEElbNetwork {
		log.Infof("pod %s/%s network type is not %s, skipping", pod.Namespace, pod.Name, CCEElbNetwork)
		return nil
	}
	sc, err := parseCCELbConfig(networkConfig)
	if err != nil {
		return cperrors.NewPluginError(cperrors.ParameterError, err.Error())
	}

	var podKeys []string
	if sc.isFixed {
		gss, err := util.GetGameServerSetOfPod(pod, c, ctx)
		if err != nil && !k8serrors.IsNotFound(err) {
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

func (s *CCEElbPlugin) allocate(lbIds []string, num int, podKey string) (string, []int32) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	var ports []int32
	var lbId string

	// find lb with adequate ports
	for _, elbId := range lbIds {
		sum := 0
		for i := s.minPort; i <= s.maxPort; i++ {
			if !s.cache[elbId][i] {
				sum++
			}
			if sum >= num {
				lbId = elbId
				break
			}
		}
	}
	if lbId == "" {
		return "", nil
	}

	// select ports
	for i := 0; i < num; i++ {
		var port int32
		s.fillCache(lbId, nil)
		for p, allocated := range s.cache[lbId] {
			if !allocated {
				port = p
				break
			}
		}
		s.cache[lbId][port] = true
		ports = append(ports, port)
	}
	s.podAllocate[podKey] = newPodAllocateValue(lbId, ports)
	log.Infof("pod %s allocate elb %s ports %v", podKey, lbId, ports)
	return lbId, ports
}

func (s *CCEElbPlugin) deAllocate(nsSvcKey string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	allocatedPorts, exist := s.podAllocate[nsSvcKey]
	if !exist {
		return
	}

	elbPorts := strings.Split(allocatedPorts, ":")
	lbId := elbPorts[0]
	ports := util.StringToInt32Slice(elbPorts[1], ",")
	for _, port := range ports {
		s.cache[lbId][port] = false
	}
	// block ports
	for _, blockPort := range s.blockPorts {
		s.cache[lbId][blockPort] = true
	}

	delete(s.podAllocate, nsSvcKey)
	log.Infof("pod %s deallocate elb %s ports %v", nsSvcKey, lbId, ports)
}

func (s *CCEElbPlugin) consSvc(sc *cceElbConfig, pod *corev1.Pod, c client.Client, ctx context.Context) (*corev1.Service, error) {
	var ports []int32
	var lbId string
	podKey := pod.GetNamespace() + "/" + pod.GetName()
	allocatedPorts, exist := s.podAllocate[podKey]
	if exist {
		elbPorts := strings.Split(allocatedPorts, ":")
		ports = util.StringToInt32Slice(elbPorts[1], ",")
	} else {
		if sc.isAutoCreateElb() {
			lbId, ports = "", s.getPortFromHead(len(sc.targetPorts))
		} else {
			lbId, ports = s.allocate(sc.elbIds, len(sc.targetPorts), podKey)
		}
		if lbId == "" && ports == nil {
			return nil, fmt.Errorf("there are no avaliable ports for %v", sc.elbIds)
		}
	}

	svcPorts := make([]corev1.ServicePort, 0)
	for i := 0; i < len(sc.targetPorts); i++ {
		if sc.protocols[i] == ProtocolTCPUDP {
			svcPorts = append(svcPorts, corev1.ServicePort{
				Name:       fmt.Sprintf("%s-%s", strconv.Itoa(sc.targetPorts[i]), strings.ToLower(string(corev1.ProtocolTCP))),
				Port:       ports[i],
				Protocol:   corev1.ProtocolTCP,
				TargetPort: intstr.FromInt(sc.targetPorts[i]),
			})

			svcPorts = append(svcPorts, corev1.ServicePort{
				Name:       fmt.Sprintf("%s-%s", strconv.Itoa(sc.targetPorts[i]), strings.ToLower(string(corev1.ProtocolUDP))),
				Port:       ports[i],
				Protocol:   corev1.ProtocolUDP,
				TargetPort: intstr.FromInt(sc.targetPorts[i]),
			})

		} else {
			svcPorts = append(svcPorts, corev1.ServicePort{
				Name:       fmt.Sprintf("%s-%s", strconv.Itoa(sc.targetPorts[i]), strings.ToLower(string(sc.protocols[i]))),
				Port:       ports[i],
				Protocol:   sc.protocols[i],
				TargetPort: intstr.FromInt(sc.targetPorts[i]),
			})
		}
	}
	svcAnnotations := make(map[string]string, 0)
	for k, v := range sc.hwOptions {
		svcAnnotations[k] = v
	}
	// add hash to svc, otherwise, the status of GS will remain in NetworkNotReady.
	svcAnnotations[ElbConfigHashKey] = util.GetHash(sc)
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:            pod.GetName(),
			Namespace:       pod.GetNamespace(),
			Annotations:     svcAnnotations,
			OwnerReferences: getCCESvcOwnerReference(c, ctx, pod, sc.isFixed),
		},
		Spec: corev1.ServiceSpec{
			Type:                  corev1.ServiceTypeLoadBalancer,
			ExternalTrafficPolicy: sc.externalTrafficPolicyType,
			Selector: map[string]string{
				SvcSelectorKey: pod.GetName(),
			},
			Ports: svcPorts,
		},
	}
	return svc, nil
}

func (s *CCEElbPlugin) getPortFromHead(num int) []int32 {
	res := make([]int32, 0)
	blocked := make(map[int32]struct{})
	for _, port := range s.blockPorts {
		blocked[port] = struct{}{}
	}
	count := 0
	for i := s.minPort; i <= s.maxPort && count < num; i++ {
		if _, exist := blocked[i]; exist {
			continue
		}
		count++
		res = append(res, i)
	}
	return res
}

func getCCESvcOwnerReference(c client.Client, ctx context.Context, pod *corev1.Pod, isFixed bool) []metav1.OwnerReference {
	ownerReferences := []metav1.OwnerReference{
		{
			APIVersion:         pod.APIVersion,
			Kind:               pod.Kind,
			Name:               pod.GetName(),
			UID:                pod.GetUID(),
			Controller:         ptr.To[bool](true),
			BlockOwnerDeletion: ptr.To[bool](true),
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
					Controller:         ptr.To[bool](true),
					BlockOwnerDeletion: ptr.To[bool](true),
				},
			}
		}
	}
	return ownerReferences
}

func newPodAllocateKey(name, namespace string) string {
	return namespace + "/" + name
}

func newPodAllocateValue(elbId string, ports []int32) string {
	return elbId + ":" + util.Int32SliceToString(ports, ",")
}

func getCCEPorts(ports []corev1.ServicePort) []int32 {
	var ret []int32
	for _, port := range ports {
		ret = append(ret, port.Port)
	}
	return ret
}

func initCCELbCache(svcList []corev1.Service, minPort, maxPort int32, blockPorts []int32) (map[string]ccePortAllocated, map[string]string) {
	newCache := make(map[string]ccePortAllocated)
	newPodAllocate := make(map[string]string)
	for _, svc := range svcList {
		lbId := svc.Annotations[ElbIdAnnotationKey]
		// Associate an existing ELB.
		if lbId != "" && svc.Spec.Type == corev1.ServiceTypeLoadBalancer {
			// init cache for that lb
			if newCache[lbId] == nil {
				newCache[lbId] = make(ccePortAllocated, maxPort-minPort+1)
				for i := minPort; i <= maxPort; i++ {
					newCache[lbId][i] = false
				}
			}

			// block ports
			for _, blockPort := range blockPorts {
				newCache[lbId][blockPort] = true
			}

			// fill in cache for that lb
			var ports []int32
			for _, port := range getCCEPorts(svc.Spec.Ports) {
				if port <= maxPort && port >= minPort {
					value, ok := newCache[lbId][port]
					if !ok || !value {
						newCache[lbId][port] = true
						ports = append(ports, port)
					}
				}
			}
			if len(ports) != 0 {
				newPodAllocate[newPodAllocateKey(svc.GetName(), svc.GetNamespace())] = newPodAllocateValue(lbId, ports)
				log.Infof("svc %s/%s allocate elb %s ports %v", svc.Namespace, svc.Name, lbId, ports)
			}
		}
	}
	return newCache, newPodAllocate
}

func parseCCELbConfig(conf []gamekruiseiov1alpha1.NetworkConfParams) (*cceElbConfig, error) {
	res := &cceElbConfig{
		targetPorts:               make([]int, 0),
		protocols:                 make([]corev1.Protocol, 0),
		isFixed:                   false,
		externalTrafficPolicyType: corev1.ServiceExternalTrafficPolicyTypeCluster,
		hwOptions:                 make(map[string]string),
	}
	specifyElbId := false
	autoCreateElb := false
	for _, c := range conf {
		switch c.Name {
		case ElbIdAnnotationKey:
			if autoCreateElb {
				return nil, fmt.Errorf("%s and %s cannot be filled in simultaneously",
					ElbIdAnnotationKey, ElbAutocreateAnnotationKey)
			}
			specifyElbId = true
			// huawei only supports one elb id
			if c.Value == "" {
				return nil, fmt.Errorf("no elb id found, must specify at least one elb id")
			}
			res.elbIds = []string{c.Value}
			res.hwOptions[c.Name] = c.Value
		case ElbAutocreateAnnotationKey:
			if specifyElbId {
				return nil, fmt.Errorf("%s and %s cannot be filled in simultaneously",
					ElbIdAnnotationKey, ElbAutocreateAnnotationKey)
			}
			autoCreateElb = true
			res.hwOptions[c.Name] = c.Value
		case PortProtocolsConfigName:
			for _, pp := range strings.Split(c.Value, ",") {
				ppSlice := strings.Split(pp, "/")
				port, err := strconv.Atoi(ppSlice[0])
				if err != nil {
					continue
				}
				res.targetPorts = append(res.targetPorts, port)
				if len(ppSlice) != 2 {
					res.protocols = append(res.protocols, corev1.ProtocolTCP)
				} else {
					res.protocols = append(res.protocols, corev1.Protocol(ppSlice[1]))
				}
			}
		case FixedConfigName:
			v, err := strconv.ParseBool(c.Value)
			if err != nil {
				continue
			}
			res.isFixed = v
		case ExternalTrafficPolicyTypeConfigName:
			if strings.EqualFold(c.Value, string(corev1.ServiceExternalTrafficPolicyTypeLocal)) {
				res.externalTrafficPolicyType = corev1.ServiceExternalTrafficPolicyTypeLocal
			}
		default:
			res.hwOptions[c.Name] = c.Value
		}
	}
	return res, nil
}
