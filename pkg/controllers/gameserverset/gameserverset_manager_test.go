package gameserverset

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/go-logr/logr/testr"
	appspub "github.com/openkruise/kruise-api/apps/pub"
	kruiseV1alpha1 "github.com/openkruise/kruise-api/apps/v1alpha1"
	kruiseV1beta1 "github.com/openkruise/kruise-api/apps/v1beta1"
	apps "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
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
	timeNow := metav1.Now()

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
		// case 11
		{
			newGssReserveIds: sets.New[int](),
			oldGssreserveIds: sets.New[int](),
			notExistIds:      sets.New[int](2, 3, 4),
			expectedReplicas: 4,
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
							appspub.LifecycleStateKey:                      string(appspub.LifecycleStatePreparingDelete),
						},
						DeletionTimestamp: &timeNow,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "xxx-3",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.None),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "0",
							appspub.LifecycleStateKey:                      string(appspub.LifecycleStatePreparingDelete),
						},
					},
				},
			},
			newReserveIds: sets.New(3),
			newManageIds:  sets.New(0, 1, 2, 4),
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
				logger:        testr.New(t),
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
	//timeNow := metav1.Now()

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
		{
			name: "case4: scale up when pods with PreDelete state",
			gss: &gameKruiseV1alpha1.GameServerSet{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "xxx",
					Name:      "case4",
				},
				Spec: gameKruiseV1alpha1.GameServerSetSpec{
					Replicas:             ptr.To[int32](5),
					ReserveGameServerIds: []intstr.IntOrString{},
				},
			},
			asts: &kruiseV1beta1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "xxx",
					Name:      "case4",
				},
				Spec: kruiseV1beta1.StatefulSetSpec{
					Replicas:        ptr.To[int32](3),
					ReserveOrdinals: []intstr.IntOrString{intstr.FromInt(3), intstr.FromInt(4), intstr.FromInt(5)},
				},
			},
			podList: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "case4-0",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.None),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "0",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "case4-1",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.None),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "0",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "case4-2",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.None),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "0",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "case4-3",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.None),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "0",
							appspub.LifecycleStateKey:                      string(appspub.LifecycleStatePreparingDelete),
						},
					},
				},
			},
			astsReserveIds: sets.New(3),
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
				logger:        testr.New(t),
			}

			if err := manager.GameServerScale(context.Background()); err != nil {
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
		if err := SyncGameServer(context.Background(), testr.New(t), test.gss, c, test.newManageIds, test.oldManageIds); err != nil {
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
			logger:        testr.New(t),
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
			logger:        testr.New(t),
		}

		if err := manager.UpdateWorkload(context.Background()); err != nil {
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
								gameKruiseV1alpha1.GameServerOwnerGssKey: "case0",
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
								gameKruiseV1alpha1.GameServerOwnerGssKey: "case0",
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
								gameKruiseV1alpha1.GameServerOwnerGssKey: "case0",
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
								gameKruiseV1alpha1.GameServerOwnerGssKey: "case0",
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
								gameKruiseV1alpha1.GameServerOwnerGssKey: "case0",
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
			logger:        testr.New(t),
		}

		ctx := context.Background()
		err, done := manager.SyncPodProbeMarker(ctx)
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

func TestGatedScaleDownAdjustsTargetPartition(t *testing.T) {
	for _, replicas := range []int32{3, 0} {
		t.Run(fmt.Sprintf("replicas-%d", replicas), func(t *testing.T) {
			ctx := context.Background()
			gss := testGatedGSS()
			gss.Spec.Replicas = ptr.To[int32](10)
			gss.Spec.UpdateStrategy.RollingUpdate.Partition = ptr.To[int32](5)
			gss.Status.UpdateProgress = &gameKruiseV1alpha1.GameServerSetUpdateProgress{UpdateGeneration: gss.Generation, StartedAt: metav1.Now()}
			gss.Status.Conditions = []metav1.Condition{{Type: updateProgressingCondition, Status: metav1.ConditionTrue, Reason: "Updating", LastTransitionTime: metav1.Now()}}
			asts := util.GetNewAstsFromGss(gss.DeepCopy(), &kruiseV1beta1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: gss.Name, Namespace: gss.Namespace}})
			asts.Spec.Replicas = ptr.To[int32](10)
			asts.Spec.UpdateStrategy.RollingUpdate.Partition = ptr.To[int32](6)
			asts.Status.UpdateRevision = "revision"
			setAstsHash(asts, gss)
			gss.Spec.Replicas = ptr.To(replicas)
			gss.Spec.UpdateStrategy.RollingUpdate.Partition = ptr.To(replicas)
			gss.Generation++
			c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&gameKruiseV1alpha1.GameServerSet{}, &kruiseV1beta1.StatefulSet{}).WithObjects(gss, asts).Build()
			manager := NewGameServerSetManager(gss, c, record.NewFakeRecorder(10), logr.Discard())
			manager.SyncStsAndPodList(asts, nil)
			if err := manager.GameServerScale(ctx); err != nil {
				t.Fatal(err)
			}
			key := client.ObjectKeyFromObject(gss)
			if err := c.Get(ctx, key, asts); err != nil {
				t.Fatal(err)
			}
			if *asts.Spec.Replicas != replicas || *asts.Spec.UpdateStrategy.RollingUpdate.Partition != replicas {
				t.Fatal("scale down must keep the partition within the new replica count without opening new slots")
			}
			pods := make([]corev1.Pod, int(replicas))
			r := &GameServerSetReconciler{Client: c}
			for i := 0; i < 2; i++ {
				if _, _, err := r.reconcileGatedUpdate(ctx, gss, asts, pods); err != nil {
					t.Fatal(err)
				}
				if err := c.Get(ctx, key, asts); err != nil {
					t.Fatal(err)
				}
			}
			if gss.Status.Conditions[0].Reason != "Completed" || gss.Status.UpdateProgress.UpdateGeneration != 3 {
				t.Fatalf("scale down must converge without invalidating the round: %+v", gss.Status)
			}
		})
	}
}

