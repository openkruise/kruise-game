package tencentcloud

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"

	kruisev1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/cloudprovider"
	cperrors "github.com/openkruise/kruise-game/cloudprovider/errors"
	provideroptions "github.com/openkruise/kruise-game/cloudprovider/options"
	"github.com/openkruise/kruise-game/cloudprovider/tencentcloud/apis/v1alpha1"
	"github.com/openkruise/kruise-game/cloudprovider/utils"
	"github.com/openkruise/kruise-game/pkg/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	log "k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ClbNetwork              = "TencentCloud-CLB"
	AliasCLB                = "CLB-Network"
	ClbIdsConfigName        = "ClbIds"
	PortProtocolsConfigName = "PortProtocols"
	MinPortConfigName       = "MinPort"
	MaxPortConfigName       = "MaxPort"
	OwnerPodKey             = "game.kruise.io/owner-pod"
	TargetPortKey           = "game.kruise.io/target-port"
)

type portAllocated map[int32]bool

type ClbPlugin struct {
	maxPort     int32
	minPort     int32
	cache       map[string]portAllocated
	podAllocate map[string][]string
	mutex       sync.RWMutex
}

type portProtocol struct {
	port     int
	protocol string
}

type clbConfig struct {
	lbIds       []string
	targetPorts []portProtocol
}

func (p *ClbPlugin) Name() string {
	return ClbNetwork
}

func (p *ClbPlugin) Alias() string {
	return AliasCLB
}

func (p *ClbPlugin) Init(c client.Client, options cloudprovider.CloudProviderOptions, ctx context.Context) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	clbOptions := options.(provideroptions.TencentCloudOptions).CLBOptions
	p.minPort = clbOptions.MinPort
	p.maxPort = clbOptions.MaxPort

	listenerList := &v1alpha1.DedicatedCLBListenerList{}
	err := c.List(ctx, listenerList)
	if err != nil {
		return err
	}
	p.cache, p.podAllocate = initLbCache(listenerList.Items, p.minPort, p.maxPort)
	log.Infof("[%s] podAllocate cache complete initialization: %v", ClbNetwork, p.podAllocate)
	return nil
}

func initLbCache(listenerList []v1alpha1.DedicatedCLBListener, minPort, maxPort int32) (map[string]portAllocated, map[string][]string) {
	newCache := make(map[string]portAllocated)
	newPodAllocate := make(map[string][]string)
	for _, lis := range listenerList {
		podName, exist := lis.GetLabels()[OwnerPodKey]
		if !exist || podName == "" {
			continue
		}
		if lis.Spec.LbPort > int64(maxPort) || lis.Spec.LbPort < int64(minPort) {
			continue
		}
		lbId := lis.Spec.LbId
		if newCache[lbId] == nil {
			newCache[lbId] = make(portAllocated, maxPort-minPort)
			for i := minPort; i < maxPort; i++ {
				newCache[lbId][i] = false
			}
		}
		newCache[lbId][int32(lis.Spec.LbPort)] = true
		podKey := lis.GetNamespace() + "/" + podName
		newPodAllocate[podKey] = append(newPodAllocate[podKey], fmt.Sprintf("%s:%d", lbId, lis.Spec.LbPort))
	}
	return newCache, newPodAllocate
}

func (p *ClbPlugin) OnPodAdded(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	return pod, nil
}

func (p *ClbPlugin) deleteListener(ctx context.Context, c client.Client, lis *v1alpha1.DedicatedCLBListener) cperrors.PluginError {
	err := c.Delete(ctx, lis)
	if err != nil {
		return cperrors.NewPluginError(cperrors.ApiCallError, err.Error())
	}
	if pm := p.cache[lis.Spec.LbId]; pm != nil {
		pm[int32(lis.Spec.LbPort)] = false
	}
	var podName string
	if targetPod := lis.Spec.TargetPod; targetPod != nil {
		podName = targetPod.PodName
	} else if lis.Labels != nil && lis.Labels[TargetPortKey] != "" && lis.Labels[OwnerPodKey] != "" {
		podName = lis.Labels[OwnerPodKey]
	} else {
		return nil
	}
	target := fmt.Sprintf("%s/%d", lis.Spec.LbId, lis.Spec.LbPort)
	p.podAllocate[podName] = slices.DeleteFunc(p.podAllocate[podName], func(el string) bool {
		return el == target
	})
	return nil
}

