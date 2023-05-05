package kubernetes

import (
	"context"
	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/cloudprovider"
	cperrors "github.com/openkruise/kruise-game/cloudprovider/errors"
	"github.com/openkruise/kruise-game/cloudprovider/utils"
	"github.com/openkruise/kruise-game/pkg/util"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strconv"
	"strings"
)

const (
	IngressNetwork = "Kubernetes-Ingress"
	// PathTypeKey determines the interpretation of the Path matching, which is same as PathType in HTTPIngressPath.
	PathTypeKey = "PathType"
	// PathKey is matched against the path of an incoming request, the meaning of which is same as Path in HTTPIngressPath.
	// Users can add <id> to any position of the path, and the network plugin will generate the path corresponding to the game server.
	// e.g. /game<id>(/|$)(.*) The ingress path of GameServer 0 is /game0(/|$)(.*), the ingress path of GameServer 1 is /game1(/|$)(.*), and so on.
	PathKey = "Path"
	// PortKey indicates the exposed port value of game server.
	PortKey = "Port"
	// IngressClassNameKey indicates the name of the IngressClass cluster resource, which is same as IngressClassName in IngressSpec.
	IngressClassNameKey = "IngressClassName"
	// HostKey indicates domain name, which is same as Host in IngressRule.
	HostKey = "Host"
	// TlsHostsKey indicates hosts that included in the TLS certificate, the meaning of which is the same as that of Hosts in IngressTLS.
	// Its corresponding value format is as follows, host1,host2,... e.g. xxx.xx.com
	TlsHostsKey = "TlsHosts"
	// TlsSecretNameKey indicates the name of the secret used to terminate TLS traffic on port 443, which is same as SecretName in IngressTLS.
	TlsSecretNameKey = "TlsSecretName"
	// AnnotationKey is the key of an annotation for ingress.
	// Its corresponding value format is as follows, key: value(note that there is a space after: ) e.g. nginx.ingress.kubernetes.io/rewrite-target: /$2
	// If the user wants to fill in multiple annotations, just fill in multiple AnnotationKeys in the network config.
	AnnotationKey = "Annotation"
)

const (
	SvcSelectorKey = "statefulset.kubernetes.io/pod-name"
	IngressHashKey = "game.kruise.io/ingress-hash"
)

func init() {
	kubernetesProvider.registerPlugin(&IngressPlugin{})
}

type IngressPlugin struct {
}

func (i IngressPlugin) Name() string {
	return IngressNetwork
}

func (i IngressPlugin) Alias() string {
	return ""
}

func (i IngressPlugin) Init(client client.Client, options cloudprovider.CloudProviderOptions, ctx context.Context) error {
	return nil
}

func (i IngressPlugin) OnPodAdded(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	networkManager := utils.NewNetworkManager(pod, c)
	conf := networkManager.GetNetworkConfig()
	ic := parseIngConfig(conf, pod)

	err := c.Create(ctx, consSvc(ic, pod))
	if err != nil {
		return pod, cperrors.NewPluginError(cperrors.ApiCallError, err.Error())
	}

	err = c.Create(ctx, consIngress(ic, pod))
	if err != nil {
		return pod, cperrors.NewPluginError(cperrors.ApiCallError, err.Error())
	}

	return pod, nil
}

func (i IngressPlugin) OnPodUpdated(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	networkManager := utils.NewNetworkManager(pod, c)
	networkStatus, _ := networkManager.GetNetworkStatus()
	if networkStatus == nil {
		pod, err := networkManager.UpdateNetworkStatus(gamekruiseiov1alpha1.NetworkStatus{
			CurrentNetworkState: gamekruiseiov1alpha1.NetworkNotReady,
		}, pod)
		return pod, cperrors.ToPluginError(err, cperrors.InternalError)
	}

	conf := networkManager.GetNetworkConfig()
	ic := parseIngConfig(conf, pod)

	// get svc
	svc := &corev1.Service{}
	err := c.Get(ctx, types.NamespacedName{
		Name:      pod.GetName(),
		Namespace: pod.GetNamespace(),
	}, svc)
	if err != nil {
		if errors.IsNotFound(err) {
			return pod, cperrors.ToPluginError(c.Create(ctx, consSvc(ic, pod)), cperrors.ApiCallError)
		}
		return pod, cperrors.NewPluginError(cperrors.ApiCallError, err.Error())
	}

	// get ingress
	ing := &v1.Ingress{}
	err = c.Get(ctx, types.NamespacedName{
		Name:      pod.GetName(),
		Namespace: pod.GetNamespace(),
	}, ing)
	if err != nil {
		if errors.IsNotFound(err) {
			return pod, cperrors.ToPluginError(c.Create(ctx, consIngress(ic, pod)), cperrors.ApiCallError)
		}
		return pod, cperrors.NewPluginError(cperrors.ApiCallError, err.Error())
	}

	networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkNotReady
	pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)

	// update
	if util.GetHash(ic) != ing.GetAnnotations()[IngressHashKey] {
		// update svc port
		if ic.port != svc.Spec.Ports[0].Port {
			svc.Spec.Ports[0].Port = ic.port
			svc.Spec.Ports[0].TargetPort = intstr.FromInt(int(ic.port))
			err := c.Update(ctx, svc)
			if err != nil {
				return pod, cperrors.NewPluginError(cperrors.ApiCallError, err.Error())
			}
		}
		// update ingress
		return pod, cperrors.ToPluginError(c.Update(ctx, consIngress(ic, pod)), cperrors.ApiCallError)
	}

	// network not ready
	if ing.Status.LoadBalancer.Ingress == nil {
		return pod, cperrors.ToPluginError(err, cperrors.InternalError)
	}

	// network ready
	internalAddresses := make([]gamekruiseiov1alpha1.NetworkAddress, 0)
	externalAddresses := make([]gamekruiseiov1alpha1.NetworkAddress, 0)

	instrIPort := intstr.FromInt(int(ic.port))
	internalAddress := gamekruiseiov1alpha1.NetworkAddress{
		IP: pod.Status.PodIP,
		Ports: []gamekruiseiov1alpha1.NetworkPort{
			{
				Name:     strconv.Itoa(int(ic.port)),
				Port:     &instrIPort,
				Protocol: corev1.ProtocolTCP,
			},
		},
	}
	externalAddress := gamekruiseiov1alpha1.NetworkAddress{
		IP:       ing.Status.LoadBalancer.Ingress[0].IP,
		EndPoint: ing.Status.LoadBalancer.Ingress[0].Hostname,
		Ports: []gamekruiseiov1alpha1.NetworkPort{
			{
				Name:     instrIPort.String(),
				Port:     &instrIPort,
				Protocol: corev1.ProtocolTCP,
			},
		},
	}

	networkStatus.InternalAddresses = append(internalAddresses, internalAddress)
	networkStatus.ExternalAddresses = append(externalAddresses, externalAddress)
	networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkReady
	pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
	return pod, cperrors.ToPluginError(err, cperrors.InternalError)
}

