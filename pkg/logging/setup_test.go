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