func (p *ClbPlugin) OnPodUpdated(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	if pod.DeletionTimestamp != nil {
		return pod, nil
	}
	networkManager := utils.NewNetworkManager(pod, c)
	networkStatus, _ := networkManager.GetNetworkStatus()
	if networkStatus == nil {
		pod, err := networkManager.UpdateNetworkStatus(kruisev1alpha1.NetworkStatus{
			CurrentNetworkState: kruisev1alpha1.NetworkNotReady,
		}, pod)
		return pod, cperrors.ToPluginError(err, cperrors.InternalError)
	}
	networkConfig := networkManager.GetNetworkConfig()
	clbConf, err := parseLbConfig(networkConfig)
	if err != nil {
		return pod, cperrors.ToPluginError(err, cperrors.ParameterError)
	}

	gss, err := util.GetGameServerSetOfPod(pod, c, ctx)
	if err != nil && !errors.IsNotFound(err) {
		return pod, cperrors.ToPluginError(err, cperrors.ApiCallError)
	}

	// get related dedicated clb listeners
	listeners := &v1alpha1.DedicatedCLBListenerList{}
	if err := c.List(
		ctx, listeners,
		client.InNamespace(pod.Namespace),
		client.MatchingLabels{
			OwnerPodKey:                          pod.Name,
			kruisev1alpha1.GameServerOwnerGssKey: gss.Name,
		},
	); err != nil {
		return pod, cperrors.NewPluginError(cperrors.ApiCallError, err.Error())
	}

	// reconcile
	lisMap := make(map[portProtocol]v1alpha1.DedicatedCLBListener)

	for _, lis := range listeners.Items {
		// ignore deleting dedicated clb listener
		if lis.DeletionTimestamp != nil {
			continue
		}
		// old dedicated clb listener remain
		if lis.OwnerReferences[0].Kind == "Pod" && lis.OwnerReferences[0].UID != pod.UID {
			log.Infof("[%s] waitting old dedicated clb listener %s/%s deleted. old owner pod uid is %s, but now is %s", ClbNetwork, lis.Namespace, lis.Name, lis.OwnerReferences[0].UID, pod.UID)
			return pod, nil
		}

		targetPod := lis.Spec.TargetPod
		if targetPod != nil && targetPod.PodName == pod.Name {
			port := portProtocol{
				port:     int(targetPod.TargetPort),
				protocol: lis.Spec.Protocol,
			}
			lisMap[port] = lis
		} else if targetPod == nil && (lis.Labels != nil && lis.Labels[TargetPortKey] != "") {
			targetPort, err := strconv.Atoi(lis.Labels[TargetPortKey])
			if err != nil {
				log.Warningf("[%s] invalid dedicated clb listener target port annotation %s/%s: %s", ClbNetwork, lis.Namespace, lis.Name, err.Error())
				continue
			}
			port := portProtocol{
				port:     targetPort,
				protocol: lis.Spec.Protocol,
			}
			// lower priority than targetPod is not nil
			if _, exists := lisMap[port]; !exists {
				lisMap[port] = lis
			}
		}
	}

	internalAddresses := make([]kruisev1alpha1.NetworkAddress, 0)
	externalAddresses := make([]kruisev1alpha1.NetworkAddress, 0)

	for _, port := range clbConf.targetPorts {
		if lis, ok := lisMap[port]; !ok { // no dedicated clb listener, try to create one
			if networkManager.GetNetworkDisabled() {
				continue
			}
			// ensure not ready while creating the listener
			networkStatus.CurrentNetworkState = kruisev1alpha1.NetworkNotReady
			pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
			if err != nil {
				return pod, cperrors.NewPluginError(cperrors.InternalError, err.Error())
			}
			// allocate and create new listener bound to pod
			newLis, err := p.consLis(ctx, c, clbConf, pod, port, gss.Name)
			if err != nil {
				return pod, cperrors.ToPluginError(err, cperrors.InternalError)
			}
			err = c.Create(ctx, newLis)
			if err != nil {
				return pod, cperrors.NewPluginError(cperrors.ApiCallError, err.Error())
			}
		} else { // already created dedicated clb listener bound to pod
			delete(lisMap, port)
			if networkManager.GetNetworkDisabled() { // disable network
				// deregister pod if networkDisabled is true
				if lis.Spec.TargetPod != nil {
					lis.Spec.TargetPod = nil
					err = c.Update(ctx, &lis)
					if err != nil {
						return pod, cperrors.ToPluginError(err, cperrors.ApiCallError)
					}
				}
			} else { // enable network
				if lis.Spec.TargetPod == nil { // ensure target pod is bound to dedicated clb listener
					lis.Spec.TargetPod = &v1alpha1.TargetPod{
						PodName:    pod.Name,
						TargetPort: int64(port.port),
					}
					err = c.Update(ctx, &lis)
					if err != nil {
						return pod, cperrors.ToPluginError(err, cperrors.ApiCallError)
					}
				} else {
					//  recreate dedicated clb listener if necessary (config changed)
					if !slices.Contains(clbConf.lbIds, lis.Spec.LbId) || lis.Spec.LbPort > int64(p.maxPort) || lis.Spec.LbPort < int64(p.minPort) || lis.Spec.Protocol != port.protocol || lis.Spec.TargetPod.TargetPort != int64(port.port) {
						// ensure not ready while recreating the listener
						networkStatus.CurrentNetworkState = kruisev1alpha1.NetworkNotReady
						pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
						if err != nil {
							return pod, cperrors.NewPluginError(cperrors.InternalError, err.Error())
						}

						// delete old listener
						err := p.deleteListener(ctx, c, &lis)
						if err != nil {
							return pod, err
						}

						// allocate and create new listener bound to pod
						if newLis, err := p.consLis(ctx, c, clbConf, pod, port, gss.Name); err != nil {
							return pod, cperrors.ToPluginError(err, cperrors.InternalError)
						} else {
							err := c.Create(ctx, newLis)
							if err != nil {
								return pod, cperrors.NewPluginError(cperrors.ApiCallError, err.Error())
							}
						}
					} else { // dedicated clb listener is desired, check status
						if lis.Status.State == v1alpha1.DedicatedCLBListenerStateBound && lis.Status.Address != "" { // network ready
							ss := strings.Split(lis.Status.Address, ":")
							if len(ss) != 2 {
								return pod, cperrors.NewPluginError(cperrors.InternalError, fmt.Sprintf("invalid dedicated clb listener address %s", lis.Status.Address))
							}
							lbPort, err := strconv.Atoi(ss[1])
							if err != nil {
								return pod, cperrors.NewPluginError(cperrors.InternalError, fmt.Sprintf("invalid dedicated clb listener port %s", ss[1]))
							}
							instrIPort := intstr.FromInt(int(port.port))
							instrEPort := intstr.FromInt(lbPort)
							internalAddresses = append(internalAddresses, kruisev1alpha1.NetworkAddress{
								IP: pod.Status.PodIP,
								Ports: []kruisev1alpha1.NetworkPort{
									{
										Name:     instrIPort.String(),
										Port:     &instrIPort,
										Protocol: corev1.Protocol(port.protocol),
									},
								},
							})
							externalAddresses = append(externalAddresses, kruisev1alpha1.NetworkAddress{
								IP: ss[0],
								Ports: []kruisev1alpha1.NetworkPort{
									{
										Name:     instrIPort.String(),
										Port:     &instrEPort,
										Protocol: corev1.Protocol(port.protocol),
									},
								},
							})
						} else { // network not ready
							networkStatus.CurrentNetworkState = kruisev1alpha1.NetworkNotReady
							pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
							if err != nil {
								return pod, cperrors.NewPluginError(cperrors.InternalError, err.Error())
							}
						}
					}
				}
			}
		}
	}

	// other dedicated clb listener is not used, delete it
	for _, lis := range lisMap {
		err := p.deleteListener(ctx, c, &lis)
		if err != nil {
			return pod, err
		}
	}

	// set network status to ready when all lb port is ready
	if len(externalAddresses) == len(clbConf.targetPorts) {
		// change network status to ready if necessary
		if !reflect.DeepEqual(externalAddresses, networkStatus.ExternalAddresses) || networkStatus.CurrentNetworkState != kruisev1alpha1.NetworkReady {
			networkStatus.InternalAddresses = internalAddresses
			networkStatus.ExternalAddresses = externalAddresses
			networkStatus.CurrentNetworkState = kruisev1alpha1.NetworkReady
			pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
			if err != nil {
				return pod, cperrors.NewPluginError(cperrors.InternalError, err.Error())
			}
		}
	}

	return pod, nil
}

