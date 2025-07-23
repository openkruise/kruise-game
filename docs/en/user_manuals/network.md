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
- Volcengine-EIP

---

### Kubernetes-HostPort

#### Plugin name

`Kubernetes-HostPort`

#### Cloud Provider

Kubernetes

#### Plugin description
- HostPort enables game servers to be accessed from the Internet by forwarding Internet traffic to the game servers by using the external IP address and ports exposed by the host where the game servers are located. The exposed IP address of the host must be a public IP address so that the host can be accessed from the Internet.

- In the configuration file, you can specify a custom range of available host ports. The default port range is 8000 to 9000. This network plugin can help you allocate and manage host ports to prevent port conflicts.

- This network plugin does not support network isolation.

#### Network parameters

ContainerPorts

- Meaning: the name of the container that provides services, the ports to be exposed, and the protocols.
- Value: in the format of containerName:port1/protocol1,port2/protocol2,... The protocol names must be in uppercase letters. Example: `game-server:25565/TCP`.
- Configuration change supported or not: no. The value of this parameter is effective until the pod lifecycle ends.

#### Plugin configuration

```
[kubernetes]
enable = true
[kubernetes.hostPort]
# Specify the range of available ports of the host. Ports in this range can be used to forward Internet traffic to pods.
max_port = 9000
min_port = 8000
```

---

### Kubernetes-Ingress

#### Plugin name

`Kubernetes-Ingress`

#### Cloud Provider

Kubernetes

#### Plugin description

- OKG provides the Ingress network model for games such as H5 games that require the application layer network model. This plugin will automatically set the corresponding path for each game server, which is related to the game server ID and is unique for each game server.

- This network plugin does not support network isolation.

#### Network parameters

PathType

- Meaning: Path type. Same as the PathType field in HTTPIngressPath.
- Value format: Same as the PathType field in HTTPIngressPath. 
- Configuration change supported or not: yes.

Path

- Meaning: Access path. Each game server has its own access path based on its ID.
- Value format: Add \<id> to any position in the original path(consistent with the Path field in HTTPIngressPath), and the plugin will generate the path corresponding to the game server ID. For example, when setting the path to /game\<id>, the path for game server 0 is /game0, the path for game server 1 is /game1, and so on.
- Configuration change supported or not: yes.

Port

- Meaning: Port value exposed by the game server.
- Value format: port number
- Configuration change supported or not: yes.

IngressClassName

- Meaning: Specify the name of the IngressClass. Same as the IngressClassName field in IngressSpec.
- Value format: Same as the IngressClassName field in IngressSpec.
- Configuration change supported or not: yes.

Host

- Meaning: Domain name. Same as the Host field in IngressRule.
- Value format: Same as the Host field in IngressRule.
- Configuration change supported or not: yes.

TlsHosts

- Meaning: List of hosts containing TLS certificates. Similar to the Hosts field in IngressTLS.
- Value format: host1,host2,... For example, xxx.xx1.com,xxx.xx2.com
- Configuration change supported or not: yes.

TlsSecretName

- Meaning: Same as the SecretName field in IngressTLS.
- Value format: Same as the SecretName field in IngressTLS.
- Configuration change supported or not: yes.

Annotation

- Meaning: as an annotation of the Ingress object
- Value format: key: value (note the space after the colon), for example: nginx.ingress.kubernetes.io/rewrite-target: /$2
- Configuration change supported or not: yes.

Fixed

- Meaning: whether the ingress object is still retained when the pod is deleted
- Value format: true / false
- Configuration change supported or not: yes.

_additional explanation_

- If you want to fill in multiple annotations, you can define multiple slices named Annotation in the networkConf.
- Supports filling in multiple paths. The path, path type, and port correspond one-to-one in the order of filling. When the number of paths is greater than the number of path types(or port), non-corresponding paths will match the path type(or port) that was filled in first.


#### Plugin configuration

None

#### Example

Set GameServerSet.Spec.Network:

```yaml
  network:
    networkConf:
    - name: IngressClassName
      value: nginx
    - name: Port
      value: "80"
    - name: Path
      value: /game<id>(/|$)(.*)
    - name: Path
      value: /test-<id>
    - name: Host
      value: test.xxx.cn-hangzhou.ali.com
    - name: PathType
      value: ImplementationSpecific
    - name: TlsHosts
      value: xxx.xx1.com,xxx.xx2.com
    - name: Annotation
      value: 'nginx.ingress.kubernetes.io/rewrite-target: /$2'
    - name: Annotation
      value: 'nginx.ingress.kubernetes.io/random: xxx'
    networkType: Kubernetes-Ingress

```
This will generate a service and an ingress object for each replica of GameServerSet. The configuration for the ingress of the 0th game server is shown below:

