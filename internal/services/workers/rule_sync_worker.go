// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package workers

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	libCommons "github.com/LerianStudio/lib-commons/v2/commons"
	libLog "github.com/LerianStudio/lib-commons/v2/commons/log"
	libOtel "github.com/LerianStudio/lib-commons/v2/commons/opentelemetry"
	"github.com/google/uuid"
	"github.com/sony/gobreaker"

	"tracer/internal/services/cache"
	"tracer/pkg/clock"
	"tracer/pkg/logging"
	"tracer/pkg/model"
	"tracer/pkg/resilience"
)

// RuleSyncWorker periodically polls the database for rule changes
// and applies deltas to the in-memory cache with CEL recompilation.
// Implements libCommons.App interface for Launcher integration.
type RuleSyncWorker struct {
	cache          RuleSyncCache
	repo           RuleSyncRepository
	compiler       ExpressionCompiler
	config         RuleSyncWorkerConfig
	logger         libLog.Logger
	clock          clock.Clock
	lastSync       time.Time
	circuitBreaker *resilience.CircuitBreaker // nil = no circuit breaker
}

// NewRuleSyncWorker creates a new rule sync worker.
// Returns ErrNilRuleCache if cache is nil.
// Returns ErrNilRepository if repo is nil.
// Returns ErrNilExpressionCompiler if compiler is nil.
// Returns ErrNilLogger if logger is nil.
// Returns ErrInvalidPollInterval if PollInterval <= 0.
// Returns ErrInvalidStalenessThreshold if StalenessThreshold <= 0.
// Returns ErrInvalidOverlapBuffer if OverlapBuffer < 0.
// The clk parameter is optional; if nil, uses clock.RealClock{}.
func NewRuleSyncWorker(
	ruleCache RuleSyncCache,
	repo RuleSyncRepository,
	compiler ExpressionCompiler,
	config RuleSyncWorkerConfig,
	logger libLog.Logger,
	clk clock.Clock,
) (*RuleSyncWorker, error) {
	if ruleCache == nil {
		return nil, ErrNilRuleCache
	}

	if repo == nil {
		return nil, ErrNilRepository
	}

	if compiler == nil {
		return nil, ErrNilExpressionCompiler
	}

	if logger == nil {
		return nil, ErrNilLogger
	}

	if config.PollInterval <= 0 {
		return nil, ErrInvalidPollInterval
	}

	if config.StalenessThreshold <= 0 {
		return nil, ErrInvalidStalenessThreshold
	}

	if config.OverlapBuffer < 0 {
		return nil, ErrInvalidOverlapBuffer
	}

	if clk == nil {
		clk = clock.RealClock{}
	}

	return &RuleSyncWorker{
		cache:    ruleCache,
		repo:     repo,
		compiler: compiler,
		config:   config,
		logger:   logger,
		clock:    clk,
	}, nil
}

// Run implements the libCommons.App interface for Launcher integration.
// Handles OS signals (SIGINT, SIGTERM) for graceful shutdown.
func (w *RuleSyncWorker) Run(_ *libCommons.Launcher) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return w.runLoop(ctx)
}

// RunWithContext runs the worker with a provided context.
// Useful for testing or external orchestration.
func (w *RuleSyncWorker) RunWithContext(ctx context.Context) error {
	return w.runLoop(ctx)
}

// runLoop is the internal loop that handles sync cycles.
func (w *RuleSyncWorker) runLoop(ctx context.Context) error {
	// Initialize lastSync from cache's warm-up timestamp
	w.lastSync = w.cache.LastSyncTime()

	w.logger.WithFields(
		"operation", "worker.rule_sync.run",
		"poll_interval", w.config.PollInterval.String(),
		"overlap_buffer", w.config.OverlapBuffer.String(),
		"last_sync", w.lastSync.Format(time.RFC3339),
	).Info("Starting rule sync worker")

	tickerChan, stopTicker := w.clock.NewTicker(w.config.PollInterval)
	defer stopTicker()

	for {
		select {
		case <-ctx.Done():
			w.logger.WithFields(
				"operation", "worker.rule_sync.run",
			).Info("Rule sync worker stopped")

			return nil

		case <-tickerChan:
			w.runSyncCycle(ctx)
		}
	}
}

