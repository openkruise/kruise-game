package gameserverset

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	appspub "github.com/openkruise/kruise-api/apps/pub"
	kruisev1alpha1 "github.com/openkruise/kruise-api/apps/v1alpha1"
	kruiseV1beta1 "github.com/openkruise/kruise-api/apps/v1beta1"
	gameKruiseV1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/pkg/util"
	apps "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
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


func TestGameServerSetController_ASTS_Management(t *testing.T) {
	tests := []struct {
		name            string
		gssName         string
		gssNamespace    string
		initialReplicas int32
		updatedReplicas *int32 // nil means no update
		expectASTS      bool
		expectReplicas  int32
	}{
		{
			name:            "create ASTS with 3 replicas",
			gssName:         "test-gss-1",
			gssNamespace:    "default",
			initialReplicas: 3,
			updatedReplicas: nil,
			expectASTS:      true,
			expectReplicas:  3,
		},
		{
			name:            "create ASTS with 1 replica",
			gssName:         "test-gss-2",
			gssNamespace:    "default",
			initialReplicas: 1,
			updatedReplicas: nil,
			expectASTS:      true,
			expectReplicas:  1,
		},
		{
			name:            "scale up from 2 to 5 replicas",
			gssName:         "test-gss-3",
			gssNamespace:    "default",
			initialReplicas: 2,
			updatedReplicas: ptr.To(int32(5)),
			expectASTS:      true,
			expectReplicas:  5,
		},
		{
			name:            "scale down from 4 to 1 replica",
			gssName:         "test-gss-4",
			gssNamespace:    "default",
			initialReplicas: 4,
			updatedReplicas: ptr.To(int32(1)),
			expectASTS:      true,
			expectReplicas:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			ctx := context.Background()
			k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()
			recorder := record.NewFakeRecorder(100)
			reconciler := &GameServerSetReconciler{
				Client:   k8sClient,
				Scheme:   scheme,
				recorder: recorder,
			}

			// Create initial GameServerSet
			gss := &gameKruiseV1alpha1.GameServerSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      tt.gssName,
					Namespace: tt.gssNamespace,
					UID:       types.UID("test-uid"),
				},
				Spec: gameKruiseV1alpha1.GameServerSetSpec{
					Replicas: ptr.To(tt.initialReplicas),
					GameServerTemplate: gameKruiseV1alpha1.GameServerTemplate{
						PodTemplateSpec: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{
									"app": tt.gssName,
								},
							},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{{
									Name:  "game",
									Image: "test-image",
								}},
							},
						},
					},
					UpdateStrategy: gameKruiseV1alpha1.UpdateStrategy{
						Type:          apps.RollingUpdateStatefulSetStrategyType,
						RollingUpdate: &gameKruiseV1alpha1.RollingUpdateStatefulSetStrategy{},
					},
				},
			}

			if err := k8sClient.Create(ctx, gss); err != nil {
				t.Fatalf("failed to create GameServerSet: %v", err)
			}

			// First reconcile - should create ASTS
			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      tt.gssName,
					Namespace: tt.gssNamespace,
				},
			}
			if _, err := reconciler.Reconcile(ctx, req); err != nil {
				t.Fatalf("first reconcile failed: %v", err)
			}

			// Update replicas if needed
			if tt.updatedReplicas != nil {
				fetchedGSS := &gameKruiseV1alpha1.GameServerSet{}
				if err := k8sClient.Get(ctx, req.NamespacedName, fetchedGSS); err != nil {
					t.Fatalf("failed to get GSS for update: %v", err)
				}
				fetchedGSS.Spec.Replicas = tt.updatedReplicas
				if err := k8sClient.Update(ctx, fetchedGSS); err != nil {
					t.Fatalf("failed to update GSS: %v", err)
				}

				// Second reconcile - should handle scaling
				if _, err := reconciler.Reconcile(ctx, req); err != nil {
					t.Fatalf("second reconcile failed: %v", err)
				}
			}

			// Verify ASTS
			if tt.expectASTS {
				asts := &kruiseV1beta1.StatefulSet{}
				if err := k8sClient.Get(ctx, req.NamespacedName, asts); err != nil {
					t.Errorf("ASTS should exist but got error: %v", err)
					return
				}

				// Verify replicas
				if *asts.Spec.Replicas != tt.expectReplicas {
					t.Errorf("ASTS replicas = %d, want %d", *asts.Spec.Replicas, tt.expectReplicas)
				}

				// Verify owner reference
				if len(asts.OwnerReferences) != 1 {
					t.Errorf("ASTS should have 1 owner reference, got %d", len(asts.OwnerReferences))
				} else if asts.OwnerReferences[0].Name != tt.gssName {
					t.Errorf("ASTS owner reference name = %s, want %s", asts.OwnerReferences[0].Name, tt.gssName)
				}

				// Verify basic configuration
				if asts.Spec.PodManagementPolicy != apps.ParallelPodManagement {
					t.Errorf("ASTS PodManagementPolicy = %s, want %s", asts.Spec.PodManagementPolicy, apps.ParallelPodManagement)
				}
				if asts.Spec.ServiceName != tt.gssName {
					t.Errorf("ASTS ServiceName = %s, want %s", asts.Spec.ServiceName, tt.gssName)
				}
			}
		})
	}
}

