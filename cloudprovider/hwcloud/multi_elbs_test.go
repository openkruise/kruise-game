package hwcloud

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestMultiElbsAllocateKeepsSnapshotForAllocatedPod(t *testing.T) {
	const nsName = "default/test-pod-0"
	level := func() []bool { return make([]bool, 1000) }
	markPorts := func(plugin *MultiElbsPlugin, index int, ports ...int32) *MultiElbsPlugin {
		for _, port := range ports {
			plugin.cache[index][port-plugin.minPort] = true
		}
		return plugin
	}

	tests := []struct {
		name          string
		plugin        *MultiElbsPlugin
		conf          *multiELBsConfig
		want          *lbsPorts
		wantUsedPorts map[int][]int32
		wantUsedCount map[int]int
	}{
		{
			name: "target port value changes",
			plugin: &MultiElbsPlugin{
				minPort: 6000,
				maxPort: 6005,
				cache:   [][]bool{{true, false, false, false, false, false}},
				podAllocate: map[string]*lbsPorts{
					nsName: {
						index:      0,
						lbIds:      []string{"elb-1"},
						lbNames:    []string{"pool-a"},
						ports:      []int32{6000},
						targetPort: []int{80},
						protocols:  []corev1.Protocol{corev1.ProtocolTCP},
					},
				},
			},
			conf: &multiELBsConfig{
				lbNames:        map[string]string{"elb-1": "pool-a"},
				idList:         [][]string{{"elb-1"}},
				targetPorts:    []int{81},
				protocols:      []corev1.Protocol{corev1.ProtocolTCP},
				allocatePolicy: "default",
			},
			want: &lbsPorts{
				index:      0,
				lbIds:      []string{"elb-1"},
				lbNames:    []string{"pool-a"},
				ports:      []int32{6000},
				targetPort: []int{80},
				protocols:  []corev1.Protocol{corev1.ProtocolTCP},
			},
			wantUsedPorts: map[int][]int32{0: []int32{6000}},
		},
		{
			name: "target port count grows",
			plugin: markPorts(&MultiElbsPlugin{
				minPort: 6000,
				maxPort: 6999,
				cache:   [][]bool{level(), level()},
				podAllocate: map[string]*lbsPorts{
					nsName: {
						index:      1,
						lbIds:      []string{"elb-1"},
						lbNames:    []string{"b"},
						ports:      []int32{6076},
						targetPort: []int{80},
						protocols:  []corev1.Protocol{corev1.ProtocolTCP},
					},
				},
			}, 1, 6076),
			conf: &multiELBsConfig{
				lbNames:        map[string]string{"elb-0": "a", "elb-1": "b"},
				idList:         [][]string{{"elb-0"}, {"elb-1"}},
				targetPorts:    []int{80, 81},
				protocols:      []corev1.Protocol{corev1.ProtocolTCP, corev1.ProtocolTCP},
				allocatePolicy: "default",
			},
			want: &lbsPorts{
				index:      1,
				lbIds:      []string{"elb-1"},
				lbNames:    []string{"b"},
				ports:      []int32{6076},
				targetPort: []int{80},
				protocols:  []corev1.Protocol{corev1.ProtocolTCP},
			},
			wantUsedPorts: map[int][]int32{1: []int32{6076}},
		},
		{
			name: "target port count shrinks",
			plugin: markPorts(&MultiElbsPlugin{
				minPort: 6000,
				maxPort: 6999,
				cache:   [][]bool{level(), level()},
				podAllocate: map[string]*lbsPorts{
					nsName: {
						index:      1,
						lbIds:      []string{"elb-1"},
						lbNames:    []string{"b"},
						ports:      []int32{6076, 6077},
						targetPort: []int{80, 81},
						protocols:  []corev1.Protocol{corev1.ProtocolTCP, corev1.ProtocolTCP},
					},
				},
			}, 1, 6076, 6077),
			conf: &multiELBsConfig{
				lbNames:        map[string]string{"elb-0": "a", "elb-1": "b"},
				idList:         [][]string{{"elb-0"}, {"elb-1"}},
				targetPorts:    []int{80},
				protocols:      []corev1.Protocol{corev1.ProtocolTCP},
				allocatePolicy: "default",
			},
			want: &lbsPorts{
				index:      1,
				lbIds:      []string{"elb-1"},
				lbNames:    []string{"b"},
				ports:      []int32{6076, 6077},
				targetPort: []int{80, 81},
				protocols:  []corev1.Protocol{corev1.ProtocolTCP, corev1.ProtocolTCP},
			},
			wantUsedPorts: map[int][]int32{1: []int32{6076, 6077}},
		},
		{
			name: "target port count grows without reserving new port",
			plugin: markPorts(&MultiElbsPlugin{
				minPort: 6000,
				maxPort: 6999,
				cache:   [][]bool{level(), level()},
				podAllocate: map[string]*lbsPorts{
					nsName: {
						index:      1,
						lbIds:      []string{"elb-1"},
						lbNames:    []string{"b"},
						ports:      []int32{6002, 6003},
						targetPort: []int{80, 81},
						protocols:  []corev1.Protocol{corev1.ProtocolTCP, corev1.ProtocolTCP},
					},
				},
			}, 1, 6002, 6003),
			conf: &multiELBsConfig{
				lbNames:        map[string]string{"elb-0": "a", "elb-1": "b"},
				idList:         [][]string{{"elb-0"}, {"elb-1"}},
				targetPorts:    []int{80, 81, 82},
				protocols:      []corev1.Protocol{corev1.ProtocolTCP, corev1.ProtocolTCP, corev1.ProtocolTCP},
				allocatePolicy: "default",
			},
			want: &lbsPorts{
				index:      1,
				lbIds:      []string{"elb-1"},
				lbNames:    []string{"b"},
				ports:      []int32{6002, 6003},
				targetPort: []int{80, 81},
				protocols:  []corev1.Protocol{corev1.ProtocolTCP, corev1.ProtocolTCP},
			},
			wantUsedCount: map[int]int{1: 2},
		},
		{
			name: "lb id changes",
			plugin: markPorts(&MultiElbsPlugin{
				minPort: 6000,
				maxPort: 6999,
				cache:   [][]bool{level()},
				podAllocate: map[string]*lbsPorts{
					nsName: {
						index:      0,
						lbIds:      []string{"elb-old"},
						lbNames:    []string{"a"},
						ports:      []int32{6000},
						targetPort: []int{80},
						protocols:  []corev1.Protocol{corev1.ProtocolTCP},
					},
				},
			}, 0, 6000),
			conf: &multiELBsConfig{
				lbNames:        map[string]string{"elb-new": "a"},
				idList:         [][]string{{"elb-new"}},
				targetPorts:    []int{80},
				protocols:      []corev1.Protocol{corev1.ProtocolTCP},
				allocatePolicy: "default",
			},
			want: &lbsPorts{
				index:      0,
				lbIds:      []string{"elb-old"},
				lbNames:    []string{"a"},
				ports:      []int32{6000},
				targetPort: []int{80},
				protocols:  []corev1.Protocol{corev1.ProtocolTCP},
			},
			wantUsedPorts: map[int][]int32{0: []int32{6000}},
		},
		{
			name: "lb name changes",
			plugin: func() *MultiElbsPlugin {
				podAllocate, cache := initMultiLBCache([]corev1.Service{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-pod-0-old",
							Namespace: "default",
							Annotations: map[string]string{
								LBIDBelongIndexKey:          "0",
								ElbIdAnnotationKey:          "elb-1",
								ElbMappingPoolAnnotationKey: "old",
							},
						},
						Spec: corev1.ServiceSpec{
							Selector: map[string]string{SvcSelectorKey: "test-pod-0"},
							Ports: []corev1.ServicePort{
								{
									Port:       6000,
									TargetPort: intstr.FromInt(80),
									Protocol:   corev1.ProtocolTCP,
								},
							},
						},
					},
				}, 6000, 6000, nil)
				return &MultiElbsPlugin{
					minPort:     6000,
					maxPort:     6000,
					cache:       cache,
					podAllocate: podAllocate,
				}
			}(),
			conf: &multiELBsConfig{
				lbNames:        map[string]string{"elb-1": "new"},
				idList:         [][]string{{"elb-1"}},
				targetPorts:    []int{80},
				protocols:      []corev1.Protocol{corev1.ProtocolTCP},
				allocatePolicy: "default",
			},
			want: &lbsPorts{
				index:      0,
				lbIds:      []string{"elb-1"},
				lbNames:    []string{"old"},
				ports:      []int32{6000},
				targetPort: []int{80},
				protocols:  []corev1.Protocol{corev1.ProtocolTCP},
			},
			wantUsedPorts: map[int][]int32{0: []int32{6000}},
		},
		{
			name: "target port grows before capacity check",
			plugin: &MultiElbsPlugin{
				minPort: 6000,
				maxPort: 6000,
				cache:   [][]bool{{true}},
				podAllocate: map[string]*lbsPorts{
					nsName: {
						index:      0,
						lbIds:      []string{"elb-0"},
						lbNames:    []string{"a"},
						ports:      []int32{6000},
						targetPort: []int{80},
						protocols:  []corev1.Protocol{corev1.ProtocolTCP},
					},
				},
			},
			conf: &multiELBsConfig{
				lbNames:        map[string]string{"elb-0": "a"},
				idList:         [][]string{{"elb-0"}},
				targetPorts:    []int{80, 81},
				protocols:      []corev1.Protocol{corev1.ProtocolTCP, corev1.ProtocolTCP},
				allocatePolicy: "default",
			},
			want: &lbsPorts{
				index:      0,
				lbIds:      []string{"elb-0"},
				lbNames:    []string{"a"},
				ports:      []int32{6000},
				targetPort: []int{80},
				protocols:  []corev1.Protocol{corev1.ProtocolTCP},
			},
			wantUsedPorts: map[int][]int32{0: []int32{6000}},
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := tt.plugin.podAllocate[nsName]
			if before == nil {
				t.Fatalf("case %d: expected existing allocation for %s", i, nsName)
			}

			gotAllocated, err := tt.plugin.allocate(tt.conf, nsName)
			if err != nil {
				t.Fatalf("case %d: expected allocate to keep existing snapshot, got: %v", i, err)
			}
			if gotAllocated != before {
				t.Fatalf("case %d: expected allocate to return existing allocation object", i)
			}
			got := tt.plugin.podAllocate[nsName]
			if got != before {
				t.Fatalf("case %d: expected existing allocation object to stay unchanged", i)
			}
			if got.index != tt.want.index {
				t.Fatalf("case %d: index actual: %d, expect: %d", i, got.index, tt.want.index)
			}
			if !reflect.DeepEqual(got.lbIds, tt.want.lbIds) {
				t.Fatalf("case %d: lbIds actual: %v, expect: %v", i, got.lbIds, tt.want.lbIds)
			}
			if !reflect.DeepEqual(got.lbNames, tt.want.lbNames) {
				t.Fatalf("case %d: lbNames actual: %v, expect: %v", i, got.lbNames, tt.want.lbNames)
			}
			if !reflect.DeepEqual(got.ports, tt.want.ports) {
				t.Fatalf("case %d: ports actual: %v, expect: %v", i, got.ports, tt.want.ports)
			}
			if !reflect.DeepEqual(got.targetPort, tt.want.targetPort) {
				t.Fatalf("case %d: targetPort actual: %v, expect: %v", i, got.targetPort, tt.want.targetPort)
			}
			if !reflect.DeepEqual(got.protocols, tt.want.protocols) {
				t.Fatalf("case %d: protocols actual: %v, expect: %v", i, got.protocols, tt.want.protocols)
			}
			for index, ports := range tt.wantUsedPorts {
				for _, port := range ports {
					if !tt.plugin.cache[index][port-tt.plugin.minPort] {
						t.Fatalf("case %d: expected port %d to stay marked used on index %d", i, port, index)
					}
				}
			}
			for index, wantCount := range tt.wantUsedCount {
				used := 0
				for _, taken := range tt.plugin.cache[index] {
					if taken {
						used++
					}
				}
				if used != wantCount {
					t.Fatalf("case %d: used port count actual: %d, expect: %d", i, used, wantCount)
				}
			}
		})
	}
}

