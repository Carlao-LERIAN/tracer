// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package cache

import (
	"time"

	"github.com/google/uuid"

	"tracer/pkg/model"
)

// RuleCacheReader provides read-only access to the rule cache.
type RuleCacheReader interface {
	// GetActiveRules returns active rules matching the given scope.
	// If txScope is nil, returns all active rules.
	// Returns deep copies — modifying returned slice does NOT mutate cache state.
	GetActiveRules(txScope *model.Scope) []*CachedRule

	// IsReady returns true if the cache has been populated at least once.
	IsReady() bool

	// Size returns the number of rules in the cache.
	Size() int

	// LastSyncTime returns when the cache was last successfully updated.
	LastSyncTime() time.Time
}

// RuleCacheWriter provides write access to the rule cache.
type RuleCacheWriter interface {
	// SetRules replaces all rules in the cache (full reload).
	SetRules(rules []*CachedRule)

	// ApplyChanges applies a delta: inserts/updates and removes.
	ApplyChanges(upserts []*CachedRule, removeIDs []uuid.UUID)

	// MarkReady signals that the cache has been populated.
	MarkReady()
}

// RuleCacheHealthProvider exposes cache health for the readiness probe.
// Implemented by RuleCache, consumed by the health checker.
type RuleCacheHealthProvider interface {
	IsReady() bool
	Staleness() time.Duration
	Size() int
}
