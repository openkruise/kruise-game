package tencentcloud

import (
	"reflect"
	"testing"

	kruisev1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
)

func TestParseLbConfig(t *testing.T) {
	tests := []struct {
		conf      []kruisev1alpha1.NetworkConfParams
		clbConfig *clbConfig
	}{
		{
			conf: []kruisev1alpha1.NetworkConfParams{
				{
					Name:  ClbIdsConfigName,
					Value: "xxx-A",
				},
				{
					Name:  PortProtocolsConfigName,
					Value: "80",
				},
			},
			clbConfig: &clbConfig{
				targetPorts: []portProtocol{
					{
						port:     80,
						protocol: "TCP",
					},
				},
			},
		},
		{
			conf: []kruisev1alpha1.NetworkConfParams{
				{
					Name:  ClbIdsConfigName,
					Value: "xxx-A,xxx-B,",
				},
				{
					Name:  PortProtocolsConfigName,
					Value: "81/UDP,82,83/TCP",
				},
			},
			clbConfig: &clbConfig{
				targetPorts: []portProtocol{
					{
						port:     81,
						protocol: "UDP",
					},
					{
						port:     82,
						protocol: "TCP",
					},
					{
						port:     83,
						protocol: "TCP",
					},
				},
			},
		},
	}

	for i, test := range tests {
		lc, err := parseLbConfig(test.conf)
		if err != nil {
			t.Error(err)
		}
		if !reflect.DeepEqual(test.clbConfig, lc) {
			t.Errorf("case %d: lbId expect: %v, actual: %v", i, test.clbConfig, lc)
		}
	}
}
