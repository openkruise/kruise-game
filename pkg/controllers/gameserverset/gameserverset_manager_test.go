package gameserverset

import (
	"context"
	kruiseV1alpha1 "github.com/openkruise/kruise-api/apps/v1alpha1"
	kruiseV1beta1 "github.com/openkruise/kruise-api/apps/v1beta1"
	gameKruiseV1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/pkg/util"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"testing"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(gameKruiseV1alpha1.AddToScheme(scheme))
	utilruntime.Must(kruiseV1beta1.AddToScheme(scheme))
	utilruntime.Must(kruiseV1alpha1.AddToScheme(scheme))
}

func TestComputeToScaleGs(t *testing.T) {
	tests := []struct {
		newGssReserveIds []int
		oldGssreserveIds []int
		notExistIds      []int
		expectedReplicas int
		pods             []corev1.Pod
		newNotExistIds   []int
	}{
		{
			newGssReserveIds: []int{2, 3, 4},
			oldGssreserveIds: []int{2, 3},
			notExistIds:      []int{5},
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
			newNotExistIds: []int{5},
		},
	}

	for _, test := range tests {
		newNotExistIds := computeToScaleGs(test.newGssReserveIds, test.oldGssreserveIds, test.notExistIds, test.expectedReplicas, test.pods)
		if !util.IsSliceEqual(newNotExistIds, test.newNotExistIds) {
			t.Errorf("expect newNotExistIds %v but got %v", test.newNotExistIds, newNotExistIds)
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
					Replicas: pointer.Int32(5),
				},
			},
			asts: &kruiseV1beta1.StatefulSet{
				Spec: kruiseV1beta1.StatefulSetSpec{
					Replicas: pointer.Int32(5),
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
					Replicas:             pointer.Int32(5),
					ReserveGameServerIds: []int{1, 5},
				},
			},
			asts: &kruiseV1beta1.StatefulSet{
				Spec: kruiseV1beta1.StatefulSetSpec{
					Replicas: pointer.Int32(5),
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
					Replicas:             pointer.Int32(3),
					ReserveGameServerIds: []int{1},
				},
			},
			asts: &kruiseV1beta1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "xxx",
					Name:      "case0",
				},
				Spec: kruiseV1beta1.StatefulSetSpec{
					Replicas:        pointer.Int32(4),
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
					Replicas:             pointer.Int32(3),
					ReserveGameServerIds: []int{1, 0},
				},
			},
			asts: &kruiseV1beta1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "xxx",
					Name:      "case1",
				},
				Spec: kruiseV1beta1.StatefulSetSpec{
					Replicas:        pointer.Int32(4),
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
					Replicas:             pointer.Int32(5),
					ReserveGameServerIds: []int{},
				},
			},
			asts: &kruiseV1beta1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "xxx",
					Name:      "case2",
				},
				Spec: kruiseV1beta1.StatefulSetSpec{
					Replicas:        pointer.Int32(4),
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
					Replicas:             pointer.Int32(5),
					ReserveGameServerIds: []int{},
				},
			},
			asts: &kruiseV1beta1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "xxx",
					Name:      "case3",
				},
				Spec: kruiseV1beta1.StatefulSetSpec{
					Replicas:        pointer.Int32(3),
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

func TestSyncGameServerReplicas(t *testing.T) {
	tests := []struct {
		gss      *gameKruiseV1alpha1.GameServerSet
		podList  []corev1.Pod
		gsList   []*gameKruiseV1alpha1.GameServer
		toDelete []int
	}{
		{
			gss: &gameKruiseV1alpha1.GameServerSet{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "xxx",
					Name:      "xxx",
				},
			},
			podList: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "xxx-0",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "xxx-2",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "xxx-4",
					},
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
			toDelete: []int{3},
		},
	}

	for _, test := range tests {
		objs := []client.Object{test.gss}
		for _, gs := range test.gsList {
			objs = append(objs, gs)
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
		recorder := record.NewFakeRecorder(100)
		manager := &GameServerSetManager{
			gameServerSet: test.gss,
			podList:       test.podList,
			eventRecorder: recorder,
			client:        c,
		}
		if err := manager.SyncGameServerReplicas(); err != nil {
			t.Error(err)
		}

		gsList := &gameKruiseV1alpha1.GameServerList{}
		if err := manager.client.List(context.Background(), gsList, &client.ListOptions{
			Namespace: test.gss.GetNamespace(),
			LabelSelector: labels.SelectorFromSet(map[string]string{
				gameKruiseV1alpha1.GameServerOwnerGssKey: test.gss.GetName(),
				gameKruiseV1alpha1.GameServerDeletingKey: "true",
			})}); err != nil {
			t.Error(err)
		}

		actual := util.GetIndexListFromGsList(gsList.Items)
		expect := test.toDelete
		if !util.IsSliceEqual(actual, expect) {
			t.Errorf("expect to delete gameservers %v but actually %v", expect, actual)
		}
	}
}
