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

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/globalaccelerator"
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
	CustomRoutingSubnetIdsConfigName        = "SubnetIds"

	// gamePortName is the NetworkPort name used in the published NetworkStatus.
	gamePortName = "game"
)

// customRoutingAPI abstracts the subset of the AWS Global Accelerator custom
// routing API that this plugin needs. It is satisfied by the concrete
// *globalaccelerator.GlobalAccelerator client and is mocked in unit tests.
type customRoutingAPI interface {
	AllowCustomRoutingTrafficWithContext(ctx aws.Context, input *globalaccelerator.AllowCustomRoutingTrafficInput, opts ...request.Option) (*globalaccelerator.AllowCustomRoutingTrafficOutput, error)
	DenyCustomRoutingTrafficWithContext(ctx aws.Context, input *globalaccelerator.DenyCustomRoutingTrafficInput, opts ...request.Option) (*globalaccelerator.DenyCustomRoutingTrafficOutput, error)
	ListCustomRoutingPortMappingsByDestinationWithContext(ctx aws.Context, input *globalaccelerator.ListCustomRoutingPortMappingsByDestinationInput, opts ...request.Option) (*globalaccelerator.ListCustomRoutingPortMappingsByDestinationOutput, error)
}

// customRoutingConfig is the parsed per-GameServerSet networkConf.
type customRoutingConfig struct {
	endpointGroupArn string
	gamePort         int64
	protocol         corev1.Protocol
	subnetIds        []string
}

// allocatedEndpoint caches the resolved state for a pod so that OnPodUpdated can
// be a no-op while nothing changes and OnPodDeleted knows which subnet to deny.
type allocatedEndpoint struct {
	podIP             string
	subnetId          string
	externalAddresses []gamekruiseiov1alpha1.NetworkAddress
}

type CustomRoutingPlugin struct {
	aga          customRoutingAPI
	cache        map[string]*allocatedEndpoint // podKey(ns/name) -> resolved endpoint
	mutex        sync.RWMutex
	newAGAClient func() (customRoutingAPI, error)
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

	// Build the AGA client lazily so that environments not using custom routing
	// (and possibly lacking IAM permissions) are not forced to construct it.
	// The credentials come from the default AWS chain (IRSA in EKS). Global
	// Accelerator is a global service whose control plane lives in us-west-2,
	// which the SDK selects by default.
	if p.aga == nil {
		newClient := p.newAGAClient
		if newClient == nil {
			newClient = defaultAGAClient
		}
		aga, err := newClient()
		if err != nil {
			return cperrors.ToPluginError(fmt.Errorf("failed to init global accelerator client: %v", err), cperrors.InternalError)
		}
		p.aga = aga
	}

	log.Infof("[%s] plugin initialized", GlobalAcceleratorCustomRoutingNetwork)
	return nil
}

func defaultAGAClient() (customRoutingAPI, error) {
	sess, err := session.NewSession()
	if err != nil {
		return nil, err
	}
	return globalaccelerator.New(sess), nil
}

func (p *CustomRoutingPlugin) OnPodAdded(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	return p.reconcile(c, pod, ctx)
}

func (p *CustomRoutingPlugin) OnPodUpdated(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	return p.reconcile(c, pod, ctx)
}

