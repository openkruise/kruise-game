# OpenKruiseGame Observability Design

This document details the architectural design and implementation choices for the OpenKruiseGame (OKG) observability stack. It is intended for maintainers and contributors who wish to extend or debug the observability features.

## Architecture Goals

1.  **Vendor Neutrality**: Use OpenTelemetry (OTel) as the single standard for all telemetry data.
2.  **Correlation**: Ensure Logs, Traces, and Metrics are tightly coupled to allow seamless navigation between them.
3.  **Low Overhead**: Minimize the performance impact on the controller manager.
4.  **Asynchronous Context Propagation**: Solve the challenge of tracing across Kubernetes asynchronous boundaries (Controller -> API Server -> Webhook).

## 1. Tracing Design

### The "Span Link" Pattern

Kubernetes controllers and webhooks are decoupled. The controller updates an object, and the API server (synchronously or asynchronously) invokes the webhook. Standard context propagation (via HTTP headers) works for the Webhook request itself, but doesn't automatically link back to the Controller's reconciliation loop that triggered the update.

To solve this, OKG uses a **Span Link** pattern:

1.  **Controller (Producer)**:
    *   Starts a `Reconcile` span.
    *   Before updating a Pod, it injects a `game.kruise.io/traceparent` annotation containing the current W3C Trace Context.
2.  **Webhook (Consumer)**:
    *   Starts a `Handle` span (triggered by API Server).
    *   Reads the `game.kruise.io/traceparent` annotation.
    *   **Crucial Step**: Instead of making the Reconcile span the *parent* (which would imply a synchronous blocking call), it adds a **Span Link** to the Reconcile span.
    *   This accurately represents the relationship: "This webhook execution is related to that reconciliation attempt."

### Attributes

We adhere to OTel Semantic Conventions where possible:
*   `k8s.namespace.name` (use `tracing.AttrK8sNamespaceName(namespace)` or `telemetryfields.FieldK8sNamespaceName`)
*   `k8s.pod.name`
*   `service.name` (default: `okg-controller-manager`)

Custom attributes use the `game.kruise.io` prefix (defined in `pkg/tracing/attributes.go`):
*   `game.kruise.io.game_server.name`
*   `game.kruise.io.game_server_set.name`
*   `game.kruise.io.network.status`
*   `game.kruise.io.network.plugin.name`

### Canonical telemetry enumerations

To keep Grafana dimensions stable and prevent high-cardinality spikes, OKG uses a small set of canonical enumeration values for fields that are surfaced as spanmetrics dimensions. These values are defined in `pkg/telemetryfields/values.go` and used by `pkg/tracing` attributes/helpers.

Error classification (business-level, low-cardinality):
- `api_call_error` — External API / cloud SDK failures (ApiCallError).
- `internal_error` — Internal runtime/code error (InternalError).
- `parameter_error` — Parameter or validation error (ParameterError).
- `not_implemented_error` — Not implemented feature invoked (NotImplementedError).
- `resource_not_ready` — Cloud resource not ready yet (ResourceNotReady).
- `port_exhausted` — No available port found (PortExhausted).

Network status:
- `ready` — network resources assigned and usable
- `not_ready` — network resources missing (IP, ports, etc.)
- `error` — plugin/provider error occurred
- `waiting` — resources allocation pending

Plugin slug canonical values:
- `kubernetes_hostport` — Kubernetes HostPort plugin
- `kubernetes_nodeport` — Kubernetes NodePort plugin
- `alibabacloud_nlb` — Alibaba Cloud NLB plugin

Design notes:
- Use `error.type` (low-cardinality) for business classification and `exception.type`/`exception.message` (OTel) for SDK/exception details.
- Avoid registering `exception.message` or other long text fields as spanmetrics or Prometheus labels — they are high-cardinality and will affect cardinality of metrics.
- Use `telemetryfields.Normalize*` helper functions to normalize and map legacy/camelCase variants into canonical snake_case enumerations.

## 2. Logging Design

### `otelzap` Integration

We use `go.opentelemetry.io/contrib/bridges/otelzap` to bridge Uber's `zap` logger to OTel.

*   **Tee Logger**: The controller logger is configured as a `zapcore.NewTee`.
    *   **Core 1 (Console)**: Writes to stdout/stderr (filtered, human-readable or JSON).
    *   **Core 2 (OTel)**: Writes to the OTel Exporter (gRPC).
*   **Graceful Degradation**: If the OTel endpoint is invalid or unreachable, the OTel core is disabled, but the Console core continues to function.

### Context Propagation

The `logging.FromContextWithTrace(ctx)` helper extracts the SpanContext from the Go `context.Context` and injects `trace_id` and `span_id` fields into the zap logger fields. This ensures that any log line written with that logger instance automatically carries the trace context.

## 3. Metrics Design

### Span Metrics

Instead of manually instrumenting every network plugin with Prometheus counters, we rely on the **SpanMetrics Connector** in the OTel Collector.

*   **Mechanism**: The Collector observes the stream of spans.
*   **Generation**: It aggregates spans based on dimensions (e.g., `game.kruise.io.network.plugin.name`) and emits Prometheus metrics (histograms, counters).
*   **Benefit**: Zero code changes needed in plugins to get detailed RED (Rate, Errors, Duration) metrics.

## 4. Development Guide

### Quick Start: Demo Chart vs E2E Infrastructure

**For users and initial exploration**, use the **kruise-game-observability-demo** Helm chart:
- Location: https://github.com/openkruise/charts/tree/main/charts/versions/kruise-game-observability-demo/0.1.0/
- Purpose: Zero-config local stack for experiencing OKG observability
- Includes: OTel Collector, Prometheus, Loki, Tempo, Grafana (all local)
- Use case: Quick demos, learning, local development

