package webhook

import (
	"testing"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/cloudprovider"
	"github.com/openkruise/kruise-game/cloudprovider/alibabacloud"
	"github.com/openkruise/kruise-game/cloudprovider/manager"
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
