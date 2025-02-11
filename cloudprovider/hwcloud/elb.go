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
	"encoding/json"
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
	PortProtocolsConfigName             = "PortProtocols"
	ExternalTrafficPolicyTypeConfigName = "ExternalTrafficPolicyType"
	PublishNotReadyAddressesConfigName  = "PublishNotReadyAddresses"

	ElbIdAnnotationKey = "kubernetes.io/elb.id"
	ElbIdsConfigName   = "ElbIds"

	ElbClassAnnotationKey = "kubernetes.io/elb.class"
	ElbClassConfigName    = "ElbClass"

	ElbAvailableZoneAnnotationKey        = "kubernetes.io/elb.availability-zones"
	ElbAvailableZoneAnnotationConfigName = "ElbAvailableZone"

	ElbConnLimitAnnotationKey = "kubernetes.io/elb.connection-limit"
	ElbConnLimitConfigName    = "ElbConnLimit"

	ElbSubnetAnnotationKey = "kubernetes.io/elb.subnet-id"
	ElbSubnetConfigName    = "ElbSubnetId"

	ElbEipAnnotationKey = "kubernetes.io/elb.eip-id"
	ElbEipConfigName    = "ElbEipId"

	ElbEipKeepAnnotationKey = "kubernetes.io/elb.keep-eip"
	ElbEipKeepConfigName    = "ElbKeepd"

	ElbEipAutoCreateOptionAnnotationKey = "kubernetes.io/elb.eip-auto-create-option"
	ElbEipAutoCreateOptionConfigName    = "ElbEipAutoCreateOption"

	ElbLbAlgorithmAnnotationKey = "kubernetes.io/elb.lb-algorithm"
	ElbLbAlgorithmConfigName    = "ElbLbAlgorithm"

	ElbSessionAffinityFlagAnnotationKey = "kubernetes.io/elb.session-affinity-flag"
	ElbSessionAffinityFlagConfigName    = "ElbSessionAffinityFlag"

	ElbSessionAffinityOptionAnnotationKey = "kubernetes.io/elb.session-affinity-option"
	ElbSessionAffinityOptionConfigName    = "ElbSessionAffinityOption"

	ElbTransparentClientIPAnnotationKey = "kubernetes.io/elb.enable-transparent-client-ip"
	ElbTransparentClientIPConfigName    = "ElbTransparentClientIP"

	ElbXForwardedHostAnnotationKey = "kubernetes.io/elb.x-forwarded-host"
	ElbXForwardedHostConfigName    = "ElbXForwardedHost"

	ElbTlsRefAnnotationKey = "kubernetes.io/elb.default-tls-container-ref"
	ElbTlsRefConfigName    = "ElbTlsRef"

	ElbIdleTimeoutAnnotationKey = "kubernetes.io/elb.idle-timeout"
	ElbIdleTimeoutConfigName    = "ElbIdleTimeout"

	ElbRequestTimeoutAnnotationKey = "kubernetes.io/elb.request-timeout"
	ElbRequestTimeoutConfigName    = "ElbRequestTimeout"

	ElbResponseTimeoutAnnotationKey = "kubernetes.io/elb.response-timeout"
	ElbResponseTimeoutConfigName    = "ElbResponseTimeout"

	ElbEnableCrossVPCAnnotationKey = "kubernetes.io/elb.enable-cross-vpc"
	ElbEnableCrossVPCConfigName    = "ElbEnableCrossVPC"

	ElbL4FlavorIDAnnotationKey = "kubernetes.io/elb.l4-flavor-id"
	ElbL4FlavorIDConfigName    = "ElbL4FlavorID"

	ElbL7FlavorIDAnnotationKey = "kubernetes.io/elb.l7-flavor-id"
	ElbL7FlavorIDConfigName    = "ElbL7FlavorID"

	LBHealthCheckSwitchAnnotationKey = "kubernetes.io/elb.health-check-flag"
	LBHealthCheckSwitchConfigName    = "LBHealthCheckFlag"

	LBHealthCheckOptionAnnotationKey = "kubernetes.io/elb.health-check-option"
	LBHealthCHeckOptionConfigName    = "LBHealthCheckOption"
)

