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

package volcengine

import (
	"context"
	"k8s.io/utils/ptr"
	"reflect"
	"sync"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/pkg/util"
)

func TestAllocateDeAllocate(t *testing.T) {
	test := struct {
		lbIds  []string
		clb    *ClbPlugin
		num    int
		podKey string
	}{
		lbIds: []string{"xxx-A"},
		clb: &ClbPlugin{
			maxPort:     int32(712),
			minPort:     int32(512),
			cache:       make(map[string]portAllocated),
			podAllocate: make(map[string]string),
			mutex:       sync.RWMutex{},
		},
		podKey: "xxx/xxx",
		num:    3,
	}

	lbId, ports := test.clb.allocate(test.lbIds, test.num, test.podKey)
	if _, exist := test.clb.podAllocate[test.podKey]; !exist {
		t.Errorf("podAllocate[%s] is empty after allocated", test.podKey)
	}
	for _, port := range ports {
		if port > test.clb.maxPort || port < test.clb.minPort {
			t.Errorf("allocate port %d, unexpected", port)
		}
		if test.clb.cache[lbId][port] == false {
			t.Errorf("Allocate port %d failed", port)
		}
	}

	test.clb.deAllocate(test.podKey)
	for _, port := range ports {
		if test.clb.cache[lbId][port] == true {
			t.Errorf("deAllocate port %d failed", port)
		}
	}
	if _, exist := test.clb.podAllocate[test.podKey]; exist {
		t.Errorf("podAllocate[%s] is not empty after deallocated", test.podKey)
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
					Name:  ClbIdsConfigName,
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
					Name:  ClbIdsConfigName,
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
		svcList     []corev1.Service
		minPort     int32
		maxPort     int32
		cache       map[string]portAllocated
		podAllocate map[string]string
	}{
		minPort: 512,
		maxPort: 712,
		cache: map[string]portAllocated{
			"xxx-A": map[int32]bool{
				666: true,
			},
			"xxx-B": map[int32]bool{
				555: true,
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
						ClbIdLabelKey: "xxx-A",
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
						ClbIdLabelKey: "xxx-B",
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

	actualCache, actualPodAllocate := initLbCache(test.svcList, test.minPort, test.maxPort)
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

func TestClbPlugin_consSvc(t *testing.T) {
	type fields struct {
		maxPort     int32
		minPort     int32
		cache       map[string]portAllocated
		podAllocate map[string]string
	}
	type args struct {
		config *clbConfig
		pod    *corev1.Pod
		client client.Client
		ctx    context.Context
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   *corev1.Service
	}{
		{
			name: "convert svc cache exist",
			fields: fields{
				maxPort: 3000,
				minPort: 1,
				cache: map[string]portAllocated{
					"default/test-pod": map[int32]bool{},
				},
				podAllocate: map[string]string{
					"default/test-pod": "clb-xxx:80,81",
				},
			},
			args: args{
				config: &clbConfig{
					lbIds:       []string{"clb-xxx"},
					targetPorts: []int{82},
					protocols: []corev1.Protocol{
						corev1.ProtocolTCP,
					},
					isFixed: false,
					annotations: map[string]string{
						"service.beta.kubernetes.io/volcengine-loadbalancer-health-check-flag": "on",
						"service.beta.kubernetes.io/volcengine-loadbalancer-healthy-threshold": "3",
						"service.beta.kubernetes.io/volcengine-loadbalancer-scheduler":         "wrr",
						"service.beta.kubernetes.io/volcengine-loadbalancer-pass-through":      "true",
					},
					allocateLoadBalancerNodePorts: true,
				},
				pod: &corev1.Pod{
					TypeMeta: metav1.TypeMeta{
						Kind:       "pod",
						APIVersion: "v1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "default",
						UID:       "32fqwfqfew",
					},
				},
				client: nil,
				ctx:    context.Background(),
			},
			want: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Annotations: map[string]string{
						ClbSchedulerKey:    ClbSchedulerWRR,
						ClbAddressTypeKey:  ClbAddressTypePublic,
						ClbIdAnnotationKey: "clb-xxx",
						ClbConfigHashKey: util.GetHash(&clbConfig{
							lbIds:       []string{"clb-xxx"},
							targetPorts: []int{82},
							protocols: []corev1.Protocol{
								corev1.ProtocolTCP,
							},
							isFixed: false,
							annotations: map[string]string{
								"service.beta.kubernetes.io/volcengine-loadbalancer-health-check-flag": "on",
								"service.beta.kubernetes.io/volcengine-loadbalancer-healthy-threshold": "3",
								"service.beta.kubernetes.io/volcengine-loadbalancer-scheduler":         "wrr",
								"service.beta.kubernetes.io/volcengine-loadbalancer-pass-through":      "true",
							},
							allocateLoadBalancerNodePorts: true,
						}),
						"service.beta.kubernetes.io/volcengine-loadbalancer-health-check-flag": "on",
						"service.beta.kubernetes.io/volcengine-loadbalancer-healthy-threshold": "3",
						"service.beta.kubernetes.io/volcengine-loadbalancer-pass-through":      "true",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         "v1",
							Kind:               "pod",
							Name:               "test-pod",
							UID:                "32fqwfqfew",
							Controller:         ptr.To[bool](true),
							BlockOwnerDeletion: ptr.To[bool](true),
						},
					},
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeLoadBalancer,
					Selector: map[string]string{
						SvcSelectorKey: "test-pod",
					},
					Ports: []corev1.ServicePort{{
						Name:     "82",
						Port:     80,
						Protocol: "TCP",
						TargetPort: intstr.IntOrString{
							Type:   0,
							IntVal: 82,
						},
					},
					},
					AllocateLoadBalancerNodePorts: ptr.To[bool](true),
				},
			},
		},
	}
	for _, tt := range tests {
		c := &ClbPlugin{
			maxPort:     tt.fields.maxPort,
			minPort:     tt.fields.minPort,
			cache:       tt.fields.cache,
			podAllocate: tt.fields.podAllocate,
		}
		if got := c.consSvc(tt.args.config, tt.args.pod, tt.args.client, tt.args.ctx); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("consSvc() = %v, want %v", got, tt.want)
		}
	}
}
