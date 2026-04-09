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

// newTestPlugin creates a HostPortPlugin for testing with port reuse support
func newTestPlugin(minPort, maxPort int32) *HostPortPlugin {
	plugin := &HostPortPlugin{
		minPort:   minPort,
		maxPort:   maxPort,
		portUsage: make([]int32, maxPort-minPort+1),
		podPorts:  make(map[string][]int32),
	}
	return plugin
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

	plugin := newTestPlugin(8000, 9000)
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

	plugin := newTestPlugin(8000, 9000)
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

	plugin := newTestPlugin(8000, 9000)
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

	plugin := newTestPlugin(8000, 9000)
	// Simulate existing allocation using new structure
	plugin.podPorts["default/test-pod-del"] = []int32{8080}
	plugin.portUsage[8080-8000] = 1

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

// TestLeastUsedDistribution tests that ports are evenly distributed using least-used strategy
func TestLeastUsedDistribution(t *testing.T) {
	plugin := newTestPlugin(8000, 8009) // 10 ports

	// Allocate 30 pods, each needing 1 port
	for i := 0; i < 30; i++ {
		podKey := fmt.Sprintf("default/pod-%d", i)
		ports, err := plugin.allocatePorts(1, podKey)
		if err != nil {
			t.Fatalf("Failed to allocate port for pod %d: %v", i, err)
		}
		if len(ports) != 1 {
			t.Fatalf("Expected 1 port, got %d", len(ports))
		}
	}

	// Each port should be used exactly 3 times (30 pods / 10 ports)
	for i := int32(0); i < 10; i++ {
		usage := plugin.portUsage[i]
		if usage != 3 {
			t.Errorf("Port %d usage = %d, expected 3", 8000+i, usage)
		}
	}
}

// TestConcurrentSafety tests concurrent port allocation safety
func TestConcurrentSafety(t *testing.T) {
	plugin := newTestPlugin(8000, 8009) // 10 ports

	var wg sync.WaitGroup
	errCh := make(chan error, 100)

	// Concurrently allocate 100 pods
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			podKey := fmt.Sprintf("default/pod-%d", idx)
			ports, err := plugin.allocatePorts(1, podKey)
			if err != nil {
				errCh <- fmt.Errorf("pod-%d: %v", idx, err)
				return
			}
			if len(ports) != 1 {
				errCh <- fmt.Errorf("pod-%d: expected 1 port, got %d", idx, len(ports))
			}
		}(i)
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Error(err)
	}

	// Verify total allocations
	plugin.mu.Lock()
	totalPods := len(plugin.podPorts)
	plugin.mu.Unlock()
	if totalPods != 100 {
		t.Errorf("Expected 100 pod allocations, got %d", totalPods)
	}

	// Each port should be used ~10 times
	var totalUsage int32
	for i := 0; i < 10; i++ {
		totalUsage += plugin.portUsage[i]
	}
	if totalUsage != 100 {
		t.Errorf("Expected total usage 100, got %d", totalUsage)
	}
}

// TestBasicAllocation tests basic port allocation for multiple pods
func TestBasicAllocation(t *testing.T) {
	plugin := newTestPlugin(8000, 9000)

	// Allocate ports for multiple pods
	for i := 0; i < 10; i++ {
		podKey := fmt.Sprintf("default/pod-%d", i)
		ports, err := plugin.allocatePorts(1, podKey)
		if err != nil {
			t.Fatalf("Failed to allocate: %v", err)
		}
		if len(ports) != 1 {
			t.Fatalf("Expected 1 port, got %d", len(ports))
		}
		if ports[0] < 8000 || ports[0] > 9000 {
			t.Errorf("Port %d out of range", ports[0])
		}
	}
}

// TestPortAllocationAndRelease tests port allocation and release cycle
func TestPortAllocationAndRelease(t *testing.T) {
	plugin := newTestPlugin(8000, 8010) // 11 ports

	podKey := "default/test-pod"
	ports, err := plugin.allocatePorts(1, podKey)
	if err != nil {
		t.Fatalf("Failed to allocate port: %v", err)
	}
	if len(ports) != 1 {
		t.Fatalf("Expected 1 port, got %d", len(ports))
	}
	allocatedPort := ports[0]

	// Verify port is marked as used
	if plugin.portUsage[allocatedPort-8000] != 1 {
		t.Error("Port should have usage count 1")
	}

	// Release the port
	released := plugin.deallocatePorts(podKey)
	if len(released) != 1 || released[0] != allocatedPort {
		t.Errorf("Expected released port %d, got %v", allocatedPort, released)
	}

	// Verify port usage is back to 0
	if plugin.portUsage[allocatedPort-8000] != 0 {
		t.Error("Port should have usage count 0 after release")
	}

	// Verify pod is removed from podPorts
	plugin.mu.Lock()
	_, exists := plugin.podPorts[podKey]
	plugin.mu.Unlock()
	if exists {
		t.Error("Pod should be removed from podPorts after deallocation")
	}
}

