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
	"encoding/json"
	"fmt"
	"sort"
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
	// GlobalProxyNlbNetwork is the value users put in spec.network.networkType
	// to pick this plugin.
	GlobalProxyNlbNetwork = "GoogleCloud-GlobalProxyNLB"
	// AliasGlobalProxyNlb is the cross-cloud alias.
	AliasGlobalProxyNlb = "GlobalProxyNLB-Network"

	// proxySuffix tags resources owned by this plugin.
	proxySuffix = "gcp-gpnlb"
)

// GlobalProxyNlbPlugin provisions a per-pod Global External Proxy Network Load
// Balancer (loadBalancingScheme=EXTERNAL_MANAGED, anycast IP, GFE-backed).
//
// Stack per pod:
//
//	Service (ClusterIP + cloud.google.com/neg annotation)
//	    -> GKE NEG controller creates zonal ComputeNetworkEndpointGroups (Pod IP)
//	ComputeHealthCheck (global, TCP, USE_SERVING_PORT)
//	ComputeBackendService (global, EXTERNAL_MANAGED, TCP, backends=NEGs, HC ref)
//	ComputeTargetTCPProxy (proxyHeader=PROXY_V1 if requested)
//	ComputeAddress (global, EXTERNAL, PREMIUM, IPV4)
//	ComputeForwardingRule (global, EXTERNAL_MANAGED, TCP, single port, address ref, target ref)
//	ComputeFirewall (allow 35.191.0.0/16 + 130.211.0.0/22 to backend pod port)
type GlobalProxyNlbPlugin struct {
	projectID             string
	defaultNetwork        string
	firewallNetworkRef    string
	retainOnDeleteDefault bool
	// apiClient is a non-caching client for KCC CR operations. See the
	// same field on PassthroughNlbPlugin for the rationale.
	apiClient client.Client
	mutex     sync.RWMutex
}

// Name implements cloudprovider.Plugin.
func (p *GlobalProxyNlbPlugin) Name() string { return GlobalProxyNlbNetwork }

// Alias implements cloudprovider.Plugin.
func (p *GlobalProxyNlbPlugin) Alias() string { return AliasGlobalProxyNlb }

// Init implements cloudprovider.Plugin.
func (p *GlobalProxyNlbPlugin) Init(c client.Client, opts cloudprovider.CloudProviderOptions, ctx context.Context) error {
	gcpOpts, ok := opts.(provideroptions.GoogleCloudOptions)
	if !ok {
		return fmt.Errorf("googlecloud proxy: expected GoogleCloudOptions, got %T", opts)
	}
	if !gcpOpts.GlobalProxyNLB.Enable {
		log.Infof("[%s] plugin disabled via GlobalProxyNLB.Enable=false", GlobalProxyNlbNetwork)
		return nil
	}

	p.mutex.Lock()
	p.projectID = gcpOpts.ProjectID
	p.defaultNetwork = gcpOpts.DefaultNetwork
	p.firewallNetworkRef = gcpOpts.GlobalProxyNLB.FirewallNetworkRef
	if p.firewallNetworkRef == "" {
		p.firewallNetworkRef = gcpOpts.DefaultNetwork
	}
	p.retainOnDeleteDefault = gcpOpts.GlobalProxyNLB.RetainOnDeleteDefault
	p.mutex.Unlock()

	cfg := ctrl.GetConfigOrDie()
	if err := VerifyKCCInstalled(cfg); err != nil {
		return err
	}
	apiClient, err := client.New(cfg, client.Options{Scheme: c.Scheme(), Mapper: c.RESTMapper()})
	if err != nil {
		return fmt.Errorf("googlecloud proxy: build non-caching client: %w", err)
	}
	p.mutex.Lock()
	p.apiClient = apiClient
	p.mutex.Unlock()
	log.Infof("[%s] initialized (projectID=%s firewallNetwork=%s)",
		GlobalProxyNlbNetwork, p.projectID, p.firewallNetworkRef)
	return nil
}

// kccClient returns the non-caching API client when Init has populated it.
func (p *GlobalProxyNlbPlugin) kccClient(fallback client.Client) client.Client {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	if p.apiClient != nil {
		return p.apiClient
	}
	return fallback
}

