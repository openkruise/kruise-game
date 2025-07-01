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
	"reflect"
	"sync"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
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
