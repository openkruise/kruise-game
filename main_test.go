package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	zapr "github.com/go-logr/zapr"
	logging "github.com/openkruise/kruise-game/pkg/logging"
	"go.opentelemetry.io/otel/trace"
	gozap "go.uber.org/zap"
	gozapcore "go.uber.org/zap/zapcore"
	klog "k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func TestDefaultLogFormat(t *testing.T) {
	var buf bytes.Buffer
	opts := zap.Options{Development: true, DestWriter: &buf}
	opts.ZapOpts = append(opts.ZapOpts,
		gozap.AddCaller(),
		gozap.WrapCore(func(c gozapcore.Core) gozapcore.Core { return logging.NewSourceCore(c, 2) }),
	)
	if err := configureEncoder("console", false, "console", false, &opts); err != nil {
		t.Fatalf("configureEncoder returned error: %v", err)
	}
	logger := zap.New(zap.UseFlagOptions(&opts))
	logger.Info("hello")
	if err := json.Unmarshal(buf.Bytes(), &map[string]interface{}{}); err == nil {
		t.Fatalf("expected non-JSON log, got %s", buf.String())
	}
}

func TestKibanaJSONLogFormat(t *testing.T) {
	var buf bytes.Buffer
	opts := zap.Options{Development: true, DestWriter: &buf}
	if err := configureEncoder("json", true, "json", true, &opts); err != nil {
		t.Fatalf("configureEncoder returned error: %v", err)
	}
	opts.ZapOpts = append(opts.ZapOpts,
		gozap.AddCaller(),
		gozap.WrapCore(func(c gozapcore.Core) gozapcore.Core { return logging.NewSourceCore(c, 2) }),
	)
	logger := zap.New(zap.UseFlagOptions(&opts))
	logger.Info("hello")

	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	if len(lines) != 1 {
		t.Fatalf("expected single line JSON, got %d lines: %q", len(lines), buf.String())
	}
	var m map[string]interface{}
	if err := json.Unmarshal(lines[0], &m); err != nil {
		t.Fatalf("expected JSON log, got %v: %s", err, buf.String())
	}
	if _, ok := m["time"]; !ok {
		t.Fatalf("missing time field")
	}
	if _, err := time.Parse(time.RFC3339Nano, m["time"].(string)); err != nil {
		t.Fatalf("time not RFC3339Nano: %v", err)
	}
	if lvl, ok := m["level"].(string); !ok || lvl != strings.ToUpper(lvl) {
		t.Fatalf("level not uppercase: %v", m["level"])
	}
	if _, ok := m["msg"]; !ok {
		t.Fatalf("missing msg field")
	}
	src, ok := m["source"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing source field")
	}
	if fn, ok := src["function"].(string); !ok || fn == "" {
		t.Fatalf("invalid source.function: %v", src["function"])
	}
	if file, ok := src["file"].(string); !ok || !strings.HasSuffix(file, ".go") {
		t.Fatalf("invalid source.file: %v", src["file"])
	}
	if line, ok := src["line"].(float64); !ok || line <= 0 {
		t.Fatalf("invalid source.line: %v", src["line"])
	}
}

func TestInvalidLogFormat(t *testing.T) {
	var buf bytes.Buffer
	opts := zap.Options{Development: true, DestWriter: &buf}
	opts.ZapOpts = append(opts.ZapOpts,
		gozap.AddCaller(),
		gozap.WrapCore(func(c gozapcore.Core) gozapcore.Core { return logging.NewSourceCore(c, 2) }),
	)
	if err := configureEncoder("xml", true, "console", false, &opts); err == nil {
		t.Fatalf("expected error for unsupported log format")
	}
}

