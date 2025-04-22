package gameserverset

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"testing"

	appspub "github.com/openkruise/kruise-api/apps/pub"
	kruiseV1alpha1 "github.com/openkruise/kruise-api/apps/v1alpha1"
	kruiseV1beta1 "github.com/openkruise/kruise-api/apps/v1beta1"
	apps "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	gameKruiseV1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/pkg/util"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(gameKruiseV1alpha1.AddToScheme(scheme))
	utilruntime.Must(kruiseV1beta1.AddToScheme(scheme))
	utilruntime.Must(kruiseV1alpha1.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
}

func TestComputeToScaleGs(t *testing.T) {
	tests := []struct {
		newGssReserveIds sets.Set[int]
		oldGssreserveIds sets.Set[int]
		notExistIds      sets.Set[int]
		expectedReplicas int
		pods             []corev1.Pod
		newReserveIds    sets.Set[int]
		newManageIds     sets.Set[int]
	}{
		// case 0
		{
			newGssReserveIds: sets.New(2, 3, 4),
			oldGssreserveIds: sets.New(2, 3),
			notExistIds:      sets.New(5),
			expectedReplicas: 3,
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "xxx-0",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.None),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "10",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "xxx-1",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.Maintaining),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "0",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "xxx-4",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.WaitToDelete),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "0",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "xxx-6",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.None),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "0",
						},
					},
				},
			},
			newReserveIds: sets.New(2, 3, 4, 5),
			newManageIds:  sets.New(0, 1, 6),
		},
		// case 1
		{
			newGssReserveIds: sets.New(0, 2, 3),
			oldGssreserveIds: sets.New(0, 4, 5),
			notExistIds:      sets.New[int](),
			expectedReplicas: 3,
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "xxx-1",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.None),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "0",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "xxx-2",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.None),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "0",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "xxx-3",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.None),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "0",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "xxx-6",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.None),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "0",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "xxx-7",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.None),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "0",
						},
					},
				},
			},
			newReserveIds: sets.New(0, 2, 3, 4, 5),
			newManageIds:  sets.New(1, 6, 7),
		},
		// case 2
		{
			newGssReserveIds: sets.New(0),
			oldGssreserveIds: sets.New(0, 4, 5),
			notExistIds:      sets.New[int](),
			expectedReplicas: 1,
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "xxx-1",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.None),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "0",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "xxx-2",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.None),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "0",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "xxx-3",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.None),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "0",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "xxx-6",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.None),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "0",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "xxx-7",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.None),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "0",
						},
					},
				},
			},
			newReserveIds: sets.New(0, 2, 3, 4, 5, 6, 7),
			newManageIds:  sets.New(1),
		},
		// case 3
		{
			newGssReserveIds: sets.New(0, 2, 3),
			oldGssreserveIds: sets.New(0, 4, 5),
			notExistIds:      sets.New[int](),
			expectedReplicas: 4,
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "xxx-1",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.None),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "0",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "xxx-2",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.None),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "0",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "xxx-3",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.None),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "0",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "xxx-6",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.None),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "0",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "xxx-7",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.None),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "0",
						},
					},
				},
			},
			newReserveIds: sets.New(0, 2, 3, 5),
			newManageIds:  sets.New(1, 4, 6, 7),
		},
		// case 4
		{
			newGssReserveIds: sets.New(0, 3, 5),
			oldGssreserveIds: sets.New(0, 3, 5),
			notExistIds:      sets.New[int](),
			expectedReplicas: 1,
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "xxx-1",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.None),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "0",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "xxx-2",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.None),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "0",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "xxx-4",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.None),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "0",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "xxx-6",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.None),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "0",
						},
					},
				},
			},
			newReserveIds: sets.New(0, 3, 5, 2, 4, 6),
			newManageIds:  sets.New(1),
		},
		// case 5
		{
			newGssReserveIds: sets.New(1, 2),
			oldGssreserveIds: sets.New[int](),
			notExistIds:      sets.New(1, 2),
			expectedReplicas: 2,
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "xxx-0",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.None),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "0",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "xxx-3",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.None),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "0",
						},
					},
				},
			},
			newReserveIds: sets.New(1, 2),
			newManageIds:  sets.New(0, 3),
		},
		// case 6
		{
			newGssReserveIds: sets.New[int](),
			oldGssreserveIds: sets.New[int](),
			notExistIds:      sets.New[int](),
			expectedReplicas: 3,
			pods:             []corev1.Pod{},
			newReserveIds:    sets.New[int](),
			newManageIds:     sets.New(0, 1, 2),
		},
		// case 7
		{
			newGssReserveIds: sets.New(1, 2),
			oldGssreserveIds: sets.New[int](),
			notExistIds:      sets.New[int](),
			expectedReplicas: 3,
			pods:             []corev1.Pod{},
			newReserveIds:    sets.New(1, 2),
			newManageIds:     sets.New(0, 3, 4),
		},
		// case 8
		{
			newGssReserveIds: sets.New(0),
			oldGssreserveIds: sets.New[int](),
			notExistIds:      sets.New(0),
			expectedReplicas: 1,
			pods:             []corev1.Pod{},
			newReserveIds:    sets.New(0),
			newManageIds:     sets.New(1),
		},
		// case 9
		{
			newGssReserveIds: sets.New[int](),
			oldGssreserveIds: sets.New(1),
			notExistIds:      sets.New[int](),
			expectedReplicas: 2,
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "xxx-0",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.None),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "0",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "xxx-2",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.None),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "0",
						},
					},
				},
			},
			newReserveIds: sets.New(1),
			newManageIds:  sets.New(0, 2),
		},
		// case 10
		{
			newGssReserveIds: sets.New(0),
			oldGssreserveIds: sets.New[int](),
			notExistIds:      sets.New(2, 3, 4),
			expectedReplicas: 4,
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "xxx-1",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.None),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "0",
						},
					},
				},
			},
			newReserveIds: sets.New(0),
			newManageIds:  sets.New(1, 2, 3, 4),
		},
	}

	for i, test := range tests {
		t.Run(fmt.Sprintf("case %d", i), func(t *testing.T) {
			t.Logf("case %d : newGssReserveIds: %v ; oldGssreserveIds: %v ; notExistIds: %v ; expectedReplicas: %d; pods: %v", i, test.newGssReserveIds, test.oldGssreserveIds, test.notExistIds, test.expectedReplicas, test.pods)
			newManageIds, newReserveIds := computeToScaleGs(test.newGssReserveIds, test.oldGssreserveIds, test.notExistIds, test.expectedReplicas, test.pods)
			if !newReserveIds.Equal(test.newReserveIds) {
				t.Errorf("case %d: expect newReserveIds %v but got %v", i, test.newReserveIds, newReserveIds)
			}
			if !newManageIds.Equal(test.newManageIds) {
				t.Errorf("case %d: expect newManageIds %v but got %v", i, test.newManageIds, newManageIds)
			}
			t.Logf("case %d : newManageIds: %v ; newReserveIds: %v", i, newManageIds, newReserveIds)
		})
	}
}

