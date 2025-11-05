package logging

import (
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestSourceCoreAddsSourceField(t *testing.T) {
	obsCore, logs := observer.New(zapcore.DebugLevel)
	core := NewSourceCore(obsCore, 1)
	logger := zap.New(core)

	logger.Info("hello world")

	entries := logs.All()
	if len(entries) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(entries))
	}

	entry := entries[0]
	if entry.Message != "hello world" {
		t.Fatalf("unexpected message %q", entry.Message)
	}

	if !entry.Caller.Defined {
		t.Fatalf("expected caller to be defined")
	}
	if !strings.HasSuffix(entry.Caller.File, "sourcecore_test.go") {
		t.Fatalf("unexpected caller file %q", entry.Caller.File)
	}
	if entry.Caller.Line <= 0 {
		t.Fatalf("expected caller line to be positive, got %d", entry.Caller.Line)
	}

	codeVal, ok := entry.ContextMap()["code"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected code field to be present, context: %+v", entry.ContextMap())
	}

	// Accept any non-empty values; exact caller/file depends on zap internals.
	function, _ := codeVal["function"].(string)
	file, _ := codeVal["filepath"].(string)
	// line may be encoded as float64 in observer
	lineAny := codeVal["lineno"]

	if function == "" {
		t.Fatalf("expected function value to be non-empty")
	}
	if !strings.HasSuffix(file, ".go") {
		t.Fatalf("unexpected file value %q", file)
	}
	switch v := lineAny.(type) {
	case float64:
		if v <= 0 {
			t.Fatalf("expected positive line number, got %v", v)
		}
	case int:
		if v <= 0 {
			t.Fatalf("expected positive line number, got %v", v)
		}
	default:
		t.Fatalf("unexpected line type %T", v)
	}

}

func TestSourceCoreWithPreservesFields(t *testing.T) {
	obsCore, logs := observer.New(zapcore.DebugLevel)
	core := NewSourceCore(obsCore, 1)

	logger := zap.New(core).With(zap.String("component", "test"))
	logger.Info("message", zap.Int("value", 42))

	entries := logs.All()
	if len(entries) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(entries))
	}

	entry := entries[0]
	ctx := entry.ContextMap()

	if ctx["component"] != "test" {
		t.Fatalf("expected component field, context: %+v", ctx)
	}
	// Numeric value can be encoded as int or float64 depending on core
	switch v := ctx["value"].(type) {
	case float64:
		if v != 42 {
			t.Fatalf("expected value field to be 42, context: %+v", ctx)
		}
	case int:
		if v != 42 {
			t.Fatalf("expected value field to be 42, context: %+v", ctx)
		}
	case int64:
		if v != 42 {
			t.Fatalf("expected value field to be 42, context: %+v", ctx)
		}
	default:
		t.Fatalf("unexpected value type %T", ctx["value"])
	}

	if _, ok := ctx["code"]; !ok {
		t.Fatalf("expected code field to be present, context: %+v", ctx)
	}
}

func TestLookupSourceFallsBackFromWrapper(t *testing.T) {
	sc := &sourceCore{callerSkip: 0}

	// entry with no caller should fallback to runtime stack and skip logging wrappers
	frame, ok := sc.lookupSource(zapcore.Entry{})
	if !ok {
		t.Fatalf("expected lookupSource to return info")
	}
	if !strings.HasSuffix(frame.File, "_test.go") {
		t.Fatalf("expected fallback file to be test file, got %s", frame.File)
	}
	if frame.Function == "" {
		t.Fatalf("expected function name to be set")
	}

	// entry caller pointing to wrapper should also fallback
	entry := zapcore.Entry{
		Caller: zapcore.EntryCaller{
			Defined: true,
			File:    "/go/pkg/mod/k8s.io/klog/v2@v2.120.1/klog.go",
			Line:    123,
			PC:      0,
		},
	}
	frame, ok = sc.lookupSource(entry)
	if !ok {
		t.Fatalf("expected lookupSource to return info for wrapper caller")
	}
	if strings.Contains(frame.File, "k8s.io/klog") {
		t.Fatalf("expected wrapper frames to be skipped, got %s", frame.File)
	}
}

func TestLookupSourceUsesEntryCallerWhenNotWrapper(t *testing.T) {
	sc := &sourceCore{callerSkip: 0}

	entry := zapcore.Entry{
		Caller: zapcore.EntryCaller{
			Defined: true,
			File:    "/workspace/main.go",
			Line:    42,
			PC:      0,
		},
	}
	frame, ok := sc.lookupSource(entry)
	if !ok {
		t.Fatalf("expected lookupSource to use provided caller")
	}
	if frame.File != "/workspace/main.go" || frame.Line != 42 {
		t.Fatalf("expected caller info to be used, got %+v", frame)
	}
}

func TestNormalizeFramePathStripsModuleVersion(t *testing.T) {
	path := "/go/pkg/mod/sigs.k8s.io/controller-runtime@v0.18.6/pkg/log/deleg.go"
	normalized := normalizeFramePath(path)
	if strings.Contains(normalized, "@") {
		t.Fatalf("expected module version to be stripped, got %s", normalized)
	}
	substr := "/sigs.k8s.io/controller-runtime/pkg/log/deleg.go"
	if !strings.HasSuffix(normalized, substr) {
		t.Fatalf("expected normalized path to end with %s, got %s", substr, normalized)
	}
}

func TestIsWrapperFrameWithModuleVersion(t *testing.T) {
	path := "/go/pkg/mod/k8s.io/klog/v2@v2.120.1/klog.go"
	if !isWrapperFrame(path) {
		t.Fatalf("expected wrapper frame to be detected for %s", path)
	}

	businessPath := "/workspace/pkg/controllers/gameserver/controller.go"
	if isWrapperFrame(businessPath) {
		t.Fatalf("did not expect business path to be treated as wrapper")
	}
}

func TestSourceCoreSkipsRuntimeFrames(t *testing.T) {
	obsCore, logs := observer.New(zapcore.DebugLevel)
	core := NewSourceCore(obsCore, 1)
	logger := zap.New(core)

	done := make(chan struct{})
	go func() {
		logger.Info("from goroutine")
		close(done)
	}()
	<-done

	var found bool
	for _, entry := range logs.All() {
		if entry.Message != "from goroutine" {
			continue
		}
		found = true
		if !entry.Caller.Defined {
			t.Fatalf("expected caller to be defined")
		}
		if strings.Contains(entry.Caller.File, "/runtime/") {
			t.Fatalf("expected runtime frames to be skipped, got caller %s", entry.Caller.File)
		}

		codeVal, ok := entry.ContextMap()["code"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected code field to be present")
		}
		file, _ := codeVal["filepath"].(string)
		if strings.Contains(file, "/runtime/") {
			t.Fatalf("expected runtime frames to be skipped in code, got %s", file)
		}

	}

	if !found {
		t.Fatalf("expected goroutine log entry to be captured")
	}
}
