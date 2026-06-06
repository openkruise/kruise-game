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

package kubernetes

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
)

func TestParseGcpNLBConfig(t *testing.T) {
	tests := []struct {
		name string
		conf []gamekruiseiov1alpha1.NetworkConfParams
		want *gcpNLBConfig
	}{
		{
			name: "single tcp port, defaults",
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{Name: PortProtocolsConfigName, Value: "9000"},
			},
			want: &gcpNLBConfig{
				ports:                 []int{9000},
				protocols:             []corev1.Protocol{corev1.ProtocolTCP},
				isFixed:               false,
				externalTrafficPolicy: corev1.ServiceExternalTrafficPolicyTypeLocal,
				loadBalancerIP:        "",
				minPort:               0,
			},
		},
		{
			name: "dual protocol same port + shared IP + offset + fixed",
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{Name: PortProtocolsConfigName, Value: "9000/TCP,9000/UDP"},
				{Name: GcpNLBLoadBalancerIPsConfigName, Value: "34.80.242.156"},
				{Name: GcpNLBMinPortConfigName, Value: "9000"},
				{Name: FixedKey, Value: "true"},
				{Name: GcpNLBExternalTrafficPolicyConfigName, Value: "Cluster"},
			},
			want: &gcpNLBConfig{
				ports:                 []int{9000, 9000},
				protocols:             []corev1.Protocol{corev1.ProtocolTCP, corev1.ProtocolUDP},
				isFixed:               true,
				externalTrafficPolicy: corev1.ServiceExternalTrafficPolicyTypeCluster,
				loadBalancerIP:        "34.80.242.156",
				minPort:               9000,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseGcpNLBConfig(tt.conf)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseGcpNLBConfig() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestUniquePorts(t *testing.T) {
	tests := []struct {
		in   []int
		want []int
	}{
		{in: []int{9000, 9000}, want: []int{9000}},                   // TCP+UDP same port collapse
		{in: []int{9000, 7777}, want: []int{9000, 7777}},             // distinct, order preserved
		{in: []int{9000, 7777, 9000}, want: []int{9000, 7777}},       // dedup keep first-seen order
		{in: []int{}, want: []int{}},                                 // empty
	}
	for _, tt := range tests {
		got := uniquePorts(tt.in)
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("uniquePorts(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func newPod(name string) *corev1.Pod {
	return &corev1.Pod{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns", UID: types.UID("uid-" + name)},
	}
}

// TestConsGcpNLBSvcDualProtocol verifies that exposing the same container port for
// both TCP and UDP produces unique ServicePort names (the bug that breaks the
// upstream Kubernetes-NodePort plugin) and a LoadBalancer Service with the GKE
// regional external load balancer class.
func TestConsGcpNLBSvcDualProtocol(t *testing.T) {
	conf := &gcpNLBConfig{
		ports:                 []int{9000, 9000},
		protocols:             []corev1.Protocol{corev1.ProtocolTCP, corev1.ProtocolUDP},
		isFixed:               false,
		externalTrafficPolicy: corev1.ServiceExternalTrafficPolicyTypeLocal,
	}
	svc := consGcpNLBSvc(conf, newPod("game-0"), nil, nil)

	if svc.Spec.Type != corev1.ServiceTypeLoadBalancer {
		t.Errorf("expected type LoadBalancer, got %v", svc.Spec.Type)
	}
	if svc.Spec.LoadBalancerClass == nil || *svc.Spec.LoadBalancerClass != GcpNLBLoadBalancerClass {
		t.Errorf("expected loadBalancerClass %q, got %v", GcpNLBLoadBalancerClass, svc.Spec.LoadBalancerClass)
	}
	if len(svc.Spec.Ports) != 2 {
		t.Fatalf("expected 2 ports, got %d", len(svc.Spec.Ports))
	}
	// Names must be unique, otherwise Kubernetes rejects the multi-port Service.
	if svc.Spec.Ports[0].Name == svc.Spec.Ports[1].Name {
		t.Errorf("duplicate port name %q — Service would be rejected", svc.Spec.Ports[0].Name)
	}
	wantNames := map[string]corev1.Protocol{"9000-tcp": corev1.ProtocolTCP, "9000-udp": corev1.ProtocolUDP}
	for _, p := range svc.Spec.Ports {
		proto, ok := wantNames[p.Name]
		if !ok {
			t.Errorf("unexpected port name %q", p.Name)
		}
		if p.Protocol != proto {
			t.Errorf("port %q expected protocol %v, got %v", p.Name, proto, p.Protocol)
		}
		if p.Port != 9000 || p.TargetPort != intstr.FromInt(9000) {
			t.Errorf("port %q expected external/target 9000, got port=%d target=%v", p.Name, p.Port, p.TargetPort)
		}
	}
}

// TestConsGcpNLBSvcPortOffset verifies that with MinPort set, each pod ordinal gets a
// distinct external port while the container targetPort stays fixed, and that TCP+UDP
// on the same container port share one external port.
func TestConsGcpNLBSvcPortOffset(t *testing.T) {
	mkConf := func() *gcpNLBConfig {
		return &gcpNLBConfig{
			ports:                 []int{9000, 9000},
			protocols:             []corev1.Protocol{corev1.ProtocolTCP, corev1.ProtocolUDP},
			minPort:               9000,
			loadBalancerIP:        "34.80.242.156",
			externalTrafficPolicy: corev1.ServiceExternalTrafficPolicyTypeLocal,
		}
	}

	cases := []struct {
		podName      string
		wantExtPort  int32
	}{
		{"game-0", 9000},
		{"game-1", 9001},
		{"game-2", 9002},
	}

	for _, c := range cases {
		conf := mkConf()
		conf.podIndex = indexFromName(c.podName)
		svc := consGcpNLBSvc(conf, newPod(c.podName), nil, nil)

		if svc.Spec.LoadBalancerIP != "34.80.242.156" {
			t.Errorf("%s: expected shared loadBalancerIP, got %q", c.podName, svc.Spec.LoadBalancerIP)
		}
		for _, p := range svc.Spec.Ports {
			if p.Port != c.wantExtPort {
				t.Errorf("%s: expected external port %d, got %d", c.podName, c.wantExtPort, p.Port)
			}
			if p.TargetPort != intstr.FromInt(9000) {
				t.Errorf("%s: expected targetPort 9000, got %v", c.podName, p.TargetPort)
			}
		}
		// TCP and UDP must share the SAME external port for the same container port.
		if svc.Spec.Ports[0].Port != svc.Spec.Ports[1].Port {
			t.Errorf("%s: TCP and UDP should share the same external port, got %d and %d",
				c.podName, svc.Spec.Ports[0].Port, svc.Spec.Ports[1].Port)
		}
	}
}

// indexFromName mirrors util.GetIndexFromGsName for the test without importing it,
// keeping the test focused on the plugin's own logic.
func indexFromName(name string) int {
	switch name {
	case "game-0":
		return 0
	case "game-1":
		return 1
	case "game-2":
		return 2
	}
	return 0
}
