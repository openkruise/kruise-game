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
	"fmt"
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
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strconv"
	"strings"
	"sync"
)

const (
	MultiNlbsNetwork = "AlibabaCloud-Multi-NLBs"
	AliasMultiNlbs   = "Multi-NLBs-Network"

	// ConfigNames defined by OKG
	NlbIdNamesConfigName = "NlbIdNames"

	// service annotation defined by OKG
	LBIDBelongIndexKey = "game.kruise.io/lb-belong-index"

	// service label defined by OKG
	ServiceBelongNetworkTypeKey = "game.kruise.io/network-type"

	ProtocolTCPUDP corev1.Protocol = "TCPUDP"

	PrefixReadyReadinessGate = "service.readiness.alibabacloud.com/"
)

type MultiNlbsPlugin struct {
	maxPort    int32
	minPort    int32
	blockPorts []int32
	cache      [][]bool
	// podAllocate format {pod ns/name}: -{lbId: xxx-a, port: -8001 -8002} -{lbId: xxx-b, port: -8001 -8002}
	podAllocate map[string]*lbsPorts
	mutex       sync.RWMutex
}

type lbsPorts struct {
	index      int
	lbIds      []string
	ports      []int32
	targetPort []int
	protocols  []corev1.Protocol
}

func (m *MultiNlbsPlugin) Name() string {
	return MultiNlbsNetwork
}

func (m *MultiNlbsPlugin) Alias() string {
	return AliasMultiNlbs
}

func (m *MultiNlbsPlugin) Init(c client.Client, options cloudprovider.CloudProviderOptions, ctx context.Context) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	nlbOptions := options.(provideroptions.AlibabaCloudOptions).NLBOptions
	m.minPort = nlbOptions.MinPort
	m.maxPort = nlbOptions.MaxPort
	m.blockPorts = nlbOptions.BlockPorts

	svcList := &corev1.ServiceList{}
	err := c.List(ctx, svcList, client.MatchingLabels{ServiceBelongNetworkTypeKey: MultiNlbsNetwork})
	if err != nil {
		return err
	}
	m.podAllocate, m.cache = initMultiLBCache(svcList.Items, m.maxPort, m.minPort, m.blockPorts)

	log.Infof("[%s] podAllocate cache complete initialization: ", MultiNlbsNetwork)
	for podNsName, lps := range m.podAllocate {
		log.Infof("[%s] pod %s: %v", MultiNlbsNetwork, podNsName, *lps)
	}
	return nil
}

func initMultiLBCache(svcList []corev1.Service, maxPort, minPort int32, blockPorts []int32) (map[string]*lbsPorts, [][]bool) {
	podAllocate := make(map[string]*lbsPorts)
	cache := make([][]bool, 0)

	for _, svc := range svcList {
		index, err := strconv.Atoi(svc.GetAnnotations()[LBIDBelongIndexKey])
		if err != nil {
			continue
		}
		lenCache := len(cache)
		for i := lenCache; i <= index; i++ {
			cacheLevel := make([]bool, int(maxPort-minPort)+1)
			for _, p := range blockPorts {
				cacheLevel[int(p-minPort)] = true
			}
			cache = append(cache, cacheLevel)
		}

		ports := make([]int32, 0)
		protocols := make([]corev1.Protocol, 0)
		targetPorts := make([]int, 0)
		for _, port := range svc.Spec.Ports {
			cache[index][(port.Port - minPort)] = true
			ports = append(ports, port.Port)
			protocols = append(protocols, port.Protocol)
			targetPorts = append(targetPorts, port.TargetPort.IntValue())
		}

		nsName := svc.GetNamespace() + "/" + svc.Spec.Selector[SvcSelectorKey]
		if podAllocate[nsName] == nil {
			podAllocate[nsName] = &lbsPorts{
				index:      index,
				lbIds:      []string{svc.Labels[SlbIdLabelKey]},
				ports:      ports,
				protocols:  protocols,
				targetPort: targetPorts,
			}
		} else {
			podAllocate[nsName].lbIds = append(podAllocate[nsName].lbIds, svc.Labels[SlbIdLabelKey])
		}
	}
	return podAllocate, cache
}