func TestMultiElbsConsSvcUsesUpdatedTargetPortAndHealthCheckConfig(t *testing.T) {
	plugin := &MultiElbsPlugin{}
	podLbsPorts := &lbsPorts{
		index:      0,
		lbIds:      []string{"elb-1"},
		lbNames:    []string{"pool-a"},
		ports:      []int32{6000},
		targetPort: []int{81},
		protocols:  []corev1.Protocol{corev1.ProtocolTCP},
	}
	conf := &multiELBsConfig{
		lbNames:               map[string]string{"elb-1": "pool-a"},
		idList:                [][]string{{"elb-1"}},
		targetPorts:           []int{81},
		protocols:             []corev1.Protocol{corev1.ProtocolTCP},
		allocatePolicy:        "default",
		elbClass:              "performance",
		lbHealthCheckFlag:     "on",
		lbHealthCheckConfig:   `[{"protocol":"tcp","pod_target_port":"TCP:81","monitor_port":"8080"}]`,
		externalTrafficPolicy: corev1.ServiceExternalTrafficPolicyTypeCluster,
	}
	pod := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Pod",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod-0",
			Namespace: "default",
			UID:       "pod-uid-1",
		},
	}

	svc, err := plugin.consSvc(podLbsPorts, conf, pod, "elb-1", "pool-a", 1, nil, context.Background())
	if err != nil {
		t.Fatalf("consSvc returned error: %v", err)
	}

	if len(svc.Spec.Ports) != 1 {
		t.Fatalf("expected 1 service port, got %d", len(svc.Spec.Ports))
	}
	if svc.Spec.Ports[0].TargetPort.IntValue() != 81 {
		t.Fatalf("expected service targetPort 81, got %d", svc.Spec.Ports[0].TargetPort.IntValue())
	}

	got := svc.Annotations[ElbHealthCheckOptionsAnnotationKey]
	var options []HealthCheckOption
	if err := json.Unmarshal([]byte(got), &options); err != nil {
		t.Fatalf("failed to unmarshal health check annotation %q: %v", got, err)
	}
	if len(options) != 1 {
		t.Fatalf("expected 1 health check option, got %d", len(options))
	}
	if options[0].Protocol != "tcp" {
		t.Fatalf("expected protocol tcp, got %s", options[0].Protocol)
	}
	if options[0].TargetServicePort != "TCP:6000" {
		t.Fatalf("expected target_service_port TCP:6000, got %s", options[0].TargetServicePort)
	}
	if options[0].MonitorPort != "8080" {
		t.Fatalf("expected monitor_port 8080, got %s", options[0].MonitorPort)
	}
}

