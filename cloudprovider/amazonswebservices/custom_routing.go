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
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/globalaccelerator"
	gatypes "github.com/aws/aws-sdk-go-v2/service/globalaccelerator/types"
	smithy "github.com/aws/smithy-go"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	log "k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/cloudprovider"
	cperrors "github.com/openkruise/kruise-game/cloudprovider/errors"
	"github.com/openkruise/kruise-game/cloudprovider/utils"
)

const (
	// GlobalAcceleratorCustomRoutingNetwork is the networkType that selects this plugin.
	GlobalAcceleratorCustomRoutingNetwork = "AmazonWebServices-GlobalAcceleratorCustomRouting"
	// AliasCustomRouting is the cross-cloud alias for the custom routing plugin.
	AliasCustomRouting = "GlobalAccelerator-CustomRouting"

	// networkConf keys (see README for details).
	CustomRoutingEndpointGroupArnConfigName = "CustomRoutingEndpointGroupArn"
	CustomRoutingGamePortConfigName         = "GamePort"
	CustomRoutingProtocolConfigName         = "Protocol"
	CustomRoutingEndpointIdConfigName       = "EndpointId"
	// CustomRoutingEndpointIdsConfigName is the multi-AZ alternative to
	// EndpointId: a comma-separated list of "subnetID=CIDR" pairs. The plugin
	// matches each Pod's IP against the CIDR of every entry and uses the
	// matching subnet as the EndpointId when calling Allow / Deny. Mutually
	// exclusive with the singular EndpointId. Example:
	//   EndpointIds: "subnet-aaa=10.0.11.0/24,subnet-bbb=10.0.12.0/24,subnet-ccc=10.0.13.0/24"
	CustomRoutingEndpointIdsConfigName = "EndpointIds"
	CustomRoutingRegionConfigName      = "Region"
	CustomRoutingFixedConfigName       = "Fixed"

	// gamePortName is the NetworkPort name used in the published NetworkStatus.
	gamePortName = "game"

	// defaultAGARegion is where the Global Accelerator control plane lives. All
	// custom routing API calls (Allow/Deny/ListPortMappings) must be issued
	// against this region regardless of the cluster's own region, otherwise they
	// hit an endpoint that has no AGA control plane. Overridable via the Region
	// networkConf key for partitions / future control-plane regions.
	defaultAGARegion = "us-west-2"
)

// customRoutingAPI abstracts the subset of the AWS Global Accelerator custom
// routing API (aws-sdk-go-v2) that this plugin needs. It is satisfied directly
// by the concrete *globalaccelerator.Client and is mocked in unit tests.
type customRoutingAPI interface {
	AllowCustomRoutingTraffic(ctx context.Context, params *globalaccelerator.AllowCustomRoutingTrafficInput, optFns ...func(*globalaccelerator.Options)) (*globalaccelerator.AllowCustomRoutingTrafficOutput, error)
	DenyCustomRoutingTraffic(ctx context.Context, params *globalaccelerator.DenyCustomRoutingTrafficInput, optFns ...func(*globalaccelerator.Options)) (*globalaccelerator.DenyCustomRoutingTrafficOutput, error)
	ListCustomRoutingPortMappingsByDestination(ctx context.Context, params *globalaccelerator.ListCustomRoutingPortMappingsByDestinationInput, optFns ...func(*globalaccelerator.Options)) (*globalaccelerator.ListCustomRoutingPortMappingsByDestinationOutput, error)
	// ListCustomRoutingPortMappings paginates ALL mappings on an endpoint group
	// (across every subnet endpoint). Used at Init for orphan detection: an
	// ALLOW state entry whose destination IP doesn't correspond to any live
	// Pod (typically left behind by a controller crash between an OKG Pod
	// delete event and DenyCustomRoutingTraffic) is reconciled away.
	ListCustomRoutingPortMappings(ctx context.Context, params *globalaccelerator.ListCustomRoutingPortMappingsInput, optFns ...func(*globalaccelerator.Options)) (*globalaccelerator.ListCustomRoutingPortMappingsOutput, error)
}

// subnetMatch is one entry in customRoutingConfig.subnets. cidr is nil only
// for the legacy single-subnet form (EndpointId), where the user explicitly
// pinned the GSS to one subnet and no Pod-IP-to-subnet matching is needed.
type subnetMatch struct {
	id   string
	cidr *net.IPNet
}

// customRoutingConfig is the parsed per-GameServerSet networkConf.
type customRoutingConfig struct {
	endpointGroupArn string
	gamePort         int32
	protocol         corev1.Protocol
	// subnets carries the candidate VPC subnet endpoints. For the legacy
	// EndpointId form it holds exactly one entry with cidr=nil. For the
	// multi-subnet EndpointIds form it holds N entries each with a parsed
	// CIDR so reconcile can match Pod IP -> subnet.
	subnets []subnetMatch
	// region overrides the AGA control-plane region (default us-west-2).
	region string
	// fixed is accepted for parity with the NLB plugin's networkConf surface
	// but is a NO-OP in custom routing: accelerator ports are statically
	// generated by AGA at EG creation time (subnet IP × dest port × proto
	// fully enumerated), so there is no per-pod port to "pin". Parsed only
	// to issue a clear WARNING and to NOT reject the otherwise-valid config.
	fixed bool
}

