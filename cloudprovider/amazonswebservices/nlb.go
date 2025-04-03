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

package amazonswebservices

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	ackv1alpha1 "github.com/aws-controllers-k8s/elbv2-controller/apis/v1alpha1"
	"github.com/kr/pretty"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	log "k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	elbv2api "sigs.k8s.io/aws-load-balancer-controller/apis/elbv2/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/cloudprovider"
	cperrors "github.com/openkruise/kruise-game/cloudprovider/errors"
	provideroptions "github.com/openkruise/kruise-game/cloudprovider/options"
	"github.com/openkruise/kruise-game/cloudprovider/utils"
	"github.com/openkruise/kruise-game/pkg/util"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

const (
	NlbNetwork               = "AmazonWebServices-NLB"
	AliasNlb                 = "NLB-Network"
	NlbARNsConfigName        = "NlbARNs"
	NlbVPCIdConfigName       = "NlbVPCId"
	NlbHealthCheckConfigName = "NlbHealthCheck"
	PortProtocolsConfigName  = "PortProtocols"
	FixedConfigName          = "Fixed"
	NlbAnnotations           = "Annotations"
	NlbARNAnnoKey            = "service.beta.kubernetes.io/aws-load-balancer-nlb-arn"
	NlbPortAnnoKey           = "service.beta.kubernetes.io/aws-load-balancer-nlb-port"
	AWSTargetGroupSyncStatus = "aws-load-balancer-nlb-target-group-synced"
	SvcSelectorKey           = "statefulset.kubernetes.io/pod-name"
	NlbConfigHashKey         = "game.kruise.io/network-config-hash"
	ResourceTagKey           = "managed-by"
	ResourceTagValue         = "game.kruise.io"
)

const (
	healthCheckEnabled         = "healthCheckEnabled"
	healthCheckIntervalSeconds = "healthCheckIntervalSeconds"
	healthCheckPath            = "healthCheckPath"
	healthCheckPort            = "healthCheckPort"
	healthCheckProtocol        = "healthCheckProtocol"
	healthCheckTimeoutSeconds  = "healthCheckTimeoutSeconds"
	healthyThresholdCount      = "healthyThresholdCount"
	unhealthyThresholdCount    = "unhealthyThresholdCount"
	listenerActionType         = "forward"
)

type portAllocated map[int32]bool
type nlbPorts struct {
	arn   string
	ports []int32
}

type NlbPlugin struct {
	maxPort     int32
	minPort     int32
	cache       map[string]portAllocated
	podAllocate map[string]*nlbPorts
	mutex       sync.RWMutex
}

type backend struct {
	targetPort int
	protocol   corev1.Protocol
}

type healthCheck struct {
	healthCheckEnabled         *bool
	healthCheckIntervalSeconds *int64
	healthCheckPath            *string
	healthCheckPort            *string
	healthCheckProtocol        *string
	healthCheckTimeoutSeconds  *int64
	healthyThresholdCount      *int64
	unhealthyThresholdCount    *int64
}

type nlbConfig struct {
	loadBalancerARNs []string
	healthCheck      *healthCheck
	vpcID            string
	backends         []*backend
	isFixed          bool
	annotations      map[string]string
}

func startWatchTargetGroup(ctx context.Context) error {
	var err error
	go func() {
		err = watchTargetGroup(ctx)
	}()
	return err
}

func watchTargetGroup(ctx context.Context) error {
	scheme := runtime.NewScheme()
	utilruntime.Must(ackv1alpha1.AddToScheme(scheme))
	utilruntime.Must(elbv2api.AddToScheme(scheme))
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
		Scheme: scheme,
	})
	if err != nil {
		return err
	}
	informer, err := mgr.GetCache().GetInformer(ctx, &ackv1alpha1.TargetGroup{})
	if err != nil {
		return fmt.Errorf("failed to get informer: %v", err)
	}

	if _, err := informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			handleTargetGroupEvent(ctx, mgr.GetClient(), obj)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			handleTargetGroupEvent(ctx, mgr.GetClient(), newObj)
		},
	}); err != nil {
		return fmt.Errorf("failed to add event handler: %v", err)
	}

	log.Info("Start to watch TargetGroups successfully")
	return mgr.Start(ctx)
}

