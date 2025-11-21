# Observability

This guide targets OpenKruiseGame operators (SRE, DevOps, platform engineers). It explains how to enable the OpenTelemetry pipeline, where each signal comes from, and how to read controller/network health from the exported data.

## 1. Enable the pipeline

The controller manager binary already contains log, trace, and metric instrumentation. You only need to:

1. Deploy an OpenTelemetry Collector (see §5). The manifests under `test/e2e/manifests` are production-ready samples.
2. Pass the following flags to the `kruise-game-controller-manager` container:

   ```yaml
   - --enable-tracing=true                # enable span export + remote logs
   - --otel-collector-endpoint=otel-collector.observability.svc.cluster.local:4317
   - --otel-sampling-rate=1.0             # tune down only if needed
   - --log-format=json                    # <optional> set stdout/stderr log format
   ```
3. Make sure the Downward API injects namespace/pod metadata:

   ```yaml
         env:
           - name: POD_NAME
             valueFrom:
               fieldRef:
                 fieldPath: metadata.name
           - name: POD_NAMESPACE
             valueFrom:
               fieldRef:
                 fieldPath: metadata.namespace
           - name: POD_UID
             valueFrom:
               fieldRef:
                 fieldPath: metadata.uid
   ```
   These values are propagated to logs/traces/metrics as `service.*` and `k8s.*` attributes.

## 2. Logging

- `--log-format` controls only what you see in `kubectl logs` (`console` or `json`). The default is console.
- **OTLP bridge**: once `--otel-collector-endpoint` is set, the controller wraps the local zap core with a tee that forwards every log entry to `otelzap` before the console/JSON encoder runs. The collector therefore always receives fully structured OTLP logs—even when stdout stays in console mode—so Grafana/Loki keep the rich metadata without sacrificing human-readable pod logs.
- `logging.FromContextWithTrace(ctx)` is used across controllers/webhooks so every log emitted inside a span carries `trace_id` and `span_id`. Collectors (using the `transform/log_trace_labels` processor from §5) can therefore correlate log lines back to traces.


## 3. Distributed tracing

- Enable tracing with `--enable-tracing=true` and point to your collector via `--otel-collector-endpoint`.
- `--otel-sampling-rate` sets the `TraceIDRatioBased` sampler. Values between 0 and 1 are accepted; invalid values abort startup.
- Controllers emit spans for every `GameServer` and `GameServerSet` reconcile. Attributes include:
  - `game.kruise.io.game_server.name` / `.namespace`
  - `game.kruise.io.game_server_set.name`
  - `game.kruise.io.network.status` (e.g., `waiting`, `ready`, `error`)
- `GameServerManager` writes a `game.kruise.io/traceparent` annotation onto Pods. The admission webhook reads it, links its own span to the reconcile trace, and records the network plugin it invoked.
- Cloud provider plugins (Alibaba NLB, Kubernetes NodePort/HostPort) wrap every allocate/deallocate/service operation in spans so you can inspect latency or errors per plugin.

**Operational tips**

- Spans fall back to a no-op provider when OTLP dialing fails; the controller logs `Tracing initialization failed, using no-op tracer` in that case.
- Search for traces & logs by attributes like `game.kruise.io.game_server.name`, `game.kruise.io.network.plugin.name`, `cloud.provider`, or `k8s.namespace.name` (use `tracing.AttrK8sNamespaceName(ns)` or `telemetryfields.FieldK8sNamespaceName`), you can find them at `pkg/tracing/attributes.go`.

## 4. Metrics

### 4.1 Native controller metrics (`/metrics`)

These are registered in `pkg/metrics/prometheus_metrics.go` and exposed at the manager’s `--metrics-bind-address` (default `:8080`). Scrape them with Prometheus or the collector’s `prometheus/native` receiver.

