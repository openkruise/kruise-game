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

package logging

import (
	"context"

	"github.com/go-logr/logr"
	"go.opentelemetry.io/otel/trace"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// FromContextWithTrace returns a logger from the context with trace_id and span_id injected.
// If no span context is present, it returns a logger without trace fields.
// This is a wrapper around log.FromContext that automatically injects OpenTelemetry trace context.
//
// Usage:
//
//	func (r *GameServerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
//	    logger := logging.FromContextWithTrace(ctx)
//	    logger.Info("Reconciling GameServer", "name", req.Name)
//	    // Output: {"level":"info","msg":"Reconciling GameServer","name":"game-0","trace_id":"4bf92f3577b34da6a3ce929d0e0e4736","span_id":"00f067aa0ba902b7"}
//	}
func FromContextWithTrace(ctx context.Context) logr.Logger {
	logger := log.FromContext(ctx)

	// Pass context to otelzap bridge core (it recognizes context.Context type fields)
	// Note: filterCore will prevent this from appearing in console/JSON output
	logger = logger.WithValues("context", ctx)

	// Extract span context from the context
	span := trace.SpanFromContext(ctx)
	if !span.SpanContext().IsValid() {
		// No valid span context, return logger as-is
		return logger
	}

	spanCtx := span.SpanContext()

	// Inject trace_id and span_id into the logger (for console/JSON cores)
	// Format:
	// - trace_id: 32-character hex string (128-bit), e.g., "4bf92f3577b34da6a3ce929d0e0e4736"
	// - span_id: 16-character hex string (64-bit), e.g., "00f067aa0ba902b7"
	return logger.WithValues(
		"trace_id", spanCtx.TraceID().String(),
		"span_id", spanCtx.SpanID().String(),
	)
}
