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
	"reflect"
	"sync"
	"testing"

	"github.com/kr/pretty"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

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
		allocatedPorts := test.nlb.allocate(test.loadBalancerARNs, test.num, test.podKey)
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
