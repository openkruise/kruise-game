package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"log"
	"strings"
	"testing"
	"time"

	zapr "github.com/go-logr/zapr"
	"github.com/openkruise/kruise-game/pkg/logging"
	"github.com/openkruise/kruise-game/pkg/tracing"
	gozap "go.uber.org/zap"
	gozapcore "go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func newTestOptions(t *testing.T, buf *bytes.Buffer, args ...string) (*logging.Options, logging.Result) {
	t.Helper()

	opts := logging.NewOptions()
	if buf != nil {
		opts.ZapOptions.DestWriter = buf
	}

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	opts.AddFlags(fs)
	if err := fs.Parse(args); err != nil {
		t.Fatalf("parse flags: %v", err)
	}

	result, err := opts.Apply(fs)
	if err != nil {
		t.Fatalf("apply logging options: %v", err)
	}

	return opts, result
}

func TestDefaultLogFormat(t *testing.T) {
	var buf bytes.Buffer
	opts, _ := newTestOptions(t, &buf)
	logger := zap.New(zap.UseFlagOptions(&opts.ZapOptions))
	if kv := logging.ResourceKeyValues(); len(kv) > 0 {
		logger = logger.WithValues(kv...)
	}
	buf.Reset()
	logger.Info("hello")
	if err := json.Unmarshal(buf.Bytes(), &map[string]interface{}{}); err == nil {
		t.Fatalf("expected non-JSON log, got %s", buf.String())
	}
}

func TestJSONLogFormat(t *testing.T) {
	var buf bytes.Buffer
	opts, _ := newTestOptions(t, &buf, "--log-format=json")
	logger := zap.New(zap.UseFlagOptions(&opts.ZapOptions))
	if kv := logging.ResourceKeyValues(); len(kv) > 0 {
		logger = logger.WithValues(kv...)
	}
	buf.Reset()
	logger.Info("hello")

	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	if len(lines) != 1 {
		t.Fatalf("expected single line JSON, got %d lines: %q", len(lines), buf.String())
	}
	var m map[string]interface{}
	if err := json.Unmarshal(lines[0], &m); err != nil {
		t.Fatalf("expected JSON log, got %v: %s", err, buf.String())
	}
	timeValue, ok := m["time"].(string)
	if !ok {
		t.Fatalf("missing time field: %v", m)
	}
	if _, err := time.Parse(time.RFC3339Nano, timeValue); err != nil {
		t.Fatalf("time not RFC3339Nano: %v", err)
	}
	if _, ok := m["ts"]; ok {
		t.Fatalf("unexpected ts field present: %v", m["ts"])
	}
	if _, ok := m["severity"]; ok {
		t.Fatalf("unexpected severity field present: %v", m["severity"])
	}
	if lvl, ok := m["level"].(string); !ok || lvl != strings.ToUpper(lvl) {
		t.Fatalf("level not uppercase: %v", m["level"])
	}
	if num, ok := m["severity_number"].(float64); !ok || int(num) != 9 {
		t.Fatalf("unexpected severity_number: %v", m["severity_number"])
	}
	if _, ok := m["msg"]; !ok {
		t.Fatalf("missing msg field")
	}
	code, ok := m["code"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing code field")
	}
	if fn, ok := code["function"].(string); !ok || fn == "" {
		t.Fatalf("invalid code.function: %v", code["function"])
	}
	if file, ok := code["filepath"].(string); !ok || !strings.HasSuffix(file, ".go") {
		t.Fatalf("invalid code.filepath: %v", code["filepath"])
	}
	if line, ok := code["lineno"].(float64); !ok || line <= 0 {
		t.Fatalf("invalid code.lineno: %v", code["lineno"])
	}
}

func TestInvalidLogFormat(t *testing.T) {
	opts := logging.NewOptions()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	opts.AddFlags(fs)
	if err := fs.Parse([]string{"--log-format=xml"}); err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	if _, err := opts.Apply(fs); err == nil {
		t.Fatalf("expected error for unsupported log format")
	}
}

func TestLogFormatZapEncoderConflict(t *testing.T) {
	var buf bytes.Buffer
	opts, result := newTestOptions(t, &buf, "--log-format=json", "--zap-encoder=console")

	if !strings.Contains(result.Warning, "overrides") {
		t.Fatalf("expected warning, got %q", result.Warning)
	}

	logger := zap.New(zap.UseFlagOptions(&opts.ZapOptions))
	if kv := logging.ResourceKeyValues(); len(kv) > 0 {
		logger = logger.WithValues(kv...)
	}
	buf.Reset()
	logger.Info("hello")

	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &map[string]interface{}{}); err != nil {
		t.Fatalf("expected JSON log, got %v: %s", err, buf.String())
	}
}