// resolveSubnet returns the EndpointId (subnet ID) that contains podIP.
//
//   - Legacy single-subnet form (EndpointId): there is exactly one entry with
//     a nil CIDR; that subnet is returned unconditionally (the user has
//     already pinned the GSS to one subnet via nodeAffinity / topology spread).
//   - Multi-subnet form (EndpointIds): the Pod IP is parsed and matched
//     against every entry's CIDR; the first match is returned. If no entry
//     matches, an error is returned (typically meaning the Pod was scheduled
//     into a node whose subnet is not declared in EndpointIds — usually a
//     misconfiguration the user must fix).
func (c *customRoutingConfig) resolveSubnet(podIP string) (string, error) {
	if len(c.subnets) == 1 && c.subnets[0].cidr == nil {
		return c.subnets[0].id, nil
	}
	ip := net.ParseIP(podIP)
	if ip == nil {
		return "", fmt.Errorf("invalid pod IP %q", podIP)
	}
	for _, s := range c.subnets {
		if s.cidr != nil && s.cidr.Contains(ip) {
			return s.id, nil
		}
	}
	ids := make([]string, 0, len(c.subnets))
	for _, s := range c.subnets {
		ids = append(ids, s.id)
	}
	return "", fmt.Errorf("pod IP %s does not fall within any of the configured EndpointIds %v", podIP, ids)
}

// allocatedEndpoint caches the resolved state for a pod so that OnPodUpdated can
// be a no-op while nothing changes, OnPodUpdated can deny the previous IP/EG
// when those change, and OnPodDeleted knows what to deny even if the GSS
// (and therefore the parsable networkConf) has already disappeared.
type allocatedEndpoint struct {
	// Persistence-equivalent fields: these are the minimum needed to reconstruct
	// what Allow was done so OnPodDeleted can issue the matching Deny without
	// re-parsing the networkConf.
	endpointGroupArn string
	endpointId       string
	podIP            string
	gamePort         int32
	protocol         corev1.Protocol
	region           string
	// externalAddresses is the result of ListCustomRoutingPortMappingsByDestination
	// at Allow time; empty when reconstructed at Init (will be backfilled on
	// the next OnPodUpdated reconcile via lookup).
	externalAddresses []gamekruiseiov1alpha1.NetworkAddress
}

func (a *allocatedEndpoint) configKey() string {
	return strings.Join([]string{a.endpointGroupArn, a.endpointId, strconv.Itoa(int(a.gamePort)), string(a.protocol)}, "|")
}

type CustomRoutingPlugin struct {
	cache        map[string]*allocatedEndpoint // podKey(ns/name) -> resolved endpoint
	mutex        sync.RWMutex
	aga          customRoutingAPI
	newAGAClient func(ctx context.Context, region string) (customRoutingAPI, error)
}

func (p *CustomRoutingPlugin) Name() string {
	return GlobalAcceleratorCustomRoutingNetwork
}

func (p *CustomRoutingPlugin) Alias() string {
	return AliasCustomRouting
}

