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
	"reflect"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/globalaccelerator"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/json"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
)

const (
	testEGArn  = "arn:aws:globalaccelerator::123456789012:accelerator/abcd/listener/1234/endpoint-group/5678"
	testSubnet = "subnet-0a1b2c3d"
	testPodIP  = "10.0.1.23"
)

// fakeAGA is an in-memory mock of customRoutingAPI.
type fakeAGA struct {
	mu sync.Mutex

	// configuration of the fake mapping table: pod IP -> (accIP, accPort)
	mappings map[string]struct {
		accIP   string
		accPort int64
	}
	// subnet that "owns" the pod IP; Allow/Deny on other subnets returns error.
	owningSubnet string

	allowCalls []string
	denyCalls  []string

	allowErr error
	listErr  error
}

func (f *fakeAGA) AllowCustomRoutingTrafficWithContext(ctx aws.Context, in *globalaccelerator.AllowCustomRoutingTrafficInput, opts ...request.Option) (*globalaccelerator.AllowCustomRoutingTrafficOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.allowErr != nil {
		return nil, f.allowErr
	}
	if f.owningSubnet != "" && aws.StringValue(in.EndpointId) != f.owningSubnet {
		return nil, fmt.Errorf("destination address not in subnet %s", aws.StringValue(in.EndpointId))
	}
	f.allowCalls = append(f.allowCalls, aws.StringValue(in.EndpointId)+"/"+aws.StringValue(in.DestinationAddresses[0]))
	return &globalaccelerator.AllowCustomRoutingTrafficOutput{}, nil
}

func (f *fakeAGA) DenyCustomRoutingTrafficWithContext(ctx aws.Context, in *globalaccelerator.DenyCustomRoutingTrafficInput, opts ...request.Option) (*globalaccelerator.DenyCustomRoutingTrafficOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.owningSubnet != "" && aws.StringValue(in.EndpointId) != f.owningSubnet {
		return nil, fmt.Errorf("destination address not in subnet %s", aws.StringValue(in.EndpointId))
	}
	f.denyCalls = append(f.denyCalls, aws.StringValue(in.EndpointId)+"/"+aws.StringValue(in.DestinationAddresses[0]))
	return &globalaccelerator.DenyCustomRoutingTrafficOutput{}, nil
}

func (f *fakeAGA) ListCustomRoutingPortMappingsByDestinationWithContext(ctx aws.Context, in *globalaccelerator.ListCustomRoutingPortMappingsByDestinationInput, opts ...request.Option) (*globalaccelerator.ListCustomRoutingPortMappingsByDestinationOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	m, ok := f.mappings[aws.StringValue(in.DestinationAddress)]
	if !ok {
		return &globalaccelerator.ListCustomRoutingPortMappingsByDestinationOutput{}, nil
	}
	return &globalaccelerator.ListCustomRoutingPortMappingsByDestinationOutput{
		DestinationPortMappings: []*globalaccelerator.DestinationPortMapping{
			{
				EndpointId:               in.EndpointId,
				DestinationSocketAddress: &globalaccelerator.SocketAddress{IpAddress: aws.String(testPodIP), Port: aws.Int64(7777)},
				AcceleratorSocketAddresses: []*globalaccelerator.SocketAddress{
					{IpAddress: aws.String(m.accIP), Port: aws.Int64(m.accPort)},
				},
			},
		},
	}, nil
}

func newTestPod(podIP string) *corev1.Pod {
	conf := []gamekruiseiov1alpha1.NetworkConfParams{
		{Name: CustomRoutingEndpointGroupArnConfigName, Value: testEGArn},
		{Name: CustomRoutingGamePortConfigName, Value: "7777"},
		{Name: CustomRoutingProtocolConfigName, Value: "UDP"},
		{Name: CustomRoutingSubnetIdsConfigName, Value: "subnet-aaa," + testSubnet},
	}
	confBytes, _ := json.Marshal(conf)
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "game-0",
			Namespace: "default",
			Annotations: map[string]string{
				gamekruiseiov1alpha1.GameServerNetworkType: GlobalAcceleratorCustomRoutingNetwork,
				gamekruiseiov1alpha1.GameServerNetworkConf: string(confBytes),
			},
		},
		Status: corev1.PodStatus{PodIP: podIP},
	}
}

func getNetworkStatus(t *testing.T, pod *corev1.Pod) *gamekruiseiov1alpha1.NetworkStatus {
	t.Helper()
	raw := pod.Annotations[gamekruiseiov1alpha1.GameServerNetworkStatus]
	if raw == "" {
		return nil
	}
	ns := &gamekruiseiov1alpha1.NetworkStatus{}
	if err := json.Unmarshal([]byte(raw), ns); err != nil {
		t.Fatalf("unmarshal network status: %v", err)
	}
	return ns
}

func newFakeAGA() *fakeAGA {
	return &fakeAGA{
		owningSubnet: testSubnet,
		mappings: map[string]struct {
			accIP   string
			accPort int64
		}{
			testPodIP: {accIP: "75.2.1.1", accPort: 50001},
		},
	}
}

