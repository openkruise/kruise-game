package logging

import (
	"flag"
	"strings"
	"testing"
)

func TestOptionsConflict(t *testing.T) {
	opts := NewOptions()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	opts.AddFlags(fs)
	if err := fs.Parse([]string{"--log-format=json", "--zap-encoder=console"}); err != nil {
		t.Fatalf("parse flags: %v", err)
	}

	result, err := opts.Apply(fs)
	if err != nil {
		t.Fatalf("apply options: %v", err)
	}

	expectedWarning := "WARNING: --log-format overrides --zap-encoder (json vs console)"
	if !strings.Contains(result.Warning, expectedWarning) {
		t.Fatalf("expected warning %q, got %q", expectedWarning, result.Warning)
	}

	if opts.ZapOptions.Encoder == nil {
		t.Fatal("expected Encoder to be configured for json format")
	}
}

func TestOptionsConsoleResetsEncoder(t *testing.T) {
	opts := NewOptions()
	fs := flag.NewFlagSet("test-json", flag.ContinueOnError)
	opts.AddFlags(fs)
	if err := fs.Parse([]string{"--log-format=json"}); err != nil {
		t.Fatalf("parse json flags: %v", err)
	}
	if _, err := opts.Apply(fs); err != nil {
		t.Fatalf("apply json options: %v", err)
	}
	if opts.ZapOptions.Encoder == nil {
		t.Fatal("expected encoder to be set for json format")
	}

	consoleOpts := NewOptions()
	fsConsole := flag.NewFlagSet("test-console", flag.ContinueOnError)
	consoleOpts.AddFlags(fsConsole)
	if err := fsConsole.Parse([]string{"--log-format=console"}); err != nil {
		t.Fatalf("parse console flags: %v", err)
	}
	if _, err := consoleOpts.Apply(fsConsole); err != nil {
		t.Fatalf("apply console options: %v", err)
	}
	if consoleOpts.ZapOptions.Encoder != nil {
		t.Fatal("expected encoder to be nil for console format")
	}
}

func TestOptionsInvalidFormat(t *testing.T) {
	opts := NewOptions()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	opts.AddFlags(fs)
	if err := fs.Parse([]string{"--log-format=invalid"}); err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	if _, err := opts.Apply(fs); err == nil {
		t.Fatal("expected an error for invalid format")
	}
}

func TestOptionsZapEncoderOnly(t *testing.T) {
	opts := NewOptions()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	opts.AddFlags(fs)
	if err := fs.Parse([]string{"--zap-encoder=json"}); err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	result, err := opts.Apply(fs)
	if err != nil {
		t.Fatalf("apply options: %v", err)
	}
	if result.Warning != "" {
		t.Fatalf("did not expect warning, got %q", result.Warning)
	}
	if result.Format != "json" {
		t.Fatalf("expected final format json, got %s", result.Format)
	}
	if opts.ZapOptions.Encoder == nil {
		t.Fatal("expected encoder to be configured when zap-encoder is set")
	}
}

func TestOTelCollectorTokenFlag(t *testing.T) {
	// Test that otel-collector-token flag is available and readable
	opts := NewOptions()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	opts.AddFlags(fs)

	// Also add tracing flags to ensure token flag is registered
	fs.String("otel-collector-endpoint", "localhost:4317", "test endpoint")
	fs.String("otel-collector-token", "", "test token")

	if err := fs.Parse([]string{"--otel-collector-endpoint=test-endpoint:4317", "--otel-collector-token=test-token-value"}); err != nil {
		t.Fatalf("parse flags: %v", err)
	}

	// Verify token flag was set
	tokenFlag := fs.Lookup("otel-collector-token")
	if tokenFlag == nil {
		t.Fatal("expected otel-collector-token flag to be registered")
	}
	if tokenFlag.Value.String() != "test-token-value" {
		t.Fatalf("expected token value 'test-token-value', got '%s'", tokenFlag.Value.String())
	}

	// Apply should not error even with token present
	_, err := opts.Apply(fs)
	if err != nil {
		t.Fatalf("apply options with token flag should not error: %v", err)
	}
}

