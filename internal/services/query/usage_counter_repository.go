// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package query

//go:generate mockgen -source=usage_counter_repository.go -destination=usage_counter_repository_mock.go -package=query

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"tracer/pkg/model"
)

// UsageCounterRepository defines the interface for usage counter persistence in queries.
// Used by the limit checking service to atomically track and update usage.
type UsageCounterRepository interface {
	// GetForUpdate retrieves an existing usage counter with row-level lock.
	// Uses SELECT FOR UPDATE to prevent race conditions during concurrent operations.
	// Returns sql.ErrNoRows if the counter doesn't exist (does NOT create one).
	// Used by rollback operations where creating a counter would be incorrect.
	GetForUpdate(ctx context.Context, limitID uuid.UUID, scopeKey, periodKey string) (*model.UsageCounter, error)

	// GetOrCreateForUpdate retrieves or creates a usage counter with row-level lock.
	// Uses SELECT FOR UPDATE to prevent race conditions during concurrent increments.
	// If the counter doesn't exist, it creates one with currentUsage=0.
	// Returns the counter with the lock held (caller must commit/rollback transaction).
	GetOrCreateForUpdate(ctx context.Context, limitID uuid.UUID, scopeKey, periodKey string) (*model.UsageCounter, error)

	// IncrementAtomic atomically increments the usage counter.
	// Must be called within the same transaction as GetOrCreateForUpdate.
	IncrementAtomic(ctx context.Context, counterID uuid.UUID, amount decimal.Decimal) error

	// DecrementAtomic atomically decrements the usage counter for rollback operations.
	// Must be called within the same transaction as GetOrCreateForUpdate.
	// Returns constant.ErrUsageCounterCurrentUsageNegative if decrement would result in negative usage.
	DecrementAtomic(ctx context.Context, counterID uuid.UUID, amount decimal.Decimal) error

	// UpsertAndIncrementAtomic performs an atomic INSERT ... ON CONFLICT DO UPDATE
	// to create or increment a usage counter. Returns the new current_usage.
	// Returns ErrUsageCounterExceedsLimit if the increment would exceed maxAmount.
	// IMPORTANT: Caller MUST pre-check amount > maxAmount before calling (INSERT path has no WHERE guard).
	// The expiresAt parameter specifies when the counter should be eligible for cleanup.
	// If expiresAt is nil, the counter will never be automatically deleted (fail-safe behavior).
	UpsertAndIncrementAtomic(ctx context.Context, limitID uuid.UUID, scopeKey string, periodKey string, amount decimal.Decimal, maxAmount decimal.Decimal, expiresAt *time.Time) (decimal.Decimal, error)

	// GetByLimitID retrieves all usage counters for a specific limit.
	// Used for the GET /limits/{id}/usage endpoint.
	// Returns empty slice if no counters exist.
	GetByLimitID(ctx context.Context, limitID uuid.UUID) ([]model.UsageCounter, error)

	// GetUsageForLimits retrieves current usage for multiple limits in a single query.
	// Used for checking multiple limits efficiently without N+1 queries.
	// scopeKey and periodKey are used to filter relevant counters.
	// Returns a map of limitID -> currentUsage. Missing entries mean usage is 0.
	GetUsageForLimits(ctx context.Context, limitIDs []uuid.UUID, scopeKey, periodKey string) (map[uuid.UUID]decimal.Decimal, error)

	// DeleteExpiredCounters removes usage counters where expires_at < now.
	// Counters with NULL expires_at are preserved (never deleted).
	// This provides more accurate cleanup based on when counters should actually expire
	// rather than when they were last updated.
	// Returns the number of deleted counters.
	DeleteExpiredCounters(ctx context.Context, now time.Time) (int64, error)
}
