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
	"strings"
	"sync"

	nlbv1 "github.com/chrisliu1995/AlibabaCloud-NLB-Operator/pkg/apis/nlboperator/v1"
	eipv1 "github.com/chrisliu1995/alibabacloud-eip-operator/api/v1alpha1"
	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/cloudprovider"
	cperrors "github.com/openkruise/kruise-game/cloudprovider/errors"
	"github.com/openkruise/kruise-game/cloudprovider/utils"
	"github.com/openkruise/kruise-game/pkg/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	log "k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	AutoNLBsV2Network = "AlibabaCloud-AutoNLBs-V2"

	// 配置参数常量
	RetainNLBOnDeleteConfigName = "RetainNLBOnDelete" // 是否在 GSS 删除时保留 NLB 和 EIP（默认 true）

	// Finalizer 常量（用于 RetainNLBOnDelete=false 场景）
	NLBFinalizerName = "game.kruise.io/nlb-cascade-delete" // NLB Finalizer，确保 Service 删除后再删除 NLB
	PodFinalizerName = "game.kruise.io/pod-cascade-delete" // Pod Finalizer，确保 Service 删除后再删除 Pod

	// NLB CRD 相关标签
	NLBPoolLabel           = "game.kruise.io/nlb-pool"
	NLBPoolIndexLabel      = "game.kruise.io/nlb-pool-index"
	NLBPoolEipIspTypeLabel = "game.kruise.io/nlb-pool-eip-isp-type"
	NLBPoolGssLabel        = "game.kruise.io/nlb-pool-gss"

	// EIP CRD 相关标签
	EIPPoolLabel           = "game.kruise.io/eip-pool"
	EIPPoolIndexLabel      = "game.kruise.io/eip-pool-index"
	EIPPoolEipIspTypeLabel = "game.kruise.io/eip-pool-eip-isp-type"
	EIPPoolGssLabel        = "game.kruise.io/eip-pool-gss"

	// NLB CRD GVK
	NLBAPIVersion = "nlboperator.alibabacloud.com/v1"
	NLBKind       = "NLB"

	// EIP CRD GVK
	EIPAPIVersion = "eip.alibabacloud.com/v1alpha1"
	EIPKind       = "EIP"
)

type AutoNLBsV2Plugin struct {
	// maxPodIndex 记录每个 GSS 的历史最大 Pod Index（只增不减）
	// 用于计算需要预热的 NLB/EIP 数量
	maxPodIndex map[string]int // "namespace/gssName" -> maxIndex
	mutex       sync.RWMutex   // 只保护 maxPodIndex 的读写
}

func (a *AutoNLBsV2Plugin) Name() string {
	return AutoNLBsV2Network
}

func (a *AutoNLBsV2Plugin) Alias() string {
	return AliasAutoNLBs
}

func (a *AutoNLBsV2Plugin) Init(c client.Client, options cloudprovider.CloudProviderOptions, ctx context.Context) error {
	log.Infof("[%s] Plugin initializing with resource prewarming...", AutoNLBsV2Network)

	gssList := &gamekruiseiov1alpha1.GameServerSetList{}
	err := c.List(ctx, gssList, &client.ListOptions{})
	if err != nil {
		log.Errorf("[%s] Failed to list GameServerSets: %v", AutoNLBsV2Network, err)
		return err
	}

	for _, gss := range gssList.Items {
		if gss.Spec.Network != nil && gss.Spec.Network.NetworkType == AutoNLBsV2Network {
			gssKey := gss.GetNamespace() + "/" + gss.GetName()

			// 初始化 maxPodIndex 为当前 Replicas
			a.mutex.Lock()
			a.maxPodIndex[gssKey] = int(*gss.Spec.Replicas)
			a.mutex.Unlock()

			log.Infof("[%s] Initializing GSS %s with maxPodIndex=%d", AutoNLBsV2Network, gssKey, int(*gss.Spec.Replicas))

			// 解析配置
			conf, err := parseAutoNLBsConfig(gss.Spec.Network.NetworkConf)
			if err != nil {
				log.Errorf("[%s] Failed to parse config for GSS %s: %v", AutoNLBsV2Network, gssKey, err)
				continue // 继续处理下一个 GSS
			}

			// 同步预热资源（确保启动后资源就绪）
			if err := a.ensurePrewarming(ctx, c, gss.GetNamespace(), gss.GetName(), conf); err != nil {
				log.Errorf("[%s] Failed to prewarm resources for GSS %s: %v", AutoNLBsV2Network, gssKey, err)
				// 不中断启动，继续处理下一个 GSS
			}
		}
	}

	log.Infof("[%s] Plugin initialization completed", AutoNLBsV2Network)
	return nil
}

func (a *AutoNLBsV2Plugin) OnPodAdded(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	log.Infof("[%s] OnPodAdded called for pod %s/%s", AutoNLBsV2Network, pod.GetNamespace(), pod.GetName())

	// 更新 maxPodIndex（只增不减）
	a.updateMaxPodIndex(pod)

	// 解析配置
	networkManager := utils.NewNetworkManager(pod, c)
	networkConfig := networkManager.GetNetworkConfig()
	if networkConfig == nil {
		return pod, nil // 没有网络配置，不需要添加 Finalizer
	}

	conf, err := parseAutoNLBsConfig(networkConfig)
	if err != nil {
		log.Errorf("[%s] Failed to parse config for pod %s/%s: %v", AutoNLBsV2Network, pod.GetNamespace(), pod.GetName(), err)
		return pod, nil // 不阻止 Pod 创建
	}

	// 如果 RetainNLBOnDelete=false，返回添加了 Finalizer 的 Pod，由 webhook 负责注入
	if !conf.retainNLBOnDelete {
		if !controllerutil.ContainsFinalizer(pod, PodFinalizerName) {
			controllerutil.AddFinalizer(pod, PodFinalizerName)
			log.Infof("[%s] Added finalizer %s to pod %s/%s (will be injected by webhook)", AutoNLBsV2Network, PodFinalizerName, pod.GetNamespace(), pod.GetName())
		}
	}

	return pod, nil
}