func TestOTelCollectorWithoutToken(t *testing.T) {
	// Test that system works without token (backward compatibility)
	opts := NewOptions()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	opts.AddFlags(fs)

	fs.String("otel-collector-endpoint", "localhost:4317", "test endpoint")
	fs.String("otel-collector-token", "", "test token")

	if err := fs.Parse([]string{"--otel-collector-endpoint=test-endpoint:4317"}); err != nil {
		t.Fatalf("parse flags: %v", err)
	}

	// Verify token flag exists but is empty
	tokenFlag := fs.Lookup("otel-collector-token")
	if tokenFlag == nil {
		t.Fatal("expected otel-collector-token flag to be registered")
	}
	if tokenFlag.Value.String() != "" {
		t.Fatalf("expected empty token value, got '%s'", tokenFlag.Value.String())
	}

	// Apply should work without token
	_, err := opts.Apply(fs)
	if err != nil {
		t.Fatalf("apply options without token should not error: %v", err)
	}
}

func TestEnableOTelLogsFlag_DefaultDisabled(t *testing.T) {
	// Test that OTLP logs export is disabled by default even with endpoint configured
	opts := NewOptions()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	opts.AddFlags(fs)

	// Register tracing flags (simulating main.go behavior)
	fs.String("otel-collector-endpoint", "localhost:4317", "test endpoint")
	fs.String("otel-collector-token", "", "test token")

	// Only set endpoint, NOT --enable-otel-logs
	if err := fs.Parse([]string{"--otel-collector-endpoint=test-endpoint:4317"}); err != nil {
		t.Fatalf("parse flags: %v", err)
	}

	// Verify enable-otel-logs flag exists and defaults to false
	enableLogsFlag := fs.Lookup(enableOTelLogsFlagName)
	if enableLogsFlag == nil {
		t.Fatal("expected enable-otel-logs flag to be registered")
	}
	if enableLogsFlag.Value.String() != "false" {
		t.Fatalf("expected enable-otel-logs to default to false, got '%s'", enableLogsFlag.Value.String())
	}

	// Apply should work without enabling OTLP logs
	_, err := opts.Apply(fs)
	if err != nil {
		t.Fatalf("apply options should not error: %v", err)
	}

	// enableOTelLogs should be false
	if opts.enableOTelLogs {
		t.Fatal("expected enableOTelLogs to be false by default")
	}
}

func TestEnableOTelLogsFlag_ExplicitlyEnabled(t *testing.T) {
	// Test that OTLP logs export can be explicitly enabled
	opts := NewOptions()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	opts.AddFlags(fs)

	// Register tracing flags (simulating main.go behavior)
	fs.String("otel-collector-endpoint", "localhost:4317", "test endpoint")
	fs.String("otel-collector-token", "", "test token")

	// Set both endpoint AND --enable-otel-logs=true
	if err := fs.Parse([]string{
		"--otel-collector-endpoint=test-endpoint:4317",
		"--enable-otel-logs=true",
	}); err != nil {
		t.Fatalf("parse flags: %v", err)
	}

	// Verify enable-otel-logs flag is set to true
	enableLogsFlag := fs.Lookup(enableOTelLogsFlagName)
	if enableLogsFlag == nil {
		t.Fatal("expected enable-otel-logs flag to be registered")
	}
	if enableLogsFlag.Value.String() != "true" {
		t.Fatalf("expected enable-otel-logs to be true, got '%s'", enableLogsFlag.Value.String())
	}

	// Apply should work with OTLP logs enabled
	_, err := opts.Apply(fs)
	if err != nil {
		t.Fatalf("apply options should not error: %v", err)
	}

	// enableOTelLogs should be true
	if !opts.enableOTelLogs {
		t.Fatal("expected enableOTelLogs to be true when explicitly enabled")
	}
}

func TestEnableOTelLogsFlag_EnabledWithoutEndpoint(t *testing.T) {
	// Test that enabling OTLP logs without endpoint doesn't cause errors
	opts := NewOptions()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	opts.AddFlags(fs)

	// Register tracing flags but with empty endpoint
	fs.String("otel-collector-endpoint", "", "test endpoint")
	fs.String("otel-collector-token", "", "test token")

	// Set --enable-otel-logs=true but no endpoint
	if err := fs.Parse([]string{"--enable-otel-logs=true"}); err != nil {
		t.Fatalf("parse flags: %v", err)
	}

	// Apply should work gracefully (no endpoint means no OTLP logs setup)
	_, err := opts.Apply(fs)
	if err != nil {
		t.Fatalf("apply options should not error even without endpoint: %v", err)
	}
}
