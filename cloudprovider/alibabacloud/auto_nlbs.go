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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	log "k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/cloudprovider"
	cperrors "github.com/openkruise/kruise-game/cloudprovider/errors"
	"github.com/openkruise/kruise-game/cloudprovider/utils"
	"github.com/openkruise/kruise-game/pkg/util"
)

const (
	AutoNLBsNetwork = "AlibabaCloud-AutoNLBs"
	AliasAutoNLBs   = "Auto-NLBs-Network"

	ReserveNlbNumConfigName = "ReserveNlbNum"
	EipTypesConfigName      = "EipTypes"
	ZoneMapsConfigName      = "ZoneMaps"
	MinPortConfigName       = "MinPort"
	MaxPortConfigName       = "MaxPort"
	BlockPortsConfigName    = "BlockPorts"

	NLBZoneMapsServiceAnnotationKey = "service.beta.kubernetes.io/alibaba-cloud-loadbalancer-zone-maps"
	NLBAddressTypeAnnotationKey     = "service.beta.kubernetes.io/alibaba-cloud-loadbalancer-address-type"

	IntranetEIPType = "intranet"
	DefaultEIPType  = "default"
)

type AutoNLBsPlugin struct {
	gssMaxPodIndex map[string]int
	mutex          sync.RWMutex
}

type autoNLBsConfig struct {
	minPort               int32
	maxPort               int32
	blockPorts            []int32
	zoneMaps              string
	reserveNlbNum         int
	targetPorts           []int
	protocols             []corev1.Protocol
	eipTypes              []string
	externalTrafficPolicy corev1.ServiceExternalTrafficPolicyType
	*nlbHealthConfig
}

func (a *AutoNLBsPlugin) Name() string {
	return AutoNLBsNetwork
}

func (a *AutoNLBsPlugin) Alias() string {
	return AliasAutoNLBs
}

func (a *AutoNLBsPlugin) Init(c client.Client, options cloudprovider.CloudProviderOptions, ctx context.Context) error {
	gssList := &gamekruiseiov1alpha1.GameServerSetList{}
	err := c.List(ctx, gssList, &client.ListOptions{})
	if err != nil {
		log.Errorf("cannot list gameserverset in cluster because %s", err.Error())
		return err
	}

	for _, gss := range gssList.Items {
		if gss.Spec.Network != nil && gss.Spec.Network.NetworkType == AutoNLBsNetwork {
			a.gssMaxPodIndex[gss.GetNamespace()+"/"+gss.GetName()] = int(*gss.Spec.Replicas)

			nc, err := parseAutoNLBsConfig(gss.Spec.Network.NetworkConf)
			if err != nil {
				log.Errorf("pasrse config wronge because %s", err.Error())
				return err
			}

			err = a.ensureServices(ctx, c, gss.GetNamespace(), gss.GetName(), nc)
			if err != nil {
				log.Errorf("ensure services error because %s", err.Error())
				return err
			}
		}
	}
	return nil
}

func (a *AutoNLBsPlugin) OnPodAdded(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	networkManager := utils.NewNetworkManager(pod, c)
	networkConfig := networkManager.GetNetworkConfig()
	conf, err := parseAutoNLBsConfig(networkConfig)
	if err != nil {
		return pod, cperrors.NewPluginError(cperrors.ParameterError, err.Error())
	}

	a.ensureMaxPodIndex(pod)
	gssName := pod.GetLabels()[gamekruiseiov1alpha1.GameServerOwnerGssKey]
	if err := a.ensureServices(ctx, c, pod.GetNamespace(), gssName, conf); err != nil {
		return pod, cperrors.NewPluginError(cperrors.ApiCallError, err.Error())
	}

	containerPorts := make([]corev1.ContainerPort, 0)
	podIndex := util.GetIndexFromGsName(pod.GetName())
	for i, port := range conf.targetPorts {
		if conf.protocols[i] == ProtocolTCPUDP {
			containerPortTCP := corev1.ContainerPort{
				ContainerPort: int32(port),
				Protocol:      corev1.ProtocolTCP,
				Name:          "tcp-" + strconv.Itoa(podIndex) + "-" + strconv.Itoa(port),
			}
			containerPortUDP := corev1.ContainerPort{
				ContainerPort: int32(port),
				Protocol:      corev1.ProtocolUDP,
				Name:          "udp-" + strconv.Itoa(podIndex) + "-" + strconv.Itoa(port),
			}
			containerPorts = append(containerPorts, containerPortTCP, containerPortUDP)
		} else {
			containerPort := corev1.ContainerPort{
				ContainerPort: int32(port),
				Protocol:      conf.protocols[i],
				Name:          strings.ToLower(string(conf.protocols[i])) + "-" + strconv.Itoa(podIndex) + "-" + strconv.Itoa(port),
			}
			containerPorts = append(containerPorts, containerPort)
		}
	}
	pod.Spec.Containers[0].Ports = containerPorts

	lenRange := int(conf.maxPort) - int(conf.minPort) - len(conf.blockPorts) + 1
	svcIndex := podIndex / (lenRange / len(conf.targetPorts))

	for _, eipType := range conf.eipTypes {
		svcName := gssName + "-" + eipType + "-" + strconv.Itoa(svcIndex)
		pod.Spec.ReadinessGates = append(pod.Spec.ReadinessGates, corev1.PodReadinessGate{
			ConditionType: corev1.PodConditionType(PrefixReadyReadinessGate + svcName),
		})
	}

	return pod, nil
}

