## Feature overview

As mentioned in [Design concept of OpenKruiseGame](../core_concepts/design_concept.md), the access network of game servers is the main concern of game developers.
For a non-gateway architecture, game developers need to consider how to expose external IP addresses and ports of game servers for access by players.
Different network products are usually required for access in different scenarios, and the network products may be provided by different cloud service providers. This increases the complexity of the access network. Cloud Provider & Network Plugin of OpenKruiseGame is designed to resolve this issue.
OpenKruiseGame integrates different network plugins of different cloud service providers. You can use GameServerSet to set network parameters for game servers. Moreover, you can view network status information in the generated GameServer. This significantly reduces the complexity of the access network of game servers.

## Example

### Kubernetes-HostPort

OpenKruiseGame allows game servers to use the HostPort network in native Kubernetes clusters. The host where game servers are located exposes its external IP address and ports by using which Internet traffic is forwarded to the internal ports of the game servers. The following example shows the details:

Deploy the GameServerSet object that contains the network field.

```
cat <<EOF | kubectl apply -f -
apiVersion: game.kruise.io/v1alpha1
kind: GameServerSet
metadata:
  name: gs-hostport
  namespace: default
spec:
  replicas: 1
  updateStrategy:
    rollingUpdate:
      podUpdatePolicy: InPlaceIfPossible
  network:
    networkType: Kubernetes-HostPort
    networkConf:
    # The network configuration is specified in the form of a key-value pair. The network configuration is determined by the network plugin. Different network plugins correspond to different network configurations.
    - name: ContainerPorts
      # The value of ContainerPorts is in the following format: {containerName}:{port1}/{protocol1},{port2}/{protocol2},...
      value: "gameserver:80"
  gameServerTemplate:
    spec:
      containers:
        - image: registry.cn-hangzhou.aliyuncs.com/gs-demo/gameserver:network
          name: gameserver
EOF
```

Use the networkStatus field in the generated GameServer to view the network status information of the game server.

```shell
  networkStatus:
    createTime: "2022-11-23T10:57:01Z"
    currentNetworkState: Ready
    desiredNetworkState: Ready
    externalAddresses:
    - ip: 48.98.98.8
      ports:
      - name: gameserver-80
        port: 8211
        protocol: TCP
    internalAddresses:
    - ip: 172.16.0.8
      ports:
      - name: gameserver-80
        port: 80
        protocol: TCP
    lastTransitionTime: "2022-11-23T10:57:01Z"
    networkType: Kubernetes-HostPort
```

Clients can access the game server by using 48.98.98.8:8211.

### AlibabaCloud-NATGW

OpenKruiseGame supports the NAT gateway model of Alibaba Cloud. A NAT gateway exposes its external IP addresses and ports by using which Internet traffic is forwarded to pods. The following example shows the details:

```shell
cat <<EOF | kubectl apply -f -
apiVersion: game.kruise.io/v1alpha1
kind: GameServerSet
metadata:
  name: gs-natgw
  namespace: default
spec:
  replicas: 1
  updateStrategy:
    rollingUpdate:
      podUpdatePolicy: InPlaceIfPossible
  network:
    networkType: AlibabaCloud-NATGW
    networkConf:
    - name: Ports
      # The ports to be exposed. The value is in the following format: {port1},{port2}...
      value: "80"
    - name: Protocol
      # The protocol. The value is TCP by default.
      value: "TCP"
#   - name: Fixed
# Specify whether the mapping relationship is fixed. By default, the mapping relationship is not fixed, that is, a new external IP address and port are generated after the pod is deleted.
#     value: true
  gameServerTemplate:
    spec:
      containers:
        - image: registry.cn-hangzhou.aliyuncs.com/gs-demo/gameserver:network
          name: gameserver
EOF
```

Use the networkStatus field in the generated GameServer to view the network status information of the game server.

```shell
  networkStatus:
    createTime: "2022-11-23T11:21:34Z"
    currentNetworkState: Ready
    desiredNetworkState: Ready
    externalAddresses:
    - ip: 47.97.227.137
      ports:
      - name: "80"
        port: "512"
        protocol: TCP
    internalAddresses:
    - ip: 172.16.0.189
      ports:
      - name: "80"
        port: "80"
        protocol: TCP
    lastTransitionTime: "2022-11-23T11:21:34Z"
    networkType: AlibabaCloud-NATGW
```

Clients can access the game server by using 47.97.227.137:512.

## Network plugins

OpenKruiseGame supports the following network plugins:
- Kubernetes-HostPort
- AlibabaCloud-NATGW
- AlibabaCloud-SLB
- AlibabaCloud-SLB-SharedPort

---

### Plugin name

`Kubernetes-HostPort`

### Cloud Provider

Kubernetes

