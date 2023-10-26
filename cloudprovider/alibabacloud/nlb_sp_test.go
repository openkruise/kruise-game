package alibabacloud

import (
	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"reflect"
	"testing"
)

func TestParseNLbSpConfig(t *testing.T) {
	tests := []struct {
		conf []gamekruiseiov1alpha1.NetworkConfParams
		nc   *nlbConfig
	}{
		{
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{
					Name:  NlbIdsConfigName,
					Value: "nlb-xxx",
				},
				{
					Name:  PortProtocolsConfigName,
					Value: "80/UDP",
				},
			},
			nc: &nlbConfig{
				protocols: []corev1.Protocol{corev1.ProtocolUDP},
				ports:     []int{80},
				lbId:      "nlb-xxx",
			},
		},
		{
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{
					Name:  NlbIdsConfigName,
					Value: "nlb-xxx",
				},
				{
					Name:  PortProtocolsConfigName,
					Value: "80",
				},
			},
			nc: &nlbConfig{
				protocols: []corev1.Protocol{corev1.ProtocolTCP},
				ports:     []int{80},
				lbId:      "nlb-xxx",
			},
		},
	}

	for i, test := range tests {
		expect := test.nc
		actual := parseNLbSpConfig(test.conf)
		if !reflect.DeepEqual(expect, actual) {
			t.Errorf("case %d: expect nlbConfig is %v, but actually is %v", i, expect, actual)
		}
	}
}