func (m *MultiNlbsPlugin) OnPodAdded(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	networkManager := utils.NewNetworkManager(pod, c)
	networkConfig := networkManager.GetNetworkConfig()
	conf, err := parseMultiNLBsConfig(networkConfig)
	if err != nil {
		return pod, cperrors.NewPluginError(cperrors.ParameterError, err.Error())
	}
	var lbNames []string
	for _, lbName := range conf.lbNames {
		if !util.IsStringInList(lbName, lbNames) {
			lbNames = append(lbNames, lbName)
		}
	}
	for _, lbName := range lbNames {
		pod.Spec.ReadinessGates = append(pod.Spec.ReadinessGates, corev1.PodReadinessGate{
			ConditionType: corev1.PodConditionType(PrefixReadyReadinessGate + pod.GetName() + "-" + strings.ToLower(lbName)),
		})
	}

	return pod, nil
}

func (m *MultiNlbsPlugin) OnPodUpdated(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	networkManager := utils.NewNetworkManager(pod, c)

	networkStatus, _ := networkManager.GetNetworkStatus()
	networkConfig := networkManager.GetNetworkConfig()
	conf, err := parseMultiNLBsConfig(networkConfig)
	if err != nil {
		return pod, cperrors.NewPluginError(cperrors.ParameterError, err.Error())
	}
	if networkStatus == nil {
		pod, err := networkManager.UpdateNetworkStatus(gamekruiseiov1alpha1.NetworkStatus{
			CurrentNetworkState: gamekruiseiov1alpha1.NetworkNotReady,
		}, pod)
		return pod, cperrors.ToPluginError(err, cperrors.InternalError)
	}

	podNsName := pod.GetNamespace() + "/" + pod.GetName()
	podLbsPorts, err := m.allocate(conf, podNsName)
	if err != nil {
		return pod, cperrors.ToPluginError(err, cperrors.ParameterError)
	}

	for _, lbId := range conf.idList[podLbsPorts.index] {
		// get svc
		lbName := conf.lbNames[lbId]
		svc := &corev1.Service{}
		err = c.Get(ctx, types.NamespacedName{
			Name:      pod.GetName() + "-" + strings.ToLower(lbName),
			Namespace: pod.GetNamespace(),
		}, svc)
		if err != nil {
			if errors.IsNotFound(err) {
				service, err := m.consSvc(podLbsPorts, conf, pod, lbName, c, ctx)
				if err != nil {
					return pod, cperrors.ToPluginError(err, cperrors.ParameterError)
				}
				return pod, cperrors.ToPluginError(c.Create(ctx, service), cperrors.ApiCallError)
			}
			return pod, cperrors.NewPluginError(cperrors.ApiCallError, err.Error())
		}
	}

	endPoints := ""
	for i, lbId := range conf.idList[podLbsPorts.index] {
		// get svc
		lbName := conf.lbNames[lbId]
		svc := &corev1.Service{}
		err = c.Get(ctx, types.NamespacedName{
			Name:      pod.GetName() + "-" + strings.ToLower(lbName),
			Namespace: pod.GetNamespace(),
		}, svc)
		if err != nil {
			if errors.IsNotFound(err) {
				service, err := m.consSvc(podLbsPorts, conf, pod, lbName, c, ctx)
				if err != nil {
					return pod, cperrors.ToPluginError(err, cperrors.ParameterError)
				}
				return pod, cperrors.ToPluginError(c.Create(ctx, service), cperrors.ApiCallError)
			}
			return pod, cperrors.NewPluginError(cperrors.ApiCallError, err.Error())
		}

		// old svc remain
		if svc.OwnerReferences[0].Kind == "Pod" && svc.OwnerReferences[0].UID != pod.UID {
			log.Infof("[%s] waitting old svc %s/%s deleted. old owner pod uid is %s, but now is %s", NlbNetwork, svc.Namespace, svc.Name, svc.OwnerReferences[0].UID, pod.UID)
			return pod, nil
		}

		// update svc
		if util.GetHash(conf) != svc.GetAnnotations()[SlbConfigHashKey] {
			networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkNotReady
			pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
			if err != nil {
				return pod, cperrors.NewPluginError(cperrors.InternalError, err.Error())
			}
			service, err := m.consSvc(podLbsPorts, conf, pod, lbName, c, ctx)
			if err != nil {
				return pod, cperrors.ToPluginError(err, cperrors.ParameterError)
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
		_, readyCondition := util.GetPodConditionFromList(pod.Status.Conditions, corev1.PodReady)
		if readyCondition == nil || readyCondition.Status == corev1.ConditionFalse {
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

		endPoints = endPoints + svc.Status.LoadBalancer.Ingress[0].Hostname + "/" + lbName
		if i != len(conf.idList[0])-1 {
			endPoints = endPoints + ","
		}
		for _, port := range svc.Spec.Ports {
			instrIPort := port.TargetPort
			instrEPort := intstr.FromInt(int(port.Port))
			internalAddress := gamekruiseiov1alpha1.NetworkAddress{
				IP: pod.Status.PodIP,
				Ports: []gamekruiseiov1alpha1.NetworkPort{
					{
						Name:     port.Name,
						Port:     &instrIPort,
						Protocol: port.Protocol,
					},
				},
			}
			externalAddress := gamekruiseiov1alpha1.NetworkAddress{
				EndPoint: endPoints,
				IP:       svc.Status.LoadBalancer.Ingress[0].IP,
				Ports: []gamekruiseiov1alpha1.NetworkPort{
					{
						Name:     port.Name,
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
	}

	networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkReady
	pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
	return pod, cperrors.ToPluginError(err, cperrors.InternalError)
}

func (m *MultiNlbsPlugin) OnPodDeleted(c client.Client, pod *corev1.Pod, ctx context.Context) cperrors.PluginError {
	networkManager := utils.NewNetworkManager(pod, c)
	networkConfig := networkManager.GetNetworkConfig()
	sc, err := parseMultiNLBsConfig(networkConfig)
	if err != nil {
		return cperrors.NewPluginError(cperrors.ApiCallError, err.Error())
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
		for key := range m.podAllocate {
			gssName := pod.GetLabels()[gamekruiseiov1alpha1.GameServerOwnerGssKey]
			if strings.Contains(key, pod.GetNamespace()+"/"+gssName) {
				podKeys = append(podKeys, key)
			}
		}
	} else {
		podKeys = append(podKeys, pod.GetNamespace()+"/"+pod.GetName())
	}

	for _, podKey := range podKeys {
		m.deAllocate(podKey)
	}

	return nil
}

func init() {
	multiNlbsPlugin := MultiNlbsPlugin{
		mutex: sync.RWMutex{},
	}
	alibabaCloudProvider.registerPlugin(&multiNlbsPlugin)
}

type multiNLBsConfig struct {
	lbNames               map[string]string
	idList                [][]string
	targetPorts           []int
	protocols             []corev1.Protocol
	isFixed               bool
	externalTrafficPolicy corev1.ServiceExternalTrafficPolicyType
	*nlbHealthConfig
}

func (m *MultiNlbsPlugin) consSvc(podLbsPorts *lbsPorts, conf *multiNLBsConfig, pod *corev1.Pod, lbName string, c client.Client, ctx context.Context) (*corev1.Service, error) {
	var selectId string
	for _, lbId := range podLbsPorts.lbIds {
		if conf.lbNames[lbId] == lbName {
			selectId = lbId
			break
		}
	}

	svcPorts := make([]corev1.ServicePort, 0)
	for i := 0; i < len(podLbsPorts.ports); i++ {
		if podLbsPorts.protocols[i] == ProtocolTCPUDP {
			svcPorts = append(svcPorts, corev1.ServicePort{
				Name:       strconv.Itoa(podLbsPorts.targetPort[i]) + "-" + strings.ToLower(string(corev1.ProtocolTCP)),
				Port:       podLbsPorts.ports[i],
				TargetPort: intstr.FromInt(podLbsPorts.targetPort[i]),
				Protocol:   corev1.ProtocolTCP,
			})
			svcPorts = append(svcPorts, corev1.ServicePort{
				Name:       strconv.Itoa(podLbsPorts.targetPort[i]) + "-" + strings.ToLower(string(corev1.ProtocolUDP)),
				Port:       podLbsPorts.ports[i],
				TargetPort: intstr.FromInt(podLbsPorts.targetPort[i]),
				Protocol:   corev1.ProtocolUDP,
			})
		} else {
			svcPorts = append(svcPorts, corev1.ServicePort{
				Name:       strconv.Itoa(podLbsPorts.targetPort[i]) + "-" + strings.ToLower(string(podLbsPorts.protocols[i])),
				Port:       podLbsPorts.ports[i],
				TargetPort: intstr.FromInt(podLbsPorts.targetPort[i]),
				Protocol:   podLbsPorts.protocols[i],
			})
		}
	}

	loadBalancerClass := "alibabacloud.com/nlb"

	svcAnnotations := map[string]string{
		SlbListenerOverrideKey:         "true",
		SlbIdAnnotationKey:             selectId,
		SlbConfigHashKey:               util.GetHash(conf),
		LBHealthCheckFlagAnnotationKey: conf.lBHealthCheckFlag,
	}
	if conf.lBHealthCheckFlag == "on" {
		svcAnnotations[LBHealthCheckTypeAnnotationKey] = conf.lBHealthCheckType
		svcAnnotations[LBHealthCheckConnectPortAnnotationKey] = conf.lBHealthCheckConnectPort
		svcAnnotations[LBHealthCheckConnectTimeoutAnnotationKey] = conf.lBHealthCheckConnectTimeout
		svcAnnotations[LBHealthCheckIntervalAnnotationKey] = conf.lBHealthCheckInterval
		svcAnnotations[LBHealthyThresholdAnnotationKey] = conf.lBHealthyThreshold
		svcAnnotations[LBUnhealthyThresholdAnnotationKey] = conf.lBUnhealthyThreshold
		if conf.lBHealthCheckType == "http" {
			svcAnnotations[LBHealthCheckDomainAnnotationKey] = conf.lBHealthCheckDomain
			svcAnnotations[LBHealthCheckUriAnnotationKey] = conf.lBHealthCheckUri
			svcAnnotations[LBHealthCheckMethodAnnotationKey] = conf.lBHealthCheckMethod
		}
	}
	svcAnnotations[LBIDBelongIndexKey] = strconv.Itoa(podLbsPorts.index)

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        pod.GetName() + "-" + strings.ToLower(lbName),
			Namespace:   pod.GetNamespace(),
			Annotations: svcAnnotations,
			Labels: map[string]string{
				ServiceBelongNetworkTypeKey: MultiNlbsNetwork,
			},
			OwnerReferences: getSvcOwnerReference(c, ctx, pod, conf.isFixed),
		},
		Spec: corev1.ServiceSpec{
			AllocateLoadBalancerNodePorts: ptr.To[bool](false),
			ExternalTrafficPolicy:         conf.externalTrafficPolicy,
			Type:                          corev1.ServiceTypeLoadBalancer,
			Selector: map[string]string{
				SvcSelectorKey: pod.GetName(),
			},
			Ports:             svcPorts,
			LoadBalancerClass: &loadBalancerClass,
		},
	}, nil
}

func (m *MultiNlbsPlugin) allocate(conf *multiNLBsConfig, nsName string) (*lbsPorts, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// check if pod is already allocated
	if m.podAllocate[nsName] != nil {
		return m.podAllocate[nsName], nil
	}

	// if the pod has not been allocated, allocate new ports to it
	var ports []int32
	needNum := len(conf.targetPorts)
	index := -1

	// init cache according to conf.idList
	lenCache := len(m.cache)
	for i := lenCache; i < len(conf.idList); i++ {
		cacheLevel := make([]bool, int(m.maxPort-m.minPort)+1)
		for _, p := range m.blockPorts {
			cacheLevel[int(p-m.minPort)] = true
		}
		m.cache = append(m.cache, cacheLevel)
	}

	// find allocated ports
	for i := 0; i < len(m.cache); i++ {
		sum := 0
		ports = make([]int32, 0)
		for j := 0; j < len(m.cache[i]); j++ {
			if !m.cache[i][j] {
				ports = append(ports, int32(j)+m.minPort)
				sum++
				if sum == needNum {
					index = i
					break
				}
			}
		}
		if index != -1 {
			break
		}
	}

	if index == -1 {
		return nil, fmt.Errorf("no available ports found")
	}
	if index >= len(conf.idList) {
		return nil, fmt.Errorf("NlbIdNames configuration have not synced")
	}
	for _, port := range ports {
		m.cache[index][port-m.minPort] = true
	}
	m.podAllocate[nsName] = &lbsPorts{
		index:      index,
		lbIds:      conf.idList[index],
		ports:      ports,
		protocols:  conf.protocols,
		targetPort: conf.targetPorts,
	}
	log.Infof("[%s] pod %s allocated: lbIds %v; ports %v", MultiNlbsNetwork, nsName, conf.idList[index], ports)
	return m.podAllocate[nsName], nil
}

func (m *MultiNlbsPlugin) deAllocate(nsName string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	podLbsPorts := m.podAllocate[nsName]
	if podLbsPorts == nil {
		return
	}
	for _, port := range podLbsPorts.ports {
		m.cache[podLbsPorts.index][port-m.minPort] = false
	}
	delete(m.podAllocate, nsName)

	log.Infof("[%s] pod %s deallocate: lbIds %s ports %v", MultiNlbsNetwork, nsName, podLbsPorts.lbIds, podLbsPorts.ports)
}

func parseMultiNLBsConfig(conf []gamekruiseiov1alpha1.NetworkConfParams) (*multiNLBsConfig, error) {
	// lbNames format {id}: {name}
	lbNames := make(map[string]string)
	idList := make([][]string, 0)
	nameNums := make(map[string]int)
	ports := make([]int, 0)
	protocols := make([]corev1.Protocol, 0)
	isFixed := false
	externalTrafficPolicy := corev1.ServiceExternalTrafficPolicyTypeLocal

	for _, c := range conf {
		switch c.Name {
		case NlbIdNamesConfigName:
			for _, nlbIdNamesConfig := range strings.Split(c.Value, ",") {
				if nlbIdNamesConfig != "" {
					idName := strings.Split(nlbIdNamesConfig, "/")
					if len(idName) != 2 {
						return nil, fmt.Errorf("invalid NlbIdNames %s. You should input as the format {nlb-id-0}/{name-0}", c.Value)
					}

					id := idName[0]
					name := idName[1]

					nameNum := nameNums[name]
					if nameNum >= len(idList) {
						idList = append(idList, []string{id})
					} else {
						idList[nameNum] = append(idList[nameNum], id)
					}
					nameNums[name]++
					lbNames[id] = name
				}
			}
		case PortProtocolsConfigName:
			for _, pp := range strings.Split(c.Value, ",") {
				ppSlice := strings.Split(pp, "/")
				port, err := strconv.Atoi(ppSlice[0])
				if err != nil {
					return nil, fmt.Errorf("invalid PortProtocols %s", c.Value)
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
				return nil, fmt.Errorf("invalid Fixed %s", c.Value)
			}
			isFixed = v
		case ExternalTrafficPolicyTypeConfigName:
			if strings.EqualFold(c.Value, string(corev1.ServiceExternalTrafficPolicyTypeCluster)) {
				externalTrafficPolicy = corev1.ServiceExternalTrafficPolicyTypeCluster
			}
		}
	}

	// check idList
	if len(idList) == 0 {
		return nil, fmt.Errorf("invalid NlbIdNames. You should input as the format {nlb-id-0}/{name-0}")
	}
	num := len(idList[0])
	for i := 1; i < len(idList); i++ {
		if num != len(idList[i]) {
			return nil, fmt.Errorf("invalid NlbIdNames. The number of names should be same")
		}
		num = len(idList[i])
	}

	// check ports & protocols
	if len(ports) == 0 || len(protocols) == 0 {
		return nil, fmt.Errorf("invalid PortProtocols, which can not be empty")
	}

	nlbHealthConfig, err := parseNlbHealthConfig(conf)
	if err != nil {
		return nil, err
	}

	return &multiNLBsConfig{
		lbNames:               lbNames,
		idList:                idList,
		targetPorts:           ports,
		protocols:             protocols,
		isFixed:               isFixed,
		externalTrafficPolicy: externalTrafficPolicy,
		nlbHealthConfig:       nlbHealthConfig,
	}, nil
}