### Plugin description
- HostPort enables game servers to be accessed from the Internet by forwarding Internet traffic to the game servers by using the external IP address and ports exposed by the host where the game servers are located. The exposed IP address of the host must be a public IP address so that the host can be accessed from the Internet.

- In the configuration file, you can specify a custom range of available host ports. The default port range is 8000 to 9000. This network plugin can help you allocate and manage host ports to prevent port conflicts.

- This network plugin does not support network isolation.

### Network parameters

ContainerPorts

- Meaning: the name of the container that provides services, the ports to be exposed, and the protocols.
- Value: in the format of containerName:port1/protocol1,port2/protocol2,... The protocol names must be in uppercase letters. Example: `game-server:25565/TCP`.
- Configuration change supported or not: no. The value of this parameter is effective until the pod lifecycle ends.

### Plugin configuration

```
[kubernetes]
enable = true
[kubernetes.hostPort]
# Specify the range of available ports of the host. Ports in this range can be used to forward Internet traffic to pods.
max_port = 9000
min_port = 8000
```

---

### Plugin name

`AlibabaCloud-NATGW`

### Cloud Provider

AlibabaCloud

### Plugin description

- AlibabaCloud-NATGW enables game servers to be accessed from the Internet by using an Internet NAT gateway of Alibaba Cloud. Internet traffic is forwarded to the corresponding game servers based on DNAT rules.

- This network plugin does not support network isolation.

### Network parameters

Ports

- Meaning: the ports in the pod to be exposed.
- Value: in the format of port1,port2,port3… Example: 80,8080,8888.
- Configuration change supported or not: no.

Protocol

- Meaning: the network protocol.
- Value: an example value can be tcp. The value is tcp by default.
- Configuration change supported or not: no.

Fixed

- Meaning: whether the mapping relationship is fixed. If the mapping relationship is fixed, the mapping relationship remains unchanged even if the pod is deleted and recreated.
- Value: false or true.
- Configuration change supported or not: no.

### Plugin configuration

None

---

### Plugin name

`AlibabaCloud-SLB`

### Cloud Provider

AlibabaCloud

### Plugin description

- AlibabaCloud-SLB enables game servers to be accessed from the Internet by using Layer 4 Classic Load Balancer (CLB) of Alibaba Cloud. CLB is a type of Server Load Balancer (SLB). AlibabaCloud-SLB uses different ports of the same CLB instance to forward Internet traffic to different game servers. The CLB instance only forwards traffic, but does not implement load balancing.

- This network plugin supports network isolation.

Related design: https://github.com/openkruise/kruise-game/issues/20

### Network parameters

SlbIds

- Meaning: the CLB instance ID. You can specify only one CLB instance ID. Multiple CLB instance IDs will be supported in the future.
- Value: an example value can be lb-9zeo7prq1m25ctpfrw1m7.
- Configuration change supported or not: no. The configuration change can be supported in future.

PortProtocols

- Meaning: the ports in the pod to be exposed and the protocols. You can specify multiple ports and protocols.
- Value: in the format of port1/protocol1,port2/protocol2,... The protocol names must be in uppercase letters.
- Configuration change supported or not: no. The configuration change can be supported in future.

Fixed

- Meaning: whether the mapping relationship is fixed. If the mapping relationship is fixed, the mapping relationship remains unchanged even if the pod is deleted and recreated.
- Value: false or true.
- Configuration change supported or not: no.

### Plugin configuration
```
[alibabacloud]
enable = true
[alibabacloud.slb]
# Specify the range of available ports of the CLB instance. Ports in this range can be used to forward Internet traffic to pods. In this example, the range includes 200 ports.
max_port = 700
min_port = 500
```

---

### Plugin name

`AlibabaCloud-SLB-SharedPort`

### Cloud Provider

AlibabaCloud

### Plugin description

- AlibabaCloud-SLB-SharedPort enables game servers to be accessed from the Internet by using Layer 4 CLB of Alibaba Cloud. Unlike AlibabaCloud-SLB, `AlibabaCloud-SLB-SharedPort` uses the same port of a CLB instance to forward traffic to game servers, and the CLB instance implements load balancing.
  This network plugin applies to stateless network services, such as proxy or gateway, in gaming scenarios.

- This network plugin supports network isolation.

### Network parameters

SlbIds

- Meaning: the CLB instance IDs. You can specify multiple CLB instance IDs.
- Value: an example value can be lb-9zeo7prq1m25ctpfrw1m7.
- Configuration change supported or not: yes.

PortProtocols

- Meaning: the ports in the pod to be exposed and the protocols. You can specify multiple ports and protocols.
- Value: in the format of port1/protocol1,port2/protocol2,... The protocol names must be in uppercase letters.
- Configuration change supported or not: no. The configuration change can be supported in future.

### Plugin configuration

None