func TestIsNeedToScale(t *testing.T) {
	tests := []struct {
		name   string
		gss    *gameKruiseV1alpha1.GameServerSet
		asts   *kruiseV1beta1.StatefulSet
		result bool
	}{
		{
			name: "case 0",
			gss: &gameKruiseV1alpha1.GameServerSet{
				Spec: gameKruiseV1alpha1.GameServerSetSpec{
					Replicas: ptr.To[int32](5),
				},
			},
			asts: &kruiseV1beta1.StatefulSet{
				Spec: kruiseV1beta1.StatefulSetSpec{
					Replicas: ptr.To[int32](5),
				},
				Status: kruiseV1beta1.StatefulSetStatus{
					Replicas: int32(5),
				},
			},
			result: false,
		},
		{
			name: "case 1",
			gss: &gameKruiseV1alpha1.GameServerSet{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{gameKruiseV1alpha1.GameServerSetReserveIdsKey: "1,5"},
				},
				Spec: gameKruiseV1alpha1.GameServerSetSpec{
					Replicas:             ptr.To[int32](5),
					ReserveGameServerIds: []intstr.IntOrString{intstr.FromInt(1), intstr.FromInt(5)},
				},
			},
			asts: &kruiseV1beta1.StatefulSet{
				Spec: kruiseV1beta1.StatefulSetSpec{
					Replicas: ptr.To[int32](5),
				},
				Status: kruiseV1beta1.StatefulSetStatus{
					Replicas: int32(5),
				},
			},
			result: false,
		},
		{
			name: "case 2",
			gss: &gameKruiseV1alpha1.GameServerSet{
				Spec: gameKruiseV1alpha1.GameServerSetSpec{
					Replicas:             ptr.To[int32](5),
					ReserveGameServerIds: []intstr.IntOrString{intstr.FromInt(1), intstr.FromInt(5)},
				},
			},
			asts: &kruiseV1beta1.StatefulSet{
				Spec: kruiseV1beta1.StatefulSetSpec{
					Replicas: ptr.To[int32](5),
				},
				Status: kruiseV1beta1.StatefulSetStatus{
					Replicas: int32(5),
				},
			},
			result: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager := &GameServerSetManager{
				gameServerSet: test.gss,
				asts:          test.asts,
			}
			actual := manager.IsNeedToScale()
			if actual != test.result {
				t.Errorf("expect spec %v but got %v", test.result, actual)
			}
		})
	}
}

