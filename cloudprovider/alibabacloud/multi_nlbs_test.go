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

func TestParseMultiNLBsConfig(t *testing.T) {
	tests := []struct {
		conf            []gamekruiseiov1alpha1.NetworkConfParams
		multiNLBsConfig *multiNLBsConfig
	}{
		// case 0
		{
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{
					Name:  NlbIdNamesConfigName,
					Value: "id-xx-A/dianxin,id-xx-B/liantong,id-xx-C/dianxin,id-xx-D/liantong",
				},
				{
					Name:  PortProtocolsConfigName,
					Value: "80/TCP,80/UDP",
				},
			},
			multiNLBsConfig: &multiNLBsConfig{
				lbNames: map[string]string{
					"id-xx-A": "dianxin",
					"id-xx-B": "liantong",
					"id-xx-C": "dianxin",
					"id-xx-D": "liantong",
				},
				idList: [][]string{
					{
						"id-xx-A", "id-xx-B",
					},
					{
						"id-xx-C", "id-xx-D",
					},
				},
			},
		},
		// case 1
		{
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{
					Name:  NlbIdNamesConfigName,
					Value: "id-xx-A/dianxin,id-xx-B/dianxin,id-xx-C/dianxin,id-xx-D/liantong,id-xx-E/liantong,id-xx-F/liantong",
				},
				{
					Name:  PortProtocolsConfigName,
					Value: "80/TCP,80/UDP",
				},
			},
			multiNLBsConfig: &multiNLBsConfig{
				lbNames: map[string]string{
					"id-xx-A": "dianxin",
					"id-xx-B": "dianxin",
					"id-xx-C": "dianxin",
					"id-xx-D": "liantong",
					"id-xx-E": "liantong",
					"id-xx-F": "liantong",
				},
				idList: [][]string{
					{
						"id-xx-A", "id-xx-D",
					},
					{
						"id-xx-B", "id-xx-E",
					},
					{
						"id-xx-C", "id-xx-F",
					},
				},
			},
		},
	}

	for i, tt := range tests {
		actual, err := parseMultiNLBsConfig(tt.conf)
		if err != nil {
			t.Error(err)
		}
		if !reflect.DeepEqual(actual.lbNames, tt.multiNLBsConfig.lbNames) {
			t.Errorf("case %d: parseMultiNLBsConfig lbNames actual: %v, expect: %v", i, actual.lbNames, tt.multiNLBsConfig.lbNames)
		}
		if !reflect.DeepEqual(actual.idList, tt.multiNLBsConfig.idList) {
			t.Errorf("case %d: parseMultiNLBsConfig idList actual: %v, expect: %v", i, actual.idList, tt.multiNLBsConfig.idList)
		}
	}
}

