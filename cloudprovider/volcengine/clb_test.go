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
	"encoding/json"
	"reflect"
	"sync"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

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

	lbId, ports, err := test.clb.allocate(test.lbIds, test.num, test.podKey)
	if err != nil {
		t.Errorf("allocate failed: %v", err)
	}
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

func TestParseLbConfig_EnableClbScatter(t *testing.T) {
	conf := []gamekruiseiov1alpha1.NetworkConfParams{
		{Name: ClbIdsConfigName, Value: "clb-1,clb-2"},
		{Name: PortProtocolsConfigName, Value: "80,81"},
		{Name: EnableClbScatterConfigName, Value: "true"},
	}
	sc := parseLbConfig(conf)
	if !sc.enableClbScatter {
		t.Errorf("enableClbScatter expect true, got false")
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
						Name:     "82-TCP",
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
		if got, _ := c.consSvc(tt.args.config, tt.args.pod, tt.args.client, tt.args.ctx); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("consSvc() = %v, want %v", got, tt.want)
		}
	}
}

func TestAllocateScatter(t *testing.T) {
	clb := &ClbPlugin{
		maxPort:     120,
		minPort:     100,
		cache:       map[string]portAllocated{"clb-1": {}, "clb-2": {}},
		podAllocate: make(map[string]string),
		mutex:       sync.RWMutex{},
	}
	// 初始化 cache
	for _, id := range []string{"clb-1", "clb-2"} {
		clb.cache[id] = make(portAllocated)
		for i := clb.minPort; i < clb.maxPort; i++ {
			clb.cache[id][i] = false
		}
	}
	lbIds := []string{"clb-1", "clb-2"}
	// 连续分配 4 次，轮询应分布到 clb-1, clb-2, clb-1, clb-2
	results := make([]string, 0)
	for i := 0; i < 4; i++ {
		lbId, _, err := clb.allocate(lbIds, 1, "ns/pod"+string(rune(i)), true)
		if err != nil {
			t.Errorf("error when allocating ports")
		}
		results = append(results, lbId)
	}
	if !(results[0] != results[1] && results[0] == results[2] && results[1] == results[3]) {
		t.Errorf("scatter allocate not round robin: %v", results)
	}
}
func TestAllocate2(t *testing.T) {
	tests := []struct {
		name             string
		lbIds            []string
		num              int
		nsName           string
		clb              *ClbPlugin
		enableClbScatter bool
		wantLbId         string
		wantPortsLen     int
		wantErr          bool
	}{
		{
			name:     "no load balancer IDs",
			lbIds:    []string{},
			num:      1,
			nsName:   "default/test-pod",
			clb:      &ClbPlugin{mutex: sync.RWMutex{}},
			wantErr:  true,
			wantLbId: "",
		},
		{
			name:   "normal allocation without scatter",
			lbIds:  []string{"lb-1", "lb-2"},
			num:    2,
			nsName: "default/test-pod",
			clb: &ClbPlugin{
				maxPort:     600,
				minPort:     500,
				cache:       map[string]portAllocated{},
				podAllocate: map[string]string{},
				mutex:       sync.RWMutex{},
			},
			wantLbId:     "lb-1",
			wantPortsLen: 2,
			wantErr:      false,
		},
		{
			name:   "allocation with scatter enabled",
			lbIds:  []string{"lb-1", "lb-2"},
			num:    2,
			nsName: "default/test-pod",
			clb: &ClbPlugin{
				maxPort:        600,
				minPort:        500,
				cache:          map[string]portAllocated{},
				podAllocate:    map[string]string{},
				mutex:          sync.RWMutex{},
				lastScatterIdx: 0,
			},
			enableClbScatter: true,
			wantLbId:         "lb-1", // First allocation should go to lb-1
			wantPortsLen:     2,
			wantErr:          false,
		},
		{
			name:   "insufficient ports available",
			lbIds:  []string{"lb-1"},
			num:    10, // Request more ports than available
			nsName: "default/test-pod",
			clb: &ClbPlugin{
				maxPort: 505, // Only 5 ports available (500-504)
				minPort: 500,
				cache: map[string]portAllocated{
					"lb-1": map[int32]bool{
						502: true, // One port already allocated
					},
				},
				podAllocate: map[string]string{},
				mutex:       sync.RWMutex{},
			},
			wantErr: true,
		},
		{
			name:   "allocate multiple ports",
			lbIds:  []string{"lb-1"},
			num:    3,
			nsName: "default/test-pod",
			clb: &ClbPlugin{
				maxPort:     510,
				minPort:     500,
				cache:       map[string]portAllocated{},
				podAllocate: map[string]string{},
				mutex:       sync.RWMutex{},
			},
			wantLbId:     "lb-1",
			wantPortsLen: 3,
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotLbId, gotPorts, err := tt.clb.allocate(tt.lbIds, tt.num, tt.nsName, tt.enableClbScatter)

			// Check error
			if (err != nil) != tt.wantErr {
				t.Errorf("allocate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// If we expect an error, we don't need to check the other conditions
			if tt.wantErr {
				return
			}

			// Check lbId
			if gotLbId != tt.wantLbId {
				t.Errorf("allocate() gotLbId = %v, want %v", gotLbId, tt.wantLbId)
			}

			// Check number of ports
			if len(gotPorts) != tt.wantPortsLen {
				t.Errorf("allocate() got %d ports, want %d", len(gotPorts), tt.wantPortsLen)
			}

			// Check if ports are within range
			for _, port := range gotPorts {
				if port < tt.clb.minPort || port >= tt.clb.maxPort {
					t.Errorf("allocated port %d out of range [%d, %d)", port, tt.clb.minPort, tt.clb.maxPort)
				}

				// Verify the port is marked as allocated in cache
				if !tt.clb.cache[gotLbId][port] {
					t.Errorf("port %d not marked as allocated in cache", port)
				}
			}

			// Check if podAllocate map is updated
			if allocStr, ok := tt.clb.podAllocate[tt.nsName]; !ok {
				t.Errorf("podAllocate not updated for %s", tt.nsName)
			} else {
				expected := gotLbId + ":" + util.Int32SliceToString(gotPorts, ",")
				if allocStr != expected {
					t.Errorf("podAllocate[%s] = %s, want %s", tt.nsName, allocStr, expected)
				}
			}
		})
	}
}
func TestClbPlugin_OnPodUpdated(t *testing.T) {
	baseAnnotations := map[string]string{
		gamekruiseiov1alpha1.GameServerNetworkType: "clb",
	}
	// Create test cases
	tests := []struct {
		name               string
		serviceExists      bool
		serviceOwnerUID    types.UID
		networkStatus      *gamekruiseiov1alpha1.NetworkStatus
		networkConfig      []gamekruiseiov1alpha1.NetworkConfParams
		serviceType        corev1.ServiceType
		hasIngress         bool
		networkDisabled    bool
		expectNetworkReady bool
		expectErr          bool
	}{
		{
			name:          "Service not found",
			serviceExists: false,
			networkStatus: &gamekruiseiov1alpha1.NetworkStatus{
				CurrentNetworkState: gamekruiseiov1alpha1.NetworkNotReady,
			},
			networkConfig: []gamekruiseiov1alpha1.NetworkConfParams{
				{Name: ClbIdsConfigName, Value: "clb-test"},
				{Name: PortProtocolsConfigName, Value: "80"},
			},
			expectErr: false,
		},
		{
			name:            "Service exists but owned by another pod",
			serviceExists:   true,
			serviceOwnerUID: "other-uid",
			networkStatus: &gamekruiseiov1alpha1.NetworkStatus{
				CurrentNetworkState: gamekruiseiov1alpha1.NetworkNotReady,
			},
			networkConfig: []gamekruiseiov1alpha1.NetworkConfParams{
				{Name: ClbIdsConfigName, Value: "clb-test"},
				{Name: PortProtocolsConfigName, Value: "80"},
			},
			expectErr: false,
		},
		{
			name:          "Network disabled",
			serviceExists: true,
			serviceType:   corev1.ServiceTypeLoadBalancer,
			networkStatus: &gamekruiseiov1alpha1.NetworkStatus{
				CurrentNetworkState: gamekruiseiov1alpha1.NetworkNotReady,
			},
			networkConfig: []gamekruiseiov1alpha1.NetworkConfParams{
				{Name: ClbIdsConfigName, Value: "clb-test"},
				{Name: PortProtocolsConfigName, Value: "80"},
			},
			networkDisabled: true,
			expectErr:       false,
		},
		{
			name:          "Network ready",
			serviceExists: true,
			serviceType:   corev1.ServiceTypeLoadBalancer,
			hasIngress:    true,
			networkStatus: &gamekruiseiov1alpha1.NetworkStatus{
				CurrentNetworkState: gamekruiseiov1alpha1.NetworkNotReady,
			},
			networkConfig: []gamekruiseiov1alpha1.NetworkConfParams{
				{Name: ClbIdsConfigName, Value: "clb-test"},
				{Name: PortProtocolsConfigName, Value: "80"},
			},
			expectNetworkReady: true,
			expectErr:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 动态生成 annotation
			ann := make(map[string]string)
			for k, v := range baseAnnotations {
				ann[k] = v
			}
			if tt.networkConfig != nil {
				confBytes, _ := json.Marshal(tt.networkConfig)
				ann[gamekruiseiov1alpha1.GameServerNetworkConf] = string(confBytes)
			}
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-pod",
					Namespace:   "default",
					UID:         "test-uid",
					Annotations: ann,
				},
				Status: corev1.PodStatus{
					PodIP: "192.168.1.1",
				},
			}

			var svc *corev1.Service
			if tt.serviceExists {
				svc = &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      pod.Name,
						Namespace: pod.Namespace,
					},
					Spec: corev1.ServiceSpec{
						Type: tt.serviceType,
						Ports: []corev1.ServicePort{
							{
								Port:       80,
								TargetPort: intstr.FromInt(8080),
								Protocol:   corev1.ProtocolTCP,
							},
						},
					},
				}
				if tt.serviceOwnerUID != "" {
					svc.OwnerReferences = []metav1.OwnerReference{
						{
							Kind: "Pod",
							UID:  tt.serviceOwnerUID,
						},
					}
				}
				if tt.hasIngress {
					svc.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{
						{IP: "192.168.1.100"},
					}
				}
			}

			scheme := runtime.NewScheme()
			_ = corev1.AddToScheme(scheme)
			_ = gamekruiseiov1alpha1.AddToScheme(scheme)
			builder := fake.NewClientBuilder().WithScheme(scheme)
			if svc != nil {
				builder = builder.WithObjects(svc)
			}
			fakeClient := builder.Build()
			clb := &ClbPlugin{
				maxPort:     600,
				minPort:     500,
				cache:       make(map[string]portAllocated),
				podAllocate: make(map[string]string),
				mutex:       sync.RWMutex{},
			}

			resultPod, err := clb.OnPodUpdated(fakeClient, pod, context.Background())

			if (err != nil) != tt.expectErr {
				t.Errorf("OnPodUpdated() error = %v, expectErr %v", err, tt.expectErr)
			}
			_ = resultPod
		})
	}
}

