# Logging

The manager supports two log formats controlled by `--log-format` (or `--zap-encoder`):

- `console` – human readable output.
- `json` – structured output for log collectors.

When `json` is selected, the field layout is determined by `--log-json-preset`:

- `kibana` *(default)* – preserves the legacy Kibana-friendly field names.
- `otel` – aligns with the OpenTelemetry log data model while keeping the Kibana-compatible fields for backward compatibility.

Regardless of preset, all JSON logs are single-line objects and share the following important keys:

| Field | Description |
|-------|-------------|
| `time` | RFC3339Nano timestamp |
| `level` | Uppercase log level (compatible with Kibana dashboards) |
| `severity_number` | Numeric mapping following the OpenTelemetry recommendation |
| `msg` | Log message |
| `caller` | Legacy callsite (short filepath:line) |
| `code.function/filepath/lineno` | Structured callsite attributes aligned with OpenTelemetry |
| `stacktrace` | Included for error logs when available |
| `service.*`, `k8s.*` | Resource metadata populated from Downward API environment variables (service name/version/instance, namespace, pod name) |

Example (`--log-json-preset=kibana`):

```json
{
  "time":"2024-05-29T12:00:00.123456789Z",
  "severity_number":9,
  "level":"INFO",
  "msg":"starting kruise-game-manager",
  "caller":"main.go:312",
  "code":{"function":"main.main","filepath":"/workspace/main.go","lineno":312},
  "service.name":"okg-controller-manager",
  "service.version":"v0.5.0-abc1234",
  "service.instance.id":"359f38b1-...",
  "k8s.namespace.name":"kruise-game-system",
  "k8s.pod.name":"kruise-game-controller-manager-..."
}
```

Example (`--log-json-preset=otel`):

```json
{
  "time":"2024-05-29T12:00:00.123456789Z",
  "level":"INFO",
  "severity_number":9,
  "msg":"starting kruise-game-manager",
  "caller":"main.go:312",
  "code":{"function":"main.main","filepath":"/workspace/main.go","lineno":312},
  "service.name":"okg-controller-manager",
  "service.version":"v0.5.0-abc1234",
  "service.instance.id":"359f38b1-...",
  "k8s.namespace.name":"kruise-game-system",
  "k8s.pod.name":"kruise-game-controller-manager-..."
}
```

`--log-format` takes precedence over `--zap-encoder`. When both are provided, `--log-format` wins and the manager emits an `INFO` log noting the override.

When `json` is selected, klog and the standard library `log` package are redirected to the same zap backend so all output shares the structured field set.

## Running with JSON logging

To enable JSON logging with the default (Kibana) preset:

```bash
./bin/manager --log-format=json
```

To switch to the OpenTelemetry preset:

```bash
./bin/manager --log-format=json --log-json-preset=otel
```

### Verifying the output

Use `jq` or any JSON parser to check that each line is valid JSON:

```bash
./bin/manager --log-format=json | jq
```

### Kibana usage

The Kibana preset keeps `time` and `level` exactly as Kibana dashboards expect. The OTel preset currently emits the same field set so collectors can map `time` → timestamp and `severity_number`/`level` → severity without additional data shims. In future releases the presets may diverge, but the base contract (`time`, `level`, `severity_number`, `msg`, `code.*`, `service.*`, `k8s.*`) will remain stable.

## Fallback to console

To revert to console logging, remove the `--log-format=json` flag (or set it back to `console`). No redeployment is required.

## Programmatic bootstrap

Controllers or utilities inside this repo should rely on the shared logging bootstrap:

```go
logOpts := logging.NewOptions()
logOpts.AddFlags(flag.CommandLine)
flag.Parse()

result, err := logOpts.Apply(flag.CommandLine)
if err != nil {
    // write to stderr and exit
}
setupLog := ctrl.Log.WithName("setup")
if result.Warning != "" {
    setupLog.Info(result.Warning)
}
```

`logging.Options` takes care of configuring controller-runtime, klog, and the Go standard library loggers so that every component uses the same zap backend. The returned `Result` exposes the effective format/preset and any override warning, which you can surface through your own logger if needed.

## Service readiness summary

During startup the manager emits a single `event="service.ready"` JSON log summarising the effective configuration (metrics/health bind addresses, namespace scope, leader election, log format/preset, scale server bind address, etc.). This log is helpful for both Kibana dashboards and OpenTelemetry-based backends when verifying deployments.

## Resource metadata configuration

The structured logs automatically populate `service.*` and `k8s.*` attributes from environment variables. The controller Deployment should expose the following Downward API fields:

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

Optionally you can override or extend the defaults with the following variables:

| Variable | Purpose |
|----------|---------|
| `OKG_SERVICE_NAME` | Overrides `service.name` (defaults to `okg-controller-manager`) |
| `OKG_SERVICE_VERSION` / `OKG_VERSION` | Sets `service.version` |
| `OKG_SERVICE_INSTANCE_ID` | Explicit service instance identifier |
| `OKG_NAMESPACE`, `OKG_POD_NAME` | Override namespace/pod name detection |

Unset values are simply omitted from the log entry, so the manager works even if some variables are not provided.
The default manifest in `config/manager/manager.yaml` already sets these variables; the snippet above is provided for reference when integrating into other deployment workflows.

## Build metadata

`make build`, `make run`, and `make docker-build` automatically inject build metadata using
`git describe --tags --dirty --always`. The value is wired through `-ldflags` into
`pkg/version.Version`, which in turn feeds the `service.version` attribute. You can override it
manually when experimenting:

```bash
VERSION=1.2.0-alpha.1 make build
# or
go build -ldflags "-X github.com/openkruise/kruise-game/pkg/version.Version=debug" ./main.go
```

## FAQ

**What happens if I provide an unsupported log format?**  
The manager fails to start and prints an error message to stderr.

**What happens if stderr is not collected?**  
Warnings (such as `--log-format` overriding `--zap-encoder`) are emitted through the structured logger, so they still appear in the aggregated logs even if stderr is discarded.
