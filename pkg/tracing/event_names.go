package tracing

// Canonical event names emitted by controllers and network plugins.
const (
	EventGameServerReconcileBootstrap             = "gameserver.reconcile.bootstrap"
	EventGameServerReconcileGameServerInitialized = "gameserver.reconcile.gameserver_initialized"
	EventGameServerReconcileCleanup               = "gameserver.reconcile.cleanup"
	EventGameServerReconcileManagerReady          = "gameserver.reconcile.manager_ready"
	EventGameServerReconcileSyncGsToPodStart      = "gameserver.reconcile.sync_gs_to_pod.start"
	EventGameServerReconcileSyncGsToPodSuccess    = "gameserver.reconcile.sync_gs_to_pod.success"
	EventGameServerReconcileSyncPodToGsStart      = "gameserver.reconcile.sync_pod_to_gs.start"
	EventGameServerReconcileSyncPodToGsSuccess    = "gameserver.reconcile.sync_pod_to_gs.success"
	EventGameServerReconcileWaitNetworkState      = "gameserver.reconcile.wait_network_state"

	EventGameServerSetReconcileSyncPodProbeMarkerStart   = "gameserverset.reconcile.sync_podprobemarker.start"
	EventGameServerSetReconcileSyncPodProbeMarkerWaiting = "gameserverset.reconcile.sync_podprobemarker.waiting"
	EventGameServerSetReconcileSyncPodProbeMarkerSuccess = "gameserverset.reconcile.sync_podprobemarker.success"
	// Network plugin events
	EventNetworkNLBPortsAllocated = "network.nlb.ports.allocated"
)
