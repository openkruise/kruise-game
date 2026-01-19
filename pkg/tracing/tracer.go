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
	"context"
	"errors"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"

	"github.com/openkruise/kruise-game/pkg/logging"
	"github.com/openkruise/kruise-game/pkg/version"
)

var (
	// ErrInvalidSamplingRate is returned when sampling rate is not in range [0.0, 1.0]
	ErrInvalidSamplingRate = errors.New("sampling rate must be between 0.0 and 1.0")

	// ErrCollectorUnavailable is returned when OTLP collector is unreachable
	ErrCollectorUnavailable = errors.New("OTel Collector unavailable")
)

// Apply initializes the global TracerProvider based on the given options.
// If initialization fails, it falls back to a no-op tracer and returns an error.
// This function should be called once during application startup.
func (o *TracingOptions) Apply() error {
	if err := o.Validate(); err != nil {
		return err
	}

	// If tracing is disabled, use no-op tracer
	if !o.Enabled {
		otel.SetTracerProvider(sdktrace.NewTracerProvider())
		return nil
	}

	// Create OTLP exporter
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Build exporter options
	exporterOpts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(o.CollectorEndpoint),
		otlptracegrpc.WithInsecure(), // TODO: Add TLS support for production
		// Note: Removed grpc.WithBlock() to allow non-blocking connection
		// gRPC will automatically retry connection in the background
	}

	// Add authentication header if token is provided
	if o.CollectorToken != "" {
		exporterOpts = append(exporterOpts, otlptracegrpc.WithHeaders(map[string]string{
			"Authorization": "Bearer " + o.CollectorToken,
		}))
	}

	exporter, err := otlptracegrpc.New(ctx, exporterOpts...)
	if err != nil {
		// Fall back to no-op tracer on failure
		otel.SetTracerProvider(sdktrace.NewTracerProvider())
		return errors.Join(ErrCollectorUnavailable, err)
	}

	metadata := logging.ResourceMetadataSnapshot()
	serviceName := metadata.ServiceName
	if serviceName == "" {
		serviceName = "okg-controller-manager"
	}
	resourceAttrs := []attribute.KeyValue{
		semconv.ServiceNameKey.String(serviceName),
	}
	if metadata.Namespace != "" {
		resourceAttrs = append(resourceAttrs, semconv.ServiceNamespaceKey.String(metadata.Namespace))
		resourceAttrs = append(resourceAttrs, semconv.K8SNamespaceNameKey.String(metadata.Namespace))
	}
	if metadata.ServiceVersion != "" {
		resourceAttrs = append(resourceAttrs, semconv.ServiceVersionKey.String(metadata.ServiceVersion))
	} else {
		resourceAttrs = append(resourceAttrs, semconv.ServiceVersionKey.String(version.Version))
	}
	if metadata.ServiceInstanceID != "" {
		resourceAttrs = append(resourceAttrs, semconv.ServiceInstanceIDKey.String(metadata.ServiceInstanceID))
	}
	if metadata.PodName != "" {
		resourceAttrs = append(resourceAttrs, semconv.K8SPodNameKey.String(metadata.PodName))
	}

	// Create resource attributes
	res, err := resource.New(ctx,
		resource.WithAttributes(resourceAttrs...),
		resource.WithHost(),
		resource.WithOS(),
		resource.WithProcess(),
	)
	if err != nil {
		// Fallback to default resource if creation fails
		res = resource.Default()
	}

	// Create TracerProvider
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(o.SamplingRate)),
		sdktrace.WithResource(res),
	)

	// Set as global TracerProvider
	otel.SetTracerProvider(tp)

	return nil
}

// Shutdown gracefully shuts down the global TracerProvider.
// This should be called during application shutdown.
func Shutdown(ctx context.Context) error {
	if tp, ok := otel.GetTracerProvider().(*sdktrace.TracerProvider); ok {
		return tp.Shutdown(ctx)
	}
	return nil
}
