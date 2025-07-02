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

package volcengine

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
	ClbNetwork                    = "Volcengine-CLB"
	AliasCLB                      = "CLB-Network"
	ClbIdLabelKey                 = "service.beta.kubernetes.io/volcengine-loadbalancer-id"
	ClbIdsConfigName              = "ClbIds"
	PortProtocolsConfigName       = "PortProtocols"
	FixedConfigName               = "Fixed"
	AllocateLoadBalancerNodePorts = "AllocateLoadBalancerNodePorts"
	ClbAnnotations                = "Annotations"
	ClbConfigHashKey              = "game.kruise.io/network-config-hash"
	ClbIdAnnotationKey            = "service.beta.kubernetes.io/volcengine-loadbalancer-id"
	ClbAddressTypeKey             = "service.beta.kubernetes.io/volcengine-loadbalancer-address-type"
	ClbAddressTypePublic          = "PUBLIC"
	ClbSchedulerKey               = "service.beta.kubernetes.io/volcengine-loadbalancer-scheduler"
	ClbSchedulerWRR               = "wrr"
	SvcSelectorKey                = "statefulset.kubernetes.io/pod-name"
	EnableClbScatterConfigName    = "EnableClbScatter"
	EnableMultiIngressConfigName  = "EnableMultiIngress"
)

type portAllocated map[int32]bool

type ClbPlugin struct {
	maxPort        int32
	minPort        int32
	blockPorts     []int32
	cache          map[string]portAllocated
	podAllocate    map[string]string
	mutex          sync.RWMutex
	lastScatterIdx int // 新增：用于轮询打散
}

type clbConfig struct {
	lbIds                         []string
	targetPorts                   []int
	protocols                     []corev1.Protocol
	isFixed                       bool
	annotations                   map[string]string
	allocateLoadBalancerNodePorts bool
	enableClbScatter              bool // 新增：打散开关
	enableMultiIngress            bool // 新增：多 ingress IP 开关
}

func (c *ClbPlugin) Name() string {
	return ClbNetwork
}

func (c *ClbPlugin) Alias() string {
	return AliasCLB
}

func (c *ClbPlugin) Init(client client.Client, options cloudprovider.CloudProviderOptions, ctx context.Context) error {
	log.Infof("[CLB] Init called, options: %+v", options)
	c.mutex.Lock()
	defer c.mutex.Unlock()
	clbOptions, ok := options.(provideroptions.VolcengineOptions)
	if !ok {
		log.Errorf("[CLB] failed to convert options to clbOptions: %+v", options)
		return cperrors.ToPluginError(fmt.Errorf("failed to convert options to clbOptions"), cperrors.InternalError)
	}
	c.minPort = clbOptions.CLBOptions.MinPort
	c.maxPort = clbOptions.CLBOptions.MaxPort
	c.blockPorts = clbOptions.CLBOptions.BlockPorts

	svcList := &corev1.ServiceList{}
	err := client.List(ctx, svcList)
	if err != nil {
		log.Errorf("[CLB] client.List failed: %v", err)
		return err
	}

	c.cache, c.podAllocate = initLbCache(svcList.Items, c.minPort, c.maxPort, c.blockPorts)
	log.Infof("[CLB] Init finished, minPort=%d, maxPort=%d, blockPorts=%v, svcCount=%d", c.minPort, c.maxPort, c.blockPorts, len(svcList.Items))
	return nil
}

