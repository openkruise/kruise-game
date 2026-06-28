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
	"reflect"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/globalaccelerator"
	gatypes "github.com/aws/aws-sdk-go-v2/service/globalaccelerator/types"
	smithy "github.com/aws/smithy-go"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/json"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	cperrors "github.com/openkruise/kruise-game/cloudprovider/errors"
)

// fakeClient builds a controller-runtime fake client preloaded with the given
// objects. Used by Init recovery / orphan cleanup tests.
func fakeClient(objs ...client.Object) client.Client {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	return ctrlfake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
}

const (
	testEGArn  = "arn:aws:globalaccelerator::123456789012:accelerator/abcd/listener/1234/endpoint-group/5678"
	testSubnet = "subnet-0a1b2c3d"
	testPodIP  = "10.0.1.23"
)

// fakeAGA is an in-memory mock of customRoutingAPI (aws-sdk-go-v2 shape).
type fakeAGA struct {
	mu sync.Mutex

	// configuration of the fake mapping table: pod IP -> list of (accIP, accPort)
	// at the game port (7777). Multiple entries exercise the paging path.
	mappings map[string][]accSock

	allowCalls []string // "endpointId/destIP"
	denyCalls  []string // "endpointId/destIP"

	allowErr error
	listErr  error
	denyErr  error

	// pageSize, when > 1, splits the mapping list across paginated responses.
	pageSize int

	// portMappings, when set, drives the EG-wide ListCustomRoutingPortMappings
	// response. Used by Init orphan-cleanup tests; left nil for the rest.
	portMappings    []gatypes.PortMapping
	portMappingsErr error
}

type accSock struct {
	accIP   string
	accPort int32
}

func (f *fakeAGA) AllowCustomRoutingTraffic(ctx context.Context, in *globalaccelerator.AllowCustomRoutingTrafficInput, optFns ...func(*globalaccelerator.Options)) (*globalaccelerator.AllowCustomRoutingTrafficOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.allowErr != nil {
		return nil, f.allowErr
	}
	f.allowCalls = append(f.allowCalls, aws.ToString(in.EndpointId)+"/"+in.DestinationAddresses[0])
	return &globalaccelerator.AllowCustomRoutingTrafficOutput{}, nil
}

func (f *fakeAGA) DenyCustomRoutingTraffic(ctx context.Context, in *globalaccelerator.DenyCustomRoutingTrafficInput, optFns ...func(*globalaccelerator.Options)) (*globalaccelerator.DenyCustomRoutingTrafficOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.denyErr != nil {
		return nil, f.denyErr
	}
	f.denyCalls = append(f.denyCalls, aws.ToString(in.EndpointId)+"/"+in.DestinationAddresses[0])
	return &globalaccelerator.DenyCustomRoutingTrafficOutput{}, nil
}

func (f *fakeAGA) ListCustomRoutingPortMappingsByDestination(ctx context.Context, in *globalaccelerator.ListCustomRoutingPortMappingsByDestinationInput, optFns ...func(*globalaccelerator.Options)) (*globalaccelerator.ListCustomRoutingPortMappingsByDestinationOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	socks, ok := f.mappings[aws.ToString(in.DestinationAddress)]
	if !ok {
		return &globalaccelerator.ListCustomRoutingPortMappingsByDestinationOutput{}, nil
	}

	// Determine the page window.
	start := 0
	if in.NextToken != nil {
		// token encodes the next start index as a decimal string.
		start = decodeToken(aws.ToString(in.NextToken))
	}
	pageSize := f.pageSize
	if pageSize <= 0 {
		pageSize = len(socks)
	}
	end := start + pageSize
	if end > len(socks) {
		end = len(socks)
	}

	mappings := make([]gatypes.DestinationPortMapping, 0, end-start)
	for _, s := range socks[start:end] {
		mappings = append(mappings, gatypes.DestinationPortMapping{
			EndpointId:               in.EndpointId,
			DestinationSocketAddress: &gatypes.SocketAddress{IpAddress: aws.String(aws.ToString(in.DestinationAddress)), Port: aws.Int32(7777)},
			AcceleratorSocketAddresses: []gatypes.SocketAddress{
				{IpAddress: aws.String(s.accIP), Port: aws.Int32(s.accPort)},
			},
		})
	}
	out := &globalaccelerator.ListCustomRoutingPortMappingsByDestinationOutput{
		DestinationPortMappings: mappings,
	}
	if end < len(socks) {
		out.NextToken = aws.String(encodeToken(end))
	}
	return out, nil
}

