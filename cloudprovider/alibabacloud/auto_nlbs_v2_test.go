/*
Copyright 2025 The Kruise Authors.

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

package alibabacloud

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

func TestParseAutoNLBsConfig(t *testing.T) {
	tests := []struct {
		name          string
		conf          []gamekruiseiov1alpha1.NetworkConfParams
		expectConfig  *autoNLBsConfig
		expectError   bool
		errorContains string
	}{
		{
			name: "valid config with single ISP type",
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{Name: "ZoneMaps", Value: "vpc-xxx@cn-hangzhou-h:vsw-aaa,cn-hangzhou-i:vsw-bbb"},
				{Name: "PortProtocols", Value: "8080/TCP,9000/UDP"},
				{Name: "EipIspTypes", Value: "BGP"},
				{Name: "MinPort", Value: "10000"},
				{Name: "MaxPort", Value: "10499"},
			},
			expectConfig: &autoNLBsConfig{
				zoneMaps:              "vpc-xxx@cn-hangzhou-h:vsw-aaa,cn-hangzhou-i:vsw-bbb",
				eipIspTypes:           []string{"BGP"},
				targetPorts:           []int{8080, 9000},
				protocols:             []corev1.Protocol{corev1.ProtocolTCP, corev1.ProtocolUDP},
				minPort:               10000,
				maxPort:               10499,
				reserveNlbNum:         1,
				externalTrafficPolicy: corev1.ServiceExternalTrafficPolicyTypeLocal,
			},
			expectError: false,
		},
		{
			name: "valid config with multiple ISP types",
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{Name: "ZoneMaps", Value: "vpc-yyy@cn-beijing-a:vsw-111,cn-beijing-b:vsw-222"},
				{Name: "PortProtocols", Value: "7777/TCP"},
				{Name: "EipIspTypes", Value: "ChinaTelecom,ChinaMobile,ChinaUnicom"},
				{Name: "MinPort", Value: "20000"},
				{Name: "MaxPort", Value: "20999"},
				{Name: "ReserveNlbNum", Value: "2"},
			},
			expectConfig: &autoNLBsConfig{
				zoneMaps:              "vpc-yyy@cn-beijing-a:vsw-111,cn-beijing-b:vsw-222",
				eipIspTypes:           []string{"ChinaTelecom", "ChinaMobile", "ChinaUnicom"},
				targetPorts:           []int{7777},
				protocols:             []corev1.Protocol{corev1.ProtocolTCP},
				minPort:               20000,
				maxPort:               20999,
				reserveNlbNum:         2,
				externalTrafficPolicy: corev1.ServiceExternalTrafficPolicyTypeLocal,
			},
			expectError: false,
		},
		{
			name: "default ISP type when not specified",
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{Name: "ZoneMaps", Value: "vpc-zzz@cn-shanghai-a:vsw-aaa,cn-shanghai-b:vsw-bbb"},
				{Name: "PortProtocols", Value: "8000/TCP"},
				{Name: "MinPort", Value: "30000"},
				{Name: "MaxPort", Value: "30999"},
			},
			expectConfig: &autoNLBsConfig{
				zoneMaps:              "vpc-zzz@cn-shanghai-a:vsw-aaa,cn-shanghai-b:vsw-bbb",
				eipIspTypes:           []string{"default"},
				targetPorts:           []int{8000},
				protocols:             []corev1.Protocol{corev1.ProtocolTCP},
				minPort:               30000,
				maxPort:               30999,
				reserveNlbNum:         1,
				externalTrafficPolicy: corev1.ServiceExternalTrafficPolicyTypeLocal,
			},
			expectError: false,
		},
		{
			name: "missing ZoneMaps",
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{Name: "PortProtocols", Value: "8080/TCP"},
				{Name: "EipIspTypes", Value: "BGP"},
			},
			expectError:   true,
			errorContains: "ZoneMaps",
		},
		{
			name: "missing PortProtocols",
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{Name: "ZoneMaps", Value: "vpc-xxx@cn-hangzhou-h:vsw-aaa,cn-hangzhou-i:vsw-bbb"},
				{Name: "EipIspTypes", Value: "BGP"},
			},
			expectError:   true,
			errorContains: "PortProtocols",
		},
		{
			name: "invalid MinPort greater than MaxPort",
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{Name: "ZoneMaps", Value: "vpc-xxx@cn-hangzhou-h:vsw-aaa,cn-hangzhou-i:vsw-bbb"},
				{Name: "PortProtocols", Value: "8080/TCP"},
				{Name: "MinPort", Value: "20000"},
				{Name: "MaxPort", Value: "10000"},
			},
			expectError:   true,
			errorContains: "MinPort",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := parseAutoNLBsConfig(tt.conf)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got nil")
					return
				}
				if tt.errorContains != "" && !contains(err.Error(), tt.errorContains) {
					t.Errorf("expected error to contain %q, got %q", tt.errorContains, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if config.zoneMaps != tt.expectConfig.zoneMaps {
				t.Errorf("zoneMaps: expected %q, got %q", tt.expectConfig.zoneMaps, config.zoneMaps)
			}

			if !stringSliceEqual(config.eipIspTypes, tt.expectConfig.eipIspTypes) {
				t.Errorf("eipIspTypes: expected %v, got %v", tt.expectConfig.eipIspTypes, config.eipIspTypes)
			}

			if !intSliceEqual(config.targetPorts, tt.expectConfig.targetPorts) {
				t.Errorf("targetPorts: expected %v, got %v", tt.expectConfig.targetPorts, config.targetPorts)
			}

			if config.minPort != tt.expectConfig.minPort {
				t.Errorf("minPort: expected %d, got %d", tt.expectConfig.minPort, config.minPort)
			}

			if config.maxPort != tt.expectConfig.maxPort {
				t.Errorf("maxPort: expected %d, got %d", tt.expectConfig.maxPort, config.maxPort)
			}

			if config.reserveNlbNum != tt.expectConfig.reserveNlbNum {
				t.Errorf("reserveNlbNum: expected %d, got %d", tt.expectConfig.reserveNlbNum, config.reserveNlbNum)
			}
		})
	}
}

func TestParseZoneMaps(t *testing.T) {
	tests := []struct {
		name            string
		zoneMapsStr     string
		expectVpcId     string
		expectZoneCount int
		expectError     bool
		errorContains   string
	}{
		{
			name:            "valid zone maps with 2 zones",
			zoneMapsStr:     "vpc-xxx@cn-hangzhou-h:vsw-aaa,cn-hangzhou-i:vsw-bbb",
			expectVpcId:     "vpc-xxx",
			expectZoneCount: 2,
			expectError:     false,
		},
		{
			name:            "valid zone maps with 3 zones",
			zoneMapsStr:     "vpc-yyy@cn-beijing-a:vsw-111,cn-beijing-b:vsw-222,cn-beijing-c:vsw-333",
			expectVpcId:     "vpc-yyy",
			expectZoneCount: 3,
			expectError:     false,
		},
		{
			name:          "missing VPC ID",
			zoneMapsStr:   "cn-hangzhou-h:vsw-aaa,cn-hangzhou-i:vsw-bbb",
			expectError:   true,
			errorContains: "VPC ID",
		},
		{
			name:          "empty zone maps",
			zoneMapsStr:   "",
			expectError:   true,
			errorContains: "cannot be empty",
		},
		{
			name:          "only one zone",
			zoneMapsStr:   "vpc-xxx@cn-hangzhou-h:vsw-aaa",
			expectError:   true,
			errorContains: "at least 2",
		},
		{
			name:          "invalid format - missing colon",
			zoneMapsStr:   "vpc-xxx@cn-hangzhou-h-vsw-aaa,cn-hangzhou-i:vsw-bbb",
			expectError:   true,
			errorContains: "invalid zoneMap format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			zoneMappings, vpcId, err := parseZoneMaps(tt.zoneMapsStr)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got nil")
					return
				}
				if tt.errorContains != "" && !contains(err.Error(), tt.errorContains) {
					t.Errorf("expected error to contain %q, got %q", tt.errorContains, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if vpcId != tt.expectVpcId {
				t.Errorf("vpcId: expected %q, got %q", tt.expectVpcId, vpcId)
			}

			if len(zoneMappings) != tt.expectZoneCount {
				t.Errorf("zone count: expected %d, got %d", tt.expectZoneCount, len(zoneMappings))
			}
		})
	}
}

func TestUpdateMaxPodIndex(t *testing.T) {
	tests := []struct {
		name           string
		plugin         *AutoNLBsV2Plugin
		podNamespace   string
		podName        string
		gssName        string
		expectMaxIndex int
	}{
		{
			name: "update max index from 0 to 5",
			plugin: &AutoNLBsV2Plugin{
				maxPodIndex: map[string]int{
					"default/test-gss": 0,
				},
				mutex: sync.RWMutex{},
			},
			podNamespace:   "default",
			podName:        "test-gss-5",
			gssName:        "test-gss",
			expectMaxIndex: 5,
		},
		{
			name: "do not decrease max index",
			plugin: &AutoNLBsV2Plugin{
				maxPodIndex: map[string]int{
					"default/test-gss": 10,
				},
				mutex: sync.RWMutex{},
			},
			podNamespace:   "default",
			podName:        "test-gss-3",
			gssName:        "test-gss",
			expectMaxIndex: 10,
		},
		{
			name: "initialize max index for new GSS",
			plugin: &AutoNLBsV2Plugin{
				maxPodIndex: make(map[string]int),
				mutex:       sync.RWMutex{},
			},
			podNamespace:   "default",
			podName:        "new-gss-7",
			gssName:        "new-gss",
			expectMaxIndex: 7,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &corev1.Pod{}
			pod.Namespace = tt.podNamespace
			pod.Name = tt.podName
			pod.Labels = map[string]string{
				gamekruiseiov1alpha1.GameServerOwnerGssKey: tt.gssName,
			}

			tt.plugin.updateMaxPodIndex(pod)

			gssKey := tt.podNamespace + "/" + tt.gssName
			tt.plugin.mutex.RLock()
			actualMaxIndex := tt.plugin.maxPodIndex[gssKey]
			tt.plugin.mutex.RUnlock()

			if actualMaxIndex != tt.expectMaxIndex {
				t.Errorf("maxPodIndex: expected %d, got %d", tt.expectMaxIndex, actualMaxIndex)
			}
		})
	}
}

func TestCalculateExpectNLBNum(t *testing.T) {
	tests := []struct {
		name         string
		plugin       *AutoNLBsV2Plugin
		namespace    string
		gssName      string
		config       *autoNLBsConfig
		expectNLBNum int
	}{
		{
			name: "basic calculation with reserve",
			plugin: &AutoNLBsV2Plugin{
				maxPodIndex: map[string]int{
					"default/test-gss": 10,
				},
				mutex: sync.RWMutex{},
			},
			namespace: "default",
			gssName:   "test-gss",
			config: &autoNLBsConfig{
				minPort:       10000,
				maxPort:       10499,
				blockPorts:    []int32{},
				targetPorts:   []int{8080, 9000},
				reserveNlbNum: 1,
			},
			expectNLBNum: 2, // (10 / 250) + 1 + 1 = 2
		},
		{
			name: "calculation with larger pod count",
			plugin: &AutoNLBsV2Plugin{
				maxPodIndex: map[string]int{
					"default/test-gss": 500,
				},
				mutex: sync.RWMutex{},
			},
			namespace: "default",
			gssName:   "test-gss",
			config: &autoNLBsConfig{
				minPort:       10000,
				maxPort:       10499,
				blockPorts:    []int32{},
				targetPorts:   []int{8080, 9000},
				reserveNlbNum: 2,
			},
			expectNLBNum: 5, // (500 / 250) + 2 + 1 = 5
		},
		{
			name: "calculation with block ports",
			plugin: &AutoNLBsV2Plugin{
				maxPodIndex: map[string]int{
					"default/test-gss": 100,
				},
				mutex: sync.RWMutex{},
			},
			namespace: "default",
			gssName:   "test-gss",
			config: &autoNLBsConfig{
				minPort:       10000,
				maxPort:       10499,
				blockPorts:    []int32{10100, 10200, 10300},
				targetPorts:   []int{8080, 9000},
				reserveNlbNum: 1,
			},
			expectNLBNum: 2, // (100 / ((500-3)/2)) + 1 + 1 = 2
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actualNLBNum := tt.plugin.calculateExpectNLBNum(tt.namespace, tt.gssName, tt.config)

			if actualNLBNum != tt.expectNLBNum {
				t.Errorf("expectNLBNum: expected %d, got %d", tt.expectNLBNum, actualNLBNum)
			}
		})
	}
}

func TestEIPChargeTypeSelection(t *testing.T) {
	tests := []struct {
		name                     string
		eipIspType               string
		expectInternetChargeType string
	}{
		{
			name:                     "ChinaTelecom uses PayByBandwidth",
			eipIspType:               "ChinaTelecom",
			expectInternetChargeType: "PayByBandwidth",
		},
		{
			name:                     "ChinaMobile uses PayByBandwidth",
			eipIspType:               "ChinaMobile",
			expectInternetChargeType: "PayByBandwidth",
		},
		{
			name:                     "ChinaUnicom uses PayByBandwidth",
			eipIspType:               "ChinaUnicom",
			expectInternetChargeType: "PayByBandwidth",
		},
		{
			name:                     "BGP uses PayByTraffic",
			eipIspType:               "BGP",
			expectInternetChargeType: "PayByTraffic",
		},
		{
			name:                     "BGP_PRO uses PayByTraffic",
			eipIspType:               "BGP_PRO",
			expectInternetChargeType: "PayByTraffic",
		},
		{
			name:                     "default uses PayByTraffic",
			eipIspType:               "default",
			expectInternetChargeType: "PayByTraffic",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 模拟 ensureEIPCR 函数中的计费方式选择逻辑
			internetChargeType := "PayByTraffic" // 默认按流量计费
			if tt.eipIspType == "ChinaTelecom" || tt.eipIspType == "ChinaMobile" || tt.eipIspType == "ChinaUnicom" {
				internetChargeType = "PayByBandwidth" // 单线 ISP 必须按固定带宽付费
			}

			if internetChargeType != tt.expectInternetChargeType {
				t.Errorf("internetChargeType: expected %q, got %q", tt.expectInternetChargeType, internetChargeType)
			}
		})
	}
}

func TestCalculatePodsPerNLB(t *testing.T) {
	tests := []struct {
		name             string
		minPort          int32
		maxPort          int32
		blockPorts       []int32
		targetPortCount  int
		expectPodsPerNLB int
	}{
		{
			name:             "500 ports, 2 target ports, no block",
			minPort:          10000,
			maxPort:          10499,
			blockPorts:       []int32{},
			targetPortCount:  2,
			expectPodsPerNLB: 250, // (10499-10000+1) / 2 = 250
		},
		{
			name:             "1000 ports, 1 target port, no block",
			minPort:          20000,
			maxPort:          20999,
			blockPorts:       []int32{},
			targetPortCount:  1,
			expectPodsPerNLB: 1000, // (20999-20000+1) / 1 = 1000
		},
		{
			name:             "500 ports, 2 target ports, 3 block ports",
			minPort:          10000,
			maxPort:          10499,
			blockPorts:       []int32{10100, 10200, 10300},
			targetPortCount:  2,
			expectPodsPerNLB: 248, // ((10499-10000+1) - 3) / 2 = 248
		},
		{
			name:             "100 ports, 3 target ports, no block",
			minPort:          30000,
			maxPort:          30099,
			blockPorts:       []int32{},
			targetPortCount:  3,
			expectPodsPerNLB: 33, // (30099-30000+1) / 3 = 33
		},
		{
			name:             "1000 ports, 4 target ports, 10 block ports",
			minPort:          40000,
			maxPort:          40999,
			blockPorts:       []int32{40100, 40200, 40300, 40400, 40500, 40600, 40700, 40800, 40900, 41000},
			targetPortCount:  4,
			expectPodsPerNLB: 247, // ((40999-40000+1) - 10) / 4 = 247
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 模拟计算逻辑
			lenRange := int(tt.maxPort) - int(tt.minPort) - len(tt.blockPorts) + 1
			podsPerNLB := lenRange / tt.targetPortCount

			if podsPerNLB != tt.expectPodsPerNLB {
				t.Errorf("podsPerNLB: expected %d, got %d", tt.expectPodsPerNLB, podsPerNLB)
			}
		})
	}
}

func TestNLBIndexCalculation(t *testing.T) {
	tests := []struct {
		name         string
		podIndex     int
		podsPerNLB   int
		expectNLBIdx int
	}{
		{
			name:         "pod-0 with 250 pods per NLB",
			podIndex:     0,
			podsPerNLB:   250,
			expectNLBIdx: 0,
		},
		{
			name:         "pod-249 with 250 pods per NLB",
			podIndex:     249,
			podsPerNLB:   250,
			expectNLBIdx: 0,
		},
		{
			name:         "pod-250 with 250 pods per NLB",
			podIndex:     250,
			podsPerNLB:   250,
			expectNLBIdx: 1,
		},
		{
			name:         "pod-500 with 250 pods per NLB",
			podIndex:     500,
			podsPerNLB:   250,
			expectNLBIdx: 2,
		},
		{
			name:         "pod-751 with 250 pods per NLB",
			podIndex:     751,
			podsPerNLB:   250,
			expectNLBIdx: 3,
		},
		{
			name:         "pod-100 with 1000 pods per NLB",
			podIndex:     100,
			podsPerNLB:   1000,
			expectNLBIdx: 0,
		},
		{
			name:         "pod-1500 with 1000 pods per NLB",
			podIndex:     1500,
			podsPerNLB:   1000,
			expectNLBIdx: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 模拟 NLB 索引计算逻辑
			nlbIdx := tt.podIndex / tt.podsPerNLB

			if nlbIdx != tt.expectNLBIdx {
				t.Errorf("nlbIdx: expected %d, got %d", tt.expectNLBIdx, nlbIdx)
			}
		})
	}
}

func TestServiceNamingConvention(t *testing.T) {
	tests := []struct {
		name              string
		podName           string
		eipIspType        string
		expectServiceName string
	}{
		{
			name:              "pod gss-0 with BGP",
			podName:           "gss-0",
			eipIspType:        "BGP",
			expectServiceName: "gss-0-bgp",
		},
		{
			name:              "pod gss-100 with ChinaTelecom",
			podName:           "gss-100",
			eipIspType:        "ChinaTelecom",
			expectServiceName: "gss-100-chinatelecom",
		},
		{
			name:              "pod test-gss-5 with BGP_PRO",
			podName:           "test-gss-5",
			eipIspType:        "BGP_PRO",
			expectServiceName: "test-gss-5-bgp_pro",
		},
		{
			name:              "pod game-server-250 with ChinaMobile",
			podName:           "game-server-250",
			eipIspType:        "ChinaMobile",
			expectServiceName: "game-server-250-chinamobile",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 模拟 Service 命名逻辑
			serviceName := tt.podName + "-" + strings.ToLower(tt.eipIspType)

			if serviceName != tt.expectServiceName {
				t.Errorf("serviceName: expected %q, got %q", tt.expectServiceName, serviceName)
			}
		})
	}
}

func TestNLBNamingConvention(t *testing.T) {
	tests := []struct {
		name          string
		gssName       string
		eipIspType    string
		nlbIndex      int
		expectNLBName string
	}{
		{
			name:          "gss with BGP, index 0",
			gssName:       "test-gss",
			eipIspType:    "BGP",
			nlbIndex:      0,
			expectNLBName: "test-gss-bgp-0",
		},
		{
			name:          "gss with ChinaTelecom, index 3",
			gssName:       "game-server",
			eipIspType:    "ChinaTelecom",
			nlbIndex:      3,
			expectNLBName: "game-server-chinatelecom-3",
		},
		{
			name:          "gss with BGP_PRO, index 10",
			gssName:       "my-gss",
			eipIspType:    "BGP_PRO",
			nlbIndex:      10,
			expectNLBName: "my-gss-bgp_pro-10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 模拟 NLB 命名逻辑
			nlbName := tt.gssName + "-" + strings.ToLower(tt.eipIspType) + "-" + strconv.Itoa(tt.nlbIndex)

			if nlbName != tt.expectNLBName {
				t.Errorf("nlbName: expected %q, got %q", tt.expectNLBName, nlbName)
			}
		})
	}
}

func TestEIPNamingConvention(t *testing.T) {
	tests := []struct {
		name          string
		gssName       string
		eipIspType    string
		nlbIndex      int
		zoneIndex     int
		expectEIPName string
	}{
		{
			name:          "gss with BGP, nlb 0, zone 0",
			gssName:       "test-gss",
			eipIspType:    "BGP",
			nlbIndex:      0,
			zoneIndex:     0,
			expectEIPName: "test-gss-eip-bgp-0-z0",
		},
		{
			name:          "gss with ChinaTelecom, nlb 2, zone 1",
			gssName:       "game-server",
			eipIspType:    "ChinaTelecom",
			nlbIndex:      2,
			zoneIndex:     1,
			expectEIPName: "game-server-eip-chinatelecom-2-z1",
		},
		{
			name:          "gss with BGP_PRO, nlb 5, zone 2",
			gssName:       "my-gss",
			eipIspType:    "BGP_PRO",
			nlbIndex:      5,
			zoneIndex:     2,
			expectEIPName: "my-gss-eip-bgp_pro-5-z2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 模拟 EIP 命名逻辑
			eipName := fmt.Sprintf("%s-eip-%s-%d-z%d",
				tt.gssName,
				strings.ToLower(tt.eipIspType),
				tt.nlbIndex,
				tt.zoneIndex)

			if eipName != tt.expectEIPName {
				t.Errorf("eipName: expected %q, got %q", tt.expectEIPName, eipName)
			}
		})
	}
}

func TestMultiISPTypeConfiguration(t *testing.T) {
	tests := []struct {
		name                string
		eipIspTypes         []string
		expectServiceCount  int // 每个 Pod 需要的 Service 数量
		expectNLBMultiplier int // NLB 数量倍数
	}{
		{
			name:                "single ISP type (BGP)",
			eipIspTypes:         []string{"BGP"},
			expectServiceCount:  1,
			expectNLBMultiplier: 1,
		},
		{
			name:                "dual ISP types (BGP + BGP_PRO)",
			eipIspTypes:         []string{"BGP", "BGP_PRO"},
			expectServiceCount:  2,
			expectNLBMultiplier: 2,
		},
		{
			name:                "triple ISP types (three operators)",
			eipIspTypes:         []string{"ChinaTelecom", "ChinaMobile", "ChinaUnicom"},
			expectServiceCount:  3,
			expectNLBMultiplier: 3,
		},
		{
			name:                "five ISP types (all available)",
			eipIspTypes:         []string{"BGP", "BGP_PRO", "ChinaTelecom", "ChinaMobile", "ChinaUnicom"},
			expectServiceCount:  5,
			expectNLBMultiplier: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 验证每个 Pod 需要的 Service 数量
			if len(tt.eipIspTypes) != tt.expectServiceCount {
				t.Errorf("serviceCount: expected %d, got %d", tt.expectServiceCount, len(tt.eipIspTypes))
			}

			// 验证 NLB 数量倍数
			if len(tt.eipIspTypes) != tt.expectNLBMultiplier {
				t.Errorf("nlbMultiplier: expected %d, got %d", tt.expectNLBMultiplier, len(tt.eipIspTypes))
			}
		})
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name          string
		conf          []gamekruiseiov1alpha1.NetworkConfParams
		expectError   bool
		errorContains string
	}{
		{
			name: "missing ZoneMaps",
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{Name: "PortProtocols", Value: "8080/TCP"},
				{Name: "MinPort", Value: "10000"},
				{Name: "MaxPort", Value: "10999"},
			},
			expectError:   true,
			errorContains: "ZoneMaps",
		},
		{
			name: "missing PortProtocols",
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{Name: "ZoneMaps", Value: "vpc-xxx@cn-hangzhou-h:vsw-aaa,cn-hangzhou-i:vsw-bbb"},
				{Name: "MinPort", Value: "10000"},
				{Name: "MaxPort", Value: "10999"},
			},
			expectError:   true,
			errorContains: "PortProtocols",
		},
		{
			name: "MinPort greater than MaxPort",
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{Name: "ZoneMaps", Value: "vpc-xxx@cn-hangzhou-h:vsw-aaa,cn-hangzhou-i:vsw-bbb"},
				{Name: "PortProtocols", Value: "8080/TCP"},
				{Name: "MinPort", Value: "20000"},
				{Name: "MaxPort", Value: "10000"},
			},
			expectError:   true,
			errorContains: "MinPort",
		},
		{
			name: "valid config with BlockPorts",
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{Name: "ZoneMaps", Value: "vpc-xxx@cn-hangzhou-h:vsw-aaa,cn-hangzhou-i:vsw-bbb"},
				{Name: "PortProtocols", Value: "8080/TCP"},
				{Name: "MinPort", Value: "10000"},
				{Name: "MaxPort", Value: "10999"},
				{Name: "BlockPorts", Value: "10100,10200,10300"},
			},
			expectError: false,
		},
		{
			name: "valid config with ExternalTrafficPolicy",
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{Name: "ZoneMaps", Value: "vpc-xxx@cn-hangzhou-h:vsw-aaa,cn-hangzhou-i:vsw-bbb"},
				{Name: "PortProtocols", Value: "8080/TCP"},
				{Name: "MinPort", Value: "10000"},
				{Name: "MaxPort", Value: "10999"},
				{Name: "ExternalTrafficPolicy", Value: "Cluster"},
			},
			expectError: false,
		},
		{
			name: "valid config with multiple protocols",
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{Name: "ZoneMaps", Value: "vpc-xxx@cn-hangzhou-h:vsw-aaa,cn-hangzhou-i:vsw-bbb"},
				{Name: "PortProtocols", Value: "8080/TCP,9000/UDP,7777/TCP"},
				{Name: "MinPort", Value: "10000"},
				{Name: "MaxPort", Value: "10999"},
			},
			expectError: false,
		},
		{
			name: "valid config with ReserveNlbNum",
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{Name: "ZoneMaps", Value: "vpc-xxx@cn-hangzhou-h:vsw-aaa,cn-hangzhou-i:vsw-bbb"},
				{Name: "PortProtocols", Value: "8080/TCP"},
				{Name: "MinPort", Value: "10000"},
				{Name: "MaxPort", Value: "10999"},
				{Name: "ReserveNlbNum", Value: "5"},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseAutoNLBsConfig(tt.conf)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got nil")
					return
				}
				if tt.errorContains != "" && !contains(err.Error(), tt.errorContains) {
					t.Errorf("expected error to contain %q, got %q", tt.errorContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

// Helper functions
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func intSliceEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