func TestGameServerScale(t *testing.T) {
	recorder := record.NewFakeRecorder(100)

	tests := []struct {
		name           string
		gss            *gameKruiseV1alpha1.GameServerSet
		asts           *kruiseV1beta1.StatefulSet
		podList        []corev1.Pod
		astsReserveIds sets.Set[int]
		gssReserveIds  string
	}{
		{
			name: "case0: scale down without reserveIds",
			gss: &gameKruiseV1alpha1.GameServerSet{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   "xxx",
					Name:        "case0",
					Annotations: map[string]string{gameKruiseV1alpha1.GameServerSetReserveIdsKey: "1"},
				},
				Spec: gameKruiseV1alpha1.GameServerSetSpec{
					Replicas:             ptr.To[int32](3),
					ReserveGameServerIds: []intstr.IntOrString{intstr.FromInt(1)},
				},
			},
			asts: &kruiseV1beta1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "xxx",
					Name:      "case0",
				},
				Spec: kruiseV1beta1.StatefulSetSpec{
					Replicas:        ptr.To[int32](4),
					ReserveOrdinals: []intstr.IntOrString{intstr.FromInt(1)},
				},
				Status: kruiseV1beta1.StatefulSetStatus{
					Replicas: int32(4),
				},
			},
			podList: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "case0-0",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.None),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "10",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "case0-2",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.WaitToDelete),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "0",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "case0-3",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.None),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "0",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "case0-4",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.Maintaining),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "0",
						},
					},
				},
			},
			astsReserveIds: sets.New(1, 2),
			gssReserveIds:  "1",
		},
		{
			name: "case1: scale down with reserveIds",
			gss: &gameKruiseV1alpha1.GameServerSet{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   "xxx",
					Name:        "case1",
					Annotations: map[string]string{gameKruiseV1alpha1.GameServerSetReserveIdsKey: "1"},
				},
				Spec: gameKruiseV1alpha1.GameServerSetSpec{
					Replicas:             ptr.To[int32](3),
					ReserveGameServerIds: []intstr.IntOrString{intstr.FromInt(1), intstr.FromInt(0)},
				},
			},
			asts: &kruiseV1beta1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "xxx",
					Name:      "case1",
				},
				Spec: kruiseV1beta1.StatefulSetSpec{
					Replicas:        ptr.To[int32](4),
					ReserveOrdinals: []intstr.IntOrString{intstr.FromInt(1)},
				},
				Status: kruiseV1beta1.StatefulSetStatus{
					Replicas: int32(4),
				},
			},
			podList: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "case1-0",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.None),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "10",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "case1-2",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.WaitToDelete),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "0",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "case1-3",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.None),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "0",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "case1-4",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.Maintaining),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "0",
						},
					},
				},
			},
			astsReserveIds: sets.New(0, 1),
			gssReserveIds:  "0,1",
		},
		{
			name: "case2: scale up with reserveIds",
			gss: &gameKruiseV1alpha1.GameServerSet{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   "xxx",
					Name:        "case2",
					Annotations: map[string]string{gameKruiseV1alpha1.GameServerSetReserveIdsKey: "1"},
				},
				Spec: gameKruiseV1alpha1.GameServerSetSpec{
					Replicas:             ptr.To[int32](5),
					ReserveGameServerIds: []intstr.IntOrString{},
				},
			},
			asts: &kruiseV1beta1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "xxx",
					Name:      "case2",
				},
				Spec: kruiseV1beta1.StatefulSetSpec{
					Replicas:        ptr.To[int32](4),
					ReserveOrdinals: []intstr.IntOrString{intstr.FromInt(1)},
				},
				Status: kruiseV1beta1.StatefulSetStatus{
					Replicas: int32(4),
				},
			},
			podList: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "case2-0",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.None),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "10",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "case2-2",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.WaitToDelete),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "0",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "case2-3",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.None),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "0",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "case2-4",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.Maintaining),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "0",
						},
					},
				},
			},
			astsReserveIds: nil,
			gssReserveIds:  "",
		},
		{
			name: "case3: scale up with both reserveIds and others",
			gss: &gameKruiseV1alpha1.GameServerSet{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   "xxx",
					Name:        "case3",
					Annotations: map[string]string{gameKruiseV1alpha1.GameServerSetReserveIdsKey: "1"},
				},
				Spec: gameKruiseV1alpha1.GameServerSetSpec{
					Replicas:             ptr.To[int32](5),
					ReserveGameServerIds: []intstr.IntOrString{},
				},
			},
			asts: &kruiseV1beta1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "xxx",
					Name:      "case3",
				},
				Spec: kruiseV1beta1.StatefulSetSpec{
					Replicas:        ptr.To[int32](3),
					ReserveOrdinals: []intstr.IntOrString{intstr.FromInt(1), intstr.FromInt(3)},
				},
				Status: kruiseV1beta1.StatefulSetStatus{
					Replicas: int32(3),
				},
			},
			podList: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "case3-0",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.None),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "10",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "case3-2",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.WaitToDelete),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "0",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "case3-4",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.Maintaining),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "0",
						},
					},
				},
			},
			astsReserveIds: nil,
			gssReserveIds:  "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			objs := []client.Object{test.asts, test.gss}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
			manager := &GameServerSetManager{
				gameServerSet: test.gss,
				asts:          test.asts,
				podList:       test.podList,
				eventRecorder: recorder,
				client:        c,
			}

			if err := manager.GameServerScale(); err != nil {
				t.Error(err)
			}

			updateAsts := &kruiseV1beta1.StatefulSet{}
			if err := manager.client.Get(context.TODO(), types.NamespacedName{
				Namespace: test.asts.Namespace,
				Name:      test.asts.Name,
			}, updateAsts); err != nil {
				t.Error(err)
			}
			gotIds := util.GetReserveOrdinalIntSet(updateAsts.Spec.ReserveOrdinals)
			if !gotIds.Equal(test.astsReserveIds) {
				t.Errorf("expect asts ReserveOrdinals %v but got %v", test.astsReserveIds, gotIds)
			}

			updateGss := &gameKruiseV1alpha1.GameServerSet{}
			if err := manager.client.Get(context.TODO(), types.NamespacedName{
				Namespace: test.gss.Namespace,
				Name:      test.gss.Name,
			}, updateGss); err != nil {
				t.Error(err)
			}
			if updateGss.GetAnnotations()[gameKruiseV1alpha1.GameServerSetReserveIdsKey] != test.gssReserveIds {
				t.Errorf("expect asts ReserveOrdinals %v but got %v", test.gssReserveIds, updateGss.GetAnnotations()[gameKruiseV1alpha1.GameServerSetReserveIdsKey])
			}
		})
	}
}

