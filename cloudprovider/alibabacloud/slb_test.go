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
	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/pkg/util"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sync"
	"testing"
)

func TestAllocate(t *testing.T) {
	test := struct {
		lbId string
		slb  *SlbPlugin
	}{
		lbId: "xxx-A",
		slb: &SlbPlugin{
			maxPort: int32(712),
			minPort: int32(512),
			cache:   make(map[string]portAllocated),
			mutex:   sync.RWMutex{},
		},
	}

	port := test.slb.allocate(test.lbId)
	if port > test.slb.maxPort || port < test.slb.minPort {
		t.Errorf("allocate port %d, unexpected", port)
	}

	test.slb.deAllocate(test.lbId, port)
	if test.slb.cache[test.lbId][port] == true {
		t.Errorf("deAllocate port %d failed", port)
	}
}

func TestParseLbConfig(t *testing.T) {
	tests := []struct {
		conf     []gamekruiseiov1alpha1.NetworkConfParams
		lbId     string
		ports    []int
		protocol []corev1.Protocol
	}{
		{
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{
					Name:  SlbIdConfigName,
					Value: "xxx-A",
				},
				{
					Name:  PortProtocolConfigName,
					Value: "80",
				},
			},
			lbId:     "xxx-A",
			ports:    []int{80},
			protocol: []corev1.Protocol{corev1.ProtocolTCP},
		},
		{
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{
					Name:  SlbIdConfigName,
					Value: "xxx-A",
				},
				{
					Name:  PortProtocolConfigName,
					Value: "81/UDP,82,83/TCP",
				},
			},
			lbId:     "xxx-A",
			ports:    []int{81, 82, 83},
			protocol: []corev1.Protocol{corev1.ProtocolUDP, corev1.ProtocolTCP, corev1.ProtocolTCP},
		},
	}

	for _, test := range tests {
		lbId, ports, protocol := parseLbConfig(test.conf)
		if lbId != test.lbId {
			t.Errorf("lbId expect: %s, actual: %s", test.lbId, lbId)
		}
		if !util.IsSliceEqual(ports, test.ports) {
			t.Errorf("ports expect: %v, actual: %v", test.ports, ports)
		}
		if len(test.protocol) != len(protocol) {
			t.Errorf("protocol expect: %v, actual: %v", test.protocol, protocol)
		}
		for i := 0; i < len(test.protocol); i++ {
			if protocol[i] != test.protocol[i] {
				t.Errorf("protocol expect: %v, actual: %v", test.protocol, protocol)
			}
		}
	}
}

func TestInitLbCache(t *testing.T) {
	test := struct {
		svcList []corev1.Service
		minPort int32
		maxPort int32
		result  map[string]portAllocated
	}{
		minPort: 512,
		maxPort: 712,
		result: map[string]portAllocated{
			"xxx-A": map[int32]bool{
				666: true,
			},
			"xxx-B": map[int32]bool{
				555: true,
			},
		},
		svcList: []corev1.Service{
			{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						SlbIdLabelKey: "xxx-A",
					},
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
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeLoadBalancer,
					Selector: map[string]string{
						SvcSelectorKey: "pod-B",
					},
					Ports: []corev1.ServicePort{
						{
							TargetPort: intstr.FromInt(80),
							Port:       9999,
							Protocol:   corev1.ProtocolTCP,
						},
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

	actual := initLbCache(test.svcList, test.minPort, test.maxPort)
	for lb, pa := range test.result {
		for port, isAllocated := range pa {
			if actual[lb][port] != isAllocated {
				t.Errorf("lb %s port %d isAllocated, expect: %t, actual: %t", lb, port, isAllocated, actual[lb][port])
			}
		}
	}
}
