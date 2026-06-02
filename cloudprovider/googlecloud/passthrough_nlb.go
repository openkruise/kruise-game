/*
Copyright 2025 The Kruise Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package googlecloud

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	log "k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/cloudprovider"
	cperrors "github.com/openkruise/kruise-game/cloudprovider/errors"
	gcpv1beta1 "github.com/openkruise/kruise-game/cloudprovider/googlecloud/apis/compute/v1beta1"
	provideroptions "github.com/openkruise/kruise-game/cloudprovider/options"
	"github.com/openkruise/kruise-game/cloudprovider/utils"
	"github.com/openkruise/kruise-game/pkg/util"
)

const (
	// PassthroughNlbNetwork is the value users put in
	// spec.network.networkType to select this plugin.
	PassthroughNlbNetwork = "GoogleCloud-PassthroughNLB"
	// AliasPassthroughNlb is the cross-cloud alias.
	AliasPassthroughNlb = "NLB-Network"

	// passthroughSuffix tags resources owned by this plugin in K8s metadata.
	passthroughSuffix = "gcp-pnlb"

	// maxPassthroughPorts mirrors the GCP Passthrough NLB forwarding-rule
	// per-rule port cap. We reject configs above this limit at parse time.
	maxPassthroughPorts = 5
)

// PassthroughNlbPlugin provisions a regional External or Internal Passthrough
// Network Load Balancer per Pod. It uses the GKE in-cluster LB controller for
// the FR/BES/HC stack (via Service of type=LoadBalancer + cloud.google.com/l4-rbs
// annotation) and reconciles a KCC ComputeAddress separately for the static VIP.
type PassthroughNlbPlugin struct {
	projectID             string
	defaultRegion         string
	defaultNetwork        string
	defaultSubnetwork     string
	defaultNetworkTier    string
	retainOnDeleteDefault bool
	// apiClient is a non-caching client used for KCC CR operations. The
	// caching client passed into On* would otherwise block waiting for
	// informers that are never started for KCC types.
	apiClient client.Client
	mutex     sync.RWMutex
}

// Name implements cloudprovider.Plugin.
func (p *PassthroughNlbPlugin) Name() string { return PassthroughNlbNetwork }

// Alias implements cloudprovider.Plugin.
func (p *PassthroughNlbPlugin) Alias() string { return AliasPassthroughNlb }

// Init implements cloudprovider.Plugin. Refuses to start when KCC CRDs are
// missing — the returned error is logged by provider_manager.Init and the
// plugin is then absent from the registry.
func (p *PassthroughNlbPlugin) Init(c client.Client, opts cloudprovider.CloudProviderOptions, ctx context.Context) error {
	gcpOpts, ok := opts.(provideroptions.GoogleCloudOptions)
	if !ok {
		return fmt.Errorf("googlecloud passthrough: expected GoogleCloudOptions, got %T", opts)
	}
	if !gcpOpts.PassthroughNLB.Enable {
		log.Infof("[%s] plugin disabled via PassthroughNLB.Enable=false", PassthroughNlbNetwork)
		return nil
	}

	p.mutex.Lock()
	p.projectID = gcpOpts.ProjectID
	p.defaultRegion = gcpOpts.DefaultRegion
	p.defaultNetwork = gcpOpts.DefaultNetwork
	p.defaultSubnetwork = gcpOpts.DefaultSubnetwork
	p.defaultNetworkTier = gcpOpts.PassthroughNLB.NetworkTier
	if p.defaultNetworkTier == "" {
		p.defaultNetworkTier = "PREMIUM"
	}
	p.retainOnDeleteDefault = gcpOpts.PassthroughNLB.RetainOnDeleteDefault
	p.mutex.Unlock()

	cfg := ctrl.GetConfigOrDie()
	if err := VerifyKCCInstalled(cfg); err != nil {
		// Refuse-to-start: the error message embeds the gcloud command.
		return err
	}
	apiClient, err := client.New(cfg, client.Options{Scheme: c.Scheme(), Mapper: c.RESTMapper()})
	if err != nil {
		return fmt.Errorf("googlecloud passthrough: build non-caching client: %w", err)
	}
	p.mutex.Lock()
	p.apiClient = apiClient
	p.mutex.Unlock()
	log.Infof("[%s] initialized (projectID=%s region=%s networkTier=%s)",
		PassthroughNlbNetwork, p.projectID, p.defaultRegion, p.defaultNetworkTier)
	return nil
}

// passthroughConfig is the parsed NetworkConfParams for this plugin.
type passthroughConfig struct {
	ProjectID         string
	Region            string
	Scheme            string // External | Internal
	Network           string
	Subnetwork        string
	AllowGlobalAccess bool
	NetworkTier       string
	TargetPorts       []int
	Protocols         []corev1.Protocol
	Annotations       map[string]string
	RetainOnDelete    bool
}

func (p *PassthroughNlbPlugin) parseConfig(conf []gamekruiseiov1alpha1.NetworkConfParams) (*passthroughConfig, error) {
	p.mutex.RLock()
	out := &passthroughConfig{
		ProjectID:      p.projectID,
		Region:         p.defaultRegion,
		Scheme:         SchemeExternal,
		Network:        p.defaultNetwork,
		Subnetwork:     p.defaultSubnetwork,
		NetworkTier:    p.defaultNetworkTier,
		RetainOnDelete: p.retainOnDeleteDefault,
		Annotations:    map[string]string{},
	}
	p.mutex.RUnlock()

	for _, c := range conf {
		switch c.Name {
		case ConfProjectID:
			out.ProjectID = strings.TrimSpace(c.Value)
		case ConfRegion:
			out.Region = strings.TrimSpace(c.Value)
		case ConfScheme:
			v := strings.TrimSpace(c.Value)
			if v != SchemeExternal && v != SchemeInternal {
				return nil, fmt.Errorf("invalid Scheme %q (want External or Internal)", c.Value)
			}
			out.Scheme = v
		case ConfNetwork:
			out.Network = strings.TrimSpace(c.Value)
		case ConfSubnetwork:
			out.Subnetwork = strings.TrimSpace(c.Value)
		case ConfAllowGlobalAccess:
			v, err := strconv.ParseBool(strings.TrimSpace(c.Value))
			if err != nil {
				return nil, fmt.Errorf("invalid AllowGlobalAccess: %w", err)
			}
			out.AllowGlobalAccess = v
		case ConfNetworkTier:
			v := strings.TrimSpace(c.Value)
			if v != "" && v != "PREMIUM" && v != "STANDARD" {
				return nil, fmt.Errorf("invalid NetworkTier %q (want PREMIUM or STANDARD)", c.Value)
			}
			if v != "" {
				out.NetworkTier = v
			}
		case ConfPortProtocols:
			for _, pp := range strings.Split(c.Value, ",") {
				pp = strings.TrimSpace(pp)
				if pp == "" {
					continue
				}
				parts := strings.Split(pp, "/")
				port, err := strconv.Atoi(strings.TrimSpace(parts[0]))
				if err != nil {
					return nil, fmt.Errorf("invalid PortProtocols port %q: %w", parts[0], err)
				}
				if port < 1 || port > 65535 {
					return nil, fmt.Errorf("invalid PortProtocols port %d (1-65535)", port)
				}
				proto := corev1.ProtocolTCP
				if len(parts) > 1 {
					proto = corev1.Protocol(strings.ToUpper(strings.TrimSpace(parts[1])))
					if proto != corev1.ProtocolTCP && proto != corev1.ProtocolUDP {
						return nil, fmt.Errorf("invalid PortProtocols protocol %q (want TCP or UDP)", parts[1])
					}
				}
				out.TargetPorts = append(out.TargetPorts, port)
				out.Protocols = append(out.Protocols, proto)
			}
		case ConfAnnotations:
			for _, kv := range strings.Split(c.Value, ",") {
				kv = strings.TrimSpace(kv)
				if kv == "" {
					continue
				}
				eq := strings.Index(kv, "=")
				if eq <= 0 || eq == len(kv)-1 {
					return nil, fmt.Errorf("invalid Annotations entry %q (want key=value)", kv)
				}
				out.Annotations[kv[:eq]] = kv[eq+1:]
			}
		case ConfRetainOnDelete:
			v, err := strconv.ParseBool(strings.TrimSpace(c.Value))
			if err != nil {
				return nil, fmt.Errorf("invalid RetainOnDelete: %w", err)
			}
			out.RetainOnDelete = v
		}
	}

	if len(out.TargetPorts) == 0 {
		return nil, fmt.Errorf("PortProtocols is required (e.g. PortProtocols=7777/UDP,7778/TCP)")
	}
	if len(out.TargetPorts) > maxPassthroughPorts {
		return nil, fmt.Errorf("PortProtocols has %d entries, GCP Passthrough NLB allows at most %d per forwarding rule",
			len(out.TargetPorts), maxPassthroughPorts)
	}
	if out.Region == "" {
		return nil, fmt.Errorf("region is required (set per-GSS via NetworkConf Region= or set default_region in TOML)")
	}
	if out.Scheme == SchemeInternal {
		if out.Network == "" {
			return nil, fmt.Errorf("network is required for Scheme=Internal")
		}
		if out.Subnetwork == "" {
			return nil, fmt.Errorf("subnetwork is required for Scheme=Internal")
		}
	}
	return out, nil
}

// OnPodAdded ensures the Finalizer is present when RetainOnDelete=false. The
// admission webhook persists the mutated Pod.
func (p *PassthroughNlbPlugin) OnPodAdded(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	nm := utils.NewNetworkManager(pod, c)
	if nm == nil {
		return pod, nil
	}
	cfg, err := p.parseConfig(nm.GetNetworkConfig())
	if err != nil {
		return pod, cperrors.NewPluginError(cperrors.ParameterError, err.Error())
	}
	if !cfg.RetainOnDelete {
		if EnsurePodFinalizer(pod, PodFinalizer) {
			log.Infof("[%s] added Finalizer to pod %s/%s", PassthroughNlbNetwork, pod.Namespace, pod.Name)
		}
	}
	return pod, nil
}

// OnPodUpdated is the main reconcile.
func (p *PassthroughNlbPlugin) OnPodUpdated(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	nm := utils.NewNetworkManager(pod, c)
	if nm == nil {
		return pod, nil
	}
	cfg, err := p.parseConfig(nm.GetNetworkConfig())
	if err != nil {
		return pod, cperrors.NewPluginError(cperrors.ParameterError, err.Error())
	}

	netStatus, err := nm.GetNetworkStatus()
	if err != nil {
		return pod, cperrors.ToPluginError(err, cperrors.InternalError)
	}
	if netStatus == nil {
		out, err := nm.UpdateNetworkStatus(gamekruiseiov1alpha1.NetworkStatus{
			CurrentNetworkState: gamekruiseiov1alpha1.NetworkNotReady,
		}, pod)
		return out, cperrors.ToPluginError(err, cperrors.InternalError)
	}

	// Resolve the owning GameServerSet once. It is the stable anchor for both
	// the resourceID (so the reserved IP survives Pod recreate) and the
	// OwnerReference (so cleanup is tied to the workload, not the Pod).
	gssName := pod.GetLabels()[gamekruiseiov1alpha1.GameServerOwnerGssKey]
	gss := &gamekruiseiov1alpha1.GameServerSet{}
	gssErr := c.Get(ctx, types.NamespacedName{Namespace: pod.Namespace, Name: gssName}, gss)
	if gssErr == nil && gss.DeletionTimestamp != nil {
		// GSS being deleted — flag the Pod so OnPodDeleted has a non-racy signal.
		if pod.Labels == nil {
			pod.Labels = map[string]string{}
		}
		if _, ok := pod.Labels[GssDeletingLabelKey]; !ok {
			pod.Labels[GssDeletingLabelKey] = "true"
			return pod, nil
		}
	}

	ordinal := util.GetIndexFromGsName(pod.Name)
	addrName := DeriveServiceName(pod.Name, passthroughSuffix) // K8s metadata.name, stable per ordinal
	// GCP-side resourceID is anchored on GSS UID + ordinal so a Pod recreate
	// re-adopts the same reserved IP instead of allocating a fresh one.
	addrResourceID := DeriveStableResourceID(gss.GetUID(), ordinal, "pnlb-addr")
	addrType := "EXTERNAL"
	if cfg.Scheme == SchemeInternal {
		addrType = "INTERNAL"
	}
	// Use non-caching client for KCC CRs (cached client would block on missing informer).
	kccClient := p.kccClient(c)
	// Anchor ownership on the GSS (survives Pod recreate). With RetainOnDelete
	// drop the owner entirely so the IP outlives even GSS deletion.
	var addrOwners []metav1.OwnerReference
	if !cfg.RetainOnDelete && gssErr == nil {
		addrOwners = []metav1.OwnerReference{gssOwnerRef(gss)}
	}
	if _, aerr := EnsureComputeAddress(ctx, kccClient, AddressSpec{
		Name:          addrName,
		Namespace:     pod.Namespace,
		Location:      cfg.Region,
		AddressType:   addrType,
		NetworkTier:   cfg.NetworkTier,
		ProjectID:     cfg.ProjectID,
		NetworkRef:    cfg.Network,
		SubnetworkRef: cfg.Subnetwork,
		ResourceID:    addrResourceID,
		OwnerRefs:     addrOwners,
	}); aerr != nil {
		return pod, cperrors.ToPluginError(aerr, cperrors.ApiCallError)
	}

	addrIP, addrReady, aerr := WaitForAddressReady(ctx, kccClient, types.NamespacedName{Namespace: pod.Namespace, Name: addrName})
	if aerr != nil {
		return pod, cperrors.ToPluginError(aerr, cperrors.ApiCallError)
	}
	if !addrReady {
		log.V(1).Infof("[%s] ComputeAddress %s/%s not ready yet", PassthroughNlbNetwork, pod.Namespace, addrName)
		out, err := nm.UpdateNetworkStatus(gamekruiseiov1alpha1.NetworkStatus{
			CurrentNetworkState: gamekruiseiov1alpha1.NetworkNotReady,
		}, pod)
		return out, cperrors.ToPluginError(err, cperrors.InternalError)
	}

	svcName := DeriveServiceName(pod.Name, passthroughSuffix)
	// The GKE LB controller adopts a reserved IP by GCP resource name
	// (ComputeAddress.spec.resourceID), not the K8s metadata.name.
	// For Internal LB, GKE expects spec.loadBalancerIP to carry the actual IP.
	// Pass the disable state in so ensureService doesn't re-assert
	// Type=LoadBalancer on every reconcile and clobber a deliberate disable.
	disabled := nm.GetNetworkDisabled()
	var svcOwner *metav1.OwnerReference
	if gssErr == nil {
		o := gssOwnerRef(gss)
		svcOwner = &o
	}
	svc, serr := p.ensureService(ctx, c, pod, cfg, svcName, addrResourceID, addrIP, disabled, svcOwner)
	if serr != nil {
		return pod, cperrors.ToPluginError(serr, cperrors.ApiCallError)
	}
	if disabled {
		// Don't go further — when disabled the LB is intentionally torn down.
		out, err := nm.UpdateNetworkStatus(gamekruiseiov1alpha1.NetworkStatus{
			CurrentNetworkState: gamekruiseiov1alpha1.NetworkNotReady,
		}, pod)
		return out, cperrors.ToPluginError(err, cperrors.InternalError)
	}

	if util.IsAllowNotReadyContainers(nm.GetNetworkConfig()) {
		changed, perr := utils.AllowNotReadyContainers(c, ctx, pod, svc, false)
		if perr != nil {
			return pod, perr
		}
		if changed {
			if uerr := c.Update(ctx, svc); uerr != nil {
				return pod, cperrors.ToPluginError(uerr, cperrors.ApiCallError)
			}
		}
	}

	// Wait for the LB to settle. The reserved IP is authoritative once the
	// Service ingress IP matches; until then mark NotReady.
	if len(svc.Status.LoadBalancer.Ingress) == 0 || svc.Status.LoadBalancer.Ingress[0].IP == "" {
		log.V(1).Infof("[%s] Service %s/%s LoadBalancer ingress not assigned yet", PassthroughNlbNetwork, pod.Namespace, svc.Name)
		out, err := nm.UpdateNetworkStatus(gamekruiseiov1alpha1.NetworkStatus{
			CurrentNetworkState: gamekruiseiov1alpha1.NetworkNotReady,
		}, pod)
		return out, cperrors.ToPluginError(err, cperrors.InternalError)
	}

	// Build NetworkStatus from cfg.TargetPorts + the reserved IP.
	internal := make([]gamekruiseiov1alpha1.NetworkAddress, 0, len(cfg.TargetPorts))
	external := make([]gamekruiseiov1alpha1.NetworkAddress, 0, len(cfg.TargetPorts))
	for i, tp := range cfg.TargetPorts {
		intInternal := intstr.FromInt(tp)
		intExternal := intstr.FromInt(tp) // passthrough preserves the wire port
		internal = append(internal, gamekruiseiov1alpha1.NetworkAddress{
			IP: pod.Status.PodIP,
			Ports: []gamekruiseiov1alpha1.NetworkPort{{
				Name:     strconv.Itoa(tp),
				Port:     &intInternal,
				Protocol: cfg.Protocols[i],
			}},
		})
		external = append(external, gamekruiseiov1alpha1.NetworkAddress{
			IP: addrIP,
			Ports: []gamekruiseiov1alpha1.NetworkPort{{
				Name:     strconv.Itoa(tp),
				Port:     &intExternal,
				Protocol: cfg.Protocols[i],
			}},
		})
	}
	netStatus.InternalAddresses = internal
	netStatus.ExternalAddresses = external
	netStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkReady
	out, uerr := nm.UpdateNetworkStatus(*netStatus, pod)
	return out, cperrors.ToPluginError(uerr, cperrors.InternalError)
}

// OnPodDeleted runs when the pod is being torn down. Resources are released
// only on a genuine scale-down or GSS deletion; a transient Pod recreate at an
// in-range ordinal keeps the reserved IP + LB so the new Pod re-adopts them.
func (p *PassthroughNlbPlugin) OnPodDeleted(c client.Client, pod *corev1.Pod, ctx context.Context) cperrors.PluginError {
	nm := utils.NewNetworkManager(pod, c)
	if nm == nil {
		return nil
	}
	cfg, err := p.parseConfig(nm.GetNetworkConfig())
	if err != nil {
		// Still try to remove the Finalizer so we don't pin the pod.
		_ = RemovePodFinalizer(ctx, c, pod, PodFinalizer)
		return cperrors.NewPluginError(cperrors.ParameterError, err.Error())
	}
	if cfg.RetainOnDelete {
		return RemovePodFinalizerPlugin(ctx, c, pod)
	}

	gssName := pod.GetLabels()[gamekruiseiov1alpha1.GameServerOwnerGssKey]
	// GssDeletingLabelKey is set by OnPodUpdated when the GSS entered deletion;
	// treat it as an immediate release signal (avoids a GSS-get race here).
	gssDeleting := pod.Labels[GssDeletingLabelKey] == "true"
	if !gssDeleting && !shouldReleaseSlot(ctx, c, pod.Namespace, gssName, pod.Name) {
		// Transient recreate at an in-range ordinal — keep resources, just let
		// the Pod finish deleting.
		log.Infof("[%s] pod %s/%s recreating at in-range ordinal; preserving reserved IP + LB",
			PassthroughNlbNetwork, pod.Namespace, pod.Name)
		return RemovePodFinalizerPlugin(ctx, c, pod)
	}

	// Genuine release: delete the per-pod Service first (the GKE LB controller
	// tears down the FR chain when the Service goes away), then the ComputeAddress.
	svcName := DeriveServiceName(pod.Name, passthroughSuffix)
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: pod.Namespace}}
	if derr := c.Delete(ctx, svc); derr != nil && !apierrors.IsNotFound(derr) {
		log.Errorf("[%s] delete Service %s/%s: %v", PassthroughNlbNetwork, pod.Namespace, svcName, derr)
		return cperrors.ToPluginError(derr, cperrors.ApiCallError)
	}
	addrName := DeriveServiceName(pod.Name, passthroughSuffix)
	addrTyped := &gcpv1beta1.ComputeAddress{ObjectMeta: metav1.ObjectMeta{Name: addrName, Namespace: pod.Namespace}}
	if derr := p.kccClient(c).Delete(ctx, addrTyped); derr != nil && !apierrors.IsNotFound(derr) {
		log.Errorf("[%s] delete ComputeAddress %s/%s: %v", PassthroughNlbNetwork, pod.Namespace, addrName, derr)
		return cperrors.ToPluginError(derr, cperrors.ApiCallError)
	}
	log.Infof("[%s] released GCP resources for pod %s/%s (scale-down or GSS deletion)",
		PassthroughNlbNetwork, pod.Namespace, pod.Name)
	return RemovePodFinalizerPlugin(ctx, c, pod)
}

// ensureService creates or updates the per-pod Service. Type=LoadBalancer +
// the right GKE annotations get the in-cluster controller to do the heavy
// lifting (FR / BES / HC) while we own only the IP via ComputeAddress.
//
// addrGCPName is the ComputeAddress.spec.resourceID (the GCP-side name) used
// to adopt the reserved External IP via annotation. addrIPValue is the actual
// IP string used to adopt the reserved Internal IP via spec.loadBalancerIP
// (GKE Internal LB controller ignores the External annotation).
//
// disabled=true sets the Service to ClusterIP (no LB), giving operators a way
// to drain a pod's external traffic without deleting the GameServer.
func (p *PassthroughNlbPlugin) ensureService(ctx context.Context, c client.Client, pod *corev1.Pod, cfg *passthroughConfig, svcName, addrGCPName, addrIPValue string, disabled bool, svcOwner *metav1.OwnerReference) (*corev1.Service, error) {
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: pod.Namespace}}
	mutate := func() error {
		if svc.Labels == nil {
			svc.Labels = map[string]string{}
		}
		svc.Labels[ResourceTagKey] = ResourceTagValue
		svc.Labels[gamekruiseiov1alpha1.GameServerOwnerGssKey] =
			pod.GetLabels()[gamekruiseiov1alpha1.GameServerOwnerGssKey]
		if svc.Annotations == nil {
			svc.Annotations = map[string]string{}
		}
		// IP adoption:
		//   External LB → annotation carries the GCP Address name (the RBS path).
		//   Internal LB → spec.loadBalancerIP carries the actual IP value.
		if cfg.Scheme == SchemeExternal {
			svc.Annotations[GKELoadBalancerIPAnnotationKey] = addrGCPName
			svc.Annotations[L4RBSAnnotationKey] = "enabled"
			svc.Annotations[NetworkTierAnnotationKey] = cfg.NetworkTier
			delete(svc.Annotations, GKELoadBalancerTypeAnnotationKey)
			delete(svc.Annotations, "networking.gke.io/internal-load-balancer-allow-global-access")
		} else {
			delete(svc.Annotations, GKELoadBalancerIPAnnotationKey)
			delete(svc.Annotations, L4RBSAnnotationKey)
			delete(svc.Annotations, NetworkTierAnnotationKey)
			svc.Annotations[GKELoadBalancerTypeAnnotationKey] = "Internal"
			if cfg.AllowGlobalAccess {
				svc.Annotations["networking.gke.io/internal-load-balancer-allow-global-access"] = "true"
			} else {
				delete(svc.Annotations, "networking.gke.io/internal-load-balancer-allow-global-access")
			}
		}
		// Drift detection.
		svc.Annotations[ConfigHashKey] = util.GetHash(cfg)
		// User-supplied passthrough Service annotations are layered last so they
		// can override LB-class defaults if absolutely required.
		for k, v := range cfg.Annotations {
			svc.Annotations[k] = v
		}

		// Service spec.
		if disabled {
			svc.Spec.Type = corev1.ServiceTypeClusterIP
			svc.Spec.LoadBalancerIP = ""
		} else {
			svc.Spec.Type = corev1.ServiceTypeLoadBalancer
			if cfg.Scheme == SchemeInternal && addrIPValue != "" {
				// Internal LB adopts the reserved IP via spec.loadBalancerIP.
				svc.Spec.LoadBalancerIP = addrIPValue
			} else {
				svc.Spec.LoadBalancerIP = ""
			}
		}
		svc.Spec.AllocateLoadBalancerNodePorts = ptr.To(false)
		svc.Spec.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicyTypeLocal
		svc.Spec.Selector = map[string]string{SvcSelectorKey: pod.Name}

		ports := make([]corev1.ServicePort, 0, len(cfg.TargetPorts))
		for i, tp := range cfg.TargetPorts {
			ports = append(ports, corev1.ServicePort{
				Name:       fmt.Sprintf("p%d", tp),
				Port:       int32(tp),
				Protocol:   cfg.Protocols[i],
				TargetPort: intstr.FromInt(tp),
			})
		}
		svc.Spec.Ports = ports

		// OwnerReference is anchored on the GameServerSet (not the Pod) so the
		// Service — and the GKE-managed LB behind it — survives a Pod recreate.
		// Cleanup on scale-down / GSS deletion is explicit (OnPodDeleted).
		// RetainOnDelete=true drops the owner so the Service outlives even the GSS.
		if !cfg.RetainOnDelete && svcOwner != nil {
			svc.OwnerReferences = []metav1.OwnerReference{*svcOwner}
		}
		return nil
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, c, svc, mutate); err != nil {
		return nil, err
	}
	return svc, nil
}

// RemovePodFinalizerPlugin is a small wrapper that maps the controller-runtime
// error onto a PluginError; the rest of the codebase consumes PluginError so
// the Plugin contract is preserved.
func RemovePodFinalizerPlugin(ctx context.Context, c client.Client, pod *corev1.Pod) cperrors.PluginError {
	if err := RemovePodFinalizer(ctx, c, pod, PodFinalizer); err != nil {
		return cperrors.ToPluginError(err, cperrors.ApiCallError)
	}
	return nil
}

// kccClient returns the non-caching API client when Init has populated it,
// falling back to the cached client (only happens before Init, e.g. in unit
// tests that bypass Init entirely).
func (p *PassthroughNlbPlugin) kccClient(fallback client.Client) client.Client {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	if p.apiClient != nil {
		return p.apiClient
	}
	return fallback
}

func init() {
	googleCloudProvider.registerPlugin(&PassthroughNlbPlugin{})
}
