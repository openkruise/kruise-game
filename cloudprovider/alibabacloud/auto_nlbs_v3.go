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
	"strconv"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/cloudprovider"
	cperrors "github.com/openkruise/kruise-game/cloudprovider/errors"
	"github.com/openkruise/kruise-game/cloudprovider/utils"
	"github.com/openkruise/kruise-game/pkg/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/intstr"
	log "k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	AutoNLBsV3Network = "AlibabaCloud-AutoNLBs-V3"

	// NLBPool Operator Label/Annotation 常量
	NLBPoolNameLabel        = "game.kruise.io/nlb-pool-name"
	SvcPoolStatusLabel      = "game.kruise.io/svc-pool-status"
	SvcPoolPortsPerPodLabel = "game.kruise.io/svc-pool-ports-per-pod"
	SvcPoolProtocolsLabel   = "game.kruise.io/svc-pool-protocols"
	SvcPoolBoundPodLabel    = "game.kruise.io/svc-pool-bound-pod"
	SvcPoolBoundGssLabel    = "game.kruise.io/svc-pool-bound-gss"
	NLBPoolPlaceholderLabel = "game.kruise.io/nlb-pool-placeholder"

	SvcPoolStatusAvailable = "available"
	SvcPoolStatusBound     = "bound"
	PlaceholderValue       = "none"

	// V3 配置参数常量
	NLBPoolNameConfigName = "NLBPoolName"
	PortProtocolsConfigV3 = "PortProtocols"
)

// AutoNLBsV3Plugin V3 网络插件
// V3 不维护任何内存状态，所有状态通过 Service Labels 管理
// NLBPool 的预热由独立的 NLBPool Controller 处理
type AutoNLBsV3Plugin struct{}

// autoNLBsV3Config V3 配置（从 GSS NetworkConf 解析）
type autoNLBsV3Config struct {
	nlbPoolName string            // NLBPool CR 名称
	targetPorts []int             // Pod 实际监听端口
	protocols   []corev1.Protocol // 协议
}

func (a *AutoNLBsV3Plugin) Name() string {
	return AutoNLBsV3Network
}

func (a *AutoNLBsV3Plugin) Alias() string {
	return AliasAutoNLBs
}

func (a *AutoNLBsV3Plugin) Init(c client.Client, options cloudprovider.CloudProviderOptions, ctx context.Context) error {
	log.Infof("[%s] Plugin initializing (stateless, no prewarming needed)...", AutoNLBsV3Network)
	// V3 不需要初始化内存状态
	// NLBPool 的预热由独立的 NLBPool Controller 处理
	log.Infof("[%s] Plugin initialization completed", AutoNLBsV3Network)
	return nil
}

func (a *AutoNLBsV3Plugin) OnPodAdded(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	log.Infof("[%s] OnPodAdded called for pod %s/%s", AutoNLBsV3Network, pod.GetNamespace(), pod.GetName())
	// V3 在 OnPodAdded 中不做绑定操作
	// 绑定在 OnPodUpdated 中处理（确保 Pod 已调度）
	return pod, nil
}