func TestMultiElbsConsSvcUsesSnapshotLbBindingWhenConfigChanged(t *testing.T) {
	plugin := &MultiElbsPlugin{}
	podLbsPorts := &lbsPorts{
		index:      0,
		lbIds:      []string{"elb-old"},
		lbNames:    []string{"old"},
		ports:      []int32{6000},
		targetPort: []int{80},
		protocols:  []corev1.Protocol{corev1.ProtocolTCP},
	}
	conf := &multiELBsConfig{
		lbNames:               map[string]string{"elb-new": "new"},
		idList:                [][]string{{"elb-new"}},
		targetPorts:           []int{80},
		protocols:             []corev1.Protocol{corev1.ProtocolTCP},
		allocatePolicy:        "default",
		elbClass:              "performance",
		lbHealthCheckFlag:     "off",
		externalTrafficPolicy: corev1.ServiceExternalTrafficPolicyTypeCluster,
	}
	pod := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Pod",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod-0",
			Namespace: "default",
			UID:       "pod-uid-1",
		},
	}

	svc, err := plugin.consSvc(podLbsPorts, conf, pod, "elb-old", "old", 1, nil, context.Background())
	if err != nil {
		t.Fatalf("consSvc returned error: %v", err)
	}

	if svc.Name != "test-pod-0-old" {
		t.Fatalf("expected old lbName in service name, got %s", svc.Name)
	}
	if got := svc.Annotations[ElbIdAnnotationKey]; got != "elb-old" {
		t.Fatalf("expected old lbId annotation, got %q", got)
	}
	if got := svc.Annotations[ElbMappingPoolAnnotationKey]; got != "old" {
		t.Fatalf("expected old lbName annotation, got %q", got)
	}
}

func TestMultiElbsAllocatedLbBindingsFallsBackAndSkips(t *testing.T) {
	conf := &multiELBsConfig{lbNames: map[string]string{"elb-1": "pool-a"}}

	tests := []struct {
		name        string
		podLbsPorts *lbsPorts
		want        []allocatedLbBinding
	}{
		{
			name: "uses snapshot names",
			podLbsPorts: &lbsPorts{
				lbIds:   []string{"elb-1", "elb-2"},
				lbNames: []string{"pool-a", "pool-b"},
			},
			want: []allocatedLbBinding{{id: "elb-1", name: "pool-a"}, {id: "elb-2", name: "pool-b"}},
		},
		{
			name: "falls back to config and skips missing lb",
			podLbsPorts: &lbsPorts{
				lbIds:   []string{"elb-1", "elb-gone"},
				lbNames: []string{"", ""},
			},
			want: []allocatedLbBinding{{id: "elb-1", name: "pool-a"}},
		},
		{
			name: "falls back when lbNames is shorter than lbIds",
			podLbsPorts: &lbsPorts{
				lbIds:   []string{"elb-1"},
				lbNames: []string{},
			},
			want: []allocatedLbBinding{{id: "elb-1", name: "pool-a"}},
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := allocatedLbBindings(tt.podLbsPorts, conf)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("case %d: allocatedLbBindings actual: %+v, expect: %+v", i, got, tt.want)
			}
		})
	}
}

