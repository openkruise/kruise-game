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

package hwcloud

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"

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
)

const (
	MultiElbsNetwork = "HwCloud-Multi-ELBs"
	AliasMultiElbs   = "Multi-ELBs-Network"

	// ConfigNames defined by OKG
	ElbIdNamesConfigName     = "ElbIdNames"
	AllocatePolicyConfigName = "AllocatePolicy"

	// service annotation defined by OKG
	LBIDBelongIndexKey = "game.kruise.io/lb-belong-index"

	// service label defined by OKG
	ServiceBelongNetworkTypeKey = "game.kruise.io/network-type"

	PrefixReadyReadinessGate = "target-health.elb.k8s.cce/"

	// ElbClassPerformance is the dedicated load balancer class.
	ElbClassPerformance = "performance"

	ElbMappingPoolAnnotationKey = "cce.io/game.kruise.isp-name"

	ElbHealthCheckFlagAnnotationKey = "kubernetes.io/elb.health-check-flag"
	ElbHealthCheckFlagConfigName    = "LBHealthCheckFlag"

	ElbHealthCheckOptionsAnnotationKey      = "kubernetes.io/elb.health-check-options"
	ElbHealthCheckOptionsConfigName         = "LBHealthCheckConfig"
	ElbUserDefineConfigName                 = "UserDefine"
	AllocateLoadBalancerNodePortsConfigName = "AllocateLoadBalancerNodePorts"

	ElbPortMappingResultCount = "cce.io/game.kruise.mapping-result-count"

	// ReadinessGateConfigName enables target-health readiness gates.
	ReadinessGateConfigName = "ReadinessGate"

	ServiceProxyName = "service.kubernetes.io/service-proxy-name"
)

var (
	notAllowedAnnotationKeyMap = map[string]struct{}{
		ElbAutocreateAnnotationKey:         {},
		ElbMappingPoolAnnotationKey:        {},
		ElbClassAnnotationKey:              {},
		ElbIdAnnotationKey:                 {},
		ElbHealthCheckOptionsAnnotationKey: {},
	}
)

type MultiElbsPlugin struct {
	maxPort    int32
	minPort    int32
	blockPorts []int32
	cache      [][]bool
	// podAllocate records the allocated LB group and service ports for each pod.
	podAllocate map[string]*lbsPorts
	mutex       sync.RWMutex
}

type lbsPorts struct {
	index      int
	lbIds      []string
	lbNames    []string
	ports      []int32
	targetPort []int
	protocols  []corev1.Protocol
}

type recoveredServicePortKey struct {
	port       int32
	targetPort int
}

type allocatedLbBinding struct {
	id   string
	name string
}

// allocatedLbBindings rebuilds allocated {id,name} pairs from the pod snapshot.
// Empty snapshot names fall back to config; unresolved bindings are skipped.
func allocatedLbBindings(podLbsPorts *lbsPorts, conf *multiELBsConfig) []allocatedLbBinding {
	bindings := make([]allocatedLbBinding, 0, len(podLbsPorts.lbIds))
	for i, lbId := range podLbsPorts.lbIds {
		lbName := ""
		if i < len(podLbsPorts.lbNames) {
			lbName = podLbsPorts.lbNames[i]
		}
		if lbName == "" {
			lbName = conf.lbNames[lbId]
		}
		if lbName == "" {
			log.Warningf("[%s] allocated lb %s has no lbName snapshot and is no longer in config; skipping", MultiElbsNetwork, lbId)
			continue
		}
		bindings = append(bindings, allocatedLbBinding{id: lbId, name: lbName})
	}
	return bindings
}

func (m *MultiElbsPlugin) Name() string {
	return MultiElbsNetwork
}

func (m *MultiElbsPlugin) Alias() string {
	return AliasMultiElbs
}

