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

package alibabacloud

import (
	"context"
	"reflect"
	"sync"
	"testing"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestAllocateDeAllocate(t *testing.T) {
	test := struct {
		lbIds  []string
		slb    *SlbPlugin
		num    int
		podKey string
	}{
		lbIds: []string{"xxx-A"},
		slb: &SlbPlugin{
			maxPort:     int32(712),
			minPort:     int32(512),
			cache:       make(map[string]portAllocated),
			podAllocate: make(map[string]string),
			mutex:       sync.RWMutex{},
		},
		podKey: "xxx/xxx",
		num:    3,
	}

	lbId, ports := test.slb.allocate(test.lbIds, test.num, test.podKey)
	if _, exist := test.slb.podAllocate[test.podKey]; !exist {
		t.Errorf("podAllocate[%s] is empty after allocated", test.podKey)
	}
	for _, port := range ports {
		if port > test.slb.maxPort || port < test.slb.minPort {
			t.Errorf("allocate port %d, unexpected", port)
		}
		if test.slb.cache[lbId][port] == false {
			t.Errorf("Allocate port %d failed", port)
		}
	}
	test.slb.deAllocate(test.podKey)
	for _, port := range ports {
		if test.slb.cache[lbId][port] == true {
			t.Errorf("deAllocate port %d failed", port)
		}
	}
	if _, exist := test.slb.podAllocate[test.podKey]; exist {
		t.Errorf("podAllocate[%s] is not empty after deallocated", test.podKey)
	}
}

func TestParseLbConfig(t *testing.T) {
	tests := []struct {
		conf      []gamekruiseiov1alpha1.NetworkConfParams
		slbConfig *slbConfig
	}{
		{
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{
					Name:  SlbIdsConfigName,
					Value: "xxx-A",
				},
				{
					Name:  PortProtocolsConfigName,
					Value: "80",
				},
				{
					Name:  LBHealthCheckSwitchConfigName,
					Value: "off",
				},
				{
					Name:  LBHealthCheckFlagConfigName,
					Value: "off",
				},
				{
					Name:  LBHealthCheckTypeConfigName,
					Value: "HTTP",
				},
				{
					Name:  LBHealthCheckConnectPortConfigName,
					Value: "6000",
				},
				{
					Name:  LBHealthCheckConnectTimeoutConfigName,
					Value: "100",
				},
				{
					Name:  LBHealthCheckIntervalConfigName,
					Value: "30",
				},
				{
					Name:  LBHealthCheckUriConfigName,
					Value: "/another?valid",
				},
				{
					Name:  LBHealthCheckDomainConfigName,
					Value: "www.test.com",
				},
				{
					Name:  LBHealthCheckMethodConfigName,
					Value: "HEAD",
				},
				{
					Name:  LBHealthyThresholdConfigName,
					Value: "5",
				},
				{
					Name:  LBUnhealthyThresholdConfigName,
					Value: "5",
				},
				{
					Name:  LBHealthCheckProtocolPortConfigName,
					Value: "http:80",
				},
			},
			slbConfig: &slbConfig{
				lbIds:                       []string{"xxx-A"},
				targetPorts:                 []int{80},
				protocols:                   []corev1.Protocol{corev1.ProtocolTCP},
				externalTrafficPolicyType:   corev1.ServiceExternalTrafficPolicyTypeCluster,
				isFixed:                     false,
				lBHealthCheckSwitch:         "off",
				lBHealthCheckFlag:           "off",
				lBHealthCheckType:           "http",
				lBHealthCheckConnectTimeout: "100",
				lBHealthCheckInterval:       "30",
				lBHealthCheckUri:            "/another?valid",
				lBHealthCheckDomain:         "www.test.com",
				lBHealthCheckMethod:         "head",
				lBHealthyThreshold:          "5",
				lBUnhealthyThreshold:        "5",
				lBHealthCheckProtocolPort:   "http:80",
			},
		},
		{
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{
					Name:  SlbIdsConfigName,
					Value: "xxx-A,xxx-B,",
				},
				{
					Name:  PortProtocolsConfigName,
					Value: "81/UDP,82,83/TCP",
				},
				{
					Name:  FixedConfigName,
					Value: "true",
				},
				{
					Name:  ExternalTrafficPolicyTypeConfigName,
					Value: "Local",
				},
			},
			slbConfig: &slbConfig{
				lbIds:                       []string{"xxx-A", "xxx-B"},
				targetPorts:                 []int{81, 82, 83},
				protocols:                   []corev1.Protocol{corev1.ProtocolUDP, corev1.ProtocolTCP, corev1.ProtocolTCP},
				externalTrafficPolicyType:   corev1.ServiceExternalTrafficPolicyTypeLocal,
				isFixed:                     true,
				lBHealthCheckSwitch:         "on",
				lBHealthCheckFlag:           "off",
				lBHealthCheckType:           "tcp",
				lBHealthCheckConnectTimeout: "5",
				lBHealthCheckInterval:       "10",
				lBUnhealthyThreshold:        "2",
				lBHealthyThreshold:          "2",
				lBHealthCheckUri:            "",
				lBHealthCheckDomain:         "",
				lBHealthCheckMethod:         "",
				lBHealthCheckProtocolPort:   "",
			},
		},
	}

	for i, test := range tests {
		sc, err := parseLbConfig(test.conf)
		if err != nil {
			t.Error(err)
		}
		if !reflect.DeepEqual(test.slbConfig, sc) {
			t.Errorf("case %d: lbId expect: %v, actual: %v", i, test.slbConfig, sc)
		}
	}
}

