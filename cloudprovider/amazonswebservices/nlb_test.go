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
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"testing"

	ackv1alpha1 "github.com/aws-controllers-k8s/elbv2-controller/apis/v1alpha1"
	ackv1alpha1core "github.com/aws-controllers-k8s/runtime/apis/core/v1alpha1"
	"github.com/kr/pretty"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	elbv2api "sigs.k8s.io/aws-load-balancer-controller/apis/elbv2/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
)

func TestAllocateDeAllocate(t *testing.T) {
	tests := []struct {
		loadBalancerARNs []string
		nlb              *NlbPlugin
		num              int
		podKey           string
	}{
		{
			loadBalancerARNs: []string{"arn:aws:elasticloadbalancing:us-east-1:888888888888:loadbalancer/net/aaa/3b332e6841f23870",
				"arn:aws:elasticloadbalancing:us-east-1:000000000000:loadbalancer/net/bbb/5fe74944d794d27e"},
			nlb: &NlbPlugin{
				maxPort:     int32(1000),
				minPort:     int32(951),
				cache:       make(map[string]portAllocated),
				podAllocate: make(map[string]*nlbPorts),
				mutex:       sync.RWMutex{},
			},
			podKey: "xxx/xxx",
			num:    3,
		},
		{
			loadBalancerARNs: []string{"arn:aws:elasticloadbalancing:us-east-1:888888888888:loadbalancer/net/aaa/3b332e6841f23870",
				"arn:aws:elasticloadbalancing:us-east-1:000000000000:loadbalancer/net/bbb/5fe74944d794d27e"},
			nlb: &NlbPlugin{
				maxPort:     int32(955),
				minPort:     int32(951),
				cache:       make(map[string]portAllocated),
				podAllocate: make(map[string]*nlbPorts),
				mutex:       sync.RWMutex{},
			},
			podKey: "xxx/xxx",
			num:    6,
		},
	}
	for _, test := range tests {
		allocatedPorts := test.nlb.allocate(test.loadBalancerARNs, test.num, test.podKey, allocatePolicyDefault)
		if int(test.nlb.maxPort-test.nlb.minPort+1) < test.num && allocatedPorts != nil {
			t.Errorf("insufficient available ports but NLB was still allocated: %s",
				pretty.Sprint(allocatedPorts))
		}
		if allocatedPorts == nil {
			continue
		}
		if _, exist := test.nlb.podAllocate[test.podKey]; !exist {
			t.Errorf("podAllocate[%s] is empty after allocated", test.podKey)
		}
		for _, port := range allocatedPorts.ports {
			if port > test.nlb.maxPort || port < test.nlb.minPort {
				t.Errorf("allocate port %d, unexpected", port)
			}
			if test.nlb.cache[allocatedPorts.arn][port] == false {
				t.Errorf("allocate port %d failed", port)
			}
		}

		test.nlb.deAllocate(test.podKey)
		for _, port := range allocatedPorts.ports {
			if test.nlb.cache[allocatedPorts.arn][port] == true {
				t.Errorf("deAllocate port %d failed", port)
			}
		}
		if _, exist := test.nlb.podAllocate[test.podKey]; exist {
			t.Errorf("podAllocate[%s] is not empty after deallocated", test.podKey)
		}
	}
}

func TestParseLbConfig(t *testing.T) {
	tests := []struct {
		conf             []gamekruiseiov1alpha1.NetworkConfParams
		loadBalancerARNs []string
		healthCheck      *healthCheck
		backends         []*backend
		isFixed          bool
	}{
		{
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{
					Name:  NlbARNsConfigName,
					Value: "arn:aws:elasticloadbalancing:us-east-1:888888888888:loadbalancer/net/aaa/3b332e6841f23870",
				},
				{
					Name:  PortProtocolsConfigName,
					Value: "80",
				},
			},
			loadBalancerARNs: []string{"arn:aws:elasticloadbalancing:us-east-1:888888888888:loadbalancer/net/aaa/3b332e6841f23870"},
			healthCheck:      &healthCheck{},
			backends: []*backend{
				{
					targetPort: 80,
					protocol:   corev1.ProtocolTCP,
				},
			},
			isFixed: false,
		},
		{
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{
					Name:  NlbARNsConfigName,
					Value: "arn:aws:elasticloadbalancing:us-east-1:888888888888:loadbalancer/net/aaa/3b332e6841f23870,arn:aws:elasticloadbalancing:us-east-1:000000000000:loadbalancer/net/bbb/5fe74944d794d27e",
				},
				{
					Name:  NlbHealthCheckConfigName,
					Value: "healthCheckEnabled:true,healthCheckIntervalSeconds:30,healthCheckPath:/health,healthCheckPort:8081,healthCheckProtocol:HTTP,healthCheckTimeoutSeconds:10,healthyThresholdCount:5,unhealthyThresholdCount:2",
				},
				{
					Name:  PortProtocolsConfigName,
					Value: "10000/UDP,10001,10002/TCP",
				},
				{
					Name:  FixedConfigName,
					Value: "true",
				},
			},
			loadBalancerARNs: []string{"arn:aws:elasticloadbalancing:us-east-1:888888888888:loadbalancer/net/aaa/3b332e6841f23870", "arn:aws:elasticloadbalancing:us-east-1:000000000000:loadbalancer/net/bbb/5fe74944d794d27e"},
			healthCheck: &healthCheck{
				healthCheckEnabled:         ptr.To[bool](true),
				healthCheckIntervalSeconds: ptr.To[int64](30),
				healthCheckPath:            ptr.To[string]("/health"),
				healthCheckPort:            ptr.To[string]("8081"),
				healthCheckProtocol:        ptr.To[string]("HTTP"),
				healthCheckTimeoutSeconds:  ptr.To[int64](10),
				healthyThresholdCount:      ptr.To[int64](5),
				unhealthyThresholdCount:    ptr.To[int64](2),
			},
			backends: []*backend{
				{
					targetPort: 10000,
					protocol:   corev1.ProtocolUDP,
				},
				{
					targetPort: 10001,
					protocol:   corev1.ProtocolTCP,
				},
				{
					targetPort: 10002,
					protocol:   corev1.ProtocolTCP,
				},
			},
			isFixed: true,
		},
	}

	for _, test := range tests {
		sc := parseLbConfig(test.conf)
		if !reflect.DeepEqual(test.loadBalancerARNs, sc.loadBalancerARNs) {
			t.Errorf("loadBalancerARNs expect: %v, actual: %v", test.loadBalancerARNs, sc.loadBalancerARNs)
		}
		if !reflect.DeepEqual(test.healthCheck, sc.healthCheck) {
			t.Errorf("healthCheck expect: %s, actual: %s", pretty.Sprint(test.healthCheck), pretty.Sprint(sc.healthCheck))
		}
		if !reflect.DeepEqual(test.backends, sc.backends) {
			t.Errorf("ports expect: %s, actual: %s", pretty.Sprint(test.backends), pretty.Sprint(sc.backends))
		}
		if test.isFixed != sc.isFixed {
			t.Errorf("isFixed expect: %v, actual: %v", test.isFixed, sc.isFixed)
		}
	}
}

