# Change Log

## v1.0.0
> Change log since v0.10.0

### Features & Enhancements
- add support svc external traffic policy for alibabacloud slb https://github.com/openkruise/kruise-game/pull/194
- alibabacloud slb support map same TCP and UDP port https://github.com/openkruise/kruise-game/pull/197
- feat: add annotation of opsState-last-changed-time https://github.com/openkruise/kruise-game/pull/200
- Add hwcloud provider and elb plugin https://github.com/openkruise/kruise-game/pull/201
- Increase the upper limit of ali-nlb ports https://github.com/openkruise/kruise-game/pull/204
- Add index-offset-scheduler https://github.com/openkruise/kruise-game/pull/205
- enhance: create service of ali-multi-nlbs in https://github.com/openkruise/kruise-game/pull/207
- feat: support multi groups for nlbs https://github.com/openkruise/kruise-game/pull/213
- feat: support range type for ReserveIDs https://github.com/openkruise/kruise-game/pull/209
- ServiceQualities support serverless pod https://github.com/openkruise/kruise-game/pull/212
- enhance: support svc external traffic policy for AlibabaCloud-Multi-NLBs https://github.com/openkruise/kruise-game/pull/216
- enhance: add network ready condition for AlibabaCloud-Multi-NLBs plugin https://github.com/openkruise/kruise-game/pull/214
- feat: add eip provider of VKE https://github.com/openkruise/kruise-game/pull/218
- feat(metrics): improve observability for GameServersOpsStateCount metrics https://github.com/openkruise/kruise-game/pull/221
- feat: support minAvailable percentage type https://github.com/openkruise/kruise-game/pull/222
- enhance: activity of externalscaler relate to minAvailable https://github.com/openkruise/kruise-game/pull/228
- enhance: Kubernetes-HostPort support container port same as host https://github.com/openkruise/kruise-game/pull/230
- enhance: AlibabaCloud-SLB-SharedPort plugin support managed services https://github.com/openkruise/kruise-game/pull/224
- cancel the limit of Ali NLB port range https://github.com/openkruise/kruise-game/pull/235
- update TencentCloud-CLB plugin https://github.com/openkruise/kruise-game/pull/239
- feat: volcengine-clb plugin support EnableClbScatter https://github.com/openkruise/kruise-game/pull/241
- feat(Kubernetes-HostPort): support TCPUDP protocol https://github.com/openkruise/kruise-game/pull/244
- feat: add annotation of state-last-changed-time https://github.com/openkruise/kruise-game/pull/238
- Add PersistentVolumeClaimRetentionPolicy support to GameServerSet https://github.com/openkruise/kruise-game/pull/243
- feat: support user-defined number of controller workers https://github.com/openkruise/kruise-game/pull/247
- feat: support new plugin named AlibabaCloud-AutoNLBs https://github.com/openkruise/kruise-game/pull/246
- AlibabaCloud-AutoNLBs support multi intranet type eip https://github.com/openkruise/kruise-game/pull/248
- feat: add PreDeleteReplicas for GameServerSet status https://github.com/openkruise/kruise-game/pull/254
- feat: support EnableMultiIngress for vke https://github.com/openkruise/kruise-game/pull/251
- feat: add enable-cert-generation option https://github.com/openkruise/kruise-game/pull/245
- enhance: network trigger time adapts to different time zones https://github.com/openkruise/kruise-game/pull/259

### Bug Fixes
- fix: support auto-scaling when replicas is 0 https://github.com/openkruise/kruise-game/pull/225
- fix: update ppmHash when ServiceQualities changed https://github.com/openkruise/kruise-game/pull/226
- fix the external scaler error when minAvailable is 0 https://github.com/openkruise/kruise-game/pull/227
- fix old svc remain after pod recreate when using Volcengine-CLB https://github.com/openkruise/kruise-game/pull/233
- fix duplicated port for Volcengine-CLB plugin https://github.com/openkruise/kruise-game/pull/240
- bugfix: gs state should be changed from PreDelete to Deleting https://github.com/openkruise/kruise-game/pull/252
- fix the meaning of CURRENT printcolumn when using kubectl https://github.com/openkruise/kruise-game/pull/253
- bugfix: consider preDelete pods when scaling https://github.com/openkruise/kruise-game/pull/257
- bugfix(Kubernetes-HostPort): allow pod update when node notfound https://github.com/openkruise/kruise-game/pull/260

### Deps
- update workflow ci go cache to v4 https://github.com/openkruise/kruise-game/pull/206
- deps: update to k8s 0.30.10 https://github.com/openkruise/kruise-game/pull/210
- update ci workflow to ubuntu-24.04 https://github.com/openkruise/kruise-game/pull/215

## v0.10.0
> Change log since v0.9.0

