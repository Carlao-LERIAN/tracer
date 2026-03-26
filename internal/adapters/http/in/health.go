// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package in

//go:generate mockgen -source=health.go -destination=health_mock.go -package=in

import (
	"context"
	"database/sql"
	"errors"
	"time"

	libCommons "github.com/LerianStudio/lib-commons/v4/commons"
	libOtel "github.com/LerianStudio/lib-commons/v4/commons/opentelemetry"
	libPostgres "github.com/LerianStudio/lib-commons/v4/commons/postgres"
	"github.com/gofiber/fiber/v2"

	"tracer/api"
	pkgHTTP "tracer/pkg/net/http"
)

// Sentinel errors for health check failures.
var (
	ErrConnectionNotEstablished = errors.New("connection not established")
	ErrConnectionFailed         = errors.New("connection failed")
	ErrPingFailed               = errors.New("ping failed")
	ErrDependenciesUnhealthy    = errors.New("dependencies unhealthy")
	ErrCacheNotReady            = errors.New("cache not ready")
	ErrCacheStale               = errors.New("cache data stale")
)

// Health check status constants.
const (
	StatusOK       = "OK"
	StatusReady    = "READY"
	StatusDegraded = "DEGRADED"
	StatusFailed   = "FAILED"
	StatusNotReady = "NOT_READY"
)

// Component name constants.
const (
	ComponentDatabase  = "database"
	ComponentRuleCache = "rule_cache"
)

// Default health check configuration values.
const (
	DefaultHealthCheckTimeout = 3 * time.Second
	// DefaultCacheStalenessThreshold is the lenient tolerance used by the K8s readiness probe.
	// Intentionally higher than RuleSyncWorkerConfig.StalenessThreshold (50s default),
	// which is the internal worker metric for detecting stale cache. The readiness probe
	// uses a wider window to avoid unnecessary pod restarts during transient DB outages.
	DefaultCacheStalenessThreshold = 5 * time.Minute
)

// RuleCacheHealthProvider exposes cache health metrics for the readiness probe.
type RuleCacheHealthProvider interface {
	IsReady() bool
	Staleness() time.Duration
	Size() int
}

// PostgresDBProvider abstracts PostgreSQL database access for testability.
// This interface allows mocking the database connection in tests.
type PostgresDBProvider interface {
	GetDB(ctx context.Context) (*sql.DB, error)
	IsConnected() bool
}

// postgresConnectionAdapter adapts *libPostgres.Client to PostgresDBProvider.
type postgresConnectionAdapter struct {
	conn *libPostgres.Client
}

// GetDB returns the underlying database connection.
// The dbresolver.DB returned by lib-commons wraps *sql.DB, so we type assert it.
func (p *postgresConnectionAdapter) GetDB(ctx context.Context) (*sql.DB, error) {
	db, err := p.conn.Resolver(ctx)
	if err != nil {
		return nil, err
	}

	// The dbresolver.DB has PrimaryDBs() method that returns []*sql.DB
	// For health checks, we need to ping the primary connection
	if dbr, ok := db.(interface{ PrimaryDBs() []*sql.DB }); ok {
		primaries := dbr.PrimaryDBs()
		if len(primaries) == 0 || primaries[0] == nil {
			return nil, ErrConnectionFailed
		}

		return primaries[0], nil
	}

	return nil, ErrConnectionFailed
}

// IsConnected returns whether the connection is established.
func (p *postgresConnectionAdapter) IsConnected() bool {
	if p.conn == nil {
		return false
	}

	connected, err := p.conn.IsConnected()

	return err == nil && connected
}

// HealthChecker holds the connection pools for dependency health checks.
type HealthChecker struct {
	dbProvider              PostgresDBProvider
	timeout                 time.Duration
	cacheHealth             RuleCacheHealthProvider
	cacheStalenessThreshold time.Duration
}

// NewHealthChecker creates a new HealthChecker instance with connection pools.
// Uses DefaultHealthCheckTimeout (3s) which is suitable for liveness probes.
func NewHealthChecker(postgresConn *libPostgres.Client) *HealthChecker {
	var provider PostgresDBProvider

	if postgresConn != nil {
		provider = &postgresConnectionAdapter{conn: postgresConn}
	}

	return &HealthChecker{
		dbProvider:              provider,
		timeout:                 DefaultHealthCheckTimeout,
		cacheStalenessThreshold: DefaultCacheStalenessThreshold,
	}
}

// NewTestableHealthChecker creates a HealthChecker with an injectable PostgresDBProvider.
// This constructor is intended for testing, allowing mock database connections.
func NewTestableHealthChecker(provider PostgresDBProvider) *HealthChecker {
	return &HealthChecker{
		dbProvider:              provider,
		timeout:                 DefaultHealthCheckTimeout,
		cacheStalenessThreshold: DefaultCacheStalenessThreshold,
	}
}

