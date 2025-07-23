package volcengine

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/cloudprovider"
	"github.com/openkruise/kruise-game/cloudprovider/errors"
	"github.com/openkruise/kruise-game/cloudprovider/utils"
	corev1 "k8s.io/api/core/v1"
	log "k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	EIPNetwork                   = "Volcengine-EIP"
	AliasSEIP                    = "EIP-Network"
	ReleaseStrategyConfigName    = "ReleaseStrategy"
	PoolIdConfigName             = "PoolId"
	ResourceGroupIdConfigName    = "ResourceGroupId"
	BandwidthConfigName          = "Bandwidth"
	BandwidthPackageIdConfigName = "BandwidthPackageId"
	ChargeTypeConfigName         = "ChargeType"
	DescriptionConfigName        = "Description"

	VkeAnnotationPrefix       = "vke.volcengine.com"
	UseExistEIPAnnotationKey  = "vke.volcengine.com/primary-eip-id"
	WithEIPAnnotationKey      = "vke.volcengine.com/primary-eip-allocate"
	EipAttributeAnnotationKey = "vke.volcengine.com/primary-eip-attributes"

	EipStatusKey     = "vke.volcengine.com/allocated-eips"
	DefaultEipConfig = "{\"type\": \"Elastic\"}"
)

type eipStatus struct {
	EipId      string `json:"EipId,omitempty"`      // EIP 实例 ID
	EipAddress string `json:"EipAddress,omitempty"` // EIP 实例公网地址
	EniId      string `json:"EniId,omitempty"`      // Pod 实例的弹性网卡 ID
	EniIp      string `json:"niIp,omitempty"`       // Pod 实例的弹性网卡的私网 IPv4 地址
}

type EipPlugin struct {
}

func (E EipPlugin) Name() string {
	return EIPNetwork
}

func (E EipPlugin) Alias() string {
	return AliasSEIP
}

func (E EipPlugin) Init(client client.Client, options cloudprovider.CloudProviderOptions, ctx context.Context) error {
	log.Infof("Initializing Volcengine EIP plugin")
	return nil
}

func (E EipPlugin) OnPodAdded(client client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, errors.PluginError) {
	log.Infof("begin to handle PodAdded for pod name %s, namespace %s", pod.Name, pod.Namespace)
	networkManager := utils.NewNetworkManager(pod, client)

	// 获取网络配置参数
	networkConfs := networkManager.GetNetworkConfig()
	log.Infof("pod %s/%s network configs: %+v", pod.Namespace, pod.Name, networkConfs)

	if networkManager.GetNetworkType() != EIPNetwork {
		log.Infof("pod %s/%s network type is not %s, skipping", pod.Namespace, pod.Name, EIPNetwork)
		return pod, nil
	}
	log.Infof("processing pod %s/%s with Volcengine EIP network", pod.Namespace, pod.Name)

	// 检查是否有 UseExistEIPAnnotationKey 的配置
	eipID := ""
	if pod.Annotations == nil {
		log.Infof("pod %s/%s has no annotations, initializing", pod.Namespace, pod.Name)
		pod.Annotations = make(map[string]string)
	}
	eipConfig := make(map[string]interface{})
	// 从配置中提取参数
	for _, conf := range networkConfs {
		log.Infof("processing network config for pod %s/%s: %s=%s", pod.Namespace, pod.Name, conf.Name, conf.Value)
		switch conf.Name {
		case UseExistEIPAnnotationKey:
			pod.Annotations[UseExistEIPAnnotationKey] = conf.Value
			eipID = conf.Value
			log.Infof("pod %s/%s using existing EIP ID: %s", pod.Namespace, pod.Name, eipID)
		case "billingType":
			var err error
			eipConfig[conf.Name], err = strconv.ParseInt(conf.Value, 10, 64)
			if err != nil {
				log.Infof("failed to parse billingType for pod %s/%s: %v", pod.Namespace, pod.Name, err)
				return pod, errors.ToPluginError(err, errors.InternalError)
			}
			log.Infof("pod %s/%s billingType set to: %v", pod.Namespace, pod.Name, eipConfig[conf.Name])
		case "bandwidth":
			var err error
			eipConfig[conf.Name], err = strconv.ParseInt(conf.Value, 10, 64)
			if err != nil {
				log.Infof("failed to parse bandwidth for pod %s/%s: %v", pod.Namespace, pod.Name, err)
				return pod, errors.ToPluginError(err, errors.InternalError)
			}
			log.Infof("pod %s/%s bandwidth set to: %v", pod.Namespace, pod.Name, eipConfig[conf.Name])
		default:
			eipConfig[conf.Name] = conf.Value
			log.Infof("pod %s/%s setting %s to: %v", pod.Namespace, pod.Name, conf.Name, conf.Value)
		}
	}

	// 更新 Pod 注解
	if eipID != "" {
		// 使用已有的 EIP
		log.Infof("pod %s/%s using existing EIP ID: %s", pod.Namespace, pod.Name, eipID)
		pod.Annotations[UseExistEIPAnnotationKey] = eipID
	} else {
		// 使用新建逻辑
		if len(eipConfig) == 0 {
			eipConfig["description"] = "Created by the OKG Volcengine EIP plugin. Do not delete or modify."
		}
		configs, _ := json.Marshal(eipConfig)
		log.Infof("pod %s/%s allocating new EIP with config: %s", pod.Namespace, pod.Name, string(configs))
		pod.Annotations[WithEIPAnnotationKey] = DefaultEipConfig
		pod.Annotations[EipAttributeAnnotationKey] = string(configs)
	}

	log.Infof("completed OnPodAdded for pod %s/%s", pod.Namespace, pod.Name)
	return pod, nil
}

