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

package kubernetes

import (
	"context"
	"strconv"
	"strings"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/cloudprovider"
	cperrors "github.com/openkruise/kruise-game/cloudprovider/errors"
	"github.com/openkruise/kruise-game/cloudprovider/utils"
	"github.com/openkruise/kruise-game/pkg/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// GcpNLBNetwork is the network type name for the GCP passthrough Network Load Balancer plugin.
	GcpNLBNetwork = "GCP-NLB"

	// GcpNLBLoadBalancerClass is the GKE L4 regional external passthrough NLB load balancer class.
	// Using this class enables backend-service based NLB which supports mixed TCP/UDP protocols
	// on the same port.
	GcpNLBLoadBalancerClass = "networking.gke.io/l4-regional-external"

	// GcpNLBExternalTrafficPolicyConfigName lets users override the externalTrafficPolicy.
	// Defaults to Local so the client source IP is preserved and traffic is sent only to the
	// node that hosts the GameServer pod.
	GcpNLBExternalTrafficPolicyConfigName = "ExternalTrafficPolicy"

	// GcpNLBLoadBalancerIPsConfigName optionally pins one reserved static IP to the
	// Service. When multiple per-pod Services share one IP this enables the
	// "single NLB IP, multiple ports" pattern.
	GcpNLBLoadBalancerIPsConfigName = "LoadBalancerIP"

	// GcpNLBMinPortConfigName, when set, enables "port offset" mode: every pod shares
	// the same LoadBalancerIP but receives a distinct external port computed from its
	// ordinal index, while the container targetPort stays fixed. For example with
	// MinPort=9000 and a single container port:
	//   NLB_IP:9000 -> pod-0:<containerPort>
	//   NLB_IP:9001 -> pod-1:<containerPort>
	// Each external port carries every protocol configured for that container port
	// (so TCP+UDP on the same external port is supported).
	// When MinPort is unset the plugin keeps the legacy behaviour: the external port
	// equals the container port (one dedicated IP per pod).
	GcpNLBMinPortConfigName = "MinPort"
)

// GcpNLBPlugin implements the OKG network Plugin interface using a GKE managed
// type=LoadBalancer Service (backend-service based passthrough NLB).
//
// Design (Option A): the plugin does NOT call the GCP Compute API directly.
// Instead it creates a per-pod Service whose OwnerReference points to the pod
// (or the GameServerSet when Fixed). The GKE loadbalancer-controller turns the
// Service into a GCP forwarding rule + NEG and writes back the external IP.
// When the pod is recycled, Kubernetes garbage-collects the owned Service via the
// OwnerReference, and the GKE controller then deletes the underlying forwarding
// rule automatically. This gives us "create forwarding rule bound to pod" plus
// "delete forwarding rule when pod is recycled" with no extra credentials.
type GcpNLBPlugin struct {
}

func (g *GcpNLBPlugin) Name() string {
	return GcpNLBNetwork
}

func (g *GcpNLBPlugin) Alias() string {
	return ""
}

func (g *GcpNLBPlugin) Init(client client.Client, options cloudprovider.CloudProviderOptions, ctx context.Context) error {
	return nil
}

func (g *GcpNLBPlugin) OnPodAdded(client client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	return pod, nil
}

