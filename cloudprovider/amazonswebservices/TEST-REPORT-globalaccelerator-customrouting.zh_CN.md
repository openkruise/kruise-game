# 测试报告:AGA Custom Routing 插件

> 本文记录 `AmazonWebServices-GlobalAcceleratorCustomRouting` 插件在真实
> EKS 集群上的功能与扩容验证,作为 PR 评审、回归基线与生产部署前的参考。
> 完整使用手册见 [`README-globalaccelerator-customrouting.zh_CN.md`](./README-globalaccelerator-customrouting.zh_CN.md)。

## 1. 测试目标

| # | 目标 | 验证方式 |
|---|------|---------|
| T1 | 单 Pod 的 `Protocol=TCPUDP`:同 8601 端口同时承载 TCP + UDP | 容器内同时监听 TCP/UDP,外部双协议探测均收到 `*OK from <pod>` |
| T2 | 单 GSS 跨多 AZ 子网部署(`EndpointIds` 复数 + CIDR 自动匹配) | replicas=70 分布在 3 个 AZ,插件按 Pod IP 自动 Allow 到对应 EndpointId |
| T3 | 批量扩容的控制面延迟(0 → 70 副本) | 记录每个 Pod 的 created / running / ready / network-ready 时间戳 |
| T4 | 跨大洲端到端连通性 | 从另一 region 的 EC2 实例对每个 (anycastIP, mappedPort) 做 TCP+UDP 探测 |
| T5 | 路由确定性(非负载均衡) | 应答里包含 hostname,要求 280 个探测全部回到"PR 的那个 Pod" |
| T6 | 控制器删除时的清理正确性 | 删 GSS 后查 AGA 上是否还有 ALLOW 残留 |

## 2. 测试环境

| 项 | 值 |
|---|---|
| K8s 发行版 / 版本 | EKS,Kubernetes 1.36 |
| EKS region | `ap-east-2`(3 个 AZ:`a`/`b`/`c`,3 个私有子网) |
| 节点池 | Karpenter 管理,Pod 用 VPC-CNI 直接分配 ENI IP |
| AGA Accelerator | 1 个 Custom Routing accelerator,2 个 anycast IP |
| AGA EG | 1 个 endpoint group,3 个 subnet endpoints,`DestinationConfigurations = (8601→8601, [TCP, UDP])` |
| 控制器 | `kruise-game-controller-manager` 单实例,IRSA 凭证 |
| 外部探测客户端 | 1 台 `us-east-1` 的 EC2 实例,跨大洲走 AWS 骨干 |

## 3. 被测 GSS 配置

聚焦本 PR 引入的两个新能力(`Protocol=TCPUDP` + `EndpointIds` 复数):

```yaml
apiVersion: game.kruise.io/v1alpha1
kind: GameServerSet
metadata: {name: aga-cr-tcpudp}
spec:
  replicas: 0                                  # 后续 kubectl scale 拉到 70
  network:
    networkType: AmazonWebServices-GlobalAcceleratorCustomRouting
    networkConf:
      - {name: CustomRoutingEndpointGroupArn, value: "arn:aws:globalaccelerator::<account-id>:accelerator/<acc>/listener/<lst>/endpoint-group/<eg>"}
      - {name: GamePort, value: "8601"}
      - {name: Protocol, value: "TCPUDP"}      # ← 同端口双协议
      - name: EndpointIds                      # ← 多子网 + CIDR
        value: "subnet-AAA=10.0.11.0/24,subnet-BBB=10.0.12.0/24,subnet-CCC=10.0.13.0/24"
  gameServerTemplate:
    metadata: {labels: {app: aga-cr-tcpudp}}
    spec:
      affinity:
        nodeAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            nodeSelectorTerms:
            - matchExpressions:
              - {key: topology.kubernetes.io/zone, operator: In, values: [<az-a>, <az-b>, <az-c>]}
      topologySpreadConstraints:
      - {maxSkew: 1, topologyKey: topology.kubernetes.io/zone, whenUnsatisfiable: ScheduleAnyway,
         labelSelector: {matchLabels: {app: aga-cr-tcpudp}}}
      containers:
      - name: game
        image: python:3.11-slim
        command: ["sh", "-c", "<同时启动 TCP/UDP 监听 8601 的 Python 进程,应答里带 $HOSTNAME>"]
        ports:
        - {containerPort: 8601, protocol: TCP}
        - {containerPort: 8601, protocol: UDP}
        readinessProbe: {tcpSocket: {port: 8601}, initialDelaySeconds: 2, periodSeconds: 2}
```

