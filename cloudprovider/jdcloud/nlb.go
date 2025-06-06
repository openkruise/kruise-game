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

package jdcloud

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
	LbType_NLB = "nlb"
)

type JdNLBElasticIp struct {
	ElasticIpId string `json:"elasticIpId"`
}

type JdNLBAlgorithm string

const (
	JdNLBDefaultConnIdleTime int            = 600
	JdNLBAlgorithmRoundRobin JdNLBAlgorithm = "RoundRobin"
	JdNLBAlgorithmLeastConn  JdNLBAlgorithm = "LeastConn"
	JdNLBAlgorithmIpHash     JdNLBAlgorithm = "IpHash"
)

type JdNLBListenerBackend struct {
	ProxyProtocol bool           `json:"proxyProtocol"`
	Algorithm     JdNLBAlgorithm `json:"algorithm"`
}

type JdNLBListener struct {
	Protocol                  string                `json:"protocol"`
	ConnectionIdleTimeSeconds int                   `json:"connectionIdleTimeSeconds"`
	Backend                   *JdNLBListenerBackend `json:"backend"`
}

type JdNLB struct {
	Version          string           `json:"version"`
	LoadBalancerId   string           `json:"loadBalancerId"`
	LoadBalancerType string           `json:"loadBalancerType"`
	Internal         bool             `json:"internal"`
	Listeners        []*JdNLBListener `json:"listeners"`
}

const (
	JdNLBVersion                  = "v1"
	NlbNetwork                    = "JdCloud-NLB"
	AliasNLB                      = "NLB-Network"
	NlbIdLabelKey                 = "service.beta.kubernetes.io/jdcloud-loadbalancer-id"
	NlbIdsConfigName              = "NlbIds"
	PortProtocolsConfigName       = "PortProtocols"
	FixedConfigName               = "Fixed"
	AllocateLoadBalancerNodePorts = "AllocateLoadBalancerNodePorts"
	NlbAnnotations                = "Annotations"
	NlbConfigHashKey              = "game.kruise.io/network-config-hash"
	NlbSpecAnnotationKey          = "service.beta.kubernetes.io/jdcloud-load-balancer-spec"
	SvcSelectorKey                = "statefulset.kubernetes.io/pod-name"
	NlbAlgorithm                  = "service.beta.kubernetes.io/jdcloud-lb-algorithm"
	NlbConnectionIdleTime         = "service.beta.kubernetes.io/jdcloud-lb-idle-time"
)

type portAllocated map[int32]bool

type NlbPlugin struct {
	maxPort     int32
	minPort     int32
	cache       map[string]portAllocated
	podAllocate map[string]string
	mutex       sync.RWMutex
}

type nlbConfig struct {
	lbIds                         []string
	targetPorts                   []int
	protocols                     []corev1.Protocol
	isFixed                       bool
	annotations                   map[string]string
	allocateLoadBalancerNodePorts bool
	algorithm                     string
	connIdleTimeSeconds           int
}

func (c *NlbPlugin) Name() string {
	return NlbNetwork
}

func (c *NlbPlugin) Alias() string {
	return AliasNLB
}

func (c *NlbPlugin) Init(client client.Client, options cloudprovider.CloudProviderOptions, ctx context.Context) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	nlbOptions, ok := options.(provideroptions.JdCloudOptions)
	if !ok {
		return cperrors.ToPluginError(fmt.Errorf("failed to convert options to nlbOptions"), cperrors.InternalError)
	}
	c.minPort = nlbOptions.NLBOptions.MinPort
	c.maxPort = nlbOptions.NLBOptions.MaxPort

	svcList := &corev1.ServiceList{}
	err := client.List(ctx, svcList)
	if err != nil {
		return err
	}

	c.cache, c.podAllocate = initLbCache(svcList.Items, c.minPort, c.maxPort)
	return nil
}

