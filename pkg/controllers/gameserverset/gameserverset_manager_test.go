package gameserverset

import (
	"context"
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
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
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
		newGssReserveIds      []int
		oldGssreserveIds      []int
		notExistIds           []int
		expectedReplicas      int
		scaleDownStrategyType gameKruiseV1alpha1.ScaleDownStrategyType
		pods                  []corev1.Pod
		newReserveIds         []int
		newManageIds          []int
	}{
		{
			newGssReserveIds:      []int{2, 3, 4},
			oldGssreserveIds:      []int{2, 3},
			notExistIds:           []int{5},
			expectedReplicas:      3,
			scaleDownStrategyType: gameKruiseV1alpha1.GeneralScaleDownStrategyType,
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
			newReserveIds: []int{2, 3, 4, 5},
			newManageIds:  []int{0, 1, 6},
		},
		{
			newGssReserveIds:      []int{0, 2, 3},
			oldGssreserveIds:      []int{0, 4, 5},
			notExistIds:           []int{},
			expectedReplicas:      3,
			scaleDownStrategyType: gameKruiseV1alpha1.GeneralScaleDownStrategyType,
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
			newReserveIds: []int{0, 2, 3, 4, 5},
			newManageIds:  []int{1, 6, 7},
		},
		{
			newGssReserveIds:      []int{0},
			oldGssreserveIds:      []int{0, 4, 5},
			notExistIds:           []int{},
			expectedReplicas:      1,
			scaleDownStrategyType: gameKruiseV1alpha1.GeneralScaleDownStrategyType,
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
			newReserveIds: []int{0},
			newManageIds:  []int{1},
		},
		{
			newGssReserveIds:      []int{0, 2, 3},
			oldGssreserveIds:      []int{0, 4, 5},
			notExistIds:           []int{},
			expectedReplicas:      4,
			scaleDownStrategyType: gameKruiseV1alpha1.GeneralScaleDownStrategyType,
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
			newReserveIds: []int{0, 2, 3, 5},
			newManageIds:  []int{1, 4, 6, 7},
		},
		{
			newGssReserveIds:      []int{0, 3, 5},
			oldGssreserveIds:      []int{0, 3, 5},
			notExistIds:           []int{},
			expectedReplicas:      1,
			scaleDownStrategyType: gameKruiseV1alpha1.ReserveIdsScaleDownStrategyType,
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
			newReserveIds: []int{0, 3, 5, 2, 4, 6},
			newManageIds:  []int{1},
		},
		{
			newGssReserveIds:      []int{1, 2},
			oldGssreserveIds:      []int{},
			notExistIds:           []int{1, 2},
			expectedReplicas:      2,
			scaleDownStrategyType: gameKruiseV1alpha1.GeneralScaleDownStrategyType,
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
			newReserveIds: []int{1, 2},
			newManageIds:  []int{0, 3},
		},
		{
			newGssReserveIds:      []int{},
			oldGssreserveIds:      []int{},
			notExistIds:           []int{},
			expectedReplicas:      3,
			scaleDownStrategyType: gameKruiseV1alpha1.GeneralScaleDownStrategyType,
			pods:                  []corev1.Pod{},
			newReserveIds:         []int{},
			newManageIds:          []int{0, 1, 2},
		},
		{
			newGssReserveIds:      []int{1, 2},
			oldGssreserveIds:      []int{},
			notExistIds:           []int{},
			expectedReplicas:      3,
			scaleDownStrategyType: gameKruiseV1alpha1.GeneralScaleDownStrategyType,
			pods:                  []corev1.Pod{},
			newReserveIds:         []int{1, 2},
			newManageIds:          []int{0, 3, 4},
		},
	}

	for i, test := range tests {
		newManageIds, newReserveIds := computeToScaleGs(test.newGssReserveIds, test.oldGssreserveIds, test.notExistIds, test.expectedReplicas, test.pods, test.scaleDownStrategyType)
		if !util.IsSliceEqual(newReserveIds, test.newReserveIds) {
			t.Errorf("case %d: expect newNotExistIds %v but got %v", i, test.newReserveIds, newReserveIds)
		}
		if !util.IsSliceEqual(newManageIds, test.newManageIds) {
			t.Errorf("case %d: expect newManageIds %v but got %v", i, test.newManageIds, newManageIds)
		}
	}
}