func TestInitLbCache(t *testing.T) {
	test := struct {
		svcList     []corev1.Service
		minPort     int32
		maxPort     int32
		blockPorts  []int32
		cache       map[string]portAllocated
		podAllocate map[string]string
	}{
		minPort:    512,
		maxPort:    712,
		blockPorts: []int32{593},
		cache: map[string]portAllocated{
			"xxx-A": map[int32]bool{
				666: true,
				593: true,
			},
			"xxx-B": map[int32]bool{
				555: true,
				593: true,
			},
		},
		podAllocate: map[string]string{
			"ns-0/name-0": "xxx-A:666",
			"ns-1/name-1": "xxx-B:555",
		},
		svcList: []corev1.Service{
			{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						SlbIdLabelKey: "xxx-A",
					},
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
							Port:       666,
							Protocol:   corev1.ProtocolTCP,
						},
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						SlbIdLabelKey: "xxx-B",
					},
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
							Port:       555,
							Protocol:   corev1.ProtocolTCP,
						},
					},
				},
			},
		},
	}

	actualCache, actualPodAllocate := initLbCache(test.svcList, test.minPort, test.maxPort, test.blockPorts)
	for lb, pa := range test.cache {
		for port, isAllocated := range pa {
			if actualCache[lb][port] != isAllocated {
				t.Errorf("lb %s port %d isAllocated, expect: %t, actual: %t", lb, port, isAllocated, actualCache[lb][port])
			}
		}
	}
	if !reflect.DeepEqual(actualPodAllocate, test.podAllocate) {
		t.Errorf("podAllocate expect %v, but actully got %v", test.podAllocate, actualPodAllocate)
	}
}

