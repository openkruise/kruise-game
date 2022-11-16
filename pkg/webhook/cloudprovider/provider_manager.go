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

package cloudprovider

import (
	"github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/cloudprovider"
	"github.com/openkruise/kruise-game/cloudprovider/alibabacloud"
	"github.com/openkruise/kruise-game/cloudprovider/kubernetes"
	corev1 "k8s.io/api/core/v1"
	log "k8s.io/klog/v2"
)

type ProviderManager struct {
	CloudProviders map[string]cloudprovider.CloudProvider
}

func (pm *ProviderManager) RegisterCloudProvider(provider cloudprovider.CloudProvider) {
	if provider.Name() == "" {
		log.Fatal("EmptyCloudProviderName")
	}

	pm.CloudProviders[provider.Name()] = provider
}

func (pm *ProviderManager) FindAvailablePlugins(pod *corev1.Pod) (cloudprovider.Plugin, bool) {
	// TODO add config file for cloud provider

	pluginType, ok := pod.Annotations[v1alpha1.GameServerNetworkType]
	if !ok {
		log.V(5).Infof("Pod %s has no plugin configured and skip", pod.Name)
		return nil, false
	}

	for _, cp := range pm.CloudProviders {
		plugins, err := cp.ListPlugins()
		if err != nil {
			log.Warningf("Cloud provider %s can not list plugins,because of %s", cp.Name(), err.Error())
			continue
		}
		for _, p := range plugins {
			// TODO add multi plugins supported
			if p.Name() == pluginType {
				return p, true
			}
		}
	}
	return nil, false
}

// NewProviderManager return a new cloud provider manager instance
func NewProviderManager() (*ProviderManager, error) {

	pm := &ProviderManager{
		CloudProviders: make(map[string]cloudprovider.CloudProvider),
	}

	// Register default kubernetes network provider
	kp, err := kubernetes.NewKubernetesProvider()
	if err != nil {
		log.Errorf("Failed to initialized kubernetes provider,because of %s", err.Error())
	} else {
		pm.RegisterCloudProvider(kp)
	}

	// build and register alibaba cloud provider
	acp, err := alibabacloud.NewAlibabaCloudProvider()
	if err != nil {
		log.Errorf("Failed to initialize alibabacloud provider.because of %s", err.Error())
	} else {
		pm.RegisterCloudProvider(acp)
	}

	return pm, nil
}