func (a *AutoNLBsPlugin) OnPodUpdated(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	networkManager := utils.NewNetworkManager(pod, c)
	networkStatus, _ := networkManager.GetNetworkStatus()
	networkConfig := networkManager.GetNetworkConfig()
	conf, err := parseAutoNLBsConfig(networkConfig)
	if err != nil {
		return pod, cperrors.NewPluginError(cperrors.ParameterError, err.Error())
	}

	if networkStatus == nil {
		pod, err := networkManager.UpdateNetworkStatus(gamekruiseiov1alpha1.NetworkStatus{
			CurrentNetworkState: gamekruiseiov1alpha1.NetworkNotReady,
		}, pod)
		return pod, cperrors.ToPluginError(err, cperrors.InternalError)
	}

	_, readyCondition := util.GetPodConditionFromList(pod.Status.Conditions, corev1.PodReady)
	if readyCondition == nil || readyCondition.Status == corev1.ConditionFalse {
		networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkNotReady
		pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
		return pod, cperrors.ToPluginError(err, cperrors.InternalError)
	}

	var internalPorts []gamekruiseiov1alpha1.NetworkPort
	var externalPorts []gamekruiseiov1alpha1.NetworkPort
	endPoints := ""

	podIndex := util.GetIndexFromGsName(pod.GetName())
	lenRange := int(conf.maxPort) - int(conf.minPort) - len(conf.blockPorts) + 1
	svcIndex := podIndex / (lenRange / len(conf.targetPorts))
	for i, eipType := range conf.eipTypes {
		svcName := pod.GetLabels()[gamekruiseiov1alpha1.GameServerOwnerGssKey] + "-" + eipType + "-" + strconv.Itoa(svcIndex)
		svc := &corev1.Service{}
		err := c.Get(ctx, types.NamespacedName{
			Name:      svcName,
			Namespace: pod.GetNamespace(),
		}, svc)
		if err != nil {
			return pod, cperrors.NewPluginError(cperrors.ApiCallError, err.Error())
		}

		if svc.Status.LoadBalancer.Ingress == nil || len(svc.Status.LoadBalancer.Ingress) == 0 {
			networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkNotReady
			pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
			return pod, cperrors.ToPluginError(err, cperrors.InternalError)
		}

		endPoints = endPoints + svc.Status.LoadBalancer.Ingress[0].Hostname + "/" + eipType

		if i == len(conf.eipTypes)-1 {
			for i, port := range conf.targetPorts {
				if conf.protocols[i] == ProtocolTCPUDP {
					portNameTCP := "tcp-" + strconv.Itoa(podIndex) + strconv.Itoa(port)
					portNameUDP := "udp-" + strconv.Itoa(podIndex) + strconv.Itoa(port)
					iPort := intstr.FromInt(port)
					internalPorts = append(internalPorts, gamekruiseiov1alpha1.NetworkPort{
						Name:     portNameTCP,
						Protocol: corev1.ProtocolTCP,
						Port:     &iPort,
					}, gamekruiseiov1alpha1.NetworkPort{
						Name:     portNameUDP,
						Protocol: corev1.ProtocolUDP,
						Port:     &iPort,
					})
					for _, svcPort := range svc.Spec.Ports {
						if svcPort.Name == portNameTCP || svcPort.Name == portNameUDP {
							ePort := intstr.FromInt32(svcPort.Port)
							externalPorts = append(externalPorts, gamekruiseiov1alpha1.NetworkPort{
								Name:     portNameTCP,
								Protocol: corev1.ProtocolTCP,
								Port:     &ePort,
							}, gamekruiseiov1alpha1.NetworkPort{
								Name:     portNameUDP,
								Protocol: corev1.ProtocolUDP,
								Port:     &ePort,
							})
							break
						}
					}
				} else {
					portName := strings.ToLower(string(conf.protocols[i])) + "-" + strconv.Itoa(podIndex) + "-" + strconv.Itoa(port)
					iPort := intstr.FromInt(port)
					internalPorts = append(internalPorts, gamekruiseiov1alpha1.NetworkPort{
						Name:     portName,
						Protocol: conf.protocols[i],
						Port:     &iPort,
					})
					for _, svcPort := range svc.Spec.Ports {
						if svcPort.Name == portName {
							ePort := intstr.FromInt32(svcPort.Port)
							externalPorts = append(externalPorts, gamekruiseiov1alpha1.NetworkPort{
								Name:     portName,
								Protocol: conf.protocols[i],
								Port:     &ePort,
							})
							break
						}
					}
				}
			}
		} else {
			endPoints = endPoints + ","
		}
	}

	networkStatus = &gamekruiseiov1alpha1.NetworkStatus{
		InternalAddresses: []gamekruiseiov1alpha1.NetworkAddress{
			{
				IP:    pod.Status.PodIP,
				Ports: internalPorts,
			},
		},
		ExternalAddresses: []gamekruiseiov1alpha1.NetworkAddress{
			{
				EndPoint: endPoints,
				Ports:    externalPorts,
			},
		},
		CurrentNetworkState: gamekruiseiov1alpha1.NetworkReady,
	}

	pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
	return pod, cperrors.ToPluginError(err, cperrors.InternalError)
}