const (
	ElbConfigHashKey                 = "game.kruise.io/network-config-hash"
	SvcSelectorKey                   = "statefulset.kubernetes.io/pod-name"
	ProtocolTCPUDP   corev1.Protocol = "TCPUDP"
	FixedConfigName                  = "Fixed"

	ElbNetwork        = "HwCloud-ELB"
	AliasELB          = "ELB-Network"
	ElbClassDedicated = "dedicated"
	ElbClassShared    = "shared"

	ElbLbAlgorithmRoundRobin = "ROUND_ROBIN"
	ElbLbAlgorithmLeastConn  = "LEAST_CONNECTIONS"
	ElbLbAlgorithmSourceIP   = "SOURCE_IP"
)

type portAllocated map[int32]bool

type ElbPlugin struct {
	maxPort     int32
	minPort     int32
	blockPorts  []int32
	cache       map[string]portAllocated
	podAllocate map[string]string
	mutex       sync.RWMutex
}

type elbConfig struct {
	lbIds       []string
	targetPorts []int
	protocols   []corev1.Protocol
	isFixed     bool

	elbClass                 string
	elbConnLimit             int32
	elbLbAlgorithm           string
	elbSessionAffinityFlag   string
	elbSessionAffinityOption string
	elbTransparentClientIP   bool
	elbXForwardedHost        bool
	elbIdleTimeout           int32
	elbRequestTimeout        int32
	elbResponseTimeout       int32

	externalTrafficPolicyType corev1.ServiceExternalTrafficPolicyType
	publishNotReadyAddresses  bool

	lBHealthCheckSwitch  string
	lBHealtchCheckOption string
}

func (s *ElbPlugin) Name() string {
	return ElbNetwork
}

func (s *ElbPlugin) Alias() string {
	return AliasELB
}

func (s *ElbPlugin) Init(c client.Client, options cloudprovider.CloudProviderOptions, ctx context.Context) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	elbOptions := options.(provideroptions.HwCloudOptions).ELBOptions
	s.minPort = elbOptions.MinPort
	s.maxPort = elbOptions.MaxPort
	s.blockPorts = elbOptions.BlockPorts

	svcList := &corev1.ServiceList{}
	err := c.List(ctx, svcList)
	if err != nil {
		return err
	}

	s.cache, s.podAllocate = initLbCache(svcList.Items, s.minPort, s.maxPort, s.blockPorts)
	log.Infof("[%s] podAllocate cache complete initialization: %v", ElbNetwork, s.podAllocate)
	return nil
}

func initLbCache(svcList []corev1.Service, minPort, maxPort int32, blockPorts []int32) (map[string]portAllocated, map[string]string) {
	newCache := make(map[string]portAllocated)
	newPodAllocate := make(map[string]string)
	for _, svc := range svcList {
		lbId := svc.Annotations[ElbIdAnnotationKey]
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
				log.Infof("svc %s/%s allocate elb %s ports %v", svc.Namespace, svc.Name, lbId, ports)
			}
		}
	}
	return newCache, newPodAllocate
}

func (s *ElbPlugin) OnPodAdded(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	return pod, nil
}