```yaml
spec:
  ingressClassName: nginx
  rules:
  - host: test.xxx.cn-hangzhou.ali.com
    http:
      paths:
      - backend:
          service:
            name: ing-nginx-0
            port:
              number: 80
        path: /game0(/|$)(.*)
        pathType: ImplementationSpecific
      - backend:
          service:
            name: ing-nginx-0
            port:
              number: 80
        path: /test-0
        pathType: ImplementationSpecific
  tls:
  - hosts:
    - xxx.xx1.com
    - xxx.xx2.com
status:
  loadBalancer:
    ingress:
    - ip: 47.xx.xxx.xxx
```

The other GameServers only have different path fields and service names, while the other generated parameters are the same.

The network status of GameServer is as follows:

```yaml
  networkStatus:
    createTime: "2023-04-28T14:00:30Z"
    currentNetworkState: Ready
    desiredNetworkState: Ready
    externalAddresses:
    - ip: 47.xx.xxx.xxx
      ports:
      - name: /game0(/|$)(.*)
        port: 80
        protocol: TCP
      - name: /test-0
        port: 80
        protocol: TCP
    internalAddresses:
    - ip: 10.xxx.x.xxx
      ports:
      - name: /game0(/|$)(.*)
        port: 80
        protocol: TCP
      - name: /test-0
        port: 80
        protocol: TCP
    lastTransitionTime: "2023-04-28T14:00:30Z"
    networkType: Kubernetes-Ingress
```

---

### AlibabaCloud-NATGW

#### Plugin name

`AlibabaCloud-NATGW`

#### Cloud Provider

AlibabaCloud

#### Plugin description

- AlibabaCloud-NATGW enables game servers to be accessed from the Internet by using an Internet NAT gateway of Alibaba Cloud. Internet traffic is forwarded to the corresponding game servers based on DNAT rules.

- This network plugin does not support network isolation.

#### Network parameters

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

#### Plugin configuration

None

---

### AlibabaCloud-SLB

#### Plugin name

`AlibabaCloud-SLB`

#### Cloud Provider

AlibabaCloud

#### Plugin description

- AlibabaCloud-SLB enables game servers to be accessed from the Internet by using Layer 4 Classic Load Balancer (CLB) of Alibaba Cloud. CLB is a type of Server Load Balancer (SLB). AlibabaCloud-SLB uses different ports of the same CLB instance to forward Internet traffic to different game servers. The CLB instance only forwards traffic, but does not implement load balancing.

- This network plugin supports network isolation.

Related design: https://github.com/openkruise/kruise-game/issues/20

#### Network parameters

SlbIds

- Meaning: the CLB instance ID. You can fill in multiple ids.
- Value: in the format of slbId-0,slbId-1,... An example value can be "lb-9zeo7prq1m25ctpfrw1m7,lb-bp1qz7h50yd3w58h2f8je"
- Configuration change supported or not: yes. You can add new slbIds at the end. However, it is recommended not to change existing slbId that is in use.

PortProtocols

- Meaning: the ports in the pod to be exposed and the protocols. You can specify multiple ports and protocols.
- Value: in the format of port1/protocol1,port2/protocol2,...  (same protocol port should like 8000/TCPUDP) The protocol names must be in uppercase letters.
- Configuration change supported or not: yes.

Fixed

- Meaning: whether the mapping relationship is fixed. If the mapping relationship is fixed, the mapping relationship remains unchanged even if the pod is deleted and recreated.
- Value: false or true.
- Configuration change supported or not: yes.

ExternalTrafficPolicyType

- Meaning: Service LB forward type, if Local， Service LB just forward traffice to local node Pod, we can keep source IP without SNAT
- Value: : Local/Cluster Default value is Cluster
- Configuration change supported or not: not. It maybe related to "IP/Port mapping relationship Fixed", recommend not to change

AllowNotReadyContainers

- Meaning: the container names that are allowed not ready when inplace updating, when traffic will not be cut.
- Value: {containerName_0},{containerName_1},... Example：sidecar
- Configuration change supported or not: It cannot be changed during the in-place updating process.

LBHealthCheckSwitch

- Meaning：Whether to enable health check
- Format："on" means on, "off" means off. Default is on
- Whether to support changes: Yes