func (a *AutoNLBsPlugin) OnPodDeleted(client client.Client, pod *corev1.Pod, ctx context.Context) cperrors.PluginError {
	return nil
}

func init() {
	autoNLBsPlugin := AutoNLBsPlugin{
		mutex:          sync.RWMutex{},
		gssMaxPodIndex: make(map[string]int),
	}
	alibabaCloudProvider.registerPlugin(&autoNLBsPlugin)
}

func (a *AutoNLBsPlugin) ensureMaxPodIndex(pod *corev1.Pod) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	podIndex := util.GetIndexFromGsName(pod.GetName())
	gssNsName := pod.GetNamespace() + "/" + pod.GetLabels()[gamekruiseiov1alpha1.GameServerOwnerGssKey]
	if podIndex > a.gssMaxPodIndex[gssNsName] {
		a.gssMaxPodIndex[gssNsName] = podIndex
	}
}

func (a *AutoNLBsPlugin) checkSvcNumToCreate(namespace, gssName string, config *autoNLBsConfig) int {
	a.mutex.RLock()
	defer a.mutex.RUnlock()
	lenRange := int(config.maxPort) - int(config.minPort) - len(config.blockPorts) + 1
	expectSvcNum := a.gssMaxPodIndex[namespace+"/"+gssName]/(lenRange/len(config.targetPorts)) + config.reserveNlbNum + 1
	return expectSvcNum
}

func (a *AutoNLBsPlugin) ensureServices(ctx context.Context, client client.Client, namespace, gssName string, config *autoNLBsConfig) error {
	expectSvcNum := a.checkSvcNumToCreate(namespace, gssName, config)

	for _, eipType := range config.eipTypes {
		for j := 0; j < expectSvcNum; j++ {
			// get svc
			svcName := gssName + "-" + eipType + "-" + strconv.Itoa(j)
			svc := &corev1.Service{}
			err := client.Get(ctx, types.NamespacedName{
				Name:      svcName,
				Namespace: namespace,
			}, svc)
			if err != nil {
				if errors.IsNotFound(err) {
					// create svc
					toAddSvc := a.consSvc(namespace, gssName, eipType, j, config)
					if err := setSvcOwner(client, ctx, toAddSvc, namespace, gssName); err != nil {
						return err
					} else {
						if err := client.Create(ctx, toAddSvc); err != nil {
							return err
						}
					}
				} else {
					return err
				}
			}
		}
	}

	return nil
}