| Metric | Type | Labels | What it tells you |
| --- | --- | --- | --- |
| `okg_gameservers_state_count` | Gauge | `state` | Live GameServer distribution across states. |
| `okg_gameservers_opsState_count` | Gauge | `opsState`, `gssName`, `namespace` | Number of GameServers in operational states. |
| `okg_gameservers_total` | Counter | – | Cumulative GameServers processed since controller start. |
| `okg_gameserversets_replicas_count` | Gauge | `gssName`, `gssNs`, `gsStatus` | Replica counts per GameServerSet broken down by GameServer status. |
| `okg_gameserver_deletion_priority` | Gauge | `gsName`, `gsNs` | Active deletion priority value for each GameServer. |
| `okg_gameserver_update_priority` | Gauge | `gsName`, `gsNs` | Update priority value. |
| `okg_gameserver_ready_duration_seconds` | Gauge | `gsName`, `gsNs`, `gssName` | Time from creation to `Ready`. |
| `okg_gameserver_network_ready_duration_seconds` | Gauge | `gsName`, `gsNs`, `gssName` | Time until `NetworkStatus.Ready`. Spikes indicate plugin issues. |

Suggested alerts/dashboards:
- `histogram_quantile(0.95, rate(okg_gameserver_network_ready_duration_seconds[5m]))` > SLA
- `okg_gameservers_state_count{state="NetworkNotReady"}` cresting triggers scaling or plugin investigation

### 4.2 Span-derived network metrics (`okg_network` namespace)

The collector’s `spanmetrics` connector turns network spans into Prometheus metrics:

- **Counters**: span count per plugin/operation/status (RED “R”).
- **Histograms**: latency of port allocation/service operations (RED “L”). Buckets are defined in the manifest (`0.01s`, `0.1s`, … `10s`).
- **Error ratios**: filter by `status.code="ERROR"` or `game.kruise.io.network.status="error"` to see failure percentages.
- **Exemplars**: enabled so Grafana panels can link directly back to Tempo traces.

Use these metrics to answer questions such as “Which provider is causing 5xx latency?” (`sum(rate(okg_network_calls_total{cloud_provider="alibaba_cloud",status_code="ERROR"}[5m]))`) or “Is NLB port allocation exceeding 500 ms?” (`histogram_quantile(0.95, rate(okg_network_latency_seconds_bucket{game_kruise_io_network_plugin_name="AlibabaCloud-NLB"}[5m]))`).

## 5. Collector deployment

You can reuse the sample stack from `test/e2e/manifests` (namespace `observability`). Two variants are available:

- `01-otel-collector.yaml`: local stack (Tempo/Loki/Prometheus inside the cluster).
- `01-otel-collector-grafana.yaml`: dual-write stack that mirrors everything to Grafana Cloud via OTLP / Prometheus Remote Write while keeping the local sinks.

- **Receivers**:
  - OTLP gRPC/HTTP (`traces`, `logs`)
  - Prometheus scrape of the controller (`prometheus/native`)
  - `spanmetrics` connector (receives spans from `traces/network` pipeline and generates RED metrics)

- **Processors**:
  - `k8sattributes`: Injects `game.kruise.io.k8s.pod_ip`/`k8s.pod.uid` labels for all signals.
  - `transform/log_trace_labels`: Copies `trace_id`/`span_id` into log attributes so Loki can pivot back to traces.
  - `tail_sampling`: Keeps error/slow traces while sampling everything else probabilistically.
  - `filter/network_only`: Feeds only network plugin spans into the spanmetrics connector to avoid noise.

- **Pipelines**:
  - `traces/network` → `filter/network_only` → `spanmetrics connector` (Generates metrics)
  - `metrics/network` (Source: spanmetrics) → `prometheus/local` (:8889) + `prometheusremotewrite` (Cloud)
  - `metrics/native` (Source: controller :8080) → `prometheus/local` (:8889) + `prometheusremotewrite` (Cloud)
  - `traces/storage` → Tempo + Grafana Cloud
  - `logs` → Loki + Grafana Cloud

