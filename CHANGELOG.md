# Change Log

## v0.2.0

> Change log since v0.1.0

### Features

- Cloud Provider & Network Plugin mechanism


- Supporting network types:

  - HostPort
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

