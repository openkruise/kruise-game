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
	"reflect"
	"sync"
	"testing"
)

func TestAllocate(t *testing.T) {
	test := struct {
		lbIds []string
		slb   *SlbPlugin
		num   int
	}{
		lbIds: []string{"xxx-A"},
		slb: &SlbPlugin{
			maxPort: int32(712),
			minPort: int32(512),
			cache:   make(map[string]portAllocated),
			mutex:   sync.RWMutex{},
		},
		num: 3,
	}

	lbId, ports := test.slb.allocate(test.lbIds, test.num)
	for _, port := range ports {
		if port > test.slb.maxPort || port < test.slb.minPort {
			t.Errorf("allocate port %d, unexpected", port)
		}

		test.slb.deAllocate(lbId, port)
		if test.slb.cache[lbId][port] == true {
			t.Errorf("deAllocate port %d failed", port)
		}
	}
}

func TestParseLbConfig(t *testing.T) {
	tests := []struct {
		conf      []gamekruiseiov1alpha1.NetworkConfParams
		lbIds     []string
		ports     []int
		protocols []corev1.Protocol
		isFixed   bool
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
			},
			lbIds:     []string{"xxx-A"},
			ports:     []int{80},
			protocols: []corev1.Protocol{corev1.ProtocolTCP},
			isFixed:   false,
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
			},
			lbIds:     []string{"xxx-A", "xxx-B"},
			ports:     []int{81, 82, 83},
			protocols: []corev1.Protocol{corev1.ProtocolUDP, corev1.ProtocolTCP, corev1.ProtocolTCP},
			isFixed:   true,
		},
	}

	for _, test := range tests {
		sc := parseLbConfig(test.conf)
		if !reflect.DeepEqual(test.lbIds, sc.lbIds) {
			t.Errorf("lbId expect: %v, actual: %v", test.lbIds, sc.lbIds)
		}
		if !util.IsSliceEqual(test.ports, sc.targetPorts) {
			t.Errorf("ports expect: %v, actual: %v", test.ports, sc.targetPorts)
		}
		if !reflect.DeepEqual(test.protocols, sc.protocols) {
			t.Errorf("protocols expect: %v, actual: %v", test.protocols, sc.protocols)
		}
		if test.isFixed != sc.isFixed {
			t.Errorf("isFixed expect: %v, actual: %v", test.isFixed, sc.isFixed)
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