// proxyConfig holds the parsed NetworkConfParams for one GameServer.
type proxyConfig struct {
	ProjectID                 string
	Network                   string
	Port                      int
	Protocol                  corev1.Protocol
	ProxyHeader               string
	HealthCheckIntervalSec    int64
	HealthCheckTimeoutSec     int64
	HealthyThreshold          int64
	UnhealthyThreshold        int64
	BalancingMode             string
	MaxConnectionsPerEndpoint int64
	Annotations               map[string]string
	RetainOnDelete            bool
}

func (p *GlobalProxyNlbPlugin) parseConfig(conf []gamekruiseiov1alpha1.NetworkConfParams) (*proxyConfig, error) {
	p.mutex.RLock()
	out := &proxyConfig{
		ProjectID:                 p.projectID,
		Network:                   p.firewallNetworkRef,
		ProxyHeader:               ProxyHeaderNone,
		HealthCheckIntervalSec:    5,
		HealthCheckTimeoutSec:     5,
		HealthyThreshold:          2,
		UnhealthyThreshold:        2,
		BalancingMode:             "CONNECTION",
		MaxConnectionsPerEndpoint: 1000,
		Protocol:                  corev1.ProtocolTCP,
		Annotations:               map[string]string{},
		RetainOnDelete:            p.retainOnDeleteDefault,
	}
	p.mutex.RUnlock()
	for _, c := range conf {
		v := strings.TrimSpace(c.Value)
		switch c.Name {
		case ConfProjectID:
			out.ProjectID = v
		case ConfNetwork:
			out.Network = v
		case ConfPort:
			port, err := strconv.Atoi(v)
			if err != nil {
				return nil, fmt.Errorf("invalid Port %q: %w", v, err)
			}
			if port < 1 || port > 65535 {
				return nil, fmt.Errorf("invalid Port %d (1-65535)", port)
			}
			out.Port = port
		case ConfProxyHeader:
			ph := strings.ToUpper(v)
			if ph != ProxyHeaderNone && ph != ProxyHeaderV1 {
				return nil, fmt.Errorf("invalid ProxyHeader %q (want NONE or PROXY_V1)", v)
			}
			out.ProxyHeader = ph
		case ConfHealthCheckIntervalSec:
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil || n < 1 {
				return nil, fmt.Errorf("invalid HealthCheckIntervalSec %q", v)
			}
			out.HealthCheckIntervalSec = n
		case ConfHealthCheckTimeoutSec:
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil || n < 1 {
				return nil, fmt.Errorf("invalid HealthCheckTimeoutSec %q", v)
			}
			out.HealthCheckTimeoutSec = n
		case ConfHealthyThreshold:
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil || n < 1 {
				return nil, fmt.Errorf("invalid HealthyThreshold %q", v)
			}
			out.HealthyThreshold = n
		case ConfUnhealthyThreshold:
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil || n < 1 {
				return nil, fmt.Errorf("invalid UnhealthyThreshold %q", v)
			}
			out.UnhealthyThreshold = n
		case ConfBalancingMode:
			bm := strings.ToUpper(v)
			if bm != "CONNECTION" && bm != "RATE" && bm != "UTILIZATION" {
				return nil, fmt.Errorf("invalid BalancingMode %q", v)
			}
			out.BalancingMode = bm
		case ConfMaxConnectionsPerEndpoint:
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil || n < 1 {
				return nil, fmt.Errorf("invalid MaxConnectionsPerEndpoint %q", v)
			}
			out.MaxConnectionsPerEndpoint = n
		case ConfAnnotations:
			for _, kv := range strings.Split(v, ",") {
				kv = strings.TrimSpace(kv)
				if kv == "" {
					continue
				}
				eq := strings.Index(kv, "=")
				if eq <= 0 || eq == len(kv)-1 {
					return nil, fmt.Errorf("invalid Annotations entry %q", kv)
				}
				out.Annotations[kv[:eq]] = kv[eq+1:]
			}
		case ConfRetainOnDelete:
			b, err := strconv.ParseBool(v)
			if err != nil {
				return nil, fmt.Errorf("invalid RetainOnDelete: %w", err)
			}
			out.RetainOnDelete = b
		}
	}
	if out.Port == 0 {
		return nil, fmt.Errorf("port is required (NetworkConf Port=<single value 1-65535>)")
	}
	if out.HealthCheckTimeoutSec > out.HealthCheckIntervalSec {
		return nil, fmt.Errorf("HealthCheckTimeoutSec (%d) must be <= HealthCheckIntervalSec (%d)",
			out.HealthCheckTimeoutSec, out.HealthCheckIntervalSec)
	}
	if out.Network == "" {
		return nil, fmt.Errorf("network is required (set per-GSS via NetworkConf Network= or default_network in TOML)")
	}
	return out, nil
}

