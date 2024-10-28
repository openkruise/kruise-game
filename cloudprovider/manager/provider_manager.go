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

package manager

import (
	"context"

	"github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/cloudprovider"
	"github.com/openkruise/kruise-game/cloudprovider/alibabacloud"
	aws "github.com/openkruise/kruise-game/cloudprovider/amazonswebservices"
	"github.com/openkruise/kruise-game/cloudprovider/kubernetes"
	"github.com/openkruise/kruise-game/cloudprovider/tencentcloud"
	volcengine "github.com/openkruise/kruise-game/cloudprovider/volcengine"
	corev1 "k8s.io/api/core/v1"
	log "k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ProviderManager struct {
	CloudProviders map[string]cloudprovider.CloudProvider
	CPOptions      map[string]cloudprovider.CloudProviderOptions
}

func (pm *ProviderManager) FindConfigs(cpName string) cloudprovider.CloudProviderOptions {
	return pm.CPOptions[cpName]
}

func (pm *ProviderManager) RegisterCloudProvider(provider cloudprovider.CloudProvider, options cloudprovider.CloudProviderOptions) {
	if provider.Name() == "" {
		log.Fatal("EmptyCloudProviderName")
	}

	pm.CloudProviders[provider.Name()] = provider
	pm.CPOptions[provider.Name()] = options
}

func (pm *ProviderManager) FindAvailablePlugins(pod *corev1.Pod) (cloudprovider.Plugin, bool) {
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

func (pm *ProviderManager) Init(client client.Client) {
	for _, cp := range pm.CloudProviders {
		name := cp.Name()
		plugins, err := cp.ListPlugins()
		if err != nil {
			continue
		}
		log.Infof("Cloud Provider [%s] has been registered with %d plugins", name, len(plugins))
		for _, p := range plugins {
			err := p.Init(client, pm.FindConfigs(cp.Name()), context.Background())
			if err != nil {
				continue
			}
			log.Infof("plugin [%s] has been registered", p.Name())
		}
	}
}

// NewProviderManager return a new cloud provider manager instance
func NewProviderManager() (*ProviderManager, error) {
	configFile := cloudprovider.NewConfigFile(cloudprovider.Opt.CloudProviderConfigFile)
	configs := configFile.Parse()

	pm := &ProviderManager{
		CloudProviders: make(map[string]cloudprovider.CloudProvider),
		CPOptions:      make(map[string]cloudprovider.CloudProviderOptions),
	}

	if configs.KubernetesOptions.Valid() && configs.KubernetesOptions.Enabled() {
		// Register default kubernetes network provider
		kp, err := kubernetes.NewKubernetesProvider()
		if err != nil {
			log.Errorf("Failed to initialized kubernetes provider,because of %s", err.Error())
		} else {
			pm.RegisterCloudProvider(kp, configs.KubernetesOptions)
		}
	}

	if configs.AlibabaCloudOptions.Valid() && configs.AlibabaCloudOptions.Enabled() {
		// build and register alibaba cloud provider
		acp, err := alibabacloud.NewAlibabaCloudProvider()
		if err != nil {
			log.Errorf("Failed to initialize alibabacloud provider.because of %s", err.Error())
		} else {
			pm.RegisterCloudProvider(acp, configs.AlibabaCloudOptions)
		}
	}

	if configs.VolcengineOptions.Valid() && configs.VolcengineOptions.Enabled() {
		// build and register volcengine cloud provider
		vcp, err := volcengine.NewVolcengineProvider()
		if err != nil {
			log.Errorf("Failed to initialize volcengine provider.because of %s", err.Error())
		} else {
			pm.RegisterCloudProvider(vcp, configs.VolcengineOptions)
		}
	}

	if configs.AmazonsWebServicesOptions.Valid() && configs.AmazonsWebServicesOptions.Enabled() {
		// build and register amazon web services provider
		vcp, err := aws.NewAmazonsWebServicesProvider()
		if err != nil {
			log.Errorf("Failed to initialize amazons web services provider.because of %s", err.Error())
		} else {
			pm.RegisterCloudProvider(vcp, configs.AmazonsWebServicesOptions)
		}
	}

	if configs.TencentCloudOptions.Valid() && configs.TencentCloudOptions.Enabled() {
		// build and register amazon web services provider
		tcp, err := tencentcloud.NewTencentCloudProvider()
		if err != nil {
			log.Errorf("Failed to initialize tencentcloud provider.because of %s", err.Error())
		} else {
			pm.RegisterCloudProvider(tcp, configs.TencentCloudOptions)
		}
	}

	return pm, nil
}
