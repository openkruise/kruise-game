# Logging

The manager supports two log formats controlled by `--log-format` (or `--zap-encoder`):

- `console` – human readable output.
- `json` – structured output for log collectors.

When `json` is selected, a custom encoder aligned with Kibana expectations is used. It produces single-line JSON objects with the following fields:

| Field | Description |
|-------|-------------|
| `time` | Timestamp encoded in RFC3339Nano |
| `level` | Upper-case log level (e.g. `INFO`, `ERROR`) |
| `msg` | Log message |
| `caller` | Source file and line of the log call |
| `stacktrace` | Included for error logs when available |

Example:

```json
{"time":"2024-05-29T12:00:00.123456789Z","level":"INFO","msg":"starting","caller":"main.go:42"}
```

`--log-format` takes precedence over `--zap-encoder` when both are set. If the values differ, `--log-format` wins and a warning is printed to stderr.

When `json` is selected, klog and the standard library `log` package are redirected to the same zap logger so all output shares the same single-line JSON structure and fields.

## Trace integration

Use `logging.WithContext(ctx, logger)` to enrich logs with trace information. The helper extracts the OpenTelemetry span context from `ctx` and injects two fields:

- `traceid` – hexadecimal trace identifier
- `sampled` – whether the span was sampled

Any logger created through controller-runtime, klog, or the standard library can include these fields when wrapped via `WithContext`.

## Source field

JSON logs include a nested `source` object with `function`, `file`, and `line` identifying the origin of the log call. The legacy `caller` field remains for compatibility, but `source` offers a structured alternative. All loggers redirected through the controller-runtime setup (including klog and `log`) emit this field.
