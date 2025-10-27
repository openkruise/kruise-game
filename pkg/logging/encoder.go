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
	// keep caller field name for compatibility with existing tooling
	enc.StacktraceKey = "stacktrace"
	enc.EncodeCaller = zapcore.ShortCallerEncoder
	return zapcore.NewJSONEncoder(enc)
}

// NewOTelJSONEncoder returns an encoder aligned with the OpenTelemetry log data model.
func NewOTelJSONEncoder() zapcore.Encoder {
	// We currently emit the same JSON layout as the Kibana preset but keep
	// the factory split so future adjustments can diverge without touching
	// callers.
	return NewKibanaJSONEncoder()
}
