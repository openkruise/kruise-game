# IMPL-NOTES — AWS Global Accelerator Custom Routing plugin

Branch: `feature/aws-aga-custom-routing` (worktree `kruise-game-customrouting`, based on clean `origin/master`).

---

## Review round 2 (2026-06-10) — addresses reviewer B1 / M2 / M3 / S4 + S-level

This round reworked `custom_routing.go` + `custom_routing_test.go` per the reviewer
report (1 Blocker + 2 Major + S-level) and Willy's decisions. **No data-path resource
creation logic changed; the plugin skeleton and `NetworkStatus` shape are unchanged.**

### S4 — explicit `EndpointId`, removed trial-and-error subnet resolution
- Dropped the `SubnetIds` (comma list) config and the `resolveEndpoint()` loop that
  called `AllowCustomRoutingTraffic` on each subnet and inferred ownership from AWS
  errors. That approach swallowed real API errors, made redundant calls, and could
  misclassify genuine failures as "wrong subnet".
- New required config key **`EndpointId`** = the VPC subnet ID the Pod runs in. The
  plugin uses exactly that ID for Allow / Deny / ListPortMappings. (DESIGN §7.)

### SDK — migrated aws-sdk-go v1 → aws-sdk-go-v2
- `github.com/aws/aws-sdk-go/...` → `github.com/aws/aws-sdk-go-v2/aws`,
  `.../config`, `.../service/globalaccelerator` (+ `/types`), `github.com/aws/smithy-go`.
  v1 (EOL) is no longer imported by this plugin.
- API-shape changes handled: context is the first positional arg (no `*WithContext`),
  inputs use value slices (`[]string`, `[]int32`) instead of `[]*string`/`[]*int64`,
  ports are `int32`, client built via `config.LoadDefaultConfig` + `NewFromConfig`.
- `go mod tidy` pulled the v2 module tree. **`go.mod` `go` directive bumped
  `1.23.0` → `1.24`** because the aws-sdk-go-v2 modules declare `go 1.24`; toolchain
  line removed. Build/vet/test all green under go1.24+ (local go1.26.3). No other repo
  code required changes.

### B1 (Blocker) — region anchored to us-west-2, configurable
- Old `session.NewSession()` used no explicit region → fell back to the IRSA-injected
  cluster region → calls would hit an endpoint with no AGA control plane on any cluster
  not in us-west-2.