func TestInitLbCache(t *testing.T) {
	test := struct {
		n           *NlbPlugin
		svcList     []corev1.Service
		cache       map[string]portAllocated
		podAllocate map[string]*nlbPorts
	}{
		n: &NlbPlugin{
			minPort: 951,
			maxPort: 1000,
		},

		cache: map[string]portAllocated{
			"arn:aws:elasticloadbalancing:us-east-1:888888888888:loadbalancer/net/aaa/3b332e6841f23870": map[int32]bool{
				988: true,
			},
			"arn:aws:elasticloadbalancing:us-east-1:000000000000:loadbalancer/net/bbb/5fe74944d794d27e": map[int32]bool{
				951: true,
				999: true,
			},
		},
		podAllocate: map[string]*nlbPorts{
			"ns-0/name-0": {
				arn:   "arn:aws:elasticloadbalancing:us-east-1:888888888888:loadbalancer/net/aaa/3b332e6841f23870",
				ports: []int32{988},
			},
			"ns-1/name-1": {
				arn:   "arn:aws:elasticloadbalancing:us-east-1:000000000000:loadbalancer/net/bbb/5fe74944d794d27e",
				ports: []int32{951, 999},
			},
		},
		svcList: []corev1.Service{
			{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						NlbARNAnnoKey: "arn:aws:elasticloadbalancing:us-east-1:888888888888:loadbalancer/net/aaa/3b332e6841f23870",
					},
					Labels:    map[string]string{ResourceTagKey: ResourceTagValue},
					Namespace: "ns-0",
					Name:      "name-0",
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeLoadBalancer,
					Selector: map[string]string{
						SvcSelectorKey: "pod-A",
					},
					Ports: []corev1.ServicePort{
						{
							TargetPort: intstr.FromInt(80),
							Port:       988,
							Protocol:   corev1.ProtocolTCP,
						},
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						NlbARNAnnoKey: "arn:aws:elasticloadbalancing:us-east-1:000000000000:loadbalancer/net/bbb/5fe74944d794d27e",
					},
					Labels:    map[string]string{ResourceTagKey: ResourceTagValue},
					Namespace: "ns-1",
					Name:      "name-1",
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeLoadBalancer,
					Selector: map[string]string{
						SvcSelectorKey: "pod-B",
					},
					Ports: []corev1.ServicePort{
						{
							TargetPort: intstr.FromInt(8080),
							Port:       951,
							Protocol:   corev1.ProtocolTCP,
						},
						{
							TargetPort: intstr.FromInt(8081),
							Port:       999,
							Protocol:   corev1.ProtocolTCP,
						},
					},
				},
			},
		},
	}

	test.n.initLbCache(test.svcList)
	for arn, pa := range test.cache {
		for port, isAllocated := range pa {
			if test.n.cache[arn][port] != isAllocated {
				t.Errorf("nlb arn %s port %d isAllocated, expect: %t, actual: %t", arn, port, isAllocated, test.n.cache[arn][port])
			}
		}
	}
	if !reflect.DeepEqual(test.n.podAllocate, test.podAllocate) {
		t.Errorf("podAllocate expect %v, but actully got %v", test.podAllocate, test.n.podAllocate)
	}
}

func TestConsSvcPorts(t *testing.T) {
	tests := []struct {
		name      string
		backends  []*backend
		ports     []int32
		wantNames []string
	}{
		{
			name: "same port TCP and UDP gets protocol suffix",
			backends: []*backend{
				{targetPort: 8601, protocol: corev1.ProtocolTCP},
				{targetPort: 8601, protocol: corev1.ProtocolUDP},
			},
			ports:     []int32{6000, 6001},
			wantNames: []string{"8601-tcp", "8601-udp"},
		},
		{
			name: "single protocol keeps numeric-only name (backward compatible)",
			backends: []*backend{
				{targetPort: 8601, protocol: corev1.ProtocolTCP},
			},
			ports:     []int32{6000},
			wantNames: []string{"8601"},
		},
		{
			name: "distinct ports keep numeric-only names",
			backends: []*backend{
				{targetPort: 8601, protocol: corev1.ProtocolTCP},
				{targetPort: 8602, protocol: corev1.ProtocolUDP},
			},
			ports:     []int32{6000, 6001},
			wantNames: []string{"8601", "8602"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svcPorts := consSvcPorts(tt.backends, tt.ports)
			if len(svcPorts) != len(tt.wantNames) {
				t.Fatalf("got %d ports, want %d", len(svcPorts), len(tt.wantNames))
			}
			seen := make(map[string]bool)
			for i, p := range svcPorts {
				if p.Name != tt.wantNames[i] {
					t.Errorf("port[%d].Name = %q, want %q", i, p.Name, tt.wantNames[i])
				}
				if seen[p.Name] {
					t.Errorf("duplicate ServicePort name %q (invalid Service)", p.Name)
				}
				seen[p.Name] = true
				if p.Port != tt.ports[i] {
					t.Errorf("port[%d].Port = %d, want %d", i, p.Port, tt.ports[i])
				}
				if p.TargetPort.IntValue() != tt.backends[i].targetPort {
					t.Errorf("port[%d].TargetPort = %d, want %d", i, p.TargetPort.IntValue(), tt.backends[i].targetPort)
				}
			}
		})
	}
}

func TestConsSvcPortsTCPUDP(t *testing.T) {
	tests := []struct {
		name     string
		backends []*backend
		ports    []int32
		want     []corev1.ServicePort
	}{
		{
			name: "single TCPUDP backend expands to TCP+UDP sharing the frontend port",
			backends: []*backend{
				{targetPort: 8601, protocol: ProtocolTCPUDP},
			},
			ports: []int32{30001},
			want: []corev1.ServicePort{
				{Name: "8601-tcp", Port: 30001, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt(8601)},
				{Name: "8601-udp", Port: 30001, Protocol: corev1.ProtocolUDP, TargetPort: intstr.FromInt(8601)},
			},
		},
		{
			name: "two TCPUDP backends each get their own frontend port",
			backends: []*backend{
				{targetPort: 8601, protocol: ProtocolTCPUDP},
				{targetPort: 8661, protocol: ProtocolTCPUDP},
			},
			ports: []int32{30001, 30002},
			want: []corev1.ServicePort{
				{Name: "8601-tcp", Port: 30001, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt(8601)},
				{Name: "8601-udp", Port: 30001, Protocol: corev1.ProtocolUDP, TargetPort: intstr.FromInt(8601)},
				{Name: "8661-tcp", Port: 30002, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt(8661)},
				{Name: "8661-udp", Port: 30002, Protocol: corev1.ProtocolUDP, TargetPort: intstr.FromInt(8661)},
			},
		},
		{
			name: "TCPUDP coexists with a single-protocol backend",
			backends: []*backend{
				{targetPort: 8601, protocol: ProtocolTCPUDP},
				{targetPort: 9000, protocol: corev1.ProtocolTCP},
			},
			ports: []int32{30001, 30002},
			want: []corev1.ServicePort{
				{Name: "8601-tcp", Port: 30001, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt(8601)},
				{Name: "8601-udp", Port: 30001, Protocol: corev1.ProtocolUDP, TargetPort: intstr.FromInt(8601)},
				{Name: "9000", Port: 30002, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt(9000)},
			},
		},
		{
			name: "DNS-style port 53 TCPUDP",
			backends: []*backend{
				{targetPort: 53, protocol: ProtocolTCPUDP},
			},
			ports: []int32{32768},
			want: []corev1.ServicePort{
				{Name: "53-tcp", Port: 32768, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt(53)},
				{Name: "53-udp", Port: 32768, Protocol: corev1.ProtocolUDP, TargetPort: intstr.FromInt(53)},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := consSvcPorts(tt.backends, tt.ports)

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("consSvcPorts mismatch\n got:  %s\nwant: %s",
					pretty.Sprint(got), pretty.Sprint(tt.want))
			}

			// All ServicePort names must be unique (otherwise the K8s API
			// server will reject the Service with a Duplicate value error).
			seen := make(map[string]bool)
			for _, p := range got {
				if seen[p.Name] {
					t.Errorf("duplicate ServicePort name %q", p.Name)
				}
				seen[p.Name] = true
			}

			// For each TCPUDP backend, the two emitted ServicePorts must
			// share the same frontend Port so they land on a single AWS
			// TCP_UDP listener.
			for _, b := range tt.backends {
				if b.protocol != ProtocolTCPUDP {
					continue
				}
				var frontendPorts []int32
				var protocols []corev1.Protocol
				for _, p := range got {
					if int(p.TargetPort.IntValue()) == b.targetPort {
						frontendPorts = append(frontendPorts, p.Port)
						protocols = append(protocols, p.Protocol)
					}
				}
				if len(frontendPorts) != 2 {
					t.Errorf("backend targetPort=%d: expected 2 emitted ServicePorts, got %d",
						b.targetPort, len(frontendPorts))
					continue
				}
				if frontendPorts[0] != frontendPorts[1] {
					t.Errorf("backend targetPort=%d: expected shared frontend Port, got %v",
						b.targetPort, frontendPorts)
				}
				wantProtos := map[corev1.Protocol]bool{corev1.ProtocolTCP: false, corev1.ProtocolUDP: false}
				for _, pr := range protocols {
					if _, ok := wantProtos[pr]; ok {
						wantProtos[pr] = true
					}
				}
				for pr, seenIt := range wantProtos {
					if !seenIt {
						t.Errorf("backend targetPort=%d: missing %s protocol entry", b.targetPort, pr)
					}
				}
			}
		})
	}
}

