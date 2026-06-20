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
	AllocatePolicyConfigName = "AllocatePolicy"
	NlbVPCIdConfigName       = "NlbVPCId"
	NlbHealthCheckConfigName = "NlbHealthCheck"
	PortProtocolsConfigName  = "PortProtocols"
	FixedConfigName          = "Fixed"
	NlbAnnotations           = "Annotations"
	NlbARNAnnoKey            = "service.beta.kubernetes.io/aws-load-balancer-nlb-arn"
	NlbPortAnnoKey           = "service.beta.kubernetes.io/aws-load-balancer-nlb-port"
	AWSTargetGroupSyncStatus = "aws-load-balancer-nlb-target-group-synced"
	SvcSelectorKey           = "statefulset.kubernetes.io/pod-name"
	// ReadinessGatePrefix is the prefix the AWS Load Balancer Controller uses for
	// the pod readiness-gate condition it manages per TargetGroupBinding. The full
	// condition type is ReadinessGatePrefix+<TargetGroupBinding name>, and the
	// controller only sets it True once the corresponding NLB target is healthy.
	// We pre-inject this gate in OnPodAdded so GameServer readiness reflects real
	// NLB reachability rather than just "Service/TargetGroupBinding created".
	ReadinessGatePrefix = "target-health.elbv2.k8s.aws/"
	NlbConfigHashKey    = "game.kruise.io/network-config-hash"
	ResourceTagKey      = "managed-by"
	ResourceTagValue    = "game.kruise.io"
)

// ghostRegistrationStuckSeconds is how long a readiness gate may stay False
// before we treat it as a stuck "ghost registration" and self-heal it.
//
// When a pod IP is reused while still draining in another TargetGroup, the AWS
// Load Balancer Controller registers the target but AWS leaves it draining and
// later drops it, so the gate stays False forever and the controller never
// retries (the TargetGroupBinding spec hash is unchanged). A normal target goes
// initial->healthy within the health-check window; anything stuck False well
// beyond that is the bug. 90s comfortably covers a default health check
// (interval*healthyThreshold) plus registration latency.
const ghostRegistrationStuckSeconds = 90

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

const (
	// allocatePolicyDefault: first-fit 溢出——按 ARN 列表顺序, 第一个够用就用, 填满前一个才用下一个。
	allocatePolicyDefault = "default"
	// allocatePolicyBalanced: 均衡——选当前剩余空闲端口最多的 NLB, 把游戏服摊平到各 NLB(故障域隔离)。
	// 移植自 cloudprovider/alibabacloud/multi_nlbs.go 的 "balanced" 策略。
	allocatePolicyBalanced = "balanced"
)

// ProtocolTCPUDP is a synthetic protocol value indicating that a target
// port should accept both TCP and UDP traffic on the same AWS NLB
// listener. It is NOT a Kubernetes-recognized protocol and lives only on
// parsed backends. When emitting a Kubernetes Service the plugin expands
// it into one TCP and one UDP ServicePort that share the same Port.
// When emitting AWS resources (TargetGroup, Listener) it is translated
// to the AWS protocol value "TCP_UDP".
const ProtocolTCPUDP corev1.Protocol = "TCPUDP"

const awsProtocolTCPUDP = "TCP_UDP"

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
	allocatePolicy   string
	healthCheck      *healthCheck
	vpcID            string
	backends         []*backend
	isFixed          bool
	annotations      map[string]string
}

