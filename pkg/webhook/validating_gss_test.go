package webhook

import (
	"context"
	"encoding/json"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/cloudprovider"
	"github.com/openkruise/kruise-game/cloudprovider/alibabacloud"
	"github.com/openkruise/kruise-game/cloudprovider/manager"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	"testing"
)

func TestValidatingCreate(t *testing.T) {
	tests := []struct {
		gss     *gamekruiseiov1alpha1.GameServerSet
		cpm     *manager.ProviderManager
		allowed bool
	}{
		{
			gss: &gamekruiseiov1alpha1.GameServerSet{
				Spec: gamekruiseiov1alpha1.GameServerSetSpec{
					Network: &gamekruiseiov1alpha1.Network{
						NetworkConf: []gamekruiseiov1alpha1.NetworkConfParams{
							{
								Name:  "xx",
								Value: "xx",
							},
						},
					},
				},
			},
			cpm: &manager.ProviderManager{
				CloudProviders: map[string]cloudprovider.CloudProvider{},
			},
			allowed: false,
		},
		{
			gss: &gamekruiseiov1alpha1.GameServerSet{
				Spec: gamekruiseiov1alpha1.GameServerSetSpec{
					Network: &gamekruiseiov1alpha1.Network{
						NetworkType: "AlibabaCloud-LB",
					},
				},
			},
			cpm: &manager.ProviderManager{
				CloudProviders: map[string]cloudprovider.CloudProvider{
					"AlibabaCloud": func() cloudprovider.CloudProvider {
						acp, _ := alibabacloud.NewAlibabaCloudProvider()
						return acp
					}(),
				},
			},
			allowed: false,
		},
	}

	for i, test := range tests {
		actual := validatingCreate(test.gss, test.cpm)
		if actual.Allowed != test.allowed {
			t.Errorf("%d: expect %v, got %v", i, test.allowed, actual.Allowed)
		}
	}
}

func TestGssValidatingHandlerRejectsInvalidReserveIds(t *testing.T) {
	gvh := &GssValidaatingHandler{
		decoder: admission.NewDecoder(runtime.NewScheme()),
		CloudProviderManager: &manager.ProviderManager{
			CloudProviders: map[string]cloudprovider.CloudProvider{},
		},
	}

	tests := []struct {
		name       string
		operation  admissionv1.Operation
		reserveIds []intstr.IntOrString
		allowed    bool
	}{
		{
			name:       "create with valid reserve ids",
			operation:  admissionv1.Create,
			reserveIds: []intstr.IntOrString{intstr.FromInt32(1), intstr.FromString("3-5")},
			allowed:    true,
		},
		{
			name:       "create with negative reserve id",
			operation:  admissionv1.Create,
			reserveIds: []intstr.IntOrString{intstr.FromInt32(-1)},
			allowed:    false,
		},
		{
			name:       "create with repeated reserve ids",
			operation:  admissionv1.Create,
			reserveIds: []intstr.IntOrString{intstr.FromInt32(2), intstr.FromString("1-3")},
			allowed:    false,
		},
		{
			name:       "update with invalid range",
			operation:  admissionv1.Update,
			reserveIds: []intstr.IntOrString{intstr.FromString("5-2")},
			allowed:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gss := &gamekruiseiov1alpha1.GameServerSet{
				TypeMeta: metav1.TypeMeta{
					APIVersion: gamekruiseiov1alpha1.GroupVersion.String(),
					Kind:       "GameServerSet",
				},
				ObjectMeta: metav1.ObjectMeta{Name: "gss", Namespace: "default"},
				Spec: gamekruiseiov1alpha1.GameServerSetSpec{
					ReserveGameServerIds: tt.reserveIds,
				},
			}
			raw, err := json.Marshal(gss)
			if err != nil {
				t.Fatal(err)
			}
			req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
				Operation: tt.operation,
				Object:    runtime.RawExtension{Raw: raw},
			}}
			if tt.operation == admissionv1.Update {
				req.OldObject = runtime.RawExtension{Raw: raw}
			}

			resp := gvh.Handle(context.TODO(), req)
			if resp.Allowed != tt.allowed {
				t.Errorf("expect allowed=%v, got %v (reason: %v)", tt.allowed, resp.Allowed, resp.Result)
			}
		})
	}
}