func TestParseLbConfigTCPUDP(t *testing.T) {
	tests := []struct {
		name         string
		value        string
		wantBackends []*backend
	}{
		{
			name:  "single TCPUDP entry",
			value: "8601/TCPUDP",
			wantBackends: []*backend{
				{targetPort: 8601, protocol: ProtocolTCPUDP},
			},
		},
		{
			name:  "multiple TCPUDP entries",
			value: "8601/TCPUDP,8661/TCPUDP",
			wantBackends: []*backend{
				{targetPort: 8601, protocol: ProtocolTCPUDP},
				{targetPort: 8661, protocol: ProtocolTCPUDP},
			},
		},
		{
			name:  "TCPUDP mixed with TCP and UDP",
			value: "53/TCPUDP,80/TCP,9000/UDP",
			wantBackends: []*backend{
				{targetPort: 53, protocol: ProtocolTCPUDP},
				{targetPort: 80, protocol: corev1.ProtocolTCP},
				{targetPort: 9000, protocol: corev1.ProtocolUDP},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conf := []gamekruiseiov1alpha1.NetworkConfParams{
				{
					Name:  NlbARNsConfigName,
					Value: "arn:aws:elasticloadbalancing:us-east-1:888888888888:loadbalancer/net/aaa/3b332e6841f23870",
				},
				{
					Name:  PortProtocolsConfigName,
					Value: tt.value,
				},
			}
			sc := parseLbConfig(conf)
			if !reflect.DeepEqual(sc.backends, tt.wantBackends) {
				t.Errorf("backends mismatch\n got:  %s\nwant: %s",
					pretty.Sprint(sc.backends), pretty.Sprint(tt.wantBackends))
			}
		})
	}
}

