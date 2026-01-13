package logging

import (
	"bytes"
	"testing"

	gozap "go.uber.org/zap"
	gozapcore "go.uber.org/zap/zapcore"
)

func TestFilterCore_With(t *testing.T) {
	tests := []struct {
		name           string
		keysToFilter   map[string]bool
		inputFields    []gozapcore.Field
		expectedKeys   []string
		unexpectedKeys []string
	}{
		{
			name:         "filters context field",
			keysToFilter: map[string]bool{"context": true},
			inputFields: []gozapcore.Field{
				gozap.String("trace_id", "abc123"),
				gozap.String("context", "should_be_filtered"),
				gozap.String("span_id", "def456"),
			},
			expectedKeys:   []string{"trace_id", "span_id"},
			unexpectedKeys: []string{"context"},
		},
		{
			name:         "filters multiple fields",
			keysToFilter: map[string]bool{"context": true, "sensitive": true},
			inputFields: []gozapcore.Field{
				gozap.String("trace_id", "abc123"),
				gozap.String("context", "filtered"),
				gozap.String("sensitive", "filtered"),
				gozap.String("span_id", "def456"),
			},
			expectedKeys:   []string{"trace_id", "span_id"},
			unexpectedKeys: []string{"context", "sensitive"},
		},
		{
			name:         "allows all fields when filter is empty",
			keysToFilter: map[string]bool{},
			inputFields: []gozapcore.Field{
				gozap.String("trace_id", "abc123"),
				gozap.String("context", "allowed"),
				gozap.String("span_id", "def456"),
			},
			expectedKeys:   []string{"trace_id", "context", "span_id"},
			unexpectedKeys: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a buffer to capture output
			var buf bytes.Buffer
			encoder := gozapcore.NewJSONEncoder(gozapcore.EncoderConfig{
				MessageKey: "msg",
				LevelKey:   "level",
				TimeKey:    "time",
				EncodeTime: gozapcore.ISO8601TimeEncoder,
			})
			baseCore := gozapcore.NewCore(encoder, gozapcore.AddSync(&buf), gozapcore.DebugLevel)

			// Create filterCore
			filterCore := newFilterCore(baseCore, tt.keysToFilter)

			// Apply With() to add fields
			coreWithFields := filterCore.With(tt.inputFields)

			// Create a logger with the core and log a message
			logger := gozap.New(coreWithFields)
			logger.Info("test message")

			output := buf.String()

			// Check expected keys are present
			for _, key := range tt.expectedKeys {
				if !bytes.Contains(buf.Bytes(), []byte(`"`+key+`"`)) {
					t.Errorf("Expected key %q not found in output: %s", key, output)
				}
			}

			// Check unexpected keys are absent
			for _, key := range tt.unexpectedKeys {
				if bytes.Contains(buf.Bytes(), []byte(`"`+key+`"`)) {
					t.Errorf("Unexpected key %q found in output: %s", key, output)
				}
			}
		})
	}
}

func TestFilterCore_Check(t *testing.T) {
	var buf bytes.Buffer
	encoder := gozapcore.NewJSONEncoder(gozapcore.EncoderConfig{
		MessageKey: "msg",
	})
	baseCore := gozapcore.NewCore(encoder, gozapcore.AddSync(&buf), gozapcore.InfoLevel)

	filterCore := newFilterCore(baseCore, map[string]bool{"context": true})

	// Test that Check returns the core for enabled levels
	entry := gozapcore.Entry{Level: gozapcore.InfoLevel}
	ce := filterCore.Check(entry, nil)
	if ce == nil {
		t.Error("Expected Check to return non-nil CheckedEntry for enabled level")
	}

	// Test that Check returns nil for disabled levels
	entry = gozapcore.Entry{Level: gozapcore.DebugLevel}
	ce = filterCore.Check(entry, nil)
	if ce != nil {
		t.Error("Expected Check to return nil CheckedEntry for disabled level")
	}
}

func TestFilterCore_ContextFieldNotInOutput(t *testing.T) {
	// This is the critical test: ensure context.Context fields don't pollute console output
	var buf bytes.Buffer
	encoder := gozapcore.NewConsoleEncoder(gozapcore.EncoderConfig{
		MessageKey:  "msg",
		LevelKey:    "level",
		TimeKey:     "time",
		EncodeTime:  gozapcore.ISO8601TimeEncoder,
		EncodeLevel: gozapcore.CapitalLevelEncoder,
	})
	baseCore := gozapcore.NewCore(encoder, gozapcore.AddSync(&buf), gozapcore.DebugLevel)

	// Create filterCore that filters "context"
	filterCore := newFilterCore(baseCore, map[string]bool{"context": true})

	// Create logger with context field
	logger := gozap.New(filterCore)
	logger = logger.With(
		gozap.String("trace_id", "4bf92f3577b34da6a3ce929d0e0e4736"),
		gozap.String("context", "context.Background.WithCancel(...)"), // Simulating context string
		gozap.String("span_id", "00f067aa0ba902b7"),
	)

	logger.Info("test message")

	output := buf.String()

	// Verify trace_id and span_id are present
	if !bytes.Contains(buf.Bytes(), []byte("trace_id")) {
		t.Errorf("Expected trace_id in output, got: %s", output)
	}
	if !bytes.Contains(buf.Bytes(), []byte("span_id")) {
		t.Errorf("Expected span_id in output, got: %s", output)
	}

	// CRITICAL: Verify "context" is NOT in output
	if bytes.Contains(buf.Bytes(), []byte("context")) {
		t.Errorf("Unexpected 'context' field found in output (should be filtered): %s", output)
	}
	if bytes.Contains(buf.Bytes(), []byte("Background")) || bytes.Contains(buf.Bytes(), []byte("WithCancel")) {
		t.Errorf("Context object details leaked into output: %s", output)
	}
}
