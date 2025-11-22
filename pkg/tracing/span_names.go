package tracing

// Controller reconcile spans.
// Keep names aligned with the controller verbs + object naming convention used across the project
// so collectors and tests can filter using stable, centralized values.
const (
	// SpanAdmissionMutatePod defines the canonical root span name for the Admission webhook
	// responsible for mutating GameServer pods. Keep this in sync with the tracing spec's
	// verb-object naming convention so tests and collectors stay aligned.
	SpanAdmissionMutatePod = "mutate pod admission"
	// SpanReconcileGameServer is the root span name for the GameServer controller reconcile loop.
	SpanReconcileGameServer = "reconcile game_server"

	// SpanReconcileGameServerSet is the root span name for the GameServerSet controller reconcile loop.
	SpanReconcileGameServerSet = "reconcile game_server_set"
	// NodePort plugin spans
	SpanProcessNodePortPod     = "process nodeport pod"
	SpanCreateNodePortService  = "create nodeport service"
	SpanToggleNodePortSelector = "toggle nodeport selector"
	SpanPublishNodePortStatus  = "publish nodeport status"
	SpanCleanupNodePortService = "cleanup nodeport service"
	// ALB/NLB plugin spans
	SpanCreateNLBService     = "create nlb service"
	SpanReconcileNLBService  = "reconcile nlb service"
	SpanProcessNLBPod        = "process nlb pod"
	SpanToggleNLBServiceType = "toggle nlb service type"
	SpanCheckNLBStatus       = "check nlb status"
	SpanPublishNLBStatus     = "publish nlb status"
	SpanCleanupNLBAllocation = "cleanup nlb allocation"
	SpanAllocateNLBPorts     = "allocate nlb ports"

	// HostPort plugin spans
	SpanPrepareHostPortPod        = "prepare hostport pod"
	SpanAllocateHostPort          = "allocate hostport"
	SpanProcessHostPortUpdate     = "process hostport update"
	SpanCleanupHostPortAllocation = "cleanup hostport allocation"
)