func TestParseMultiElbsConfig(t *testing.T) {
	tests := []struct {
		conf                []gamekruiseiov1alpha1.NetworkConfParams
		lbNames             map[string]string
		idList              [][]string
		lbHealthCheckFlag   string
		lbHealthCheckConfig string
	}{
		{
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{Name: ElbIdNamesConfigName, Value: "elb-1/pool-a,elb-2/pool-a,elb-3/pool-b,elb-4/pool-b"},
				{Name: PortProtocolsConfigName, Value: "81/TCP"},
				{Name: ElbHealthCheckFlagConfigName, Value: "on"},
				{Name: ElbHealthCheckOptionsConfigName, Value: `[{"protocol":"tcp","pod_target_port":"TCP:81"}]`},
			},
			lbNames: map[string]string{
				"elb-1": "pool-a",
				"elb-2": "pool-a",
				"elb-3": "pool-b",
				"elb-4": "pool-b",
			},
			idList:              [][]string{{"elb-1", "elb-3"}, {"elb-2", "elb-4"}},
			lbHealthCheckFlag:   "on",
			lbHealthCheckConfig: `[{"protocol":"tcp","pod_target_port":"TCP:81"}]`,
		},
	}

	for i, tt := range tests {
		actual, err := parseMultiELBsConfig(tt.conf)
		if err != nil {
			t.Fatalf("case %d: parseMultiELBsConfig returned error: %v", i, err)
		}
		if !reflect.DeepEqual(actual.lbNames, tt.lbNames) {
			t.Fatalf("case %d: lbNames actual: %v, expect: %v", i, actual.lbNames, tt.lbNames)
		}
		if !reflect.DeepEqual(actual.idList, tt.idList) {
			t.Fatalf("case %d: idList actual: %v, expect: %v", i, actual.idList, tt.idList)
		}
		if actual.lbHealthCheckFlag != tt.lbHealthCheckFlag {
			t.Fatalf("case %d: lbHealthCheckFlag actual: %s, expect: %s", i, actual.lbHealthCheckFlag, tt.lbHealthCheckFlag)
		}
		if actual.lbHealthCheckConfig != tt.lbHealthCheckConfig {
			t.Fatalf("case %d: lbHealthCheckConfig actual: %s, expect: %s", i, actual.lbHealthCheckConfig, tt.lbHealthCheckConfig)
		}
	}
}

func TestInitMultiLBCache(t *testing.T) {
	tests := []struct {
		name        string
		svcList     []corev1.Service
		minPort     int32
		maxPort     int32
		blockPorts  []int32
		podAllocate map[string]*lbsPorts
		cache       [][]bool
	}{
		{
			name: "skips out-of-range service ports",
			svcList: []corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod-0-pool-a",
						Namespace: "default",
						Annotations: map[string]string{
							LBIDBelongIndexKey: "0",
							ElbIdAnnotationKey: "elb-1",
						},
					},
					Spec: corev1.ServiceSpec{
						Selector: map[string]string{
							SvcSelectorKey: "test-pod-0",
						},
						Ports: []corev1.ServicePort{
							{
								Port:       6001,
								Protocol:   corev1.ProtocolTCP,
								TargetPort: intstr.FromInt(80),
							},
							{
								Port:       7001,
								Protocol:   corev1.ProtocolUDP,
								TargetPort: intstr.FromInt(81),
							},
						},
					},
				},
			},
			minPort:    7000,
			maxPort:    7002,
			blockPorts: nil,
			podAllocate: map[string]*lbsPorts{
				"default/test-pod-0": {
					index:      0,
					lbIds:      []string{"elb-1"},
					lbNames:    []string{"pool-a"},
					ports:      []int32{7001},
					targetPort: []int{81},
					protocols:  []corev1.Protocol{corev1.ProtocolUDP},
				},
			},
			cache: [][]bool{{false, true, false}},
		},
		{
			name: "merges TCP and UDP service ports",
			svcList: []corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod-0-pool-a",
						Namespace: "default",
						Annotations: map[string]string{
							LBIDBelongIndexKey: "0",
							ElbIdAnnotationKey: "elb-1",
						},
					},
					Spec: corev1.ServiceSpec{
						Selector: map[string]string{
							SvcSelectorKey: "test-pod-0",
						},
						Ports: []corev1.ServicePort{
							{
								Port:       6076,
								Protocol:   corev1.ProtocolTCP,
								TargetPort: intstr.FromInt(8601),
							},
							{
								Port:       6076,
								Protocol:   corev1.ProtocolUDP,
								TargetPort: intstr.FromInt(8601),
							},
							{
								Port:       6077,
								Protocol:   corev1.ProtocolTCP,
								TargetPort: intstr.FromInt(8661),
							},
							{
								Port:       6077,
								Protocol:   corev1.ProtocolUDP,
								TargetPort: intstr.FromInt(8661),
							},
						},
					},
				},
			},
			minPort:    6076,
			maxPort:    6077,
			blockPorts: nil,
			podAllocate: map[string]*lbsPorts{
				"default/test-pod-0": {
					index:      0,
					lbIds:      []string{"elb-1"},
					lbNames:    []string{"pool-a"},
					ports:      []int32{6076, 6077},
					targetPort: []int{8601, 8661},
					protocols:  []corev1.Protocol{ProtocolTCPUDP, ProtocolTCPUDP},
				},
			},
			cache: [][]bool{{true, true}},
		},
		{
			name: "skips out-of-range block ports",
			svcList: []corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod-0-pool-a",
						Namespace: "default",
						Annotations: map[string]string{
							LBIDBelongIndexKey: "0",
							ElbIdAnnotationKey: "elb-1",
						},
					},
					Spec: corev1.ServiceSpec{
						Selector: map[string]string{
							SvcSelectorKey: "test-pod-0",
						},
						Ports: []corev1.ServicePort{
							{
								Port:       7003,
								Protocol:   corev1.ProtocolTCP,
								TargetPort: intstr.FromInt(80),
							},
						},
					},
				},
			},
			minPort:    7000,
			maxPort:    7003,
			blockPorts: []int32{6999, 7001, 14001},
			podAllocate: map[string]*lbsPorts{
				"default/test-pod-0": {
					index:      0,
					lbIds:      []string{"elb-1"},
					lbNames:    []string{"pool-a"},
					ports:      []int32{7003},
					targetPort: []int{80},
					protocols:  []corev1.Protocol{corev1.ProtocolTCP},
				},
			},
			cache: [][]bool{{false, true, false, true}},
		},
		{
			name: "uses min max parameter order",
			svcList: []corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod-0-pool-a",
						Namespace: "default",
						Annotations: map[string]string{
							LBIDBelongIndexKey: "0",
							ElbIdAnnotationKey: "elb-1",
						},
					},
					Spec: corev1.ServiceSpec{
						Selector: map[string]string{
							SvcSelectorKey: "test-pod-0",
						},
						Ports: []corev1.ServicePort{
							{
								Port:       7002,
								Protocol:   corev1.ProtocolTCP,
								TargetPort: intstr.FromInt(80),
							},
						},
					},
				},
			},
			minPort:    7000,
			maxPort:    7002,
			blockPorts: []int32{7001},
			podAllocate: map[string]*lbsPorts{
				"default/test-pod-0": {
					index:      0,
					lbIds:      []string{"elb-1"},
					lbNames:    []string{"pool-a"},
					ports:      []int32{7002},
					targetPort: []int{80},
					protocols:  []corev1.Protocol{corev1.ProtocolTCP},
				},
			},
			cache: [][]bool{{false, true, true}},
		},
		{
			name: "recovers lb name from service name",
			svcList: []corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod-0-pool-a",
						Namespace: "default",
						Annotations: map[string]string{
							LBIDBelongIndexKey: "0",
							ElbIdAnnotationKey: "elb-1",
						},
					},
					Spec: corev1.ServiceSpec{
						Selector: map[string]string{SvcSelectorKey: "test-pod-0"},
						Ports: []corev1.ServicePort{
							{
								Port:       6000,
								TargetPort: intstr.FromInt(80),
								Protocol:   corev1.ProtocolTCP,
							},
						},
					},
				},
			},
			minPort:    6000,
			maxPort:    6000,
			blockPorts: nil,
			podAllocate: map[string]*lbsPorts{
				"default/test-pod-0": {
					index:      0,
					lbIds:      []string{"elb-1"},
					lbNames:    []string{"pool-a"},
					ports:      []int32{6000},
					targetPort: []int{80},
					protocols:  []corev1.Protocol{corev1.ProtocolTCP},
				},
			},
			cache: [][]bool{{true}},
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var podAllocate map[string]*lbsPorts
			var cache [][]bool
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("case %d: initMultiLBCache panicked: %v", i, r)
					}
				}()
				podAllocate, cache = initMultiLBCache(tt.svcList, tt.minPort, tt.maxPort, tt.blockPorts)
			}()

			if !reflect.DeepEqual(podAllocate, tt.podAllocate) {
				t.Fatalf("case %d: podAllocate actual: %v, expect: %v", i, podAllocate, tt.podAllocate)
			}
			if !reflect.DeepEqual(cache, tt.cache) {
				t.Fatalf("case %d: cache actual: %v, expect: %v", i, cache, tt.cache)
			}
		})
	}
}