func (g *GcpNLBPlugin) OnPodUpdated(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	networkManager := utils.NewNetworkManager(pod, c)

	networkStatus, _ := networkManager.GetNetworkStatus()
	networkConfig := networkManager.GetNetworkConfig()
	conf, err := parseGcpNLBConfig(networkConfig)
	if err != nil {
		return pod, cperrors.NewPluginError(cperrors.ParameterError, err.Error())
	}
	// Port-offset mode needs the pod ordinal to compute its external port block.
	conf.podIndex = util.GetIndexFromGsName(pod.GetName())

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
			// Service does not exist yet, create it. OwnerReference handles cascade delete.
			return pod, cperrors.ToPluginError(c.Create(ctx, consGcpNLBSvc(conf, pod, c, ctx)), cperrors.ApiCallError)
		}
		return pod, cperrors.NewPluginError(cperrors.ApiCallError, err.Error())
	}

	// config changed, update svc
	if util.GetHash(conf) != svc.GetAnnotations()[ServiceHashKey] {
		networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkNotReady
		pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
		if err != nil {
			return pod, cperrors.NewPluginError(cperrors.InternalError, err.Error())
		}
		newSvc := consGcpNLBSvc(conf, pod, c, ctx)
		newSvc.ResourceVersion = svc.ResourceVersion
		newSvc.Spec.ClusterIP = svc.Spec.ClusterIP
		return pod, cperrors.ToPluginError(c.Update(ctx, newSvc), cperrors.ApiCallError)
	}

	// disable network: drop the selector so traffic stops reaching the pod, keep the IP.
	if networkManager.GetNetworkDisabled() && svc.Spec.Selector[SvcSelectorKey] == pod.GetName() {
		newSelector := svc.Spec.Selector
		newSelector[SvcSelectorDisabledKey] = pod.GetName()
		delete(newSelector, SvcSelectorKey)
		svc.Spec.Selector = newSelector
		return pod, cperrors.ToPluginError(c.Update(ctx, svc), cperrors.ApiCallError)
	}

	// enable network
	if !networkManager.GetNetworkDisabled() && svc.Spec.Selector[SvcSelectorDisabledKey] == pod.GetName() {
		newSelector := svc.Spec.Selector
		newSelector[SvcSelectorKey] = pod.GetName()
		delete(newSelector, SvcSelectorDisabledKey)
		svc.Spec.Selector = newSelector
		return pod, cperrors.ToPluginError(c.Update(ctx, svc), cperrors.ApiCallError)
	}

	// network not ready until the GKE controller assigns an external IP
	if len(svc.Status.LoadBalancer.Ingress) == 0 || svc.Status.LoadBalancer.Ingress[0].IP == "" {
		networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkNotReady
		pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
		return pod, cperrors.ToPluginError(err, cperrors.InternalError)
	}

	if pod.Status.PodIP == "" {
		networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkNotReady
		pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
		return pod, cperrors.ToPluginError(err, cperrors.InternalError)
	}

	// network ready: build internal/external addresses from the Service.
	lbIP := svc.Status.LoadBalancer.Ingress[0].IP
	internalAddresses := make([]gamekruiseiov1alpha1.NetworkAddress, 0)
	externalAddresses := make([]gamekruiseiov1alpha1.NetworkAddress, 0)
	for _, port := range svc.Spec.Ports {
		instrIPort := port.TargetPort
		instrEPort := intstr.FromInt(int(port.Port))
		internalAddresses = append(internalAddresses, gamekruiseiov1alpha1.NetworkAddress{
			IP: pod.Status.PodIP,
			Ports: []gamekruiseiov1alpha1.NetworkPort{
				{
					Name:     instrIPort.String(),
					Port:     &instrIPort,
					Protocol: port.Protocol,
				},
			},
		})
		externalAddresses = append(externalAddresses, gamekruiseiov1alpha1.NetworkAddress{
			IP: lbIP,
			Ports: []gamekruiseiov1alpha1.NetworkPort{
				{
					Name:     instrIPort.String(),
					Port:     &instrEPort,
					Protocol: port.Protocol,
				},
			},
		})
	}
	networkStatus.InternalAddresses = internalAddresses
	networkStatus.ExternalAddresses = externalAddresses
	networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkReady
	pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
	return pod, cperrors.ToPluginError(err, cperrors.InternalError)
}

