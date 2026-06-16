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
	"fmt"
	"strings"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/cloudprovider"
	cperrors "github.com/openkruise/kruise-game/cloudprovider/errors"
	"github.com/openkruise/kruise-game/cloudprovider/utils"
	log "k8s.io/klog/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// AutoNLBsV3Network is the unique network type identifier of the V3 plugin.
// V3 是基于 PortAllocation(PA) + NLBPool 的实现：
//   - kruise-game 仅在 Pod 上写 annotation，不再直接管理 Service / NLB 云资源
//   - PA Controller (独立仓库) 通过 watch Pod 完成 PA 分配/绑定/释放
//   - kruise-game 在 OnPodUpdated 阶段读取 PA.Spec.Endpoints 构建 NetworkStatus
const AutoNLBsV3Network = "AlibabaCloud-AutoNLBs-V3"

// AutoNLBsV3Plugin 实现 cloudprovider.Plugin 接口
type AutoNLBsV3Plugin struct{}

// autoNLBsV3Config 从 GameServerSet.spec.network.networkConf 解析得到
type autoNLBsV3Config struct {
	poolName string
}

func (a *AutoNLBsV3Plugin) Name() string {
	return AutoNLBsV3Network
}

func (a *AutoNLBsV3Plugin) Alias() string {
	return AliasAutoNLBs
}

func (a *AutoNLBsV3Plugin) Init(c client.Client, options cloudprovider.CloudProviderOptions, ctx context.Context) error {
	log.Infof("[%s] Plugin initialized (PA-based, stateless)", AutoNLBsV3Network)
	return nil
}

// OnPodAdded 仅向 Pod 写入 NLBPoolName annotation，告知 PA Controller 该 Pod 想要绑定的 pool。
// 真正的 PA 分配/绑定逻辑由 PA Controller 在独立仓库完成。
func (a *AutoNLBsV3Plugin) OnPodAdded(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	networkManager := utils.NewNetworkManager(pod, c)
	networkConfig := networkManager.GetNetworkConfig()

	conf, err := parseAutoNLBsV3Config(networkConfig)
	if err != nil {
		log.Errorf("[%s] OnPodAdded parse config failed for pod %s/%s: %v",
			AutoNLBsV3Network, pod.GetNamespace(), pod.GetName(), err)
		return pod, cperrors.NewPluginError(cperrors.ParameterError, err.Error())
	}

	if pod.Annotations == nil {
		pod.Annotations = make(map[string]string)
	}
	if pod.Annotations[AnnotationNLBPoolName] != conf.poolName {
		pod.Annotations[AnnotationNLBPoolName] = conf.poolName
	}

	log.Infof("[%s] OnPodAdded pod=%s/%s pool=%s",
		AutoNLBsV3Network, pod.GetNamespace(), pod.GetName(), conf.poolName)
	return pod, nil
}

// OnPodUpdated 读取 Pod annotation 中的 PA claim，Get 对应 PA 并校验绑定关系，
// 然后从 PA.Spec.Endpoints 构建 NetworkStatus 写回 Pod。
func (a *AutoNLBsV3Plugin) OnPodUpdated(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	networkManager := utils.NewNetworkManager(pod, c)
	networkStatus, _ := networkManager.GetNetworkStatus()

	if networkStatus == nil {
		networkStatus = &gamekruiseiov1alpha1.NetworkStatus{}
	}

	// 0. 同步网络隔离状态到 Pod annotation，PA Controller 通过此 annotation 触发 Disable/Enable
	disabled := networkManager.GetNetworkDisabled()
	if pod.Annotations == nil {
		pod.Annotations = make(map[string]string)
	}
	if disabled {
		pod.Annotations[AnnotationNetworkDisabled] = "true"
	} else {
		delete(pod.Annotations, AnnotationNetworkDisabled)
	}

	// 1. 从 Pod annotation 中获取 PA claim
	paName := ""
	if pod.Annotations != nil {
		paName = pod.Annotations[AnnotationPAClaim]
	}
	if paName == "" {
		log.Infof("[%s] pod %s/%s has no PA claim yet, waiting for PA Controller",
			AutoNLBsV3Network, pod.GetNamespace(), pod.GetName())
		return a.markNotReady(networkManager, pod, networkStatus)
	}

	// 2. Get PA（与 Pod 同 namespace）
	pa := &PortAllocation{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: pod.GetNamespace(), Name: paName}, pa); err != nil {
		log.Warningf("[%s] failed to get PA %s/%s for pod %s/%s: %v",
			AutoNLBsV3Network, pod.GetNamespace(), paName, pod.GetNamespace(), pod.GetName(), err)
		return a.markNotReady(networkManager, pod, networkStatus)
	}

	// 3. 二次验证：PA.Status.Phase == Bound 且 PA.Spec.BoundPod == pod.Name
	if pa.Status.Phase != PortAllocationPhaseBound || pa.Spec.BoundPod != pod.GetName() {
		log.Infof("[%s] PA %s/%s not yet bound to pod %s (phase=%s, boundPod=%s)",
			AutoNLBsV3Network, pa.GetNamespace(), pa.GetName(), pod.GetName(),
			pa.Status.Phase, pa.Spec.BoundPod)
		return a.markNotReady(networkManager, pod, networkStatus)
	}

	// 4. 从 PA.Spec.Endpoints 构建 NetworkStatus
	built := buildNetworkStatusV3(pod, pa)
	if built == nil || len(built.ExternalAddresses) == 0 {
		log.Infof("[%s] PA %s/%s has no usable endpoints yet for pod %s",
			AutoNLBsV3Network, pa.GetNamespace(), pa.GetName(), pod.GetName())
		return a.markNotReady(networkManager, pod, networkStatus)
	}

	networkStatus.InternalAddresses = built.InternalAddresses
	networkStatus.ExternalAddresses = built.ExternalAddresses
	networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkReady

	pod, err := networkManager.UpdateNetworkStatus(*networkStatus, pod)
	return pod, cperrors.ToPluginError(err, cperrors.InternalError)
}