// OnPodAdded attaches the Finalizer when !RetainOnDelete.
func (p *GlobalProxyNlbPlugin) OnPodAdded(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
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
			log.Infof("[%s] added Finalizer to pod %s/%s", GlobalProxyNlbNetwork, pod.Namespace, pod.Name)
		}
	}
	return pod, nil
}

// OnPodUpdated reconciles the full KCC stack. Three-stage gate:
//  1. Service+NEG annotation present, neg-status published by GKE
//  2. KCC HC + BES + TargetTCPProxy + Address all Ready
//  3. ComputeForwardingRule Ready + IP assigned
func (p *GlobalProxyNlbPlugin) OnPodUpdated(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
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

	// Resolve the owning GameServerSet — the stable anchor for resourceID
	// (so the anycast IP + KCC graph survive Pod recreate) and OwnerReference.
	gssName := pod.GetLabels()[gamekruiseiov1alpha1.GameServerOwnerGssKey]
	gss := &gamekruiseiov1alpha1.GameServerSet{}
	gssErr := c.Get(ctx, types.NamespacedName{Namespace: pod.Namespace, Name: gssName}, gss)
	if gssErr == nil && gss.DeletionTimestamp != nil {
		if pod.Labels == nil {
			pod.Labels = map[string]string{}
		}
		if _, ok := pod.Labels[GssDeletingLabelKey]; !ok {
			pod.Labels[GssDeletingLabelKey] = "true"
			return pod, nil
		}
	}
	ordinal := util.GetIndexFromGsName(pod.Name)
	var gssRef *metav1.OwnerReference
	if !cfg.RetainOnDelete && gssErr == nil {
		o := gssOwnerRef(gss)
		gssRef = &o
	}

	// (1) Ensure the per-pod Service + NEG annotation. The GKE NEG controller
	// reads the annotation and creates per-zone NEGs targeting Pod IP:port.
	svcName := DeriveServiceName(pod.Name, proxySuffix)
	svc, serr := p.ensureService(ctx, c, pod, cfg, svcName, gss.GetUID(), ordinal, gssRef)
	if serr != nil {
		return pod, cperrors.ToPluginError(serr, cperrors.ApiCallError)
	}
	negRefs, perr := ParseNEGStatusAnnotation(svc)
	if perr != nil {
		return pod, cperrors.NewPluginError(cperrors.InternalError, perr.Error())
	}
	thisPortNEGs, hasNEGs := negRefs[int32(cfg.Port)]
	if !hasNEGs || len(thisPortNEGs) == 0 {
		log.V(1).Infof("[%s] waiting for GKE NEG controller to publish neg-status for %s/%s", GlobalProxyNlbNetwork, pod.Namespace, svc.Name)
		out, uerr := nm.UpdateNetworkStatus(gamekruiseiov1alpha1.NetworkStatus{
			CurrentNetworkState: gamekruiseiov1alpha1.NetworkNotReady,
		}, pod)
		return out, cperrors.ToPluginError(uerr, cperrors.InternalError)
	}

	// (2) Reconcile KCC graph in dependency order. Names anchored on GSS+ordinal.
	hcName := DeriveStableResourceID(gss.GetUID(), ordinal, "gpnlb-hc")
	besName := DeriveStableResourceID(gss.GetUID(), ordinal, "gpnlb-bes")
	tcpProxyName := DeriveStableResourceID(gss.GetUID(), ordinal, "gpnlb-tp")
	addrName := DeriveStableResourceID(gss.GetUID(), ordinal, "gpnlb-addr")
	frName := DeriveStableResourceID(gss.GetUID(), ordinal, "gpnlb-fr")
	fwName := DeriveStableResourceID(gss.GetUID(), ordinal, "gpnlb-fw")

	kccClient := p.kccClient(c)
	if perr := p.ensureHealthCheck(ctx, kccClient, pod, cfg, hcName, gssRef); perr != nil {
		return pod, cperrors.ToPluginError(perr, cperrors.ApiCallError)
	}
	if perr := p.ensureBackendService(ctx, kccClient, pod, cfg, besName, hcName, thisPortNEGs, gssRef); perr != nil {
		return pod, cperrors.ToPluginError(perr, cperrors.ApiCallError)
	}
	if perr := p.ensureTargetTCPProxy(ctx, kccClient, pod, cfg, tcpProxyName, besName, gssRef); perr != nil {
		return pod, cperrors.ToPluginError(perr, cperrors.ApiCallError)
	}
	var addrOwners []metav1.OwnerReference
	if gssRef != nil {
		addrOwners = []metav1.OwnerReference{*gssRef}
	}
	if _, aerr := EnsureComputeAddress(ctx, kccClient, AddressSpec{
		Name:        addrName,
		Namespace:   pod.Namespace,
		Location:    "global",
		AddressType: "EXTERNAL",
		NetworkTier: "PREMIUM",
		ProjectID:   cfg.ProjectID,
		ResourceID:  addrName,
		OwnerRefs:   addrOwners,
	}); aerr != nil {
		return pod, cperrors.ToPluginError(aerr, cperrors.ApiCallError)
	}
	if perr := p.ensureForwardingRule(ctx, kccClient, pod, cfg, frName, tcpProxyName, addrName, gssRef); perr != nil {
		return pod, cperrors.ToPluginError(perr, cperrors.ApiCallError)
	}
	if perr := p.ensureFirewall(ctx, kccClient, pod, cfg, fwName, gssRef); perr != nil {
		return pod, cperrors.ToPluginError(perr, cperrors.ApiCallError)
	}

	// (3) Read back readiness from each KCC object.
	allReady, ip, gateMsg := p.checkReady(ctx, kccClient, pod, cfg, hcName, besName, tcpProxyName, addrName, frName, fwName)
	if !allReady {
		log.V(1).Infof("[%s] not yet ready for %s/%s: %s", GlobalProxyNlbNetwork, pod.Namespace, pod.Name, gateMsg)
		out, uerr := nm.UpdateNetworkStatus(gamekruiseiov1alpha1.NetworkStatus{
			CurrentNetworkState: gamekruiseiov1alpha1.NetworkNotReady,
		}, pod)
		return out, cperrors.ToPluginError(uerr, cperrors.InternalError)
	}

	intPort := intstr.FromInt(cfg.Port)
	netStatus.InternalAddresses = []gamekruiseiov1alpha1.NetworkAddress{{
		IP: pod.Status.PodIP,
		Ports: []gamekruiseiov1alpha1.NetworkPort{{
			Name: strconv.Itoa(cfg.Port), Port: &intPort, Protocol: cfg.Protocol,
		}},
	}}
	netStatus.ExternalAddresses = []gamekruiseiov1alpha1.NetworkAddress{{
		IP: ip,
		Ports: []gamekruiseiov1alpha1.NetworkPort{{
			Name: strconv.Itoa(cfg.Port), Port: &intPort, Protocol: cfg.Protocol,
		}},
	}}
	netStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkReady
	out, uerr := nm.UpdateNetworkStatus(*netStatus, pod)
	return out, cperrors.ToPluginError(uerr, cperrors.InternalError)
}