func TestAWSTargetGroupProtocol(t *testing.T) {
	tests := []struct {
		name string
		in   corev1.Protocol
		want string
	}{
		{"TCP passes through", corev1.ProtocolTCP, "TCP"},
		{"UDP passes through", corev1.ProtocolUDP, "UDP"},
		{"TCPUDP translates to AWS TCP_UDP", ProtocolTCPUDP, "TCP_UDP"},
		{"unknown values pass through unchanged", corev1.Protocol("SCTP"), "SCTP"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := awsTargetGroupProtocol(tt.in)
			if got != tt.want {
				t.Errorf("awsTargetGroupProtocol(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestConfigHashIgnoresARNs verifies that changing the NlbARNs list does NOT
// change configHash (so already-allocated pods are not forced to reconfigure
// when an ARN is added/removed), while changing any other field DOES change it
// (so legitimate config changes still trigger a reconfigure).
func TestConfigHashIgnoresARNs(t *testing.T) {
	base := &nlbConfig{
		loadBalancerARNs: []string{"arn:aaa"},
		vpcID:            "vpc-1",
		backends:         []*backend{{targetPort: 8601, protocol: ProtocolTCPUDP}},
		isFixed:          true,
		healthCheck:      &healthCheck{healthCheckProtocol: ptr.To("TCP")},
	}

	// Adding an ARN must NOT change the hash.
	addedARN := *base
	addedARN.loadBalancerARNs = []string{"arn:aaa", "arn:bbb"}
	if base.configHash() != addedARN.configHash() {
		t.Errorf("configHash changed when only NlbARNs changed; adding an ARN must not reconfigure existing pods")
	}

	// A completely different ARN set must NOT change the hash either.
	swapped := *base
	swapped.loadBalancerARNs = []string{"arn:zzz"}
	if base.configHash() != swapped.configHash() {
		t.Errorf("configHash changed when only the ARN set changed")
	}

	// Changing a real field (ports) MUST change the hash.
	changedPorts := *base
	changedPorts.backends = []*backend{{targetPort: 9000, protocol: corev1.ProtocolTCP}}
	if base.configHash() == changedPorts.configHash() {
		t.Errorf("configHash did not change when backends changed; legitimate reconfigure would be missed")
	}

	// Changing health check MUST change the hash.
	changedHC := *base
	changedHC.healthCheck = &healthCheck{healthCheckProtocol: ptr.To("HTTP")}
	if base.configHash() == changedHC.configHash() {
		t.Errorf("configHash did not change when healthCheck changed")
	}

	// Changing isFixed MUST change the hash.
	changedFixed := *base
	changedFixed.isFixed = false
	if base.configHash() == changedFixed.configHash() {
		t.Errorf("configHash did not change when isFixed changed")
	}
}

func newNlbForPolicy() *NlbPlugin {
	return &NlbPlugin{
		maxPort:     int32(953), // 3 ports per NLB: 951,952,953
		minPort:     int32(951),
		cache:       make(map[string]portAllocated),
		podAllocate: make(map[string]*nlbPorts),
		mutex:       sync.RWMutex{},
	}
}

// default(first-fit/溢出): 先填满第一个 NLB, 再溢出第二个。
func TestAllocatePolicyDefaultSpillover(t *testing.T) {
	arnA := "arn:aws:elasticloadbalancing:us-east-1:1:loadbalancer/net/aaa/1"
	arnB := "arn:aws:elasticloadbalancing:us-east-1:2:loadbalancer/net/bbb/2"
	n := newNlbForPolicy()
	// 4 个单端口服: 3 个应全落 A(填满), 第 4 个溢出 B
	got := map[string]int{}
	for i := 0; i < 4; i++ {
		p := n.allocate([]string{arnA, arnB}, 1, "ns/p"+string(rune('0'+i)), allocatePolicyDefault)
		if p == nil {
			t.Fatalf("allocate %d returned nil", i)
		}
		got[p.arn]++
	}
	if got[arnA] != 3 || got[arnB] != 1 {
		t.Errorf("default spillover want A=3,B=1; got A=%d,B=%d", got[arnA], got[arnB])
	}
}

// balanced: 摊平到空位最多的 NLB, 前两个应分落 A、B(各 1), 而非都堆 A。
func TestAllocatePolicyBalancedSpread(t *testing.T) {
	arnA := "arn:aws:elasticloadbalancing:us-east-1:1:loadbalancer/net/aaa/1"
	arnB := "arn:aws:elasticloadbalancing:us-east-1:2:loadbalancer/net/bbb/2"
	n := newNlbForPolicy()
	p0 := n.allocate([]string{arnA, arnB}, 1, "ns/p0", allocatePolicyBalanced)
	p1 := n.allocate([]string{arnA, arnB}, 1, "ns/p1", allocatePolicyBalanced)
	if p0 == nil || p1 == nil {
		t.Fatal("balanced allocate returned nil")
	}
	if p0.arn == p1.arn {
		t.Errorf("balanced should spread first two pods across NLBs, both landed on %s", p0.arn)
	}
}

// 同一端口号可分配给不同 NLB(cache 按 ARN 隔离, 不互相占用)。
func TestSamePortDifferentNLBs(t *testing.T) {
	arnA := "arn:aws:elasticloadbalancing:us-east-1:1:loadbalancer/net/aaa/1"
	arnB := "arn:aws:elasticloadbalancing:us-east-1:2:loadbalancer/net/bbb/2"
	n := newNlbForPolicy()
	// 用 balanced 让两个服分落 A、B, 二者首个端口都应是 minPort(951), 互不冲突
	p0 := n.allocate([]string{arnA, arnB}, 1, "ns/p0", allocatePolicyBalanced)
	p1 := n.allocate([]string{arnA, arnB}, 1, "ns/p1", allocatePolicyBalanced)
	if p0 == nil || p1 == nil {
		t.Fatal("allocate returned nil")
	}
	if p0.arn == p1.arn {
		t.Fatalf("expected different NLBs, both on %s", p0.arn)
	}
	if p0.ports[0] != 951 || p1.ports[0] != 951 {
		t.Errorf("same port 951 should be usable on both NLBs; got p0=%d p1=%d", p0.ports[0], p1.ports[0])
	}
	// 且各自 cache 独立标记
	if !n.cache[p0.arn][951] || !n.cache[p1.arn][951] {
		t.Errorf("port 951 should be marked occupied independently on each NLB")
	}
}

// OnPodAdded 应提前分配端口并预置 readiness gate, gate 名 == TGB 名(target-health.elbv2.k8s.aws/<pod>-<port>)。
// 无 NLB 网络注解的 pod: OnPodAdded 原样返回, 不分配端口、不注入 gate。
func TestOnPodAddedNonNlbPodUntouched(t *testing.T) {
	n := newNlbForPolicy()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "plain-0", Namespace: "default"},
	}
	got, perr := n.OnPodAdded(nil, pod, nil)
	if perr != nil {
		t.Fatalf("OnPodAdded error: %v", perr)
	}
	if len(got.Spec.ReadinessGates) != 0 {
		t.Errorf("non-NLB pod should not get readiness gates, got %#v", got.Spec.ReadinessGates)
	}
	if _, ok := n.podAllocate["default/plain-0"]; ok {
		t.Errorf("non-NLB pod should not allocate ports")
	}
}

func TestValidateLbConfig(t *testing.T) {
	validARN := []string{"arn:aws:elasticloadbalancing:us-east-1:888888888888:loadbalancer/net/aaa/3b332e6841f23870"}
	validBackends := []*backend{{targetPort: 8601, protocol: corev1.ProtocolTCP}}
	base := func() *nlbConfig {
		return &nlbConfig{
			loadBalancerARNs: validARN,
			backends:         validBackends,
			healthCheck:      &healthCheck{},
		}
	}
	tests := []struct {
		name      string
		config    *nlbConfig
		expectErr bool
	}{
		{"nil config", nil, false},
		{"valid minimal", base(), false},
		{"missing NlbARNs", &nlbConfig{backends: validBackends, healthCheck: &healthCheck{}}, true},
		{"missing PortProtocols", &nlbConfig{loadBalancerARNs: validARN, healthCheck: &healthCheck{}}, true},
		{"port too low", &nlbConfig{loadBalancerARNs: validARN, backends: []*backend{{targetPort: 0, protocol: corev1.ProtocolTCP}}, healthCheck: &healthCheck{}}, true},
		{"port too high", &nlbConfig{loadBalancerARNs: validARN, backends: []*backend{{targetPort: 70000, protocol: corev1.ProtocolTCP}}, healthCheck: &healthCheck{}}, true},
		{"bad protocol", &nlbConfig{loadBalancerARNs: validARN, backends: []*backend{{targetPort: 8601, protocol: corev1.Protocol("SCTP")}}, healthCheck: &healthCheck{}}, true},
		{"TCPUDP protocol ok", &nlbConfig{loadBalancerARNs: validARN, backends: []*backend{{targetPort: 8601, protocol: ProtocolTCPUDP}}, healthCheck: &healthCheck{}}, false},
		{"healthCheckEnabled false rejected", &nlbConfig{loadBalancerARNs: validARN, backends: validBackends, healthCheck: &healthCheck{healthCheckEnabled: ptr.To[bool](false)}}, true},
		{"healthCheckEnabled true ok", &nlbConfig{loadBalancerARNs: validARN, backends: validBackends, healthCheck: &healthCheck{healthCheckEnabled: ptr.To[bool](true)}}, false},
		{"healthCheckProtocol UDP rejected", &nlbConfig{loadBalancerARNs: validARN, backends: validBackends, healthCheck: &healthCheck{healthCheckProtocol: ptr.To[string]("UDP")}}, true},
		{"healthCheckProtocol TCP ok", &nlbConfig{loadBalancerARNs: validARN, backends: validBackends, healthCheck: &healthCheck{healthCheckProtocol: ptr.To[string]("TCP")}}, false},
		{"healthCheckProtocol HTTP ok", &nlbConfig{loadBalancerARNs: validARN, backends: validBackends, healthCheck: &healthCheck{healthCheckProtocol: ptr.To[string]("HTTP")}}, false},
		{"interval out of range", &nlbConfig{loadBalancerARNs: validARN, backends: validBackends, healthCheck: &healthCheck{healthCheckIntervalSeconds: ptr.To[int64](1)}}, true},
		{"interval ok", &nlbConfig{loadBalancerARNs: validARN, backends: validBackends, healthCheck: &healthCheck{healthCheckIntervalSeconds: ptr.To[int64](30)}}, false},
		{"timeout out of range", &nlbConfig{loadBalancerARNs: validARN, backends: validBackends, healthCheck: &healthCheck{healthCheckTimeoutSeconds: ptr.To[int64](200)}}, true},
		{"healthy threshold out of range", &nlbConfig{loadBalancerARNs: validARN, backends: validBackends, healthCheck: &healthCheck{healthyThresholdCount: ptr.To[int64](11)}}, true},
		{"unhealthy threshold out of range", &nlbConfig{loadBalancerARNs: validARN, backends: validBackends, healthCheck: &healthCheck{unhealthyThresholdCount: ptr.To[int64](1)}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateLbConfig(tt.config)
			if tt.expectErr && err == nil {
				t.Errorf("%s: expected error, got nil", tt.name)
			}
			if !tt.expectErr && err != nil {
				t.Errorf("%s: expected no error, got %v", tt.name, err)
			}
		})
	}
}

// TestOnPodDeleted exercises every branch of OnPodDeleted's port-release
// decision (nlb.go: OnPodDeleted). It is the primary regression guard for the
// behavior documented in docs/issues/aws-nlb-plugin-cases.md §1 (Scenario 1):
// "scale=0 then delete gss" leaks NLB port cache under Fixed=true because the
// early-return for live GSS combined with the absence of an OnGameServerSetDeleted
// hook leaves no opportunity to release. The cases below pin down the current
// branch behavior so any future change has to consciously update them.
//
// Coverage matrix (matches docs §1.12 semantic table):
//
//	isFixed | GSS state                     | want podAllocate cleared?
//	------- | ----------------------------- | -----------------------------
//	false   | (irrelevant)                  | yes — only this pod
//	true    | exists, deletionTimestamp=nil | NO  (fixed-IP keeps slot)
//	true    | exists, being deleted         | yes — all pods of GSS
//	true    | NotFound                      | yes — all pods of GSS
func TestOnPodDeleted(t *testing.T) {
	const (
		ns      = "default"
		gssName = "gss-A"
		nlbARN  = "arn:aws:elasticloadbalancing:us-east-1:1:loadbalancer/net/aaa/1"
	)

	// mkPod builds a pod that carries the OKG NLB network-conf annotation so
	// OnPodDeleted's parseLbConfig sees the right Fixed value, and the
	// owner-gss label so GetGameServerSetOfPod resolves the right GSS name.
	mkPod := func(name, owner string, fixed bool) *corev1.Pod {
		conf := fmt.Sprintf(
			`[{"name":"NlbARNs","value":"%s"},`+
				`{"name":"PortProtocols","value":"8601/TCP"},`+
				`{"name":"NlbVPCId","value":"vpc-1"},`+
				`{"name":"Fixed","value":"%t"}]`,
			nlbARN, fixed)
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: ns,
				Labels:    map[string]string{gamekruiseiov1alpha1.GameServerOwnerGssKey: owner},
				Annotations: map[string]string{
					gamekruiseiov1alpha1.GameServerNetworkType: NlbNetwork,
					gamekruiseiov1alpha1.GameServerNetworkConf: conf,
				},
			},
		}
	}

	// mkPlugin pre-populates 3 pods of gss-A (ports 9001/9002/9003) plus 1
	// pod of an unrelated gss-B (port 9004), all on the same NLB. This lets
	// the assertions check both "released the right pods" and "did not touch
	// pods of other GSS".
	mkPlugin := func() *NlbPlugin {
		n := &NlbPlugin{
			minPort:     9001,
			maxPort:     9050,
			cache:       map[string]portAllocated{nlbARN: make(portAllocated)},
			podAllocate: map[string]*nlbPorts{},
			mutex:       sync.RWMutex{},
		}
		for i, podName := range []string{"gss-A-0", "gss-A-1", "gss-A-2"} {
			port := int32(9001 + i)
			n.podAllocate[ns+"/"+podName] = &nlbPorts{arn: nlbARN, ports: []int32{port}}
			n.cache[nlbARN][port] = true
		}
		n.podAllocate[ns+"/gss-B-0"] = &nlbPorts{arn: nlbARN, ports: []int32{9004}}
		n.cache[nlbARN][9004] = true
		return n
	}

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = gamekruiseiov1alpha1.AddToScheme(scheme)

	// Live GSS — no deletionTimestamp.
	liveGSS := &gamekruiseiov1alpha1.GameServerSet{
		ObjectMeta: metav1.ObjectMeta{Name: gssName, Namespace: ns},
	}
	// Deleting GSS — set deletionTimestamp + a finalizer so the fake client
	// retains the object instead of immediately removing it.
	now := metav1.Now()
	deletingGSS := &gamekruiseiov1alpha1.GameServerSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:              gssName,
			Namespace:         ns,
			DeletionTimestamp: &now,
			Finalizers:        []string{"keep-for-test"},
		},
	}

	tests := []struct {
		name            string
		fixed           bool
		gssInCluster    *gamekruiseiov1alpha1.GameServerSet // nil = NotFound
		deletedPodName  string
		wantPresent     map[string]bool // podAllocate state expected after
		wantPortFreed   []int32         // cache slots that should be false
		wantPortRetain  []int32         // cache slots that should remain true
	}{
		{
			name:           "Fixed=false releases only this pod",
			fixed:          false,
			gssInCluster:   liveGSS, // present but irrelevant: code does not check GSS for !isFixed
			deletedPodName: "gss-A-1",
			wantPresent: map[string]bool{
				ns + "/gss-A-0": true,
				ns + "/gss-A-1": false,
				ns + "/gss-A-2": true,
				ns + "/gss-B-0": true,
			},
			wantPortFreed:  []int32{9002},
			wantPortRetain: []int32{9001, 9003, 9004},
		},
		{
			name:           "Fixed=true with live GSS keeps allocation (fixed-IP semantics)",
			fixed:          true,
			gssInCluster:   liveGSS,
			deletedPodName: "gss-A-1",
			wantPresent: map[string]bool{
				ns + "/gss-A-0": true,
				ns + "/gss-A-1": true, // NOT released — this is the early-return branch
				ns + "/gss-A-2": true,
				ns + "/gss-B-0": true,
			},
			wantPortFreed:  nil,
			wantPortRetain: []int32{9001, 9002, 9003, 9004},
		},
		{
			name:           "Fixed=true with deleting GSS releases all pods of that GSS",
			fixed:          true,
			gssInCluster:   deletingGSS,
			deletedPodName: "gss-A-1",
			wantPresent: map[string]bool{
				ns + "/gss-A-0": false,
				ns + "/gss-A-1": false,
				ns + "/gss-A-2": false,
				ns + "/gss-B-0": true, // sibling GSS unaffected
			},
			wantPortFreed:  []int32{9001, 9002, 9003},
			wantPortRetain: []int32{9004},
		},
		{
			name:           "Fixed=true with GSS NotFound releases all pods of that GSS",
			fixed:          true,
			gssInCluster:   nil, // empty cluster
			deletedPodName: "gss-A-1",
			wantPresent: map[string]bool{
				ns + "/gss-A-0": false,
				ns + "/gss-A-1": false,
				ns + "/gss-A-2": false,
				ns + "/gss-B-0": true,
			},
			wantPortFreed:  []int32{9001, 9002, 9003},
			wantPortRetain: []int32{9004},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := mkPlugin()
			pod := mkPod(tt.deletedPodName, gssName, tt.fixed)
			cb := fake.NewClientBuilder().WithScheme(scheme)
			if tt.gssInCluster != nil {
				cb = cb.WithObjects(tt.gssInCluster)
			}
			c := cb.Build()

			if perr := n.OnPodDeleted(c, pod, context.Background()); perr != nil {
				t.Fatalf("OnPodDeleted error: %v", perr)
			}

			for key, want := range tt.wantPresent {
				_, got := n.podAllocate[key]
				if got != want {
					t.Errorf("podAllocate[%q]: present=%v, want=%v", key, got, want)
				}
			}
			for _, port := range tt.wantPortFreed {
				if n.cache[nlbARN][port] {
					t.Errorf("cache[%d]: still occupied, want freed", port)
				}
			}
			for _, port := range tt.wantPortRetain {
				if !n.cache[nlbARN][port] {
					t.Errorf("cache[%d]: freed, want still occupied", port)
				}
			}
		})
	}
}