func TestDefaultIsConsole(t *testing.T) {
	var buf bytes.Buffer
	opts, result := newTestOptions(t, &buf)
	if result.Format != "console" {
		t.Fatalf("expected console format, got %s", result.Format)
	}
	logger := zap.New(zap.UseFlagOptions(&opts.ZapOptions))
	if kv := logging.ResourceKeyValues(); len(kv) > 0 {
		logger = logger.WithValues(kv...)
	}
	buf.Reset()
	logger.Info("hello")
	if strings.Contains(buf.String(), "\"level\":\"info\"") {
		t.Fatalf("expected console log, got JSON log: %s", buf.String())
	}
}

func TestStdLogRedirectsToZap(t *testing.T) {
	var buf bytes.Buffer
	opts, _ := newTestOptions(t, &buf, "--log-format=json")
	logger := zap.New(zap.UseFlagOptions(&opts.ZapOptions))
	if zl, ok := logger.GetSink().(zapr.Underlier); ok {
		if u := zl.GetUnderlying(); u != nil {
			_ = gozap.ReplaceGlobals(u)
			_ = gozap.RedirectStdLog(u)
		}
	}
	buf.Reset()
	log.Println("hello")

	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	if len(lines) != 1 {
		t.Fatalf("expected single line JSON, got %d lines: %q", len(lines), buf.String())
	}
	var m map[string]interface{}
	if err := json.Unmarshal(lines[0], &m); err != nil {
		t.Fatalf("expected JSON log, got %v: %s", err, buf.String())
	}
	if _, ok := m["code"]; !ok {
		t.Fatalf("missing code field")
	}
}

func TestLogServiceReadySummary(t *testing.T) {
	logging.SetJSONConfig(logging.JSONConfig{})
	core, logs := observer.New(gozapcore.InfoLevel)
	zapLogger := gozap.New(core)
	logger := zapr.NewLogger(zapLogger)

	summary := serviceSummary{
		MetricsAddr:     ":8080",
		HealthAddr:      ":8082",
		Namespace:       "kruise-game-system",
		SyncPeriodRaw:   "5m",
		LeaderElection:  true,
		LogFormat:       "json",
		LogJSONPreset:   logging.JSONPresetOTel,
		ScaleServerAddr: ":6000",
	}

	logServiceReadySummary(logger, summary)

	entries := logs.All()
	if len(entries) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(entries))
	}

	entry := entries[0]
	if entry.Message != "service configuration snapshot" {
		t.Fatalf("unexpected message %q", entry.Message)
	}

	ctx := entry.ContextMap()
	if ctx[tracing.FieldEvent] != "service.ready" {
		t.Fatalf("expected event field, got %v", ctx[tracing.FieldEvent])
	}
	if ctx["leader_election"] != true {
		t.Fatalf("expected leader_election=true, got %v", ctx["leader_election"])
	}
	expected := map[string]string{
		"metrics.bind_address":      ":8080",
		"healthz.bind_address":      ":8082",
		"namespace_scope":           "kruise-game-system",
		"sync_period":               "5m",
		"log.format":                "json",
		"log.json_preset":           string(logging.JSONPresetOTel),
		"scale_server.bind_address": ":6000",
	}
	for key, want := range expected {
		if got, ok := ctx[key]; !ok || got != want {
			t.Fatalf("field %s mismatch: got %v want %v", key, got, want)
		}
	}
}

func TestJSONLogFormatOTelPreset(t *testing.T) {
	var buf bytes.Buffer
	opts, result := newTestOptions(t, &buf, "--log-format=json", "--log-json-preset=otel")
	if result.JSONPreset != logging.JSONPresetOTel {
		t.Fatalf("expected otel preset, got %s", result.JSONPreset)
	}
	logger := zap.New(zap.UseFlagOptions(&opts.ZapOptions))
	if kv := logging.ResourceKeyValues(); len(kv) > 0 {
		logger = logger.WithValues(kv...)
	}
	buf.Reset()
	logger.Info("hello")

	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	if len(lines) != 1 {
		t.Fatalf("expected single line JSON, got %d lines: %q", len(lines), buf.String())
	}
	var m map[string]interface{}
	if err := json.Unmarshal(lines[0], &m); err != nil {
		t.Fatalf("expected JSON log, got %v: %s", err, buf.String())
	}
	timeValue, ok := m["time"].(string)
	if !ok {
		t.Fatalf("missing time field: %v", m)
	}
	if _, err := time.Parse(time.RFC3339Nano, timeValue); err != nil {
		t.Fatalf("time not RFC3339Nano: %v", err)
	}
	if _, ok := m["ts"]; ok {
		t.Fatalf("did not expect ts field in OTel preset")
	}
	if _, ok := m["severity"]; ok {
		t.Fatalf("did not expect severity field in OTel preset")
	}
	if lvl, ok := m["level"].(string); !ok || lvl != strings.ToUpper(lvl) {
		t.Fatalf("level not uppercase: %v", m["level"])
	}
	if num, ok := m["severity_number"].(float64); !ok || int(num) != 9 {
		t.Fatalf("unexpected severity_number: %v", m["severity_number"])
	}
	if serviceName, ok := m["service.name"].(string); !ok || serviceName == "" {
		t.Fatalf("resource fields missing, got %v", m)
	}
}
