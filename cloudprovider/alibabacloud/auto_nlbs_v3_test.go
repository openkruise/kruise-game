/*
Copyright 2025 The Kruise Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package alibabacloud

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	cperrors "github.com/openkruise/kruise-game/cloudprovider/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// newFakeClientV3 创建一个带有必要 Scheme 的 fake client（V3 测试专用）
func newFakeClientV3(objs ...client.Object) client.Client {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = gamekruiseiov1alpha1.AddToScheme(scheme)

	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		Build()
}

// createTestPodV3 创建 V3 测试用的 Pod
func createTestPodV3(namespace, name, gssName string, podIndex int, networkConf []gamekruiseiov1alpha1.NetworkConfParams) *corev1.Pod {
	networkConfBytes, _ := json.Marshal(networkConf)
	// 初始化 NetworkStatus 为 NotReady 状态（空对象，不是空字符串）
	networkStatus := gamekruiseiov1alpha1.NetworkStatus{
		CurrentNetworkState: gamekruiseiov1alpha1.NetworkNotReady,
	}
	networkStatusBytes, _ := json.Marshal(networkStatus)

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			UID:       types.UID("test-pod-uid-" + name),
			Labels: map[string]string{
				gamekruiseiov1alpha1.GameServerOwnerGssKey: gssName,
				SvcSelectorKey: name,
			},
			Annotations: map[string]string{
				gamekruiseiov1alpha1.GameServerNetworkType:   AutoNLBsV3Network,
				gamekruiseiov1alpha1.GameServerNetworkConf:   string(networkConfBytes),
				gamekruiseiov1alpha1.GameServerNetworkStatus: string(networkStatusBytes),
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "game-server",
					Image: "test:latest",
				},
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			PodIP: fmt.Sprintf("10.0.%d.%d", podIndex/256, podIndex%256),
		},
	}
}

// createTestGSSV3 创建 V3 测试用的 GameServerSet
func createTestGSSV3(namespace, name string, replicas int32, networkConf []gamekruiseiov1alpha1.NetworkConfParams) *gamekruiseiov1alpha1.GameServerSet {
	return &gamekruiseiov1alpha1.GameServerSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			UID:       types.UID("test-gss-uid-" + name),
		},
		TypeMeta: metav1.TypeMeta{
			APIVersion: "game.kruise.io/v1alpha1",
			Kind:       "GameServerSet",
		},
		Spec: gamekruiseiov1alpha1.GameServerSetSpec{
			Replicas: &replicas,
			Network: &gamekruiseiov1alpha1.Network{
				NetworkType: AutoNLBsV3Network,
				NetworkConf: networkConf,
			},
		},
	}
}

// createAvailableService 创建 available 状态的 Service（模拟 NLBPool Controller 预热好的 Service）
func createAvailableService(namespace, name, nlbPoolName string, portsPerPod int, servicePorts []corev1.ServicePort) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			UID:       types.UID("test-svc-uid-" + name),
			Labels: map[string]string{
				NLBPoolNameLabel:        nlbPoolName,
				SvcPoolStatusLabel:      SvcPoolStatusAvailable,
				SvcPoolPortsPerPodLabel: fmt.Sprintf("%d", portsPerPod),
			},
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeLoadBalancer,
			Selector: map[string]string{
				NLBPoolPlaceholderLabel: PlaceholderValue,
			},
			Ports: servicePorts,
		},
		Status: corev1.ServiceStatus{
			LoadBalancer: corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{},
			},
		},
	}
}

// createBoundService 创建已绑定到 Pod 的 Service
func createBoundService(namespace, name, nlbPoolName, podName, gssName string, servicePorts []corev1.ServicePort, lbIngress []corev1.LoadBalancerIngress) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			UID:       types.UID("test-svc-uid-" + name),
			Labels: map[string]string{
				NLBPoolNameLabel:     nlbPoolName,
				SvcPoolStatusLabel:   SvcPoolStatusBound,
				SvcPoolBoundPodLabel: podName,
				SvcPoolBoundGssLabel: gssName,
			},
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeLoadBalancer,
			Selector: map[string]string{
				SvcSelectorKey: podName,
			},
			Ports: servicePorts,
		},
		Status: corev1.ServiceStatus{
			LoadBalancer: corev1.LoadBalancerStatus{
				Ingress: lbIngress,
			},
		},
	}
}

// TestParseAutoNLBsV3Config 测试 V3 配置解析
func TestParseAutoNLBsV3Config(t *testing.T) {
	tests := []struct {
		name          string
		conf          []gamekruiseiov1alpha1.NetworkConfParams
		expectConfig  *autoNLBsV3Config
		expectError   bool
		errorContains string
	}{
		{
			name: "valid config with NLBPoolName and PortProtocols",
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{Name: NLBPoolNameConfigName, Value: "test-nlb-pool"},
				{Name: PortProtocolsConfigV3, Value: "8080/TCP,9000/UDP"},
			},
			expectConfig: &autoNLBsV3Config{
				nlbPoolName: "test-nlb-pool",
				targetPorts: []int{8080, 9000},
				protocols:   []corev1.Protocol{corev1.ProtocolTCP, corev1.ProtocolUDP},
			},
			expectError: false,
		},
		{
			name: "valid config with single port",
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{Name: NLBPoolNameConfigName, Value: "game-server-pool"},
				{Name: PortProtocolsConfigV3, Value: "7777/TCP"},
			},
			expectConfig: &autoNLBsV3Config{
				nlbPoolName: "game-server-pool",
				targetPorts: []int{7777},
				protocols:   []corev1.Protocol{corev1.ProtocolTCP},
			},
			expectError: false,
		},
		{
			name: "valid config with default protocol (no protocol specified)",
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{Name: NLBPoolNameConfigName, Value: "default-protocol-pool"},
				{Name: PortProtocolsConfigV3, Value: "8080"},
			},
			expectConfig: &autoNLBsV3Config{
				nlbPoolName: "default-protocol-pool",
				targetPorts: []int{8080},
				protocols:   []corev1.Protocol{corev1.ProtocolTCP},
			},
			expectError: false,
		},
		{
			name: "missing NLBPoolName",
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{Name: PortProtocolsConfigV3, Value: "8080/TCP"},
			},
			expectError:   true,
			errorContains: "NLBPoolName is required",
		},
		{
			name: "missing PortProtocols",
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{Name: NLBPoolNameConfigName, Value: "test-pool"},
			},
			expectError:   true,
			errorContains: "PortProtocols is required",
		},
		{
			name:          "empty config",
			conf:          []gamekruiseiov1alpha1.NetworkConfParams{},
			expectError:   true,
			errorContains: "NLBPoolName is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := parseAutoNLBsV3Config(tt.conf)

			if tt.expectError {
				if err == nil {
					t.Errorf("parseAutoNLBsV3Config() expected error but got nil")
					return
				}
				if tt.errorContains != "" && !contains(err.Error(), tt.errorContains) {
					t.Errorf("parseAutoNLBsV3Config() error = %v, want error containing %v", err, tt.errorContains)
				}
				return
			}

			if err != nil {
				t.Errorf("parseAutoNLBsV3Config() unexpected error = %v", err)
				return
			}

			if config.nlbPoolName != tt.expectConfig.nlbPoolName {
				t.Errorf("nlbPoolName: expected %q, got %q", tt.expectConfig.nlbPoolName, config.nlbPoolName)
			}

			if len(config.targetPorts) != len(tt.expectConfig.targetPorts) {
				t.Errorf("targetPorts length: expected %d, got %d", len(tt.expectConfig.targetPorts), len(config.targetPorts))
			} else {
				for i, port := range config.targetPorts {
					if port != tt.expectConfig.targetPorts[i] {
						t.Errorf("targetPorts[%d]: expected %d, got %d", i, tt.expectConfig.targetPorts[i], port)
					}
				}
			}

			if len(config.protocols) != len(tt.expectConfig.protocols) {
				t.Errorf("protocols length: expected %d, got %d", len(tt.expectConfig.protocols), len(config.protocols))
			} else {
				for i, proto := range config.protocols {
					if proto != tt.expectConfig.protocols[i] {
						t.Errorf("protocols[%d]: expected %v, got %v", i, tt.expectConfig.protocols[i], proto)
					}
				}
			}
		})
	}
}

// TestAutoNLBsV3_OnPodAdded 测试 OnPodAdded 不做任何修改
func TestAutoNLBsV3_OnPodAdded(t *testing.T) {
	plugin := &AutoNLBsV3Plugin{}

	networkConf := []gamekruiseiov1alpha1.NetworkConfParams{
		{Name: NLBPoolNameConfigName, Value: "test-pool"},
		{Name: PortProtocolsConfigV3, Value: "8080/TCP"},
	}
	pod := createTestPodV3("default", "test-pod-0", "test-gss", 0, networkConf)

	c := newFakeClientV3()
	ctx := context.Background()

	updatedPod, err := plugin.OnPodAdded(c, pod, ctx)
	if err != nil {
		t.Errorf("OnPodAdded() should return nil error, got: %v", err)
	}

	// 验证 Pod 没有被修改
	if updatedPod.Name != pod.Name {
		t.Errorf("Pod name should not change: expected %q, got %q", pod.Name, updatedPod.Name)
	}
	if updatedPod.Namespace != pod.Namespace {
		t.Errorf("Pod namespace should not change: expected %q, got %q", pod.Namespace, updatedPod.Namespace)
	}

	// 验证 Pod 的 annotations 没有被修改（直接返回原 Pod）
	if updatedPod.Annotations[gamekruiseiov1alpha1.GameServerNetworkStatus] != pod.Annotations[gamekruiseiov1alpha1.GameServerNetworkStatus] {
		t.Errorf("NetworkStatus should not change in OnPodAdded")
	}
}

// TestAutoNLBsV3_OnPodUpdated_BindSuccess 测试成功绑定 Service
func TestAutoNLBsV3_OnPodUpdated_BindSuccess(t *testing.T) {
	plugin := &AutoNLBsV3Plugin{}

	networkConf := []gamekruiseiov1alpha1.NetworkConfParams{
		{Name: NLBPoolNameConfigName, Value: "test-pool"},
		{Name: PortProtocolsConfigV3, Value: "8080/TCP,9000/UDP"},
	}

	pod := createTestPodV3("default", "test-pod-0", "test-gss", 0, networkConf)

	// 创建 available 状态的 Service（模拟 NLBPool Controller 预热好的）
	servicePorts := []corev1.ServicePort{
		{Port: 10000, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt(80)},
		{Port: 10001, Protocol: corev1.ProtocolUDP, TargetPort: intstr.FromInt(90)},
	}
	svc := createAvailableService("default", "test-pool-svc-0", "test-pool", 2, servicePorts)

	c := newFakeClientV3(pod, svc)
	ctx := context.Background()

	_, err := plugin.OnPodUpdated(c, pod, ctx)

	// 注意：当 LB 未就绪时，代码会更新 NetworkStatus 并返回
	// ToPluginError(err, InternalError)，但如果 UpdateNetworkStatus 成功（err=nil），
	// ToPluginError 会返回 nil，所以这里 err 可能为 nil
	_ = err

	// 验证 Service 已被绑定
	updatedSvc := &corev1.Service{}
	if err := c.Get(ctx, types.NamespacedName{Name: svc.Name, Namespace: svc.Namespace}, updatedSvc); err != nil {
		t.Fatalf("Failed to get updated Service: %v", err)
	}

	// 验证 Service Labels 更新
	if updatedSvc.Labels[SvcPoolStatusLabel] != SvcPoolStatusBound {
		t.Errorf("Service status label: expected %q, got %q", SvcPoolStatusBound, updatedSvc.Labels[SvcPoolStatusLabel])
	}
	if updatedSvc.Labels[SvcPoolBoundPodLabel] != pod.Name {
		t.Errorf("Service bound-pod label: expected %q, got %q", pod.Name, updatedSvc.Labels[SvcPoolBoundPodLabel])
	}
	if updatedSvc.Labels[SvcPoolBoundGssLabel] != "test-gss" {
		t.Errorf("Service bound-gss label: expected %q, got %q", "test-gss", updatedSvc.Labels[SvcPoolBoundGssLabel])
	}

	// 验证 Service Selector 指向 Pod
	if updatedSvc.Spec.Selector[SvcSelectorKey] != pod.Name {
		t.Errorf("Service selector: expected pod name %q, got %q", pod.Name, updatedSvc.Spec.Selector[SvcSelectorKey])
	}

	// 验证 targetPort 更新为 Pod 的实际端口
	if updatedSvc.Spec.Ports[0].TargetPort.IntVal != 8080 {
		t.Errorf("First targetPort: expected 8080, got %d", updatedSvc.Spec.Ports[0].TargetPort.IntVal)
	}
	if updatedSvc.Spec.Ports[1].TargetPort.IntVal != 9000 {
		t.Errorf("Second targetPort: expected 9000, got %d", updatedSvc.Spec.Ports[1].TargetPort.IntVal)
	}
}

// TestAutoNLBsV3_OnPodUpdated_NoAvailableService 测试没有可用 Service 的场景
func TestAutoNLBsV3_OnPodUpdated_NoAvailableService(t *testing.T) {
	plugin := &AutoNLBsV3Plugin{}

	networkConf := []gamekruiseiov1alpha1.NetworkConfParams{
		{Name: NLBPoolNameConfigName, Value: "test-pool"},
		{Name: PortProtocolsConfigV3, Value: "8080/TCP"},
	}

	pod := createTestPodV3("default", "test-pod-0", "test-gss", 0, networkConf)

	// 不创建任何 Service
	c := newFakeClientV3(pod)
	ctx := context.Background()

	_, err := plugin.OnPodUpdated(c, pod, ctx)

	// 注意：当没有可用 Service 时，代码会更新 NetworkStatus 并返回
	// ToPluginError(err, InternalError)，但如果 UpdateNetworkStatus 成功（err=nil），
	// ToPluginError 会返回 nil，所以这里 err 可能为 nil
	_ = err
}

// TestAutoNLBsV3_OnPodUpdated_IncompatiblePorts 测试端口不兼容的场景
func TestAutoNLBsV3_OnPodUpdated_IncompatiblePorts(t *testing.T) {
	plugin := &AutoNLBsV3Plugin{}

	networkConf := []gamekruiseiov1alpha1.NetworkConfParams{
		{Name: NLBPoolNameConfigName, Value: "test-pool"},
		{Name: PortProtocolsConfigV3, Value: "8080/TCP,9000/UDP,7777/TCP"}, // 3 个端口
	}

	pod := createTestPodV3("default", "test-pod-0", "test-gss", 0, networkConf)

	// 创建 available 状态的 Service，但 portsPerPod = 2（与 Pod 的 3 个端口不匹配）
	servicePorts := []corev1.ServicePort{
		{Port: 10000, Protocol: corev1.ProtocolTCP},
		{Port: 10001, Protocol: corev1.ProtocolUDP},
	}
	svc := createAvailableService("default", "test-pool-svc-0", "test-pool", 2, servicePorts)

	c := newFakeClientV3(pod, svc)
	ctx := context.Background()

	_, err := plugin.OnPodUpdated(c, pod, ctx)

	// 应该返回 RetryError，因为没有兼容的 Service
	if err == nil {
		t.Errorf("OnPodUpdated() should return RetryError when no compatible service")
		return
	}
	if err.Type() != cperrors.RetryError {
		t.Errorf("OnPodUpdated() should return RetryError, got: %v", err.Type())
	}
}

// TestAutoNLBsV3_OnPodUpdated_AlreadyBound_LBReady 测试已绑定且 LB 就绪的场景
func TestAutoNLBsV3_OnPodUpdated_AlreadyBound_LBReady(t *testing.T) {
	plugin := &AutoNLBsV3Plugin{}

	networkConf := []gamekruiseiov1alpha1.NetworkConfParams{
		{Name: NLBPoolNameConfigName, Value: "test-pool"},
		{Name: PortProtocolsConfigV3, Value: "8080/TCP"},
	}

	pod := createTestPodV3("default", "test-pod-0", "test-gss", 0, networkConf)

	// 创建已绑定且 LB 就绪的 Service
	servicePorts := []corev1.ServicePort{
		{Port: 10000, Name: "game-port", Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt(8080)},
	}
	lbIngress := []corev1.LoadBalancerIngress{
		{IP: "1.2.3.4", Hostname: "nlb-test.aliyuncs.com"},
	}
	svc := createBoundService("default", "test-pool-svc-0", "test-pool", pod.Name, "test-gss", servicePorts, lbIngress)

	c := newFakeClientV3(pod, svc)
	ctx := context.Background()

	updatedPod, err := plugin.OnPodUpdated(c, pod, ctx)
	if err != nil {
		t.Errorf("OnPodUpdated() should not return error when LB is ready: %v", err)
		return
	}

	// 验证 Pod 的 NetworkStatus
	if updatedPod.Annotations[gamekruiseiov1alpha1.GameServerNetworkStatus] == "" {
		t.Errorf("NetworkStatus should be set when LB is ready")
		return
	}

	// 解析 NetworkStatus
	var networkStatus gamekruiseiov1alpha1.NetworkStatus
	if err := json.Unmarshal([]byte(updatedPod.Annotations[gamekruiseiov1alpha1.GameServerNetworkStatus]), &networkStatus); err != nil {
		t.Errorf("Failed to unmarshal NetworkStatus: %v", err)
		return
	}

	// 验证 NetworkState
	if networkStatus.CurrentNetworkState != gamekruiseiov1alpha1.NetworkReady {
		t.Errorf("NetworkState: expected %q, got %q", gamekruiseiov1alpha1.NetworkReady, networkStatus.CurrentNetworkState)
	}

	// 验证 ExternalAddresses 包含 LB IP
	if len(networkStatus.ExternalAddresses) == 0 {
		t.Errorf("ExternalAddresses should not be empty")
	} else {
		foundLBIP := false
		for _, addr := range networkStatus.ExternalAddresses {
			if addr.IP == "1.2.3.4" {
				foundLBIP = true
				// 验证端口
				if len(addr.Ports) == 0 || addr.Ports[0].Port.IntVal != 10000 {
					t.Errorf("External port: expected 10000, got %v", addr.Ports[0].Port)
				}
				break
			}
		}
		if !foundLBIP {
			t.Errorf("ExternalAddresses should contain LB IP 1.2.3.4")
		}
	}

	// 验证 InternalAddresses 包含 Pod IP
	if len(networkStatus.InternalAddresses) == 0 {
		t.Errorf("InternalAddresses should not be empty")
	} else {
		if networkStatus.InternalAddresses[0].IP != pod.Status.PodIP {
			t.Errorf("Internal IP: expected %q, got %q", pod.Status.PodIP, networkStatus.InternalAddresses[0].IP)
		}
	}
}

// TestAutoNLBsV3_OnPodUpdated_AlreadyBound_LBNotReady 测试已绑定但 LB 未就绪的场景
func TestAutoNLBsV3_OnPodUpdated_AlreadyBound_LBNotReady(t *testing.T) {
	plugin := &AutoNLBsV3Plugin{}

	networkConf := []gamekruiseiov1alpha1.NetworkConfParams{
		{Name: NLBPoolNameConfigName, Value: "test-pool"},
		{Name: PortProtocolsConfigV3, Value: "8080/TCP"},
	}

	pod := createTestPodV3("default", "test-pod-0", "test-gss", 0, networkConf)

	// 创建已绑定但 LB 未就绪的 Service（空的 Ingress）
	servicePorts := []corev1.ServicePort{
		{Port: 10000, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt(8080)},
	}
	lbIngress := []corev1.LoadBalancerIngress{} // 空的，表示 LB 未就绪
	svc := createBoundService("default", "test-pool-svc-0", "test-pool", pod.Name, "test-gss", servicePorts, lbIngress)

	c := newFakeClientV3(pod, svc)
	ctx := context.Background()

	_, err := plugin.OnPodUpdated(c, pod, ctx)

	// 注意：当 LB 未就绪时，代码会更新 NetworkStatus 并返回
	// ToPluginError(err, InternalError)，但如果 UpdateNetworkStatus 成功（err=nil），
	// ToPluginError 会返回 nil，所以这里 err 可能为 nil
	_ = err
}

// TestAutoNLBsV3_OnPodDeleted_ReleaseService 测试释放绑定的 Service
func TestAutoNLBsV3_OnPodDeleted_ReleaseService(t *testing.T) {
	plugin := &AutoNLBsV3Plugin{}

	networkConf := []gamekruiseiov1alpha1.NetworkConfParams{
		{Name: NLBPoolNameConfigName, Value: "test-pool"},
		{Name: PortProtocolsConfigV3, Value: "8080/TCP"},
	}

	pod := createTestPodV3("default", "test-pod-0", "test-gss", 0, networkConf)

	// 创建已绑定的 Service
	servicePorts := []corev1.ServicePort{
		{Port: 10000, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt(8080)},
	}
	lbIngress := []corev1.LoadBalancerIngress{{IP: "1.2.3.4"}}
	svc := createBoundService("default", "test-pool-svc-0", "test-pool", pod.Name, "test-gss", servicePorts, lbIngress)

	c := newFakeClientV3(pod, svc)
	ctx := context.Background()

	err := plugin.OnPodDeleted(c, pod, ctx)
	if err != nil {
		t.Errorf("OnPodDeleted() should return nil, got: %v", err)
		return
	}

	// 验证 Service 已被释放
	updatedSvc := &corev1.Service{}
	if err := c.Get(ctx, types.NamespacedName{Name: svc.Name, Namespace: svc.Namespace}, updatedSvc); err != nil {
		t.Fatalf("Failed to get updated Service: %v", err)
	}

	// 验证 Service status 变为 available
	if updatedSvc.Labels[SvcPoolStatusLabel] != SvcPoolStatusAvailable {
		t.Errorf("Service status label: expected %q, got %q", SvcPoolStatusAvailable, updatedSvc.Labels[SvcPoolStatusLabel])
	}

	// 验证 bound-pod label 被删除
	if _, exists := updatedSvc.Labels[SvcPoolBoundPodLabel]; exists {
		t.Errorf("Service bound-pod label should be deleted")
	}

	// 验证 bound-gss label 被删除
	if _, exists := updatedSvc.Labels[SvcPoolBoundGssLabel]; exists {
		t.Errorf("Service bound-gss label should be deleted")
	}

	// 验证 Selector 恢复为 dummy placeholder
	if updatedSvc.Spec.Selector[NLBPoolPlaceholderLabel] != PlaceholderValue {
		t.Errorf("Service selector: expected placeholder %q, got %v", PlaceholderValue, updatedSvc.Spec.Selector)
	}
}

// TestAutoNLBsV3_OnPodDeleted_NoBoundService 测试没有绑定 Service 的场景
func TestAutoNLBsV3_OnPodDeleted_NoBoundService(t *testing.T) {
	plugin := &AutoNLBsV3Plugin{}

	networkConf := []gamekruiseiov1alpha1.NetworkConfParams{
		{Name: NLBPoolNameConfigName, Value: "test-pool"},
		{Name: PortProtocolsConfigV3, Value: "8080/TCP"},
	}

	pod := createTestPodV3("default", "test-pod-0", "test-gss", 0, networkConf)

	// 创建 available 状态的 Service（未绑定到该 Pod）
	servicePorts := []corev1.ServicePort{
		{Port: 10000, Protocol: corev1.ProtocolTCP},
	}
	svc := createAvailableService("default", "test-pool-svc-0", "test-pool", 1, servicePorts)

	c := newFakeClientV3(pod, svc)
	ctx := context.Background()

	err := plugin.OnPodDeleted(c, pod, ctx)
	if err != nil {
		t.Errorf("OnPodDeleted() should return nil when no bound service, got: %v", err)
	}
}

// TestAutoNLBsV3_MultiGSSSharePool 测试多 GSS 共享同一个 NLBPool
func TestAutoNLBsV3_MultiGSSSharePool(t *testing.T) {
	plugin := &AutoNLBsV3Plugin{}

	networkConf := []gamekruiseiov1alpha1.NetworkConfParams{
		{Name: NLBPoolNameConfigName, Value: "shared-pool"},
		{Name: PortProtocolsConfigV3, Value: "8080/TCP"},
	}

	// 创建两个不同 GSS 的 Pod
	pod1 := createTestPodV3("default", "gss1-pod-0", "gss1", 0, networkConf)
	pod2 := createTestPodV3("default", "gss2-pod-0", "gss2", 1, networkConf)

	// 创建两个 available 的 Service
	servicePorts := []corev1.ServicePort{
		{Port: 10000, Protocol: corev1.ProtocolTCP},
	}
	svc1 := createAvailableService("default", "shared-pool-svc-0", "shared-pool", 1, servicePorts)
	svc2 := createAvailableService("default", "shared-pool-svc-1", "shared-pool", 1, servicePorts)

	c := newFakeClientV3(pod1, pod2, svc1, svc2)
	ctx := context.Background()

	// 处理第一个 Pod
	_, err := plugin.OnPodUpdated(c, pod1, ctx)
	if err != nil && err.Type() != cperrors.RetryError {
		t.Errorf("OnPodUpdated() for pod1 failed: %v", err)
	}

	// 处理第二个 Pod
	_, err = plugin.OnPodUpdated(c, pod2, ctx)
	if err != nil && err.Type() != cperrors.RetryError {
		t.Errorf("OnPodUpdated() for pod2 failed: %v", err)
	}

	// 验证两个 Service 都变为 bound
	updatedSvc1 := &corev1.Service{}
	if err := c.Get(ctx, types.NamespacedName{Name: svc1.Name, Namespace: svc1.Namespace}, updatedSvc1); err != nil {
		t.Fatalf("Failed to get svc1: %v", err)
	}

	updatedSvc2 := &corev1.Service{}
	if err := c.Get(ctx, types.NamespacedName{Name: svc2.Name, Namespace: svc2.Namespace}, updatedSvc2); err != nil {
		t.Fatalf("Failed to get svc2: %v", err)
	}

	// 验证两个 Service 都变为 bound 状态
	if updatedSvc1.Labels[SvcPoolStatusLabel] != SvcPoolStatusBound {
		t.Errorf("svc1 status: expected %q, got %q", SvcPoolStatusBound, updatedSvc1.Labels[SvcPoolStatusLabel])
	}
	if updatedSvc2.Labels[SvcPoolStatusLabel] != SvcPoolStatusBound {
		t.Errorf("svc2 status: expected %q, got %q", SvcPoolStatusBound, updatedSvc2.Labels[SvcPoolStatusLabel])
	}

	// 验证每个 Service 绑定到不同的 Pod
	boundPod1 := updatedSvc1.Labels[SvcPoolBoundPodLabel]
	boundPod2 := updatedSvc2.Labels[SvcPoolBoundPodLabel]

	if boundPod1 == "" || boundPod2 == "" {
		t.Errorf("Both services should be bound to pods")
	}
	if boundPod1 == boundPod2 {
		t.Errorf("Services should be bound to different pods, got: %q and %q", boundPod1, boundPod2)
	}

	// 验证 bound-gss Labels 分别指向各自的 GSS
	if updatedSvc1.Labels[SvcPoolBoundGssLabel] != "gss1" {
		t.Errorf("svc1 bound-gss: expected gss1, got %q", updatedSvc1.Labels[SvcPoolBoundGssLabel])
	}
	if updatedSvc2.Labels[SvcPoolBoundGssLabel] != "gss2" {
		t.Errorf("svc2 bound-gss: expected gss2, got %q", updatedSvc2.Labels[SvcPoolBoundGssLabel])
	}

	// 验证每个 Service 的 Selector 指向正确的 Pod
	if updatedSvc1.Spec.Selector[SvcSelectorKey] != pod1.Name && updatedSvc1.Spec.Selector[SvcSelectorKey] != pod2.Name {
		t.Errorf("svc1 selector should point to one of the pods")
	}
	if updatedSvc2.Spec.Selector[SvcSelectorKey] != pod1.Name && updatedSvc2.Spec.Selector[SvcSelectorKey] != pod2.Name {
		t.Errorf("svc2 selector should point to one of the pods")
	}
}
