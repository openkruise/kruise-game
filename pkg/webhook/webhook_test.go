package webhook

import (
	"context"
	"reflect"
	"testing"

	v1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestCheckValidatingConfiguration(t *testing.T) {
	tests := []struct {
		vwcNow   *v1.ValidatingWebhookConfiguration
		caBundle []byte
		vwcNew   *v1.ValidatingWebhookConfiguration
	}{
		{
			vwcNow:   nil,
			caBundle: []byte(`xxx`),
			vwcNew: &v1.ValidatingWebhookConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: validatingWebhookConfigurationName,
				},
				Webhooks: getValidatingWebhookConf([]byte(`xxx`)),
			},
		},
		{
			vwcNow: &v1.ValidatingWebhookConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: validatingWebhookConfigurationName,
				},
				Webhooks: getValidatingWebhookConf([]byte(`old`)),
			},
			caBundle: []byte(`new`),
			vwcNew: &v1.ValidatingWebhookConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: validatingWebhookConfigurationName,
				},
				Webhooks: getValidatingWebhookConf([]byte(`new`)),
			},
		},
		{
			vwcNow: &v1.ValidatingWebhookConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: validatingWebhookConfigurationName,
				},
			},
			caBundle: []byte(`new`),
			vwcNew: &v1.ValidatingWebhookConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: validatingWebhookConfigurationName,
				},
				Webhooks: getValidatingWebhookConf([]byte(`new`)),
			},
		},
	}

	for i, test := range tests {
		clientSet := fake.NewSimpleClientset()
		if test.vwcNow != nil {
			_, err := clientSet.AdmissionregistrationV1().ValidatingWebhookConfigurations().Create(context.TODO(), test.vwcNow, metav1.CreateOptions{})
			if err != nil {
				t.Error(err)
			}
		}

		if err := checkValidatingConfiguration(clientSet, test.caBundle); err != nil {
			t.Error(err)
		}

		expect := test.vwcNew
		actual, err := clientSet.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(context.TODO(), validatingWebhookConfigurationName, metav1.GetOptions{})
		if err != nil {
			t.Error(err)
		}
		if !reflect.DeepEqual(expect.Webhooks, actual.Webhooks) {
			t.Errorf("case %d: expect validatingWebhookConfiguration webhooks %v, but actually got %v", i, expect.Webhooks, actual.Webhooks)
		}
	}
}

func TestCheckMutatingConfiguration(t *testing.T) {
	tests := []struct {
		mwcNow   *v1.MutatingWebhookConfiguration
		caBundle []byte
		mwcNew   *v1.MutatingWebhookConfiguration
	}{
		{
			mwcNow:   nil,
			caBundle: []byte(`xxx`),
			mwcNew: &v1.MutatingWebhookConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: mutatingWebhookConfigurationName,
				},
				Webhooks: getMutatingWebhookConf([]byte(`xxx`)),
			},
		},
		{
			mwcNow: &v1.MutatingWebhookConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: mutatingWebhookConfigurationName,
				},
				Webhooks: getMutatingWebhookConf([]byte(`old`)),
			},
			caBundle: []byte(`new`),
			mwcNew: &v1.MutatingWebhookConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: mutatingWebhookConfigurationName,
				},
				Webhooks: getMutatingWebhookConf([]byte(`new`)),
			},
		},
		{
			mwcNow: &v1.MutatingWebhookConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: mutatingWebhookConfigurationName,
				},
			},
			caBundle: []byte(`new`),
			mwcNew: &v1.MutatingWebhookConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: mutatingWebhookConfigurationName,
				},
				Webhooks: getMutatingWebhookConf([]byte(`new`)),
			},
		},
	}

	for i, test := range tests {
		clientSet := fake.NewSimpleClientset()
		if test.mwcNow != nil {
			_, err := clientSet.AdmissionregistrationV1().MutatingWebhookConfigurations().Create(context.TODO(), test.mwcNow, metav1.CreateOptions{})
			if err != nil {
				t.Error(err)
			}
		}

		if err := checkMutatingConfiguration(clientSet, test.caBundle); err != nil {
			t.Error(err)
		}

		expect := test.mwcNew
		actual, err := clientSet.AdmissionregistrationV1().MutatingWebhookConfigurations().Get(context.TODO(), mutatingWebhookConfigurationName, metav1.GetOptions{})
		if err != nil {
			t.Error(err)
		}
		if !reflect.DeepEqual(expect.Webhooks, actual.Webhooks) {
			t.Errorf("case %d: expect validatingWebhookConfiguration webhooks %v, but actually got %v", i, expect.Webhooks, actual.Webhooks)
		}
	}
}