func TestWorkloadPartitionSyncWithoutGatedRound(t *testing.T) {
	for _, tc := range []struct {
		name          string
		withoutPolicy bool
		activeRound   bool
		wantUpdate    bool
	}{
		{name: "policy added without a round", wantUpdate: true},
		{name: "active round owns partition", activeRound: true},
		{name: "unconfigured keeps legacy behavior", withoutPolicy: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gss := testGatedGSS()
			if tc.withoutPolicy {
				gss.Spec.UpdateStrategy.RollingUpdate.CompletionPolicy = nil
			}
			if tc.activeRound {
				gss.Status.UpdateProgress = &gameKruiseV1alpha1.GameServerSetUpdateProgress{UpdateGeneration: gss.Generation}
			}
			asts := util.GetNewAstsFromGss(gss.DeepCopy(), &kruiseV1beta1.StatefulSet{})
			setAstsHash(asts, gss)
			asts.Spec.UpdateStrategy.RollingUpdate.Partition = ptr.To[int32](1)
			manager := NewGameServerSetManager(gss, nil, nil, logr.Discard())
			manager.SyncStsAndPodList(asts, nil)
			if manager.IsNeedToUpdateWorkload() != tc.wantUpdate {
				t.Fatalf("unexpected partition ownership: want update=%v", tc.wantUpdate)
			}
		})
	}
}

func TestSyncStatusPreservesConcurrentUpdateProgress(t *testing.T) {
	ctx := context.Background()
	gss := testGatedGSS()
	client := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&gameKruiseV1alpha1.GameServerSet{}).WithObjects(gss).Build()
	current := &gameKruiseV1alpha1.GameServerSet{}
	key := types.NamespacedName{Name: gss.Name, Namespace: gss.Namespace}
	if err := client.Get(ctx, key, current); err != nil {
		t.Fatal(err)
	}
	current.Status.UpdateProgress = &gameKruiseV1alpha1.GameServerSetUpdateProgress{UpdateGeneration: gss.Generation, StartedAt: metav1.Now()}
	if err := client.Status().Update(ctx, current); err != nil {
		t.Fatal(err)
	}
	manager := NewGameServerSetManager(gss, client, nil, logr.Discard())
	manager.SyncStsAndPodList(&kruiseV1beta1.StatefulSet{Status: kruiseV1beta1.StatefulSetStatus{ReadyReplicas: 1}}, nil)
	if err := manager.SyncStatus(ctx); err != nil {
		t.Fatal(err)
	}
	if err := client.Get(ctx, key, current); err != nil {
		t.Fatal(err)
	}
	if current.Status.UpdateProgress == nil || current.Status.UpdateProgress.UpdateGeneration != gss.Generation || current.Status.ReadyReplicas != 1 {
		t.Fatalf("status sync lost progress or replicas: %+v", current.Status)
	}
}

