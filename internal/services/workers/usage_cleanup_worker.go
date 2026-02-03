// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package workers

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	libCommons "github.com/LerianStudio/lib-commons/v2/commons"
	libLog "github.com/LerianStudio/lib-commons/v2/commons/log"
	libOtel "github.com/LerianStudio/lib-commons/v2/commons/opentelemetry"

	"tracer/pkg/clock"
	"tracer/pkg/logging"
)

// UsageCleanupWorkerConfig holds configuration for the cleanup worker.
type UsageCleanupWorkerConfig struct {
	// CleanupInterval is how often the cleanup runs (default: 24 hours).
	CleanupInterval time.Duration
	// RetentionPeriod is how long to keep counters before deletion (default: 90 days).
	RetentionPeriod time.Duration
}

// DefaultUsageCleanupWorkerConfig returns default configuration values.
func DefaultUsageCleanupWorkerConfig() UsageCleanupWorkerConfig {
	return UsageCleanupWorkerConfig{
		CleanupInterval: 24 * time.Hour,
		RetentionPeriod: 90 * 24 * time.Hour,
	}
}

// UsageCleanupWorker periodically cleans up expired usage counters.
// It runs in the background and deletes counters that haven't been updated
// within the retention period.
// Implements libCommons.App interface for Launcher integration.
type UsageCleanupWorker struct {
	repo   UsageCounterCleanupRepository
	config UsageCleanupWorkerConfig
	logger libLog.Logger
	clock  clock.Clock
}

// NewUsageCleanupWorker creates a new cleanup worker.
// Returns ErrNilRepository if repo is nil.
// Returns ErrNilLogger if logger is nil.
// Returns ErrInvalidCleanupInterval if CleanupInterval <= 0.
// Returns ErrInvalidRetentionPeriod if RetentionPeriod <= 0.
// The clk parameter is optional; if nil, uses clock.RealClock{}.
func NewUsageCleanupWorker(repo UsageCounterCleanupRepository, config UsageCleanupWorkerConfig, logger libLog.Logger, clk clock.Clock) (*UsageCleanupWorker, error) {
	if repo == nil {
		return nil, ErrNilRepository
	}

	if logger == nil {
		return nil, ErrNilLogger
	}

	if config.CleanupInterval <= 0 {
		return nil, ErrInvalidCleanupInterval
	}

	if config.RetentionPeriod <= 0 {
		return nil, ErrInvalidRetentionPeriod
	}

	if clk == nil {
		clk = clock.RealClock{}
	}

	return &UsageCleanupWorker{
		repo:   repo,
		config: config,
		logger: logger,
		clock:  clk,
	}, nil
}

// Run implements the libCommons.App interface for Launcher integration.
// Handles OS signals (SIGINT, SIGTERM) for graceful shutdown.
// Cleanup errors are logged but do not stop the worker.
func (w *UsageCleanupWorker) Run(_ *libCommons.Launcher) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return w.runLoop(ctx)
}

// RunWithContext runs the worker with a provided context.
// This is useful for testing or external orchestration (e.g., K8s CronJob).
func (w *UsageCleanupWorker) RunWithContext(ctx context.Context) error {
	return w.runLoop(ctx)
}

// runLoop is the internal loop that handles cleanup cycles.
func (w *UsageCleanupWorker) runLoop(ctx context.Context) error {
	w.logger.WithFields(
		"operation", "worker.usage_cleanup.run",
		"cleanup_interval", w.config.CleanupInterval.String(),
		"retention_period", w.config.RetentionPeriod.String(),
	).Info("Starting usage cleanup worker")

	// Use injected clock's ticker for deterministic testing
	tickerChan, stopTicker := w.clock.NewTicker(w.config.CleanupInterval)
	defer stopTicker()

	// Run cleanup immediately on start, then on interval
	// Check for cancellation before initial cleanup to avoid work after shutdown
	select {
	case <-ctx.Done():
		w.logger.WithFields(
			"operation", "worker.usage_cleanup.run",
		).Info("Usage cleanup worker stopped before initial cycle")

		return nil
	default:
		w.runCleanupCycle(ctx)
	}

	for {
		select {
		case <-ctx.Done():
			w.logger.WithFields(
				"operation", "worker.usage_cleanup.run",
			).Info("Usage cleanup worker stopped")

			return nil

		case <-tickerChan:
			w.runCleanupCycle(ctx)
		}
	}
}

// runCleanupCycle executes a single cleanup and logs the result.
// Errors are logged but not returned - the worker continues running.
func (w *UsageCleanupWorker) runCleanupCycle(ctx context.Context) {
	_, tracer, _, _ := libCommons.NewTrackingFromContext(ctx) //nolint:dogsled

	ctx, span := tracer.Start(ctx, "worker.usage_cleanup.run_cycle")
	defer span.End()

	// Use w.logger (guaranteed non-nil) instead of context logger which may be empty
	logger := logging.WithTrace(ctx, w.logger)

	logger.WithFields(
		"operation", "worker.usage_cleanup.run_cycle",
	).Info("Running usage counter cleanup cycle")

	deleted, err := w.RunOnce(ctx)
	if err != nil {
		libOtel.HandleSpanError(&span, "Cleanup cycle failed", err)
		logger.WithFields(
			"operation", "worker.usage_cleanup.run_cycle",
			"error.message", err.Error(),
		).Error("Failed to cleanup expired counters")

		return
	}

	logger.WithFields(
		"operation", "worker.usage_cleanup.run_cycle",
		"deleted_count", deleted,
	).Info("Cleanup cycle completed successfully")
}

// RunOnce executes a single cleanup operation.
// Returns the number of deleted counters.
// This method can be called directly for manual/on-demand cleanup,
// or used by external schedulers (e.g., K8s CronJob).
func (w *UsageCleanupWorker) RunOnce(ctx context.Context) (int64, error) {
	_, tracer, _, _ := libCommons.NewTrackingFromContext(ctx) //nolint:dogsled

	ctx, span := tracer.Start(ctx, "worker.usage_cleanup.run_once")
	defer span.End()

	logger := logging.WithTrace(ctx, w.logger)

	// Calculate the cutoff time based on retention period
	olderThan := w.clock.Now().UTC().Add(-w.config.RetentionPeriod)

	logger.WithFields(
		"operation", "worker.usage_cleanup.run_once",
		"older_than", olderThan.Format(time.RFC3339),
		"retention_period", w.config.RetentionPeriod.String(),
	).Info("Deleting expired usage counters")

	deleted, err := w.repo.DeleteExpiredCounters(ctx, olderThan)
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to delete expired counters", err)

		return 0, fmt.Errorf("failed to delete expired counters: %w", err)
	}

	logger.WithFields(
		"operation", "worker.usage_cleanup.run_once",
		"deleted_count", deleted,
	).Info("Deleted expired usage counters")

	return deleted, nil
}