// OnPodDeleted cleans up the Service (and therefore the GCP forwarding rule).
//
//   - Fixed=false: the Service is owned by the Pod, so Kubernetes garbage-collects
//     it automatically via the OwnerReference whenever the pod is deleted. This gives
//     immediate cleanup on scale-down, at the cost of a LB teardown+rebuild on update.
//   - Fixed=true: the Service is owned by the GameServerSet so it survives pod
//     recreation (smooth, zero-downtime updates) and also persists across scale-down
//     so a replica that comes back keeps the same IP:port (stable address semantics,
//     consistent with other OKG cloud plugins). The Service is only removed once the
//     whole GameServerSet is deleted, which we handle explicitly here because the
//     OwnerReference cascade does not fire until then.
func (g *GcpNLBPlugin) OnPodDeleted(c client.Client, pod *corev1.Pod, ctx context.Context) cperrors.PluginError {
	networkManager := utils.NewNetworkManager(pod, c)
	conf, err := parseGcpNLBConfig(networkManager.GetNetworkConfig())
	if err != nil {
		return cperrors.NewPluginError(cperrors.ParameterError, err.Error())
	}

	// Non-fixed: the Service is owned by the GameServer CR. On scale-down the
	// GameServer is deleted and the Service is cascade garbage-collected; on update
	// the GameServer persists so the Service survives. Nothing to do here.
	if !conf.isFixed {
		return nil
	}

	// Fixed: keep the Service while the GameServerSet still exists (update or
	// scale-down where the index may return). Only delete once the GSS is gone.
	gss, err := util.GetGameServerSetOfPod(pod, c, ctx)
	if err == nil && gss.GetDeletionTimestamp() == nil {
		return nil
	}
	if err != nil && !errors.IsNotFound(err) {
		return cperrors.ToPluginError(err, cperrors.ApiCallError)
	}

	// GameServerSet gone: delete the Service so GKE removes the forwarding rule.
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pod.GetName(),
			Namespace: pod.GetNamespace(),
		},
	}
	err = c.Delete(ctx, svc)
	if err != nil && !errors.IsNotFound(err) {
		return cperrors.ToPluginError(err, cperrors.ApiCallError)
	}
	return nil
}

func init() {
	kubernetesProvider.registerPlugin(&GcpNLBPlugin{})
}

type gcpNLBConfig struct {
	ports                 []int
	protocols             []corev1.Protocol
	isFixed               bool
	externalTrafficPolicy corev1.ServiceExternalTrafficPolicyType
	loadBalancerIP        string
	// minPort > 0 enables port-offset mode (shared IP, per-pod external port).
	minPort int
	// podIndex is the ordinal of the pod, used to compute the external port offset.
	podIndex int
}

func parseGcpNLBConfig(conf []gamekruiseiov1alpha1.NetworkConfParams) (*gcpNLBConfig, error) {
	var ports []int
	var protocols []corev1.Protocol
	isFixed := false
	etp := corev1.ServiceExternalTrafficPolicyTypeLocal
	loadBalancerIP := ""
	minPort := 0

	for _, c := range conf {
		switch c.Name {
		case PortProtocolsConfigName:
			ports, protocols = parsePortProtocols(c.Value)
		case FixedKey:
			v, err := strconv.ParseBool(c.Value)
			if err != nil {
				return nil, err
			}
			isFixed = v
		case GcpNLBExternalTrafficPolicyConfigName:
			if strings.EqualFold(c.Value, string(corev1.ServiceExternalTrafficPolicyTypeCluster)) {
				etp = corev1.ServiceExternalTrafficPolicyTypeCluster
			}
		case GcpNLBLoadBalancerIPsConfigName:
			loadBalancerIP = strings.TrimSpace(c.Value)
		case GcpNLBMinPortConfigName:
			v, err := strconv.Atoi(strings.TrimSpace(c.Value))
			if err != nil {
				return nil, err
			}
			minPort = v
		}
	}
	return &gcpNLBConfig{
		ports:                 ports,
		protocols:             protocols,
		isFixed:               isFixed,
		externalTrafficPolicy: etp,
		loadBalancerIP:        loadBalancerIP,
		minPort:               minPort,
	}, nil
}