### Features & Enhancements
- Feat: add tencent cloud provider and clb plugin. https://github.com/openkruise/kruise-game/pull/179
- Enhance: update kruise-api to v1.7.1 https://github.com/openkruise/kruise-game/pull/173
- Enhance: add block ports config for AlibabaCloud LB network models. https://github.com/openkruise/kruise-game/pull/175
- Enhance: add block port in volc engine. https://github.com/openkruise/kruise-game/pull/182
- Feat: add jdcloud provider and the nlb&eip plugin. https://github.com/openkruise/kruise-game/pull/180
- Enhance: support network isolation for tencentcloud clb plugin. https://github.com/openkruise/kruise-game/pull/183
- Feat: add maxAvailable param for external scaler. https://github.com/openkruise/kruise-game/pull/190
- Feat: add new networkType named AlibabaCloud-Multi-NLBs. https://github.com/openkruise/kruise-game/pull/187

### Bug Fixes
- Reconstruct the logic of GameServers scaling. https://github.com/openkruise/kruise-game/pull/171
- Semantic fixes for network port ranges. https://github.com/openkruise/kruise-game/pull/181

## v0.9.0
> Change log since v0.8.0

### Features & Enhancements
- Enhance: support custom health checks for AlibabaCloud-NLB. https://github.com/openkruise/kruise-game/pull/147
- Feat: add AmazonWebServices-NLB network plugin. https://github.com/openkruise/kruise-game/pull/150
- Enhance: support custom health checks for AlibabaCloud-SLB. https://github.com/openkruise/kruise-game/pull/154
- Enhance: Kubernetes-NodePort supports network disabled. https://github.com/openkruise/kruise-game/pull/156
- Enhance: check networkType when create GameServerSet. https://github.com/openkruise/kruise-game/pull/157
- Enhance: service quality support patch labels & annotations. https://github.com/openkruise/kruise-game/pull/159
- Enhance: labels from gs can be synced to pod. https://github.com/openkruise/kruise-game/pull/160
- Feat: add lifecycle field for gameserverset. https://github.com/openkruise/kruise-game/pull/162

### Bug Fixes
- Fix the allocation error when Ali loadbalancers reache the limit of ports number. https://github.com/openkruise/kruise-game/pull/149
- Fix: AmazonWebServices-NLB controller parameter modification. https://github.com/openkruise/kruise-game/pull/164
- Fix old svc remain after pod recreate when using ali-lb models. https://github.com/openkruise/kruise-game/pull/165

## v0.8.0
> Change log since v0.7.0

### Features & Enhancements
- Add AlibabaCloud-NLB network plugin. https://github.com/openkruise/kruise-game/pull/135
- Add Volcengine-CLB network plugin. https://github.com/openkruise/kruise-game/pull/127
- Add Kubernetes-NodePort network plugin. https://github.com/openkruise/kruise-game/pull/138
- Sync annotations from gs to pod. https://github.com/openkruise/kruise-game/pull/140
- FailurePolicy of PodMutatingWebhook turn to Fail. https://github.com/openkruise/kruise-game/pull/129
- Replace patch asts with update. https://github.com/openkruise/kruise-game/pull/131
- Kubernetes-HostPort plugin support to wait for network ready. https://github.com/openkruise/kruise-game/pull/136
- Add AllocateLoadBalancerNodePorts in clb plugin. https://github.com/openkruise/kruise-game/pull/141
### Bug Fixes
- Avoid patching gameserver continuously. https://github.com/openkruise/kruise-game/pull/124

## v0.7.0
> Change log since v0.6.0

### Features & Enhancements
- Add ReclaimPolicy for GameServer. https://github.com/openkruise/kruise-game/pull/115
- ServiceQuality supports multiple results returned by one probe. https://github.com/openkruise/kruise-game/pull/117
- Support differentiated updates to GameServers. https://github.com/openkruise/kruise-game/pull/120

### Bug Fixes
- Fix the error of patching pod image failure when gs image is nil. https://github.com/openkruise/kruise-game/pull/121

## v0.6.0
> Change log since v0.5.0

### Features & Enhancements
- Support auto scaling-up based on minAvailable. https://github.com/openkruise/kruise-game/pull/88
- Update go version to 1.19. https://github.com/openkruise/kruise-game/pull/104
- Add GameServerConditions. https://github.com/openkruise/kruise-game/pull/95
- Add network plugin AlibabaCloud-NLB-SharedPort. https://github.com/openkruise/kruise-game/pull/98
- Support AllowNotReadyContainers for network plugin. https://github.com/openkruise/kruise-game/pull/98
- Add qps and burst settings of controller-manager. https://github.com/openkruise/kruise-game/pull/108

### Bug Fixes
- Fix AlibabaCloud-NATGW network ready condition when multi-ports. https://github.com/openkruise/kruise-game/pull/94
- Hostport network should be not ready when no ports exist. https://github.com/openkruise/kruise-game/pull/100

## v0.5.0
> Change log since v0.4.0