// OnPodDeleted clears the KCC graph and the Pod Finalizer when RetainOnDelete=false.
func (p *GlobalProxyNlbPlugin) OnPodDeleted(c client.Client, pod *corev1.Pod, ctx context.Context) cperrors.PluginError {
	nm := utils.NewNetworkManager(pod, c)
	if nm == nil {
		return nil
	}
	cfg, err := p.parseConfig(nm.GetNetworkConfig())
	if err != nil {
		_ = RemovePodFinalizer(ctx, c, pod, PodFinalizer)
		return cperrors.NewPluginError(cperrors.ParameterError, err.Error())
	}
	if cfg.RetainOnDelete {
		return RemovePodFinalizerPlugin(ctx, c, pod)
	}

	gssName := pod.GetLabels()[gamekruiseiov1alpha1.GameServerOwnerGssKey]
	gssDeleting := pod.Labels[GssDeletingLabelKey] == "true"
	if !gssDeleting && !shouldReleaseSlot(ctx, c, pod.Namespace, gssName, pod.Name) {
		// Transient recreate at an in-range ordinal — keep the anycast IP + KCC
		// graph so the new Pod re-adopts them; just release the Pod.
		log.Infof("[%s] pod %s/%s recreating at in-range ordinal; preserving anycast IP + KCC graph",
			GlobalProxyNlbNetwork, pod.Namespace, pod.Name)
		return RemovePodFinalizerPlugin(ctx, c, pod)
	}

	// Resolve the GSS UID to rebuild the stable resource names. If the GSS is
	// already gone we fall back to a zero UID — the names won't match live CRs,
	// but the owner-reference GC will reap them when the GSS was deleted.
	gss := &gamekruiseiov1alpha1.GameServerSet{}
	_ = c.Get(ctx, types.NamespacedName{Namespace: pod.Namespace, Name: gssName}, gss)
	ordinal := util.GetIndexFromGsName(pod.Name)
	svcName := DeriveServiceName(pod.Name, proxySuffix)
	hcName := DeriveStableResourceID(gss.GetUID(), ordinal, "gpnlb-hc")
	besName := DeriveStableResourceID(gss.GetUID(), ordinal, "gpnlb-bes")
	tcpProxyName := DeriveStableResourceID(gss.GetUID(), ordinal, "gpnlb-tp")
	addrName := DeriveStableResourceID(gss.GetUID(), ordinal, "gpnlb-addr")
	frName := DeriveStableResourceID(gss.GetUID(), ordinal, "gpnlb-fr")
	fwName := DeriveStableResourceID(gss.GetUID(), ordinal, "gpnlb-fw")

	// Delete in reverse dependency order. Errors other than NotFound surface
	// as RetryError so the controller revisits.
	deleteOrder := []client.Object{
		&gcpv1beta1.ComputeForwardingRule{ObjectMeta: metav1.ObjectMeta{Name: frName, Namespace: pod.Namespace}},
		&gcpv1beta1.ComputeFirewall{ObjectMeta: metav1.ObjectMeta{Name: fwName, Namespace: pod.Namespace}},
		&gcpv1beta1.ComputeTargetTCPProxy{ObjectMeta: metav1.ObjectMeta{Name: tcpProxyName, Namespace: pod.Namespace}},
		&gcpv1beta1.ComputeBackendService{ObjectMeta: metav1.ObjectMeta{Name: besName, Namespace: pod.Namespace}},
		&gcpv1beta1.ComputeHealthCheck{ObjectMeta: metav1.ObjectMeta{Name: hcName, Namespace: pod.Namespace}},
		&gcpv1beta1.ComputeAddress{ObjectMeta: metav1.ObjectMeta{Name: addrName, Namespace: pod.Namespace}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: pod.Namespace}},
	}
	kccClient := p.kccClient(c)
	for _, obj := range deleteOrder {
		delClient := kccClient
		if _, isService := obj.(*corev1.Service); isService {
			delClient = c
		}
		if derr := delClient.Delete(ctx, obj); derr != nil && !apierrors.IsNotFound(derr) {
			log.Errorf("[%s] delete %T %s/%s: %v", GlobalProxyNlbNetwork, obj, obj.GetNamespace(), obj.GetName(), derr)
			return cperrors.ToPluginError(derr, cperrors.ApiCallError)
		}
	}
	return RemovePodFinalizerPlugin(ctx, c, pod)
}