func (a *AutoNLBsV2Plugin) OnPodUpdated(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	log.Infof("[%s] OnPodUpdated called for pod %s/%s", AutoNLBsV2Network, pod.GetNamespace(), pod.GetName())
	networkManager := utils.NewNetworkManager(pod, c)
	networkStatus, _ := networkManager.GetNetworkStatus()
	networkConfig := networkManager.GetNetworkConfig()

	conf, err := parseAutoNLBsConfig(networkConfig)
	if err != nil {
		log.Errorf("[%s] Failed to parse config for pod %s/%s: %v", AutoNLBsV2Network, pod.GetNamespace(), pod.GetName(), err)
		return pod, cperrors.NewPluginErrorWithMessage(cperrors.ParameterError, err.Error())
	}

	gssName := pod.GetLabels()[gamekruiseiov1alpha1.GameServerOwnerGssKey]
	podIndex := util.GetIndexFromGsName(pod.GetName())

	// 更新 maxPodIndex（只增不减）
	a.updateMaxPodIndex(pod)

	// 兜底：确保资源预热充足（幂等操作，多次调用无害）
	if err := a.ensurePrewarming(ctx, c, pod.GetNamespace(), gssName, conf); err != nil {
		log.Errorf("[%s] Failed to ensure prewarming for pod %s/%s: %v", AutoNLBsV2Network, pod.GetNamespace(), pod.GetName(), err)
		// 不返回错误，继续处理，避免阻塞 Pod 更新
	}

	if networkStatus == nil {
		pod, err := networkManager.UpdateNetworkStatus(gamekruiseiov1alpha1.NetworkStatus{
			CurrentNetworkState: gamekruiseiov1alpha1.NetworkNotReady,
		}, pod)
		return pod, cperrors.ToPluginError(err, cperrors.InternalError)
	}

	// 计算 Pod 对应的 NLB 索引（无状态计算）
	podsPerNLB := calculatePodsPerNLB(conf)
	if podsPerNLB <= 0 {
		log.Errorf("[%s] Invalid config for pod %s/%s: podsPerNLB=%d (minPort=%d, maxPort=%d, targetPorts=%d)",
			AutoNLBsV2Network, pod.GetNamespace(), pod.GetName(), podsPerNLB, conf.minPort, conf.maxPort, len(conf.targetPorts))
		return pod, cperrors.NewPluginErrorWithMessage(cperrors.ParameterError,
			"invalid config: port range is too small for the number of target ports")
	}
	nlbIndex := podIndex / podsPerNLB

	allServicesReady := true
	internalAddresses := make([]gamekruiseiov1alpha1.NetworkAddress, 0)
	externalAddresses := make([]gamekruiseiov1alpha1.NetworkAddress, 0)

	// 遍历每个 eipIspType
	for _, eipIspType := range conf.eipIspTypes {
		// 构造 NLB 名称（基于规律）
		nlbName := gssName + "-" + strings.ToLower(eipIspType) + "-" + strconv.Itoa(nlbIndex)

		// 直接 Get NLB CR（O(1) 复杂度）
		nlbCR := &nlbv1.NLB{}
		err := c.Get(ctx, types.NamespacedName{
			Name:      nlbName,
			Namespace: pod.GetNamespace(),
		}, nlbCR)

		if errors.IsNotFound(err) {
			// NLB 不存在，确保 NLB 和 EIP 被创建
			log.Infof("[%s] NLB %s not found, creating for pod %s/%s",
				AutoNLBsV2Network, nlbName, pod.GetNamespace(), pod.GetName())
			if err := a.ensureNLBForPod(ctx, c, pod.GetNamespace(), gssName, eipIspType, nlbIndex, conf); err != nil {
				log.Errorf("[%s] Failed to ensure NLB %s: %v", AutoNLBsV2Network, nlbName, err)
				allServicesReady = false
				continue
			}
			// 重新获取 NLB CR
			err = c.Get(ctx, types.NamespacedName{
				Name:      nlbName,
				Namespace: pod.GetNamespace(),
			}, nlbCR)
			if err != nil {
				log.Errorf("[%s] Failed to get NLB %s after creation: %v", AutoNLBsV2Network, nlbName, err)
				allServicesReady = false
				continue
			}
		} else if err != nil {
			log.Errorf("[%s] Failed to get NLB %s: %v", AutoNLBsV2Network, nlbName, err)
			return pod, cperrors.NewPluginErrorWithMessage(cperrors.ApiCallError, err.Error())
		}

		// 检查 NLB 状态
		if nlbCR.Status.LoadBalancerId == "" {
			log.Infof("[%s] NLB %s not ready yet (no LoadBalancerId)", AutoNLBsV2Network, nlbName)
			allServicesReady = false
			continue
		}

		nlbId := nlbCR.Status.LoadBalancerId
		log.Infof("[%s] NLB %s found with ID: %s", AutoNLBsV2Network, nlbName, nlbId)

		// 构造 Service 名称（基于规律）
		svcName := pod.GetName() + "-" + strings.ToLower(eipIspType)

		// 直接 Get Service（O(1) 复杂度）
		svc := &corev1.Service{}
		err = c.Get(ctx, types.NamespacedName{
			Name:      svcName,
			Namespace: pod.GetNamespace(),
		}, svc)

		if errors.IsNotFound(err) {
			// Service 不存在，创建
			log.Infof("[%s] Service %s not found, creating for pod %s/%s",
				AutoNLBsV2Network, svcName, pod.GetNamespace(), pod.GetName())
			if err := a.ensureServiceForPod(ctx, c, pod, gssName, eipIspType, nlbIndex, nlbId, conf); err != nil {
				log.Errorf("[%s] Failed to create Service %s: %v", AutoNLBsV2Network, svcName, err)
				allServicesReady = false
				continue
			}
			// 重新获取 Service
			err = c.Get(ctx, types.NamespacedName{
				Name:      svcName,
				Namespace: pod.GetNamespace(),
			}, svc)
			if err != nil {
				log.Errorf("[%s] Failed to get Service %s after creation: %v", AutoNLBsV2Network, svcName, err)
				allServicesReady = false
				continue
			}
		} else if err != nil {
			log.Errorf("[%s] Failed to get Service %s: %v", AutoNLBsV2Network, svcName, err)
			return pod, cperrors.NewPluginErrorWithMessage(cperrors.ApiCallError, err.Error())
		}

		// 禁用网络
		if networkManager.GetNetworkDisabled() && svc.Spec.Type == corev1.ServiceTypeLoadBalancer {
			svc.Spec.Type = corev1.ServiceTypeClusterIP
			if err := c.Update(ctx, svc); err != nil {
				return pod, cperrors.ToPluginError(err, cperrors.ApiCallError)
			}
			continue
		}

		// 启用网络
		if !networkManager.GetNetworkDisabled() && svc.Spec.Type == corev1.ServiceTypeClusterIP {
			svc.Spec.Type = corev1.ServiceTypeLoadBalancer
			if err := c.Update(ctx, svc); err != nil {
				return pod, cperrors.ToPluginError(err, cperrors.ApiCallError)
			}
			continue
		}

		// 网络未就绪
		if len(svc.Status.LoadBalancer.Ingress) == 0 {
			log.Infof("[%s] Service %s LoadBalancer not ready yet", AutoNLBsV2Network, svcName)
			allServicesReady = false
			continue
		}

		// allow not ready containers
		if util.IsAllowNotReadyContainers(networkManager.GetNetworkConfig()) {
			toUpDateSvc, err := utils.AllowNotReadyContainers(c, ctx, pod, svc, false)
			if err != nil {
				return pod, err
			}

			if toUpDateSvc {
				if err := c.Update(ctx, svc); err != nil {
					return pod, cperrors.ToPluginError(err, cperrors.ApiCallError)
				}
			}
		}

		// 收集该 Service 的网络地址信息
		endpointWithType := svc.Status.LoadBalancer.Ingress[0].Hostname + "/" + eipIspType

		for _, port := range svc.Spec.Ports {
			instrIPort := port.TargetPort
			instrEPort := intstr.FromInt(int(port.Port))
			internalAddress := gamekruiseiov1alpha1.NetworkAddress{
				IP: pod.Status.PodIP,
				Ports: []gamekruiseiov1alpha1.NetworkPort{
					{
						Name:     instrIPort.String(),
						Port:     &instrIPort,
						Protocol: port.Protocol,
					},
				},
			}
			externalAddress := gamekruiseiov1alpha1.NetworkAddress{
				EndPoint: endpointWithType,
				IP:       svc.Status.LoadBalancer.Ingress[0].IP,
				Ports: []gamekruiseiov1alpha1.NetworkPort{
					{
						Name:     instrIPort.String(),
						Port:     &instrEPort,
						Protocol: port.Protocol,
					},
				},
			}
			internalAddresses = append(internalAddresses, internalAddress)
			externalAddresses = append(externalAddresses, externalAddress)
		}
	}

	// 检查所有 Service 是否就绪
	if !allServicesReady {
		log.Infof("[%s] Not all Services ready for pod %s/%s", AutoNLBsV2Network, pod.GetNamespace(), pod.GetName())
		networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkNotReady
		pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
		return pod, cperrors.ToPluginError(err, cperrors.InternalError)
	}

	// 所有 Service 就绪，更新网络状态
	networkStatus.InternalAddresses = internalAddresses
	networkStatus.ExternalAddresses = externalAddresses
	networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkReady
	pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
	return pod, cperrors.ToPluginError(err, cperrors.InternalError)
}

