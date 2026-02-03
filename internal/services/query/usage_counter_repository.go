// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package query

//go:generate mockgen -source=usage_counter_repository.go -destination=usage_counter_repository_mock.go -package=query

import (
	"context"
	"time"

	"github.com/google/uuid"

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
	// Returns constant.ErrUsageCounterOverflow if increment would cause overflow.
	IncrementAtomic(ctx context.Context, counterID uuid.UUID, amount int64) error

	// DecrementAtomic atomically decrements the usage counter for rollback operations.
	// Must be called within the same transaction as GetOrCreateForUpdate.
	// Returns constant.ErrUsageCounterCurrentUsageNegative if decrement would result in negative usage.
	DecrementAtomic(ctx context.Context, counterID uuid.UUID, amount int64) error

	// GetByLimitID retrieves all usage counters for a specific limit.
	// Used for the GET /limits/{id}/usage endpoint.
	// Returns empty slice if no counters exist.
	GetByLimitID(ctx context.Context, limitID uuid.UUID) ([]model.UsageCounter, error)

	// GetUsageForLimits retrieves current usage for multiple limits in a single query.
	// Used for checking multiple limits efficiently without N+1 queries.
	// scopeKey and periodKey are used to filter relevant counters.
	// Returns a map of limitID -> currentUsage. Missing entries mean usage is 0.
	GetUsageForLimits(ctx context.Context, limitIDs []uuid.UUID, scopeKey, periodKey string) (map[uuid.UUID]int64, error)

	// DeleteExpiredCounters removes usage counters that haven't been updated since the specified time.
	// This is used for cleanup of old period counters that are no longer relevant.
	// Period counters become stale when a new period starts (e.g., new day for DAILY, new month for MONTHLY).
	// The automatic reset happens via periodKey: new periods create new counters, old ones need cleanup.
	// Returns the number of deleted counters.
	DeleteExpiredCounters(ctx context.Context, olderThan time.Time) (int64, error)
}
