/*
Copyright 2025 The Kruise Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package googlecloud

import (
	log "k8s.io/klog/v2"

	"github.com/openkruise/kruise-game/cloudprovider"
)

// GoogleCloud is the name of this cloud provider, returned by Provider.Name().
const GoogleCloud = "GoogleCloud"

var googleCloudProvider = &Provider{
	plugins: make(map[string]cloudprovider.Plugin),
}

// Provider implements cloudprovider.CloudProvider for Google Cloud.
type Provider struct {
	plugins map[string]cloudprovider.Plugin
}

// Name returns the cloud provider name.
func (p *Provider) Name() string { return GoogleCloud }

// ListPlugins returns all registered Google Cloud network plugins.
func (p *Provider) ListPlugins() (map[string]cloudprovider.Plugin, error) {
	if p.plugins == nil {
		return make(map[string]cloudprovider.Plugin), nil
	}
	return p.plugins, nil
}

// registerPlugin registers a plugin into the provider. Each plugin's init()
// calls this to attach itself.
func (p *Provider) registerPlugin(plugin cloudprovider.Plugin) {
	name := plugin.Name()
	if name == "" {
		log.Fatal("empty google cloud plugin name")
	}
	p.plugins[name] = plugin
}

// NewGoogleCloudProvider returns the singleton Google Cloud provider for the
// manager to register.
func NewGoogleCloudProvider() (cloudprovider.CloudProvider, error) {
	return googleCloudProvider, nil
}
