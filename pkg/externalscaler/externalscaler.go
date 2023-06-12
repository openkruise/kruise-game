package externalscaler

import (
	"context"
	"fmt"
	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ExternalScaler struct {
	client client.Client
}

func (e *ExternalScaler) mustEmbedUnimplementedExternalScalerServer() {
}

func (e *ExternalScaler) IsActive(ctx context.Context, scaledObjectRef *ScaledObjectRef) (*IsActiveResponse, error) {
	name := scaledObjectRef.GetName()
	ns := scaledObjectRef.GetNamespace()
	gss := &gamekruiseiov1alpha1.GameServerSet{}
	err := e.client.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, gss)
	if err != nil {
		klog.Error(err)
		return nil, err
	}
	currentReplicas := gss.Status.CurrentReplicas
	numWaitToBeDeleted := gss.Status.WaitToBeDeletedReplicas
	if numWaitToBeDeleted == nil {
		return nil, fmt.Errorf("GameServerSet %s/%s has not inited", ns, name)
	}
	desireReplicas := currentReplicas - *numWaitToBeDeleted
	return &IsActiveResponse{
		Result: desireReplicas > 0,
	}, nil
}

func (e *ExternalScaler) StreamIsActive(scaledObject *ScaledObjectRef, epsServer ExternalScaler_StreamIsActiveServer) error {
	return nil
}

func (e *ExternalScaler) GetMetricSpec(ctx context.Context, scaledObjectRef *ScaledObjectRef) (*GetMetricSpecResponse, error) {
	return &GetMetricSpecResponse{
		MetricSpecs: []*MetricSpec{{
			MetricName: "gssReplicas",
			TargetSize: int64(1),
		}},
	}, nil
}

func (e *ExternalScaler) GetMetrics(ctx context.Context, metricRequest *GetMetricsRequest) (*GetMetricsResponse, error) {
	name := metricRequest.ScaledObjectRef.GetName()
	ns := metricRequest.ScaledObjectRef.GetNamespace()
	gss := &gamekruiseiov1alpha1.GameServerSet{}
	err := e.client.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, gss)
	if err != nil {
		klog.Error(err)
		return nil, err
	}

	isWaitToDelete, _ := labels.NewRequirement(gamekruiseiov1alpha1.GameServerOpsStateKey, selection.Equals, []string{string(gamekruiseiov1alpha1.WaitToDelete)})
	notDeleting, _ := labels.NewRequirement(gamekruiseiov1alpha1.GameServerStateKey, selection.NotEquals, []string{string(gamekruiseiov1alpha1.Deleting)})
	isGssOwner, _ := labels.NewRequirement(gamekruiseiov1alpha1.GameServerOwnerGssKey, selection.Equals, []string{name})

	podList := &corev1.PodList{}
	err = e.client.List(ctx, podList, &client.ListOptions{
		Namespace: ns,
		LabelSelector: labels.NewSelector().Add(
			*isWaitToDelete,
			*notDeleting,
			*isGssOwner,
		),
	})
	if err != nil {
		klog.Error(err)
		return nil, err
	}

	desireReplicas := int(*gss.Spec.Replicas)
	numWaitToBeDeleted := len(podList.Items)

	klog.Infof("GameServerSet %s/%s desire replicas is %d", ns, name, desireReplicas-numWaitToBeDeleted)
	return &GetMetricsResponse{
		MetricValues: []*MetricValue{{
			MetricName:  "gssReplicas",
			MetricValue: int64(desireReplicas - numWaitToBeDeleted),
		}},
	}, nil
}

func NewExternalScaler(client client.Client) *ExternalScaler {
	return &ExternalScaler{
		client: client,
	}
}