LBHealthCheckFlag

- Meaning: Whether to enable http type health check
- Format: "on" means on, "off" means off. Default is on
- Whether to support changes: Yes

LBHealthCheckType

- Meaning: Health Check Protocol
- Format: fill in "tcp" or "http", the default is tcp
- Whether to support changes: Yes

LBHealthCheckConnectTimeout

- Meaning: Maximum timeout for health check response.
- Format: Unit: seconds. The value range is [1, 300]. The default value is "5"
- Whether to support changes: Yes

LBHealthyThreshold

- Meaning: After the number of consecutive successful health checks, the health check status of the server will be determined from failure to success.
- Format: Value range [2, 10]. Default value is "2"
- Whether to support changes: Yes

LBUnhealthyThreshold

- Meaning: After the number of consecutive health check failures, the health check status of the server will be determined from success to failure.
- Format: Value range [2, 10]. The default value is "2"
- Whether to support changes: Yes

LBHealthCheckInterval

- Meaning: health check interval.
- Format: Unit: seconds. The value range is [1, 50]. The default value is "10"
- Whether to support changes: Yes

LBHealthCheckProtocolPort

- Meaning：the protocols & ports of HTTP type health check.
- Format：Multiple values are separated by ','. e.g. https:443,http:80
- Whether to support changes: Yes

LBHealthCheckUri

- Meaning: The corresponding uri when the health check type is HTTP.
- Format: The length is 1~80 characters, only letters, numbers, and characters can be used. Must start with a forward slash (/). Such as "/test/index.html"
- Whether to support changes: Yes

LBHealthCheckDomain

- Meaning: The corresponding domain name when the health check type is HTTP.
- Format: The length of a specific domain name is limited to 1~80 characters. Only lowercase letters, numbers, dashes (-), and half-width periods (.) can be used.
- Whether to support changes: Yes

LBHealthCheckMethod

- Meaning: The corresponding method when the health check type is HTTP.
- Format: "GET" or "HEAD"
- Whether to support changes: Yes

#### Plugin configuration
```
[alibabacloud]
enable = true
[alibabacloud.slb]
# Specify the range of available ports of the CLB instance. Ports in this range can be used to forward Internet traffic to pods. In this example, the range includes 200 ports.
max_port = 700
min_port = 500
```

---

### AlibabaCloud-SLB-SharedPort

#### Plugin name

`AlibabaCloud-SLB-SharedPort`

#### Cloud Provider

AlibabaCloud

#### Plugin description

- AlibabaCloud-SLB-SharedPort enables game servers to be accessed from the Internet by using Layer 4 CLB of Alibaba Cloud. Unlike AlibabaCloud-SLB, `AlibabaCloud-SLB-SharedPort` uses the same port of a CLB instance to forward traffic to game servers, and the CLB instance implements load balancing.
  This network plugin applies to stateless network services, such as proxy or gateway, in gaming scenarios.

- This network plugin supports network isolation.

#### Network parameters

SlbIds

- Meaning: the CLB instance IDs. You can specify multiple CLB instance IDs.
- Value: an example value can be lb-9zeo7prq1m25ctpfrw1m7.
- Configuration change supported or not: yes.

PortProtocols

- Meaning: the ports in the pod to be exposed and the protocols. You can specify multiple ports and protocols.
- Value: in the format of port1/protocol1,port2/protocol2,... The protocol names must be in uppercase letters.
- Configuration change supported or not: no. The configuration change can be supported in future.

AllowNotReadyContainers

- Meaning: the container names that are allowed not ready when inplace updating, when traffic will not be cut.
- Value: {containerName_0},{containerName_1},... Example：sidecar
- Configuration change supported or not: It cannot be changed during the in-place updating process.

#### Plugin configuration

None

---

### AlibabaCloud-NLB
#### Plugin name

`AlibabaCloud-NLB`

#### Cloud Provider

AlibabaCloud

#### Plugin description

- AlibabaCloud-NLB enables game servers to be accessed from the Internet by using Layer 4 Network Load Balancer (NLB) of Alibaba Cloud. AlibabaCloud-NLB uses different ports of the same NLB instance to forward Internet traffic to different game servers. The NLB instance only forwards traffic, but does not implement load balancing.

- This network plugin supports network isolation.

#### Network parameters

NlbIds

- Meaning: the NLB instance ID. You can fill in multiple ids.
- Value: in the format of nlbId-0,nlbId-1,... An example value can be "nlb-ji8l844c0qzii1x6mc,nlb-26jbknebrjlejt5abu"
- Configuration change supported or not: yes. You can add new nlbIds at the end. However, it is recommended not to change existing nlbId that is in use.

