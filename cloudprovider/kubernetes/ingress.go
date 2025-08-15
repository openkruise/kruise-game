package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/cloudprovider"
	cperrors "github.com/openkruise/kruise-game/cloudprovider/errors"
	"github.com/openkruise/kruise-game/cloudprovider/utils"
	"github.com/openkruise/kruise-game/pkg/util"
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
	// FixedKey indicates whether the ingress object is still retained when the pod is deleted.
	// If True, ingress will not be deleted even though the pod is deleted.
	// If False, ingress will be deleted along with pod deletion.
	FixedKey = "Fixed"
)

const (
	SvcSelectorKey = "statefulset.kubernetes.io/pod-name"
	IngressHashKey = "game.kruise.io/ingress-hash"
	ServiceHashKey = "game.kruise.io/svc-hash"
)

const paramsError = "Network Config Params Error"

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
	ic, err := parseIngConfig(conf, pod)
	if err != nil {
		return pod, cperrors.NewPluginError(cperrors.ParameterError, err.Error())
	}

	// get svc
	svc := &corev1.Service{}
	err = c.Get(ctx, types.NamespacedName{
		Name:      pod.GetName(),
		Namespace: pod.GetNamespace(),
	}, svc)
	if err != nil {
		if errors.IsNotFound(err) {
			return pod, cperrors.ToPluginError(c.Create(ctx, consSvc(ic, pod, c, ctx)), cperrors.ApiCallError)
		}
		return pod, cperrors.NewPluginError(cperrors.ApiCallError, err.Error())
	}
	// update svc
	if util.GetHash(ic.ports) != svc.GetAnnotations()[ServiceHashKey] {
		networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkNotReady
		pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
		if err != nil {
			return pod, cperrors.NewPluginError(cperrors.InternalError, err.Error())
		}

		newSvc := consSvc(ic, pod, c, ctx)
		patchSvc := map[string]interface{}{"metadata": map[string]map[string]string{"annotations": newSvc.Annotations}, "spec": newSvc.Spec}
		patchSvcBytes, err := json.Marshal(patchSvc)
		if err != nil {
			return pod, cperrors.NewPluginError(cperrors.InternalError, err.Error())
		}

		return pod, cperrors.ToPluginError(c.Patch(ctx, svc, client.RawPatch(types.MergePatchType, patchSvcBytes)), cperrors.ApiCallError)
	}

	// get ingress
	ing := &v1.Ingress{}
	err = c.Get(ctx, types.NamespacedName{
		Name:      pod.GetName(),
		Namespace: pod.GetNamespace(),
	}, ing)
	if err != nil {
		if errors.IsNotFound(err) {
			return pod, cperrors.ToPluginError(c.Create(ctx, consIngress(ic, pod, c, ctx)), cperrors.ApiCallError)
		}
		return pod, cperrors.NewPluginError(cperrors.ApiCallError, err.Error())
	}

	// update ingress
	if util.GetHash(ic) != ing.GetAnnotations()[IngressHashKey] {
		networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkNotReady
		pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
		if err != nil {
			return pod, cperrors.NewPluginError(cperrors.InternalError, err.Error())
		}
		return pod, cperrors.ToPluginError(c.Update(ctx, consIngress(ic, pod, c, ctx)), cperrors.ApiCallError)
	}

	// network ready
	internalAddresses := make([]gamekruiseiov1alpha1.NetworkAddress, 0)
	externalAddresses := make([]gamekruiseiov1alpha1.NetworkAddress, 0)
	networkPorts := make([]gamekruiseiov1alpha1.NetworkPort, 0)

	for _, p := range ing.Spec.Rules[0].HTTP.Paths {
		instrIPort := intstr.FromInt(int(p.Backend.Service.Port.Number))
		networkPort := gamekruiseiov1alpha1.NetworkPort{
			Name: p.Path,
			Port: &instrIPort,
		}
		networkPorts = append(networkPorts, networkPort)
	}

	internalAddress := gamekruiseiov1alpha1.NetworkAddress{
		IP:    pod.Status.PodIP,
		Ports: networkPorts,
	}
	externalAddress := gamekruiseiov1alpha1.NetworkAddress{
		EndPoint: ing.Spec.Rules[0].Host,
		Ports:    networkPorts,
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
	paths            []string
	pathTypes        []*v1.PathType
	ports            []int32
	host             string
	ingressClassName *string
	tlsHosts         []string
	tlsSecretName    string
	annotations      map[string]string
	fixed            bool
}