func handleTargetGroupEvent(ctx context.Context, c client.Client, obj interface{}) {
	targetGroup, ok := obj.(*ackv1alpha1.TargetGroup)
	if !ok {
		log.Warning("Failed to convert event.Object to TargetGroup")
		return
	}
	if targetGroup.Labels[AWSTargetGroupSyncStatus] == "false" {
		targetGroupARN, err := getACKTargetGroupARN(targetGroup)
		if err != nil {
			return
		}
		log.Infof("targetGroup sync request watched, start to sync %s/%s, ARN: %s",
			targetGroup.GetNamespace(), targetGroup.GetName(), targetGroupARN)
		err = syncListenerAndTargetGroupBinding(ctx, c, targetGroup, &targetGroupARN)
		if err != nil {
			log.Errorf("syncListenerAndTargetGroupBinding by targetGroup %s error %v",
				pretty.Sprint(targetGroup), err)
			return
		}

		patch := client.RawPatch(types.MergePatchType,
			[]byte(fmt.Sprintf(`{"metadata":{"labels":{"%s":"true"}}}`, AWSTargetGroupSyncStatus)))
		err = c.Patch(ctx, targetGroup, patch)
		if err != nil {
			log.Warningf("patch targetGroup %s %s error %v",
				pretty.Sprint(targetGroup), AWSTargetGroupSyncStatus, err)
		}
	}
}

func (n *NlbPlugin) Name() string {
	return NlbNetwork
}

func (n *NlbPlugin) Alias() string {
	return AliasNlb
}

func (n *NlbPlugin) Init(c client.Client, options cloudprovider.CloudProviderOptions, ctx context.Context) error {
	n.mutex.Lock()
	defer n.mutex.Unlock()
	err := startWatchTargetGroup(ctx)
	if err != nil {
		return err
	}
	nlbOptions, ok := options.(provideroptions.AmazonsWebServicesOptions)
	if !ok {
		return cperrors.ToPluginError(fmt.Errorf("failed to convert options to nlbOptions"), cperrors.InternalError)
	}
	n.minPort = nlbOptions.NLBOptions.MinPort
	n.maxPort = nlbOptions.NLBOptions.MaxPort

	svcList := &corev1.ServiceList{}
	err = c.List(ctx, svcList, client.MatchingLabels{ResourceTagKey: ResourceTagValue})
	if err != nil {
		return err
	}

	n.initLbCache(svcList.Items)
	if err != nil {
		return err
	}
	log.Infof("[%s] podAllocate cache complete initialization: %s", NlbNetwork, pretty.Sprint(n.podAllocate))
	return nil
}

func (n *NlbPlugin) initCache(nlbARN string) {
	if n.cache[nlbARN] == nil {
		n.cache[nlbARN] = make(portAllocated, n.maxPort-n.minPort+1)
		for j := n.minPort; j <= n.maxPort; j++ {
			n.cache[nlbARN][j] = false
		}
	}
}

func (n *NlbPlugin) initLbCache(svcList []corev1.Service) {
	if n.cache == nil {
		n.cache = make(map[string]portAllocated)
	}
	if n.podAllocate == nil {
		n.podAllocate = make(map[string]*nlbPorts)
	}
	for _, svc := range svcList {
		lbARN := svc.Annotations[NlbARNAnnoKey]
		if lbARN != "" {
			n.initCache(lbARN)
			var ports []int32
			for _, port := range getPorts(svc.Spec.Ports) {
				if port <= n.maxPort && port >= n.minPort {
					n.cache[lbARN][port] = true
					ports = append(ports, port)
				}
			}
			if len(ports) != 0 {
				n.podAllocate[svc.GetNamespace()+"/"+svc.GetName()] = &nlbPorts{arn: lbARN, ports: ports}
			}
		}
	}
}

func (n *NlbPlugin) OnPodAdded(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	return pod, nil
}