// ensureService creates the ClusterIP Service that carries the cloud.google.com/neg
// annotation. The GKE NEG controller reacts and publishes zonal NEGs.
func (p *GlobalProxyNlbPlugin) ensureService(ctx context.Context, c client.Client, pod *corev1.Pod, cfg *proxyConfig, svcName string, gssUID types.UID, ordinal int, owner *metav1.OwnerReference) (*corev1.Service, error) {
	negName := DeriveStableResourceID(gssUID, ordinal, fmt.Sprintf("neg-%d", cfg.Port))
	annValue, _ := json.Marshal(map[string]any{
		"exposed_ports": map[string]any{
			strconv.Itoa(cfg.Port): map[string]string{"name": negName},
		},
	})

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
		svc.Annotations[NEGAnnotationKey] = string(annValue)
		svc.Annotations[ConfigHashKey] = util.GetHash(cfg)
		for k, v := range cfg.Annotations {
			svc.Annotations[k] = v
		}
		svc.Spec.Type = corev1.ServiceTypeClusterIP
		svc.Spec.Selector = map[string]string{SvcSelectorKey: pod.Name}
		port := int32(cfg.Port)
		svc.Spec.Ports = []corev1.ServicePort{{
			Name:       fmt.Sprintf("p%d", cfg.Port),
			Port:       port,
			TargetPort: intstr.FromInt(cfg.Port),
			Protocol:   corev1.ProtocolTCP,
		}}
		if owner != nil {
			svc.OwnerReferences = []metav1.OwnerReference{*owner}
		}
		return nil
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, c, svc, mutate); err != nil {
		return nil, err
	}
	return svc, nil
}