func (a *AutoNLBsPlugin) consSvcPorts(svcIndex int, config *autoNLBsConfig) []corev1.ServicePort {
	lenRange := int(config.maxPort) - int(config.minPort) - len(config.blockPorts) + 1
	ports := make([]corev1.ServicePort, 0)
	toAllocatedPort := config.minPort
	portNumPerPod := lenRange / len(config.targetPorts)
	for podIndex := svcIndex * portNumPerPod; podIndex < (svcIndex+1)*portNumPerPod; podIndex++ {
		for i, protocol := range config.protocols {
			if protocol == ProtocolTCPUDP {
				svcPortTCP := corev1.ServicePort{
					Name:       "tcp-" + strconv.Itoa(podIndex) + "-" + strconv.Itoa(config.targetPorts[i]),
					TargetPort: intstr.FromString("tcp-" + strconv.Itoa(podIndex) + "-" + strconv.Itoa(config.targetPorts[i])),
					Port:       toAllocatedPort,
					Protocol:   corev1.ProtocolTCP,
				}
				svcPortUDP := corev1.ServicePort{
					Name:       "udp-" + strconv.Itoa(podIndex) + "-" + strconv.Itoa(config.targetPorts[i]),
					TargetPort: intstr.FromString("udp-" + strconv.Itoa(podIndex) + "-" + strconv.Itoa(config.targetPorts[i])),
					Port:       toAllocatedPort,
					Protocol:   corev1.ProtocolUDP,
				}
				ports = append(ports, svcPortTCP, svcPortUDP)
			} else {
				svcPort := corev1.ServicePort{
					Name:       strings.ToLower(string(protocol)) + "-" + strconv.Itoa(podIndex) + "-" + strconv.Itoa(config.targetPorts[i]),
					TargetPort: intstr.FromString(strings.ToLower(string(protocol)) + "-" + strconv.Itoa(podIndex) + "-" + strconv.Itoa(config.targetPorts[i])),
					Port:       toAllocatedPort,
					Protocol:   protocol,
				}
				ports = append(ports, svcPort)
			}
			toAllocatedPort++
			for util.IsNumInListInt32(toAllocatedPort, config.blockPorts) {
				toAllocatedPort++
			}
		}
	}
	return ports
}

func (a *AutoNLBsPlugin) consSvc(namespace, gssName, eipType string, svcIndex int, conf *autoNLBsConfig) *corev1.Service {
	loadBalancerClass := "alibabacloud.com/nlb"
	svcAnnotations := map[string]string{
		//SlbConfigHashKey:               util.GetHash(conf),
		NLBZoneMapsServiceAnnotationKey: conf.zoneMaps,
		LBHealthCheckFlagAnnotationKey:  conf.lBHealthCheckFlag,
	}
	if conf.lBHealthCheckFlag == "on" {
		svcAnnotations[LBHealthCheckTypeAnnotationKey] = conf.lBHealthCheckType
		svcAnnotations[LBHealthCheckConnectPortAnnotationKey] = conf.lBHealthCheckConnectPort
		svcAnnotations[LBHealthCheckConnectTimeoutAnnotationKey] = conf.lBHealthCheckConnectTimeout
		svcAnnotations[LBHealthCheckIntervalAnnotationKey] = conf.lBHealthCheckInterval
		svcAnnotations[LBHealthyThresholdAnnotationKey] = conf.lBHealthyThreshold
		svcAnnotations[LBUnhealthyThresholdAnnotationKey] = conf.lBUnhealthyThreshold
		if conf.lBHealthCheckType == "http" {
			svcAnnotations[LBHealthCheckDomainAnnotationKey] = conf.lBHealthCheckDomain
			svcAnnotations[LBHealthCheckUriAnnotationKey] = conf.lBHealthCheckUri
			svcAnnotations[LBHealthCheckMethodAnnotationKey] = conf.lBHealthCheckMethod
		}
	}
	if strings.Contains(eipType, IntranetEIPType) {
		svcAnnotations[NLBAddressTypeAnnotationKey] = IntranetEIPType
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        gssName + "-" + eipType + "-" + strconv.Itoa(svcIndex),
			Namespace:   namespace,
			Annotations: svcAnnotations,
		},
		Spec: corev1.ServiceSpec{
			Ports: a.consSvcPorts(svcIndex, conf),
			Type:  corev1.ServiceTypeLoadBalancer,
			Selector: map[string]string{
				gamekruiseiov1alpha1.GameServerOwnerGssKey: gssName,
			},
			LoadBalancerClass:             &loadBalancerClass,
			AllocateLoadBalancerNodePorts: ptr.To[bool](false),
			ExternalTrafficPolicy:         conf.externalTrafficPolicy,
		},
	}
}

