package logging

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// NewKibanaJSONEncoder returns a zapcore.Encoder configured for Kibana consumption.
// It uses production defaults and adjusts field names and encoders to match
// Kibana's expectations: time in RFC3339Nano format, upper-case levels and
// consistent key names.
func NewKibanaJSONEncoder() zapcore.Encoder {
	enc := zap.NewProductionEncoderConfig()
	enc.TimeKey = "time"
	enc.EncodeTime = zapcore.RFC3339NanoTimeEncoder
	enc.LevelKey = "level"
	enc.EncodeLevel = zapcore.CapitalLevelEncoder
	enc.MessageKey = "msg"
	// keep caller field name; alternatively could be renamed to "source"
	enc.StacktraceKey = "stacktrace"
	enc.EncodeCaller = zapcore.ShortCallerEncoder
	return zapcore.NewJSONEncoder(enc)
}
