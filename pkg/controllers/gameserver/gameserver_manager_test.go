package gameserver

import (
	gameKruiseV1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"reflect"
	"testing"
)

func TestSyncServiceQualities(t *testing.T) {
	up := intstr.FromInt(20)
	dp := intstr.FromInt(10)
	fakeProbeTime := metav1.Now()
	fakeActionTime := metav1.Now()
	tests := []struct {
		serviceQualities []gameKruiseV1alpha1.ServiceQuality
		podConditions    []corev1.PodCondition
		sqConditions     []gameKruiseV1alpha1.ServiceQualityCondition
		spec             gameKruiseV1alpha1.GameServerSpec
		newSqConditions  []gameKruiseV1alpha1.ServiceQualityCondition
	}{
		{
			serviceQualities: []gameKruiseV1alpha1.ServiceQuality{
				{
					Name:      "healthy",
					Permanent: true,
					ServiceQualityAction: []gameKruiseV1alpha1.ServiceQualityAction{
						{
							State: true,
							GameServerSpec: gameKruiseV1alpha1.GameServerSpec{
								UpdatePriority: &up,
							},
						},
						{
							State: false,
							GameServerSpec: gameKruiseV1alpha1.GameServerSpec{
								DeletionPriority: &dp,
							},
						},
					},
				},
			},
			podConditions: []corev1.PodCondition{
				{
					Type:          "game.kruise.io/healthy",
					Status:        corev1.ConditionTrue,
					LastProbeTime: fakeProbeTime,
				},
				{
					Type:          "otherA",
					Status:        corev1.ConditionFalse,
					LastProbeTime: fakeProbeTime,
				},
			},
			sqConditions: nil,
			spec: gameKruiseV1alpha1.GameServerSpec{
				UpdatePriority: &up,
			},
			newSqConditions: []gameKruiseV1alpha1.ServiceQualityCondition{
				{
					Name:                     "healthy",
					Status:                   string(corev1.ConditionTrue),
					LastProbeTime:            fakeProbeTime,
					LastActionTransitionTime: fakeActionTime,
				},
			},
		},
		{
			serviceQualities: []gameKruiseV1alpha1.ServiceQuality{
				{
					Name:      "healthy",
					Permanent: true,
					ServiceQualityAction: []gameKruiseV1alpha1.ServiceQualityAction{
						{
							State: true,
							GameServerSpec: gameKruiseV1alpha1.GameServerSpec{
								UpdatePriority: &up,
							},
						},
						{
							State: false,
							GameServerSpec: gameKruiseV1alpha1.GameServerSpec{
								DeletionPriority: &dp,
							},
						},
					},
				},
			},
			podConditions: []corev1.PodCondition{
				{
					Type:          "game.kruise.io/healthy",
					Status:        corev1.ConditionTrue,
					LastProbeTime: fakeProbeTime,
				},
				{
					Type:          "otherA",
					Status:        corev1.ConditionFalse,
					LastProbeTime: fakeProbeTime,
				},
			},
			sqConditions: []gameKruiseV1alpha1.ServiceQualityCondition{
				{
					Name:                     "healthy",
					Status:                   string(corev1.ConditionFalse),
					LastProbeTime:            fakeProbeTime,
					LastActionTransitionTime: fakeActionTime,
				},
			},
			spec: gameKruiseV1alpha1.GameServerSpec{},
			newSqConditions: []gameKruiseV1alpha1.ServiceQualityCondition{
				{
					Name:                     "healthy",
					Status:                   string(corev1.ConditionTrue),
					LastProbeTime:            fakeProbeTime,
					LastActionTransitionTime: fakeActionTime,
				},
			},
		},
		{
			serviceQualities: []gameKruiseV1alpha1.ServiceQuality{
				{
					Name:      "healthy",
					Permanent: true,
					ServiceQualityAction: []gameKruiseV1alpha1.ServiceQualityAction{
						{
							State: true,
							GameServerSpec: gameKruiseV1alpha1.GameServerSpec{
								UpdatePriority: &up,
							},
						},
						{
							State: false,
							GameServerSpec: gameKruiseV1alpha1.GameServerSpec{
								DeletionPriority: &dp,
							},
						},
					},
				},
			},
			podConditions: []corev1.PodCondition{
				{
					Type:          "otherA",
					Status:        corev1.ConditionFalse,
					LastProbeTime: fakeProbeTime,
				},
			},
			sqConditions: nil,
			spec:         gameKruiseV1alpha1.GameServerSpec{},
			newSqConditions: []gameKruiseV1alpha1.ServiceQualityCondition{
				{
					Name: "healthy",
				},
			},
		},
		{
			serviceQualities: []gameKruiseV1alpha1.ServiceQuality{
				{
					Name:      "healthy",
					Permanent: true,
					ServiceQualityAction: []gameKruiseV1alpha1.ServiceQualityAction{
						{
							State: true,
							GameServerSpec: gameKruiseV1alpha1.GameServerSpec{
								UpdatePriority: &up,
							},
						},
					},
				},
			},
			podConditions: []corev1.PodCondition{
				{
					Type:          "game.kruise.io/healthy",
					Status:        corev1.ConditionFalse,
					LastProbeTime: fakeProbeTime,
				},
				{
					Type:          "otherA",
					Status:        corev1.ConditionFalse,
					LastProbeTime: fakeProbeTime,
				},
			},
			sqConditions: nil,
			spec:         gameKruiseV1alpha1.GameServerSpec{},
			newSqConditions: []gameKruiseV1alpha1.ServiceQualityCondition{
				{
					Name:          "healthy",
					Status:        string(corev1.ConditionFalse),
					LastProbeTime: fakeProbeTime,
				},
			},
		},
		{
			serviceQualities: []gameKruiseV1alpha1.ServiceQuality{
				{
					Name:      "healthy",
					Permanent: false,
					ServiceQualityAction: []gameKruiseV1alpha1.ServiceQualityAction{
						{
							State: true,
							GameServerSpec: gameKruiseV1alpha1.GameServerSpec{
								UpdatePriority: &up,
							},
						},
						{
							State: false,
							GameServerSpec: gameKruiseV1alpha1.GameServerSpec{
								DeletionPriority: &dp,
							},
						},
					},
				},
			},
			podConditions: []corev1.PodCondition{
				{
					Type:          "game.kruise.io/healthy",
					Status:        corev1.ConditionTrue,
					LastProbeTime: fakeProbeTime,
				},
				{
					Type:          "otherA",
					Status:        corev1.ConditionFalse,
					LastProbeTime: fakeProbeTime,
				},
			},
			sqConditions: []gameKruiseV1alpha1.ServiceQualityCondition{
				{
					Name:                     "healthy",
					Status:                   string(corev1.ConditionFalse),
					LastProbeTime:            fakeProbeTime,
					LastActionTransitionTime: fakeActionTime,
				},
			},
			spec: gameKruiseV1alpha1.GameServerSpec{
				UpdatePriority: &up,
			},
			newSqConditions: []gameKruiseV1alpha1.ServiceQualityCondition{
				{
					Name:                     "healthy",
					Status:                   string(corev1.ConditionTrue),
					LastProbeTime:            fakeProbeTime,
					LastActionTransitionTime: fakeActionTime,
				},
			},
		},
	}

	for _, test := range tests {
		actualSpec, actualNewSqConditions := syncServiceQualities(test.serviceQualities, test.podConditions, test.sqConditions)
		expectSpec := test.spec
		expectNewSqConditions := test.newSqConditions
		if !reflect.DeepEqual(actualSpec, expectSpec) {
			t.Errorf("expect spec %v but got %v", expectSpec, actualSpec)
		}
		if len(actualNewSqConditions) != len(expectNewSqConditions) {
			t.Errorf("expect sq conditions len %v but got %v", len(expectNewSqConditions), len(actualNewSqConditions))
		}
		for _, expectNewSqCondition := range expectNewSqConditions {
			exist := false
			for _, actualNewSqCondition := range actualNewSqConditions {
				if actualNewSqCondition.Name == expectNewSqCondition.Name {
					exist = true
					if actualNewSqCondition.Status != expectNewSqCondition.Status {
						t.Errorf("expect sq condition status %v but got %v", expectNewSqCondition.Status, actualNewSqCondition.Status)
					}
					if actualNewSqCondition.LastProbeTime != expectNewSqCondition.LastProbeTime {
						t.Errorf("expect sq condition LastProbeTime %v but got %v", expectNewSqCondition.LastProbeTime, actualNewSqCondition.LastProbeTime)
					}
					if actualNewSqCondition.LastActionTransitionTime.IsZero() != expectNewSqCondition.LastActionTransitionTime.IsZero() {
						t.Errorf("expect sq condition LastActionTransitionTime IsZero %v but got %v", expectNewSqCondition.LastActionTransitionTime.IsZero(), actualNewSqCondition.LastActionTransitionTime.IsZero())
					}
					break
				}
			}
			if !exist {
				t.Errorf("expect sq condition %s exist, but actually not", expectNewSqCondition.Name)
			}
		}
	}
}
