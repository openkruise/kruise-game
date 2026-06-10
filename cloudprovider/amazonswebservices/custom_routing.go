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
	"strconv"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/globalaccelerator"
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
	CustomRoutingRegionConfigName           = "Region"

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
}

// customRoutingConfig is the parsed per-GameServerSet networkConf.
type customRoutingConfig struct {
	endpointGroupArn string
	gamePort         int32
	protocol         corev1.Protocol
	// endpointId is the VPC subnet ID the Pod lands in. The user supplies it
	// explicitly (DESIGN §7); the plugin never guesses by trial-and-error.
	endpointId string
	// region overrides the AGA control-plane region (default us-west-2).
	region string
}

// allocatedEndpoint caches the resolved state for a pod so that OnPodUpdated can
// be a no-op while nothing changes, OnPodUpdated can deny the previous IP when
// the Pod IP changes, and OnPodDeleted knows which endpoint to deny.
type allocatedEndpoint struct {
	podIP             string
	endpointId        string
	externalAddresses []gamekruiseiov1alpha1.NetworkAddress
}

type CustomRoutingPlugin struct {
	aga          customRoutingAPI
	cache        map[string]*allocatedEndpoint // podKey(ns/name) -> resolved endpoint
	mutex        sync.RWMutex
	newAGAClient func(ctx context.Context, region string) (customRoutingAPI, error)
}

func (p *CustomRoutingPlugin) Name() string {
	return GlobalAcceleratorCustomRoutingNetwork
}

func (p *CustomRoutingPlugin) Alias() string {
	return AliasCustomRouting
}

func (p *CustomRoutingPlugin) Init(c client.Client, options cloudprovider.CloudProviderOptions, ctx context.Context) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.cache == nil {
		p.cache = make(map[string]*allocatedEndpoint)
	}

	// The AGA client is built lazily on first use (see getClient): its region
	// comes from per-GameServerSet networkConf, which is not available here.
	// Environments not using custom routing (and possibly lacking IAM
	// permissions) therefore never construct it.
	log.Infof("[%s] plugin initialized", GlobalAcceleratorCustomRoutingNetwork)
	return nil
}