// reconcile is shared by OnPodAdded and OnPodUpdated. It is idempotent: the pod
// IP is allowed on the custom routing endpoint group, the deterministic port
// mapping is looked up, and the result is written to the network-status
// annotation. When the pod IP is not yet assigned or the mapping cannot be
// resolved, NetworkNotReady is published so OKG keeps retrying.
func (p *CustomRoutingPlugin) reconcile(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	networkManager := utils.NewNetworkManager(pod, c)

	conf, err := parseCustomRoutingConfig(networkManager.GetNetworkConfig())
	if err != nil {
		return pod, cperrors.NewPluginErrorWithMessage(cperrors.ParameterError, err.Error())
	}

	podIP := pod.Status.PodIP
	if podIP == "" {
		// Pod not scheduled / IP not assigned yet; stay NotReady and retry.
		pod, err := networkManager.UpdateNetworkStatus(gamekruiseiov1alpha1.NetworkStatus{
			CurrentNetworkState: gamekruiseiov1alpha1.NetworkNotReady,
		}, pod)
		return pod, cperrors.ToPluginError(err, cperrors.InternalError)
	}

	podKey := pod.GetNamespace() + "/" + pod.GetName()

	// Fast path: already resolved for this pod IP, just republish.
	if cached := p.getCache(podKey); cached != nil && cached.podIP == podIP && len(cached.externalAddresses) > 0 {
		return p.publishReady(networkManager, pod, conf, podIP, cached.externalAddresses)
	}

	// Resolve the subnet endpoint that owns this pod IP, allow the traffic and
	// fetch the deterministic accelerator port mapping.
	subnetId, externalAddresses, perr := p.resolveEndpoint(ctx, conf, podIP)
	if perr != nil {
		return pod, perr
	}
	if len(externalAddresses) == 0 {
		// Mapping not visible yet (eventual consistency); retry.
		pod, err := networkManager.UpdateNetworkStatus(gamekruiseiov1alpha1.NetworkStatus{
			CurrentNetworkState: gamekruiseiov1alpha1.NetworkNotReady,
		}, pod)
		if err != nil {
			return pod, cperrors.ToPluginError(err, cperrors.InternalError)
		}
		return pod, cperrors.NewPluginErrorWithMessage(cperrors.RetryError,
			fmt.Sprintf("custom routing port mapping for %s not found yet", podIP))
	}

	p.setCache(podKey, &allocatedEndpoint{
		podIP:             podIP,
		subnetId:          subnetId,
		externalAddresses: externalAddresses,
	})

	return p.publishReady(networkManager, pod, conf, podIP, externalAddresses)
}

// resolveEndpoint iterates the configured subnets, allows traffic to podIP and
// returns the resolved subnet plus external (AGA static IP + mapped port)
// addresses. The pod lands in exactly one subnet; allow/list on the others
// fail (IP not a subset of the subnet) and are skipped.
func (p *CustomRoutingPlugin) resolveEndpoint(ctx context.Context, conf *customRoutingConfig, podIP string) (string, []gamekruiseiov1alpha1.NetworkAddress, cperrors.PluginError) {
	var lastErr error
	for _, subnetId := range conf.subnetIds {
		_, err := p.aga.AllowCustomRoutingTrafficWithContext(ctx, &globalaccelerator.AllowCustomRoutingTrafficInput{
			EndpointGroupArn:     aws.String(conf.endpointGroupArn),
			EndpointId:           aws.String(subnetId),
			DestinationAddresses: []*string{aws.String(podIP)},
			DestinationPorts:     []*int64{aws.Int64(conf.gamePort)},
		})
		if err != nil {
			// podIP is most likely not part of this subnet; try the next one.
			lastErr = err
			log.V(5).Infof("[%s] allow traffic for %s on subnet %s failed: %v",
				GlobalAcceleratorCustomRoutingNetwork, podIP, subnetId, err)
			continue
		}

		externalAddresses, err := p.lookupExternalAddresses(ctx, conf, subnetId, podIP)
		if err != nil {
			lastErr = err
			continue
		}
		return subnetId, externalAddresses, nil
	}

	if lastErr != nil {
		return "", nil, cperrors.NewPluginErrorWithMessage(cperrors.ApiCallError,
			fmt.Sprintf("failed to allow custom routing traffic for %s: %v", podIP, lastErr))
	}
	return "", nil, nil
}