func (m *MultiElbsPlugin) Init(c client.Client, options cloudprovider.CloudProviderOptions, ctx context.Context) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	elbOptions := options.(provideroptions.HwCloudOptions).CCEELBOptions.MultiELBOptions
	m.minPort = elbOptions.MinPort
	m.maxPort = elbOptions.MaxPort
	m.blockPorts = elbOptions.BlockPorts

	svcList := &corev1.ServiceList{}
	err := c.List(ctx, svcList, client.MatchingLabels{ServiceBelongNetworkTypeKey: MultiElbsNetwork})
	if err != nil {
		return err
	}
	m.podAllocate, m.cache = initMultiLBCache(svcList.Items, m.minPort, m.maxPort, m.blockPorts)

	log.Infof("[%s] podAllocate cache complete initialization: ", MultiElbsNetwork)
	for podNsName, lps := range m.podAllocate {
		log.Infof("[%s] pod %s: %v", MultiElbsNetwork, podNsName, *lps)
	}
	return nil
}

func initMultiLBCache(svcList []corev1.Service, minPort, maxPort int32, blockPorts []int32) (map[string]*lbsPorts, [][]bool) {
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
				if p < minPort || p > maxPort {
					log.Warningf("[%s] skip out-of-range block port %d for cache [%d, %d]", MultiElbsNetwork, p, minPort, maxPort)
					continue
				}
				cacheLevel[int(p-minPort)] = true
			}
			cache = append(cache, cacheLevel)
		}

		ports := make([]int32, 0)
		protocols := make([]corev1.Protocol, 0)
		targetPorts := make([]int, 0)
		portIndexes := make(map[recoveredServicePortKey]int)
		for _, port := range svc.Spec.Ports {
			if port.Port < minPort || port.Port > maxPort {
				log.Warningf("[%s] skip out-of-range service port %d for svc %s/%s cache [%d, %d]", MultiElbsNetwork, port.Port, svc.Namespace, svc.Name, minPort, maxPort)
				continue
			}
			cache[index][(port.Port - minPort)] = true
			key := recoveredServicePortKey{
				port:       port.Port,
				targetPort: port.TargetPort.IntValue(),
			}
			if portIndex, ok := portIndexes[key]; ok {
				if isTCPUDPProtocolPair(protocols[portIndex], port.Protocol) {
					protocols[portIndex] = ProtocolTCPUDP
				}
				continue
			}
			portIndexes[key] = len(ports)
			ports = append(ports, port.Port)
			protocols = append(protocols, port.Protocol)
			targetPorts = append(targetPorts, port.TargetPort.IntValue())
		}

		nsName := svc.GetNamespace() + "/" + svc.Spec.Selector[SvcSelectorKey]
		lbName := svc.Annotations[ElbMappingPoolAnnotationKey]
		if lbName == "" {
			// Recover lbName from "<pod>-<lbName>" for older Services.
			if podName := svc.Spec.Selector[SvcSelectorKey]; podName != "" && strings.HasPrefix(svc.Name, podName+"-") {
				lbName = svc.Name[len(podName)+1:]
			}
		}
		if podAllocate[nsName] == nil {
			podAllocate[nsName] = &lbsPorts{
				index:      index,
				lbIds:      []string{svc.Annotations[ElbIdAnnotationKey]},
				lbNames:    []string{lbName},
				ports:      ports,
				protocols:  protocols,
				targetPort: targetPorts,
			}
		} else {
			podAllocate[nsName].lbIds = append(podAllocate[nsName].lbIds, svc.Annotations[ElbIdAnnotationKey])
			podAllocate[nsName].lbNames = append(podAllocate[nsName].lbNames, lbName)
		}
	}
	return podAllocate, cache
}

func isTCPUDPProtocolPair(existing, incoming corev1.Protocol) bool {
	return (existing == corev1.ProtocolTCP && incoming == corev1.ProtocolUDP) ||
		(existing == corev1.ProtocolUDP && incoming == corev1.ProtocolTCP) ||
		(existing == ProtocolTCPUDP && (incoming == corev1.ProtocolTCP || incoming == corev1.ProtocolUDP))
}