func TestParseCustomRoutingConfig(t *testing.T) {
	tests := []struct {
		name    string
		conf    []gamekruiseiov1alpha1.NetworkConfParams
		wantErr bool
		want    *customRoutingConfig
	}{
		{
			name: "valid",
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{Name: CustomRoutingEndpointGroupArnConfigName, Value: testEGArn},
				{Name: CustomRoutingGamePortConfigName, Value: "7777"},
				{Name: CustomRoutingProtocolConfigName, Value: "tcp"},
				{Name: CustomRoutingSubnetIdsConfigName, Value: "subnet-a, subnet-b"},
			},
			want: &customRoutingConfig{
				endpointGroupArn: testEGArn,
				gamePort:         7777,
				protocol:         corev1.ProtocolTCP,
				subnetIds:        []string{"subnet-a", "subnet-b"},
			},
		},
		{
			name: "default protocol udp",
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{Name: CustomRoutingEndpointGroupArnConfigName, Value: testEGArn},
				{Name: CustomRoutingGamePortConfigName, Value: "8000"},
				{Name: CustomRoutingSubnetIdsConfigName, Value: testSubnet},
			},
			want: &customRoutingConfig{
				endpointGroupArn: testEGArn,
				gamePort:         8000,
				protocol:         corev1.ProtocolUDP,
				subnetIds:        []string{testSubnet},
			},
		},
		{
			name: "missing arn",
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{Name: CustomRoutingGamePortConfigName, Value: "7777"},
				{Name: CustomRoutingSubnetIdsConfigName, Value: testSubnet},
			},
			wantErr: true,
		},
		{
			name: "missing subnets",
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{Name: CustomRoutingEndpointGroupArnConfigName, Value: testEGArn},
				{Name: CustomRoutingGamePortConfigName, Value: "7777"},
			},
			wantErr: true,
		},
		{
			name: "invalid port",
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{Name: CustomRoutingEndpointGroupArnConfigName, Value: testEGArn},
				{Name: CustomRoutingGamePortConfigName, Value: "abc"},
				{Name: CustomRoutingSubnetIdsConfigName, Value: testSubnet},
			},
			wantErr: true,
		},
		{
			name: "port out of range",
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{Name: CustomRoutingEndpointGroupArnConfigName, Value: testEGArn},
				{Name: CustomRoutingGamePortConfigName, Value: "70000"},
				{Name: CustomRoutingSubnetIdsConfigName, Value: testSubnet},
			},
			wantErr: true,
		},
		{
			name: "invalid protocol",
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{Name: CustomRoutingEndpointGroupArnConfigName, Value: testEGArn},
				{Name: CustomRoutingGamePortConfigName, Value: "7777"},
				{Name: CustomRoutingProtocolConfigName, Value: "sctp"},
				{Name: CustomRoutingSubnetIdsConfigName, Value: testSubnet},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseCustomRoutingConfig(tt.conf)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestOnPodAddedReady(t *testing.T) {
	fake := newFakeAGA()
	p := &CustomRoutingPlugin{aga: fake, cache: make(map[string]*allocatedEndpoint)}
	pod := newTestPod(testPodIP)

	out, perr := p.OnPodAdded(nil, pod, context.Background())
	if perr != nil {
		t.Fatalf("OnPodAdded error: %v", perr)
	}

	ns := getNetworkStatus(t, out)
	if ns == nil || ns.CurrentNetworkState != gamekruiseiov1alpha1.NetworkReady {
		t.Fatalf("expected NetworkReady, got %#v", ns)
	}
	if len(ns.ExternalAddresses) != 1 || ns.ExternalAddresses[0].IP != "75.2.1.1" {
		t.Fatalf("unexpected external addresses: %#v", ns.ExternalAddresses)
	}
	wantPort := intstr.FromInt(50001)
	if ns.ExternalAddresses[0].Ports[0].Port == nil || *ns.ExternalAddresses[0].Ports[0].Port != wantPort {
		t.Errorf("unexpected mapped port: %#v", ns.ExternalAddresses[0].Ports[0].Port)
	}
	if ns.ExternalAddresses[0].Ports[0].Protocol != corev1.ProtocolUDP {
		t.Errorf("unexpected protocol: %v", ns.ExternalAddresses[0].Ports[0].Protocol)
	}
	if len(ns.InternalAddresses) != 1 || ns.InternalAddresses[0].IP != testPodIP {
		t.Fatalf("unexpected internal addresses: %#v", ns.InternalAddresses)
	}
	// Only the owning subnet should have a successful allow call.
	if len(fake.allowCalls) != 1 || fake.allowCalls[0] != testSubnet+"/"+testPodIP {
		t.Errorf("unexpected allow calls: %#v", fake.allowCalls)
	}
	// Cache must be populated for the owning subnet.
	if cached := p.getCache("default/game-0"); cached == nil || cached.subnetId != testSubnet {
		t.Errorf("cache not populated correctly: %#v", cached)
	}
}

func TestOnPodAddedNoPodIP(t *testing.T) {
	fake := newFakeAGA()
	p := &CustomRoutingPlugin{aga: fake, cache: make(map[string]*allocatedEndpoint)}
	pod := newTestPod("")

	out, perr := p.OnPodAdded(nil, pod, context.Background())
	if perr != nil {
		t.Fatalf("OnPodAdded error: %v", perr)
	}
	ns := getNetworkStatus(t, out)
	if ns == nil || ns.CurrentNetworkState != gamekruiseiov1alpha1.NetworkNotReady {
		t.Fatalf("expected NetworkNotReady, got %#v", ns)
	}
	if len(fake.allowCalls) != 0 {
		t.Errorf("should not call allow without pod IP: %#v", fake.allowCalls)
	}
}

func TestOnPodAddedMappingNotFound(t *testing.T) {
	fake := newFakeAGA()
	fake.mappings = map[string]struct {
		accIP   string
		accPort int64
	}{} // no mapping yet
	p := &CustomRoutingPlugin{aga: fake, cache: make(map[string]*allocatedEndpoint)}
	pod := newTestPod(testPodIP)

	_, perr := p.OnPodAdded(nil, pod, context.Background())
	if perr == nil {
		t.Fatalf("expected retry error when mapping not found")
	}
	if perr.Type() != "retryError" {
		t.Errorf("expected retryError, got %v", perr.Type())
	}
	if p.getCache("default/game-0") != nil {
		t.Errorf("cache should not be populated when mapping not found")
	}
}

func TestOnPodUpdatedIdempotent(t *testing.T) {
	fake := newFakeAGA()
	p := &CustomRoutingPlugin{aga: fake, cache: make(map[string]*allocatedEndpoint)}
	pod := newTestPod(testPodIP)

	if _, perr := p.OnPodAdded(nil, pod, context.Background()); perr != nil {
		t.Fatalf("OnPodAdded error: %v", perr)
	}
	allowAfterAdd := len(fake.allowCalls)

	// Second reconcile with same pod IP must not call Allow again (fast path).
	out, perr := p.OnPodUpdated(nil, pod, context.Background())
	if perr != nil {
		t.Fatalf("OnPodUpdated error: %v", perr)
	}
	if len(fake.allowCalls) != allowAfterAdd {
		t.Errorf("OnPodUpdated should be no-op, allow calls grew: %#v", fake.allowCalls)
	}
	ns := getNetworkStatus(t, out)
	if ns == nil || ns.CurrentNetworkState != gamekruiseiov1alpha1.NetworkReady {
		t.Fatalf("expected NetworkReady after update, got %#v", ns)
	}
}

func TestOnPodUpdatedPodIPChanged(t *testing.T) {
	fake := newFakeAGA()
	fake.mappings["10.0.1.99"] = struct {
		accIP   string
		accPort int64
	}{accIP: "75.2.1.1", accPort: 50002}
	p := &CustomRoutingPlugin{aga: fake, cache: make(map[string]*allocatedEndpoint)}

	pod := newTestPod(testPodIP)
	if _, perr := p.OnPodAdded(nil, pod, context.Background()); perr != nil {
		t.Fatalf("OnPodAdded error: %v", perr)
	}

	// Pod re-scheduled with a new IP -> must re-allow and refresh.
	pod.Status.PodIP = "10.0.1.99"
	out, perr := p.OnPodUpdated(nil, pod, context.Background())
	if perr != nil {
		t.Fatalf("OnPodUpdated error: %v", perr)
	}
	if cached := p.getCache("default/game-0"); cached == nil || cached.podIP != "10.0.1.99" {
		t.Errorf("cache not refreshed for new pod IP: %#v", cached)
	}
	ns := getNetworkStatus(t, out)
	if ns.InternalAddresses[0].IP != "10.0.1.99" {
		t.Errorf("internal address not refreshed: %#v", ns.InternalAddresses)
	}
}

func TestOnPodDeleted(t *testing.T) {
	fake := newFakeAGA()
	p := &CustomRoutingPlugin{aga: fake, cache: make(map[string]*allocatedEndpoint)}
	pod := newTestPod(testPodIP)

	if _, perr := p.OnPodAdded(nil, pod, context.Background()); perr != nil {
		t.Fatalf("OnPodAdded error: %v", perr)
	}

	if perr := p.OnPodDeleted(nil, pod, context.Background()); perr != nil {
		t.Fatalf("OnPodDeleted error: %v", perr)
	}
	if len(fake.denyCalls) != 1 || fake.denyCalls[0] != testSubnet+"/"+testPodIP {
		t.Errorf("unexpected deny calls: %#v", fake.denyCalls)
	}
	if p.getCache("default/game-0") != nil {
		t.Errorf("cache should be cleared after delete")
	}
}

func TestOnPodDeletedNoPodIP(t *testing.T) {
	fake := newFakeAGA()
	p := &CustomRoutingPlugin{aga: fake, cache: make(map[string]*allocatedEndpoint)}
	pod := newTestPod("")

	if perr := p.OnPodDeleted(nil, pod, context.Background()); perr != nil {
		t.Fatalf("OnPodDeleted error: %v", perr)
	}
	if len(fake.denyCalls) != 0 {
		t.Errorf("should not deny without pod IP: %#v", fake.denyCalls)
	}
}