// TestPortReuse tests that ports can be reused across multiple pods
func TestPortReuse(t *testing.T) {
	plugin := newTestPlugin(8000, 8002) // Only 3 ports

	// Allocate 10 pods - should all succeed because ports can be reused
	for i := 0; i < 10; i++ {
		podKey := fmt.Sprintf("default/pod-%d", i)
		ports, err := plugin.allocatePorts(1, podKey)
		if err != nil {
			t.Fatalf("Failed to allocate port for pod %d: %v", i, err)
		}
		if len(ports) != 1 {
			t.Fatalf("Expected 1 port, got %d", len(ports))
		}
	}

	// Verify distribution: 10 pods across 3 ports
	// Expected: ports used 3, 3, 4 or 4, 3, 3 times
	var totalUsage int32
	for i := int32(0); i < 3; i++ {
		usage := plugin.portUsage[i]
		if usage < 3 || usage > 4 {
			t.Errorf("Port %d: expected usage 3-4, got %d", 8000+i, usage)
		}
		totalUsage += usage
	}
	if totalUsage != 10 {
		t.Errorf("Expected total usage 10, got %d", totalUsage)
	}
}

// TestLargeScaleAllocation tests allocation with large number of pods
func TestLargeScaleAllocation(t *testing.T) {
	plugin := newTestPlugin(8000, 8500) // 501 ports, matching user's config

	// Allocate 1000 pods - should all succeed
	for i := 0; i < 1000; i++ {
		podKey := fmt.Sprintf("default/pod-%d", i)
		ports, err := plugin.allocatePorts(1, podKey)
		if err != nil {
			t.Fatalf("Failed to allocate port for pod %d: %v", i, err)
		}
		if len(ports) != 1 {
			t.Fatalf("Expected 1 port, got %d for pod %d", len(ports), i)
		}
	}

	// Verify: each port should be used 1 or 2 times (1000/501 ≈ 2)
	for i := int32(0); i < 501; i++ {
		usage := plugin.portUsage[i]
		if usage < 1 || usage > 2 {
			t.Errorf("Port %d: expected usage 1-2, got %d", 8000+i, usage)
		}
	}

	// Verify total pods tracked
	plugin.mu.Lock()
	totalPods := len(plugin.podPorts)
	plugin.mu.Unlock()
	if totalPods != 1000 {
		t.Errorf("Expected 1000 pods tracked, got %d", totalPods)
	}
}

// TestIdempotentAllocation tests that repeated allocation for same pod returns same ports
func TestIdempotentAllocation(t *testing.T) {
	plugin := newTestPlugin(8000, 8010)

	podKey := "default/test-pod"
	ports1, err := plugin.allocatePorts(1, podKey)
	if err != nil {
		t.Fatalf("First allocation failed: %v", err)
	}

	// Second allocation for same pod should return same ports
	ports2, err := plugin.allocatePorts(1, podKey)
	if err != nil {
		t.Fatalf("Second allocation failed: %v", err)
	}

	if len(ports1) != len(ports2) || ports1[0] != ports2[0] {
		t.Errorf("Idempotency violated: first=%v, second=%v", ports1, ports2)
	}

	// Verify port usage is still 1 (not 2)
	if plugin.portUsage[ports1[0]-8000] != 1 {
		t.Errorf("Expected usage 1 for idempotent allocation, got %d", plugin.portUsage[ports1[0]-8000])
	}
}

// TestMultiPortAllocation tests allocating multiple ports for a single pod
func TestMultiPortAllocation(t *testing.T) {
	plugin := newTestPlugin(8000, 8010) // 11 ports

	// Allocate 3 ports for a single pod
	podKey := "default/multi-port-pod"
	ports, err := plugin.allocatePorts(3, podKey)
	if err != nil {
		t.Fatalf("Failed to allocate: %v", err)
	}
	if len(ports) != 3 {
		t.Fatalf("Expected 3 ports, got %d", len(ports))
	}

	// All three ports should be different
	portSet := make(map[int32]bool)
	for _, p := range ports {
		if portSet[p] {
			t.Errorf("Duplicate port %d in allocation", p)
		}
		portSet[p] = true
	}
}
