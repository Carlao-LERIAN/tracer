// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

// Package testutil provides shared test utilities.
package testutil

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// SpanStub is a type alias for tracetest.SpanStub for convenience.
type SpanStub = tracetest.SpanStub

// TestTracer encapsulates tracing setup for tests.
// It captures spans in memory and automatically restores the previous
// global tracer provider when the test completes.
type TestTracer struct {
	Exporter         *tracetest.InMemoryExporter
	Provider         *sdktrace.TracerProvider
	previousProvider trace.TracerProvider
}

// SetupTestTracing creates a test tracer and sets it as the global provider.
// The previous global provider is automatically restored when the test completes.
// Uses t.Cleanup() to ensure proper teardown even if the test fails.
func SetupTestTracing(t *testing.T) *TestTracer {
	t.Helper()

	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))

	previousProvider := otel.GetTracerProvider()

	otel.SetTracerProvider(tp)

	tt := &TestTracer{
		Exporter:         exporter,
		Provider:         tp,
		previousProvider: previousProvider,
	}

	t.Cleanup(func() {
		tt.Cleanup()
	})

	return tt
}

// Cleanup restores the previous provider and shuts down the test provider.
// This is called automatically via t.Cleanup(), but can be called manually if needed.
func (tt *TestTracer) Cleanup() {
	otel.SetTracerProvider(tt.previousProvider)
	_ = tt.Provider.Shutdown(context.Background())
}

// GetSpans returns all captured spans.
func (tt *TestTracer) GetSpans() tracetest.SpanStubs {
	return tt.Exporter.GetSpans()
}

// Reset clears all captured spans.
func (tt *TestTracer) Reset() {
	tt.Exporter.Reset()
}