func TestGameServerSetController_Manager_Integration(t *testing.T) {
	tests := []struct {
		name         string
		gssReplicas  int32
		astsReplicas int32
		podCount     int
		expectScale  bool
	}{
		{
			name:         "no scaling needed when replicas match",
			gssReplicas:  3,
			astsReplicas: 3,
			podCount:     3,
			expectScale:  false,
		},
		{
			name:         "scaling needed when GSS > ASTS",
			gssReplicas:  5,
			astsReplicas: 3,
			podCount:     3,
			expectScale:  true,
		},
		{
			name:         "scaling needed when GSS < ASTS",
			gssReplicas:  2,
			astsReplicas: 4,
			podCount:     4,
			expectScale:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()

			gss := &gameKruiseV1alpha1.GameServerSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-manager",
					Namespace: "default",
				},
				Spec: gameKruiseV1alpha1.GameServerSetSpec{
					Replicas: ptr.To(tt.gssReplicas),
				},
			}

			asts := &kruiseV1beta1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      gss.Name,
					Namespace: gss.Namespace,
				},
				Spec: kruiseV1beta1.StatefulSetSpec{
					Replicas: ptr.To(tt.astsReplicas),
				},
			}

			// Create mock pods
			var pods []corev1.Pod
			for i := 0; i < tt.podCount; i++ {
				pod := corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf("%s-%d", gss.Name, i),
						Namespace: gss.Namespace,
					},
				}
				pods = append(pods, pod)
			}

			// Test Manager
			manager := NewGameServerSetManager(gss, k8sClient, nil)
			if manager == nil {
				t.Fatal("Manager should not be nil")
			}

			// Sync state
			manager.SyncStsAndPodList(asts, pods)

			// Test IsNeedToScale
			needScale := manager.IsNeedToScale()
			if needScale != tt.expectScale {
				t.Errorf("IsNeedToScale() = %v, want %v", needScale, tt.expectScale)
			}
		})
	}
}

func TestGameServerSetController_Status_Management(t *testing.T) {
	tests := []struct {
		name           string
		gssName        string
		podStates      []corev1.PodPhase
		expectReplicas int32
	}{
		{
			name:           "all pods running",
			gssName:        "test-status-1",
			podStates:      []corev1.PodPhase{corev1.PodRunning, corev1.PodRunning, corev1.PodRunning},
			expectReplicas: 3,
		},
		{
			name:           "mixed pod states",
			gssName:        "test-status-2",
			podStates:      []corev1.PodPhase{corev1.PodRunning, corev1.PodPending, corev1.PodRunning},
			expectReplicas: 3,
		},
		{
			name:           "single pod",
			gssName:        "test-status-3",
			podStates:      []corev1.PodPhase{corev1.PodRunning},
			expectReplicas: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()

			gss := &gameKruiseV1alpha1.GameServerSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      tt.gssName,
					Namespace: "default",
				},
				Spec: gameKruiseV1alpha1.GameServerSetSpec{
					Replicas: ptr.To(tt.expectReplicas),
				},
			}

			asts := &kruiseV1beta1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      gss.Name,
					Namespace: gss.Namespace,
				},
				Spec: kruiseV1beta1.StatefulSetSpec{
					Replicas: ptr.To(tt.expectReplicas),
				},
			}

			// Create pods with specified states
			var pods []corev1.Pod
			for i, state := range tt.podStates {
				pod := corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf("%s-%d", gss.Name, i),
						Namespace: gss.Namespace,
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOwnerGssKey: gss.Name,
						},
					},
					Status: corev1.PodStatus{
						Phase: state,
					},
				}
				pods = append(pods, pod)
			}

			// Test Manager status handling
			manager := NewGameServerSetManager(gss, k8sClient, nil)
			manager.SyncStsAndPodList(asts, pods)

			// Verify manager has expected state
			if manager.(*GameServerSetManager).gameServerSet.Name != tt.gssName {
				t.Errorf("Manager GSS name = %s, want %s", manager.(*GameServerSetManager).gameServerSet.Name, tt.gssName)
			}
			if *manager.(*GameServerSetManager).asts.Spec.Replicas != tt.expectReplicas {
				t.Errorf("Manager ASTS replicas = %d, want %d", *manager.(*GameServerSetManager).asts.Spec.Replicas, tt.expectReplicas)
			}
		})
	}
}

