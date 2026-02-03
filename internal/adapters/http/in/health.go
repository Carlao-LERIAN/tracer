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

	libCommons "github.com/LerianStudio/lib-commons/v2/commons"
	libHTTP "github.com/LerianStudio/lib-commons/v2/commons/net/http"
	libOtel "github.com/LerianStudio/lib-commons/v2/commons/opentelemetry"
	libPostgres "github.com/LerianStudio/lib-commons/v2/commons/postgres"
	"github.com/gofiber/fiber/v2"

	"tracer/api"
)

// Sentinel errors for health check failures.
var (
	ErrConnectionNotEstablished = errors.New("connection not established")
	ErrConnectionFailed         = errors.New("connection failed")
	ErrPingFailed               = errors.New("ping failed")
	ErrDependenciesUnhealthy    = errors.New("dependencies unhealthy")
)

// Health check status constants.
const (
	StatusOK       = "OK"
	StatusReady    = "READY"
	StatusFailed   = "FAILED"
	StatusNotReady = "NOT_READY"
)

// Component name constants.
const (
	ComponentDatabase = "database"
)

// Default health check configuration values.
const (
	DefaultHealthCheckTimeout = 3 * time.Second
)

// PostgresDBProvider abstracts PostgreSQL database access for testability.
// This interface allows mocking the database connection in tests.
type PostgresDBProvider interface {
	GetDB() (*sql.DB, error)
	IsConnected() bool
}

// postgresConnectionAdapter adapts *libPostgres.PostgresConnection to PostgresDBProvider.
type postgresConnectionAdapter struct {
	conn *libPostgres.PostgresConnection
}

// GetDB returns the underlying database connection.
// The dbresolver.DB returned by lib-commons wraps *sql.DB, so we type assert it.
func (p *postgresConnectionAdapter) GetDB() (*sql.DB, error) {
	db, err := p.conn.GetDB()
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
	return p.conn != nil && p.conn.Connected
}

// HealthChecker holds the connection pools for dependency health checks.
type HealthChecker struct {
	dbProvider PostgresDBProvider
	timeout    time.Duration
}

// NewHealthChecker creates a new HealthChecker instance with connection pools.
// Uses DefaultHealthCheckTimeout (3s) which is suitable for liveness probes.
func NewHealthChecker(postgresConn *libPostgres.PostgresConnection) *HealthChecker {
	var provider PostgresDBProvider

	if postgresConn != nil {
		provider = &postgresConnectionAdapter{conn: postgresConn}
	}

	return &HealthChecker{
		dbProvider: provider,
		timeout:    DefaultHealthCheckTimeout,
	}
}

// NewTestableHealthChecker creates a HealthChecker with an injectable PostgresDBProvider.
// This constructor is intended for testing, allowing mock database connections.
func NewTestableHealthChecker(provider PostgresDBProvider) *HealthChecker {
	return &HealthChecker{
		dbProvider: provider,
		timeout:    DefaultHealthCheckTimeout,
	}
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
		checks := make([]api.HealthCheck, 0, 1)

		allOK := true

		// Check database
		dbStatus := h.checkPostgres(ctx)

		checks = append(checks, dbStatus)

		if dbStatus.Status != StatusOK {
			allOK = false
		}

		response := api.ReadinessResponse{
			Status: StatusReady,
			Checks: checks,
		}

		if !allOK {
			response.Status = StatusNotReady

			libOtel.HandleSpanError(&span, "readiness check failed", ErrDependenciesUnhealthy)

			return libHTTP.JSONResponse(c, fiber.StatusServiceUnavailable, response)
		}

		return libHTTP.OK(c, response)
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

		libOtel.HandleSpanError(&span, status.Message, ErrConnectionNotEstablished)

		return status
	}

	db, err := h.dbProvider.GetDB()
	if err != nil {
		status.Status = StatusFailed
		status.Message = ErrConnectionFailed.Error()

		libOtel.HandleSpanError(&span, status.Message, ErrConnectionFailed)

		return status
	}

	if err := db.PingContext(ctx); err != nil {
		status.Status = StatusFailed
		status.Message = ErrPingFailed.Error()

		libOtel.HandleSpanError(&span, status.Message, ErrPingFailed)

		return status
	}

	return status
}