// Init mirrors the cluster-state recovery pattern of nlb.go:initLbCache, but
// since this plugin does NOT create any K8s persistence object (no Service /
// TargetGroupBinding), the persistence layer is the Pod annotation itself
// (game.kruise.io/network-type + game.kruise.io/network-conf, written by the
// GameServerSet controller before the pod is scheduled). On controller
// restart we:
//
//  1. List all Pods cluster-wide and rebuild p.cache from those whose
//     network-type equals this plugin. This restores the (podKey -> endpoint)
//     mapping so a subsequent OnPodDeleted can issue the matching Deny even
//     after the GSS has been removed.
//  2. For every (endpointGroupArn, endpointId) referenced by a live Pod,
//     enumerate the AGA mappings filtered to DestinationTrafficState=ALLOW
//     and Deny any destination IP that has no live Pod. This cleans up
//     orphan Allow entries left by a controller crash between an OKG Pod
//     delete event and the matching DenyCustomRoutingTraffic call.
//
// Step (2) is best-effort: failure to reach AGA (e.g. transient IAM /
// network) is logged and Init still succeeds, because the lazy path
// (OnPodUpdated re-issuing Allow + Lookup) keeps the steady state correct.
func (p *CustomRoutingPlugin) Init(c client.Client, options cloudprovider.CloudProviderOptions, ctx context.Context) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.cache == nil {
		p.cache = make(map[string]*allocatedEndpoint)
	}

	// (1) List Pods, rebuild cache.
	podList := &corev1.PodList{}
	if err := c.List(ctx, podList); err != nil {
		// List failure on Init is a hard error: without the cluster state we
		// risk doing the wrong thing on subsequent Deletes. Mirrors nlb.go.
		return cperrors.ToPluginError(err, cperrors.InternalError)
	}

	// Group live (egArn, endpointId) -> set of podIPs allowed, for orphan diff.
	type egKey struct{ egArn, endpointId, region string }
	live := map[egKey]map[string]bool{}

	recovered := 0
	for i := range podList.Items {
		pod := &podList.Items[i]
		if pod.DeletionTimestamp != nil {
			continue
		}
		nt := pod.Annotations[gamekruiseiov1alpha1.GameServerNetworkType]
		if nt != GlobalAcceleratorCustomRoutingNetwork {
			continue
		}
		nm := utils.NewNetworkManager(pod, c)
		if nm == nil {
			continue
		}
		conf, err := parseCustomRoutingConfig(nm.GetNetworkConfig())
		if err != nil {
			log.Warningf("[%s] init: skip pod %s/%s with unparsable networkConf: %v",
				GlobalAcceleratorCustomRoutingNetwork, pod.Namespace, pod.Name, err)
			continue
		}
		if pod.Status.PodIP == "" {
			// No IP yet; reconcile() will pick it up later.
			continue
		}
		resolvedSubnet, err := conf.resolveSubnet(pod.Status.PodIP)
		if err != nil {
			// Pod is scheduled on a node whose subnet is not in the configured
			// list. The next reconcile will surface the same error; skip Init
			// recovery for this pod so we don't allocate against the wrong
			// subnet.
			log.Warningf("[%s] init: skip pod %s/%s — %v",
				GlobalAcceleratorCustomRoutingNetwork, pod.Namespace, pod.Name, err)
			continue
		}
		podKey := pod.Namespace + "/" + pod.Name
		p.cache[podKey] = &allocatedEndpoint{
			endpointGroupArn: conf.endpointGroupArn,
			endpointId:       resolvedSubnet,
			podIP:            pod.Status.PodIP,
			gamePort:         conf.gamePort,
			protocol:         conf.protocol,
			region:           conf.region,
			// externalAddresses left empty: will be lazily backfilled by the
			// next OnPodUpdated reconcile via ListCustomRoutingPortMappingsByDestination.
		}
		recovered++
		k := egKey{conf.endpointGroupArn, resolvedSubnet, conf.region}
		if live[k] == nil {
			live[k] = map[string]bool{}
		}
		live[k][pod.Status.PodIP] = true
	}

	log.Infof("[%s] init: recovered %d live allocations from cluster state (%d pods scanned)",
		GlobalAcceleratorCustomRoutingNetwork, recovered, len(podList.Items))

	// (2) Orphan cleanup per (egArn, endpointId).
	for k, liveSet := range live {
		aga, perr := p.ensureClientLocked(ctx, k.region)
		if perr != nil {
			log.Warningf("[%s] init: orphan cleanup skipped for %s/%s: %v",
				GlobalAcceleratorCustomRoutingNetwork, k.egArn, k.endpointId, perr)
			continue
		}
		if err := p.cleanupOrphansOnEG(ctx, aga, k.egArn, k.endpointId, liveSet); err != nil {
			log.Warningf("[%s] init: orphan cleanup error for %s/%s: %v",
				GlobalAcceleratorCustomRoutingNetwork, k.egArn, k.endpointId, awsErrMessage(err))
		}
	}
	return nil
}

// cleanupOrphansOnEG enumerates ALLOW mappings on the given (egArn, endpointId)
// and Denies anything not in liveSet. liveSet is keyed by Pod IP.
//
// AGA returns one PortMapping per (destIP × destPort), so a single Pod IP with
// one destination port produces one entry, but a Pod IP that was Allowed for
// multiple ports produces multiple entries. We deduplicate Deny calls per
// (destIP, destPort).
//
// Note on the listing API: AWS Global Accelerator's
// ListCustomRoutingPortMappings requires the AcceleratorArn parameter (the
// EndpointGroupArn is an optional filter on top). The AcceleratorArn is a
// strict prefix of the EndpointGroupArn — everything before "/listener/" — so
// we derive it lexically rather than making an extra DescribeEndpointGroup
// round trip.
func (p *CustomRoutingPlugin) cleanupOrphansOnEG(ctx context.Context, aga customRoutingAPI, egArn, endpointId string, liveSet map[string]bool) error {
	acceleratorArn := egArn
	if idx := strings.Index(egArn, "/listener/"); idx > 0 {
		acceleratorArn = egArn[:idx]
	}

	type denyKey struct {
		ip   string
		port int32
	}
	toDeny := map[denyKey]bool{}

	var nextToken *string
	for {
		out, err := aga.ListCustomRoutingPortMappings(ctx, &globalaccelerator.ListCustomRoutingPortMappingsInput{
			AcceleratorArn:   aws.String(acceleratorArn),
			EndpointGroupArn: aws.String(egArn),
			NextToken:        nextToken,
		})
		if err != nil {
			return err
		}
		for _, m := range out.PortMappings {
			if m.EndpointGroupArn == nil || *m.EndpointGroupArn != egArn {
				continue
			}
			if m.EndpointId == nil || *m.EndpointId != endpointId {
				continue
			}
			if m.DestinationTrafficState != gatypes.CustomRoutingDestinationTrafficStateAllow {
				continue
			}
			if m.DestinationSocketAddress == nil || m.DestinationSocketAddress.IpAddress == nil || m.DestinationSocketAddress.Port == nil {
				continue
			}
			ip := *m.DestinationSocketAddress.IpAddress
			if liveSet[ip] {
				continue
			}
			toDeny[denyKey{ip: ip, port: *m.DestinationSocketAddress.Port}] = true
		}
		if out.NextToken == nil || *out.NextToken == "" {
			break
		}
		nextToken = out.NextToken
	}

	// Group by IP to batch ports per Deny call.
	byIP := map[string][]int32{}
	for k := range toDeny {
		byIP[k.ip] = append(byIP[k.ip], k.port)
	}
	for ip, ports := range byIP {
		if _, err := aga.DenyCustomRoutingTraffic(ctx, &globalaccelerator.DenyCustomRoutingTrafficInput{
			EndpointGroupArn:     aws.String(egArn),
			EndpointId:           aws.String(endpointId),
			DestinationAddresses: []string{ip},
			DestinationPorts:     ports,
		}); err != nil {
			log.Warningf("[%s] init: orphan deny %s on %s/%s failed: %v",
				GlobalAcceleratorCustomRoutingNetwork, ip, egArn, endpointId, awsErrMessage(err))
			continue
		}
		log.Infof("[%s] init: orphan denied %s ports %v on %s/%s",
			GlobalAcceleratorCustomRoutingNetwork, ip, ports, egArn, endpointId)
	}
	return nil
}

