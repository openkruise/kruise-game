/*
Copyright 2024 The Kruise Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package tracing

import (
	"errors"
	"flag"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestNewOptions(t *testing.T) {
	opts := NewOptions()

	if opts.Enabled {
		t.Error("Expected Enabled to be false by default")
	}

	if opts.CollectorEndpoint != "localhost:4317" {
		t.Errorf("Expected CollectorEndpoint to be 'localhost:4317', got '%s'", opts.CollectorEndpoint)
	}

	if opts.CollectorToken != "" {
		t.Errorf("Expected CollectorToken to be empty by default, got '%s'", opts.CollectorToken)
	}

	if opts.SamplingRate != 1.0 {
		t.Errorf("Expected SamplingRate to be 1.0, got %f", opts.SamplingRate)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name         string
		samplingRate float64
		wantErr      bool
	}{
		{
			name:         "valid sampling rate 1.0",
			samplingRate: 1.0,
			wantErr:      false,
		},
		{
			name:         "valid sampling rate 0.5",
			samplingRate: 0.5,
			wantErr:      false,
		},
		{
			name:         "valid sampling rate 0.0",
			samplingRate: 0.0,
			wantErr:      false,
		},
		{
			name:         "invalid sampling rate -0.1",
			samplingRate: -0.1,
			wantErr:      true,
		},
		{
			name:         "invalid sampling rate 1.1",
			samplingRate: 1.1,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &TracingOptions{
				SamplingRate: tt.samplingRate,
			}
			err := opts.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestApply_Disabled(t *testing.T) {
	opts := &TracingOptions{
		Enabled:      false,
		SamplingRate: 1.0,
	}

	err := opts.Apply()
	if err != nil {
		t.Fatalf("Apply() with disabled tracing should not error, got: %v", err)
	}

	// Verify a no-op tracer was set
	tp := otel.GetTracerProvider()
	if _, ok := tp.(*sdktrace.TracerProvider); !ok {
		t.Error("Expected a TracerProvider to be set")
	}
}

func TestApply_InvalidCollectorEndpoint(t *testing.T) {
	opts := &TracingOptions{
		Enabled:           true,
		CollectorEndpoint: "invalid-endpoint:9999", // Unreachable
		SamplingRate:      1.0,
	}

	err := opts.Apply()
	// Should prefer best-effort initialization; tolerate nil or known collector errors
	if err != nil && !errors.Is(err, ErrCollectorUnavailable) {
		t.Fatalf("Unexpected error type when collector unreachable: %v", err)
	}

	// Verify it fell back to a no-op tracer (should still work)
	tp := otel.GetTracerProvider()
	if _, ok := tp.(*sdktrace.TracerProvider); !ok {
		t.Error("Expected a TracerProvider to be set even on failure")
	}
}

func TestAddFlags(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	opts := NewOptions()
	opts.AddFlags(fs)

	// Verify all flags are registered
	tests := []struct {
		flagName string
	}{
		{flagName: FlagEnableTracing},
		{flagName: FlagOtelCollectorEndpoint},
		{flagName: FlagOtelCollectorToken},
		{flagName: FlagOtelSamplingRate},
	}

	for _, tt := range tests {
		t.Run(tt.flagName, func(t *testing.T) {
			if fs.Lookup(tt.flagName) == nil {
				t.Errorf("Flag %s not registered", tt.flagName)
			}
		})
	}

	// Test setting token flag
	err := fs.Set(FlagOtelCollectorToken, "test-token-123")
	if err != nil {
		t.Fatalf("Failed to set token flag: %v", err)
	}
	if opts.CollectorToken != "test-token-123" {
		t.Errorf("Expected CollectorToken to be 'test-token-123', got '%s'", opts.CollectorToken)
	}
}

func TestApply_WithToken(t *testing.T) {
	// This test verifies that Apply() accepts token parameter without error
	// Even though collector is unreachable, it should handle token gracefully
	opts := &TracingOptions{
		Enabled:           true,
		CollectorEndpoint: "unreachable:4317",
		CollectorToken:    "test-bearer-token",
		SamplingRate:      1.0,
	}

	err := opts.Apply()
	// Should fail due to unreachable endpoint but not due to token
	if err != nil && !errors.Is(err, ErrCollectorUnavailable) {
		t.Fatalf("Unexpected error type: %v", err)
	}

	// Verify fallback tracer is set
	tp := otel.GetTracerProvider()
	if _, ok := tp.(*sdktrace.TracerProvider); !ok {
		t.Error("Expected a TracerProvider to be set")
	}
}

func TestApply_WithoutToken(t *testing.T) {
	// Test that Apply() works without token (backward compatibility)
	opts := &TracingOptions{
		Enabled:           true,
		CollectorEndpoint: "unreachable:4317",
		CollectorToken:    "", // No token
		SamplingRate:      0.5,
	}

	err := opts.Apply()
	if err != nil && !errors.Is(err, ErrCollectorUnavailable) {
		t.Fatalf("Unexpected error type: %v", err)
	}

	// Verify fallback tracer is set
	tp := otel.GetTracerProvider()
	if _, ok := tp.(*sdktrace.TracerProvider); !ok {
		t.Error("Expected a TracerProvider to be set")
	}
}