// TestGetOwnerReference covers the isFixed branch in getOwnerReference
// (nlb.go: getOwnerReference). The two non-error branches are also docs §1.12's
// pivot: Fixed=true makes Service/TG owner = GSS so they survive Pod deletion
// (TC-02 retention behavior), Fixed=false makes them owner = Pod so K8s GC
// cascades them away on Pod deletion (TC-04 release behavior).
func TestGetOwnerReference(t *testing.T) {
	const (
		ns      = "default"
		gssName = "gss-A"
		podName = "gss-A-0"
		gssUID  = "gss-uid-1234"
		podUID  = "pod-uid-5678"
	)

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = gamekruiseiov1alpha1.AddToScheme(scheme)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: ns,
			UID:       podUID,
			Labels:    map[string]string{gamekruiseiov1alpha1.GameServerOwnerGssKey: gssName},
		},
	}
	gss := &gamekruiseiov1alpha1.GameServerSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gssName,
			Namespace: ns,
			UID:       gssUID,
		},
	}

	tests := []struct {
		name           string
		isFixed        bool
		gssInCluster   *gamekruiseiov1alpha1.GameServerSet
		wantOwnerName  string
		wantOwnerUID   types.UID
	}{
		{
			name:          "Fixed=false uses Pod as owner",
			isFixed:       false,
			gssInCluster:  gss, // present but ignored — Fixed=false skips GSS lookup
			wantOwnerName: podName,
			wantOwnerUID:  podUID,
		},
		{
			name:          "Fixed=true with GSS present uses GameServerSet as owner",
			isFixed:       true,
			gssInCluster:  gss,
			wantOwnerName: gssName,
			wantOwnerUID:  gssUID,
		},
		{
			name:          "Fixed=true with GSS NotFound falls back to Pod owner",
			isFixed:       true,
			gssInCluster:  nil,
			wantOwnerName: podName,
			wantOwnerUID:  podUID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cb := fake.NewClientBuilder().WithScheme(scheme)
			if tt.gssInCluster != nil {
				cb = cb.WithObjects(tt.gssInCluster)
			}
			c := cb.Build()

			refs := getOwnerReference(c, context.Background(), pod, tt.isFixed)
			if len(refs) != 1 {
				t.Fatalf("expected exactly 1 OwnerReference, got %d: %#v", len(refs), refs)
			}
			ref := refs[0]
			if ref.Name != tt.wantOwnerName {
				t.Errorf("Name = %q, want %q", ref.Name, tt.wantOwnerName)
			}
			if ref.UID != tt.wantOwnerUID {
				t.Errorf("UID = %q, want %q", ref.UID, tt.wantOwnerUID)
			}
			if ref.Controller == nil || !*ref.Controller {
				t.Errorf("Controller = %v, want true", ref.Controller)
			}
			if ref.BlockOwnerDeletion == nil || !*ref.BlockOwnerDeletion {
				t.Errorf("BlockOwnerDeletion = %v, want true", ref.BlockOwnerDeletion)
			}
		})
	}
}


// ---------------------------------------------------------------------------
// Helpers used by the OnPodUpdated / sync* unit tests below.
// ---------------------------------------------------------------------------

// nlbTestScheme returns a runtime.Scheme registered with every API type
// touched by OnPodUpdated and the sync* helpers (corev1 + elbv2 TGB +
// ACK TargetGroup/Listener + OKG game.kruise.io v1alpha1).
func nlbTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatalf("add corev1: %v", err)
	}
	if err := elbv2api.AddToScheme(s); err != nil {
		t.Fatalf("add elbv2api: %v", err)
	}
	if err := ackv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("add ackv1alpha1: %v", err)
	}
	if err := gamekruiseiov1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("add gamekruiseiov1alpha1: %v", err)
	}
	return s
}

// validNlbConf returns a minimal but valid NlbARNs+PortProtocols JSON network
// conf string for use as a Pod annotation. validateLbConfig accepts it.
func validNlbConf(arn string, ports string) string {
	return fmt.Sprintf(
		`[{"name":"NlbARNs","value":"%s"},`+
			`{"name":"PortProtocols","value":"%s"},`+
			`{"name":"NlbVPCId","value":"vpc-1"}]`,
		arn, ports)
}

// mkNlbPod builds a Pod with OKG NLB annotations so utils.NewNetworkManager
// returns a non-nil manager and parseLbConfig parses the conf.
func mkNlbPod(name, ns, conf, status string) *corev1.Pod {
	ann := map[string]string{
		gamekruiseiov1alpha1.GameServerNetworkType: NlbNetwork,
		gamekruiseiov1alpha1.GameServerNetworkConf: conf,
	}
	if status != "" {
		ann[gamekruiseiov1alpha1.GameServerNetworkStatus] = status
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   ns,
			Annotations: ann,
		},
		Status: corev1.PodStatus{PodIP: "10.0.0.1"},
	}
}

// ---------------------------------------------------------------------------
// generateNlbEndpoint: pure ARN -> hostname formatter. We test the happy path
// (canonical ARN) and the malformed-ARN guard which returns "" instead of
// panicking.
// ---------------------------------------------------------------------------