func TestAllocate(t *testing.T) {
	tests := []struct {
		plugin           *MultiNlbsPlugin
		conf             *multiNLBsConfig
		nsName           string
		lbsPorts         *lbsPorts
		cacheAfter       [][]bool
		podAllocateAfter map[string]*lbsPorts
	}{
		// case 0: cache is nil
		{
			plugin: &MultiNlbsPlugin{
				maxPort:     int32(8002),
				minPort:     int32(8000),
				blockPorts:  []int32{8001},
				mutex:       sync.RWMutex{},
				podAllocate: make(map[string]*lbsPorts),
				cache:       make([][]bool, 0),
			},
			conf: &multiNLBsConfig{
				lbNames: map[string]string{
					"id-xx-A": "dianxin",
					"id-xx-B": "liantong",
					"id-xx-C": "dianxin",
					"id-xx-D": "liantong",
				},
				idList: [][]string{
					{
						"id-xx-A", "id-xx-B",
					},
					{
						"id-xx-C", "id-xx-D",
					},
				},
				targetPorts: []int{80, 80},
				protocols:   []corev1.Protocol{corev1.ProtocolTCP, corev1.ProtocolUDP},
			},
			nsName: "default/test-0",
			lbsPorts: &lbsPorts{
				index:      0,
				lbIds:      []string{"id-xx-A", "id-xx-B"},
				ports:      []int32{8000, 8002},
				targetPort: []int{80, 80},
				protocols:  []corev1.Protocol{corev1.ProtocolTCP, corev1.ProtocolUDP},
			},
			cacheAfter: [][]bool{{true, true, true}, {false, true, false}},
			podAllocateAfter: map[string]*lbsPorts{
				"default/test-0": {
					index:      0,
					lbIds:      []string{"id-xx-A", "id-xx-B"},
					ports:      []int32{8000, 8002},
					targetPort: []int{80, 80},
					protocols:  []corev1.Protocol{corev1.ProtocolTCP, corev1.ProtocolUDP},
				},
			},
		},
		// case 1: cache not nil & new pod
		{
			plugin: &MultiNlbsPlugin{
				maxPort:    int32(8002),
				minPort:    int32(8000),
				blockPorts: []int32{8001},
				mutex:      sync.RWMutex{},
				podAllocate: map[string]*lbsPorts{
					"default/test-0": {
						index:      0,
						lbIds:      []string{"id-xx-A", "id-xx-B"},
						ports:      []int32{8000},
						targetPort: []int{80},
						protocols:  []corev1.Protocol{corev1.ProtocolTCP},
					},
				},
				cache: [][]bool{{true, true, false}},
			},
			conf: &multiNLBsConfig{
				lbNames: map[string]string{
					"id-xx-A": "dianxin",
					"id-xx-B": "liantong",
					"id-xx-C": "dianxin",
					"id-xx-D": "liantong",
				},
				idList: [][]string{
					{
						"id-xx-A", "id-xx-B",
					},
					{
						"id-xx-C", "id-xx-D",
					},
				},
				targetPorts: []int{80, 80},
				protocols:   []corev1.Protocol{corev1.ProtocolTCP, corev1.ProtocolUDP},
			},
			nsName: "default/test-1",
			lbsPorts: &lbsPorts{
				index:      1,
				lbIds:      []string{"id-xx-C", "id-xx-D"},
				ports:      []int32{8000, 8002},
				targetPort: []int{80, 80},
				protocols:  []corev1.Protocol{corev1.ProtocolTCP, corev1.ProtocolUDP},
			},
			cacheAfter: [][]bool{{true, true, false}, {true, true, true}},
			podAllocateAfter: map[string]*lbsPorts{
				"default/test-0": {
					index:      0,
					lbIds:      []string{"id-xx-A", "id-xx-B"},
					ports:      []int32{8000},
					targetPort: []int{80},
					protocols:  []corev1.Protocol{corev1.ProtocolTCP},
				},
				"default/test-1": {
					index:      1,
					lbIds:      []string{"id-xx-C", "id-xx-D"},
					ports:      []int32{8000, 8002},
					targetPort: []int{80, 80},
					protocols:  []corev1.Protocol{corev1.ProtocolTCP, corev1.ProtocolUDP},
				},
			},
		},
		// case 2: cache not nil & old pod
		{
			plugin: &MultiNlbsPlugin{
				maxPort:    int32(8002),
				minPort:    int32(8000),
				blockPorts: []int32{8001},
				mutex:      sync.RWMutex{},
				podAllocate: map[string]*lbsPorts{
					"default/test-0": {
						index:      0,
						lbIds:      []string{"id-xx-A", "id-xx-B"},
						ports:      []int32{8000},
						targetPort: []int{80},
						protocols:  []corev1.Protocol{corev1.ProtocolTCP},
					},
				},
				cache: [][]bool{{true, true, false}},
			},
			conf: &multiNLBsConfig{
				lbNames: map[string]string{
					"id-xx-A": "dianxin",
					"id-xx-B": "liantong",
					"id-xx-C": "dianxin",
					"id-xx-D": "liantong",
				},
				idList: [][]string{
					{
						"id-xx-A", "id-xx-B",
					},
					{
						"id-xx-C", "id-xx-D",
					},
				},
				targetPorts: []int{80, 80},
				protocols:   []corev1.Protocol{corev1.ProtocolTCP, corev1.ProtocolUDP},
			},
			nsName: "default/test-0",
			lbsPorts: &lbsPorts{
				index:      0,
				lbIds:      []string{"id-xx-A", "id-xx-B"},
				ports:      []int32{8000},
				targetPort: []int{80},
				protocols:  []corev1.Protocol{corev1.ProtocolTCP},
			},
			cacheAfter: [][]bool{{true, true, false}},
			podAllocateAfter: map[string]*lbsPorts{
				"default/test-0": {
					index:      0,
					lbIds:      []string{"id-xx-A", "id-xx-B"},
					ports:      []int32{8000},
					targetPort: []int{80},
					protocols:  []corev1.Protocol{corev1.ProtocolTCP},
				},
			},
		},
	}

	for i, tt := range tests {
		plugin := tt.plugin
		lbsPorts, err := plugin.allocate(tt.conf, tt.nsName)
		if err != nil {
			t.Error(err)
		}
		if !reflect.DeepEqual(lbsPorts, tt.lbsPorts) {
			t.Errorf("case %d: allocate actual: %v, expect: %v", i, lbsPorts, tt.lbsPorts)
		}
		if !reflect.DeepEqual(plugin.podAllocate, tt.podAllocateAfter) {
			t.Errorf("case %d: podAllocate actual: %v, expect: %v", i, plugin.podAllocate, tt.podAllocateAfter)
		}
		if !reflect.DeepEqual(plugin.cache, tt.cacheAfter) {
			t.Errorf("case %d: cache actual: %v, expect: %v", i, plugin.cache, tt.cacheAfter)
		}
	}
}

