English | [中文](./README.md)

Based on JdCloud Container Service, for game scenarios, combine OKG to provide various network model plugins.

## JdCloud-NLB configuration

JdCloud Container Service supports the reuse of NLB (Network Load Balancer) in Kubernetes. Different services (svcs) can use different ports of the same NLB. As a result, the JdCloud-NLB network plugin will record the port allocation for each NLB. For services that specify the network type as JdCloud-NLB, the JdCloud-NLB network plugin will automatically allocate a port and create a service object. Once it detects that the public IP of the svc has been successfully created, the GameServer's network will transition to the Ready state, completing the process.

### plugin configuration
```toml
[jdcloud]
enable = true
[jdcloud.nlb]
#To allocate external access ports for Pods, you need to define the idle port ranges that the NLB (Network Load Balancer) can use. The maximum range for each port segment is 200 ports.
max_port = 700
min_port = 500
```

### Parameters
#### NlbIds
- Meaning: fill in the id of the clb. You can fill in more than one. You need to create the clb in [JdCloud].
- Value: each clbId is divided by `,` . For example:`netlb-aaa,netlb-bbb,...`
- Configurable: Y

#### PortProtocols
- Meaning: the ports and protocols exposed by the pod, support filling in multiple ports/protocols
- Value: `port1/protocol1`,`port2/protocol2`,... The protocol names must be in uppercase letters.
- Configurable: Y

#### Fixed
- Meaning: whether the mapping relationship is fixed. If the mapping relationship is fixed, the mapping relationship remains unchanged even if the pod is deleted and recreated.
- Value: false / true
- Configurable: Y

#### AllocateLoadBalancerNodePorts
- Meaning: Whether the generated service is assigned nodeport, this can be set to false only in nlb passthrough mode
- Value: false / true
- Configurable: Y

#### AllowNotReadyContainers
- Meaning: the container names that are allowed not ready when inplace updating, when traffic will not be cut.
- Value:{containerName_0},{containerName_1},... eg: sidecar
- Configurable: It cannot be changed during the in-place updating process.

#### Annotations
- Meaning: the anno added to the service
- Value: key1: value1,key2: value2...
- Configurable: Y


### Example
```yaml
cat <<EOF | kubectl apply -f -
apiVersion: game.kruise.io/v1alpha1
kind: GameServerSet
metadata:
  name: nlb
  namespace: default
spec:
  replicas: 3
  updateStrategy:
    rollingUpdate:
      podUpdatePolicy: InPlaceIfPossible
  network:
    networkType: JdCloud-NLB
    networkConf:
      - name: NlbIds
        #Fill in Jdcloud Cloud LoadBalancer Id here
        value: netlb-xxxxx
      - name: PortProtocols
        #Fill in the exposed ports and their corresponding protocols here. 
        #If there are multiple ports, the format is as follows: {port1}/{protocol1},{port2}/{protocol2}...
        #If the protocol is not filled in, the default is TCP
        value: 80/TCP
      - name: AllocateLoadBalancerNodePorts
        # Whether the generated service is assigned nodeport.
        value: "true"
      - name: Fixed
        #Fill in here whether a fixed IP is required [optional] ; Default is false
        value: "false"
      - name: Annotations
        #Fill in the anno related to clb on the service
        #The format is as follows: {key1}:{value1},{key2}:{value2}...
        value: "key1: value1,key2: value2"
  gameServerTemplate: 
    spec:
      containers:
        - args:
          - /data/server/start.sh
          command:
          - /bin/bash
          image: gss-cn-north-1.jcr.service.jdcloud.com/gsshosting/pal: v1
          name: game-server
EOF
```

Check the network status in GameServer:
```
networkStatus:
    createTime: "2024-11-04T08: 00: 20Z"
    currentNetworkState: Ready
    desiredNetworkState: Ready
    externalAddresses:
    - ip: xxx.xxx.xxx.xxx
      ports:
      - name: "8211"
        port: 531
        protocol: UDP
    internalAddresses:
    - ip: 10.0.0.95
      ports:
      - name: "8211"
        port: 8211
        protocol: UDP
    lastTransitionTime: "2024-11-04T08: 00: 20Z"
    networkType: JdCloud-NLB
```


## JdCloud-EIP configuration
JdCloud Container Service supports binding an Elastic Public IP directly to a pod in Kubernetes, allowing the pod to communicate directly with the external network.
- The cluster's network plugin uses Yunjian-CNI and cannot use Flannel to create the cluster.
- For specific usage restrictions of Elastic Public IPs, please refer to the JdCloud Elastic Public IP product documentation.
- Install the EIP-Controller component.
- The Elastic Public IP will not be deleted when the pod is destroyed.

###  Parameter

#### BandwidthConfigName
- Meaning: The bandwidth of the Elastic Public IP, measured in Mbps, has a value range of [1, 1024].
- Value: Must be an integer
- Configurable: Y

#### ChargeTypeConfigName
- Meaning: The billing method for the Elastic Public IP
- Value: string, `postpaid_by_usage`/`postpaid_by_duration`
- Configurable: Y

#### FixedEIPConfigName
- Meaning: Whether to fixed the Elastic Public IP,if so, the EIP will not be changed when the pod is recreated.
- Value: string, "false" / "true"
- Configurable: Y

#### AssignEIPConfigName
- Meaning: Whether to designate a specific Elastic Public IP. If true, provide the ID of the Elastic Public IP; otherwise, an EIP will be automatically allocated.
- Value: string, "false" / "true"

#### EIPIdConfigName
- Meaning: If a specific Elastic Public IP is designated, the ID of the Elastic Public IP must be provided, and the component will automatically perform the lookup and binding.
- Value: string，for example:`fip-xxxxxxxx`

### Example
```yaml
cat <<EOF | kubectl apply -f -
apiVersion: game.kruise.io/v1alpha1
kind: GameServerSet
metadata:
  name: eip
  namespace: default
spec:
  containers:
    - args:
        - /data/server/start.sh
      command:
        - /bin/bash
      image: gss-cn-north-1.jcr.service.jdcloud.com/gsshosting/pal: v1
      name: game-server
  network:
    networkType: JdCloud-EIP
    networkConf:
      - name: "BandWidth"
        value: "10"
      - name: "ChargeType"
        value: postpaid_by_usage
      - name: "Fixed"
        value: "false"
  replicas: 3
EOF
```

Check the network status in GameServer:
```
networkStatus:
    createTime: "2024-11-04T10: 53: 14Z"
    currentNetworkState: Ready
    desiredNetworkState: Ready
    externalAddresses:
    - ip: xxx.xxx.xxx.xxx
    internalAddresses:
    - ip: 10.0.0.95
    lastTransitionTime: "2024-11-04T10: 53: 14Z"
    networkType: JdCloud-EIP
```