func TestGenerateNlbEndpoint(t *testing.T) {
	tests := []struct {
		name string
		arn  string
		want string
	}{
		{
			name: "valid canonical ARN -> <name>-<id>.elb.<region>.amazonaws.com",
			arn:  "arn:aws:elasticloadbalancing:us-east-1:888888888888:loadbalancer/net/aaa/3b332e6841f23870",
			want: "aaa-3b332e6841f23870.elb.us-east-1.amazonaws.com",
		},
		{
			name: "valid ARN in different region",
			arn:  "arn:aws:elasticloadbalancing:eu-west-1:1:loadbalancer/net/my-nlb/abc",
			want: "my-nlb-abc.elb.eu-west-1.amazonaws.com",
		},
		{
			// Malformed ARN: not 6 colon-separated parts. Must return "" rather
			// than panic, so callers can degrade gracefully.
			name: "malformed ARN -> empty",
			arn:  "garbage",
			want: "",
		},
		{
			name: "empty ARN -> empty",
			arn:  "",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := generateNlbEndpoint(tt.arn); got != tt.want {
				t.Errorf("generateNlbEndpoint(%q) = %q, want %q", tt.arn, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// getACKTargetGroupARN: error+success branches. Used by the watch handler to
// surface the ARN AWS assigned to the ACK TargetGroup, so the listener and
// TargetGroupBinding can reference it.
// ---------------------------------------------------------------------------

func TestGetACKTargetGroupARN(t *testing.T) {
	arn := ackv1alpha1core.AWSResourceName("arn:aws:elasticloadbalancing:us-east-1:1:targetgroup/tg/abc")

	t.Run("no conditions -> error", func(t *testing.T) {
		_, err := getACKTargetGroupARN(&ackv1alpha1.TargetGroup{})
		if err == nil {
			t.Fatal("expected error when status has no conditions")
		}
	})

	t.Run("first condition not True -> error", func(t *testing.T) {
		tg := &ackv1alpha1.TargetGroup{
			Status: ackv1alpha1.TargetGroupStatus{
				Conditions: []*ackv1alpha1core.Condition{{
					Status:  "False",
					Message: ptr.To[string]("nope"),
					Reason:  ptr.To[string]("rejected"),
				}},
			},
		}
		_, err := getACKTargetGroupARN(tg)
		if err == nil {
			t.Fatal("expected error when first condition is not True")
		}
	})

	t.Run("True but ACKResourceMetadata nil -> error (status not ready)", func(t *testing.T) {
		tg := &ackv1alpha1.TargetGroup{
			Status: ackv1alpha1.TargetGroupStatus{
				Conditions: []*ackv1alpha1core.Condition{{Status: "True"}},
				// ACKResourceMetadata intentionally nil
			},
		}
		_, err := getACKTargetGroupARN(tg)
		if err == nil {
			t.Fatal("expected error when ACKResourceMetadata is nil")
		}
	})

	t.Run("True with ARN -> returns ARN", func(t *testing.T) {
		tg := &ackv1alpha1.TargetGroup{
			Status: ackv1alpha1.TargetGroupStatus{
				Conditions: []*ackv1alpha1core.Condition{{Status: "True"}},
				ACKResourceMetadata: &ackv1alpha1core.ResourceMetadata{
					ARN: &arn,
				},
			},
		}
		got, err := getACKTargetGroupARN(tg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != string(arn) {
			t.Errorf("ARN = %q, want %q", got, arn)
		}
	})
}

// ---------------------------------------------------------------------------
// OnPodUpdated covers the lion's share of the missing patch coverage. We
// exercise each independent branch in isolation.
// ---------------------------------------------------------------------------

const testNlbARN = "arn:aws:elasticloadbalancing:us-east-1:1:loadbalancer/net/aaa/1"

// Branch: validateLbConfig returns error -> OnPodUpdated must surface a
// ParameterError without touching any cluster state.
func TestOnPodUpdated_InvalidConfig_ParameterError(t *testing.T) {
	scheme := nlbTestScheme(t)
	// Missing NlbARNs -> validateLbConfig fails.
	conf := `[{"name":"PortProtocols","value":"8080/TCP"}]`
	pod := mkNlbPod("p0", "default", conf, `{"currentNetworkState":"NotReady"}`)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()

	n := newNlbForPolicy()
	_, perr := n.OnPodUpdated(c, pod, context.Background())
	if perr == nil {
		t.Fatalf("expected error for invalid config, got nil")
	}
	if perr.Type() != "parameterError" {
		t.Errorf("error type = %q, want parameterError", perr.Type())
	}
}

// Branch: networkStatus annotation absent -> OnPodUpdated writes
// CurrentNetworkState=NotReady and returns. The pod's annotation should be
// set so subsequent webhook calls have a starting point.
func TestOnPodUpdated_NoNetworkStatusAnnotation_SetsNotReady(t *testing.T) {
	scheme := nlbTestScheme(t)
	conf := validNlbConf(testNlbARN, "8080/TCP")
	pod := mkNlbPod("p0", "default", conf, "") // no NetworkStatus annotation
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()

	n := newNlbForPolicy()
	got, perr := n.OnPodUpdated(c, pod, context.Background())
	if perr != nil {
		t.Fatalf("OnPodUpdated error: %v", perr)
	}
	annStatus := got.Annotations[gamekruiseiov1alpha1.GameServerNetworkStatus]
	if annStatus == "" {
		t.Fatal("expected NetworkStatus annotation to be set")
	}
	if !contains(annStatus, "NotReady") {
		t.Errorf("expected NotReady in annotation, got %q", annStatus)
	}
}

// Branch: Service does not exist for the pod -> OnPodUpdated calls
// syncTargetGroupAndService, which must:
//   - allocate a port (or reuse podAllocate);
//   - create the Service (ClusterIP) with the right annotations/labels;
//   - create one TargetGroup per backend port.
//
// This single test exercises both OnPodUpdated's "svc not found" branch and
// the entire syncTargetGroupAndService function (TG + Service creation).
func TestOnPodUpdated_SvcNotFound_CreatesTargetGroupAndService(t *testing.T) {
	scheme := nlbTestScheme(t)
	conf := validNlbConf(testNlbARN, "8080/TCP,8081/UDP")
	pod := mkNlbPod("gd-0", "default", conf, `{"currentNetworkState":"NotReady"}`)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()

	n := newNlbForPolicy()
	if _, perr := n.OnPodUpdated(c, pod, context.Background()); perr != nil {
		t.Fatalf("OnPodUpdated error: %v", perr)
	}

	// Service must be created with NlbARN + ConfigHash annotations.
	svc := &corev1.Service{}
	if err := c.Get(context.Background(), types.NamespacedName{Name: "gd-0", Namespace: "default"}, svc); err != nil {
		t.Fatalf("Service not created: %v", err)
	}
	if svc.Annotations[NlbARNAnnoKey] != testNlbARN {
		t.Errorf("svc.Annotations[%s] = %q, want %q", NlbARNAnnoKey, svc.Annotations[NlbARNAnnoKey], testNlbARN)
	}
	if svc.Annotations[NlbConfigHashKey] == "" {
		t.Errorf("svc.Annotations[%s] should not be empty", NlbConfigHashKey)
	}
	if len(svc.Spec.Ports) != 2 {
		t.Errorf("svc.Spec.Ports = %d, want 2", len(svc.Spec.Ports))
	}
	if svc.Labels[ResourceTagKey] != ResourceTagValue {
		t.Errorf("svc.Labels[%s] = %q, want %q", ResourceTagKey, svc.Labels[ResourceTagKey], ResourceTagValue)
	}

	// One TargetGroup per backend port. Names are <pod>-<port>.
	tgList := &ackv1alpha1.TargetGroupList{}
	if err := c.List(context.Background(), tgList); err != nil {
		t.Fatalf("list TargetGroups: %v", err)
	}
	if len(tgList.Items) != 2 {
		t.Errorf("TargetGroup count = %d, want 2", len(tgList.Items))
	}
	for _, tg := range tgList.Items {
		if tg.Annotations[NlbARNAnnoKey] != testNlbARN {
			t.Errorf("TG %s missing NlbARN annotation", tg.GetName())
		}
		if tg.Labels[AWSTargetGroupSyncStatus] != "false" {
			t.Errorf("TG %s sync label = %q, want false", tg.GetName(), tg.Labels[AWSTargetGroupSyncStatus])
		}
	}

	// allocate must have pinned the port set in podAllocate so subsequent calls reuse it.
	if alloc, ok := n.podAllocate["default/gd-0"]; !ok || len(alloc.ports) != 2 {
		t.Errorf("podAllocate not pinned correctly: %#v", alloc)
	}
}

// Branch: Service exists, networkStatus already Ready, hash matches ->
// OnPodUpdated must short-circuit (no readiness-gate re-check, no resource
// updates). This pins the S4/InPlace pass-through documented in nlb.go.
func TestOnPodUpdated_PassThroughReadyAndHashMatch(t *testing.T) {
	scheme := nlbTestScheme(t)
	conf := validNlbConf(testNlbARN, "8080/TCP")
	pod := mkNlbPod("gd-0", "default", conf,
		`{"currentNetworkState":"Ready","externalAddresses":[{"endPoint":"x.elb"}]}`)

	// Build the matching Service whose hash equals what configHash() will
	// compute for the parsed conf, so OnPodUpdated takes the pass-through path.
	parsed := parseLbConfig(parseConf(t, conf))
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gd-0",
			Namespace: "default",
			Annotations: map[string]string{
				NlbARNAnnoKey:    testNlbARN,
				NlbConfigHashKey: parsed.configHash(),
			},
			Labels: map[string]string{
				ResourceTagKey: ResourceTagValue,
				SvcSelectorKey: "gd-0",
			},
		},
		Spec: corev1.ServiceSpec{
			Type:  corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{{Port: 951, TargetPort: intstr.FromInt(8080), Protocol: corev1.ProtocolTCP}},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod, svc).Build()

	n := newNlbForPolicy()
	got, perr := n.OnPodUpdated(c, pod, context.Background())
	if perr != nil {
		t.Fatalf("OnPodUpdated error: %v", perr)
	}
	// Pass-through: pod returned unchanged, no annotation rewrites.
	annStatus := got.Annotations[gamekruiseiov1alpha1.GameServerNetworkStatus]
	if annStatus != `{"currentNetworkState":"Ready","externalAddresses":[{"endPoint":"x.elb"}]}` {
		t.Errorf("annotation should be unchanged in pass-through, got %q", annStatus)
	}
}

// Branch: Service exists, hash matches, pod IS PodReady -> OnPodUpdated
// constructs the internal/external address lists from svc.Spec.Ports and
// flips CurrentNetworkState to NetworkReady.
func TestOnPodUpdated_PodReady_FillsExternalAddresses(t *testing.T) {
	scheme := nlbTestScheme(t)
	conf := validNlbConf(testNlbARN, "8080/TCP")
	// networkStatus exists but is still NotReady (e.g. initial reconciliation).
	pod := mkNlbPod("gd-0", "default", conf, `{"currentNetworkState":"NotReady"}`)
	pod.Status = corev1.PodStatus{
		PodIP:      "10.0.0.7",
		Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
	}

	parsed := parseLbConfig(parseConf(t, conf))
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gd-0",
			Namespace: "default",
			Annotations: map[string]string{
				NlbARNAnnoKey:    testNlbARN,
				NlbConfigHashKey: parsed.configHash(),
			},
			Labels: map[string]string{
				ResourceTagKey: ResourceTagValue,
				SvcSelectorKey: "gd-0",
			},
		},
		Spec: corev1.ServiceSpec{
			Type:  corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{{Port: 951, TargetPort: intstr.FromInt(8080), Protocol: corev1.ProtocolTCP}},
		},
	}
	tgb := &elbv2api.TargetGroupBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gd-0-951",
			Namespace: "default",
			Labels: map[string]string{
				ResourceTagKey: ResourceTagValue,
				SvcSelectorKey: "gd-0",
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod, svc, tgb).Build()

	n := newNlbForPolicy()
	got, perr := n.OnPodUpdated(c, pod, context.Background())
	if perr != nil {
		t.Fatalf("OnPodUpdated error: %v", perr)
	}
	annStatus := got.Annotations[gamekruiseiov1alpha1.GameServerNetworkStatus]
	// Must transition to Ready and populate external endpoint derived from ARN.
	if !contains(annStatus, `"currentNetworkState":"Ready"`) {
		t.Errorf("expected currentNetworkState Ready, got %q", annStatus)
	}
	wantEndpoint := "aaa-1.elb.us-east-1.amazonaws.com" // generateNlbEndpoint(testNlbARN)
	if !contains(annStatus, wantEndpoint) {
		t.Errorf("expected external endpoint %q in status, got %q", wantEndpoint, annStatus)
	}
	if !contains(annStatus, `"ip":"10.0.0.7"`) {
		t.Errorf("expected internal IP 10.0.0.7 in status, got %q", annStatus)
	}
}

// Branch: networkDisabled label set -> OnPodUpdated must DeleteAllOf
// the pod's TargetGroupBindings (network shut off) and return without
// reaching the readiness-gate code.
func TestOnPodUpdated_NetworkDisabled_DeletesTGBs(t *testing.T) {
	scheme := nlbTestScheme(t)
	conf := validNlbConf(testNlbARN, "8080/TCP")
	pod := mkNlbPod("gd-0", "default", conf, `{"currentNetworkState":"Ready"}`)
	if pod.Labels == nil {
		pod.Labels = map[string]string{}
	}
	pod.Labels[gamekruiseiov1alpha1.GameServerNetworkDisabled] = "true"

	parsed := parseLbConfig(parseConf(t, conf))
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gd-0",
			Namespace: "default",
			Annotations: map[string]string{
				NlbARNAnnoKey:    testNlbARN,
				NlbConfigHashKey: parsed.configHash(),
			},
			Labels: map[string]string{
				ResourceTagKey: ResourceTagValue,
				SvcSelectorKey: "gd-0",
			},
		},
		Spec: corev1.ServiceSpec{
			Type:  corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{{Port: 951, TargetPort: intstr.FromInt(8080), Protocol: corev1.ProtocolTCP}},
		},
	}
	tgb := &elbv2api.TargetGroupBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gd-0-951",
			Namespace: "default",
			Labels: map[string]string{
				ResourceTagKey: ResourceTagValue,
				SvcSelectorKey: "gd-0",
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod, svc, tgb).Build()

	n := newNlbForPolicy()
	if _, perr := n.OnPodUpdated(c, pod, context.Background()); perr != nil {
		t.Fatalf("OnPodUpdated error: %v", perr)
	}
	// The TGB should be gone after disable.
	got := &elbv2api.TargetGroupBinding{}
	err := c.Get(context.Background(), types.NamespacedName{Name: "gd-0-951", Namespace: "default"}, got)
	if !errors.IsNotFound(err) {
		t.Errorf("expected TGB to be deleted, err=%v", err)
	}
}

// Branch: SVC exists but its hash differs from the parsed conf -> OnPodUpdated
// resets the network state to NotReady and re-runs syncTargetGroupAndService.
// We assert the Service annotation is updated to the new hash.
func TestOnPodUpdated_HashMismatch_ResyncsService(t *testing.T) {
	scheme := nlbTestScheme(t)
	conf := validNlbConf(testNlbARN, "8080/TCP")
	pod := mkNlbPod("gd-0", "default", conf, `{"currentNetworkState":"Ready"}`)

	// Pre-existing svc with a stale hash.
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gd-0",
			Namespace: "default",
			Annotations: map[string]string{
				NlbARNAnnoKey:    testNlbARN,
				NlbConfigHashKey: "stale-hash-doesnt-match",
			},
			Labels: map[string]string{
				ResourceTagKey: ResourceTagValue,
				SvcSelectorKey: "gd-0",
			},
		},
		Spec: corev1.ServiceSpec{
			Type:  corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{{Port: 951, TargetPort: intstr.FromInt(8080), Protocol: corev1.ProtocolTCP}},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod, svc).Build()

	n := newNlbForPolicy()
	got, perr := n.OnPodUpdated(c, pod, context.Background())
	if perr != nil {
		t.Fatalf("OnPodUpdated error: %v", perr)
	}
	// Status moved to NotReady (we entered the resync branch).
	if !contains(got.Annotations[gamekruiseiov1alpha1.GameServerNetworkStatus], "NotReady") {
		t.Errorf("expected NotReady annotation after hash mismatch, got %q",
			got.Annotations[gamekruiseiov1alpha1.GameServerNetworkStatus])
	}
	// Service annotation hash should now match the parsed conf hash.
	parsed := parseLbConfig(parseConf(t, conf))
	updatedSvc := &corev1.Service{}
	if err := c.Get(context.Background(), types.NamespacedName{Name: "gd-0", Namespace: "default"}, updatedSvc); err != nil {
		t.Fatalf("get updated svc: %v", err)
	}
	if updatedSvc.Annotations[NlbConfigHashKey] != parsed.configHash() {
		t.Errorf("svc hash = %q, want %q", updatedSvc.Annotations[NlbConfigHashKey], parsed.configHash())
	}
}

