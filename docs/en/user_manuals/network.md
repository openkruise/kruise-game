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
- Kubernetes-NodePort
- Kubernetes-Ingress
- AlibabaCloud-NATGW
- AlibabaCloud-SLB
- AlibabaCloud-NLB
- AlibabaCloud-AutoNLBs-V2
- AlibabaCloud-EIP
- AlibabaCloud-SLB-SharedPort
- AlibabaCloud-NLB-SharedPort
- Volcengine-CLB
- Volcengine-EIP
- HwCloud-ELB
- HwCloud-CCE-ELB
- HwCloud-CCE-EIP

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

### AlibabaCloud-AutoNLBs-V2

#### Plugin name

`AlibabaCloud-AutoNLBs-V2`

#### Cloud Provider

AlibabaCloud

#### Plugin description

AutoNLBs-V2 is an enhanced version of the Alibaba Cloud NLB (Network Load Balancer) network model, designed specifically for large-scale game server scenarios. This plugin manages NLB and EIP resources through CRD (Custom Resource Definition), implementing resource pooling, prewarming mechanisms, and multi-ISP access capabilities, significantly improving network readiness speed and resource utilization.

**Key Features:**

1. **Resource Pooling and Reuse**
   - NLB instances are independent of GameServerSet lifecycle and support cross-GSS reuse
   - EIP resource pooling management to avoid frequent creation and deletion
   - Reduces costs and resource creation time

2. **Service Prewarming Mechanism**
   - Pre-create Service resource pool, Pods directly associate when created
   - Network readiness time reduced from minutes to seconds
   - Supports large-scale concurrent Pod creation scenarios

3. **Multi-ISP Access Support**
   - Supports BGP, BGP_PRO, and single-line ISP (ChinaTelecom, ChinaMobile, ChinaUnicom)
   - A single Pod can have multiple network lines simultaneously
   - Suitable for multi-carrier access scenarios

4. **Automatic Resource Management**
   - Automatically calculates NLB allocation based on Pod index
   - Intelligent port allocation to avoid port conflicts
   - Supports dynamic scaling

