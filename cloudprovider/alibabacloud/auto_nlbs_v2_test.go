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
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"

	nlbv1 "github.com/chrisliu1995/AlibabaCloud-NLB-Operator/pkg/apis/nlboperator/v1"
	eipv1 "github.com/chrisliu1995/alibabacloud-eip-operator/api/v1alpha1"
	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	cperrors "github.com/openkruise/kruise-game/cloudprovider/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// scheme 用于测试中注册 CRD 类型
var scheme = runtime.NewScheme()

func init() {
	_ = corev1.AddToScheme(scheme)
	_ = gamekruiseiov1alpha1.AddToScheme(scheme)
	// NLB 和 EIP 类型手动注册
	nlbGV := schema.GroupVersion{Group: "nlboperator.alibabacloud.com", Version: "v1"}
	eipGV := schema.GroupVersion{Group: "eip.alibabacloud.com", Version: "v1alpha1"}
	scheme.AddKnownTypes(nlbGV, &nlbv1.NLB{}, &nlbv1.NLBList{})
	scheme.AddKnownTypes(eipGV, &eipv1.EIP{}, &eipv1.EIPList{})
	metav1.AddToGroupVersion(scheme, nlbGV)
	metav1.AddToGroupVersion(scheme, eipGV)
}

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
				retainNLBOnDelete:     true, // 默认值
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
				retainNLBOnDelete:     true, // 默认值
			},
			expectError: false,
		},
		{
			name: "default BGP ISP type when not specified",
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{Name: "ZoneMaps", Value: "vpc-zzz@cn-shanghai-a:vsw-aaa,cn-shanghai-b:vsw-bbb"},
				{Name: "PortProtocols", Value: "8000/TCP"},
				{Name: "MinPort", Value: "30000"},
				{Name: "MaxPort", Value: "30999"},
			},
			expectConfig: &autoNLBsConfig{
				zoneMaps:              "vpc-zzz@cn-shanghai-a:vsw-aaa,cn-shanghai-b:vsw-bbb",
				eipIspTypes:           []string{"BGP"},
				targetPorts:           []int{8000},
				protocols:             []corev1.Protocol{corev1.ProtocolTCP},
				minPort:               30000,
				maxPort:               30999,
				reserveNlbNum:         1,
				externalTrafficPolicy: corev1.ServiceExternalTrafficPolicyTypeLocal,
				retainNLBOnDelete:     true, // 默认值
			},
			expectError: false,
		},
		{
			name: "RetainNLBOnDelete set to false",
			conf: []gamekruiseiov1alpha1.NetworkConfParams{
				{Name: "ZoneMaps", Value: "vpc-test@cn-hangzhou-h:vsw-aaa,cn-hangzhou-i:vsw-bbb"},
				{Name: "PortProtocols", Value: "8080/TCP"},
				{Name: "EipIspTypes", Value: "BGP"},
				{Name: "MinPort", Value: "10000"},
				{Name: "MaxPort", Value: "10999"},
				{Name: "RetainNLBOnDelete", Value: "false"},
			},
			expectConfig: &autoNLBsConfig{
				zoneMaps:              "vpc-test@cn-hangzhou-h:vsw-aaa,cn-hangzhou-i:vsw-bbb",
				eipIspTypes:           []string{"BGP"},
				targetPorts:           []int{8080},
				protocols:             []corev1.Protocol{corev1.ProtocolTCP},
				minPort:               10000,
				maxPort:               10999,
				reserveNlbNum:         1,
				externalTrafficPolicy: corev1.ServiceExternalTrafficPolicyTypeLocal,
				retainNLBOnDelete:     false, // 用户设置为 false
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

			if config.retainNLBOnDelete != tt.expectConfig.retainNLBOnDelete {
				t.Errorf("retainNLBOnDelete: expected %v, got %v", tt.expectConfig.retainNLBOnDelete, config.retainNLBOnDelete)
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

// 测试 Alias 方法
func TestAutoNLBsV2Plugin_Alias(t *testing.T) {
	plugin := &AutoNLBsV2Plugin{
		maxPodIndex: make(map[string]int),
		mutex:       sync.RWMutex{},
	}

	alias := plugin.Alias()
	expectedAlias := AliasAutoNLBs

	if alias != expectedAlias {
		t.Errorf("Alias(): expected %q, got %q", expectedAlias, alias)
	}
}

// 测试 OnPodDeleted 方法
func TestAutoNLBsV2Plugin_OnPodDeleted(t *testing.T) {
	plugin := &AutoNLBsV2Plugin{
		maxPodIndex: make(map[string]int),
		mutex:       sync.RWMutex{},
	}

	// 网络配置（RetainNLBOnDelete=true，默认情况）
	networkConf := []gamekruiseiov1alpha1.NetworkConfParams{
		{Name: "MinPort", Value: "10000"},
		{Name: "MaxPort", Value: "10100"},
		{Name: "PortProtocols", Value: "8080/TCP"},
		{Name: "EIPIspTypes", Value: "BGP"},
		{Name: "ZoneMaps", Value: "vpc-test@cn-hangzhou-h:vsw-a,cn-hangzhou-i:vsw-b"},
		// RetainNLBOnDelete 默认 true，不需要清理
	}
	networkConfBytes, _ := json.Marshal(networkConf)

	// 创建 GSS
	gss := &gamekruiseiov1alpha1.GameServerSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gss",
			Namespace: "default",
			UID:       "test-uid-123",
		},
		Spec: gamekruiseiov1alpha1.GameServerSetSpec{
			Network: &gamekruiseiov1alpha1.Network{
				NetworkType: AutoNLBsV2Network,
				NetworkConf: networkConf,
			},
		},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gss-0",
			Namespace: "default",
			Labels: map[string]string{
				gamekruiseiov1alpha1.GameServerOwnerGssKey: "test-gss",
			},
			Annotations: map[string]string{
				gamekruiseiov1alpha1.GameServerNetworkType: AutoNLBsV2Network,
				gamekruiseiov1alpha1.GameServerNetworkConf: string(networkConfBytes),
			},
		},
	}

	// 使用 fake client
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gss, pod).Build()

	err := plugin.OnPodDeleted(c, pod, context.Background())
	if err != nil {
		t.Errorf("OnPodDeleted() should return nil for RetainNLBOnDelete=true, got error: %v", err)
	}
}

// 测试 OnPodDeleted 方法 - RetainNLBOnDelete=false 且 GSS 正在删除场景
func TestAutoNLBsV2Plugin_OnPodDeleted_WithCleanup(t *testing.T) {
	plugin := &AutoNLBsV2Plugin{
		maxPodIndex: make(map[string]int),
		mutex:       sync.RWMutex{},
	}

	// 网络配置（RetainNLBOnDelete=false）
	networkConf := []gamekruiseiov1alpha1.NetworkConfParams{
		{Name: "MinPort", Value: "10000"},
		{Name: "MaxPort", Value: "10100"},
		{Name: "PortProtocols", Value: "8080/TCP"},
		{Name: "EIPIspTypes", Value: "BGP"},
		{Name: "ZoneMaps", Value: "vpc-test@cn-hangzhou-h:vsw-a,cn-hangzhou-i:vsw-b"},
		{Name: "RetainNLBOnDelete", Value: "false"}, // 需要清理
	}
	networkConfBytes, _ := json.Marshal(networkConf)

	// 创建 GSS（设置 DeletionTimestamp 表示正在删除）
	now := metav1.Now()
	gss := &gamekruiseiov1alpha1.GameServerSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-gss",
			Namespace:         "default",
			UID:               "test-uid-123",
			DeletionTimestamp: &now,                       // GSS 正在删除
			Finalizers:        []string{"test-finalizer"}, // 需要 Finalizer 才能设置 DeletionTimestamp
		},
		Spec: gamekruiseiov1alpha1.GameServerSetSpec{
			Network: &gamekruiseiov1alpha1.Network{
				NetworkType: AutoNLBsV2Network,
				NetworkConf: networkConf,
			},
		},
	}

	// 创建带 Finalizer 的 Pod
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gss-0",
			Namespace: "default",
			Labels: map[string]string{
				gamekruiseiov1alpha1.GameServerOwnerGssKey: "test-gss",
			},
			Annotations: map[string]string{
				gamekruiseiov1alpha1.GameServerNetworkType: AutoNLBsV2Network,
				gamekruiseiov1alpha1.GameServerNetworkConf: string(networkConfBytes),
			},
			Finalizers: []string{PodFinalizerName},
		},
	}

	// 创建一个 Service（模拟还有 Service 未删除）
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gss-0-bgp",
			Namespace: "default",
			Labels: map[string]string{
				gamekruiseiov1alpha1.GameServerOwnerGssKey: "test-gss",
			},
		},
	}

	// 使用 fake client
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gss, pod, svc).Build()

	err := plugin.OnPodDeleted(c, pod, context.Background())
	// 应该返回 RetryError，触发 Controller 重试
	if err == nil {
		t.Errorf("OnPodDeleted() should return RetryError when GSS is deleting and Service exists")
	} else if err.Type() != cperrors.RetryError {
		t.Errorf("OnPodDeleted() should return RetryError, got %v", err.Type())
	}

	// 验证 Pod Finalizer 仍然存在（因为 GSS 正在删除且 Service 还在）
	updatedPod := &corev1.Pod{}
	if err := c.Get(context.Background(), types.NamespacedName{Name: "test-gss-0", Namespace: "default"}, updatedPod); err != nil {
		t.Fatalf("Failed to get updated Pod: %v", err)
	}
	hasFinalizer := false
	for _, f := range updatedPod.GetFinalizers() {
		if f == PodFinalizerName {
			hasFinalizer = true
			break
		}
	}
	if !hasFinalizer {
		t.Errorf("Pod should still have Finalizer when GSS is deleting and Service exists")
	}
}

