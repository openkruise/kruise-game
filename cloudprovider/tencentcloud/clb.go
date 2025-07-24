package tencentcloud

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kruisev1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/cloudprovider"
	cperrors "github.com/openkruise/kruise-game/cloudprovider/errors"
	"github.com/openkruise/kruise-game/cloudprovider/utils"
	"github.com/openkruise/kruise-game/pkg/util"
)

const (
	ClbNetwork                      = "TencentCloud-CLB"
	AliasCLB                        = "CLB-Network"
	ClbIdsConfigName                = "ClbIds"
	PortProtocolsConfigName         = "PortProtocols"
	CLBPortMappingAnnotation        = "networking.cloud.tencent.com/clb-port-mapping"
	EnableCLBPortMappingAnnotation  = "networking.cloud.tencent.com/enable-clb-port-mapping"
	CLBPortMappingResultAnnotation  = "networking.cloud.tencent.com/clb-port-mapping-result"
	CLBPortMappingStatuslAnnotation = "networking.cloud.tencent.com/clb-port-mapping-status"
)

type ClbPlugin struct{}

type portProtocol struct {
	port     int
	protocol string
}

type clbConfig struct {
	targetPorts []portProtocol
}

type portMapping struct {
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
	Address  string `json:"address"`
}

func (p *ClbPlugin) Name() string {
	return ClbNetwork
}

func (p *ClbPlugin) Alias() string {
	return AliasCLB
}

func (p *ClbPlugin) Init(c client.Client, options cloudprovider.CloudProviderOptions, ctx context.Context) error {
	return nil
}

func (p *ClbPlugin) OnPodAdded(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	return p.reconcile(c, pod, ctx)
}

func (p *ClbPlugin) OnPodUpdated(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	if pod.DeletionTimestamp != nil {
		return pod, nil
	}
	return p.reconcile(c, pod, ctx)
}

// Ensure the annotation of pod is correct.
func (p *ClbPlugin) reconcile(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	networkManager := utils.NewNetworkManager(pod, c)
	networkStatus, _ := networkManager.GetNetworkStatus()
	if networkStatus == nil {
		pod, err := networkManager.UpdateNetworkStatus(kruisev1alpha1.NetworkStatus{
			CurrentNetworkState: kruisev1alpha1.NetworkWaiting,
		}, pod)
		return pod, cperrors.ToPluginError(err, cperrors.InternalError)
	}
	networkConfig := networkManager.GetNetworkConfig()
	clbConf, err := parseLbConfig(networkConfig)
	if err != nil {
		return pod, cperrors.ToPluginError(err, cperrors.ParameterError)
	}
	gss, err := util.GetGameServerSetOfPod(pod, c, ctx)
	if err != nil && !apierrors.IsNotFound(err) {
		return pod, cperrors.ToPluginError(err, cperrors.ApiCallError)
	}
	if pod.Annotations == nil {
		pod.Annotations = make(map[string]string)
	}
	pod.Annotations[CLBPortMappingAnnotation] = getClbPortMappingAnnotation(clbConf, gss)
	enableCLBPortMapping := "true"
	if networkManager.GetNetworkDisabled() {
		enableCLBPortMapping = "false"
	}
	pod.Annotations[EnableCLBPortMappingAnnotation] = enableCLBPortMapping
	if pod.Annotations[CLBPortMappingStatuslAnnotation] == "Ready" {
		if result := pod.Annotations[CLBPortMappingResultAnnotation]; result != "" {
			mappings := []portMapping{}
			if err := json.Unmarshal([]byte(result), &mappings); err != nil {
				return pod, cperrors.ToPluginError(err, cperrors.InternalError)
			}
			if len(mappings) != 0 {
				internalAddresses := make([]kruisev1alpha1.NetworkAddress, 0)
				externalAddresses := make([]kruisev1alpha1.NetworkAddress, 0)
				for _, mapping := range mappings {
					ss := strings.Split(mapping.Address, ":")
					if len(ss) != 2 {
						continue
					}
					lbIP := ss[0]
					lbPort, err := strconv.Atoi(ss[1])
					if err != nil {
						continue
					}
					port := mapping.Port
					instrIPort := intstr.FromInt(port)
					instrEPort := intstr.FromInt(lbPort)
					portName := instrIPort.String()
					protocol := corev1.Protocol(mapping.Protocol)
					internalAddresses = append(internalAddresses, kruisev1alpha1.NetworkAddress{
						IP: pod.Status.PodIP,
						Ports: []kruisev1alpha1.NetworkPort{
							{
								Name:     portName,
								Port:     &instrIPort,
								Protocol: protocol,
							},
						},
					})
					externalAddresses = append(externalAddresses, kruisev1alpha1.NetworkAddress{
						IP: lbIP,
						Ports: []kruisev1alpha1.NetworkPort{
							{
								Name:     portName,
								Port:     &instrEPort,
								Protocol: protocol,
							},
						},
					})
					networkStatus.InternalAddresses = internalAddresses
					networkStatus.ExternalAddresses = externalAddresses
					networkStatus.CurrentNetworkState = kruisev1alpha1.NetworkReady
					pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
					if err != nil {
						return pod, cperrors.ToPluginError(err, cperrors.InternalError)
					}
				}
			}
		}
	}
	return pod, nil
}

func (p *ClbPlugin) OnPodDeleted(c client.Client, pod *corev1.Pod, ctx context.Context) cperrors.PluginError {
	return nil
}

func init() {
	clbPlugin := ClbPlugin{}
	tencentCloudProvider.registerPlugin(&clbPlugin)
}

func getClbPortMappingAnnotation(clbConf *clbConfig, gss *kruisev1alpha1.GameServerSet) string {
	poolName := fmt.Sprintf("%s-%s", gss.Namespace, gss.Name)
	var buf strings.Builder
	for _, pp := range clbConf.targetPorts {
		buf.WriteString(fmt.Sprintf("%d %s %s\n", pp.port, pp.protocol, poolName))
	}
	return buf.String()
}

var ErrMissingPortProtocolsConfig = fmt.Errorf("missing %s config", PortProtocolsConfigName)

func parseLbConfig(conf []kruisev1alpha1.NetworkConfParams) (*clbConfig, error) {
	ports := []portProtocol{}
	for _, c := range conf {
		switch c.Name {
		case PortProtocolsConfigName:
			for _, pp := range strings.Split(c.Value, ",") {
				ppSlice := strings.Split(pp, "/")
				port, err := strconv.Atoi(ppSlice[0])
				if err != nil {
					continue
				}
				protocol := "TCP"
				if len(ppSlice) == 2 {
					protocol = ppSlice[1]
				}
				ports = append(ports, portProtocol{
					port:     port,
					protocol: protocol,
				})
			}
		}
	}
	if len(ports) == 0 {
		return nil, ErrMissingPortProtocolsConfig
	}
	return &clbConfig{
		targetPorts: ports,
	}, nil
}
