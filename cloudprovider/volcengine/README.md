English | [中文](./README.zh_CN.md)

The Volcaengine Kubernetes Engine supports the CLB reuse mechanism in k8s. Different SVCs can use different ports of the same CLB. Therefore, the Volcengine-CLB network plugin will record the port allocation corresponding to each CLB. For the specified network type as Volcengine-CLB, the Volcengine-CLB network plugin will automatically allocate a port and create a service object. Wait for the svc ingress field. After the public network IP is successfully created, the GameServer network is in the Ready state and the process is completed.
![image](https://github.com/lizhipeng629/kruise-game/assets/110802158/209de309-b9b7-4ba8-b2fb-da0d299e2edb)

## Volcengine-CLB configuration
### plugin configuration
```toml
[volcengine]
enable = true
[volcengine.clb]
#Fill in the free port segment that clb can use to allocate external access ports to pods, The maximum port range is 200.
max_port = 700
min_port = 500
```
### Parameter
#### ClbIds
- Meaning：fill in the id of the clb. You can fill in more than one. You need to create the clb in [Volcano Engine].
- Value：each clbId is divided by `,` . For example: `clb-9zeo7prq1m25ctpfrw1m7`,`clb-bp1qz7h50yd3w58h2f8je`,...
- Configurable：Y

#### PortProtocols
- Meaning：the ports and protocols exposed by the pod, support filling in multiple ports/protocols
- Value：`port1/protocol1`,`port2/protocol2`,... The protocol names must be in uppercase letters.
- Configurable：Y

#### AllocateLoadBalancerNodePorts
- Meaning：Whether the generated service is assigned nodeport, this can be set to false only in clb passthrough mode
- Value：false / true
- Configurable：Y

#### Fixed
- Meaning：whether the mapping relationship is fixed. If the mapping relationship is fixed, the mapping relationship remains unchanged even if the pod is deleted and recreated.
- Value：false / true
- Configurable：Y

#### AllowNotReadyContainers
- Meaning：the container names that are allowed not ready when inplace updating, when traffic will not be cut.
- Value：{containerName_0},{containerName_1},... eg：sidecar
- Configurable：It cannot be changed during the in-place updating process.

#### Annotations
- Meaning：the anno added to the service
- Value：key1:value1,key2:value2...
- Configurable：Y

### Example
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

Check the network status in GameServer:
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