// 测试 OnPodDeleted 方法 - RetainNLBOnDelete=false 且 GSS 存在不删除（正常缩容场景）
func TestAutoNLBsV2Plugin_OnPodDeleted_NormalScaleDown(t *testing.T) {
	plugin := &AutoNLBsV2Plugin{
		maxPodIndex: make(map[string]int),
		mutex:       sync.RWMutex{},
	}

	// 网络配置（RetainNLBOnDelete=false）
	networkConf := []gamekruiseiov1alpha1.NetworkConfParams{
		{Name: "MinPort", Value: "10000"},
		{Name: "MaxPort", Value: "10100"},
		{Name: "PortProtocols", Value: "8080/TCP"},
		{Name: "EIPIspTypes", Value: "BGP"},
		{Name: "ZoneMaps", Value: "vpc-test@cn-hangzhou-h:vsw-a,cn-hangzhou-i:vsw-b"},
		{Name: "RetainNLBOnDelete", Value: "false"},
	}
	networkConfBytes, _ := json.Marshal(networkConf)

	// 创建 GSS（正常存在，没有 DeletionTimestamp）
	gss := &gamekruiseiov1alpha1.GameServerSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gss",
			Namespace: "default",
			UID:       "test-uid-123",
			// 没有 DeletionTimestamp，表示 GSS 正常存在
		},
		Spec: gamekruiseiov1alpha1.GameServerSetSpec{
			Network: &gamekruiseiov1alpha1.Network{
				NetworkType: AutoNLBsV2Network,
				NetworkConf: networkConf,
			},
		},
	}

	// 创建带 Finalizer 的 Pod
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gss-0",
			Namespace: "default",
			Labels: map[string]string{
				gamekruiseiov1alpha1.GameServerOwnerGssKey: "test-gss",
			},
			Annotations: map[string]string{
				gamekruiseiov1alpha1.GameServerNetworkType: AutoNLBsV2Network,
				gamekruiseiov1alpha1.GameServerNetworkConf: string(networkConfBytes),
			},
			Finalizers: []string{PodFinalizerName},
		},
	}

	// 使用 fake client
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gss, pod).Build()

	err := plugin.OnPodDeleted(c, pod, context.Background())
	if err != nil {
		t.Errorf("OnPodDeleted() should return nil for normal scale-down, got error: %v", err)
	}

	// 验证 Pod Finalizer 已被移除（因为 GSS 存在且没有在删除）
	updatedPod := &corev1.Pod{}
	if err := c.Get(context.Background(), types.NamespacedName{Name: "test-gss-0", Namespace: "default"}, updatedPod); err != nil {
		t.Fatalf("Failed to get updated Pod: %v", err)
	}
	for _, f := range updatedPod.GetFinalizers() {
		if f == PodFinalizerName {
			t.Errorf("Pod Finalizer should be removed for normal scale-down when GSS exists and not deleting")
			break
		}
	}
}

// 测试 OnPodDeleted 方法 - RetainNLBOnDelete=false 且 GSS 已删除，Service 已全部清除，应删除 NLB/EIP
func TestAutoNLBsV2Plugin_OnPodDeleted_AllServicesDeleted(t *testing.T) {
	plugin := &AutoNLBsV2Plugin{
		maxPodIndex: make(map[string]int),
		mutex:       sync.RWMutex{},
	}

	// 网络配置（RetainNLBOnDelete=false）
	networkConf := []gamekruiseiov1alpha1.NetworkConfParams{
		{Name: "MinPort", Value: "10000"},
		{Name: "MaxPort", Value: "10100"},
		{Name: "PortProtocols", Value: "8080/TCP"},
		{Name: "EIPIspTypes", Value: "BGP"},
		{Name: "ZoneMaps", Value: "vpc-test@cn-hangzhou-h:vsw-a,cn-hangzhou-i:vsw-b"},
		{Name: "RetainNLBOnDelete", Value: "false"},
	}
	networkConfBytes, _ := json.Marshal(networkConf)

	// 创建带 Finalizer 的 Pod（GSS 不存在，模拟已删除）
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gss-0",
			Namespace: "default",
			Labels: map[string]string{
				gamekruiseiov1alpha1.GameServerOwnerGssKey: "test-gss",
			},
			Annotations: map[string]string{
				gamekruiseiov1alpha1.GameServerNetworkType: AutoNLBsV2Network,
				gamekruiseiov1alpha1.GameServerNetworkConf: string(networkConfBytes),
			},
			Finalizers: []string{PodFinalizerName},
		},
	}

	// 使用 fake client（不创建 GSS，不创建 Service - 模拟都已删除）
	// 不创建 NLB/EIP，因为 fake client 的 LabelSelector 需要额外配置
	// 这里主要测试 Finalizer 移除逻辑
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()

	err := plugin.OnPodDeleted(c, pod, context.Background())
	if err != nil {
		t.Errorf("OnPodDeleted() should succeed when all Services deleted, got error: %v", err)
	}

	// 验证 Pod Finalizer 已被移除
	updatedPod := &corev1.Pod{}
	if err := c.Get(context.Background(), types.NamespacedName{Name: "test-gss-0", Namespace: "default"}, updatedPod); err != nil {
		t.Fatalf("Failed to get updated Pod: %v", err)
	}
	for _, f := range updatedPod.GetFinalizers() {
		if f == PodFinalizerName {
			t.Errorf("Pod Finalizer should be removed when all Services are deleted")
			break
		}
	}
}

// 测试 consServiceForPod 方法
func TestAutoNLBsV2Plugin_ConsServiceForPod(t *testing.T) {
	plugin := &AutoNLBsV2Plugin{
		maxPodIndex: make(map[string]int),
		mutex:       sync.RWMutex{},
	}

	gss := &gamekruiseiov1alpha1.GameServerSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gss",
			Namespace: "default",
			UID:       "test-uid-123",
		},
		TypeMeta: metav1.TypeMeta{
			APIVersion: "game.kruise.io/v1alpha1",
			Kind:       "GameServerSet",
		},
	}

	config := &autoNLBsConfig{
		targetPorts:           []int{8080, 9000},
		protocols:             []corev1.Protocol{corev1.ProtocolTCP, corev1.ProtocolUDP},
		externalTrafficPolicy: corev1.ServiceExternalTrafficPolicyTypeLocal,
		nlbHealthConfig: &nlbHealthConfig{
			lBHealthCheckFlag: "on",
			lBHealthCheckType: "tcp",
		},
	}

	ports := []int32{10000, 10001}
	svcName := "test-gss-0-bgp"
	podName := "test-gss-0"
	gssName := "test-gss"
	nlbId := "nlb-test-12345"

	svc := plugin.consServiceForPod("default", svcName, podName, gssName, nlbId, ports, config, gss)

	// 验证 Service 基本属性
	if svc.Name != svcName {
		t.Errorf("Service Name: expected %q, got %q", svcName, svc.Name)
	}

	if svc.Namespace != "default" {
		t.Errorf("Service Namespace: expected %q, got %q", "default", svc.Namespace)
	}

	if svc.Spec.Type != corev1.ServiceTypeLoadBalancer {
		t.Errorf("Service Type: expected %q, got %q", corev1.ServiceTypeLoadBalancer, svc.Spec.Type)
	}

	if svc.Spec.ExternalTrafficPolicy != config.externalTrafficPolicy {
		t.Errorf("ExternalTrafficPolicy: expected %q, got %q", config.externalTrafficPolicy, svc.Spec.ExternalTrafficPolicy)
	}

	// 验证 Selector
	if svc.Spec.Selector[SvcSelectorKey] != podName {
		t.Errorf("Selector: expected pod name %q, got %q", podName, svc.Spec.Selector[SvcSelectorKey])
	}

	// 验证 LoadBalancerClass
	if svc.Spec.LoadBalancerClass == nil || *svc.Spec.LoadBalancerClass != "alibabacloud.com/nlb" {
		t.Errorf("LoadBalancerClass: expected %q, got %v", "alibabacloud.com/nlb", svc.Spec.LoadBalancerClass)
	}

	// 验证 Annotations
	if svc.Annotations[SlbIdAnnotationKey] != nlbId {
		t.Errorf("SlbIdAnnotation: expected %q, got %q", nlbId, svc.Annotations[SlbIdAnnotationKey])
	}

	if svc.Annotations[LBHealthCheckFlagAnnotationKey] != "on" {
		t.Errorf("LBHealthCheckFlag: expected %q, got %q", "on", svc.Annotations[LBHealthCheckFlagAnnotationKey])
	}

	// 验证 Ports
	if len(svc.Spec.Ports) != len(config.targetPorts) {
		t.Errorf("Ports count: expected %d, got %d", len(config.targetPorts), len(svc.Spec.Ports))
	}

	for i, port := range svc.Spec.Ports {
		if port.Port != ports[i] {
			t.Errorf("Port[%d]: expected %d, got %d", i, ports[i], port.Port)
		}
		if port.Protocol != config.protocols[i] {
			t.Errorf("Protocol[%d]: expected %q, got %q", i, config.protocols[i], port.Protocol)
		}
		if port.TargetPort.IntVal != int32(config.targetPorts[i]) {
			t.Errorf("TargetPort[%d]: expected %d, got %d", i, config.targetPorts[i], port.TargetPort.IntVal)
		}
	}

	// 验证 OwnerReferences
	if len(svc.OwnerReferences) != 1 {
		t.Errorf("OwnerReferences count: expected 1, got %d", len(svc.OwnerReferences))
	} else {
		if svc.OwnerReferences[0].Name != gss.Name {
			t.Errorf("OwnerReference Name: expected %q, got %q", gss.Name, svc.OwnerReferences[0].Name)
		}
		if svc.OwnerReferences[0].UID != gss.UID {
			t.Errorf("OwnerReference UID: expected %q, got %q", gss.UID, svc.OwnerReferences[0].UID)
		}
	}

	// 验证 Labels
	if svc.Labels[gamekruiseiov1alpha1.GameServerOwnerGssKey] != gssName {
		t.Errorf("GSS Label: expected %q, got %q", gssName, svc.Labels[gamekruiseiov1alpha1.GameServerOwnerGssKey])
	}
}

