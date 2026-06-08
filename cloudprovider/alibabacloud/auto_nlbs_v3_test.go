/*
Copyright 2025 The Kruise Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package alibabacloud

import (
	"context"
	"encoding/json"
	"testing"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func init() {
	scheme.AddKnownTypes(NLBPoolGroupVersion,
		&PortAllocation{}, &PortAllocationList{},
		&NLBPool{}, &NLBPoolList{},
	)
	metav1.AddToGroupVersion(scheme, NLBPoolGroupVersion)
}

func TestParseAutoNLBsV3Config(t *testing.T) {
	tests := []struct {
		name        string
		conf        []gamekruiseiov1alpha1.NetworkConfParams
		wantPool    string
		wantNS      string
		wantErr     bool
	}{
		{
			name: "valid NLBPoolName only",
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{Name: "NLBPoolName", Value: "my-pool"},
			},
			wantPool: "my-pool",
		},
		{
			name: "valid NLBPoolName + NLBPoolNamespace",
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{Name: "NLBPoolName", Value: "giant-pool"},
				{Name: "NLBPoolNamespace", Value: "game-ns"},
			},
			wantPool: "giant-pool",
			wantNS:   "game-ns",
		},
		{
			name:    "missing NLBPoolName",
			conf:    []gamekruiseiov1alpha1.NetworkConfParams{},
			wantErr: true,
		},
		{
			name: "irrelevant params only",
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{Name: "SomethingElse", Value: "value"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseAutoNLBsV3Config(tt.conf)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.poolName != tt.wantPool {
				t.Errorf("poolName = %q, want %q", got.poolName, tt.wantPool)
			}
			if got.poolNamespace != tt.wantNS {
				t.Errorf("poolNamespace = %q, want %q", got.poolNamespace, tt.wantNS)
			}
		})
	}
}

func TestBuildNetworkStatusV3(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "default"},
		Status:     corev1.PodStatus{PodIP: "10.0.0.1"},
	}

	tests := []struct {
		name              string
		pa                *PortAllocation
		wantExternalCount int
		wantInternalCount int
		wantEndPoint      string
		wantExtPort       int32
		wantIntPort       int32
		wantProtocol      corev1.Protocol
	}{
		{
			name: "multi-lane multi-port",
			pa: &PortAllocation{
				Spec: PortAllocationSpec{
					Endpoints: []LaneEndpoint{
						{
							Lane: "bgp-1",
							EIP:  "nlb-1.example.com",
							Ports: []EndpointPort{
								{Name: "game", ListenerPort: 30000, ContainerPort: 80, Protocol: "TCP"},
								{Name: "voice", ListenerPort: 30001, ContainerPort: 8081, Protocol: "UDP"},
							},
						},
						{
							Lane: "bgp-2",
							EIP:  "nlb-2.example.com",
							Ports: []EndpointPort{
								{Name: "game", ListenerPort: 30000, ContainerPort: 80, Protocol: "TCP"},
								{Name: "voice", ListenerPort: 30001, ContainerPort: 8081, Protocol: "UDP"},
							},
						},
					},
				},
			},
			wantExternalCount: 2,
			wantInternalCount: 2,
			wantEndPoint:      "nlb-1.example.com/bgp-1,nlb-2.example.com/bgp-2",
			wantExtPort:       30000,
			wantIntPort:       80,
			wantProtocol:      corev1.ProtocolTCP,
		},
		{
			name: "empty endpoints",
			pa: &PortAllocation{
				Spec: PortAllocationSpec{
					Endpoints: []LaneEndpoint{},
				},
			},
			wantExternalCount: 0,
			wantInternalCount: 0,
		},
		{
			name: "containerPort=0 falls back to listenerPort",
			pa: &PortAllocation{
				Spec: PortAllocationSpec{
					Endpoints: []LaneEndpoint{
						{
							Lane: "bgp-1",
							EIP:  "nlb.example.com",
							Ports: []EndpointPort{
								{Name: "game", ListenerPort: 30000, ContainerPort: 0, Protocol: "TCP"},
							},
						},
					},
				},
			},
			wantExternalCount: 1,
			wantInternalCount: 1,
			wantExtPort:       30000,
			wantIntPort:       30000,
		},
		{
			name: "empty protocol defaults to TCP",
			pa: &PortAllocation{
				Spec: PortAllocationSpec{
					Endpoints: []LaneEndpoint{
						{
							Lane: "bgp-1",
							EIP:  "nlb.example.com",
							Ports: []EndpointPort{
								{Name: "game", ListenerPort: 30000, ContainerPort: 80, Protocol: ""},
							},
						},
					},
				},
			},
			wantExternalCount: 1,
			wantInternalCount: 1,
			wantProtocol:      corev1.ProtocolTCP,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ns := buildNetworkStatusV3(pod, tt.pa)
			if ns == nil {
				t.Fatal("buildNetworkStatusV3 returned nil")
			}
			if len(ns.ExternalAddresses) != tt.wantExternalCount {
				t.Errorf("ExternalAddresses count = %d, want %d", len(ns.ExternalAddresses), tt.wantExternalCount)
			}
			if len(ns.InternalAddresses) != tt.wantInternalCount {
				t.Errorf("InternalAddresses count = %d, want %d", len(ns.InternalAddresses), tt.wantInternalCount)
			}
			if tt.wantExternalCount == 0 {
				return
			}
			if tt.wantEndPoint != "" && ns.ExternalAddresses[0].EndPoint != tt.wantEndPoint {
				t.Errorf("EndPoint = %q, want %q", ns.ExternalAddresses[0].EndPoint, tt.wantEndPoint)
			}
			if tt.wantExtPort != 0 {
				got := ns.ExternalAddresses[0].Ports[0].Port.IntValue()
				if int32(got) != tt.wantExtPort {
					t.Errorf("external port = %d, want %d", got, tt.wantExtPort)
				}
			}
			if tt.wantIntPort != 0 {
				got := ns.InternalAddresses[0].Ports[0].Port.IntValue()
				if int32(got) != tt.wantIntPort {
					t.Errorf("internal port = %d, want %d", got, tt.wantIntPort)
				}
			}
			if tt.wantProtocol != "" && ns.ExternalAddresses[0].Ports[0].Protocol != tt.wantProtocol {
				t.Errorf("protocol = %v, want %v", ns.ExternalAddresses[0].Ports[0].Protocol, tt.wantProtocol)
			}
			if ns.InternalAddresses[0].IP != "10.0.0.1" {
				t.Errorf("internal IP = %q, want %q", ns.InternalAddresses[0].IP, "10.0.0.1")
			}
		})
	}
}

func newV3TestPod(name string, poolName string, paClaim string) *corev1.Pod {
	confParams := []gamekruiseiov1alpha1.NetworkConfParams{
		{Name: "NLBPoolName", Value: poolName},
	}
	confBytes, _ := json.Marshal(confParams)

	annotations := map[string]string{
		gamekruiseiov1alpha1.GameServerNetworkType: AutoNLBsV3Network,
		gamekruiseiov1alpha1.GameServerNetworkConf: string(confBytes),
	}
	if paClaim != "" {
		annotations[AnnotationPAClaim] = paClaim
	}

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   "default",
			Annotations: annotations,
		},
		Status: corev1.PodStatus{PodIP: "10.0.0.1"},
	}
}

func newV3TestPA(name string, boundPod string, phase string, endpoints []LaneEndpoint) *PortAllocation {
	pa := &PortAllocation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: PortAllocationSpec{
			BoundPod:  boundPod,
			Endpoints: endpoints,
		},
		Status: PortAllocationStatus{
			Phase: phase,
		},
	}
	pa.SetGroupVersionKind(NLBPoolGroupVersion.WithKind("PortAllocation"))
	return pa
}

func TestAutoNLBsV3OnPodAdded(t *testing.T) {
	plugin := &AutoNLBsV3Plugin{}
	ctx := context.Background()

	t.Run("sets NLBPoolName annotation", func(t *testing.T) {
		pod := newV3TestPod("pod-1", "giant-pool", "")
		c := fake.NewClientBuilder().WithScheme(scheme).Build()

		got, pluginErr := plugin.OnPodAdded(c, pod, ctx)
		if pluginErr != nil {
			t.Fatalf("unexpected error: %v", pluginErr)
		}
		if got.Annotations[AnnotationNLBPoolName] != "giant-pool" {
			t.Errorf("annotation %s = %q, want %q", AnnotationNLBPoolName, got.Annotations[AnnotationNLBPoolName], "giant-pool")
		}
	})

	t.Run("error when NLBPoolName missing", func(t *testing.T) {
		confBytes, _ := json.Marshal([]gamekruiseiov1alpha1.NetworkConfParams{})
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod-2",
				Namespace: "default",
				Annotations: map[string]string{
					gamekruiseiov1alpha1.GameServerNetworkType: AutoNLBsV3Network,
					gamekruiseiov1alpha1.GameServerNetworkConf: string(confBytes),
				},
			},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).Build()

		_, pluginErr := plugin.OnPodAdded(c, pod, ctx)
		if pluginErr == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("initializes nil annotations", func(t *testing.T) {
		confBytes, _ := json.Marshal([]gamekruiseiov1alpha1.NetworkConfParams{
			{Name: "NLBPoolName", Value: "test-pool"},
		})
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod-3",
				Namespace: "default",
				Annotations: map[string]string{
					gamekruiseiov1alpha1.GameServerNetworkType: AutoNLBsV3Network,
					gamekruiseiov1alpha1.GameServerNetworkConf: string(confBytes),
				},
			},
		}
		pod.Annotations = nil
		// Re-set the required annotations after nil (simulating OnPodAdded receiving a pod with GSS annotations already)
		pod.Annotations = map[string]string{
			gamekruiseiov1alpha1.GameServerNetworkType: AutoNLBsV3Network,
			gamekruiseiov1alpha1.GameServerNetworkConf: string(confBytes),
		}

		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		got, pluginErr := plugin.OnPodAdded(c, pod, ctx)
		if pluginErr != nil {
			t.Fatalf("unexpected error: %v", pluginErr)
		}
		if got.Annotations[AnnotationNLBPoolName] != "test-pool" {
			t.Errorf("expected annotation set, got %q", got.Annotations[AnnotationNLBPoolName])
		}
	})
}

func TestAutoNLBsV3OnPodUpdated(t *testing.T) {
	plugin := &AutoNLBsV3Plugin{}
	ctx := context.Background()

	sampleEndpoints := []LaneEndpoint{
		{
			Lane: "bgp-1",
			EIP:  "nlb-1.example.com",
			Ports: []EndpointPort{
				{Name: "game", ListenerPort: 30000, ContainerPort: 80, Protocol: "TCP"},
			},
		},
		{
			Lane: "bgp-2",
			EIP:  "nlb-2.example.com",
			Ports: []EndpointPort{
				{Name: "game", ListenerPort: 30000, ContainerPort: 80, Protocol: "TCP"},
			},
		},
	}

	t.Run("bound PA with matching pod → NetworkReady", func(t *testing.T) {
		pod := newV3TestPod("game-pod-0", "giant-pool", "pa-slot-0")
		pa := newV3TestPA("pa-slot-0", "game-pod-0", PortAllocationPhaseBound, sampleEndpoints)

		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pa).Build()

		got, pluginErr := plugin.OnPodUpdated(c, pod, ctx)
		if pluginErr != nil {
			t.Fatalf("unexpected error: %v", pluginErr)
		}

		var ns gamekruiseiov1alpha1.NetworkStatus
		if err := json.Unmarshal([]byte(got.Annotations[gamekruiseiov1alpha1.GameServerNetworkStatus]), &ns); err != nil {
			t.Fatalf("failed to unmarshal network status: %v", err)
		}
		if ns.CurrentNetworkState != gamekruiseiov1alpha1.NetworkReady {
			t.Errorf("state = %v, want NetworkReady", ns.CurrentNetworkState)
		}
		if len(ns.ExternalAddresses) != 1 {
			t.Errorf("external addresses = %d, want 1", len(ns.ExternalAddresses))
		}
	})

	t.Run("no PA claim annotation → NotReady", func(t *testing.T) {
		pod := newV3TestPod("game-pod-1", "giant-pool", "")

		c := fake.NewClientBuilder().WithScheme(scheme).Build()

		got, pluginErr := plugin.OnPodUpdated(c, pod, ctx)
		if pluginErr != nil {
			t.Fatalf("unexpected error: %v", pluginErr)
		}

		var ns gamekruiseiov1alpha1.NetworkStatus
		_ = json.Unmarshal([]byte(got.Annotations[gamekruiseiov1alpha1.GameServerNetworkStatus]), &ns)
		if ns.CurrentNetworkState != gamekruiseiov1alpha1.NetworkNotReady {
			t.Errorf("state = %v, want NetworkNotReady", ns.CurrentNetworkState)
		}
	})

	t.Run("PA not found → NotReady", func(t *testing.T) {
		pod := newV3TestPod("game-pod-2", "giant-pool", "nonexistent-pa")

		c := fake.NewClientBuilder().WithScheme(scheme).Build()

		got, pluginErr := plugin.OnPodUpdated(c, pod, ctx)
		if pluginErr != nil {
			t.Fatalf("unexpected error: %v", pluginErr)
		}

		var ns gamekruiseiov1alpha1.NetworkStatus
		_ = json.Unmarshal([]byte(got.Annotations[gamekruiseiov1alpha1.GameServerNetworkStatus]), &ns)
		if ns.CurrentNetworkState != gamekruiseiov1alpha1.NetworkNotReady {
			t.Errorf("state = %v, want NetworkNotReady", ns.CurrentNetworkState)
		}
	})

	t.Run("PA phase not Bound → NotReady", func(t *testing.T) {
		pod := newV3TestPod("game-pod-3", "giant-pool", "pa-binding")
		pa := newV3TestPA("pa-binding", "game-pod-3", PortAllocationPhaseBinding, sampleEndpoints)

		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pa).Build()

		got, pluginErr := plugin.OnPodUpdated(c, pod, ctx)
		if pluginErr != nil {
			t.Fatalf("unexpected error: %v", pluginErr)
		}

		var ns gamekruiseiov1alpha1.NetworkStatus
		_ = json.Unmarshal([]byte(got.Annotations[gamekruiseiov1alpha1.GameServerNetworkStatus]), &ns)
		if ns.CurrentNetworkState != gamekruiseiov1alpha1.NetworkNotReady {
			t.Errorf("state = %v, want NetworkNotReady", ns.CurrentNetworkState)
		}
	})

	t.Run("PA bound to different pod → NotReady", func(t *testing.T) {
		pod := newV3TestPod("game-pod-4", "giant-pool", "pa-other")
		pa := newV3TestPA("pa-other", "different-pod", PortAllocationPhaseBound, sampleEndpoints)

		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pa).Build()

		got, pluginErr := plugin.OnPodUpdated(c, pod, ctx)
		if pluginErr != nil {
			t.Fatalf("unexpected error: %v", pluginErr)
		}

		var ns gamekruiseiov1alpha1.NetworkStatus
		_ = json.Unmarshal([]byte(got.Annotations[gamekruiseiov1alpha1.GameServerNetworkStatus]), &ns)
		if ns.CurrentNetworkState != gamekruiseiov1alpha1.NetworkNotReady {
			t.Errorf("state = %v, want NetworkNotReady", ns.CurrentNetworkState)
		}
	})

	t.Run("PA bound but empty endpoints → NotReady", func(t *testing.T) {
		pod := newV3TestPod("game-pod-5", "giant-pool", "pa-empty")
		pa := newV3TestPA("pa-empty", "game-pod-5", PortAllocationPhaseBound, []LaneEndpoint{})

		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pa).Build()

		got, pluginErr := plugin.OnPodUpdated(c, pod, ctx)
		if pluginErr != nil {
			t.Fatalf("unexpected error: %v", pluginErr)
		}

		var ns gamekruiseiov1alpha1.NetworkStatus
		_ = json.Unmarshal([]byte(got.Annotations[gamekruiseiov1alpha1.GameServerNetworkStatus]), &ns)
		if ns.CurrentNetworkState != gamekruiseiov1alpha1.NetworkNotReady {
			t.Errorf("state = %v, want NetworkNotReady", ns.CurrentNetworkState)
		}
	})
}

func TestAutoNLBsV3OnPodDeleted(t *testing.T) {
	plugin := &AutoNLBsV3Plugin{}
	ctx := context.Background()

	pod := newV3TestPod("pod-del", "giant-pool", "pa-del")
	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	pluginErr := plugin.OnPodDeleted(c, pod, ctx)
	if pluginErr != nil {
		t.Fatalf("OnPodDeleted should be no-op, got error: %v", pluginErr)
	}
}

func TestAutoNLBsV3PluginMetadata(t *testing.T) {
	plugin := &AutoNLBsV3Plugin{}

	if plugin.Name() != AutoNLBsV3Network {
		t.Errorf("Name() = %q, want %q", plugin.Name(), AutoNLBsV3Network)
	}
	if plugin.Alias() != AliasAutoNLBs {
		t.Errorf("Alias() = %q, want %q", plugin.Alias(), AliasAutoNLBs)
	}

	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	if err := plugin.Init(c, nil, context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
}