// ensureClientLocked returns the (lazily constructed) AGA client. Caller MUST
// already hold p.mutex.
func (p *CustomRoutingPlugin) ensureClientLocked(ctx context.Context, region string) (customRoutingAPI, cperrors.PluginError) {
	if p.aga != nil {
		return p.aga, nil
	}
	newClient := p.newAGAClient
	if newClient == nil {
		newClient = defaultAGAClient
	}
	aga, err := newClient(ctx, region)
	if err != nil {
		return nil, cperrors.ToPluginError(fmt.Errorf("failed to init global accelerator client: %v", err), cperrors.InternalError)
	}
	p.aga = aga
	return p.aga, nil
}

func (p *CustomRoutingPlugin) getClient(ctx context.Context, region string) (customRoutingAPI, cperrors.PluginError) {
	p.mutex.RLock()
	existing := p.aga
	p.mutex.RUnlock()
	if existing != nil {
		return existing, nil
	}
	p.mutex.Lock()
	defer p.mutex.Unlock()
	return p.ensureClientLocked(ctx, region)
}

func defaultAGAClient(ctx context.Context, region string) (customRoutingAPI, error) {
	if region == "" {
		region = defaultAGARegion
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, err
	}
	return globalaccelerator.NewFromConfig(cfg), nil
}

func (p *CustomRoutingPlugin) OnPodAdded(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	return p.reconcile(c, pod, ctx)
}

func (p *CustomRoutingPlugin) OnPodUpdated(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	return p.reconcile(c, pod, ctx)
}

