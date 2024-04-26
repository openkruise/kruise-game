package alibabacloud

import (
	"context"
	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/pkg/util"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sync"
	"testing"
)

func TestNLBAllocateDeAllocate(t *testing.T) {
	test := struct {
		lbIds  []string
		nlb    *NlbPlugin
		num    int
		podKey string
	}{
		lbIds: []string{"xxx-A"},
		nlb: &NlbPlugin{
			maxPort:     int32(712),
			minPort:     int32(512),
			cache:       make(map[string]portAllocated),
			podAllocate: make(map[string]string),
			mutex:       sync.RWMutex{},
		},
		podKey: "xxx/xxx",
		num:    3,
	}

	lbId, ports := test.nlb.allocate(test.lbIds, test.num, test.podKey)
	if _, exist := test.nlb.podAllocate[test.podKey]; !exist {
		t.Errorf("podAllocate[%s] is empty after allocated", test.podKey)
	}
	for _, port := range ports {
		if port > test.nlb.maxPort || port < test.nlb.minPort {
			t.Errorf("allocate port %d, unexpected", port)
		}
		if test.nlb.cache[lbId][port] == false {
			t.Errorf("Allocate port %d failed", port)
		}
	}
	test.nlb.deAllocate(test.podKey)
	for _, port := range ports {
		if test.nlb.cache[lbId][port] == true {
			t.Errorf("deAllocate port %d failed", port)
		}
	}
	if _, exist := test.nlb.podAllocate[test.podKey]; exist {
		t.Errorf("podAllocate[%s] is not empty after deallocated", test.podKey)
	}
}

func TestParseNlbConfig(t *testing.T) {
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
					Name:  NlbIdsConfigName,
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
					Name:  NlbIdsConfigName,
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
		sc := parseNlbConfig(test.conf)
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

func TestNlbPlugin_consSvc(t *testing.T) {
	loadBalancerClass := "alibabacloud.com/nlb"
	type fields struct {
		maxPort     int32
		minPort     int32
		cache       map[string]portAllocated
		podAllocate map[string]string
	}
	type args struct {
		config *nlbConfig
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
				config: &nlbConfig{
					lbIds:       []string{"clb-xxx"},
					targetPorts: []int{82},
					protocols: []corev1.Protocol{
						corev1.ProtocolTCP,
					},
					isFixed: false,
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
						SlbListenerOverrideKey: "true",
						SlbIdAnnotationKey:     "clb-xxx",
						SlbConfigHashKey: util.GetHash(&nlbConfig{
							lbIds:       []string{"clb-xxx"},
							targetPorts: []int{82},
							protocols: []corev1.Protocol{
								corev1.ProtocolTCP,
							},
							isFixed: false,
						}),
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         "v1",
							Kind:               "pod",
							Name:               "test-pod",
							UID:                "32fqwfqfew",
							Controller:         pointer.BoolPtr(true),
							BlockOwnerDeletion: pointer.BoolPtr(true),
						},
					},
				},
				Spec: corev1.ServiceSpec{
					Type:                  corev1.ServiceTypeLoadBalancer,
					ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyTypeLocal,
					LoadBalancerClass:     &loadBalancerClass,
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
				},
			},
		},
	}
	for _, tt := range tests {
		c := &NlbPlugin{
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