func TestSyncStatusPreservesGatedProgress(t *testing.T) {
	ctx := context.Background()
	gss := testGatedGSS()
	gss.Status.UpdateProgress = &gameKruiseV1alpha1.GameServerSetUpdateProgress{UpdateGeneration: 3, StartedAt: metav1.Now(), PendingCompletionReplicas: 1}
	gss.Status.Conditions = []metav1.Condition{{Type: updateProgressingCondition, Status: metav1.ConditionTrue, Reason: "Updating", LastTransitionTime: metav1.Now()}}
	asts := &kruiseV1beta1.StatefulSet{Status: kruiseV1beta1.StatefulSetStatus{ReadyReplicas: 1}}
	client := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&gameKruiseV1alpha1.GameServerSet{}).WithObjects(gss).Build()
	manager := NewGameServerSetManager(gss, client, nil, logr.Discard())
	manager.SyncStsAndPodList(asts, nil)
	if err := manager.SyncStatus(ctx); err != nil {
		t.Fatal(err)
	}
	stored := &gameKruiseV1alpha1.GameServerSet{}
	if err := client.Get(ctx, types.NamespacedName{Name: gss.Name, Namespace: gss.Namespace}, stored); err != nil {
		t.Fatal(err)
	}
	if stored.Status.UpdateProgress == nil || stored.Status.UpdateProgress.PendingCompletionReplicas != 1 || len(stored.Status.Conditions) != 1 {
		t.Fatal("ordinary GSS status synchronization must preserve gated progress and conditions")
	}
}

func TestSyncStatusWithoutPolicyPreservesLegacyPatchBehavior(t *testing.T) {
	ctx := context.Background()
	gss := testGatedGSS()
	gss.Spec.UpdateStrategy.RollingUpdate.CompletionPolicy = nil
	gss.Status.Conditions = []metav1.Condition{{Type: "OtherCondition", Status: metav1.ConditionTrue, Reason: "Ready", LastTransitionTime: metav1.Now()}}
	asts := &kruiseV1beta1.StatefulSet{}
	client := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&gameKruiseV1alpha1.GameServerSet{}).WithObjects(gss).Build()
	manager := NewGameServerSetManager(gss, client, nil, logr.Discard())
	manager.SyncStsAndPodList(asts, nil)
	if err := manager.SyncStatus(ctx); err != nil {
		t.Fatal(err)
	}
	stored := &gameKruiseV1alpha1.GameServerSet{}
	if err := client.Get(ctx, types.NamespacedName{Name: gss.Name, Namespace: gss.Namespace}, stored); err != nil {
		t.Fatal(err)
	}
	if stored.Status.Replicas != 2 || stored.Status.UpdateProgress != nil || !reflect.DeepEqual(stored.Status.Conditions, gss.Status.Conditions) {
		t.Fatal("legacy merge patch must update counters without changing unrelated conditions")
	}
}

func TestSyncStatusWithoutPolicyClearsGatedStatus(t *testing.T) {
	for _, tc := range []struct {
		name          string
		withProgress  bool
		withCondition bool
	}{
		{name: "progress and condition", withProgress: true, withCondition: true},
		{name: "progress only", withProgress: true},
		{name: "condition only", withCondition: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			gss := testGatedGSS()
			gss.Spec.UpdateStrategy.RollingUpdate.CompletionPolicy = nil
			c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&gameKruiseV1alpha1.GameServerSet{}).WithObjects(gss).Build()
			manager := NewGameServerSetManager(gss, c, nil, logr.Discard())
			manager.SyncStsAndPodList(&kruiseV1beta1.StatefulSet{}, nil)
			if err := manager.SyncStatus(ctx); err != nil {
				t.Fatal(err)
			}
			if err := c.Get(ctx, client.ObjectKeyFromObject(gss), gss); err != nil {
				t.Fatal(err)
			}
			other := metav1.Condition{Type: "OtherCondition", Status: metav1.ConditionTrue, Reason: "Ready", LastTransitionTime: metav1.NewTime(time.Unix(100, 0))}
			gss.Status.Conditions = []metav1.Condition{other}
			if tc.withProgress {
				gss.Status.UpdateProgress = &gameKruiseV1alpha1.GameServerSetUpdateProgress{UpdateGeneration: gss.Generation, PendingCompletionReplicas: 1}
			}
			if tc.withCondition {
				gss.Status.Conditions = append(gss.Status.Conditions, metav1.Condition{Type: updateProgressingCondition, Status: metav1.ConditionTrue, Reason: "WaitingForBusinessObservation", LastTransitionTime: metav1.Now()})
			}
			if err := c.Status().Update(ctx, gss); err != nil {
				t.Fatal(err)
			}
			manager = NewGameServerSetManager(gss, c, nil, logr.Discard())
			manager.SyncStsAndPodList(&kruiseV1beta1.StatefulSet{}, nil)
			if err := manager.SyncStatus(ctx); err != nil {
				t.Fatal(err)
			}
			if err := c.Get(ctx, client.ObjectKeyFromObject(gss), gss); err != nil {
				t.Fatal(err)
			}
			if gss.Status.UpdateProgress != nil || !equality.Semantic.DeepEqual(gss.Status.Conditions, []metav1.Condition{other}) {
				t.Fatalf("cancellation must clear only gated status even with unchanged counters: %+v", gss.Status)
			}
			version := gss.ResourceVersion
			if err := manager.SyncStatus(ctx); err != nil {
				t.Fatal(err)
			}
			if err := c.Get(ctx, client.ObjectKeyFromObject(gss), gss); err != nil {
				t.Fatal(err)
			}
			if gss.ResourceVersion != version {
				t.Fatal("cleanup must not keep writing unchanged status")
			}
		})
	}
}

