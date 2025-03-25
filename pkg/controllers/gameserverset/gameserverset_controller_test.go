package gameserverset

import (
	"context"
	"reflect"
	"testing"

	appspub "github.com/openkruise/kruise-api/apps/pub"
	kruiseV1beta1 "github.com/openkruise/kruise-api/apps/v1beta1"
	gameKruiseV1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/pkg/util"
	apps "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestInitAsts(t *testing.T) {
	tests := []struct {
		gss  *gameKruiseV1alpha1.GameServerSet
		asts *kruiseV1beta1.StatefulSet
	}{
		{
			gss: &gameKruiseV1alpha1.GameServerSet{
				TypeMeta: metav1.TypeMeta{
					Kind:       "GameServerSet",
					APIVersion: "game.kruise.io/v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "xxx",
					Name:      "case0",
					UID:       "xxx0",
				},
				Spec: gameKruiseV1alpha1.GameServerSetSpec{
					Replicas: ptr.To[int32](5),
					UpdateStrategy: gameKruiseV1alpha1.UpdateStrategy{
						Type:          apps.RollingUpdateStatefulSetStrategyType,
						RollingUpdate: &gameKruiseV1alpha1.RollingUpdateStatefulSetStrategy{},
					},
				},
			},
			asts: &kruiseV1beta1.StatefulSet{
				TypeMeta: metav1.TypeMeta{
					Kind:       "StatefulSet",
					APIVersion: "apps.kruise.io/v1beta1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "xxx",
					Name:      "case0",
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         "game.kruise.io/v1alpha1",
							Kind:               "GameServerSet",
							Name:               "case0",
							UID:                "xxx0",
							Controller:         ptr.To[bool](true),
							BlockOwnerDeletion: ptr.To[bool](true),
						},
					},
					ResourceVersion: "1",
				},
				Spec: kruiseV1beta1.StatefulSetSpec{
					Replicas:            ptr.To[int32](5),
					PodManagementPolicy: apps.ParallelPodManagement,
					ServiceName:         "case0",
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{gameKruiseV1alpha1.GameServerOwnerGssKey: "case0"},
					},
					UpdateStrategy: kruiseV1beta1.StatefulSetUpdateStrategy{
						Type: apps.RollingUpdateStatefulSetStrategyType,
						RollingUpdate: &kruiseV1beta1.RollingUpdateStatefulSetStrategy{
							UnorderedUpdate: &kruiseV1beta1.UnorderedUpdateStrategy{
								PriorityStrategy: &appspub.UpdatePriorityStrategy{
									OrderPriority: []appspub.UpdatePriorityOrderTerm{
										{
											OrderedKey: gameKruiseV1alpha1.GameServerUpdatePriorityKey,
										},
									},
								},
							},
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								gameKruiseV1alpha1.GameServerOwnerGssKey: "case0",
							},
						},
						Spec: corev1.PodSpec{
							ReadinessGates: []corev1.PodReadinessGate{
								{
									ConditionType: appspub.InPlaceUpdateReady,
								},
							},
						},
					},
					ScaleStrategy: &kruiseV1beta1.StatefulSetScaleStrategy{},
				},
			},
		},

		{
			gss: &gameKruiseV1alpha1.GameServerSet{
				TypeMeta: metav1.TypeMeta{
					Kind:       "GameServerSet",
					APIVersion: "game.kruise.io/v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "xxx",
					Name:      "case1",
					UID:       "xxx1",
				},
				Spec: gameKruiseV1alpha1.GameServerSetSpec{
					Replicas:             ptr.To[int32](4),
					ReserveGameServerIds: []intstr.IntOrString{intstr.FromInt(0)},
					UpdateStrategy: gameKruiseV1alpha1.UpdateStrategy{
						Type:          apps.RollingUpdateStatefulSetStrategyType,
						RollingUpdate: &gameKruiseV1alpha1.RollingUpdateStatefulSetStrategy{},
					},
				},
			},
			asts: &kruiseV1beta1.StatefulSet{
				TypeMeta: metav1.TypeMeta{
					Kind:       "StatefulSet",
					APIVersion: "apps.kruise.io/v1beta1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "xxx",
					Name:      "case1",
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         "game.kruise.io/v1alpha1",
							Kind:               "GameServerSet",
							Name:               "case1",
							UID:                "xxx1",
							Controller:         ptr.To[bool](true),
							BlockOwnerDeletion: ptr.To[bool](true),
						},
					},
					ResourceVersion: "1",
				},
				Spec: kruiseV1beta1.StatefulSetSpec{
					Replicas:            ptr.To[int32](4),
					ReserveOrdinals:     []intstr.IntOrString{intstr.FromInt(0)},
					PodManagementPolicy: apps.ParallelPodManagement,
					ServiceName:         "case1",
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{gameKruiseV1alpha1.GameServerOwnerGssKey: "case1"},
					},
					UpdateStrategy: kruiseV1beta1.StatefulSetUpdateStrategy{
						Type: apps.RollingUpdateStatefulSetStrategyType,
						RollingUpdate: &kruiseV1beta1.RollingUpdateStatefulSetStrategy{
							UnorderedUpdate: &kruiseV1beta1.UnorderedUpdateStrategy{
								PriorityStrategy: &appspub.UpdatePriorityStrategy{
									OrderPriority: []appspub.UpdatePriorityOrderTerm{
										{
											OrderedKey: gameKruiseV1alpha1.GameServerUpdatePriorityKey,
										},
									},
								},
							},
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								gameKruiseV1alpha1.GameServerOwnerGssKey: "case1",
							},
						},
						Spec: corev1.PodSpec{
							ReadinessGates: []corev1.PodReadinessGate{
								{
									ConditionType: appspub.InPlaceUpdateReady,
								},
							},
						},
					},
					ScaleStrategy: &kruiseV1beta1.StatefulSetScaleStrategy{},
				},
			},
		},
	}

	for i, test := range tests {
		var objs []client.Object
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
		reconcile := &GameServerSetReconciler{
			Client: c,
			Scheme: scheme,
		}
		if err := reconcile.initAsts(test.gss); err != nil {
			t.Errorf("case %d: %s", i, err.Error())
		}
		initAsts := &kruiseV1beta1.StatefulSet{}
		if err := reconcile.Get(context.TODO(), types.NamespacedName{
			Namespace: test.gss.Namespace,
			Name:      test.gss.Name,
		}, initAsts); err != nil {
			t.Errorf("case %d: %s", i, err.Error())
		}
		if test.asts.Annotations == nil {
			test.asts.Annotations = make(map[string]string)
		}
		test.asts.Annotations[gameKruiseV1alpha1.AstsHashKey] = util.GetAstsHash(test.gss)
		if !reflect.DeepEqual(initAsts, test.asts) {
			t.Errorf("expect asts %v but got %v", test.asts, initAsts)
		}
	}
}