- **Exports**:
  - **Local (Pull)**: `prometheus/local` exposes **aggregated metrics** (both native and span-derived) on `:8889` for the local Prometheus to scrape.
  - **Local (Push)**: `otlp/tempo` and `otlphttp/loki` push directly to local storage backends.
  - **Cloud (Push)**: `otlphttp/grafana_cloud` and `prometheusremotewrite/grafana_cloud` push data to Grafana Cloud endpoints.
  - **Self-Monitoring**: The collector exposes its own telemetry on `:8888`.

```mermaid
graph LR
    classDef metrics fill:#e1f5fe,stroke:#01579b,stroke-width:2px;
    classDef traces fill:#fce4ec,stroke:#880e4f,stroke-width:2px;
    classDef logs fill:#e8f5e9,stroke:#1b5e20,stroke-width:2px;
    classDef store fill:#fff3e0,stroke:#e65100,stroke-width:2px,stroke-dasharray: 5 5;
    classDef cloud fill:#f3e5f5,stroke:#4a148c,stroke-width:2px,stroke-dasharray: 5 5;

    subgraph Sources [Telemetry Sources]
        direction TB
        CM[Controller Manager]
        CM_Metrics[Native Metrics :8080]:::metrics
        CM_Trace[OTLP Trace]:::traces
        CM_Log[OTLP Log]:::logs
        
        CM --> CM_Metrics
        CM --> CM_Trace
        CM --> CM_Log
    end

    subgraph Collector [OTel Collector]
        
        subgraph Receivers [Receivers]
            direction TB
            Receiver_Prom[Prometheus/Native]:::metrics
            Receiver_OTLP[OTLP Receiver]:::traces
        end

        subgraph Processors [Processors & Connectors]
            direction TB
            Processor_Filter[Filter: Network Only]:::traces
            Connector_SpanMetrics[Connector: SpanMetrics]:::metrics
        end
        
        subgraph Pipelines [Pipelines]
            direction TB
            Pipeline_Native[Metrics: Native]:::metrics
            Pipeline_Network[Metrics: Network]:::metrics
            Pipeline_Trace[Traces: Storage]:::traces
            Pipeline_Log[Logs]:::logs
        end

        subgraph Exporters [Exporters]
            direction TB
            Exporter_PromLocal[Prometheus Local :8889]:::metrics
            Exporter_RemoteWrite[Remote Write]:::metrics
            Exporter_Tempo[OTLP Tempo]:::traces
            Exporter_Loki[OTLP Loki]:::logs
            Exporter_CloudOTLP[OTLP Grafana]:::traces
        end
    end

    subgraph Storage [Storage Backends]
        direction TB
        
        subgraph Local_Cluster [Local Cluster]
            LocalProm[Local Prometheus]:::store
            Tempo[Tempo]:::store
            Loki[Loki]:::store
        end

        subgraph Cloud_Platform [Grafana Cloud]
            GC_Prom[Hosted Prometheus]:::cloud
            GC_OTLP[Hosted OTLP - Trace/Log]:::cloud
        end
    end

    %% 1. Sources -> Receivers
    CM_Metrics -->|Scrape| Receiver_Prom
    CM_Trace -->|gRPC| Receiver_OTLP
    CM_Log -->|gRPC| Receiver_OTLP

    %% 2. Receivers -> Pipelines / Processors
    Receiver_Prom --> Pipeline_Native
    Receiver_OTLP --> Pipeline_Trace
    Receiver_OTLP --> Pipeline_Log
    
    Receiver_OTLP --> Processor_Filter --> Connector_SpanMetrics
    Connector_SpanMetrics --> Pipeline_Network

    %% 3. Pipelines -> Exporters
    
    %% Metrics Flow
    Pipeline_Native --> Exporter_PromLocal
    Pipeline_Network --> Exporter_PromLocal
    Pipeline_Native --> Exporter_RemoteWrite
    Pipeline_Network --> Exporter_RemoteWrite

    %% Trace Flow
    Pipeline_Trace --> Exporter_Tempo
    Pipeline_Trace --> Exporter_CloudOTLP

    %% Log Flow
    Pipeline_Log --> Exporter_Loki
    Pipeline_Log --> Exporter_CloudOTLP

    %% 4. Exporters -> Storage
    Exporter_PromLocal -->|Scrape| LocalProm
    Exporter_RemoteWrite -->|Push| GC_Prom
    
    Exporter_Tempo -->|Push| Tempo
    Exporter_Loki -->|Push| Loki
    
    Exporter_CloudOTLP -->|Push| GC_OTLP
```

