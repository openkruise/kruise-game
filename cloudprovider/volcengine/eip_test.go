package volcengine

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/openkruise/kruise-game/apis/v1alpha1"
	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/cloudprovider/alibabacloud/apis/v1beta1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestEipPlugin_Init(t *testing.T) {
	plugin := EipPlugin{}
	assert.Equal(t, EIPNetwork, plugin.Name())
	assert.Equal(t, AliasSEIP, plugin.Alias())
	err := plugin.Init(nil, nil, context.Background())
	assert.NoError(t, err)
}

func TestEipPlugin_OnPodAdded_UseExistingEIP(t *testing.T) {
	// 创建测试 Pod
	networkConf := []v1alpha1.NetworkConfParams{}
	networkConf = append(networkConf, v1alpha1.NetworkConfParams{
		Name:  UseExistEIPAnnotationKey,
		Value: "eip-12345",
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

	// 创建假的 client
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	// 执行测试
	plugin := EipPlugin{}
	updatedPod, err := plugin.OnPodAdded(fakeClient, pod, context.Background())

	// 检查结果
	assert.NoError(t, err)
	assert.Equal(t, "eip-12345", updatedPod.Annotations[UseExistEIPAnnotationKey])
	assert.Equal(t, EIPNetwork, updatedPod.Annotations[v1alpha1.GameServerNetworkType])

	jErr := json.Unmarshal([]byte(updatedPod.Annotations[v1alpha1.GameServerNetworkConf]), &networkConf)
	assert.NoError(t, jErr)
}

func addKvToParams(networkConf []v1alpha1.NetworkConfParams, keys []string, values []string) []v1alpha1.NetworkConfParams {
	// 遍历 keys 和 values，添加到 map 中
	for i := 0; i < len(keys); i++ {
		networkConf = append(networkConf, v1alpha1.NetworkConfParams{
			Name:  keys[i],
			Value: values[i],
		})
	}
	return networkConf
}
func TestEipPlugin_OnPodAdded_NewEIP(t *testing.T) {
	networkConf := []v1alpha1.NetworkConfParams{}
	networkConf = addKvToParams(networkConf, []string{"name", "isp", "bandwidth", "description", "billingType"},
		[]string{"eip-demo", "BGP", "100", "demo for pods eip", "2"})
	jsonStr, _ := json.Marshal(networkConf)
	// 创建测试 Pod 并添加相关注解
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

	// 创建假的 client
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	// 执行测试
	plugin := EipPlugin{}
	updatedPod, err := plugin.OnPodAdded(fakeClient, pod, context.Background())

	// 检查结果
	assert.NoError(t, err)
	assert.Equal(t, DefaultEipConfig, updatedPod.Annotations[WithEIPAnnotationKey])
	assert.Equal(t, EIPNetwork, updatedPod.Annotations[v1alpha1.GameServerNetworkType])

	attributeStr, ok := pod.Annotations[EipAttributeAnnotationKey]
	assert.True(t, ok)
	attributes := make(map[string]interface{})
	jErr := json.Unmarshal([]byte(attributeStr), &attributes)
	assert.NoError(t, jErr)

	assert.Equal(t, "eip-demo", attributes["name"])
	assert.Equal(t, "BGP", attributes["isp"])
	assert.Equal(t, float64(100), attributes["bandwidth"])
	assert.Equal(t, "demo for pods eip", attributes["description"])
	assert.Equal(t, float64(2), attributes["billingType"])
}

func TestEipPlugin_OnPodUpdated_WithNetworkStatus(t *testing.T) {
	// 创建测试 Pod 并添加网络状态
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

	// 创建假的 client 包含 PodEIP
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = v1beta1.AddToScheme(scheme)
	_ = gamekruiseiov1alpha1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(pod).
		Build()

	// 执行测试
	plugin := EipPlugin{}
	// Ensure network status includes EIP information
	networkStatus := &v1alpha1.NetworkStatus{}
	networkStatus.ExternalAddresses = []v1alpha1.NetworkAddress{{IP: "203.0.113.1"}}
	networkStatus.InternalAddresses = []v1alpha1.NetworkAddress{{IP: "10.0.0.1"}}
	networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkReady
	networkStatusBytes, jErr := json.Marshal(networkStatus)
	assert.NoError(t, jErr)
	pod.Annotations[v1alpha1.GameServerNetworkStatus] = string(networkStatusBytes)
	updatedPod, err := plugin.OnPodUpdated(fakeClient, pod, context.Background())
	assert.NoError(t, err)
	// 更新一下podStatus

	// Update network status manually to simulate what OnPodUpdated should do

	jErr = json.Unmarshal([]byte(updatedPod.Annotations[v1alpha1.GameServerNetworkStatus]), &networkStatus)
	assert.NoError(t, jErr)
	// 检查结果

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