// When ElbIdNames shrinks, already-allocated pods must keep their snapshot
// allocation and must not migrate to another index.
func TestMultiElbsAllocateKeepsSnapshotForShrunkIdListForAllocatedPod(t *testing.T) {
	level := func() []bool { return make([]bool, 1000) } // ports [6000,6999]
	plugin := &MultiElbsPlugin{
		minPort: 6000,
		maxPort: 6999,
		cache: [][]bool{
			level(), // index 0
			level(), // index 1
			level(), // index 2 (removed from config but pod still here)
		},
		podAllocate: map[string]*lbsPorts{
			"default/test-pod-b": {
				index:      2,
				lbIds:      []string{"elb-x"},
				lbNames:    []string{"pool-x"},
				ports:      []int32{6076},
				targetPort: []int{80},
				protocols:  []corev1.Protocol{corev1.ProtocolTCP},
			},
		},
	}
	// mark pod-b's port as taken on its (still-valid) level 2
	plugin.cache[2][6076-6000] = true

	// conf shrank to a single ELB group
	conf := &multiELBsConfig{
		lbNames:        map[string]string{"elb-1": "pool-a"},
		idList:         [][]string{{"elb-1"}},
		targetPorts:    []int{80},
		protocols:      []corev1.Protocol{corev1.ProtocolTCP},
		allocatePolicy: "default",
	}

	// 1) new pod reconciles first -> fresh allocate. The cache is NOT
	//    truncated (it only grows), so level 2 survives; pod-c lands on index 0.
	if _, err := plugin.allocate(conf, "default/test-pod-c"); err != nil {
		t.Fatalf("allocate for new pod c returned error: %v", err)
	}
	if len(plugin.cache) != 3 {
		t.Fatalf("expected cache to keep all 3 levels (no truncation), got %d", len(plugin.cache))
	}

	// 2) stale high-index pod reconciles -> must not panic, must not migrate
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("allocate panicked on shrunk-config pod b: %v", r)
			}
		}()
		got, err := plugin.allocate(conf, "default/test-pod-b")
		if err != nil {
			t.Fatalf("expected allocate to keep stale pod snapshot, got: %v", err)
		}
		if got.index != 2 || got.ports[0] != 6076 || got.lbIds[0] != "elb-x" || got.lbNames[0] != "pool-x" {
			t.Fatalf("expected stale pod snapshot unchanged, got %+v", got)
		}
	}()

	allocated := plugin.podAllocate["default/test-pod-b"]
	if allocated == nil || allocated.index != 2 {
		t.Fatalf("expected stale pod to stay on index 2, got %+v", allocated)
	}
	if !plugin.cache[2][6076-6000] {
		t.Fatalf("expected pod-b's old port 6076 to stay marked on level 2")
	}
}