func (m *MultiElbsPlugin) OnPodAdded(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	networkManager := utils.NewNetworkManager(pod, c)
	networkConfig := networkManager.GetNetworkConfig()
	conf, err := parseMultiELBsConfig(networkConfig)
	if err != nil {
		return pod, cperrors.NewPluginError(cperrors.ParameterError, err.Error())
	}
	var lbNames []string
	for _, lbName := range conf.lbNames {
		if !util.IsStringInList(lbName, lbNames) {
			lbNames = append(lbNames, lbName)
		}
	}
	// Readiness gates are supported only in CCE Turbo passthrough scenarios
	// with dedicated ELBs.
	if conf.readinessGate && conf.elbClass == ElbClassPerformance {
		for _, lbName := range lbNames {
			pod.Spec.ReadinessGates = append(pod.Spec.ReadinessGates, corev1.PodReadinessGate{
				ConditionType: corev1.PodConditionType(PrefixReadyReadinessGate + pod.GetName() + "-" + strings.ToLower(lbName)),
			})
		}
	}

	return pod, nil
}

func (m *MultiElbsPlugin) OnPodUpdated(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	networkManager := utils.NewNetworkManager(pod, c)

	networkStatus, _ := networkManager.GetNetworkStatus()
	networkConfig := networkManager.GetNetworkConfig()
	if networkStatus == nil {
		pod, err := networkManager.UpdateNetworkStatus(gamekruiseiov1alpha1.NetworkStatus{
			CurrentNetworkState: gamekruiseiov1alpha1.NetworkNotReady,
		}, pod)
		return pod, cperrors.ToPluginError(err, cperrors.InternalError)
	}

	conf, err := parseMultiELBsConfig(networkConfig)
	if err != nil {
		return pod, cperrors.NewPluginError(cperrors.ParameterError, err.Error())
	}

	podNsName := pod.GetNamespace() + "/" + pod.GetName()
	podLbsPorts, err := m.allocate(conf, podNsName)
	if err != nil {
		return pod, cperrors.ToPluginError(err, cperrors.ParameterError)
	}
	lbBindings := allocatedLbBindings(podLbsPorts, conf)
	if len(lbBindings) == 0 {
		// Avoid publishing Ready when every allocated LB binding is unresolved.
		networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkNotReady
		pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
		return pod, cperrors.ToPluginError(err, cperrors.InternalError)
	}

	var servicesToUpdate []*corev1.Service
	var servicesToCreate []*corev1.Service
	var needNetworkNotReady bool

	for _, lbBinding := range lbBindings {
		// get svc
		svc := &corev1.Service{}
		err = c.Get(ctx, types.NamespacedName{
			Name:      pod.GetName() + "-" + strings.ToLower(lbBinding.name),
			Namespace: pod.GetNamespace(),
		}, svc)
		if err != nil {
			if errors.IsNotFound(err) {
				service, err := m.consSvc(podLbsPorts, conf, pod, lbBinding.id, lbBinding.name, len(lbBindings), c, ctx)
				if err != nil {
					return pod, cperrors.ToPluginError(err, cperrors.ParameterError)
				}
				servicesToCreate = append(servicesToCreate, service)
			} else {
				return pod, cperrors.NewPluginError(cperrors.ApiCallError, err.Error())
			}
		} else {
			// old svc remain
			if svc.OwnerReferences[0].Kind == "Pod" && svc.OwnerReferences[0].UID != pod.UID {
				log.Infof("[%s] waiting old svc %s/%s deleted. old owner pod uid is %s, but now is %s", "HwCloud-ELB", svc.Namespace, svc.Name, svc.OwnerReferences[0].UID, pod.UID)
				return pod, nil
			}

			// update svc
			if util.GetHash(conf) != svc.GetAnnotations()[ElbConfigHashKey] {
				needNetworkNotReady = true
				service, err := m.consSvc(podLbsPorts, conf, pod, lbBinding.id, lbBinding.name, len(lbBindings), c, ctx)
				if err != nil {
					return pod, cperrors.ToPluginError(err, cperrors.ParameterError)
				}
				// Preserve health-check options for this pod's frozen target ports.
				preserveHealthCheckAnnotation(service, svc.GetAnnotations(), conf)
				servicesToUpdate = append(servicesToUpdate, service)
			}
		}
	}

	for _, service := range servicesToCreate {
		err = c.Create(ctx, service)
		if err != nil {
			if errors.IsAlreadyExists(err) {
				log.Infof("[%s] service %s/%s already exists, skip creation", MultiElbsNetwork, service.Namespace, service.Name)
				continue
			}
			return pod, cperrors.NewPluginError(cperrors.ApiCallError, err.Error())
		}
	}

	for _, service := range servicesToUpdate {
		err = c.Update(ctx, service)
		if err != nil {
			return pod, cperrors.NewPluginError(cperrors.ApiCallError, err.Error())
		}
	}

	if len(servicesToUpdate) > 0 || len(servicesToCreate) > 0 {
		if needNetworkNotReady && networkStatus != nil {
			networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkNotReady
			pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
			if err != nil {
				return pod, cperrors.NewPluginError(cperrors.InternalError, err.Error())
			}
		}
		// Let the next reconcile observe Service status.
		return pod, nil
	}

	// Build status from all allocated LB Services.
	endPoints := ""
	internalAddresses := make([]gamekruiseiov1alpha1.NetworkAddress, 0)
	externalAddresses := make([]gamekruiseiov1alpha1.NetworkAddress, 0)
	for i, lbBinding := range lbBindings {
		svc := &corev1.Service{}
		err = c.Get(ctx, types.NamespacedName{
			Name:      pod.GetName() + "-" + strings.ToLower(lbBinding.name),
			Namespace: pod.GetNamespace(),
		}, svc)
		if err != nil {
			if !errors.IsNotFound(err) {
				return pod, cperrors.NewPluginError(cperrors.ApiCallError, err.Error())
			}
			continue
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
		if len(svc.Status.LoadBalancer.Ingress) == 0 {
			networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkNotReady
			pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
			return pod, cperrors.ToPluginError(err, cperrors.InternalError)
		}

		ingressIP := svc.Status.LoadBalancer.Ingress[0].IP
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
					return pod, cperrors.ToPluginError(err, cperrors.InternalError)
				}
			}
		}

		// network ready
		host := svc.Status.LoadBalancer.Ingress[0].Hostname
		if host == "" {
			host = ingressIP
		}
		endPoints = endPoints + host + "/" + lbBinding.name
		if i != len(lbBindings)-1 {
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
				IP: "",
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
	}

	// Every external address carries the full endpoint list.
	for i := range externalAddresses {
		externalAddresses[i].EndPoint = endPoints
	}
	networkStatus.InternalAddresses = internalAddresses
	networkStatus.ExternalAddresses = externalAddresses

	networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkReady
	pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
	return pod, cperrors.ToPluginError(err, cperrors.InternalError)
}

