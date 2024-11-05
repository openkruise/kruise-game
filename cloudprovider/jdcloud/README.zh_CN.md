中文 | [English](./README.md)

基于京东云容器服务，针对游戏场景，结合OKG提供各网络模型插件。

## JdCloud-NLB 相关配置

京东云容器服务支持在k8s中对NLB复用的机制，不同的svc可以使用同一个NLB的不同端口。由此，JdCloud-NLB network plugin将记录各NLB对应的端口分配情况，对于指定了网络类型为JdCloud-NLB，JdCloud-NLB网络插件将会自动分配一个端口并创建一个service对象，待检测到svc公网IP创建成功后，GameServer的网络变为Ready状态，该过程执行完成。

### plugin配置
```toml
[jdcloud]
enable = true
[jdcloud.nlb]
#填写nlb可使用的空闲端口段，用于为pod分配外部接入端口，范围最大为200
max_port = 700
min_port = 500
```
### 参数
#### NlbIds
- 含义：填写nlb的id，可填写多个，需要先在【京东云】中创建好nlb。
- 填写格式：各个nlbId用,分割。例如：netlb-aaa,netlb-bbb,...
- 是否支持变更：是

#### PortProtocols
- 含义：pod暴露的端口及协议，支持填写多个端口/协议
- 填写格式：port1/protocol1,port2/protocol2,...（协议需大写）
- 是否支持变更：是

#### Fixed
- 含义：是否固定访问IP/端口。若是，即使pod删除重建，网络内外映射关系不会改变
- 填写格式：false / true
- 是否支持变更：是

#### AllocateLoadBalancerNodePorts
- 含义：生成的service是否分配nodeport, 仅在nlb的直通模式（passthrough）下，才能设置为false
- 填写格式：true/false
- 是否支持变更：是

#### AllowNotReadyContainers
- 含义：在容器原地升级时允许不断流的对应容器名称，可填写多个
- 填写格式：{containerName_0},{containerName_1},... 例如：sidecar
- 是否支持变更：在原地升级过程中不可变更

#### Annotations
- 含义：添加在service上的anno，可填写多个
- 填写格式：key1:value1,key2:value2...
- 是否支持变更：是


### 使用示例
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
        value: "key1:value1,key2:value2"
  gameServerTemplate: 
    spec:
      containers:
        - args:
          - /data/server/start.sh
          command:
          - /bin/bash
          image: gss-cn-north-1.jcr.service.jdcloud.com/gsshosting/pal:v1
          name: game-server
EOF
```

检查GameServer中的网络状态:
```
networkStatus:
    createTime: "2024-11-04T08:00:20Z"
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
    lastTransitionTime: "2024-11-04T08:00:20Z"
    networkType: JdCloud-NLB
```


## JdCloud-EIP 相关配置
京东云容器服务支持在k8s中，让一个 pod 和弹性公网 IP 直接进行绑定，可以让 pod 直接与外部网络进行通信。
- 集群的网络插件使用 yunjian-CNI，不可使用 flannel 创建集群
- 弹性公网 IP 使用限制请具体参考京东云弹性公网 IP 产品文档
- 安装 EIP-Controller 组件
- 弹性公网 IP 不会随 POD 的销毁而删除

### 参数

#### BandwidthConfigName
- 含义：弹性公网IP的带宽，单位为 Mbps，取值范围为 [1,1024]
- 填写格式：必须填整数，且不带单位
- 是否支持变更：是

#### ChargeTypeConfigName
- 含义：弹性公网IP的计费方式，取值：按量计费：postpaid_by_usage，包年包月：postpaid_by_duration
- 填写格式：字符串
- 是否支持变更：是

#### FixedEIPConfigName
- 含义：是否固定弹性公网IP。若是，即使pod删除重建，弹性公网IP也不会改变
- 填写格式："false" / "true"，字符串
- 是否支持变更：是

#### AssignEIPConfigName
- 含义：是否指定使用某个弹性公网IP，请填写 true，否则自动分配一个EIP
- 填写格式："false" / "true"，字符串

#### EIPIdConfigName
- 含义：若指定使用某个弹性公网IP，则必须填写弹性公网IP的ID，，组件会自动进行进行查询和绑定
- 填写格式：字符串，例如：fip-xxxxxxxx

### 使用示例
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
      image: gss-cn-north-1.jcr.service.jdcloud.com/gsshosting/pal:v1
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

检查GameServer中的网络状态:
```
networkStatus:
    createTime: "2024-11-04T10:53:14Z"
    currentNetworkState: Ready
    desiredNetworkState: Ready
    externalAddresses:
    - ip: xxx.xxx.xxx.xxx
    internalAddresses:
    - ip: 10.0.0.95
    lastTransitionTime: "2024-11-04T10:53:14Z"
    networkType: JdCloud-EIP
```