func TestLogFormatZapEncoderConflict(t *testing.T) {
	var buf bytes.Buffer
	opts := zap.Options{Development: true, DestWriter: &buf}
	r, w, _ := os.Pipe()
	oldStderr := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = oldStderr }()

	if err := configureEncoder("json", true, "console", true, &opts); err != nil {
		t.Fatalf("configureEncoder returned error: %v", err)
	}
	opts.ZapOpts = append(opts.ZapOpts,
		gozap.AddCaller(),
		gozap.WrapCore(func(c gozapcore.Core) gozapcore.Core { return logging.NewSourceCore(c, 2) }),
	)
	logger := zap.New(zap.UseFlagOptions(&opts))
	logger.Info("hello")

	w.Close()
	warningBytes, _ := io.ReadAll(r)
	if !strings.Contains(string(warningBytes), "overrides") {
		t.Fatalf("expected warning, got %q", string(warningBytes))
	}

	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &map[string]interface{}{}); err != nil {
		t.Fatalf("expected JSON log, got %v: %s", err, buf.String())
	}
}

func TestTraceFieldInjection(t *testing.T) {
	var buf bytes.Buffer
	opts := zap.Options{Development: true, DestWriter: &buf}
	if err := configureEncoder("json", true, "json", true, &opts); err != nil {
		t.Fatalf("configureEncoder returned error: %v", err)
	}
	opts.ZapOpts = append(opts.ZapOpts,
		gozap.AddCaller(),
		gozap.WrapCore(func(c gozapcore.Core) gozapcore.Core { return logging.NewSourceCore(c, 2) }),
	)
	logger := zap.New(zap.UseFlagOptions(&opts))

	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
		SpanID:     trace.SpanID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08},
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)
	logging.WithContext(ctx, logger).Info("hello")

	var m map[string]interface{}
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &m); err != nil {
		t.Fatalf("expected JSON log, got %v: %s", err, buf.String())
	}
	if got := m["traceid"]; got != sc.TraceID().String() {
		t.Fatalf("traceid mismatch: %v", got)
	}
	if sampled, ok := m["sampled"].(bool); !ok || !sampled {
		t.Fatalf("sampled field missing or false: %v", m["sampled"])
	}
	if _, ok := m["source"]; !ok {
		t.Fatalf("missing source field")
	}
}

func TestKlogRedirectsToZap(t *testing.T) {
	var buf bytes.Buffer
	opts := zap.Options{Development: true, DestWriter: &buf}
	if err := configureEncoder("json", true, "json", true, &opts); err != nil {
		t.Fatalf("configureEncoder returned error: %v", err)
	}
	opts.ZapOpts = append(opts.ZapOpts,
		gozap.AddCaller(),
		gozap.WrapCore(func(c gozapcore.Core) gozapcore.Core { return logging.NewSourceCore(c, 2) }),
	)
	logger := zap.New(zap.UseFlagOptions(&opts))
	klog.SetLogger(logger)
	klog.InfoS("hello")

	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	if len(lines) != 1 {
		t.Fatalf("expected single line JSON, got %d lines: %q", len(lines), buf.String())
	}
	var m map[string]interface{}
	if err := json.Unmarshal(lines[0], &m); err != nil {
		t.Fatalf("expected JSON log, got %v: %s", err, buf.String())
	}
	if _, ok := m["source"]; !ok {
		t.Fatalf("missing source field")
	}
}

func TestStdLogRedirectsToZap(t *testing.T) {
	var buf bytes.Buffer
	opts := zap.Options{Development: true, DestWriter: &buf}
	if err := configureEncoder("json", true, "json", true, &opts); err != nil {
		t.Fatalf("configureEncoder returned error: %v", err)
	}
	opts.ZapOpts = append(opts.ZapOpts,
		gozap.AddCaller(),
		gozap.WrapCore(func(c gozapcore.Core) gozapcore.Core { return logging.NewSourceCore(c, 2) }),
	)
	logger := zap.New(zap.UseFlagOptions(&opts))
	if zl, ok := logger.GetSink().(zapr.Underlier); ok {
		if u := zl.GetUnderlying(); u != nil {
			_ = gozap.ReplaceGlobals(u)
			_ = gozap.RedirectStdLog(u)
		}
	}
	log.Println("hello")

	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	if len(lines) != 1 {
		t.Fatalf("expected single line JSON, got %d lines: %q", len(lines), buf.String())
	}
	var m map[string]interface{}
	if err := json.Unmarshal(lines[0], &m); err != nil {
		t.Fatalf("expected JSON log, got %v: %s", err, buf.String())
	}
	if _, ok := m["source"]; !ok {
		t.Fatalf("missing source field")
	}
}