**Operational checklist**

1. `kubectl logs -n observability deploy/otel-collector` should show the health endpoint (`0.0.0.0:13133`) reporting `PASS`.
2. Prometheus must scrape both the controller service (`kruise-game-controller-manager-metrics-service:http-metrics`) and the collector’s `:8888`/`:8889` endpoints.
3. Ensure Tempo/Loki services are reachable from Grafana. Exemplars require Tempo + Prometheus 2.44+.


## 6. Troubleshooting

| Symptom | Checks |
| --- | --- |
| Logs missing from Loki | Confirm `--otel-collector-endpoint` is set, the collector pipeline (`transform/log_trace_labels`) is healthy, and there are no `otelzap` warnings about OTLP exporter failures. `--log-format` does not affect remote delivery. |
| No traces in Tempo | Confirm collector `traces/storage` pipeline is healthy and Tempo service reachable. |
| Spanmetrics empty | Confirm `game.kruise.io.network.plugin.name` attribute exists (look at a single trace). Validate `filter/network_only` processor isn’t excluding spans unintentionally. Prometheus must scrape `otel-collector:8889`. |
| Metrics endpoint missing | Ensure `--metrics-bind-address` is set (default `:8080`) and the Service `kruise-game-controller-manager-metrics-service` is pointing to it. |
| Network plugin slowness | Check `okg_gameserver_network_ready_duration_seconds` (controller) and corresponding spanmetrics histograms for the same timeframe. Use exemplars to correlate spikes with specific traces. |

Following this guide provides end-to-end visibility across controller reconciles, admission webhooks, and the cloud-provider networking layer with minimal configuration changes.



## 7. Diagnosis Scenarios (Cookbook)

This section demonstrates how to leverage the correlation between **Metrics**, **Traces**, and **Logs** to diagnose complex issues in OpenKruiseGame. We use real-world examples to show how to move from a high-level alert to the root cause.

### Scenario 1: Debugging Network Plugin "Not Ready" Errors
**Problem:** You observe a spike in the **Network Error Rate** dashboard, or users report that GameServers are taking longer than expected to reach the `Ready` state.

**Investigation Steps:**

1.  **Identify the Spike:**
    Open the Grafana Dashboard. Locate the "Network Operation Status" or "Error Rate" panel. You notice a cluster of errors associated with the `kubernetes-hostport` plugin.
    > **[Screenshot Placeholder]**
    > *Action:* Capture the Grafana panel showing a red line/spike in error rate.
    > *Highlight:* Hover over a data point to show the **Exemplar** (the small dot linking to a trace).

2.  **Drill Down via Exemplar:**
    Click on one of the **Exemplars** on the graph. This automatically opens the corresponding Trace ID in Tempo (or your tracing backend).
    > **[Screenshot Placeholder]**
    > *Action:* Capture the "Query with Tempo" button or the direct jump to the Trace view.

3.  **Analyze Span Attributes:**
    In the Trace view, identify the failed span (marked in red). In this example, the span `process hostport update` failed. Look at the **Attributes** panel on the right.
    *   `game.kruise.io.network.internal_ports`: `1` (Correct)
    *   `game.kruise.io.network.external_ports`: `1` (Correct)
    *   `game.kruise.io.k8s.pod_ip`: `""` (Empty String)
    > **[Screenshot Placeholder]**
    > *Action:* Capture the Tempo trace view, highlighting the Attributes section where `game.kruise.io.k8s.pod_ip` is empty but ports are correct.

