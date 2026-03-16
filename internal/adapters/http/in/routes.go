// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package in

import (
	"os"

	libLog "github.com/LerianStudio/lib-commons/v2/commons/log"
	libHTTP "github.com/LerianStudio/lib-commons/v2/commons/net/http"
	libOtel "github.com/LerianStudio/lib-commons/v2/commons/opentelemetry"
	"github.com/gofiber/contrib/otelfiber/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/recover"
	fiberSwagger "github.com/swaggo/fiber-swagger"

	"tracer/internal/adapters/http/in/middleware"
	"tracer/pkg/clock"
)

// defaultCORSOrigins is the restrictive default when CORS_ALLOWED_ORIGINS is not set.
// This prevents accidental exposure in production. Operators must explicitly configure origins.
const defaultCORSOrigins = ""

// getCORSAllowedOrigins returns the configured CORS origins or empty string (restrictive).
// Operators should set CORS_ALLOWED_ORIGINS explicitly:
// - Production: "https://app.example.com,https://admin.example.com"
// - Development: "*" (only if explicitly needed)
func getCORSAllowedOrigins(configured string) string {
	if configured == "" {
		return defaultCORSOrigins
	}

	return configured
}

// RouteConfig holds non-auth configuration for route setup.
// Auth configuration lives in middleware.AuthGuardConfig.
type RouteConfig struct {
	// CORSAllowedOrigins is a comma-separated list of allowed origins.
	// If empty, defaults to restrictive behavior (no wildcard in production).
	// Set to "*" explicitly for development environments only.
	CORSAllowedOrigins string

	// APIKeyOnlyValidation enables API-key-only auth for the validation endpoint.
	// When true AND PluginAuthEnabled=true (dual mode):
	// - Routes registered with guard.With(..., true) use API key auth only, bypassing plugin auth.
	// - Routes registered with guard.With(..., false) use plugin auth exclusively (no fallback).
	// When PluginAuthEnabled=false, all routes use API key auth regardless of this flag.
	APIKeyOnlyValidation bool
}

// skipTelemetryPaths returns true for paths that should skip detailed telemetry.
// Health/readiness probes generate high-frequency, low-value spans.
func skipTelemetryPaths(c *fiber.Ctx) bool {
	switch c.Path() {
	case "/health", "/ready":
		return true
	default:
		return false
	}
}