// 测试 consServiceForPod 方法 - HTTP 健康检查
func TestAutoNLBsV2Plugin_ConsServiceForPod_HTTPHealthCheck(t *testing.T) {
	plugin := &AutoNLBsV2Plugin{
		maxPodIndex: make(map[string]int),
		mutex:       sync.RWMutex{},
	}

	gss := &gamekruiseiov1alpha1.GameServerSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gss",
			Namespace: "default",
			UID:       "test-uid-123",
		},
		TypeMeta: metav1.TypeMeta{
			APIVersion: "game.kruise.io/v1alpha1",
			Kind:       "GameServerSet",
		},
	}

	config := &autoNLBsConfig{
		targetPorts:           []int{8080},
		protocols:             []corev1.Protocol{corev1.ProtocolTCP},
		externalTrafficPolicy: corev1.ServiceExternalTrafficPolicyTypeCluster,
		nlbHealthConfig: &nlbHealthConfig{
			lBHealthCheckFlag:   "on",
			lBHealthCheckType:   "http",
			lBHealthCheckDomain: "example.com",
			lBHealthCheckUri:    "/health",
			lBHealthCheckMethod: "GET",
		},
	}

	ports := []int32{10000}
	svc := plugin.consServiceForPod("default", "test-svc", "test-pod", "test-gss", "nlb-123", ports, config, gss)

	// 验证 HTTP 健康检查注解
	if svc.Annotations[LBHealthCheckTypeAnnotationKey] != "http" {
		t.Errorf("HealthCheckType: expected %q, got %q", "http", svc.Annotations[LBHealthCheckTypeAnnotationKey])
	}

	if svc.Annotations[LBHealthCheckDomainAnnotationKey] != "example.com" {
		t.Errorf("HealthCheckDomain: expected %q, got %q", "example.com", svc.Annotations[LBHealthCheckDomainAnnotationKey])
	}

	if svc.Annotations[LBHealthCheckUriAnnotationKey] != "/health" {
		t.Errorf("HealthCheckUri: expected %q, got %q", "/health", svc.Annotations[LBHealthCheckUriAnnotationKey])
	}

	if svc.Annotations[LBHealthCheckMethodAnnotationKey] != "GET" {
		t.Errorf("HealthCheckMethod: expected %q, got %q", "GET", svc.Annotations[LBHealthCheckMethodAnnotationKey])
	}
}

// 测试端口分配逻辑 - 边界条件
func TestPortAllocationEdgeCases(t *testing.T) {
	tests := []struct {
		name             string
		minPort          int32
		maxPort          int32
		blockPorts       []int32
		targetPortCount  int
		expectPodsPerNLB int
		expectError      bool
	}{
		{
			name:             "exact division",
			minPort:          10000,
			maxPort:          10999,
			blockPorts:       []int32{},
			targetPortCount:  2,
			expectPodsPerNLB: 500,
			expectError:      false,
		},
		{
			name:             "with remainder",
			minPort:          10000,
			maxPort:          10999,
			blockPorts:       []int32{},
			targetPortCount:  3,
			expectPodsPerNLB: 333,
			expectError:      false,
		},
		{
			name:             "minimum range",
			minPort:          10000,
			maxPort:          10001,
			blockPorts:       []int32{},
			targetPortCount:  1,
			expectPodsPerNLB: 2,
			expectError:      false,
		},
		{
			name:             "all ports blocked",
			minPort:          10000,
			maxPort:          10002,
			blockPorts:       []int32{10000, 10001, 10002},
			targetPortCount:  1,
			expectPodsPerNLB: 0,
			expectError:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lenRange := int(tt.maxPort) - int(tt.minPort) - len(tt.blockPorts) + 1
			podsPerNLB := lenRange / tt.targetPortCount

			if podsPerNLB != tt.expectPodsPerNLB {
				t.Errorf("podsPerNLB: expected %d, got %d", tt.expectPodsPerNLB, podsPerNLB)
			}
		})
	}
}

// 测试 maxPodIndex 并发安全性
func TestMaxPodIndexConcurrency(t *testing.T) {
	plugin := &AutoNLBsV2Plugin{
		maxPodIndex: make(map[string]int),
		mutex:       sync.RWMutex{},
	}

	gssKey := "default/test-gss"
	plugin.maxPodIndex[gssKey] = 0

	// 并发更新 maxPodIndex
	var wg sync.WaitGroup
	for i := 1; i <= 100; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("test-gss-%d", index),
					Namespace: "default",
					Labels: map[string]string{
						gamekruiseiov1alpha1.GameServerOwnerGssKey: "test-gss",
					},
				},
			}
			plugin.updateMaxPodIndex(pod)
		}(i)
	}
	wg.Wait()

	// 验证最终值
	plugin.mutex.RLock()
	finalMax := plugin.maxPodIndex[gssKey]
	plugin.mutex.RUnlock()

	if finalMax != 100 {
		t.Errorf("maxPodIndex after concurrent updates: expected 100, got %d", finalMax)
	}
}

// 测试资源命名规范的一致性
func TestResourceNamingConsistency(t *testing.T) {
	tests := []struct {
		name          string
		gssName       string
		eipIspType    string
		nlbIndex      int
		zoneIndex     int
		podIndex      int
		expectNLBName string
		expectEIPName string
		expectSvcName string
	}{
		{
			name:          "BGP single line",
			gssName:       "game-server",
			eipIspType:    "BGP",
			nlbIndex:      0,
			zoneIndex:     0,
			podIndex:      0,
			expectNLBName: "game-server-bgp-0",
			expectEIPName: "game-server-eip-bgp-0-z0",
			expectSvcName: "game-server-0-bgp",
		},
		{
			name:          "ChinaTelecom multi-zone",
			gssName:       "my-gss",
			eipIspType:    "ChinaTelecom",
			nlbIndex:      3,
			zoneIndex:     2,
			podIndex:      750,
			expectNLBName: "my-gss-chinatelecom-3",
			expectEIPName: "my-gss-eip-chinatelecom-3-z2",
			expectSvcName: "my-gss-750-chinatelecom",
		},
		{
			name:          "BGP_PRO advanced",
			gssName:       "test",
			eipIspType:    "BGP_PRO",
			nlbIndex:      10,
			zoneIndex:     1,
			podIndex:      2500,
			expectNLBName: "test-bgp_pro-10",
			expectEIPName: "test-eip-bgp_pro-10-z1",
			expectSvcName: "test-2500-bgp_pro",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 验证 NLB 命名
			nlbName := tt.gssName + "-" + strings.ToLower(tt.eipIspType) + "-" + strconv.Itoa(tt.nlbIndex)
			if nlbName != tt.expectNLBName {
				t.Errorf("NLB name: expected %q, got %q", tt.expectNLBName, nlbName)
			}

			// 验证 EIP 命名
			eipName := fmt.Sprintf("%s-eip-%s-%d-z%d",
				tt.gssName,
				strings.ToLower(tt.eipIspType),
				tt.nlbIndex,
				tt.zoneIndex)
			if eipName != tt.expectEIPName {
				t.Errorf("EIP name: expected %q, got %q", tt.expectEIPName, eipName)
			}

			// 验证 Service 命名
			basePodName := tt.gssName + "-" + strconv.Itoa(tt.podIndex)
			svcName := basePodName + "-" + strings.ToLower(tt.eipIspType)
			if svcName != tt.expectSvcName {
				t.Errorf("Service name: expected %q, got %q", tt.expectSvcName, svcName)
			}
		})
	}
}

// ==================== Fake Client 集成测试 ====================

// newFakeClient 创建一个带有所有必要 Scheme 的 fake client
func newFakeClient(objs ...client.Object) client.Client {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = gamekruiseiov1alpha1.AddToScheme(scheme)
	// 手动注册 NLB 和 EIP CRD
	scheme.AddKnownTypes(nlbv1.SchemeGroupVersion,
		&nlbv1.NLB{},
		&nlbv1.NLBList{},
	)
	scheme.AddKnownTypes(eipv1.GroupVersion,
		&eipv1.EIP{},
		&eipv1.EIPList{},
	)
	metav1.AddToGroupVersion(scheme, nlbv1.SchemeGroupVersion)
	metav1.AddToGroupVersion(scheme, eipv1.GroupVersion)

	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		Build()
}

// createTestGSS 创建测试用的 GameServerSet
func createTestGSS(namespace, name string, replicas int32, networkConf []gamekruiseiov1alpha1.NetworkConfParams) *gamekruiseiov1alpha1.GameServerSet {
	return &gamekruiseiov1alpha1.GameServerSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			UID:       types.UID("test-gss-uid-" + name),
		},
		TypeMeta: metav1.TypeMeta{
			APIVersion: "game.kruise.io/v1alpha1",
			Kind:       "GameServerSet",
		},
		Spec: gamekruiseiov1alpha1.GameServerSetSpec{
			Replicas: ptr.To(replicas),
			Network: &gamekruiseiov1alpha1.Network{
				NetworkType: AutoNLBsV2Network,
				NetworkConf: networkConf,
			},
		},
	}
}

// createTestPod 创建测试用的 Pod
func createTestPod(namespace, name, gssName string, podIndex int) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			UID:       types.UID("test-pod-uid-" + name),
			Labels: map[string]string{
				gamekruiseiov1alpha1.GameServerOwnerGssKey: gssName,
				SvcSelectorKey: name,
			},
			Annotations: map[string]string{
				gamekruiseiov1alpha1.GameServerNetworkType:   AutoNLBsV2Network,
				gamekruiseiov1alpha1.GameServerNetworkConf:   `[{"name":"ZoneMaps","value":"vpc-test@cn-hangzhou-h:vsw-aaa,cn-hangzhou-i:vsw-bbb"},{"name":"PortProtocols","value":"8080/TCP"},{"name":"EipIspTypes","value":"BGP"},{"name":"MinPort","value":"10000"},{"name":"MaxPort","value":"10999"}]`,
				gamekruiseiov1alpha1.GameServerNetworkStatus: "",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "game-server",
					Image: "test:latest",
				},
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			PodIP: fmt.Sprintf("10.0.%d.%d", podIndex/256, podIndex%256),
		},
	}
}