// getClient returns the (lazily constructed) AGA client. The client is built
// once with the region anchored to the AGA control plane (us-west-2 by default,
// or the configured override) using the default AWS credential chain (IRSA in
// EKS). Tests inject a client by setting p.aga directly.
func (p *CustomRoutingPlugin) getClient(ctx context.Context, region string) (customRoutingAPI, cperrors.PluginError) {
	p.mutex.RLock()
	existing := p.aga
	p.mutex.RUnlock()
	if existing != nil {
		return existing, nil
	}

	p.mutex.Lock()
	defer p.mutex.Unlock()
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
// Because this plugin runs inside a mutating webhook, returning an error makes
// OKG discard the mutated Pod (DeepCopy) and deny admission, which also swallows
// any NetworkNotReady status we wrote. So transient "not yet ready" conditions
// (no Pod IP, mapping not visible yet) publish NetworkNotReady and return
// (pod, nil); only genuine API/parameter failures return a PluginError.
func (p *CustomRoutingPlugin) reconcile(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	networkManager := utils.NewNetworkManager(pod, c)

	conf, err := parseCustomRoutingConfig(networkManager.GetNetworkConfig())
	if err != nil {
		return pod, cperrors.NewPluginErrorWithMessage(cperrors.ParameterError, err.Error())
	}

	podKey := pod.GetNamespace() + "/" + pod.GetName()
	podIP := pod.Status.PodIP
	if podIP == "" {
		// Pod not scheduled / IP not assigned yet; stay NotReady and retry.
		return p.publishNotReady(networkManager, pod)
	}

	cached := p.getCache(podKey)

	// Fast path: already resolved for this pod IP, just republish.
	if cached != nil && cached.podIP == podIP && len(cached.externalAddresses) > 0 {
		return p.publishReady(networkManager, pod, conf, podIP, cached.externalAddresses)
	}

	aga, perr := p.getClient(ctx, conf.region)
	if perr != nil {
		return pod, perr
	}

	// M3: the Pod was rescheduled with a new IP. Deny the previous IP first so
	// we do not leak the (limited) custom routing mapping capacity.
	if cached != nil && cached.podIP != "" && cached.podIP != podIP {
		if derr := p.denyTraffic(ctx, aga, conf, cached.endpointId, cached.podIP); derr != nil {
			// Best-effort drain of the stale IP: log and continue so the new IP
			// can still be allocated. The stale Allow will be cleaned up on a
			// later OnPodDeleted / reconcile.
			log.Warningf("[%s] failed to deny stale ip %s on endpoint %s: %v",
				GlobalAcceleratorCustomRoutingNetwork, cached.podIP, cached.endpointId, awsErrMessage(derr))
		}
	}

	// Allow traffic to the Pod IP on the user-specified subnet endpoint.
	_, err = aga.AllowCustomRoutingTraffic(ctx, &globalaccelerator.AllowCustomRoutingTrafficInput{
		EndpointGroupArn:     aws.String(conf.endpointGroupArn),
		EndpointId:           aws.String(conf.endpointId),
		DestinationAddresses: []string{podIP},
		DestinationPorts:     []int32{conf.gamePort},
	})
	if err != nil {
		return pod, cperrors.NewPluginErrorWithMessage(cperrors.ApiCallError,
			fmt.Sprintf("failed to allow custom routing traffic for %s on %s: %v", podIP, conf.endpointId, awsErrMessage(err)))
	}

	externalAddresses, err := p.lookupExternalAddresses(ctx, aga, conf, podIP)
	if err != nil {
		return pod, cperrors.NewPluginErrorWithMessage(cperrors.ApiCallError,
			fmt.Sprintf("failed to list custom routing port mappings for %s: %v", podIP, awsErrMessage(err)))
	}
	if len(externalAddresses) == 0 {
		// Mapping not visible yet (eventual consistency). Publish NotReady and
		// let OKG re-enqueue; do NOT return an error from the webhook path.
		return p.publishNotReady(networkManager, pod)
	}

	p.setCache(podKey, &allocatedEndpoint{
		podIP:             podIP,
		endpointId:        conf.endpointId,
		externalAddresses: externalAddresses,
	})

	return p.publishReady(networkManager, pod, conf, podIP, externalAddresses)
}

// lookupExternalAddresses queries the deterministic port mappings for podIP and
// builds one ExternalAddress per accelerator static IP / mapped port, paging
// through all results.
func (p *CustomRoutingPlugin) lookupExternalAddresses(ctx context.Context, aga customRoutingAPI, conf *customRoutingConfig, podIP string) ([]gamekruiseiov1alpha1.NetworkAddress, error) {
	externalAddresses := make([]gamekruiseiov1alpha1.NetworkAddress, 0)
	var nextToken *string
	for {
		out, err := aga.ListCustomRoutingPortMappingsByDestination(ctx,
			&globalaccelerator.ListCustomRoutingPortMappingsByDestinationInput{
				EndpointId:         aws.String(conf.endpointId),
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
				externalAddresses = append(externalAddresses, gamekruiseiov1alpha1.NetworkAddress{
					IP: *sa.IpAddress,
					Ports: []gamekruiseiov1alpha1.NetworkPort{
						{
							Name:     gamePortName,
							Protocol: conf.protocol,
							Port:     &mappedPort,
						},
					},
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

// denyTraffic removes the Allow entry for destIP on the given endpoint.
func (p *CustomRoutingPlugin) denyTraffic(ctx context.Context, aga customRoutingAPI, conf *customRoutingConfig, endpointId, destIP string) error {
	_, err := aga.DenyCustomRoutingTraffic(ctx, &globalaccelerator.DenyCustomRoutingTrafficInput{
		EndpointGroupArn:     aws.String(conf.endpointGroupArn),
		EndpointId:           aws.String(endpointId),
		DestinationAddresses: []string{destIP},
		DestinationPorts:     []int32{conf.gamePort},
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
	networkStatus := gamekruiseiov1alpha1.NetworkStatus{
		InternalAddresses: []gamekruiseiov1alpha1.NetworkAddress{
			{
				IP: podIP,
				Ports: []gamekruiseiov1alpha1.NetworkPort{
					{
						Name:     gamePortName,
						Protocol: conf.protocol,
						Port:     &gamePort,
					},
				},
			},
		},
		ExternalAddresses:   externalAddresses,
		CurrentNetworkState: gamekruiseiov1alpha1.NetworkReady,
	}
	pod, err := networkManager.UpdateNetworkStatus(networkStatus, pod)
	return pod, cperrors.ToPluginError(err, cperrors.InternalError)
}

func (p *CustomRoutingPlugin) OnPodDeleted(c client.Client, pod *corev1.Pod, ctx context.Context) cperrors.PluginError {
	conf, err := parseCustomRoutingConfig(utils.NewNetworkManager(pod, c).GetNetworkConfig())
	if err != nil {
		return cperrors.NewPluginErrorWithMessage(cperrors.ParameterError, err.Error())
	}

	podKey := pod.GetNamespace() + "/" + pod.GetName()

	podIP := pod.Status.PodIP
	endpointId := conf.endpointId
	if cached := p.getCache(podKey); cached != nil {
		if podIP == "" {
			podIP = cached.podIP
		}
		if cached.endpointId != "" {
			endpointId = cached.endpointId
		}
	}
	if podIP == "" {
		// Nothing to deny.
		p.deleteCache(podKey)
		return nil
	}

	aga, perr := p.getClient(ctx, conf.region)
	if perr != nil {
		return perr
	}

	if err := p.denyTraffic(ctx, aga, conf, endpointId, podIP); err != nil {
		// Surface the error so OKG retries the delete hook; keep the cache so a
		// retry still knows the owning endpoint.
		return cperrors.NewPluginErrorWithMessage(cperrors.ApiCallError,
			fmt.Sprintf("failed to deny custom routing traffic for %s on %s: %v", podIP, endpointId, awsErrMessage(err)))
	}

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
			c.endpointId = strings.TrimSpace(kv.Value)
		case CustomRoutingRegionConfigName:
			if v := strings.TrimSpace(kv.Value); v != "" {
				c.region = v
			}
		}
	}

	if c.endpointGroupArn == "" {
		return nil, fmt.Errorf("%s is required", CustomRoutingEndpointGroupArnConfigName)
	}
	if c.gamePort <= 0 || c.gamePort > 65535 {
		return nil, fmt.Errorf("%s must be in [1,65535]", CustomRoutingGamePortConfigName)
	}
	if c.endpointId == "" {
		return nil, fmt.Errorf("%s is required", CustomRoutingEndpointIdConfigName)
	}
	if c.protocol != corev1.ProtocolTCP && c.protocol != corev1.ProtocolUDP {
		return nil, fmt.Errorf("%s must be TCP or UDP", CustomRoutingProtocolConfigName)
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