func initLbCache(svcList []corev1.Service, minPort, maxPort int32) (map[string]portAllocated, map[string]string) {
	newCache := make(map[string]portAllocated)
	newPodAllocate := make(map[string]string)
	for _, svc := range svcList {
		lbId := svc.Labels[NlbIdLabelKey]
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
	log.Infof("[%s] podAllocate cache complete initialization: %v", NlbNetwork, newPodAllocate)
	return newCache, newPodAllocate
}

func (c *NlbPlugin) OnPodAdded(client client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	return pod, nil
}

func (c *NlbPlugin) OnPodUpdated(client client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	networkManager := utils.NewNetworkManager(pod, client)

	networkStatus, err := networkManager.GetNetworkStatus()
	if err != nil {
		return pod, cperrors.ToPluginError(err, cperrors.InternalError)
	}
	networkConfig := networkManager.GetNetworkConfig()
	config := parseLbConfig(networkConfig)
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
			return pod, cperrors.ToPluginError(client.Create(ctx, c.consSvc(config, pod, client, ctx)), cperrors.ApiCallError)
		}
		return pod, cperrors.NewPluginError(cperrors.ApiCallError, err.Error())
	}

	// update svc
	if util.GetHash(config) != svc.GetAnnotations()[NlbConfigHashKey] {
		networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkNotReady
		pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
		if err != nil {
			return pod, cperrors.NewPluginError(cperrors.InternalError, err.Error())
		}
		return pod, cperrors.ToPluginError(client.Update(ctx, c.consSvc(config, pod, client, ctx)), cperrors.ApiCallError)
	}

	// disable network
	if networkManager.GetNetworkDisabled() && svc.Spec.Type == corev1.ServiceTypeLoadBalancer {
		svc.Spec.Type = corev1.ServiceTypeClusterIP
		return pod, cperrors.ToPluginError(client.Update(ctx, svc), cperrors.ApiCallError)
	}

	// enable network
	if !networkManager.GetNetworkDisabled() && svc.Spec.Type == corev1.ServiceTypeClusterIP {
		svc.Spec.Type = corev1.ServiceTypeLoadBalancer
		return pod, cperrors.ToPluginError(client.Update(ctx, svc), cperrors.ApiCallError)
	}

	// network not ready
	if len(svc.Status.LoadBalancer.Ingress) == 0 {
		networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkNotReady
		pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
		return pod, cperrors.ToPluginError(err, cperrors.InternalError)
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

func (c *NlbPlugin) OnPodDeleted(client client.Client, pod *corev1.Pod, ctx context.Context) cperrors.PluginError {
	networkManager := utils.NewNetworkManager(pod, client)
	networkConfig := networkManager.GetNetworkConfig()
	sc := parseLbConfig(networkConfig)

	var podKeys []string
	if sc.isFixed {
		gss, err := util.GetGameServerSetOfPod(pod, client, ctx)
		if err != nil && !errors.IsNotFound(err) {
			return cperrors.ToPluginError(err, cperrors.ApiCallError)
		}
		// gss exists in cluster, do not deAllocate.
		if err == nil && gss.GetDeletionTimestamp() == nil {
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
		c.deAllocate(ctx, client, podKey, pod.Namespace)
	}

	return nil
}

func (c *NlbPlugin) allocate(ctx context.Context, cli client.Client, lbIds []string, num int, nsName string, pod *corev1.Pod) (string, []int32) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	var ports []int32
	var lbId string

	// find lb with adequate ports
	for _, nlbId := range lbIds {
		sum := 0
		for i := c.minPort; i < c.maxPort; i++ {
			if !c.cache[nlbId][i] {
				sum++
			}
			if sum >= num {
				lbId = nlbId
				break
			}
		}
	}

	// select ports
	for i := 0; i < num; i++ {
		var port int32
		if c.cache[lbId] == nil {
			c.cache[lbId] = make(portAllocated, c.maxPort-c.minPort)
			for i := c.minPort; i < c.maxPort; i++ {
				c.cache[lbId][i] = false
			}
		}

		for p, allocated := range c.cache[lbId] {
			if !allocated {
				port = p
				break
			}
		}
		c.cache[lbId][port] = true
		ports = append(ports, port)
	}

	c.podAllocate[nsName] = lbId + ":" + util.Int32SliceToString(ports, ",")
	log.Infof("pod %s allocate nlb %s ports %v", nsName, lbId, ports)

	for _, p := range ports {
		alloc := &gamekruiseiov1alpha1.NetworkAllocation{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%s-%d", pod.Name, lbId, p),
				Namespace: pod.Namespace,
			},
			Spec: gamekruiseiov1alpha1.NetworkAllocationSpec{
				LbID:     lbId,
				Port:     p,
				Protocol: string(corev1.ProtocolTCP),
				PodRef: corev1.ObjectReference{
					Kind:      "Pod",
					Namespace: pod.Namespace,
					Name:      pod.Name,
					UID:       pod.UID,
				},
			},
		}
		_ = cli.Create(ctx, alloc)
	}
	return lbId, ports
}

func (c *NlbPlugin) deAllocate(ctx context.Context, cli client.Client, nsName string, namespace string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	allocatedPorts, exist := c.podAllocate[nsName]
	if !exist {
		return
	}

	nlbPorts := strings.Split(allocatedPorts, ":")
	lbId := nlbPorts[0]
	ports := util.StringToInt32Slice(nlbPorts[1], ",")
	for _, port := range ports {
		c.cache[lbId][port] = false
		name := fmt.Sprintf("%s-%s-%d", strings.Split(nsName, "/")[1], lbId, port)
		alloc := &gamekruiseiov1alpha1.NetworkAllocation{}
		if err := cli.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, alloc); err == nil {
			_ = cli.Delete(ctx, alloc)
		}
	}

	delete(c.podAllocate, nsName)
	log.Infof("pod %s deallocate nlb %s ports %v", nsName, lbId, ports)
}

