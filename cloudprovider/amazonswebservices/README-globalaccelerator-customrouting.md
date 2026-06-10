# AmazonWebServices-GlobalAcceleratorCustomRouting

For latency-sensitive game servers that run as ordinary Deployment/StatefulSet Pods (Pod IP reachable via VPC-CNI), the `AmazonWebServices-GlobalAcceleratorCustomRouting` plugin exposes each Pod through an [AWS Global Accelerator **custom routing** accelerator](https://docs.aws.amazon.com/global-accelerator/latest/dg/about-custom-routing-accelerators.html). Players connect to a static anycast IP at the AWS edge; Global Accelerator deterministically maps an accelerator IP/port to the Pod IP/port inside a registered VPC subnet, preserving the real client source IP with no load balancer in the data path.

Unlike the `AmazonWebServices-NLB` plugin (see [README.md](./README.md)), this plugin does **not** create any AWS or Kubernetes resources for the data path (no NLB, no TargetGroup, no Service). It only toggles per-Pod traffic on a **pre-created** custom routing endpoint group and looks up the deterministic port mapping, then publishes the result into the GameServer `network-status` annotation. The game server reads the annotation (commonly via the downward API) and self-reports its `agaStaticIP:mappedPort` to clients.

## Prerequisites (created by the operator, NOT by the plugin)

The plugin assumes the following are provisioned out-of-band (CLI / SDK / Terraform). Skipping any of these will not produce a clean error from the plugin — the Pod will simply never receive traffic.

### 1. Custom routing accelerator + listener + endpoint group + registered subnet(s)

Port-mapping capacity (`subnet usable IPs × destination ports × protocols`) is a one-time capacity-planning decision and is intentionally kept out of the Pod hot path. The plugin only reads the endpoint group ARN and the Pod's subnet (`EndpointId`) from `networkConf`.

When creating the endpoint group, leave `DenyAllTrafficToEndpoint=true` (the default). The plugin's only runtime job is to selectively `Allow` per-Pod-IP destinations on this default-deny endpoint group.

References:
[Custom routing accelerators](https://docs.aws.amazon.com/global-accelerator/latest/dg/about-custom-routing-accelerators.html) · [Getting started — custom routing](https://docs.aws.amazon.com/global-accelerator/latest/dg/getting-started-custom-routing.html) · [Guidelines and restrictions](https://docs.aws.amazon.com/global-accelerator/latest/dg/about-custom-routing-guidelines.html)

#### Option 1 — AWS Console

The Global Accelerator console must be used in the **US West (Oregon) `us-west-2`** region (the AGA control plane lives there regardless of where your EKS cluster is).

1. Open the [Global Accelerator console](https://console.aws.amazon.com/globalaccelerator/home?region=us-west-2#/accelerators) → **Create accelerator** → select **Custom routing**. Pick a name and IP address pool (Amazon-owned is fine for most cases).
2. **Add listener**: choose a port range wide enough to cover `subnet usable IPs × destination ports × protocols` (see [Limits and quotas](#limits-and-quotas)). A single wide range like `16000–65535` is usually safest. Listener port ranges cannot be shrunk later, only grown.
3. **Add endpoint group**: pick the AWS region of your EKS cluster.
   - Under **Destination configurations**, add one row per game port: `From port`, `To port`, `Protocols` (TCP / UDP / both).
   - Leave **Deny all traffic to endpoints** **enabled** (this is the default and is what the plugin relies on).
4. **Add VPC subnet endpoints**: for each EKS Pod subnet you intend to register, add a row with that subnet ID. Subnets must be `/28`–`/17`. The same subnet may be registered to only one custom routing accelerator at a time.
5. Wait until the accelerator status reaches **Deployed**, then copy the **endpoint group ARN** — that is the value for `CustomRoutingEndpointGroupArn` in `networkConf`.

#### Option 2 — AWS CLI

All commands target `--region us-west-2` (the AGA control plane region). See the [API reference](https://docs.aws.amazon.com/global-accelerator/latest/api/Welcome.html) for full parameter detail.

```sh
# 1. Create the custom routing accelerator
ACC_ARN=$(aws globalaccelerator create-custom-routing-accelerator \
  --region us-west-2 \
  --name okg-cr-accelerator \
  --idempotency-token "$(date +%s)" \
  --query 'Accelerator.AcceleratorArn' --output text)

# Wait for status DEPLOYED before adding listeners (poll every 30s)
aws globalaccelerator describe-custom-routing-accelerator \
  --region us-west-2 --accelerator-arn "$ACC_ARN" \
  --query 'Accelerator.{Status:Status,DnsName:DnsName,IPs:IpSets[0].IpAddresses}'

# 2. Create a listener with one wide port range
LST_ARN=$(aws globalaccelerator create-custom-routing-listener \
  --region us-west-2 \
  --accelerator-arn "$ACC_ARN" \
  --port-ranges FromPort=16000,ToPort=65535 \
  --idempotency-token "$(date +%s)-lst" \
  --query 'Listener.ListenerArn' --output text)

# 3. Create an endpoint group in the EKS cluster's region.
#    DestinationConfigurations is a listener-level allow-list of destination
#    (port,protocol) pairs that the EG can map to. Add one entry per game port.
EG_ARN=$(aws globalaccelerator create-custom-routing-endpoint-group \
  --region us-west-2 \
  --listener-arn "$LST_ARN" \
  --endpoint-group-region <eks-cluster-region> \
  --destination-configurations FromPort=7777,ToPort=7777,Protocols=UDP \
  --idempotency-token "$(date +%s)-eg" \
  --query 'EndpointGroup.EndpointGroupArn' --output text)

# 4. Register each EKS Pod subnet as an endpoint. The subnet ID is what the
#    plugin's networkConf 'EndpointId' must reference.
aws globalaccelerator add-custom-routing-endpoints \
  --region us-west-2 \
  --endpoint-group-arn "$EG_ARN" \
  --endpoint-configurations EndpointId=<subnet-id>

echo "CustomRoutingEndpointGroupArn = $EG_ARN"
```

The DNS name printed by step 1 is the address the game client connects to; the plugin writes it (and the per-Pod mapped port) into the GameServer `network-status`.

#### Option 3 — Terraform / Infrastructure as code

Terraform's `aws_globalaccelerator_*` resources cover custom routing accelerators, listeners, endpoint groups, and subnet endpoints. Note that **AWS CloudFormation does not support custom routing accelerators**; CFN deployments must wrap the AWS CLI / SDK in a custom resource.

### 2. Node Security Group — open ingress to the game port

The node Security Group attached to the EKS worker nodes hosting the game-server Pods must allow inbound on the game port:

```
ingress: <Protocol> <GamePort> from 0.0.0.0/0
```

Custom routing preserves the real client IP and the source cannot be restricted to an AGA-owned address range. Traffic arrives directly at the node ENI hosting the Pod, so the **node SG**, not any AGA-side SG, is the gating control. If this rule is missing, AGA→Pod packets are silently dropped at the node SG: the listener inside the Pod looks healthy, but no packets ever arrive.

The AGA-managed ENIs/SGs in your VPC must not be hand-modified.

#### Identifying the right Security Group

Managed node groups attach a SG named like `eks-cluster-sg-<cluster>-<id>` (the EKS ["cluster security group"](https://docs.aws.amazon.com/eks/latest/userguide/sec-group-reqs.html)) to every worker ENI. Custom node groups may use a different SG — check the actual ENI:

```sh
# Inspect any worker node’s primary ENI to see which SGs are attached
NODE_INSTANCE=$(kubectl get node <node-name> -o jsonpath='{.spec.providerID}' | sed 's|.*/||')
aws ec2 describe-instances --instance-ids "$NODE_INSTANCE" \
  --query 'Reservations[].Instances[].SecurityGroups'
```

If you partition node groups (e.g. game servers vs. system workloads), apply the rule only to the SG used by the game-server node group.

#### Option 1 — AWS Console

1. Open the [VPC console → Security Groups](https://console.aws.amazon.com/vpc/home#SecurityGroups:) in the EKS cluster's region.
2. Select the node SG identified above → **Inbound rules** → **Edit inbound rules**.
3. **Add rule**: Type = `Custom UDP` (or `Custom TCP` to match `Protocol`), Port range = `<GamePort>`, Source = `0.0.0.0/0` (and `::/0` for IPv6 if you publish AAAA). Description = e.g. `okg-aga-customrouting`.
4. Save.

#### Option 2 — AWS CLI

```sh
aws ec2 authorize-security-group-ingress \
  --region <eks-cluster-region> \
  --group-id <node-sg-id> \
  --ip-permissions \
      'IpProtocol=udp,FromPort=7777,ToPort=7777,IpRanges=[{CidrIp=0.0.0.0/0,Description=okg-aga-customrouting}]'
```

Adjust `IpProtocol` and `FromPort/ToPort` to match your `Protocol` and `GamePort`. For TCP+UDP, repeat with `IpProtocol=tcp`. For IPv6 add a parallel entry with `Ipv6Ranges`.

> **VPC-CNI SNAT requires no change.** Custom routing inbound traffic reaches the Pod ENI without traversing the node's SNAT chain, and the return path is handled by conntrack. AGA Custom Routing works with the default EKS VPC-CNI settings; do not set `EXTERNALSNAT=true` or `RANDOMIZESNAT=none` for AGA's sake.

### 3. IAM

The controller's pod must be able to call three Global Accelerator APIs:

- `globalaccelerator:AllowCustomRoutingTraffic`
- `globalaccelerator:DenyCustomRoutingTraffic`
- `globalaccelerator:ListCustomRoutingPortMappingsByDestination`

**Recommended policy** (attach to the role assumed by the controller pod):

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

> Custom routing accelerators do not support resource-level IAM policies on the Allow/Deny APIs; `Resource: "*"` is the only working scope. Tighten exposure with separate AWS accounts / OUs if needed.

The plugin uses [`aws-sdk-go-v2`'s default credential chain](https://aws.github.io/aws-sdk-go-v2/docs/configuring-sdk/), so **both EKS auth methods work without code changes**:

#### Option A — EKS Pod Identity (recommended for new clusters)

1. Enable the EKS Pod Identity Agent add-on on the cluster (one-click in the EKS console or `aws eks create-addon --addon-name eks-pod-identity-agent`).
2. Create the IAM role with the policy above and the following trust policy:
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
3. Associate the role with the controller's service account:
   ```sh
   aws eks create-pod-identity-association \
     --cluster-name <cluster> \
     --namespace kruise-game-system \
     --service-account kruise-game-controller-manager \
     --role-arn arn:aws:iam::<account>:role/<role-name>
   ```

No annotation on the service account is required; the agent injects credentials via `AWS_CONTAINER_CREDENTIALS_FULL_URI`, which the SDK picks up automatically.

#### Option B — IRSA (works on every EKS version)

1. Ensure the cluster's OIDC provider is registered with IAM (`eksctl utils associate-iam-oidc-provider --cluster <cluster> --approve`).
2. Create the IAM role with the policy above and the following trust policy (replace placeholders):
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
3. Annotate the controller service account:
   ```sh
   kubectl annotate sa kruise-game-controller-manager -n kruise-game-system \
     eks.amazonaws.com/role-arn=arn:aws:iam::<account>:role/<role-name>
   ```
4. Restart the controller deployment so the projected token is mounted.

#### Other environments

Off-EKS deployments can supply credentials through any provider the default chain understands (instance profile, `~/.aws/credentials`, `AWS_ACCESS_KEY_ID`, etc.). The plugin does not bind a specific auth method.

### 4. No health checking

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

## Limits and quotas

Key AWS Global Accelerator quotas to plan around (defaults shown; see the [official quotas page](https://docs.aws.amazon.com/global-accelerator/latest/dg/limits-global-accelerator.html) for the current list and which quotas are adjustable):

| Resource | Default | Adjustable |
| --- | --- | --- |
| Custom routing accelerators per AWS account | **10** | yes |
| Listeners per accelerator | 10 | yes |
| Port ranges per listener | 10 | no |
| Endpoint groups per accelerator (across all listeners) | 42 | no |
| Subnet endpoints per endpoint group | 10 | yes |
| Subnet size | /28 – /17 | n/a |
| Listener port range minimum width | 16 ports | n/a |

**Port-mapping capacity** for one subnet is approximately `subnet usable IPs × destination ports × protocols`. Provision listener port ranges large enough to cover this product, and prefer a single wide listener range per accelerator (port ranges per listener cap at 10 and cannot be lowered after creation).

**Other constraints worth knowing**

- The plugin's API calls all run against the AGA control plane, which lives **only in `us-west-2`** regardless of the EKS cluster's own region. The plugin defaults to `us-west-2`; override with `Region` only for non-commercial partitions.
- **Custom routing accelerators are not supported by AWS CloudFormation.** Provision them with the AWS CLI, SDK, or Terraform.
- Custom routing accelerators are **IPv4-only**.
- No native health checks or failover — traffic is routed deterministically regardless of backend health.

## Operational notes

- **Deletion is eventually consistent.** When tearing down a custom routing accelerator, `update-custom-routing-accelerator --no-enabled` may report `Status=DEPLOYED, Enabled=False` while `delete-custom-routing-accelerator` still returns `AcceleratorNotDisabledException` for tens of seconds to a couple of minutes. Endpoint groups and listeners cannot be deleted while the accelerator is in `IN_PROGRESS` either. **Cleanup scripts must poll-and-retry** (e.g. 30s × 6 rounds). This is a Global Accelerator service behavior, not a plugin issue.
- **Mapping visibility.** If `ListCustomRoutingPortMappingsByDestination` does not yet show a freshly allowed Pod IP, the plugin publishes `NetworkNotReady` and returns no error, so the GameServer simply stays not-ready until the next reconcile. The mutating-webhook path never fails for this reason.
