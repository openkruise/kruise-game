package hwcloud

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/openkruise/kruise-game/apis/v1alpha1"
	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/cloudprovider/alibabacloud/apis/v1beta1"
)

func TestEipPlugin_Init(t *testing.T) {
	plugin := EipPlugin{}
	assert.Equal(t, EIPNetwork, plugin.Name())
	assert.Equal(t, AliasSEIP, plugin.Alias())
	err := plugin.Init(nil, nil, context.Background())
	assert.NoError(t, err)
}

func TestEipPlugin_OnPodAdded_UseExistingEIP(t *testing.T) {
	// create test pod
	var networkConf []v1alpha1.NetworkConfParams
	networkConf = append(networkConf, v1alpha1.NetworkConfParams{
		Name:  "yangtse.io/eip-id",
		Value: "huawei-eip-12345",
	})
	jsonStr, _ := json.Marshal(networkConf)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			Annotations: map[string]string{
				v1alpha1.GameServerNetworkType: EIPNetwork,
				v1alpha1.GameServerNetworkConf: string(jsonStr),
			},
		},
	}

	// create fake client.
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	// execute the test
	plugin := EipPlugin{}
	updatedPod, err := plugin.OnPodAdded(fakeClient, pod, context.Background())

	// check the result.
	assert.NoError(t, err)
	assert.Equal(t, EIPNetwork, updatedPod.Annotations[v1alpha1.GameServerNetworkType])
	unmarshalErr := json.Unmarshal([]byte(updatedPod.Annotations[v1alpha1.GameServerNetworkConf]), &networkConf)
	assert.NoError(t, unmarshalErr)
	assert.Equal(t, "huawei-eip-12345", networkConf[0].Value)
}

func addKvToParams(networkConf []v1alpha1.NetworkConfParams, keys []string, values []string) []v1alpha1.NetworkConfParams {
	for i := 0; i < len(keys); i++ {
		networkConf = append(networkConf, v1alpha1.NetworkConfParams{
			Name:  keys[i],
			Value: values[i],
		})
	}
	return networkConf
}
func TestEipPlugin_OnPodAdded_NewEIP(t *testing.T) {
	var networkConf []v1alpha1.NetworkConfParams
	networkConf = addKvToParams(networkConf,
		[]string{
			"name",
			"yangtse.io/pod-with-eip",
			"yangtse.io/eip-bandwidth-size",
			"yangtse.io/eip-network-type",
			"yangtse.io/eip-charge-mode",
		},
		[]string{
			"huawei-eip-demo",
			"true",
			"5",
			"5-bgp",
			"traffic",
		},
	)
	jsonStr, _ := json.Marshal(networkConf)
	// create test Pod and add related annotations.
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			Annotations: map[string]string{
				v1alpha1.GameServerNetworkType: EIPNetwork,
				v1alpha1.GameServerNetworkConf: string(jsonStr),
			},
		},
	}

	// create fake client.
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	// execute the test.
	plugin := EipPlugin{}
	updatedPod, err := plugin.OnPodAdded(fakeClient, pod, context.Background())

	// check the result.
	assert.NoError(t, err)
	assert.Equal(t, EIPNetwork, updatedPod.Annotations[v1alpha1.GameServerNetworkType])
	assert.Equal(t, "true", updatedPod.Annotations["yangtse.io/pod-with-eip"])
	assert.Equal(t, "5", updatedPod.Annotations["yangtse.io/eip-bandwidth-size"])
	assert.Equal(t, "5-bgp", updatedPod.Annotations["yangtse.io/eip-network-type"])
	assert.Equal(t, "traffic", updatedPod.Annotations["yangtse.io/eip-charge-mode"])
}

func TestEipPlugin_OnPodUpdated_WithNetworkStatus(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			Annotations: map[string]string{
				v1alpha1.GameServerNetworkType:   EIPNetwork,
				"cloud.kruise.io/network-status": `{"currentNetworkState":"Waiting"}`,
			},
		},
		Status: corev1.PodStatus{},
	}

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = v1beta1.AddToScheme(scheme)
	_ = gamekruiseiov1alpha1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(pod).
		Build()

	plugin := EipPlugin{}
	networkStatus := &v1alpha1.NetworkStatus{}
	networkStatus.ExternalAddresses = []v1alpha1.NetworkAddress{{IP: "203.0.113.1"}}
	networkStatus.InternalAddresses = []v1alpha1.NetworkAddress{{IP: "10.0.0.1"}}
	networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkReady
	networkStatusBytes, jErr := json.Marshal(networkStatus)
	assert.NoError(t, jErr)
	pod.Annotations[v1alpha1.GameServerNetworkStatus] = string(networkStatusBytes)
	updatedPod, err := plugin.OnPodUpdated(fakeClient, pod, context.Background())
	assert.NoError(t, err)

	jErr = json.Unmarshal([]byte(updatedPod.Annotations[v1alpha1.GameServerNetworkStatus]), &networkStatus)
	assert.NoError(t, jErr)

	assert.Contains(t, updatedPod.Annotations[v1alpha1.GameServerNetworkStatus], "Ready")
	assert.Contains(t, updatedPod.Annotations[v1alpha1.GameServerNetworkStatus], "203.0.113.1")
	assert.Contains(t, updatedPod.Annotations[v1alpha1.GameServerNetworkStatus], "10.0.0.1")
}

func TestEipPlugin_OnPodDeleted(t *testing.T) {
	plugin := EipPlugin{}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			Annotations: map[string]string{
				v1alpha1.GameServerNetworkType:   EIPNetwork,
				"cloud.kruise.io/network-status": `{"currentNetworkState":"Waiting"}`,
			},
		},
		Status: corev1.PodStatus{},
	}

	err := plugin.OnPodDeleted(nil, pod, context.Background())
	assert.Nil(t, err)
}
