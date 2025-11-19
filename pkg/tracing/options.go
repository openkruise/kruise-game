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
	"flag"
)

// TracingOptions holds configuration for OpenTelemetry tracing
type TracingOptions struct {
	// Enabled controls whether tracing is enabled
	Enabled bool

	// CollectorEndpoint is the OTLP gRPC endpoint (e.g., "localhost:4317")
	CollectorEndpoint string

	// SamplingRate is the trace sampling ratio (0.0 to 1.0)
	// 1.0 means sample all traces, 0.1 means sample 10%
	SamplingRate float64
}

// NewOptions returns a new TracingOptions with default values
func NewOptions() *TracingOptions {
	return &TracingOptions{
		Enabled:           false, // Default disabled for safety
		CollectorEndpoint: "localhost:4317",
		SamplingRate:      1.0, // Sample all traces by default
	}
}

// AddFlags adds tracing-related flags to the given FlagSet
func (o *TracingOptions) AddFlags(fs *flag.FlagSet) {
	fs.BoolVar(&o.Enabled, "enable-tracing", o.Enabled,
		"Enable OpenTelemetry distributed tracing. If disabled, uses no-op tracer.")

	fs.StringVar(&o.CollectorEndpoint, "otel-collector-endpoint", o.CollectorEndpoint,
		"OpenTelemetry Collector OTLP gRPC endpoint (e.g., 'otel-collector.observability.svc.cluster.local:4317')")

	fs.Float64Var(&o.SamplingRate, "otel-sampling-rate", o.SamplingRate,
		"OpenTelemetry trace sampling rate (0.0 to 1.0). 1.0 samples all traces, 0.1 samples 10%.")
}

// Validate checks if the options are valid
func (o *TracingOptions) Validate() error {
	if o.SamplingRate < 0.0 || o.SamplingRate > 1.0 {
		return ErrInvalidSamplingRate
	}
	return nil
}