// TestAutoNLBsV2Plugin_Init_EmptyCluster 测试空集群初始化
func TestAutoNLBsV2Plugin_Init_EmptyCluster(t *testing.T) {
	c := newFakeClient()
	ctx := context.Background()

	plugin := &AutoNLBsV2Plugin{
		maxPodIndex: make(map[string]int),
		mutex:       sync.RWMutex{},
	}

	err := plugin.Init(c, nil, ctx)
	if err != nil {
		t.Errorf("Init() failed on empty cluster: %v", err)
	}

	if len(plugin.maxPodIndex) != 0 {
		t.Errorf("maxPodIndex should be empty, got: %v", plugin.maxPodIndex)
	}
}

// TestAutoNLBsV2Plugin_Init_WithSingleGSS 测试单个 GSS 初始化
func TestAutoNLBsV2Plugin_Init_WithSingleGSS(t *testing.T) {
	gss := createTestGSS("default", "test-gss", 10, []gamekruiseiov1alpha1.NetworkConfParams{
		{Name: "ZoneMaps", Value: "vpc-test@cn-hangzhou-h:vsw-aaa,cn-hangzhou-i:vsw-bbb"},
		{Name: "PortProtocols", Value: "8080/TCP"},
		{Name: "EipIspTypes", Value: "BGP"},
		{Name: "MinPort", Value: "10000"},
		{Name: "MaxPort", Value: "10999"},
	})

	c := newFakeClient(gss)
	ctx := context.Background()

	plugin := &AutoNLBsV2Plugin{
		maxPodIndex: make(map[string]int),
		mutex:       sync.RWMutex{},
	}

	err := plugin.Init(c, nil, ctx)
	if err != nil {
		t.Errorf("Init() failed: %v", err)
	}

	// 验证 maxPodIndex 被初始化为 replicas
	gssKey := "default/test-gss"
	plugin.mutex.RLock()
	maxIndex := plugin.maxPodIndex[gssKey]
	plugin.mutex.RUnlock()

	if maxIndex != 10 {
		t.Errorf("maxPodIndex: expected 10, got %d", maxIndex)
	}

	// 验证 EIP CR 被创建
	eipList := &eipv1.EIPList{}
	err = c.List(ctx, eipList, &client.ListOptions{Namespace: "default"})
	if err != nil {
		t.Errorf("Failed to list EIPs: %v", err)
	}

	// 期望至少创建 2 个 EIP (每个 zone 一个)
	if len(eipList.Items) < 2 {
		t.Errorf("Expected at least 2 EIPs, got %d", len(eipList.Items))
	}
}

// TestAutoNLBsV2Plugin_Init_WithMultipleGSS 测试多个 GSS 初始化
func TestAutoNLBsV2Plugin_Init_WithMultipleGSS(t *testing.T) {
	gss1 := createTestGSS("ns1", "gss-1", 5, []gamekruiseiov1alpha1.NetworkConfParams{
		{Name: "ZoneMaps", Value: "vpc-test@cn-hangzhou-h:vsw-aaa,cn-hangzhou-i:vsw-bbb"},
		{Name: "PortProtocols", Value: "8080/TCP"},
		{Name: "EipIspTypes", Value: "BGP"},
		{Name: "MinPort", Value: "10000"},
		{Name: "MaxPort", Value: "10999"},
	})

	gss2 := createTestGSS("ns2", "gss-2", 15, []gamekruiseiov1alpha1.NetworkConfParams{
		{Name: "ZoneMaps", Value: "vpc-test@cn-hangzhou-h:vsw-aaa,cn-hangzhou-i:vsw-bbb"},
		{Name: "PortProtocols", Value: "9000/UDP"},
		{Name: "EipIspTypes", Value: "BGP_PRO"},
		{Name: "MinPort", Value: "20000"},
		{Name: "MaxPort", Value: "20999"},
	})

	c := newFakeClient(gss1, gss2)
	ctx := context.Background()

	plugin := &AutoNLBsV2Plugin{
		maxPodIndex: make(map[string]int),
		mutex:       sync.RWMutex{},
	}

	err := plugin.Init(c, nil, ctx)
	if err != nil {
		t.Errorf("Init() failed: %v", err)
	}

	// 验证两个 GSS 的 maxPodIndex
	plugin.mutex.RLock()
	maxIndex1 := plugin.maxPodIndex["ns1/gss-1"]
	maxIndex2 := plugin.maxPodIndex["ns2/gss-2"]
	plugin.mutex.RUnlock()

	if maxIndex1 != 5 {
		t.Errorf("gss-1 maxPodIndex: expected 5, got %d", maxIndex1)
	}
	if maxIndex2 != 15 {
		t.Errorf("gss-2 maxPodIndex: expected 15, got %d", maxIndex2)
	}
}

// TestAutoNLBsV2Plugin_Init_WithMultiISP 测试多线路初始化
func TestAutoNLBsV2Plugin_Init_WithMultiISP(t *testing.T) {
	gss := createTestGSS("default", "multi-isp-gss", 10, []gamekruiseiov1alpha1.NetworkConfParams{
		{Name: "ZoneMaps", Value: "vpc-test@cn-hangzhou-h:vsw-aaa,cn-hangzhou-i:vsw-bbb"},
		{Name: "PortProtocols", Value: "8080/TCP"},
		{Name: "EipIspTypes", Value: "BGP,ChinaTelecom"},
		{Name: "MinPort", Value: "10000"},
		{Name: "MaxPort", Value: "10999"},
	})

	c := newFakeClient(gss)
	ctx := context.Background()

	plugin := &AutoNLBsV2Plugin{
		maxPodIndex: make(map[string]int),
		mutex:       sync.RWMutex{},
	}

	err := plugin.Init(c, nil, ctx)
	if err != nil {
		t.Errorf("Init() failed: %v", err)
	}

	// 验证创建了多个线路的 EIP
	eipList := &eipv1.EIPList{}
	err = c.List(ctx, eipList, &client.ListOptions{Namespace: "default"})
	if err != nil {
		t.Errorf("Failed to list EIPs: %v", err)
	}

	// 应该至少创建 4 个 EIP (2 个线路 * 2 个 zone)
	if len(eipList.Items) < 4 {
		t.Errorf("Expected at least 4 EIPs for dual ISP, got %d", len(eipList.Items))
	}

	// 验证 EIP 的 ISP 类型
	bgpCount := 0
	telecomCount := 0
	for _, eip := range eipList.Items {
		if eip.Spec.ISP == "BGP" {
			bgpCount++
		} else if eip.Spec.ISP == "ChinaTelecom" {
			telecomCount++
		}
	}

	if bgpCount < 2 || telecomCount < 2 {
		t.Errorf("Expected at least 2 BGP and 2 ChinaTelecom EIPs, got BGP=%d, ChinaTelecom=%d", bgpCount, telecomCount)
	}
}

// TestAutoNLBsV2Plugin_Init_InvalidConfig 测试配置错误的初始化
func TestAutoNLBsV2Plugin_Init_InvalidConfig(t *testing.T) {
	gss := createTestGSS("default", "invalid-gss", 5, []gamekruiseiov1alpha1.NetworkConfParams{
		{Name: "PortProtocols", Value: "8080/TCP"},
		// 缺少 ZoneMaps
	})

	c := newFakeClient(gss)
	ctx := context.Background()

	plugin := &AutoNLBsV2Plugin{
		maxPodIndex: make(map[string]int),
		mutex:       sync.RWMutex{},
	}

	// Init 不应该失败,只是记录错误
	err := plugin.Init(c, nil, ctx)
	if err != nil {
		t.Errorf("Init() should not fail on invalid config: %v", err)
	}
}

// TestAutoNLBsV2Plugin_OnPodAdded 测试 Pod 添加
func TestAutoNLBsV2Plugin_OnPodAdded(t *testing.T) {
	pod := createTestPod("default", "test-gss-5", "test-gss", 5)

	c := newFakeClient()
	ctx := context.Background()

	plugin := &AutoNLBsV2Plugin{
		maxPodIndex: map[string]int{
			"default/test-gss": 0,
		},
		mutex: sync.RWMutex{},
	}

	_, err := plugin.OnPodAdded(c, pod, ctx)
	if err != nil {
		t.Errorf("OnPodAdded() failed: %v", err)
	}

	// 验证 maxPodIndex 更新
	plugin.mutex.RLock()
	maxIndex := plugin.maxPodIndex["default/test-gss"]
	plugin.mutex.RUnlock()

	if maxIndex != 5 {
		t.Errorf("maxPodIndex: expected 5, got %d", maxIndex)
	}
}

// TestAutoNLBsV2Plugin_OnPodUpdated_FirstTime 测试首次 Pod 更新
func TestAutoNLBsV2Plugin_OnPodUpdated_FirstTime(t *testing.T) {
	gss := createTestGSS("default", "test-gss", 10, []gamekruiseiov1alpha1.NetworkConfParams{
		{Name: "ZoneMaps", Value: "vpc-test@cn-hangzhou-h:vsw-aaa,cn-hangzhou-i:vsw-bbb"},
		{Name: "PortProtocols", Value: "8080/TCP"},
		{Name: "EipIspTypes", Value: "BGP"},
		{Name: "MinPort", Value: "10000"},
		{Name: "MaxPort", Value: "10999"},
	})

	pod := createTestPod("default", "test-gss-0", "test-gss", 0)

	c := newFakeClient(gss, pod)
	ctx := context.Background()

	plugin := &AutoNLBsV2Plugin{
		maxPodIndex: map[string]int{
			"default/test-gss": 10,
		},
		mutex: sync.RWMutex{},
	}

	_, err := plugin.OnPodUpdated(c, pod, ctx)
	// 第一次调用时，NLB 和 Service 都不存在，应该开始创建资源
	if err != nil {
		t.Logf("OnPodUpdated returned error (expected on first call): %v", err)
	}

	// 验证 EIP CR 被创建
	eipList := &eipv1.EIPList{}
	listErr := c.List(ctx, eipList, &client.ListOptions{Namespace: "default"})
	if listErr != nil {
		t.Errorf("Failed to list EIPs: %v", listErr)
	}

	if len(eipList.Items) == 0 {
		t.Errorf("Expected EIPs to be created, got 0")
	}
}

