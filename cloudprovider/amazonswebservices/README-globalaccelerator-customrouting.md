# AmazonWebServices-GlobalAcceleratorCustomRouting

For latency-sensitive game servers that run as ordinary Deployment/StatefulSet Pods (Pod IP reachable via VPC-CNI), the `AmazonWebServices-GlobalAcceleratorCustomRouting` plugin exposes each Pod through an [AWS Global Accelerator **custom routing** accelerator](https://docs.aws.amazon.com/global-accelerator/latest/dg/about-custom-routing-accelerators.html). Players connect to a static anycast IP at the AWS edge; Global Accelerator deterministically maps an accelerator IP/port to the Pod IP/port inside a registered VPC subnet, preserving the real client source IP with no load balancer in the data path.

Unlike the `AmazonWebServices-NLB` plugin (see [README.md](./README.md)), this plugin does **not** create any AWS or Kubernetes resources for the data path (no NLB, no TargetGroup, no Service). It only toggles per-Pod traffic on a **pre-created** custom routing endpoint group and looks up the deterministic port mapping, then publishes the result into the GameServer `network-status` annotation. The game server reads the annotation (commonly via the downward API) and self-reports its `agaStaticIP:mappedPort` to clients.

> **Verified end-to-end on EKS (us-west-2) on 2026-06-10.** Real client UDP echo through AGA → Pod, OnPodDeleted Deny, OnPodUpdated PodIP-change, paged `ListCustomRoutingPortMappings` (25 Pods, 8187 mappings), and high-frequency Allow/Deny scaling all passed without any AGA throttling.

## Prerequisites (created by the operator, NOT by the plugin)

The plugin assumes the following are provisioned out-of-band (CLI / SDK / Terraform). Skipping any of these will not produce a clean error from the plugin — the Pod will simply never receive traffic.

### 1. Custom routing accelerator + listener + endpoint group + registered subnet(s)

Port-mapping capacity (`subnet usable IPs × destination ports × protocols`) is a one-time capacity-planning decision and is intentionally kept out of the Pod hot path. The plugin only reads the endpoint group ARN and the Pod's subnet (`EndpointId`) from `networkConf`.

When creating the endpoint group, set `DenyAllTrafficToEndpoint=true`. The plugin's only runtime job is to selectively `Allow` per-Pod-IP destinations on this default-deny endpoint group.

### 2. Cluster-level SNAT — must use `EXTERNALSNAT=true`

Custom routing's main value is preserving the real client IP all the way to the Pod ENI. To make this work end-to-end, return traffic from the Pod must NOT be SNAT'd by the node:

```sh
kubectl set env ds/aws-node -n kube-system AWS_VPC_K8S_CNI_EXTERNALSNAT=true
kubectl rollout restart ds/aws-node -n kube-system
```

Node groups should sit in **private subnets behind a NAT Gateway** so Pod egress (to the public Internet, AWS APIs, etc.) still works after SNAT is disabled. This is a cluster-wide change with known side effects (Pods in public subnets lose direct egress).

> **Note on `RANDOMIZESNAT=none`**: AWS VPC CNI has a second SNAT-related env, `AWS_VPC_K8S_CNI_RANDOMIZESNAT=none`, that keeps SNAT enabled but stops randomizing source ports (used for protocols like SIP that need predictable ports). It is **not a substitute** for `EXTERNALSNAT=true`. In our 2026-06-10 e2e the inbound UDP path happened to work under `RANDOMIZESNAT=none` because the inbound flow does not traverse the SNAT chain, but production deployments should set `EXTERNALSNAT=true` directly so return traffic also bypasses SNAT.

### 3. Node Security Group — open ingress to the game port

```
ingress: <Protocol> <GamePort> from 0.0.0.0/0
```

Custom routing preserves the real client IP and the source cannot be restricted to an AGA-owned address range. Traffic arrives directly at the node ENI hosting the Pod, so the **node SG**, not any AGA-side SG, is the gating control. If this rule is missing, AGA→Pod packets are silently dropped at the node SG: the listener inside the Pod looks healthy, but no packets ever arrive — the most common deployment trap. Verified on 2026-06-10.

The AGA-managed ENIs/SGs in your VPC must not be hand-modified.

### 4. IAM (IRSA)

The controller's service account needs:

- `globalaccelerator:AllowCustomRoutingTraffic`
- `globalaccelerator:DenyCustomRoutingTraffic`
- `globalaccelerator:ListCustomRoutingPortMappingsByDestination`

Credentials are picked up from the default AWS credential chain; the plugin does not bind any auth method.

### 5. No health checking

Custom routing has no native health checks or failover — delivery is deterministic regardless of backend health. The plugin does not implement health probing; unhealthy Pods are recycled by Kubernetes (recreate → `OnPodDeleted` → `OnPodAdded` re-resolves the mapping).

If the application needs UDP-style health, expose a separate TCP/HTTP probe and have the application call `Deny` itself; this plugin will not do that for you.

## `networkConf` parameters

| Name | Required | Description |
| --- | --- | --- |
| `CustomRoutingEndpointGroupArn` | yes | ARN of the pre-created custom routing endpoint group. |
| `GamePort` | yes | Destination port the game server listens on (`1`–`65535`). |
| `Protocol` | no | `TCP` or `UDP`. Defaults to `UDP`. |
| `EndpointId` | yes | The registered VPC **subnet ID** the Pod runs in (the custom routing endpoint ID). Supplied explicitly by the user; the plugin never guesses by trial-and-error. In a multi-AZ deployment, pin each GameServerSet to the subnet of its node pool / AZ — one GameServerSet per subnet. |
| `Region` | no | AWS region of the Global Accelerator **control plane**. Defaults to `us-west-2`. All Allow/Deny/ListPortMappings calls are issued here regardless of the cluster's own region. Override only for other partitions / future control-plane regions. |

## Lifecycle

| Plugin hook | Action |
| --- | --- |
| `Init` | Initialize the in-memory cache. The Global Accelerator client (aws-sdk-go-v2, default AWS credential chain, region anchored to the AGA control plane) is built lazily on first use. The plugin creates no accelerator / listener / subnet. |
| `OnPodAdded` / `OnPodUpdated` | Take `pod.Status.PodIP`; `AllowCustomRoutingTraffic` (destination `podIP:GamePort`) on the user-specified `EndpointId`; `ListCustomRoutingPortMappingsByDestination` (paged) to obtain the AGA static IP + mapped port; write `NetworkStatus.ExternalAddresses{IP: agaStaticIP, Ports: mappedPort}` with `InternalAddresses` set to the Pod IP and state `Ready`. If the Pod IP is not assigned yet or the mapping is not yet visible, `NetworkNotReady` is published and `(pod, nil)` returned (no error escapes the mutating-webhook path). Idempotent: an unchanged Pod IP is a no-op. If the Pod IP changes (reschedule), the previous IP is `Deny`'d before the new IP is allowed, so mapping capacity is not leaked. |
| `OnPodDeleted` | `DenyCustomRoutingTraffic` (destination `podIP:GamePort`) to drain the Pod. |

## Example GameServerSet

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
      # - name: Region        # optional, defaults to us-west-2
      #   value: "us-west-2"
  gameServerTemplate:
    spec:
      containers:
        - name: game
          image: your-game-image:latest
```

The resulting GameServer `network-status` looks like:

```yaml
networkStatus:
  currentNetworkState: Ready
  desiredNetworkState: Ready
  externalAddresses:
  - ip: 75.2.1.1            # AGA static anycast IP — self-reported by the game server
    ports:
    - name: game
      port: 50001            # custom routing mapped port
      protocol: UDP
  internalAddresses:
  - ip: 10.0.1.23            # Pod IP
    ports:
    - name: game
      port: 7777
      protocol: UDP
  networkType: AmazonWebServices-GlobalAcceleratorCustomRouting
```

## Operational notes

- **Deletion is eventually consistent.** When you tear down a custom routing accelerator, `update-custom-routing-accelerator --no-enabled` may report `Status=DEPLOYED, Enabled=False` while `delete-custom-routing-accelerator` still returns `AcceleratorNotDisabledException` for tens of seconds to a couple of minutes. Endpoint groups and listeners cannot be deleted while the accelerator is in `IN_PROGRESS` either. **Cleanup scripts must poll-and-retry** (e.g. 30s × 6 rounds). This is a Global Accelerator service behavior, not a plugin issue.
- **Mapping visibility.** After `AllowCustomRoutingTraffic` succeeds, `ListCustomRoutingPortMappingsByDestination` returns the mapping immediately in our test (no observable eventual-consistency gap). The plugin still treats "not visible yet" as `NetworkNotReady` rather than as an error, so a slow propagation would result in a brief `NetworkNotReady` window, never a webhook failure.