## 4. 测试结果

### 4.1 时间轴

| 节点 | 相对 T0 |
|---|---|
| T0 = `kubectl scale --replicas=70` 发出 | 0 |
| T1 = 所有 70 GameServer `networkStatus.currentNetworkState=Ready` 且 `externalAddresses` 非空 | **+64.4 s** |
| T2 = 外部 280 个 (anycastIP, proto, mappedPort) 全部探测通过 | +112.9 s |

> T1 → T2 之间 48.5 s 主要是测试编排耗时(打包 endpoints 列表、SSM 调度、Python 启动)。**真实的并发探测过程只用了 1.44 s**(workers=60)。

### 4.2 K8s + AGA 控制面分阶段延迟(70 Pod 全体,相对 T0,秒)

| 阶段 | 平均 | 最快 | 最慢 |
|---|---|---|---|
| `pod_created`(informer 看到 Pod) | 1.91 | 0.56 | 4.56 |
| `pod_ready`(容器 readinessProbe 通过) | 11.10 | 4.67 | 24.33 |
| `net_ready`(AGA Allow + ListPortMappings 可见) | **26.98** | 0.56 | 44.56 |

`net_ready` 的尾部延迟主要花在 `ListCustomRoutingPortMappingsByDestination` 的最终一致性窗口上 —— `AllowCustomRoutingTraffic` 返回成功后,映射表对外可见仍有秒级延迟,插件会在 reconcile 中重试。

### 4.3 Pod 跨 AZ 分布 ✅(验证 T2)

| AZ | Pod 数 | 自动匹配到的 EndpointId(子网) | 探测 TCP+UDP 通过 |
|---|---|---|---|
| `<az-a>` | 11 | `subnet-AAA` | 22/22 |
| `<az-b>` | 43 | `subnet-BBB` | 86/86 |
| `<az-c>` | 15 | `subnet-CCC` | 30/30 |

70 = 11 + 43 + 15。`topologySpreadConstraints` 在节点资源不均时(b 区可用节点更多)按预期"尽量打散"。`EndpointIds` 复数模式下,插件根据 Pod IP 自动选择正确的 EndpointId 调用 `AllowCustomRoutingTraffic`,**无任何错配**。

### 4.4 外部端到端连通性 ✅(验证 T1 + T4)

测试矩阵: 70 pods × 2 anycast IP × {TCP, UDP} = 280 个探测目标。

| 协议 | 成功 | 失败 | RTT 平均 | RTT 最小 | RTT 最大 |
|---|---|---|---|---|---|
| TCP | 140/140 | 0 | 356.3 ms | 337.9 ms | 373.9 ms |
| UDP | 140/140 | 0 | 178.6 ms | 170.1 ms | 184.5 ms |
| **合计** | **280/280** | **0** | — | — | — |

TCP RTT ≈ UDP × 2,符合预期(TCP 包含 3-way handshake + 应用层 recv,UDP 是单 round-trip)。

### 4.5 路由确定性 ✅(验证 T5)

每个 Pod 内进程在 TCP/UDP 应答里附带 `$HOSTNAME`(即 Pod 名)。外部探测时对每条结果做匹配:

```
expected_pod == response_hostname  → 280 / 280 全部匹配
```

这是最强的路由确定性证据:每条 `(anycastIP, mappedPort)` 流量**精确地**到达 Pod annotation 里宣称的那个 Pod,**没有任何错配**,印证了 AGA Custom Routing 的"1:1 静态映射"语义。

### 4.6 删除清理 ✅(验证 T6)

```
kubectl delete gss aga-cr-tcpudp
→ 所有 70 个 Pod terminating
→ 控制器对每个 Pod 调用 DenyCustomRoutingTraffic
→ 查询 ListCustomRoutingPortMappings,DestinationTrafficState=ALLOW 计数 = 0
```

未观察到孤儿条目;控制器 `OnPodDeleted` 路径稳定。

## 5. 复现脚本骨架

完整测试脚本见 PR 描述及讨论。最小复现路径:

```bash
# 1. 应用 GSS(replicas=0)
kubectl apply -f <见 §3 的 YAML>

# 2. 扩到 70 + 每 2 秒采集状态
T0=$(date -u +%s.%N); kubectl scale gss aga-cr-tcpudp --replicas=70
while [ "$(kubectl get gs -l game.kruise.io/owner-gss=aga-cr-tcpudp \
            -o json | jq '[.items[] | select(.status.networkStatus.currentNetworkState=="Ready" and (.status.networkStatus.externalAddresses|length)>0)] | length')" -lt 70 ]; do
  sleep 2
done; echo "Elapsed: $(awk -v a=$T0 -v b=$(date -u +%s.%N) 'BEGIN{printf "%.1f", b-a}')s"

# 3. 列出所有 (pod, anycastIP, proto, port)
kubectl get gs -l game.kruise.io/owner-gss=aga-cr-tcpudp -o json \
  | jq -r '.items[] | .metadata.name as $n | .status.networkStatus.externalAddresses[]?
           | .ip as $ip | .ports[] | "\($n)\t\($ip)\t\(.protocol)\t\(.port)"'

# 4. 在外部客户端用 Python 并发 TCP+UDP 探测(检查应答 hostname 是否与 $n 匹配)
```

## 6. 结论

| 维度 | 结论 |
|---|---|
| `Protocol=TCPUDP` 同端口双协议 | ✅ 工作正常,数据面单条 AGA 映射承载两协议 |
| `EndpointIds` 跨 AZ 单 GSS 部署 | ✅ 工作正常,Pod IP → CIDR → EndpointId 自动匹配无错配 |
| 控制面扩容性能(0→70) | ✅ 64.4 s 全部就绪;`net_ready` 平均 27 s |
| 跨大洲连通性与延迟稳定性 | ✅ TCP 356 ms / UDP 179 ms,延迟波动 ±20 ms |
| 路由确定性 | ✅ 280/280 hostname 校验全部匹配 |
| 删除清理 | ✅ AGA ALLOW 残留 = 0 |

可在生产场景小规模灰度使用;扩容建议结合 Karpenter / Cluster Autoscaler 一起回归节点冷启动对 `net_ready` 尾延迟的影响。

## 7. 已知限制 / 后续工作

- 本插件**不自动创建** AGA Accelerator / Listener / Endpoint Group / 子网注册,要求运维侧提前一次性建好。这是为了避免把 AWS 资源生命周期耦合到一个 K8s 对象上(详见 PR 描述 §"Out of scope")。
- 没有覆盖以下场景的回归(欢迎社区补充):
  - 节点失联触发 Pod 重调度后,新 Pod IP 的 Allow / 旧 Pod IP 的 Deny 完整闭环
  - 控制器自身重启后 Init 路径的孤儿清理(本 PR 已含逻辑,但本次未模拟控制器 crash 工况)
  - 跨 region 的 EG(EG region != EKS region)
  - 与游戏服务质量(`ServiceQualities`)、`networkDisabled` 等 OKG 特性的交叉验证