func TestSyncGameServer(t *testing.T) {
	tests := []struct {
		gss           *gameKruiseV1alpha1.GameServerSet
		gsList        []*gameKruiseV1alpha1.GameServer
		newManageIds  sets.Set[int]
		oldManageIds  sets.Set[int]
		IdsLabelTure  []int
		IdsLabelFalse []int
	}{
		// case 0
		{
			gss: &gameKruiseV1alpha1.GameServerSet{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "xxx",
					Name:      "xxx",
				},
			},
			gsList: []*gameKruiseV1alpha1.GameServer{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "xxx",
						Name:      "xxx-0",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOwnerGssKey: "xxx",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "xxx",
						Name:      "xxx-2",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOwnerGssKey: "xxx",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "xxx",
						Name:      "xxx-3",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOwnerGssKey: "xxx",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "xxx",
						Name:      "xxx-4",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOwnerGssKey: "xxx",
						},
					},
				},
			},
			oldManageIds:  sets.New(0, 2, 3, 4),
			newManageIds:  sets.New(0, 1),
			IdsLabelTure:  []int{2, 3, 4},
			IdsLabelFalse: []int{},
		},

		// case 1
		{
			gss: &gameKruiseV1alpha1.GameServerSet{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "xxx",
					Name:      "xxx",
				},
			},
			gsList:        []*gameKruiseV1alpha1.GameServer{},
			oldManageIds:  sets.New[int](),
			newManageIds:  sets.New(0, 1, 3),
			IdsLabelTure:  []int{},
			IdsLabelFalse: []int{},
		},

		// case 2
		{
			gss: &gameKruiseV1alpha1.GameServerSet{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "xxx",
					Name:      "xxx",
				},
			},
			gsList: []*gameKruiseV1alpha1.GameServer{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "xxx",
						Name:      "xxx-0",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOwnerGssKey: "xxx",
							gameKruiseV1alpha1.GameServerDeletingKey: "true",
						},
					},
				},
			},
			oldManageIds:  sets.New[int](),
			newManageIds:  sets.New(0),
			IdsLabelTure:  []int{},
			IdsLabelFalse: []int{0},
		},
	}

	for i, test := range tests {
		objs := []client.Object{test.gss}
		for _, gs := range test.gsList {
			objs = append(objs, gs)
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
		if err := SyncGameServer(test.gss, c, test.newManageIds, test.oldManageIds); err != nil {
			t.Error(err)
		}

		for _, id := range test.IdsLabelTure {
			gs := &gameKruiseV1alpha1.GameServer{}
			if err := c.Get(context.Background(), types.NamespacedName{
				Namespace: test.gss.GetNamespace(),
				Name:      test.gss.GetName() + "-" + strconv.Itoa(id),
			}, gs); err != nil {
				t.Errorf("case %d: err: %s", i, err.Error())
			}
			if gs.GetLabels()[gameKruiseV1alpha1.GameServerDeletingKey] != "true" {
				t.Errorf("case %d: gs %d GameServerDeletingKey is not true", i, id)
			}
		}

		for _, id := range test.IdsLabelFalse {
			gs := &gameKruiseV1alpha1.GameServer{}
			if err := c.Get(context.Background(), types.NamespacedName{
				Namespace: test.gss.GetNamespace(),
				Name:      test.gss.GetName() + "-" + strconv.Itoa(id),
			}, gs); err != nil {
				t.Error(err)
			}
			if gs.GetLabels()[gameKruiseV1alpha1.GameServerDeletingKey] != "false" {
				t.Errorf("case %d: gs %d GameServerDeletingKey is not false", i, id)
			}
		}
	}
}

