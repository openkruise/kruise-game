package logging

import (
	"path/filepath"
	"runtime"
	"strings"

	"go.uber.org/zap/zapcore"
)

// sourceCore is a zapcore.Core that injects callsite metadata into the log entry.
type sourceCore struct {
	zapcore.Core
	callerSkip int
}

// NewSourceCore wraps the given core so that each log entry includes structured
// callsite attributes (`code.*`) and, when compatibility is enabled, the legacy
// `source` object. callerSkip controls how many stack frames to skip when
// determining the call site.
// TODO: Add trace support back in the future.
func NewSourceCore(core zapcore.Core, callerSkip int) zapcore.Core {
	return &sourceCore{Core: core, callerSkip: callerSkip}
}

func (c *sourceCore) With(fields []zapcore.Field) zapcore.Core {
	return &sourceCore{Core: c.Core.With(fields), callerSkip: c.callerSkip}
}

func (c *sourceCore) Check(entry zapcore.Entry, checked *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(entry.Level) {
		return checked.AddCore(entry, c)
	}
	return checked
}

func (c *sourceCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	if frame, ok := c.lookupSource(entry); ok {
		fields = append(fields, zapcore.Field{
			Key:       "code",
			Type:      zapcore.ObjectMarshalerType,
			Interface: frame.asCode(),
		})
		entry.Caller = zapcore.NewEntryCaller(frame.PC, frame.File, frame.Line, true)
	}
	return c.Core.Write(entry, fields)
}

func (c *sourceCore) lookupSource(entry zapcore.Entry) (sourceFrame, bool) {
	// 1. Prefer the caller recorded by zap if it points outside logging wrappers.
	if entry.Caller.Defined && !isWrapperFrame(entry.Caller.File) {
		fn := runtime.FuncForPC(entry.Caller.PC)
		name := ""
		if fn != nil {
			name = fn.Name()
		}
		return sourceFrame{
			Function: name,
			File:     entry.Caller.File,
			Line:     entry.Caller.Line,
			PC:       entry.Caller.PC,
		}, true
	}

	// 2. Walk the runtime stack to find the first frame outside known wrappers.
	const extraSkip = 2 // skip Write and runtime.Callers itself
	pcs := make([]uintptr, 32)
	n := runtime.Callers(c.callerSkip+extraSkip, pcs)
	if n == 0 {
		return sourceFrame{}, false
	}

	frames := runtime.CallersFrames(pcs[:n])
	for {
		frame, more := frames.Next()
		if frame.Function == "" && frame.File == "" {
			if !more {
				break
			}
			continue
		}
		if !isWrapperFrame(frame.File) && !isRuntimeFrame(frame.Function) {
			return sourceFrame{
				Function: frame.Function,
				File:     frame.File,
				Line:     frame.Line,
				PC:       frame.PC,
			}, true
		}
		if !more {
			break
		}
	}

	return sourceFrame{}, false
}

func isWrapperFrame(file string) bool {
	path := normalizeFramePath(file)
	if path == "" {
		return false
	}

	// Exclude test files from being treated as wrappers
	if strings.HasSuffix(path, "_test.go") {
		return false
	}

	wrapperSubstrings := []string{
		"/k8s.io/klog/",
		"/github.com/go-logr/",
		"/go.uber.org/zap/",
		"/sigs.k8s.io/controller-runtime/pkg/log/",
		"/github.com/openkruise/kruise-game/pkg/logging/",
	}
	for _, sub := range wrapperSubstrings {
		if strings.Contains(path, sub) {
			return true
		}
	}
	return false
}

func isRuntimeFrame(function string) bool {
	return strings.HasPrefix(function, "runtime.") ||
		strings.HasPrefix(function, "testing.")
}

func normalizeFramePath(file string) string {
	if file == "" {
		return ""
	}
	normalized := filepath.ToSlash(file)
	segments := strings.Split(normalized, "/")
	for i, segment := range segments {
		if idx := strings.Index(segment, "@"); idx != -1 {
			segments[i] = segment[:idx]
		}
	}
	return strings.Join(segments, "/")
}

type sourceFrame struct {
	Function string
	File     string
	Line     int
	PC       uintptr
}

func (f sourceFrame) asCode() codeAttributes {
	return codeAttributes{
		Function: f.Function,
		File:     f.File,
		Line:     f.Line,
	}
}

type codeAttributes struct {
	Function string
	File     string
	Line     int
}

func (c codeAttributes) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	enc.AddString("function", c.Function)
	enc.AddString("filepath", c.File)
	enc.AddInt("lineno", c.Line)
	return nil
}