func (p *GlobalProxyNlbPlugin) ensureHealthCheck(ctx context.Context, c client.Client, pod *corev1.Pod, cfg *proxyConfig, name string, owner *metav1.OwnerReference) error {
	hc := &gcpv1beta1.ComputeHealthCheck{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: pod.Namespace}}
	mutate := func() error {
		applyProjectAndOwner(hc, cfg.ProjectID, pod.GetLabels()[gamekruiseiov1alpha1.GameServerOwnerGssKey], owner)
		hc.Spec.Location = "global"
		hc.Spec.ResourceID = ptr.To(name)
		hc.Spec.CheckIntervalSec = ptr.To(cfg.HealthCheckIntervalSec)
		hc.Spec.TimeoutSec = ptr.To(cfg.HealthCheckTimeoutSec)
		hc.Spec.HealthyThreshold = ptr.To(cfg.HealthyThreshold)
		hc.Spec.UnhealthyThreshold = ptr.To(cfg.UnhealthyThreshold)
		hc.Spec.TCPHealthCheck = &gcpv1beta1.HealthCheckTCP{
			PortSpecification: ptr.To("USE_SERVING_PORT"),
			ProxyHeader:       ptr.To(ProxyHeaderNone),
		}
		return nil
	}
	_, err := controllerutil.CreateOrUpdate(ctx, c, hc, mutate)
	return err
}

func (p *GlobalProxyNlbPlugin) ensureBackendService(ctx context.Context, c client.Client, pod *corev1.Pod, cfg *proxyConfig, name, hcName string, negs []NEGRef, owner *metav1.OwnerReference) error {
	bes := &gcpv1beta1.ComputeBackendService{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: pod.Namespace}}
	mutate := func() error {
		applyProjectAndOwner(bes, cfg.ProjectID, pod.GetLabels()[gamekruiseiov1alpha1.GameServerOwnerGssKey], owner)
		bes.Spec.Location = "global"
		bes.Spec.ResourceID = ptr.To(name)
		bes.Spec.Protocol = ptr.To("TCP")
		bes.Spec.LoadBalancingScheme = ptr.To("EXTERNAL_MANAGED")
		bes.Spec.TimeoutSec = ptr.To(int64(30))
		bes.Spec.ConnectionDrainingTimeoutSec = ptr.To(int64(60))
		bes.Spec.HealthChecks = []gcpv1beta1.BackendServiceHealthCheckRef{{
			HealthCheckRef: &gcpv1beta1.ResourceRef{Name: hcName},
		}}
		// Stable backend order — sort NEGRefs by zone so reconciles don't
		// thrash on slice ordering.
		sortedNegs := append([]NEGRef(nil), negs...)
		sort.Slice(sortedNegs, func(i, j int) bool { return sortedNegs[i].Zone < sortedNegs[j].Zone })
		bes.Spec.Backend = make([]gcpv1beta1.BackendServiceBackend, 0, len(sortedNegs))
		for _, n := range sortedNegs {
			bes.Spec.Backend = append(bes.Spec.Backend, gcpv1beta1.BackendServiceBackend{
				Group: gcpv1beta1.BackendGroup{
					NetworkEndpointGroupRef: &gcpv1beta1.ResourceRef{
						External: n.SelfLink(cfg.ProjectID),
					},
				},
				BalancingMode:             ptr.To(cfg.BalancingMode),
				MaxConnectionsPerEndpoint: ptr.To(cfg.MaxConnectionsPerEndpoint),
			})
		}
		return nil
	}
	_, err := controllerutil.CreateOrUpdate(ctx, c, bes, mutate)
	return err
}