func TestIsNeedToScale(t *testing.T) {
	tests := []struct {
		gss    *gameKruiseV1alpha1.GameServerSet
		asts   *kruiseV1beta1.StatefulSet
		result bool
	}{
		{
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
			gss: &gameKruiseV1alpha1.GameServerSet{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{gameKruiseV1alpha1.GameServerSetReserveIdsKey: "1,5"},
				},
				Spec: gameKruiseV1alpha1.GameServerSetSpec{
					Replicas:             ptr.To[int32](5),
					ReserveGameServerIds: []int{1, 5},
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
	}
	for _, test := range tests {
		manager := &GameServerSetManager{
			gameServerSet: test.gss,
			asts:          test.asts,
		}
		actual := manager.IsNeedToScale()
		if actual != test.result {
			t.Errorf("expect spec %v but got %v", test.result, actual)
		}
	}
}

func TestGameServerScale(t *testing.T) {
	recorder := record.NewFakeRecorder(100)

	tests := []struct {
		gss            *gameKruiseV1alpha1.GameServerSet
		asts           *kruiseV1beta1.StatefulSet
		podList        []corev1.Pod
		astsReserveIds []int
		gssReserveIds  string
	}{
		// case0: scale down without reserveIds
		{
			gss: &gameKruiseV1alpha1.GameServerSet{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   "xxx",
					Name:        "case0",
					Annotations: map[string]string{gameKruiseV1alpha1.GameServerSetReserveIdsKey: "1"},
				},
				Spec: gameKruiseV1alpha1.GameServerSetSpec{
					Replicas:             ptr.To[int32](3),
					ReserveGameServerIds: []int{1},
				},
			},
			asts: &kruiseV1beta1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "xxx",
					Name:      "case0",
				},
				Spec: kruiseV1beta1.StatefulSetSpec{
					Replicas:        ptr.To[int32](4),
					ReserveOrdinals: []int{1},
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
			astsReserveIds: []int{1, 2},
			gssReserveIds:  "1",
		},
		// case1: scale down with reserveIds
		{
			gss: &gameKruiseV1alpha1.GameServerSet{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   "xxx",
					Name:        "case1",
					Annotations: map[string]string{gameKruiseV1alpha1.GameServerSetReserveIdsKey: "1"},
				},
				Spec: gameKruiseV1alpha1.GameServerSetSpec{
					Replicas:             ptr.To[int32](3),
					ReserveGameServerIds: []int{1, 0},
				},
			},
			asts: &kruiseV1beta1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "xxx",
					Name:      "case1",
				},
				Spec: kruiseV1beta1.StatefulSetSpec{
					Replicas:        ptr.To[int32](4),
					ReserveOrdinals: []int{1},
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
			astsReserveIds: []int{1, 0},
			gssReserveIds:  "1,0",
		},
		// case2: scale up with reserveIds
		{
			gss: &gameKruiseV1alpha1.GameServerSet{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   "xxx",
					Name:        "case2",
					Annotations: map[string]string{gameKruiseV1alpha1.GameServerSetReserveIdsKey: "1"},
				},
				Spec: gameKruiseV1alpha1.GameServerSetSpec{
					Replicas:             ptr.To[int32](5),
					ReserveGameServerIds: []int{},
				},
			},
			asts: &kruiseV1beta1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "xxx",
					Name:      "case2",
				},
				Spec: kruiseV1beta1.StatefulSetSpec{
					Replicas:        ptr.To[int32](4),
					ReserveOrdinals: []int{1},
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
		// case3: scale up with both reserveIds and others
		{
			gss: &gameKruiseV1alpha1.GameServerSet{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   "xxx",
					Name:        "case3",
					Annotations: map[string]string{gameKruiseV1alpha1.GameServerSetReserveIdsKey: "1"},
				},
				Spec: gameKruiseV1alpha1.GameServerSetSpec{
					Replicas:             ptr.To[int32](5),
					ReserveGameServerIds: []int{},
				},
			},
			asts: &kruiseV1beta1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "xxx",
					Name:      "case3",
				},
				Spec: kruiseV1beta1.StatefulSetSpec{
					Replicas:        ptr.To[int32](3),
					ReserveOrdinals: []int{1, 3},
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
		if !util.IsSliceEqual(updateAsts.Spec.ReserveOrdinals, test.astsReserveIds) {
			t.Errorf("expect asts ReserveOrdinals %v but got %v", test.astsReserveIds, updateAsts.Spec.ReserveOrdinals)
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
	}
}

func TestSyncGameServer(t *testing.T) {
	tests := []struct {
		gss           *gameKruiseV1alpha1.GameServerSet
		gsList        []*gameKruiseV1alpha1.GameServer
		newManageIds  []int
		oldManageIds  []int
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
			oldManageIds:  []int{0, 2, 3, 4},
			newManageIds:  []int{0, 1},
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
			oldManageIds:  []int{},
			newManageIds:  []int{0, 1, 3},
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
			oldManageIds:  []int{},
			newManageIds:  []int{0},
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