func (m *MultiElbsPlugin) OnPodDeleted(c client.Client, pod *corev1.Pod, ctx context.Context) cperrors.PluginError {
	log.Infof("执行OnPodDeleted：%s/%s", pod.GetNamespace(), pod.GetName())
	networkManager := utils.NewNetworkManager(pod, c)
	networkConfig := networkManager.GetNetworkConfig()
	sc, err := parseMultiELBsConfig(networkConfig)
	if err != nil {
		return cperrors.NewPluginError(cperrors.ApiCallError, err.Error())
	}

	var podKeys []string
	if sc.isFixed {
		gss, err := util.GetGameServerSetOfPod(pod, c, ctx)
		if err != nil && !errors.IsNotFound(err) {
			return cperrors.ToPluginError(err, cperrors.ApiCallError)
		}
		// Keep fixed allocations while the owning GameServerSet still exists.
		if err == nil && gss.GetDeletionTimestamp() == nil {
			return nil
		}
		// Release allocations for pods owned by the deleted GameServerSet.
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
	log.Infof("完成OnPodDeleted：%s/%s", pod.GetNamespace(), pod.GetName())
	return nil
}

func init() {
	MultiElbsPlugin := MultiElbsPlugin{
		mutex: sync.RWMutex{},
	}
	hwCloudProvider.registerPlugin(&MultiElbsPlugin)
}

type multiELBsConfig struct {
	lbNames                       map[string]string
	idList                        [][]string
	targetPorts                   []int
	protocols                     []corev1.Protocol
	isFixed                       bool
	externalTrafficPolicy         corev1.ServiceExternalTrafficPolicyType
	allocatePolicy                string
	elbClass                      string
	lbHealthCheckFlag             string
	lbHealthCheckConfig           string
	userDefine                    string
	allocateLoadBalancerNodePorts bool
	readinessGate                 bool
}

func (m *MultiElbsPlugin) consSvc(podLbsPorts *lbsPorts, conf *multiELBsConfig, pod *corev1.Pod, lbId, lbName string, lbCount int, c client.Client, ctx context.Context) (*corev1.Service, error) {
	portProtocolNum := 0
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
			portProtocolNum += 2
		} else {
			svcPorts = append(svcPorts, corev1.ServicePort{
				Name:       strconv.Itoa(podLbsPorts.targetPort[i]) + "-" + strings.ToLower(string(podLbsPorts.protocols[i])),
				Port:       podLbsPorts.ports[i],
				TargetPort: intstr.FromInt(podLbsPorts.targetPort[i]),
				Protocol:   podLbsPorts.protocols[i],
			})
			portProtocolNum += 1
		}
	}

	svcAnnotations := map[string]string{
		ElbIdAnnotationKey:              lbId,
		ElbConfigHashKey:                util.GetHash(conf),
		ElbHealthCheckFlagAnnotationKey: conf.lbHealthCheckFlag,
	}
	if conf.userDefine != "" {
		hwOptions := make(map[string]string)
		err := json.Unmarshal([]byte(conf.userDefine), &hwOptions)
		if err != nil {
			log.Warningf("[%s] failed to unmarshal userDefine config: %s, err: %v", MultiElbsNetwork, conf.userDefine, err)
		} else {
			log.Infof("[%s] successfully unmarshaled userDefine config: %v", MultiElbsNetwork, hwOptions)
		}
		for k, v := range hwOptions {
			if _, exists := notAllowedAnnotationKeyMap[k]; !exists {
				svcAnnotations[k] = v
			} else {
				log.Warningf("[%s] not allowed annotation key %s in UserDefine", MultiElbsNetwork, k)
			}
		}
	}

	if conf.lbHealthCheckFlag == "on" && conf.lbHealthCheckConfig != "" {
		processedHealthCheckConfig, err := processHealthCheckOptions(conf.lbHealthCheckConfig, podLbsPorts)
		if err != nil {
			log.Warningf("[%s] failed to process health check options: %v", MultiElbsNetwork, err)
		} else if processedHealthCheckConfig != "" {
			svcAnnotations[ElbHealthCheckOptionsAnnotationKey] = processedHealthCheckConfig
		} else {
			log.Warningf("[%s] Health check options processing returned empty result, skipping health check annotation", MultiElbsNetwork)
		}
	}

	svcAnnotations[LBIDBelongIndexKey] = strconv.Itoa(podLbsPorts.index)
	svcAnnotations[ElbMappingPoolAnnotationKey] = lbName
	svcAnnotations[ElbClassAnnotationKey] = conf.elbClass
	svcAnnotations[ElbPortMappingResultCount] = strconv.Itoa(lbCount * portProtocolNum)

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        pod.GetName() + "-" + strings.ToLower(lbName),
			Namespace:   pod.GetNamespace(),
			Annotations: svcAnnotations,
			Labels: map[string]string{
				ServiceBelongNetworkTypeKey: MultiElbsNetwork,
				ServiceProxyName:            "dummy",
			},
			OwnerReferences: getSvcOwnerReference(c, ctx, pod, conf.isFixed),
		},
		Spec: corev1.ServiceSpec{
			AllocateLoadBalancerNodePorts: ptr.To[bool](conf.allocateLoadBalancerNodePorts),
			ExternalTrafficPolicy:         conf.externalTrafficPolicy,
			Type:                          corev1.ServiceTypeLoadBalancer,
			Selector: map[string]string{
				SvcSelectorKey: pod.GetName(),
			},
			Ports: svcPorts,
		},
	}, nil
}

