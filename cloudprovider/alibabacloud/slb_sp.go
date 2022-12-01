package alibabacloud

import (
	"context"
	"fmt"
	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/cloudprovider"
	"github.com/openkruise/kruise-game/cloudprovider/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strconv"
	"strings"
	"sync"
)

const (
	SlbSPNetwork  = "AlibabaCloud-SLB-SharedPort"
	SvcSLBSPLabel = "game.kruise.io/AlibabaCloud-SLB-SharedPort"
)

const (
	ErrorUpperLimit = "the number of backends supported by slb reaches the upper limit"
)

func init() {
	slbSpPlugin := SlbSpPlugin{
		mutex: sync.RWMutex{},
	}
	alibabaCloudProvider.registerPlugin(&slbSpPlugin)
}

type SlbSpPlugin struct {
	numBackends map[string]int
	podSlbId    map[string]string
	mutex       sync.RWMutex
}

type lbSpConfig struct {
	lbIds     []string
	ports     []int
	protocols []corev1.Protocol
}

func (s *SlbSpPlugin) OnPodAdded(c client.Client, pod *corev1.Pod) (*corev1.Pod, error) {
	networkManager := utils.NewNetworkManager(pod, c)
	podNetConfig := parseLbSpConfig(networkManager.GetNetworkConfig())

	lbId, err := s.getOrAllocate(podNetConfig, pod)
	if err != nil {
		return pod, err
	}

	// Get Svc
	svc := &corev1.Service{}
	err = c.Get(context.Background(), types.NamespacedName{
		Namespace: pod.GetNamespace(),
		Name:      lbId,
	}, svc)
	if err != nil {
		if errors.IsNotFound(err) {
			// Create Svc
			return pod, s.createSvc(c, pod, podNetConfig, lbId)
		}
		return pod, err
	}

	return networkManager.UpdateNetworkStatus(gamekruiseiov1alpha1.NetworkStatus{
		CurrentNetworkState: gamekruiseiov1alpha1.NetworkNotReady,
	}, pod)
}

func (s *SlbSpPlugin) OnPodUpdated(c client.Client, pod *corev1.Pod) (*corev1.Pod, error) {
	networkManager := utils.NewNetworkManager(pod, c)
	networkStatus, _ := networkManager.GetNetworkStatus()
	if networkStatus == nil {
		return networkManager.UpdateNetworkStatus(gamekruiseiov1alpha1.NetworkStatus{
			CurrentNetworkState: gamekruiseiov1alpha1.NetworkNotReady,
		}, pod)
	}

	podNetConfig := parseLbSpConfig(networkManager.GetNetworkConfig())
	podSlbId, err := s.getOrAllocate(podNetConfig, pod)
	if err != nil {
		return pod, err
	}

	// Get Svc
	svc := &corev1.Service{}
	err = c.Get(context.Background(), types.NamespacedName{
		Namespace: pod.GetNamespace(),
		Name:      podSlbId,
	}, svc)
	if err != nil {
		if errors.IsNotFound(err) {
			// Create Svc
			return pod, s.createSvc(c, pod, podNetConfig, podSlbId)
		}
		return pod, err
	}

	_, hasLabel := pod.Labels[SlbIdLabelKey]
	// disable network
	if networkManager.GetNetworkDisabled() && hasLabel {
		newLabels := pod.GetLabels()
		delete(newLabels, SlbIdLabelKey)
		pod.Labels = newLabels
		networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkNotReady
		return networkManager.UpdateNetworkStatus(*networkStatus, pod)
	}

	// enable network
	if !networkManager.GetNetworkDisabled() && !hasLabel {
		pod.Labels[SlbIdLabelKey] = podSlbId
		networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkReady
		return networkManager.UpdateNetworkStatus(*networkStatus, pod)
	}

	// network not ready
	if svc.Status.LoadBalancer.Ingress == nil {
		return pod, nil
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
			IP: svc.Status.LoadBalancer.Ingress[0].IP,
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
	return networkManager.UpdateNetworkStatus(*networkStatus, pod)
}

func (s *SlbSpPlugin) OnPodDeleted(c client.Client, pod *corev1.Pod) error {
	s.deAllocate(pod.GetNamespace() + "/" + pod.GetName())
	return nil
}

func (s *SlbSpPlugin) Name() string {
	return SlbSPNetwork
}

func (s *SlbSpPlugin) Alias() string {
	return ""
}

func (s *SlbSpPlugin) Init(c client.Client, options cloudprovider.CloudProviderOptions) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	svcList := &corev1.ServiceList{}
	err := c.List(context.Background(), svcList, &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(map[string]string{
			SvcSLBSPLabel: "true",
		})})
	if err != nil {
		return err
	}

	numBackends := make(map[string]int)
	podSlbId := make(map[string]string)
	for _, svc := range svcList.Items {
		slbId := svc.Labels[SlbIdLabelKey]
		podList := &corev1.PodList{}
		err := c.List(context.Background(), podList, &client.ListOptions{
			Namespace: svc.GetNamespace(),
			LabelSelector: labels.SelectorFromSet(map[string]string{
				SlbIdLabelKey: slbId,
			})})
		if err != nil {
			return err
		}
		num := len(podList.Items)
		numBackends[slbId] += num
		for _, pod := range podList.Items {
			podSlbId[pod.GetNamespace()+"/"+pod.GetName()] = slbId
		}
	}

	s.numBackends = numBackends
	s.podSlbId = podSlbId
	return nil
}