func setSvcOwner(c client.Client, ctx context.Context, svc *corev1.Service, namespace, gssName string) error {
	gss := &gamekruiseiov1alpha1.GameServerSet{}
	err := c.Get(ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      gssName,
	}, gss)
	if err != nil {
		return err
	}
	ownerRef := []metav1.OwnerReference{
		{
			APIVersion:         gss.APIVersion,
			Kind:               gss.Kind,
			Name:               gss.GetName(),
			UID:                gss.GetUID(),
			Controller:         ptr.To[bool](true),
			BlockOwnerDeletion: ptr.To[bool](true),
		},
	}
	svc.OwnerReferences = ownerRef
	return nil
}

func parseAutoNLBsConfig(conf []gamekruiseiov1alpha1.NetworkConfParams) (*autoNLBsConfig, error) {
	reserveNlbNum := 1
	eipTypes := []string{"default"}
	ports := make([]int, 0)
	protocols := make([]corev1.Protocol, 0)
	externalTrafficPolicy := corev1.ServiceExternalTrafficPolicyTypeLocal
	zoneMaps := ""
	blockPorts := make([]int32, 0)
	minPort := int32(1000)
	maxPort := int32(1499)

	for _, c := range conf {
		switch c.Name {
		case PortProtocolsConfigName:
			for _, pp := range strings.Split(c.Value, ",") {
				ppSlice := strings.Split(pp, "/")
				port, err := strconv.Atoi(ppSlice[0])
				if err != nil {
					return nil, fmt.Errorf("invalid PortProtocols %s", c.Value)
				}
				ports = append(ports, port)
				if len(ppSlice) != 2 {
					protocols = append(protocols, corev1.ProtocolTCP)
				} else {
					protocols = append(protocols, corev1.Protocol(ppSlice[1]))
				}
			}
		case ExternalTrafficPolicyTypeConfigName:
			if strings.EqualFold(c.Value, string(corev1.ServiceExternalTrafficPolicyTypeCluster)) {
				externalTrafficPolicy = corev1.ServiceExternalTrafficPolicyTypeCluster
			}
		case ReserveNlbNumConfigName:
			reserveNlbNum, _ = strconv.Atoi(c.Value)
		case EipTypesConfigName:
			eipTypes = strings.Split(c.Value, ",")
		case ZoneMapsConfigName:
			zoneMaps = c.Value
		case BlockPortsConfigName:
			blockPorts = util.StringToInt32Slice(c.Value, ",")
		case MinPortConfigName:
			val, err := strconv.ParseInt(c.Value, 10, 32)
			if err != nil {
				return nil, fmt.Errorf("invalid MinPort %s", c.Value)
			} else {
				minPort = int32(val)
			}
		case MaxPortConfigName:
			val, err := strconv.ParseInt(c.Value, 10, 32)
			if err != nil {
				return nil, fmt.Errorf("invalid MaxPort %s", c.Value)
			} else {
				maxPort = int32(val)
			}
		}
	}

	if minPort > maxPort {
		return nil, fmt.Errorf("invalid MinPort %d and MaxPort %d", minPort, maxPort)
	}

	if zoneMaps == "" {
		return nil, fmt.Errorf("invalid ZoneMaps, which can not be empty")
	}

	// check ports & protocols
	if len(ports) == 0 || len(protocols) == 0 {
		return nil, fmt.Errorf("invalid PortProtocols, which can not be empty")
	}

	nlbHealthConfig, err := parseNlbHealthConfig(conf)
	if err != nil {
		return nil, err
	}

	return &autoNLBsConfig{
		blockPorts:            blockPorts,
		minPort:               minPort,
		maxPort:               maxPort,
		nlbHealthConfig:       nlbHealthConfig,
		reserveNlbNum:         reserveNlbNum,
		eipTypes:              eipTypes,
		protocols:             protocols,
		targetPorts:           ports,
		zoneMaps:              zoneMaps,
		externalTrafficPolicy: externalTrafficPolicy,
	}, nil
}
