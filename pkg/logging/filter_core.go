package logging

import (
	gozapcore "go.uber.org/zap/zapcore"
)

// filterCore wraps a zapcore.Core and filters out specific field keys.
// This is used to prevent "context" fields from polluting console/JSON output
// while still allowing otelzap bridge core to access them.
type filterCore struct {
	gozapcore.Core
	keysToFilter map[string]bool
}

// newFilterCore creates a new filterCore that wraps the given core and filters specified keys.
func newFilterCore(core gozapcore.Core, keysToFilter map[string]bool) gozapcore.Core {
	return &filterCore{
		Core:         core,
		keysToFilter: keysToFilter,
	}
}

// With filters out specified keys before passing fields to the wrapped core.
func (c *filterCore) With(fields []gozapcore.Field) gozapcore.Core {
	filtered := make([]gozapcore.Field, 0, len(fields))
	for _, f := range fields {
		if !c.keysToFilter[f.Key] {
			filtered = append(filtered, f)
		}
	}
	return &filterCore{
		Core:         c.Core.With(filtered),
		keysToFilter: c.keysToFilter,
	}
}

// Check ensures the wrapped core is included in the checked entry.
func (c *filterCore) Check(e gozapcore.Entry, ce *gozapcore.CheckedEntry) *gozapcore.CheckedEntry {
	if c.Enabled(e.Level) {
		return ce.AddCore(e, c)
	}
	return ce
}