**For contributors and CI testing**, use the **e2e manifests**:
- Location: `kruise-game/test/e2e/manifests/`
- Purpose: CI-grade infrastructure with dual-write capability
- Includes: Same stack + optional Grafana Cloud remote write
- Use case: Debugging ephemeral CI runners, long-term trace/log retention, multi-cluster aggregation

The key difference is **data persistence**: demo chart uses local storage (deleted with the cluster), while e2e infra can mirror data to Grafana Cloud for post-mortem analysis of CI test failures.

### Adding New Spans

Use the `pkg/tracing` helper constants and functions.

```go
import "github.com/openkruise/kruise-game/pkg/tracing"

// Start a span
ctx, span := tracer.Start(ctx, "MyOperation",
    trace.WithAttributes(
        tracing.AttrGameServerName(gs.Name),
    ),
)
defer span.End()

// Log with trace context and structured fields
logger := logging.FromContextWithTrace(ctx)
logger.Info("Allocating network resources", 
    telemetryfields.FieldGameServerName, gs.Name,
    telemetryfields.FieldNetworkPluginName, "HostPort",
)
```

### Running Observability E2E Tests

Currently, we verify the observability stack in CI by integrating with **Grafana Cloud**.

To run these tests in your own fork or environment:

1.  Register for a [Grafana Cloud](https://grafana.com/) account (free tier works).
2.  Obtain your OTel and Prometheus endpoints and API keys.
3.  Configure the following **GitHub Actions Secrets** in your repository.

    > **Where to find these?**
    > Go to your [Grafana Cloud Portal](https://grafana.com/profile/org), click **Details** on your Stack.
    > *   **OTLP Info**: Found under the "OpenTelemetry" section.
    > *   **Prometheus Info**: Found under the "Prometheus" or "Hosted Metrics" section.

    *   **OTel / Grafana Connection:**
        *   `GRAFANA_CLOUD_STACK_ID`: Your Stack ID.
        *   `GRAFANA_CLOUD_API_KEY`: A Cloud Access Policy token with write permissions.
        *   `GRAFANA_CLOUD_REGION`: The region slug (e.g., `prod-us-east-0`).
    *   **Prometheus (Metrics):**
        *   `GRAFANA_CLOUD_PROM_INSTANCE_ID`: The Prometheus "Username" / "Instance ID".
        *   `GRAFANA_CLOUD_PROM_API_TOKEN`: The Prometheus "Password" / API Token.
        *   `GRAFANA_CLOUD_PROM_REMOTE_WRITE_URL`: The Prometheus "Remote Write Endpoint".
4.  Set the variable `GRAFANA_CLOUD_ENABLED` to `true` in your workflow or repository variables.

The CI workflow (`e2e-1.34.yaml`) will automatically detect these secrets, configure the OTel Collector to export data to Grafana Cloud, and run the observability verification tests.

---

## 5. Using Observability in Development & CI

For contributors and maintainers the main payoff is faster debugging, especially on ephemeral CI runners where the cluster disappears as soon as the job completes. When GitHub Actions is configured with Grafana Cloud secrets, each e2e run streams traces/logs/metrics to Grafana Cloud; you debug in Grafana, not by re-running tests locally or fishing through CI dumps. While iterating on a new network plugin, you add spans/logs, push a commit, wait for CI, and then open Grafana Cloud to see the full trace tree and correlated logs for that run. No extra plumbing beyond the OTel defaults and the secrets from §4.

### Case Studies: Debugging with OTel Traces

**Case study 1 – CNI race, discovered from traces**

- Situation: an e2e run intermittently reported Network NotReady, but the CI artifacts only showed that Pods were scheduled and CNI logs looked normal at first glance.
- What we saw in Grafana Cloud: error spans with `game.kruise.io.network.status=not_ready` and `game.kruise.io.k8s.pod_ip=""`, even though internal/external ports were present. The webhook span fired <100ms after the scheduler placed the Pod.
- Interpretation: the webhook was legitimately running in the window between “Pod scheduled” and “CNI assigned PodIP”. This is normal Kubernetes timing, not a plugin bug.
- Outcome: instead of chasing this as a production error, we could treated it as a transient condition and marked it as a candidate for `codes.Ok` + `span.AddEvent()`, so error-rate panels reflect only issues that need attention.

**Case study 2 – Informer cache staleness during scale-out**

- Situation: during GameServerSet scale-out, CI occasionally logged "Advanced StatefulSet already exists" and failed a test. The raw dump suggested a logic bug in the creation path.
- What we saw in Grafana Cloud: very short error spans (<50ms) on the create path, with `exception.message` containing `AlreadyExists`. The trace showed the controller deciding “resource missing → create”, while the API server already had the object.
- Interpretation: classic informer cache lag between controller-runtime’s cache and the API server. The retry loop handles it transparently; there was no long-latency or external API failure in the timeline.
- Outcome: we kept the retry logic, documented this as an expected pattern, and again treated it as a candidate to be downgraded from error span to event.

### Takeaways for contributors

- Use Grafana Cloud traces/logs as the primary artifact for debugging observability and networking issues in CI; the free tier is sufficient for correlating reconcilers, webhooks, and plugins.
- Let actionability drive span status: persistent, operator-facing problems stay `codes.Error`; expected transient states should be `codes.Ok` + events. This keeps spanmetrics-based error rates meaningful.
- Keep attributes low-cardinality and consistent (via `telemetryfields.*` helpers) so spanmetrics remain queryable across runs and dashboards stay stable.
