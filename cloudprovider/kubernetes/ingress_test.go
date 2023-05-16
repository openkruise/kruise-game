package kubernetes

import (
	"fmt"
	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/pkg/util"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
	"reflect"
	"testing"
)

func TestParseIngConfig(t *testing.T) {
	pathTypePrefix := v1.PathTypePrefix
	tests := []struct {
		conf []gamekruiseiov1alpha1.NetworkConfParams
		pod  *corev1.Pod
		ic   ingConfig
		err  error
	}{
		{
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{
					Name:  PathKey,
					Value: "/game<id>(/|$)(.*)",
				},
				{
					Name:  AnnotationKey,
					Value: "nginx.ingress.kubernetes.io/rewrite-target: /$2",
				},
				{
					Name:  AnnotationKey,
					Value: "alb.ingress.kubernetes.io/server-snippets: |\n      proxy_set_header Upgrade $http_upgrade;\n      proxy_set_header Connection \"upgrade\";",
				},
				{
					Name:  TlsHostsKey,
					Value: "xxx-xxx.com,xxx-xx.com",
				},
				{
					Name:  PortKey,
					Value: "8080",
				},
				{
					Name:  PathTypeKey,
					Value: string(v1.PathTypePrefix),
				},
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "pod-3",
				},
			},
			ic: ingConfig{
				paths: []string{"/game3(/|$)(.*)"},
				tlsHosts: []string{
					"xxx-xxx.com",
					"xxx-xx.com",
				},
				annotations: map[string]string{
					"nginx.ingress.kubernetes.io/rewrite-target": "/$2",
					"alb.ingress.kubernetes.io/server-snippets":  "|\n      proxy_set_header Upgrade $http_upgrade;\n      proxy_set_header Connection \"upgrade\";",
				},
				ports:     []int32{8080},
				pathTypes: []*v1.PathType{&pathTypePrefix},
			},
		},
		{
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{
					Name:  PathKey,
					Value: "/game<id>",
				},
				{
					Name:  AnnotationKey,
					Value: "nginx.ingress.kubernetes.io/rewrite-target: /$2",
				},
				{
					Name:  TlsHostsKey,
					Value: "xxx-xxx.com,xxx-xx.com",
				},
				{
					Name:  PathTypeKey,
					Value: string(v1.PathTypePrefix),
				},
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "pod-3",
				},
			},
			err: fmt.Errorf("%s", paramsError),
		},
		{
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{
					Name:  PathKey,
					Value: "/game",
				},
				{
					Name:  PortKey,
					Value: "8080",
				},
				{
					Name:  PathTypeKey,
					Value: string(v1.PathTypePrefix),
				},
				{
					Name:  HostKey,
					Value: "instance<id>.xxx.xxx.com",
				},
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "pod-2",
				},
			},
			ic: ingConfig{
				paths:       []string{"/game"},
				ports:       []int32{8080},
				pathTypes:   []*v1.PathType{&pathTypePrefix},
				host:        "instance2.xxx.xxx.com",
				annotations: map[string]string{},
			},
		},
	}

	for i, test := range tests {
		expect := test.ic
		actual, err := parseIngConfig(test.conf, test.pod)
		if !reflect.DeepEqual(err, test.err) {
			t.Errorf("case %d: expect err: %v , but actual: %v", i, test.err, err)
		}
		if !reflect.DeepEqual(actual, expect) {
			if !reflect.DeepEqual(expect.paths, actual.paths) {
				t.Errorf("case %d: expect paths: %v , but actual: %v", i, expect.paths, actual.paths)
			}
			if !reflect.DeepEqual(expect.ports, actual.ports) {
				t.Errorf("case %d: expect ports: %v , but actual: %v", i, expect.ports, actual.ports)
			}
			if !reflect.DeepEqual(expect.pathTypes, actual.pathTypes) {
				t.Errorf("case %d: expect annotations: %v , but actual: %v", i, expect.pathTypes, actual.pathTypes)
			}
			if !reflect.DeepEqual(expect.host, actual.host) {
				t.Errorf("case %d: expect host: %v , but actual: %v", i, expect.host, actual.host)
			}
			if !reflect.DeepEqual(expect.tlsHosts, actual.tlsHosts) {
				t.Errorf("case %d: expect tlsHosts: %v , but actual: %v", i, expect.tlsHosts, actual.tlsHosts)
			}
			if !reflect.DeepEqual(expect.tlsSecretName, actual.tlsSecretName) {
				t.Errorf("case %d: expect tlsSecretName: %v , but actual: %v", i, expect.tlsSecretName, actual.tlsSecretName)
			}
			if !reflect.DeepEqual(expect.ingressClassName, actual.ingressClassName) {
				t.Errorf("case %d: expect ingressClassName: %v , but actual: %v", i, expect.ingressClassName, actual.ingressClassName)
			}
			if !reflect.DeepEqual(expect.annotations, actual.annotations) {
				t.Errorf("case %d: expect annotations: %v , but actual: %v", i, expect.annotations, actual.annotations)
			}
		}
	}
}

