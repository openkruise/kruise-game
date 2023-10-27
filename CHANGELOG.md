# Change Log

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