func (a *AutoNLBsV2Plugin) OnPodDeleted(c client.Client, pod *corev1.Pod, ctx context.Context) cperrors.PluginError {
	log.Infof("[%s] OnPodDeleted called for pod %s/%s", AutoNLBsV2Network, pod.GetNamespace(), pod.GetName())

	// 解析配置
	networkManager := utils.NewNetworkManager(pod, c)
	if networkManager == nil {
		log.Infof("[%s] No network manager for pod %s/%s, skip cleanup", AutoNLBsV2Network, pod.GetNamespace(), pod.GetName())
		return nil
	}
	networkConfig := networkManager.GetNetworkConfig()
	if networkConfig == nil {
		log.Infof("[%s] No network config for pod %s/%s, skip cleanup", AutoNLBsV2Network, pod.GetNamespace(), pod.GetName())
		return nil
	}

	conf, err := parseAutoNLBsConfig(networkConfig)
	if err != nil {
		log.Errorf("[%s] Failed to parse config for pod %s/%s: %v", AutoNLBsV2Network, pod.GetNamespace(), pod.GetName(), err)
		return nil
	}

	// RetainNLBOnDelete=true 时，不需要做任何处理
	if conf.retainNLBOnDelete {
		log.Infof("[%s] RetainNLBOnDelete=true, no cleanup needed for pod %s/%s", AutoNLBsV2Network, pod.GetNamespace(), pod.GetName())
		return nil
	}

	// 检查 Pod 是否有 Finalizer
	if !controllerutil.ContainsFinalizer(pod, PodFinalizerName) {
		log.Infof("[%s] Pod %s/%s has no finalizer, skip cleanup", AutoNLBsV2Network, pod.GetNamespace(), pod.GetName())
		return nil
	}

	gssName := pod.GetLabels()[gamekruiseiov1alpha1.GameServerOwnerGssKey]

	// 检查 GSS 是否在删除
	gss := &gamekruiseiov1alpha1.GameServerSet{}
	err = c.Get(ctx, types.NamespacedName{
		Namespace: pod.GetNamespace(),
		Name:      gssName,
	}, gss)

	gssDeleting := false
	if err != nil {
		if errors.IsNotFound(err) {
			// GSS 已经不存在，说明已经删除
			gssDeleting = true
		} else {
			log.Errorf("[%s] Failed to get GSS %s/%s: %v", AutoNLBsV2Network, pod.GetNamespace(), gssName, err)
			return cperrors.ToPluginError(err, cperrors.ApiCallError)
		}
	} else {
		gssDeleting = gss.DeletionTimestamp != nil
	}

	// 如果 GSS 没在删除，说明只是 Pod 重建，直接移除 Finalizer
	if !gssDeleting {
		log.Infof("[%s] GSS %s/%s is not deleting, pod %s is just being recreated, removing finalizer",
			AutoNLBsV2Network, pod.GetNamespace(), gssName, pod.GetName())
		controllerutil.RemoveFinalizer(pod, PodFinalizerName)
		if err := c.Update(ctx, pod); err != nil {
			log.Errorf("[%s] Failed to remove finalizer from pod %s/%s: %v", AutoNLBsV2Network, pod.GetNamespace(), pod.GetName(), err)
			return cperrors.ToPluginError(err, cperrors.ApiCallError)
		}
		return nil
	}

	// GSS 正在删除，需要处理级联删除逻辑
	log.Infof("[%s] GSS %s/%s is deleting, handling cascade delete for pod %s",
		AutoNLBsV2Network, pod.GetNamespace(), gssName, pod.GetName())

	// 检查当前 Pod 的 Service 是否已删除
	allServicesDeleted := true
	for _, eipIspType := range conf.eipIspTypes {
		svcName := pod.GetName() + "-" + strings.ToLower(eipIspType)
		svc := &corev1.Service{}
		err := c.Get(ctx, types.NamespacedName{
			Name:      svcName,
			Namespace: pod.GetNamespace(),
		}, svc)
		if err == nil {
			// Service 还存在
			allServicesDeleted = false
			log.Infof("[%s] Service %s/%s still exists, waiting for deletion", AutoNLBsV2Network, pod.GetNamespace(), svcName)
			break
		} else if !errors.IsNotFound(err) {
			log.Errorf("[%s] Failed to get Service %s/%s: %v", AutoNLBsV2Network, pod.GetNamespace(), svcName, err)
		}
	}

	// 如果 Service 还没删完，等待下次调谐
	if !allServicesDeleted {
		log.Infof("[%s] Services for pod %s/%s not fully deleted, will retry later", AutoNLBsV2Network, pod.GetNamespace(), pod.GetName())
		return cperrors.NewPluginErrorWithMessage(cperrors.InternalError, "waiting for services to be deleted")
	}

	// 当前 Pod 的 Service 已删除，移除 Pod Finalizer
	log.Infof("[%s] All services for pod %s/%s deleted, removing pod finalizer", AutoNLBsV2Network, pod.GetNamespace(), pod.GetName())
	controllerutil.RemoveFinalizer(pod, PodFinalizerName)
	if err := c.Update(ctx, pod); err != nil {
		log.Errorf("[%s] Failed to remove finalizer from pod %s/%s: %v", AutoNLBsV2Network, pod.GetNamespace(), pod.GetName(), err)
		return cperrors.ToPluginError(err, cperrors.ApiCallError)
	}

	// 检查是否所有 Service 都删除了（用于移除 NLB Finalizer）
	// 通过查询属于该 GSS 的 Service 数量
	svcList := &corev1.ServiceList{}
	err = c.List(ctx, svcList, &client.ListOptions{
		Namespace: pod.GetNamespace(),
		LabelSelector: labels.SelectorFromSet(map[string]string{
			gamekruiseiov1alpha1.GameServerOwnerGssKey: gssName,
		}),
	})
	if err != nil {
		log.Errorf("[%s] Failed to list services for GSS %s/%s: %v", AutoNLBsV2Network, pod.GetNamespace(), gssName, err)
		return nil // 不阻止 Pod 删除
	}

	if len(svcList.Items) > 0 {
		log.Infof("[%s] GSS %s/%s still has %d services, NLB finalizers will be removed later",
			AutoNLBsV2Network, pod.GetNamespace(), gssName, len(svcList.Items))
		return nil
	}

	// 所有 Service 都删除了，移除所有 NLB 的 Finalizer
	log.Infof("[%s] All services for GSS %s/%s deleted, removing NLB finalizers", AutoNLBsV2Network, pod.GetNamespace(), gssName)
	if err := a.removeNLBFinalizers(ctx, c, pod.GetNamespace(), gssName, conf); err != nil {
		log.Errorf("[%s] Failed to remove NLB finalizers: %v", AutoNLBsV2Network, err)
		// 不阻止 Pod 删除
	}

	return nil
}

