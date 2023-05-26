# Change Log

## v0.3.0

> Change log since v0.2.0

### Features

- Add prometheus metrics and monitor dashboard for game servers. https://github.com/openkruise/kruise-game/pull/40
- Add external scaler to make the game servers in the WaitToBeDeleted opsState automatically deleted. https://github.com/openkruise/kruise-game/pull/39
- Support ReserveIds ScaleDownStrategyType (backfill the deleted Gs ID to the Reserve field). https://github.com/openkruise/kruise-game/pull/52
- Update AlibabaCloud API Group Version from v1alpha1 to v1beta1. https://github.com/openkruise/kruise-game/pull/41
- Add more print columns for GameServer & GameServerSet. https://github.com/openkruise/kruise-game/pull/48
- Add default serviceName for GameServerSet. https://github.com/openkruise/kruise-game/pull/51
- Add new networkType Kubernetes-Ingress. https://github.com/openkruise/kruise-game/pull/54
- Add network-related environment variables to allow users to adjust the network waiting time and detection interval. https://github.com/openkruise/kruise-game/pull/57

### Others

- Avoid GameServer status sync delay when network failure. https://github.com/openkruise/kruise-game/pull/45
- Avoid GameServerSet status sync failed when template metadata is not null. https://github.com/openkruise/kruise-game/pull/46
- Add marginal conditions to avoid fatal errors when scaling. https://github.com/openkruise/kruise-game/pull/49

---

## v0.2.0

> Change log since v0.1.0

### Features

- Cloud Provider & Network Plugin mechanism


- Supporting network types:

  - Kubernetes-HostPort
  - AlibabaCloud-NATGW
  - AlibabaCloud-SLB
  - AlibabaCloud-SLB-SharedPort

---

## v0.1.0

### Features

- New CRDs: GameServer & GameServerSet

  - GameServer provides game servers state definition interface, such as deletion priority, update priority, and opsState.
  - GameServerSet can update/scale GameServers by their states.


- User-Defined Quality Service

  - Probing GameServers‘ containers and marking GameServers state automatically

