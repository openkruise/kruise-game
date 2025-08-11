package webhook

import (
	"testing"

	"github.com/stretchr/testify/assert"
	ctrlwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"
)

func TestWebhookServer_NeedLeaderElection(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		enabled bool
	}{
		{"enabled", true},
		{"disabled", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := NewServer(ctrlwebhook.Options{}, tt.enabled)
			assert.Equal(t, tt.enabled, s.NeedLeaderElection(), "NeedLeaderElection() mismatch")
		})
	}
}