- Client now built with `config.WithRegion(region)`, `region` defaulting to
  `defaultAGARegion = "us-west-2"`, overridable via the new optional **`Region`**
  config key. The misleading comment ("control plane lives in us-west-2, which the SDK
  selects by default" — which it did NOT) was replaced with one describing the actual
  anchoring.

### M2 — mapping-not-found no longer returns RetryError
- The plugin runs inside a mutating webhook: returning a non-nil `PluginError` makes
  OKG discard the mutated Pod (DeepCopy) and deny admission, silently dropping the
  `NetworkNotReady` status we wrote. So the `RetryError` dead-code path is removed.
- "Not ready yet" conditions (no Pod IP, mapping not visible yet) now publish
  `NetworkNotReady` and return `(pod, nil)` — matching the nlb.go / eip.go pattern.
  Only genuine Allow/List/Deny API failures or parameter errors return a `PluginError`.

### M3 — OnPodUpdated denies the old IP on Pod-IP change
- When the cached Pod IP differs from the current one (reschedule), the plugin now
  `DenyCustomRoutingTraffic` on the old IP before allowing the new one, so the limited
  custom-routing mapping capacity is not leaked. (Best-effort: a failed stale-deny is
  logged, not fatal, so the new IP can still be allocated.)
- The previously fake-green unit test now asserts the old IP is actually denied and the
  new IP is allowed (`TestOnPodUpdatedPodIPChanged`).

### S-level — precise error classification + error/paging tests
- Error inspection uses typed errors (`errors.As` on `smithy.APIError`) via
  `awsErrMessage()` for precise `Code: message` reporting instead of string matching.
- New tests: `TestOnPodAddedAllowError`, `TestOnPodAddedListError` (both assert
  `apiCallError`), `TestOnPodAddedListPaging` (multi-page `ListPortMappings` results all
  collected), `TestOnPodDeletedDenyError` (deny failure surfaces + cache retained),
  `TestRegionConfigPassedToClient` / `TestDefaultRegionAnchored` (region threaded into
  the client factory, default us-west-2). `MappingNotFound` now asserts NotReady +
  `(pod, nil)` instead of RetryError.

### Verification (this round, all green)
```
go build ./...                                                  # exit 0
go vet ./cloudprovider/amazonswebservices/                      # exit 0
go test ./cloudprovider/amazonswebservices/ -run CustomRouting  # ok
go test ./cloudprovider/amazonswebservices/                     # ok (full package)
gofmt -l cloudprovider/amazonswebservices/                      # empty (clean)
```
13 test funcs pass (parse table + 12 lifecycle/error/region cases).

> Note: `-run CustomRouting` matches only `TestParseCustomRoutingConfig`; the lifecycle
> tests are named `TestOnPod*` / `TestRegion*` / `TestDefaultRegion*`, covered by the
> full-package run. Both runs are green.

### Still open (unchanged from round 1)
downward-API annotation shape (DESIGN §9.1), real-AWS mapping-visibility latency
(§9.5), throttling under high churn (§9.4) — all need real-AWS / customer confirmation.

---

## What was implemented (round 1 — superseded where noted above)

A new, self-contained OKG network plugin that exposes ordinary Pods through an AWS
Global Accelerator **custom routing** accelerator. It does not touch the existing
`AmazonWebServices-NLB` plugin and coexists under the same `amazonswebservices`
provider, selected by `networkType`.

- `networkType` / plugin name constant: **`AmazonWebServices-GlobalAcceleratorCustomRouting`**
  (`GlobalAcceleratorCustomRoutingNetwork`); alias `GlobalAccelerator-CustomRouting`.

### Lifecycle (matches DESIGN §3)

- `Init`: lazily builds the AGA client (default AWS credential chain / IRSA), inits an
  in-memory cache. **Creates no AWS resources** (accelerator/listener/subnet/EG are
  pre-created by the operator). Client construction is injectable via `newAGAClient`
  for testing; default is `globalaccelerator.New(session.NewSession())`.
- `OnPodAdded` / `OnPodUpdated`: shared `reconcile()`:
  1. parse + validate `networkConf`;
  2. if `pod.Status.PodIP == ""` → publish `NetworkNotReady` (OKG retries);
  3. fast path: if cache already has this Pod IP resolved → just republish `Ready`
     (no-op, idempotent);
  4. otherwise iterate `SubnetIds`, call `AllowCustomRoutingTraffic`
     (dest `podIP:GamePort`) — only the owning subnet succeeds, others error and are
     skipped — then `ListCustomRoutingPortMappingsByDestination` to get the AGA static
     IP + mapped port;
  5. build `NetworkStatus` (`ExternalAddresses` = AGA IP+mapped port,
     `InternalAddresses` = Pod IP+GamePort), state `Ready`, write annotation;
  6. if mapping not yet visible → `NetworkNotReady` + `RetryError` (eventual
     consistency, DESIGN §9.5).
- `OnPodDeleted`: `DenyCustomRoutingTraffic` (dest `podIP:GamePort`). Uses the cached
  owning subnet when known, else falls back to denying across all configured subnets;
  clears the cache.

### Files

| File | Purpose |
| --- | --- |
| `cloudprovider/amazonswebservices/custom_routing.go` | Plugin implementation (~430 LOC). |
| `cloudprovider/amazonswebservices/custom_routing_test.go` | Unit tests (table-driven config parse + lifecycle with mock AGA). |
| `cloudprovider/amazonswebservices/README.md` | English plugin section appended. |
| `cloudprovider/amazonswebservices/README.zh_CN.md` | Chinese plugin section appended. |
| `go.mod` / `go.sum` | `aws-sdk-go v1.50.20` promoted indirect→direct (+`jmespath` indirect) via `go mod tidy`. |

Registration: `init()` in `custom_routing.go` calls
`amazonsWebServicesProvider.registerPlugin(&CustomRoutingPlugin{...})`, parallel to the
NLB plugin's `init()`.

## networkConf (DESIGN §7)

`CustomRoutingEndpointGroupArn` (required), `GamePort` (required, 1–65535),
`Protocol` (TCP/UDP, default UDP), `SubnetIds` (required, comma-separated multi-AZ).
Subnet ownership of the Pod IP is auto-resolved at runtime, so no per-subnet config is
needed beyond the list.

## Verification (all green)

```
go build ./...                         # exit 0
go vet ./...                           # exit 0
go test ./cloudprovider/...            # all ok (amazonswebservices + others)
gofmt -l custom_routing*.go            # empty (clean)
```

Test cases: `TestParseCustomRoutingConfig` (valid / default proto / missing arn /
missing subnets / bad port / out-of-range port / bad protocol), `TestOnPodAddedReady`,
`TestOnPodAddedNoPodIP`, `TestOnPodAddedMappingNotFound` (RetryError),
`TestOnPodUpdatedIdempotent` (no extra Allow call), `TestOnPodUpdatedPodIPChanged`
(re-allow + refresh), `TestOnPodDeleted`, `TestOnPodDeletedNoPodIP`.

## Design decisions / rationale

- **AGA client abstraction**: `customRoutingAPI` interface wraps only the three
  `*WithContext` SDK methods used (`AllowCustomRoutingTraffic`,
  `DenyCustomRoutingTraffic`, `ListCustomRoutingPortMappingsByDestination`). The
  concrete `*globalaccelerator.GlobalAccelerator` satisfies it directly; tests inject a
  `fakeAGA`. This keeps the SDK out of the unit-test path and leaves room for a future
  lazy-import refactor.
- **SDK v1 vs v2**: chose **aws-sdk-go v1 `service/globalaccelerator` v1.50.20**.
  Confirmed (via `go doc` against the module cache) that v1 already exposes all three
  custom routing APIs with the needed input/output structs (`DestinationPortMapping`,
  `SocketAddress`, etc.). v1 was already an indirect dependency, so this keeps the code
  stack consistent with the rest of the repo (the NLB plugin path also pulls AWS v1
  transitively) and avoids adding a v2 module tree. `go mod tidy` promoted it to a
  direct require — no version bump.
- **No CRD / RBAC changes**: the plugin only reads the Pod handed to it by the mutating
  webhook and calls AWS APIs; it creates no Kubernetes objects. So no new kubebuilder
  markers and no `config/rbac/role.yaml` change. (AWS IAM perms are an operator
  prerequisite, documented in README, not in code.)
- **Subnet resolution**: rather than asking the user which subnet a Pod lands in, the
  plugin tries `AllowCustomRoutingTraffic` on each configured subnet and treats the one
  that succeeds as the owner (AWS rejects an IP that isn't a subset of the subnet). This
  keeps `networkConf` minimal and survives multi-AZ scheduling without extra wiring.
- **Hard constraints (DESIGN §6)** — SNAT off, no health check, direct Pod IP (no
  HostPort), no external controller/finalizer — are treated as deployment prerequisites
  and documented in the README, not implemented in the plugin, exactly as specified.

## Open / TODO (needs real-AWS or customer confirmation)

1. **downward-API annotation shape (DESIGN §9.1)** — still the one open customer
   question: do they read the whole `network-status` JSON or a specific subfield
   (`externalAddresses[0].ip` + ports)? The schema written here is the standard OKG
   `NetworkStatus`; if the customer needs a different external shape, adjust
   `publishReady`.
2. **Mapping visibility latency (DESIGN §9.5)** — `ListCustomRoutingPortMappingsByDestination`
   is assumed to return immediately once the Pod IP exists in a registered subnet.
   Handled defensively with a `RetryError` path, but the real eventual-consistency
   window is unverified on real AWS.
3. **Allow/Deny under high churn (DESIGN §9.4)** — no client-side rate limiting/backoff
   added; relies on OKG's reconcile retry. If throttling shows up at scale, add backoff
   in `resolveEndpoint`.
4. **`DestinationSocketAddress.Port` filter** — `lookupExternalAddresses` filters
   mappings to those whose destination port equals `GamePort`. If a deployment registers
   multiple destination ports and expects all of them surfaced, this would need to drop
   the filter / emit multiple ports. Current scope is single `GamePort` per
   GameServerSet per DESIGN §7.
5. **Deny granularity on delete** — when the owning subnet isn't cached (e.g. controller
   restarted between add and delete), `OnPodDeleted` best-effort denies across all
   subnets and does not surface an error (the non-owning subnets legitimately reject).
   Only a known-subnet failure is returned for retry.