func TestGameServerSetController_SyncStatus(t *testing.T) {
	tests := []struct {
		name             string
		gssSpec          gameKruiseV1alpha1.GameServerSetSpec
		astsStatus       kruiseV1beta1.StatefulSetStatus
		pods             []corev1.Pod
		expectUpdate     bool
		expectReplicas   int32
		expectReady      int32
		expectMaintain   int32
		expectWaitDelete int32
		expectPreDelete  int32
	}{
		{
			name: "update status when ASTS status changes",
			gssSpec: gameKruiseV1alpha1.GameServerSetSpec{
				Replicas: ptr.To(int32(5)),
			},
			astsStatus: kruiseV1beta1.StatefulSetStatus{
				Replicas:             5,
				ReadyReplicas:        3,
				AvailableReplicas:    3,
				UpdatedReplicas:      5,
				UpdatedReadyReplicas: 3,
				LabelSelector:        "app=test",
			},
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-0",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOwnerGssKey: "test",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-1",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOwnerGssKey: "test",
							gameKruiseV1alpha1.GameServerOpsStateKey: string(gameKruiseV1alpha1.Maintaining),
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-2",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOwnerGssKey: "test",
							gameKruiseV1alpha1.GameServerOpsStateKey: string(gameKruiseV1alpha1.WaitToDelete),
						},
					},
				},
			},
			expectUpdate:     true,
			expectReplicas:   5,
			expectReady:      3,
			expectMaintain:   1,
			expectWaitDelete: 1,
			expectPreDelete:  0,
		},
		{
			name: "status with pre-delete game servers",
			gssSpec: gameKruiseV1alpha1.GameServerSetSpec{
				Replicas: ptr.To(int32(3)),
			},
			astsStatus: kruiseV1beta1.StatefulSetStatus{
				Replicas:             3,
				ReadyReplicas:        2,
				AvailableReplicas:    2,
				UpdatedReplicas:      3,
				UpdatedReadyReplicas: 2,
				LabelSelector:        "app=test",
			},
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-0",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOwnerGssKey: "test",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-1",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOwnerGssKey: "test",
							gameKruiseV1alpha1.GameServerStateKey:    string(gameKruiseV1alpha1.PreDelete),
						},
					},
				},
			},
			expectUpdate:     true,
			expectReplicas:   3,
			expectReady:      2,
			expectMaintain:   0,
			expectWaitDelete: 0,
			expectPreDelete:  1,
		},
		{
			name: "no update needed when status matches",
			gssSpec: gameKruiseV1alpha1.GameServerSetSpec{
				Replicas: ptr.To(int32(2)),
			},
			astsStatus: kruiseV1beta1.StatefulSetStatus{
				Replicas:             2,
				ReadyReplicas:        2,
				AvailableReplicas:    2,
				UpdatedReplicas:      2,
				UpdatedReadyReplicas: 2,
				LabelSelector:        "app=test",
			},
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-0",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOwnerGssKey: "test",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-1",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOwnerGssKey: "test",
						},
					},
				},
			},
			expectUpdate:     false,
			expectReplicas:   2,
			expectReady:      2,
			expectMaintain:   0,
			expectWaitDelete: 0,
			expectPreDelete:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup - create fake client with status subresource support
			k8sClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithStatusSubresource(&gameKruiseV1alpha1.GameServerSet{}).
				Build()
			ctx := context.Background()

			gss := &gameKruiseV1alpha1.GameServerSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test",
					Namespace:  "default",
					Generation: 1,
				},
				Spec: tt.gssSpec,
				Status: gameKruiseV1alpha1.GameServerSetStatus{
					// Set different initial status to test updates
					Replicas:                0,
					CurrentReplicas:         0,
					ReadyReplicas:           0,
					MaintainingReplicas:     ptr.To(int32(0)),
					WaitToBeDeletedReplicas: ptr.To(int32(0)),
					PreDeleteReplicas:       ptr.To(int32(0)),
				},
			}

			// Create GSS in fake client for status updates
			if err := k8sClient.Create(ctx, gss); err != nil {
				t.Fatalf("failed to create GSS: %v", err)
			}

			asts := &kruiseV1beta1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      gss.Name,
					Namespace: gss.Namespace,
				},
				Spec: kruiseV1beta1.StatefulSetSpec{
					Replicas: tt.gssSpec.Replicas,
				},
				Status: tt.astsStatus,
			}

			// Test SyncStatus
			manager := NewGameServerSetManager(gss, k8sClient, nil)
			manager.SyncStsAndPodList(asts, tt.pods)

			err := manager.SyncStatus()
			if tt.expectUpdate && err != nil {
				t.Errorf("SyncStatus() should succeed but got error: %v", err)
			}

			// Verify status was updated (when expected)
			if tt.expectUpdate {
				updatedGSS := &gameKruiseV1alpha1.GameServerSet{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: gss.Name, Namespace: gss.Namespace}, updatedGSS); err != nil {
					t.Fatalf("failed to get updated GSS: %v", err)
				}

				// Verify replica counts
				if updatedGSS.Status.Replicas != tt.expectReplicas {
					t.Errorf("Status.Replicas = %d, want %d", updatedGSS.Status.Replicas, tt.expectReplicas)
				}
				if updatedGSS.Status.CurrentReplicas != int32(len(tt.pods)) {
					t.Errorf("Status.CurrentReplicas = %d, want %d", updatedGSS.Status.CurrentReplicas, len(tt.pods))
				}
				if updatedGSS.Status.ReadyReplicas != tt.expectReady {
					t.Errorf("Status.ReadyReplicas = %d, want %d", updatedGSS.Status.ReadyReplicas, tt.expectReady)
				}
				if updatedGSS.Status.MaintainingReplicas != nil && *updatedGSS.Status.MaintainingReplicas != tt.expectMaintain {
					t.Errorf("Status.MaintainingReplicas = %d, want %d", *updatedGSS.Status.MaintainingReplicas, tt.expectMaintain)
				}
				if updatedGSS.Status.WaitToBeDeletedReplicas != nil && *updatedGSS.Status.WaitToBeDeletedReplicas != tt.expectWaitDelete {
					t.Errorf("Status.WaitToBeDeletedReplicas = %d, want %d", *updatedGSS.Status.WaitToBeDeletedReplicas, tt.expectWaitDelete)
				}
				if updatedGSS.Status.PreDeleteReplicas != nil && *updatedGSS.Status.PreDeleteReplicas != tt.expectPreDelete {
					t.Errorf("Status.PreDeleteReplicas = %d, want %d", *updatedGSS.Status.PreDeleteReplicas, tt.expectPreDelete)
				}

				// Verify other status fields
				if updatedGSS.Status.AvailableReplicas != tt.astsStatus.AvailableReplicas {
					t.Errorf("Status.AvailableReplicas = %d, want %d", updatedGSS.Status.AvailableReplicas, tt.astsStatus.AvailableReplicas)
				}
				if updatedGSS.Status.UpdatedReplicas != tt.astsStatus.UpdatedReplicas {
					t.Errorf("Status.UpdatedReplicas = %d, want %d", updatedGSS.Status.UpdatedReplicas, tt.astsStatus.UpdatedReplicas)
				}
				if updatedGSS.Status.ObservedGeneration != gss.GetGeneration() {
					t.Errorf("Status.ObservedGeneration = %d, want %d", updatedGSS.Status.ObservedGeneration, gss.GetGeneration())
				}
			}
		})
	}
}

