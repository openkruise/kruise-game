package gameserver

import (
	"testing"

	gameKruiseV1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
)

func TestSyncServiceQualities_StateFalseFix(t *testing.T) {
	tests := []struct {
		name          string
		state         bool
		result        string
		message       string
		expectMatched bool
		description   string
	}{
		{
			name:          "state false with result and empty message - should match (fix)",
			state:         false,
			result:        "nginxok",
			message:       "",
			expectMatched: true,
			description:   "Fallback matching handles kruise-daemon limitation",
		},
		{
			name:          "state false with result and matching message - should match",
			state:         false,
			result:        "error",
			message:       "error",
			expectMatched: true,
			description:   "Exact match still works",
		},
		{
			name:          "state true with result and matching message - should match",
			state:         true,
			result:        "healthy",
			message:       "healthy",
			expectMatched: true,
			description:   "Normal pattern works",
		},
		{
			name:          "state false without result - should match",
			state:         false,
			result:        "",
			message:       "",
			expectMatched: true,
			description:   "State-only matching works",
		},
		{
			name:          "state true with result but wrong message - should not match",
			state:         true,
			result:        "healthy",
			message:       "unhealthy",
			expectMatched: false,
			description:   "Non-matching result prevents match",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eventRecorder := record.NewFakeRecorder(10)

			sq := []gameKruiseV1alpha1.ServiceQuality{{
				Name: "test",
				ServiceQualityAction: []gameKruiseV1alpha1.ServiceQualityAction{{
					State:  tt.state,
					Result: tt.result,
					GameServerSpec: gameKruiseV1alpha1.GameServerSpec{
						OpsState: gameKruiseV1alpha1.WaitToDelete,
					},
				}},
			}}

			condStatus := corev1.ConditionTrue
			if !tt.state {
				condStatus = corev1.ConditionFalse
			}

			pods := []corev1.PodCondition{{
				Type:          "game.kruise.io/test",
				Status:        condStatus,
				Message:       tt.message,
				LastProbeTime: metav1.Now(),
			}}

			gs := &gameKruiseV1alpha1.GameServer{
				ObjectMeta: metav1.ObjectMeta{Name: "test-gs", Namespace: "default"},
				Spec:       gameKruiseV1alpha1.GameServerSpec{},
			}

			syncServiceQualities(sq, pods, gs, eventRecorder)

			// Check if action was applied
			if tt.expectMatched {
				if gs.Spec.OpsState != gameKruiseV1alpha1.WaitToDelete {
					t.Errorf("%s: expected action to be applied (OpsState=WaitToDelete) but got %v",
						tt.description, gs.Spec.OpsState)
				}
			} else {
				if gs.Spec.OpsState == gameKruiseV1alpha1.WaitToDelete {
					t.Errorf("%s: expected action NOT to be applied but it was", tt.description)
				}
			}
		})
	}
}