func (E EipPlugin) OnPodUpdated(client client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, errors.PluginError) {
	log.Infof("begin to handle PodUpdated for pod name %s, namespace %s", pod.Name, pod.Namespace)
	networkManager := utils.NewNetworkManager(pod, client)

	networkStatus, _ := networkManager.GetNetworkStatus()
	if networkStatus == nil {
		log.Infof("network status is nil for pod %s/%s, updating to waiting state", pod.Namespace, pod.Name)
		pod, err := networkManager.UpdateNetworkStatus(gamekruiseiov1alpha1.NetworkStatus{
			CurrentNetworkState: gamekruiseiov1alpha1.NetworkWaiting,
		}, pod)
		if err != nil {
			log.Infof("failed to update network status for pod %s/%s: %v", pod.Namespace, pod.Name, err)
			return pod, errors.ToPluginError(err, errors.InternalError)
		}
		return pod, nil
	}

	podEipStatus := []eipStatus{}
	if str, ok := pod.Annotations[EipStatusKey]; ok {
		log.Infof("found EIP status annotation for pod %s/%s: %s", pod.Namespace, pod.Name, str)
		err := json.Unmarshal([]byte(str), &podEipStatus)
		if err != nil {
			log.Infof("failed to unmarshal EipStatusKey for pod %s/%s: %v", pod.Namespace, pod.Name, err)
			return pod, errors.ToPluginError(fmt.Errorf("failed to unmarshal EipStatusKey, err: %w", err), errors.ParameterError)
		}

		log.Infof("updating network status for pod %s/%s, internal IP: %s, external IP: %s",
			pod.Namespace, pod.Name, podEipStatus[0].EniIp, podEipStatus[0].EipAddress)

		var internalAddresses []gamekruiseiov1alpha1.NetworkAddress
		var externalAddresses []gamekruiseiov1alpha1.NetworkAddress

		for _, eipStatus := range podEipStatus {
			internalAddresses = append(internalAddresses, gamekruiseiov1alpha1.NetworkAddress{
				IP: eipStatus.EniIp,
			})
			externalAddresses = append(externalAddresses, gamekruiseiov1alpha1.NetworkAddress{
				IP: eipStatus.EipAddress,
			})
		}

		networkStatus.InternalAddresses = internalAddresses
		networkStatus.ExternalAddresses = externalAddresses
		networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkReady
		log.Infof("network for pod %s/%s is ready, EIP: %s", pod.Namespace, pod.Name, podEipStatus[0].EipAddress)

		pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
		if err != nil {
			log.Infof("failed to update network status for pod %s/%s: %v", pod.Namespace, pod.Name, err)
		}
		return pod, errors.ToPluginError(err, errors.InternalError)
	}

	log.Infof("no EIP status found for pod %s/%s, waiting for allocation", pod.Namespace, pod.Name)
	return pod, nil
}

func (E EipPlugin) OnPodDeleted(client client.Client, pod *corev1.Pod, ctx context.Context) errors.PluginError {
	log.Infof("handling pod deletion for pod %s/%s", pod.Namespace, pod.Name)
	// 检查是否需要额外处理
	if pod.Annotations != nil {
		if eipID, ok := pod.Annotations[UseExistEIPAnnotationKey]; ok {
			log.Infof("pod %s/%s being deleted had existing EIP ID: %s", pod.Namespace, pod.Name, eipID)
		}
		if _, ok := pod.Annotations[WithEIPAnnotationKey]; ok {
			log.Infof("pod %s/%s being deleted had allocated EIP", pod.Namespace, pod.Name)
		}
	}
	log.Infof("completed deletion handling for pod %s/%s", pod.Namespace, pod.Name)
	return nil
}

func init() {
	volcengineProvider.registerPlugin(&EipPlugin{})
}