func (p *ClbPlugin) OnPodDeleted(c client.Client, pod *corev1.Pod, ctx context.Context) cperrors.PluginError {
	p.deAllocate(ctx, c, pod.GetNamespace()+"/"+pod.GetName(), pod.Namespace)
	return nil
}

func (p *ClbPlugin) consLis(ctx context.Context, c client.Client, clbConf *clbConfig, pod *corev1.Pod, port portProtocol, gssName string) (*v1alpha1.DedicatedCLBListener, error) {
	lbId, lbPort := p.allocate(ctx, c, clbConf.lbIds, pod.GetNamespace()+"/"+pod.GetName(), pod)
	if lbId == "" {
		return nil, fmt.Errorf("there are no avaialable ports for %v", clbConf.lbIds)
	}
	lis := &v1alpha1.DedicatedCLBListener{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: pod.Name + "-",
			Namespace:    pod.Namespace,
			Labels: map[string]string{
				OwnerPodKey:                          pod.Name,                // used to select pod related dedicated clb listener
				TargetPortKey:                        strconv.Itoa(port.port), // used to recover clb pod binding when networkDisabled set from true to false
				kruisev1alpha1.GameServerOwnerGssKey: gssName,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         pod.APIVersion,
					Kind:               pod.Kind,
					Name:               pod.GetName(),
					UID:                pod.GetUID(),
					Controller:         ptr.To[bool](true),
					BlockOwnerDeletion: ptr.To[bool](true),
				},
			},
		},
		Spec: v1alpha1.DedicatedCLBListenerSpec{
			LbId:     lbId,
			LbPort:   int64(lbPort),
			Protocol: port.protocol,
			TargetPod: &v1alpha1.TargetPod{
				PodName:    pod.Name,
				TargetPort: int64(port.port),
			},
		},
	}
	return lis, nil
}

