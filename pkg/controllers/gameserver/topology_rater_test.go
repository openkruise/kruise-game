package gameserver

import (
	"context"
	"testing"

	gameKruiseV1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestTopologyRater_Basic covers the main happy-path calculation using defaults.
func TestTopologyRater_Basic(t *testing.T) {
	ctx := context.TODO()

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = gameKruiseV1alpha1.AddToScheme(scheme)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "pod-1",
			OwnerReferences: []metav1.OwnerReference{
				{UID: types.UID("owner-1")},
			},
		},
		Spec: corev1.PodSpec{
			NodeName: "node-a",
		},
	}

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(pod).
		WithIndex(&corev1.Pod{}, "spec.nodeName", func(obj client.Object) []string {
			pod := obj.(*corev1.Pod)
			return []string{pod.Spec.NodeName}
		}).
		Build()
	rater := NewTopologyRater(cl)

	cfg := &gameKruiseV1alpha1.TopologyDeletionPriorityConfig{}

	got, err := rater.CalculateDeletionPriority(ctx, &gameKruiseV1alpha1.GameServer{}, pod, cfg)
	if err != nil {
		t.Fatalf("CalculateDeletionPriority returned error: %v", err)
	}
	if got == nil {
		t.Fatalf("expected non-nil priority")
	}

	// With one pod and one owner on the node and default weights:
	// priority = 100 - 1*10 - 1*5 = 85.
	if got.IntVal != 85 {
		t.Fatalf("unexpected priority: got=%d want=%d", got.IntVal, 85)
	}
}