// reconcile is shared by OnPodAdded and OnPodUpdated. It is idempotent: the pod
// IP is allowed on the user-specified custom routing endpoint, the deterministic
// port mapping is looked up, and the result is written to the network-status
// annotation.
//
// State machine (mirrors nlb.go:OnPodUpdated):
//
//   - parse + validate networkConf  → ParameterError if invalid
//   - if pod has no IP yet          → publish NotReady, return (no error)
//   - FAST PATH: cache hit AND
//     (cached.podIP == podIP) AND
//     (cached.configKey() == conf.configKey()) AND
//     (cached.externalAddresses non-empty)
//     → publish Ready from cache, return
//   - DETECT TRANSITIONS:
//     · cached.configKey() != conf.configKey() →
//     Deny on the OLD (egArn, endpointId) for cached.podIP & cached.gamePort
//     · cached.podIP != podIP →
//     Deny on the SAME EG for cached.podIP
//     (intentionally before allocating the new one to free quota slots)
//   - Allow on the new (egArn, endpointId, podIP, gamePort)
//   - List port mappings for the new (endpointId, podIP) to learn the
//     accelerator IPs/ports; if empty (eventual consistency) → publish
//     NotReady, return (no error)
//   - Cache the new (configKey + externalAddresses)
//   - Publish Ready with the new externalAddresses
//
// Because this plugin runs inside a mutating webhook, returning an error makes
// OKG discard the mutated Pod (DeepCopy) and deny admission, which also
// swallows any NetworkNotReady status we wrote. So transient "not yet ready"
// conditions (no Pod IP, mapping not visible yet) publish NetworkNotReady and
// return (pod, nil); only genuine API/parameter failures return a PluginError.
func (p *CustomRoutingPlugin) reconcile(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	networkManager := utils.NewNetworkManager(pod, c)
	if networkManager == nil {
		return pod, nil
	}

	conf, err := parseCustomRoutingConfig(networkManager.GetNetworkConfig())
	if err != nil {
		return pod, cperrors.NewPluginErrorWithMessage(cperrors.ParameterError, err.Error())
	}
	if conf.fixed {
		// Custom Routing port mappings are statically generated by AGA at EG
		// creation time, so "Fixed=true" has no enforceable effect here.
		// Surface the misconfiguration as a log line, then ignore.
		log.Warningf("[%s] pod %s/%s: networkConf.%s=true has no effect in custom routing (port mappings are statically generated by AGA)",
			GlobalAcceleratorCustomRoutingNetwork, pod.Namespace, pod.Name, CustomRoutingFixedConfigName)
	}

	podKey := pod.Namespace + "/" + pod.Name
	podIP := pod.Status.PodIP
	if podIP == "" {
		// Pod not scheduled / IP not assigned yet; stay NotReady and retry.
		return p.publishNotReady(networkManager, pod)
	}

	// Resolve which configured subnet this Pod's IP belongs to. For the legacy
	// single-EndpointId form this is the user-supplied subnet directly; for
	// EndpointIds (multi-subnet) the Pod IP is matched against each entry's
	// CIDR. A mismatch (Pod scheduled into a subnet not in EndpointIds) is a
	// genuine user-side configuration error — surface it as a ParameterError
	// so OKG events show the cause.
	resolvedSubnet, rerr := conf.resolveSubnet(podIP)
	if rerr != nil {
		return pod, cperrors.NewPluginErrorWithMessage(cperrors.ParameterError, rerr.Error())
	}
	// configKey is computed from the RESOLVED subnet (per-Pod), not the
	// possibly-multi-subnet list, so a Pod that stays inside its resolved
	// subnet across config edits is treated as unchanged.
	configKey := func(egArn, subnetId string, port int32, proto corev1.Protocol) string {
		return strings.Join([]string{egArn, subnetId, strconv.Itoa(int(port)), string(proto)}, "|")
	}
	currentConfigKey := configKey(conf.endpointGroupArn, resolvedSubnet, conf.gamePort, conf.protocol)

	cached := p.getCache(podKey)

	// FAST PATH: nothing has changed and we already know the answer.
	if cached != nil &&
		cached.podIP == podIP &&
		cached.configKey() == currentConfigKey &&
		len(cached.externalAddresses) > 0 {
		return p.publishReady(networkManager, pod, conf, podIP, cached.externalAddresses)
	}

	aga, perr := p.getClient(ctx, conf.region)
	if perr != nil {
		return pod, perr
	}

	// DETECT TRANSITIONS: clean up stale Allow on the OLD identity first, to
	// free the (limited) per-EG mapping capacity.
	if cached != nil {
		switch {
		case cached.configKey() != currentConfigKey:
			// Config changed (EG, subnet, port, protocol). Deny on the OLD
			// destination, which may live on a different EG / subnet entirely.
			if cached.podIP != "" && cached.endpointGroupArn != "" && cached.endpointId != "" {
				if err := p.denyTraffic(ctx, aga, cached.endpointGroupArn, cached.endpointId, cached.podIP, cached.gamePort); err != nil {
					log.Warningf("[%s] failed to deny stale config for pod %s (eg=%s ep=%s ip=%s:%d): %v",
						GlobalAcceleratorCustomRoutingNetwork, podKey, cached.endpointGroupArn, cached.endpointId, cached.podIP, cached.gamePort, awsErrMessage(err))
				} else {
					log.Infof("[%s] pod %s: config changed, denied OLD (eg=%s ep=%s ip=%s:%d)",
						GlobalAcceleratorCustomRoutingNetwork, podKey, cached.endpointGroupArn, cached.endpointId, cached.podIP, cached.gamePort)
				}
			}
		case cached.podIP != "" && cached.podIP != podIP:
			// Pod IP changed but everything else identical; same-EG Deny is enough.
			if err := p.denyTraffic(ctx, aga, conf.endpointGroupArn, resolvedSubnet, cached.podIP, conf.gamePort); err != nil {
				log.Warningf("[%s] failed to deny stale ip %s on %s/%s: %v",
					GlobalAcceleratorCustomRoutingNetwork, cached.podIP, conf.endpointGroupArn, resolvedSubnet, awsErrMessage(err))
			}
		}
	}

	// Allow traffic to the Pod IP on the resolved subnet endpoint.
	if _, err := aga.AllowCustomRoutingTraffic(ctx, &globalaccelerator.AllowCustomRoutingTrafficInput{
		EndpointGroupArn:     aws.String(conf.endpointGroupArn),
		EndpointId:           aws.String(resolvedSubnet),
		DestinationAddresses: []string{podIP},
		DestinationPorts:     []int32{conf.gamePort},
	}); err != nil {
		return pod, cperrors.NewPluginErrorWithMessage(cperrors.ApiCallError,
			fmt.Sprintf("failed to allow custom routing traffic for %s on %s: %v", podIP, resolvedSubnet, awsErrMessage(err)))
	}

	externalAddresses, err := p.lookupExternalAddresses(ctx, aga, conf, resolvedSubnet, podIP)
	if err != nil {
		return pod, cperrors.NewPluginErrorWithMessage(cperrors.ApiCallError,
			fmt.Sprintf("failed to list custom routing port mappings for %s: %v", podIP, awsErrMessage(err)))
	}
	if len(externalAddresses) == 0 {
		// Mapping not visible yet (eventual consistency). Cache the partial
		// state so OnPodDeleted can still Deny, then publish NotReady.
		p.setCache(podKey, &allocatedEndpoint{
			endpointGroupArn: conf.endpointGroupArn,
			endpointId:       resolvedSubnet,
			podIP:            podIP,
			gamePort:         conf.gamePort,
			protocol:         conf.protocol,
			region:           conf.region,
		})
		return p.publishNotReady(networkManager, pod)
	}

	p.setCache(podKey, &allocatedEndpoint{
		endpointGroupArn:  conf.endpointGroupArn,
		endpointId:        resolvedSubnet,
		podIP:             podIP,
		gamePort:          conf.gamePort,
		protocol:          conf.protocol,
		region:            conf.region,
		externalAddresses: externalAddresses,
	})

	return p.publishReady(networkManager, pod, conf, podIP, externalAddresses)
}

