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
	"flag"

	"github.com/BurntSushi/toml"
	"github.com/openkruise/kruise-game/cloudprovider/options"
	"k8s.io/klog/v2"
)

var Opt *Options

type Options struct {
	CloudProviderConfigFile string
}

func init() {
	Opt = &Options{}
}

func InitCloudProviderFlags() {
	flag.StringVar(&Opt.CloudProviderConfigFile, "provider-config", "/etc/kruise-game/config.toml", "Cloud Provider Config File Path.")
}

type ConfigFile struct {
	Path string
}

type CloudProviderConfig struct {
	KubernetesOptions         CloudProviderOptions
	AlibabaCloudOptions       CloudProviderOptions
	VolcengineOptions         CloudProviderOptions
	AmazonsWebServicesOptions CloudProviderOptions
	TencentCloudOptions       CloudProviderOptions
}

type tomlConfigs struct {
	Kubernetes         options.KubernetesOptions         `toml:"kubernetes"`
	AlibabaCloud       options.AlibabaCloudOptions       `toml:"alibabacloud"`
	Volcengine         options.VolcengineOptions         `toml:"volcengine"`
	AmazonsWebServices options.AmazonsWebServicesOptions `toml:"aws"`
	TencentCloud       options.TencentCloudOptions       `toml:"tencentcloud"`
}

func (cf *ConfigFile) Parse() *CloudProviderConfig {
	var config tomlConfigs
	if _, err := toml.DecodeFile(cf.Path, &config); err != nil {
		klog.Fatal(err)
	}

	return &CloudProviderConfig{
		KubernetesOptions:         config.Kubernetes,
		AlibabaCloudOptions:       config.AlibabaCloud,
		VolcengineOptions:         config.Volcengine,
		AmazonsWebServicesOptions: config.AmazonsWebServices,
		TencentCloudOptions:       config.TencentCloud,
	}
}

func NewConfigFile(path string) *ConfigFile {
	return &ConfigFile{
		Path: path,
	}
}