func (a *AutoNLBsV3Plugin) OnPodUpdated(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	log.Infof("[%s] OnPodUpdated called for pod %s/%s", AutoNLBsV3Network, pod.GetNamespace(), pod.GetName())

	networkManager := utils.NewNetworkManager(pod, c)
	networkStatus, _ := networkManager.GetNetworkStatus()
	networkConfig := networkManager.GetNetworkConfig()

	conf, err := parseAutoNLBsV3Config(networkConfig)
	if err != nil {
		log.Errorf("[%s] Failed to parse config for pod %s/%s: %v", AutoNLBsV3Network, pod.GetNamespace(), pod.GetName(), err)
		return pod, cperrors.NewPluginError(cperrors.ParameterError, err.Error())
	}

	podName := pod.GetName()
	ns := pod.GetNamespace()
	gssName := pod.GetLabels()[gamekruiseiov1alpha1.GameServerOwnerGssKey]

	// 初始化 networkStatus（如果为 nil）
	if networkStatus == nil {
		pod, err = networkManager.UpdateNetworkStatus(gamekruiseiov1alpha1.NetworkStatus{
			CurrentNetworkState: gamekruiseiov1alpha1.NetworkNotReady,
		}, pod)
		return pod, cperrors.ToPluginError(err, cperrors.InternalError)
	}

	// 1. 检查是否已绑定 Service
	boundSvcList := &corev1.ServiceList{}
	err = c.List(ctx, boundSvcList,
		client.InNamespace(ns),
		client.MatchingLabels{
			SvcPoolBoundPodLabel: podName,
			NLBPoolNameLabel:     conf.nlbPoolName,
		})
	if err != nil {
		log.Errorf("[%s] Failed to list bound services for pod %s/%s: %v", AutoNLBsV3Network, ns, podName, err)
		return pod, cperrors.NewPluginError(cperrors.ApiCallError, err.Error())
	}

	var boundSvc *corev1.Service
	if len(boundSvcList.Items) > 0 {
		boundSvc = &boundSvcList.Items[0]
		log.Infof("[%s] Pod %s/%s already bound to service %s", AutoNLBsV3Network, ns, podName, boundSvc.Name)
	}

	// 2. 未绑定：执行绑定
	if boundSvc == nil {
		// 查找可用 Service
		availableSvcList := &corev1.ServiceList{}
		err = c.List(ctx, availableSvcList,
			client.InNamespace(ns),
			client.MatchingLabels{
				NLBPoolNameLabel:   conf.nlbPoolName,
				SvcPoolStatusLabel: SvcPoolStatusAvailable,
			})
		if err != nil {
			log.Errorf("[%s] Failed to list available services for pod %s/%s: %v", AutoNLBsV3Network, ns, podName, err)
			return pod, cperrors.NewPluginError(cperrors.ApiCallError, err.Error())
		}

		if len(availableSvcList.Items) == 0 {
			// 无可用 Service，等待 NLBPool Controller 扩容
			log.Infof("[%s] No available service in NLBPool %s for pod %s/%s, waiting for pool expansion",
				AutoNLBsV3Network, conf.nlbPoolName, ns, podName)
			networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkNotReady
			pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
			return pod, cperrors.ToPluginError(err, cperrors.InternalError)
		}

		// 尝试绑定第一个兼容的可用 Service（乐观锁）
		targetPortsCount := strconv.Itoa(len(conf.targetPorts))
		for i := range availableSvcList.Items {
			svc := &availableSvcList.Items[i]

			// 验证兼容性：portsPerPod 匹配
			svcPortsPerPod := svc.Labels[SvcPoolPortsPerPodLabel]
			if svcPortsPerPod != targetPortsCount {
				log.Infof("[%s] Service %s portsPerPod=%s does not match targetPorts count=%s, skipping",
					AutoNLBsV3Network, svc.Name, svcPortsPerPod, targetPortsCount)
				continue
			}

			// 执行绑定：更新 Service Labels
			if svc.Labels == nil {
				svc.Labels = make(map[string]string)
			}
			svc.Labels[SvcPoolStatusLabel] = SvcPoolStatusBound
			svc.Labels[SvcPoolBoundPodLabel] = podName
			svc.Labels[SvcPoolBoundGssLabel] = gssName

			// 更新 Selector 指向当前 Pod
			svc.Spec.Selector = map[string]string{
				SvcSelectorKey: podName,
			}

			// 更新 targetPort
			for j := range svc.Spec.Ports {
				if j < len(conf.targetPorts) {
					svc.Spec.Ports[j].TargetPort = intstr.FromInt(conf.targetPorts[j])
				}
			}

			// 使用 Update（带 resourceVersion 乐观锁）
			if err := c.Update(ctx, svc); err != nil {
				if errors.IsConflict(err) {
					log.Infof("[%s] Conflict when binding service %s, trying next available service",
						AutoNLBsV3Network, svc.Name)
					continue // 冲突，尝试下一个
				}
				log.Errorf("[%s] Failed to update service %s for binding: %v", AutoNLBsV3Network, svc.Name, err)
				return pod, cperrors.NewPluginError(cperrors.ApiCallError, err.Error())
			}

			// 绑定成功
			boundSvc = svc
			log.Infof("[%s] Successfully bound pod %s/%s to service %s",
				AutoNLBsV3Network, ns, podName, svc.Name)
			break
		}

		if boundSvc == nil {
			log.Infof("[%s] Failed to bind service for pod %s/%s due to conflicts, retrying",
				AutoNLBsV3Network, ns, podName)
			return pod, cperrors.NewPluginError(cperrors.RetryError,
				"failed to bind service due to conflicts, retrying")
		}
	}

	// 3. 处理网络禁用/启用
	if networkManager.GetNetworkDisabled() && boundSvc.Spec.Type == corev1.ServiceTypeLoadBalancer {
		boundSvc.Spec.Type = corev1.ServiceTypeClusterIP
		if err := c.Update(ctx, boundSvc); err != nil {
			return pod, cperrors.ToPluginError(err, cperrors.ApiCallError)
		}
		networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkNotReady
		pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
		return pod, cperrors.ToPluginError(err, cperrors.InternalError)
	}

	if !networkManager.GetNetworkDisabled() && boundSvc.Spec.Type == corev1.ServiceTypeClusterIP {
		boundSvc.Spec.Type = corev1.ServiceTypeLoadBalancer
		if err := c.Update(ctx, boundSvc); err != nil {
			return pod, cperrors.ToPluginError(err, cperrors.ApiCallError)
		}
		networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkNotReady
		pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
		return pod, cperrors.ToPluginError(err, cperrors.InternalError)
	}

	// 4. 已绑定：检查 Service LoadBalancer 是否就绪
	if len(boundSvc.Status.LoadBalancer.Ingress) == 0 {
		log.Infof("[%s] Service %s LoadBalancer not ready yet for pod %s/%s",
			AutoNLBsV3Network, boundSvc.Name, ns, podName)
		networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkNotReady
		pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
		return pod, cperrors.ToPluginError(err, cperrors.InternalError)
	}

	// allow not ready containers
	if util.IsAllowNotReadyContainers(networkManager.GetNetworkConfig()) {
		toUpdateSvc, err := utils.AllowNotReadyContainers(c, ctx, pod, boundSvc, false)
		if err != nil {
			return pod, err
		}
		if toUpdateSvc {
			if err := c.Update(ctx, boundSvc); err != nil {
				return pod, cperrors.ToPluginError(err, cperrors.ApiCallError)
			}
		}
	}

	// 5. 更新 Pod NetworkStatus
	internalAddresses := make([]gamekruiseiov1alpha1.NetworkAddress, 0)
	externalAddresses := make([]gamekruiseiov1alpha1.NetworkAddress, 0)

	// Internal: Pod IP + target ports
	for _, port := range conf.targetPorts {
		iPort := intstr.FromInt(port)
		internalAddresses = append(internalAddresses, gamekruiseiov1alpha1.NetworkAddress{
			IP: pod.Status.PodIP,
			Ports: []gamekruiseiov1alpha1.NetworkPort{
				{
					Name:     strconv.Itoa(port),
					Port:     &iPort,
					Protocol: corev1.ProtocolTCP,
				},
			},
		})
	}

	// External: LB IP/Hostname + service ports
	for _, ingress := range boundSvc.Status.LoadBalancer.Ingress {
		lbIP := ingress.IP
		if lbIP == "" {
			lbIP = ingress.Hostname
		}

		networkPorts := make([]gamekruiseiov1alpha1.NetworkPort, 0)
		for _, svcPort := range boundSvc.Spec.Ports {
			ePort := intstr.FromInt(int(svcPort.Port))
			networkPorts = append(networkPorts, gamekruiseiov1alpha1.NetworkPort{
				Name:     svcPort.Name,
				Port:     &ePort,
				Protocol: svcPort.Protocol,
			})
		}
		externalAddresses = append(externalAddresses, gamekruiseiov1alpha1.NetworkAddress{
			IP:    lbIP,
			Ports: networkPorts,
		})
	}

	networkStatus.InternalAddresses = internalAddresses
	networkStatus.ExternalAddresses = externalAddresses
	networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkReady

	pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
	return pod, cperrors.ToPluginError(err, cperrors.InternalError)
}

