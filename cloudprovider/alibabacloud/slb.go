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
	"fmt"
	"strconv"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
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
	SlbNetwork                          = "AlibabaCloud-SLB"
	AliasSLB                            = "LB-Network"
	SlbIdsConfigName                    = "SlbIds"
	PortProtocolsConfigName             = "PortProtocols"
	ExternalTrafficPolicyTypeConfigName = "ExternalTrafficPolicyType"
	SlbListenerOverrideKey              = "service.beta.kubernetes.io/alibaba-cloud-loadbalancer-force-override-listeners"
	SlbIdAnnotationKey                  = "service.beta.kubernetes.io/alibaba-cloud-loadbalancer-id"
	SlbIdLabelKey                       = "service.k8s.alibaba/loadbalancer-id"
	SvcSelectorKey                      = "statefulset.kubernetes.io/pod-name"
	SlbConfigHashKey                    = "game.kruise.io/network-config-hash"
)

const (
	// annotations provided by AlibabaCloud Cloud Controller Manager
	LBHealthCheckSwitchAnnotationKey       = "service.beta.kubernetes.io/alibaba-cloud-loadbalancer-health-check-switch"
	LBHealthCheckProtocolPortAnnotationKey = "service.beta.kubernetes.io/alibaba-cloud-loadbalancer-protocol-port"

	// ConfigNames defined by OKG
	LBHealthCheckSwitchConfigName       = "LBHealthCheckSwitch"
	LBHealthCheckProtocolPortConfigName = "LBHealthCheckProtocolPort"
)

type portAllocated map[int32]bool

type SlbPlugin struct {
	maxPort     int32
	minPort     int32
	blockPorts  []int32
	cache       map[string]portAllocated
	podAllocate map[string]string
	mutex       sync.RWMutex
}

type slbConfig struct {
	lbIds       []string
	targetPorts []int
	protocols   []corev1.Protocol
	isFixed     bool

	externalTrafficPolicyType   corev1.ServiceExternalTrafficPolicyType
	lBHealthCheckSwitch         string
	lBHealthCheckProtocolPort   string
	lBHealthCheckFlag           string
	lBHealthCheckType           string
	lBHealthCheckConnectTimeout string
	lBHealthCheckInterval       string
	lBHealthCheckUri            string
	lBHealthCheckDomain         string
	lBHealthCheckMethod         string
	lBHealthyThreshold          string
	lBUnhealthyThreshold        string
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
	s.blockPorts = slbOptions.BlockPorts

	svcList := &corev1.ServiceList{}
	err := c.List(ctx, svcList)
	if err != nil {
		return err
	}

	s.cache, s.podAllocate = initLbCache(svcList.Items, s.minPort, s.maxPort, s.blockPorts)
	log.Infof("[%s] podAllocate cache complete initialization: %v", SlbNetwork, s.podAllocate)
	return nil
}

