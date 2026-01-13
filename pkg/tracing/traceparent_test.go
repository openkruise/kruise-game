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
	"testing"

	"go.opentelemetry.io/otel/trace"
)

func TestGenerateTraceparent(t *testing.T) {
	tests := []struct {
		name     string
		sc       trace.SpanContext
		expected string
	}{
		{
			name: "valid sampled span context",
			sc: trace.NewSpanContext(trace.SpanContextConfig{
				TraceID:    trace.TraceID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
				SpanID:     trace.SpanID{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18},
				TraceFlags: trace.FlagsSampled,
			}),
			expected: "00-0102030405060708090a0b0c0d0e0f10-1112131415161718-01",
		},
		{
			name: "valid unsampled span context",
			sc: trace.NewSpanContext(trace.SpanContextConfig{
				TraceID:    trace.TraceID{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0x00},
				SpanID:     trace.SpanID{0xa1, 0xa2, 0xa3, 0xa4, 0xa5, 0xa6, 0xa7, 0xa8},
				TraceFlags: 0,
			}),
			expected: "00-aabbccddeeff11223344556677889900-a1a2a3a4a5a6a7a8-00",
		},
		{
			name:     "invalid span context",
			sc:       trace.SpanContext{},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GenerateTraceparent(tt.sc)
			if result != tt.expected {
				t.Errorf("GenerateTraceparent() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestParseTraceparent(t *testing.T) {
	tests := []struct {
		name        string
		traceparent string
		wantErr     bool
		validate    func(t *testing.T, sc trace.SpanContext)
	}{
		{
			name:        "valid traceparent with sampled flag",
			traceparent: "00-0102030405060708090a0b0c0d0e0f10-1112131415161718-01",
			wantErr:     false,
			validate: func(t *testing.T, sc trace.SpanContext) {
				if !sc.IsValid() {
					t.Error("span context should be valid")
				}
				if !sc.TraceFlags().IsSampled() {
					t.Error("span context should be sampled")
				}
				expectedTraceID := trace.TraceID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}
				if sc.TraceID() != expectedTraceID {
					t.Errorf("trace ID mismatch: got %v, want %v", sc.TraceID(), expectedTraceID)
				}
				expectedSpanID := trace.SpanID{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18}
				if sc.SpanID() != expectedSpanID {
					t.Errorf("span ID mismatch: got %v, want %v", sc.SpanID(), expectedSpanID)
				}
				if !sc.IsRemote() {
					t.Error("span context should be marked as remote")
				}
			},
		},
		{
			name:        "valid traceparent with unsampled flag",
			traceparent: "00-aabbccddeeff11223344556677889900-a1a2a3a4a5a6a7a8-00",
			wantErr:     false,
			validate: func(t *testing.T, sc trace.SpanContext) {
				if !sc.IsValid() {
					t.Error("span context should be valid")
				}
				if sc.TraceFlags().IsSampled() {
					t.Error("span context should not be sampled")
				}
			},
		},
		{
			name:        "empty traceparent",
			traceparent: "",
			wantErr:     true,
		},
		{
			name:        "invalid format - too few parts",
			traceparent: "00-0102030405060708090a0b0c0d0e0f10-1112131415161718",
			wantErr:     true,
		},
		{
			name:        "invalid format - too many parts",
			traceparent: "00-0102030405060708090a0b0c0d0e0f10-1112131415161718-01-extra",
			wantErr:     true,
		},
		{
			name:        "unsupported version",
			traceparent: "01-0102030405060708090a0b0c0d0e0f10-1112131415161718-01",
			wantErr:     true,
		},
		{
			name:        "invalid trace_id length",
			traceparent: "00-0102030405060708090a0b0c0d0e0f-1112131415161718-01",
			wantErr:     true,
		},
		{
			name:        "invalid trace_id hex",
			traceparent: "00-0102030405060708090a0b0c0d0e0fXX-1112131415161718-01",
			wantErr:     true,
		},
		{
			name:        "invalid span_id length",
			traceparent: "00-0102030405060708090a0b0c0d0e0f10-11121314151617-01",
			wantErr:     true,
		},
		{
			name:        "invalid span_id hex",
			traceparent: "00-0102030405060708090a0b0c0d0e0f10-111213141516171X-01",
			wantErr:     true,
		},
		{
			name:        "invalid trace_flags length",
			traceparent: "00-0102030405060708090a0b0c0d0e0f10-1112131415161718-1",
			wantErr:     true,
		},
		{
			name:        "invalid trace_flags hex",
			traceparent: "00-0102030405060708090a0b0c0d0e0f10-1112131415161718-0X",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc, err := ParseTraceparent(tt.traceparent)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseTraceparent() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.validate != nil {
				tt.validate(t, sc)
			}
		})
	}
}

func TestRoundTrip(t *testing.T) {
	// Test that GenerateTraceparent -> ParseTraceparent is idempotent
	original := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
		SpanID:     trace.SpanID{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18},
		TraceFlags: trace.FlagsSampled,
	})

	traceparent := GenerateTraceparent(original)
	if traceparent == "" {
		t.Fatal("GenerateTraceparent returned empty string")
	}

	parsed, err := ParseTraceparent(traceparent)
	if err != nil {
		t.Fatalf("ParseTraceparent failed: %v", err)
	}

	if parsed.TraceID() != original.TraceID() {
		t.Errorf("TraceID mismatch: got %v, want %v", parsed.TraceID(), original.TraceID())
	}
	if parsed.SpanID() != original.SpanID() {
		t.Errorf("SpanID mismatch: got %v, want %v", parsed.SpanID(), original.SpanID())
	}
	if parsed.TraceFlags() != original.TraceFlags() {
		t.Errorf("TraceFlags mismatch: got %v, want %v", parsed.TraceFlags(), original.TraceFlags())
	}
	if !parsed.IsRemote() {
		t.Error("Parsed span context should be marked as remote")
	}
}

