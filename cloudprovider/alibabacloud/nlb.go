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
	"regexp"
	"strconv"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	log "k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/cloudprovider"
	cperrors "github.com/openkruise/kruise-game/cloudprovider/errors"
	provideroptions "github.com/openkruise/kruise-game/cloudprovider/options"
	"github.com/openkruise/kruise-game/cloudprovider/utils"
	"github.com/openkruise/kruise-game/pkg/util"
)

const (
	NlbNetwork = "AlibabaCloud-NLB"
	AliasNLB   = "NLB-Network"

	// annotations provided by AlibabaCloud Cloud Controller Manager
	LBHealthCheckFlagAnnotationKey           = "service.beta.kubernetes.io/alibaba-cloud-loadbalancer-health-check-flag"
	LBHealthCheckTypeAnnotationKey           = "service.beta.kubernetes.io/alibaba-cloud-loadbalancer-health-check-type"
	LBHealthCheckConnectPortAnnotationKey    = "service.beta.kubernetes.io/alibaba-cloud-loadbalancer-health-check-connect-port"
	LBHealthCheckConnectTimeoutAnnotationKey = "service.beta.kubernetes.io/alibaba-cloud-loadbalancer-health-check-connect-timeout"
	LBHealthyThresholdAnnotationKey          = "service.beta.kubernetes.io/alibaba-cloud-loadbalancer-healthy-threshold"
	LBUnhealthyThresholdAnnotationKey        = "service.beta.kubernetes.io/alibaba-cloud-loadbalancer-unhealthy-threshold"
	LBHealthCheckIntervalAnnotationKey       = "service.beta.kubernetes.io/alibaba-cloud-loadbalancer-health-check-interval"
	LBHealthCheckUriAnnotationKey            = "service.beta.kubernetes.io/alibaba-cloud-loadbalancer-health-check-uri"
	LBHealthCheckDomainAnnotationKey         = "service.beta.kubernetes.io/alibaba-cloud-loadbalancer-health-check-domain"
	LBHealthCheckMethodAnnotationKey         = "service.beta.kubernetes.io/alibaba-cloud-loadbalancer-health-check-method"

	// ConfigNames defined by OKG
	LBHealthCheckFlagConfigName           = "LBHealthCheckFlag"
	LBHealthCheckTypeConfigName           = "LBHealthCheckType"
	LBHealthCheckConnectPortConfigName    = "LBHealthCheckConnectPort"
	LBHealthCheckConnectTimeoutConfigName = "LBHealthCheckConnectTimeout"
	LBHealthCheckIntervalConfigName       = "LBHealthCheckInterval"
	LBHealthCheckUriConfigName            = "LBHealthCheckUri"
	LBHealthCheckDomainConfigName         = "LBHealthCheckDomain"
	LBHealthCheckMethodConfigName         = "LBHealthCheckMethod"
	LBHealthyThresholdConfigName          = "LBHealthyThreshold"
	LBUnhealthyThresholdConfigName        = "LBUnhealthyThreshold"
)

type NlbPlugin struct {
	maxPort     int32
	minPort     int32
	blockPorts  []int32
	cache       map[string]portAllocated
	podAllocate map[string]string
	mutex       sync.RWMutex
}

type nlbConfig struct {
	lbIds       []string
	targetPorts []int
	protocols   []corev1.Protocol
	isFixed     bool
	*nlbHealthConfig
}

