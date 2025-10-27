package logging

import "go.uber.org/zap/zapcore"

type augmentOptions struct {
	SeverityNumberKey string
}

type augmentCore struct {
	zapcore.Core
	opts augmentOptions
}

func newAugmentCore(core zapcore.Core, opts augmentOptions) zapcore.Core {
	if opts.SeverityNumberKey == "" {
		return core
	}
	return &augmentCore{
		Core: core,
		opts: opts,
	}
}

func (c *augmentCore) With(fields []zapcore.Field) zapcore.Core {
	return &augmentCore{
		Core: c.Core.With(fields),
		opts: c.opts,
	}
}

func (c *augmentCore) Check(entry zapcore.Entry, checked *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(entry.Level) {
		return checked.AddCore(entry, c)
	}
	return checked
}

func (c *augmentCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	if c.opts.SeverityNumberKey != "" {
		fields = append(fields, zapcore.Field{
			Key:     c.opts.SeverityNumberKey,
			Type:    zapcore.Int64Type,
			Integer: int64(mapSeverityNumber(entry.Level)),
		})
	}

	return c.Core.Write(entry, fields)
}

func mapSeverityNumber(level zapcore.Level) int {
	switch level {
	case zapcore.DebugLevel:
		return 5
	case zapcore.InfoLevel:
		return 9
	case zapcore.WarnLevel:
		return 13
	case zapcore.ErrorLevel:
		return 17
	case zapcore.DPanicLevel, zapcore.PanicLevel, zapcore.FatalLevel:
		return 21
	default:
		return 0
	}
}
