// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package workers

//go:generate mockgen -source=rule_sync_cache.go -destination=mocks/rule_sync_cache_mock.go -package=mocks

import (
	"time"

	"github.com/google/uuid"

	"tracer/internal/services/cache"
	"tracer/pkg/model"
)

// RuleSyncCache defines the cache operations needed by the sync worker.
// Combines read (for ClassifyChanges) and write (for applying deltas).
// Satisfied by *cache.RuleCache.
type RuleSyncCache interface {
	// GetActiveRules returns cached rules matching the given scope.
	// Pass nil to get all cached rules (used to build ClassifyChanges map).
	GetActiveRules(txScope *model.Scope) []*cache.CachedRule

	// ApplyChanges applies a delta: upserts rules and removes by ID.
	ApplyChanges(upserts []*cache.CachedRule, removeIDs []uuid.UUID)

	// LastSyncTime returns when the cache was last successfully updated.
	// Used to initialize the worker's lastSync from the warm-up timestamp.
	LastSyncTime() time.Time

	// Size returns the number of rules currently in the cache.
	// Used for observability metrics (cache size gauge).
	Size() int
}
