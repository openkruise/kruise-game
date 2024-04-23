package kubernetes

import (
	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/pkg/util"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
	"reflect"
	"testing"
)

func TestParseNPConfig(t *testing.T) {
	tests := []struct {
		conf         []gamekruiseiov1alpha1.NetworkConfParams
		podNetConfig *nodePortConfig
	}{
		{
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{
					Name:  PortProtocolsConfigName,
					Value: "80",
				},
			},
			podNetConfig: &nodePortConfig{
				ports:     []int{80},
				protocols: []corev1.Protocol{corev1.ProtocolTCP},
			},
		},

		{
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{
					Name:  PortProtocolsConfigName,
					Value: "8021/UDP",
				},
			},
			podNetConfig: &nodePortConfig{
				ports:     []int{8021},
				protocols: []corev1.Protocol{corev1.ProtocolUDP},
			},
		},
	}

	for _, test := range tests {
		podNetConfig, _ := parseNodePortConfig(test.conf)
		if !reflect.DeepEqual(podNetConfig, test.podNetConfig) {
			t.Errorf("expect podNetConfig: %v, but actual: %v", test.podNetConfig, podNetConfig)
		}
	}
}

func TestConsNPSvc(t *testing.T) {
	pod := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Pod",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-3",
			Namespace: "ns",
			UID:       "bff0afd6-bb30-4641-8607-8329547324eb",
		},
	}

	// case 0
	npcCase0 := &nodePortConfig{
		ports: []int{
			80,
			8080,
		},
		protocols: []corev1.Protocol{
			corev1.ProtocolTCP,
			corev1.ProtocolTCP,
		},
		isFixed: false,
	}
	svcCase0 := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-3",
			Namespace: "ns",
			Annotations: map[string]string{
				ServiceHashKey: util.GetHash(npcCase0),
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         "v1",
					Kind:               "Pod",
					Name:               "pod-3",
					UID:                "bff0afd6-bb30-4641-8607-8329547324eb",
					Controller:         pointer.BoolPtr(true),
					BlockOwnerDeletion: pointer.BoolPtr(true),
				},
			},
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeNodePort,
			Selector: map[string]string{
				SvcSelectorKey: "pod-3",
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "80",
					Port:       int32(80),
					TargetPort: intstr.FromInt(80),
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "8080",
					Port:       int32(8080),
					TargetPort: intstr.FromInt(8080),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	// case 1
	npcCase1 := &nodePortConfig{
		ports: []int{
			8021,
		},
		protocols: []corev1.Protocol{
			corev1.ProtocolUDP,
		},
		isFixed: false,
	}
	svcCase1 := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-3",
			Namespace: "ns",
			Annotations: map[string]string{
				ServiceHashKey: util.GetHash(npcCase1),
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         "v1",
					Kind:               "Pod",
					Name:               "pod-3",
					UID:                "bff0afd6-bb30-4641-8607-8329547324eb",
					Controller:         pointer.BoolPtr(true),
					BlockOwnerDeletion: pointer.BoolPtr(true),
				},
			},
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeNodePort,
			Selector: map[string]string{
				SvcSelectorKey: "pod-3",
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "8021",
					Port:       int32(8021),
					TargetPort: intstr.FromInt(8021),
					Protocol:   corev1.ProtocolUDP,
				},
			},
		},
	}

	tests := []struct {
		npc *nodePortConfig
		svc *corev1.Service
	}{
		{
			npc: npcCase0,
			svc: svcCase0,
		},
		{
			npc: npcCase1,
			svc: svcCase1,
		},
	}

	for i, test := range tests {
		actual := consNodePortSvc(test.npc, pod, nil, nil)
		if !reflect.DeepEqual(actual, test.svc) {
			t.Errorf("case %d: expect service: %v , but actual: %v", i, test.svc, actual)
		}
	}
}
