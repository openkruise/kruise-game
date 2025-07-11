package webhook

import (
	ctrlwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"
)

var _ ctrlwebhook.Server = &WebhookServer{}

// WebhookServer 封装的 webhook server
type WebhookServer struct {
	ctrlwebhook.Server

	enableLeaderElection bool
}

func NewServer(options ctrlwebhook.Options, enableLeaderElection bool) *WebhookServer {
	return &WebhookServer{
		Server:               ctrlwebhook.NewServer(options),
		enableLeaderElection: enableLeaderElection,
	}
}

// NeedLeaderElection indicates whether the webhook server needs leader election.
// This is set to true to ensure that the webhook server runs only on the leader instance in a multi-instance setup.
func (s *WebhookServer) NeedLeaderElection() bool {
	return s.enableLeaderElection
}