func (p *GlobalProxyNlbPlugin) ensureTargetTCPProxy(ctx context.Context, c client.Client, pod *corev1.Pod, cfg *proxyConfig, name, besName string, owner *metav1.OwnerReference) error {
	tp := &gcpv1beta1.ComputeTargetTCPProxy{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: pod.Namespace}}
	mutate := func() error {
		applyProjectAndOwner(tp, cfg.ProjectID, pod.GetLabels()[gamekruiseiov1alpha1.GameServerOwnerGssKey], owner)
		tp.Spec.ResourceID = ptr.To(name)
		tp.Spec.BackendServiceRef = gcpv1beta1.ResourceRef{Name: besName}
		tp.Spec.ProxyHeader = ptr.To(cfg.ProxyHeader)
		return nil
	}
	_, err := controllerutil.CreateOrUpdate(ctx, c, tp, mutate)
	return err
}

func (p *GlobalProxyNlbPlugin) ensureForwardingRule(ctx context.Context, c client.Client, pod *corev1.Pod, cfg *proxyConfig, name, tpName, addrName string, owner *metav1.OwnerReference) error {
	fr := &gcpv1beta1.ComputeForwardingRule{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: pod.Namespace}}
	mutate := func() error {
		applyProjectAndOwner(fr, cfg.ProjectID, pod.GetLabels()[gamekruiseiov1alpha1.GameServerOwnerGssKey], owner)
		fr.Spec.Location = "global"
		fr.Spec.ResourceID = ptr.To(name)
		fr.Spec.LoadBalancingScheme = ptr.To("EXTERNAL_MANAGED")
		fr.Spec.NetworkTier = ptr.To("PREMIUM")
		fr.Spec.IPProtocol = ptr.To("TCP")
		// Do NOT set IPVersion when ipAddress.addressRef is also set — GCP rejects
		// the combination because the address resource already constrains the family.
		fr.Spec.IPVersion = nil
		fr.Spec.PortRange = ptr.To(strconv.Itoa(cfg.Port))
		fr.Spec.Target = &gcpv1beta1.ForwardingRuleTarget{
			TargetTCPProxyRef: &gcpv1beta1.ResourceRef{Name: tpName},
		}
		fr.Spec.IPAddress = &gcpv1beta1.ForwardingRuleIPAddress{
			AddressRef: &gcpv1beta1.ResourceRef{Name: addrName},
		}
		return nil
	}
	_, err := controllerutil.CreateOrUpdate(ctx, c, fr, mutate)
	return err
}

func (p *GlobalProxyNlbPlugin) ensureFirewall(ctx context.Context, c client.Client, pod *corev1.Pod, cfg *proxyConfig, name string, owner *metav1.OwnerReference) error {
	fw := &gcpv1beta1.ComputeFirewall{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: pod.Namespace}}
	mutate := func() error {
		applyProjectAndOwner(fw, cfg.ProjectID, pod.GetLabels()[gamekruiseiov1alpha1.GameServerOwnerGssKey], owner)
		fw.Spec.ResourceID = ptr.To(name)
		fw.Spec.Direction = ptr.To("INGRESS")
		fw.Spec.Priority = ptr.To(int64(1000))
		// Reference the GCP network by selfLink (External) rather than by KCC
		// ComputeNetwork name. The "default" VPC is a GCP-native resource that
		// usually does not have a corresponding KCC CR.
		project := cfg.ProjectID
		if project == "" {
			project = p.projectID
		}
		fw.Spec.NetworkRef = gcpv1beta1.ResourceRef{
			External: fmt.Sprintf("projects/%s/global/networks/%s", project, cfg.Network),
		}
		fw.Spec.SourceRanges = append([]string(nil), DefaultHealthCheckSourceRanges...)
		fw.Spec.Allowed = []gcpv1beta1.FirewallRule{{
			Protocol: "tcp",
			Ports:    []string{strconv.Itoa(cfg.Port)},
		}}
		return nil
	}
	_, err := controllerutil.CreateOrUpdate(ctx, c, fw, mutate)
	return err
}

