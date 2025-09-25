## GameServerSet

### GameServerSetSpec

```
type GameServerSetSpec struct {
    // The number of game servers. Must be specified, with a minimum value of 0.
    Replicas *int32 `json:"replicas"`

    // Game server template. The new game server will be created with the parameters defined in GameServerTemplate.
    GameServerTemplate   GameServerTemplate `json:"gameServerTemplate,omitempty"`

    // Reserved game server IDs, optional. If specified, existing game servers with those IDs will be deleted,
    // and new game servers will not be created with those IDs.
    ReserveGameServerIds []int              `json:"reserveGameServerIds,omitempty"`

    // Custom service qualities for game servers.
    ServiceQualities     []ServiceQuality   `json:"serviceQualities,omitempty"`

    // Batch update strategy for game servers.
    UpdateStrategy       UpdateStrategy     `json:"updateStrategy,omitempty"`
 
    // Horizontal scaling strategy for game servers.
    ScaleStrategy        ScaleStrategy      `json:"scaleStrategy,omitempty"`

    // Network settings for game server access layer.
    Network              *Network           `json:"network,omitempty"`
}

```

#### GameServerTemplate

```yaml
type GameServerTemplate struct {
    // All fields inherited from PodTemplateSpec.
    corev1.PodTemplateSpec `json:",inline"`

    // Requests and claims for persistent volumes.
    VolumeClaimTemplates   []corev1.PersistentVolumeClaim `json:"volumeClaimTemplates,omitempty"`

    // ReclaimPolicy indicates the reclaim policy for GameServer.
    // Default is Cascade.
    ReclaimPolicy GameServerReclaimPolicy `json:"reclaimPolicy,omitempty"`
}

type GameServerReclaimPolicy string

const (
    // CascadeGameServerReclaimPolicy indicates that GameServer is deleted when the pod is deleted.
    // The age of GameServer is exactly the same as that of the pod.
    CascadeGameServerReclaimPolicy GameServerReclaimPolicy = "Cascade"
    
    // DeleteGameServerReclaimPolicy indicates that GameServers will be deleted when replicas of GameServerSet decreases.
    // The GameServer will not be deleted when the corresponding pod is deleted due to manual deletion, update, eviction, etc.
    DeleteGameServerReclaimPolicy GameServerReclaimPolicy = "Delete"
)

```

#### UpdateStrategy

```
type UpdateStrategy struct {
    // Type indicates the type of the StatefulSetUpdateStrategy.
    // Default is RollingUpdate.
    // +optional
    Type apps.StatefulSetUpdateStrategyType `json:"type,omitempty"`

    // RollingUpdate is used to communicate parameters when Type is RollingUpdateStatefulSetStrategyType.
    // +optional
    RollingUpdate *RollingUpdateStatefulSetStrategy `json:"rollingUpdate,omitempty"`
}

type RollingUpdateStatefulSetStrategy struct {
    // Partition indicates the ordinal at which the StatefulSet should be partitioned by default.
    // But if unorderedUpdate has been set:
    //   - Partition indicates the number of pods with non-updated revisions when rolling update.
    //   - It means controller will update $(replicas - partition) number of pod.
    // Default value is 0.
    // +optional
    Partition *int32 `json:"partition,omitempty"`
    
    // The maximum number of pods that can be unavailable during the update.
    // Value can be an absolute number (ex: 5) or a percentage of desired pods (ex: 10%).
    // Absolute number is calculated from percentage by rounding down.
    // Also, maxUnavailable can just be allowed to work with Parallel podManagementPolicy.
    // Defaults to 1.
    // +optional
    MaxUnavailable *intstr.IntOrString `json:"maxUnavailable,omitempty"`
    
    // PodUpdatePolicy indicates how pods should be updated
    // Default value is "ReCreate"
    // +optional
    PodUpdatePolicy kruiseV1beta1.PodUpdateStrategyType `json:"podUpdatePolicy,omitempty"`
    
    // Paused indicates that the StatefulSet is paused.
    // Default value is false
    // +optional
    Paused bool `json:"paused,omitempty"`
    
    // UnorderedUpdate contains strategies for non-ordered update.
    // If it is not nil, pods will be updated with non-ordered sequence.
    // Noted that UnorderedUpdate can only be allowed to work with Parallel podManagementPolicy
    // +optional
    // UnorderedUpdate *kruiseV1beta1.UnorderedUpdateStrategy `json:"unorderedUpdate,omitempty"`
    
    // InPlaceUpdateStrategy contains strategies for in-place update.
    // +optional
    InPlaceUpdateStrategy *appspub.InPlaceUpdateStrategy `json:"inPlaceUpdateStrategy,omitempty"`
    
    // MinReadySeconds indicates how long will the pod be considered ready after it's updated.
    // MinReadySeconds works with both OrderedReady and Parallel podManagementPolicy.
    // It affects the pod scale up speed when the podManagementPolicy is set to be OrderedReady.
    // Combined with MaxUnavailable, it affects the pod update speed regardless of podManagementPolicy.
    // Default value is 0, max is 300.
    // +optional
    MinReadySeconds *int32 `json:"minReadySeconds,omitempty"`
}

type InPlaceUpdateStrategy struct {
    // GracePeriodSeconds is the timespan between set Pod status to not-ready and update images in Pod spec
    // when in-place update a Pod.
    GracePeriodSeconds int32 `json:"gracePeriodSeconds,omitempty"`
}
```