### Features & Enhancements
- Improve hostport cache to record allocated ports of pod. https://github.com/openkruise/kruise-game/pull/82
- Enhance pod scaling efficiency. https://github.com/openkruise/kruise-game/pull/81
- Support to sync gs metadata from from gsTemplate. https://github.com/openkruise/kruise-game/pull/85
- Refactor NetworkPortRange into a pointer. https://github.com/openkruise/kruise-game/pull/87
- Add network plugin AlibabaCloud-EIP. https://github.com/openkruise/kruise-game/pull/86
- Add new opsState type named Allocated. https://github.com/openkruise/kruise-game/pull/89
- Add new opsState type named Kill. https://github.com/openkruise/kruise-game/pull/90
- AlibabaCloud-EIP support to define EIP name & description. https://github.com/openkruise/kruise-game/pull/91
- Support customized serviceName. https://github.com/openkruise/kruise-game/pull/92

### Bug Fixes
- correct gs network status when pod network status is nil. https://github.com/openkruise/kruise-game/pull/80 

## v0.4.0

> Change log since v0.3.0

### Features & Enhancements
- Avoid Network NotReady too long for Kubernetes-Ingress NetworkType. https://github.com/openkruise/kruise-game/pull/61/commits/7f66be508004d393299e037cba052c5111d6678a
- Add param Fixed for Kubernetes-Ingress NetworkType. https://github.com/openkruise/kruise-game/pull/61/commits/00c8d5502f1813f0092f05ffb3b0c2dbca5a63f7
- Change autoscaler trigger metricType from Value to AverageValue. https://github.com/openkruise/kruise-game/pull/64
- Decouple triggering network update from Gs Ready. https://github.com/openkruise/kruise-game/pull/71
- Add reserveIds when init asts. https://github.com/openkruise/kruise-game/pull/73
- AlibabaCloud-SLB support muti-slbIds. https://github.com/openkruise/kruise-game/pull/69/commits/42b8ab3e739c872f477d4faa7286ace3a87a07d6


### Bug Fixes
- Fix the issue of unable to complete scaling down from 1 to 0 when autoscaling. https://github.com/openkruise/kruise-game/pull/59
- Avoid metrics controller fatal when gss status is nil. https://github.com/openkruise/kruise-game/pull/61/commits/c56574ecaf5af1e5d0625f4555a470c90125f4d4
- Fix the problem that the hostPort plugin repeatedly allocates ports when pods with the same name are created. https://github.com/openkruise/kruise-game/pull/66
- Assume that slices without elements are equal to avoid marginal problem. https://github.com/openkruise/kruise-game/pull/74
- Fix AlibabaCloud-SLB allocate and deallocate imprecisely. https://github.com/openkruise/kruise-game/pull/69/commits/05fea83e08a39f496cb9f93a83a0775460cb160a


## v0.3.0

> Change log since v0.2.0

### Features & Enhancements

- Add prometheus metrics and monitor dashboard for game servers. https://github.com/openkruise/kruise-game/pull/40
- Add external scaler to make the game servers in the WaitToBeDeleted opsState automatically deleted. https://github.com/openkruise/kruise-game/pull/39
- Support ReserveIds ScaleDownStrategyType (backfill the deleted Gs ID to the Reserve field). https://github.com/openkruise/kruise-game/pull/52
- Update AlibabaCloud API Group Version from v1alpha1 to v1beta1. https://github.com/openkruise/kruise-game/pull/41
- Add more print columns for GameServer & GameServerSet. https://github.com/openkruise/kruise-game/pull/48
- Add default serviceName for GameServerSet. https://github.com/openkruise/kruise-game/pull/51
- Add new networkType Kubernetes-Ingress. https://github.com/openkruise/kruise-game/pull/54
- Add network-related environment variables to allow users to adjust the network waiting time and detection interval. https://github.com/openkruise/kruise-game/pull/57

### Bug Fixes

- Avoid GameServer status sync delay when network failure. https://github.com/openkruise/kruise-game/pull/45
- Avoid GameServerSet status sync failed when template metadata is not null. https://github.com/openkruise/kruise-game/pull/46
- Add marginal conditions to avoid fatal errors when scaling. https://github.com/openkruise/kruise-game/pull/49

## v0.2.0

> Change log since v0.1.0

### Features

- Cloud Provider & Network Plugin mechanism


- Supporting network types:

  - Kubernetes-HostPort
  - AlibabaCloud-NATGW
  - AlibabaCloud-SLB
  - AlibabaCloud-SLB-SharedPort

## v0.1.0

### Features

- New CRDs: GameServer & GameServerSet

  - GameServer provides game servers state definition interface, such as deletion priority, update priority, and opsState.
  - GameServerSet can update/scale GameServers by their states.


- User-Defined Quality Service

  - Probing GameServers‘ containers and marking GameServers state automatically