// removeNLBFinalizers 移除属于指定 GSS 的所有 NLB 的 Finalizer
func (a *AutoNLBsV2Plugin) removeNLBFinalizers(ctx context.Context, c client.Client, namespace, gssName string, config *autoNLBsConfig) error {
	for _, eipIspType := range config.eipIspTypes {
		// 查询属于该 GSS 的 NLB
		nlbList := &nlbv1.NLBList{}
		err := c.List(ctx, nlbList, &client.ListOptions{
			Namespace: namespace,
			LabelSelector: labels.SelectorFromSet(map[string]string{
				NLBPoolGssLabel:        gssName,
				NLBPoolEipIspTypeLabel: eipIspType,
			}),
		})
		if err != nil {
			log.Errorf("[%s] Failed to list NLBs for GSS %s/%s: %v", AutoNLBsV2Network, namespace, gssName, err)
			continue
		}

		for _, nlb := range nlbList.Items {
			// 只处理正在删除的 NLB
			if nlb.DeletionTimestamp == nil {
				continue
			}

			if controllerutil.ContainsFinalizer(&nlb, NLBFinalizerName) {
				log.Infof("[%s] Removing finalizer from NLB %s/%s", AutoNLBsV2Network, namespace, nlb.Name)
				controllerutil.RemoveFinalizer(&nlb, NLBFinalizerName)
				if err := c.Update(ctx, &nlb); err != nil {
					log.Errorf("[%s] Failed to remove finalizer from NLB %s/%s: %v", AutoNLBsV2Network, namespace, nlb.Name, err)
					continue
				}
				log.Infof("[%s] Successfully removed finalizer from NLB %s/%s", AutoNLBsV2Network, namespace, nlb.Name)
			}
		}
	}
	return nil
}

func init() {
	autoNLBsV2Plugin := AutoNLBsV2Plugin{
		maxPodIndex: make(map[string]int),
		mutex:       sync.RWMutex{},
	}
	alibabaCloudProvider.registerPlugin(&autoNLBsV2Plugin)
}

// updateMaxPodIndex 更新 Pod 最大索引（只增不减）
func (a *AutoNLBsV2Plugin) updateMaxPodIndex(pod *corev1.Pod) {
	podIndex := util.GetIndexFromGsName(pod.GetName())
	gssKey := pod.GetNamespace() + "/" + pod.GetLabels()[gamekruiseiov1alpha1.GameServerOwnerGssKey]

	a.mutex.Lock()
	defer a.mutex.Unlock()

	if podIndex > a.maxPodIndex[gssKey] {
		log.Infof("[%s] Updating maxPodIndex for %s: %d -> %d", AutoNLBsV2Network, gssKey, a.maxPodIndex[gssKey], podIndex)
		a.maxPodIndex[gssKey] = podIndex
	}
}

// calculatePodsPerNLB 计算每个 NLB 可以支持的 Pod 数量
// 返回值 <= 0 表示配置错误（端口范围不足）
func calculatePodsPerNLB(config *autoNLBsConfig) int {
	lenRange := int(config.maxPort) - int(config.minPort) - len(config.blockPorts) + 1
	if lenRange <= 0 || len(config.targetPorts) == 0 {
		return 0
	}
	podsPerNLB := lenRange / len(config.targetPorts)
	return podsPerNLB
}

// calculateExpectNLBNum 计算期望的 NLB 实例数量
func (a *AutoNLBsV2Plugin) calculateExpectNLBNum(namespace, gssName string, config *autoNLBsConfig) int {
	a.mutex.RLock()
	maxIndex := a.maxPodIndex[namespace+"/"+gssName]
	a.mutex.RUnlock()

	podsPerNLB := calculatePodsPerNLB(config)
	if podsPerNLB <= 0 {
		log.Errorf("[%s] Invalid config: podsPerNLB=%d (minPort=%d, maxPort=%d, targetPorts=%d, blockPorts=%d)",
			AutoNLBsV2Network, podsPerNLB, config.minPort, config.maxPort, len(config.targetPorts), len(config.blockPorts))
		return 1 // 返回最小值，避免后续除零错误
	}

	// maxIndex 是最大的 Pod 索引，需要计算能容纳的 NLB 数量
	// 加上预留的 NLB 数量
	expectNLBNum := (maxIndex / podsPerNLB) + config.reserveNlbNum + 1
	if expectNLBNum < 1 {
		expectNLBNum = 1
	}

	return expectNLBNum
}

// ensurePrewarming 确保资源预热充足（NLB/EIP/Service）
// 幂等操作，支持并发调用
func (a *AutoNLBsV2Plugin) ensurePrewarming(ctx context.Context, c client.Client, namespace, gssName string, config *autoNLBsConfig) error {
	expectNLBNum := a.calculateExpectNLBNum(namespace, gssName, config)
	log.Infof("[%s] ensurePrewarming for %s/%s, expectNLBNum=%d", AutoNLBsV2Network, namespace, gssName, expectNLBNum)

	// 获取 GameServerSet（用于创建 Service）
	gss := &gamekruiseiov1alpha1.GameServerSet{}
	err := c.Get(ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      gssName,
	}, gss)
	if err != nil {
		log.Errorf("[%s] Failed to get GameServerSet %s/%s: %v", AutoNLBsV2Network, namespace, gssName, err)
		return err
	}

	for _, eipIspType := range config.eipIspTypes {
		// 查询已有的 NLB CR（无需锁，直接查询 K8s）
		nlbList := &nlbv1.NLBList{}
		err := c.List(ctx, nlbList, &client.ListOptions{
			Namespace: namespace,
			LabelSelector: labels.SelectorFromSet(map[string]string{
				NLBPoolGssLabel:        gssName,
				NLBPoolEipIspTypeLabel: eipIspType,
			}),
		})
		if err != nil {
			log.Errorf("[%s] Failed to list NLB CRs: %v", AutoNLBsV2Network, err)
			return err
		}

		existingCount := len(nlbList.Items)
		log.Infof("[%s] GSS %s/%s (eipIspType=%s): existing NLBs=%d, expect=%d",
			AutoNLBsV2Network, namespace, gssName, eipIspType, existingCount, expectNLBNum)

		// 创建缺失的 NLB 和 EIP
		for i := existingCount; i < expectNLBNum; i++ {
			log.Infof("[%s] Creating NLB instance %d for %s/%s (eipIspType=%s)",
				AutoNLBsV2Network, i, namespace, gssName, eipIspType)

			// 创建 EIP CR（为每个 zone）
			if err := a.ensureEIPsForNLB(ctx, c, namespace, gssName, eipIspType, i, config, gss); err != nil {
				log.Errorf("[%s] Failed to ensure EIPs for NLB %d: %v", AutoNLBsV2Network, i, err)
				// 继续创建下一个
			}

			// 创建 NLB CR（幂等）
			if err := a.createNLBInstanceCR(ctx, c, namespace, gssName, eipIspType, i, config, gss); err != nil {
				log.Errorf("[%s] Failed to create NLB instance %d: %v", AutoNLBsV2Network, i, err)
				// 继续创建下一个
			}
		}

		// 预热 Service（为每个 NLB 预创建所有可能的 Service）
		if err := a.prewarmServices(ctx, c, namespace, gssName, eipIspType, nlbList.Items, config, gss); err != nil {
			log.Errorf("[%s] Failed to prewarm services: %v", AutoNLBsV2Network, err)
			// 不返回错误，允许下次重试
		}
	}

	log.Infof("[%s] ensurePrewarming completed for %s/%s", AutoNLBsV2Network, namespace, gssName)
	return nil
}

