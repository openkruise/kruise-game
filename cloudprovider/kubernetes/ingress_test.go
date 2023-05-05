package kubernetes

import (
	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"reflect"
	"testing"
)

func TestParseIngConfig(t *testing.T) {
	tests := []struct {
		conf []gamekruiseiov1alpha1.NetworkConfParams
		pod  *corev1.Pod
		ic   ingConfig
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
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "pod-3",
				},
			},
			ic: ingConfig{
				path: "/game3(/|$)(.*)",
				tlsHosts: []string{
					"xxx-xxx.com",
					"xxx-xx.com",
				},
				annotations: map[string]string{
					"nginx.ingress.kubernetes.io/rewrite-target": "/$2",
					"alb.ingress.kubernetes.io/server-snippets":  "|\n      proxy_set_header Upgrade $http_upgrade;\n      proxy_set_header Connection \"upgrade\";",
				},
				port: 8080,
			},
		},
	}

	for _, test := range tests {
		actual := parseIngConfig(test.conf, test.pod)
		if !reflect.DeepEqual(actual, test.ic) {
			t.Errorf("expect ingConfig: %v , but actual: %v", test.ic, actual)
		}
	}
}