func (a *AutoNLBsV3Plugin) OnPodDeleted(c client.Client, pod *corev1.Pod, ctx context.Context) cperrors.PluginError {
	log.Infof("[%s] OnPodDeleted called for pod %s/%s", AutoNLBsV3Network, pod.GetNamespace(), pod.GetName())

	// 解析配置获取 poolName
	networkManager := utils.NewNetworkManager(pod, c)
	networkConfig := networkManager.GetNetworkConfig()
	conf, err := parseAutoNLBsV3Config(networkConfig)
	if err != nil {
		log.Errorf("[%s] Failed to parse config for pod %s/%s: %v, skipping cleanup",
			AutoNLBsV3Network, pod.GetNamespace(), pod.GetName(), err)
		return nil // 解析失败直接返回，不阻塞 Pod 删除
	}

	podName := pod.GetName()
	ns := pod.GetNamespace()

	// 查找绑定到该 Pod 的 Service
	boundSvcList := &corev1.ServiceList{}
	err = c.List(ctx, boundSvcList,
		client.InNamespace(ns),
		client.MatchingLabels{
			SvcPoolBoundPodLabel: podName,
			NLBPoolNameLabel:     conf.nlbPoolName,
		})
	if err != nil {
		log.Errorf("[%s] Failed to list bound services for pod %s/%s: %v",
			AutoNLBsV3Network, ns, podName, err)
		return nil // 查询失败不阻塞 Pod 删除
	}

	if len(boundSvcList.Items) == 0 {
		log.Infof("[%s] No bound services found for pod %s/%s", AutoNLBsV3Network, ns, podName)
		return nil
	}

	// 释放所有绑定的 Service
	for i := range boundSvcList.Items {
		svc := &boundSvcList.Items[i]

		// 恢复为 available
		if svc.Labels == nil {
			svc.Labels = make(map[string]string)
		}
		svc.Labels[SvcPoolStatusLabel] = SvcPoolStatusAvailable
		delete(svc.Labels, SvcPoolBoundPodLabel)
		delete(svc.Labels, SvcPoolBoundGssLabel)

		// 恢复 dummy Selector
		svc.Spec.Selector = map[string]string{
			NLBPoolPlaceholderLabel: PlaceholderValue,
		}

		if err := c.Update(ctx, svc); err != nil {
			log.Errorf("[%s] Failed to release service %s: %v", AutoNLBsV3Network, svc.Name, err)
			// 记录日志但不阻塞 Pod 删除
		} else {
			log.Infof("[%s] Successfully released service %s from pod %s/%s",
				AutoNLBsV3Network, svc.Name, ns, podName)
		}
	}

	return nil
}