func TestDeAllocate(t *testing.T) {
	tests := []struct {
		plugin           *MultiNlbsPlugin
		nsName           string
		cacheAfter       [][]bool
		podAllocateAfter map[string]*lbsPorts
	}{
		{
			plugin: &MultiNlbsPlugin{
				maxPort:    int32(8002),
				minPort:    int32(8000),
				blockPorts: []int32{8001},
				mutex:      sync.RWMutex{},
				podAllocate: map[string]*lbsPorts{
					"default/test-0": {
						index:      0,
						lbIds:      []string{"id-xx-A", "id-xx-B"},
						ports:      []int32{8000},
						targetPort: []int{80},
						protocols:  []corev1.Protocol{corev1.ProtocolTCP},
					},
					"default/test-1": {
						index:      1,
						lbIds:      []string{"id-xx-C", "id-xx-D"},
						ports:      []int32{8000, 8002},
						targetPort: []int{80, 80},
						protocols:  []corev1.Protocol{corev1.ProtocolTCP, corev1.ProtocolUDP},
					},
				},
				cache: [][]bool{{true, true, false}, {true, true, true}},
			},
			nsName:     "default/test-1",
			cacheAfter: [][]bool{{true, true, false}, {false, true, false}},
			podAllocateAfter: map[string]*lbsPorts{
				"default/test-0": {
					index:      0,
					lbIds:      []string{"id-xx-A", "id-xx-B"},
					ports:      []int32{8000},
					targetPort: []int{80},
					protocols:  []corev1.Protocol{corev1.ProtocolTCP},
				},
			},
		},
	}

	for i, tt := range tests {
		plugin := tt.plugin
		plugin.deAllocate(tt.nsName)

		if !reflect.DeepEqual(plugin.podAllocate, tt.podAllocateAfter) {
			t.Errorf("case %d: podAllocate actual: %v, expect: %v", i, plugin.podAllocate, tt.podAllocateAfter)
		}
		if !reflect.DeepEqual(plugin.cache, tt.cacheAfter) {
			t.Errorf("case %d: cache actual: %v, expect: %v", i, plugin.cache, tt.cacheAfter)
		}
	}
}

func TestInitMultiLBCache(t *testing.T) {
	tests := []struct {
		svcList     []corev1.Service
		maxPort     int32
		minPort     int32
		blockPorts  []int32
		podAllocate map[string]*lbsPorts
		cache       [][]bool
	}{
		{
			svcList: []corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							LBIDBelongIndexKey: "0",
						},
						Labels: map[string]string{
							SlbIdLabelKey:               "xxx-A",
							ServiceBelongNetworkTypeKey: MultiNlbsNetwork,
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
						Annotations: map[string]string{
							LBIDBelongIndexKey: "0",
						},
						Labels: map[string]string{
							SlbIdLabelKey:               "xxx-B",
							ServiceBelongNetworkTypeKey: MultiNlbsNetwork,
						},
						Namespace: "ns-0",
						Name:      "name-1",
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
			},
			maxPort:    int32(667),
			minPort:    int32(665),
			blockPorts: []int32{},
			podAllocate: map[string]*lbsPorts{
				"ns-0/pod-A": {
					index:      0,
					lbIds:      []string{"xxx-A", "xxx-B"},
					ports:      []int32{666},
					targetPort: []int{80},
					protocols:  []corev1.Protocol{corev1.ProtocolTCP},
				},
			},
			cache: [][]bool{{false, true, false}},
		},
	}
	for i, tt := range tests {
		podAllocate, cache := initMultiLBCache(tt.svcList, tt.maxPort, tt.minPort, tt.blockPorts)

		if !reflect.DeepEqual(podAllocate, tt.podAllocate) {
			t.Errorf("case %d: podAllocate actual: %v, expect: %v", i, podAllocate, tt.podAllocate)
		}
		if !reflect.DeepEqual(cache, tt.cache) {
			t.Errorf("case %d: cache actual: %v, expect: %v", i, cache, tt.cache)
		}
	}
}