// checkReady inspects the readiness of every KCC object the plugin owns and
// returns (allReady, externalIP, message).
func (p *GlobalProxyNlbPlugin) checkReady(ctx context.Context, c client.Client, pod *corev1.Pod, cfg *proxyConfig, hcName, besName, tpName, addrName, frName, fwName string) (bool, string, string) {
	hc := &gcpv1beta1.ComputeHealthCheck{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: pod.Namespace, Name: hcName}, hc); err != nil {
		return false, "", fmt.Sprintf("get HC: %v", err)
	}
	if !IsKCCReady(hc.Status.Conditions, derefInt64(hc.Status.ObservedGeneration), hc.Generation) {
		return false, "", "HealthCheck not Ready"
	}
	bes := &gcpv1beta1.ComputeBackendService{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: pod.Namespace, Name: besName}, bes); err != nil {
		return false, "", fmt.Sprintf("get BES: %v", err)
	}
	if !IsKCCReady(bes.Status.Conditions, derefInt64(bes.Status.ObservedGeneration), bes.Generation) {
		return false, "", "BackendService not Ready"
	}
	tp := &gcpv1beta1.ComputeTargetTCPProxy{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: pod.Namespace, Name: tpName}, tp); err != nil {
		return false, "", fmt.Sprintf("get TargetTcpProxy: %v", err)
	}
	if !IsKCCReady(tp.Status.Conditions, derefInt64(tp.Status.ObservedGeneration), tp.Generation) {
		return false, "", "TargetTCPProxy not Ready"
	}
	fw := &gcpv1beta1.ComputeFirewall{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: pod.Namespace, Name: fwName}, fw); err != nil {
		return false, "", fmt.Sprintf("get FW: %v", err)
	}
	if !IsKCCReady(fw.Status.Conditions, derefInt64(fw.Status.ObservedGeneration), fw.Generation) {
		return false, "", "Firewall not Ready"
	}
	fr := &gcpv1beta1.ComputeForwardingRule{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: pod.Namespace, Name: frName}, fr); err != nil {
		return false, "", fmt.Sprintf("get FR: %v", err)
	}
	if !IsKCCReady(fr.Status.Conditions, derefInt64(fr.Status.ObservedGeneration), fr.Generation) {
		return false, "", "ForwardingRule not Ready"
	}
	addrIP, addrReady, _ := WaitForAddressReady(ctx, c, types.NamespacedName{Namespace: pod.Namespace, Name: addrName})
	if !addrReady {
		return false, "", "Address not Ready"
	}
	if addrIP == "" {
		return false, "", "Address Ready but IP empty"
	}
	return true, addrIP, "all KCC objects Ready"
}

// applyProjectAndOwner stamps the project-id annotation, managed-by label, and
// (when owner != nil) the GameServerSet OwnerReference on a KCC CR. Anchoring
// on the GSS keeps the resource alive across Pod recreate; owner is nil when
// RetainOnDelete=true so the resource outlives even the GSS.
func applyProjectAndOwner(obj client.Object, projectID, gssName string, owner *metav1.OwnerReference) {
	labels := obj.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	labels[ResourceTagKey] = ResourceTagValue
	labels[gamekruiseiov1alpha1.GameServerOwnerGssKey] = gssName
	obj.SetLabels(labels)
	if projectID != "" {
		anns := obj.GetAnnotations()
		if anns == nil {
			anns = map[string]string{}
		}
		anns[ProjectIDAnnotation] = projectID
		obj.SetAnnotations(anns)
	}
	if owner != nil {
		obj.SetOwnerReferences([]metav1.OwnerReference{*owner})
	}
}

func init() {
	googleCloudProvider.registerPlugin(&GlobalProxyNlbPlugin{})
}