func TestConsIngress(t *testing.T) {
	pathTypePrefix := v1.PathTypePrefix
	pathTypeImplementationSpecific := v1.PathTypeImplementationSpecific
	ingressClassNameNginx := "nginx"

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

	baseIngObjectMeta := metav1.ObjectMeta{
		Name:      "pod-3",
		Namespace: "ns",
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
	}

	// case 0
	icCase0 := ingConfig{
		ports: []int32{
			int32(80),
		},
		pathTypes: []*v1.PathType{
			&pathTypePrefix,
		},
		paths: []string{
			"/path1-3",
			"/path2-3",
		},
		host:             "xxx.xx.com",
		ingressClassName: &ingressClassNameNginx,
		annotations: map[string]string{
			"nginx.ingress.kubernetes.io/rewrite-target": "/$2",
		},
	}
	ingObjectMetaCase0 := baseIngObjectMeta
	ingObjectMetaCase0.Annotations = map[string]string{
		"nginx.ingress.kubernetes.io/rewrite-target": "/$2",
		IngressHashKey: util.GetHash(icCase0),
	}
	ingCase0 := &v1.Ingress{
		ObjectMeta: ingObjectMetaCase0,
		Spec: v1.IngressSpec{
			IngressClassName: &ingressClassNameNginx,
			Rules: []v1.IngressRule{
				{
					Host: "xxx.xx.com",
					IngressRuleValue: v1.IngressRuleValue{
						HTTP: &v1.HTTPIngressRuleValue{
							Paths: []v1.HTTPIngressPath{
								{
									Path:     "/path1-3",
									PathType: &pathTypePrefix,
									Backend: v1.IngressBackend{
										Service: &v1.IngressServiceBackend{
											Name: "pod-3",
											Port: v1.ServiceBackendPort{
												Number: int32(80),
											},
										},
									},
								},
								{
									Path:     "/path2-3",
									PathType: &pathTypePrefix,
									Backend: v1.IngressBackend{
										Service: &v1.IngressServiceBackend{
											Name: "pod-3",
											Port: v1.ServiceBackendPort{
												Number: int32(80),
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	// case 1
	icCase1 := ingConfig{
		ports: []int32{
			int32(80),
			int32(8080),
		},
		pathTypes: []*v1.PathType{
			&pathTypePrefix,
			&pathTypeImplementationSpecific,
		},
		paths: []string{
			"/path1-3",
			"/path2-3",
			"/path3-3",
		},
		host:             "xxx.xx.com",
		ingressClassName: &ingressClassNameNginx,
	}
	ingObjectMetaCase1 := baseIngObjectMeta
	ingObjectMetaCase1.Annotations = map[string]string{
		IngressHashKey: util.GetHash(icCase1),
	}
	ingCase1 := &v1.Ingress{
		ObjectMeta: ingObjectMetaCase1,
		Spec: v1.IngressSpec{
			IngressClassName: &ingressClassNameNginx,
			Rules: []v1.IngressRule{
				{
					Host: "xxx.xx.com",
					IngressRuleValue: v1.IngressRuleValue{
						HTTP: &v1.HTTPIngressRuleValue{
							Paths: []v1.HTTPIngressPath{
								{
									Path:     "/path1-3",
									PathType: &pathTypePrefix,
									Backend: v1.IngressBackend{
										Service: &v1.IngressServiceBackend{
											Name: "pod-3",
											Port: v1.ServiceBackendPort{
												Number: int32(80),
											},
										},
									},
								},
								{
									Path:     "/path2-3",
									PathType: &pathTypeImplementationSpecific,
									Backend: v1.IngressBackend{
										Service: &v1.IngressServiceBackend{
											Name: "pod-3",
											Port: v1.ServiceBackendPort{
												Number: int32(8080),
											},
										},
									},
								},
								{
									Path:     "/path3-3",
									PathType: &pathTypePrefix,
									Backend: v1.IngressBackend{
										Service: &v1.IngressServiceBackend{
											Name: "pod-3",
											Port: v1.ServiceBackendPort{
												Number: int32(80),
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	tests := []struct {
		ic  ingConfig
		ing *v1.Ingress
	}{
		{
			ic:  icCase0,
			ing: ingCase0,
		},
		{
			ic:  icCase1,
			ing: ingCase1,
		},
	}

	for i, test := range tests {
		actual := consIngress(test.ic, pod)
		if !reflect.DeepEqual(actual, test.ing) {
			t.Errorf("case %d: expect ingress: %v , but actual: %v", i, test.ing, actual)
		}
	}
}

func TestConsSvc(t *testing.T) {
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

	baseSvcObjectMeta := metav1.ObjectMeta{
		Name:      "pod-3",
		Namespace: "ns",
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
	}

	// case 0
	icCase0 := ingConfig{
		ports: []int32{
			int32(80),
			int32(8080),
		},
	}
	svcObjectMetaCase0 := baseSvcObjectMeta
	svcObjectMetaCase0.Annotations = map[string]string{
		ServiceHashKey: util.GetHash(icCase0.ports),
	}
	svcCase0 := &corev1.Service{
		ObjectMeta: svcObjectMetaCase0,
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Selector: map[string]string{
				SvcSelectorKey: "pod-3",
			},
			Ports: []corev1.ServicePort{
				{
					Name: "80",
					Port: int32(80),
				},
				{
					Name: "8080",
					Port: int32(8080),
				},
			},
		},
	}

	tests := []struct {
		ic  ingConfig
		svc *corev1.Service
	}{
		{
			ic:  icCase0,
			svc: svcCase0,
		},
	}

	for i, test := range tests {
		actual := consSvc(test.ic, pod)
		if !reflect.DeepEqual(actual, test.svc) {
			t.Errorf("case %d: expect service: %v , but actual: %v", i, test.svc, actual)
		}
	}
}
