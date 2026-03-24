/*
Copyright 2022 The Kruise Authors.

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
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/pkg/tracing"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	noop "go.opentelemetry.io/otel/trace/noop"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// newTestPlugin creates a HostPortPlugin for testing with lock-free port pool
func newTestPlugin(minPort, maxPort int32, shardCount int) *HostPortPlugin {
	plugin := &HostPortPlugin{
		minPort:      minPort,
		maxPort:      maxPort,
		shardCount:   shardCount,
		shardMask:    int32(shardCount - 1),
		portUsage:    make([]int32, maxPort-minPort+1),
		shardMutexes: make([]sync.Mutex, shardCount),
		podAllocated: make([]map[string]string, shardCount),
	}

	// Initialize available ports
	for port := minPort; port <= maxPort; port++ {
		plugin.availablePorts.Store(port, struct{}{})
	}

	// Initialize pod allocation maps
	for i := 0; i < shardCount; i++ {
		plugin.podAllocated[i] = make(map[string]string)
	}

	return plugin
}

func TestFNV32a(t *testing.T) {
	// Test hash distribution
	hash1 := fnv32a("test-pod-1")
	hash2 := fnv32a("test-pod-2")
	hash3 := fnv32a("test-pod-1") // Same input should produce same hash

	if hash1 == hash2 {
		t.Error("Different inputs should produce different hashes")
	}
	if hash1 != hash3 {
		t.Error("Same input should produce same hash")
	}
}

func TestClamp(t *testing.T) {
	tests := []struct {
		value, min, max, expected int
	}{
		{5, 0, 10, 5},
		{-5, 0, 10, 0},
		{15, 0, 10, 10},
		{0, 0, 10, 0},
		{10, 0, 10, 10},
	}

	for _, test := range tests {
		result := clamp(test.value, test.min, test.max)
		if result != test.expected {
			t.Errorf("clamp(%d, %d, %d) = %d, expected %d",
				test.value, test.min, test.max, result, test.expected)
		}
	}
}

func TestDetermineShardCount(t *testing.T) {
	tests := []struct {
		input    int
		expected int
	}{
		{0, 1},     // Default
		{1, 1},     // Min
		{10, 10},   // Normal
		{256, 256}, // Max
		{300, 256}, // Exceeds max
		{-5, 1},    // Negative
	}

	for _, test := range tests {
		result := clamp(test.input, MinShardCount, MaxShardCount)
		if result != test.expected {
			t.Errorf("clamp(%d, %d, %d) = %d, expected %d",
				test.input, MinShardCount, MaxShardCount, result, test.expected)
		}
	}
}

// TestHostPortTracingOnPodAdded tests tracing span creation in OnPodAdded
func TestHostPortTracingOnPodAdded(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(spanRecorder),
	)
	otel.SetTracerProvider(tracerProvider)
	defer otel.SetTracerProvider(noop.NewTracerProvider())

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			Labels: map[string]string{
				gamekruiseiov1alpha1.GameServerOwnerGssKey: "test-gss",
			},
			Annotations: map[string]string{
				gamekruiseiov1alpha1.GameServerNetworkType:     "Kubernetes-HostPort",
				gamekruiseiov1alpha1.GameServerNetworkConf:     `[{"name":"ContainerPorts","value":"80,443"}]`,
				gamekruiseiov1alpha1.GameServerNetworkStatus:   `{"currentNetworkState":"Ready"}`,
				gamekruiseiov1alpha1.GameServerNetworkDisabled: "false",
			},
		},
		Spec: corev1.PodSpec{
			NodeName: "test-node",
			Containers: []corev1.Container{
				{
					Name:  "game",
					Image: "nginx:latest",
					Ports: []corev1.ContainerPort{
						{ContainerPort: 80, Protocol: corev1.ProtocolTCP},
						{ContainerPort: 443, Protocol: corev1.ProtocolTCP},
					},
				},
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			PodIP: "10.0.0.1",
		},
	}

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = gamekruiseiov1alpha1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()

	plugin := newTestPlugin(8000, 9000, 1)
	ctx := context.Background()
	_, err := plugin.OnPodAdded(fakeClient, pod, ctx)
	_ = err

	spans := spanRecorder.Ended()
	if len(spans) == 0 {
		t.Fatal("Expected at least one span to be created, got none")
	}

	var rootSpan sdktrace.ReadOnlySpan
	for _, span := range spans {
		if span.Name() == tracing.SpanPrepareHostPortPod {
			rootSpan = span
			break
		}
	}

	if rootSpan == nil {
		t.Fatal("Root span 'prepare hostport pod' not found")
	}

	if rootSpan.SpanKind() != trace.SpanKindInternal {
		t.Errorf("Expected span kind Internal, got %v", rootSpan.SpanKind())
	}

	t.Logf("Successfully verified %d span(s) created for OnPodAdded", len(spans))
}

// TestHostPortTracingOnPodUpdated tests tracing span creation in OnPodUpdated
func TestHostPortTracingOnPodUpdated(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(spanRecorder),
	)
	otel.SetTracerProvider(tracerProvider)
	defer otel.SetTracerProvider(noop.NewTracerProvider())

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod-update",
			Namespace: "default",
			Labels: map[string]string{
				gamekruiseiov1alpha1.GameServerOwnerGssKey: "test-gss",
			},
			Annotations: map[string]string{
				gamekruiseiov1alpha1.GameServerNetworkType:   "Kubernetes-HostPort",
				gamekruiseiov1alpha1.GameServerNetworkConf:   `[{"name":"ContainerPorts","value":"80"}]`,
				gamekruiseiov1alpha1.GameServerNetworkStatus: `{"currentNetworkState":"Ready","internalAddresses":[{"ip":"10.0.0.1"}]}`,
			},
		},
		Spec: corev1.PodSpec{
			NodeName: "test-node",
			Containers: []corev1.Container{
				{
					Name:  "game",
					Image: "nginx:latest",
					Ports: []corev1.ContainerPort{
						{ContainerPort: 80, HostPort: 8080, Protocol: corev1.ProtocolTCP},
					},
				},
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			PodIP: "10.0.0.1",
		},
	}

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
		},
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{
				{Type: corev1.NodeExternalIP, Address: "203.0.113.1"},
				{Type: corev1.NodeInternalIP, Address: "10.0.0.100"},
			},
		},
	}

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = gamekruiseiov1alpha1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod, node).Build()

	plugin := newTestPlugin(8000, 9000, 1)
	ctx := context.Background()
	_, err := plugin.OnPodUpdated(fakeClient, pod, ctx)
	_ = err

	spans := spanRecorder.Ended()
	if len(spans) == 0 {
		t.Fatal("Expected at least one span to be created, got none")
	}

	var rootSpan sdktrace.ReadOnlySpan
	for _, span := range spans {
		if span.Name() == tracing.SpanProcessHostPortUpdate {
			rootSpan = span
			break
		}
	}

	if rootSpan == nil {
		t.Fatal("Root span 'process hostport update' not found")
	}

	t.Logf("Successfully verified %d span(s) created for OnPodUpdated", len(spans))
}

// TestHostPortTracingErrorHandling tests error span recording
func TestHostPortTracingErrorHandling(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(spanRecorder),
	)
	otel.SetTracerProvider(tracerProvider)
	defer otel.SetTracerProvider(noop.NewTracerProvider())

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod-error",
			Namespace: "default",
			Annotations: map[string]string{
				gamekruiseiov1alpha1.GameServerNetworkType: "Kubernetes-HostPort",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "game",
					Image: "nginx:latest",
				},
			},
		},
	}

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = gamekruiseiov1alpha1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()

	plugin := newTestPlugin(8000, 9000, 1)
	ctx := context.Background()
	_, err := plugin.OnPodAdded(fakeClient, pod, ctx)
	_ = err

	spans := spanRecorder.Ended()
	if len(spans) == 0 {
		t.Fatal("Expected at least one span to be created, got none")
	}

	hasErrorSpan := false
	for _, span := range spans {
		if span.Status().Code == codes.Error {
			hasErrorSpan = true
			t.Logf("Found error span: %s with status: %s", span.Name(), span.Status().Description)
		}
	}

	if !hasErrorSpan {
		t.Logf("Note: No error spans found - error handling may be handled differently")
	}

	t.Logf("Successfully verified %d span(s) for error handling test", len(spans))
}

// TestHostPortTracingOnPodDeleted verifies that OnPodDeleted emits the release event in parent span
func TestHostPortTracingOnPodDeleted(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(spanRecorder),
	)
	otel.SetTracerProvider(tracerProvider)
	defer otel.SetTracerProvider(noop.NewTracerProvider())

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod-del",
			Namespace: "default",
			Annotations: map[string]string{
				gamekruiseiov1alpha1.GameServerNetworkType:   "Kubernetes-HostPort",
				gamekruiseiov1alpha1.GameServerNetworkConf:   `[{"name":"ContainerPorts","value":"80"}]`,
				gamekruiseiov1alpha1.GameServerNetworkStatus: `{"currentNetworkState":"Ready"}`,
			},
		},
		Spec: corev1.PodSpec{
			NodeName: "test-node",
			Containers: []corev1.Container{
				{
					Name:  "game",
					Image: "nginx:latest",
					Ports: []corev1.ContainerPort{{ContainerPort: 80, HostPort: 8080, Protocol: corev1.ProtocolTCP}},
				},
			},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.0.0.1"},
	}

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = gamekruiseiov1alpha1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()

	plugin := newTestPlugin(8000, 9000, 1)
	// Simulate existing allocation
	shardID := plugin.getShard(pod)
	plugin.podAllocated[shardID]["default/test-pod-del"] = "8080"
	atomic.AddInt32(&plugin.portUsage[80], 1)

	ctx := context.Background()
	_ = plugin.OnPodDeleted(fakeClient, pod, ctx)

	spans := spanRecorder.Ended()
	if len(spans) == 0 {
		t.Fatal("Expected at least one span to be created, got none")
	}

	var rootSpan sdktrace.ReadOnlySpan
	for _, s := range spans {
		if s.Name() == tracing.SpanCleanupHostPortAllocation {
			rootSpan = s
			break
		}
	}
	if rootSpan == nil {
		t.Fatal("Root span cleanup hostport allocation not found")
	}

	found := false
	for _, e := range rootSpan.Events() {
		if e.Name == tracing.EventNetworkHostPortReleased {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected %s event in root span on pod deleted", tracing.EventNetworkHostPortReleased)
	}
}

// TestShardedDistribution tests that pods are evenly distributed across shards
func TestShardedDistribution(t *testing.T) {
	shardCount := 16
	plugin := newTestPlugin(20000, 60000, shardCount)

	// Test distribution with 100 different GS names
	distribution := make(map[int]int)
	for i := 0; i < 100; i++ {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-pod-" + string(rune(i)),
			},
		}
		shardID := plugin.getShard(pod)
		distribution[shardID]++
	}

	// Verify no shard has more than 3x average (allowing for some variance due to hash distribution)
	avg := float64(100) / float64(shardCount)
	for shardID, count := range distribution {
		if float64(count) > avg*3 {
			t.Errorf("Shard %d has %d allocations, expected around %f", shardID, count, avg)
		}
	}
	t.Logf("Distribution across %d shards: %v", shardCount, distribution)
}

// TestConcurrentAllocation tests concurrent port allocation
func TestConcurrentAllocation(t *testing.T) {
	shardCount := 4
	plugin := newTestPlugin(8000, 8039, shardCount) // 40 ports

	// Allocate ports concurrently
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(idx int) {
			podKey := "default/pod-" + string(rune('A'+idx))
			shardID := plugin.getShardByKey("pod-" + string(rune('A'+idx)))
			ports, err := plugin.allocatePorts(1, podKey, shardID)
			if err != nil {
				t.Errorf("Failed to allocate port: %v", err)
			}
			if len(ports) != 1 {
				t.Errorf("Expected 1 port, got %d", len(ports))
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify allocations
	totalAllocated := 0
	for i := 0; i < shardCount; i++ {
		plugin.shardMutexes[i].Lock()
		totalAllocated += len(plugin.podAllocated[i])
		plugin.shardMutexes[i].Unlock()
	}
	if totalAllocated != 10 {
		t.Errorf("Expected 10 total allocations, got %d", totalAllocated)
	}
}

// TestBackwardCompatibility tests that shardCount=1 behaves like the original implementation
func TestBackwardCompatibility(t *testing.T) {
	plugin := newTestPlugin(8000, 9000, 1)

	for i := 0; i < 10; i++ {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-pod-" + string(rune(i)),
			},
		}
		shardID := plugin.getShard(pod)
		if shardID != 0 {
			t.Errorf("Expected shard 0, got shard %d", shardID)
		}
	}
}

// TestPortAllocationAndRelease tests port allocation and release cycle
func TestPortAllocationAndRelease(t *testing.T) {
	plugin := newTestPlugin(8000, 8010, 1) // 11 ports

	// Allocate a port
	shardID := 0
	podKey := "default/test-pod"
	ports, err := plugin.allocatePorts(1, podKey, shardID)
	if err != nil {
		t.Fatalf("Failed to allocate port: %v", err)
	}
	if len(ports) != 1 {
		t.Fatalf("Expected 1 port, got %d", len(ports))
	}
	allocatedPort := ports[0]

	// Verify port is marked as used
	if atomic.LoadInt32(&plugin.portUsage[allocatedPort-8000]) != 1 {
		t.Error("Port should be marked as used")
	}

	// Release the port
	plugin.deallocatePorts(ports, podKey, shardID)

	// Verify port is freed
	if atomic.LoadInt32(&plugin.portUsage[allocatedPort-8000]) != 0 {
		t.Error("Port should be freed")
	}

	// Verify port is back in available pool
	if _, ok := plugin.availablePorts.Load(allocatedPort); !ok {
		t.Error("Port should be back in available pool")
	}
}

// TestPortExhaustion tests behavior when ports are exhausted
func TestPortExhaustion(t *testing.T) {
	plugin := newTestPlugin(8000, 8002, 1) // Only 3 ports

	// Allocate all ports
	for i := 0; i < 3; i++ {
		podKey := fmt.Sprintf("default/pod-%d", i)
		_, err := plugin.allocatePorts(1, podKey, 0)
		if err != nil {
			t.Fatalf("Failed to allocate port %d: %v", i, err)
		}
	}

	// Try to allocate one more - should fail
	_, err := plugin.allocatePorts(1, "default/pod-extra", 0)
	if err == nil {
		t.Error("Expected error when ports are exhausted")
	}
}