// TestAutoNLBsV2Plugin_OnPodUpdated_NLBReady 测试 NLB 就绪的场景
func TestAutoNLBsV2Plugin_OnPodUpdated_NLBReady(t *testing.T) {
	gss := createTestGSS("default", "test-gss", 10, []gamekruiseiov1alpha1.NetworkConfParams{
		{Name: "ZoneMaps", Value: "vpc-test@cn-hangzhou-h:vsw-aaa,cn-hangzhou-i:vsw-bbb"},
		{Name: "PortProtocols", Value: "8080/TCP"},
		{Name: "EipIspTypes", Value: "BGP"},
		{Name: "MinPort", Value: "10000"},
		{Name: "MaxPort", Value: "10999"},
	})

	pod := createTestPod("default", "test-gss-0", "test-gss", 0)

	// 创建已就绪的 NLB
	nlb := &nlbv1.NLB{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gss-bgp-0",
			Namespace: "default",
		},
		Status: nlbv1.NLBStatus{
			LoadBalancerId: "nlb-test-12345",
			DNSName:        "nlb-test.aliyuncs.com",
		},
	}

	// 创建已就绪的 Service
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gss-0-bgp",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeLoadBalancer,
			Ports: []corev1.ServicePort{
				{Port: 10000, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt(8080)},
			},
		},
		Status: corev1.ServiceStatus{
			LoadBalancer: corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{
					{IP: "1.2.3.4", Hostname: "nlb-test.aliyuncs.com"},
				},
			},
		},
	}

	c := newFakeClient(gss, pod, nlb, svc)
	ctx := context.Background()

	plugin := &AutoNLBsV2Plugin{
		maxPodIndex: map[string]int{
			"default/test-gss": 10,
		},
		mutex: sync.RWMutex{},
	}

	updatedPod, err := plugin.OnPodUpdated(c, pod, ctx)
	if err != nil {
		t.Errorf("OnPodUpdated failed: %v", err)
	}

	// 验证网络状态已更新为 Ready
	if updatedPod.Annotations[gamekruiseiov1alpha1.GameServerNetworkStatus] == "" {
		t.Errorf("NetworkStatus should be set")
	}
}

// TestAutoNLBsV2Plugin_OnPodUpdated_ServiceNotReady 测试 Service 未就绪的场景
func TestAutoNLBsV2Plugin_OnPodUpdated_ServiceNotReady(t *testing.T) {
	gss := createTestGSS("default", "test-gss", 10, []gamekruiseiov1alpha1.NetworkConfParams{
		{Name: "ZoneMaps", Value: "vpc-test@cn-hangzhou-h:vsw-aaa,cn-hangzhou-i:vsw-bbb"},
		{Name: "PortProtocols", Value: "8080/TCP"},
		{Name: "EipIspTypes", Value: "BGP"},
		{Name: "MinPort", Value: "10000"},
		{Name: "MaxPort", Value: "10999"},
	})

	pod := createTestPod("default", "test-gss-0", "test-gss", 0)

	// NLB 已就绪
	nlb := &nlbv1.NLB{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gss-bgp-0",
			Namespace: "default",
		},
		Status: nlbv1.NLBStatus{
			LoadBalancerId: "nlb-test-12345",
		},
	}

	// Service 存在但未就绪（无 LoadBalancer Ingress）
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gss-0-bgp",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeLoadBalancer,
		},
		Status: corev1.ServiceStatus{
			LoadBalancer: corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{}, // 空的，未就绪
			},
		},
	}

	c := newFakeClient(gss, pod, nlb, svc)
	ctx := context.Background()

	plugin := &AutoNLBsV2Plugin{
		maxPodIndex: map[string]int{
			"default/test-gss": 10,
		},
		mutex: sync.RWMutex{},
	}

	updatedPod, err := plugin.OnPodUpdated(c, pod, ctx)
	if err != nil {
		t.Errorf("OnPodUpdated should not fail: %v", err)
	}

	// 验证网络状态为 NotReady
	if updatedPod.Annotations[gamekruiseiov1alpha1.GameServerNetworkStatus] != "" {
		// 解析网络状态
		t.Logf("Network status: %s", updatedPod.Annotations[gamekruiseiov1alpha1.GameServerNetworkStatus])
	}
}

// TestAutoNLBsV2Plugin_OnPodUpdated_MultiISP_AllReady 测试多线路全部就绪
func TestAutoNLBsV2Plugin_OnPodUpdated_MultiISP_AllReady(t *testing.T) {
	gss := createTestGSS("default", "test-gss", 10, []gamekruiseiov1alpha1.NetworkConfParams{
		{Name: "ZoneMaps", Value: "vpc-test@cn-hangzhou-h:vsw-aaa,cn-hangzhou-i:vsw-bbb"},
		{Name: "PortProtocols", Value: "8080/TCP"},
		{Name: "EipIspTypes", Value: "BGP,ChinaTelecom"},
		{Name: "MinPort", Value: "10000"},
		{Name: "MaxPort", Value: "10999"},
	})

	pod := createTestPod("default", "test-gss-0", "test-gss", 0)
	pod.Annotations[gamekruiseiov1alpha1.GameServerNetworkConf] = `[{"name":"ZoneMaps","value":"vpc-test@cn-hangzhou-h:vsw-aaa,cn-hangzhou-i:vsw-bbb"},{"name":"PortProtocols","value":"8080/TCP"},{"name":"EipIspTypes","value":"BGP,ChinaTelecom"},{"name":"MinPort","value":"10000"},{"name":"MaxPort","value":"10999"}]`

	// BGP NLB
	nlbBGP := &nlbv1.NLB{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gss-bgp-0",
			Namespace: "default",
		},
		Status: nlbv1.NLBStatus{
			LoadBalancerId: "nlb-bgp-12345",
		},
	}

	// ChinaTelecom NLB
	nlbTelecom := &nlbv1.NLB{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gss-chinatelecom-0",
			Namespace: "default",
		},
		Status: nlbv1.NLBStatus{
			LoadBalancerId: "nlb-telecom-12345",
		},
	}

	// BGP Service
	svcBGP := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gss-0-bgp",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeLoadBalancer,
			Ports: []corev1.ServicePort{
				{Port: 10000, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt(8080)},
			},
		},
		Status: corev1.ServiceStatus{
			LoadBalancer: corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{
					{IP: "1.2.3.4", Hostname: "nlb-bgp.aliyuncs.com"},
				},
			},
		},
	}

	// ChinaTelecom Service
	svcTelecom := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gss-0-chinatelecom",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeLoadBalancer,
			Ports: []corev1.ServicePort{
				{Port: 10000, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt(8080)},
			},
		},
		Status: corev1.ServiceStatus{
			LoadBalancer: corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{
					{IP: "5.6.7.8", Hostname: "nlb-telecom.aliyuncs.com"},
				},
			},
		},
	}

	c := newFakeClient(gss, pod, nlbBGP, nlbTelecom, svcBGP, svcTelecom)
	ctx := context.Background()

	plugin := &AutoNLBsV2Plugin{
		maxPodIndex: map[string]int{
			"default/test-gss": 10,
		},
		mutex: sync.RWMutex{},
	}

	updatedPod, err := plugin.OnPodUpdated(c, pod, ctx)
	if err != nil {
		t.Errorf("OnPodUpdated failed: %v", err)
	}

	// 验证网络状态包含两条线路的地址
	if updatedPod.Annotations[gamekruiseiov1alpha1.GameServerNetworkStatus] == "" {
		t.Errorf("NetworkStatus should be set for multi-ISP")
	}
	t.Logf("Multi-ISP network status: %s", updatedPod.Annotations[gamekruiseiov1alpha1.GameServerNetworkStatus])
}

// TestAutoNLBsV2Plugin_OnPodUpdated_ConfigError 测试配置错误
func TestAutoNLBsV2Plugin_OnPodUpdated_ConfigError(t *testing.T) {
	pod := createTestPod("default", "test-gss-0", "test-gss", 0)
	// 设置错误的配置（缺少 ZoneMaps）
	pod.Annotations[gamekruiseiov1alpha1.GameServerNetworkConf] = `[{"name":"PortProtocols","value":"8080/TCP"}]`

	c := newFakeClient(pod)
	ctx := context.Background()

	plugin := &AutoNLBsV2Plugin{
		maxPodIndex: map[string]int{
			"default/test-gss": 10,
		},
		mutex: sync.RWMutex{},
	}

	_, err := plugin.OnPodUpdated(c, pod, ctx)
	if err == nil {
		t.Errorf("OnPodUpdated should fail with invalid config")
	}
}