5. **Depends on CRD Operators**
   - Requires deployment of [AlibabaCloud-NLB-Operator](https://github.com/chrisliu1995/AlibabaCloud-NLB-Operator)
   - Requires deployment of [alibabacloud-eip-operator](https://github.com/chrisliu1995/alibabacloud-eip-operator)

- This network plugin supports network isolation: Yes

#### Prerequisites

1. **Clone Helm Chart Repository**
```bash
git clone https://github.com/chrisliu1995/AlibabaCloud-Operator-Charts.git
cd AlibabaCloud-Operator-Charts
```

2. **Deploy NLB and EIP Operators**
```bash
helm install alibabacloud-operators . \
  --namespace kruise-game-system \
  --create-namespace \
  --set global.alibabacloud.accessKeyId=<your-access-key-id> \
  --set global.alibabacloud.accessKeySecret=<your-access-key-secret> \
  --set global.alibabacloud.region=<your-region>
```

3. **Verify Installation**
```bash
kubectl get pods -n kruise-game-system
```

**Notes:**
- Ensure the Alibaba Cloud account has permissions to create/manage NLB and EIP (`AliyunNLBFullAccess`, `AliyunEIPFullAccess`)
- Operators are deployed in the `kruise-game-system` namespace by default, which can be customized via the `--namespace` parameter
- `region` parameter examples: `cn-hangzhou`, `cn-beijing`, etc.
- For more configuration options, please refer to: https://github.com/chrisliu1995/AlibabaCloud-Operator-Charts

#### Network parameters

ZoneMaps

- Meaning: VPC and availability zone mapping configuration for creating NLB instances
- Format: `vpc-id@zone1:vswitch1,zone2:vswitch2,...`
- Example: `vpc-xxx@cn-hangzhou-h:vsw-aaa,cn-hangzhou-i:vsw-bbb`
- Configuration change supported: No (immutable after creation)
- Note: At least 2 availability zones required

PortProtocols

- Meaning: Ports and protocols to be exposed by Pod, supports multiple ports/protocols
- Format: `port1/protocol1,port2/protocol2,...` (protocol names must be in uppercase)
- Example: `8080/TCP,9000/UDP`
- Configuration change supported: No

EipIspTypes

- Meaning: List of EIP line types, supports multi-line access
- Format: `type1,type2,...`
- Available values:
  - `BGP`: BGP multi-line (default)
  - `BGP_PRO`: BGP premium line
  - `ChinaTelecom`: China Telecom single-line
  - `ChinaMobile`: China Mobile single-line
  - `ChinaUnicom`: China Unicom single-line
- Example: `BGP,BGP_PRO` or `ChinaTelecom,ChinaMobile,ChinaUnicom`
- Configuration change supported: No

MinPort

- Meaning: Minimum value for NLB external port allocation
- Format: Number, range [1, 65535]
- Default: 1000
- Configuration change supported: No

MaxPort

- Meaning: Maximum value for NLB external port allocation
- Format: Number, range [1, 65535]
- Default: 1499
- Configuration change supported: No

BlockPorts

- Meaning: List of ports to skip (blocked ports)
- Format: `port1,port2,...`
- Example: `1100,1200,1300`
- Configuration change supported: No

ReserveNlbNum

- Meaning: Number of reserved NLB instances (for prewarming)
- Format: Number
- Default: 1
- Configuration change supported: No

ExternalTrafficPolicyType

- Meaning: Service external traffic policy
- Format: `Local` or `Cluster`
- Default: `Local` (preserves source IP)
- Configuration change supported: No

LBHealthCheckFlag

- Meaning: Whether to enable NLB health check
- Format: `on` or `off`
- Default: `on`
- Configuration change supported: Yes

LBHealthCheckType

- Meaning: Health check protocol type
- Format: `tcp` or `http`
- Default: `tcp`
- Configuration change supported: Yes
- Note: Only effective when `LBHealthCheckFlag=on`

LBHealthCheckConnectPort

- Meaning: Health check port
- Format: Number, range [0, 65535]
- Default: `0` (uses backend server port)
- Configuration change supported: Yes

LBHealthCheckConnectTimeout

- Meaning: Health check timeout (seconds)
- Format: Number, range [1, 300]
- Default: `5`
- Configuration change supported: Yes

LBHealthCheckInterval

- Meaning: Health check interval (seconds)
- Format: Number, range [1, 50]
- Default: `10`
- Configuration change supported: Yes

LBHealthyThreshold

- Meaning: Health check success threshold (consecutive successful times)
- Format: Number, range [2, 10]
- Default: `2`
- Configuration change supported: Yes

LBUnhealthyThreshold

- Meaning: Health check failure threshold (consecutive failure times)
- Format: Number, range [2, 10]
- Default: `2`
- Configuration change supported: Yes

LBHealthCheckUri

- Meaning: HTTP health check path
- Format: Path starting with `/`, length 1-80 characters
- Example: `/health`
- Configuration change supported: Yes
- Note: Only required when `LBHealthCheckType=http`

LBHealthCheckDomain

- Meaning: HTTP health check domain
- Format: Domain string, length 1-80 characters
- Configuration change supported: Yes
- Note: Only effective when `LBHealthCheckType=http`

LBHealthCheckMethod

- Meaning: HTTP health check method
- Format: `GET` or `HEAD`
- Default: `GET`
- Configuration change supported: Yes
- Note: Only effective when `LBHealthCheckType=http`

RetainNLBOnDelete

- Meaning: Whether to retain NLB and EIP resources when GameServerSet is deleted
- Format: `true` or `false`
- Default: `true` (retain resources, support reuse)
- Configuration change supported: No
- Description:
  - When set to `true`, NLB and EIP resources will be retained after GSS deletion, allowing reuse by other GSS, reducing costs and creation time
  - When set to `false`, NLB and EIP resources will be cascade deleted when GSS is deleted
  - Regardless of this setting, Service resources are always deleted with GSS

#### How it works

**Resource Mapping Relationships:**

1. **NLB Allocation**
   - Each Pod calculates its NLB based on index: `nlbIndex = podIndex / podsPerNLB`
   - NLB naming rule: `{gssName}-{eipIspType(lowercase)}-{nlbIndex}`
   - Example: With `EipIspTypes=BGP`, generates `game-server-bgp-0` (note: ISP type is converted to lowercase)

2. **EIP Allocation**
   - Each NLB corresponds to multiple EIPs (one per zone)
   - EIP naming rule: `{gssName}-eip-{eipIspType(lowercase)}-{nlbIndex}-z{zoneIndex}`
   - Example: `game-server-eip-bgp-0-z0`, `game-server-eip-bgp-0-z1`

3. **Service Mapping**
   - Each Pod corresponds to multiple Services (one per line)
   - Service naming rule: `{podName}-{eipIspType(lowercase)}`
   - Example: With `EipIspTypes=BGP,BGP_PRO`, Pod `game-server-0` generates `game-server-0-bgp` and `game-server-0-bgp_pro`

4. **Port Allocation**
   - Each Pod is allocated a unique port range on the NLB
   - Base formula: `port = minPort + (podIndexInNLB * portCount) + portOffset`
   - Automatically skips ports configured in `BlockPorts` (actual port may be greater than formula result)
   - Example: If `MinPort=10000`, `BlockPorts=10005`, Pod-5's first port will be `10006` instead of `10005`

**Resource Lifecycle:**

- **NLB & EIP**: By default, no OwnerReference is set, independent of GameServerSet, supports cross-GSS reuse (can be changed to cascade deletion via `RetainNLBOnDelete=false` parameter)
- **Service**: OwnerReference points to GameServerSet, deleted when GSS is deleted
- **Resource Cleanup**:
  - `RetainNLBOnDelete=true` (default): NLB and EIP require manual deletion or batch management through labels
  - `RetainNLBOnDelete=false`: NLB and EIP will be automatically deleted with GSS

#### Plugin configuration

No additional configuration needed (resources created dynamically via CRD)

#### Example

**Example 1: Single-line BGP Access**

```yaml
apiVersion: game.kruise.io/v1alpha1
kind: GameServerSet
metadata:
  name: gs-auto-nlb-v2
  namespace: default
spec:
  replicas: 3
  updateStrategy:
    rollingUpdate:
      podUpdatePolicy: InPlaceIfPossible
  network:
    networkType: AlibabaCloud-AutoNLBs-V2
    networkConf:
    - name: ZoneMaps
      value: "vpc-xxx@cn-hangzhou-h:vsw-aaa,cn-hangzhou-i:vsw-bbb"
    - name: PortProtocols
      value: "8080/TCP,9000/UDP"
    - name: EipIspTypes
      value: "BGP"
    - name: MinPort
      value: "10000"
    - name: MaxPort
      value: "10499"
    - name: ReserveNlbNum
      value: "1"
  gameServerTemplate:
    spec:
      containers:
      - image: registry.cn-hangzhou.aliyuncs.com/gs-demo/gameserver:network
        name: gameserver
```

**Example 2: Multi-line Access (BGP + BGP_PRO)**

```yaml
apiVersion: game.kruise.io/v1alpha1
kind: GameServerSet
metadata:
  name: gs-multi-isp
  namespace: default
spec:
  replicas: 5
  updateStrategy:
    rollingUpdate:
      podUpdatePolicy: InPlaceIfPossible
  network:
    networkType: AlibabaCloud-AutoNLBs-V2
    networkConf:
    - name: ZoneMaps
      value: "vpc-yyy@cn-beijing-a:vsw-111,cn-beijing-b:vsw-222"
    - name: PortProtocols
      value: "7777/TCP"
    - name: EipIspTypes
      value: "BGP,BGP_PRO"
    - name: MinPort
      value: "20000"
    - name: MaxPort
      value: "20999"
    - name: LBHealthCheckFlag
      value: "on"
    - name: LBHealthCheckType
      value: "tcp"
  gameServerTemplate:
    spec:
      containers:
      - image: my-game-server:v1.0
        name: gameserver
        ports:
        - containerPort: 7777
          protocol: TCP
```

**Example 3: Three-network Single-line Access**

```yaml
apiVersion: game.kruise.io/v1alpha1
kind: GameServerSet
metadata:
  name: gs-three-isp
  namespace: default
spec:
  replicas: 10
  updateStrategy:
    rollingUpdate:
      podUpdatePolicy: InPlaceIfPossible
  network:
    networkType: AlibabaCloud-AutoNLBs-V2
    networkConf:
    - name: ZoneMaps
      value: "vpc-zzz@cn-shanghai-a:vsw-aaa,cn-shanghai-b:vsw-bbb"
    - name: PortProtocols
      value: "8000/TCP,8001/UDP"
    - name: EipIspTypes
      value: "ChinaTelecom,ChinaMobile,ChinaUnicom"
    - name: MinPort
      value: "30000"
    - name: MaxPort
      value: "30999"
    - name: BlockPorts
      value: "30100,30200,30300"
  gameServerTemplate:
    spec:
      containers:
      - image: game-server:latest
        name: gameserver
```

**Example 4: Enable Cascade Deletion**

```yaml
apiVersion: game.kruise.io/v1alpha1
kind: GameServerSet
metadata:
  name: gs-cascade-delete
  namespace: default
spec:
  replicas: 5
  updateStrategy:
    rollingUpdate:
      podUpdatePolicy: InPlaceIfPossible
  network:
    networkType: AlibabaCloud-AutoNLBs-V2
    networkConf:
    - name: ZoneMaps
      value: "vpc-xxx@cn-hangzhou-h:vsw-aaa,cn-hangzhou-i:vsw-bbb"
    - name: PortProtocols
      value: "8080/TCP"
    - name: EipIspTypes
      value: "BGP"
    - name: MinPort
      value: "10000"
    - name: MaxPort
      value: "10999"
    - name: RetainNLBOnDelete
      value: "false"  # Enable cascade deletion, automatically clean up NLB and EIP when GSS is deleted
  gameServerTemplate:
    spec:
      containers:
      - image: registry.cn-hangzhou.aliyuncs.com/gs-demo/gameserver:network
        name: gameserver
```

#### Generated GameServer Network Status

> **Note**: In Auto NLB V2 mode, `externalAddresses` will be populated with:
> - `endPoint` field: NLB hostname + ISP type (format: `{nlb-hostname}/{ispType}`)
> - `ip` field: Usually empty (Alibaba Cloud NLB Service only returns hostname, not IP)

**Single-line Scenario NetworkStatus Example:**

```yaml
networkStatus:
  createTime: "2025-01-15T08:30:00Z"
  currentNetworkState: Ready
  desiredNetworkState: Ready
  externalAddresses:
  - endPoint: nlb-xxx.cn-hangzhou.nlb.aliyuncs.com/BGP
    ip: ""
    ports:
    - name: "8080"
      port: 10000
      protocol: TCP
    - name: "9000"
      port: 10001
      protocol: UDP
  internalAddresses:
  - ip: 192.168.1.10
    ports:
    - name: "8080"
      port: 8080
      protocol: TCP
    - name: "9000"
      port: 9000
      protocol: UDP
  lastTransitionTime: "2025-01-15T08:30:05Z"
  networkType: AlibabaCloud-AutoNLBs-V2
```

**Multi-line Scenario NetworkStatus Example:**

```yaml
networkStatus:
  createTime: "2025-01-15T09:00:00Z"
  currentNetworkState: Ready
  desiredNetworkState: Ready
  externalAddresses:
  # BGP line
  - endPoint: nlb-aaa.cn-beijing.nlb.aliyuncs.com/BGP
    ip: ""
    ports:
    - name: "7777"
      port: 20000
      protocol: TCP
  # BGP_PRO line
  - endPoint: nlb-bbb.cn-beijing.nlb.aliyuncs.com/BGP_PRO
    ip: ""
    ports:
    - name: "7777"
      port: 20000
      protocol: TCP
  internalAddresses:
  - ip: 192.168.2.20
    ports:
    - name: "7777"
      port: 7777
      protocol: TCP
  lastTransitionTime: "2025-01-15T09:00:10Z"
  networkType: AlibabaCloud-AutoNLBs-V2
```

#### Resource Viewing

**View NLB CRD resources:**

```bash
kubectl get nlb -n default
```

Example output:
```
NAME                   LOADBALANCERID         STATUS   AGE
gs-auto-nlb-v2-bgp-0   nlb-xxx123            Active   10m
gs-auto-nlb-v2-bgp-1   nlb-yyy456            Active   8m
```

**View EIP CRD resources:**

```bash
kubectl get eip -n default
```

Example output:
```
NAME                           ALLOCATIONID      IP              STATUS
gs-auto-nlb-v2-eip-bgp-0-z0   eip-aaa111        47.96.100.1     InUse
gs-auto-nlb-v2-eip-bgp-0-z1   eip-bbb222        47.96.100.2     InUse
```

**View Service resources:**

```bash
kubectl get svc -l game.kruise.io/owner-gss=gs-auto-nlb-v2
```

#### Best Practices

1. **Port Planning**
   - Set reasonable `MinPort` and `MaxPort` ranges to ensure sufficient port space
   - Calculation formula: `Total ports = (MaxPort - MinPort + 1) - BlockPorts count`
   - Pods per NLB: `podsPerNLB = Total ports / PortProtocols count`

2. **Resource Reuse**
   - NLB and EIP resources are retained and can be reused by new GSS after deletion
   - Use label `game.kruise.io/nlb-pool-gss` to manage NLB ownership

3. **Multi-line Selection**
   - **BGP multi-line**: Suitable for nationwide users, automatically selects optimal line
   - **BGP_PRO premium**: Suitable for latency-sensitive games, higher cost
   - **Three-network single-line**: Suitable for scenarios with concentrated carrier users, most cost-effective

4. **Health Check Configuration**
   - Production environments recommend enabling health checks (`LBHealthCheckFlag=on`)
   - TCP checks suitable for most scenarios, HTTP checks suitable for web games

5. **Capacity Planning**
   - Set appropriate `ReserveNlbNum` to prewarm resources in advance
   - Avoid frequent NLB creation causing network jitter

#### Important Notes

1. **Resource Cleanup**
   - **Default mode (`RetainNLBOnDelete=true`)**: NLB and EIP resources are not automatically cleaned when GameServerSet is deleted
   - Manually delete unused NLB/EIP CRD resources
   - Batch cleanup via labels:
     ```bash
     kubectl delete nlb -l game.kruise.io/nlb-pool-gss=gs-name
     kubectl delete eip -l game.kruise.io/eip-pool-gss=gs-name
     ```
   - **Cascade deletion mode (`RetainNLBOnDelete=false`)**: NLB and EIP resources will be automatically deleted when GSS is deleted, no manual cleanup required

2. **Network Configuration Immutability**
   - Parameters like `ZoneMaps`, `PortProtocols`, `EipIspTypes` cannot be changed after creation
   - When changes are needed, recommend creating a new GameServerSet and migrating

3. **Single-line ISP Billing**
   - `ChinaTelecom`, `ChinaMobile`, `ChinaUnicom` only support bandwidth-based billing (PayByBandwidth)
   - BGP and BGP_PRO support traffic-based billing (PayByTraffic)

4. **Permission Requirements**
   - Operators require Alibaba Cloud RAM permissions: `AliyunNLBFullAccess`, `AliyunEIPFullAccess`
   - Ensure network resources like VPC, VSwitch, security groups are configured correctly

5. **Performance Considerations**
   - In large-scale scenarios (100+ Pods), recommend creating sufficient NLB instances in advance
   - Service prewarming mechanism can significantly reduce Pod startup network readiness time

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
### HwCloud-CCE-ELB
#### Plugin name
`HwCloud-CCE-ELB`  
**Note**: 
- This plugin is only applicable to Huawei Cloud's CCE Standard and CCE Turbo clusters.
- If using an existing ELB, ensure its VPC matches the CCE cluster's VPC; otherwise, access will fail.

#### Cloud Provider
HuaweiCloud

#### Plugin description
- HwCloud-ELB uses Huawei Cloud Load Balancer (ELB) as the entity for external service hosting. It distributes external traffic to multiple Pods within the cluster through Elastic Load Balancing (ELB), providing higher reliability compared to the NodePort type.
- Supported annotations, please refer to the documentation: https://support.huaweicloud.com/usermanual-cce/cce_10_0681.html
- The exposed public network access port is consistent with the port being listened to in the container.
- You can bind security groups for management ([Use annotations to bind security groups to Pods](https://support.huaweicloud.com/usermanual-cce/cce_10_0897.html)), which is only supported in CCE Turbo clusters.
  - The network interface of the Pod uses the security group configured via the annotation: `yangtse.io/security-group-ids`. 
  - The Pod's network interface will use the existing security groups and additionally include the security group configured via the annotation: `yangtse.io/additional-security-group-ids`.
- Supports Network Isolation: Yes.

#### Network parameters
PortProtocols

- Meaning: Exposed ports and protocols of the Pod. Supports multiple ports/protocols.
- Format: port1/protocol1,port2/protocol2,... (Protocols must be uppercase).
- Supports Modification: Yes.

Fixed
- Meaning: Whether to retain fixed access IP/port. If enabled, the external/internal mapping relationship remains unchanged even if Pods are recreated.
- Format: false / true
- Supports Modification: Yes.

AllowNotReadyContainers
- Meaning: Container names allowed to maintain traffic flow during in-place upgrades.
- Format: {containerName_0},{containerName_1},... e.g., sidecar
- Supports Modification: Not modifiable during in-place upgrades.

ExternalTrafficPolicyType
- Meaning: Determines whether Service LB forwards traffic only to local instances. Setting to Local creates a Local-type Service and retains client source IP addresses when configured with cloud-manager.
- Format: Local / Cluster (Default: Cluster)
- Supports Modification: No. Due to dependencies on fixed IP/port settings, modification is not recommended.

Other Huawei CCE Cluster Parameters  
Refer to annotations' keys/values in the documentation: 
- [LoadBalancer](https://support.huaweicloud.com/usermanual-cce/cce_10_0014.html)


#### Plugin configuration
The port range here can be configured according to your business requirements. For block_ports, please refer to this issue: https://github.com/openkruise/kruise-game/issues/174
```
[hwcloud]
enable = true
[hwcloud.cce.elb]
max_port = 65535
min_port = 32768
block_ports = []
```

---
#### Example
Using Existing ELB  
https://support.huaweicloud.com/usermanual-cce/cce_10_0385.html#section1
```yaml
apiVersion: game.kruise.io/v1alpha1
kind: GameServerSet
metadata:
  name: hw-cce-elb-nginx
  namespace: default
spec:
  replicas: 2
  updateStrategy:
    rollingUpdate:
      podUpdatePolicy: InPlaceIfPossible
  network:
    networkType: HwCloud-CCE-ELB
    networkConf:
      - name: PortProtocols
        value: "80/TCP"
      - name: kubernetes.io/elb.class # The type of the ELB instance
        value: performance
      - name: kubernetes.io/elb.id # The ID of the ELB instance
        value: 8f4cf216-a659-40dc-8c77-xxxx
  gameServerTemplate:
    spec:
      containers:
        - image: nginx
          name: nginx
```

The generated svc is shown below. As you can see, both svcs point to the same ELB.
```yaml
apiVersion: v1
kind: Service
metadata:
  annotations:
    game.kruise.io/network-config-hash: "3594992400"
    kubernetes.io/elb.class: performance
    kubernetes.io/elb.connection-drain-enable: "true"
    kubernetes.io/elb.connection-drain-timeout: "300"
    kubernetes.io/elb.id: 8f4cf216-a659-40dc-8c77-xxxx
    kubernetes.io/elb.mark: "0"
  creationTimestamp: "2025-07-23T08:15:09Z"
  finalizers:
    - service.kubernetes.io/load-balancer-cleanup
  name: hw-cce-elb-nginx-0
  namespace: kruise-game-system
  ownerReferences:
    - apiVersion: v1
      blockOwnerDeletion: true
      controller: true
      kind: Pod
      name: hw-cce-elb-nginx-0
      uid: 4f9f37f9-16d4-4ee7-b553-9b6e0039c5d5
  resourceVersion: "13369506"
  uid: 23815818-a626-4be3-b31f-4b95a4f89786
spec:
  allocateLoadBalancerNodePorts: true
  clusterIP: 10.247.213.xxx
  clusterIPs:
    - 10.247.213.xxx
  externalTrafficPolicy: Cluster
  internalTrafficPolicy: Cluster
  ipFamilies:
    - IPv4
  ipFamilyPolicy: SingleStack
  loadBalancerIP: 192.168.0.xxx
  ports:
    - name: 80-tcp
      nodePort: 30622
      port: 3308
      protocol: TCP
      targetPort: 80
    - name: 80-udp
      nodePort: 30622
      port: 3308
      protocol: UDP
      targetPort: 80
  selector:
    statefulset.kubernetes.io/pod-name: hw-cce-elb-nginx-0
  sessionAffinity: None
  type: LoadBalancer
status:
  loadBalancer:
    ingress:
      - ip: 192.168.0.xxx
      - ip: 189.1.225.xxx

---
apiVersion: v1
kind: Service
metadata:
  annotations:
    game.kruise.io/network-config-hash: "3594992400"
    kubernetes.io/elb.class: performance
    kubernetes.io/elb.connection-drain-enable: "true"
    kubernetes.io/elb.connection-drain-timeout: "300"
    kubernetes.io/elb.id: 8f4cf216-a659-40dc-8c77-xxxx
    kubernetes.io/elb.mark: "0"
  creationTimestamp: "2025-07-23T08:15:08Z"
  finalizers:
    - service.kubernetes.io/load-balancer-cleanup
  name: hw-cce-elb-nginx-1
  namespace: kruise-game-system
  ownerReferences:
    - apiVersion: v1
      blockOwnerDeletion: true
      controller: true
      kind: Pod
      name: hw-cce-elb-nginx-1
      uid: 0f42b430-49ba-4203-8b50-4be059619b79
  resourceVersion: "13369489"
  uid: 92a56054-ad92-4dbd-9d1b-e717e0a14af2
spec:
  allocateLoadBalancerNodePorts: true
  clusterIP: 10.247.14.xxx
  clusterIPs:
    - 10.247.14.xxx
  externalTrafficPolicy: Cluster
  internalTrafficPolicy: Cluster
  ipFamilies:
    - IPv4
  ipFamilyPolicy: SingleStack
  loadBalancerIP: 192.168.0.xxx
  ports:
    - name: 80-tcp
      nodePort: 32227
      port: 3611
      protocol: TCP
      targetPort: 80
    - name: 80-udp
      nodePort: 32227
      port: 3611
      protocol: UDP
      targetPort: 80
  selector:
    statefulset.kubernetes.io/pod-name: hw-cce-elb-nginx-1
  sessionAffinity: None
  type: LoadBalancer
status:
  loadBalancer:
    ingress:
      - ip: 192.168.0.xxx
      - ip: 189.1.225.xxx
```
The generated svc is shown below. As you can see, both svcs point to the same IP address, differing only in their ports:  
```bash
kubectl get svc |grep hw-cce-elb-nginx
hw-cce-elb-nginx-0                           LoadBalancer   10.247.213.xxx   189.1.225.xxx,192.168.0.xxx   3308:30622/TCP,3308:30622/UDP   2m3s
hw-cce-elb-nginx-1                           LoadBalancer   10.247.14.xxx    189.1.225.xxx,192.168.0.xxx   3611:32227/TCP,3611:32227/UDP   2m4s
```
---
Automatically create an ELB and bind it to the created pod.
**Note**:
- When ELBs are automatically created for multiple replicas, each svc will use its own auto-created ELB. Each ELB will have a unique ID and a distinct external IP address.
- When the svc is deleted, the associated auto-created ELB will also be deleted.
```yaml
apiVersion: game.kruise.io/v1alpha1
kind: GameServerSet
metadata:
  name: hw-cce-elb-auto-performance
  namespace: kruise-game-system
spec:
  replicas: 2
  updateStrategy:
    rollingUpdate:
      podUpdatePolicy: InPlaceIfPossible
  network:
    networkType: HwCloud-CCE-ELB
    networkConf:
      - name: PortProtocols
        value: "80/TCP"
      - name: kubernetes.io/elb.class
        value: performance # The type of the ELB instance.
      - name: kubernetes.io/elb.autocreate # Options for automatically creating an ELB: https://support.huaweicloud.com/usermanual-cce/cce_10_0385.html#section21
        value: '{
                  "type": "public",
                  "bandwidth_name": "bandwidth-xxxx",
                  "bandwidth_chargemode": "traffic",
                  "bandwidth_size": 5,
                  "bandwidth_sharetype": "PER",
                  "eip_type": "5_bgp",
                  "available_zone": [
                     "ap-southeast-1a",
                     "ap-southeast-1b"
                  ],
                  "l4_flavor_name": "L4_flavor.elb.s1.small"
                }'
      - name: kubernetes.io/elb.enterpriseID # The enterprise project ID to which the created load balancer belongs.
        value: 'aff97261-4dbd-4593-8236-xxxx'
      - name: kubernetes.io/elb.lb-algorithm
        value: ROUND_ROBIN # Load balancer algorithm
  gameServerTemplate:
    spec:
      containers:
        - image: nginx
          name: nginx
         
```
The generated svc is shown below. As you can see, both svcs point to different ELBs.
```yaml
apiVersion: v1
kind: Service
metadata:
  annotations:
    game.kruise.io/network-config-hash: "3090934611"
    kubernetes.io/elb.autocreate: '{ "type": "public", "bandwidth_name": "bandwidth-89f0",
      "bandwidth_chargemode": "traffic", "bandwidth_size": 5, "bandwidth_sharetype":
      "PER", "eip_type": "5_bgp", "available_zone": [ "ap-southeast-1a", "ap-southeast-1b"
      ], "l4_flavor_name": "L4_flavor.elb.s1.small" }'
    kubernetes.io/elb.class: performance
    kubernetes.io/elb.eip-id: 566d5f4c-3484-4d7e-aa6b-xxxx
    kubernetes.io/elb.enterpriseID: aff97261-4dbd-4593-8236-xxxx
    kubernetes.io/elb.id: 75e06e8b-a246-48cb-b05c-xxxx
    kubernetes.io/elb.lb-algorithm: ROUND_ROBIN
    kubernetes.io/elb.mark: "0"
  creationTimestamp: "2025-07-23T09:25:01Z"
  finalizers:
    - service.kubernetes.io/load-balancer-cleanup
  name: hw-cce-elb-auto-performance-0
  namespace: kruise-game-system
  ownerReferences:
    - apiVersion: v1
      blockOwnerDeletion: true
      controller: true
      kind: Pod
      name: hw-cce-elb-auto-performance-0
      uid: 1da0edf4-f45d-4635-8db0-ed5ccea2441d
  resourceVersion: "13401553"
  uid: 13efd440-65a7-4b45-bafc-2268102a4fd7
spec:
  allocateLoadBalancerNodePorts: true
  clusterIP: 10.247.50.xxx
  clusterIPs:
    - 10.247.50.xxx
  externalTrafficPolicy: Cluster
  internalTrafficPolicy: Cluster
  ipFamilies:
    - IPv4
  ipFamilyPolicy: SingleStack
  loadBalancerIP: 49.0.251.xxx
  ports:
    - name: 80-tcp
      nodePort: 30918
      port: 1
      protocol: TCP
      targetPort: 80
  selector:
    statefulset.kubernetes.io/pod-name: hw-cce-elb-auto-performance-0
  sessionAffinity: None
  type: LoadBalancer
status:
  loadBalancer:
    ingress:
      - ip: 49.0.251.xxx
      - ip: 192.168.1.xxx
---
apiVersion: v1
kind: Service
metadata:
  annotations:
    game.kruise.io/network-config-hash: "3090934611"
    kubernetes.io/elb.autocreate: '{ "type": "public", "bandwidth_name": "bandwidth-89f0",
      "bandwidth_chargemode": "traffic", "bandwidth_size": 5, "bandwidth_sharetype":
      "PER", "eip_type": "5_bgp", "available_zone": [ "ap-southeast-1a", "ap-southeast-1b"
      ], "l4_flavor_name": "L4_flavor.elb.s1.small" }'
    kubernetes.io/elb.class: performance
    kubernetes.io/elb.eip-id: 4a5396b1-e750-4ba5-a5d3-xxxx
    kubernetes.io/elb.enterpriseID: aff97261-4dbd-4593-8236-xxxx
    kubernetes.io/elb.id: b093db79-3c3e-4e77-a2ee-xxxx
    kubernetes.io/elb.lb-algorithm: ROUND_ROBIN
    kubernetes.io/elb.mark: "0"
  creationTimestamp: "2025-07-23T09:25:01Z"
  finalizers:
    - service.kubernetes.io/load-balancer-cleanup
  name: hw-cce-elb-auto-performance-1
  namespace: kruise-game-system
  ownerReferences:
    - apiVersion: v1
      blockOwnerDeletion: true
      controller: true
      kind: Pod
      name: hw-cce-elb-auto-performance-1
      uid: abfc9ad1-1ae3-45fa-b956-4617c465a44f
  resourceVersion: "13401664"
  uid: 01dd8e13-b1c8-4d9f-8b1c-13c2f001c614
spec:
  allocateLoadBalancerNodePorts: true
  clusterIP: 10.247.196.xxx
  clusterIPs:
    - 10.247.196.xxx
  externalTrafficPolicy: Cluster
  internalTrafficPolicy: Cluster
  ipFamilies:
    - IPv4
  ipFamilyPolicy: SingleStack
  loadBalancerIP: 150.40.245.xxx
  ports:
    - name: 80-tcp
      nodePort: 30942
      port: 1
      protocol: TCP
      targetPort: 80
  selector:
    statefulset.kubernetes.io/pod-name: hw-cce-elb-auto-performance-1
  sessionAffinity: None
  type: LoadBalancer
status:
  loadBalancer:
    ingress:
      - ip: 150.40.245.xxx
      - ip: 192.168.1.xxx
```
The generated svc is shown below. As you can see, both svcs are assigned different external IPs:
```bash
kubectl get svc |grep hw-cce-elb-auto-performance
hw-cce-elb-auto-performance-0                    LoadBalancer   10.247.50.xxx    192.168.1.xxx,49.0.251.xxx      1:30918/TCP                       4m29s
hw-cce-elb-auto-performance-1                    LoadBalancer   10.247.196.xxx   150.40.245.xxx,192.168.1.xxx    1:30942/TCP                       4m29s
```

#### Plugin Name
`HwCloud-EIP`
**Note**: This plugin is only applicable to Huawei Cloud's CCE Turbo clusters.

#### Cloud Provider
HuaweiCloud

#### Plugin Description
- Only Huawei Cloud CCE Turbo clusters are supported: https://support.huaweicloud.com/usermanual-cce/cce_10_0284.html#section1
- Assigns a separate Elastic IP (EIP) to each pod.
- The exposed public network access port is consistent with the port being listened to in the container. Security groups can be bound for management ([Binding Security Groups to Pods Using Annotations](https://support.huaweicloud.com/usermanual-cce/cce_10_0897.html))
  - The Pod's network interface uses the security group configured via the annotation: `yangtse.io/security-group-ids`.
  - The Pod's network interface will use the existing security groups while additionally applying the security group configured via the annotation: `yangtse.io/additional-security-group-ids`
- The automatically created EIP does not support specifying the 'enterprise project' during creation.

#### Network Parameters
Refer to Huawei Cloud documentation: https://support.huaweicloud.com/usermanual-cce/cce_10_0734.html. This plugin supports all annotations on this page.

#### Plugin Configuration
None

#### Example
Exclusive Bandwidth EIP Created with Pod  
Note: The EIP created here belongs to the `default` enterprise project. Huawei Cloud currently does not support specifying enterprise projects in this mode.  
```yaml
apiVersion: game.kruise.io/v1alpha1
kind: GameServerSet
metadata:
  name: hwcloud-cce-eip-performance
  namespace: default
spec:
  replicas: 2
  updateStrategy:
    rollingUpdate:
      podUpdatePolicy: InPlaceIfPossible
  network:
    networkType: HwCloud-CCE-EIP
    networkConf:
      # https://support.huaweicloud.com/usermanual-cce/cce_10_0734.html
      - name: yangtse.io/pod-with-eip
        value: "true"
      - name: yangtse.io/eip-bandwidth-size
        value: "5"
      - name: yangtse.io/eip-network-type
        value: "5_bgp"
      - name: yangtse.io/eip-charge-mode
        value: "traffic"
  gameServerTemplate:
    spec:
      containers:
        - image: nginx
          name: nginx
```

Generated Pod Annotations:  
`yangtse.io/allocated-eip-id` corresponds to the EIP viewable in Huawei Cloud's Elastic IP details.   
`yangtse.io/allocated-ipv4-eip` is the pod's EIP.  
```yaml
apiVersion: v1
kind: Pod
metadata:
  annotations:
    apps.kruise.io/runtime-containers-meta: '{"containers":[{"name":"nginx","containerID":"containerd://302f710dc7fb5771be5b16a31de84ff457fd84c9aa1ce00b7e7f2ddc3b7c3978","restartCount":0,"hashes":{"plainHash":2641665875,"plainHashWithoutResources":0,"extractedEnvFromMetadataHash":86995377}}]}'
    game.kruise.io/network-conf: '[{"name":"yangtse.io/pod-with-eip","value":"true"},{"name":"yangtse.io/eip-bandwidth-size","value":"5"},{"name":"yangtse.io/eip-network-type","value":"5_bgp"},{"name":"yangtse.io/eip-charge-mode","value":"traffic"}]'
    game.kruise.io/network-status: '{"currentNetworkState":"Ready","createTime":null,"lastTransitionTime":null}'
    game.kruise.io/network-trigger-time: "2025-07-16 17:03:07"
    game.kruise.io/network-type: HwCloud-EIP
    game.kruise.io/opsState-last-changed-time: "2025-07-16 17:03:07"
    game.kruise.io/state-last-changed-time: "2025-07-16 09:03:13"
    lifecycle.apps.kruise.io/timestamp: "2025-07-16T09:03:03Z"
    yangtse.io/allocated-eip-id: 3a52ca79-d78d-4fc2-8590-xxx
    yangtse.io/allocated-ipv4-eip: 94.74.110.xxx
    yangtse.io/eip-bandwidth-size: "5"
    yangtse.io/eip-charge-mode: traffic
    yangtse.io/eip-network-type: 5_bgp
    yangtse.io/pod-with-eip: "true"
```

To use an existing EIP, add yangtse.io/eip-id in spec.network.networkConf. You need to create the EIP in Huawei Cloud in advance.
```yaml
apiVersion: game.kruise.io/v1alpha1
kind: GameServerSet
metadata:
  name: hw-cce-eip-exist
  namespace: kruise-game-system
spec:
  replicas: 1
  updateStrategy:
    rollingUpdate:
      podUpdatePolicy: InPlaceIfPossible
  network:
    networkType: HwCloud-CCE-EIP
    networkConf:
      - name: yangtse.io/eip-id
        value: "7ec474aa-3bd9-46a2-a45c-xxx" # Use an existing EIP.
  gameServerTemplate:
    spec:
      containers:
        - image: nginx
          name: nginx
```
In the pod's YAML, you can see that the yangtse.io/allocated-eip-id in the pod's annotations corresponds to the EIP we specified. 
By logging into the Huawei Cloud EIP console, you can verify that this EIP is already bound to the pod.
```yaml
apiVersion: v1
kind: Pod
metadata:
  annotations:
    apps.kruise.io/runtime-containers-meta: '{"containers":[{"name":"nginx","containerID":"containerd://0fc9de69e30b48cf13ad2d2c6f5fe3be86e48e922a982dbb77b53ffd0ca6f54b","restartCount":0,"hashes":{"plainHash":2957831032,"plainHashWithoutResources":0,"extractedEnvFromMetadataHash":86995377}}]}'
    game.kruise.io/network-conf: '[{"name":"yangtse.io/eip-id","value":"7ec474aa-3bd9-46a2-a45c-xxxx"}]'
    game.kruise.io/network-status: '{"currentNetworkState":"Ready","createTime":null,"lastTransitionTime":null}'
    game.kruise.io/network-trigger-time: "2025-07-18 15:38:21"
    game.kruise.io/network-type: HwCloud-EIP
    game.kruise.io/opsState-last-changed-time: "2025-07-18 15:38:21"
    game.kruise.io/state-last-changed-time: "2025-07-18 15:38:31"
    lifecycle.apps.kruise.io/timestamp: "2025-07-18T07:38:13Z"
    yangtse.io/allocated-eip-id: 7ec474aa-3bd9-46a2-a45c-xxxx
    yangtse.io/allocated-ipv4-eip: 159.138.21.xxx
    yangtse.io/eip-id: 7ec474aa-3bd9-46a2-a45c-xxxx
  creationTimestamp: "2025-07-18T07:38:14Z
# other info ignored
```
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