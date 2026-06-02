# Google Cloud Network Plugins for OpenKruiseGame

This provider exposes two network plugins that publish game-server pods on
Google Cloud, chosen via the `spec.network.networkType` field on a
`GameServerSet`:

| Plugin | Best for | Protocols | Topology | Notes |
|---|---|---|---|---|
| `GoogleCloud-PassthroughNLB` | UDP-heavy real-time games (FPS, MOBA, action) and TCP workloads that want absolute lowest latency + native client IP | TCP, UDP, mixed | Regional (External or Internal), per-pod static IP | Backed by GKE container-native L4 RBS + a KCC `ComputeAddress` for the shared static VIP |
| `GoogleCloud-GlobalProxyNLB` | TCP-only games with globally distributed players (MMO, SLG, card, casual) needing anycast + cross-region failover + Cloud Armor | TCP | Global anycast IP, GFE-backed | Authors the full KCC L4 proxy stack (ForwardingRule → TargetTcpProxy → BackendService → HealthCheck) plus a per-pod Firewall |

## When to pick which

| Your game | Recommended plugin |
|---|---|
| TCP, players globally distributed | `GoogleCloud-GlobalProxyNLB` |
| TCP, esports / lowest possible latency | `GoogleCloud-PassthroughNLB` |
| TCP, needs TLS offload or Cloud Armor | `GoogleCloud-GlobalProxyNLB` |
| UDP (any scenario) | `GoogleCloud-PassthroughNLB` |
| TCP + UDP mixed (KCP, QUIC, custom) | `GoogleCloud-PassthroughNLB` |
| Needs real client IP, no app changes | `GoogleCloud-PassthroughNLB` |

## Prerequisites (one-time, cluster-admin)

1. **VPC-native GKE cluster** (alias IPs). Autopilot is VPC-native by default.
2. **Config Connector (KCC) add-on** enabled and healthy:
   ```bash
   gcloud container clusters update <CLUSTER> --update-addons=ConfigConnector=ENABLED --region <REGION>
   kubectl wait --for=condition=Available deploy -n cnrm-system --all --timeout=300s
   ```
3. **Workload Identity** for the `cnrm-system` ServiceAccount bound to a GCP IAM
   service account holding `roles/compute.networkAdmin` (or a custom role
   covering forwardingRules / targetTcpProxies / backendServices / healthChecks
   / networkEndpointGroups / addresses / firewalls).
4. **GCP quotas** raised proportionally to expected scale. Each game-server pod
   consumes one `ForwardingRule`, one `BackendService`, one `HealthCheck`, one
   `Address`, one NEG per zone, and (Global Proxy NLB only) one `TargetTcpProxy`
   + one `Firewall`.

If KCC CRDs are not installed when the plugin starts, the plugin refuses to
register and logs the precise `gcloud` command to remediate.

## Operator configuration (`/etc/kruise-game/config.toml`)

```toml
[googlecloud]
enable = true
project_id = "my-game-project"
default_region = "us-central1"
default_network = "default"
default_subnetwork = "default-us-central1"

[googlecloud.passthrough_nlb]
enable = true
retain_on_delete_default = false
network_tier = "PREMIUM"

[googlecloud.global_proxy_nlb]
enable = true
retain_on_delete_default = false
firewall_network_ref = "default"
```

## `NetworkConf` keys

### `GoogleCloud-PassthroughNLB`

| Key | Required | Default | Notes |
|---|---|---|---|
| `PortProtocols` | yes | — | `7777/UDP,8000/TCP` — up to 5 entries per pod |
| `Scheme` | no | `External` | `External` or `Internal` |
| `Region` | yes (or via TOML) | TOML `default_region` | GCP region (e.g. `us-central1`) |
| `Network` | only `Scheme=Internal` | TOML `default_network` | VPC network name |
| `Subnetwork` | only `Scheme=Internal` | TOML `default_subnetwork` | Subnet name |
| `AllowGlobalAccess` | no | `false` | Internal LB only |
| `NetworkTier` | no | `PREMIUM` | `PREMIUM` or `STANDARD` |
| `ProjectId` | no | TOML `project_id` | Per-GameServerSet project override |
| `Annotations` | no | — | `k1=v1,k2=v2`, passed through to the Service |
| `RetainOnDelete` | no | `false` | `true` keeps the Address + Service across pod restarts |

### `GoogleCloud-GlobalProxyNLB`

