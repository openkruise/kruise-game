package logging

import (
	"runtime"

	"go.uber.org/zap/zapcore"
)

// sourceCore is a zapcore.Core that injects the source function, file and line
// of the logging call into a nested "source" field.
type sourceCore struct {
	zapcore.Core
	callerSkip int
}

// NewSourceCore wraps the given core so that each log entry includes a
// "source" object with function, file and line information. callerSkip controls
// how many stack frames to skip when determining the call site.
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
	if pc, file, line, ok := runtime.Caller(c.callerSkip); ok {
		fn := runtime.FuncForPC(pc)
		fields = append(fields, zapcore.Field{
			Key:  "source",
			Type: zapcore.ObjectMarshalerType,
			Interface: sourceInfo{
				Function: fn.Name(),
				File:     file,
				Line:     line,
			},
		})
	}
	return c.Core.Write(entry, fields)
}

type sourceInfo struct {
	Function string
	File     string
	Line     int
}

func (s sourceInfo) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	enc.AddString("function", s.Function)
	enc.AddString("file", s.File)
	enc.AddInt("line", s.Line)
	return nil
}