func (m *MultiElbsPlugin) allocate(conf *multiELBsConfig, nsName string) (*lbsPorts, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if m.podAllocate == nil {
		return nil, cperrors.NewPluginError(cperrors.ApiCallError, "podAllocate is nil")
	}

	// check if pod is already allocated
	if m.podAllocate[nsName] != nil {
		return m.podAllocate[nsName], nil
	}

	// if the pod has not been allocated, allocate new ports to it
	var ports []int32
	needNum := len(conf.targetPorts)
	index := -1

	// init cache according to conf.idList.
	// Keep stale cache levels so existing pod indexes survive ElbIdNames shrink.
	lenCache := len(m.cache)
	for i := lenCache; i < len(conf.idList); i++ {
		cacheLevel := make([]bool, int(m.maxPort-m.minPort)+1)
		for _, p := range m.blockPorts {
			if p < m.minPort || p > m.maxPort {
				log.Warningf("[%s] skip out-of-range block port %d for cache [%d, %d]", MultiElbsNetwork, p, m.minPort, m.maxPort)
				continue
			}
			cacheLevel[int(p-m.minPort)] = true
		}
		m.cache = append(m.cache, cacheLevel)
	}

	// find allocated ports
	switch conf.allocatePolicy {
	case "default":
		for i := 0; i < len(conf.idList); i++ {
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
	case "balanced":
		maxAvailable := 0
		for i := 0; i < len(conf.idList); i++ {
			sum := 0
			for j := 0; j < len(m.cache[i]); j++ {
				if !m.cache[i][j] {
					sum++
				}
			}
			if sum > maxAvailable {
				maxAvailable = sum
				index = i
			}
		}
		if maxAvailable < needNum {
			return nil, fmt.Errorf("no available ports found")
		}
		ports = make([]int32, 0)
		for j := 0; j < len(m.cache[index]); j++ {
			if !m.cache[index][j] {
				ports = append(ports, int32(j)+m.minPort)
				if len(ports) == needNum {
					break
				}
			}
		}
	}

	if index == -1 {
		return nil, fmt.Errorf("no available ports found")
	}
	for _, port := range ports {
		m.cache[index][port-m.minPort] = true
	}
	lbIds := append([]string(nil), conf.idList[index]...)
	lbNames := make([]string, 0, len(lbIds))
	for _, lbId := range lbIds {
		lbNames = append(lbNames, conf.lbNames[lbId])
	}
	m.podAllocate[nsName] = &lbsPorts{
		index:      index,
		lbIds:      lbIds,
		lbNames:    lbNames,
		ports:      ports,
		protocols:  append([]corev1.Protocol(nil), conf.protocols...),
		targetPort: append([]int(nil), conf.targetPorts...),
	}
	log.Infof("[%s] pod %s allocated: lbIds %v; ports %v", MultiElbsNetwork, nsName, conf.idList[index], ports)
	return m.podAllocate[nsName], nil
}

func processHealthCheckOptions(healthCheckConfig string, podLbsPorts *lbsPorts) (string, error) {
	var healthCheckOptions []HealthCheckOption
	err := json.Unmarshal([]byte(healthCheckConfig), &healthCheckOptions)
	if err != nil {
		return "", fmt.Errorf("failed to unmarshal health check options: %v", err)
	}

	var processedOptions []HealthCheckOption
	for _, option := range healthCheckOptions {
		if option.PodTargetPort != "" {
			// Convert pod_target_port, for example "TCP:80", to the allocated Service port.
			parts := strings.Split(option.PodTargetPort, ":")
			if len(parts) != 2 {
				log.Warningf("Invalid pod_target_port format: %s, skipping this option", option.PodTargetPort)
				continue
			}
			protocol := parts[0]
			originalPortStr := parts[1]

			podPort, err := strconv.Atoi(originalPortStr)
			if err != nil {
				log.Warningf("Invalid port number in pod_target_port: %s, skipping this option", originalPortStr)
				continue
			}

			found := false

			for j, targetPodPort := range podLbsPorts.targetPort {
				if targetPodPort == podPort && j < len(podLbsPorts.protocols) {
					serviceProtocol := strings.ToUpper(string(podLbsPorts.protocols[j]))

					if serviceProtocol == "TCPUDP" {
						servicePort := podLbsPorts.ports[j]
						newOption := option
						newOption.TargetServicePort = fmt.Sprintf("%s:%d", protocol, servicePort)
						newOption.PodTargetPort = ""
						processedOptions = append(processedOptions, newOption)
						found = true
						break
					} else if serviceProtocol == protocol {
						servicePort := podLbsPorts.ports[j]
						newOption := option
						newOption.TargetServicePort = fmt.Sprintf("%s:%d", protocol, servicePort)
						newOption.PodTargetPort = ""
						processedOptions = append(processedOptions, newOption)
						found = true
						break
					}
				}
			}

			if !found {
				log.Warningf("pod_target_port %s does not match any port in GSS PortProtocols, health check will be skipped", option.PodTargetPort)
			}

		} else {
			log.Warningf("[%s] Found health check option without pod_target_port field, this health check will be ignored: %+v", MultiElbsNetwork, option)
		}
	}

	if len(processedOptions) == 0 {
		log.Warningf("[%s] No valid health check options with pod_target_port found after processing, health check configuration will not be applied", MultiElbsNetwork)
		return "", nil
	}

	updatedConfig, err := json.Marshal(processedOptions)
	if err != nil {
		return "", fmt.Errorf("failed to marshal updated health check options: %v", err)
	}

	return string(updatedConfig), nil
}

// preserveHealthCheckAnnotation keeps old health-check options when a config
// update no longer matches this pod's frozen target ports.
func preserveHealthCheckAnnotation(newSvc *corev1.Service, oldAnnotations map[string]string, conf *multiELBsConfig) {
	if conf.lbHealthCheckFlag != "on" || conf.lbHealthCheckConfig == "" {
		return
	}
	if _, ok := newSvc.Annotations[ElbHealthCheckOptionsAnnotationKey]; ok {
		return
	}
	if prev, ok := oldAnnotations[ElbHealthCheckOptionsAnnotationKey]; ok && prev != "" {
		if newSvc.Annotations == nil {
			newSvc.Annotations = map[string]string{}
		}
		newSvc.Annotations[ElbHealthCheckOptionsAnnotationKey] = prev
	}
}

func (m *MultiElbsPlugin) deAllocate(nsName string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	podLbsPorts := m.podAllocate[nsName]
	if podLbsPorts == nil {
		return
	}
	// Skip cache cleanup if the stored index is above the current cache size.
	if podLbsPorts.index < len(m.cache) {
		for _, port := range podLbsPorts.ports {
			m.cache[podLbsPorts.index][port-m.minPort] = false
		}
	}
	delete(m.podAllocate, nsName)

	log.Infof("[%s] pod %s deallocate: lbIds %s ports %v", MultiElbsNetwork, nsName, podLbsPorts.lbIds, podLbsPorts.ports)
}

// HealthCheckOption represents a single health check configuration
type HealthCheckOption struct {
	Protocol          string `json:"protocol,omitempty"`
	Delay             string `json:"delay,omitempty"`
	Timeout           string `json:"timeout,omitempty"`
	MaxRetries        string `json:"max_retries,omitempty"`
	PodTargetPort     string `json:"pod_target_port,omitempty"` // From GSS config
	TargetServicePort string `json:"target_service_port"`       // Service annotation field
	MonitorPort       string `json:"monitor_port,omitempty"`
	Path              string `json:"path,omitempty"`
	ExpectedCodes     string `json:"expected_codes,omitempty"`
}

func parseMultiELBsConfig(conf []gamekruiseiov1alpha1.NetworkConfParams) (*multiELBsConfig, error) {
	// lbNames format {id}: {name}
	var elbHealthCheckConfig, userDefine string
	lbNames := make(map[string]string)
	idList := make([][]string, 0)
	nameNums := make(map[string]int)
	ports := make([]int, 0)
	protocols := make([]corev1.Protocol, 0)
	isFixed := false
	externalTrafficPolicy := corev1.ServiceExternalTrafficPolicyTypeCluster
	allocatePolicy := "default"
	elbClass := ElbClassPerformance
	elbHealthCheckFlag := "on"
	allocateLoadBalancerNodePorts := false
	readinessGate := false

	for _, c := range conf {
		switch c.Name {
		case ElbIdNamesConfigName:
			for _, ElbIdNamesConfig := range strings.Split(c.Value, ",") {
				if ElbIdNamesConfig != "" {
					// Parse format: {elb-id-0}/{name-0}
					parts := strings.Split(ElbIdNamesConfig, "/")
					if len(parts) != 2 {
						return nil, fmt.Errorf("invalid ElbIdNames %s. You should input as the format {elb-id-0}/{name-0}", c.Value)
					}

					id := parts[0]
					name := parts[1]

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
			if strings.EqualFold(c.Value, string(corev1.ServiceExternalTrafficPolicyTypeLocal)) {
				externalTrafficPolicy = corev1.ServiceExternalTrafficPolicyTypeLocal
			}
		case AllocateLoadBalancerNodePortsConfigName:
			v, err := strconv.ParseBool(c.Value)
			if err != nil {
				return nil, fmt.Errorf("invalid AllocateLoadBalancerNodePorts %s", c.Value)
			}
			allocateLoadBalancerNodePorts = v
		case AllocatePolicyConfigName:
			allocatePolicy = c.Value
			if allocatePolicy != "default" && allocatePolicy != "balanced" {
				return nil, fmt.Errorf("invalid AllocatePolicy %s", allocatePolicy)
			}
		case ElbClassConfigName:
			elbClass = c.Value
		case ReadinessGateConfigName:
			v, err := strconv.ParseBool(c.Value)
			if err != nil {
				return nil, fmt.Errorf("invalid ReadinessGate %s", c.Value)
			}
			readinessGate = v
		case ElbHealthCheckFlagConfigName:
			elbHealthCheckFlag = c.Value
		case ElbHealthCheckOptionsConfigName:
			elbHealthCheckConfig = c.Value
		case ElbUserDefineConfigName:
			userDefine = c.Value
		default:
		}
	}

	// check idList
	if len(idList) == 0 {
		return nil, fmt.Errorf("invalid ElbIdNames. You should input as the format {elb-id-0}/{name-0}")
	}
	num := len(idList[0])
	for i := 1; i < len(idList); i++ {
		if num != len(idList[i]) {
			return nil, fmt.Errorf("invalid ElbIdNames. The number of names should be same")
		}
		num = len(idList[i])
	}

	// check ports & protocols
	if len(ports) == 0 || len(protocols) == 0 {
		return nil, fmt.Errorf("invalid PortProtocols, which can not be empty")
	}

	return &multiELBsConfig{
		lbNames:                       lbNames,
		idList:                        idList,
		targetPorts:                   ports,
		protocols:                     protocols,
		isFixed:                       isFixed,
		externalTrafficPolicy:         externalTrafficPolicy,
		allocatePolicy:                allocatePolicy,
		elbClass:                      elbClass,
		lbHealthCheckFlag:             elbHealthCheckFlag,
		lbHealthCheckConfig:           elbHealthCheckConfig,
		userDefine:                    userDefine,
		allocateLoadBalancerNodePorts: allocateLoadBalancerNodePorts,
		readinessGate:                 readinessGate,
	}, nil
}