// expandProtocols returns the list of concrete protocols a customRoutingConfig
// translates to when emitting NetworkStatus entries. ProtocolTCPUDP (synthetic
// "both TCP and UDP on the same port", mirrored from the NLB plugin) expands
// to {TCP, UDP}; anything else returns itself. The AGA control plane treats
// (subnet IP, destination port) as a single mapping unit regardless of
// protocol — the protocol is selected by the EG's destination-configurations
// at EG creation time — so one AllowCustomRoutingTraffic call covers both
// protocols simultaneously. We only fan out at the NetworkStatus layer so
// game clients can read an unambiguous per-protocol view.
func expandProtocols(p corev1.Protocol) []corev1.Protocol {
	if p == ProtocolTCPUDP {
		return []corev1.Protocol{corev1.ProtocolTCP, corev1.ProtocolUDP}
	}
	return []corev1.Protocol{p}
}

// gamePortNameFor returns the NetworkPort.Name for a given protocol. Single-
// protocol GSS keep the historical "game" name; ProtocolTCPUDP-derived entries
// disambiguate as "game-tcp" and "game-udp" so they remain uniquely named
// inside a single NetworkAddress.Ports list.
func gamePortNameFor(declared, concrete corev1.Protocol) string {
	if declared == ProtocolTCPUDP {
		return gamePortName + "-" + strings.ToLower(string(concrete))
	}
	return gamePortName
}

// lookupExternalAddresses queries the deterministic port mappings for podIP on
// the resolved subnet and builds one ExternalAddress per accelerator static IP /
// mapped port, paging through all results.
func (p *CustomRoutingPlugin) lookupExternalAddresses(ctx context.Context, aga customRoutingAPI, conf *customRoutingConfig, endpointId, podIP string) ([]gamekruiseiov1alpha1.NetworkAddress, error) {
	concreteProtocols := expandProtocols(conf.protocol)
	externalAddresses := make([]gamekruiseiov1alpha1.NetworkAddress, 0)
	var nextToken *string
	for {
		out, err := aga.ListCustomRoutingPortMappingsByDestination(ctx,
			&globalaccelerator.ListCustomRoutingPortMappingsByDestinationInput{
				EndpointId:         aws.String(endpointId),
				DestinationAddress: aws.String(podIP),
				NextToken:          nextToken,
			})
		if err != nil {
			return nil, err
		}
		for _, mapping := range out.DestinationPortMappings {
			if mapping.DestinationSocketAddress == nil || mapping.DestinationSocketAddress.Port == nil {
				continue
			}
			// Only the mapping for the requested game port is relevant.
			if *mapping.DestinationSocketAddress.Port != conf.gamePort {
				continue
			}
			for _, sa := range mapping.AcceleratorSocketAddresses {
				if sa.IpAddress == nil || sa.Port == nil {
					continue
				}
				mappedPort := intstr.FromInt(int(*sa.Port))
				// Emit one NetworkPort per concrete protocol the GSS declares.
				// For ProtocolTCPUDP this fans out to TCP+UDP entries that
				// share the same accelerator IP/port — matching the real AGA
				// data plane where (IP, port) carries both protocols.
				ports := make([]gamekruiseiov1alpha1.NetworkPort, 0, len(concreteProtocols))
				for _, proto := range concreteProtocols {
					p := mappedPort
					ports = append(ports, gamekruiseiov1alpha1.NetworkPort{
						Name:     gamePortNameFor(conf.protocol, proto),
						Protocol: proto,
						Port:     &p,
					})
				}
				externalAddresses = append(externalAddresses, gamekruiseiov1alpha1.NetworkAddress{
					IP:    *sa.IpAddress,
					Ports: ports,
				})
			}
		}
		if out.NextToken == nil || *out.NextToken == "" {
			break
		}
		nextToken = out.NextToken
	}
	return externalAddresses, nil
}

// denyTraffic removes the Allow entry for (destIP, destPort) on the given
// (egArn, endpointId).
func (p *CustomRoutingPlugin) denyTraffic(ctx context.Context, aga customRoutingAPI, egArn, endpointId, destIP string, destPort int32) error {
	_, err := aga.DenyCustomRoutingTraffic(ctx, &globalaccelerator.DenyCustomRoutingTrafficInput{
		EndpointGroupArn:     aws.String(egArn),
		EndpointId:           aws.String(endpointId),
		DestinationAddresses: []string{destIP},
		DestinationPorts:     []int32{destPort},
	})
	return err
}

