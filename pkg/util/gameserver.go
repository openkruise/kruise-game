/*
Copyright 2022 The Kruise Authors.

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

package util

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	apps "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appspub "github.com/openkruise/kruise-api/apps/pub"
	kruiseV1beta1 "github.com/openkruise/kruise-api/apps/v1beta1"
	gameKruiseV1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
)

type DeleteSequenceGs []corev1.Pod

func (dg DeleteSequenceGs) Len() int {
	return len(dg)
}

func (dg DeleteSequenceGs) Swap(i, j int) {
	dg[i], dg[j] = dg[j], dg[i]
}

func (dg DeleteSequenceGs) Less(i, j int) bool {
	iLabels := dg[i].GetLabels()
	jLabels := dg[j].GetLabels()
	iOpsStatePriority := opsStateDeletePrority(iLabels[gameKruiseV1alpha1.GameServerOpsStateKey])
	jOpsStatePriority := opsStateDeletePrority(jLabels[gameKruiseV1alpha1.GameServerOpsStateKey])
	iDeletionPriority := iLabels[gameKruiseV1alpha1.GameServerDeletePriorityKey]
	jDeletionPriority := jLabels[gameKruiseV1alpha1.GameServerDeletePriorityKey]

	// OpsState
	if iOpsStatePriority != jOpsStatePriority {
		return iOpsStatePriority > jOpsStatePriority
	}
	// Deletion Priority
	if iDeletionPriority != jDeletionPriority {
		iDeletionPriorityInt, _ := strconv.Atoi(iDeletionPriority)
		jDeletionPriorityInt, _ := strconv.Atoi(jDeletionPriority)
		return iDeletionPriorityInt > jDeletionPriorityInt
	}
	// Index Number
	return GetIndexFromGsName(dg[i].GetName()) > GetIndexFromGsName(dg[j].GetName())
}

func opsStateDeletePrority(opsState string) int {
	switch opsState {
	case string(gameKruiseV1alpha1.Kill):
		return 100
	case string(gameKruiseV1alpha1.WaitToDelete):
		return 1
	case string(gameKruiseV1alpha1.None):
		return 0
	case string(gameKruiseV1alpha1.Allocated):
		return -1
	case string(gameKruiseV1alpha1.Maintaining):
		return -2
	}
	return 0
}

func GetIndexFromGsName(gsName string) int {
	temp := strings.Split(gsName, "-")
	index, _ := strconv.Atoi(temp[len(temp)-1])
	return index
}

func GetIndexListFromPodList(podList []corev1.Pod) []int {
	var indexList []int
	for i := 0; i < len(podList); i++ {
		indexList = append(indexList, GetIndexFromGsName(podList[i].GetName()))
	}
	return indexList
}

func GetIndexSetFromPodList(podList []corev1.Pod) sets.Set[int] {
	return sets.New[int](GetIndexListFromPodList(podList)...)
}

func GetIndexListFromGsList(gsList []gameKruiseV1alpha1.GameServer) []int {
	var indexList []int
	for i := 0; i < len(gsList); i++ {
		indexList = append(indexList, GetIndexFromGsName(gsList[i].GetName()))
	}
	return indexList
}

func GetNewAstsFromGss(gss *gameKruiseV1alpha1.GameServerSet, asts *kruiseV1beta1.StatefulSet) *kruiseV1beta1.StatefulSet {
	// default: set ParallelPodManagement
	asts.Spec.PodManagementPolicy = apps.ParallelPodManagement

	// set pod labels
	podLabels := gss.Spec.GameServerTemplate.GetLabels()
	if podLabels == nil {
		podLabels = make(map[string]string)
	}
	podLabels[gameKruiseV1alpha1.GameServerOwnerGssKey] = gss.GetName()
	asts.Spec.Template.SetLabels(podLabels)

	// set pod annotations
	podAnnotations := gss.Spec.GameServerTemplate.GetAnnotations()
	if gss.Spec.Network != nil {
		if podAnnotations == nil {
			podAnnotations = make(map[string]string)
		}
		networkConfig, _ := json.Marshal(gss.Spec.Network.NetworkConf)
		podAnnotations[gameKruiseV1alpha1.GameServerNetworkConf] = string(networkConfig)
		podAnnotations[gameKruiseV1alpha1.GameServerNetworkType] = gss.Spec.Network.NetworkType
	}
	asts.Spec.Template.SetAnnotations(podAnnotations)

	// set template spec
	asts.Spec.Template.Spec = gss.Spec.GameServerTemplate.Spec
	// default: add InPlaceUpdateReady condition
	readinessGates := gss.Spec.GameServerTemplate.Spec.ReadinessGates
	readinessGates = append(readinessGates, corev1.PodReadinessGate{ConditionType: appspub.InPlaceUpdateReady})
	asts.Spec.Template.Spec.ReadinessGates = readinessGates

	// set Lifecycle
	asts.Spec.Lifecycle = gss.Spec.Lifecycle
	// AllowNotReadyContainers
	if gss.Spec.Network != nil && IsAllowNotReadyContainers(gss.Spec.Network.NetworkConf) {
		if asts.Spec.Lifecycle == nil {
			asts.Spec.Lifecycle = &appspub.Lifecycle{}
		}
		if asts.Spec.Lifecycle.InPlaceUpdate == nil {
			asts.Spec.Lifecycle.InPlaceUpdate = &appspub.LifecycleHook{}
		}
		if asts.Spec.Lifecycle.InPlaceUpdate.LabelsHandler == nil {
			asts.Spec.Lifecycle.InPlaceUpdate.LabelsHandler = make(map[string]string)
		}
		asts.Spec.Lifecycle.InPlaceUpdate.LabelsHandler[gameKruiseV1alpha1.InplaceUpdateNotReadyBlocker] = "true"
	}

	// set VolumeClaimTemplates
	asts.Spec.VolumeClaimTemplates = gss.Spec.GameServerTemplate.VolumeClaimTemplates

	// set ScaleStrategy
	asts.Spec.ScaleStrategy = &kruiseV1beta1.StatefulSetScaleStrategy{
		MaxUnavailable: gss.Spec.ScaleStrategy.MaxUnavailable,
	}

	// set UpdateStrategy
	asts.Spec.UpdateStrategy.Type = gss.Spec.UpdateStrategy.Type
	var rollingUpdateStatefulSetStrategy *kruiseV1beta1.RollingUpdateStatefulSetStrategy
	if gss.Spec.UpdateStrategy.RollingUpdate != nil {
		asts.Spec.UpdateStrategy.Type = apps.RollingUpdateStatefulSetStrategyType
		rollingUpdateStatefulSetStrategy = &kruiseV1beta1.RollingUpdateStatefulSetStrategy{
			Partition:             gss.Spec.UpdateStrategy.RollingUpdate.Partition,
			MaxUnavailable:        gss.Spec.UpdateStrategy.RollingUpdate.MaxUnavailable,
			PodUpdatePolicy:       gss.Spec.UpdateStrategy.RollingUpdate.PodUpdatePolicy,
			Paused:                gss.Spec.UpdateStrategy.RollingUpdate.Paused,
			InPlaceUpdateStrategy: gss.Spec.UpdateStrategy.RollingUpdate.InPlaceUpdateStrategy,
			MinReadySeconds:       gss.Spec.UpdateStrategy.RollingUpdate.MinReadySeconds,
			UnorderedUpdate: &kruiseV1beta1.UnorderedUpdateStrategy{
				PriorityStrategy: &appspub.UpdatePriorityStrategy{
					OrderPriority: []appspub.UpdatePriorityOrderTerm{
						{
							OrderedKey: gameKruiseV1alpha1.GameServerUpdatePriorityKey,
						},
					},
				},
			},
		}
	}
	asts.Spec.UpdateStrategy.RollingUpdate = rollingUpdateStatefulSetStrategy

	return asts
}

type astsToUpdate struct {
	UpdateStrategy gameKruiseV1alpha1.UpdateStrategy
	Template       gameKruiseV1alpha1.GameServerTemplate
	NetworkConfigs []gameKruiseV1alpha1.NetworkConfParams
}

func GetAstsHash(gss *gameKruiseV1alpha1.GameServerSet) string {
	var networkConfigs []gameKruiseV1alpha1.NetworkConfParams
	if gss.Spec.Network != nil {
		networkConfigs = gss.Spec.Network.NetworkConf
	}
	return GetHash(astsToUpdate{
		UpdateStrategy: gss.Spec.UpdateStrategy,
		Template:       gss.Spec.GameServerTemplate,
		NetworkConfigs: networkConfigs,
	})
}

func GetGsTemplateMetadataHash(gss *gameKruiseV1alpha1.GameServerSet) string {
	return GetHash(metav1.ObjectMeta{
		Labels:      gss.Spec.GameServerTemplate.GetLabels(),
		Annotations: gss.Spec.GameServerTemplate.GetAnnotations(),
	})
}

func AddPrefixGameKruise(s string) string {
	return "game.kruise.io/" + s
}

func AddPrefixGsSyncToPod(s string) string {
	return "gs-sync/" + s
}

func IsHasPrefixGsSyncToPod(s string) bool {
	return strings.HasPrefix(s, "gs-sync/")
}

func RemovePrefixGameKruise(s string) string {
	return strings.TrimPrefix(s, "game.kruise.io/")
}

func GetGameServerSetOfPod(pod *corev1.Pod, c client.Client, ctx context.Context) (*gameKruiseV1alpha1.GameServerSet, error) {
	gssName := pod.GetLabels()[gameKruiseV1alpha1.GameServerOwnerGssKey]
	gss := &gameKruiseV1alpha1.GameServerSet{}
	err := c.Get(ctx, types.NamespacedName{
		Namespace: pod.GetNamespace(),
		Name:      gssName,
	}, gss)
	return gss, err
}

func IsAllowNotReadyContainers(networkConfParams []gameKruiseV1alpha1.NetworkConfParams) bool {
	for _, networkConfParam := range networkConfParams {
		if networkConfParam.Name == gameKruiseV1alpha1.AllowNotReadyContainersNetworkConfName {
			return true
		}
	}
	return false
}

func InitGameServer(gss *gameKruiseV1alpha1.GameServerSet, name string) *gameKruiseV1alpha1.GameServer {
	gs := &gameKruiseV1alpha1.GameServer{}
	gs.Name = name
	gs.Namespace = gss.GetNamespace()

	ors := make([]metav1.OwnerReference, 0)
	or := metav1.OwnerReference{
		APIVersion:         gss.APIVersion,
		Kind:               gss.Kind,
		Name:               gss.GetName(),
		UID:                gss.GetUID(),
		Controller:         ptr.To[bool](true),
		BlockOwnerDeletion: ptr.To[bool](true),
	}
	ors = append(ors, or)
	gs.OwnerReferences = ors

	// set Labels
	gsLabels := gss.Spec.GameServerTemplate.DeepCopy().GetLabels()
	if gsLabels == nil {
		gsLabels = make(map[string]string)
	}
	gsLabels[gameKruiseV1alpha1.GameServerOwnerGssKey] = gss.GetName()
	gs.SetLabels(gsLabels)

	// set Annotations
	gsAnnotations := gss.Spec.GameServerTemplate.DeepCopy().GetAnnotations()
	if gsAnnotations == nil {
		gsAnnotations = make(map[string]string)
	}
	gsAnnotations[gameKruiseV1alpha1.GsTemplateMetadataHashKey] = GetGsTemplateMetadataHash(gss)
	gs.SetAnnotations(gsAnnotations)

	// set NetWork
	gs.Spec.NetworkDisabled = false

	// set OpsState
	gs.Spec.OpsState = gameKruiseV1alpha1.None

	// set UpdatePriority
	updatePriority := intstr.FromInt(0)
	gs.Spec.UpdatePriority = &updatePriority

	// set deletionPriority
	deletionPriority := intstr.FromInt(0)
	gs.Spec.DeletionPriority = &deletionPriority

	return gs
}
