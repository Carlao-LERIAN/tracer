// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package bootstrap

import (
	"context"

	libCommons "github.com/LerianStudio/lib-commons/v2/commons"
	libLog "github.com/LerianStudio/lib-commons/v2/commons/log"

	"tracer/internal/services/workers"
)

// Service is the application glue where we put all top level components to be used.
type Service struct {
	*HTTPServer
	libLog.Logger
	cleanupWorker *workers.UsageCleanupWorker
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

	// Run all services (blocks until shutdown)
	libCommons.NewLauncher(opts...).Run()
}

// Shutdown gracefully shuts down the application.
func (app *Service) Shutdown(ctx context.Context) error {
	logger, _, _, _ := libCommons.NewTrackingFromContext(ctx) //nolint:dogsled

	if app.HTTPServer != nil && app.app != nil {
		if err := app.app.ShutdownWithContext(ctx); err != nil {
			logger.WithFields(
				"service.name", "HTTP Service",
				"error.message", err.Error(),
			).Error("failed to shutdown HTTP server")

			return err
		}
	}

	// The cleanup worker uses signal.NotifyContext for graceful shutdown.
	// When running via Launcher, shutdown is coordinated through OS signals.
	// For programmatic shutdown scenarios, the worker stops when its context is cancelled.
	if app.cleanupWorker != nil {
		logger.WithFields(
			"service.name", "Usage Cleanup Worker",
		).Info("cleanup worker shutdown is managed by Launcher via OS signals")
	}

	return nil
}