| Key | Required | Default | Notes |
|---|---|---|---|
| `Port` | yes | — | Single TCP port 1-65535 |
| `ProxyHeader` | no | `NONE` | `NONE` or `PROXY_V1` (game must parse PROXY v1 line) |
| `BalancingMode` | no | `CONNECTION` | `CONNECTION`, `RATE`, `UTILIZATION` |
| `MaxConnectionsPerEndpoint` | no | `1000` | |
| `HealthCheckIntervalSec` | no | `5` | |
| `HealthCheckTimeoutSec` | no | `5` | Must be ≤ `HealthCheckIntervalSec` |
| `HealthyThreshold` | no | `2` | |
| `UnhealthyThreshold` | no | `2` | |
| `Network` | no | TOML `firewall_network_ref` (then `default_network`) | VPC for the per-pod firewall |
| `ProjectId` | no | TOML `project_id` | |
| `Annotations` | no | — | Service annotations |
| `RetainOnDelete` | no | `false` | |

## Resources created per pod

### Passthrough NLB

```
ComputeAddress  (per-pod static external/internal IP, KCC)
        ↑
Service (type=LoadBalancer, cloud.google.com/l4-rbs="enabled",
         networking.gke.io/load-balancer-ip-addresses=<addr name>)
        ↓ owned by GKE LB controller
ForwardingRule + BackendService + HealthCheck (in GCP project, not in K8s)
```

### Global Proxy NLB

```
Service (type=ClusterIP, cloud.google.com/neg='{"exposed_ports":{...}}')
   ↓ owned by GKE NEG controller
ComputeNetworkEndpointGroup × N zones (Pod IP endpoints, automatic)

ComputeHealthCheck (global, TCP, USE_SERVING_PORT)
ComputeBackendService (global, EXTERNAL_MANAGED, TCP, NEG backends)
ComputeTargetTCPProxy (proxyHeader)
ComputeAddress (global, EXTERNAL, PREMIUM, IPV4)
ComputeForwardingRule (global, EXTERNAL_MANAGED, one TCP port, target+address refs)
ComputeFirewall (allow 35.191.0.0/16 + 130.211.0.0/22 → backend port)
```

## IP stability across Pod recreate

The GCP resources for a replica are keyed on the owning **GameServerSet UID +
replica ordinal**, not the Pod UID. A Pod that is deleted and recreated at the
same ordinal (rolling update, eviction, manual `kubectl delete pod`, node drain)
re-adopts the identical `ComputeAddress`, so:

- the external/anycast IP does **not** change, and
- the load balancer is **not** rebuilt (no multi-minute GCP propagation).

Resources are released only on a genuine **scale-down** (the ordinal moves out
of `spec.replicas` range) or **GameServerSet deletion**. The Passthrough Service
and KCC CRs are owner-referenced to the GameServerSet (so Kubernetes GC reaps
them when the GSS is deleted); the per-replica scale-down case is handled
explicitly in the plugin's `OnPodDeleted`.

With `RetainOnDelete=true` the owner reference is dropped entirely, so the
reserved IP and load balancer survive even GameServerSet deletion (you then
manage their lifecycle yourself).

## Disabling a single GameServer's network

To drain external traffic from one replica without deleting it, set
`spec.networkDisabled: true` on the **GameServer** object (not a Pod label — the
GameServer controller continuously reconciles the Pod label from the GameServer
spec, so a direct Pod-label edit is reverted):

```bash
kubectl patch gameserver <gs-name> --type=merge -p '{"spec":{"networkDisabled":true}}'
```

The plugin flips the Service to `ClusterIP` (tearing down the LB) while keeping
the reserved IP; set it back to `false` to re-attach the LB to the same IP.

## Health-check latency tuning (GlobalProxyNLB)

Backend health-check timing is exposed via `NetworkConf`. Defaults
(`HealthCheckIntervalSec=5`, `UnhealthyThreshold=2`) detect a dead backend in
~30-40s including GFE state propagation. For faster failure detection:

```yaml
networkConf:
- {name: HealthCheckIntervalSec, value: "2"}
- {name: HealthCheckTimeoutSec, value: "2"}
- {name: UnhealthyThreshold, value: "1"}
- {name: HealthyThreshold, value: "1"}
```

This brings detection down to ~15s. GCP's minimum check interval is 1s, but
global GFE propagation sets a practical floor around 10-15s.

## Limitations

- **Global Proxy NLB is TCP-only**; UDP must use the Passthrough plugin.
- **Global Proxy NLB has no Shared VPC support**; pods in a Shared VPC service
  project must use Passthrough.
- **Per-pod resource fan-out**: each pod gets its own forwarding rule + backend
  service. At thousands of pods you will hit per-region quotas before you hit
  cluster limits — request quota increases ahead of time.
- **PROXY-protocol awareness**: with `ProxyHeader=PROXY_V1` the game-server
  binary must parse and strip the PROXY v1 line, or the first packet will look
  like garbage application data.