#### ScaleStrategy
```

type ScaleStrategy struct {
    // The maximum number of pods that can be unavailable during scaling.
    // Value can be an absolute number (ex: 5) or a percentage of desired pods (ex: 10%).
    // Absolute number is calculated from percentage by rounding down.
    // It can just be allowed to work with Parallel podManagementPolicy.
    MaxUnavailable *intstr.IntOrString `json:"maxUnavailable,omitempty"`

    // ScaleDownStrategyType indicates the scaling down strategy, include two types: General & ReserveIds
    // General will first consider the ReserveGameServerIds field when game server scaling down. 
    // When the number of reserved game servers does not meet the scale down number, continue to 
    // select and delete the game servers from the current game server list.
    // ReserveIds will backfill the sequence numbers into ReserveGameServerIds field when
    // GameServers scale down, whether set by ReserveGameServerIds field or the GameServerSet 
    // controller chooses to remove it.
    // Default is General
    // +optional
    ScaleDownStrategyType ScaleDownStrategyType `json:"scaleDownStrategyType,omitempty"`
}
```

#### ServiceQualities

```
type ServiceQuality struct {
    // Inherits all fields from corev1.Probe
    corev1.Probe  `json:",inline"`
    
    // Custom name for the service quality, distinguishes different service qualities that are defined.
    Name          string `json:"name"`
    
    // Name of the container to be probed.
    ContainerName string `json:"containerName,omitempty"`

    // Whether to make GameServerSpec not change after the ServiceQualityAction is executed.
    // When Permanent is true, regardless of the detection results, ServiceQualityAction will only be executed once.
    // When Permanent is false, ServiceQualityAction can be executed again even though ServiceQualityAction has been executed.
    Permanent            bool                   `json:"permanent"`
    
    // Corresponding actions to be executed for the service quality.
    ServiceQualityAction []ServiceQualityAction `json:"serviceQualityAction,omitempty"`
}

type ServiceQualityAction struct {
    // Defines to change the GameServerSpec field when the detection is true/false.
    State          bool `json:"state"`
    GameServerSpec `json:",inline"`
}
```

#### Network

```
type Network struct {
    // Different network types correspond to different network plugins.
    NetworkType string              `json:"networkType,omitempty"`

    // Different network types need to fill in different network parameters.
    NetworkConf []NetworkConfParams `json:"networkConf,omitempty"`
}

type NetworkConfParams KVParams

type KVParams struct {
    // Parameter name, the name is determined by the network plugin
    Name  string `json:"name,omitempty"`

    // Parameter value, the format is determined by the network plugin
    Value string `json:"value,omitempty"`
}
```

