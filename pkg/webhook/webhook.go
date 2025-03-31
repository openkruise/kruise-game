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

package webhook

import (
	"context"
	"flag"
	"fmt"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	manager2 "github.com/openkruise/kruise-game/cloudprovider/manager"
	"github.com/openkruise/kruise-game/pkg/webhook/util/generator"
	"github.com/openkruise/kruise-game/pkg/webhook/util/writer"
)

var (
	mutatePodPath                      = "/mutate-v1-pod"
	validateGssPath                    = "/validate-v1alpha1-gss"
	mutatingWebhookConfigurationName   = "kruise-game-mutating-webhook"
	validatingWebhookConfigurationName = "kruise-game-validating-webhook"
)

var (
	webhookPort             int
	webhookCertDir          string
	webhookServiceNamespace string
	webhookServiceName      string
)

func init() {
	flag.IntVar(&webhookPort, "webhook-port", 9876, "The port of the MutatingWebhookConfiguration object.")
	flag.StringVar(&webhookCertDir, "webhook-server-certs-dir", "/tmp/webhook-certs/", "Path to the X.509-formatted webhook certificate.")
	flag.StringVar(&webhookServiceNamespace, "webhook-service-namespace", "kruise-game-system", "kruise game webhook service namespace.")
	flag.StringVar(&webhookServiceName, "webhook-service-name", "kruise-game-webhook-service", "kruise game wehook service name.")
}

// +kubebuilder:rbac:groups=apps.kruise.io,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps.kruise.io,resources=statefulsets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps.kruise.io,resources=podprobemarkers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=nodes/status,verbs=get
// +kubebuilder:rbac:groups=core,resources=persistentvolumeclaims,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=persistentvolumeclaims/status,verbs=get
// +kubebuilder:rbac:groups=core,resources=persistentvolumes,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=persistentvolumes/status,verbs=get
// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=mutatingwebhookconfigurations,verbs=create;get;list;watch;update;patch
// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=validatingwebhookconfigurations,verbs=create;get;list;watch;update;patch
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=alibabacloud.com,resources=poddnats,verbs=get;list;watch
// +kubebuilder:rbac:groups=alibabacloud.com,resources=poddnats/status,verbs=get
// +kubebuilder:rbac:groups=alibabacloud.com,resources=podeips,verbs=get;list;watch
// +kubebuilder:rbac:groups=alibabacloud.com,resources=podeips/status,verbs=get
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=elbv2.k8s.aws,resources=targetgroupbindings,verbs=create;get;list;patch;update;watch
// +kubebuilder:rbac:groups=elbv2.services.k8s.aws,resources=listeners,verbs=create;get;list;patch;update;watch
// +kubebuilder:rbac:groups=elbv2.services.k8s.aws,resources=targetgroups,verbs=create;get;list;patch;update;watch
// +kubebuilder:rbac:groups=networking.cloud.tencent.com,resources=dedicatedclblisteners,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.cloud.tencent.com,resources=dedicatedclblisteners/status,verbs=get

type Webhook struct {
	mgr manager.Manager
	cpm *manager2.ProviderManager
}

func NewWebhookServer(mgr manager.Manager, cpm *manager2.ProviderManager) *Webhook {
	return &Webhook{
		mgr: mgr,
		cpm: cpm,
	}
}

func (ws *Webhook) SetupWithManager(mgr manager.Manager) *Webhook {
	server := mgr.GetWebhookServer()
	decoder := admission.NewDecoder(runtime.NewScheme())

	// Register the webhook server
	recorder := mgr.GetEventRecorderFor("kruise-game-webhook")
	server.Register(mutatePodPath, &webhook.Admission{Handler: NewPodMutatingHandler(mgr.GetClient(), decoder, ws.cpm, recorder)})
	server.Register(validateGssPath, &webhook.Admission{Handler: &GssValidaatingHandler{Client: mgr.GetClient(), decoder: decoder, CloudProviderManager: ws.cpm}})
	return ws
}

// Initialize create MutatingWebhookConfiguration before start
func (ws *Webhook) Initialize(cfg *rest.Config) error {
	dnsName := generator.ServiceToCommonName(webhookServiceNamespace, webhookServiceName)

	var certWriter writer.CertWriter
	var err error

	certWriter, err = writer.NewFSCertWriter(writer.FSCertWriterOptions{Path: webhookCertDir})
	if err != nil {
		return fmt.Errorf("failed to constructs FSCertWriter: %v", err)
	}

	certs, _, err := certWriter.EnsureCert(dnsName)
	if err != nil {
		return fmt.Errorf("failed to ensure certs: %v", err)
	}

	if err := writer.WriteCertsToDir(webhookCertDir, certs); err != nil {
		return fmt.Errorf("failed to write certs to dir: %v", err)
	}

	clientSet, err := clientset.NewForConfig(cfg)
	if err != nil {
		return err
	}

	if err := checkValidatingConfiguration(dnsName, clientSet, certs.CACert); err != nil {
		return fmt.Errorf("failed to check mutating webhook,because of %s", err.Error())
	}

	if err := checkMutatingConfiguration(dnsName, clientSet, certs.CACert); err != nil {
		return fmt.Errorf("failed to check mutating webhook,because of %s", err.Error())
	}
	return nil
}

