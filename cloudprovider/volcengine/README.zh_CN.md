中文 | [English](./README.md)

火山引擎容器服务支持在k8s中对CLB复用的机制，不同的svc可以使用同一个CLB的不同端口。由此，Volcengine-CLB network plugin将记录各CLB对应的端口分配情况，对于指定了网络类型为Volcengine-CLB，Volcengine-CLB网络插件将会自动分配一个端口并创建一个service对象，待svc ingress字段的公网IP创建成功后，GameServer的网络处于Ready状态，该过程执行完成。
![image](https://github.com/lizhipeng629/kruise-game/assets/110802158/209de309-b9b7-4ba8-b2fb-da0d299e2edb)

## Volcengine-CLB 相关配置
### plugin配置
```toml
[volcengine]
enable = true
[volcengine.clb]
#填写clb可使用的空闲端口段，用于为pod分配外部接入端口，范围最大为200
max_port = 700
min_port = 500
```
### 参数
#### ClbIds
- 含义：填写clb的id，可填写多个，需要现在【火山引擎】中创建好clb。
- 填写格式：各个clbId用,分割。例如：clb-9zeo7prq1m25ctpfrw1m7,clb-bp1qz7h50yd3w58h2f8je,...
- 是否支持变更：是

#### PortProtocols
- 含义：pod暴露的端口及协议，支持填写多个端口/协议
- 填写格式：port1/protocol1,port2/protocol2,...（协议需大写）
- 是否支持变更：是

#### Fixed
- 含义：是否固定访问IP/端口。若是，即使pod删除重建，网络内外映射关系不会改变
- 填写格式：false / true
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
  name: gss-2048-clb
  namespace: default
spec:
  replicas: 3
  updateStrategy:
    rollingUpdate:
      podUpdatePolicy: InPlaceIfPossible
  network:
    networkType: Volcengine-CLB
    networkConf:
      - name: ClbIds
        #Fill in Volcengine Cloud LoadBalancer Id here
        value: clb-xxxxx
      - name: PortProtocols
        #Fill in the exposed ports and their corresponding protocols here. 
        #If there are multiple ports, the format is as follows: {port1}/{protocol1},{port2}/{protocol2}...
        #If the protocol is not filled in, the default is TCP
        value: 80/TCP
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
        - image: cr-helm2-cn-beijing.cr.volces.com/kruise/2048:v1.0
          name: app-2048
          volumeMounts:
            - name: shared-dir
              mountPath: /var/www/html/js
        - image: cr-helm2-cn-beijing.cr.volces.com/kruise/2048-sidecar:v1.0
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
EOF
```

检查GameServer中的网络状态:
```
networkStatus:
    createTime: "2024-01-19T08:19:49Z"
    currentNetworkState: Ready
    desiredNetworkState: Ready
    externalAddresses:
    - ip: xxx.xxx.xx.xxx
      ports:
      - name: "80"
        port: 6611
        protocol: TCP
    internalAddresses:
    - ip: 172.16.200.60
      ports:
      - name: "80"
        port: 80
        protocol: TCP
    lastTransitionTime: "2024-01-19T08:19:49Z"
    networkType: Volcengine-CLB
```
