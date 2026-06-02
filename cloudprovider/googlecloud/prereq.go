/*
Copyright 2025 The Kruise Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package googlecloud

import (
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"

	gcpv1beta1 "github.com/openkruise/kruise-game/cloudprovider/googlecloud/apis/compute/v1beta1"
)

// VerifyKCCInstalled probes the discovery API for the Config Connector compute
// CRDs we depend on. On miss it returns a wrapped error whose message embeds
// the gcloud commands required to install KCC. Plugins call this from Init()
// and refuse to start when it fails (per-plugin failure — does not crash the
// manager).
func VerifyKCCInstalled(restConfig *rest.Config) error {
	if restConfig == nil {
		return fmt.Errorf("googlecloud: nil rest.Config passed to VerifyKCCInstalled")
	}
	dc, err := discovery.NewDiscoveryClientForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("googlecloud: build discovery client: %w", err)
	}
	resources, err := dc.ServerResourcesForGroupVersion(gcpv1beta1.SchemeGroupVersion.String())
	if err != nil {
		if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) || discovery.IsGroupDiscoveryFailedError(err) {
			return notInstalledError()
		}
		return fmt.Errorf("googlecloud: probe %s: %w", gcpv1beta1.SchemeGroupVersion.String(), err)
	}
	required := map[string]bool{
		"ComputeAddress":        false,
		"ComputeForwardingRule": false,
		"ComputeBackendService": false,
		"ComputeHealthCheck":    false,
		"ComputeTargetTCPProxy": false,
		"ComputeFirewall":       false,
	}
	for _, r := range resources.APIResources {
		if _, want := required[r.Kind]; want {
			required[r.Kind] = true
		}
	}
	missing := []string{}
	for k, present := range required {
		if !present {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf(
			"googlecloud: Config Connector compute CRDs missing in cluster (%v). "+
				"Install KCC via the GKE add-on then wait for the cnrm-system controller-manager to settle:\n"+
				"  gcloud container clusters update <CLUSTER> --update-addons=ConfigConnector=ENABLED --region <REGION>\n"+
				"  kubectl wait --for=condition=Available deploy -n cnrm-system --all --timeout=300s",
			missing,
		)
	}
	return nil
}

func notInstalledError() error {
	return fmt.Errorf(
		"googlecloud: Config Connector API group %s not installed. "+
			"Install KCC via the GKE add-on:\n"+
			"  gcloud container clusters update <CLUSTER> --update-addons=ConfigConnector=ENABLED --region <REGION>\n"+
			"  kubectl wait --for=condition=Available deploy -n cnrm-system --all --timeout=300s\n"+
			"Alternatively, for non-GKE clusters install the standalone Config Connector operator: "+
			"https://docs.cloud.google.com/config-connector/docs/how-to/install-upgrade-uninstall",
		gcpv1beta1.SchemeGroupVersion.String(),
	)
}
