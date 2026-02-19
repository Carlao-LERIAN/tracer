// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package cache

import (
	"math"
	"sync"
	"time"

	"github.com/google/uuid"

	"tracer/pkg/clock"
	"tracer/pkg/model"
)

// RuleCache provides thread-safe in-memory storage for compiled rules.
// Read operations use RLock for maximum concurrency.
// Write operations use full Lock.
type RuleCache struct {
	mu           sync.RWMutex
	rules        map[uuid.UUID]*CachedRule
	ready        bool
	lastSyncTime time.Time
	clock        clock.Clock
}

// NewRuleCache creates a new empty rule cache.
func NewRuleCache(clk clock.Clock) *RuleCache {
	if clk == nil {
		clk = clock.New()
	}

	return &RuleCache{
		rules: make(map[uuid.UUID]*CachedRule),
		clock: clk,
	}
}

// GetActiveRules returns cached rules matching the given scope.
// If txScope is nil, returns all rules (bypasses scope filtering).
// Otherwise, returns rules whose scopes match txScope via model.RuleScopesMatch.
// Returns deep copies to prevent concurrent mutation.
func (c *RuleCache) GetActiveRules(txScope *model.Scope) []*CachedRule {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make([]*CachedRule, 0, len(c.rules))

	for _, cached := range c.rules {
		if cached == nil || cached.Rule == nil {
			continue
		}

		// Scope filtering: ruleScope.Matches(txScope) — direction is critical
		if txScope == nil || model.RuleScopesMatch(cached.Rule.Scopes, txScope) {
			copied := deepCopy(cached)
			if copied == nil {
				continue // defense-in-depth: deepCopy returns nil only for nil input (guarded above)
			}

			result = append(result, copied)
		}
	}

	return result
}

// SetRules replaces all rules in the cache (full reload).
func (c *RuleCache) SetRules(rules []*CachedRule) {
	c.mu.Lock()
	defer c.mu.Unlock()

	newRules := make(map[uuid.UUID]*CachedRule, len(rules))
	for _, r := range rules {
		if r == nil || r.Rule == nil {
			continue
		}

		newRules[r.Rule.ID] = r
	}

	c.rules = newRules
	c.lastSyncTime = c.clock.Now()
}

// ApplyChanges applies a delta: upserts rules and removes by ID.
func (c *RuleCache) ApplyChanges(upserts []*CachedRule, removeIDs []uuid.UUID) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, r := range upserts {
		if r == nil || r.Rule == nil {
			continue
		}

		c.rules[r.Rule.ID] = r
	}

	for _, id := range removeIDs {
		delete(c.rules, id)
	}

	c.lastSyncTime = c.clock.Now()
}

// MarkReady signals that the cache has been populated.
func (c *RuleCache) MarkReady() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.ready = true
}

// IsReady returns true if the cache has been populated at least once.
func (c *RuleCache) IsReady() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.ready
}

// Size returns the number of rules in the cache.
func (c *RuleCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.rules)
}

// LastSyncTime returns when the cache was last successfully updated.
func (c *RuleCache) LastSyncTime() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.lastSyncTime
}

// Staleness returns how long since the last successful sync.
// Returns math.MaxInt64 if the cache was never synced (zero lastSyncTime).
func (c *RuleCache) Staleness() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.lastSyncTime.IsZero() {
		return time.Duration(math.MaxInt64)
	}

	return c.clock.Now().Sub(c.lastSyncTime)
}

// deepCopy creates a deep copy of a CachedRule to prevent concurrent mutation.
// Copies ALL pointer fields: Rule-level (*string, *time.Time) and Scope-level (*uuid.UUID, *TransactionType, *string).
func deepCopy(src *CachedRule) *CachedRule {
	if src == nil || src.Rule == nil {
		return nil
	}

	ruleCopy := *src.Rule

	// Deep copy Rule pointer fields
	if src.Rule.Description != nil {
		desc := *src.Rule.Description
		ruleCopy.Description = &desc
	}

	if src.Rule.ActivatedAt != nil {
		t := *src.Rule.ActivatedAt
		ruleCopy.ActivatedAt = &t
	}

	if src.Rule.DeactivatedAt != nil {
		t := *src.Rule.DeactivatedAt
		ruleCopy.DeactivatedAt = &t
	}

	if src.Rule.DeletedAt != nil {
		t := *src.Rule.DeletedAt
		ruleCopy.DeletedAt = &t
	}

	// Deep copy scopes slice with all pointer fields
	if src.Rule.Scopes != nil {
		scopesCopy := make([]model.Scope, len(src.Rule.Scopes))
		for i, s := range src.Rule.Scopes {
			scopesCopy[i] = s

			if s.SegmentID != nil {
				id := *s.SegmentID
				scopesCopy[i].SegmentID = &id
			}

			if s.PortfolioID != nil {
				id := *s.PortfolioID
				scopesCopy[i].PortfolioID = &id
			}

			if s.AccountID != nil {
				id := *s.AccountID
				scopesCopy[i].AccountID = &id
			}

			if s.MerchantID != nil {
				id := *s.MerchantID
				scopesCopy[i].MerchantID = &id
			}

			if s.TransactionType != nil {
				tt := *s.TransactionType
				scopesCopy[i].TransactionType = &tt
			}

			if s.SubType != nil {
				st := *s.SubType
				scopesCopy[i].SubType = &st
			}
		}

		ruleCopy.Scopes = scopesCopy
	}

	return &CachedRule{
		Rule: &ruleCopy,
		// Program is shared, not cloned: compiled CEL programs are immutable after compilation.
		Program: src.Program,
	}
}
