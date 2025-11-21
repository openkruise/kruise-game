/*
Copyright 2024 The Kruise Authors.

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
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/go-logr/logr"
	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/cloudprovider"
	cperrors "github.com/openkruise/kruise-game/cloudprovider/errors"
	provideroptions "github.com/openkruise/kruise-game/cloudprovider/options"
	"github.com/openkruise/kruise-game/cloudprovider/utils"
	"github.com/openkruise/kruise-game/pkg/logging"
	"github.com/openkruise/kruise-game/pkg/telemetryfields"
	"github.com/openkruise/kruise-game/pkg/tracing"
	"github.com/openkruise/kruise-game/pkg/util"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	NlbNetwork = "AlibabaCloud-NLB"
	AliasNLB   = "NLB-Network"

	// annotations provided by AlibabaCloud Cloud Controller Manager
	LBHealthCheckFlagAnnotationKey           = "service.beta.kubernetes.io/alibaba-cloud-loadbalancer-health-check-flag"
	LBHealthCheckTypeAnnotationKey           = "service.beta.kubernetes.io/alibaba-cloud-loadbalancer-health-check-type"
	LBHealthCheckConnectPortAnnotationKey    = "service.beta.kubernetes.io/alibaba-cloud-loadbalancer-health-check-connect-port"
	LBHealthCheckConnectTimeoutAnnotationKey = "service.beta.kubernetes.io/alibaba-cloud-loadbalancer-health-check-connect-timeout"
	LBHealthyThresholdAnnotationKey          = "service.beta.kubernetes.io/alibaba-cloud-loadbalancer-healthy-threshold"
	LBUnhealthyThresholdAnnotationKey        = "service.beta.kubernetes.io/alibaba-cloud-loadbalancer-unhealthy-threshold"
	LBHealthCheckIntervalAnnotationKey       = "service.beta.kubernetes.io/alibaba-cloud-loadbalancer-health-check-interval"
	LBHealthCheckUriAnnotationKey            = "service.beta.kubernetes.io/alibaba-cloud-loadbalancer-health-check-uri"
	LBHealthCheckDomainAnnotationKey         = "service.beta.kubernetes.io/alibaba-cloud-loadbalancer-health-check-domain"
	LBHealthCheckMethodAnnotationKey         = "service.beta.kubernetes.io/alibaba-cloud-loadbalancer-health-check-method"

	// ConfigNames defined by OKG
	LBHealthCheckFlagConfigName           = "LBHealthCheckFlag"
	LBHealthCheckTypeConfigName           = "LBHealthCheckType"
	LBHealthCheckConnectPortConfigName    = "LBHealthCheckConnectPort"
	LBHealthCheckConnectTimeoutConfigName = "LBHealthCheckConnectTimeout"
	LBHealthCheckIntervalConfigName       = "LBHealthCheckInterval"
	LBHealthCheckUriConfigName            = "LBHealthCheckUri"
	LBHealthCheckDomainConfigName         = "LBHealthCheckDomain"
	LBHealthCheckMethodConfigName         = "LBHealthCheckMethod"
	LBHealthyThresholdConfigName          = "LBHealthyThreshold"
	LBUnhealthyThresholdConfigName        = "LBUnhealthyThreshold"

	nlbComponentName = "okg-controller-manager"
	nlbPluginSlug    = telemetryfields.NetworkPluginAlibabaCloudNLB
)

var (
	nlbAttrLBIDsKey               = attribute.Key("game.kruise.io.network.plugin.alibabacloud.nlb.lb_ids")
	nlbAttrLBIDKey                = attribute.Key("game.kruise.io.network.plugin.alibabacloud.nlb.lb_id")
	nlbAttrPortCountKey           = attribute.Key("game.kruise.io.network.plugin.alibabacloud.nlb.port_count")
	nlbAttrAllocatedPortsKey      = attribute.Key("game.kruise.io.network.plugin.alibabacloud.nlb.allocated_ports")
	nlbAttrAllocatedCountKey      = attribute.Key("game.kruise.io.network.plugin.alibabacloud.nlb.allocated_count")
	nlbAttrRequestedPortCountKey  = attribute.Key("game.kruise.io.network.plugin.alibabacloud.nlb.requested_port_count")
	nlbAttrPodKey                 = attribute.Key("game.kruise.io.network.plugin.alibabacloud.nlb.pod_key")
	nlbAttrServiceActionKey       = attribute.Key("game.kruise.io.network.plugin.alibabacloud.nlb.service_action")
	nlbAttrServiceNameKey         = attribute.Key("game.kruise.io.network.plugin.alibabacloud.nlb.service_name")
	nlbAttrServiceNamespaceKey    = attribute.Key("game.kruise.io.network.plugin.alibabacloud.nlb.service_namespace")
	nlbAttrServiceTypeKey         = attribute.Key("game.kruise.io.network.plugin.alibabacloud.nlb.service_type")
	nlbAttrIngressIPKey           = attribute.Key("game.kruise.io.network.plugin.alibabacloud.nlb.ingress_ip")
	nlbAttrIngressHostnameKey     = attribute.Key("game.kruise.io.network.plugin.alibabacloud.nlb.ingress_hostname")
	nlbAttrDeallocatedKeysKey     = attribute.Key("game.kruise.io.network.plugin.alibabacloud.nlb.deallocated_keys")
	nlbAttrDeallocatedCntKey      = attribute.Key("game.kruise.io.network.plugin.alibabacloud.nlb.deallocated_keys_count")
	nlbAttrHealthCheckFlagKey     = attribute.Key("game.kruise.io.network.plugin.alibabacloud.nlb.health_check.flag")
	nlbAttrHealthCheckTypeKey     = attribute.Key("game.kruise.io.network.plugin.alibabacloud.nlb.health_check.type")
	nlbAttrHealthCheckPortKey     = attribute.Key("game.kruise.io.network.plugin.alibabacloud.nlb.health_check.connect_port")
	nlbAttrHealthCheckTimeoutKey  = attribute.Key("game.kruise.io.network.plugin.alibabacloud.nlb.health_check.connect_timeout")
	nlbAttrHealthCheckIntervalKey = attribute.Key("game.kruise.io.network.plugin.alibabacloud.nlb.health_check.interval")
	nlbAttrHealthCheckURIKey      = attribute.Key("game.kruise.io.network.plugin.alibabacloud.nlb.health_check.uri")
	nlbAttrHealthCheckDomainKey   = attribute.Key("game.kruise.io.network.plugin.alibabacloud.nlb.health_check.domain")
	nlbAttrHealthCheckMethodKey   = attribute.Key("game.kruise.io.network.plugin.alibabacloud.nlb.health_check.method")
	nlbAttrHealthyThresholdKey    = attribute.Key("game.kruise.io.network.plugin.alibabacloud.nlb.health_check.healthy_threshold")
	nlbAttrUnhealthyThresholdKey  = attribute.Key("game.kruise.io.network.plugin.alibabacloud.nlb.health_check.unhealthy_threshold")
)

type NlbPlugin struct {
	maxPort     int32
	minPort     int32
	blockPorts  []int32
	cache       map[string]portAllocated
	podAllocate map[string]string
	mutex       sync.RWMutex
}

type nlbConfig struct {
	lbIds       []string
	targetPorts []int
	protocols   []corev1.Protocol
	isFixed     bool
	*nlbHealthConfig
}

type nlbHealthConfig struct {
	lBHealthCheckFlag           string
	lBHealthCheckType           string
	lBHealthCheckConnectPort    string
	lBHealthCheckConnectTimeout string
	lBHealthCheckInterval       string
	lBHealthCheckUri            string
	lBHealthCheckDomain         string
	lBHealthCheckMethod         string
	lBHealthyThreshold          string
	lBUnhealthyThreshold        string
}

func (n *NlbPlugin) Name() string {
	return NlbNetwork
}

func (n *NlbPlugin) Alias() string {
	return AliasNLB
}

func (n *NlbPlugin) Init(c client.Client, options cloudprovider.CloudProviderOptions, ctx context.Context) error {
	n.mutex.Lock()
	defer n.mutex.Unlock()
	slbOptions := options.(provideroptions.AlibabaCloudOptions).NLBOptions
	n.minPort = slbOptions.MinPort
	n.maxPort = slbOptions.MaxPort
	n.blockPorts = slbOptions.BlockPorts

	svcList := &corev1.ServiceList{}
	err := c.List(ctx, svcList)
	if err != nil {
		return err
	}

	n.cache, n.podAllocate = initLbCache(svcList.Items, n.minPort, n.maxPort, n.blockPorts)
	logger := nlbLogger(ctx, nil).WithValues(
		telemetryfields.FieldOperation, "init",
	)
	logger.Info("podAllocate cache initialized",
		telemetryfields.FieldPodAllocate, n.podAllocate,
		telemetryfields.FieldMinPort, n.minPort,
		telemetryfields.FieldMaxPort, n.maxPort,
		telemetryfields.FieldBlockPorts, n.blockPorts,
	)
	return nil
}

func (n *NlbPlugin) OnPodAdded(client client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	return pod, nil
}

func (n *NlbPlugin) OnPodUpdated(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	tracer := otel.Tracer("okg-controller-manager")
	ctx, span := startNLBSpan(ctx, tracer, tracing.SpanProcessNLBPod, pod)
	defer span.End()
	span.SetAttributes(tracing.AttrNetworkStatus(telemetryfields.NetworkStatusWaiting))
	logger := nlbLogger(ctx, pod).WithValues(telemetryfields.FieldOperation, "update")
	logger.Info("processing NLB pod update")

	networkManager := utils.NewNetworkManager(pod, c)

	networkStatus, _ := networkManager.GetNetworkStatus()
	networkConfig := networkManager.GetNetworkConfig()
	sc, err := parseNlbConfig(networkConfig)
	if err != nil {
		logger.Error(err, "failed to parse NLB config")
		span.RecordError(err)
		span.SetAttributes(
			tracing.AttrNetworkStatus(telemetryfields.NetworkStatusError),
			tracing.AttrErrorType(telemetryfields.ErrorTypeParameter),
		)
		span.SetStatus(codes.Error, "failed to parse NLB config")
		return pod, cperrors.NewPluginError(cperrors.ParameterError, err.Error())
	}
	span.SetAttributes(
		nlbAttrLBIDsKey.StringSlice(sc.lbIds),
		nlbAttrPortCountKey.Int64(int64(len(sc.targetPorts))),
		nlbAttrServiceNameKey.String(pod.GetName()),
		nlbAttrServiceNamespaceKey.String(pod.GetNamespace()),
	)
	if networkStatus == nil {
		logger.Info("network status missing; marking pod not ready")
		span.SetAttributes(tracing.AttrNetworkStatus(telemetryfields.NetworkStatusNotReady))
		pod, err := networkManager.UpdateNetworkStatus(gamekruiseiov1alpha1.NetworkStatus{
			CurrentNetworkState: gamekruiseiov1alpha1.NetworkNotReady,
		}, pod)
		return pod, cperrors.ToPluginError(err, cperrors.InternalError)
	}

	svc := &corev1.Service{}
	err = c.Get(ctx, types.NamespacedName{
		Name:      pod.GetName(),
		Namespace: pod.GetNamespace(),
	}, svc)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("creating NLB service for pod")
			_, createSpan := startNLBSpan(ctx, tracer, tracing.SpanCreateNLBService, pod,
				tracing.AttrNetworkStatus(telemetryfields.NetworkStatusNotReady),
				nlbAttrPortCountKey.Int64(int64(len(sc.targetPorts))),
				nlbAttrLBIDsKey.StringSlice(sc.lbIds),
				nlbAttrServiceNameKey.String(pod.GetName()),
				nlbAttrServiceNamespaceKey.String(pod.GetNamespace()),
			)
			defer createSpan.End()
			service, err := n.consSvc(sc, pod, c, ctx)
			if err != nil {
				createSpan.RecordError(err)
				createSpan.SetAttributes(tracing.AttrErrorType(telemetryfields.ErrorTypeParameter))
				createSpan.SetStatus(codes.Error, "failed to build nlb service")
				return pod, cperrors.ToPluginError(err, cperrors.ParameterError)
			}
			if err := c.Create(ctx, service); err != nil {
				logger.Error(err, "failed to create NLB service")
				createSpan.RecordError(err)
				createSpan.SetAttributes(tracing.AttrErrorType(telemetryfields.ErrorTypeAPICall))
				createSpan.SetStatus(codes.Error, "failed to create nlb service")
				return pod, cperrors.ToPluginError(err, cperrors.ApiCallError)
			}
			lbID := service.Annotations[SlbIdAnnotationKey]
			createSpan.SetAttributes(
				tracing.AttrNetworkResourceID(lbID),
				nlbAttrLBIDKey.String(lbID),
				nlbAttrPortCountKey.Int64(int64(len(service.Spec.Ports))),
				nlbAttrAllocatedPortsKey.String(formatNLBServicePorts(service.Spec.Ports)),
				nlbAttrServiceTypeKey.String(string(service.Spec.Type)),
			)
			createSpan.SetStatus(codes.Ok, "nlb service created")
			span.SetAttributes(tracing.AttrNetworkStatus(telemetryfields.NetworkStatusNotReady))
			return pod, nil
		}
		logger.Error(err, "failed to get NLB service")
		span.RecordError(err)
		span.SetAttributes(
			tracing.AttrNetworkStatus(telemetryfields.NetworkStatusError),
			tracing.AttrErrorType(telemetryfields.ErrorTypeAPICall),
		)
		span.SetStatus(codes.Error, "failed to get service")
		return pod, cperrors.NewPluginError(cperrors.ApiCallError, err.Error())
	}

	if svc.OwnerReferences[0].Kind == "Pod" && svc.OwnerReferences[0].UID != pod.UID {
		logger.Info("waiting for previous Service owner cleanup",
			telemetryfields.FieldServiceNamespace, svc.Namespace,
			telemetryfields.FieldServiceName, svc.Name,
			telemetryfields.FieldOldUID, svc.OwnerReferences[0].UID,
			telemetryfields.FieldNewUID, pod.UID,
		)
		return pod, nil
	}

	if util.GetHash(sc) != svc.GetAnnotations()[SlbConfigHashKey] {
		logger.Info("detected Service config drift; reconciling",
			telemetryfields.FieldCurrentHash, svc.GetAnnotations()[SlbConfigHashKey],
			telemetryfields.FieldDesiredPorts, len(sc.targetPorts),
		)
		networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkNotReady
		pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
		if err != nil {
			return pod, cperrors.NewPluginError(cperrors.InternalError, err.Error())
		}
		_, updateSpan := startNLBSpan(ctx, tracer, tracing.SpanReconcileNLBService, pod,
			tracing.AttrNetworkStatus(telemetryfields.NetworkStatusNotReady),
			nlbAttrPortCountKey.Int64(int64(len(sc.targetPorts))),
			nlbAttrServiceNameKey.String(pod.GetName()),
			nlbAttrServiceNamespaceKey.String(pod.GetNamespace()),
		)
		defer updateSpan.End()
		service, err := n.consSvc(sc, pod, c, ctx)
		if err != nil {
			logger.Error(err, "failed to build Service for reconciliation")
			updateSpan.RecordError(err)
			updateSpan.SetAttributes(tracing.AttrErrorType(telemetryfields.ErrorTypeParameter))
			updateSpan.SetStatus(codes.Error, "failed to build nlb service")
			return pod, cperrors.ToPluginError(err, cperrors.ParameterError)
		}
		if err := c.Update(ctx, service); err != nil {
			logger.Error(err, "failed to update NLB service during reconciliation")
			updateSpan.RecordError(err)
			updateSpan.SetAttributes(tracing.AttrErrorType(telemetryfields.ErrorTypeAPICall))
			updateSpan.SetStatus(codes.Error, "failed to update nlb service")
			return pod, cperrors.ToPluginError(err, cperrors.ApiCallError)
		}
		lbID := service.Annotations[SlbIdAnnotationKey]
		updateSpan.SetAttributes(
			tracing.AttrNetworkResourceID(lbID),
			nlbAttrLBIDKey.String(lbID),
			nlbAttrPortCountKey.Int64(int64(len(service.Spec.Ports))),
			nlbAttrAllocatedPortsKey.String(formatNLBServicePorts(service.Spec.Ports)),
			nlbAttrServiceTypeKey.String(string(service.Spec.Type)),
		)
		updateSpan.SetStatus(codes.Ok, "nlb service updated")
		logger.Info("reconciled NLB service",
			telemetryfields.FieldLBID, lbID,
			telemetryfields.FieldPortCount, len(service.Spec.Ports),
		)
		span.SetAttributes(tracing.AttrNetworkStatus(telemetryfields.NetworkStatusNotReady))
		return pod, nil
	}

	if networkManager.GetNetworkDisabled() && svc.Spec.Type == corev1.ServiceTypeLoadBalancer {
		logger.Info("disabling NLB service due to network disable flag")
		_, toggleSpan := startNLBSpan(ctx, tracer, tracing.SpanToggleNLBServiceType, pod,
			tracing.AttrNetworkStatus(telemetryfields.NetworkStatusNotReady),
			nlbAttrServiceActionKey.String("disable"),
			nlbAttrServiceNameKey.String(svc.GetName()),
			nlbAttrServiceNamespaceKey.String(svc.GetNamespace()),
			nlbAttrServiceTypeKey.String(string(svc.Spec.Type)),
		)
		defer toggleSpan.End()
		svc.Spec.Type = corev1.ServiceTypeClusterIP
		err = c.Update(ctx, svc)
		if err != nil {
			logger.Error(err, "failed to disable NLB service")
			toggleSpan.RecordError(err)
			toggleSpan.SetAttributes(tracing.AttrErrorType(telemetryfields.ErrorTypeAPICall))
			toggleSpan.SetStatus(codes.Error, "failed to disable nlb service")
		} else {
			toggleSpan.SetStatus(codes.Ok, "nlb service disabled")
		}
		return pod, cperrors.ToPluginError(err, cperrors.ApiCallError)
	}

	if !networkManager.GetNetworkDisabled() && svc.Spec.Type == corev1.ServiceTypeClusterIP {
		logger.Info("re-enabling NLB service after network resume")
		_, toggleSpan := startNLBSpan(ctx, tracer, tracing.SpanToggleNLBServiceType, pod,
			tracing.AttrNetworkStatus(telemetryfields.NetworkStatusWaiting),
			nlbAttrServiceActionKey.String("enable"),
			nlbAttrServiceNameKey.String(svc.GetName()),
			nlbAttrServiceNamespaceKey.String(svc.GetNamespace()),
			nlbAttrServiceTypeKey.String(string(corev1.ServiceTypeLoadBalancer)),
		)
		defer toggleSpan.End()
		svc.Spec.Type = corev1.ServiceTypeLoadBalancer
		err = c.Update(ctx, svc)
		if err != nil {
			logger.Error(err, "failed to enable NLB service")
			toggleSpan.RecordError(err)
			toggleSpan.SetAttributes(tracing.AttrErrorType(telemetryfields.ErrorTypeAPICall))
			toggleSpan.SetStatus(codes.Error, "failed to enable nlb service")
		} else {
			toggleSpan.SetStatus(codes.Ok, "nlb service enabled")
		}
		return pod, cperrors.ToPluginError(err, cperrors.ApiCallError)
	}

	lbSpanAttrs := []attribute.KeyValue{
		tracing.AttrNetworkStatus(telemetryfields.NetworkStatusWaiting),
		nlbAttrServiceNameKey.String(svc.GetName()),
		nlbAttrServiceNamespaceKey.String(svc.GetNamespace()),
		nlbAttrServiceTypeKey.String(string(svc.Spec.Type)),
		nlbAttrLBIDKey.String(svc.Annotations[SlbIdAnnotationKey]),
		tracing.AttrNetworkResourceID(svc.Annotations[SlbIdAnnotationKey]),
	}
	lbSpanAttrs = append(lbSpanAttrs, nlbHealthCheckAttrs(sc.nlbHealthConfig)...)
	_, lbSpan := startNLBSpan(ctx, tracer, tracing.SpanCheckNLBStatus, pod, lbSpanAttrs...)
	if svc.Status.LoadBalancer.Ingress == nil {
		logger.Info("load balancer ingress not yet assigned")
		lbSpan.SetAttributes(
			tracing.AttrNetworkStatus(telemetryfields.NetworkStatusNotReady),
			tracing.AttrErrorType(telemetryfields.ErrorTypeResourceNotReady),
		)
		lbSpan.SetStatus(codes.Error, "LoadBalancer ingress not yet assigned")
		lbSpan.End()
		span.SetAttributes(tracing.AttrNetworkStatus(telemetryfields.NetworkStatusNotReady))

		networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkNotReady
		pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
		return pod, cperrors.ToPluginError(err, cperrors.InternalError)
	}

	lbSpan.SetAttributes(
		tracing.AttrNetworkStatus(telemetryfields.NetworkStatusReady),
		nlbAttrIngressIPKey.String(svc.Status.LoadBalancer.Ingress[0].IP),
		nlbAttrIngressHostnameKey.String(svc.Status.LoadBalancer.Ingress[0].Hostname),
	)
	lbSpan.SetStatus(codes.Ok, "LoadBalancer ready")
	lbSpan.End()

	if util.IsAllowNotReadyContainers(networkManager.GetNetworkConfig()) {
		toUpDateSvc, err := utils.AllowNotReadyContainers(c, ctx, pod, svc, false)
		if err != nil {
			logger.Error(err, "failed to adjust allow-not-ready containers on Service")
			return pod, err
		}

		if toUpDateSvc {
			logger.Info("updating Service to include not-ready endpoints")
			err := c.Update(ctx, svc)
			if err != nil {
				logger.Error(err, "failed to update Service for allow-not-ready containers")
				return pod, cperrors.ToPluginError(err, cperrors.ApiCallError)
			}
		}
	}

	internalAddresses := make([]gamekruiseiov1alpha1.NetworkAddress, 0)
	externalAddresses := make([]gamekruiseiov1alpha1.NetworkAddress, 0)
	for _, port := range svc.Spec.Ports {
		instrIPort := port.TargetPort
		instrEPort := intstr.FromInt(int(port.Port))
		internalAddress := gamekruiseiov1alpha1.NetworkAddress{
			IP: pod.Status.PodIP,
			Ports: []gamekruiseiov1alpha1.NetworkPort{
				{
					Name:     instrIPort.String(),
					Port:     &instrIPort,
					Protocol: port.Protocol,
				},
			},
		}
		externalAddress := gamekruiseiov1alpha1.NetworkAddress{
			EndPoint: svc.Status.LoadBalancer.Ingress[0].Hostname,
			IP:       svc.Status.LoadBalancer.Ingress[0].IP,
			Ports: []gamekruiseiov1alpha1.NetworkPort{
				{
					Name:     instrIPort.String(),
					Port:     &instrEPort,
					Protocol: port.Protocol,
				},
			},
		}
		internalAddresses = append(internalAddresses, internalAddress)
		externalAddresses = append(externalAddresses, externalAddress)
	}
	networkStatus.InternalAddresses = internalAddresses
	networkStatus.ExternalAddresses = externalAddresses
	networkStatus.CurrentNetworkState = gamekruiseiov1alpha1.NetworkReady

	_, publishSpan := startNLBSpan(ctx, tracer, tracing.SpanPublishNLBStatus, pod,
		tracing.AttrNetworkStatus(telemetryfields.NetworkStatusReady),
		attribute.Int(telemetryfields.FieldInternalAddresses, len(internalAddresses)),
		attribute.Int(telemetryfields.FieldExternalAddresses, len(externalAddresses)),
		nlbAttrPortCountKey.Int64(int64(len(svc.Spec.Ports))),
		nlbAttrAllocatedPortsKey.String(formatNLBServicePorts(svc.Spec.Ports)),
	)
	publishSpan.SetStatus(codes.Ok, "nlb addresses published")
	publishSpan.End()

	pod, err = networkManager.UpdateNetworkStatus(*networkStatus, pod)
	if err != nil {
		logger.Error(err, "failed to publish NLB network status to pod")
		span.RecordError(err)
		span.SetAttributes(
			tracing.AttrNetworkStatus(telemetryfields.NetworkStatusError),
			tracing.AttrErrorType(telemetryfields.ErrorTypeInternal),
		)
		span.SetStatus(codes.Error, "failed to update network status")
		return pod, cperrors.ToPluginError(err, cperrors.InternalError)
	}

	span.SetAttributes(tracing.AttrNetworkStatus(telemetryfields.NetworkStatusReady))
	span.SetStatus(codes.Ok, "nlb pod processed")
	logger.Info("NLB pod update complete", telemetryfields.FieldState, networkStatus.CurrentNetworkState)
	return pod, nil
}

func (n *NlbPlugin) OnPodDeleted(c client.Client, pod *corev1.Pod, ctx context.Context) cperrors.PluginError {
	tracer := otel.Tracer("okg-controller-manager")
	ctx, span := startNLBSpan(ctx, tracer, tracing.SpanCleanupNLBAllocation, pod,
		tracing.AttrNetworkStatus(telemetryfields.NetworkStatusNotReady),
	)
	defer span.End()
	logger := nlbLogger(ctx, pod).WithValues(telemetryfields.FieldOperation, "delete")

	networkManager := utils.NewNetworkManager(pod, c)
	networkConfig := networkManager.GetNetworkConfig()
	sc, err := parseNlbConfig(networkConfig)
	if err != nil {
		logger.Error(err, "failed to parse NLB config during cleanup")
		span.RecordError(err)
		span.SetAttributes(tracing.AttrErrorType(telemetryfields.ErrorTypeParameter))
		span.SetStatus(codes.Error, "failed to parse NLB config")
		return cperrors.NewPluginError(cperrors.ApiCallError, err.Error())
	}

	var podKeys []string
	if sc.isFixed {
		gss, err := util.GetGameServerSetOfPod(pod, c, ctx)
		if err != nil && !errors.IsNotFound(err) {
			logger.Error(err, "failed to fetch GameServerSet during cleanup")
			span.RecordError(err)
			span.SetAttributes(tracing.AttrErrorType(telemetryfields.ErrorTypeAPICall))
			span.SetStatus(codes.Error, "failed to fetch GameServerSet")
			return cperrors.ToPluginError(err, cperrors.ApiCallError)
		}
		if err == nil && gss.GetDeletionTimestamp() == nil {
			logger.Info("GameServerSet still active; skipping NLB cleanup")
			span.SetStatus(codes.Ok, "gss still alive, skip cleanup")
			return nil
		}
		for key := range n.podAllocate {
			gssName := pod.GetLabels()[gamekruiseiov1alpha1.GameServerOwnerGssKey]
			if strings.Contains(key, pod.GetNamespace()+"/"+gssName) {
				podKeys = append(podKeys, key)
			}
		}
	} else {
		podKeys = append(podKeys, pod.GetNamespace()+"/"+pod.GetName())
	}

	for _, podKey := range podKeys {
		logger.Info("deallocating NLB ports for pod key", telemetryfields.FieldPodKeyQualified, podKey)
		n.deAllocate(podKey)
	}

	span.SetAttributes(
		nlbAttrDeallocatedCntKey.Int64(int64(len(podKeys))),
		nlbAttrDeallocatedKeysKey.StringSlice(podKeys),
	)
	logger.Info("cleaned up NLB allocations", telemetryfields.FieldCount, len(podKeys), telemetryfields.FieldKeys, podKeys)
	span.SetStatus(codes.Ok, "nlb allocation cleaned up")
	return nil
}

func init() {
	nlbPlugin := NlbPlugin{
		mutex: sync.RWMutex{},
	}
	alibabaCloudProvider.registerPlugin(&nlbPlugin)
}

func (n *NlbPlugin) consSvc(nc *nlbConfig, pod *corev1.Pod, c client.Client, ctx context.Context) (*corev1.Service, error) {
	var ports []int32
	var lbId string
	podKey := pod.GetNamespace() + "/" + pod.GetName()
	allocatedPorts, exist := n.podAllocate[podKey]
	if exist {
		slbPorts := strings.Split(allocatedPorts, ":")
		lbId = slbPorts[0]
		ports = util.StringToInt32Slice(slbPorts[1], ",")
	} else {
		lbId, ports = n.allocate(ctx, pod, nc.lbIds, len(nc.targetPorts))
		if lbId == "" && ports == nil {
			return nil, fmt.Errorf("there are no avaialable ports for %v", nc.lbIds)
		}
	}

	svcPorts := make([]corev1.ServicePort, 0)
	for i := 0; i < len(nc.targetPorts); i++ {
		svcPorts = append(svcPorts, corev1.ServicePort{
			Name:       strconv.Itoa(nc.targetPorts[i]),
			Port:       ports[i],
			Protocol:   nc.protocols[i],
			TargetPort: intstr.FromInt(nc.targetPorts[i]),
		})
	}

	loadBalancerClass := "alibabacloud.com/nlb"

	svcAnnotations := map[string]string{
		SlbListenerOverrideKey:         "true",
		SlbIdAnnotationKey:             lbId,
		SlbConfigHashKey:               util.GetHash(nc),
		LBHealthCheckFlagAnnotationKey: nc.lBHealthCheckFlag,
	}
	if nc.lBHealthCheckFlag == "on" {
		svcAnnotations[LBHealthCheckTypeAnnotationKey] = nc.lBHealthCheckType
		svcAnnotations[LBHealthCheckConnectPortAnnotationKey] = nc.lBHealthCheckConnectPort
		svcAnnotations[LBHealthCheckConnectTimeoutAnnotationKey] = nc.lBHealthCheckConnectTimeout
		svcAnnotations[LBHealthCheckIntervalAnnotationKey] = nc.lBHealthCheckInterval
		svcAnnotations[LBHealthyThresholdAnnotationKey] = nc.lBHealthyThreshold
		svcAnnotations[LBUnhealthyThresholdAnnotationKey] = nc.lBUnhealthyThreshold
		if nc.lBHealthCheckType == "http" {
			svcAnnotations[LBHealthCheckDomainAnnotationKey] = nc.lBHealthCheckDomain
			svcAnnotations[LBHealthCheckUriAnnotationKey] = nc.lBHealthCheckUri
			svcAnnotations[LBHealthCheckMethodAnnotationKey] = nc.lBHealthCheckMethod
		}
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        pod.GetName(),
			Namespace:   pod.GetNamespace(),
			Annotations: svcAnnotations,
			Labels: map[string]string{
				ServiceProxyName: "dummy",
			},
			OwnerReferences: getSvcOwnerReference(c, ctx, pod, nc.isFixed),
		},
		Spec: corev1.ServiceSpec{
			ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyTypeLocal,
			Type:                  corev1.ServiceTypeLoadBalancer,
			Selector: map[string]string{
				SvcSelectorKey: pod.GetName(),
			},
			Ports:             svcPorts,
			LoadBalancerClass: &loadBalancerClass,
		},
	}

	// Record success
	return svc, nil
}

func formatNLBServicePorts(ports []corev1.ServicePort) string {
	type portSnapshot struct {
		Name       string `json:"name,omitempty"`
		Port       int32  `json:"port"`
		TargetPort string `json:"targetPort"`
		Protocol   string `json:"protocol"`
	}
	snapshot := make([]portSnapshot, 0, len(ports))
	for _, p := range ports {
		snapshot = append(snapshot, portSnapshot{
			Name:       p.Name,
			Port:       p.Port,
			TargetPort: p.TargetPort.String(),
			Protocol:   string(p.Protocol),
		})
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return ""
	}
	return string(data)
}

func (n *NlbPlugin) allocate(ctx context.Context, pod *corev1.Pod, lbIds []string, num int) (string, []int32) {
	tracer := otel.Tracer("okg-controller-manager")
	podKey := "unknown"
	if pod != nil {
		podKey = pod.GetNamespace() + "/" + pod.GetName()
	}
	logger := nlbLogger(ctx, pod).WithValues(
		telemetryfields.FieldOperation, "allocate",
		telemetryfields.FieldPodKeyQualified, podKey,
	)
	_, span := startNLBSpan(ctx, tracer, tracing.SpanAllocateNLBPorts, pod,
		tracing.AttrNetworkStatus(telemetryfields.NetworkStatusWaiting),
		nlbAttrLBIDsKey.StringSlice(lbIds),
		nlbAttrRequestedPortCountKey.Int64(int64(num)),
		nlbAttrPodKey.String(podKey),
	)
	defer span.End()

	n.mutex.Lock()
	defer n.mutex.Unlock()

	var ports []int32
	var lbId string

	// find lb with adequate ports
	for _, slbId := range lbIds {
		sum := 0
		for i := n.minPort; i <= n.maxPort; i++ {
			if !n.cache[slbId][i] {
				sum++
			}
			if sum >= num {
				lbId = slbId
				break
			}
		}
	}
	if lbId == "" {
		err := fmt.Errorf("no available ports found")
		span.RecordError(err)
		span.SetAttributes(
			tracing.AttrNetworkStatus(telemetryfields.NetworkStatusError),
			tracing.AttrErrorType(telemetryfields.ErrorTypePortExhausted),
		)
		span.SetStatus(codes.Error, err.Error())
		return "", nil
	}

	// select ports
	for i := 0; i < num; i++ {
		var port int32
		if n.cache[lbId] == nil {
			// init cache for new lb
			n.cache[lbId] = make(portAllocated, n.maxPort-n.minPort+1)
			for i := n.minPort; i <= n.maxPort; i++ {
				n.cache[lbId][i] = false
			}
			// block ports
			for _, blockPort := range n.blockPorts {
				n.cache[lbId][blockPort] = true
			}
		}

		for p, allocated := range n.cache[lbId] {
			if !allocated {
				port = p
				break
			}
		}
		n.cache[lbId][port] = true
		ports = append(ports, port)
	}

	n.podAllocate[podKey] = lbId + ":" + util.Int32SliceToString(ports, ",")
	logger.Info("allocated NLB ports",
		telemetryfields.FieldLBID, lbId,
		telemetryfields.FieldPorts, ports,
	)

	// Record successful allocation in span
	span.SetAttributes(
		tracing.AttrNetworkStatus(telemetryfields.NetworkStatusReady),
		tracing.AttrNetworkResourceID(lbId),
		nlbAttrLBIDKey.String(lbId),
		nlbAttrAllocatedPortsKey.String(util.Int32SliceToString(ports, ",")),
		nlbAttrAllocatedCountKey.Int64(int64(len(ports))),
	)
	span.SetStatus(codes.Ok, "ports allocated successfully")

	return lbId, ports
}

func (n *NlbPlugin) deAllocate(nsName string) {
	n.mutex.Lock()
	defer n.mutex.Unlock()

	allocatedPorts, exist := n.podAllocate[nsName]
	if !exist {
		return
	}

	slbPorts := strings.Split(allocatedPorts, ":")
	lbId := slbPorts[0]
	ports := util.StringToInt32Slice(slbPorts[1], ",")
	for _, port := range ports {
		n.cache[lbId][port] = false
	}
	// block ports
	for _, blockPort := range n.blockPorts {
		n.cache[lbId][blockPort] = true
	}

	delete(n.podAllocate, nsName)
}

func nlbSpanAttrs(pod *corev1.Pod, extras ...attribute.KeyValue) []attribute.KeyValue {
	attrExtras := []attribute.KeyValue{
		tracing.AttrCloudProvider(tracing.CloudProviderAlibabaCloud),
	}
	if pod != nil && pod.Spec.NodeName != "" {
		attrExtras = append(attrExtras, tracing.AttrK8sNodeName(pod.Spec.NodeName))
	}
	attrExtras = append(attrExtras, extras...)
	attrExtras = tracing.EnsureNetworkStatusAttr(attrExtras, telemetryfields.NetworkStatusWaiting)
	return tracing.BaseNetworkAttrs(nlbComponentName, nlbPluginSlug, pod, attrExtras...)
}

func nlbLogger(ctx context.Context, pod *corev1.Pod) logr.Logger {
	logger := logging.FromContextWithTrace(ctx).WithValues(
		telemetryfields.FieldComponent, "cloudprovider",
		telemetryfields.FieldNetworkPluginName, nlbPluginSlug,
		telemetryfields.FieldPluginSlug, nlbPluginSlug,
	)
	if pod != nil {
		podLabels := pod.GetLabels()
		logger = logger.WithValues(
			telemetryfields.FieldGameServerNamespace, pod.GetNamespace(),
			telemetryfields.FieldGameServerName, pod.GetName(),
		)
		if gss, ok := podLabels[gamekruiseiov1alpha1.GameServerOwnerGssKey]; ok {
			logger = logger.WithValues(
				telemetryfields.FieldGameServerSetNamespace, pod.GetNamespace(),
				telemetryfields.FieldGameServerSetName, gss,
			)
		}
	}
	return logger
}
func startNLBSpan(ctx context.Context, tracer trace.Tracer, name string, pod *corev1.Pod, extras ...attribute.KeyValue) (context.Context, trace.Span) {
	return tracer.Start(ctx, name,
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(nlbSpanAttrs(pod, extras...)...),
	)
}

func nlbHealthCheckAttrs(cfg *nlbHealthConfig) []attribute.KeyValue {
	if cfg == nil {
		return nil
	}
	attrs := []attribute.KeyValue{
		nlbAttrHealthCheckFlagKey.String(cfg.lBHealthCheckFlag),
		nlbAttrHealthCheckTypeKey.String(cfg.lBHealthCheckType),
		nlbAttrHealthCheckPortKey.String(cfg.lBHealthCheckConnectPort),
		nlbAttrHealthCheckTimeoutKey.String(cfg.lBHealthCheckConnectTimeout),
		nlbAttrHealthCheckIntervalKey.String(cfg.lBHealthCheckInterval),
		nlbAttrHealthyThresholdKey.String(cfg.lBHealthyThreshold),
		nlbAttrUnhealthyThresholdKey.String(cfg.lBUnhealthyThreshold),
	}
	if cfg.lBHealthCheckUri != "" {
		attrs = append(attrs, nlbAttrHealthCheckURIKey.String(cfg.lBHealthCheckUri))
	}
	if cfg.lBHealthCheckDomain != "" {
		attrs = append(attrs, nlbAttrHealthCheckDomainKey.String(cfg.lBHealthCheckDomain))
	}
	if cfg.lBHealthCheckMethod != "" {
		attrs = append(attrs, nlbAttrHealthCheckMethodKey.String(cfg.lBHealthCheckMethod))
	}
	return attrs
}

func parseNlbConfig(conf []gamekruiseiov1alpha1.NetworkConfParams) (*nlbConfig, error) {
	var lbIds []string
	ports := make([]int, 0)
	protocols := make([]corev1.Protocol, 0)
	isFixed := false

	for _, c := range conf {
		switch c.Name {
		case NlbIdsConfigName:
			for _, slbId := range strings.Split(c.Value, ",") {
				if slbId != "" {
					lbIds = append(lbIds, slbId)
				}
			}
		case PortProtocolsConfigName:
			for _, pp := range strings.Split(c.Value, ",") {
				ppSlice := strings.Split(pp, "/")
				port, err := strconv.Atoi(ppSlice[0])
				if err != nil {
					continue
				}
				ports = append(ports, port)
				if len(ppSlice) != 2 {
					protocols = append(protocols, corev1.ProtocolTCP)
				} else {
					protocols = append(protocols, corev1.Protocol(ppSlice[1]))
				}
			}
		case FixedConfigName:
			v, err := strconv.ParseBool(c.Value)
			if err != nil {
				continue
			}
			isFixed = v
		}
	}

	nlbHealthConfig, err := parseNlbHealthConfig(conf)
	if err != nil {
		return nil, err
	}

	return &nlbConfig{
		lbIds:           lbIds,
		protocols:       protocols,
		targetPorts:     ports,
		isFixed:         isFixed,
		nlbHealthConfig: nlbHealthConfig,
	}, nil
}

func parseNlbHealthConfig(conf []gamekruiseiov1alpha1.NetworkConfParams) (*nlbHealthConfig, error) {
	lBHealthCheckFlag := "on"
	lBHealthCheckType := "tcp"
	lBHealthCheckConnectPort := "0"
	lBHealthCheckConnectTimeout := "5"
	lBHealthCheckInterval := "10"
	lBUnhealthyThreshold := "2"
	lBHealthyThreshold := "2"
	lBHealthCheckUri := ""
	lBHealthCheckDomain := ""
	lBHealthCheckMethod := ""

	for _, c := range conf {
		switch c.Name {
		case LBHealthCheckFlagConfigName:
			flag := strings.ToLower(c.Value)
			if flag != "on" && flag != "off" {
				return nil, fmt.Errorf("invalid lb health check flag value: %s", c.Value)
			}
			lBHealthCheckFlag = flag
		case LBHealthCheckTypeConfigName:
			checkType := strings.ToLower(c.Value)
			if checkType != "tcp" && checkType != "http" {
				return nil, fmt.Errorf("invalid lb health check type: %s", c.Value)
			}
			lBHealthCheckType = checkType
		case LBHealthCheckConnectPortConfigName:
			portInt, err := strconv.Atoi(c.Value)
			if err != nil {
				return nil, fmt.Errorf("invalid lb health check connect port: %s", c.Value)
			}
			if portInt < 0 || portInt > 65535 {
				return nil, fmt.Errorf("invalid lb health check connect port: %d", portInt)
			}
			lBHealthCheckConnectPort = c.Value
		case LBHealthCheckConnectTimeoutConfigName:
			timeoutInt, err := strconv.Atoi(c.Value)
			if err != nil {
				return nil, fmt.Errorf("invalid lb health check connect timeout: %s", c.Value)
			}
			if timeoutInt < 1 || timeoutInt > 300 {
				return nil, fmt.Errorf("invalid lb health check connect timeout: %d", timeoutInt)
			}
			lBHealthCheckConnectTimeout = c.Value
		case LBHealthCheckIntervalConfigName:
			intervalInt, err := strconv.Atoi(c.Value)
			if err != nil {
				return nil, fmt.Errorf("invalid lb health check interval: %s", c.Value)
			}
			if intervalInt < 1 || intervalInt > 50 {
				return nil, fmt.Errorf("invalid lb health check interval: %d", intervalInt)
			}
			lBHealthCheckInterval = c.Value
		case LBHealthyThresholdConfigName:
			thresholdInt, err := strconv.Atoi(c.Value)
			if err != nil {
				return nil, fmt.Errorf("invalid lb healthy threshold: %s", c.Value)
			}
			if thresholdInt < 2 || thresholdInt > 10 {
				return nil, fmt.Errorf("invalid lb healthy threshold: %d", thresholdInt)
			}
			lBHealthyThreshold = c.Value
		case LBUnhealthyThresholdConfigName:
			thresholdInt, err := strconv.Atoi(c.Value)
			if err != nil {
				return nil, fmt.Errorf("invalid lb unhealthy threshold: %s", c.Value)
			}
			if thresholdInt < 2 || thresholdInt > 10 {
				return nil, fmt.Errorf("invalid lb unhealthy threshold: %d", thresholdInt)
			}
			lBUnhealthyThreshold = c.Value
		case LBHealthCheckUriConfigName:
			if validateUri(c.Value) != nil {
				return nil, fmt.Errorf("invalid lb health check uri: %s", c.Value)
			}
			lBHealthCheckUri = c.Value
		case LBHealthCheckDomainConfigName:
			if validateDomain(c.Value) != nil {
				return nil, fmt.Errorf("invalid lb health check domain: %s", c.Value)
			}
			lBHealthCheckDomain = c.Value
		case LBHealthCheckMethodConfigName:
			method := strings.ToLower(c.Value)
			if method != "get" && method != "head" {
				return nil, fmt.Errorf("invalid lb health check method: %s", c.Value)
			}
			lBHealthCheckMethod = method
		}
	}

	return &nlbHealthConfig{
		lBHealthCheckFlag:           lBHealthCheckFlag,
		lBHealthCheckType:           lBHealthCheckType,
		lBHealthCheckConnectPort:    lBHealthCheckConnectPort,
		lBHealthCheckConnectTimeout: lBHealthCheckConnectTimeout,
		lBHealthCheckInterval:       lBHealthCheckInterval,
		lBHealthCheckUri:            lBHealthCheckUri,
		lBHealthCheckDomain:         lBHealthCheckDomain,
		lBHealthCheckMethod:         lBHealthCheckMethod,
		lBHealthyThreshold:          lBHealthyThreshold,
		lBUnhealthyThreshold:        lBUnhealthyThreshold,
	}, nil
}

func validateDomain(domain string) error {
	if len(domain) < 1 || len(domain) > 80 {
		return fmt.Errorf("the domain length must be between 1 and 80 characters")
	}

	// Regular expression matches lowercase letters, numbers, dashes and periods
	domainRegex := regexp.MustCompile(`^[a-z0-9-.]+$`)
	if !domainRegex.MatchString(domain) {
		return fmt.Errorf("the domain must only contain lowercase letters, numbers, hyphens, and periods")
	}

	// make sure the domain name does not start or end with a dash or period
	if domain[0] == '-' || domain[0] == '.' || domain[len(domain)-1] == '-' || domain[len(domain)-1] == '.' {
		return fmt.Errorf("the domain must not start or end with a hyphen or period")
	}

	// make sure the domain name does not contain consecutive dots or dashes
	if regexp.MustCompile(`(--|\.\.)`).MatchString(domain) {
		return fmt.Errorf("the domain must not contain consecutive hyphens or periods")
	}

	return nil
}

func validateUri(uri string) error {
	if len(uri) < 1 || len(uri) > 80 {
		return fmt.Errorf("string length must be between 1 and 80 characters")
	}

	regexPattern := `^/[0-9a-zA-Z.!$%&'*+/=?^_` + "`" + `{|}~-]*$`
	matched, err := regexp.MatchString(regexPattern, uri)

	if err != nil {
		return fmt.Errorf("regex error: %v", err)
	}

	if !matched {
		return fmt.Errorf("string does not match the required pattern")
	}

	return nil
}