func TestNumberToKill(t *testing.T) {
	now := metav1.Now()
	tests := []struct {
		gss     *gameKruiseV1alpha1.GameServerSet
		asts    *kruiseV1beta1.StatefulSet
		podList []corev1.Pod
		number  int32
	}{
		// case 0
		{
			gss: &gameKruiseV1alpha1.GameServerSet{
				Spec: gameKruiseV1alpha1.GameServerSetSpec{
					Replicas: ptr.To[int32](3),
				},
			},
			asts: &kruiseV1beta1.StatefulSet{
				Spec: kruiseV1beta1.StatefulSetSpec{
					Replicas: ptr.To[int32](3),
				},
			},
			podList: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "xxx-0",
						Namespace: "xxx",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey: string(gameKruiseV1alpha1.Kill),
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "xxx-2",
						Namespace: "xxx",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "xxx-4",
						Namespace: "xxx",
					},
				},
			},
			number: 2,
		},
		// case 1
		{
			gss: &gameKruiseV1alpha1.GameServerSet{
				Spec: gameKruiseV1alpha1.GameServerSetSpec{
					Replicas: ptr.To[int32](3),
				},
				Status: gameKruiseV1alpha1.GameServerSetStatus{
					Replicas: int32(3),
				},
			},
			asts: &kruiseV1beta1.StatefulSet{
				Spec: kruiseV1beta1.StatefulSetSpec{
					Replicas: ptr.To[int32](3),
				},
			},
			podList: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "xxx-0",
						Namespace: "xxx",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey: string(gameKruiseV1alpha1.Kill),
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "xxx-2",
						Namespace:         "xxx",
						DeletionTimestamp: &now,
						Finalizers:        []string{"test"},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "xxx-4",
						Namespace: "xxx",
					},
				},
			},
			number: 3,
		},
		// case 2
		{
			gss: &gameKruiseV1alpha1.GameServerSet{
				Spec: gameKruiseV1alpha1.GameServerSetSpec{
					Replicas: ptr.To[int32](2),
				},
				Status: gameKruiseV1alpha1.GameServerSetStatus{
					Replicas: int32(2),
				},
			},
			asts: &kruiseV1beta1.StatefulSet{
				Spec: kruiseV1beta1.StatefulSetSpec{
					Replicas: ptr.To[int32](2),
				},
			},
			podList: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "xxx-0",
						Namespace: "xxx",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey: string(gameKruiseV1alpha1.Kill),
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "xxx-2",
						Namespace:         "xxx",
						DeletionTimestamp: &now,
						Finalizers:        []string{"test"},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "xxx-4",
						Namespace: "xxx",
					},
				},
			},
			number: 2,
		},
		// case 3
		{
			gss: &gameKruiseV1alpha1.GameServerSet{
				Spec: gameKruiseV1alpha1.GameServerSetSpec{
					Replicas: ptr.To[int32](4),
				},
				Status: gameKruiseV1alpha1.GameServerSetStatus{
					Replicas: int32(3),
				},
			},
			asts: &kruiseV1beta1.StatefulSet{
				Spec: kruiseV1beta1.StatefulSetSpec{
					Replicas: ptr.To[int32](3),
				},
			},
			podList: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "xxx-0",
						Namespace: "xxx",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey: string(gameKruiseV1alpha1.Kill),
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "xxx-2",
						Namespace:         "xxx",
						DeletionTimestamp: &now,
						Finalizers:        []string{"test"},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "xxx-4",
						Namespace: "xxx",
					},
				},
			},
			number: 4,
		},
	}

	for i, test := range tests {
		objs := []client.Object{test.gss}
		for _, pod := range test.podList {
			objs = append(objs, pod.DeepCopy())
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
		manager := &GameServerSetManager{
			podList:       test.podList,
			asts:          test.asts,
			gameServerSet: test.gss,
			client:        c,
		}
		actual := manager.GetReplicasAfterKilling()
		expect := test.number
		if *actual != expect {
			t.Errorf("case %d: expect gs replicas %v but actually %v", i, expect, *actual)
		}
	}
}