// Regression (bug 1): shrink then delete a stale high-index pod that never got
// re-updated. Previously a fresh allocate for another pod truncated m.cache,
// and deAllocate then panicked indexing the stale level. With no-truncation the
// level survives and deAllocate frees it cleanly.
func TestMultiElbsDeAllocateShrunkConfigDoesNotPanic(t *testing.T) {
	level := func() []bool { return make([]bool, 1000) } // ports [6000,6999]
	plugin := &MultiElbsPlugin{
		minPort: 6000,
		maxPort: 6999,
		cache: [][]bool{
			level(), // index 0
			level(), // index 1
			level(), // index 2
		},
		podAllocate: map[string]*lbsPorts{
			"default/test-pod-a": {
				index:      2,
				lbIds:      []string{"elb-x"},
				lbNames:    []string{"pool-x"},
				ports:      []int32{6076},
				targetPort: []int{80},
				protocols:  []corev1.Protocol{corev1.ProtocolTCP},
			},
		},
	}
	plugin.cache[2][6076-6000] = true

	// conf shrank to a single ELB group
	shrunkConf := &multiELBsConfig{
		lbNames:        map[string]string{"elb-1": "pool-a"},
		idList:         [][]string{{"elb-1"}},
		targetPorts:    []int{80},
		protocols:      []corev1.Protocol{corev1.ProtocolTCP},
		allocatePolicy: "default",
	}

	// 1) new pod reconciles first -> fresh allocate (cache stays 3 levels)
	if _, err := plugin.allocate(shrunkConf, "default/test-pod-c"); err != nil {
		t.Fatalf("allocate for new pod c returned error: %v", err)
	}
	if len(plugin.cache) != 3 {
		t.Fatalf("expected cache to keep 3 levels, got %d", len(plugin.cache))
	}

	// 2) stale high-index pod is DELETED (never re-updated) -> deAllocate
	//    must not panic, must free its port on the surviving level 2.
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("deAllocate panicked on stale high-index pod: %v", r)
			}
		}()
		plugin.deAllocate("default/test-pod-a")
	}()

	if plugin.podAllocate["default/test-pod-a"] != nil {
		t.Fatalf("expected stale pod removed from podAllocate")
	}
	if plugin.cache[2][6076-6000] {
		t.Fatalf("expected port 6076 freed on level 2 after deAllocate")
	}
}

// Regression (bug 3): shrink -> IDENTICAL regrow must not let a later pod steal
// a stale pod's external port on the same ELB. Previously the refresh branch left
// the cache desynced (level 2 recreated empty), so a later pod was handed the
// same port+index as the stale pod. With no-truncation the cache keeps the
// stale pod's port marked, so the later pod fails with "no available ports"
// instead of double-assigning.
func TestMultiElbsShrinkIdenticalRegrowNoPortConflict(t *testing.T) {
	one := func() []bool { return []bool{false} } // single slot: port 6000
	plugin := &MultiElbsPlugin{
		minPort: 6000,
		maxPort: 6000,
		cache: [][]bool{
			one(), // idx 0
			one(), // idx 1
			one(), // idx 2
		},
		podAllocate: map[string]*lbsPorts{
			"default/pod-a": {
				index:      2,
				lbIds:      []string{"elb-2"},
				lbNames:    []string{"c"},
				ports:      []int32{6000},
				targetPort: []int{80},
				protocols:  []corev1.Protocol{corev1.ProtocolTCP},
			},
		},
	}
	plugin.cache[2][0] = true // pod-a holds 6000 on idx 2

	originalConf := &multiELBsConfig{
		lbNames:        map[string]string{"elb-0": "a", "elb-1": "b", "elb-2": "c"},
		idList:         [][]string{{"elb-0"}, {"elb-1"}, {"elb-2"}},
		targetPorts:    []int{80},
		protocols:      []corev1.Protocol{corev1.ProtocolTCP},
		allocatePolicy: "default",
	}
	shrunkConf := &multiELBsConfig{
		lbNames:        map[string]string{"elb-0": "a"},
		idList:         [][]string{{"elb-0"}},
		targetPorts:    []int{80},
		protocols:      []corev1.Protocol{corev1.ProtocolTCP},
		allocatePolicy: "default",
	}

	// 1) shrink to 1 group; pod-c fresh-allocates 6000 on idx 0. Cache stays 3 levels.
	if _, err := plugin.allocate(shrunkConf, "default/pod-c"); err != nil {
		t.Fatalf("pod-c allocate: %v", err)
	}
	if len(plugin.cache) != 3 {
		t.Fatalf("expected cache to keep 3 levels, got %d", len(plugin.cache))
	}

	// 2) identical regrow; pod-a refreshes and keeps idx 2 / 6000.
	a, err := plugin.allocate(originalConf, "default/pod-a")
	if err != nil {
		t.Fatalf("pod-a re-allocate: %v", err)
	}
	if a.index != 2 || a.ports[0] != 6000 {
		t.Fatalf("pod-a should still hold idx 2 / 6000, got idx=%d ports=%v", a.index, a.ports)
	}
	if !plugin.cache[2][0] {
		t.Fatalf("pod-a's port 6000 on level 2 must stay marked taken")
	}

	// 3) another pod fills idx 1 (idx 0 is full).
	if _, err := plugin.allocate(originalConf, "default/pod-d1"); err != nil {
		t.Fatalf("pod-d1 allocate: %v", err)
	}

	// 4) a 4th pod cannot steal pod-a's idx 2 / 6000: the cache still marks it
	//    taken, so the allocate must fail rather than double-assign.
	if _, err := plugin.allocate(originalConf, "default/pod-d2"); err == nil {
		t.Fatalf("expected 4th allocate to fail (no free port) instead of double-assigning pod-a's port")
	}
	// pod-a is untouched.
	if got := plugin.podAllocate["default/pod-a"]; got == nil || got.index != 2 || got.ports[0] != 6000 {
		t.Fatalf("pod-a allocation must be unchanged, got %+v", got)
	}
}