// ensureEIPsForNLB 为指定的 NLB 实例创建所有 zone 的 EIP CR
func (a *AutoNLBsV2Plugin) ensureEIPsForNLB(ctx context.Context, c client.Client, namespace, gssName, eipIspType string, nlbIndex int, config *autoNLBsConfig, gss *gamekruiseiov1alpha1.GameServerSet) error {
	// 解析 ZoneMaps，获取 zone 数量
	zoneMappings, _, err := parseZoneMaps(config.zoneMaps)
	if err != nil {
		log.Errorf("[%s] Failed to parse zoneMaps: %v", AutoNLBsV2Network, err)
		return err
	}

	// 为每个 zone 创建 EIP CR
	for zoneIdx := range zoneMappings {
		_, err := a.ensureEIPCR(ctx, c, namespace, gssName, eipIspType, nlbIndex, zoneIdx, config, gss)
		if err != nil {
			log.Errorf("[%s] Failed to ensure EIP CR for NLB %d zone %d: %v",
				AutoNLBsV2Network, nlbIndex, zoneIdx, err)
			// 继续创建其他 EIP
		}
	}

	return nil
}

// prewarmServices 预热 Service（为每个 NLB 预创建所有可能的 Service）
func (a *AutoNLBsV2Plugin) prewarmServices(ctx context.Context, c client.Client, namespace, gssName, eipIspType string, nlbs []nlbv1.NLB, config *autoNLBsConfig, gss *gamekruiseiov1alpha1.GameServerSet) error {
	podsPerNLB := calculatePodsPerNLB(config)
	if podsPerNLB <= 0 {
		log.Errorf("[%s] Invalid config for prewarmServices: podsPerNLB=%d, skip prewarming",
			AutoNLBsV2Network, podsPerNLB)
		return nil // 配置错误，跳过预热
	}

	log.Infof("[%s] prewarmServices for %s/%s (eipIspType=%s, nlbs=%d, podsPerNLB=%d)",
		AutoNLBsV2Network, namespace, gssName, eipIspType, len(nlbs), podsPerNLB)

	createdCount := 0
	skippedCount := 0
	skippedNLBCount := 0

	for nlbIdx, nlb := range nlbs {
		// 如果 NLB 还没有 LoadBalancerId，跳过（等待 NLB Operator 创建完成）
		if nlb.Status.LoadBalancerId == "" {
			log.Infof("[%s] NLB %s (index=%d) not ready yet, skip prewarming services",
				AutoNLBsV2Network, nlb.Name, nlbIdx)
			skippedNLBCount++
			continue
		}

		nlbId := nlb.Status.LoadBalancerId
		log.Infof("[%s] Prewarming services for NLB %s (nlbId=%s)", AutoNLBsV2Network, nlb.Name, nlbId)

		// 为这个 NLB 预创建所有可能的 Service
		for podIdx := 0; podIdx < podsPerNLB; podIdx++ {
			// 计算全局 Service 索引（对应未来可能的 Pod 索引）
			globalServiceIndex := nlbIdx*podsPerNLB + podIdx

			// Service 命名：podName-eipIspType（如：gss-0-bgp, gss-0-bgp-pro）
			basePodName := gssName + "-" + strconv.Itoa(globalServiceIndex)
			svcName := basePodName + "-" + strings.ToLower(eipIspType)

			// 检查 Service 是否已存在
			svc := &corev1.Service{}
			err := c.Get(ctx, types.NamespacedName{
				Name:      svcName,
				Namespace: namespace,
			}, svc)

			if err == nil {
				// Service 已存在
				skippedCount++
				continue
			}

			if !errors.IsNotFound(err) {
				log.Errorf("[%s] Failed to get Service %s/%s: %v", AutoNLBsV2Network, namespace, svcName, err)
				continue
			}

			// 分配端口
			ports := make([]int32, len(config.targetPorts))
			basePort := config.minPort
			for i := 0; i < len(config.targetPorts); i++ {
				portOffset := int32(podIdx*len(config.targetPorts) + i)
				port := basePort + portOffset

				// 跳过阻塞端口
				for util.IsNumInListInt32(port, config.blockPorts) {
					portOffset++
					port = basePort + portOffset
				}

				ports[i] = port
			}

			// 创建 Service（预热：提前创建好，Pod 创建时直接关联）
			toCreateSvc := a.consServiceForPod(namespace, svcName, basePodName, gssName, nlbId, ports, config, gss)
			if err := c.Create(ctx, toCreateSvc); err != nil {
				if !errors.IsAlreadyExists(err) {
					log.Errorf("[%s] Failed to create Service %s/%s: %v",
						AutoNLBsV2Network, namespace, svcName, err)
					continue
				}
				skippedCount++
			} else {
				createdCount++
			}
		}
	}

	log.Infof("[%s] prewarmServices completed: created=%d, skipped=%d, nlbNotReady=%d",
		AutoNLBsV2Network, createdCount, skippedCount, skippedNLBCount)
	return nil
}

// ensureNLBForPod 为指定 Pod 确保 NLB 及其依赖的 EIP 资源存在
func (a *AutoNLBsV2Plugin) ensureNLBForPod(ctx context.Context, c client.Client, namespace, gssName, eipIspType string, nlbIndex int, config *autoNLBsConfig) error {
	log.Infof("[%s] ensureNLBForPod: namespace=%s, gssName=%s, eipIspType=%s, nlbIndex=%d",
		AutoNLBsV2Network, namespace, gssName, eipIspType, nlbIndex)

	// 获取 GameServerSet
	gss := &gamekruiseiov1alpha1.GameServerSet{}
	err := c.Get(ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      gssName,
	}, gss)
	if err != nil {
		log.Errorf("[%s] Failed to get GameServerSet %s/%s: %v", AutoNLBsV2Network, namespace, gssName, err)
		return err
	}

	// 解析 ZoneMaps，获取 zone 数量
	zoneMappings, _, err := parseZoneMaps(config.zoneMaps)
	if err != nil {
		log.Errorf("[%s] Failed to parse zoneMaps: %v", AutoNLBsV2Network, err)
		return err
	}

	// 步骤1: 确保所有 zone 的 EIP CR 存在
	for zoneIdx := range zoneMappings {
		_, err := a.ensureEIPCR(ctx, c, namespace, gssName, eipIspType, nlbIndex, zoneIdx, config, gss)
		if err != nil {
			log.Errorf("[%s] Failed to ensure EIP CR for NLB %d zone %d: %v",
				AutoNLBsV2Network, nlbIndex, zoneIdx, err)
			// 继续创建其他 EIP
		}
	}

	// 步骤2: 创建 NLB CR（如果不存在）
	err = a.createNLBInstanceCR(ctx, c, namespace, gssName, eipIspType, nlbIndex, config, gss)
	return err
}