func TestGameServerSetManager_UpdateWorkload(t *testing.T) {
	tests := []struct {
		gss     *gameKruiseV1alpha1.GameServerSet
		asts    *kruiseV1beta1.StatefulSet
		newAsts *kruiseV1beta1.StatefulSet
	}{
		{
			gss: &gameKruiseV1alpha1.GameServerSet{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "xxx",
					Name:      "case0",
				},
				Spec: gameKruiseV1alpha1.GameServerSetSpec{
					GameServerTemplate: gameKruiseV1alpha1.GameServerTemplate{},
				},
			},
			asts: &kruiseV1beta1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   "xxx",
					Name:        "case0",
					Annotations: map[string]string{gameKruiseV1alpha1.AstsHashKey: "xx"},
				},
				Spec: kruiseV1beta1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Affinity: &corev1.Affinity{
								NodeAffinity: &corev1.NodeAffinity{
									PreferredDuringSchedulingIgnoredDuringExecution: []corev1.PreferredSchedulingTerm{
										{
											Weight: 1,
											Preference: corev1.NodeSelectorTerm{
												MatchFields: []corev1.NodeSelectorRequirement{
													{
														Key:      "role",
														Operator: corev1.NodeSelectorOpIn,
														Values:   []string{"test"},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			newAsts: &kruiseV1beta1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   "xxx",
					Name:        "case0",
					Annotations: map[string]string{gameKruiseV1alpha1.AstsHashKey: "xxx"},
				},
				Spec: kruiseV1beta1.StatefulSetSpec{
					ScaleStrategy: &kruiseV1beta1.StatefulSetScaleStrategy{
						MaxUnavailable: nil,
					},
					PodManagementPolicy: apps.ParallelPodManagement,
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{gameKruiseV1alpha1.GameServerOwnerGssKey: "case0"},
						},
						Spec: corev1.PodSpec{
							ReadinessGates: []corev1.PodReadinessGate{
								{
									ConditionType: appspub.InPlaceUpdateReady,
								},
							},
						},
					},
				},
			},
		},
	}
	recorder := record.NewFakeRecorder(100)

	for _, test := range tests {
		objs := []client.Object{test.asts, test.gss}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
		manager := &GameServerSetManager{
			gameServerSet: test.gss,
			asts:          test.asts,
			eventRecorder: recorder,
			client:        c,
		}

		if err := manager.UpdateWorkload(); err != nil {
			t.Error(err)
		}

		updateAsts := &kruiseV1beta1.StatefulSet{}
		if err := manager.client.Get(context.TODO(), types.NamespacedName{
			Namespace: test.asts.Namespace,
			Name:      test.asts.Name,
		}, updateAsts); err != nil {
			t.Error(err)
		}

		if !reflect.DeepEqual(updateAsts.Spec, test.newAsts.Spec) {
			t.Errorf("expect new asts spec %v but got %v", test.newAsts.Spec, updateAsts.Spec)
		}
	}
}

func TestGameServerSetManager_SyncPodProbeMarker(t *testing.T) {
	tests := []struct {
		name         string
		getGss       func() *gameKruiseV1alpha1.GameServerSet
		getPPM       func() *kruiseV1alpha1.PodProbeMarker
		newPPM       func() *kruiseV1alpha1.PodProbeMarker
		expectedDone bool
	}{
		{
			name: "first create PPM",
			getGss: func() *gameKruiseV1alpha1.GameServerSet {
				obj := &gameKruiseV1alpha1.GameServerSet{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "xxx",
						Name:      "case0",
					},
					Spec: gameKruiseV1alpha1.GameServerSetSpec{
						ServiceQualities: []gameKruiseV1alpha1.ServiceQuality{
							{
								Name:          "healthy",
								ContainerName: "main",
								Probe: corev1.Probe{
									ProbeHandler: corev1.ProbeHandler{
										Exec: &corev1.ExecAction{
											Command: []string{"/bin/sh", "-c", "/healthy.sh"},
										},
									},
								},
							},
						},
					},
				}
				return obj
			},
			getPPM: func() *kruiseV1alpha1.PodProbeMarker {
				return nil
			},
			newPPM: func() *kruiseV1alpha1.PodProbeMarker {
				obj := &kruiseV1alpha1.PodProbeMarker{
					Spec: kruiseV1alpha1.PodProbeMarkerSpec{
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"game.kruise.io/owner-gss": "case0",
							},
						},
						Probes: []kruiseV1alpha1.PodContainerProbe{
							{
								Name:             "healthy",
								ContainerName:    "main",
								PodConditionType: "game.kruise.io/healthy",
								Probe: kruiseV1alpha1.ContainerProbeSpec{
									Probe: corev1.Probe{
										ProbeHandler: corev1.ProbeHandler{
											Exec: &corev1.ExecAction{
												Command: []string{"/bin/sh", "-c", "/healthy.sh"},
											},
										},
										InitialDelaySeconds: DefaultInitialDelaySeconds,
										TimeoutSeconds:      DefaultTimeoutSeconds,
										PeriodSeconds:       DefaultPeriodSeconds,
										SuccessThreshold:    DefaultSuccessThreshold,
										FailureThreshold:    DefaultFailureThreshold,
									},
								},
							},
						},
					},
				}
				return obj
			},
			expectedDone: false,
		},
		{
			name: "second check PPM status, and false",
			getGss: func() *gameKruiseV1alpha1.GameServerSet {
				obj := &gameKruiseV1alpha1.GameServerSet{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "xxx",
						Name:      "case0",
					},
					Spec: gameKruiseV1alpha1.GameServerSetSpec{
						ServiceQualities: []gameKruiseV1alpha1.ServiceQuality{
							{
								Name:          "healthy",
								ContainerName: "main",
								Probe: corev1.Probe{
									ProbeHandler: corev1.ProbeHandler{
										Exec: &corev1.ExecAction{
											Command: []string{"/bin/sh", "-c", "/healthy.sh"},
										},
									},
								},
							},
						},
					},
				}
				return obj
			},
			getPPM: func() *kruiseV1alpha1.PodProbeMarker {
				obj := &kruiseV1alpha1.PodProbeMarker{
					ObjectMeta: metav1.ObjectMeta{
						Namespace:  "xxx",
						Name:       "case0",
						Generation: 1,
						Annotations: map[string]string{
							"game.kruise.io/ppm-hash": "3716291985",
						},
					},
					Spec: kruiseV1alpha1.PodProbeMarkerSpec{
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"game.kruise.io/owner-gss": "case0",
							},
						},
						Probes: []kruiseV1alpha1.PodContainerProbe{
							{
								Name:             "healthy",
								ContainerName:    "main",
								PodConditionType: "game.kruise.io/healthy",
								Probe: kruiseV1alpha1.ContainerProbeSpec{
									Probe: corev1.Probe{
										ProbeHandler: corev1.ProbeHandler{
											Exec: &corev1.ExecAction{
												Command: []string{"/bin/sh", "-c", "/healthy.sh"},
											},
										},
										InitialDelaySeconds: DefaultInitialDelaySeconds,
										TimeoutSeconds:      DefaultTimeoutSeconds,
										PeriodSeconds:       DefaultPeriodSeconds,
										SuccessThreshold:    DefaultSuccessThreshold,
										FailureThreshold:    DefaultFailureThreshold,
									},
								},
							},
						},
					},
				}
				return obj
			},
			newPPM: func() *kruiseV1alpha1.PodProbeMarker {
				obj := &kruiseV1alpha1.PodProbeMarker{
					Spec: kruiseV1alpha1.PodProbeMarkerSpec{
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"game.kruise.io/owner-gss": "case0",
							},
						},
						Probes: []kruiseV1alpha1.PodContainerProbe{
							{
								Name:             "healthy",
								ContainerName:    "main",
								PodConditionType: "game.kruise.io/healthy",
								Probe: kruiseV1alpha1.ContainerProbeSpec{
									Probe: corev1.Probe{
										ProbeHandler: corev1.ProbeHandler{
											Exec: &corev1.ExecAction{
												Command: []string{"/bin/sh", "-c", "/healthy.sh"},
											},
										},
										InitialDelaySeconds: DefaultInitialDelaySeconds,
										TimeoutSeconds:      DefaultTimeoutSeconds,
										PeriodSeconds:       DefaultPeriodSeconds,
										SuccessThreshold:    DefaultSuccessThreshold,
										FailureThreshold:    DefaultFailureThreshold,
									},
								},
							},
						},
					},
				}
				return obj
			},
			expectedDone: false,
		},
		{
			name: "third check PPM status, and true",
			getGss: func() *gameKruiseV1alpha1.GameServerSet {
				obj := &gameKruiseV1alpha1.GameServerSet{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "xxx",
						Name:      "case0",
					},
					Spec: gameKruiseV1alpha1.GameServerSetSpec{
						ServiceQualities: []gameKruiseV1alpha1.ServiceQuality{
							{
								Name:          "healthy",
								ContainerName: "main",
								Probe: corev1.Probe{
									ProbeHandler: corev1.ProbeHandler{
										Exec: &corev1.ExecAction{
											Command: []string{"/bin/sh", "-c", "/healthy.sh"},
										},
									},
								},
							},
						},
					},
				}
				return obj
			},
			getPPM: func() *kruiseV1alpha1.PodProbeMarker {
				obj := &kruiseV1alpha1.PodProbeMarker{
					ObjectMeta: metav1.ObjectMeta{
						Namespace:  "xxx",
						Name:       "case0",
						Generation: 1,
						Annotations: map[string]string{
							"game.kruise.io/ppm-hash": "3716291985",
						},
					},
					Spec: kruiseV1alpha1.PodProbeMarkerSpec{
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"game.kruise.io/owner-gss": "case0",
							},
						},
						Probes: []kruiseV1alpha1.PodContainerProbe{
							{
								Name:             "healthy",
								ContainerName:    "main",
								PodConditionType: "game.kruise.io/healthy",
								Probe: kruiseV1alpha1.ContainerProbeSpec{
									Probe: corev1.Probe{
										ProbeHandler: corev1.ProbeHandler{
											Exec: &corev1.ExecAction{
												Command: []string{"/bin/sh", "-c", "/healthy.sh"},
											},
										},
										InitialDelaySeconds: DefaultInitialDelaySeconds,
										TimeoutSeconds:      DefaultTimeoutSeconds,
										PeriodSeconds:       DefaultPeriodSeconds,
										SuccessThreshold:    DefaultSuccessThreshold,
										FailureThreshold:    DefaultFailureThreshold,
									},
								},
							},
						},
					},
					Status: kruiseV1alpha1.PodProbeMarkerStatus{
						ObservedGeneration: 1,
					},
				}
				return obj
			},
			newPPM: func() *kruiseV1alpha1.PodProbeMarker {
				obj := &kruiseV1alpha1.PodProbeMarker{
					Spec: kruiseV1alpha1.PodProbeMarkerSpec{
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"game.kruise.io/owner-gss": "case0",
							},
						},
						Probes: []kruiseV1alpha1.PodContainerProbe{
							{
								Name:             "healthy",
								ContainerName:    "main",
								PodConditionType: "game.kruise.io/healthy",
								Probe: kruiseV1alpha1.ContainerProbeSpec{
									Probe: corev1.Probe{
										ProbeHandler: corev1.ProbeHandler{
											Exec: &corev1.ExecAction{
												Command: []string{"/bin/sh", "-c", "/healthy.sh"},
											},
										},
										InitialDelaySeconds: DefaultInitialDelaySeconds,
										TimeoutSeconds:      DefaultTimeoutSeconds,
										PeriodSeconds:       DefaultPeriodSeconds,
										SuccessThreshold:    DefaultSuccessThreshold,
										FailureThreshold:    DefaultFailureThreshold,
									},
								},
							},
						},
					},
				}
				return obj
			},
			expectedDone: true,
		},
	}
	recorder := record.NewFakeRecorder(100)
	for _, test := range tests {
		gss := test.getGss()
		objs := []client.Object{gss}
		ppm := test.getPPM()
		if ppm != nil {
			objs = append(objs, ppm)
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
		manager := &GameServerSetManager{
			gameServerSet: gss,
			client:        c,
			eventRecorder: recorder,
		}

		err, done := manager.SyncPodProbeMarker()
		if err != nil {
			t.Errorf("SyncPodProbeMarker failed: %s", err.Error())
		} else if done != test.expectedDone {
			t.Errorf("expected(%v), but get(%v)", test.expectedDone, done)
		}
		newObj := &kruiseV1alpha1.PodProbeMarker{}
		if err = manager.client.Get(context.TODO(), types.NamespacedName{
			Namespace: gss.Namespace,
			Name:      gss.Name,
		}, newObj); err != nil {
			t.Error(err)
		}
		if !reflect.DeepEqual(newObj.Spec, test.newPPM().Spec) {
			t.Errorf("expect new asts spec %v but got %v", test.newPPM().Spec, newObj.Spec)
		}
	}
}