func init() {
	clbPlugin := ClbPlugin{
		mutex: sync.RWMutex{},
	}
	tencentCloudProvider.registerPlugin(&clbPlugin)
}

func parseLbConfig(conf []kruisev1alpha1.NetworkConfParams) (*clbConfig, error) {
	var lbIds []string
	ports := []portProtocol{}
	for _, c := range conf {
		switch c.Name {
		case ClbIdsConfigName:
			for _, clbId := range strings.Split(c.Value, ",") {
				if clbId != "" {
					lbIds = append(lbIds, clbId)
				}
			}
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
	return &clbConfig{
		lbIds:       lbIds,
		targetPorts: ports,
	}, nil
}

func (p *ClbPlugin) allocate(ctx context.Context, c client.Client, lbIds []string, podKey string, pod *corev1.Pod) (string, int32) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	var lbId string
	var port int32

	// find avaialable port
	for _, clbId := range lbIds {
		for i := p.minPort; i < p.maxPort; i++ {
			if !p.cache[clbId][i] {
				lbId = clbId
				port = i
				break
			}
		}
	}
	// update cache
	if lbId != "" {
		if p.cache[lbId] == nil { // init lb cache if not exist
			p.cache[lbId] = make(portAllocated, p.maxPort-p.minPort)
			for i := p.minPort; i < p.maxPort; i++ {
				p.cache[lbId][i] = false
			}
		}
		p.cache[lbId][port] = true
		p.podAllocate[podKey] = append(p.podAllocate[podKey], fmt.Sprintf("%s:%d", lbId, port))
		log.Infof("pod %s allocate clb %s port %d", podKey, lbId, port)

		alloc := &kruisev1alpha1.NetworkAllocation{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%s-%d", pod.Name, lbId, port),
				Namespace: pod.Namespace,
			},
			Spec: kruisev1alpha1.NetworkAllocationSpec{
				LbID:     lbId,
				Port:     port,
				Protocol: string(corev1.ProtocolTCP),
				PodRef: corev1.ObjectReference{
					Kind:      "Pod",
					Namespace: pod.Namespace,
					Name:      pod.Name,
					UID:       pod.UID,
				},
			},
		}
		_ = c.Create(ctx, alloc)
	}
	return lbId, port
}

func (p *ClbPlugin) deAllocate(ctx context.Context, c client.Client, podKey, namespace string) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	allocatedPorts, exist := p.podAllocate[podKey]
	if !exist {
		return
	}

	for _, port := range allocatedPorts {
		ss := strings.Split(port, ":")
		if len(ss) != 2 {
			log.Errorf("bad allocated port cache format %s", port)
			continue
		}
		lbId := ss[0]
		lbPort, err := strconv.Atoi(ss[1])
		if err != nil {
			log.Errorf("failed to parse allocated port %s: %s", port, err.Error())
			continue
		}
		p.cache[lbId][int32(lbPort)] = false
		log.Infof("pod %s deallocate clb %s ports %d", podKey, lbId, lbPort)
		name := fmt.Sprintf("%s-%s-%d", strings.Split(podKey, "/")[1], lbId, lbPort)
		alloc := &kruisev1alpha1.NetworkAllocation{}
		if err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, alloc); err == nil {
			_ = c.Delete(ctx, alloc)
		}
	}
	delete(p.podAllocate, podKey)
}