func TestConsSvc(t *testing.T) {
	tests := []struct {
		name        string
		sc          *slbConfig
		pod         *corev1.Pod
		setup       func(*SlbPlugin)
		expectErr   bool
		validateSvc func(*testing.T, *corev1.Service)
	}{
		{
			name: "basic service construction",
			sc: &slbConfig{
				lbIds:                     []string{"lb-123"},
				targetPorts:               []int{80},
				protocols:                 []corev1.Protocol{corev1.ProtocolTCP},
				isFixed:                   false,
				externalTrafficPolicyType: corev1.ServiceExternalTrafficPolicyTypeCluster,
				lBHealthCheckSwitch:       "on",
				lBHealthCheckType:         "tcp",
				lBHealthCheckFlag:         "off",
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
			},
			setup: func(s *SlbPlugin) {
				s.minPort = 10000
				s.maxPort = 11000
				s.cache = make(map[string]portAllocated)
				s.podAllocate = make(map[string]string)
			},
			expectErr: false,
			validateSvc: func(t *testing.T, svc *corev1.Service) {
				if svc.Name != "test-pod" {
					t.Errorf("expected service name 'test-pod', got '%s'", svc.Name)
				}
				if len(svc.Spec.Ports) != 1 {
					t.Errorf("expected 1 port, got %d", len(svc.Spec.Ports))
				}
				if svc.Spec.Ports[0].Port < 10000 || svc.Spec.Ports[0].Port > 11000 {
					t.Errorf("port %d is out of range", svc.Spec.Ports[0].Port)
				}
				if svc.Spec.Ports[0].TargetPort.IntValue() != 80 {
					t.Errorf("expected target port 80, got %d", svc.Spec.Ports[0].TargetPort.IntValue())
				}
				if svc.Spec.Type != corev1.ServiceTypeLoadBalancer {
					t.Errorf("expected service type LoadBalancer, got %s", svc.Spec.Type)
				}
				if svc.Annotations[SlbIdAnnotationKey] == "" {
					t.Error("expected SlbIdAnnotationKey to be set")
				}
			},
		},
		{
			name: "service construction with existing allocation",
			sc: &slbConfig{
				lbIds:                     []string{"lb-123"},
				targetPorts:               []int{8080},
				protocols:                 []corev1.Protocol{corev1.ProtocolTCP},
				isFixed:                   false,
				externalTrafficPolicyType: corev1.ServiceExternalTrafficPolicyTypeLocal,
				lBHealthCheckSwitch:       "off",
				lBHealthCheckFlag:         "on",
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod-2",
					Namespace: "default",
				},
			},
			setup: func(s *SlbPlugin) {
				s.minPort = 10000
				s.maxPort = 11000
				s.cache = map[string]portAllocated{
					"lb-123": {
						10240: true,
						10241: false,
					},
				}
				s.podAllocate = map[string]string{
					"default/test-pod-2": "lb-123:10240",
				}
			},
			expectErr: false,
			validateSvc: func(t *testing.T, svc *corev1.Service) {
				if svc.Spec.ExternalTrafficPolicy != corev1.ServiceExternalTrafficPolicyTypeLocal {
					t.Errorf("expected ExternalTrafficPolicy Local, got %s", svc.Spec.ExternalTrafficPolicy)
				}
				if svc.Annotations[LBHealthCheckFlagAnnotationKey] != "on" {
					t.Errorf("expected health check flag 'on', got '%s'", svc.Annotations[LBHealthCheckFlagAnnotationKey])
				}
				// Should use the already allocated port
				if len(svc.Spec.Ports) > 0 && svc.Spec.Ports[0].Port != 10240 {
					t.Errorf("expected port 10240 (pre-allocated), got %d", svc.Spec.Ports[0].Port)
				}
			},
		},
		{
			name: "TCP/UDP protocol service",
			sc: &slbConfig{
				lbIds:                     []string{"lb-456"},
				targetPorts:               []int{53},
				protocols:                 []corev1.Protocol{ProtocolTCPUDP},
				isFixed:                   false,
				externalTrafficPolicyType: corev1.ServiceExternalTrafficPolicyTypeCluster,
				lBHealthCheckSwitch:       "on",
				lBHealthCheckType:         "http",
				lBHealthCheckFlag:         "off",
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dns-pod",
					Namespace: "kube-system",
				},
			},
			setup: func(s *SlbPlugin) {
				s.minPort = 30000
				s.maxPort = 32767
				s.cache = make(map[string]portAllocated)
				s.podAllocate = make(map[string]string)
			},
			expectErr: false,
			validateSvc: func(t *testing.T, svc *corev1.Service) {
				// Should create both TCP and UDP ports
				if len(svc.Spec.Ports) != 2 {
					t.Errorf("expected 2 ports for TCPUDP protocol, got %d", len(svc.Spec.Ports))
				}
				if len(svc.Spec.Ports) >= 2 {
					if svc.Spec.Ports[0].Protocol != corev1.ProtocolTCP {
						t.Errorf("first port should be TCP, got %s", svc.Spec.Ports[0].Protocol)
					}
					if svc.Spec.Ports[1].Protocol != corev1.ProtocolUDP {
						t.Errorf("second port should be UDP, got %s", svc.Spec.Ports[1].Protocol)
					}
					if svc.Spec.Ports[0].Port != svc.Spec.Ports[1].Port {
						t.Error("TCP and UDP ports should have the same port number")
					}
				}
			},
		},
		{
			name: "no available ports",
			sc: &slbConfig{
				lbIds:                     []string{"lb-789"},
				targetPorts:               []int{80},
				protocols:                 []corev1.Protocol{corev1.ProtocolTCP},
				isFixed:                   false,
				externalTrafficPolicyType: corev1.ServiceExternalTrafficPolicyTypeCluster,
				lBHealthCheckSwitch:       "on",
				lBHealthCheckType:         "tcp",
				lBHealthCheckFlag:         "off",
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod-3",
					Namespace: "default",
				},
			},
			setup: func(s *SlbPlugin) {
				s.minPort = 100
				s.maxPort = 100
				s.cache = map[string]portAllocated{
					"lb-789": {
						100: true, // Only available port is taken
					},
				}
				s.podAllocate = make(map[string]string)
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			slb := &SlbPlugin{
				mutex: sync.RWMutex{},
			}

			if tt.setup != nil {
				tt.setup(slb)
			}

			svc, err := slb.consSvc(tt.sc, tt.pod, nil, context.Background())

			if tt.expectErr {
				if err == nil {
					t.Error("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if tt.validateSvc != nil {
				tt.validateSvc(t, svc)
			}
		})
	}
}