// Branch: TGB inventory smaller than svc.Spec.Ports -> OnPodUpdated patches
// each TG's sync label to "false" so the watch reconciler will re-create the
// missing TGBs. This pins the heal-on-mismatch path inside OnPodUpdated.
func TestOnPodUpdated_TgbCountMismatch_PatchesTGSyncLabel(t *testing.T) {
	scheme := nlbTestScheme(t)
	conf := validNlbConf(testNlbARN, "8080/TCP,8081/UDP")
	pod := mkNlbPod("gd-0", "default", conf, `{"currentNetworkState":"NotReady"}`)
	pod.Status.Conditions = []corev1.PodCondition{
		{Type: corev1.PodReady, Status: corev1.ConditionFalse},
	}

	parsed := parseLbConfig(parseConf(t, conf))
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gd-0",
			Namespace: "default",
			Annotations: map[string]string{
				NlbARNAnnoKey:    testNlbARN,
				NlbConfigHashKey: parsed.configHash(),
			},
			Labels: map[string]string{
				ResourceTagKey: ResourceTagValue,
				SvcSelectorKey: "gd-0",
			},
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{
				{Port: 951, TargetPort: intstr.FromInt(8080), Protocol: corev1.ProtocolTCP},
				{Port: 952, TargetPort: intstr.FromInt(8081), Protocol: corev1.ProtocolUDP},
			},
		},
	}
	// Only ONE TGB exists for two svc ports -> mismatch path triggers.
	tgb := &elbv2api.TargetGroupBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gd-0-951",
			Namespace: "default",
			Labels: map[string]string{
				ResourceTagKey: ResourceTagValue,
				SvcSelectorKey: "gd-0",
			},
		},
	}
	tg1 := &ackv1alpha1.TargetGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gd-0-951",
			Namespace: "default",
			Labels: map[string]string{
				ResourceTagKey:           ResourceTagValue,
				SvcSelectorKey:           "gd-0",
				AWSTargetGroupSyncStatus: "true",
			},
		},
	}
	tg2 := &ackv1alpha1.TargetGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gd-0-952",
			Namespace: "default",
			Labels: map[string]string{
				ResourceTagKey:           ResourceTagValue,
				SvcSelectorKey:           "gd-0",
				AWSTargetGroupSyncStatus: "true",
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod, svc, tgb, tg1, tg2).Build()

	n := newNlbForPolicy()
	if _, perr := n.OnPodUpdated(c, pod, context.Background()); perr != nil {
		t.Fatalf("OnPodUpdated error: %v", perr)
	}
	// Both TGs should now have sync=false so the watch handler re-creates the TGB.
	for _, name := range []string{"gd-0-951", "gd-0-952"} {
		got := &ackv1alpha1.TargetGroup{}
		if err := c.Get(context.Background(), types.NamespacedName{Name: name, Namespace: "default"}, got); err != nil {
			t.Fatalf("get TG %s: %v", name, err)
		}
		if got.Labels[AWSTargetGroupSyncStatus] != "false" {
			t.Errorf("TG %s sync label = %q, want false", name, got.Labels[AWSTargetGroupSyncStatus])
		}
	}
}