func TestNormalizeTraceID(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    string
		shouldError bool
	}{
		{
			name:     "already normalized 32-char ID",
			input:    "0102030405060708090a0b0c0d0e0f10",
			expected: "0102030405060708090a0b0c0d0e0f10",
		},
		{
			name:     "uppercase letters should be lowercased",
			input:    "AABBCCDDEEFF11223344556677889900",
			expected: "aabbccddeeff11223344556677889900",
		},
		{
			name:     "mixed case should be normalized",
			input:    "AaBbCcDdEeFf11223344556677889900",
			expected: "aabbccddeeff11223344556677889900",
		},
		{
			name:     "31-char ID with leading zero stripped (Tempo Search API case)",
			input:    "54c2597c70465fdf381a6e5e3660d0d",
			expected: "054c2597c70465fdf381a6e5e3660d0d",
		},
		{
			name:     "30-char ID should be left-padded",
			input:    "1234567890abcdef1234567890abcd",
			expected: "001234567890abcdef1234567890abcd",
		},
		{
			name:     "1-char ID should be left-padded to 32",
			input:    "a",
			expected: "0000000000000000000000000000000a",
		},
		{
			name:     "whitespace should be trimmed",
			input:    "  0102030405060708090a0b0c0d0e0f10  ",
			expected: "0102030405060708090a0b0c0d0e0f10",
		},
		{
			name:        "all zeros should be rejected",
			input:       "00000000000000000000000000000000",
			shouldError: true,
		},
		{
			name:        "all zeros with padding should be rejected",
			input:       "0",
			shouldError: true,
		},
		{
			name:        "too long (33 chars) should error",
			input:       "0102030405060708090a0b0c0d0e0f101",
			shouldError: true,
		},
		{
			name:        "invalid characters (g) should error",
			input:       "0102030405060708090a0b0c0d0e0fgg",
			shouldError: true,
		},
		{
			name:        "invalid characters (space in middle) should error",
			input:       "01020304 05060708090a0b0c0d0e0f10",
			shouldError: true,
		},
		{
			name:        "empty string should error",
			input:       "",
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := NormalizeTraceID(tt.input)

			if tt.shouldError {
				if err == nil {
					t.Errorf("expected error but got none, result: %s", result)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if result != tt.expected {
					t.Errorf("got %s, want %s", result, tt.expected)
				}
			}
		})
	}
}

func TestEqualTraceID(t *testing.T) {
	tests := []struct {
		name     string
		idA      string
		idB      string
		expected bool
	}{
		{
			name:     "identical 32-char IDs",
			idA:      "0102030405060708090a0b0c0d0e0f10",
			idB:      "0102030405060708090a0b0c0d0e0f10",
			expected: true,
		},
		{
			name:     "same ID with different cases",
			idA:      "AABBCCDDEEFF11223344556677889900",
			idB:      "aabbccddeeff11223344556677889900",
			expected: true,
		},
		{
			name:     "31-char vs 32-char (leading zero stripped)",
			idA:      "54c2597c70465fdf381a6e5e3660d0d",
			idB:      "054c2597c70465fdf381a6e5e3660d0d",
			expected: true,
		},
		{
			name:     "30-char vs 32-char (two leading zeros stripped)",
			idA:      "1234567890abcdef1234567890abcd",
			idB:      "001234567890abcdef1234567890abcd",
			expected: true,
		},
		{
			name:     "different IDs",
			idA:      "0102030405060708090a0b0c0d0e0f10",
			idB:      "aabbccddeeff11223344556677889900",
			expected: false,
		},
		{
			name:     "one invalid ID (too long)",
			idA:      "0102030405060708090a0b0c0d0e0f101",
			idB:      "0102030405060708090a0b0c0d0e0f10",
			expected: false,
		},
		{
			name:     "both invalid IDs (all zeros)",
			idA:      "00000000000000000000000000000000",
			idB:      "0",
			expected: false,
		},
		{
			name:     "whitespace handling",
			idA:      "  0102030405060708090a0b0c0d0e0f10  ",
			idB:      "0102030405060708090a0b0c0d0e0f10",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EqualTraceID(tt.idA, tt.idB)
			if result != tt.expected {
				t.Errorf("EqualTraceID(%q, %q) = %v, want %v", tt.idA, tt.idB, result, tt.expected)
			}
		})
	}
}