type nlbHealthConfig struct {
	lBHealthCheckFlag           string
	lBHealthCheckType           string
	lBHealthCheckConnectPort    string
	lBHealthCheckConnectTimeout string
	lBHealthCheckInterval       string
	lBHealthCheckUri            string
	lBHealthCheckDomain         string
	lBHealthCheckMethod         string
	lBHealthyThreshold          string
	lBUnhealthyThreshold        string
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
	n.blockPorts = slbOptions.BlockPorts

	svcList := &corev1.ServiceList{}
	err := c.List(ctx, svcList)
	if err != nil {
		return err
	}

	n.cache, n.podAllocate = initLbCache(svcList.Items, n.minPort, n.maxPort, n.blockPorts)
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
	sc, err := parseNlbConfig(networkConfig)
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
	err = c.Get(ctx, types.NamespacedName{
		Name:      pod.GetName(),
		Namespace: pod.GetNamespace(),
	}, svc)
	if err != nil {
		if errors.IsNotFound(err) {
			service, err := n.consSvc(sc, pod, c, ctx)
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
	if util.GetHash(sc) != svc.GetAnnotations()[SlbConfigHashKey] {
		networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkNotReady
		pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
		if err != nil {
			return pod, cperrors.NewPluginError(cperrors.InternalError, err.Error())
		}
		service, err := n.consSvc(sc, pod, c, ctx)
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
	sc, err := parseNlbConfig(networkConfig)
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

func (n *NlbPlugin) consSvc(nc *nlbConfig, pod *corev1.Pod, c client.Client, ctx context.Context) (*corev1.Service, error) {
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
		if lbId == "" && ports == nil {
			return nil, fmt.Errorf("there are no avaialable ports for %v", nc.lbIds)
		}
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

	svcAnnotations := map[string]string{
		SlbListenerOverrideKey:         "true",
		SlbIdAnnotationKey:             lbId,
		SlbConfigHashKey:               util.GetHash(nc),
		LBHealthCheckFlagAnnotationKey: nc.lBHealthCheckFlag,
	}
	if nc.lBHealthCheckFlag == "on" {
		svcAnnotations[LBHealthCheckTypeAnnotationKey] = nc.lBHealthCheckType
		svcAnnotations[LBHealthCheckConnectPortAnnotationKey] = nc.lBHealthCheckConnectPort
		svcAnnotations[LBHealthCheckConnectTimeoutAnnotationKey] = nc.lBHealthCheckConnectTimeout
		svcAnnotations[LBHealthCheckIntervalAnnotationKey] = nc.lBHealthCheckInterval
		svcAnnotations[LBHealthyThresholdAnnotationKey] = nc.lBHealthyThreshold
		svcAnnotations[LBUnhealthyThresholdAnnotationKey] = nc.lBUnhealthyThreshold
		if nc.lBHealthCheckType == "http" {
			svcAnnotations[LBHealthCheckDomainAnnotationKey] = nc.lBHealthCheckDomain
			svcAnnotations[LBHealthCheckUriAnnotationKey] = nc.lBHealthCheckUri
			svcAnnotations[LBHealthCheckMethodAnnotationKey] = nc.lBHealthCheckMethod
		}
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:            pod.GetName(),
			Namespace:       pod.GetNamespace(),
			Annotations:     svcAnnotations,
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
	return svc, nil
}

func (n *NlbPlugin) allocate(lbIds []string, num int, nsName string) (string, []int32) {
	n.mutex.Lock()
	defer n.mutex.Unlock()

	var ports []int32
	var lbId string

	// find lb with adequate ports
	for _, slbId := range lbIds {
		sum := 0
		for i := n.minPort; i <= n.maxPort; i++ {
			if !n.cache[slbId][i] {
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
		if n.cache[lbId] == nil {
			// init cache for new lb
			n.cache[lbId] = make(portAllocated, n.maxPort-n.minPort+1)
			for i := n.minPort; i <= n.maxPort; i++ {
				n.cache[lbId][i] = false
			}
			// block ports
			for _, blockPort := range n.blockPorts {
				n.cache[lbId][blockPort] = true
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
	// block ports
	for _, blockPort := range n.blockPorts {
		n.cache[lbId][blockPort] = true
	}

	delete(n.podAllocate, nsName)
	log.Infof("pod %s deallocate nlb %s ports %v", nsName, lbId, ports)
}

func parseNlbConfig(conf []gamekruiseiov1alpha1.NetworkConfParams) (*nlbConfig, error) {
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

	nlbHealthConfig, err := parseNlbHealthConfig(conf)
	if err != nil {
		return nil, err
	}

	return &nlbConfig{
		lbIds:           lbIds,
		protocols:       protocols,
		targetPorts:     ports,
		isFixed:         isFixed,
		nlbHealthConfig: nlbHealthConfig,
	}, nil
}

func parseNlbHealthConfig(conf []gamekruiseiov1alpha1.NetworkConfParams) (*nlbHealthConfig, error) {
	lBHealthCheckFlag := "on"
	lBHealthCheckType := "tcp"
	lBHealthCheckConnectPort := "0"
	lBHealthCheckConnectTimeout := "5"
	lBHealthCheckInterval := "10"
	lBUnhealthyThreshold := "2"
	lBHealthyThreshold := "2"
	lBHealthCheckUri := ""
	lBHealthCheckDomain := ""
	lBHealthCheckMethod := ""

	for _, c := range conf {
		switch c.Name {
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
		case LBHealthCheckConnectPortConfigName:
			portInt, err := strconv.Atoi(c.Value)
			if err != nil {
				return nil, fmt.Errorf("invalid lb health check connect port: %s", c.Value)
			}
			if portInt < 0 || portInt > 65535 {
				return nil, fmt.Errorf("invalid lb health check connect port: %d", portInt)
			}
			lBHealthCheckConnectPort = c.Value
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

	return &nlbHealthConfig{
		lBHealthCheckFlag:           lBHealthCheckFlag,
		lBHealthCheckType:           lBHealthCheckType,
		lBHealthCheckConnectPort:    lBHealthCheckConnectPort,
		lBHealthCheckConnectTimeout: lBHealthCheckConnectTimeout,
		lBHealthCheckInterval:       lBHealthCheckInterval,
		lBHealthCheckUri:            lBHealthCheckUri,
		lBHealthCheckDomain:         lBHealthCheckDomain,
		lBHealthCheckMethod:         lBHealthCheckMethod,
		lBHealthyThreshold:          lBHealthyThreshold,
		lBUnhealthyThreshold:        lBUnhealthyThreshold,
	}, nil
}

func validateDomain(domain string) error {
	if len(domain) < 1 || len(domain) > 80 {
		return fmt.Errorf("the domain length must be between 1 and 80 characters")
	}

	// Regular expression matches lowercase letters, numbers, dashes and periods
	domainRegex := regexp.MustCompile(`^[a-z0-9-.]+$`)
	if !domainRegex.MatchString(domain) {
		return fmt.Errorf("the domain must only contain lowercase letters, numbers, hyphens, and periods")
	}

	// make sure the domain name does not start or end with a dash or period
	if domain[0] == '-' || domain[0] == '.' || domain[len(domain)-1] == '-' || domain[len(domain)-1] == '.' {
		return fmt.Errorf("the domain must not start or end with a hyphen or period")
	}

	// make sure the domain name does not contain consecutive dots or dashes
	if regexp.MustCompile(`(--|\.\.)`).MatchString(domain) {
		return fmt.Errorf("the domain must not contain consecutive hyphens or periods")
	}

	return nil
}

func validateUri(uri string) error {
	if len(uri) < 1 || len(uri) > 80 {
		return fmt.Errorf("string length must be between 1 and 80 characters")
	}

	regexPattern := `^/[0-9a-zA-Z.!$%&'*+/=?^_` + "`" + `{|}~-]*$`
	matched, err := regexp.MatchString(regexPattern, uri)

	if err != nil {
		return fmt.Errorf("regex error: %v", err)
	}

	if !matched {
		return fmt.Errorf("string does not match the required pattern")
	}

	return nil
}
