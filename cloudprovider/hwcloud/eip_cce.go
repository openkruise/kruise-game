/*
Copyright 2022 The Kruise Authors.

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

package hwcloud

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	log "k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/cloudprovider"
	"github.com/openkruise/kruise-game/cloudprovider/errors"
	"github.com/openkruise/kruise-game/cloudprovider/utils"
)

const (
	EIPNetwork = "HwCloud-CCE-EIP"
	AliasSEIP  = "CCE-EIP-Network"
)

var allowedAnnotations = []string{
	"yangtse.io/pod-with-eip",
	"yangtse.io/eip-bandwidth-size",
	"yangtse.io/eip-network-type",
	"yangtse.io/eip-charge-mode",
	"yangtse.io/eip-bandwidth-name",
	"yangtse.io/eip-network-type",
	"yangtse.io/eip-bandwidth-id",
	"yangtse.io/eip-id",
	"yangtse.io/security-group-ids",
	"yangtse.io/additional-security-group-ids",
}

func init() {
	eipPlugin := EipPlugin{}
	hwCloudProvider.registerPlugin(&eipPlugin)
}

type EipPlugin struct{}

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
	log.Infof("pod %s/%s network config: %#v", pod.Namespace, pod.Name, conf)

	if networkManager.GetNetworkType() != EIPNetwork {
		log.Infof("pod %s/%s network type is not %s, skipping", pod.Namespace, pod.Name, EIPNetwork)
		return pod, nil
	}
	allowedAnnotationsMap := make(map[string]struct{})
	for _, item := range allowedAnnotations {
		allowedAnnotationsMap[item] = struct{}{}
	}
	for _, c := range conf {
		_, ok := allowedAnnotationsMap[c.Name]
		if ok {
			pod.Annotations[c.Name] = c.Value
		} else {
			log.Warningf("pod %s/%s network config %s is not allowed", pod.Namespace, pod.Name, c.Name)
		}
	}

	return pod, nil
}

func (E EipPlugin) OnPodUpdated(client client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, errors.PluginError) {
	networkManager := utils.NewNetworkManager(pod, client)
	if networkManager.GetNetworkType() != EIPNetwork {
		log.Infof("pod %s/%s network type is not %s, skipping", pod.Namespace, pod.Name, EIPNetwork)
		return pod, nil
	}
	networkStatus, _ := networkManager.GetNetworkStatus()
	if networkStatus == nil {
		pod, err := networkManager.UpdateNetworkStatus(gamekruiseiov1alpha1.NetworkStatus{
			CurrentNetworkState: gamekruiseiov1alpha1.NetworkWaiting,
		}, pod)
		return pod, errors.ToPluginError(err, errors.InternalError)
	}

	networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkReady

	pod, err := networkManager.UpdateNetworkStatus(*networkStatus, pod)
	return pod, errors.ToPluginError(err, errors.InternalError)
}

func (E EipPlugin) OnPodDeleted(client client.Client, pod *corev1.Pod, ctx context.Context) errors.PluginError {
	return nil
}
