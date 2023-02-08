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
	"context"
	"github.com/openkruise/kruise-game/cloudprovider/errors"
	corev1 "k8s.io/api/core/v1"
	client "sigs.k8s.io/controller-runtime/pkg/client"
)

/*
	|-Cloud Provider
		|------ Kubernetes
					|------ plugins
		|------ AlibabaCloud
					|------- plugins
		|------ others
*/

type Plugin interface {
	Name() string
	// Alias define the plugin with similar func cross multi cloud provider
	Alias() string
	Init(client client.Client, options CloudProviderOptions, ctx context.Context) error
	// Pod Event handler
	OnPodAdded(client client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, errors.PluginError)
	OnPodUpdated(client client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, errors.PluginError)
	OnPodDeleted(client client.Client, pod *corev1.Pod, ctx context.Context) errors.PluginError
}

type CloudProvider interface {
	Name() string
	ListPlugins() (map[string]Plugin, error)
}

type CloudProviderOptions interface {
	Enabled() bool
	Valid() bool
}