// uniquePorts returns the distinct container port numbers preserving first-seen order.
// TCP+UDP on the same container port collapse to a single entry so they share one
// external port.
func uniquePorts(ports []int) []int {
	seen := make(map[int]bool)
	out := make([]int, 0, len(ports))
	for _, p := range ports {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

func consGcpNLBSvc(conf *gcpNLBConfig, pod *corev1.Pod, c client.Client, ctx context.Context) *corev1.Service {
	// Detect duplicate port numbers (e.g. the same container port exposed for both
	// TCP and UDP). Kubernetes requires every ServicePort in a multi-port Service to
	// have a unique Name, so when a port number repeats we disambiguate the Name by
	// appending the protocol.
	portCount := make(map[int]int)
	for i := 0; i < len(conf.ports); i++ {
		portCount[conf.ports[i]]++
	}

	// In port-offset mode every pod shares one LoadBalancerIP, so each pod must get
	// a distinct external port. We allocate a contiguous block per pod based on its
	// ordinal: blockBase = MinPort + podIndex * (number of distinct container ports).
	// The k-th distinct container port maps to external port blockBase + k, and every
	// protocol on that container port reuses the same external port (TCP+UDP share it).
	externalPortOf := func(containerPort int) int32 { return int32(containerPort) }
	if conf.minPort > 0 {
		distinct := uniquePorts(conf.ports)
		blockBase := conf.minPort + conf.podIndex*len(distinct)
		extPortByContainerPort := make(map[int]int32, len(distinct))
		for k, cp := range distinct {
			extPortByContainerPort[cp] = int32(blockBase + k)
		}
		externalPortOf = func(containerPort int) int32 { return extPortByContainerPort[containerPort] }
	}

	svcPorts := make([]corev1.ServicePort, 0)
	for i := 0; i < len(conf.ports); i++ {
		extPort := externalPortOf(conf.ports[i])
		name := strconv.Itoa(int(extPort))
		if portCount[conf.ports[i]] > 1 {
			name = name + "-" + strings.ToLower(string(conf.protocols[i]))
		}
		svcPorts = append(svcPorts, corev1.ServicePort{
			Name:       name,
			Port:       extPort,                          // external port on the NLB IP
			Protocol:   conf.protocols[i],
			TargetPort: intstr.FromInt(conf.ports[i]),    // fixed container port
		})
	}

	// OwnerReference selection drives the Service lifecycle:
	//   - Fixed=false (default): owned by the GameServer CR. The GameServer persists
	//     across pod recreation (OnDelete update) but is deleted on scale-down, so the
	//     Service (and its forwarding rule) survives updates yet is auto-cleaned on
	//     scale-down — exactly "keep on update, delete on scale-down".
	//   - Fixed=true: owned by the GameServerSet, so the address persists even across
	//     scale-down and is only removed when the whole GameServerSet is deleted.
	var ownerRefs []metav1.OwnerReference
	if conf.isFixed {
		ownerRefs = consOwnerReference(c, ctx, pod, true)
	} else {
		ownerRefs = consGameServerOwnerReference(c, ctx, pod)
	}

	loadBalancerClass := GcpNLBLoadBalancerClass
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pod.GetName(),
			Namespace: pod.GetNamespace(),
			Annotations: map[string]string{
				ServiceHashKey: util.GetHash(conf),
			},
			OwnerReferences: ownerRefs,
		},
		Spec: corev1.ServiceSpec{
			Type:                  corev1.ServiceTypeLoadBalancer,
			LoadBalancerClass:     &loadBalancerClass,
			ExternalTrafficPolicy: conf.externalTrafficPolicy,
			Selector: map[string]string{
				SvcSelectorKey: pod.GetName(),
			},
			Ports: svcPorts,
		},
	}
	if conf.loadBalancerIP != "" {
		svc.Spec.LoadBalancerIP = conf.loadBalancerIP
	}
	return svc
}

// consGameServerOwnerReference returns an OwnerReference pointing at the GameServer
// CR with the same name as the pod. The GameServer is the stable identity of a
// replica: it survives pod recreation during an OnDelete update, but is deleted on
// scale-down. Owning the Service by it therefore yields "keep on update, delete on
// scale-down" purely through Kubernetes garbage collection.
//
// If the client is nil (unit tests) or the GameServer cannot be fetched, it falls
// back to owning the Service by the pod.
func consGameServerOwnerReference(c client.Client, ctx context.Context, pod *corev1.Pod) []metav1.OwnerReference {
	podOwner := []metav1.OwnerReference{
		{
			APIVersion:         pod.APIVersion,
			Kind:               pod.Kind,
			Name:               pod.GetName(),
			UID:                pod.GetUID(),
			Controller:         ptr.To[bool](true),
			BlockOwnerDeletion: ptr.To[bool](true),
		},
	}
	if c == nil {
		return podOwner
	}
	gs := &gamekruiseiov1alpha1.GameServer{}
	if err := c.Get(ctx, types.NamespacedName{Name: pod.GetName(), Namespace: pod.GetNamespace()}, gs); err != nil {
		return podOwner
	}
	return []metav1.OwnerReference{
		{
			APIVersion:         gamekruiseiov1alpha1.GroupVersion.String(),
			Kind:               "GameServer",
			Name:               gs.GetName(),
			UID:                gs.GetUID(),
			Controller:         ptr.To[bool](true),
			BlockOwnerDeletion: ptr.To[bool](true),
		},
	}
}