func initLbCache(svcList []corev1.Service, minPort, maxPort int32, blockPorts []int32) (map[string]portAllocated, map[string]string) {
	newCache := make(map[string]portAllocated)
	newPodAllocate := make(map[string]string)
	for _, svc := range svcList {
		lbId := svc.Labels[SlbIdLabelKey]
		if lbId != "" && svc.Spec.Type == corev1.ServiceTypeLoadBalancer {
			// init cache for that lb
			if newCache[lbId] == nil {
				newCache[lbId] = make(portAllocated, maxPort-minPort+1)
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
			for _, port := range getPorts(svc.Spec.Ports) {
				if port <= maxPort && port >= minPort {
					value, ok := newCache[lbId][port]
					if !ok || !value {
						newCache[lbId][port] = true
						ports = append(ports, port)
					}
				}
			}
			if len(ports) != 0 {
				newPodAllocate[svc.GetNamespace()+"/"+svc.GetName()] = lbId + ":" + util.Int32SliceToString(ports, ",")
			}
		}
	}
	return newCache, newPodAllocate
}

func (s *SlbPlugin) OnPodAdded(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	return pod, nil
}

func (s *SlbPlugin) OnPodUpdated(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	networkManager := utils.NewNetworkManager(pod, c)

	networkStatus, _ := networkManager.GetNetworkStatus()
	if networkStatus == nil {
		pod, err := networkManager.UpdateNetworkStatus(gamekruiseiov1alpha1.NetworkStatus{
			CurrentNetworkState: gamekruiseiov1alpha1.NetworkNotReady,
		}, pod)
		return pod, cperrors.ToPluginError(err, cperrors.InternalError)
	}
	networkConfig := networkManager.GetNetworkConfig()
	sc, err := parseLbConfig(networkConfig)
	if err != nil {
		return pod, cperrors.ToPluginError(err, cperrors.ParameterError)
	}

	// get svc
	svc := &corev1.Service{}
	err = c.Get(ctx, types.NamespacedName{
		Name:      pod.GetName(),
		Namespace: pod.GetNamespace(),
	}, svc)
	if err != nil {
		if errors.IsNotFound(err) {
			service, err := s.consSvc(sc, pod, c, ctx)
			if err != nil {
				return pod, cperrors.ToPluginError(err, cperrors.ParameterError)
			}
			return pod, cperrors.ToPluginError(c.Create(ctx, service), cperrors.ApiCallError)
		}
		return pod, cperrors.NewPluginError(cperrors.ApiCallError, err.Error())
	}

	// old svc remain
	if svc.OwnerReferences[0].Kind == "Pod" && svc.OwnerReferences[0].UID != pod.UID {
		log.Infof("[%s] waitting old svc %s/%s deleted. old owner pod uid is %s, but now is %s", SlbNetwork, svc.Namespace, svc.Name, svc.OwnerReferences[0].UID, pod.UID)
		return pod, nil
	}

	// update svc
	if util.GetHash(sc) != svc.GetAnnotations()[SlbConfigHashKey] {
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

func (s *SlbPlugin) OnPodDeleted(c client.Client, pod *corev1.Pod, ctx context.Context) cperrors.PluginError {
	networkManager := utils.NewNetworkManager(pod, c)
	networkConfig := networkManager.GetNetworkConfig()
	sc, err := parseLbConfig(networkConfig)
	if err != nil {
		return cperrors.NewPluginError(cperrors.ParameterError, err.Error())
	}

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
		for i := s.minPort; i <= s.maxPort; i++ {
			if !s.cache[slbId][i] {
				sum++
			}
			if sum >= num {
				lbId = slbId
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
		if s.cache[lbId] == nil {
			// init cache for new lb
			s.cache[lbId] = make(portAllocated, s.maxPort-s.minPort+1)
			for i := s.minPort; i <= s.maxPort; i++ {
				s.cache[lbId][i] = false
			}
			// block ports
			for _, blockPort := range s.blockPorts {
				s.cache[lbId][blockPort] = true
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
	// block ports
	for _, blockPort := range s.blockPorts {
		s.cache[lbId][blockPort] = true
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

func parseLbConfig(conf []gamekruiseiov1alpha1.NetworkConfParams) (*slbConfig, error) {
	var lbIds []string
	ports := make([]int, 0)
	protocols := make([]corev1.Protocol, 0)
	isFixed := false

	externalTrafficPolicy := corev1.ServiceExternalTrafficPolicyTypeCluster
	lBHealthCheckSwitch := "on"
	lBHealthCheckProtocolPort := ""
	lBHealthCheckFlag := "off"
	lBHealthCheckType := "tcp"
	lBHealthCheckConnectTimeout := "5"
	lBHealthCheckInterval := "10"
	lBUnhealthyThreshold := "2"
	lBHealthyThreshold := "2"
	lBHealthCheckUri := ""
	lBHealthCheckDomain := ""
	lBHealthCheckMethod := ""
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
		case ExternalTrafficPolicyTypeConfigName:
			if strings.EqualFold(c.Value, string(corev1.ServiceExternalTrafficPolicyTypeLocal)) {
				externalTrafficPolicy = corev1.ServiceExternalTrafficPolicyTypeLocal
			}
		case LBHealthCheckSwitchConfigName:
			checkSwitch := strings.ToLower(c.Value)
			if checkSwitch != "on" && checkSwitch != "off" {
				return nil, fmt.Errorf("invalid lb health check switch value: %s", c.Value)
			}
			lBHealthCheckSwitch = checkSwitch
		case LBHealthCheckFlagConfigName:
			flag := strings.ToLower(c.Value)
			if flag != "on" && flag != "off" {
				return nil, fmt.Errorf("invalid lb health check flag value: %s", c.Value)
			}
			lBHealthCheckFlag = flag
		case LBHealthCheckTypeConfigName:
			checkType := strings.ToLower(c.Value)
			if checkType != "tcp" && checkType != "http" {
				return nil, fmt.Errorf("invalid lb health check type: %s", c.Value)
			}
			lBHealthCheckType = checkType
		case LBHealthCheckProtocolPortConfigName:
			if validateHttpProtocolPort(c.Value) != nil {
				return nil, fmt.Errorf("invalid lb health check protocol port: %s", c.Value)
			}
			lBHealthCheckProtocolPort = c.Value
		case LBHealthCheckConnectTimeoutConfigName:
			timeoutInt, err := strconv.Atoi(c.Value)
			if err != nil {
				return nil, fmt.Errorf("invalid lb health check connect timeout: %s", c.Value)
			}
			if timeoutInt < 1 || timeoutInt > 300 {
				return nil, fmt.Errorf("invalid lb health check connect timeout: %d", timeoutInt)
			}
			lBHealthCheckConnectTimeout = c.Value
		case LBHealthCheckIntervalConfigName:
			intervalInt, err := strconv.Atoi(c.Value)
			if err != nil {
				return nil, fmt.Errorf("invalid lb health check interval: %s", c.Value)
			}
			if intervalInt < 1 || intervalInt > 50 {
				return nil, fmt.Errorf("invalid lb health check interval: %d", intervalInt)
			}
			lBHealthCheckInterval = c.Value
		case LBHealthyThresholdConfigName:
			thresholdInt, err := strconv.Atoi(c.Value)
			if err != nil {
				return nil, fmt.Errorf("invalid lb healthy threshold: %s", c.Value)
			}
			if thresholdInt < 2 || thresholdInt > 10 {
				return nil, fmt.Errorf("invalid lb healthy threshold: %d", thresholdInt)
			}
			lBHealthyThreshold = c.Value
		case LBUnhealthyThresholdConfigName:
			thresholdInt, err := strconv.Atoi(c.Value)
			if err != nil {
				return nil, fmt.Errorf("invalid lb unhealthy threshold: %s", c.Value)
			}
			if thresholdInt < 2 || thresholdInt > 10 {
				return nil, fmt.Errorf("invalid lb unhealthy threshold: %d", thresholdInt)
			}
			lBUnhealthyThreshold = c.Value
		case LBHealthCheckUriConfigName:
			if validateUri(c.Value) != nil {
				return nil, fmt.Errorf("invalid lb health check uri: %s", c.Value)
			}
			lBHealthCheckUri = c.Value
		case LBHealthCheckDomainConfigName:
			if validateDomain(c.Value) != nil {
				return nil, fmt.Errorf("invalid lb health check domain: %s", c.Value)
			}
			lBHealthCheckDomain = c.Value
		case LBHealthCheckMethodConfigName:
			method := strings.ToLower(c.Value)
			if method != "get" && method != "head" {
				return nil, fmt.Errorf("invalid lb health check method: %s", c.Value)
			}
			lBHealthCheckMethod = method
		}
	}
	return &slbConfig{
		lbIds:                       lbIds,
		protocols:                   protocols,
		targetPorts:                 ports,
		isFixed:                     isFixed,
		externalTrafficPolicyType:   externalTrafficPolicy,
		lBHealthCheckSwitch:         lBHealthCheckSwitch,
		lBHealthCheckFlag:           lBHealthCheckFlag,
		lBHealthCheckType:           lBHealthCheckType,
		lBHealthCheckProtocolPort:   lBHealthCheckProtocolPort,
		lBHealthCheckConnectTimeout: lBHealthCheckConnectTimeout,
		lBHealthCheckInterval:       lBHealthCheckInterval,
		lBHealthCheckUri:            lBHealthCheckUri,
		lBHealthCheckDomain:         lBHealthCheckDomain,
		lBHealthCheckMethod:         lBHealthCheckMethod,
		lBHealthyThreshold:          lBHealthyThreshold,
		lBUnhealthyThreshold:        lBUnhealthyThreshold,
	}, nil
}

func getPorts(ports []corev1.ServicePort) []int32 {
	var ret []int32
	for _, port := range ports {
		ret = append(ret, port.Port)
	}
	return ret
}

func (s *SlbPlugin) consSvc(sc *slbConfig, pod *corev1.Pod, c client.Client, ctx context.Context) (*corev1.Service, error) {
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
		if lbId == "" && ports == nil {
			return nil, fmt.Errorf("there are no avaialable ports for %v", sc.lbIds)
		}
	}

	svcPorts := make([]corev1.ServicePort, 0)
	for i := 0; i < len(sc.targetPorts); i++ {
		if sc.protocols[i] == ProtocolTCPUDP {
			svcPorts = append(svcPorts, corev1.ServicePort{
				Name:       fmt.Sprintf("%s-%s", strconv.Itoa(sc.targetPorts[i]), corev1.ProtocolTCP),
				Port:       ports[i],
				Protocol:   corev1.ProtocolTCP,
				TargetPort: intstr.FromInt(sc.targetPorts[i]),
			})

			svcPorts = append(svcPorts, corev1.ServicePort{
				Name:       fmt.Sprintf("%s-%s", strconv.Itoa(sc.targetPorts[i]), corev1.ProtocolUDP),
				Port:       ports[i],
				Protocol:   corev1.ProtocolUDP,
				TargetPort: intstr.FromInt(sc.targetPorts[i]),
			})

		} else {
			svcPorts = append(svcPorts, corev1.ServicePort{
				Name:       fmt.Sprintf("%s-%s", strconv.Itoa(sc.targetPorts[i]), sc.protocols[i]),
				Port:       ports[i],
				Protocol:   sc.protocols[i],
				TargetPort: intstr.FromInt(sc.targetPorts[i]),
			})
		}
	}

	svcAnnotations := map[string]string{
		SlbListenerOverrideKey:           "true",
		SlbIdAnnotationKey:               lbId,
		SlbConfigHashKey:                 util.GetHash(sc),
		LBHealthCheckFlagAnnotationKey:   sc.lBHealthCheckFlag,
		LBHealthCheckSwitchAnnotationKey: sc.lBHealthCheckSwitch,
	}
	if sc.lBHealthCheckSwitch == "on" {
		svcAnnotations[LBHealthCheckTypeAnnotationKey] = sc.lBHealthCheckType
		svcAnnotations[LBHealthCheckConnectTimeoutAnnotationKey] = sc.lBHealthCheckConnectTimeout
		svcAnnotations[LBHealthCheckIntervalAnnotationKey] = sc.lBHealthCheckInterval
		svcAnnotations[LBHealthyThresholdAnnotationKey] = sc.lBHealthyThreshold
		svcAnnotations[LBUnhealthyThresholdAnnotationKey] = sc.lBUnhealthyThreshold
		if sc.lBHealthCheckType == "http" {
			svcAnnotations[LBHealthCheckProtocolPortAnnotationKey] = sc.lBHealthCheckProtocolPort
			svcAnnotations[LBHealthCheckDomainAnnotationKey] = sc.lBHealthCheckDomain
			svcAnnotations[LBHealthCheckUriAnnotationKey] = sc.lBHealthCheckUri
			svcAnnotations[LBHealthCheckMethodAnnotationKey] = sc.lBHealthCheckMethod
		}
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        pod.GetName(),
			Namespace:   pod.GetNamespace(),
			Annotations: svcAnnotations,
			Labels: map[string]string{
				ServiceProxyName: "dummy",
			},
			OwnerReferences: getSvcOwnerReference(c, ctx, pod, sc.isFixed),
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

func getSvcOwnerReference(c client.Client, ctx context.Context, pod *corev1.Pod, isFixed bool) []metav1.OwnerReference {
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

func validateHttpProtocolPort(protocolPort string) error {
	protocolPorts := strings.Split(protocolPort, ",")
	for _, pp := range protocolPorts {
		protocol := strings.Split(pp, ":")[0]
		if protocol != "http" && protocol != "https" {
			return fmt.Errorf("invalid http protocol: %s", protocol)
		}
		port := strings.Split(pp, ":")[1]
		_, err := strconv.Atoi(port)
		if err != nil {
			return fmt.Errorf("invalid http port: %s", port)
		}
	}
	return nil
}