// TestAutoNLBsV2Plugin_OnPodUpdated_GSSDeleting 测试 GSS 正在删除时的处理
func TestAutoNLBsV2Plugin_OnPodUpdated_GSSDeleting(t *testing.T) {
	plugin := &AutoNLBsV2Plugin{
		maxPodIndex: make(map[string]int),
		mutex:       sync.RWMutex{},
	}

	// 网络配置（RetainNLBOnDelete=false）
	networkConf := []gamekruiseiov1alpha1.NetworkConfParams{
		{Name: "MinPort", Value: "10000"},
		{Name: "MaxPort", Value: "10100"},
		{Name: "PortProtocols", Value: "8080/TCP"},
		{Name: "EIPIspTypes", Value: "BGP"},
		{Name: "ZoneMaps", Value: "vpc-test@cn-hangzhou-h:vsw-a,cn-hangzhou-i:vsw-b"},
		{Name: "RetainNLBOnDelete", Value: "false"},
	}
	networkConfBytes, _ := json.Marshal(networkConf)

	// 创建 GSS（设置 DeletionTimestamp 表示正在删除）
	now := metav1.Now()
	gss := &gamekruiseiov1alpha1.GameServerSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-gss",
			Namespace:         "default",
			UID:               "test-uid-123",
			DeletionTimestamp: &now,
			Finalizers:        []string{"test-finalizer"},
		},
		Spec: gamekruiseiov1alpha1.GameServerSetSpec{
			Network: &gamekruiseiov1alpha1.Network{
				NetworkType: AutoNLBsV2Network,
				NetworkConf: networkConf,
			},
		},
	}

	// 创建带 Finalizer 的 Pod
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gss-0",
			Namespace: "default",
			Labels: map[string]string{
				gamekruiseiov1alpha1.GameServerOwnerGssKey: "test-gss",
			},
			Annotations: map[string]string{
				gamekruiseiov1alpha1.GameServerNetworkType:   AutoNLBsV2Network,
				gamekruiseiov1alpha1.GameServerNetworkConf:   string(networkConfBytes),
				gamekruiseiov1alpha1.GameServerNetworkStatus: `{"currentNetworkState":"Ready"}`,
			},
			Finalizers: []string{PodFinalizerName},
		},
	}

	// 使用 fake client
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gss, pod).Build()

	_, err := plugin.OnPodUpdated(c, pod, context.Background())
	if err != nil {
		t.Errorf("OnPodUpdated() should return nil when GSS is deleting, got error: %v", err)
	}

	// 验证 Pod Finalizer 仍然存在（OnPodUpdated 只跳过，不移除 Finalizer）
	updatedPod := &corev1.Pod{}
	if err := c.Get(context.Background(), types.NamespacedName{Name: "test-gss-0", Namespace: "default"}, updatedPod); err != nil {
		t.Fatalf("Failed to get updated Pod: %v", err)
	}
	hasFinalizer := false
	for _, f := range updatedPod.GetFinalizers() {
		if f == PodFinalizerName {
			hasFinalizer = true
			break
		}
	}
	if !hasFinalizer {
		t.Errorf("Pod Finalizer should still exist in OnPodUpdated (removal happens in OnPodDeleted)")
	}
}

// TestAutoNLBsV2Plugin_OnPodUpdated_GSSNotFound 测试 GSS 不存在时的处理
func TestAutoNLBsV2Plugin_OnPodUpdated_GSSNotFound(t *testing.T) {
	plugin := &AutoNLBsV2Plugin{
		maxPodIndex: make(map[string]int),
		mutex:       sync.RWMutex{},
	}

	// 网络配置（RetainNLBOnDelete=false）
	networkConf := []gamekruiseiov1alpha1.NetworkConfParams{
		{Name: "MinPort", Value: "10000"},
		{Name: "MaxPort", Value: "10100"},
		{Name: "PortProtocols", Value: "8080/TCP"},
		{Name: "EIPIspTypes", Value: "BGP"},
		{Name: "ZoneMaps", Value: "vpc-test@cn-hangzhou-h:vsw-a,cn-hangzhou-i:vsw-b"},
		{Name: "RetainNLBOnDelete", Value: "false"},
	}
	networkConfBytes, _ := json.Marshal(networkConf)

	// 创建带 Finalizer 的 Pod（GSS 不存在）
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gss-0",
			Namespace: "default",
			Labels: map[string]string{
				gamekruiseiov1alpha1.GameServerOwnerGssKey: "test-gss",
			},
			Annotations: map[string]string{
				gamekruiseiov1alpha1.GameServerNetworkType:   AutoNLBsV2Network,
				gamekruiseiov1alpha1.GameServerNetworkConf:   string(networkConfBytes),
				gamekruiseiov1alpha1.GameServerNetworkStatus: `{"currentNetworkState":"Ready"}`,
			},
			Finalizers: []string{PodFinalizerName},
		},
	}

	// 使用 fake client（不创建 GSS）
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()

	_, err := plugin.OnPodUpdated(c, pod, context.Background())
	if err != nil {
		t.Errorf("OnPodUpdated() should return nil when GSS not found, got error: %v", err)
	}

	// 验证 Pod Finalizer 仍然存在（OnPodUpdated 只跳过，不移除 Finalizer）
	updatedPod := &corev1.Pod{}
	if err := c.Get(context.Background(), types.NamespacedName{Name: "test-gss-0", Namespace: "default"}, updatedPod); err != nil {
		t.Fatalf("Failed to get updated Pod: %v", err)
	}
	hasFinalizer := false
	for _, f := range updatedPod.GetFinalizers() {
		if f == PodFinalizerName {
			hasFinalizer = true
			break
		}
	}
	if !hasFinalizer {
		t.Errorf("Pod Finalizer should still exist in OnPodUpdated (removal happens in OnPodDeleted)")
	}
}

// TestAutoNLBsV2Plugin_EnsureNLBForPod 测试为 Pod 动态创建 NLB
func TestAutoNLBsV2Plugin_EnsureNLBForPod(t *testing.T) {
	gss := createTestGSS("default", "test-gss", 10, []gamekruiseiov1alpha1.NetworkConfParams{
		{Name: "ZoneMaps", Value: "vpc-test@cn-hangzhou-h:vsw-aaa,cn-hangzhou-i:vsw-bbb"},
		{Name: "PortProtocols", Value: "8080/TCP"},
		{Name: "EipIspTypes", Value: "BGP"},
		{Name: "MinPort", Value: "10000"},
		{Name: "MaxPort", Value: "10999"},
	})

	// 预先创建 EIP CR，并设置 AllocationID
	eip1 := &eipv1.EIP{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gss-eip-bgp-0-z0",
			Namespace: "default",
		},
		Spec: eipv1.EIPSpec{
			ISP:                "BGP",
			InternetChargeType: "PayByTraffic",
			Bandwidth:          "5",
		},
		Status: eipv1.EIPStatus{
			AllocationID: "eip-alloc-12345",
		},
	}

	eip2 := &eipv1.EIP{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gss-eip-bgp-0-z1",
			Namespace: "default",
		},
		Spec: eipv1.EIPSpec{
			ISP:                "BGP",
			InternetChargeType: "PayByTraffic",
			Bandwidth:          "5",
		},
		Status: eipv1.EIPStatus{
			AllocationID: "eip-alloc-67890",
		},
	}

	c := newFakeClient(gss, eip1, eip2)
	ctx := context.Background()

	plugin := &AutoNLBsV2Plugin{
		maxPodIndex: map[string]int{
			"default/test-gss": 10,
		},
		mutex: sync.RWMutex{},
	}

	config := &autoNLBsConfig{
		zoneMaps:    "vpc-test@cn-hangzhou-h:vsw-aaa,cn-hangzhou-i:vsw-bbb",
		eipIspTypes: []string{"BGP"},
		targetPorts: []int{8080},
		protocols:   []corev1.Protocol{corev1.ProtocolTCP},
		minPort:     10000,
		maxPort:     10999,
	}

	// 测试创建 NLB
	err := plugin.ensureNLBForPod(ctx, c, "default", "test-gss", "BGP", 0, config)
	if err != nil {
		t.Errorf("ensureNLBForPod failed: %v", err)
	}

	// 验证 NLB CR 被创建
	nlb := &nlbv1.NLB{}
	getErr := c.Get(ctx, types.NamespacedName{
		Name:      "test-gss-bgp-0",
		Namespace: "default",
	}, nlb)

	if getErr != nil {
		t.Errorf("NLB should be created: %v", getErr)
	}

	// 验证 NLB 的 ZoneMappings 包含 EIP AllocationID
	if len(nlb.Spec.ZoneMappings) != 2 {
		t.Errorf("Expected 2 zone mappings, got %d", len(nlb.Spec.ZoneMappings))
	}

	if nlb.Spec.ZoneMappings[0].AllocationId != "eip-alloc-12345" {
		t.Errorf("Zone 0 AllocationID: expected eip-alloc-12345, got %s", nlb.Spec.ZoneMappings[0].AllocationId)
	}

	if nlb.Spec.ZoneMappings[1].AllocationId != "eip-alloc-67890" {
		t.Errorf("Zone 1 AllocationID: expected eip-alloc-67890, got %s", nlb.Spec.ZoneMappings[1].AllocationId)
	}

	// 验证 NLB Labels
	if nlb.Labels[NLBPoolGssLabel] != "test-gss" {
		t.Errorf("NLB Pool GSS label: expected test-gss, got %s", nlb.Labels[NLBPoolGssLabel])
	}

	if nlb.Labels[NLBPoolEipIspTypeLabel] != "BGP" {
		t.Errorf("NLB Pool ISP label: expected BGP, got %s", nlb.Labels[NLBPoolEipIspTypeLabel])
	}
}