// ensureServiceForPod 为指定 Pod 创建 Service
func (a *AutoNLBsV2Plugin) ensureServiceForPod(ctx context.Context, c client.Client, pod *corev1.Pod, gssName, eipIspType string, nlbIndex int, nlbId string, config *autoNLBsConfig) error {
	svcName := pod.GetName() + "-" + strings.ToLower(eipIspType)
	log.Infof("[%s] ensureServiceForPod: creating Service %s for pod %s/%s",
		AutoNLBsV2Network, svcName, pod.GetNamespace(), pod.GetName())

	// 获取 GameServerSet
	gss := &gamekruiseiov1alpha1.GameServerSet{}
	err := c.Get(ctx, types.NamespacedName{
		Namespace: pod.GetNamespace(),
		Name:      gssName,
	}, gss)
	if err != nil {
		log.Errorf("[%s] Failed to get GameServerSet %s/%s: %v", AutoNLBsV2Network, pod.GetNamespace(), gssName, err)
		return err
	}

	// 计算端口分配
	podIndex := util.GetIndexFromGsName(pod.GetName())
	podsPerNLB := calculatePodsPerNLB(config)
	if podsPerNLB <= 0 {
		log.Errorf("[%s] Invalid config for ensureServiceForPod: podsPerNLB=%d",
			AutoNLBsV2Network, podsPerNLB)
		return fmt.Errorf("invalid config: port range is too small for the number of target ports")
	}
	podIndexInNLB := podIndex % podsPerNLB // Pod 在当前 NLB 中的相对索引

	ports := make([]int32, len(config.targetPorts))
	basePort := config.minPort
	for i := 0; i < len(config.targetPorts); i++ {
		portOffset := int32(podIndexInNLB*len(config.targetPorts) + i)
		port := basePort + portOffset

		// 跳过阻塞端口
		for util.IsNumInListInt32(port, config.blockPorts) {
			portOffset++
			port = basePort + portOffset
		}

		ports[i] = port
	}

	// 构造 Service
	toCreateSvc := a.consServiceForPod(pod.GetNamespace(), svcName, pod.GetName(), gssName, nlbId, ports, config, gss)
	if err := c.Create(ctx, toCreateSvc); err != nil {
		if !errors.IsAlreadyExists(err) {
			log.Errorf("[%s] Failed to create Service %s/%s: %v",
				AutoNLBsV2Network, pod.GetNamespace(), svcName, err)
			return err
		}
		log.Infof("[%s] Service %s/%s already exists", AutoNLBsV2Network, pod.GetNamespace(), svcName)
	}

	log.Infof("[%s] Service %s/%s created with ports %v",
		AutoNLBsV2Network, pod.GetNamespace(), svcName, ports)
	return nil
}

// consServiceForPod 构造单个 Pod 的 Service（OwnerReference 指向 GameServerSet）
func (a *AutoNLBsV2Plugin) consServiceForPod(namespace, svcName, podName, gssName, nlbId string, ports []int32, config *autoNLBsConfig, gss *gamekruiseiov1alpha1.GameServerSet) *corev1.Service {
	// 构造 Service Ports
	svcPorts := make([]corev1.ServicePort, 0)
	for i := 0; i < len(config.targetPorts); i++ {
		svcPorts = append(svcPorts, corev1.ServicePort{
			Name:       strconv.Itoa(config.targetPorts[i]),
			Port:       ports[i],
			Protocol:   config.protocols[i],
			TargetPort: intstr.FromInt(config.targetPorts[i]),
		})
	}

	loadBalancerClass := "alibabacloud.com/nlb"
	svcAnnotations := map[string]string{
		SlbListenerOverrideKey:         "true",
		SlbIdAnnotationKey:             nlbId,
		SlbConfigHashKey:               util.GetHash(config),
		LBHealthCheckFlagAnnotationKey: config.lBHealthCheckFlag,
	}

	if config.lBHealthCheckFlag == "on" {
		svcAnnotations[LBHealthCheckTypeAnnotationKey] = config.lBHealthCheckType
		svcAnnotations[LBHealthCheckConnectPortAnnotationKey] = config.lBHealthCheckConnectPort
		svcAnnotations[LBHealthCheckConnectTimeoutAnnotationKey] = config.lBHealthCheckConnectTimeout
		svcAnnotations[LBHealthCheckIntervalAnnotationKey] = config.lBHealthCheckInterval
		svcAnnotations[LBHealthyThresholdAnnotationKey] = config.lBHealthyThreshold
		svcAnnotations[LBUnhealthyThresholdAnnotationKey] = config.lBUnhealthyThreshold
		if config.lBHealthCheckType == "http" {
			svcAnnotations[LBHealthCheckDomainAnnotationKey] = config.lBHealthCheckDomain
			svcAnnotations[LBHealthCheckUriAnnotationKey] = config.lBHealthCheckUri
			svcAnnotations[LBHealthCheckMethodAnnotationKey] = config.lBHealthCheckMethod
		}
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        svcName,
			Namespace:   namespace,
			Annotations: svcAnnotations,
			Labels: map[string]string{
				ServiceProxyName: "dummy",
				gamekruiseiov1alpha1.GameServerOwnerGssKey: gssName,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         gss.APIVersion,
					Kind:               gss.Kind,
					Name:               gss.GetName(),
					UID:                gss.GetUID(),
					Controller:         ptr.To(true),
					BlockOwnerDeletion: ptr.To(true),
				},
			},
		},
		Spec: corev1.ServiceSpec{
			ExternalTrafficPolicy:         config.externalTrafficPolicy,
			Type:                          corev1.ServiceTypeLoadBalancer,
			AllocateLoadBalancerNodePorts: ptr.To(false), // 禁用 NodePort 分配，仅通过 LB 访问
			Selector: map[string]string{
				SvcSelectorKey: podName, // Service Selector 指向 Pod 名称
			},
			Ports:             svcPorts,
			LoadBalancerClass: &loadBalancerClass,
		},
	}
}