PortProtocols

- Meaning: the ports in the pod to be exposed and the protocols. You can specify multiple ports and protocols.
- Value: in the format of port1/protocol1,port2/protocol2,... The protocol names must be in uppercase letters.
- Configuration change supported or not: yes.

Fixed

- Meaning: whether the mapping relationship is fixed. If the mapping relationship is fixed, the mapping relationship remains unchanged even if the pod is deleted and recreated.
- Value: false or true.
- Configuration change supported or not: yes.

AllowNotReadyContainers

- Meaning: the container names that are allowed not ready when inplace updating, when traffic will not be cut.
- Value: {containerName_0},{containerName_1},... Example：sidecar
- Configuration change supported or not: It cannot be changed during the in-place updating process.

LBHealthCheckFlag

- Meaning: Whether to enable health check
- Format: "on" means on, "off" means off. Default is on
- Whether to support changes: Yes

LBHealthCheckType

- Meaning: Health Check Protocol
- Format: fill in "tcp" or "http", the default is tcp
- Whether to support changes: Yes

LBHealthCheckConnectPort

- Meaning: Server port for health check.
- Format: Value range [0, 65535]. Default value is "0"
- Whether to support changes: Yes

LBHealthCheckConnectTimeout

- Meaning: Maximum timeout for health check response.
- Format: Unit: seconds. The value range is [1, 300]. The default value is "5"
- Whether to support changes: Yes

LBHealthyThreshold

- Meaning: After the number of consecutive successful health checks, the health check status of the server will be determined from failure to success.
- Format: Value range [2, 10]. Default value is "2"
- Whether to support changes: Yes

LBUnhealthyThreshold

- Meaning: After the number of consecutive health check failures, the health check status of the server will be determined from success to failure.
- Format: Value range [2, 10]. The default value is "2"
- Whether to support changes: Yes

LBHealthCheckInterval

- Meaning: health check interval.
- Format: Unit: seconds. The value range is [1, 50]. The default value is "10"
- Whether to support changes: Yes

LBHealthCheckUri

- Meaning: The corresponding uri when the health check type is HTTP.
- Format: The length is 1~80 characters, only letters, numbers, and characters can be used. Must start with a forward slash (/). Such as "/test/index.html"
- Whether to support changes: Yes

LBHealthCheckDomain

- Meaning: The corresponding domain name when the health check type is HTTP.
- Format: The length of a specific domain name is limited to 1~80 characters. Only lowercase letters, numbers, dashes (-), and half-width periods (.) can be used.
- Whether to support changes: Yes

LBHealthCheckMethod

- Meaning: The corresponding method when the health check type is HTTP.
- Format: "GET" or "HEAD"
- Whether to support changes: Yes

#### Plugin configuration
```
[alibabacloud]
enable = true
[alibabacloud.nlb]
# Specify the range of available ports of the NLB instance. Ports in this range can be used to forward Internet traffic to pods. In this example, the range includes 500 ports.
max_port = 1500
min_port = 1000
```

#### Example

```
cat <<EOF | kubectl apply -f -
apiVersion: game.kruise.io/v1alpha1
kind: GameServerSet
metadata:
  name: gs-nlb
  namespace: default
spec:
  replicas: 1
  updateStrategy:
    rollingUpdate:
      podUpdatePolicy: InPlaceIfPossible
  network:
    networkConf:
    - name: NlbIds
      value: nlb-muyo7fv6z646ygcxxx
    - name: PortProtocols
      value: "80"
    - name: Fixed
      value: "true"
    networkType: AlibabaCloud-NLB
  gameServerTemplate:
    spec:
      containers:
        - image: registry.cn-hangzhou.aliyuncs.com/gs-demo/gameserver:network
          name: gameserver
EOF
```

The network status of GameServer would be as follows:

```
  networkStatus:
    createTime: "2024-04-28T12:41:56Z"
    currentNetworkState: Ready
    desiredNetworkState: Ready
    externalAddresses:
    - endPoint: nlb-muyo7fv6z646ygcxxx.cn-xxx.nlb.aliyuncs.com
      ip: ""
      ports:
      - name: "80"
        port: 1047
        protocol: TCP
    internalAddresses:
    - ip: 172.16.0.1
      ports:
      - name: "80"
        port: 80
        protocol: TCP
    lastTransitionTime: "2024-04-28T12:41:56Z"
    networkType: AlibabaCloud-NLB
```