func TestClbPlugin_OnPodDeleted(t *testing.T) {
	ctx := context.Background()
	// 非 fixed 情况
	clb := &ClbPlugin{
		podAllocate: map[string]string{
			"ns1/pod1": "clb-xxx:100",
			"ns2/pod2": "clb-xxx:101",
		},
		cache: map[string]portAllocated{
			"clb-xxx": {
				100: true,
				101: true,
			},
		},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod1",
			Namespace: "ns1",
			Annotations: map[string]string{
				gamekruiseiov1alpha1.GameServerNetworkType: "clb",
				gamekruiseiov1alpha1.GameServerNetworkConf: `[{"Name":"ClbIds","Value":"clb-xxx"},{"Name":"PortProtocols","Value":"100"}]`,
			},
		},
	}
	fakeClient := fake.NewClientBuilder().Build()
	_ = clb.OnPodDeleted(fakeClient, pod, ctx)
	if _, ok := clb.podAllocate["ns1/pod1"]; ok {
		t.Errorf("OnPodDeleted should deAllocate podKey ns1/pod1")
	}

	// fixed 情况，gss 不存在，应该 deAllocate 所有关联 key
	clb2 := &ClbPlugin{
		podAllocate: map[string]string{
			"ns2/gss2": "clb-xxx:201",
		},
		cache: map[string]portAllocated{
			"clb-xxx": {
				200: true,
				201: true,
			},
		},
	}
	pod2 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod2",
			Namespace: "ns2",
			Annotations: map[string]string{
				gamekruiseiov1alpha1.GameServerNetworkType: "clb",
				gamekruiseiov1alpha1.GameServerNetworkConf: `[{"Name":"ClbIds","Value":"clb-xxx"},{"Name":"PortProtocols","Value":"200"},{"Name":"Fixed","Value":"true"}]`,
			},
			Labels: map[string]string{
				gamekruiseiov1alpha1.GameServerOwnerGssKey: "gss1",
			},
		},
	}
	fakeClient2 := fake.NewClientBuilder().Build() // 不包含 gss，模拟 not found
	_ = clb2.OnPodDeleted(fakeClient2, pod2, ctx)
	if _, ok := clb2.podAllocate["ns2/gss1"]; ok {
		t.Errorf("OnPodDeleted should deAllocate podKey ns2/gss1 for fixed case (gss not found)")
	}

	// fixed 情况，gss 存在且无删除时间戳，不应 deAllocate
	gss := &gamekruiseiov1alpha1.GameServerSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gss1",
			Namespace: "ns2",
		},
	}
	clb3 := &ClbPlugin{
		podAllocate: map[string]string{
			"ns2/gss1": "clb-xxx:200",
		},
		cache: map[string]portAllocated{
			"clb-xxx": {
				200: true,
				101: true,
			},
		},
	}
	pod3 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod3",
			Namespace: "ns2",
			Annotations: map[string]string{
				gamekruiseiov1alpha1.GameServerNetworkType: "clb",
				gamekruiseiov1alpha1.GameServerNetworkConf: `[{"Name":"ClbIds","Value":"clb-xxx"},{"Name":"PortProtocols","Value":"200"},{"Name":"Fixed","Value":"true"}]`,
			},
			Labels: map[string]string{
				gamekruiseiov1alpha1.GameServerOwnerGssKey: "gss1",
			},
		},
	}
	scheme := runtime.NewScheme()
	_ = gamekruiseiov1alpha1.AddToScheme(scheme)
	fakeClient3 := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gss).Build()
	_ = clb3.OnPodDeleted(fakeClient3, pod3, ctx)
	if _, ok := clb3.podAllocate["ns2/gss1"]; !ok {
		t.Errorf("OnPodDeleted should NOT deAllocate podKey ns2/gss1 for fixed case (gss exists)")
	}
}