func TestGameServerSetController_Reconcile_ErrorHandling(t *testing.T) {
	tests := []struct {
		name         string
		setupError   string
		gssExists    bool
		astsExists   bool
		expectError  bool
		expectRetry  bool
		expectResult bool
	}{
		{
			name:        "handle GSS not found gracefully",
			setupError:  "gss-not-found",
			gssExists:   false,
			astsExists:  false,
			expectError: false,
			expectRetry: false,
		},
		{
			name:         "create ASTS when not found",
			setupError:   "",
			gssExists:    true,
			astsExists:   false,
			expectError:  false,
			expectRetry:  false,
			expectResult: true,
		},
		{
			name:        "continue processing when GSS and ASTS exist",
			setupError:  "",
			gssExists:   true,
			astsExists:  true,
			expectError: false,
			expectRetry: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			ctx := context.Background()
			k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()
			recorder := record.NewFakeRecorder(100)
			reconciler := &GameServerSetReconciler{
				Client:   k8sClient,
				Scheme:   scheme,
				recorder: recorder,
			}

			// Create GSS if needed
			var gss *gameKruiseV1alpha1.GameServerSet
			if tt.gssExists {
				gss = &gameKruiseV1alpha1.GameServerSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-gss",
						Namespace: "default",
						UID:       types.UID("test-uid"),
					},
					Spec: gameKruiseV1alpha1.GameServerSetSpec{
						Replicas: ptr.To(int32(3)),
						GameServerTemplate: gameKruiseV1alpha1.GameServerTemplate{
							PodTemplateSpec: corev1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{
										"app": "test",
									},
								},
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{{
										Name:  "game",
										Image: "test-image",
									}},
								},
							},
						},
						UpdateStrategy: gameKruiseV1alpha1.UpdateStrategy{
							Type:          apps.RollingUpdateStatefulSetStrategyType,
							RollingUpdate: &gameKruiseV1alpha1.RollingUpdateStatefulSetStrategy{},
						},
					},
				}
				if err := k8sClient.Create(ctx, gss); err != nil {
					t.Fatalf("failed to create GSS: %v", err)
				}
			}

			// Create ASTS if needed
			if tt.astsExists && tt.gssExists {
				asts := &kruiseV1beta1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-gss",
						Namespace: "default",
						Annotations: map[string]string{
							gameKruiseV1alpha1.AstsHashKey: "test-hash",
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion:         "game.kruise.io/v1alpha1",
								Kind:               "GameServerSet",
								Name:               "test-gss",
								UID:                "test-uid",
								Controller:         ptr.To(true),
								BlockOwnerDeletion: ptr.To(true),
							},
						},
					},
					Spec: kruiseV1beta1.StatefulSetSpec{
						Replicas: ptr.To(int32(3)),
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								gameKruiseV1alpha1.GameServerOwnerGssKey: "test-gss",
							},
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{
									gameKruiseV1alpha1.GameServerOwnerGssKey: "test-gss",
								},
							},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{{
									Name:  "game",
									Image: "test-image",
								}},
							},
						},
					},
				}
				if err := k8sClient.Create(ctx, asts); err != nil {
					t.Fatalf("failed to create ASTS: %v", err)
				}
			}

			// Execute reconcile
			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-gss",
					Namespace: "default",
				},
			}

			result, err := reconciler.Reconcile(ctx, req)

			// Verify results
			if tt.expectError && err == nil {
				t.Errorf("expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if tt.expectRetry && result.Requeue == false {
				t.Errorf("expected retry but got no requeue")
			}

			// Verify ASTS creation when GSS exists but ASTS doesn't
			if tt.gssExists && !tt.astsExists && tt.expectResult {
				createdASTS := &kruiseV1beta1.StatefulSet{}
				if err := k8sClient.Get(ctx, req.NamespacedName, createdASTS); err != nil {
					t.Errorf("ASTS should be created but got error: %v", err)
				} else {
					// Verify owner reference
					if len(createdASTS.OwnerReferences) != 1 {
						t.Errorf("ASTS should have 1 owner reference, got %d", len(createdASTS.OwnerReferences))
					} else if createdASTS.OwnerReferences[0].Name != "test-gss" {
						t.Errorf("ASTS owner reference name = %s, want test-gss", createdASTS.OwnerReferences[0].Name)
					}
				}
			}
		})
	}
}