Clients can access the game server by using nlb-muyo7fv6z646ygcxxx.cn-xxx.nlb.aliyuncs.com:1047

---

### AlibabaCloud-EIP

#### Plugin name

`AlibabaCloud-EIP`

#### Cloud Provider

AlibabaCloud

#### Plugin description

- Allocate a separate EIP for each GameServer
- The exposed public access port is consistent with the port monitored in the container, which is managed by security group.
- It is necessary to install the latest version of the ack-extend-network-controller component in the ACK cluster. For details, please refer to the [component description page](https://cs.console.aliyun.com/#/next/app-catalog/ack/incubator/ack-extend-network-controller).
#### Network parameters

ReleaseStrategy

- Meaning: Specifies the EIP release policy.
- Value:
  - Follow: follows the lifecycle of the pod that is associated with the EIP. This is the default value.
  - Never: does not release the EIP. You need to manually release the EIP when you no longer need the EIP. ( By 'kubectl delete podeip {gameserver name} -n {gameserver namespace}')
  - You can also specify the timeout period of the EIP. For example, if you set the time period to 5m30s, the EIP is released 5.5 minutes after the pod is deleted. Time expressions written in Go are supported.
- Configuration change supported or not: no.

PoolId

- Meaning: Specifies the EIP address pool. For more information. It could be nil.
- Configuration change supported or not: no.

ResourceGroupId

- Meaning: Specifies the resource group to which the EIP belongs. It could be nil.
- Configuration change supported or not: no.

Bandwidth

- Meaning: Specifies the maximum bandwidth of the EIP. Unit: Mbit/s. It could be nil. Default is 5.
- Configuration change supported or not: no.

BandwidthPackageId

- Meaning: Specifies the EIP bandwidth plan that you want to use.
- Configuration change supported or not: no.

ChargeType

- Meaning: Specifies the metering method of the EIP.
- Value：
  - PayByTraffic: Fees are charged based on data transfer.
  - PayByBandwidth: Fees are charged based on bandwidth usage.
- Configuration change supported or not: no.

Description

- Meaning: The description of EIP resource
- Configuration change supported or not: no.

#### Plugin configuration

None

#### Example

```yaml
apiVersion: game.kruise.io/v1alpha1
kind: GameServerSet
metadata:
  name: eip-nginx
  namespace: default
spec:
  replicas: 1
  updateStrategy:
    rollingUpdate:
      podUpdatePolicy: InPlaceIfPossible
  network:
    networkType: AlibabaCloud-EIP
    networkConf:
      - name: ReleaseStrategy
        value: Never
      - name: Bandwidth
        value: "3"
      - name: ChargeType
        value: PayByTraffic
  gameServerTemplate:
    spec:
      containers:
        - image: nginx
          name: nginx
```

The network status of GameServer would be as follows:

```yaml
  networkStatus:
    createTime: "2023-07-17T10:10:18Z"
    currentNetworkState: Ready
    desiredNetworkState: Ready
    externalAddresses:
    - ip: 47.98.xxx.xxx
    internalAddresses:
    - ip: 192.168.1.51
    lastTransitionTime: "2023-07-17T10:10:18Z"
    networkType: AlibabaCloud-EIP
```

The generated podeip eip-nginx-0 would be as follows：

```yaml
apiVersion: alibabacloud.com/v1beta1
kind: PodEIP
metadata:
  annotations:
    k8s.aliyun.com/eip-controller: ack-extend-network-controller
  creationTimestamp: "2023-07-17T09:58:12Z"
  finalizers:
  - podeip-controller.alibabacloud.com/finalizer
  generation: 1
  name: eip-nginx-1
  namespace: default
  resourceVersion: "41443319"
  uid: 105a9575-998e-4e17-ab91-8f2597eeb55f
spec:
  allocationID: eip-xxx
  allocationType:
    releaseStrategy: Never
    type: Auto
status:
  eipAddress: 47.98.xxx.xxx
  internetChargeType: PayByTraffic
  isp: BGP
  networkInterfaceID: eni-xxx
  podLastSeen: "2023-07-17T10:36:02Z"
  privateIPAddress: 192.168.1.51
  resourceGroupID: rg-xxx
  status: InUse
```

In addition, the generated EIP resource will be named after {pod namespace}/{pod name} in the Alibaba Cloud console, which corresponds to each game server one by one.

---

### AlibabaCloud-NLB-SharedPort

#### Plugin name

`AlibabaCloud-NLB-SharedPort`

#### Cloud Provider

AlibabaCloud

#### Plugin description

- AlibabaCloud-NLB-SharedPort enables game servers to be accessed from the Internet by using Layer 4 NLB of Alibaba Cloud, which is similar to AlibabaCloud-SLB-SharedPort.
  This network plugin applies to stateless network services, such as proxy or gateway, in gaming scenarios.

- This network plugin supports network isolation.

#### Network parameters

SlbIds

- Meaning: the CLB instance IDs. You can specify multiple NLB instance IDs.
- Value: an example value can be nlb-9zeo7prq1m25ctpfrw1m7
- Configuration change supported or not: no.

PortProtocols

- Meaning: the ports in the pod to be exposed and the protocols. You can specify multiple ports and protocols.
- Value: in the format of port1/protocol1,port2/protocol2,... The protocol names must be in uppercase letters.
- Configuration change supported or not: no.

AllowNotReadyContainers

- Meaning: the container names that are allowed not ready when inplace updating, when traffic will not be cut.
- Value: {containerName_0},{containerName_1},... Example：sidecar
- Configuration change supported or not: It cannot be changed during the in-place updating process.

#### Plugin configuration

None

#### Example

Deploy a GameServerSet with two containers, one named app-2048 and the other named sidecar.

Specify the network parameter AllowNotReadyContainers as sidecar, 
then the entire pod will still provide services when the sidecar is updated in place.

```yaml
apiVersion: game.kruise.io/v1alpha1
kind: GameServerSet
metadata:
  name: gss-2048-nlb
  namespace: default
spec:
  replicas: 3
  updateStrategy:
    rollingUpdate:
      maxUnavailable: 100%
      podUpdatePolicy: InPlaceIfPossible
  network:
    networkType: AlibabaCloud-NLB-SharedPort
    networkConf:
      - name: NlbIds
        value: nlb-26jbknebrjlejt5abu
      - name: PortProtocols
        value: 80/TCP
      - name: AllowNotReadyContainers
        value: sidecar
  gameServerTemplate:
    spec:
      containers:
        - image: registry.cn-beijing.aliyuncs.com/acs/2048:v1.0
          name: app-2048
          volumeMounts:
            - name: shared-dir
              mountPath: /var/www/html/js
        - image: registry.cn-beijing.aliyuncs.com/acs/2048-sidecar:v1.0
          name: sidecar
          args:
            - bash
            - -c
            - rsync -aP /app/js/* /app/scripts/ && while true; do echo 11;sleep 2; done
          volumeMounts:
            - name: shared-dir
              mountPath: /app/scripts
      volumes:
        - name: shared-dir
          emptyDir: {}
```

After successful deployment, update the sidecar image to v2.0 and observe the corresponding endpoint:

```bash
kubectl get ep -w | grep nlb-26jbknebrjlejt5abu
nlb-26jbknebrjlejt5abu      192.168.0.8:80,192.168.0.82:80,192.168.63.228:80    10m

```

After waiting for the entire update process to end, you can find that there are no changes in the ep, indicating that no extraction has been performed.

---

### TencentCloud-CLB

#### Plugin name

`TencentCloud-CLB`

#### Cloud Provider

TencentCloud

#### Plugin description

- TencentCloud-CLB enables game servers to be accessed from the Internet by using Cloud Load Balancer (CLB) of Tencent Cloud. CLB is a type of Server Load Balancer (CLB). TencentCloud-CLB uses different ports for different game servers. The CLB instance only forwards traffic, but does not implement load balancing.
- The [tke-extend-network-controller](https://github.com/tkestack/tke-extend-network-controller) network plugin needs to be installed (can be installed through the TKE application market).
- This network plugin supports network isolation.

#### Network parameters

ClbIds

- Meaning: the CLB instance ID. You can fill in multiple ids.
- Value: in the format of slbId-0,slbId-1,... An example value can be "lb-9zeo7prq1m25ctpfrw1m7,lb-bp1qz7h50yd3w58h2f8je"
- Configuration change supported or not: yes. You can add new slbIds at the end. However, it is recommended not to change existing slbId that is in use.

PortProtocols

- Meaning: the ports in the pod to be exposed and the protocols. You can specify multiple ports and protocols.
- Value: in the format of port1/protocol1,port2/protocol2,... The protocol names must be in uppercase letters.
- Configuration change supported or not: yes.

#### Plugin configuration

```
[tencentcloud]
enable = true
[tencentcloud.clb]
# Specify the range of available ports of the CLB instance. Ports in this range can be used to forward Internet traffic to pods. In this example, the range includes 200 ports.
min_port = 1000
max_port = 1100
```

#### Example

```yaml
apiVersion: game.kruise.io/v1alpha1
kind: GameServerSet
metadata:
  name: clb-nginx
  namespace: default
spec:
  replicas: 1
  updateStrategy:
    rollingUpdate:
      podUpdatePolicy: InPlaceIfPossible
  network:
    networkType: TencentCloud-CLB
    networkConf:
      - name: ClbIds
        value: "lb-3ip9k5kr,lb-4ia8k0yh"
      - name: PortProtocols
        value: "80/TCP,7777/UDP"
  gameServerTemplate:
    spec:
      containers:
        - image: nginx
          name: nginx
```

The network status of GameServer would be as follows:

```yaml
  networkStatus:
    createTime: "2024-10-28T03:16:20Z"
    currentNetworkState: Ready
    desiredNetworkState: Ready
    externalAddresses:
    - ip: 139.155.64.52
      ports:
      - name: "80"
        port: 1002
        protocol: TCP
    - ip: 139.155.64.52
      ports:
      - name: "7777"
        port: 1003
        protocol: UDP
    internalAddresses:
    - ip: 172.16.7.106
      ports:
      - name: "80"
        port: 80
        protocol: TCP
    - ip: 172.16.7.106
      ports:
      - name: "7777"
        port: 7777
        protocol: UDP
    lastTransitionTime: "2024-10-28T03:16:20Z"
    networkType: TencentCloud-CLB
```

---

### HwCloud-ELB

#### Plugin name

`HwCloud-ELB`

#### Cloud Provider

HwCloud

#### Plugin description

- HwCloud-ELB enables game servers to be accessed from the Internet by using Layer 4 Load Balancer (ELB) of Huawei Cloud. ELB is a type of Server Load Balancer (SLB). HwCloud-ELB uses different ports of the same ELB instance to forward Internet traffic to different game servers. The ELB instance only forwards traffic, but does not implement load balancing.

- This network plugin supports network isolation.

#### Network parameters

ElbIds

- Meaning: the ELB instance ID. You can fill in multiple ids. （at least one）
- Value: in the format of elbId-0,elbId-1,... An example value can be "lb-9zeo7prq1m25ctpfrw1m7,lb-bp1qz7h50yd3w58h2f8je"
- Configuration change supported or not: yes. You can add new elbIds at the end. However, it is recommended not to change existing elbId that is in use.

PortProtocols

- Meaning: the ports in the pod to be exposed and the protocols. You can specify multiple ports and protocols.
- Value: in the format of port1/protocol1,port2/protocol2,... (same protocol port should like 8000/TCPUDP) The protocol names must be in uppercase letters.
- Configuration change supported or not: yes.

Fixed

- Meaning: whether the mapping relationship is fixed. If the mapping relationship is fixed, the mapping relationship remains unchanged even if the pod is deleted and recreated.
- Value: false or true.
- Configuration change supported or not: yes.

AllowNotReadyContainers

- Meaning: the container names that are allowed not ready when inplace updating, when traffic will not be cut.
- Value: {containerName_0},{containerName_1},... Example：sidecar
- Configuration change supported or not: It cannot be changed during the in-place updating process.


ExternalTrafficPolicyType

- Meaning: Service LB forward type, if Local， Service LB just forward traffice to local node Pod, we can keep source IP without SNAT
- Value: : Local/Cluster Default value is Cluster
- Configuration change supported or not: not. It maybe related to "IP/Port mapping relationship Fixed", recommend not to change


LB config parameters consistent with huawei cloud ccm https://github.com/kubernetes-sigs/cloud-provider-huaweicloud/blob/master/docs/usage-guide.md

LBHealthCheckFlag

- Meaning: Whether to enable health check
- Format: "on" means on, "off" means off. Default is on
- Whether to support changes: Yes

LBHealthCheckOption

- Meaning: Health Check Config
- Format: json string link {"delay": 3, "timeout": 15, "max_retries": 3}
- Whether to support changes: Yes

ElbClass

- Meaning: huawei lb class
- Format: dedicated or shared  (default dedicated)
- Whether to support changes: No


ElbConnLimit

- Meaning: elb conn limit work with shared class lb
- Format: the value ranges from -1 to 2147483647. The default value is -1
- Whether to support changes: No

ElbLbAlgorithm

- Meaning: Specifies the load balancing algorithm of the backend server group
- Format: ROUND_ROBIN,LEAST_CONNECTIONS,SOURCE_IP default ROUND_ROBIN
- Whether to support changes: Yes

ElbSessionAffinityFlag

- Meaning: Specifies whether to enable session affinity
- Format: on, off default off
- Whether to support changes: Yes

ElbSessionAffinityOption

- Meaning: Specifies the sticky session timeout duration in minutes.
- Format: json string like {"type": "SOURCE_IP", "persistence_timeout": 15}
- Whether to support changes: Yes

ElbTransparentClientIP

- Meaning: Specifies whether to pass source IP addresses of the clients to backend servers
- Format: true or false default false
- Whether to support changes: Yes

ElbXForwardedHost

- Meaning: Specifies whether to rewrite the X-Forwarded-Host header
- Format: true or false default false
- Whether to support changes: Yes

ElbIdleTimeout

- Meaning: Specifies the idle timeout for the listener
- Format: 0 to 4000 default not set, use lb default value
- Whether to support changes: Yes

ElbRequestTimeout

- Meaning: Specifies the request timeout for the listener.
- Format: 1 to 300 default not set, use lb default value
- Whether to support changes: Yes

ElbResponseTimeout

- Meaning: Specifies the response timeout for the listener
- Format: 1 to 300 default not set, use lb default value
- Whether to support changes: Yes

#### Plugin configuration
```
[hwcloud]
enable = true
[hwcloud.elb]
max_port = 700
min_port = 500
block_ports = []
```

---

### Volcengine-EIP

#### Plugin name

`Volcengine-EIP`

#### Cloud Provider

Volcengine

#### Plugin description

- Allocates or binds a dedicated Elastic IP (EIP) from Volcengine for each GameServer. You can specify an existing EIP via annotation or `networkConf`, or let the system allocate a new EIP automatically.
- The exposed public access port is consistent with the port listened to in the container. Security group policies need to be configured by the user.
- Suitable for game server scenarios that require public network access.
- Requires the `vpc-cni-controlplane` component to be installed in the cluster. For details, see [component documentation](https://www.volcengine.com/docs/6460/101015).

#### Network parameters

> For more parameters, refer to: https://www.volcengine.com/docs/6460/1152127

name

- EIP name. If not specified, the system will generate one automatically.
- Whether to support changes: no.

isp

- EIP type.
- Whether to support changes: no.

projectName

- Meaning: Project name to which the EIP belongs. Default is `default`.
- Whether to support changes: no.

bandwidth

- Meaning: Peak bandwidth in Mbps. Optional.
- Whether to support changes: no.

bandwidthPackageId

- Meaning: Shared bandwidth package ID to bind. Optional. If not set, EIP will not be bound to a shared bandwidth package.
- Whether to support changes: no.

billingType

- Meaning: EIP billing type.
- Value:
  - 2: (default) Pay-by-bandwidth.
  - 3: Pay-by-traffic.
- Whether to support changes: no.

description

- Meaning: Description of the EIP resource.
- Whether to support changes: no.

#### Annotation parameters

- `vke.volcengine.com/primary-eip-id`: Specify an existing EIP ID. The Pod will bind this EIP at startup.

#### Plugin configuration

None

#### Example

```yaml
apiVersion: game.kruise.io/v1alpha1
kind: GameServerSet
metadata:
  name: eip-nginx
  namespace: default
spec:
  replicas: 1
  updateStrategy:
    rollingUpdate:
      podUpdatePolicy: InPlaceIfPossible
  network:
    networkType: Volcengine-EIP
  gameServerTemplate:
    spec:
      containers:
        - image: nginx
          name: nginx
```

The network status of the generated GameServer is as follows:

```yaml
  networkStatus:
    createTime: "2025-01-17T10:10:18Z"
    currentNetworkState: Ready
    desiredNetworkState: Ready
    externalAddresses:
    - ip: 106.xx.xx.xx
    internalAddresses:
    - ip: 192.168.1.51
    lastTransitionTime: "2025-01-17T10:10:18Z"
    networkType: Volcengine-EIP
```

Pod annotation example:

```yaml
metadata:
  annotations:
    vke.volcengine.com/primary-eip-id: eip-xxx
    vke.volcengine.com/primary-eip-attributes: '{"bandwidth":3,"billingType":"2"}'
```

The EIP resource will be named `{pod namespace}/{pod name}` in the Volcengine console, corresponding one-to-one with each GameServer.

---