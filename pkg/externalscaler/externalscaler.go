package externalscaler

import (
	"context"
	"fmt"
	"math"
	"strconv"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	NoneGameServerMinNumberKey = "minAvailable"
	NoneGameServerMaxNumberKey = "maxAvailable"
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

	minNum := 0.0
	minNumStr := scaledObjectRef.GetScalerMetadata()[NoneGameServerMinNumberKey]
	if minNumStr != "" {
		minNum, err = strconv.ParseFloat(minNumStr, 32)
		if err != nil {
			return nil, err
		}
	}
	if minNum > 0.0 {
		return &IsActiveResponse{
			Result: true,
		}, nil
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

	// scale up when number of GameServers with None opsState less than minAvailable defined by user
	isGssOwner, _ := labels.NewRequirement(gamekruiseiov1alpha1.GameServerOwnerGssKey, selection.Equals, []string{name})
	isNone, _ := labels.NewRequirement(gamekruiseiov1alpha1.GameServerOpsStateKey, selection.Equals, []string{string(gamekruiseiov1alpha1.None)})
	podList := &corev1.PodList{}
	err = e.client.List(ctx, podList, &client.ListOptions{
		Namespace: ns,
		LabelSelector: labels.NewSelector().Add(
			*isGssOwner,
		),
	})
	if err != nil {
		klog.Error(err)
		return nil, err
	}

	totalNum := len(podList.Items)
	noneNum := 0
	for _, pod := range podList.Items {
		if isNone.Matches(labels.Set(pod.Labels)) {
			noneNum++
		}
	}

	maxNumStr := metricRequest.ScaledObjectRef.GetScalerMetadata()[NoneGameServerMaxNumberKey]
	var maxNumP *int
	if maxNumStr != "" {
		mn, err := strconv.ParseInt(maxNumStr, 10, 32)
		if err != nil {
			klog.Errorf("maxAvailable should be integer type, err: %s", err.Error())
		} else {
			maxNumP = ptr.To(int(mn))
		}
	}

	minNum, err := handleMinNum(totalNum, noneNum, metricRequest.ScaledObjectRef.GetScalerMetadata()[NoneGameServerMinNumberKey])
	if err != nil {
		klog.Error(err)
		return nil, err
	}

	if maxNumP != nil && minNum > *maxNumP {
		minNum = *maxNumP
	}

	if noneNum < minNum {
		desireReplicas := *gss.Spec.Replicas + int32(minNum) - int32(noneNum)
		klog.Infof("GameServerSet %s/%s desire replicas is %d", ns, name, desireReplicas)
		return &GetMetricsResponse{
			MetricValues: []*MetricValue{{
				MetricName:  "gssReplicas",
				MetricValue: int64(desireReplicas),
			}},
		}, nil
	}

	//  scale down those GameServers with WaitToBeDeleted opsState
	isWaitToDelete, _ := labels.NewRequirement(gamekruiseiov1alpha1.GameServerOpsStateKey, selection.Equals, []string{string(gamekruiseiov1alpha1.WaitToDelete)})
	notDeleting, _ := labels.NewRequirement(gamekruiseiov1alpha1.GameServerStateKey, selection.NotEquals, []string{string(gamekruiseiov1alpha1.Deleting)})
	podList = &corev1.PodList{}
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
	if numWaitToBeDeleted != 0 {
		desireReplicas = desireReplicas - numWaitToBeDeleted
	} else {
		// scale down when number of GameServers with None opsState more than maxAvailable defined by user
		if maxNumP != nil && noneNum > *maxNumP {
			desireReplicas = (desireReplicas) + *maxNumP - (noneNum)
		}
	}

	klog.Infof("GameServerSet %s/%s desire replicas is %d", ns, name, desireReplicas)
	return &GetMetricsResponse{
		MetricValues: []*MetricValue{{
			MetricName:  "gssReplicas",
			MetricValue: int64(desireReplicas),
		}},
	}, nil
}

func NewExternalScaler(client client.Client) *ExternalScaler {
	return &ExternalScaler{
		client: client,
	}
}

// handleMinNum calculate the expected min number of GameServers from the give minNumStr,
// supported format:
//   - integer: minNum >= 1,
//     return the fixed min number of none opState GameServers.
//   - float: 0 < minNum < 1,
//     return the min number of none opState GameServers
//     calculated by the percentage of the total number of GameServers after scaled.
func handleMinNum(totalNum, noneNum int, minNumStr string) (int, error) {
	if minNumStr == "" {
		return 0, nil
	}
	n, err := strconv.ParseFloat(minNumStr, 32)
	if err != nil {
		return 0, err
	}
	switch {
	case n > 0 && n < 1:
		// for (noneNum + delta) / (totalNum + delta) >= n
		// => delta >= (totalNum * n - noneNum) / (1 - n)
		delta := (float64(totalNum)*n - float64(noneNum)) / (1 - n)
		if delta <= 0 {
			// no need to scale up
			return 0, nil
		}
		// ceil the delta to avoid the float number
		delta = math.Round(delta*100) / 100
		minNum := int(math.Ceil(delta)) + noneNum
		return minNum, nil
	case n >= 1 || n == 0:
		n = math.Ceil(n)
		return int(n), nil
	}

	return 0, fmt.Errorf("invalid min number: must be greater than 0 or a valid percentage between 0 and 1")
}