func (s *SlbSpPlugin) createSvc(c client.Client, pod *corev1.Pod, podConfig *lbSpConfig, lbId string) error {
	svcPorts := make([]corev1.ServicePort, 0)
	for i := 0; i < len(podConfig.ports); i++ {
		svcPorts = append(svcPorts, corev1.ServicePort{
			Name:       strconv.Itoa(podConfig.ports[i]),
			Port:       int32(podConfig.ports[i]),
			Protocol:   podConfig.protocols[i],
			TargetPort: intstr.FromInt(podConfig.ports[i]),
		})
	}

	return c.Create(context.Background(), &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      lbId,
			Namespace: pod.GetNamespace(),
			Annotations: map[string]string{
				SlbIdAnnotationKey:     lbId,
				SlbListenerOverrideKey: "true",
			},
			Labels: map[string]string{
				SvcSLBSPLabel: "true",
			},
			OwnerReferences: getSvcOwnerReference(pod, c, true),
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeLoadBalancer,
			Selector: map[string]string{
				SlbIdLabelKey: lbId,
			},
			Ports: svcPorts,
		},
	})
}

func (s *SlbSpPlugin) getOrAllocate(podNetConfig *lbSpConfig, pod *corev1.Pod) (string, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if slbId, ok := s.podSlbId[pod.GetNamespace()+"/"+pod.GetName()]; ok {
		return slbId, nil
	}

	minValue := 200
	selectId := ""
	for _, id := range podNetConfig.lbIds {
		numBackends := s.numBackends[id]
		if numBackends < 200 && numBackends < minValue {
			minValue = numBackends
			selectId = id
		}
	}

	if selectId == "" {
		return "", fmt.Errorf(ErrorUpperLimit)
	}

	s.numBackends[selectId]++
	s.podSlbId[pod.GetNamespace()+"/"+pod.GetName()] = selectId
	pod.Labels[SlbIdLabelKey] = selectId
	return selectId, nil
}

func (s *SlbSpPlugin) deAllocate(nsName string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	slbId, ok := s.podSlbId[nsName]
	if !ok {
		return
	}

	s.numBackends[slbId]--
	delete(s.podSlbId, nsName)
}

func parseLbSpConfig(conf []gamekruiseiov1alpha1.NetworkConfParams) *lbSpConfig {
	var lbIds []string
	var ports []int
	var protocols []corev1.Protocol
	for _, c := range conf {
		switch c.Name {
		case SlbIdsConfigName:
			lbIds = parseLbIds(c.Value)
		case PortProtocolsConfigName:
			ports, protocols = parsePortProtocols(c.Value)
		}
	}
	return &lbSpConfig{
		lbIds:     lbIds,
		ports:     ports,
		protocols: protocols,
	}
}

func parsePortProtocols(value string) ([]int, []corev1.Protocol) {
	ports := make([]int, 0)
	protocols := make([]corev1.Protocol, 0)
	for _, pp := range strings.Split(value, ",") {
		ppSlice := strings.Split(pp, "/")
		port, err := strconv.Atoi(ppSlice[0])
		if err != nil {
			continue
		}
		ports = append(ports, port)
		if len(ppSlice) != 2 {
			protocols = append(protocols, corev1.ProtocolTCP)
		} else {
			protocols = append(protocols, corev1.Protocol(ppSlice[1]))
		}
	}
	return ports, protocols
}

func parseLbIds(value string) []string {
	return strings.Split(value, ",")
}