// Regression (scan bound): when ElbIdNames shrank but stale cache levels remain,
// a fresh allocate must scan only [0, len(conf.idList)) and report "no available
// ports" when the valid levels are full -- not pick a stale high level and fail
// with the misleading "ElbIdNames configuration have not synced".
func TestMultiElbsAllocateShrunkScanBoundedByConfig(t *testing.T) {
	plugin := &MultiElbsPlugin{
		minPort: 6000,
		maxPort: 6000,
		cache: [][]bool{
			{true},  // idx 0: full (port 6000 taken)
			{false}, // idx 1: stale, empty (left over from a prior shrink)
			{false}, // idx 2: stale, empty
		},
		podAllocate: map[string]*lbsPorts{},
	}
	// conf shrank to a single ELB group; idx 0 is saturated.
	conf := &multiELBsConfig{
		lbNames:        map[string]string{"elb-1": "pool-a"},
		idList:         [][]string{{"elb-1"}},
		targetPorts:    []int{80},
		protocols:      []corev1.Protocol{corev1.ProtocolTCP},
		allocatePolicy: "default",
	}

	_, err := plugin.allocate(conf, "default/test-pod-x")
	if err == nil {
		t.Fatalf("expected allocate to fail because idx 0 is full, got nil error")
	}
	if !strings.Contains(err.Error(), "no available ports") {
		t.Fatalf("expected 'no available ports' error (scan bounded by conf), got: %v", err)
	}
	// stale levels must not be touched/marked.
	if plugin.cache[1][0] || plugin.cache[2][0] {
		t.Fatalf("stale cache levels must not be marked by a bounded scan")
	}
	if len(plugin.podAllocate) != 0 {
		t.Fatalf("no pod should be recorded on a failed allocate, got %v", plugin.podAllocate)
	}
}

func TestPreserveHealthCheckAnnotationCarriesOverPrevious(t *testing.T) {
	// frozen targetPort => consSvc produces no health-check annotation; the
	// previous one must be carried over so the ELB keeps its health check.
	prev := `[{"protocol":"tcp","target_service_port":"TCP:6000","monitor_port":"8080"}]`
	conf := &multiELBsConfig{
		lbHealthCheckFlag:   "on",
		lbHealthCheckConfig: `[{"protocol":"tcp","pod_target_port":"TCP:81","monitor_port":"8080"}]`,
	}

	svc := &corev1.Service{}
	preserveHealthCheckAnnotation(svc, map[string]string{ElbHealthCheckOptionsAnnotationKey: prev}, conf)
	if got := svc.Annotations[ElbHealthCheckOptionsAnnotationKey]; got != prev {
		t.Fatalf("expected previous health-check annotation carried over, got %q", got)
	}

	// flag=off intentionally drops the annotation -> must NOT carry over.
	conf.lbHealthCheckFlag = "off"
	svc2 := &corev1.Service{}
	preserveHealthCheckAnnotation(svc2, map[string]string{ElbHealthCheckOptionsAnnotationKey: prev}, conf)
	if _, ok := svc2.Annotations[ElbHealthCheckOptionsAnnotationKey]; ok {
		t.Fatalf("expected no carry-over when health check flag is off")
	}

	// new svc already has a fresh annotation -> do not overwrite with the old one.
	conf.lbHealthCheckFlag = "on"
	fresh := `[{"protocol":"tcp","target_service_port":"TCP:6001"}]`
	svc3 := &corev1.Service{}
	svc3.Annotations = map[string]string{ElbHealthCheckOptionsAnnotationKey: fresh}
	preserveHealthCheckAnnotation(svc3, map[string]string{ElbHealthCheckOptionsAnnotationKey: prev}, conf)
	if got := svc3.Annotations[ElbHealthCheckOptionsAnnotationKey]; got != fresh {
		t.Fatalf("expected fresh annotation preserved, got %q", got)
	}
}

func TestMultiElbsAllocateSkipsOutOfRangeBlockPortsOnCacheGrow(t *testing.T) {
	// block port 5000 is below minPort 6000; initMultiLBCache skips it, allocate's
	// cache-grow loop must skip it too — previously it indexed cacheLevel[-1000]
	// and panicked when conf.idList grew beyond the existing cache.
	plugin := &MultiElbsPlugin{
		minPort:     6000,
		maxPort:     6999,
		blockPorts:  []int32{5000, 6500},
		cache:       [][]bool{make([]bool, 1000)}, // index 0 already exists
		podAllocate: map[string]*lbsPorts{},
	}
	conf := &multiELBsConfig{
		lbNames:        map[string]string{"elb-a": "pool-a", "elb-b": "pool-b"},
		idList:         [][]string{{"elb-a"}, {"elb-b"}}, // 2 groups -> cache grows to 2 levels
		targetPorts:    []int{80},
		protocols:      []corev1.Protocol{corev1.ProtocolTCP},
		allocatePolicy: "default",
	}

	got, err := plugin.allocate(conf, "default/test-pod-0")
	if err != nil {
		t.Fatalf("allocate returned error: %v", err)
	}
	if len(plugin.cache) != 2 {
		t.Fatalf("expected cache to grow to 2 levels, got %d", len(plugin.cache))
	}
	// in-range block port 6500 marked on the freshly grown level 1; out-of-range
	// 5000 skipped (no panic, no mark at negative index).
	if !plugin.cache[1][500] { // 6500-6000=500
		t.Fatalf("expected in-range block port 6500 marked on level 1")
	}
	if got.ports[0] != 6000 {
		t.Fatalf("expected first free port 6000, got %v", got.ports)
	}
}

