# AmazonWebServices-GlobalAcceleratorCustomRouting

对于以普通 Deployment/StatefulSet Pod 形式运行（Pod IP 经 VPC-CNI 可直连）的低延迟游戏服，`AmazonWebServices-GlobalAcceleratorCustomRouting` 插件通过 [AWS Global Accelerator **自定义路由（custom routing）** 加速器](https://docs.aws.amazon.com/global-accelerator/latest/dg/about-custom-routing-accelerators.html) 暴露每个 Pod。玩家连接到 AWS 边缘的静态 anycast IP，Global Accelerator 会把加速器 IP/端口确定性地映射到已注册 VPC 子网内的 Pod IP/端口，数据路径上没有负载均衡，且保留真实客户端源 IP。

与 `AmazonWebServices-NLB` 插件（见 [README.zh_CN.md](./README.zh_CN.md)）不同，本插件**不**为数据面创建任何 AWS 或 Kubernetes 资源（无 NLB、无 TargetGroup、无 Service）。它只在**预先创建好**的 custom routing endpoint group 上对每个 Pod 切换 Allow/Deny，并查询确定性端口映射，再把结果写入 GameServer 的 `network-status`。游戏服通过 downward API 等方式读取该字段，自行把 `agaStaticIP:mappedPort` 上报给客户端。


## 部署前置（运维方提供，**不**由插件创建）

下列资源由部署方通过 CLI / SDK / Terraform 提前建好。**任何一项缺失插件都不会报清晰的错误，Pod 只会"安装上但永远收不到流量"**——务必逐条核对。

### 1. Custom routing accelerator + listener + endpoint group + 子网注册

端口映射容量（`子网可用 IP × 目标端口 × 协议组合数`）是一次性的容量规划决策，刻意不放进 Pod 热路径。插件只从 `networkConf` 读取 endpoint group ARN 和 Pod 所在子网 ID（`EndpointId`）。

创建 endpoint group 时设置 `DenyAllTrafficToEndpoint=true`。插件运行时唯一的职责就是在这个默认拒绝的 EG 上选择性 `Allow` 各 Pod IP 目的地。

### 2. 节点 Security Group —— 放行游戏端口入站

```
ingress: <Protocol> <GamePort> from 0.0.0.0/0
```

Custom routing 保留真实客户端 IP，源地址范围无法限定到 AGA 拥有的网段。流量直接到达承载 Pod 的 node ENI，所以是**节点 SG**（而非任何 AGA 侧 SG）作为流量门控。这条规则缺失 → AGA→Pod 的包被节点 SG 直接 drop，Pod 内监听看似正常却收不到任何包。

AGA 在你 VPC 中托管的 ENI/SG 不要手改。

> **VPC-CNI SNAT 无需修改。** Custom routing 的入向流量直接到达 Pod ENI，不经节点 SNAT 链；回程由 conntrack 处理。AGA Custom Routing 使用 EKS VPC-CNI 默认设置即可，不要为 AGA 去设置 `EXTERNALSNAT=true` / `RANDOMIZESNAT=none`。

### 3. IAM

Controller pod 需能调用三个 Global Accelerator API：

- `globalaccelerator:AllowCustomRoutingTraffic`
- `globalaccelerator:DenyCustomRoutingTraffic`
- `globalaccelerator:ListCustomRoutingPortMappingsByDestination`

**推荐 policy**（挂到 controller pod 拿的 IAM role 上）：

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "OkgCustomRoutingPlugin",
      "Effect": "Allow",
      "Action": [
        "globalaccelerator:AllowCustomRoutingTraffic",
        "globalaccelerator:DenyCustomRoutingTraffic",
        "globalaccelerator:ListCustomRoutingPortMappingsByDestination"
      ],
      "Resource": "*"
    }
  ]
}
```

> Custom routing 的 Allow/Deny API 不支持资源级 IAM，`Resource: "*"` 是唯一可行范围。需隔离请用 AWS 账号 / OU 级分隔。

插件用 [`aws-sdk-go-v2` 默认 credential chain](https://aws.github.io/aws-sdk-go-v2/docs/configuring-sdk/)，**EKS 两种身份方式都原生支持，不需代码改动**：

#### 方式 A——EKS Pod Identity（新集群推荐）

1. 在集群上启用 EKS Pod Identity Agent add-on（EKS 控制台一键开，或 `aws eks create-addon --addon-name eks-pod-identity-agent`）。
2. 创建一个 IAM role，挂上面那块 policy，使用以下 trust policy：
   ```json
   {
     "Version": "2012-10-17",
     "Statement": [{
       "Effect": "Allow",
       "Principal": {"Service": "pods.eks.amazonaws.com"},
       "Action": ["sts:AssumeRole", "sts:TagSession"]
     }]
   }
   ```
3. 将该 role 关联到 controller 的 ServiceAccount：
   ```sh
   aws eks create-pod-identity-association \
     --cluster-name <cluster> \
     --namespace kruise-game-system \
     --service-account kruise-game-controller-manager \
     --role-arn arn:aws:iam::<account>:role/<role-name>
   ```

ServiceAccount 上不需 annotation；agent 会注入 `AWS_CONTAINER_CREDENTIALS_FULL_URI`，SDK 自动识别。

#### 方式 B——IRSA（所有 EKS 版本都能用）

1. 确认集群的 OIDC provider 已在 IAM 注册（`eksctl utils associate-iam-oidc-provider --cluster <cluster> --approve`）。
2. 创建 IAM role，挂上面 policy，使用以下 trust policy（替换占位符）：
   ```json
   {
     "Version": "2012-10-17",
     "Statement": [{
       "Effect": "Allow",
       "Principal": {"Federated": "arn:aws:iam::<account>:oidc-provider/oidc.eks.<region>.amazonaws.com/id/<OIDC-ID>"},
       "Action": "sts:AssumeRoleWithWebIdentity",
       "Condition": {"StringEquals": {
         "oidc.eks.<region>.amazonaws.com/id/<OIDC-ID>:sub": "system:serviceaccount:kruise-game-system:kruise-game-controller-manager",
         "oidc.eks.<region>.amazonaws.com/id/<OIDC-ID>:aud": "sts.amazonaws.com"
       }}
     }]
   }
   ```
3. 给 controller ServiceAccount 打 annotation：
   ```sh
   kubectl annotate sa kruise-game-controller-manager -n kruise-game-system \
     eks.amazonaws.com/role-arn=arn:aws:iam::<account>:role/<role-name>
   ```
4. 重启 controller deployment，让 projected token 重新 mount。

#### 其他环境

非 EKS 部署可用默认 chain 能识别的任何提供者（instance profile、`~/.aws/credentials`、`AWS_ACCESS_KEY_ID` 等）。插件不绑死特定 auth 方式。

### 4. 不做健康检查

Custom routing 原生没有健康检查 / 故障转移——确定性投递不管后端死活。插件不实现健康探测；不健康的 Pod 由 Kubernetes 自身重建（重建走 `OnPodDeleted` → `OnPodAdded` 重新解析映射）。

如果应用要 UDP 类健康检查，请在应用侧暴露独立 TCP/HTTP 探针并由应用自行调 `Deny`，本插件不负责。

## `networkConf` 参数

| 名称 | 必填 | 说明 |
| --- | --- | --- |
| `CustomRoutingEndpointGroupArn` | 是 | 预先创建好的 custom routing endpoint group ARN。 |
| `GamePort` | 是 | 游戏服监听的目标端口（`1`–`65535`）。 |
| `Protocol` | 否 | `TCP` 或 `UDP`，默认 `UDP`。 |
| `EndpointId` | 是 | Pod 所在的、已注册到 endpoint group 的 VPC **子网 ID**（即 custom routing endpoint ID）。**由用户显式填写，插件不会试错猜测**。多 AZ 场景下，每个 GameServerSet 应通过 nodeSelector / affinity 钉到对应 AZ 的子网——一个 GameServerSet 对应一个子网。 |
| `Region` | 否 | Global Accelerator **控制面**所在的 AWS 区域，默认 `us-west-2`。所有 Allow/Deny/ListPortMappings 调用都打到这里，与集群自身所在区域无关。仅在其他 partition / 未来新增控制面区域时覆盖。 |

## 生命周期

| 插件 hook | 行为 |
| --- | --- |
| `Init` | 初始化内存缓存。Global Accelerator 客户端（aws-sdk-go-v2，默认 AWS 凭证链，区域钉到 AGA 控制面）首次使用时懒加载。插件不创建 accelerator / listener / 子网。 |
| `OnPodAdded` / `OnPodUpdated` | 取 `pod.Status.PodIP`；在用户指定的 `EndpointId` 上调 `AllowCustomRoutingTraffic`（目的 `podIP:GamePort`）；调 `ListCustomRoutingPortMappingsByDestination`（分页）拿到 AGA 静态 IP + 映射端口；把 `NetworkStatus.ExternalAddresses{IP: agaStaticIP, Ports: mappedPort}` + `InternalAddresses=PodIP` 写入并置状态 `Ready`。Pod IP 还没下来或映射尚不可见时，发布 `NetworkNotReady` 并返回 `(pod, nil)`（mutating webhook 路径上不抛 error）。幂等：Pod IP 不变是 no-op。Pod IP 改变（重调度）时先 `Deny` 旧 IP 再 Allow 新 IP，避免泄漏映射容量。 |
| `OnPodDeleted` | 调 `DenyCustomRoutingTraffic`（目的 `podIP:GamePort`）排空该 Pod。 |

## GameServerSet 示例

```yaml
apiVersion: game.kruise.io/v1alpha1
kind: GameServerSet
metadata:
  name: game
  namespace: default
