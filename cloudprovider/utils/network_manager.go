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

package utils

import (
	"context"
	"errors"
	"github.com/openkruise/kruise-game/apis/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/json"
	log "k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strconv"
)

type NetworkManager struct {
	pod             *corev1.Pod
	networkType     string
	networkConf     []v1alpha1.NetworkConfParams
	networkStatus   *v1alpha1.NetworkStatus
	networkDisabled bool
	client          client.Client
}

func (nm *NetworkManager) GetNetworkDisabled() bool {
	return nm.networkDisabled
}

func (nm *NetworkManager) SetNetworkState(disabled bool) error {
	patchPod := nm.pod.DeepCopy()
	if patchPod == nil {
		return errors.New("EmptyPodError")
	}

	// Initial annotations if necessary
	if patchPod.Labels == nil {
		patchPod.Labels = make(map[string]string)
	}

	patchPod.Labels[v1alpha1.GameServerNetworkDisabled] = strconv.FormatBool(disabled)
	patch := client.MergeFrom(patchPod)
	return nm.client.Patch(context.Background(), nm.pod, patch, nil)
}

func (nm *NetworkManager) GetNetworkStatus() (*v1alpha1.NetworkStatus, error) {
	p := nm.pod.DeepCopy()
	if p == nil || p.Annotations == nil {
		return nil, errors.New("EmptyPodError")
	}
	networkStatusStr := p.Annotations[v1alpha1.GameServerNetworkStatus]

	if networkStatusStr == "" {
		return nil, nil
	}
	networkStatus := &v1alpha1.NetworkStatus{}

	err := json.Unmarshal([]byte(networkStatusStr), networkStatus)
	if err != nil {
		log.Errorf("Failed to unmarshal pod %s networkStatus,because of %s", p.Name, err.Error())
		return nil, err
	}

	return networkStatus, nil
}

func (nm *NetworkManager) UpdateNetworkStatus(networkStatus v1alpha1.NetworkStatus, pod *corev1.Pod) (*corev1.Pod, error) {
	networkStatusBytes, err := json.Marshal(networkStatus)
	if err != nil {
		log.Errorf("pod %s can not update networkStatus,because of %s", nm.pod.Name, err.Error())
		return pod, err
	}
	pod.Annotations[v1alpha1.GameServerNetworkStatus] = string(networkStatusBytes)
	return pod, nil
}

func (nm *NetworkManager) GetNetworkConfig() []v1alpha1.NetworkConfParams {
	return nm.networkConf
}

func (nm *NetworkManager) GetNetworkType() string {
	return nm.networkType
}

func NewNetworkManager(pod *corev1.Pod, client client.Client) *NetworkManager {
	var ok bool
	var err error

	var networkType string
	if networkType, ok = pod.Annotations[v1alpha1.GameServerNetworkType]; !ok {
		log.V(5).Infof("Pod %s has no network conf", pod.Name)
		return nil
	}

	var networkConfStr string
	var networkConf []v1alpha1.NetworkConfParams
	if networkConfStr, ok = pod.Annotations[v1alpha1.GameServerNetworkConf]; ok {
		err = json.Unmarshal([]byte(networkConfStr), &networkConf)
		if err != nil {
			log.Warningf("Pod %s has invalid network conf, err: %s", pod.Name, err.Error())
			return nil
		}
	}

	// If valid and use status as default
	var networkStatusStr string
	networkStatus := &v1alpha1.NetworkStatus{}
	if networkStatusStr, ok = pod.Annotations[v1alpha1.GameServerNetworkStatus]; ok {
		err = json.Unmarshal([]byte(networkStatusStr), networkStatus)
		if err != nil {
			log.Warningf("Pod %s has invalid network status, err: %s", pod.Name, err.Error())
		}
	}

	var networkDisabled bool
	if networkDisabledStr, ok := pod.Labels[v1alpha1.GameServerNetworkDisabled]; ok {
		networkDisabled, err = strconv.ParseBool(networkDisabledStr)
		if err != nil {
			log.Warningf("Pod %s has invalid network disabled option, err: %s", pod.Name, err.Error())
		}
	}

	return &NetworkManager{
		pod:             pod,
		networkType:     networkType,
		networkConf:     networkConf,
		networkStatus:   networkStatus,
		networkDisabled: networkDisabled,
		client:          client,
	}
}
