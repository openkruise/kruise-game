/*
Copyright 2025 The Kruise Authors.

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
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestIsNeedToCreateService(t *testing.T) {
	tests := []struct {
		ns           string
		gssName      string
		config       *autoNLBsConfig
		a            *AutoNLBsPlugin
		expectSvcNum int
	}{
		// case 0
		{
			ns:      "default",
			gssName: "pod",
			config: &autoNLBsConfig{
				protocols: []corev1.Protocol{
					corev1.ProtocolTCP,
					corev1.ProtocolUDP,
				},
				reserveNlbNum: 2,
				targetPorts: []int{
					6666,
					8888,
				},
				maxPort:    2500,
				minPort:    1000,
				blockPorts: []int32{},
			},
			a: &AutoNLBsPlugin{
				gssMaxPodIndex: map[string]int{
					"default/pod": 1499,
				},
				mutex: sync.RWMutex{},
			},
			expectSvcNum: 4,
		},

		// case 1
		{
			ns:      "default",
			gssName: "pod",
			config: &autoNLBsConfig{
				protocols: []corev1.Protocol{
					corev1.ProtocolTCP,
					corev1.ProtocolTCP,
					corev1.ProtocolUDP,
				},
				reserveNlbNum: 2,
				targetPorts: []int{
					6666,
					7777,
					8888,
				},
				maxPort:    1005,
				minPort:    1000,
				blockPorts: []int32{},
			},
			a: &AutoNLBsPlugin{
				gssMaxPodIndex: map[string]int{
					"default/pod": 1,
				},
				mutex: sync.RWMutex{},
			},
			expectSvcNum: 3,
		},
	}
	for i, test := range tests {
		a := test.a
		expectSvcNum := a.checkSvcNumToCreate(test.ns, test.gssName, test.config)
		if expectSvcNum != test.expectSvcNum {
			t.Errorf("case %d: expect toAddSvcNum: %d, but got toAddSvcNum: %d", i, test.expectSvcNum, expectSvcNum)
		}
	}
}

func TestConsSvcPorts(t *testing.T) {
	tests := []struct {
		a              *AutoNLBsPlugin
		svcIndex       int
		config         *autoNLBsConfig
		expectSvcPorts []corev1.ServicePort
	}{
		// case 0
		{
			a: &AutoNLBsPlugin{
				mutex: sync.RWMutex{},
			},
			svcIndex: 0,
			config: &autoNLBsConfig{
				protocols: []corev1.Protocol{
					corev1.ProtocolTCP,
					corev1.ProtocolUDP,
				},
				targetPorts: []int{
					6666,
					8888,
				},
				maxPort:    1003,
				minPort:    1000,
				blockPorts: []int32{},
			},
			expectSvcPorts: []corev1.ServicePort{
				{
					Name:       "tcp-0-6666",
					TargetPort: intstr.FromString("tcp-0-6666"),
					Port:       1000,
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "udp-0-8888",
					TargetPort: intstr.FromString("udp-0-8888"),
					Port:       1001,
					Protocol:   corev1.ProtocolUDP,
				},
				{
					Name:       "tcp-1-6666",
					TargetPort: intstr.FromString("tcp-1-6666"),
					Port:       1002,
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "udp-1-8888",
					TargetPort: intstr.FromString("udp-1-8888"),
					Port:       1003,
					Protocol:   corev1.ProtocolUDP,
				},
			},
		},

		// case 1
		{
			a: &AutoNLBsPlugin{
				mutex: sync.RWMutex{},
			},
			svcIndex: 1,
			config: &autoNLBsConfig{
				protocols: []corev1.Protocol{
					corev1.ProtocolTCP,
					corev1.ProtocolTCP,
					corev1.ProtocolUDP,
				},
				targetPorts: []int{
					6666,
					7777,
					8888,
				},
				maxPort:    1004,
				minPort:    1000,
				blockPorts: []int32{},
			},
			expectSvcPorts: []corev1.ServicePort{
				{
					Name:       "tcp-1-6666",
					TargetPort: intstr.FromString("tcp-1-6666"),
					Port:       1000,
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "tcp-1-7777",
					TargetPort: intstr.FromString("tcp-1-7777"),
					Port:       1001,
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "udp-1-8888",
					TargetPort: intstr.FromString("udp-1-8888"),
					Port:       1002,
					Protocol:   corev1.ProtocolUDP,
				},
			},
		},

		// case 2
		{
			a: &AutoNLBsPlugin{
				mutex: sync.RWMutex{},
			},
			svcIndex: 3,
			config: &autoNLBsConfig{
				protocols: []corev1.Protocol{
					ProtocolTCPUDP,
				},
				targetPorts: []int{
					6666,
				},
				maxPort:    1004,
				minPort:    1000,
				blockPorts: []int32{1002},
			},
			expectSvcPorts: []corev1.ServicePort{
				{
					Name:       "tcp-12-6666",
					TargetPort: intstr.FromString("tcp-12-6666"),
					Port:       1000,
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "udp-12-6666",
					TargetPort: intstr.FromString("udp-12-6666"),
					Port:       1000,
					Protocol:   corev1.ProtocolUDP,
				},
				{
					Name:       "tcp-13-6666",
					TargetPort: intstr.FromString("tcp-13-6666"),
					Port:       1001,
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "udp-13-6666",
					TargetPort: intstr.FromString("udp-13-6666"),
					Port:       1001,
					Protocol:   corev1.ProtocolUDP,
				},
				{
					Name:       "tcp-14-6666",
					TargetPort: intstr.FromString("tcp-14-6666"),
					Port:       1003,
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "udp-14-6666",
					TargetPort: intstr.FromString("udp-14-6666"),
					Port:       1003,
					Protocol:   corev1.ProtocolUDP,
				},
				{
					Name:       "tcp-15-6666",
					TargetPort: intstr.FromString("tcp-15-6666"),
					Port:       1004,
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "udp-15-6666",
					TargetPort: intstr.FromString("udp-15-6666"),
					Port:       1004,
					Protocol:   corev1.ProtocolUDP,
				},
			},
		},
	}
	for i, test := range tests {
		svcPorts := test.a.consSvcPorts(test.svcIndex, test.config)
		if !reflect.DeepEqual(svcPorts, test.expectSvcPorts) {
			t.Errorf("case %d: expect svcPorts: %v, but got svcPorts: %v", i, test.expectSvcPorts, svcPorts)
		}
	}
}