func (p *CustomRoutingPlugin) publishNotReady(networkManager *utils.NetworkManager, pod *corev1.Pod) (*corev1.Pod, cperrors.PluginError) {
	pod, err := networkManager.UpdateNetworkStatus(gamekruiseiov1alpha1.NetworkStatus{
		CurrentNetworkState: gamekruiseiov1alpha1.NetworkNotReady,
	}, pod)
	return pod, cperrors.ToPluginError(err, cperrors.InternalError)
}

func (p *CustomRoutingPlugin) publishReady(networkManager *utils.NetworkManager, pod *corev1.Pod,
	conf *customRoutingConfig, podIP string, externalAddresses []gamekruiseiov1alpha1.NetworkAddress) (*corev1.Pod, cperrors.PluginError) {
	gamePort := intstr.FromInt(int(conf.gamePort))
	// Internal addresses mirror the protocol fan-out used for externalAddresses
	// so business code reading either gets a consistent multi-protocol view.
	internalProtocols := expandProtocols(conf.protocol)
	internalPorts := make([]gamekruiseiov1alpha1.NetworkPort, 0, len(internalProtocols))
	for _, proto := range internalProtocols {
		p := gamePort
		internalPorts = append(internalPorts, gamekruiseiov1alpha1.NetworkPort{
			Name:     gamePortNameFor(conf.protocol, proto),
			Protocol: proto,
			Port:     &p,
		})
	}
	networkStatus := gamekruiseiov1alpha1.NetworkStatus{
		InternalAddresses: []gamekruiseiov1alpha1.NetworkAddress{
			{
				IP:    podIP,
				Ports: internalPorts,
			},
		},
		ExternalAddresses:   externalAddresses,
		CurrentNetworkState: gamekruiseiov1alpha1.NetworkReady,
	}
	pod, err := networkManager.UpdateNetworkStatus(networkStatus, pod)
	return pod, cperrors.ToPluginError(err, cperrors.InternalError)
}

// OnPodDeleted issues Deny matching the prior Allow. Robustness contract:
//
//  1. Prefer the in-memory cache (populated by reconcile OR Init recovery).
//     This is the only path that survives "GSS already deleted, then pod
//     deleted" because the networkConf annotation may have been wiped from
//     the Pod by the GSS controller during cascade delete.
//
//  2. Fall back to parsing the Pod's networkConf annotation. This handles the
//     happy path where the GSS is still around (or its annotations remain on
//     the Pod) and the cache happens to be cold (cluster-scoped restart edge).
//
//  3. If neither has enough info, log a warning and return nil. Init-time
//     orphan cleanup will catch this on the next controller restart.
//
// Errors from AWS bubble up as ApiCallError (retryable). Parameter errors are
// downgraded to a warning log: there is nothing the user can do to make a
// dropped pod re-parseable.
func (p *CustomRoutingPlugin) OnPodDeleted(c client.Client, pod *corev1.Pod, ctx context.Context) cperrors.PluginError {
	podKey := pod.Namespace + "/" + pod.Name
	cached := p.getCache(podKey)

	// Source of truth: cache, falling back to parsed conf.
	var egArn, endpointId, podIP, region string
	var port int32
	if cached != nil {
		egArn, endpointId, podIP, port, region = cached.endpointGroupArn, cached.endpointId, cached.podIP, cached.gamePort, cached.region
	}
	if egArn == "" || endpointId == "" || podIP == "" {
		nm := utils.NewNetworkManager(pod, c)
		if nm != nil {
			if conf, perr := parseCustomRoutingConfig(nm.GetNetworkConfig()); perr == nil {
				if egArn == "" {
					egArn = conf.endpointGroupArn
				}
				if endpointId == "" && podIP != "" {
					// Try to resolve from Pod IP first (multi-subnet case).
					if rs, rerr := conf.resolveSubnet(podIP); rerr == nil {
						endpointId = rs
					}
				}
				if endpointId == "" && pod.Status.PodIP != "" {
					if rs, rerr := conf.resolveSubnet(pod.Status.PodIP); rerr == nil {
						endpointId = rs
					}
				}
				if endpointId == "" && len(conf.subnets) == 1 {
					endpointId = conf.subnets[0].id
				}
				if port == 0 {
					port = conf.gamePort
				}
				if region == "" {
					region = conf.region
				}
				if podIP == "" {
					podIP = pod.Status.PodIP
				}
			}
		}
	}

	if egArn == "" || endpointId == "" || podIP == "" || port == 0 {
		log.Warningf("[%s] OnPodDeleted: insufficient info to deny for %s "+
			"(eg=%q endpoint=%q ip=%q port=%d) — relying on Init orphan cleanup on next restart",
			GlobalAcceleratorCustomRoutingNetwork, podKey, egArn, endpointId, podIP, port)
		p.deleteCache(podKey)
		return nil
	}

	aga, perr := p.getClient(ctx, region)
	if perr != nil {
		return perr
	}
	if err := p.denyTraffic(ctx, aga, egArn, endpointId, podIP, port); err != nil {
		// Surface so OKG retries; keep the cache so a retry has the same data.
		return cperrors.NewPluginErrorWithMessage(cperrors.ApiCallError,
			fmt.Sprintf("failed to deny custom routing traffic for %s on %s/%s: %v", podIP, egArn, endpointId, awsErrMessage(err)))
	}
	log.Infof("[%s] pod %s deleted: denied %s:%d on %s/%s",
		GlobalAcceleratorCustomRoutingNetwork, podKey, podIP, port, egArn, endpointId)
	p.deleteCache(podKey)
	return nil
}

