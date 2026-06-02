/*
Copyright 2025 The Kruise Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package googlecloud

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	gcpv1beta1 "github.com/openkruise/kruise-game/cloudprovider/googlecloud/apis/compute/v1beta1"
	provideroptions "github.com/openkruise/kruise-game/cloudprovider/options"
)

// These tests exercise the cluster-bootstrap paths (VerifyKCCInstalled discovery
// + Init) that a fake client cannot reach. They require the envtest control-plane
// binaries; when KUBEBUILDER_ASSETS is unset (bare `go test`), they skip. The
// Makefile's `test` target sets KUBEBUILDER_ASSETS so CI exercises them.

func startEnvtest(t *testing.T) *rest.Config {
	t.Helper()
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS not set; skipping envtest-backed test")
	}
	env := &envtest.Environment{}
	cfg, err := env.Start()
	if err != nil {
		t.Fatalf("start envtest: %v", err)
	}
	t.Cleanup(func() { _ = env.Stop() })
	return cfg
}

// kccComputeCRDs builds minimal CRDs for the six KCC compute kinds the plugins
// depend on, so VerifyKCCInstalled's discovery probe finds them.
func kccComputeCRDs() []*apiextensionsv1.CustomResourceDefinition {
	kinds := []struct{ kind, plural string }{
		{"ComputeAddress", "computeaddresses"},
		{"ComputeForwardingRule", "computeforwardingrules"},
		{"ComputeBackendService", "computebackendservices"},
		{"ComputeHealthCheck", "computehealthchecks"},
		{"ComputeTargetTCPProxy", "computetargettcpproxies"},
		{"ComputeFirewall", "computefirewalls"},
	}
	preserve := true
	out := make([]*apiextensionsv1.CustomResourceDefinition, 0, len(kinds))
	for _, k := range kinds {
		out = append(out, &apiextensionsv1.CustomResourceDefinition{
			ObjectMeta: metav1.ObjectMeta{Name: k.plural + "." + gcpv1beta1.GroupName},
			Spec: apiextensionsv1.CustomResourceDefinitionSpec{
				Group: gcpv1beta1.GroupName,
				Names: apiextensionsv1.CustomResourceDefinitionNames{
					Plural: k.plural,
					Kind:   k.kind,
				},
				Scope: apiextensionsv1.NamespaceScoped,
				Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
					Name:    gcpv1beta1.GroupVersion,
					Served:  true,
					Storage: true,
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
							Type:                   "object",
							XPreserveUnknownFields: &preserve,
						},
					},
				}},
			},
		})
	}
	return out
}

// installPartial installs an explicit subset of CRDs (used to simulate a
// cluster where the compute group exists but a required kind is missing).
func installPartial(t *testing.T, cfg *rest.Config, crds []*apiextensionsv1.CustomResourceDefinition) ([]*apiextensionsv1.CustomResourceDefinition, error) {
	t.Helper()
	return envtest.InstallCRDs(cfg, envtest.CRDInstallOptions{CRDs: crds})
}

// newEnvtestClient builds a controller-runtime client wired with the schemes the
// plugins expect.
func newEnvtestClient(t *testing.T, cfg *rest.Config) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}
	if err := gcpv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("gcp scheme: %v", err)
	}
	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	return c
}

func TestVerifyKCCInstalled_EnvtestMissingThenPresent(t *testing.T) {
	cfg := startEnvtest(t)

	// (1) Compute group absent -> notInstalledError path.
	if err := VerifyKCCInstalled(cfg); err == nil {
		t.Fatalf("expected error before KCC CRDs are installed")
	}

	// (2) Install the KCC compute CRDs, then the probe should pass.
	if _, err := envtest.InstallCRDs(cfg, envtest.CRDInstallOptions{
		CRDs: kccComputeCRDs(),
	}); err != nil {
		t.Fatalf("install KCC CRDs: %v", err)
	}
	if err := VerifyKCCInstalled(cfg); err != nil {
		t.Fatalf("expected success after KCC CRDs installed, got %v", err)
	}
}

func TestInit_EnvtestHappyPath(t *testing.T) {
	cfg := startEnvtest(t)
	if _, err := envtest.InstallCRDs(cfg, envtest.CRDInstallOptions{CRDs: kccComputeCRDs()}); err != nil {
		t.Fatalf("install KCC CRDs: %v", err)
	}

	// Init calls ctrl.GetConfigOrDie(), which reads KUBECONFIG. Point it at a
	// kubeconfig synthesized from the envtest control plane.
	writeKubeconfig(t, cfg)

	c := newEnvtestClient(t, cfg)

	opts := provideroptions.GoogleCloudOptions{
		Enable:         true,
		ProjectID:      "demo-project",
		PassthroughNLB: provideroptions.PassthroughNLBOptions{Enable: true},
		GlobalProxyNLB: provideroptions.GlobalProxyNLBOptions{Enable: true},
	}

	pp := &PassthroughNlbPlugin{}
	if err := pp.Init(c, opts, context.Background()); err != nil {
		t.Fatalf("passthrough Init happy path: %v", err)
	}
	if pp.kccClient(c) == nil {
		t.Errorf("passthrough apiClient should be populated after Init")
	}

	gp := &GlobalProxyNlbPlugin{}
	if err := gp.Init(c, opts, context.Background()); err != nil {
		t.Fatalf("proxy Init happy path: %v", err)
	}
	if gp.kccClient(c) == nil {
		t.Errorf("proxy apiClient should be populated after Init")
	}
}

// writeKubeconfig synthesizes a kubeconfig from the envtest control plane's
// admin TLS credentials and points KUBECONFIG at it for the test's duration.
func writeKubeconfig(t *testing.T, cfg *rest.Config) {
	t.Helper()
	kc := clientcmdapi.NewConfig()
	kc.Clusters["envtest"] = &clientcmdapi.Cluster{
		Server:                   cfg.Host,
		CertificateAuthorityData: cfg.CAData,
	}
	kc.AuthInfos["admin"] = &clientcmdapi.AuthInfo{
		ClientCertificateData: cfg.CertData,
		ClientKeyData:         cfg.KeyData,
	}
	kc.Contexts["envtest"] = &clientcmdapi.Context{Cluster: "envtest", AuthInfo: "admin"}
	kc.CurrentContext = "envtest"
	kubeconfig, err := clientcmd.Write(*kc)
	if err != nil {
		t.Fatalf("write kubeconfig bytes: %v", err)
	}
	path := filepath.Join(t.TempDir(), "kubeconfig")
	if err := os.WriteFile(path, kubeconfig, 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	old := os.Getenv("KUBECONFIG")
	if err := os.Setenv("KUBECONFIG", path); err != nil {
		t.Fatalf("set KUBECONFIG: %v", err)
	}
	t.Cleanup(func() {
		if old == "" {
			_ = os.Unsetenv("KUBECONFIG")
		} else {
			_ = os.Setenv("KUBECONFIG", old)
		}
	})
}
