package alibabacloud

import (
	"fmt"
	"reflect"
	"sync"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/pkg/util"
)

func TestSlpSpAllocate(t *testing.T) {
	tests := []struct {
		slbsp        *SlbSpPlugin
		pod          *corev1.Pod
		podNetConfig *lbSpConfig
		numBackends  map[string]int
		podSlbId     map[string]string
		expErr       error
	}{
		{
			slbsp: &SlbSpPlugin{
				numBackends: make(map[string]int),
				podSlbId:    make(map[string]string),
				mutex:       sync.RWMutex{},
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod-name",
					Namespace: "pod-ns",
					Labels: map[string]string{
						"xxx": "xxx",
					},
				},
			},
			podNetConfig: &lbSpConfig{
				lbIds:     []string{"lb-xxa"},
				ports:     []int{80},
				protocols: []corev1.Protocol{corev1.ProtocolTCP},
			},
			numBackends: map[string]int{"lb-xxa": 1},
			podSlbId:    map[string]string{"pod-ns/pod-name": "lb-xxa"},
			expErr:      nil,
		},

		{
			slbsp: &SlbSpPlugin{
				numBackends: map[string]int{"lb-xxa": 200},
				podSlbId:    map[string]string{"a-ns/a-name": "lb-xxa"},
				mutex:       sync.RWMutex{},
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod-name",
					Namespace: "pod-ns",
					Labels: map[string]string{
						"xxx": "xxx",
					},
				},
			},
			podNetConfig: &lbSpConfig{
				lbIds:     []string{"lb-xxa"},
				ports:     []int{80},
				protocols: []corev1.Protocol{corev1.ProtocolTCP},
			},
			numBackends: map[string]int{"lb-xxa": 200},
			podSlbId:    map[string]string{"a-ns/a-name": "lb-xxa"},
			expErr:      fmt.Errorf(ErrorUpperLimit),
		},
	}

	for _, test := range tests {
		slbId, err := test.slbsp.getOrAllocate(test.podNetConfig, test.pod)
		if (err == nil) != (test.expErr == nil) {
			t.Errorf("expect err: %v, but acutal err: %v", test.expErr, err)
		}

		if test.pod.GetLabels()[SlbIdLabelKey] != slbId {
			t.Errorf("expect pod have slblabel value: %s, but actual value: %s", slbId, test.pod.GetLabels()[SlbIdLabelKey])
		}

		if !reflect.DeepEqual(test.numBackends, test.slbsp.numBackends) {
			t.Errorf("expect numBackends: %v, but actual: %v", test.numBackends, test.slbsp.numBackends)
		}

		if !reflect.DeepEqual(test.podSlbId, test.slbsp.podSlbId) {
			t.Errorf("expect numBackends: %v, but actual: %v", test.podSlbId, test.slbsp.podSlbId)
		}
	}
}

func TestParseLbSpConfig(t *testing.T) {
	tests := []struct {
		conf         []gamekruiseiov1alpha1.NetworkConfParams
		podNetConfig *lbSpConfig
	}{
		{
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{
					Name:  PortProtocolsConfigName,
					Value: "80",
				},
				{
					Name:  SlbIdsConfigName,
					Value: "lb-xxa",
				},
				{
					Name:  ManagedServiceNamesConfigName,
					Value: "service-clusterIp",
				},
				{
					Name:  ManagedServiceSelectorConfigName,
					Value: "game=v1",
				},
			},
			podNetConfig: &lbSpConfig{
				lbIds:                       []string{"lb-xxa"},
				ports:                       []int{80},
				protocols:                   []corev1.Protocol{corev1.ProtocolTCP},
				managedServiceNames:         []string{"service-clusterIp"},
				managedServiceSelectorKey:   "game",
				managedServiceSelectorValue: "v1",
			},
		},
	}

	for _, test := range tests {
		podNetConfig := parseLbSpConfig(test.conf)
		if !reflect.DeepEqual(podNetConfig, test.podNetConfig) {
			t.Errorf("expect podNetConfig: %v, but actual: %v", test.podNetConfig, podNetConfig)
		}
	}
}

func TestParsePortProtocols(t *testing.T) {
	tests := []struct {
		value     string
		ports     []int
		protocols []corev1.Protocol
	}{
		{
			value:     "80",
			ports:     []int{80},
			protocols: []corev1.Protocol{corev1.ProtocolTCP},
		},
		{
			value:     "8080/UDP,80/TCP",
			ports:     []int{8080, 80},
			protocols: []corev1.Protocol{corev1.ProtocolUDP, corev1.ProtocolTCP},
		},
	}

	for i, test := range tests {
		actualPorts, actualProtocols := parsePortProtocols(test.value)
		if !util.IsSliceEqual(actualPorts, test.ports) {
			t.Errorf("case %d: expect ports is %v, but actually is %v", i, test.ports, actualPorts)
		}
		if !reflect.DeepEqual(actualProtocols, test.protocols) {
			t.Errorf("case %d: expect protocols is %v, but actually is %v", i, test.protocols, actualProtocols)
		}
	}
}