// ensureMaxPodIndex 更新 Pod 最大索引
// createNLBInstanceCR 创建一个 NLB 实例(使用 NLB CRD)
func (a *AutoNLBsV2Plugin) createNLBInstanceCR(ctx context.Context, c client.Client, namespace, gssName, eipIspType string, index int, config *autoNLBsConfig, gss *gamekruiseiov1alpha1.GameServerSet) error {
	// 将 eipIspType 转换为小写以符合 Kubernetes 资源命名规范
	nlbName := gssName + "-" + strings.ToLower(eipIspType) + "-" + strconv.Itoa(index)
	log.Infof("createNLBInstanceCR: nlbName=%s, namespace=%s, eipIspType=%s, index=%d",
		nlbName, namespace, eipIspType, index)

	// 解析 ZoneMaps，获取 VPC ID 和 ZoneMappings
	log.Infof("parsing zoneMaps: %s", config.zoneMaps)
	zoneMappings, vpcId, err := parseZoneMaps(config.zoneMaps)
	if err != nil {
		log.Errorf("[%s] Failed to parse zoneMaps '%s': %v", AutoNLBsV2Network, config.zoneMaps, err)
		return fmt.Errorf("failed to parse zoneMaps: %w", err)
	}
	log.Infof("parsed zoneMaps successfully: vpcId=%s, zones=%d", vpcId, len(zoneMappings))

	// 检查 NLB CR 是否已存在
	log.Infof("checking if NLB CR %s/%s exists", namespace, nlbName)
	nlbCR := &nlbv1.NLB{}
	err = c.Get(ctx, types.NamespacedName{
		Name:      nlbName,
		Namespace: namespace,
	}, nlbCR)

	if err == nil {
		// NLB CR 已存在，获取状态
		log.Infof("NLB CR %s/%s already exists, nlbId=%s, endpoint=%s, status=%s, ready=%v",
			namespace, nlbName, nlbCR.Status.LoadBalancerId, nlbCR.Status.DNSName, nlbCR.Status.LoadBalancerStatus, nlbCR.Status.LoadBalancerStatus == "Active")
		return nil
	}

	if !errors.IsNotFound(err) {
		log.Errorf("failed to get NLB CR %s/%s, %s", namespace, nlbName, err.Error())
		return err
	}

	log.Infof("NLB CR %s/%s not found, creating new one", namespace, nlbName)

	// 解析 ZoneMaps，获取 VPC ID 和 ZoneMappings
	zoneMappings, vpcId, err = parseZoneMaps(config.zoneMaps)
	if err != nil {
		log.Errorf("[%s] Failed to parse zoneMaps '%s': %v", AutoNLBsV2Network, config.zoneMaps, err)
		return fmt.Errorf("failed to parse zoneMaps: %w", err)
	}

	// 步骤1: 查询已创建的 EIP CR，获取 AllocationID 列表（不在这里创建）
	// 不管是 BGP 还是单线 ISP，都需要查询 EIP
	eipAllocationIDs := make([]string, 0)
	allEIPsReady := true

	// 查询每个 zone 对应的 EIP
	log.Infof("[%s] Querying EIPs for %d zones", AutoNLBsV2Network, len(zoneMappings))
	for zoneIdx := range zoneMappings {
		eipName := fmt.Sprintf("%s-eip-%s-%d-z%d", gssName, strings.ToLower(eipIspType), index, zoneIdx)
		eipCR := &eipv1.EIP{}
		err := c.Get(ctx, types.NamespacedName{
			Name:      eipName,
			Namespace: namespace,
		}, eipCR)

		if err == nil && eipCR.Status.AllocationID != "" {
			eipAllocationIDs = append(eipAllocationIDs, eipCR.Status.AllocationID)
			log.Infof("[%s] Found EIP %s with allocationID=%s", AutoNLBsV2Network, eipName, eipCR.Status.AllocationID)
		} else {
			// EIP 必须先有 AllocationID,NLB 才能创建
			allEIPsReady = false
			if err != nil {
				log.Warningf("[%s] EIP %s not found yet, cannot create NLB: %v", AutoNLBsV2Network, eipName, err)
			} else {
				log.Infof("[%s] EIP %s exists but AllocationID not ready yet, cannot create NLB", AutoNLBsV2Network, eipName)
			}
			break // 不需要继续查询其他 EIP
		}
	}

	// 步骤1.5: 检查是否所有 EIP 都已就绪
	if !allEIPsReady {
		// 如果 EIP 还未就绪,返回错误,等待下次调谐
		log.Warningf("[%s] Cannot create NLB %s/%s yet: waiting for all EIPs to be ready (all types require EIP AllocationID before NLB creation)",
			AutoNLBsV2Network, namespace, nlbName)
		return fmt.Errorf("waiting for EIP AllocationIDs: all types require all EIPs ready before NLB creation")
	}

	// 步骤2: 构建 ZoneMappings,为每个 zone 绑定对应的 EIP
	nlbZoneMappings := make([]nlbv1.ZoneMapping, 0)
	for i, zm := range zoneMappings {
		zmMap := zm.(map[string]interface{})
		zoneMapping := nlbv1.ZoneMapping{
			ZoneId:    zmMap["zoneId"].(string),
			VSwitchId: zmMap["vSwitchId"].(string),
		}
		// 为每个 zone 绑定对应的 EIP AllocationID
		// 到这里所有 EIP 必定已经就绪
		if i < len(eipAllocationIDs) {
			zoneMapping.AllocationId = eipAllocationIDs[i]
			log.Infof("[%s] Binding EIP %s to zone %s", AutoNLBsV2Network, eipAllocationIDs[i], zoneMapping.ZoneId)
		}
		nlbZoneMappings = append(nlbZoneMappings, zoneMapping)
	}

	// 步骤3: 创建 NLB CR
	addressType := "Internet"
	if strings.Contains(eipIspType, IntranetEIPType) {
		addressType = "Intranet"
	}

	nlbCR = &nlbv1.NLB{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nlbName,
			Namespace: namespace,
			Labels: map[string]string{
				NLBPoolLabel:           "true",
				NLBPoolIndexLabel:      strconv.Itoa(index),
				NLBPoolEipIspTypeLabel: eipIspType,
				NLBPoolGssLabel:        gssName,
			},
		},
		Spec: nlbv1.NLBSpec{
			LoadBalancerName: nlbName,
			AddressType:      addressType,
			AddressIpVersion: "ipv4",
			VpcId:            vpcId,
			ZoneMappings:     nlbZoneMappings,
		},
	}

	// 根据 RetainNLBOnDelete 配置决定是否设置 OwnerReference
	if !config.retainNLBOnDelete {
		// 如果不保留，设置 OwnerReference 指向 GSS，GSS 删除时 NLB 会级联删除
		nlbCR.OwnerReferences = []metav1.OwnerReference{
			{
				APIVersion:         gss.APIVersion,
				Kind:               gss.Kind,
				Name:               gss.GetName(),
				UID:                gss.GetUID(),
				Controller:         ptr.To(false), // NLB 不是由 GSS 控制器管理
				BlockOwnerDeletion: ptr.To(true),
			},
		}
		// 添加 Finalizer，确保 Service 删除后再删除 NLB
		nlbCR.Finalizers = []string{NLBFinalizerName}
		log.Infof("[%s] NLB CR %s will be deleted with GSS (RetainNLBOnDelete=false), added finalizer %s", AutoNLBsV2Network, nlbName, NLBFinalizerName)
	} else {
		// 保留模式：不设置 OwnerReference，允许 NLB 实例在 GSS 删除时保留
		// 实现 NLB 资源池复用，降低成本和创建时间
		log.Infof("[%s] NLB CR %s will be retained after GSS deletion (RetainNLBOnDelete=true)", AutoNLBsV2Network, nlbName)
	}

	// 创建 NLB CR
	log.Infof("creating NLB CR %s/%s with vpcId=%s, addressType=%s, zones=%d, eips=%d",
		namespace, nlbName, vpcId, addressType, len(nlbZoneMappings), len(eipAllocationIDs))
	err = c.Create(ctx, nlbCR)
	if err != nil {
		log.Errorf("[%s] Failed to create NLB CR %s/%s: %v", AutoNLBsV2Network, namespace, nlbName, err)
		return fmt.Errorf("failed to create NLB CR: %w", err)
	}

	log.Infof("[%s] Successfully created NLB CR %s/%s (vpcId=%s, addressType=%s, eips=%v)",
		AutoNLBsV2Network, namespace, nlbName, vpcId, addressType, eipAllocationIDs)
	return nil
}

