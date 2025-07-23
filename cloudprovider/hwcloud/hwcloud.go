package hwcloud

import (
	"github.com/openkruise/kruise-game/cloudprovider"
	"k8s.io/klog/v2"
)

const (
	HwCloud = "HwCloud"
)

var (
	hwCloudProvider = &Provider{
		plugins: make(map[string]cloudprovider.Plugin),
	}
)

type Provider struct {
	plugins map[string]cloudprovider.Plugin
}

func (ap *Provider) Name() string {
	return HwCloud
}

func (ap *Provider) ListPlugins() (map[string]cloudprovider.Plugin, error) {
	if ap.plugins == nil {
		return make(map[string]cloudprovider.Plugin), nil
	}

	return ap.plugins, nil
}

// register plugin of cloud provider and different cloud providers
func (ap *Provider) registerPlugin(plugin cloudprovider.Plugin) {
	name := plugin.Name()
	if name == "" {
		klog.Fatal("empty plugin name")
	}
	ap.plugins[name] = plugin
}

func NewHwCloudProvider() (cloudprovider.CloudProvider, error) {
	return hwCloudProvider, nil
}