func (n *NlbPlugin) OnPodUpdated(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	networkManager := utils.NewNetworkManager(pod, c)

	networkStatus, err := networkManager.GetNetworkStatus()
	if err != nil {
		return pod, cperrors.ToPluginError(err, cperrors.InternalError)
	}
	networkConfig := networkManager.GetNetworkConfig()
	lbConfig := parseLbConfig(networkConfig)
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
			return pod, cperrors.ToPluginError(n.syncTargetGroupAndService(lbConfig, pod, c, ctx), cperrors.ApiCallError)
		}
		return pod, cperrors.NewPluginError(cperrors.ApiCallError, err.Error())
	}

	// update svc
	if util.GetHash(lbConfig) != svc.GetAnnotations()[NlbConfigHashKey] {
		networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkNotReady
		pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
		if err != nil {
			return pod, cperrors.NewPluginError(cperrors.InternalError, err.Error())
		}
		return pod, cperrors.ToPluginError(n.syncTargetGroupAndService(lbConfig, pod, c, ctx), cperrors.ApiCallError)
	}

	// disable network
	if networkManager.GetNetworkDisabled() {
		return pod, cperrors.ToPluginError(c.DeleteAllOf(ctx, &elbv2api.TargetGroupBinding{},
			client.InNamespace(pod.GetNamespace()),
			client.MatchingLabels(map[string]string{ResourceTagKey: ResourceTagValue, SvcSelectorKey: pod.GetName()})),
			cperrors.ApiCallError)
	}

	// enable network
	if !networkManager.GetNetworkDisabled() {
		selector := client.MatchingLabels{
			ResourceTagKey: ResourceTagValue,
			SvcSelectorKey: pod.GetName(),
		}
		var tgbList elbv2api.TargetGroupBindingList
		err = c.List(ctx, &tgbList, selector)
		if err != nil {
			return pod, cperrors.ToPluginError(err, cperrors.ApiCallError)
		}
		if len(tgbList.Items) != len(svc.Spec.Ports) {
			var tgList ackv1alpha1.TargetGroupList
			err = c.List(ctx, &tgList, selector)
			if err != nil {
				return pod, cperrors.ToPluginError(err, cperrors.ApiCallError)
			}
			patch := client.RawPatch(types.MergePatchType,
				[]byte(fmt.Sprintf(`{"metadata":{"labels":{"%s":"false"}}}`, AWSTargetGroupSyncStatus)))
			for _, tg := range tgList.Items {
				err = c.Patch(ctx, &tg, patch)
				if err != nil {
					return pod, cperrors.ToPluginError(err, cperrors.ApiCallError)
				}
			}
		}
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
			EndPoint: generateNlbEndpoint(svc.Annotations[NlbARNAnnoKey]),
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

func generateNlbEndpoint(nlbARN string) string {
	const arnPartsCount = 6
	const loadBalancerPrefix = "loadbalancer/net/"
	parts := strings.Split(nlbARN, ":")
	if len(parts) != arnPartsCount {
		return ""
	}
	region := parts[3]
	loadBalancerName := strings.ReplaceAll(strings.TrimPrefix(parts[5], loadBalancerPrefix), "/", "-")
	return fmt.Sprintf("%s.elb.%s.amazonaws.com", loadBalancerName, region)
}

func (n *NlbPlugin) OnPodDeleted(client client.Client, pod *corev1.Pod, ctx context.Context) cperrors.PluginError {
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

func (n *NlbPlugin) allocate(lbARNs []string, num int, nsName string) *nlbPorts {
	n.mutex.Lock()
	defer n.mutex.Unlock()

	// Initialize cache for each lbARN if not already done
	for _, nlbARN := range lbARNs {
		n.initCache(nlbARN)
	}

	// Find lbARN with enough free ports
	selectedARN := n.findLbWithFreePorts(lbARNs, num)
	if selectedARN == "" {
		return nil
	}

	// Allocate ports
	ports := n.allocatePorts(selectedARN, num)

	n.podAllocate[nsName] = &nlbPorts{arn: selectedARN, ports: ports}
	log.Infof("pod %s allocate nlb %s ports %v", nsName, selectedARN, ports)
	return &nlbPorts{arn: selectedARN, ports: ports}
}

func (n *NlbPlugin) findLbWithFreePorts(lbARNs []string, num int) string {
	for _, nlbARN := range lbARNs {
		freePorts := 0
		for i := n.minPort; i <= n.maxPort && freePorts < num; i++ {
			if !n.cache[nlbARN][i] {
				freePorts++
			}
		}
		if freePorts >= num {
			return nlbARN
		}
	}
	return ""
}

func (n *NlbPlugin) allocatePorts(lbARN string, num int) []int32 {
	var ports []int32
	for i := 0; i < num; i++ {
		for p := n.minPort; p <= n.maxPort; p++ {
			if !n.cache[lbARN][p] {
				n.cache[lbARN][p] = true
				ports = append(ports, p)
				break
			}
		}
	}
	return ports
}

func (n *NlbPlugin) deAllocate(nsName string) {
	n.mutex.Lock()
	defer n.mutex.Unlock()

	allocatedPorts, exist := n.podAllocate[nsName]
	if !exist {
		return
	}

	lbARN := allocatedPorts.arn
	ports := allocatedPorts.ports
	for _, port := range ports {
		n.cache[lbARN][port] = false
	}

	delete(n.podAllocate, nsName)
	log.Infof("pod %s deallocate nlb %s ports %v", nsName, lbARN, ports)
}

func init() {
	nlbPlugin := NlbPlugin{
		mutex: sync.RWMutex{},
	}
	amazonsWebServicesProvider.registerPlugin(&nlbPlugin)
}

func parseLbConfig(conf []gamekruiseiov1alpha1.NetworkConfParams) *nlbConfig {
	var lbARNs []string
	var hc healthCheck
	var vpcId string
	backends := make([]*backend, 0)
	isFixed := false
	annotations := map[string]string{}
	for _, c := range conf {
		switch c.Name {
		case NlbARNsConfigName:
			for _, nlbARN := range strings.Split(c.Value, ",") {
				if nlbARN != "" {
					lbARNs = append(lbARNs, nlbARN)
				}
			}
		case NlbHealthCheckConfigName:
			for _, healthCheckConf := range strings.Split(c.Value, ",") {
				confKV := strings.Split(healthCheckConf, ":")
				if len(confKV) == 2 {
					switch confKV[0] {
					case healthCheckEnabled:
						v, err := strconv.ParseBool(confKV[1])
						if err != nil {
							continue
						}
						hc.healthCheckEnabled = &v
					case healthCheckIntervalSeconds:
						v, err := strconv.ParseInt(confKV[1], 10, 64)
						if err != nil {
							continue
						}
						hc.healthCheckIntervalSeconds = &v
					case healthCheckPath:
						hc.healthCheckPath = &confKV[1]
					case healthCheckPort:
						hc.healthCheckPort = &confKV[1]
					case healthCheckProtocol:
						hc.healthCheckProtocol = &confKV[1]
					case healthCheckTimeoutSeconds:
						v, err := strconv.ParseInt(confKV[1], 10, 64)
						if err != nil {
							continue
						}
						hc.healthCheckTimeoutSeconds = &v
					case healthyThresholdCount:
						v, err := strconv.ParseInt(confKV[1], 10, 64)
						if err != nil {
							continue
						}
						hc.healthyThresholdCount = &v
					case unhealthyThresholdCount:
						v, err := strconv.ParseInt(confKV[1], 10, 64)
						if err != nil {
							continue
						}
						hc.unhealthyThresholdCount = &v
					}
				} else {
					log.Warningf("nlb %s %s is invalid", NlbHealthCheckConfigName, confKV)
				}
			}
		case NlbVPCIdConfigName:
			vpcId = c.Value
		case PortProtocolsConfigName:
			for _, pp := range strings.Split(c.Value, ",") {
				ppSlice := strings.Split(pp, "/")
				port, err := strconv.Atoi(ppSlice[0])
				if err != nil {
					continue
				}
				var protocol corev1.Protocol
				if len(ppSlice) != 2 {
					protocol = corev1.ProtocolTCP
				} else {
					protocol = corev1.Protocol(ppSlice[1])
				}
				backends = append(backends, &backend{
					targetPort: port,
					protocol:   protocol,
				})
			}
		case FixedConfigName:
			v, err := strconv.ParseBool(c.Value)
			if err != nil {
				continue
			}
			isFixed = v
		case NlbAnnotations:
			for _, anno := range strings.Split(c.Value, ",") {
				annoKV := strings.Split(anno, ":")
				if len(annoKV) == 2 {
					annotations[annoKV[0]] = annoKV[1]
				} else {
					log.Warningf("nlb %s %s is invalid", NlbAnnotations, c.Value)
				}
			}
		}
	}
	return &nlbConfig{
		loadBalancerARNs: lbARNs,
		healthCheck:      &hc,
		vpcID:            vpcId,
		backends:         backends,
		isFixed:          isFixed,
		annotations:      annotations,
	}
}

func getACKTargetGroupARN(tg *ackv1alpha1.TargetGroup) (string, error) {
	if len(tg.Status.Conditions) == 0 {
		return "", fmt.Errorf("targetGroup status not ready")
	}
	if tg.Status.Conditions[0].Status != "True" {
		return "", fmt.Errorf("targetGroup status error: %s %s",
			*tg.Status.Conditions[0].Message, *tg.Status.Conditions[0].Reason)
	}
	if tg.Status.ACKResourceMetadata != nil && tg.Status.ACKResourceMetadata.ARN != nil {
		return string(*tg.Status.ACKResourceMetadata.ARN), nil
	} else {
		return "", fmt.Errorf("targetGroup status not ready")
	}
}

func (n *NlbPlugin) syncTargetGroupAndService(config *nlbConfig,
	pod *corev1.Pod, client client.Client, ctx context.Context) error {
	var ports []int32
	var lbARN string
	podKey := pod.GetNamespace() + "/" + pod.GetName()
	allocatedPorts, exist := n.podAllocate[podKey]
	if !exist {
		allocatedPorts = n.allocate(config.loadBalancerARNs, len(config.backends), podKey)
		if allocatedPorts == nil {
			return fmt.Errorf("no NLB has %d enough available ports for %s", len(config.backends), podKey)
		}
	}
	lbARN = allocatedPorts.arn
	ports = allocatedPorts.ports

	ownerReference := getOwnerReference(client, ctx, pod, config.isFixed)
	for i := range ports {
		targetGroupName := fmt.Sprintf("%s-%d", pod.GetName(), ports[i])
		protocol := string(config.backends[i].protocol)
		targetPort := int64(config.backends[i].targetPort)
		var targetTypeIP = string(ackv1alpha1.TargetTypeEnum_ip)
		_, err := controllerutil.CreateOrUpdate(ctx, client, &ackv1alpha1.TargetGroup{
			ObjectMeta: metav1.ObjectMeta{
				Name:            targetGroupName,
				Namespace:       pod.GetNamespace(),
				OwnerReferences: ownerReference,
				Labels: map[string]string{
					ResourceTagKey:           ResourceTagValue,
					SvcSelectorKey:           pod.GetName(),
					AWSTargetGroupSyncStatus: "false",
				},
				Annotations: map[string]string{
					NlbARNAnnoKey:  lbARN,
					NlbPortAnnoKey: fmt.Sprintf("%d", ports[i]),
				},
			},
			Spec: ackv1alpha1.TargetGroupSpec{
				HealthCheckEnabled:         config.healthCheck.healthCheckEnabled,
				HealthCheckIntervalSeconds: config.healthCheck.healthCheckIntervalSeconds,
				HealthCheckPath:            config.healthCheck.healthCheckPath,
				HealthCheckPort:            config.healthCheck.healthCheckPort,
				HealthCheckProtocol:        config.healthCheck.healthCheckProtocol,
				HealthCheckTimeoutSeconds:  config.healthCheck.healthCheckTimeoutSeconds,
				HealthyThresholdCount:      config.healthCheck.healthyThresholdCount,
				UnhealthyThresholdCount:    config.healthCheck.unhealthyThresholdCount,
				Name:                       &targetGroupName,
				Protocol:                   &protocol,
				Port:                       &targetPort,
				VPCID:                      &config.vpcID,
				TargetType:                 &targetTypeIP,
				Tags: []*ackv1alpha1.Tag{{Key: ptr.To[string](ResourceTagKey),
					Value: ptr.To[string](ResourceTagValue)}},
			},
		}, func() error { return nil })
		if err != nil {
			return err
		}
	}

	svcPorts := make([]corev1.ServicePort, 0)
	for i := 0; i < len(config.backends); i++ {
		svcPorts = append(svcPorts, corev1.ServicePort{
			Name:       strconv.Itoa(config.backends[i].targetPort),
			Port:       ports[i],
			Protocol:   config.backends[i].protocol,
			TargetPort: intstr.FromInt(config.backends[i].targetPort),
		})
	}
	annotations := map[string]string{
		NlbARNAnnoKey:    lbARN,
		NlbConfigHashKey: util.GetHash(config),
	}
	for key, value := range config.annotations {
		annotations[key] = value
	}
	_, err := controllerutil.CreateOrUpdate(ctx, client, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:            pod.GetName(),
			Namespace:       pod.GetNamespace(),
			Annotations:     annotations,
			OwnerReferences: ownerReference,
			Labels: map[string]string{
				ResourceTagKey: ResourceTagValue,
				SvcSelectorKey: pod.GetName(),
			},
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Selector: map[string]string{
				SvcSelectorKey: pod.GetName(),
			},
			Ports: svcPorts,
		},
	}, func() error { return nil })
	if err != nil {
		return err
	}

	return nil
}