### GameServerSetStatus

```yaml
type GameServerSetStatus struct {
    // The iteration version of the GameServerSet observed by the controller.
    ObservedGeneration int64 `json:"observedGeneration,omitempty"`

    // The number of game servers.
    Replicas                int32  `json:"replicas"`

    // The number of game servers that are ready.
    ReadyReplicas           int32  `json:"readyReplicas"`

    // The number of game servers that are available.
    AvailableReplicas       int32  `json:"availableReplicas"`

    // The current number of game servers.
    CurrentReplicas         int32  `json:"currentReplicas"`

    // The number of game servers that have been updated.
    UpdatedReplicas         int32  `json:"updatedReplicas"`

    // The number of game servers that have been updated and are ready.
    UpdatedReadyReplicas    int32  `json:"updatedReadyReplicas,omitempty"`

    // The number of game servers that are in Maintaining state.
    MaintainingReplicas     *int32 `json:"maintainingReplicas,omitempty"`

    // The number of game servers that are in WaitToBeDeleted state.
    WaitToBeDeletedReplicas *int32 `json:"waitToBeDeletedReplicas,omitempty"`

    // The label selector used to query game servers that should match the replica count used by HPA.
    LabelSelector string `json:"labelSelector,omitempty"`
}

```


## GameServer

### GameServerSpec

```
type GameServerSpec struct {
   // The O&M state of the game server, not pod runtime state, more biased towards the state of the game itself.
   // Currently, the states that can be specified are: None / WaitToBeDeleted / Maintaining. 
   // Default is None
   OpsState         OpsState            `json:"opsState,omitempty"`

   // Update priority. If the priority is higher, it will be updated first.
   UpdatePriority   *intstr.IntOrString `json:"updatePriority,omitempty"`

   // Deletion priority. If the priority is higher, it will be deleted first.
   DeletionPriority *intstr.IntOrString `json:"deletionPriority,omitempty"`

   // Whether to perform network isolation and cut off the access layer network
   // Default is false
   // Optional override; when nil, inherit from GameServer template (defaults to false).
   NetworkDisabled  *bool               `json:"networkDisabled,omitempty"`
   
   // Containers can be used to make the corresponding GameServer container fields
   // different from the fields defined by GameServerTemplate in GameServerSetSpec.
   Containers []GameServerContainer `json:"containers,omitempty"`
}

type GameServerContainer struct {
	// Name indicates the name of the container to update.
	Name string `json:"name"`
	
	// Image indicates the image of the container to update.
	// When Image updated, pod.spec.containers[*].image will be updated immediately.
	Image string `json:"image,omitempty"`
	
	// Resources indicates the resources of the container to update.
	// When Resources updated, pod.spec.containers[*].Resources will be not updated immediately,
	// which will be updated when pod recreate.
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}
```

### GameServerStatus

```
type GameServerStatus struct {
    // Expected game server status
    DesiredState              GameServerState           `json:"desiredState,omitempty"`

    // The actual status of the current game server
    CurrentState              GameServerState           `json:"currentState,omitempty"`

    // network status information
    NetworkStatus             NetworkStatus             `json:"networkStatus,omitempty"`

    // The game server corresponds to the pod status
    PodStatus                 corev1.PodStatus          `json:"podStatus,omitempty"`

    // Service quality status of game server
    ServiceQualitiesCondition []ServiceQualityCondition `json:"serviceQualitiesConditions,omitempty"`

    // Current update priority
    UpdatePriority     *intstr.IntOrString `json:"updatePriority,omitempty"`

    // Current deletion priority
    DeletionPriority   *intstr.IntOrString `json:"deletionPriority,omitempty"`

    // Last change time
    LastTransitionTime metav1.Time         `json:"lastTransitionTime,omitempty"`
}
```