spec:
  replicas: 3
  network:
    networkType: AmazonWebServices-GlobalAcceleratorCustomRouting
    networkConf:
      - name: CustomRoutingEndpointGroupArn
        value: "arn:aws:globalaccelerator::123456789012:accelerator/abcd1234/listener/5678/endpoint-group/9012"
      - name: GamePort
        value: "7777"
      - name: Protocol
        value: "UDP"
      - name: EndpointId
        value: "subnet-0a1b2c3d"
      # - name: Region        # 可选，默认 us-west-2
      #   value: "us-west-2"
  gameServerTemplate:
    spec:
      containers:
        - name: game
          image: your-game-image:latest
```

得到的 GameServer `network-status` 形如：

```yaml
networkStatus:
  currentNetworkState: Ready
  desiredNetworkState: Ready
  externalAddresses:
  - ip: 75.2.1.1            # AGA 静态 anycast IP，由游戏服自行上报
    ports:
    - name: game
      port: 50001            # custom routing 映射端口
      protocol: UDP
  internalAddresses:
  - ip: 10.0.1.23            # Pod IP
    ports:
    - name: game
      port: 7777
      protocol: UDP
  networkType: AmazonWebServices-GlobalAcceleratorCustomRouting
```

## 限制与 quota

需重点关注的 AWS Global Accelerator quota（默认值，最新及可调项以[官方 quota 页面](https://docs.aws.amazon.com/global-accelerator/latest/dg/limits-global-accelerator.html)为准）：

| 资源 | 默认值 | 可调 |
| --- | --- | --- |
| 每个 AWS 账号 custom routing accelerator 数 | **10** | 是 |
| 每个 accelerator 的 listener 数 | 10 | 是 |
| 每个 listener 的 port range 数 | 10 | 否 |
| 每个 accelerator 的 endpoint group 数（跨所有 listener） | 42 | 否 |
| 每个 endpoint group 的子网 endpoint 数 | 10 | 是 |
| 子网大小 | /28 – /17 | 不适用 |
| 单个 listener port range 最小宽度 | 16 个端口 | 不适用 |

**单子网端口映射容量** 约 = `子网可用 IP 数 × 目标端口数 × 协议数`。listener portRange 要足够大以覆盖这个乘积；建议一个 accelerator 只用一个宽范围 listener（每 listener 的 port range 上限 10 且创建后不可缩小）。

**其他需知的限制**

- 插件的 API 调用全部打到 AGA 控制面，**仅位于 `us-west-2`**，与 EKS 集群所在 region 无关。插件默认钉到 `us-west-2`；只有跨 partition 时才需要用 `Region` 参数覆盖。
- **Custom routing accelerator 不支持 AWS CloudFormation**。资源请用 AWS CLI / SDK / Terraform 创建。
- Custom routing accelerator **仅支持 IPv4**。
- 无原生健康检查、无故障转移 —— 流量以确定性方式投递，不看后端健康。

## 运维注意

- **删除是最终一致的。** 拆除 custom routing accelerator 时，`update-custom-routing-accelerator --no-enabled` 后 `describe` 即使返回 `Status=DEPLOYED, Enabled=False`，`delete-custom-routing-accelerator` 仍可能持续报 `AcceleratorNotDisabledException` 几十秒到 1-2 分钟。Endpoint group / listener 在 accelerator 处于 `IN_PROGRESS` 时也无法删除。**清理脚本必须带轮询重试**（建议 30s × 6 轮）。这是 Global Accelerator 服务行为，不是插件 bug。
- **映射可见性。** 若 `ListCustomRoutingPortMappingsByDestination` 尚未返回刚 Allow 的 Pod IP，插件发布 `NetworkNotReady` 不报错，GameServer 会保持 not-ready 直到下一轮 reconcile；mutating webhook 路径不会因此失败。