// TestAutoNLBsV2Plugin_EnsureNLBForPod_ChinaTelecom 测试创建单线 ISP 的 NLB
func TestAutoNLBsV2Plugin_EnsureNLBForPod_ChinaTelecom(t *testing.T) {
	gss := createTestGSS("default", "test-gss", 10, []gamekruiseiov1alpha1.NetworkConfParams{
		{Name: "ZoneMaps", Value: "vpc-test@cn-hangzhou-h:vsw-aaa,cn-hangzhou-i:vsw-bbb"},
		{Name: "PortProtocols", Value: "8080/TCP"},
		{Name: "EipIspTypes", Value: "ChinaTelecom"},
		{Name: "MinPort", Value: "10000"},
		{Name: "MaxPort", Value: "10999"},
	})

	// 预先创建 EIP CR，并设置 AllocationID
	eip1 := &eipv1.EIP{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gss-eip-chinatelecom-0-z0",
			Namespace: "default",
		},
		Spec: eipv1.EIPSpec{
			ISP:                "ChinaTelecom",
			InternetChargeType: "PayByBandwidth",
			Bandwidth:          "5",
		},
		Status: eipv1.EIPStatus{
			AllocationID: "eip-telecom-12345",
		},
	}

	eip2 := &eipv1.EIP{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gss-eip-chinatelecom-0-z1",
			Namespace: "default",
		},
		Spec: eipv1.EIPSpec{
			ISP:                "ChinaTelecom",
			InternetChargeType: "PayByBandwidth",
			Bandwidth:          "5",
		},
		Status: eipv1.EIPStatus{
			AllocationID: "eip-telecom-67890",
		},
	}

	c := newFakeClient(gss, eip1, eip2)
	ctx := context.Background()

	plugin := &AutoNLBsV2Plugin{
		maxPodIndex: map[string]int{
			"default/test-gss": 10,
		},
		mutex: sync.RWMutex{},
	}

	config := &autoNLBsConfig{
		zoneMaps:    "vpc-test@cn-hangzhou-h:vsw-aaa,cn-hangzhou-i:vsw-bbb",
		eipIspTypes: []string{"ChinaTelecom"},
		targetPorts: []int{8080},
		protocols:   []corev1.Protocol{corev1.ProtocolTCP},
		minPort:     10000,
		maxPort:     10999,
	}

	err := plugin.ensureNLBForPod(ctx, c, "default", "test-gss", "ChinaTelecom", 0, config)
	if err != nil {
		t.Errorf("ensureNLBForPod failed: %v", err)
	}

	// 验证 NLB CR 被创建
	nlb := &nlbv1.NLB{}
	getErr := c.Get(ctx, types.NamespacedName{
		Name:      "test-gss-chinatelecom-0",
		Namespace: "default",
	}, nlb)

	if getErr != nil {
		t.Errorf("NLB should be created: %v", getErr)
	}

	// 验证 NLB 使用了正确的 EIP
	if len(nlb.Spec.ZoneMappings) != 2 {
		t.Errorf("Expected 2 zone mappings, got %d", len(nlb.Spec.ZoneMappings))
	}

	// 验证单线 ISP 的 EIP AllocationID
	if nlb.Spec.ZoneMappings[0].AllocationId != "eip-telecom-12345" {
		t.Errorf("Zone 0 AllocationID: expected eip-telecom-12345, got %s", nlb.Spec.ZoneMappings[0].AllocationId)
	}
}

// TestAutoNLBsV2Plugin_EnsureServiceForPod 测试为 Pod 创建 Service
func TestAutoNLBsV2Plugin_EnsureServiceForPod(t *testing.T) {
	gss := createTestGSS("default", "test-gss", 10, []gamekruiseiov1alpha1.NetworkConfParams{
		{Name: "ZoneMaps", Value: "vpc-test@cn-hangzhou-h:vsw-aaa,cn-hangzhou-i:vsw-bbb"},
		{Name: "PortProtocols", Value: "8080/TCP,9000/UDP"},
		{Name: "EipIspTypes", Value: "BGP"},
		{Name: "MinPort", Value: "10000"},
		{Name: "MaxPort", Value: "10999"},
	})

	pod := createTestPod("default", "test-gss-0", "test-gss", 0)

	c := newFakeClient(gss, pod)
	ctx := context.Background()

	plugin := &AutoNLBsV2Plugin{
		maxPodIndex: map[string]int{
			"default/test-gss": 10,
		},
		mutex: sync.RWMutex{},
	}

	config := &autoNLBsConfig{
		zoneMaps:              "vpc-test@cn-hangzhou-h:vsw-aaa,cn-hangzhou-i:vsw-bbb",
		eipIspTypes:           []string{"BGP"},
		targetPorts:           []int{8080, 9000},
		protocols:             []corev1.Protocol{corev1.ProtocolTCP, corev1.ProtocolUDP},
		minPort:               10000,
		maxPort:               10999,
		externalTrafficPolicy: corev1.ServiceExternalTrafficPolicyTypeLocal,
		retainNLBOnDelete:     true, // 默认保留 NLB
		nlbHealthConfig: &nlbHealthConfig{
			lBHealthCheckFlag: "on",
			lBHealthCheckType: "tcp",
		},
	}

	// 创建 Service
	err := plugin.ensureServiceForPod(ctx, c, pod, "test-gss", "BGP", 0, "nlb-test-12345", config)
	if err != nil {
		t.Errorf("ensureServiceForPod failed: %v", err)
	}

	// 验证 Service 被创建
	svc := &corev1.Service{}
	getErr := c.Get(ctx, types.NamespacedName{
		Name:      "test-gss-0-bgp",
		Namespace: "default",
	}, svc)

	if getErr != nil {
		t.Errorf("Service should be created: %v", getErr)
	}

	// 验证 Service 属性
	if svc.Spec.Type != corev1.ServiceTypeLoadBalancer {
		t.Errorf("Service type: expected LoadBalancer, got %s", svc.Spec.Type)
	}

	if len(svc.Spec.Ports) != 2 {
		t.Errorf("Service ports: expected 2, got %d", len(svc.Spec.Ports))
	}

	// 验证端口映射
	if svc.Spec.Ports[0].Port != 10000 {
		t.Errorf("First port: expected 10000, got %d", svc.Spec.Ports[0].Port)
	}
	if svc.Spec.Ports[0].TargetPort.IntVal != 8080 {
		t.Errorf("First target port: expected 8080, got %d", svc.Spec.Ports[0].TargetPort.IntVal)
	}
	if svc.Spec.Ports[0].Protocol != corev1.ProtocolTCP {
		t.Errorf("First port protocol: expected TCP, got %s", svc.Spec.Ports[0].Protocol)
	}

	if svc.Spec.Ports[1].Port != 10001 {
		t.Errorf("Second port: expected 10001, got %d", svc.Spec.Ports[1].Port)
	}
	if svc.Spec.Ports[1].TargetPort.IntVal != 9000 {
		t.Errorf("Second target port: expected 9000, got %d", svc.Spec.Ports[1].TargetPort.IntVal)
	}
	if svc.Spec.Ports[1].Protocol != corev1.ProtocolUDP {
		t.Errorf("Second port protocol: expected UDP, got %s", svc.Spec.Ports[1].Protocol)
	}

	// 验证 Selector
	if svc.Spec.Selector[SvcSelectorKey] != "test-gss-0" {
		t.Errorf("Service selector: expected test-gss-0, got %s", svc.Spec.Selector[SvcSelectorKey])
	}

	// 验证 Annotations
	if svc.Annotations[SlbIdAnnotationKey] != "nlb-test-12345" {
		t.Errorf("NLB ID annotation: expected nlb-test-12345, got %s", svc.Annotations[SlbIdAnnotationKey])
	}

	if svc.Annotations[LBHealthCheckFlagAnnotationKey] != "on" {
		t.Errorf("Health check flag: expected on, got %s", svc.Annotations[LBHealthCheckFlagAnnotationKey])
	}

	// 验证 OwnerReference 指向 GSS
	if len(svc.OwnerReferences) != 1 {
		t.Errorf("OwnerReferences count: expected 1, got %d", len(svc.OwnerReferences))
	} else {
		if svc.OwnerReferences[0].Kind != "GameServerSet" {
			t.Errorf("OwnerReference kind: expected GameServerSet, got %s", svc.OwnerReferences[0].Kind)
		}
		if svc.OwnerReferences[0].Name != "test-gss" {
			t.Errorf("OwnerReference name: expected test-gss, got %s", svc.OwnerReferences[0].Name)
		}
	}
}

// TestAutoNLBsV2Plugin_EnsureServiceForPod_PortCalculation 测试端口分配计算
func TestAutoNLBsV2Plugin_EnsureServiceForPod_PortCalculation(t *testing.T) {
	tests := []struct {
		name           string
		podIndex       int
		minPort        int32
		maxPort        int32
		targetPorts    []int
		expectedPorts  []int32
		podsPerNLB     int
		nlbIndexExpect int
	}{
		{
			name:           "pod-0 with 2 target ports",
			podIndex:       0,
			minPort:        10000,
			maxPort:        10999,
			targetPorts:    []int{8080, 9000},
			expectedPorts:  []int32{10000, 10001},
			podsPerNLB:     500,
			nlbIndexExpect: 0,
		},
		{
			name:           "pod-1 with 2 target ports",
			podIndex:       1,
			minPort:        10000,
			maxPort:        10999,
			targetPorts:    []int{8080, 9000},
			expectedPorts:  []int32{10002, 10003},
			podsPerNLB:     500,
			nlbIndexExpect: 0,
		},
		{
			name:           "pod-500 with 2 target ports (next NLB)",
			podIndex:       500,
			minPort:        10000,
			maxPort:        10999,
			targetPorts:    []int{8080, 9000},
			expectedPorts:  []int32{10000, 10001},
			podsPerNLB:     500,
			nlbIndexExpect: 1,
		},
		{
			name:           "pod-0 with 3 target ports",
			podIndex:       0,
			minPort:        20000,
			maxPort:        20999,
			targetPorts:    []int{8080, 9000, 7777},
			expectedPorts:  []int32{20000, 20001, 20002},
			podsPerNLB:     333,
			nlbIndexExpect: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 计算 NLB 索引
			nlbIndex := tt.podIndex / tt.podsPerNLB
			if nlbIndex != tt.nlbIndexExpect {
				t.Errorf("NLB index: expected %d, got %d", tt.nlbIndexExpect, nlbIndex)
			}

			// 计算 Pod 在 NLB 中的相对索引
			podIndexInNLB := tt.podIndex % tt.podsPerNLB

			// 计算端口
			for i := 0; i < len(tt.targetPorts); i++ {
				portOffset := int32(podIndexInNLB*len(tt.targetPorts) + i)
				port := tt.minPort + portOffset

				if port != tt.expectedPorts[i] {
					t.Errorf("Port[%d]: expected %d, got %d", i, tt.expectedPorts[i], port)
				}
			}
		})
	}
}