// runSyncCycle executes a single poll-classify-compile-apply cycle.
// Errors are logged but not returned — the worker continues running.
func (w *RuleSyncWorker) runSyncCycle(ctx context.Context) {
	_, tracer, _, _ := libCommons.NewTrackingFromContext(ctx) //nolint:dogsled

	ctx, span := tracer.Start(ctx, "worker.rule_sync.sync_cycle")
	defer span.End()

	logger := logging.WithTrace(ctx, w.logger)

	logger.WithFields(
		"operation", "worker.rule_sync.sync_cycle",
		"last_sync", w.lastSync.Format(time.RFC3339),
	).Info("Running rule sync cycle")

	// 1. Query delta with overlap buffer (circuit breaker protected)
	since := w.lastSync.Add(-w.config.OverlapBuffer)

	fetched, err := w.queryDelta(ctx, since)
	if err != nil {
		// Circuit breaker open or half-open rejecting: skip cycle, serve stale cache
		if errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests) {
			logger.WithFields(
				"operation", "worker.rule_sync.sync_cycle",
				"circuit_breaker.state", "open_or_half_open",
			).Warn("Circuit breaker rejecting request, skipping sync cycle - serving stale cache")

			return
		}

		// Context cancellation: normal during shutdown, not a real failure
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			logger.WithFields(
				"operation", "worker.rule_sync.sync_cycle",
			).Info("Sync cycle interrupted by context cancellation")

			return
		}

		libOtel.HandleSpanError(&span, "Delta query failed", err)
		logger.WithFields(
			"operation", "worker.rule_sync.sync_cycle",
			"error.message", err.Error(),
		).Error("Failed to query rule changes")

		return // lastSync NOT updated on error
	}

	// 2. If no results, touch cache staleness and update lastSync
	if len(fetched) == 0 {
		w.cache.ApplyChanges(nil, nil)
		w.lastSync = w.clock.Now()

		logger.WithFields(
			"operation", "worker.rule_sync.sync_cycle",
		).Info("No rule changes detected")

		return
	}

	// 3. Get current cache state for classification
	allCached := w.cache.GetActiveRules(nil)
	cachedMap := make(map[uuid.UUID]*cache.CachedRule, len(allCached))

	for _, r := range allCached {
		if r == nil || r.Rule == nil {
			continue
		}

		cachedMap[r.Rule.ID] = r
	}

	// 4. Classify changes
	changes := ClassifyChanges(cachedMap, fetched)

	if changes.IsEmpty() {
		w.cache.ApplyChanges(nil, nil)
		w.updateLastSync(fetched)

		logger.WithFields(
			"operation", "worker.rule_sync.sync_cycle",
			"fetched_count", len(fetched),
		).Info("Overlap buffer: all changes already applied")

		return
	}

	// 5. Compile CEL for new + updated rules
	toCompile := make([]*model.Rule, 0, len(changes.New)+len(changes.Updated))
	toCompile = append(toCompile, changes.New...)
	toCompile = append(toCompile, changes.Updated...)

	upserts := make([]*cache.CachedRule, 0, len(toCompile))

	for _, rule := range toCompile {
		var program any

		compiled, compileErr := w.compiler.Compile(ctx, rule.Expression)
		if compileErr != nil {
			logger.WithFields(
				"operation", "worker.rule_sync.compile",
				"rule_id", rule.ID.String(),
				"error.message", compileErr.Error(),
			).Warn("CEL compilation failed for rule, using nil program")
		} else {
			program = compiled
		}

		upserts = append(upserts, &cache.CachedRule{
			Rule:    rule,
			Program: program,
		})
	}

	// 6. Apply changes to cache
	w.cache.ApplyChanges(upserts, changes.Deleted)

	// 7. Update lastSync
	w.updateLastSync(fetched)

	logger.WithFields(
		"operation", "worker.rule_sync.sync_cycle",
		"new_count", len(changes.New),
		"updated_count", len(changes.Updated),
		"deleted_count", len(changes.Deleted),
	).Info("Rule sync cycle completed")
}

// updateLastSync sets lastSync to the maximum UpdatedAt from fetched results.
// If no UpdatedAt exceeds current lastSync (overlap-only), advances to clock.Now()
// to prevent stagnation.
//
// NOTE: The empty guard below is defense-in-depth. Currently unreachable because
// callers (runSyncCycle) only invoke updateLastSync when len(fetched) > 0.
// Kept as a safety net against future refactoring.
func (w *RuleSyncWorker) updateLastSync(fetched []*model.Rule) {
	if len(fetched) == 0 {
		w.lastSync = w.clock.Now()
		return
	}

	maxTime := w.lastSync

	for _, r := range fetched {
		if r == nil {
			continue
		}

		if r.UpdatedAt.After(maxTime) {
			maxTime = r.UpdatedAt
		}
	}

	// If no rule advanced past lastSync (overlap-only re-fetch),
	// advance to clock.Now() to break stagnation.
	if maxTime.Equal(w.lastSync) {
		w.lastSync = w.clock.Now()
		return
	}

	w.lastSync = maxTime
}

// SetCircuitBreaker configures the circuit breaker for the sync worker.
// Must be called before Run/RunWithContext. If not called, the worker
// operates without circuit breaker protection.
// Passing nil disables circuit breaker protection (logged as warning).
func (w *RuleSyncWorker) SetCircuitBreaker(cb *resilience.CircuitBreaker) {
	if cb == nil {
		w.logger.WithFields(
			"operation", "worker.rule_sync.set_circuit_breaker",
		).Warn("SetCircuitBreaker called with nil - circuit breaker protection disabled")
	}

	w.circuitBreaker = cb
}

// queryDelta executes the delta query, optionally wrapped in circuit breaker.
func (w *RuleSyncWorker) queryDelta(ctx context.Context, since time.Time) ([]*model.Rule, error) {
	if w.circuitBreaker == nil {
		return w.repo.GetRulesUpdatedSince(ctx, since)
	}

	result, err := w.circuitBreaker.Execute(ctx, func() (any, error) {
		return w.repo.GetRulesUpdatedSince(ctx, since)
	})
	if err != nil {
		return nil, err
	}

	// Use comma-ok pattern to safely handle nil result from Execute.
	// When repo returns (nil, nil), Execute returns (nil, nil) and
	// a bare type assertion on nil interface would panic.
	rules, ok := result.([]*model.Rule)
	if !ok {
		if result != nil {
			// This should never happen -- indicates a programming error in Execute wrapper
			w.logger.WithFields(
				"operation", "worker.rule_sync.query_delta",
				"result_type", fmt.Sprintf("%T", result),
			).Error("Unexpected type from circuit breaker Execute - returning nil rules")
		}

		return nil, nil
	}

	return rules, nil
}
