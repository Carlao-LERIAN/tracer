// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package workers

//go:generate mockgen -source=usage_counter_cleanup_repository.go -destination=mocks/usage_counter_cleanup_repository_mock.go -package=mocks

import (
	"context"
	"time"
)

// UsageCounterCleanupRepository defines the interface for usage counter cleanup operations.
// This is a subset of query.UsageCounterRepository, containing only the methods needed
// for the cleanup worker.
type UsageCounterCleanupRepository interface {
	// DeleteExpiredCounters removes usage counters that haven't been updated since the specified time.
	// This is used for cleanup of old period counters that are no longer relevant.
	// Returns the number of deleted counters.
	DeleteExpiredCounters(ctx context.Context, olderThan time.Time) (int64, error)
}
