// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

// Package middleware provides HTTP middleware for the Tracer API.
package middleware

import (
	"context"
	"crypto/subtle"

	libCommons "github.com/LerianStudio/lib-commons/v4/commons"
	libLog "github.com/LerianStudio/lib-commons/v4/commons/log"
	libOtel "github.com/LerianStudio/lib-commons/v4/commons/opentelemetry"
	libMetrics "github.com/LerianStudio/lib-commons/v4/commons/opentelemetry/metrics"
	"github.com/gofiber/fiber/v2"

	pkgHTTP "tracer/pkg/net/http"
)

// HeaderAPIKey is the HTTP header name for API key authentication.
const HeaderAPIKey = "X-API-Key"

// CounterAdder is the interface for adding values to a counter metric.
type CounterAdder interface {
	WithLabels(labels map[string]string) CounterAdder
	Add(ctx context.Context, value int64)
}

// MetricsRecorder is the interface for recording metrics.
// This abstraction allows for easy mocking in tests.
type MetricsRecorder interface {
	Counter(m Metric) CounterAdder
}

// metricsFactoryAdapter wraps *libMetrics.MetricsFactory to implement MetricsRecorder.
type metricsFactoryAdapter struct {
	factory *libMetrics.MetricsFactory
	logger  libLog.Logger
}

// NewMetricsRecorder creates a MetricsRecorder from a *libMetrics.MetricsFactory.
func NewMetricsRecorder(factory *libMetrics.MetricsFactory, logger libLog.Logger) MetricsRecorder {
	if factory == nil {
		return nil
	}

	return &metricsFactoryAdapter{factory: factory, logger: logger}
}

func (a *metricsFactoryAdapter) Counter(m Metric) CounterAdder {
	builder, err := a.factory.Counter(m)
	if err != nil || builder == nil {
		if a.logger != nil {
			a.logger.With(libLog.Any("error", err)).
				Log(context.Background(), libLog.LevelWarn, "Failed to create metrics counter, using no-op fallback")
		}

		return &noopCounterAdder{}
	}

	return &counterBuilderAdapter{builder: builder}
}

// noopCounterAdder is a no-op implementation of CounterAdder used when counter
// creation fails. This prevents nil pointer panics while allowing the middleware
// to continue processing requests.
type noopCounterAdder struct{}

func (n *noopCounterAdder) WithLabels(_ map[string]string) CounterAdder { return n }
func (n *noopCounterAdder) Add(_ context.Context, _ int64)              {}

// counterBuilderAdapter wraps *libMetrics.CounterBuilder to implement CounterAdder.
type counterBuilderAdapter struct {
	builder *libMetrics.CounterBuilder
}

func (c *counterBuilderAdapter) WithLabels(labels map[string]string) CounterAdder {
	return &counterBuilderAdapter{builder: c.builder.WithLabels(labels)}
}

func (c *counterBuilderAdapter) Add(ctx context.Context, value int64) {
	_ = c.builder.Add(ctx, value)
}

// Auth failure reasons for logging and metrics.
const (
	ReasonMissingAPIKey = "missing_api_key"
	ReasonInvalidAPIKey = "invalid_api_key"
)

// APIKeyConfig holds the configuration for API Key authentication.
type APIKeyConfig struct {
	// Key is the expected API key value for authentication.
	Key string

	// Enabled controls whether authentication is enforced.
	// When false, all requests pass through without validation (dev mode).
	Enabled bool
}

// validateAPIKey checks if the provided API key is valid.
// Returns the failure reason ("missing_api_key" or "invalid_api_key") or empty string if valid.
// Uses constant-time comparison to prevent timing attacks.
func validateAPIKey(apiKey, expectedKey string) string {
	if apiKey == "" {
		return ReasonMissingAPIKey
	}

	if subtle.ConstantTimeCompare([]byte(apiKey), []byte(expectedKey)) != 1 {
		return ReasonInvalidAPIKey
	}

	return ""
}

// APIKeyAuth creates a Fiber middleware handler that validates API key authentication.
//
// The middleware extracts the API key from the X-API-Key header and validates it
// against the configured key using constant-time comparison to prevent timing attacks.
//
// Security considerations:
//   - Uses crypto/subtle.ConstantTimeCompare to prevent timing attacks
//   - Returns the same error message for missing and invalid keys to prevent enumeration
//   - Never logs the API key value
//
// When cfg.Enabled is false, the middleware passes all requests through without
// validation (useful for development environments).
func APIKeyAuth(cfg APIKeyConfig) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Skip authentication if disabled (dev mode)
		if !cfg.Enabled {
			return c.Next()
		}

		if reason := validateAPIKey(c.Get(HeaderAPIKey), cfg.Key); reason != "" {
			return pkgHTTP.Unauthorized(c, "Unauthenticated", "Unauthorized", "API Key missing or invalid")
		}

		return c.Next()
	}
}