func (f *fakeAGA) ListCustomRoutingPortMappings(ctx context.Context, in *globalaccelerator.ListCustomRoutingPortMappingsInput, optFns ...func(*globalaccelerator.Options)) (*globalaccelerator.ListCustomRoutingPortMappingsOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.portMappingsErr != nil {
		return nil, f.portMappingsErr
	}
	return &globalaccelerator.ListCustomRoutingPortMappingsOutput{PortMappings: f.portMappings}, nil
}

func encodeToken(i int) string {
	return "tok-" + itoa(i)
}

func decodeToken(s string) int {
	if len(s) <= 4 {
		return 0
	}
	return atoi(s[4:])
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// apiErr is a minimal smithy.APIError implementation for typed-error tests.
type apiErr struct {
	code string
	msg  string
}

func (e *apiErr) Error() string                 { return e.code + ": " + e.msg }
func (e *apiErr) ErrorCode() string             { return e.code }
func (e *apiErr) ErrorMessage() string          { return e.msg }
func (e *apiErr) ErrorFault() smithy.ErrorFault { return smithy.FaultClient }

func newTestPod(podIP string) *corev1.Pod {
	conf := []gamekruiseiov1alpha1.NetworkConfParams{
		{Name: CustomRoutingEndpointGroupArnConfigName, Value: testEGArn},
		{Name: CustomRoutingGamePortConfigName, Value: "7777"},
		{Name: CustomRoutingProtocolConfigName, Value: "UDP"},
		{Name: CustomRoutingEndpointIdConfigName, Value: testSubnet},
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
		mappings: map[string][]accSock{
			testPodIP: {{accIP: "75.2.1.1", accPort: 50001}},
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
				{Name: CustomRoutingEndpointIdConfigName, Value: "subnet-a"},
			},
			want: &customRoutingConfig{
				endpointGroupArn: testEGArn,
				gamePort:         7777,
				protocol:         corev1.ProtocolTCP,
				endpointId:       "subnet-a",
				region:           defaultAGARegion,
			},
		},
		{
			name: "default protocol udp and region",
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{Name: CustomRoutingEndpointGroupArnConfigName, Value: testEGArn},
				{Name: CustomRoutingGamePortConfigName, Value: "8000"},
				{Name: CustomRoutingEndpointIdConfigName, Value: testSubnet},
			},
			want: &customRoutingConfig{
				endpointGroupArn: testEGArn,
				gamePort:         8000,
				protocol:         corev1.ProtocolUDP,
				endpointId:       testSubnet,
				region:           defaultAGARegion,
			},
		},
		{
			name: "region override",
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{Name: CustomRoutingEndpointGroupArnConfigName, Value: testEGArn},
				{Name: CustomRoutingGamePortConfigName, Value: "7777"},
				{Name: CustomRoutingEndpointIdConfigName, Value: testSubnet},
				{Name: CustomRoutingRegionConfigName, Value: "us-east-1"},
			},
			want: &customRoutingConfig{
				endpointGroupArn: testEGArn,
				gamePort:         7777,
				protocol:         corev1.ProtocolUDP,
				endpointId:       testSubnet,
				region:           "us-east-1",
			},
		},
		{
			name: "missing arn",
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{Name: CustomRoutingGamePortConfigName, Value: "7777"},
				{Name: CustomRoutingEndpointIdConfigName, Value: testSubnet},
			},
			wantErr: true,
		},
		{
			name: "missing endpoint id",
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
				{Name: CustomRoutingEndpointIdConfigName, Value: testSubnet},
			},
			wantErr: true,
		},
		{
			name: "port out of range",
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{Name: CustomRoutingEndpointGroupArnConfigName, Value: testEGArn},
				{Name: CustomRoutingGamePortConfigName, Value: "70000"},
				{Name: CustomRoutingEndpointIdConfigName, Value: testSubnet},
			},
			wantErr: true,
		},
		{
			name: "invalid protocol",
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{Name: CustomRoutingEndpointGroupArnConfigName, Value: testEGArn},
				{Name: CustomRoutingGamePortConfigName, Value: "7777"},
				{Name: CustomRoutingProtocolConfigName, Value: "sctp"},
				{Name: CustomRoutingEndpointIdConfigName, Value: testSubnet},
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

func TestCustomRoutingOnPodAddedReady(t *testing.T) {
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
	// Exactly one allow call, on the user-specified endpoint (no trial-and-error).
	if len(fake.allowCalls) != 1 || fake.allowCalls[0] != testSubnet+"/"+testPodIP {
		t.Errorf("unexpected allow calls: %#v", fake.allowCalls)
	}
	// Cache must be populated for the endpoint.
	if cached := p.getCache("default/game-0"); cached == nil || cached.endpointId != testSubnet {
		t.Errorf("cache not populated correctly: %#v", cached)
	}
}

func TestCustomRoutingOnPodAddedNoPodIP(t *testing.T) {
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

// M2: mapping not visible yet must publish NotReady and return (pod, nil) — no
// error escapes the webhook path (otherwise the status write is swallowed).
// The cache MUST be populated with the partial state (without externalAddresses)
// so that a subsequent OnPodDeleted can still issue the matching Deny — the
// Allow API call has already been issued at this point, so we must not "forget".
func TestCustomRoutingOnPodAddedMappingNotFound(t *testing.T) {
	fake := newFakeAGA()
	fake.mappings = map[string][]accSock{} // no mapping yet
	p := &CustomRoutingPlugin{aga: fake, cache: make(map[string]*allocatedEndpoint)}
	pod := newTestPod(testPodIP)

	out, perr := p.OnPodAdded(nil, pod, context.Background())
	if perr != nil {
		t.Fatalf("expected no error when mapping not found, got %v", perr)
	}
	ns := getNetworkStatus(t, out)
	if ns == nil || ns.CurrentNetworkState != gamekruiseiov1alpha1.NetworkNotReady {
		t.Fatalf("expected NetworkNotReady, got %#v", ns)
	}
	cached := p.getCache("default/game-0")
	if cached == nil || cached.podIP != testPodIP || cached.endpointId != testSubnet {
		t.Errorf("partial-state cache expected after Allow even when mapping invisible, got %#v", cached)
	}
	if len(cached.externalAddresses) != 0 {
		t.Errorf("externalAddresses must be empty when mapping invisible, got %#v", cached.externalAddresses)
	}
}

// S: Allow failure must surface as an apiCallError (not silently swallowed).
func TestCustomRoutingOnPodAddedAllowError(t *testing.T) {
	fake := newFakeAGA()
	fake.allowErr = &apiErr{code: "AccessDeniedException", msg: "not authorized"}
	p := &CustomRoutingPlugin{aga: fake, cache: make(map[string]*allocatedEndpoint)}
	pod := newTestPod(testPodIP)

	_, perr := p.OnPodAdded(nil, pod, context.Background())
	if perr == nil {
		t.Fatalf("expected error when allow fails")
	}
	if perr.Type() != cperrors.ApiCallError {
		t.Errorf("expected apiCallError, got %v", perr.Type())
	}
	if p.getCache("default/game-0") != nil {
		t.Errorf("cache should not be populated on allow failure")
	}
}

// S: List failure must surface as an apiCallError.
func TestCustomRoutingOnPodAddedListError(t *testing.T) {
	fake := newFakeAGA()
	fake.listErr = &apiErr{code: "InternalServiceErrorException", msg: "boom"}
	p := &CustomRoutingPlugin{aga: fake, cache: make(map[string]*allocatedEndpoint)}
	pod := newTestPod(testPodIP)

	_, perr := p.OnPodAdded(nil, pod, context.Background())
	if perr == nil {
		t.Fatalf("expected error when list fails")
	}
	if perr.Type() != cperrors.ApiCallError {
		t.Errorf("expected apiCallError, got %v", perr.Type())
	}
}

// S: List paging — multiple accelerator sockets returned across pages must all
// be collected into ExternalAddresses.
func TestCustomRoutingOnPodAddedListPaging(t *testing.T) {
	fake := newFakeAGA()
	fake.pageSize = 1
	fake.mappings[testPodIP] = []accSock{
		{accIP: "75.2.1.1", accPort: 50001},
		{accIP: "75.2.1.2", accPort: 50002},
		{accIP: "75.2.1.3", accPort: 50003},
	}
	p := &CustomRoutingPlugin{aga: fake, cache: make(map[string]*allocatedEndpoint)}
	pod := newTestPod(testPodIP)

	out, perr := p.OnPodAdded(nil, pod, context.Background())
	if perr != nil {
		t.Fatalf("OnPodAdded error: %v", perr)
	}
	ns := getNetworkStatus(t, out)
	if len(ns.ExternalAddresses) != 3 {
		t.Fatalf("expected 3 external addresses across pages, got %d: %#v", len(ns.ExternalAddresses), ns.ExternalAddresses)
	}
	gotIPs := map[string]bool{}
	for _, ea := range ns.ExternalAddresses {
		gotIPs[ea.IP] = true
	}
	for _, want := range []string{"75.2.1.1", "75.2.1.2", "75.2.1.3"} {
		if !gotIPs[want] {
			t.Errorf("missing paged external address %s: %#v", want, ns.ExternalAddresses)
		}
	}
}

func TestCustomRoutingOnPodUpdatedIdempotent(t *testing.T) {
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

// M3: when the Pod IP changes the plugin must Deny the old IP (to free the
// limited mapping capacity) and Allow the new IP.
func TestCustomRoutingOnPodUpdatedPodIPChanged(t *testing.T) {
	fake := newFakeAGA()
	fake.mappings["10.0.1.99"] = []accSock{{accIP: "75.2.1.1", accPort: 50002}}
	p := &CustomRoutingPlugin{aga: fake, cache: make(map[string]*allocatedEndpoint)}

	pod := newTestPod(testPodIP)
	if _, perr := p.OnPodAdded(nil, pod, context.Background()); perr != nil {
		t.Fatalf("OnPodAdded error: %v", perr)
	}

	// Pod re-scheduled with a new IP -> must deny old IP, re-allow new IP, refresh.
	pod.Status.PodIP = "10.0.1.99"
	out, perr := p.OnPodUpdated(nil, pod, context.Background())
	if perr != nil {
		t.Fatalf("OnPodUpdated error: %v", perr)
	}

	// Old IP must have been denied on the owning endpoint.
	if len(fake.denyCalls) != 1 || fake.denyCalls[0] != testSubnet+"/"+testPodIP {
		t.Errorf("expected old IP %s to be denied, deny calls: %#v", testPodIP, fake.denyCalls)
	}
	// New IP must have been allowed.
	foundNewAllow := false
	for _, a := range fake.allowCalls {
		if a == testSubnet+"/10.0.1.99" {
			foundNewAllow = true
		}
	}
	if !foundNewAllow {
		t.Errorf("expected new IP to be allowed, allow calls: %#v", fake.allowCalls)
	}
	if cached := p.getCache("default/game-0"); cached == nil || cached.podIP != "10.0.1.99" {
		t.Errorf("cache not refreshed for new pod IP: %#v", cached)
	}
	ns := getNetworkStatus(t, out)
	if ns.InternalAddresses[0].IP != "10.0.1.99" {
		t.Errorf("internal address not refreshed: %#v", ns.InternalAddresses)
	}
}

func TestCustomRoutingOnPodDeleted(t *testing.T) {
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

// S: Deny failure on delete must surface for retry and keep the cache.
func TestCustomRoutingOnPodDeletedDenyError(t *testing.T) {
	fake := newFakeAGA()
	fake.denyErr = &apiErr{code: "InternalServiceErrorException", msg: "boom"}
	p := &CustomRoutingPlugin{aga: fake, cache: make(map[string]*allocatedEndpoint)}
	pod := newTestPod(testPodIP)

	if _, perr := p.OnPodAdded(nil, pod, context.Background()); perr != nil {
		t.Fatalf("OnPodAdded error: %v", perr)
	}
	perr := p.OnPodDeleted(nil, pod, context.Background())
	if perr == nil {
		t.Fatalf("expected error when deny fails")
	}
	if perr.Type() != cperrors.ApiCallError {
		t.Errorf("expected apiCallError, got %v", perr.Type())
	}
	if p.getCache("default/game-0") == nil {
		t.Errorf("cache should be retained when deny fails (for retry)")
	}
}

func TestCustomRoutingOnPodDeletedNoPodIP(t *testing.T) {
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

// S: region from networkConf must be threaded into the client factory.
func TestCustomRoutingRegionConfigPassedToClient(t *testing.T) {
	var gotRegion string
	p := &CustomRoutingPlugin{
		cache: make(map[string]*allocatedEndpoint),
		newAGAClient: func(ctx context.Context, region string) (customRoutingAPI, error) {
			gotRegion = region
			return newFakeAGA(), nil
		},
	}
	conf := []gamekruiseiov1alpha1.NetworkConfParams{
		{Name: CustomRoutingEndpointGroupArnConfigName, Value: testEGArn},
		{Name: CustomRoutingGamePortConfigName, Value: "7777"},
		{Name: CustomRoutingEndpointIdConfigName, Value: testSubnet},
		{Name: CustomRoutingRegionConfigName, Value: "eu-west-1"},
	}
	confBytes, _ := json.Marshal(conf)
	pod := newTestPod(testPodIP)
	pod.Annotations[gamekruiseiov1alpha1.GameServerNetworkConf] = string(confBytes)

	if _, perr := p.OnPodAdded(nil, pod, context.Background()); perr != nil {
		t.Fatalf("OnPodAdded error: %v", perr)
	}
	if gotRegion != "eu-west-1" {
		t.Errorf("expected client built with region eu-west-1, got %q", gotRegion)
	}
}

// S: default region (us-west-2) is used when no Region override is supplied.
func TestCustomRoutingDefaultRegionAnchored(t *testing.T) {
	var gotRegion string
	p := &CustomRoutingPlugin{
		cache: make(map[string]*allocatedEndpoint),
		newAGAClient: func(ctx context.Context, region string) (customRoutingAPI, error) {
			gotRegion = region
			return newFakeAGA(), nil
		},
	}
	pod := newTestPod(testPodIP)
	if _, perr := p.OnPodAdded(nil, pod, context.Background()); perr != nil {
		t.Fatalf("OnPodAdded error: %v", perr)
	}
	if gotRegion != defaultAGARegion {
		t.Errorf("expected default region %q, got %q", defaultAGARegion, gotRegion)
	}
}

func TestCustomRoutingAliasAndName(t *testing.T) {
	p := &CustomRoutingPlugin{}
	if got, want := p.Name(), GlobalAcceleratorCustomRoutingNetwork; got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
	if got, want := p.Alias(), AliasCustomRouting; got != want {
		t.Errorf("Alias() = %q, want %q", got, want)
	}
}

// TestCustomRoutingInitRecoversFromExistingPods covers Phase 0.2 P0-1:
// controller restart must rebuild the in-memory cache from cluster state.
// We seed two live pods (one with our plugin's networkType, one with a
// different one) and verify Init populates the cache for only the matching
// pod, with the full endpoint identity needed for a subsequent Deny.
func TestCustomRoutingInitRecoversFromExistingPods(t *testing.T) {
	fake := newFakeAGA()
	p := &CustomRoutingPlugin{
		aga:          fake,
		cache:        make(map[string]*allocatedEndpoint),
		newAGAClient: func(ctx context.Context, region string) (customRoutingAPI, error) { return fake, nil },
	}

	// pod-A: belongs to this plugin, has an IP -> should be recovered.
	podA := newTestPod(testPodIP)
	podA.Name = "game-A"
	// pod-B: same shape but a different networkType -> must be ignored.
	podB := newTestPod("10.0.2.99")
	podB.Name = "game-B"
	podB.Annotations[gamekruiseiov1alpha1.GameServerNetworkType] = "AmazonWebServices-NLB"
	// pod-C: belongs to this plugin but has no IP yet -> skipped at Init.
	podC := newTestPod("")
	podC.Name = "game-C"
	// pod-D: same shape, has a deletion timestamp -> skipped at Init.
	podD := newTestPod("10.0.3.55")
	podD.Name = "game-D"
	now := metav1.Now()
	podD.DeletionTimestamp = &now
	podD.Finalizers = []string{"keep"}

	c := fakeClient(podA, podB, podC, podD)
	if err := p.Init(c, nil, context.Background()); err != nil {
		t.Fatalf("Init error: %v", err)
	}

	got := p.getCache("default/game-A")
	if got == nil {
		t.Fatalf("expected pod-A to be recovered")
	}
	if got.endpointGroupArn != testEGArn || got.endpointId != testSubnet || got.podIP != testPodIP || got.gamePort != 7777 {
		t.Errorf("partial recovery, got %+v", got)
	}
	if p.getCache("default/game-B") != nil {
		t.Errorf("pod-B (different plugin) must NOT be recovered")
	}
	if p.getCache("default/game-C") != nil {
		t.Errorf("pod-C (no IP) must NOT be recovered yet")
	}
	if p.getCache("default/game-D") != nil {
		t.Errorf("pod-D (being deleted) must NOT be recovered")
	}
}

// TestCustomRoutingInitOrphanCleanup covers Phase 0.2 P0-1 (second half):
// after rebuilding the cache from live pods, Init compares against AGA's
// ALLOW set and Denies anything not in the live set. Simulates a controller
// crash that missed a single OnPodDeleted.
func TestCustomRoutingInitOrphanCleanup(t *testing.T) {
	fake := newFakeAGA()
	// AGA still has TWO allowed destinations: the live one + an orphan.
	fake.portMappings = []gatypes.PortMapping{
		{
			EndpointGroupArn:         aws.String(testEGArn),
			EndpointId:               aws.String(testSubnet),
			DestinationSocketAddress: &gatypes.SocketAddress{IpAddress: aws.String(testPodIP), Port: aws.Int32(7777)},
			DestinationTrafficState:  gatypes.CustomRoutingDestinationTrafficStateAllow,
		},
		{
			EndpointGroupArn:         aws.String(testEGArn),
			EndpointId:               aws.String(testSubnet),
			DestinationSocketAddress: &gatypes.SocketAddress{IpAddress: aws.String("10.0.9.99"), Port: aws.Int32(7777)},
			DestinationTrafficState:  gatypes.CustomRoutingDestinationTrafficStateAllow,
		},
		{
			// A DENY entry on a live IP MUST NOT be considered an orphan.
			EndpointGroupArn:         aws.String(testEGArn),
			EndpointId:               aws.String(testSubnet),
			DestinationSocketAddress: &gatypes.SocketAddress{IpAddress: aws.String("10.0.10.10"), Port: aws.Int32(7777)},
			DestinationTrafficState:  gatypes.CustomRoutingDestinationTrafficStateDeny,
		},
	}
	p := &CustomRoutingPlugin{
		aga:          fake,
		cache:        make(map[string]*allocatedEndpoint),
		newAGAClient: func(ctx context.Context, region string) (customRoutingAPI, error) { return fake, nil },
	}

	pod := newTestPod(testPodIP)
	c := fakeClient(pod)
	if err := p.Init(c, nil, context.Background()); err != nil {
		t.Fatalf("Init error: %v", err)
	}

	// Exactly one orphan must have been denied — the 10.0.9.99 entry.
	if len(fake.denyCalls) != 1 || fake.denyCalls[0] != testSubnet+"/10.0.9.99" {
		t.Errorf("expected exactly one orphan deny for 10.0.9.99, got %#v", fake.denyCalls)
	}
}

// TestCustomRoutingOnPodDeletedFallbackToConf covers Phase 0.2 P0-2: when the
// in-memory cache is cold (e.g. cluster restart edge that never re-reconciled
// this Pod) but the Pod's networkConf annotation is still parseable, the
// plugin must still issue the Deny rather than silently dropping the cleanup.
func TestCustomRoutingOnPodDeletedFallbackToConf(t *testing.T) {
	fake := newFakeAGA()
	p := &CustomRoutingPlugin{aga: fake, cache: make(map[string]*allocatedEndpoint)} // cold cache

	pod := newTestPod(testPodIP)
	if perr := p.OnPodDeleted(nil, pod, context.Background()); perr != nil {
		t.Fatalf("OnPodDeleted error: %v", perr)
	}
	if len(fake.denyCalls) != 1 || fake.denyCalls[0] != testSubnet+"/"+testPodIP {
		t.Errorf("expected deny via conf fallback, got: %#v", fake.denyCalls)
	}
}

// TestCustomRoutingConfigChangeDeniesOld covers Phase 0.2 P1-1: when the
// networkConf is mutated to point at a different (EG, subnet) tuple, the
// next reconcile must Deny the old destination on the OLD EG before Allowing
// the new one — otherwise mapping capacity leaks on the original EG.
func TestCustomRoutingConfigChangeDeniesOld(t *testing.T) {
	fake := newFakeAGA()
	p := &CustomRoutingPlugin{aga: fake, cache: make(map[string]*allocatedEndpoint)}

	pod := newTestPod(testPodIP)
	if _, perr := p.OnPodAdded(nil, pod, context.Background()); perr != nil {
		t.Fatalf("first reconcile failed: %v", perr)
	}
	if len(fake.denyCalls) != 0 {
		t.Fatalf("no Deny expected on first reconcile, got %#v", fake.denyCalls)
	}

	// Re-write networkConf with a NEW EG ARN + NEW subnet endpoint.
	newEG := "arn:aws:globalaccelerator::123456789012:accelerator/zzzz/listener/9999/endpoint-group/aaaa"
	newSubnet := "subnet-99999999"
	newConf := []gamekruiseiov1alpha1.NetworkConfParams{
		{Name: CustomRoutingEndpointGroupArnConfigName, Value: newEG},
		{Name: CustomRoutingGamePortConfigName, Value: "7777"},
		{Name: CustomRoutingProtocolConfigName, Value: "UDP"},
		{Name: CustomRoutingEndpointIdConfigName, Value: newSubnet},
	}
	confBytes, _ := json.Marshal(newConf)
	pod.Annotations[gamekruiseiov1alpha1.GameServerNetworkConf] = string(confBytes)
	// Add a mapping for the new pair so reconcile reaches Ready.
	fake.mappings[testPodIP] = []accSock{{accIP: "75.2.99.99", accPort: 60001}}

	if _, perr := p.OnPodUpdated(nil, pod, context.Background()); perr != nil {
		t.Fatalf("second reconcile failed: %v", perr)
	}

	// Deny must have hit the OLD subnet endpoint (not the new one).
	if len(fake.denyCalls) != 1 || fake.denyCalls[0] != testSubnet+"/"+testPodIP {
		t.Errorf("expected exactly one deny on the OLD subnet, got %#v", fake.denyCalls)
	}
	// Allow must include the NEW subnet endpoint.
	foundNewAllow := false
	for _, a := range fake.allowCalls {
		if a == newSubnet+"/"+testPodIP {
			foundNewAllow = true
		}
	}
	if !foundNewAllow {
		t.Errorf("expected an Allow on the NEW subnet, got %#v", fake.allowCalls)
	}
	cached := p.getCache("default/game-0")
	if cached == nil || cached.endpointGroupArn != newEG || cached.endpointId != newSubnet {
		t.Errorf("cache not refreshed to new identity: %#v", cached)
	}
}

// TestCustomRoutingFixedIgnored covers Phase 0.2 P0-3: Fixed=true is parsed
// (no error) but has no behavioural effect — the rest of the reconcile path
// runs exactly as the Fixed=false case.
func TestCustomRoutingFixedIgnored(t *testing.T) {
	fake := newFakeAGA()
	p := &CustomRoutingPlugin{aga: fake, cache: make(map[string]*allocatedEndpoint)}

	pod := newTestPod(testPodIP)
	conf := []gamekruiseiov1alpha1.NetworkConfParams{
		{Name: CustomRoutingEndpointGroupArnConfigName, Value: testEGArn},
		{Name: CustomRoutingGamePortConfigName, Value: "7777"},
		{Name: CustomRoutingProtocolConfigName, Value: "UDP"},
		{Name: CustomRoutingEndpointIdConfigName, Value: testSubnet},
		{Name: CustomRoutingFixedConfigName, Value: "true"},
	}
	confBytes, _ := json.Marshal(conf)
	pod.Annotations[gamekruiseiov1alpha1.GameServerNetworkConf] = string(confBytes)

	out, perr := p.OnPodAdded(nil, pod, context.Background())
	if perr != nil {
		t.Fatalf("OnPodAdded with Fixed=true returned error: %v", perr)
	}
	ns := getNetworkStatus(t, out)
	if ns == nil || ns.CurrentNetworkState != gamekruiseiov1alpha1.NetworkReady {
		t.Fatalf("expected Ready, got %#v", ns)
	}
	if len(fake.allowCalls) != 1 {
		t.Errorf("Fixed=true should NOT change allow semantics, got %#v", fake.allowCalls)
	}

	// And OnPodDeleted MUST still Deny regardless of Fixed=true (Custom Routing
	// has no notion of "preserve allocation while GSS alive" since port
	// mappings are statically generated).
	if perr := p.OnPodDeleted(nil, pod, context.Background()); perr != nil {
		t.Fatalf("OnPodDeleted error: %v", perr)
	}
	if len(fake.denyCalls) != 1 {
		t.Errorf("expected Deny even with Fixed=true, got %#v", fake.denyCalls)
	}
}
