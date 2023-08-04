package alibabacloud

import (
	"context"
	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/cloudprovider"
	"github.com/openkruise/kruise-game/cloudprovider/alibabacloud/apis/v1beta1"
	"github.com/openkruise/kruise-game/cloudprovider/errors"
	"github.com/openkruise/kruise-game/cloudprovider/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	EIPNetwork                      = "AlibabaCloud-EIP"
	AliasSEIP                       = "EIP-Network"
	ReleaseStrategyConfigName       = "ReleaseStrategy"
	PoolIdConfigName                = "PoolId"
	ResourceGroupIdConfigName       = "ResourceGroupId"
	BandwidthConfigName             = "Bandwidth"
	BandwidthPackageIdConfigName    = "BandwidthPackageId"
	ChargeTypeConfigName            = "ChargeType"
	DescriptionConfigName           = "Description"
	WithEIPAnnotationKey            = "k8s.aliyun.com/pod-with-eip"
	ReleaseStrategyAnnotationkey    = "k8s.aliyun.com/pod-eip-release-strategy"
	PoolIdAnnotationkey             = "k8s.aliyun.com/eip-public-ip-address-pool-id"
	ResourceGroupIdAnnotationkey    = "k8s.aliyun.com/eip-resource-group-id"
	BandwidthAnnotationkey          = "k8s.aliyun.com/eip-bandwidth"
	BandwidthPackageIdAnnotationkey = "k8s.aliyun.com/eip-common-bandwidth-package-id"
	ChargeTypeConfigAnnotationkey   = "k8s.aliyun.com/eip-internet-charge-type"
	EIPNameAnnotationKey            = "k8s.aliyun.com/eip-name"
	EIPDescriptionAnnotationKey     = "k8s.aliyun.com/eip-description"
)

type EipPlugin struct {
}

func (E EipPlugin) Name() string {
	return EIPNetwork
}

func (E EipPlugin) Alias() string {
	return AliasSEIP
}

func (E EipPlugin) Init(client client.Client, options cloudprovider.CloudProviderOptions, ctx context.Context) error {
	return nil
}

func (E EipPlugin) OnPodAdded(client client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, errors.PluginError) {
	networkManager := utils.NewNetworkManager(pod, client)
	conf := networkManager.GetNetworkConfig()

	pod.Annotations[WithEIPAnnotationKey] = "true"
	pod.Annotations[EIPNameAnnotationKey] = pod.GetNamespace() + "/" + pod.GetName()
	// parse network configuration
	for _, c := range conf {
		switch c.Name {
		case ReleaseStrategyConfigName:
			pod.Annotations[ReleaseStrategyAnnotationkey] = c.Value
		case PoolIdConfigName:
			pod.Annotations[PoolIdAnnotationkey] = c.Value
		case ResourceGroupIdConfigName:
			pod.Annotations[ResourceGroupIdAnnotationkey] = c.Value
		case BandwidthConfigName:
			pod.Annotations[BandwidthAnnotationkey] = c.Value
		case BandwidthPackageIdConfigName:
			pod.Annotations[BandwidthPackageIdAnnotationkey] = c.Value
		case ChargeTypeConfigName:
			pod.Annotations[ChargeTypeConfigAnnotationkey] = c.Value
		case DescriptionConfigName:
			pod.Annotations[EIPDescriptionAnnotationKey] = c.Value
		}
	}

	return pod, nil
}

func (E EipPlugin) OnPodUpdated(client client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, errors.PluginError) {
	networkManager := utils.NewNetworkManager(pod, client)

	networkStatus, _ := networkManager.GetNetworkStatus()
	if networkStatus == nil {
		pod, err := networkManager.UpdateNetworkStatus(gamekruiseiov1alpha1.NetworkStatus{
			CurrentNetworkState: gamekruiseiov1alpha1.NetworkWaiting,
		}, pod)
		return pod, errors.ToPluginError(err, errors.InternalError)
	}

	podEip := &v1beta1.PodEIP{}
	err := client.Get(ctx, types.NamespacedName{
		Name:      pod.GetName(),
		Namespace: pod.GetNamespace(),
	}, podEip)
	if err != nil || podEip.Status.EipAddress == "" {
		return pod, nil
	}

	networkStatus.InternalAddresses = []gamekruiseiov1alpha1.NetworkAddress{
		{
			IP: podEip.Status.PrivateIPAddress,
		},
	}
	networkStatus.ExternalAddresses = []gamekruiseiov1alpha1.NetworkAddress{
		{
			IP: podEip.Status.EipAddress,
		},
	}

	networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkReady

	pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
	return pod, errors.ToPluginError(err, errors.InternalError)
}

func (E EipPlugin) OnPodDeleted(client client.Client, pod *corev1.Pod, ctx context.Context) errors.PluginError {
	return nil
}

func init() {
	alibabaCloudProvider.registerPlugin(&EipPlugin{})
}
