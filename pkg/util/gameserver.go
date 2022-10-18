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
	appspub "github.com/openkruise/kruise-api/apps/pub"
	kruiseV1beta1 "github.com/openkruise/kruise-api/apps/v1beta1"
	gameKruiseV1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	apps "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"strconv"
	"strings"
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
	iOpsState := iLabels[gameKruiseV1alpha1.GameServerOpsStateKey]
	jOpsState := jLabels[gameKruiseV1alpha1.GameServerOpsStateKey]
	iDeletionPriority := iLabels[gameKruiseV1alpha1.GameServerDeletePriorityKey]
	jDeletionPriority := jLabels[gameKruiseV1alpha1.GameServerDeletePriorityKey]

	// OpsState
	if iOpsState != jOpsState {
		return opsStateDeletePrority(iOpsState) > opsStateDeletePrority(jOpsState)
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
	case string(gameKruiseV1alpha1.WaitToDelete):
		return 1
	case string(gameKruiseV1alpha1.None):
		return 0
	case string(gameKruiseV1alpha1.Maintaining):
		return -1
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
	asts.Spec.Template.SetAnnotations(podAnnotations)

	// set template spec
	asts.Spec.Template.Spec = gss.Spec.GameServerTemplate.Spec
	// default: add InPlaceUpdateReady condition
	readinessGates := gss.Spec.GameServerTemplate.Spec.ReadinessGates
	readinessGates = append(readinessGates, corev1.PodReadinessGate{ConditionType: appspub.InPlaceUpdateReady})
	asts.Spec.Template.Spec.ReadinessGates = readinessGates

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
}

func GetAstsHash(gss *gameKruiseV1alpha1.GameServerSet) string {
	return GetHash(astsToUpdate{
		UpdateStrategy: gss.Spec.UpdateStrategy,
		Template:       gss.Spec.GameServerTemplate,
	})
}

func AddPrefixGameKruise(s string) string {
	return "game.kruise.io/" + s
}

func RemovePrefixGameKruise(s string) string {
	return strings.TrimPrefix(s, "game.kruise.io/")
}