// APIKeyAuthWithLogger creates a Fiber middleware handler that validates API key authentication
// with structured logging for auth events.
//
// The middleware extracts the API key from the X-API-Key header and validates it
// against the configured key using constant-time comparison to prevent timing attacks.
//
// Logging behavior:
//   - Logs "auth_failed" at WARN level with reason="missing_api_key" when key is missing
//   - Logs "auth_failed" at WARN level with reason="invalid_api_key" when key is invalid
//   - Logs "auth_success" at DEBUG level on successful authentication
//   - NEVER logs the actual API key value (security requirement)
//
// Security considerations:
//   - Uses crypto/subtle.ConstantTimeCompare to prevent timing attacks
//   - Returns the same error message for missing and invalid keys to prevent enumeration
//   - Never logs the API key value
//
// When cfg.Enabled is false, the middleware passes all requests through without
// validation or logging (useful for development environments).
func APIKeyAuthWithLogger(cfg APIKeyConfig, logger libLog.Logger) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Skip authentication if disabled (dev mode)
		if !cfg.Enabled {
			return c.Next()
		}

		apiKey := c.Get(HeaderAPIKey)
		path := c.Path()

		if reason := validateAPIKey(apiKey, cfg.Key); reason != "" {
			logger.With(
				libLog.String("reason", reason),
				libLog.String("path", path),
				libLog.String("remote_ip", c.IP()),
			).Log(c.UserContext(), libLog.LevelWarn, "auth_failed")

			return pkgHTTP.Unauthorized(c, "Unauthenticated", "Unauthorized", "API Key missing or invalid")
		}

		logger.With(libLog.String("path", path)).Log(c.UserContext(), libLog.LevelDebug, "auth_success")

		return c.Next()
	}
}

// APIKeyAuthWithMetrics creates a Fiber middleware handler that validates API key authentication
// with structured logging, metrics instrumentation, and distributed tracing.
//
// This is the recommended middleware for production use as it provides full observability:
//   - Structured logging for auth events (same as APIKeyAuthWithLogger)
//   - Metrics: increments tracer_auth_failures_total counter on authentication failures
//   - Tracing: creates a child span "middleware.api_key_auth" per PROJECT_RULES.md
//
// The middleware extracts the API key from the X-API-Key header and validates it
// against the configured key using constant-time comparison to prevent timing attacks.
//
// Metrics labels:
//   - reason="missing_api_key" when X-API-Key header is missing or empty
//   - reason="invalid_api_key" when the provided key doesn't match
//
// Security considerations:
//   - Uses crypto/subtle.ConstantTimeCompare to prevent timing attacks
//   - Returns the same error message for missing and invalid keys to prevent enumeration
//   - Never logs the API key value
//
// When cfg.Enabled is false, the middleware passes all requests through without
// validation, logging, metrics, or tracing (useful for development environments).
//
// Parameters:
//   - cfg: API Key configuration (key value and enabled flag)
//   - logger: Structured logger for auth events
//   - mr: MetricsRecorder for recording auth failure metrics (can be nil if metrics disabled)
//   - telemetry: Telemetry for distributed tracing (can be nil if tracing disabled)
func APIKeyAuthWithMetrics(cfg APIKeyConfig, logger libLog.Logger, mr MetricsRecorder, telemetry *libOtel.Telemetry) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Skip authentication if disabled (dev mode)
		if !cfg.Enabled {
			return c.Next()
		}

		// Start tracing span per PROJECT_RULES.md
		ctx := c.UserContext()
		_, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

		ctx, span := tracer.Start(ctx, "middleware.api_key.auth")
		defer span.End()

		// Propagate span context to downstream handlers
		c.SetUserContext(ctx)

		apiKey := c.Get(HeaderAPIKey)
		path := c.Path()

		if reason := validateAPIKey(apiKey, cfg.Key); reason != "" {
			logger.With(
				libLog.String("reason", reason),
				libLog.String("path", path),
				libLog.String("remote_ip", c.IP()),
			).Log(ctx, libLog.LevelWarn, "auth_failed")

			// Record auth failure in span - business error (expected, span stays OK)
			libOtel.HandleSpanBusinessErrorEvent(span, "authentication failed: "+reason, nil)

			// Increment metric if MetricsRecorder is provided
			if mr != nil {
				mr.Counter(MetricAuthFailures).
					WithLabels(map[string]string{"reason": reason}).
					Add(ctx, 1)
			}

			return pkgHTTP.Unauthorized(c, "Unauthenticated", "Unauthorized", "API Key missing or invalid")
		}

		logger.With(libLog.String("path", path)).Log(ctx, libLog.LevelDebug, "auth_success")

		return c.Next()
	}
}