// lookupExternalAddresses queries the deterministic port mappings for podIP and
// builds one ExternalAddress per accelerator static IP / mapped port.
func (p *CustomRoutingPlugin) lookupExternalAddresses(ctx context.Context, conf *customRoutingConfig, subnetId, podIP string) ([]gamekruiseiov1alpha1.NetworkAddress, error) {
	externalAddresses := make([]gamekruiseiov1alpha1.NetworkAddress, 0)
	var nextToken *string
	for {
		out, err := p.aga.ListCustomRoutingPortMappingsByDestinationWithContext(ctx,
			&globalaccelerator.ListCustomRoutingPortMappingsByDestinationInput{
				EndpointId:         aws.String(subnetId),
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
				if sa == nil || sa.IpAddress == nil || sa.Port == nil {
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
	var subnetId string
	if cached := p.getCache(podKey); cached != nil {
		if podIP == "" {
			podIP = cached.podIP
		}
		subnetId = cached.subnetId
	}
	if podIP == "" {
		// Nothing to deny.
		p.deleteCache(podKey)
		return nil
	}

	// Deny the resolved subnet if known, otherwise fall back to all subnets.
	subnets := conf.subnetIds
	if subnetId != "" {
		subnets = []string{subnetId}
	}
	var lastErr error
	for _, sn := range subnets {
		_, err := p.aga.DenyCustomRoutingTrafficWithContext(ctx, &globalaccelerator.DenyCustomRoutingTrafficInput{
			EndpointGroupArn:     aws.String(conf.endpointGroupArn),
			EndpointId:           aws.String(sn),
			DestinationAddresses: []*string{aws.String(podIP)},
			DestinationPorts:     []*int64{aws.Int64(conf.gamePort)},
		})
		if err != nil {
			lastErr = err
			log.V(5).Infof("[%s] deny traffic for %s on subnet %s failed: %v",
				GlobalAcceleratorCustomRoutingNetwork, podIP, sn, err)
			continue
		}
		// Succeeded on the owning subnet.
		lastErr = nil
		break
	}

	p.deleteCache(podKey)

	if lastErr != nil && subnetId != "" {
		// We knew the subnet and still failed; surface the error for retry.
		return cperrors.NewPluginErrorWithMessage(cperrors.ApiCallError,
			fmt.Sprintf("failed to deny custom routing traffic for %s: %v", podIP, lastErr))
	}
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

func parseCustomRoutingConfig(conf []gamekruiseiov1alpha1.NetworkConfParams) (*customRoutingConfig, error) {
	c := &customRoutingConfig{
		protocol: corev1.ProtocolUDP,
	}
	for _, kv := range conf {
		switch kv.Name {
		case CustomRoutingEndpointGroupArnConfigName:
			c.endpointGroupArn = strings.TrimSpace(kv.Value)
		case CustomRoutingGamePortConfigName:
			port, err := strconv.ParseInt(strings.TrimSpace(kv.Value), 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid %s %q: %v", CustomRoutingGamePortConfigName, kv.Value, err)
			}
			c.gamePort = port
		case CustomRoutingProtocolConfigName:
			c.protocol = corev1.Protocol(strings.ToUpper(strings.TrimSpace(kv.Value)))
		case CustomRoutingSubnetIdsConfigName:
			for _, sn := range strings.Split(kv.Value, ",") {
				if sn = strings.TrimSpace(sn); sn != "" {
					c.subnetIds = append(c.subnetIds, sn)
				}
			}
		}
	}

	if c.endpointGroupArn == "" {
		return nil, fmt.Errorf("%s is required", CustomRoutingEndpointGroupArnConfigName)
	}
	if c.gamePort <= 0 || c.gamePort > 65535 {
		return nil, fmt.Errorf("%s must be in [1,65535]", CustomRoutingGamePortConfigName)
	}
	if len(c.subnetIds) == 0 {
		return nil, fmt.Errorf("%s is required", CustomRoutingSubnetIdsConfigName)
	}
	if c.protocol != corev1.ProtocolTCP && c.protocol != corev1.ProtocolUDP {
		return nil, fmt.Errorf("%s must be TCP or UDP", CustomRoutingProtocolConfigName)
	}
	return c, nil
}

func init() {
	amazonsWebServicesProvider.registerPlugin(&CustomRoutingPlugin{
		cache: make(map[string]*allocatedEndpoint),
	})
}