func checkValidatingConfiguration(dnsName string, kubeClient clientset.Interface, caBundle []byte) error {
	vwc, err := kubeClient.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(context.TODO(), validatingWebhookConfigurationName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// create new webhook
			return createValidatingWebhook(dnsName, kubeClient, caBundle)
		} else {
			return err
		}
	}
	return updateValidatingWebhook(vwc, dnsName, kubeClient, caBundle)
}

func checkMutatingConfiguration(dnsName string, kubeClient clientset.Interface, caBundle []byte) error {
	mwc, err := kubeClient.AdmissionregistrationV1().MutatingWebhookConfigurations().Get(context.TODO(), mutatingWebhookConfigurationName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// create new webhook
			return createMutatingWebhook(dnsName, kubeClient, caBundle)
		} else {
			return err
		}
	}
	return updateMutatingWebhook(mwc, dnsName, kubeClient, caBundle)
}

func createValidatingWebhook(dnsName string, kubeClient clientset.Interface, caBundle []byte) error {
	webhookConfig := &admissionregistrationv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: validatingWebhookConfigurationName,
		},
		Webhooks: getValidatingWebhookConf(dnsName, caBundle),
	}

	if _, err := kubeClient.AdmissionregistrationV1().ValidatingWebhookConfigurations().Create(context.TODO(), webhookConfig, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("failed to create %s: %v", validatingWebhookConfigurationName, err)
	}
	return nil
}

func createMutatingWebhook(dnsName string, kubeClient clientset.Interface, caBundle []byte) error {
	webhookConfig := &admissionregistrationv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: mutatingWebhookConfigurationName,
		},
		Webhooks: getMutatingWebhookConf(dnsName, caBundle),
	}

	if _, err := kubeClient.AdmissionregistrationV1().MutatingWebhookConfigurations().Create(context.TODO(), webhookConfig, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("failed to create %s: %v", mutatingWebhookConfigurationName, err)
	}
	return nil
}

func updateValidatingWebhook(vwc *admissionregistrationv1.ValidatingWebhookConfiguration, dnsName string, kubeClient clientset.Interface, caBundle []byte) error {
	vwc.Webhooks = getValidatingWebhookConf(dnsName, caBundle)
	if _, err := kubeClient.AdmissionregistrationV1().ValidatingWebhookConfigurations().Update(context.TODO(), vwc, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("failed to update %s: %v", validatingWebhookConfigurationName, err)
	}
	return nil
}

func updateMutatingWebhook(mwc *admissionregistrationv1.MutatingWebhookConfiguration, dnsName string, kubeClient clientset.Interface, caBundle []byte) error {
	mwc.Webhooks = getMutatingWebhookConf(dnsName, caBundle)
	if _, err := kubeClient.AdmissionregistrationV1().MutatingWebhookConfigurations().Update(context.TODO(), mwc, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("failed to update %s: %v", mutatingWebhookConfigurationName, err)
	}
	return nil
}

func getValidatingWebhookConf(dnsName string, caBundle []byte) []admissionregistrationv1.ValidatingWebhook {
	sideEffectClassNone := admissionregistrationv1.SideEffectClassNone
	fail := admissionregistrationv1.Fail
	return []admissionregistrationv1.ValidatingWebhook{
		{
			Name:                    dnsName,
			SideEffects:             &sideEffectClassNone,
			FailurePolicy:           &fail,
			AdmissionReviewVersions: []string{"v1", "v1beta1"},
			ClientConfig: admissionregistrationv1.WebhookClientConfig{
				Service: &admissionregistrationv1.ServiceReference{
					Namespace: webhookServiceNamespace,
					Name:      webhookServiceName,
					Path:      &validateGssPath,
				},
				CABundle: caBundle,
			},
			Rules: []admissionregistrationv1.RuleWithOperations{
				{
					Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create, admissionregistrationv1.Update},
					Rule: admissionregistrationv1.Rule{
						APIGroups:   []string{"game.kruise.io"},
						APIVersions: []string{"v1alpha1"},
						Resources:   []string{"gameserversets"},
					},
				},
			},
		},
	}
}

func getMutatingWebhookConf(dnsName string, caBundle []byte) []admissionregistrationv1.MutatingWebhook {
	sideEffectClassNone := admissionregistrationv1.SideEffectClassNone
	fail := admissionregistrationv1.Fail
	return []admissionregistrationv1.MutatingWebhook{
		{
			Name:                    dnsName,
			SideEffects:             &sideEffectClassNone,
			FailurePolicy:           &fail,
			AdmissionReviewVersions: []string{"v1", "v1beta1"},
			ClientConfig: admissionregistrationv1.WebhookClientConfig{
				Service: &admissionregistrationv1.ServiceReference{
					Namespace: webhookServiceNamespace,
					Name:      webhookServiceName,
					Path:      &mutatePodPath,
				},
				CABundle: caBundle,
			},
			Rules: []admissionregistrationv1.RuleWithOperations{
				{
					Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create, admissionregistrationv1.Update, admissionregistrationv1.Delete},
					Rule: admissionregistrationv1.Rule{
						APIGroups:   []string{""},
						APIVersions: []string{"v1"},
						Resources:   []string{"pods"},
					},
				},
			},
			ObjectSelector: &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      gamekruiseiov1alpha1.GameServerOwnerGssKey,
						Operator: metav1.LabelSelectorOpExists,
						Values:   []string{},
					},
				},
			},
		},
	}
}

func GetPort() int {
	return webhookPort
}

func GetCertDir() string {
	return webhookCertDir
}