4.  **Conclusion:**
    The trace reveals that the Webhook was triggered **after** the Pod was scheduled (NodeName exists) but **before** the CNI plugin assigned an IP address.
    *   **Verdict:** This is a transient issue caused by the timing difference between Kubelet updates and CNI operations. It will resolve automatically in the next retry.
    *   **Why OTel shines here:** Without these attributes, you might waste time checking port configurations or Node status. The trace immediately pinpointed the *missing dependency* (Pod IP).


### Scenario 2: Investigating Controller Race Conditions
**Problem:** You see intermittent error logs regarding "StatefulSet already exists" during GameServerSet scaling, but the system seems to recover eventually. You want to understand if this is a logic bug.

**Investigation Steps:**

1.  **Isolate the Trace:**
    Search for traces with `status=error` and `service.name=okg-controller-manager`. Open a trace that shows a short duration (e.g., < 20ms).
    > **[Screenshot Placeholder]**
    > *Action:* Capture a Trace timeline showing a very short, single red bar (fast fail).

2.  **Correlate with Events & Logs:**
    Click the failed span to expand details.
    *   **Span Events:** You see an exception event: `statefulsets... "default-gss" already exists`.
    *   **Logs:** Click the **"Logs for this span"** button. It jumps directly to the log line generated *during this specific request*.
    > **[Screenshot Placeholder]**
    > *Action:* Capture the detailed view showing the "Logs" tab or split screen with the error log: "failed to create Advanced StatefulSet".

3.  **Root Cause Analysis:**
    The log indicates the error occurred in the `initAsts` function.
    *   **Logic Trace:** The code only enters `initAsts` (Creation) if the previous `Get` call returned `NotFound`.
    *   **Conflict:** However, the Creation failed because the API Server reported it `AlreadyExists`.
    > **[Screenshot Placeholder]**
    > *Action:* (Optional) Capture the log line showing the specific code path (file/line number) provided by `otelzap`.

4.  **Conclusion:**
    This confirms a classic Kubernetes **Informer Cache Race Condition**. The local cache was stale (reporting "Not Found"), but the API Server had the latest state.
    *   **Verdict:** Benign noise. The controller's retry mechanism handles this gracefully.
    *   **Why OTel shines here:** The timeline view (19ms duration) visually confirms this is a **logic fast-fail**, ruling out network timeouts or slow API calls instantly.


### Scenario 3: Contextual Log Analysis (Bottom-Up)
**Problem:** You find a generic error log in your console or Loki (e.g., "failed to update workload") and need to know *which* GameServer caused it and *what* happened before.

**Investigation Steps:**

1.  **Locate the Log:**
    In Grafana Explore (Loki), expand the log line. Notice the `trace_id` and `span_id` fields automatically injected by the OKG instrumentation.
    > **[Screenshot Placeholder]**
    > *Action:* Capture a log line in Loki expanded to show the `trace_id` field.

2.  **Pivot to Trace:**
    Click the **TraceID** link (or "Derived Field" button). This opens the full transaction history.
    > **[Screenshot Placeholder]**
    > *Action:* Capture the "Split View" where the log is on one side and the trace has opened on the other.

3.  **Gain Context:**
    Now you can see the **parent span**.
    *   **Question:** Who triggered this update?
    *   **Answer:** Look at the `game.kruise.io.game_server_set.name` attribute in the parent span.
    *   **Question:** Did the previous steps (e.g., PodProbeMarker sync) succeed?
    *   **Answer:** Yes, you can see the `sync_podprobemarker.success` event in the timeline *before* the error occurred.


**Note:** The screenshots above demonstrate the standard workflow using Grafana, Tempo, and Loki, but the same principles apply to any OTel-compatible backend.