func TestSyncStatusCancellationSkipsNewerGeneration(t *testing.T) {
	ctx := context.Background()
	stale := testGatedGSS()
	stale.Spec.UpdateStrategy.RollingUpdate.CompletionPolicy = nil
	stale.Status.UpdateProgress = &gameKruiseV1alpha1.GameServerSetUpdateProgress{UpdateGeneration: stale.Generation}
	current := testGatedGSS()
	current.Generation++
	current.Status.UpdateProgress = &gameKruiseV1alpha1.GameServerSetUpdateProgress{UpdateGeneration: current.Generation, PendingCompletionReplicas: 1}
	current.Status.Conditions = []metav1.Condition{{Type: updateProgressingCondition, Status: metav1.ConditionTrue, Reason: "Updating", LastTransitionTime: metav1.NewTime(time.Unix(100, 0))}}
	want := current.DeepCopy()
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&gameKruiseV1alpha1.GameServerSet{}).WithObjects(current).Build()
	manager := NewGameServerSetManager(stale, c, nil, logr.Discard())
	manager.SyncStsAndPodList(&kruiseV1beta1.StatefulSet{}, nil)
	if err := manager.SyncStatus(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(current), current); err != nil {
		t.Fatal(err)
	}
	if !equality.Semantic.DeepEqual(current.Status, want.Status) {
		t.Fatal("a stale cancellation must not clear progress from a newer rollout")
	}
}

func TestLegacyWorkloadUpdateDoesNotStartGatedRoundWhenPolicyAdded(t *testing.T) {
	gss := testGatedGSS()
	asts := util.GetNewAstsFromGss(gss.DeepCopy(), &kruiseV1beta1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: gss.Name, Namespace: gss.Namespace}})
	asts.Spec.Replicas = ptr.To[int32](2)
	setAstsHash(asts, gss)
	gss.Spec.UpdateStrategy.RollingUpdate.CompletionPolicy = nil
	gss.Spec.GameServerTemplate.Spec.Containers[0].Image = "v3"
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gss, asts).Build()
	manager := NewGameServerSetManager(gss, c, nil, logr.Discard())
	manager.SyncStsAndPodList(asts, nil)
	ctx := context.Background()
	if err := manager.UpdateWorkload(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, types.NamespacedName{Name: gss.Name, Namespace: gss.Namespace}, asts); err != nil {
		t.Fatal(err)
	}
	if _, exists := asts.Annotations[gatedTemplateHashKey]; exists || asts.Spec.Template.Spec.Containers[0].Image != "v3" {
		t.Fatal("legacy template update must not retain a stale gated template fingerprint")
	}
	gss.Spec.UpdateStrategy.RollingUpdate.CompletionPolicy = testGatedGSS().Spec.UpdateStrategy.RollingUpdate.CompletionPolicy
	r := &GameServerSetReconciler{Client: c}
	// First reconcile initializes the fingerprint; the next returns to legacy.
	for i := 0; i < 2; i++ {
		handled, _, err := r.reconcileGatedUpdate(ctx, gss, asts, nil)
		if err != nil || (i == 1 && handled) || gss.Status.UpdateProgress != nil {
			t.Fatalf("adding a policy alone must not start a rollout, step=%d handled=%v err=%v", i, handled, err)
		}
		if err := c.Get(ctx, types.NamespacedName{Name: gss.Name, Namespace: gss.Namespace}, asts); err != nil {
			t.Fatal(err)
		}
	}
}