func syncListenerAndTargetGroupBinding(ctx context.Context, client client.Client,
	tg *ackv1alpha1.TargetGroup, targetGroupARN *string) error {
	actionType := listenerActionType
	port, err := strconv.ParseInt(tg.Annotations[NlbPortAnnoKey], 10, 64)
	if err != nil {
		return err
	}
	lbARN := tg.Annotations[NlbARNAnnoKey]
	podName := tg.Labels[SvcSelectorKey]
	_, err = controllerutil.CreateOrUpdate(ctx, client, &ackv1alpha1.Listener{
		ObjectMeta: metav1.ObjectMeta{
			Name:            tg.GetName(),
			Namespace:       tg.GetNamespace(),
			OwnerReferences: tg.GetOwnerReferences(),
			Labels: map[string]string{
				ResourceTagKey: ResourceTagValue,
				SvcSelectorKey: podName,
			},
		},
		Spec: ackv1alpha1.ListenerSpec{
			Protocol:        tg.Spec.Protocol,
			Port:            &port,
			LoadBalancerARN: &lbARN,
			DefaultActions: []*ackv1alpha1.Action{
				{
					TargetGroupARN: targetGroupARN,
					Type:           &actionType,
				},
			},
			Tags: []*ackv1alpha1.Tag{{Key: ptr.To[string](ResourceTagKey),
				Value: ptr.To[string](ResourceTagValue)}},
		},
	}, func() error { return nil })
	if err != nil {
		return err
	}

	var targetTypeIP = elbv2api.TargetTypeIP
	_, err = controllerutil.CreateOrUpdate(ctx, client, &elbv2api.TargetGroupBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:            tg.GetName(),
			Namespace:       tg.GetNamespace(),
			OwnerReferences: tg.GetOwnerReferences(),
			Labels: map[string]string{
				ResourceTagKey: ResourceTagValue,
				SvcSelectorKey: podName,
			},
		},
		Spec: elbv2api.TargetGroupBindingSpec{
			TargetGroupARN: *targetGroupARN,
			TargetType:     &targetTypeIP,
			ServiceRef: elbv2api.ServiceReference{
				Name: podName,
				Port: intstr.FromInt(int(port)),
			},
		},
	}, func() error { return nil })
	if err != nil {
		return err
	}

	return nil
}

func getPorts(ports []corev1.ServicePort) []int32 {
	var ret []int32
	for _, port := range ports {
		ret = append(ret, port.Port)
	}
	return ret
}

func getOwnerReference(c client.Client, ctx context.Context, pod *corev1.Pod, isFixed bool) []metav1.OwnerReference {
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