// parseZoneMaps 解析 ZoneMaps 配置
// 格式: "vpc-id@cn-hangzhou-h:vsw-xxx,cn-hangzhou-i:vsw-yyy" (必须带 VPC ID)
// 返回: zoneMappings 数组和 vpcId
func parseZoneMaps(zoneMapsStr string) ([]interface{}, string, error) {
	if zoneMapsStr == "" {
		return nil, "", fmt.Errorf("zoneMaps cannot be empty")
	}

	zoneMappings := make([]interface{}, 0)
	vpcId := ""

	// 检查是否包含 VPC ID (格式: vpc-xxx@zone:vsw,...)
	if !strings.Contains(zoneMapsStr, "@") {
		return nil, "", fmt.Errorf("zoneMaps must include VPC ID in format 'vpc-id@zone:vsw,...', got: %s", zoneMapsStr)
	}

	parts := strings.SplitN(zoneMapsStr, "@", 2)
	if len(parts) != 2 {
		return nil, "", fmt.Errorf("invalid zoneMaps format, expected 'vpc-id@zone:vsw,...', got: %s", zoneMapsStr)
	}

	vpcId = strings.TrimSpace(parts[0])
	if vpcId == "" {
		return nil, "", fmt.Errorf("VPC ID cannot be empty in zoneMaps")
	}

	zoneMapsStr = parts[1]

	// 解析格式: "cn-hangzhou-h:vsw-xxx,cn-hangzhou-i:vsw-yyy"
	pairs := strings.Split(zoneMapsStr, ",")
	for _, pair := range pairs {
		parts := strings.Split(strings.TrimSpace(pair), ":")
		if len(parts) != 2 {
			return nil, "", fmt.Errorf("invalid zoneMap format: %s, expected 'zoneId:vSwitchId'", pair)
		}

		zoneId := strings.TrimSpace(parts[0])
		vSwitchId := strings.TrimSpace(parts[1])

		if zoneId == "" || vSwitchId == "" {
			return nil, "", fmt.Errorf("zoneId and vSwitchId cannot be empty in: %s", pair)
		}

		zoneMapping := map[string]interface{}{
			"zoneId":    zoneId,
			"vSwitchId": vSwitchId,
		}
		zoneMappings = append(zoneMappings, zoneMapping)
	}

	if len(zoneMappings) < 2 {
		return nil, "", fmt.Errorf("at least 2 zone mappings are required, got %d", len(zoneMappings))
	}

	return zoneMappings, vpcId, nil
}

// ensureEIPCR 确保 EIP CR 存在并返回 AllocationID
// eipIspType: EIP 线路类型（如 BGP、BGP_PRO、ChinaTelecom、ChinaMobile 等）
// zoneIndex: zone 索引，用于区分同一个 NLB 的不同 zone 的 EIP
func (a *AutoNLBsV2Plugin) ensureEIPCR(ctx context.Context, c client.Client, namespace, gssName, eipIspType string, nlbIndex, zoneIndex int, config *autoNLBsConfig, gss *gamekruiseiov1alpha1.GameServerSet) (string, error) {
	// EIP CR 命名：{gssName}-eip-{eipIspType}-{nlbIndex}-z{zoneIndex}
	eipName := fmt.Sprintf("%s-eip-%s-%d-z%d", gssName, strings.ToLower(eipIspType), nlbIndex, zoneIndex)
	log.Infof("[%s] ensureEIPCR: eipName=%s, namespace=%s, eipIspType=%s, nlbIndex=%d, zoneIndex=%d",
		AutoNLBsV2Network, eipName, namespace, eipIspType, nlbIndex, zoneIndex)

	// 检查 EIP CR 是否已存在
	eipCR := &eipv1.EIP{}
	err := c.Get(ctx, types.NamespacedName{
		Name:      eipName,
		Namespace: namespace,
	}, eipCR)

	if err == nil {
		// EIP CR 已存在
		if eipCR.Status.AllocationID != "" {
			log.Infof("[%s] EIP CR %s/%s already exists, allocationID=%s, status=%s",
				AutoNLBsV2Network, namespace, eipName, eipCR.Status.AllocationID, eipCR.Status.Status)
			return eipCR.Status.AllocationID, nil
		}
		// EIP 还未就绪，等待 Operator 处理
		log.Infof("[%s] EIP CR %s/%s exists but not ready yet, waiting for allocation",
			AutoNLBsV2Network, namespace, eipName)
		return "", nil
	}

	if !errors.IsNotFound(err) {
		log.Errorf("[%s] Failed to get EIP CR %s/%s: %v", AutoNLBsV2Network, namespace, eipName, err)
		return "", err
	}

	// EIP CR 不存在，创建新的
	log.Infof("[%s] EIP CR %s/%s not found, creating new one with ISP type: %s",
		AutoNLBsV2Network, namespace, eipName, eipIspType)

	// 根据 ISP 类型选择计费方式
	// 单线 ISP（ChinaTelecom、ChinaMobile、ChinaUnicom）只支持按固定带宽付费
	// BGP、BGP_PRO 可以按流量付费
	internetChargeType := "PayByTraffic" // 默认按流量计费
	if eipIspType == "ChinaTelecom" || eipIspType == "ChinaMobile" || eipIspType == "ChinaUnicom" {
		internetChargeType = "PayByBandwidth" // 单线 ISP 必须按固定带宽付费
		log.Infof("[%s] Using PayByBandwidth for single-ISP type: %s", AutoNLBsV2Network, eipIspType)
	} else {
		log.Infof("[%s] Using PayByTraffic for ISP type: %s", AutoNLBsV2Network, eipIspType)
	}

	// 构建 EIP CR
	eipCR = &eipv1.EIP{
		ObjectMeta: metav1.ObjectMeta{
			Name:      eipName,
			Namespace: namespace,
			Labels: map[string]string{
				EIPPoolLabel:           "true",
				EIPPoolIndexLabel:      fmt.Sprintf("%d-z%d", nlbIndex, zoneIndex),
				EIPPoolEipIspTypeLabel: eipIspType,
				EIPPoolGssLabel:        gssName,
			},
		},
		Spec: eipv1.EIPSpec{
			Name:               eipName,
			Bandwidth:          "5",                // 默认带宽 5Mbps，可以后续通过配置调整
			InternetChargeType: internetChargeType, // 根据 ISP 类型选择计费方式
			ISP:                eipIspType,         // 设置 ISP 线路类型（支持单线 EIP）
			ReleaseStrategy:    "OnDelete",         // CR 删除时释放 EIP
			Description:        fmt.Sprintf("EIP for GameServerSet %s, NLB index %d, zone %d", gssName, nlbIndex, zoneIndex),
		},
	}

	// 根据 RetainNLBOnDelete 配置决定是否设置 OwnerReference
	if !config.retainNLBOnDelete {
		// 如果不保留，设置 OwnerReference 指向 GSS，GSS 删除时 EIP 会级联删除
		eipCR.OwnerReferences = []metav1.OwnerReference{
			{
				APIVersion:         gss.APIVersion,
				Kind:               gss.Kind,
				Name:               gss.GetName(),
				UID:                gss.GetUID(),
				Controller:         ptr.To(false), // EIP 不是由 GSS 控制器管理
				BlockOwnerDeletion: ptr.To(true),
			},
		}
		log.Infof("[%s] EIP CR %s will be deleted with GSS (RetainNLBOnDelete=false)", AutoNLBsV2Network, eipName)
	} else {
		// 保留模式：不设置 OwnerReference，允许 EIP 在 GSS 删除时保留
		// 实现 EIP 资源池复用
		log.Infof("[%s] EIP CR %s will be retained after GSS deletion (RetainNLBOnDelete=true)", AutoNLBsV2Network, eipName)
	}

	// 创建 EIP CR
	err = c.Create(ctx, eipCR)
	if err != nil {
		log.Errorf("[%s] Failed to create EIP CR %s/%s: %v", AutoNLBsV2Network, namespace, eipName, err)
		return "", fmt.Errorf("failed to create EIP CR: %w", err)
	}

	log.Infof("[%s] Successfully created EIP CR %s/%s with ISP type %s, waiting for allocation",
		AutoNLBsV2Network, namespace, eipName, eipIspType)

	// EIP 创建成功，但 AllocationID 需要等待 EIP Operator 分配
	// 返回空字符串，后续调谐时会获取到 AllocationID
	return "", nil
}