type statusPatchTrackingClient struct {
	client.Client
	patches   int
	lastPatch []byte
}

func (c *statusPatchTrackingClient) Status() client.SubResourceWriter {
	return &statusPatchTrackingWriter{SubResourceWriter: c.Client.Status(), owner: c}
}

type statusPatchTrackingWriter struct {
	client.SubResourceWriter
	owner *statusPatchTrackingClient
}

func (w *statusPatchTrackingWriter) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
	w.owner.patches++
	data, err := patch.Data(obj)
	if err != nil {
		return err
	}
	w.owner.lastPatch = data
	return w.SubResourceWriter.Patch(ctx, obj, patch, opts...)
}

func TestSyncStatusWithoutPolicySkipsUnchangedCounters(t *testing.T) {
	for _, residual := range []bool{false, true} {
		name := "never-configured"
		if residual {
			name = "policy-removed"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			gss := testGatedGSS()
			gss.Spec.UpdateStrategy.RollingUpdate.CompletionPolicy = nil
			if residual {
				gss.Status.UpdateProgress = &gameKruiseV1alpha1.GameServerSetUpdateProgress{UpdateGeneration: 3, StartedAt: metav1.Now(), CompletedReplicas: 2}
				gss.Status.Conditions = []metav1.Condition{{Type: updateProgressingCondition, Status: metav1.ConditionFalse, Reason: "Completed"}}
			}
			c := &statusPatchTrackingClient{Client: fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&gameKruiseV1alpha1.GameServerSet{}).WithObjects(gss).Build()}
			manager := NewGameServerSetManager(gss, c, nil, logr.Discard())
			manager.SyncStsAndPodList(&kruiseV1beta1.StatefulSet{}, nil)
			if err := manager.SyncStatus(ctx); err != nil {
				t.Fatal(err)
			}
			if err := c.Get(ctx, client.ObjectKeyFromObject(gss), gss); err != nil {
				t.Fatal(err)
			}
			if gss.Status.UpdateProgress != nil || len(gss.Status.Conditions) != 0 {
				t.Fatal("policy removal must clear gated status before ordinary counter synchronization")
			}
			c.patches = 0
			manager = NewGameServerSetManager(gss, c, nil, logr.Discard())
			manager.SyncStsAndPodList(&kruiseV1beta1.StatefulSet{}, nil)
			if err := manager.SyncStatus(ctx); err != nil {
				t.Fatal(err)
			}
			if c.patches != 0 {
				t.Fatalf("unchanged counters still issued %d status patches after policy removal", c.patches)
			}
			asts := &kruiseV1beta1.StatefulSet{}
			manager.SyncStsAndPodList(asts, nil)
			for _, ready := range []int32{1, 0} {
				asts.Status.ReadyReplicas = ready
				c.patches = 0
				if err := manager.SyncStatus(ctx); err != nil {
					t.Fatal(err)
				}
				if c.patches != 1 {
					t.Fatalf("changed counters issued %d patches, want 1", c.patches)
				}
				var payload map[string]map[string]interface{}
				if err := json.Unmarshal(c.lastPatch, &payload); err != nil {
					t.Fatal(err)
				}
				for _, field := range []string{"updateProgress", "conditions"} {
					if _, exists := payload["status"][field]; exists {
						t.Fatalf("comparison-only field %s leaked into patch", field)
					}
				}
				if err := c.Get(ctx, client.ObjectKeyFromObject(gss), gss); err != nil {
					t.Fatal(err)
				}
				if gss.Status.ReadyReplicas != ready {
					t.Fatalf("ready=%d, want %d", gss.Status.ReadyReplicas, ready)
				}
				if gss.Status.UpdateProgress != nil || len(gss.Status.Conditions) != 0 {
					t.Fatal("counter sync must not restore cancelled progress")
				}
				c.patches = 0
				if err := manager.SyncStatus(ctx); err != nil {
					t.Fatal(err)
				}
				if c.patches != 0 {
					t.Fatalf("stable counters still issued %d patches", c.patches)
				}
			}
		})
	}
}