func initLbCache(svcList []corev1.Service, minPort, maxPort int32, blockPorts []int32) (map[string]portAllocated, map[string]string) {
	newCache := make(map[string]portAllocated)
	newPodAllocate := make(map[string]string)
	for _, svc := range svcList {
		lbId := svc.Labels[ClbIdLabelKey]
		if lbId != "" && svc.Spec.Type == corev1.ServiceTypeLoadBalancer {
			if newCache[lbId] == nil {
				newCache[lbId] = make(portAllocated, maxPort-minPort)
				for i := minPort; i < maxPort; i++ {
					newCache[lbId][i] = false
				}
			}

			// block ports
			for _, blockPort := range blockPorts {
				newCache[lbId][blockPort] = true
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
	log.Infof("[%s] podAllocate cache complete initialization: %v", ClbNetwork, newPodAllocate)
	return newCache, newPodAllocate
}

func (c *ClbPlugin) OnPodAdded(client client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	log.Infof("[CLB] OnPodAdded called for pod %s/%s", pod.GetNamespace(), pod.GetName())
	return pod, nil
}

func (c *ClbPlugin) OnPodUpdated(client client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	log.Infof("[CLB] OnPodUpdated called for pod %s/%s", pod.GetNamespace(), pod.GetName())
	networkManager := utils.NewNetworkManager(pod, client)

	networkStatus, err := networkManager.GetNetworkStatus()
	if err != nil {
		log.Errorf("[CLB] GetNetworkStatus failed: %v", err)
		return pod, cperrors.ToPluginError(err, cperrors.InternalError)
	}
	networkConfig := networkManager.GetNetworkConfig()
	log.V(4).Infof("[CLB] NetworkConfig: %+v", networkConfig)
	config := parseLbConfig(networkConfig)
	log.V(4).Infof("[CLB] Parsed clbConfig: %+v", config)
	if networkStatus == nil {
		log.Infof("[CLB] networkStatus is nil, set NetworkNotReady for pod %s/%s", pod.GetNamespace(), pod.GetName())
		pod, err := networkManager.UpdateNetworkStatus(gamekruiseiov1alpha1.NetworkStatus{
			CurrentNetworkState: gamekruiseiov1alpha1.NetworkNotReady,
		}, pod)
		if err != nil {
			return pod, cperrors.ToPluginError(err, cperrors.InternalError)
		}
		networkStatus = &gamekruiseiov1alpha1.NetworkStatus{
			CurrentNetworkState: gamekruiseiov1alpha1.NetworkNotReady,
		}
	}

	// get svc
	svc := &corev1.Service{}
	err = client.Get(ctx, types.NamespacedName{
		Name:      pod.GetName(),
		Namespace: pod.GetNamespace(),
	}, svc)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Infof("[CLB] Service not found for pod %s/%s, will create new svc", pod.GetNamespace(), pod.GetName())
			svc, err := c.consSvc(config, pod, client, ctx)
			if err != nil {
				return pod, cperrors.ToPluginError(err, cperrors.InternalError)
			}
			return pod, cperrors.ToPluginError(client.Create(ctx, svc), cperrors.ApiCallError)
		}
		log.Errorf("[CLB] client.Get svc failed: %v", err)
		return pod, cperrors.NewPluginError(cperrors.ApiCallError, err.Error())
	}
	if len(svc.OwnerReferences) > 0 && svc.OwnerReferences[0].Kind == "Pod" && svc.OwnerReferences[0].UID != pod.UID {
		log.Infof("[CLB] waiting old svc %s/%s deleted. old owner pod uid is %s, but now is %s", svc.Namespace, svc.Name, svc.OwnerReferences[0].UID, pod.UID)
		return pod, nil
	}

	// update svc
	if util.GetHash(config) != svc.GetAnnotations()[ClbConfigHashKey] {
		log.Infof("[CLB] config hash changed for pod %s/%s, updating svc", pod.GetNamespace(), pod.GetName())
		networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkNotReady
		pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
		if err != nil {
			return pod, cperrors.NewPluginError(cperrors.InternalError, err.Error())
		}
		newSvc, err := c.consSvc(config, pod, client, ctx)
		if err != nil {
			return pod, cperrors.ToPluginError(err, cperrors.InternalError)
		}
		return pod, cperrors.ToPluginError(client.Update(ctx, newSvc), cperrors.ApiCallError)
	}

	// disable network
	if networkManager.GetNetworkDisabled() && svc.Spec.Type == corev1.ServiceTypeLoadBalancer {
		log.V(4).Infof("[CLB] Network disabled, set svc type to ClusterIP for pod %s/%s", pod.GetNamespace(), pod.GetName())
		svc.Spec.Type = corev1.ServiceTypeClusterIP
		return pod, cperrors.ToPluginError(client.Update(ctx, svc), cperrors.ApiCallError)
	}

	// enable network
	if !networkManager.GetNetworkDisabled() && svc.Spec.Type == corev1.ServiceTypeClusterIP {
		log.V(4).Infof("[CLB] Network enabled, set svc type to LoadBalancer for pod %s/%s", pod.GetNamespace(), pod.GetName())
		svc.Spec.Type = corev1.ServiceTypeLoadBalancer
		return pod, cperrors.ToPluginError(client.Update(ctx, svc), cperrors.ApiCallError)
	}

	// network not ready
	if len(svc.Status.LoadBalancer.Ingress) == 0 {
		log.Infof("[CLB] svc %s/%s has no ingress, network not ready", svc.Namespace, svc.Name)
		networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkNotReady
		pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
		return pod, cperrors.ToPluginError(err, cperrors.InternalError)
	}

	// allow not ready containers
	if util.IsAllowNotReadyContainers(networkManager.GetNetworkConfig()) {
		log.V(4).Infof("[CLB] AllowNotReadyContainers enabled for pod %s/%s", pod.GetNamespace(), pod.GetName())
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
	networkReady(svc, pod, networkStatus, config)
	pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
	return pod, cperrors.ToPluginError(err, cperrors.InternalError)
}

func networkReady(svc *corev1.Service, pod *corev1.Pod, networkStatus *gamekruiseiov1alpha1.NetworkStatus, config *clbConfig) {
	internalAddresses := make([]gamekruiseiov1alpha1.NetworkAddress, 0)
	externalAddresses := make([]gamekruiseiov1alpha1.NetworkAddress, 0)

	// 检查是否启用多 ingress IP 支持
	if config.enableMultiIngress && len(svc.Status.LoadBalancer.Ingress) > 1 {
		// 多 ingress IP 模式：为每个 ingress IP 创建单独的 external address
		for _, ingress := range svc.Status.LoadBalancer.Ingress {
			for _, port := range svc.Spec.Ports {
				instrIPort := port.TargetPort
				instrEPort := intstr.FromInt(int(port.Port))

				// 每个 ingress IP 都创建一个单独的 external address
				externalAddress := gamekruiseiov1alpha1.NetworkAddress{
					IP: ingress.IP,
					Ports: []gamekruiseiov1alpha1.NetworkPort{
						{
							Name:     instrIPort.String(),
							Port:     &instrEPort,
							Protocol: port.Protocol,
						},
					},
				}
				externalAddresses = append(externalAddresses, externalAddress)
			}
		}
	} else {
		// 单 ingress IP 模式（原有逻辑）
		if len(svc.Status.LoadBalancer.Ingress) > 0 {
			for _, port := range svc.Spec.Ports {
				instrIPort := port.TargetPort
				instrEPort := intstr.FromInt(int(port.Port))
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
				externalAddresses = append(externalAddresses, externalAddress)
			}
		}
	}

	// internal addresses 逻辑保持不变
	for _, port := range svc.Spec.Ports {
		instrIPort := port.TargetPort
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
		internalAddresses = append(internalAddresses, internalAddress)
	}

	networkStatus.InternalAddresses = internalAddresses
	networkStatus.ExternalAddresses = externalAddresses
	networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkReady
}

func (c *ClbPlugin) OnPodDeleted(client client.Client, pod *corev1.Pod, ctx context.Context) cperrors.PluginError {
	log.Infof("[CLB] OnPodDeleted called for pod %s/%s", pod.GetNamespace(), pod.GetName())
	networkManager := utils.NewNetworkManager(pod, client)
	networkConfig := networkManager.GetNetworkConfig()
	sc := parseLbConfig(networkConfig)

	var podKeys []string
	if sc.isFixed {
		log.Infof("[CLB] isFixed=true, check gss for pod %s/%s", pod.GetNamespace(), pod.GetName())
		gss, err := util.GetGameServerSetOfPod(pod, client, ctx)
		if err != nil && !errors.IsNotFound(err) {
			return cperrors.ToPluginError(err, cperrors.ApiCallError)
		}
		// gss exists in cluster, do not deAllocate.
		if err == nil && gss.GetDeletionTimestamp() == nil {
			log.Infof("[CLB] gss exists, skip deAllocate for pod %s/%s", pod.GetNamespace(), pod.GetName())
			return nil
		}
		// gss not exists in cluster, deAllocate all the ports related to it.
		for key := range c.podAllocate {
			gssName := pod.GetLabels()[gamekruiseiov1alpha1.GameServerOwnerGssKey]
			if strings.Contains(key, pod.GetNamespace()+"/"+gssName) {
				podKeys = append(podKeys, key)
			}
		}
	} else {
		podKeys = append(podKeys, pod.GetNamespace()+"/"+pod.GetName())
	}

	for _, podKey := range podKeys {
		log.Infof("[CLB] deAllocate for podKey %s", podKey)
		c.deAllocate(podKey)
	}

	return nil
}

func (c *ClbPlugin) allocate(lbIds []string, num int, nsName string, enableClbScatter ...bool) (string, []int32, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	log.Infof("[CLB] allocate called, lbIds=%v, num=%d, nsName=%s, scatter=%v", lbIds, num, nsName, enableClbScatter)

	if len(lbIds) == 0 {
		return "", nil, fmt.Errorf("no load balancer IDs provided")
	}

	var ports []int32
	var lbId string
	useScatter := false
	if len(enableClbScatter) > 0 {
		useScatter = enableClbScatter[0]
	}

	if useScatter && len(lbIds) > 0 {
		log.V(4).Infof("[CLB] scatter enabled, round robin from idx %d", c.lastScatterIdx)
		// 轮询分配
		startIdx := c.lastScatterIdx % len(lbIds)
		for i := 0; i < len(lbIds); i++ {
			idx := (startIdx + i) % len(lbIds)
			clbId := lbIds[idx]
			if c.cache[clbId] == nil {
				// we assume that an empty cache is allways allocatable
				c.newCacheForSingleLb(clbId)
				lbId = clbId
				c.lastScatterIdx = idx + 1 // 下次从下一个开始
				break
			}
			sum := 0
			for p := c.minPort; p < c.maxPort; p++ {
				if !c.cache[clbId][p] {
					sum++
				}
				if sum >= num {
					lbId = clbId
					c.lastScatterIdx = idx + 1 // 下次从下一个开始
					break
				}
			}
			if lbId != "" {
				break
			}
		}
	} else {
		log.V(4).Infof("[CLB] scatter disabled, use default order")
		// 原有逻辑
		for _, clbId := range lbIds {
			if c.cache[clbId] == nil {
				c.newCacheForSingleLb(clbId)
				lbId = clbId
				break
			}
			sum := 0
			for i := c.minPort; i < c.maxPort; i++ {
				if !c.cache[clbId][i] {
					sum++
				}
				if sum >= num {
					lbId = clbId
					break
				}
			}
			if lbId != "" {
				break
			}
		}
	}

	if lbId == "" {
		return "", nil, fmt.Errorf("unable to find load balancer with %d available ports", num)
	}
	// Find available ports sequentially
	portCount := 0
	for port := c.minPort; port < c.maxPort && portCount < num; port++ {
		if !c.cache[lbId][port] {
			c.cache[lbId][port] = true
			ports = append(ports, port)
			portCount++
		}
	}

	// Check if we found enough ports
	if len(ports) < num {
		// Rollback: release allocated ports
		for _, port := range ports {
			c.cache[lbId][port] = false
		}
		return "", nil, fmt.Errorf("insufficient available ports on load balancer %s: found %d, need %d", lbId, len(ports), num)
	}

	c.podAllocate[nsName] = lbId + ":" + util.Int32SliceToString(ports, ",")
	log.Infof("[CLB] pod %s allocate clb %s ports %v", nsName, lbId, ports)
	return lbId, ports, nil
}

// newCacheForSingleLb initializes the port allocation cache for a single load balancer. MUST BE CALLED IN LOCK STATE
func (c *ClbPlugin) newCacheForSingleLb(lbId string) {
	if c.cache[lbId] == nil {
		c.cache[lbId] = make(portAllocated, c.maxPort-c.minPort+1)
		for i := c.minPort; i <= c.maxPort; i++ {
			c.cache[lbId][i] = false
		}
		// block ports
		for _, blockPort := range c.blockPorts {
			c.cache[lbId][blockPort] = true
		}
	}
}

func (c *ClbPlugin) deAllocate(nsName string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	log.Infof("[CLB] deAllocate called for nsName=%s", nsName)
	allocatedPorts, exist := c.podAllocate[nsName]
	if !exist {
		log.Warningf("[CLB] deAllocate: nsName=%s not found in podAllocate", nsName)
		return
	}

	clbPorts := strings.Split(allocatedPorts, ":")
	lbId := clbPorts[0]
	ports := util.StringToInt32Slice(clbPorts[1], ",")
	for _, port := range ports {
		c.cache[lbId][port] = false
	}
	// block ports
	for _, blockPort := range c.blockPorts {
		c.cache[lbId][blockPort] = true
	}

	delete(c.podAllocate, nsName)
	log.Infof("pod %s deallocate clb %s ports %v", nsName, lbId, ports)
}

func init() {
	clbPlugin := ClbPlugin{
		mutex: sync.RWMutex{},
	}
	volcengineProvider.registerPlugin(&clbPlugin)
}

func parseLbConfig(conf []gamekruiseiov1alpha1.NetworkConfParams) *clbConfig {
	log.Infof("[CLB] parseLbConfig called, conf=%+v", conf)
	var lbIds []string
	ports := make([]int, 0)
	protocols := make([]corev1.Protocol, 0)
	isFixed := false
	allocateLoadBalancerNodePorts := true
	annotations := map[string]string{}
	enableClbScatter := false
	enableMultiIngress := false
	for _, c := range conf {
		switch c.Name {
		case ClbIdsConfigName:
			seenIds := make(map[string]struct{})
			for _, clbId := range strings.Split(c.Value, ",") {
				if clbId != "" {
					if _, exists := seenIds[clbId]; !exists {
						lbIds = append(lbIds, clbId)
						seenIds[clbId] = struct{}{}
					}
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
		case AllocateLoadBalancerNodePorts:
			v, err := strconv.ParseBool(c.Value)
			if err != nil {
				continue
			}
			allocateLoadBalancerNodePorts = v
		case ClbAnnotations:
			for _, anno := range strings.Split(c.Value, ",") {
				annoKV := strings.Split(anno, ":")
				if len(annoKV) == 2 {
					annotations[annoKV[0]] = annoKV[1]
				} else {
					log.Warningf("clb annotation %s is invalid", annoKV[0])
				}
			}
		case EnableClbScatterConfigName:
			v, err := strconv.ParseBool(c.Value)
			if err == nil {
				enableClbScatter = v
			}
		case EnableMultiIngressConfigName:
			v, err := strconv.ParseBool(c.Value)
			if err == nil {
				enableMultiIngress = v
			}
		}
	}
	return &clbConfig{
		lbIds:                         lbIds,
		protocols:                     protocols,
		targetPorts:                   ports,
		isFixed:                       isFixed,
		annotations:                   annotations,
		allocateLoadBalancerNodePorts: allocateLoadBalancerNodePorts,
		enableClbScatter:              enableClbScatter,
		enableMultiIngress:            enableMultiIngress,
	}
}

func getPorts(ports []corev1.ServicePort) []int32 {
	var ret []int32
	for _, port := range ports {
		ret = append(ret, port.Port)
	}
	return ret
}

func (c *ClbPlugin) consSvc(config *clbConfig, pod *corev1.Pod, client client.Client, ctx context.Context) (*corev1.Service, error) {
	var ports []int32
	var lbId string
	podKey := pod.GetNamespace() + "/" + pod.GetName()
	allocatedPorts, exist := c.podAllocate[podKey]
	if exist {
		clbPorts := strings.Split(allocatedPorts, ":")
		lbId = clbPorts[0]
		ports = util.StringToInt32Slice(clbPorts[1], ",")
	} else {
		var err error
		lbId, ports, err = c.allocate(config.lbIds, len(config.targetPorts), podKey, config.enableClbScatter)
		if err != nil {
			log.Errorf("[CLB] pod %s allocate clb failed: %v", podKey, err)
			return nil, err
		}
	}

	svcPorts := make([]corev1.ServicePort, 0)
	for i := 0; i < len(config.targetPorts); i++ {
		portName := fmt.Sprintf("%d-%s", config.targetPorts[i], strings.ToLower(string(config.protocols[i])))
		svcPorts = append(svcPorts, corev1.ServicePort{
			Name:       portName,
			Port:       ports[i],
			Protocol:   config.protocols[i],
			TargetPort: intstr.FromInt(config.targetPorts[i]),
		})
	}

	annotations := map[string]string{
		ClbSchedulerKey:    ClbSchedulerWRR,
		ClbAddressTypeKey:  ClbAddressTypePublic,
		ClbIdAnnotationKey: lbId,
		ClbConfigHashKey:   util.GetHash(config),
	}
	for key, value := range config.annotations {
		annotations[key] = value
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:            pod.GetName(),
			Namespace:       pod.GetNamespace(),
			Annotations:     annotations,
			OwnerReferences: getSvcOwnerReference(client, ctx, pod, config.isFixed),
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeLoadBalancer,
			Selector: map[string]string{
				SvcSelectorKey: pod.GetName(),
			},
			Ports:                         svcPorts,
			AllocateLoadBalancerNodePorts: ptr.To[bool](config.allocateLoadBalancerNodePorts),
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