func TestMultiElbsOnPodUpdatedMarksNotReadyWhenNoResolvableBinding(t *testing.T) {
	// Pod snapshot holds a lbId removed from conf and an empty lbName (e.g. a
	// service recovered without the pool annotation, before the name-fallback in
	// initMultiLBCache applies). allocatedLbBindings skips it -> lbBindings is
	// empty -> OnPodUpdated must mark NotReady instead of falling through to
	// NetworkReady with no addresses.
	plugin := &MultiElbsPlugin{
		minPort: 6000,
		maxPort: 6999,
		cache:   [][]bool{make([]bool, 1000)},
		podAllocate: map[string]*lbsPorts{
			"default/test-pod-0": {
				index:      0,
				lbIds:      []string{"elb-gone"},
				lbNames:    []string{""},
				ports:      []int32{6000},
				targetPort: []int{80},
				protocols:  []corev1.Protocol{corev1.ProtocolTCP},
			},
		},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod-0",
			Namespace: "default",
			UID:       "pod-uid-1",
			Annotations: map[string]string{
				gamekruiseiov1alpha1.GameServerNetworkType:   MultiElbsNetwork,
				gamekruiseiov1alpha1.GameServerNetworkConf:   `[{"name":"ElbIdNames","value":"elb-1/pool-a"},{"name":"PortProtocols","value":"80/TCP"}]`,
				gamekruiseiov1alpha1.GameServerNetworkStatus: `{"currentNetworkState":"Ready"}`,
			},
		},
	}
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()

	updatedPod, err := plugin.OnPodUpdated(fakeClient, pod, context.Background())
	if err != nil {
		t.Fatalf("OnPodUpdated returned error: %v", err)
	}
	statusStr := updatedPod.Annotations[gamekruiseiov1alpha1.GameServerNetworkStatus]
	if !strings.Contains(statusStr, "NotReady") {
		t.Fatalf("expected NetworkNotReady when no binding is resolvable, got %s", statusStr)
	}
}

func TestMultiElbsOnPodAddedReadinessGateInjection(t *testing.T) {
	// Readiness gates are intended only for CCE Turbo passthrough scenarios
	// with dedicated (performance) ELBs, and the switch defaults to off.
	cases := []struct {
		name      string
		conf      string
		wantGates bool
	}{
		{
			name:      "switch on + performance -> inject",
			conf:      `[{"name":"ElbIdNames","value":"elb-1/pool-a,elb-2/pool-b"},{"name":"PortProtocols","value":"80/TCP"},{"name":"ElbClass","value":"performance"},{"name":"ReadinessGate","value":"true"}]`,
			wantGates: true,
		},
		{
			name:      "switch on + union -> skip",
			conf:      `[{"name":"ElbIdNames","value":"elb-1/pool-a,elb-2/pool-b"},{"name":"PortProtocols","value":"80/TCP"},{"name":"ElbClass","value":"union"},{"name":"ReadinessGate","value":"true"}]`,
			wantGates: false,
		},
		{
			name:      "switch off + performance -> skip",
			conf:      `[{"name":"ElbIdNames","value":"elb-1/pool-a,elb-2/pool-b"},{"name":"PortProtocols","value":"80/TCP"},{"name":"ElbClass","value":"performance"},{"name":"ReadinessGate","value":"false"}]`,
			wantGates: false,
		},
		{
			name:      "switch omitted (default off) + performance -> skip",
			conf:      `[{"name":"ElbIdNames","value":"elb-1/pool-a,elb-2/pool-b"},{"name":"PortProtocols","value":"80/TCP"},{"name":"ElbClass","value":"performance"}]`,
			wantGates: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plugin := &MultiElbsPlugin{}
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod-0",
					Namespace: "default",
					Annotations: map[string]string{
						gamekruiseiov1alpha1.GameServerNetworkType: MultiElbsNetwork,
						gamekruiseiov1alpha1.GameServerNetworkConf: tc.conf,
					},
				},
			}

			got, err := plugin.OnPodAdded(nil, pod, context.Background())
			if err != nil {
				t.Fatalf("OnPodAdded returned error: %v", err)
			}

			if !tc.wantGates {
				if len(got.Spec.ReadinessGates) != 0 {
					t.Fatalf("expected no readiness gates, got %v", got.Spec.ReadinessGates)
				}
				return
			}

			want := map[corev1.PodConditionType]struct{}{
				corev1.PodConditionType(PrefixReadyReadinessGate + "test-pod-0-pool-a"): {},
				corev1.PodConditionType(PrefixReadyReadinessGate + "test-pod-0-pool-b"): {},
			}
			if len(got.Spec.ReadinessGates) != len(want) {
				t.Fatalf("expected %d readiness gates, got %d: %v", len(want), len(got.Spec.ReadinessGates), got.Spec.ReadinessGates)
			}
			for _, rg := range got.Spec.ReadinessGates {
				if _, ok := want[rg.ConditionType]; !ok {
					t.Fatalf("unexpected readiness gate %q", rg.ConditionType)
				}
			}
		})
	}
}

func TestProcessHealthCheckOptionsSkipsMalformedKeepsValid(t *testing.T) {
	lps := &lbsPorts{
		ports:      []int32{6000},
		targetPort: []int{81},
		protocols:  []corev1.Protocol{corev1.ProtocolTCP},
	}
	tests := []struct {
		name string
		cfg  string
	}{
		{
			name: "malformed missing colon is skipped",
			cfg:  `[{"protocol":"tcp","pod_target_port":"TCP:81","monitor_port":"8080"},{"protocol":"tcp","pod_target_port":"badformat","monitor_port":"9090"}]`,
		},
		{
			name: "non-numeric port is skipped",
			cfg:  `[{"protocol":"tcp","pod_target_port":"TCP:8o","monitor_port":"9090"},{"protocol":"tcp","pod_target_port":"TCP:81","monitor_port":"8080"}]`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := processHealthCheckOptions(tt.cfg, lps)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			var options []HealthCheckOption
			if err := json.Unmarshal([]byte(got), &options); err != nil {
				t.Fatalf("failed to unmarshal result %q: %v", got, err)
			}
			if len(options) != 1 {
				t.Fatalf("expected 1 valid option to survive, got %d (result=%s)", len(options), got)
			}
			if options[0].TargetServicePort != "TCP:6000" {
				t.Fatalf("expected target_service_port TCP:6000, got %s", options[0].TargetServicePort)
			}
		})
	}
}
