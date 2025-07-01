package alibabacloud

import (
	"context"
	"reflect"
	"sync"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/pkg/util"
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
		nlbConfig *nlbConfig
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
				{
					Name:  LBHealthCheckFlagConfigName,
					Value: "On",
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
			},
			nlbConfig: &nlbConfig{
				lbIds:       []string{"xxx-A"},
				targetPorts: []int{80},
				protocols:   []corev1.Protocol{corev1.ProtocolTCP},
				isFixed:     false,
				nlbHealthConfig: &nlbHealthConfig{
					lBHealthCheckFlag:           "on",
					lBHealthCheckType:           "http",
					lBHealthCheckConnectPort:    "6000",
					lBHealthCheckConnectTimeout: "100",
					lBHealthCheckInterval:       "30",
					lBHealthCheckUri:            "/another?valid",
					lBHealthCheckDomain:         "www.test.com",
					lBHealthCheckMethod:         "head",
					lBHealthyThreshold:          "5",
					lBUnhealthyThreshold:        "5",
				},
			},
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
			nlbConfig: &nlbConfig{
				lbIds:       []string{"xxx-A", "xxx-B"},
				targetPorts: []int{81, 82, 83},
				protocols:   []corev1.Protocol{corev1.ProtocolUDP, corev1.ProtocolTCP, corev1.ProtocolTCP},
				isFixed:     true,
				nlbHealthConfig: &nlbHealthConfig{
					lBHealthCheckFlag:           "on",
					lBHealthCheckType:           "tcp",
					lBHealthCheckConnectPort:    "0",
					lBHealthCheckConnectTimeout: "5",
					lBHealthCheckInterval:       "10",
					lBUnhealthyThreshold:        "2",
					lBHealthyThreshold:          "2",
					lBHealthCheckUri:            "",
					lBHealthCheckDomain:         "",
					lBHealthCheckMethod:         "",
				},
			},
		},
	}

	for i, test := range tests {
		sc, err := parseNlbConfig(test.conf)
		if err != nil {
			t.Error(err)
		}
		if !reflect.DeepEqual(test.nlbConfig, sc) {
			t.Errorf("case %d: lbId expect: %v, actual: %v", i, test.nlbConfig, sc)
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
					nlbHealthConfig: &nlbHealthConfig{
						lBHealthCheckFlag:           "on",
						lBHealthCheckType:           "tcp",
						lBHealthCheckConnectPort:    "0",
						lBHealthCheckConnectTimeout: "5",
						lBHealthCheckInterval:       "10",
						lBUnhealthyThreshold:        "2",
						lBHealthyThreshold:          "2",
						lBHealthCheckUri:            "",
						lBHealthCheckDomain:         "",
						lBHealthCheckMethod:         "",
					},
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
							nlbHealthConfig: &nlbHealthConfig{
								lBHealthCheckFlag:           "on",
								lBHealthCheckType:           "tcp",
								lBHealthCheckConnectPort:    "0",
								lBHealthCheckConnectTimeout: "5",
								lBHealthCheckInterval:       "10",
								lBUnhealthyThreshold:        "2",
								lBHealthyThreshold:          "2",
								lBHealthCheckUri:            "",
								lBHealthCheckDomain:         "",
								lBHealthCheckMethod:         "",
							},
						}),
						LBHealthCheckFlagAnnotationKey:           "on",
						LBHealthCheckTypeAnnotationKey:           "tcp",
						LBHealthCheckConnectPortAnnotationKey:    "0",
						LBHealthCheckConnectTimeoutAnnotationKey: "5",
						LBHealthCheckIntervalAnnotationKey:       "10",
						LBUnhealthyThresholdAnnotationKey:        "2",
						LBHealthyThresholdAnnotationKey:          "2",
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
		got, err := c.consSvc(tt.args.config, tt.args.pod, tt.args.client, tt.args.ctx)
		if err != nil {
			t.Error(err)
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("consSvc() = %v, want %v", got, tt.want)
		}
	}
}