// OnPodDeleted no-op：PA 资源的释放由 PA Controller 通过 watch Pod 删除事件处理。
func (a *AutoNLBsV3Plugin) OnPodDeleted(c client.Client, pod *corev1.Pod, ctx context.Context) cperrors.PluginError {
	log.Infof("[%s] OnPodDeleted pod=%s/%s (release handled by PA Controller)",
		AutoNLBsV3Network, pod.GetNamespace(), pod.GetName())
	return nil
}

// markNotReady 将 Pod NetworkStatus 标记为 NetworkNotReady 并写回。
func (a *AutoNLBsV3Plugin) markNotReady(
	nm *utils.NetworkManager,
	pod *corev1.Pod,
	ns *gamekruiseiov1alpha1.NetworkStatus,
) (*corev1.Pod, cperrors.PluginError) {
	ns.CurrentNetworkState = gamekruiseiov1alpha1.NetworkNotReady
	updated, err := nm.UpdateNetworkStatus(*ns, pod)
	return updated, cperrors.ToPluginError(err, cperrors.InternalError)
}

// buildNetworkStatusV3 根据 PA.Spec.Endpoints 构建 NetworkStatus：
//   - endPoint = 所有 lane 的 "eipDNS/laneName" 用逗号拼接
//   - 每个端口：1 条 ExternalAddress（endPoint=拼接串, port=ListenerPort）
//   - 每个端口：1 条 InternalAddress（ip=PodIP, port=ContainerPort，0 时 fallback ListenerPort）
func buildNetworkStatusV3(pod *corev1.Pod, pa *PortAllocation) *gamekruiseiov1alpha1.NetworkStatus {
	ns := &gamekruiseiov1alpha1.NetworkStatus{}
	endpoints := pa.Spec.Endpoints
	if len(endpoints) == 0 {
		return ns
	}

	endPointParts := make([]string, 0, len(endpoints))
	for _, ep := range endpoints {
		endPointParts = append(endPointParts, ep.EIP+"/"+ep.Lane)
	}
	endPointStr := strings.Join(endPointParts, ",")

	internalAddresses := make([]gamekruiseiov1alpha1.NetworkAddress, 0, len(endpoints[0].Ports))
	externalAddresses := make([]gamekruiseiov1alpha1.NetworkAddress, 0, len(endpoints[0].Ports))

	for _, port := range endpoints[0].Ports {
		listenerPort := port.ListenerPort
		containerPort := port.ContainerPort
		if containerPort == 0 {
			containerPort = listenerPort
		}

		protocol := corev1.Protocol(strings.ToUpper(port.Protocol))
		if protocol == "" {
			protocol = corev1.ProtocolTCP
		}

		ePort := intstr.FromInt(int(listenerPort))
		iPort := intstr.FromInt(int(containerPort))

		externalAddresses = append(externalAddresses, gamekruiseiov1alpha1.NetworkAddress{
			EndPoint: endPointStr,
			IP:       endpoints[0].EIP,
			Ports: []gamekruiseiov1alpha1.NetworkPort{
				{
					Name:     port.Name,
					Port:     &ePort,
					Protocol: protocol,
				},
			},
		})

		internalAddresses = append(internalAddresses, gamekruiseiov1alpha1.NetworkAddress{
			IP: pod.Status.PodIP,
			Ports: []gamekruiseiov1alpha1.NetworkPort{
				{
					Name:     port.Name,
					Port:     &iPort,
					Protocol: protocol,
				},
			},
		})
	}

	ns.InternalAddresses = internalAddresses
	ns.ExternalAddresses = externalAddresses
	return ns
}

// parseAutoNLBsV3Config 解析 GSS NetworkConf：仅识别 NLBPoolName。
// 其它字段（如端口协议）由 PA Controller 自行从 NLBPool CR 读取，与 plugin 无关。
func parseAutoNLBsV3Config(conf []gamekruiseiov1alpha1.NetworkConfParams) (*autoNLBsV3Config, error) {
	c := &autoNLBsV3Config{}
	for _, p := range conf {
		if p.Name == NLBPoolNameConfig {
			c.poolName = p.Value
		}
	}
	if c.poolName == "" {
		return nil, fmt.Errorf("NLBPoolName is required")
	}
	return c, nil
}

func init() {
	alibabaCloudProvider.registerPlugin(&AutoNLBsV3Plugin{})
}

// 编译期检查：确保插件实现 cloudprovider.Plugin 接口
var _ cloudprovider.Plugin = &AutoNLBsV3Plugin{}