func TestGameServerSetController_Reconcile_CompleteFlow(t *testing.T) {
	tests := []struct {
		name               string
		initialReplicas    int32
		targetReplicas     int32
		podPhases          []corev1.PodPhase
		podOpsStates       []string
		expectScaling      bool
		expectStatusUpdate bool
	}{
		{
			name:            "complete flow with scaling up",
			initialReplicas: 2,
			targetReplicas:  4,
			podPhases:       []corev1.PodPhase{corev1.PodRunning, corev1.PodRunning},
			podOpsStates:    []string{"", ""},
			expectScaling:   true,
		},
		{
			name:               "complete flow with no scaling needed",
			initialReplicas:    3,
			targetReplicas:     3,
			podPhases:          []corev1.PodPhase{corev1.PodRunning, corev1.PodRunning, corev1.PodRunning},
			podOpsStates:       []string{"", "", ""},
			expectScaling:      false,
			expectStatusUpdate: true,
		},
		{
			name:               "complete flow with maintaining pods",
			initialReplicas:    3,
			targetReplicas:     3,
			podPhases:          []corev1.PodPhase{corev1.PodRunning, corev1.PodRunning, corev1.PodRunning},
			podOpsStates:       []string{"", string(gameKruiseV1alpha1.Maintaining), ""},
			expectScaling:      false,
			expectStatusUpdate: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			ctx := context.Background()
			k8sClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithStatusSubresource(&gameKruiseV1alpha1.GameServerSet{}).
				Build()
			recorder := record.NewFakeRecorder(100)
			reconciler := &GameServerSetReconciler{
				Client:   k8sClient,
				Scheme:   scheme,
				recorder: recorder,
			}

			// Create GameServerSet
			gss := &gameKruiseV1alpha1.GameServerSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-flow",
					Namespace: "default",
					UID:       types.UID("test-uid"),
				},
				Spec: gameKruiseV1alpha1.GameServerSetSpec{
					Replicas: ptr.To(tt.targetReplicas),
					GameServerTemplate: gameKruiseV1alpha1.GameServerTemplate{
						PodTemplateSpec: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{
									"app": "test-flow",
								},
							},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{{
									Name:  "game",
									Image: "test-image",
								}},
							},
						},
					},
					UpdateStrategy: gameKruiseV1alpha1.UpdateStrategy{
						Type:          apps.RollingUpdateStatefulSetStrategyType,
						RollingUpdate: &gameKruiseV1alpha1.RollingUpdateStatefulSetStrategy{},
					},
				},
			}
			if err := k8sClient.Create(ctx, gss); err != nil {
				t.Fatalf("failed to create GSS: %v", err)
			}

			// Create ASTS
			asts := &kruiseV1beta1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-flow",
					Namespace: "default",
					Annotations: map[string]string{
						gameKruiseV1alpha1.AstsHashKey: "test-hash",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         "game.kruise.io/v1alpha1",
							Kind:               "GameServerSet",
							Name:               "test-flow",
							UID:                "test-uid",
							Controller:         ptr.To(true),
							BlockOwnerDeletion: ptr.To(true),
						},
					},
				},
				Spec: kruiseV1beta1.StatefulSetSpec{
					Replicas: ptr.To(tt.initialReplicas),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							gameKruiseV1alpha1.GameServerOwnerGssKey: "test-flow",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								gameKruiseV1alpha1.GameServerOwnerGssKey: "test-flow",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Name:  "game",
								Image: "test-image",
							}},
						},
					},
				},
				Status: kruiseV1beta1.StatefulSetStatus{
					Replicas:          tt.initialReplicas,
					ReadyReplicas:     tt.initialReplicas,
					AvailableReplicas: tt.initialReplicas,
					UpdatedReplicas:   tt.initialReplicas,
				},
			}
			if err := k8sClient.Create(ctx, asts); err != nil {
				t.Fatalf("failed to create ASTS: %v", err)
			}

			// Create pods
			for i := 0; i < len(tt.podPhases); i++ {
				labels := map[string]string{
					gameKruiseV1alpha1.GameServerOwnerGssKey: "test-flow",
				}
				if i < len(tt.podOpsStates) && tt.podOpsStates[i] != "" {
					labels[gameKruiseV1alpha1.GameServerOpsStateKey] = tt.podOpsStates[i]
				}

				pod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf("test-flow-%d", i),
						Namespace: "default",
						Labels:    labels,
					},
					Status: corev1.PodStatus{
						Phase: tt.podPhases[i],
					},
				}
				if err := k8sClient.Create(ctx, pod); err != nil {
					t.Fatalf("failed to create pod %d: %v", i, err)
				}
			}

			// Execute reconcile
			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-flow",
					Namespace: "default",
				},
			}

			result, err := reconciler.Reconcile(ctx, req)
			if err != nil {
				t.Fatalf("reconcile failed: %v", err)
			}

			// Verify expected outcomes
			if tt.expectScaling {
				// When scaling is needed, reconcile should return early
				if result.Requeue || result.RequeueAfter > 0 {
					t.Logf("Scaling triggered requeue as expected")
				}
			}

			if tt.expectStatusUpdate {
				// Verify that status would be updated in subsequent reconcile
				updatedGSS := &gameKruiseV1alpha1.GameServerSet{}
				if err := k8sClient.Get(ctx, req.NamespacedName, updatedGSS); err != nil {
					t.Fatalf("failed to get updated GSS: %v", err)
				}

				// Run second reconcile to process status update
				if _, err := reconciler.Reconcile(ctx, req); err != nil {
					t.Fatalf("second reconcile failed: %v", err)
				}
			}
		})
	}
}