// configHash returns the hash used to detect whether a pod's network must be
// reconfigured. It intentionally EXCLUDES loadBalancerARNs.
//
// Which NLB a pod uses is decided once at allocation time and then pinned in
// the podAllocate cache and the per-pod Service/TargetGroup/Listener objects
// (named after the pod). Adding or removing an ARN in NlbARNs must NOT force
// already-allocated pods to reconfigure: doing so re-runs port allocation
// against a freshly-added NLB whose port cache is empty, causing double
// allocation, orphan listeners and a non-self-healing deadlock (servers that
// were healthy get knocked to NotReady). By hashing everything EXCEPT the ARN
// list, a change to NlbARNs leaves existing pods untouched (they keep their
// pinned ARN/ports), while only NEW pods pick up the newly added ARN via
// allocate(). Other config changes (health check, ports, protocols, fixed,
// annotations) still change the hash and trigger a legitimate reconfigure.
func (c *nlbConfig) configHash() string {
	if c == nil {
		return ""
	}
	view := *c
	view.loadBalancerARNs = nil
	return util.GetHash(view)
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

// OnPodAdded allocates the NLB ports up-front and pre-injects the AWS Load
// Balancer Controller pod readiness gates, one per allocated listener port.
//
// Why allocate here (not lazily in OnPodUpdated): the readiness-gate condition
// type must equal the TargetGroupBinding name (<pod>-<port>), so the port must
// be known at pod-creation time. The allocation is pinned in podAllocate and
// reused by syncTargetGroupAndService in OnPodUpdated, so no double allocation.
//
// Why gates at all: without them the GameServer is marked Ready as soon as the
// Service/TargetGroupBinding exist — even when the NLB target never becomes
// healthy (e.g. an IP reused while still draining in another TargetGroup).
// The controller only flips this gate True once the target is actually healthy,
// so pod (hence GameServer) readiness reflects real reachability.
func (n *NlbPlugin) OnPodAdded(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	networkManager := utils.NewNetworkManager(pod, c)
	if networkManager == nil {
		return pod, nil
	}
	conf := parseLbConfig(networkManager.GetNetworkConfig())
	if conf == nil || len(conf.loadBalancerARNs) == 0 || len(conf.backends) == 0 {
		return pod, nil
	}

	podKey := pod.GetNamespace() + "/" + pod.GetName()
	allocatedPorts, exist := n.podAllocate[podKey]
	if !exist {
		allocatedPorts = n.allocate(conf.loadBalancerARNs, len(conf.backends), podKey, conf.allocatePolicy)
		if allocatedPorts == nil {
			return pod, cperrors.NewPluginErrorWithMessage(cperrors.ApiCallError,
				fmt.Sprintf("no NLB has %d enough available ports for %s", len(conf.backends), podKey))
		}
	}

	// One readiness gate per listener port; condition type == TargetGroupBinding name.
	existing := make(map[corev1.PodConditionType]bool, len(pod.Spec.ReadinessGates))
	for _, g := range pod.Spec.ReadinessGates {
		existing[g.ConditionType] = true
	}
	for _, port := range allocatedPorts.ports {
		condType := corev1.PodConditionType(ReadinessGatePrefix + fmt.Sprintf("%s-%d", pod.GetName(), port))
		if !existing[condType] {
			pod.Spec.ReadinessGates = append(pod.Spec.ReadinessGates, corev1.PodReadinessGate{
				ConditionType: condType,
			})
		}
	}
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
	if err := validateLbConfig(lbConfig); err != nil {
		return pod, cperrors.NewPluginErrorWithMessage(cperrors.ParameterError, err.Error())
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
			return pod, cperrors.ToPluginError(n.syncTargetGroupAndService(lbConfig, pod, c, ctx), cperrors.ApiCallError)
		}
		return pod, cperrors.NewPluginErrorWithMessage(cperrors.ApiCallError, err.Error())
	}

	// update svc
	// Use configHash (excludes loadBalancerARNs) so that adding/removing an ARN
	// in NlbARNs does NOT trigger a reconfigure of already-allocated pods —
	// avoiding the full-reset + port-collision deadlock (see configHash doc).
	if lbConfig.configHash() != svc.GetAnnotations()[NlbConfigHashKey] {
		networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkNotReady
		pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
		if err != nil {
			return pod, cperrors.NewPluginErrorWithMessage(cperrors.InternalError, err.Error())
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

	// Readiness gating: the Service/TargetGroupBinding existing is NOT enough to
	// declare the network ready — the NLB target may never become healthy (e.g.
	// an IP reused while still draining in another TargetGroup leaves the target
	// stuck/empty). The pod readiness gates injected in OnPodAdded are flipped
	// True by the AWS Load Balancer Controller only once the targets are healthy,
	// which is reflected in the aggregate PodReady condition. Until PodReady is
	// True, keep the GameServer NetworkNotReady so its state reflects real
	// reachability rather than just "resources created".
	//
	// IMPORTANT: refresh pod status from the API server before checking PodReady.
	// OnPodUpdated runs only inside the mutating admission webhook for the `pods`
	// resource. The kubelet writes the pod readiness condition through the
	// `pods/status` subresource, which does NOT trigger this webhook. So the
	// pod object the webhook decoded from the request body can carry a stale
	// Status (PodReady=False) even when the live pod is already PodReady=True.
	// Reading stale Status here pins GameServer NetworkState to NotReady forever
	// (no further `pods` UPDATE will retrigger the check). Fetch the latest pod
	// directly so the readiness gate decision uses authoritative state.
	freshPod := &corev1.Pod{}
	if err := c.Get(ctx, types.NamespacedName{Name: pod.GetName(), Namespace: pod.GetNamespace()}, freshPod); err == nil {
		pod.Status = freshPod.Status
	}
	_, readyCondition := util.GetPodConditionFromList(pod.Status.Conditions, corev1.PodReady)
	if readyCondition == nil || readyCondition.Status != corev1.ConditionTrue {
		// Not ready yet. If a readiness gate has been stuck False well past the
		// health-check window, it is a ghost registration that never self-heals;
		// delete + re-sync the affected TargetGroupBinding(s) so the target
		// registers cleanly. Scoped to this pod only.
		n.healStuckReadinessGates(ctx, c, pod, metav1.Now().Unix())
		networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkNotReady
		pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
		return pod, cperrors.ToPluginError(err, cperrors.InternalError)
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

func (n *NlbPlugin) allocate(lbARNs []string, num int, nsName string, policy string) *nlbPorts {
	n.mutex.Lock()
	defer n.mutex.Unlock()

	// Initialize cache for each lbARN if not already done
	for _, nlbARN := range lbARNs {
		n.initCache(nlbARN)
	}

	// Find lbARN with enough free ports according to the allocate policy
	selectedARN := n.findLbWithFreePorts(lbARNs, num, policy)
	if selectedARN == "" {
		return nil
	}

	// Allocate ports
	ports := n.allocatePorts(selectedARN, num)

	n.podAllocate[nsName] = &nlbPorts{arn: selectedARN, ports: ports}
	log.Infof("pod %s allocate nlb %s ports %v", nsName, selectedARN, ports)
	return &nlbPorts{arn: selectedARN, ports: ports}
}

// findLbWithFreePorts selects a NLB ARN that has at least num free ports.
//   - "default" (first-fit / 溢出): iterate ARNs in order, return the first one
//     with enough free ports — fills the earlier NLB before spilling to the next.
//   - "balanced": pick the NLB with the MOST free ports, spreading game servers
//     across NLBs (fault-domain isolation). Ported from the alibabacloud plugin.
func (n *NlbPlugin) findLbWithFreePorts(lbARNs []string, num int, policy string) string {
	if policy == allocatePolicyBalanced {
		bestARN := ""
		maxFree := 0
		for _, nlbARN := range lbARNs {
			freePorts := 0
			for i := n.minPort; i <= n.maxPort; i++ {
				if !n.cache[nlbARN][i] {
					freePorts++
				}
			}
			if freePorts > maxFree {
				maxFree = freePorts
				bestARN = nlbARN
			}
		}
		if maxFree >= num {
			return bestARN
		}
		return ""
	}

	// default: first-fit / 溢出
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
	allocatePolicy := allocatePolicyDefault
	for _, c := range conf {
		switch c.Name {
		case NlbARNsConfigName:
			for _, nlbARN := range strings.Split(c.Value, ",") {
				if nlbARN != "" {
					lbARNs = append(lbARNs, nlbARN)
				}
			}
		case AllocatePolicyConfigName:
			if c.Value == allocatePolicyBalanced {
				allocatePolicy = allocatePolicyBalanced
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
		allocatePolicy:   allocatePolicy,
		healthCheck:      &hc,
		vpcID:            vpcId,
		backends:         backends,
		isFixed:          isFixed,
		annotations:      annotations,
	}
}

// validateLbConfig rejects network configurations that AWS would reject (or that
// would otherwise stall silently) when the plugin creates the Service / target
// group / listener, surfacing a clear error early instead of failing deep in an
// AWS API call or leaving the GameServer stuck in NotReady. All limits follow
// the AWS ELBv2 API (see CreateTargetGroup / Health checks for NLB target
// groups). The NLB plugin always uses target type "ip".
func validateLbConfig(config *nlbConfig) error {
	if config == nil {
		return nil
	}

	// NlbARNs is required: without a load balancer ARN the plugin cannot
	// allocate a frontend port.
	if len(config.loadBalancerARNs) == 0 {
		return fmt.Errorf("%s is required: at least one NLB ARN must be provided", NlbARNsConfigName)
	}

	// PortProtocols is required and each entry must be a valid port/protocol.
	if len(config.backends) == 0 {
		return fmt.Errorf("%s is required: at least one port/protocol must be provided", PortProtocolsConfigName)
	}
	for _, b := range config.backends {
		if b.targetPort < 1 || b.targetPort > 65535 {
			return fmt.Errorf("%s port %d is invalid: must be in [1,65535]", PortProtocolsConfigName, b.targetPort)
		}
		switch b.protocol {
		case corev1.ProtocolTCP, corev1.ProtocolUDP, ProtocolTCPUDP:
		default:
			return fmt.Errorf("%s protocol %q is invalid: must be one of TCP, UDP, TCPUDP", PortProtocolsConfigName, b.protocol)
		}
	}

	hc := config.healthCheck
	if hc == nil {
		return nil
	}

	// Target type "ip" requires health checks to be always enabled and they
	// cannot be disabled. healthCheckEnabled=false makes ack-elbv2 fail to sync
	// the target group ("Health check enabled must be true with groups with
	// target type ip"), silently stalling listener creation.
	if hc.healthCheckEnabled != nil && !*hc.healthCheckEnabled {
		return fmt.Errorf("%s healthCheckEnabled=false is not allowed: the plugin creates target groups with target type \"ip\", for which AWS requires health checks to be always enabled; remove healthCheckEnabled or set it to true", NlbHealthCheckConfigName)
	}

	// For NLB, only TCP/HTTP/HTTPS health-check protocols are supported;
	// UDP/TCP_UDP/TLS/GENEVE are not valid health-check protocols.
	if hc.healthCheckProtocol != nil {
		p := strings.ToUpper(*hc.healthCheckProtocol)
		switch p {
		case "TCP", "HTTP", "HTTPS":
		default:
			return fmt.Errorf("%s healthCheckProtocol %q is invalid: NLB health checks support only TCP, HTTP or HTTPS", NlbHealthCheckConfigName, *hc.healthCheckProtocol)
		}
	}
	if hc.healthCheckIntervalSeconds != nil && (*hc.healthCheckIntervalSeconds < 5 || *hc.healthCheckIntervalSeconds > 300) {
		return fmt.Errorf("%s healthCheckIntervalSeconds %d is invalid: must be in [5,300]", NlbHealthCheckConfigName, *hc.healthCheckIntervalSeconds)
	}
	if hc.healthCheckTimeoutSeconds != nil && (*hc.healthCheckTimeoutSeconds < 2 || *hc.healthCheckTimeoutSeconds > 120) {
		return fmt.Errorf("%s healthCheckTimeoutSeconds %d is invalid: must be in [2,120]", NlbHealthCheckConfigName, *hc.healthCheckTimeoutSeconds)
	}
	if hc.healthyThresholdCount != nil && (*hc.healthyThresholdCount < 2 || *hc.healthyThresholdCount > 10) {
		return fmt.Errorf("%s healthyThresholdCount %d is invalid: must be in [2,10]", NlbHealthCheckConfigName, *hc.healthyThresholdCount)
	}
	if hc.unhealthyThresholdCount != nil && (*hc.unhealthyThresholdCount < 2 || *hc.unhealthyThresholdCount > 10) {
		return fmt.Errorf("%s unhealthyThresholdCount %d is invalid: must be in [2,10]", NlbHealthCheckConfigName, *hc.unhealthyThresholdCount)
	}
	return nil
}

// consSvcPorts builds the ServicePort list for the backing ClusterIP Service.
//
// Kubernetes requires every ServicePort in a multi-port Service to have a
// unique Name. When the same target port is exposed for more than one protocol
// (e.g. PortProtocols "8601/TCP,8601/UDP"), using the bare port number as the
// Name produces duplicate names and an invalid Service that the API server
// rejects. In that case we disambiguate by appending the lowercased protocol
// (e.g. "8601-tcp" / "8601-udp"). Port numbers that appear only once keep the
// legacy numeric-only name to stay backward compatible.
func consSvcPorts(backends []*backend, ports []int32) []corev1.ServicePort {
	// portCount counts how many ServicePorts will land on each target port
	// number so we know whether to add a protocol suffix to the
	// ServicePort name. TCPUDP entries contribute two (TCP and UDP).
	portCount := make(map[int]int)
	for i := 0; i < len(backends); i++ {
		if backends[i].protocol == ProtocolTCPUDP {
			portCount[backends[i].targetPort] += 2
		} else {
			portCount[backends[i].targetPort]++
		}
	}

	svcPorts := make([]corev1.ServicePort, 0)
	for i := 0; i < len(backends); i++ {
		baseName := strconv.Itoa(backends[i].targetPort)
		if backends[i].protocol == ProtocolTCPUDP {
			// TCPUDP expands to TCP+UDP sharing the same Port. Always add
			// the protocol suffix because two ServicePorts on the same
			// Port number must have unique Names.
			svcPorts = append(svcPorts, corev1.ServicePort{
				Name:       baseName + "-tcp",
				Port:       ports[i],
				Protocol:   corev1.ProtocolTCP,
				TargetPort: intstr.FromInt(backends[i].targetPort),
			})
			svcPorts = append(svcPorts, corev1.ServicePort{
				Name:       baseName + "-udp",
				Port:       ports[i],
				Protocol:   corev1.ProtocolUDP,
				TargetPort: intstr.FromInt(backends[i].targetPort),
			})
			continue
		}
		name := baseName
		if portCount[backends[i].targetPort] > 1 {
			name = name + "-" + strings.ToLower(string(backends[i].protocol))
		}
		svcPorts = append(svcPorts, corev1.ServicePort{
			Name:       name,
			Port:       ports[i],
			Protocol:   backends[i].protocol,
			TargetPort: intstr.FromInt(backends[i].targetPort),
		})
	}
	return svcPorts
}

// awsTargetGroupProtocol maps a backend protocol (which may be the
// synthetic ProtocolTCPUDP) to the string accepted by the AWS ELBv2 API
// for TargetGroups and Listeners. NLB Listener Protocol is inherited
// from the TargetGroup, so this single mapping is sufficient.
func awsTargetGroupProtocol(p corev1.Protocol) string {
	if p == ProtocolTCPUDP {
		return awsProtocolTCPUDP
	}
	return string(p)
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
		allocatedPorts = n.allocate(config.loadBalancerARNs, len(config.backends), podKey, config.allocatePolicy)
		if allocatedPorts == nil {
			return fmt.Errorf("no NLB has %d enough available ports for %s", len(config.backends), podKey)
		}
	}
	lbARN = allocatedPorts.arn
	ports = allocatedPorts.ports

	ownerReference := getOwnerReference(client, ctx, pod, config.isFixed)
	for i := range ports {
		targetGroupName := fmt.Sprintf("%s-%d", pod.GetName(), ports[i])
		protocol := awsTargetGroupProtocol(config.backends[i].protocol)
		targetPort := int64(config.backends[i].targetPort)
		var targetTypeIP = string(ackv1alpha1.TargetTypeEnum_ip)
		tg := &ackv1alpha1.TargetGroup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      targetGroupName,
				Namespace: pod.GetNamespace(),
			},
		}
		_, err := controllerutil.CreateOrUpdate(ctx, client, tg, func() error {
			tg.OwnerReferences = ownerReference
			tg.Labels = map[string]string{
				ResourceTagKey:           ResourceTagValue,
				SvcSelectorKey:           pod.GetName(),
				AWSTargetGroupSyncStatus: "false",
			}
			tg.Annotations = map[string]string{
				NlbARNAnnoKey:  lbARN,
				NlbPortAnnoKey: fmt.Sprintf("%d", ports[i]),
			}
			tg.Spec = ackv1alpha1.TargetGroupSpec{
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
			}
			return nil
		})
		if err != nil {
			return err
		}
	}

	svcPorts := consSvcPorts(config.backends, ports)
	annotations := map[string]string{
		NlbARNAnnoKey:    lbARN,
		NlbConfigHashKey: config.configHash(),
	}
	for key, value := range config.annotations {
		annotations[key] = value
	}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pod.GetName(),
			Namespace: pod.GetNamespace(),
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, client, svc, func() error {
		svc.Annotations = annotations
		svc.OwnerReferences = ownerReference
		svc.Labels = map[string]string{
			ResourceTagKey: ResourceTagValue,
			SvcSelectorKey: pod.GetName(),
		}
		svc.Spec.Type = corev1.ServiceTypeClusterIP
		svc.Spec.Selector = map[string]string{
			SvcSelectorKey: pod.GetName(),
		}
		svc.Spec.Ports = svcPorts
		return nil
	})
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
	listener := &ackv1alpha1.Listener{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tg.GetName(),
			Namespace: tg.GetNamespace(),
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, client, listener, func() error {
		listener.OwnerReferences = tg.GetOwnerReferences()
		listener.Labels = map[string]string{
			ResourceTagKey: ResourceTagValue,
			SvcSelectorKey: podName,
		}
		listener.Spec = ackv1alpha1.ListenerSpec{
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
		}
		return nil
	})
	if err != nil {
		return err
	}

	var targetTypeIP = elbv2api.TargetTypeIP
	tgb := &elbv2api.TargetGroupBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tg.GetName(),
			Namespace: tg.GetNamespace(),
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, client, tgb, func() error {
		tgb.OwnerReferences = tg.GetOwnerReferences()
		tgb.Labels = map[string]string{
			ResourceTagKey: ResourceTagValue,
			SvcSelectorKey: podName,
		}
		tgb.Spec = elbv2api.TargetGroupBindingSpec{
			TargetGroupARN: *targetGroupARN,
			TargetType:     &targetTypeIP,
			ServiceRef: elbv2api.ServiceReference{
				Name: podName,
				Port: intstr.FromInt(int(port)),
			},
		}
		return nil
	})
	if err != nil {
		return err
	}

	return nil
}

// healStuckReadinessGates self-heals "ghost registration": pod readiness gates
// that have been False far longer than a healthy target should take. Such a
// target is stuck draining/empty in AWS and the controller will never retry on
// its own because the TargetGroupBinding spec hash is unchanged.
//
// The fix (validated): delete the stuck TargetGroupBinding and re-trigger a
// fresh sync (reset the TargetGroup sync label to "false", which makes
// handleTargetGroupEvent recreate the Listener + a brand-new TargetGroupBinding).
// A brand-new TGB is reconciled from scratch — by which point the reused IP's
// prior draining has finished — so the target registers cleanly.
//
// Scoped to a single pod's own TGBs (named <pod>-<port>), so it cannot disturb
// other GameServers. Returns the number of TGBs healed.
func (n *NlbPlugin) healStuckReadinessGates(ctx context.Context, c client.Client, pod *corev1.Pod, nowUnix int64) int {
	healed := 0
	for _, cond := range pod.Status.Conditions {
		if !strings.HasPrefix(string(cond.Type), ReadinessGatePrefix) {
			continue
		}
		if cond.Status == corev1.ConditionTrue {
			continue
		}
		// Stuck only if it has been False long enough to rule out normal
		// initial->healthy registration.
		if cond.LastTransitionTime.IsZero() ||
			nowUnix-cond.LastTransitionTime.Unix() < ghostRegistrationStuckSeconds {
			continue
		}
		tgbName := strings.TrimPrefix(string(cond.Type), ReadinessGatePrefix)

		// Delete the stuck TargetGroupBinding.
		tgb := &elbv2api.TargetGroupBinding{}
		if err := c.Get(ctx, types.NamespacedName{Name: tgbName, Namespace: pod.GetNamespace()}, tgb); err != nil {
			if !errors.IsNotFound(err) {
				log.Warningf("[%s] heal: get TGB %s/%s error %v", NlbNetwork, pod.GetNamespace(), tgbName, err)
			}
			continue
		}
		if err := c.Delete(ctx, tgb); err != nil && !errors.IsNotFound(err) {
			log.Warningf("[%s] heal: delete TGB %s/%s error %v", NlbNetwork, pod.GetNamespace(), tgbName, err)
			continue
		}

		// Re-trigger a fresh sync via the matching TargetGroup (same name).
		tg := &ackv1alpha1.TargetGroup{}
		if err := c.Get(ctx, types.NamespacedName{Name: tgbName, Namespace: pod.GetNamespace()}, tg); err != nil {
			log.Warningf("[%s] heal: get TG %s/%s error %v", NlbNetwork, pod.GetNamespace(), tgbName, err)
			continue
		}
		patch := client.RawPatch(types.MergePatchType,
			[]byte(fmt.Sprintf(`{"metadata":{"labels":{"%s":"false"}}}`, AWSTargetGroupSyncStatus)))
		if err := c.Patch(ctx, tg, patch); err != nil {
			log.Warningf("[%s] heal: patch TG %s/%s sync=false error %v", NlbNetwork, pod.GetNamespace(), tgbName, err)
			continue
		}
		log.Infof("[%s] heal: stuck readiness gate %s (False %ds) -> deleted TGB + re-synced",
			NlbNetwork, cond.Type, nowUnix-cond.LastTransitionTime.Unix())
		healed++
	}
	return healed
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