// SetCacheHealthProvider attaches a cache health provider to the health checker.
// Must be called after cache warm-up completes.
func (h *HealthChecker) SetCacheHealthProvider(provider RuleCacheHealthProvider) {
	h.cacheHealth = provider
}

// ReadinessHandler returns a handler that checks all dependencies.
// Creates a tracing span if tracer is available in context.
//
//	@Summary		Readiness check
//	@Description	Check if the service is ready to accept traffic by verifying all dependencies
//	@ID				getReadiness
//	@Tags			health
//	@Accept			json
//	@Produce		json
//	@Success		200	{object}	api.ReadinessResponse	"Service is ready"
//	@Failure		503	{object}	api.ReadinessResponse	"Service is not ready"
//	@Router			/ready [get]
func (h *HealthChecker) ReadinessHandler() fiber.Handler {
	return func(c *fiber.Ctx) error {
		ctx, cancel := context.WithTimeout(c.UserContext(), h.timeout)
		defer cancel()

		_, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

		ctx, span := tracer.Start(ctx, "handler.health.readiness")
		defer span.End()

		// Initialize explicitly to ensure JSON serializes as [] not null
		checks := make([]api.HealthCheck, 0, 2)

		allOK := true

		// Check database
		dbStatus := h.checkPostgres(ctx)

		checks = append(checks, dbStatus)

		if dbStatus.Status != StatusOK {
			allOK = false
		}

		// Check rule cache
		cacheCheck := h.checkRuleCache(ctx)
		checks = append(checks, cacheCheck)

		response := api.ReadinessResponse{
			Status: StatusReady,
			Checks: checks,
		}

		if !allOK {
			// DB failed — return 503 regardless of cache state
			response.Status = StatusNotReady

			libOtel.HandleSpanError(span, "readiness check failed", ErrDependenciesUnhealthy)

			return pkgHTTP.JSONResponse(c, fiber.StatusServiceUnavailable, response)
		}

		if cacheCheck.Status == StatusFailed {
			// Cache degraded but DB healthy — return 200 DEGRADED (avoids K8s restarts)
			response.Status = StatusDegraded

			return pkgHTTP.OK(c, response)
		}

		return pkgHTTP.OK(c, response)
	}
}

// checkPostgres verifies PostgreSQL connectivity using the existing connection pool.
// Error messages are sanitized to avoid leaking internal details (hostnames, SQL errors).
// Creates a child span if tracer is available in context.
func (h *HealthChecker) checkPostgres(ctx context.Context) api.HealthCheck {
	//nolint:dogsled // only tracer needed for span creation; logger/headerID/metrics unused here
	_, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "repository.postgresql.health_check")
	defer span.End()

	status := api.HealthCheck{
		Component: ComponentDatabase,
		Status:    StatusOK,
	}

	// Check if provider is available and connected
	if h.dbProvider == nil || !h.dbProvider.IsConnected() {
		status.Status = StatusFailed
		status.Message = ErrConnectionNotEstablished.Error()

		libOtel.HandleSpanError(span, status.Message, ErrConnectionNotEstablished)

		return status
	}

	db, err := h.dbProvider.GetDB(ctx)
	if err != nil {
		status.Status = StatusFailed
		status.Message = ErrConnectionFailed.Error()

		libOtel.HandleSpanError(span, status.Message, ErrConnectionFailed)

		return status
	}

	if err := db.PingContext(ctx); err != nil {
		status.Status = StatusFailed
		status.Message = ErrPingFailed.Error()

		libOtel.HandleSpanError(span, status.Message, ErrPingFailed)

		return status
	}

	return status
}

// checkRuleCache verifies rule cache health for the readiness probe.
// Returns FAILED if cache is not ready or data is stale beyond threshold.
// Returns OK if cache is healthy or not configured.
// Creates a child span if tracer is available in context.
func (h *HealthChecker) checkRuleCache(ctx context.Context) api.HealthCheck {
	//nolint:dogsled // only tracer needed for span creation; logger/headerID/metrics unused here
	_, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	_, span := tracer.Start(ctx, "repository.rule_cache.health_check")
	defer span.End()

	if h.cacheHealth == nil {
		return api.HealthCheck{Component: ComponentRuleCache, Status: StatusOK, Message: "cache not configured"}
	}

	if !h.cacheHealth.IsReady() {
		libOtel.HandleSpanError(span, "cache not ready", ErrCacheNotReady)
		return api.HealthCheck{Component: ComponentRuleCache, Status: StatusFailed, Message: "cache not ready"}
	}

	if h.cacheHealth.Staleness() > h.cacheStalenessThreshold {
		libOtel.HandleSpanError(span, "cache data stale", ErrCacheStale)
		return api.HealthCheck{Component: ComponentRuleCache, Status: StatusFailed, Message: "cache data stale"}
	}

	return api.HealthCheck{Component: ComponentRuleCache, Status: StatusOK, Message: ""}
}
