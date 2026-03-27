// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package bootstrap

import (
	"context"

	libCommons "github.com/LerianStudio/lib-commons/v4/commons"
	libLog "github.com/LerianStudio/lib-commons/v4/commons/log"
	libPostgres "github.com/LerianStudio/lib-commons/v4/commons/postgres"

	"tracer/internal/services/workers"
)

// Service is the application glue where we put all top level components to be used.
type Service struct {
	*HTTPServer
	libLog.Logger
	postgresConn  *libPostgres.Client
	cleanupWorker *workers.UsageCleanupWorker
	syncWorker    *workers.RuleSyncWorker
}

// Run starts the application.
// This is the only necessary code to run an app in main.go
func (app *Service) Run() {
	opts := []libCommons.LauncherOption{
		libCommons.WithLogger(app.Logger),
		libCommons.RunApp("HTTP Service", app.HTTPServer),
	}

	// Register cleanup worker with Launcher if configured
	if app.cleanupWorker != nil {
		opts = append(opts, libCommons.RunApp("Usage Cleanup Worker", app.cleanupWorker))
	}

	// Register rule sync worker with Launcher
	if app.syncWorker != nil {
		opts = append(opts, libCommons.RunApp("Rule Sync Worker", app.syncWorker))
	}

	// Run all services (blocks until shutdown)
	libCommons.NewLauncher(opts...).Run()
}

// Shutdown gracefully shuts down the application.
func (app *Service) Shutdown(ctx context.Context) error {
	logger, _, _, _ := libCommons.NewTrackingFromContext(ctx) //nolint:dogsled

	if app.HTTPServer != nil && app.app != nil {
		if err := app.app.ShutdownWithContext(ctx); err != nil {
			logger.With(
				libLog.String("service.name", "HTTP Service"),
				libLog.String("error.message", err.Error()),
			).Log(ctx, libLog.LevelError, "failed to shutdown HTTP server")

			return err
		}
	}

	// The cleanup worker uses signal.NotifyContext for graceful shutdown.
	// When running via Launcher, shutdown is coordinated through OS signals.
	// For programmatic shutdown scenarios, the worker stops when its context is cancelled.
	if app.cleanupWorker != nil {
		logger.With(
			libLog.String("service.name", "Usage Cleanup Worker"),
		).Log(ctx, libLog.LevelInfo, "cleanup worker shutdown is managed by Launcher via OS signals")
	}

	if app.syncWorker != nil {
		logger.With(
			libLog.String("service.name", "Rule Sync Worker"),
		).Log(ctx, libLog.LevelInfo, "rule sync worker shutdown is managed by Launcher via OS signals")
	}

	// Close the PostgreSQL connection pool to release database connections.
	// This is critical for repeated restarts (e.g., integration tests with
	// RestartServerWithConfig) to avoid exhausting the database's max_connections.
	if app.postgresConn != nil {
		if err := app.postgresConn.Close(); err != nil {
			logger.With(
				libLog.String("error.message", err.Error()),
			).Log(ctx, libLog.LevelWarn, "Failed to close PostgreSQL connection pool")
		}
	}

	return nil
}
