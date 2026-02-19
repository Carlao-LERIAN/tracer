// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package cache

import (
	"context"
	"fmt"
	"time"

	libLog "github.com/LerianStudio/lib-commons/v2/commons/log"

	"tracer/pkg/clock"
	"tracer/pkg/constant"
)

// WarmUp loads all active rules from the database, compiles their CEL expressions,
// populates the cache, and marks it as ready.
// MUST complete successfully before the instance reports READY.
// Uses the provided clock for timing (enables deterministic tests).
func WarmUp(ctx context.Context, c *RuleCache, repo RuleSyncRepository, compiler ExpressionCompiler, logger libLog.Logger, clk clock.Clock) (int, time.Duration, error) {
	if clk == nil {
		clk = clock.New()
	}

	start := clk.Now()

	if c == nil {
		return 0, 0, ErrNilCache
	}

	if repo == nil {
		return 0, 0, ErrNilRepository
	}

	if compiler == nil {
		return 0, 0, ErrNilCompiler
	}

	if logger == nil {
		return 0, 0, ErrNilLogger
	}

	logger.WithFields(
		"operation", "cache.warmup",
	).Info("Starting rule cache warm-up")

	rules, err := repo.GetAllActiveRules(ctx)
	if err != nil {
		return 0, clk.Now().Sub(start), fmt.Errorf("%w: %w", constant.ErrRuleCacheWarmUpFailed, err)
	}

	cachedRules := make([]*CachedRule, 0, len(rules))

	for _, rule := range rules {
		if err := ctx.Err(); err != nil {
			return 0, clk.Now().Sub(start), fmt.Errorf("%w: %w", constant.ErrRuleCacheWarmUpFailed, err)
		}

		if rule == nil {
			logger.WithFields(
				"operation", "cache.warmup",
			).Warn("Skipping nil rule from repository")

			continue
		}

		program, compileErr := compiler.Compile(ctx, rule.Expression)
		if compileErr != nil {
			logger.WithFields(
				"operation", "cache.warmup",
				"rule.id", rule.ID.String(),
				"error.message", compileErr.Error(),
			).Error("Failed to compile rule expression — skipping rule")

			continue
		}

		cachedRules = append(cachedRules, &CachedRule{
			Rule:    rule,
			Program: program,
		})
	}

	// Fail if ALL rules failed to compile (total failure)
	if len(rules) > 0 && len(cachedRules) == 0 {
		return 0, clk.Now().Sub(start), fmt.Errorf("%w: all %d rules failed to compile", constant.ErrRuleCacheWarmUpFailed, len(rules))
	}

	c.SetRules(cachedRules)
	c.MarkReady()

	duration := clk.Now().Sub(start)

	logger.WithFields(
		"operation", "cache.warmup",
		"rules.total", len(rules),
		"rules.cached", len(cachedRules),
		"rules.skipped", len(rules)-len(cachedRules),
		"duration_ms", duration.Milliseconds(),
	).Info("Rule cache warm-up completed")

	return len(cachedRules), duration, nil
}
