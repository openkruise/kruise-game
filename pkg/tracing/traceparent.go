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
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel/trace"
)

var (
	// hex32Pattern matches exactly 32 lowercase hexadecimal characters
	hex32Pattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
	// allZeroTraceID represents an invalid all-zero trace ID
	allZeroTraceID = "00000000000000000000000000000000"
)

// GenerateTraceparent generates a W3C traceparent string from a SpanContext.
// Format: "00-{trace_id}-{span_id}-01"
// - version: "00" (current W3C spec version)
// - trace_id: 32 hex characters (16 bytes)
// - span_id: 16 hex characters (8 bytes)
// - trace_flags: "01" (sampled) or "00" (not sampled)
func GenerateTraceparent(sc trace.SpanContext) string {
	if !sc.IsValid() {
		return ""
	}

	traceID := sc.TraceID().String()
	spanID := sc.SpanID().String()

	// Determine trace flags: "01" if sampled, "00" otherwise
	traceFlags := "00"
	if sc.TraceFlags().IsSampled() {
		traceFlags = "01"
	}

	return fmt.Sprintf("00-%s-%s-%s", traceID, spanID, traceFlags)
}

// ParseTraceparent parses a W3C traceparent string into a SpanContext.
// Expected format: "00-{trace_id}-{span_id}-{trace_flags}"
// Returns an error if the format is invalid.
func ParseTraceparent(traceparent string) (trace.SpanContext, error) {
	if traceparent == "" {
		return trace.SpanContext{}, fmt.Errorf("traceparent is empty")
	}

	parts := strings.Split(traceparent, "-")
	if len(parts) != 4 {
		return trace.SpanContext{}, fmt.Errorf("invalid traceparent format: expected 4 parts, got %d", len(parts))
	}

	version := parts[0]
	traceIDStr := parts[1]
	spanIDStr := parts[2]
	traceFlagsStr := parts[3]

	// Validate version
	if version != "00" {
		return trace.SpanContext{}, fmt.Errorf("unsupported traceparent version: %s (only '00' is supported)", version)
	}

	// Parse trace ID (32 hex characters = 16 bytes)
	if len(traceIDStr) != 32 {
		return trace.SpanContext{}, fmt.Errorf("invalid trace_id length: expected 32, got %d", len(traceIDStr))
	}
	traceIDBytes, err := hex.DecodeString(traceIDStr)
	if err != nil {
		return trace.SpanContext{}, fmt.Errorf("invalid trace_id hex: %w", err)
	}
	var traceID trace.TraceID
	copy(traceID[:], traceIDBytes)

	// Parse span ID (16 hex characters = 8 bytes)
	if len(spanIDStr) != 16 {
		return trace.SpanContext{}, fmt.Errorf("invalid span_id length: expected 16, got %d", len(spanIDStr))
	}
	spanIDBytes, err := hex.DecodeString(spanIDStr)
	if err != nil {
		return trace.SpanContext{}, fmt.Errorf("invalid span_id hex: %w", err)
	}
	var spanID trace.SpanID
	copy(spanID[:], spanIDBytes)

	// Parse trace flags (2 hex characters = 1 byte)
	if len(traceFlagsStr) != 2 {
		return trace.SpanContext{}, fmt.Errorf("invalid trace_flags length: expected 2, got %d", len(traceFlagsStr))
	}
	traceFlagsBytes, err := hex.DecodeString(traceFlagsStr)
	if err != nil {
		return trace.SpanContext{}, fmt.Errorf("invalid trace_flags hex: %w", err)
	}
	var traceFlags trace.TraceFlags
	if len(traceFlagsBytes) > 0 {
		traceFlags = trace.TraceFlags(traceFlagsBytes[0])
	}

	// Create SpanContext
	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: traceFlags,
		Remote:     true, // This is a remote span context (from another service/component)
	})

	if !spanContext.IsValid() {
		return trace.SpanContext{}, fmt.Errorf("parsed span context is invalid")
	}

	return spanContext, nil
}

// NormalizeTraceID normalizes a trace ID string to W3C Trace Context specification.
// It handles common format variations found in different tracing backends:
// - Converts to lowercase
// - Left-pads with zeros to 32 characters (handles Tempo Search API returning 31-char IDs)
// - Validates hexadecimal format and rejects all-zero IDs
//
// W3C Trace Context requires trace-id to be exactly 32 lowercase hex characters (16 bytes).
// This function ensures compatibility with backends that may return non-standard formats,
// particularly Grafana Tempo's Search API which historically strips leading zeros.
//
// References:
// - W3C Trace Context: https://www.w3.org/TR/trace-context/
// - Tempo issue: https://github.com/grafana/tempo/issues/5395
func NormalizeTraceID(id string) (string, error) {
	// Trim whitespace and convert to lowercase
	normalized := strings.ToLower(strings.TrimSpace(id))

	// Reject if too long
	if len(normalized) > 32 {
		return "", fmt.Errorf("trace ID too long: got %d characters, max 32", len(normalized))
	}

	// Left-pad with zeros to 32 characters
	// This handles cases where leading zeros were stripped (e.g., Tempo Search API)
	if len(normalized) < 32 {
		normalized = strings.Repeat("0", 32-len(normalized)) + normalized
	}

	// Validate format: must be exactly 32 lowercase hex characters
	if !hex32Pattern.MatchString(normalized) {
		return "", fmt.Errorf("trace ID contains invalid characters: must be 32 lowercase hex chars")
	}

	// Reject all-zero trace ID (invalid per W3C spec)
	if normalized == allZeroTraceID {
		return "", fmt.Errorf("trace ID cannot be all zeros")
	}

	return normalized, nil
}

// EqualTraceID compares two trace IDs for equality after normalization.
// This is useful for comparing trace IDs from different sources that may use
// different string representations (e.g., with or without leading zeros).
func EqualTraceID(a, b string) bool {
	normalizedA, errA := NormalizeTraceID(a)
	normalizedB, errB := NormalizeTraceID(b)
	return errA == nil && errB == nil && normalizedA == normalizedB
}