// ---------------------------------------------------------------------------
// syncListenerAndTargetGroupBinding: directly exercise the watch-driven
// helper that creates/updates the Listener and TargetGroupBinding from a
// ready TargetGroup. This drove most of the missing patch coverage outside
// OnPodUpdated.
// ---------------------------------------------------------------------------

func TestSyncListenerAndTargetGroupBinding(t *testing.T) {
	scheme := nlbTestScheme(t)
	tg := &ackv1alpha1.TargetGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gd-0-951",
			Namespace: "default",
			Labels: map[string]string{
				ResourceTagKey: ResourceTagValue,
				SvcSelectorKey: "gd-0",
			},
			Annotations: map[string]string{
				NlbARNAnnoKey:  testNlbARN,
				NlbPortAnnoKey: "951",
			},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "v1", Kind: "Pod", Name: "gd-0", UID: "abc",
				Controller: ptr.To[bool](true), BlockOwnerDeletion: ptr.To[bool](true),
			}},
		},
		Spec: ackv1alpha1.TargetGroupSpec{
			Protocol: ptr.To[string]("TCP"),
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tg).Build()
	tgARN := "arn:aws:elasticloadbalancing:us-east-1:1:targetgroup/tg/x"

	if err := syncListenerAndTargetGroupBinding(context.Background(), c, tg, &tgARN); err != nil {
		t.Fatalf("syncListenerAndTargetGroupBinding: %v", err)
	}

	// Listener should now exist with the expected port and load balancer ARN.
	l := &ackv1alpha1.Listener{}
	if err := c.Get(context.Background(), types.NamespacedName{Name: "gd-0-951", Namespace: "default"}, l); err != nil {
		t.Fatalf("Listener not created: %v", err)
	}
	if l.Spec.Port == nil || *l.Spec.Port != 951 {
		t.Errorf("Listener port = %v, want 951", l.Spec.Port)
	}
	if l.Spec.LoadBalancerARN == nil || *l.Spec.LoadBalancerARN != testNlbARN {
		t.Errorf("Listener LB ARN = %v, want %q", l.Spec.LoadBalancerARN, testNlbARN)
	}
	if l.Labels[SvcSelectorKey] != "gd-0" {
		t.Errorf("Listener svc-selector label = %q, want gd-0", l.Labels[SvcSelectorKey])
	}

	// TargetGroupBinding should reference the pod's Service by name + the listener port.
	tgb := &elbv2api.TargetGroupBinding{}
	if err := c.Get(context.Background(), types.NamespacedName{Name: "gd-0-951", Namespace: "default"}, tgb); err != nil {
		t.Fatalf("TGB not created: %v", err)
	}
	if tgb.Spec.TargetGroupARN != tgARN {
		t.Errorf("TGB targetGroupARN = %q, want %q", tgb.Spec.TargetGroupARN, tgARN)
	}
	if tgb.Spec.ServiceRef.Name != "gd-0" {
		t.Errorf("TGB service name = %q, want gd-0", tgb.Spec.ServiceRef.Name)
	}
	if tgb.Spec.ServiceRef.Port.IntValue() != 951 {
		t.Errorf("TGB service port = %v, want 951", tgb.Spec.ServiceRef.Port)
	}
}

// Malformed annotations -> the helper should surface the strconv error rather
// than silently creating broken resources.
func TestSyncListenerAndTargetGroupBinding_BadPortAnnotation(t *testing.T) {
	scheme := nlbTestScheme(t)
	tg := &ackv1alpha1.TargetGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gd-0-bad",
			Namespace: "default",
			Annotations: map[string]string{
				NlbARNAnnoKey:  testNlbARN,
				NlbPortAnnoKey: "not-a-number",
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tg).Build()
	tgARN := "arn:aws:elasticloadbalancing:us-east-1:1:targetgroup/tg/x"
	if err := syncListenerAndTargetGroupBinding(context.Background(), c, tg, &tgARN); err == nil {
		t.Fatal("expected error for bad NlbPort annotation, got nil")
	}
}

// ---------------------------------------------------------------------------
// Tiny utilities used above. We don't pull in strings.Contains via the
// already-imported strings package because nlb_test.go does not import it; a
// local helper keeps the import set minimal.
// ---------------------------------------------------------------------------

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	if len(needle) == 0 {
		return 0
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

// parseConf unmarshals the JSON network-conf string into a
// []NetworkConfParams the same way utils.NewNetworkManager does, without
// pulling that whole scaffolding into the test path. The conf JSON shape is
// pinned by GameServerNetworkConf consumers, so a direct json.Unmarshal is
// sufficient and avoids a circular helper.
func parseConf(t *testing.T, jsonStr string) []gamekruiseiov1alpha1.NetworkConfParams {
	t.Helper()
	var conf []gamekruiseiov1alpha1.NetworkConfParams
	if err := json.Unmarshal([]byte(jsonStr), &conf); err != nil {
		t.Fatalf("unmarshal conf %q: %v", jsonStr, err)
	}
	return conf
}
