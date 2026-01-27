package gameserver

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gameKruiseV1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
)

const (
	defaultBasePriority    = 100
	defaultPodCountWeight  = 10
	defaultOwnerCountWeight = 5
)

type TopologyRater struct {
	client client.Client
}

func NewTopologyRater(c client.Client) *TopologyRater {
	return &TopologyRater{client: c}
}

func (r *TopologyRater) CalculateDeletionPriority(
	ctx context.Context,
	gs *gameKruiseV1alpha1.GameServer,
	pod *corev1.Pod,
	config *gameKruiseV1alpha1.TopologyDeletionPriorityConfig,
) (*intstr.IntOrString, error) {
	if config == nil {
		return nil, nil
	}

	nodeName := pod.Spec.NodeName
	if nodeName == "" {
		return nil, nil
	}

	basePriority := defaultBasePriority
	podCountWeight := defaultPodCountWeight
	ownerCountWeight := defaultOwnerCountWeight

	if config.BasePriority != 0 {
		basePriority = config.BasePriority
	}
	if config.PodCountWeight != 0 {
		podCountWeight = config.PodCountWeight
	}
	if config.OwnerCountWeight != 0 {
		ownerCountWeight = config.OwnerCountWeight
	}

	podList := &corev1.PodList{}
	listOpts := &client.ListOptions{
		Namespace:     pod.Namespace,
		FieldSelector: fields.SelectorFromSet(fields.Set{"spec.nodeName": nodeName}),
	}

	if err := r.client.List(ctx, podList, listOpts); err != nil {
		return nil, fmt.Errorf("failed to list pods on node %s: %w", nodeName, err)
	}

	podCount := len(podList.Items)
	ownerSet := make(map[types.UID]struct{})

	for _, p := range podList.Items {
		ownerRefs := p.GetOwnerReferences()
		for _, owner := range ownerRefs {
			ownerSet[owner.UID] = struct{}{}
		}
	}
	ownerCount := len(ownerSet)

	priority := basePriority - (podCount * podCountWeight) - (ownerCount * ownerCountWeight)

	if priority < 0 {
		priority = 0
	}

	result := intstr.FromInt(priority)
	return &result, nil
}
