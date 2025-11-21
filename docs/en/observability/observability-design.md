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