func NewRoutes(lg libLog.Logger, tl *libOtel.Telemetry, hc *HealthChecker, cfg *RouteConfig, ruleService RuleService, limitService LimitService, validationService ValidationService, transactionValidationService TransactionValidationService, auditEventService AuditEventService, guard *middleware.AuthGuard, clk clock.Clock) *fiber.App {
	f := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		ErrorHandler: func(ctx *fiber.Ctx, err error) error {
			return libHTTP.HandleFiberError(ctx, err)
		},
	})
	// Check if telemetry should be skipped to avoid data race in lib-commons ContextWithLogger.
	// The race occurs when multiple goroutines call WithTelemetry concurrently in tests.
	skipTelemetry := os.Getenv("SKIP_LIB_COMMONS_TELEMETRY") == "true"

	tlMid := libHTTP.NewTelemetryMiddleware(tl)

	// Middleware order is CRITICAL per Ring Standards:
	// 1. WithTelemetry - First: injects tracer/logger into context
	// Skipped when SKIP_LIB_COMMONS_TELEMETRY=true to avoid data race in lib-commons ContextWithLogger.
	if !skipTelemetry {
		f.Use(tlMid.WithTelemetry(tl))
	}

	// 2. Recover - Second: captures panics before they propagate
	// Stack trace disabled in production to prevent information leakage (OWASP).
	// Panics are still logged via telemetry for debugging.
	f.Use(recover.New(recover.Config{
		EnableStackTrace: false,
	}))

	// 3. CORS - Third: handles preflight before auth
	f.Use(cors.New(cors.Config{
		AllowOrigins:     getCORSAllowedOrigins(cfg.CORSAllowedOrigins),
		AllowMethods:     "GET,POST,PUT,PATCH,DELETE,OPTIONS",
		AllowHeaders:     "Origin,Content-Type,Accept,Authorization,X-Request-ID,X-API-Key",
		AllowCredentials: false,
		MaxAge:           3600,
	}))

	// 4. OTel Fiber - Fourth: HTTP metrics and request tracing
	f.Use(otelfiber.Middleware(
		otelfiber.WithNext(skipTelemetryPaths),
	))

	// 5. Client IP - Fifth: extract and inject client IP into context for audit trail
	f.Use(middleware.ClientIPMiddleware())

	// 6. HTTP Logging - Sixth: structured request/response logging
	// Skipped when SKIP_LIB_COMMONS_TELEMETRY=true to avoid data race in lib-commons ContextWithLogger.
	if !skipTelemetry {
		f.Use(libHTTP.WithHTTPLogging(libHTTP.WithCustomLogger(lg)))
	}

	// 7. Fault Injection - Seventh: ONLY for integration tests
	// Enabled via FAULT_INJECTION_ENABLED=true environment variable.
	// NEVER enable in production - allows simulating 504/503 errors.
	f.Use(middleware.FaultInjection())

	// Public endpoints (no auth required)
	f.Get("/health", Health)
	f.Get("/ready", hc.ReadinessHandler())
	f.Get("/version", Version)

	// Doc Swagger
	f.Get("/swagger/*", WithSwaggerEnvConfig(), fiberSwagger.WrapHandler)

	// Protected API group (uses /v1/ prefix per API Design v1.3.0)
	// Auth is handled per-endpoint by AuthGuard based on configuration flags.
	api := f.Group("/v1")

	// Rule endpoints
	ruleHandler := NewHandler(ruleService)
	api.Post("/rules", guard.With("rules", "post", false), ruleHandler.CreateRule)
	api.Get("/rules", guard.With("rules", "get", false), ruleHandler.ListRules)
	api.Get("/rules/:id", guard.With("rules", "get", false), ruleHandler.GetRule)
	api.Patch("/rules/:id", guard.With("rules", "patch", false), ruleHandler.UpdateRule)
	api.Delete("/rules/:id", guard.With("rules", "delete", false), ruleHandler.DeleteRule)
	api.Post("/rules/:id/activate", guard.With("rules", "post", false), ruleHandler.ActivateRule)
	api.Post("/rules/:id/deactivate", guard.With("rules", "post", false), ruleHandler.DeactivateRule)
	api.Post("/rules/:id/draft", guard.With("rules", "post", false), ruleHandler.DraftRule)

	// Limit endpoints
	limitHandler := NewLimitHandler(limitService)
	api.Post("/limits", guard.With("limits", "post", false), limitHandler.CreateLimit)
	api.Get("/limits", guard.With("limits", "get", false), limitHandler.ListLimits)
	api.Get("/limits/:id", guard.With("limits", "get", false), limitHandler.GetLimit)
	api.Get("/limits/:id/usage", guard.With("limits", "get", false), limitHandler.GetLimitUsage)
	api.Patch("/limits/:id", guard.With("limits", "patch", false), limitHandler.UpdateLimit)
	api.Delete("/limits/:id", guard.With("limits", "delete", false), limitHandler.DeleteLimit)
	api.Post("/limits/:id/activate", guard.With("limits", "post", false), limitHandler.ActivateLimit)
	api.Post("/limits/:id/deactivate", guard.With("limits", "post", false), limitHandler.DeactivateLimit)
	api.Post("/limits/:id/draft", guard.With("limits", "post", false), limitHandler.DraftLimit)

	// Transaction Validation endpoints (read-only per SOX/GLBA requirements)
	transactionValidationHandler := NewTransactionValidationHandler(transactionValidationService)
	api.Get("/validations", guard.With("validations", "get", false), transactionValidationHandler.ListTransactionValidations)
	api.Get("/validations/:id", guard.With("validations", "get", false), transactionValidationHandler.GetTransactionValidation)

	// Validation endpoint POST
	// When APIKeyOnlyValidation=true, uses API key auth only (bypasses plugin auth)
	validationHandler, err := NewValidationHandler(validationService, clk)
	if err != nil {
		lg.Fatalf("failed to create validation handler: %v", err)
	}

	api.Post("/validations", guard.With("validations", "post", cfg.APIKeyOnlyValidation), validationHandler.Validate)

	// Audit Event endpoints (read-only per SOX/GLBA requirements)
	auditEventHandler := NewAuditEventHandler(auditEventService)
	api.Get("/audit-events", guard.With("audit-events", "get", false), auditEventHandler.ListAuditEvents)
	api.Get("/audit-events/:id", guard.With("audit-events", "get", false), auditEventHandler.GetAuditEvent)
	api.Get("/audit-events/:id/verify", guard.With("audit-events", "get", false), auditEventHandler.VerifyHashChain)

	// End tracing spans middleware - skipped when telemetry is disabled
	if !skipTelemetry {
		f.Use(tlMid.EndTracingSpans)
	}

	return f
}