func (s *ElbPlugin) OnPodUpdated(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
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
		log.Infof("[%s] waitting old svc %s/%s deleted. old owner pod uid is %s, but now is %s", ElbNetwork, svc.Namespace, svc.Name, svc.OwnerReferences[0].UID, pod.UID)
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

func (s *ElbPlugin) OnPodDeleted(c client.Client, pod *corev1.Pod, ctx context.Context) cperrors.PluginError {
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

func (s *ElbPlugin) allocate(lbIds []string, num int, nsName string) (string, []int32) {
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

func (s *ElbPlugin) deAllocate(nsName string) {
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
	elbPlugin := ElbPlugin{
		mutex: sync.RWMutex{},
	}
	hwCloudProvider.registerPlugin(&elbPlugin)
}

func parseLbConfig(conf []gamekruiseiov1alpha1.NetworkConfParams) (*elbConfig, error) {
	var lbIds []string
	ports := make([]int, 0)
	protocols := make([]corev1.Protocol, 0)
	isFixed := false

	externalTrafficPolicy := corev1.ServiceExternalTrafficPolicyTypeCluster
	publishNotReadyAddresses := false

	elbClass := ElbClassDedicated
	elbConnLimit := int32(-1)
	elbLbAlgorithm := ElbLbAlgorithmRoundRobin
	elbSessionAffinityFlag := "off"
	elbSessionAffinityOption := ""
	elbTransparentClientIP := false
	elbXForwardedHost := false
	elbIdleTimeout := int32(-1)
	elbRequestTimeout := int32(-1)
	elbResponseTimeout := int32(-1)

	lBHealthCheckSwitch := "on"
	LBHealthCHeckOptionConfig := ""

	for _, c := range conf {
		switch c.Name {
		case ElbIdsConfigName:
			for _, slbId := range strings.Split(c.Value, ",") {
				if slbId != "" {
					lbIds = append(lbIds, slbId)
				}
			}

			if len(lbIds) <= 0 {
				return nil, fmt.Errorf("no elb id found, must specify at least one elb id")
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
		case PublishNotReadyAddressesConfigName:
			v, err := strconv.ParseBool(c.Value)
			if err != nil {
				continue
			}
			publishNotReadyAddresses = v
		case ElbClassConfigName:
			if strings.EqualFold(c.Value, string(ElbClassShared)) {
				elbClass = ElbClassShared
			}

		case ElbConnLimitConfigName:
			v, err := strconv.Atoi(c.Value)
			if err != nil {
				_ = fmt.Errorf("ignore invalid elb connection limit value: %s", c.Value)
				continue
			}
			elbConnLimit = int32(v)
		case ElbLbAlgorithmConfigName:
			if strings.EqualFold(c.Value, ElbLbAlgorithmRoundRobin) {
				elbLbAlgorithm = ElbLbAlgorithmRoundRobin
			}

			if strings.EqualFold(c.Value, ElbLbAlgorithmLeastConn) {
				elbLbAlgorithm = ElbLbAlgorithmLeastConn
			}

			if strings.EqualFold(c.Value, ElbLbAlgorithmSourceIP) {
				elbLbAlgorithm = ElbLbAlgorithmSourceIP
			}
		case ElbSessionAffinityFlagConfigName:
			if strings.EqualFold(c.Value, "on") {
				elbSessionAffinityFlag = "on"
			}
		case ElbSessionAffinityOptionConfigName:
			if json.Valid([]byte(c.Value)) {
				LBHealthCHeckOptionConfig = c.Value
			} else {
				return nil, fmt.Errorf("invalid elb session affinity option value: %s", c.Value)
			}
			elbSessionAffinityOption = c.Value
		case ElbTransparentClientIPConfigName:
			v, err := strconv.ParseBool(c.Value)
			if err != nil {
				_ = fmt.Errorf("ignore invalid elb transparent client ip value: %s", c.Value)
				continue
			}
			elbTransparentClientIP = v
		case ElbXForwardedHostConfigName:
			v, err := strconv.ParseBool(c.Value)
			if err != nil {
				_ = fmt.Errorf("ignore invalid elb x forwarded host value: %s", c.Value)
				continue
			}
			elbXForwardedHost = v
		case ElbIdleTimeoutConfigName:
			v, err := strconv.Atoi(c.Value)
			if err != nil {
				_ = fmt.Errorf("ignore invalid elb idle timeout value: %s", c.Value)
				continue
			}
			if v >= 0 && v <= 4000 {
				elbIdleTimeout = int32(v)
			} else {
				_ = fmt.Errorf("ignore invalid elb idle timeout value: %s", c.Value)
				continue
			}
		case ElbRequestTimeoutConfigName:
			v, err := strconv.Atoi(c.Value)
			if err != nil {
				_ = fmt.Errorf("ignore invalid elb request timeout value: %s", c.Value)
				continue
			}
			if v >= 1 && v <= 300 {
				elbRequestTimeout = int32(v)
			} else {
				_ = fmt.Errorf("ignore invalid elb request timeout value: %s", c.Value)
				continue
			}

		case ElbResponseTimeoutConfigName:
			v, err := strconv.Atoi(c.Value)
			if err != nil {
				_ = fmt.Errorf("ignore invalid elb response timeout value: %s", c.Value)
				continue
			}
			if v >= 1 && v <= 300 {
				elbResponseTimeout = int32(v)
			} else {
				_ = fmt.Errorf("ignore invalid elb response timeout value: %s", c.Value)
				continue
			}
		case LBHealthCheckSwitchConfigName:
			checkSwitch := strings.ToLower(c.Value)
			if checkSwitch != "on" && checkSwitch != "off" {
				return nil, fmt.Errorf("invalid lb health check switch value: %s", c.Value)
			}
			lBHealthCheckSwitch = checkSwitch
		case LBHealthCHeckOptionConfigName:
			if json.Valid([]byte(c.Value)) {
				LBHealthCHeckOptionConfig = c.Value
			} else {
				return nil, fmt.Errorf("invalid lb health check option value: %s", c.Value)
			}
		}
	}
	return &elbConfig{
		lbIds:                     lbIds,
		protocols:                 protocols,
		targetPorts:               ports,
		isFixed:                   isFixed,
		externalTrafficPolicyType: externalTrafficPolicy,
		publishNotReadyAddresses:  publishNotReadyAddresses,
		elbClass:                  elbClass,
		elbConnLimit:              elbConnLimit,
		elbLbAlgorithm:            elbLbAlgorithm,
		elbSessionAffinityFlag:    elbSessionAffinityFlag,
		elbSessionAffinityOption:  elbSessionAffinityOption,
		elbTransparentClientIP:    elbTransparentClientIP,
		elbXForwardedHost:         elbXForwardedHost,
		elbIdleTimeout:            elbIdleTimeout,
		elbRequestTimeout:         elbRequestTimeout,
		elbResponseTimeout:        elbResponseTimeout,
		lBHealthCheckSwitch:       lBHealthCheckSwitch,
		lBHealtchCheckOption:      LBHealthCHeckOptionConfig,
	}, nil
}

func getPorts(ports []corev1.ServicePort) []int32 {
	var ret []int32
	for _, port := range ports {
		ret = append(ret, port.Port)
	}
	return ret
}

func (s *ElbPlugin) consSvc(sc *elbConfig, pod *corev1.Pod, c client.Client, ctx context.Context) (*corev1.Service, error) {
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

	svcAnnotations := map[string]string{
		ElbIdAnnotationKey:                    lbId,
		ElbConfigHashKey:                      util.GetHash(sc),
		ElbClassAnnotationKey:                 sc.elbClass,
		ElbLbAlgorithmAnnotationKey:           sc.elbLbAlgorithm,
		ElbSessionAffinityFlagAnnotationKey:   sc.elbSessionAffinityFlag,
		ElbSessionAffinityOptionAnnotationKey: sc.elbSessionAffinityOption,
		ElbTransparentClientIPAnnotationKey:   strconv.FormatBool(sc.elbTransparentClientIP),
		ElbXForwardedHostAnnotationKey:        strconv.FormatBool(sc.elbXForwardedHost),
		LBHealthCheckSwitchAnnotationKey:      sc.lBHealthCheckSwitch,
	}

	if sc.elbClass == ElbClassDedicated {
	} else {
		svcAnnotations[ElbConnLimitAnnotationKey] = strconv.Itoa(int(sc.elbConnLimit))
	}

	if sc.elbIdleTimeout != -1 {
		svcAnnotations[ElbIdleTimeoutAnnotationKey] = strconv.Itoa(int(sc.elbIdleTimeout))
	}

	if sc.elbRequestTimeout != -1 {
		svcAnnotations[ElbRequestTimeoutAnnotationKey] = strconv.Itoa(int(sc.elbRequestTimeout))
	}

	if sc.elbResponseTimeout != -1 {
		svcAnnotations[ElbResponseTimeoutAnnotationKey] = strconv.Itoa(int(sc.elbResponseTimeout))
	}

	if sc.lBHealthCheckSwitch == "on" {
		svcAnnotations[LBHealthCheckOptionAnnotationKey] = sc.lBHealtchCheckOption
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:            pod.GetName(),
			Namespace:       pod.GetNamespace(),
			Annotations:     svcAnnotations,
			OwnerReferences: getSvcOwnerReference(c, ctx, pod, sc.isFixed),
		},
		Spec: corev1.ServiceSpec{
			Type:                     corev1.ServiceTypeLoadBalancer,
			ExternalTrafficPolicy:    sc.externalTrafficPolicyType,
			PublishNotReadyAddresses: sc.publishNotReadyAddresses,
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
