package alibabacloud

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/pkg/util"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
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
			expErr:      errors.New(ErrorUpperLimit),
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

func TestSlbSpOnPodUpdatedReturnsManagedServiceGetError(t *testing.T) {
	const slbId = "lb-xxa"
	networkConf, err := json.Marshal([]gamekruiseiov1alpha1.NetworkConfParams{
		{Name: SlbIdsConfigName, Value: slbId},
		{Name: PortProtocolsConfigName, Value: "80/TCP"},
		{Name: ManagedServiceNamesConfigName, Value: "missing-service"},
		{Name: ManagedServiceSelectorConfigName, Value: "game=test"},
		{Name: gamekruiseiov1alpha1.AllowNotReadyContainersNetworkConfName, Value: "sidecar"},
	})
	if err != nil {
		t.Fatal(err)
	}
	networkStatus, err := json.Marshal(gamekruiseiov1alpha1.NetworkStatus{CurrentNetworkState: gamekruiseiov1alpha1.NetworkNotReady})
	if err != nil {
		t.Fatal(err)
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-0",
			Namespace: "default",
			Labels: map[string]string{
				SlbIdLabelKey: slbId,
				gamekruiseiov1alpha1.GameServerOwnerGssKey: "gss",
			},
			Annotations: map[string]string{
				gamekruiseiov1alpha1.GameServerNetworkType:   SlbSPNetwork,
				gamekruiseiov1alpha1.GameServerNetworkConf:   string(networkConf),
				gamekruiseiov1alpha1.GameServerNetworkStatus: string(networkStatus),
			},
		},
	}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: slbId, Namespace: "default"},
		Status: corev1.ServiceStatus{
			LoadBalancer: corev1.LoadBalancerStatus{Ingress: []corev1.LoadBalancerIngress{{IP: "203.0.113.1"}}},
		},
	}
	gss := &gamekruiseiov1alpha1.GameServerSet{
		ObjectMeta: metav1.ObjectMeta{Name: "gss", Namespace: "default"},
		Spec: gamekruiseiov1alpha1.GameServerSetSpec{
			Network: &gamekruiseiov1alpha1.Network{NetworkType: SlbSPNetwork},
		},
	}

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = gamekruiseiov1alpha1.AddToScheme(scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod, svc, gss).Build()

	plugin := &SlbSpPlugin{numBackends: map[string]int{}, podSlbId: map[string]string{}}
	// The managed service does not exist, so getting it fails.
	if _, pluginErr := plugin.OnPodUpdated(c, pod, context.Background()); pluginErr == nil {
		t.Fatal("expected an error because the managed service could not be fetched, got nil")
	}
}