// parseAutoNLBsV3Config 解析 V3 配置
func parseAutoNLBsV3Config(conf []gamekruiseiov1alpha1.NetworkConfParams) (*autoNLBsV3Config, error) {
	config := &autoNLBsV3Config{}
	for _, c := range conf {
		switch c.Name {
		case NLBPoolNameConfigName:
			config.nlbPoolName = c.Value
		case PortProtocolsConfigV3:
			// 复用已有的端口协议解析逻辑
			ports, protocols := parsePortProtocols(c.Value)
			config.targetPorts = ports
			config.protocols = protocols
		}
	}

	if config.nlbPoolName == "" {
		return nil, fmt.Errorf("NLBPoolName is required")
	}
	if len(config.targetPorts) == 0 {
		return nil, fmt.Errorf("PortProtocols is required")
	}

	// 默认协议为 TCP
	if len(config.protocols) == 0 {
		config.protocols = make([]corev1.Protocol, len(config.targetPorts))
		for i := range config.protocols {
			config.protocols[i] = corev1.ProtocolTCP
		}
	}

	return config, nil
}

func init() {
	autoNLBsV3Plugin := AutoNLBsV3Plugin{}
	alibabaCloudProvider.registerPlugin(&autoNLBsV3Plugin)
}

// Ensure AutoNLBsV3Plugin implements cloudprovider.Plugin interface
var _ cloudprovider.Plugin = &AutoNLBsV3Plugin{}