func (i IngressPlugin) OnPodDeleted(c client.Client, pod *corev1.Pod, ctx context.Context) cperrors.PluginError {
	return nil
}

type ingConfig struct {
	path             string
	pathType         *v1.PathType
	port             int32
	host             string
	ingressClassName *string
	tlsHosts         []string
	tlsSecretName    string
	annotations      map[string]string
}

func parseIngConfig(conf []gamekruiseiov1alpha1.NetworkConfParams, pod *corev1.Pod) ingConfig {
	var ic ingConfig
	ic.annotations = make(map[string]string)
	id := util.GetIndexFromGsName(pod.GetName())
	for _, c := range conf {
		switch c.Name {
		case PathTypeKey:
			pathType := v1.PathType(c.Value)
			ic.pathType = &pathType
		case PortKey:
			port, _ := strconv.ParseInt(c.Value, 10, 32)
			ic.port = int32(port)
		case HostKey:
			ic.host = c.Value
		case IngressClassNameKey:
			ic.ingressClassName = pointer.String(c.Value)
		case TlsSecretNameKey:
			ic.tlsSecretName = c.Value
		case TlsHostsKey:
			ic.tlsHosts = strings.Split(c.Value, ",")
		case PathKey:
			strs := strings.Split(c.Value, "<id>")
			ic.path = strs[0] + strconv.Itoa(id) + strs[1]
		case AnnotationKey:
			kv := strings.Split(c.Value, ": ")
			ic.annotations[kv[0]] = kv[1]
		}
	}
	return ic
}

func consIngress(ic ingConfig, pod *corev1.Pod) *v1.Ingress {
	ingAnnotations := ic.annotations
	ingAnnotations[IngressHashKey] = util.GetHash(ic)
	ing := &v1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        pod.GetName(),
			Namespace:   pod.GetNamespace(),
			Annotations: ingAnnotations,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         pod.APIVersion,
					Kind:               pod.Kind,
					Name:               pod.GetName(),
					UID:                pod.GetUID(),
					Controller:         pointer.BoolPtr(true),
					BlockOwnerDeletion: pointer.BoolPtr(true),
				},
			},
		},
		Spec: v1.IngressSpec{
			IngressClassName: ic.ingressClassName,
			Rules: []v1.IngressRule{
				{
					Host: ic.host,
					IngressRuleValue: v1.IngressRuleValue{
						HTTP: &v1.HTTPIngressRuleValue{
							Paths: []v1.HTTPIngressPath{
								{
									Path:     ic.path,
									PathType: ic.pathType,
									Backend: v1.IngressBackend{
										Service: &v1.IngressServiceBackend{
											Name: pod.GetName(),
											Port: v1.ServiceBackendPort{
												Number: ic.port,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	if ic.tlsHosts != nil || ic.tlsSecretName != "" {
		ing.Spec.TLS = []v1.IngressTLS{
			{
				SecretName: ic.tlsSecretName,
				Hosts:      ic.tlsHosts,
			},
		}
	}
	return ing
}

func consSvc(ic ingConfig, pod *corev1.Pod) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pod.GetName(),
			Namespace: pod.GetNamespace(),
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         pod.APIVersion,
					Kind:               pod.Kind,
					Name:               pod.GetName(),
					UID:                pod.GetUID(),
					Controller:         pointer.BoolPtr(true),
					BlockOwnerDeletion: pointer.BoolPtr(true),
				},
			},
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Selector: map[string]string{
				SvcSelectorKey: pod.GetName(),
			},
			Ports: []corev1.ServicePort{
				{
					Port: ic.port,
				},
			},
		},
	}
}
