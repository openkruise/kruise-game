package logging

import (
	"go.uber.org/zap/zapcore"
)

// WrapCore applies standard logging augmentations before delegating to the base core.
func WrapCore(core zapcore.Core, callerSkip int) zapcore.Core {

	if isActiveJSON() {
		opts := augmentOptions{
			SeverityNumberKey: "severity_number",
		}

		core = newAugmentCore(core, opts)
	}

	core = NewSourceCore(core, callerSkip)
	return core
}
