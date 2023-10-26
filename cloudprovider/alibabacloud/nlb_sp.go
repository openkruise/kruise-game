package alibabacloud

import (
	"context"
	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/cloudprovider"
	cperrors "github.com/openkruise/kruise-game/cloudprovider/errors"
	"github.com/openkruise/kruise-game/cloudprovider/utils"
	"github.com/openkruise/kruise-game/pkg/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strconv"
)

const (
	NlbSPNetwork     = "AlibabaCloud-NLB-SharedPort"
	NlbIdsConfigName = "NlbIds"
)

func init() {
	alibabaCloudProvider.registerPlugin(&NlbSpPlugin{})
}

type NlbSpPlugin struct {
}

func (N *NlbSpPlugin) Name() string {
	return NlbSPNetwork
}

func (N *NlbSpPlugin) Alias() string {
	return ""
}

func (N *NlbSpPlugin) Init(client client.Client, options cloudprovider.CloudProviderOptions, ctx context.Context) error {
	return nil
}

func (N *NlbSpPlugin) OnPodAdded(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	networkManager := utils.NewNetworkManager(pod, c)
	podNetConfig := parseNLbSpConfig(networkManager.GetNetworkConfig())

	pod.Labels[SlbIdLabelKey] = podNetConfig.lbId

	// Get Svc
	svc := &corev1.Service{}
	err := c.Get(ctx, types.NamespacedName{
		Namespace: pod.GetNamespace(),
		Name:      podNetConfig.lbId,
	}, svc)
	if err != nil {
		if errors.IsNotFound(err) {
			// Create Svc
			return pod, cperrors.ToPluginError(c.Create(ctx, consNlbSvc(podNetConfig, pod, c, ctx)), cperrors.ApiCallError)
		}
		return pod, cperrors.NewPluginError(cperrors.ApiCallError, err.Error())
	}
	return pod, nil
}

func (N *NlbSpPlugin) OnPodUpdated(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	networkManager := utils.NewNetworkManager(pod, c)
	networkStatus, _ := networkManager.GetNetworkStatus()
	if networkStatus == nil {
		pod, err := networkManager.UpdateNetworkStatus(gamekruiseiov1alpha1.NetworkStatus{
			CurrentNetworkState: gamekruiseiov1alpha1.NetworkNotReady,
		}, pod)
		return pod, cperrors.ToPluginError(err, cperrors.InternalError)
	}

	networkConfig := networkManager.GetNetworkConfig()
	podNetConfig := parseNLbSpConfig(networkConfig)

	// Get Svc
	svc := &corev1.Service{}
	err := c.Get(context.Background(), types.NamespacedName{
		Namespace: pod.GetNamespace(),
		Name:      podNetConfig.lbId,
	}, svc)
	if err != nil {
		if errors.IsNotFound(err) {
			// Create Svc
			return pod, cperrors.ToPluginError(c.Create(ctx, consNlbSvc(podNetConfig, pod, c, ctx)), cperrors.ApiCallError)
		}
		return pod, cperrors.NewPluginError(cperrors.ApiCallError, err.Error())
	}

	// update svc
	if util.GetHash(podNetConfig) != svc.GetAnnotations()[SlbConfigHashKey] {
		networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkNotReady
		pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
		if err != nil {
			return pod, cperrors.NewPluginError(cperrors.InternalError, err.Error())
		}
		return pod, cperrors.ToPluginError(c.Update(ctx, consNlbSvc(podNetConfig, pod, c, ctx)), cperrors.ApiCallError)
	}

	_, hasLabel := pod.Labels[SlbIdLabelKey]
	// disable network
	if networkManager.GetNetworkDisabled() && hasLabel {
		newLabels := pod.GetLabels()
		delete(newLabels, SlbIdLabelKey)
		pod.Labels = newLabels
		networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkNotReady
		pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
		return pod, cperrors.ToPluginError(err, cperrors.InternalError)
	}

	// enable network
	if !networkManager.GetNetworkDisabled() && !hasLabel {
		pod.Labels[SlbIdLabelKey] = podNetConfig.lbId
		networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkReady
		pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
		return pod, cperrors.ToPluginError(err, cperrors.InternalError)
	}

	// network not ready
	if svc.Status.LoadBalancer.Ingress == nil {
		return pod, nil
	}

	// allow not ready containers
	if util.IsAllowNotReadyContainers(networkConfig) {
		toUpDateSvc, err := utils.AllowNotReadyContainers(c, ctx, pod, svc, true)
		if err != nil {
			return pod, err
		}

		if toUpDateSvc {
			err := c.Update(ctx, svc)
			if err != nil {
				return pod, cperrors.ToPluginError(err, cperrors.ApiCallError)
			}
		}
	}

	// network ready
	internalAddresses := make([]gamekruiseiov1alpha1.NetworkAddress, 0)
	externalAddresses := make([]gamekruiseiov1alpha1.NetworkAddress, 0)
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
			EndPoint: svc.Status.LoadBalancer.Ingress[0].Hostname,
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
	networkStatus.InternalAddresses = internalAddresses
	networkStatus.ExternalAddresses = externalAddresses
	networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkReady
	pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
	return pod, cperrors.ToPluginError(err, cperrors.InternalError)
}

func (N *NlbSpPlugin) OnPodDeleted(client client.Client, pod *corev1.Pod, ctx context.Context) cperrors.PluginError {
	return nil
}

type nlbConfig struct {
	lbId      string
	ports     []int
	protocols []corev1.Protocol
}

func parseNLbSpConfig(conf []gamekruiseiov1alpha1.NetworkConfParams) *nlbConfig {
	var lbIds string
	var ports []int
	var protocols []corev1.Protocol
	for _, c := range conf {
		switch c.Name {
		case NlbIdsConfigName:
			lbIds = c.Value
		case PortProtocolsConfigName:
			ports, protocols = parsePortProtocols(c.Value)
		}
	}
	return &nlbConfig{
		lbId:      lbIds,
		ports:     ports,
		protocols: protocols,
	}
}

func consNlbSvc(nc *nlbConfig, pod *corev1.Pod, c client.Client, ctx context.Context) *corev1.Service {
	svcPorts := make([]corev1.ServicePort, 0)
	for i := 0; i < len(nc.ports); i++ {
		svcPorts = append(svcPorts, corev1.ServicePort{
			Name:       strconv.Itoa(nc.ports[i]),
			Port:       int32(nc.ports[i]),
			Protocol:   nc.protocols[i],
			TargetPort: intstr.FromInt(nc.ports[i]),
		})
	}
	loadBalancerClass := "alibabacloud.com/nlb"

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nc.lbId,
			Namespace: pod.GetNamespace(),
			Annotations: map[string]string{
				SlbListenerOverrideKey: "true",
				SlbIdAnnotationKey:     nc.lbId,
				SlbConfigHashKey:       util.GetHash(nc),
			},
			OwnerReferences: getSvcOwnerReference(c, ctx, pod, true),
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeLoadBalancer,
			Selector: map[string]string{
				SlbIdLabelKey: nc.lbId,
			},
			Ports:             svcPorts,
			LoadBalancerClass: &loadBalancerClass,
		},
	}
	return svc
}