func init() {
	JdNlbPlugin := NlbPlugin{
		mutex: sync.RWMutex{},
	}
	jdcloudProvider.registerPlugin(&JdNlbPlugin)
}

func parseLbConfig(conf []gamekruiseiov1alpha1.NetworkConfParams) *nlbConfig {
	var lbIds []string
	ports := make([]int, 0)
	protocols := make([]corev1.Protocol, 0)
	isFixed := false
	allocateLoadBalancerNodePorts := true
	annotations := map[string]string{}
	algo := string(JdNLBAlgorithmRoundRobin)
	idleTime := JdNLBDefaultConnIdleTime
	for _, c := range conf {
		switch c.Name {
		case NlbIdsConfigName:
			for _, clbId := range strings.Split(c.Value, ",") {
				if clbId != "" {
					lbIds = append(lbIds, clbId)
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
		case NlbAlgorithm:
			algo = c.Value
		case NlbConnectionIdleTime:
			t, err := strconv.Atoi(c.Value)
			if err == nil {
				idleTime = t
			}
		case AllocateLoadBalancerNodePorts:
			v, err := strconv.ParseBool(c.Value)
			if err != nil {
				continue
			}
			allocateLoadBalancerNodePorts = v
		case NlbAnnotations:
			for _, anno := range strings.Split(c.Value, ",") {
				annoKV := strings.Split(anno, ":")
				if len(annoKV) == 2 {
					annotations[annoKV[0]] = annoKV[1]
				} else {
					log.Warningf("nlb annotation %s is invalid", annoKV[0])
				}
			}
		}
	}
	return &nlbConfig{
		lbIds:                         lbIds,
		protocols:                     protocols,
		targetPorts:                   ports,
		isFixed:                       isFixed,
		annotations:                   annotations,
		allocateLoadBalancerNodePorts: allocateLoadBalancerNodePorts,
		algorithm:                     algo,
		connIdleTimeSeconds:           idleTime,
	}
}

func getPorts(ports []corev1.ServicePort) []int32 {
	var ret []int32
	for _, port := range ports {
		ret = append(ret, port.Port)
	}
	return ret
}

func (c *NlbPlugin) consSvc(config *nlbConfig, pod *corev1.Pod, client client.Client, ctx context.Context) *corev1.Service {
	var ports []int32
	var lbId string
	podKey := pod.GetNamespace() + "/" + pod.GetName()
	allocatedPorts, exist := c.podAllocate[podKey]
	if exist {
		nlbPorts := strings.Split(allocatedPorts, ":")
		lbId = nlbPorts[0]
		ports = util.StringToInt32Slice(nlbPorts[1], ",")
	} else {
		lbId, ports = c.allocate(ctx, client, config.lbIds, len(config.targetPorts), podKey, pod)
	}

	svcPorts := make([]corev1.ServicePort, 0)
	for i := 0; i < len(config.targetPorts); i++ {
		svcPorts = append(svcPorts, corev1.ServicePort{
			Name:       strconv.Itoa(config.targetPorts[i]),
			Port:       ports[i],
			Protocol:   config.protocols[i],
			TargetPort: intstr.FromInt(config.targetPorts[i]),
		})
	}

	annotations := map[string]string{
		NlbIdLabelKey:        lbId,
		NlbSpecAnnotationKey: getNLBSpec(svcPorts, lbId, config.algorithm, config.connIdleTimeSeconds),
		NlbConfigHashKey:     util.GetHash(config),
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
	return svc
}

func getNLBSpec(ports []corev1.ServicePort, lbId, algorithm string, connIdleTimeSeconds int) string {
	jdNlb := _getNLBSpec(ports, lbId, algorithm, connIdleTimeSeconds)
	bytes, err := json.Marshal(jdNlb)
	if err != nil {
		return ""
	}
	return string(bytes)
}

func _getNLBSpec(ports []corev1.ServicePort, lbId, algorithm string, connIdleTimeSeconds int) *JdNLB {
	var listeners = make([]*JdNLBListener, len(ports))
	for k, v := range ports {
		listeners[k] = &JdNLBListener{
			Protocol:                  strings.ToUpper(string(v.Protocol)),
			ConnectionIdleTimeSeconds: connIdleTimeSeconds,
			Backend: &JdNLBListenerBackend{
				Algorithm: JdNLBAlgorithm(algorithm),
			},
		}
	}
	return &JdNLB{
		Version:          JdNLBVersion,
		LoadBalancerId:   lbId,
		LoadBalancerType: LbType_NLB,
		Internal:         false,
		Listeners:        listeners,
	}
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