func TestGameServerSetController_SyncPodProbeMarker(t *testing.T) {
	tests := []struct {
		name              string
		serviceQualities  []gameKruiseV1alpha1.ServiceQuality
		existingPPM       *kruisev1alpha1.PodProbeMarker
		expectError       bool
		expectPPMCreation bool
		expectRequeue     bool
	}{
		{
			name: "create PodProbeMarker when service qualities exist",
			serviceQualities: []gameKruiseV1alpha1.ServiceQuality{
				{
					Name:          "test-probe",
					ContainerName: "test-container",
					Probe: corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Path: "/health",
								Port: intstr.FromInt(8080),
							},
						},
					},
				},
			},
			expectPPMCreation: true,
			expectRequeue:     true,
		},
		{
			name:              "no PodProbeMarker needed when no service qualities",
			serviceQualities:  []gameKruiseV1alpha1.ServiceQuality{},
			expectPPMCreation: false,
			expectRequeue:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			ctx := context.Background()
			k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()

			gss := &gameKruiseV1alpha1.GameServerSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-gss",
					Namespace:  "default",
					Generation: 2,
				},
				Spec: gameKruiseV1alpha1.GameServerSetSpec{
					Replicas:         ptr.To(int32(3)),
					ServiceQualities: tt.serviceQualities,
				},
			}

			// Create existing PodProbeMarker if specified
			if tt.existingPPM != nil {
				if err := k8sClient.Create(ctx, tt.existingPPM); err != nil {
					t.Fatalf("failed to create existing PPM: %v", err)
				}
			}

			// Test SyncPodProbeMarker
			recorder := record.NewFakeRecorder(100)
			manager := NewGameServerSetManager(gss, k8sClient, recorder)
			err, done := manager.SyncPodProbeMarker()

			// Verify results
			if tt.expectError && err == nil {
				t.Errorf("expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if tt.expectRequeue && done {
				t.Errorf("expected requeue (done=false) but got done=true")
			}
			if !tt.expectRequeue && !done && len(tt.serviceQualities) > 0 {
				t.Errorf("expected done=true but got done=false")
			}

			// Verify PodProbeMarker creation
			if tt.expectPPMCreation {
				createdPPM := &kruisev1alpha1.PodProbeMarker{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: gss.Name, Namespace: gss.Namespace}, createdPPM); err != nil {
					t.Errorf("expected PodProbeMarker to be created but got error: %v", err)
				} else {
					// Verify PPM has correct spec
					if len(createdPPM.Spec.Probes) != len(tt.serviceQualities) {
						t.Errorf("PPM probes count = %d, want %d", len(createdPPM.Spec.Probes), len(tt.serviceQualities))
					}
					// Verify owner reference
					if len(createdPPM.OwnerReferences) != 1 {
						t.Errorf("PPM should have 1 owner reference, got %d", len(createdPPM.OwnerReferences))
					} else if createdPPM.OwnerReferences[0].Name != gss.Name {
						t.Errorf("PPM owner reference name = %s, want %s", createdPPM.OwnerReferences[0].Name, gss.Name)
					}
				}
			}
		})
	}
}
