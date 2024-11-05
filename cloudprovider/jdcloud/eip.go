package jdcloud

import (
	"context"
	cerr "errors"
	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/cloudprovider"
	"github.com/openkruise/kruise-game/cloudprovider/errors"
	"github.com/openkruise/kruise-game/cloudprovider/utils"
	"github.com/openkruise/kruise-game/pkg/util"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
)

const (
	EIPNetwork = "JdCloud-EIP"
	AliasSEIP  = "EIP-Network"

	EIPIdAnnotationKey      = "jdos.jd.com/eip.id"
	EIPIfaceAnnotationKey   = "jdos.jd.com/eip.iface"
	EIPAnnotationKey        = "jdos.jd.com/eip.ip"
	BandwidthAnnotationkey  = "jdos.jd.com/eip.bandwith"
	ChargeTypeAnnotationkey = "jdos.jd.com/eip.chargeMode"
	EnableEIPAnnotationKey  = "jdos.jd.com/eip.enable"
	FixedEIPAnnotationKey   = "jdos.jd.com/eip.static"
	EIPNameAnnotationKey    = "jdos.jd.com/eip-name"
	AssignEIPAnnotationKey  = "jdos.jd.com/eip.userAssign"
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
	gss, err := util.GetGameServerSetOfPod(pod, client, ctx)
	if err != nil {
		return pod, errors.ToPluginError(err, errors.ApiCallError)
	}
	gssAnnotations := gss.GetAnnotations()
	for k, v := range gssAnnotations {
		if strings.Contains(k, "jdos.jd.com") {
			pod.Annotations[k] = v
		}
	}
	pod.Annotations[EIPNameAnnotationKey] = pod.GetNamespace() + "/" + pod.GetName()
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

	if enable, ok := pod.Annotations[EnableEIPAnnotationKey]; !ok || (ok && enable != "true") {
		return pod, errors.ToPluginError(cerr.New("eip plugin is not enabled"), errors.InternalError)
	}
	if _, ok := pod.Annotations[EIPIdAnnotationKey]; !ok {
		return pod, nil
	}
	if _, ok := pod.Annotations[EIPAnnotationKey]; !ok {
		return pod, nil
	}
	networkStatus.ExternalAddresses = []gamekruiseiov1alpha1.NetworkAddress{
		{
			IP: pod.Annotations[EIPAnnotationKey],
		},
	}
	networkStatus.InternalAddresses = []gamekruiseiov1alpha1.NetworkAddress{
		{
			IP: pod.Status.PodIP,
		},
	}
	networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkReady

	pod, err := networkManager.UpdateNetworkStatus(*networkStatus, pod)
	return pod, errors.ToPluginError(err, errors.InternalError)
}

func (E EipPlugin) OnPodDeleted(client client.Client, pod *corev1.Pod, ctx context.Context) errors.PluginError {
	return nil
}

func init() {
	jdcloudProvider.registerPlugin(&EipPlugin{})
}