func parseIngConfig(conf []gamekruiseiov1alpha1.NetworkConfParams, pod *corev1.Pod) (ingConfig, error) {
	var ic ingConfig
	ic.annotations = make(map[string]string)
	ic.paths = make([]string, 0)
	ic.pathTypes = make([]*v1.PathType, 0)
	ic.ports = make([]int32, 0)
	id := util.GetIndexFromGsName(pod.GetName())
	for _, c := range conf {
		switch c.Name {
		case PathTypeKey:
			pathType := v1.PathType(c.Value)
			ic.pathTypes = append(ic.pathTypes, &pathType)
		case PortKey:
			port, _ := strconv.ParseInt(c.Value, 10, 32)
			ic.ports = append(ic.ports, int32(port))
		case HostKey:
			strs := strings.Split(c.Value, "<id>")
			switch len(strs) {
			case 2:
				ic.host = strs[0] + strconv.Itoa(id) + strs[1]
			case 1:
				ic.host = strs[0]
			default:
				return ingConfig{}, fmt.Errorf("%s", paramsError)
			}
		case IngressClassNameKey:
			ic.ingressClassName = ptr.To[string](c.Value)
		case TlsSecretNameKey:
			ic.tlsSecretName = c.Value
		case TlsHostsKey:
			ic.tlsHosts = strings.Split(c.Value, ",")
		case PathKey:
			strs := strings.Split(c.Value, "<id>")
			switch len(strs) {
			case 2:
				ic.paths = append(ic.paths, strs[0]+strconv.Itoa(id)+strs[1])
			case 1:
				ic.paths = append(ic.paths, strs[0])
			default:
				return ingConfig{}, fmt.Errorf("%s", paramsError)
			}
		case AnnotationKey:
			kv := strings.Split(c.Value, ": ")
			if len(kv) != 2 {
				return ingConfig{}, fmt.Errorf("%s", paramsError)
			}
			ic.annotations[kv[0]] = kv[1]
		case FixedKey:
			fixed, _ := strconv.ParseBool(c.Value)
			ic.fixed = fixed
		}
	}

	if len(ic.paths) == 0 || len(ic.pathTypes) == 0 || len(ic.ports) == 0 {
		return ingConfig{}, fmt.Errorf("%s", paramsError)
	}

	return ic, nil
}

func consIngress(ic ingConfig, pod *corev1.Pod, c client.Client, ctx context.Context) *v1.Ingress {
	pathSlice := ic.paths
	pathTypeSlice := ic.pathTypes
	pathPortSlice := ic.ports
	lenPathTypeSlice := len(pathTypeSlice)
	lenPathPortSlice := len(pathPortSlice)
	for i := 0; i < len(pathSlice)-lenPathTypeSlice; i++ {
		pathTypeSlice = append(pathTypeSlice, pathTypeSlice[0])
	}
	for i := 0; i < len(pathSlice)-lenPathPortSlice; i++ {
		pathPortSlice = append(pathPortSlice, pathPortSlice[0])
	}

	ingAnnotations := ic.annotations
	if ingAnnotations == nil {
		ingAnnotations = make(map[string]string)
	}
	ingAnnotations[IngressHashKey] = util.GetHash(ic)
	ingPaths := make([]v1.HTTPIngressPath, 0)
	for i := 0; i < len(pathSlice); i++ {
		ingPath := v1.HTTPIngressPath{
			Path:     pathSlice[i],
			PathType: pathTypeSlice[i],
			Backend: v1.IngressBackend{
				Service: &v1.IngressServiceBackend{
					Name: pod.GetName(),
					Port: v1.ServiceBackendPort{
						Number: pathPortSlice[i],
					},
				},
			},
		}
		ingPaths = append(ingPaths, ingPath)
	}
	ing := &v1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:            pod.GetName(),
			Namespace:       pod.GetNamespace(),
			Annotations:     ingAnnotations,
			OwnerReferences: consOwnerReference(c, ctx, pod, ic.fixed),
		},
		Spec: v1.IngressSpec{
			IngressClassName: ic.ingressClassName,
			Rules: []v1.IngressRule{
				{
					Host: ic.host,
					IngressRuleValue: v1.IngressRuleValue{
						HTTP: &v1.HTTPIngressRuleValue{
							Paths: ingPaths,
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

func consSvc(ic ingConfig, pod *corev1.Pod, c client.Client, ctx context.Context) *corev1.Service {
	annoatations := make(map[string]string)
	annoatations[ServiceHashKey] = util.GetHash(ic.ports)
	ports := make([]corev1.ServicePort, 0)
	for _, p := range ic.ports {
		port := corev1.ServicePort{
			Port: p,
			Name: strconv.Itoa(int(p)),
		}
		ports = append(ports, port)
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:            pod.GetName(),
			Namespace:       pod.GetNamespace(),
			OwnerReferences: consOwnerReference(c, ctx, pod, ic.fixed),
			Annotations:     annoatations,
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Selector: map[string]string{
				SvcSelectorKey: pod.GetName(),
			},
			Ports: ports,
		},
	}
}

func consOwnerReference(c client.Client, ctx context.Context, pod *corev1.Pod, isFixed bool) []metav1.OwnerReference {
	ownerReferences := []metav1.OwnerReference{
		{
			APIVersion:         pod.APIVersion,
			Kind:               pod.Kind,
			Name:               pod.GetName(),
			UID:                pod.GetUID(),
			Controller:         ptr.To[bool](true),
			BlockOwnerDeletion: ptr.To[bool](true),
		},
	}
	if isFixed {
		gss, err := util.GetGameServerSetOfPod(pod, c, ctx)
		if err == nil {
			ownerReferences = []metav1.OwnerReference{
				{
					APIVersion:         gss.APIVersion,
					Kind:               gss.Kind,
					Name:               gss.GetName(),
					UID:                gss.GetUID(),
					Controller:         ptr.To[bool](true),
					BlockOwnerDeletion: ptr.To[bool](true),
				},
			}
		}
	}
	return ownerReferences
}