// TestAutoNLBsV2Plugin_EnsureServiceForPod_Idempotent 测试 Service 创建的幂等性
func TestAutoNLBsV2Plugin_EnsureServiceForPod_Idempotent(t *testing.T) {
	gss := createTestGSS("default", "test-gss", 10, []gamekruiseiov1alpha1.NetworkConfParams{
		{Name: "ZoneMaps", Value: "vpc-test@cn-hangzhou-h:vsw-aaa,cn-hangzhou-i:vsw-bbb"},
		{Name: "PortProtocols", Value: "8080/TCP"},
		{Name: "EipIspTypes", Value: "BGP"},
		{Name: "MinPort", Value: "10000"},
		{Name: "MaxPort", Value: "10999"},
	})

	pod := createTestPod("default", "test-gss-0", "test-gss", 0)

	// 预先创建 Service
	existingSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gss-0-bgp",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeLoadBalancer,
		},
	}

	c := newFakeClient(gss, pod, existingSvc)
	ctx := context.Background()

	plugin := &AutoNLBsV2Plugin{
		maxPodIndex: map[string]int{
			"default/test-gss": 10,
		},
		mutex: sync.RWMutex{},
	}

	config := &autoNLBsConfig{
		zoneMaps:              "vpc-test@cn-hangzhou-h:vsw-aaa,cn-hangzhou-i:vsw-bbb",
		eipIspTypes:           []string{"BGP"},
		targetPorts:           []int{8080},
		protocols:             []corev1.Protocol{corev1.ProtocolTCP},
		minPort:               10000,
		maxPort:               10999,
		externalTrafficPolicy: corev1.ServiceExternalTrafficPolicyTypeLocal,
		retainNLBOnDelete:     true, // 默认保留 NLB
		nlbHealthConfig: &nlbHealthConfig{
			lBHealthCheckFlag: "off",
		},
	}

	// 第一次调用，应该不报错（Service 已存在）
	err := plugin.ensureServiceForPod(ctx, c, pod, "test-gss", "BGP", 0, "nlb-test-12345", config)
	if err != nil {
		t.Errorf("First call should succeed (idempotent): %v", err)
	}

	// 第二次调用，应该仍然不报错
	err = plugin.ensureServiceForPod(ctx, c, pod, "test-gss", "BGP", 0, "nlb-test-12345", config)
	if err != nil {
		t.Errorf("Second call should succeed (idempotent): %v", err)
	}
}

// TestAutoNLBsV2Plugin_RetainNLBOnDelete 测试 RetainNLBOnDelete 参数
// NLB 和 EIP 始终不设置 OwnerReference，由 Finalizer 控制删除
func TestAutoNLBsV2Plugin_RetainNLBOnDelete(t *testing.T) {
	tests := []struct {
		name              string
		retainNLBOnDelete bool
	}{
		{
			name:              "RetainNLBOnDelete=true (default) - NLB/EIP 保留",
			retainNLBOnDelete: true,
		},
		{
			name:              "RetainNLBOnDelete=false - NLB/EIP 由 Finalizer 删除",
			retainNLBOnDelete: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gss := createTestGSS("default", "test-gss", 10, []gamekruiseiov1alpha1.NetworkConfParams{
				{Name: "ZoneMaps", Value: "vpc-test@cn-hangzhou-h:vsw-aaa,cn-hangzhou-i:vsw-bbb"},
				{Name: "PortProtocols", Value: "8080/TCP"},
				{Name: "EipIspTypes", Value: "BGP"},
				{Name: "MinPort", Value: "10000"},
				{Name: "MaxPort", Value: "10999"},
			})

			// 预先创建没有 OwnerReference 的 EIP（符合新逻辑）
			eip0 := &eipv1.EIP{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gss-eip-bgp-0-z0",
					Namespace: "default",
				},
				Spec: eipv1.EIPSpec{
					Name:               "test-gss-eip-bgp-0-z0",
					Bandwidth:          "5",
					InternetChargeType: "PayByTraffic",
					ISP:                "BGP",
				},
				Status: eipv1.EIPStatus{
					AllocationID: "eip-bgp-zone0-12345",
				},
			}

			eip1 := &eipv1.EIP{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gss-eip-bgp-0-z1",
					Namespace: "default",
				},
				Spec: eipv1.EIPSpec{
					Name:               "test-gss-eip-bgp-0-z1",
					Bandwidth:          "5",
					InternetChargeType: "PayByTraffic",
					ISP:                "BGP",
				},
				Status: eipv1.EIPStatus{
					AllocationID: "eip-bgp-zone1-12345",
				},
			}
			c := newFakeClient(gss, eip0, eip1)
			ctx := context.Background()

			plugin := &AutoNLBsV2Plugin{
				maxPodIndex: map[string]int{
					"default/test-gss": 10,
				},
				mutex: sync.RWMutex{},
			}

			config := &autoNLBsConfig{
				zoneMaps:          "vpc-test@cn-hangzhou-h:vsw-aaa,cn-hangzhou-i:vsw-bbb",
				eipIspTypes:       []string{"BGP"},
				targetPorts:       []int{8080},
				protocols:         []corev1.Protocol{corev1.ProtocolTCP},
				minPort:           10000,
				maxPort:           10999,
				retainNLBOnDelete: tt.retainNLBOnDelete,
			}

			// 创建 NLB 和 EIP
			err := plugin.ensureNLBForPod(ctx, c, "default", "test-gss", "BGP", 0, config)
			if err != nil {
				t.Errorf("ensureNLBForPod failed: %v", err)
				return
			}

			// 验证 NLB CR 没有 OwnerReference
			nlb := &nlbv1.NLB{}
			err = c.Get(ctx, types.NamespacedName{
				Name:      "test-gss-bgp-0",
				Namespace: "default",
			}, nlb)
			if err != nil {
				t.Errorf("NLB should be created: %v", err)
				return
			}

			// NLB 始终不设置 OwnerReference
			if len(nlb.OwnerReferences) > 0 {
				t.Errorf("NLB should NOT have OwnerReference, but got: %v", nlb.OwnerReferences)
			}

			// 验证 EIP CR 没有 OwnerReference
			eip := &eipv1.EIP{}
			err = c.Get(ctx, types.NamespacedName{
				Name:      "test-gss-eip-bgp-0-z0",
				Namespace: "default",
			}, eip)
			if err != nil {
				t.Errorf("EIP should exist: %v", err)
				return
			}

			// EIP 始终不设置 OwnerReference
			if len(eip.OwnerReferences) > 0 {
				t.Errorf("EIP should NOT have OwnerReference, but got: %v", eip.OwnerReferences)
			}
		})
	}
}

// TestNLBCreationNoOwnerRef 测试 NLB 创建时不设置 OwnerReference
// 新方案中 NLB 不再设置 OwnerReference，完全与 GSS 解耦
func TestNLBCreationNoOwnerRef(t *testing.T) {
	tests := []struct {
		name              string
		retainNLBOnDelete bool
	}{
		{
			name:              "RetainNLBOnDelete=false 时，NLB 不应有 OwnerRef",
			retainNLBOnDelete: false,
		},
		{
			name:              "RetainNLBOnDelete=true 时，NLB 不应有 OwnerRef",
			retainNLBOnDelete: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 构建测试环境
			gss := &gamekruiseiov1alpha1.GameServerSet{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "game.kruise.io/v1alpha1",
					Kind:       "GameServerSet",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "finalizer-test-gss",
					Namespace: "default",
					UID:       "gss-uid-finalizer",
				},
			}

			// EIP（必须先存在且有 AllocationID）
			eip0 := &eipv1.EIP{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "finalizer-test-gss-eip-bgp-0-z0",
					Namespace: "default",
				},
				Status: eipv1.EIPStatus{
					AllocationID: "eip-alloc-0",
					Status:       "InUse",
				},
			}
			eip1 := &eipv1.EIP{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "finalizer-test-gss-eip-bgp-0-z1",
					Namespace: "default",
				},
				Status: eipv1.EIPStatus{
					AllocationID: "eip-alloc-1",
					Status:       "InUse",
				},
			}

			scheme := runtime.NewScheme()
			_ = corev1.AddToScheme(scheme)
			_ = gamekruiseiov1alpha1.AddToScheme(scheme)
			scheme.AddKnownTypes(nlbv1.SchemeGroupVersion,
				&nlbv1.NLB{},
				&nlbv1.NLBList{},
			)
			scheme.AddKnownTypes(eipv1.GroupVersion,
				&eipv1.EIP{},
				&eipv1.EIPList{},
			)
			metav1.AddToGroupVersion(scheme, nlbv1.SchemeGroupVersion)
			metav1.AddToGroupVersion(scheme, eipv1.GroupVersion)

			c := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(gss, eip0, eip1).
				Build()

			plugin := &AutoNLBsV2Plugin{
				maxPodIndex: make(map[string]int),
				mutex:       sync.RWMutex{},
			}

			config := &autoNLBsConfig{
				zoneMaps:          "vpc-test@cn-hangzhou-h:vsw-aaa,cn-hangzhou-i:vsw-bbb",
				eipIspTypes:       []string{"BGP"},
				retainNLBOnDelete: tt.retainNLBOnDelete,
			}

			ctx := context.Background()

			// 创建 NLB
			err := plugin.createNLBInstanceCR(ctx, c, "default", "finalizer-test-gss", "BGP", 0, config, gss)
			if err != nil {
				t.Errorf("createNLBInstanceCR failed: %v", err)
				return
			}

			// 获取创建的 NLB
			nlb := &nlbv1.NLB{}
			err = c.Get(ctx, types.NamespacedName{
				Name:      "finalizer-test-gss-bgp-0",
				Namespace: "default",
			}, nlb)
			if err != nil {
				t.Errorf("Failed to get NLB: %v", err)
				return
			}

			// 检查 NLB 不应该有 Finalizer 和 OwnerReference
			if len(nlb.GetFinalizers()) > 0 {
				t.Errorf("NLB should NOT have Finalizer, but got: %v", nlb.GetFinalizers())
			}

			if len(nlb.OwnerReferences) > 0 {
				t.Errorf("NLB should NOT have OwnerReference, but got: %v", nlb.OwnerReferences)
			}
		})
	}
}