func (p *CustomRoutingPlugin) getCache(podKey string) *allocatedEndpoint {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	return p.cache[podKey]
}

func (p *CustomRoutingPlugin) setCache(podKey string, ep *allocatedEndpoint) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	if p.cache == nil {
		p.cache = make(map[string]*allocatedEndpoint)
	}
	p.cache[podKey] = ep
}

func (p *CustomRoutingPlugin) deleteCache(podKey string) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	delete(p.cache, podKey)
}

// awsErrMessage extracts a precise "Code: message" string from a typed AWS SDK
// (smithy) API error, falling back to the raw error string. Used instead of
// brittle substring matching on error text.
func awsErrMessage(err error) string {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		return fmt.Sprintf("%s: %s", apiErr.ErrorCode(), apiErr.ErrorMessage())
	}
	return err.Error()
}

func parseCustomRoutingConfig(conf []gamekruiseiov1alpha1.NetworkConfParams) (*customRoutingConfig, error) {
	c := &customRoutingConfig{
		protocol: corev1.ProtocolUDP,
		region:   defaultAGARegion,
	}
	var singleEndpointId string
	for _, kv := range conf {
		switch kv.Name {
		case CustomRoutingEndpointGroupArnConfigName:
			c.endpointGroupArn = strings.TrimSpace(kv.Value)
		case CustomRoutingGamePortConfigName:
			port, err := strconv.ParseInt(strings.TrimSpace(kv.Value), 10, 32)
			if err != nil {
				return nil, fmt.Errorf("invalid %s %q: %v", CustomRoutingGamePortConfigName, kv.Value, err)
			}
			c.gamePort = int32(port)
		case CustomRoutingProtocolConfigName:
			c.protocol = corev1.Protocol(strings.ToUpper(strings.TrimSpace(kv.Value)))
		case CustomRoutingEndpointIdConfigName:
			singleEndpointId = strings.TrimSpace(kv.Value)
		case CustomRoutingEndpointIdsConfigName:
			if v := strings.TrimSpace(kv.Value); v != "" {
				for _, raw := range strings.Split(v, ",") {
					entry := strings.TrimSpace(raw)
					if entry == "" {
						continue
					}
					parts := strings.SplitN(entry, "=", 2)
					if len(parts) != 2 {
						return nil, fmt.Errorf(`%s entry %q must be "subnet-id=cidr"`,
							CustomRoutingEndpointIdsConfigName, entry)
					}
					sid := strings.TrimSpace(parts[0])
					cidrStr := strings.TrimSpace(parts[1])
					_, ipnet, err := net.ParseCIDR(cidrStr)
					if err != nil {
						return nil, fmt.Errorf("invalid CIDR %q for subnet %q in %s: %v",
							cidrStr, sid, CustomRoutingEndpointIdsConfigName, err)
					}
					c.subnets = append(c.subnets, subnetMatch{id: sid, cidr: ipnet})
				}
			}
		case CustomRoutingRegionConfigName:
			if v := strings.TrimSpace(kv.Value); v != "" {
				c.region = v
			}
		case CustomRoutingFixedConfigName:
			if v, err := strconv.ParseBool(strings.TrimSpace(kv.Value)); err == nil {
				c.fixed = v
			}
		}
	}

	if c.endpointGroupArn == "" {
		return nil, fmt.Errorf("%s is required", CustomRoutingEndpointGroupArnConfigName)
	}
	if c.gamePort <= 0 || c.gamePort > 65535 {
		return nil, fmt.Errorf("%s must be in [1,65535]", CustomRoutingGamePortConfigName)
	}
	// Reconcile EndpointId / EndpointIds. They are mutually exclusive: a single
	// pinned subnet (legacy form, no CIDR needed) vs. a list of candidate
	// subnets with CIDR for Pod-IP-to-subnet resolution.
	if singleEndpointId != "" && len(c.subnets) > 0 {
		return nil, fmt.Errorf("specify either %s or %s, not both",
			CustomRoutingEndpointIdConfigName, CustomRoutingEndpointIdsConfigName)
	}
	if singleEndpointId != "" {
		c.subnets = []subnetMatch{{id: singleEndpointId, cidr: nil}}
	}
	if len(c.subnets) == 0 {
		return nil, fmt.Errorf("either %s or %s is required",
			CustomRoutingEndpointIdConfigName, CustomRoutingEndpointIdsConfigName)
	}
	if c.protocol != corev1.ProtocolTCP && c.protocol != corev1.ProtocolUDP && c.protocol != ProtocolTCPUDP {
		return nil, fmt.Errorf("%s must be TCP, UDP, or TCPUDP", CustomRoutingProtocolConfigName)
	}
	return c, nil
}

// compile-time assertion that the concrete v2 client satisfies customRoutingAPI.
var _ customRoutingAPI = (*globalaccelerator.Client)(nil)

func init() {
	amazonsWebServicesProvider.registerPlugin(&CustomRoutingPlugin{
		cache: make(map[string]*allocatedEndpoint),
	})
}
