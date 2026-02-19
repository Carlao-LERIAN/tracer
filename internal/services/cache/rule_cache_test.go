// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package cache_test

import (
	"math"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"tracer/internal/services/cache"
	"tracer/internal/testutil"
	"tracer/pkg/clock"
	"tracer/pkg/model"
)

func TestRuleCache_GetActiveRules_EmptyCache(t *testing.T) {
	t.Parallel()

	c := cache.NewRuleCache(clock.New())
	rules := c.GetActiveRules(nil)

	require.NotNil(t, rules, "should return empty slice, not nil")
	assert.Empty(t, rules)
}

func TestRuleCache_GetActiveRules_ScopeFiltering(t *testing.T) {
	t.Parallel()

	accountID := testutil.MustDeterministicUUID(100)
	portfolioID := testutil.MustDeterministicUUID(200)

	globalRule := newTestCachedRule(newTestRule(1))
	accountRule := newTestCachedRule(newTestRuleWithScope(2, model.Scope{
		AccountID: testutil.UUIDPtr(accountID),
	}))
	portfolioRule := newTestCachedRule(newTestRuleWithScope(3, model.Scope{
		PortfolioID: testutil.UUIDPtr(portfolioID),
	}))

	tests := []struct {
		name          string
		txScope       *model.Scope
		expectedCount int
		description   string
	}{
		{
			name:          "nil scope returns all rules",
			txScope:       nil,
			expectedCount: 3,
			description:   "nil txScope is wildcard",
		},
		{
			name:          "matching account scope",
			txScope:       &model.Scope{AccountID: testutil.UUIDPtr(accountID)},
			expectedCount: 2, // global + account-scoped
			description:   "global rules always match + account rule matches",
		},
		{
			name:          "matching portfolio scope",
			txScope:       &model.Scope{PortfolioID: testutil.UUIDPtr(portfolioID)},
			expectedCount: 2, // global + portfolio-scoped
			description:   "global rules always match + portfolio rule matches",
		},
		{
			name:          "non-matching scope",
			txScope:       &model.Scope{AccountID: testutil.UUIDPtr(testutil.MustDeterministicUUID(999))},
			expectedCount: 1, // global only
			description:   "only global rules match unrelated scope",
		},
		{
			name:          "empty scope returns only global",
			txScope:       &model.Scope{},
			expectedCount: 1,
			description:   "empty scope (all-nil fields) matches only rules with no scope constraints",
		},
	}

	// Precondition: globalRule must have no scopes (empty slice = global rule)
	require.Empty(t, globalRule.Rule.Scopes, "precondition: global rule must have no scopes")

	c := cache.NewRuleCache(clock.New())
	c.SetRules([]*cache.CachedRule{globalRule, accountRule, portfolioRule})
	c.MarkReady()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rules := c.GetActiveRules(tt.txScope)
			assert.Len(t, rules, tt.expectedCount, tt.description)
		})
	}
}

func TestRuleCache_GetActiveRules_ScopeDirection(t *testing.T) {
	t.Parallel()

	// Rule scoped to account A (nil fields = wildcard)
	accountA := testutil.MustDeterministicUUID(10)
	accountB := testutil.MustDeterministicUUID(20)

	ruleForA := newTestCachedRule(newTestRuleWithScope(1, model.Scope{
		AccountID: testutil.UUIDPtr(accountA),
	}))

	c := cache.NewRuleCache(clock.New())
	c.SetRules([]*cache.CachedRule{ruleForA})
	c.MarkReady()

	// Transaction from account A should match
	rulesA := c.GetActiveRules(&model.Scope{AccountID: testutil.UUIDPtr(accountA)})
	assert.Len(t, rulesA, 1, "rule scoped to A should match transaction from A")

	// Transaction from account B should NOT match
	rulesB := c.GetActiveRules(&model.Scope{AccountID: testutil.UUIDPtr(accountB)})
	assert.Empty(t, rulesB, "rule scoped to A should NOT match transaction from B")
}

func TestRuleCache_GetActiveRules_DeepCopy(t *testing.T) {
	t.Parallel()

	accountID := testutil.MustDeterministicUUID(100)
	rule := newTestCachedRule(newTestRuleWithScope(1, model.Scope{
		AccountID: testutil.UUIDPtr(accountID),
	}))
	desc := "original description"
	rule.Rule.Description = &desc
	base := testutil.FixedTime()
	activatedAt := base
	rule.Rule.ActivatedAt = &activatedAt
	deactivatedAt := base.Add(-1 * time.Hour)
	rule.Rule.DeactivatedAt = &deactivatedAt
	deletedAt := base.Add(-2 * time.Hour)
	rule.Rule.DeletedAt = &deletedAt
	segmentID := testutil.MustDeterministicUUID(300)
	rule.Rule.Scopes[0].SegmentID = testutil.UUIDPtr(segmentID)
	portfolioID := testutil.MustDeterministicUUID(400)
	rule.Rule.Scopes[0].PortfolioID = testutil.UUIDPtr(portfolioID)
	merchantID := testutil.MustDeterministicUUID(200)
	rule.Rule.Scopes[0].MerchantID = testutil.UUIDPtr(merchantID)
	txType := model.TransactionTypeCard
	rule.Rule.Scopes[0].TransactionType = &txType
	subType := "online"
	rule.Rule.Scopes[0].SubType = &subType

	c := cache.NewRuleCache(clock.New())
	c.SetRules([]*cache.CachedRule{rule})
	c.MarkReady()

	// Get rules and modify returned data
	rules1 := c.GetActiveRules(nil)
	require.Len(t, rules1, 1)

	// Mutate string field
	rules1[0].Rule.Name = "MUTATED"
	// Mutate *string pointer field
	*rules1[0].Rule.Description = "MUTATED-DESC"
	// Mutate *time.Time pointer fields
	*rules1[0].Rule.ActivatedAt = time.Time{}
	*rules1[0].Rule.DeactivatedAt = time.Time{}
	*rules1[0].Rule.DeletedAt = time.Time{}
	// Mutate scope UUID pointers
	mutatedID := testutil.MustDeterministicUUID(999)
	*rules1[0].Rule.Scopes[0].AccountID = mutatedID
	mutatedSegment := testutil.MustDeterministicUUID(997)
	*rules1[0].Rule.Scopes[0].SegmentID = mutatedSegment
	mutatedPortfolio := testutil.MustDeterministicUUID(996)
	*rules1[0].Rule.Scopes[0].PortfolioID = mutatedPortfolio
	mutatedMerchant := testutil.MustDeterministicUUID(998)
	*rules1[0].Rule.Scopes[0].MerchantID = mutatedMerchant
	// Mutate scope TransactionType pointer
	mutatedTxType := model.TransactionTypeWire
	*rules1[0].Rule.Scopes[0].TransactionType = mutatedTxType
	// Mutate scope SubType pointer
	*rules1[0].Rule.Scopes[0].SubType = "MUTATED-SUBTYPE"

	// Get again — should NOT see any mutation
	rules2 := c.GetActiveRules(nil)
	require.Len(t, rules2, 1)
	assert.NotEqual(t, "MUTATED", rules2[0].Rule.Name,
		"string field mutation must NOT propagate to cache")
	assert.Equal(t, "original description", *rules2[0].Rule.Description,
		"*string pointer mutation must NOT propagate to cache")
	assert.Equal(t, activatedAt, *rules2[0].Rule.ActivatedAt,
		"*time.Time (ActivatedAt) pointer mutation must NOT propagate to cache")
	assert.Equal(t, deactivatedAt, *rules2[0].Rule.DeactivatedAt,
		"*time.Time (DeactivatedAt) pointer mutation must NOT propagate to cache")
	assert.Equal(t, deletedAt, *rules2[0].Rule.DeletedAt,
		"*time.Time (DeletedAt) pointer mutation must NOT propagate to cache")
	assert.Equal(t, accountID, *rules2[0].Rule.Scopes[0].AccountID,
		"scope AccountID pointer mutation must NOT propagate to cache")
	assert.Equal(t, segmentID, *rules2[0].Rule.Scopes[0].SegmentID,
		"scope SegmentID pointer mutation must NOT propagate to cache")
	assert.Equal(t, portfolioID, *rules2[0].Rule.Scopes[0].PortfolioID,
		"scope PortfolioID pointer mutation must NOT propagate to cache")
	assert.Equal(t, merchantID, *rules2[0].Rule.Scopes[0].MerchantID,
		"scope MerchantID pointer mutation must NOT propagate to cache")
	assert.Equal(t, model.TransactionTypeCard, *rules2[0].Rule.Scopes[0].TransactionType,
		"scope TransactionType pointer mutation must NOT propagate to cache")
	assert.Equal(t, "online", *rules2[0].Rule.Scopes[0].SubType,
		"scope SubType pointer mutation must NOT propagate to cache")
}

func TestRuleCache_SetRules_PopulatesCache(t *testing.T) {
	t.Parallel()

	rules := []*cache.CachedRule{
		newTestCachedRule(newTestRule(1)),
		newTestCachedRule(newTestRule(2)),
		newTestCachedRule(newTestRule(3)),
	}

	c := cache.NewRuleCache(clock.New())
	c.SetRules(rules)

	assertCacheSize(t, c, 3)
}

func TestRuleCache_SetRules_NilSlice(t *testing.T) {
	t.Parallel()

	c := cache.NewRuleCache(clock.New())
	c.SetRules([]*cache.CachedRule{newTestCachedRule(newTestRule(1))})
	assertCacheSize(t, c, 1)

	// SetRules(nil) should clear the cache
	c.SetRules(nil)
	assertCacheSize(t, c, 0)
}

func TestRuleCache_SetRules_EmptySlice(t *testing.T) {
	t.Parallel()

	c := cache.NewRuleCache(clock.New())
	c.SetRules([]*cache.CachedRule{newTestCachedRule(newTestRule(1))})
	assertCacheSize(t, c, 1)

	// SetRules with empty slice should clear the cache
	c.SetRules([]*cache.CachedRule{})
	assertCacheSize(t, c, 0)
}

func TestRuleCache_SetRules_DuplicateID(t *testing.T) {
	t.Parallel()

	rule1a := newTestCachedRule(newTestRule(1))
	rule1a.Rule.Name = "first"
	rule1b := newTestCachedRule(newTestRule(1)) // same ID
	rule1b.Rule.Name = "second"

	c := cache.NewRuleCache(clock.New())
	c.SetRules([]*cache.CachedRule{rule1a, rule1b})

	// Last one wins (map semantics)
	assertCacheSize(t, c, 1)
	rules := c.GetActiveRules(nil)
	assert.Equal(t, "second", rules[0].Rule.Name, "last entry with same ID should win")
}

func TestRuleCache_SetRules_NilElement(t *testing.T) {
	t.Parallel()

	c := cache.NewRuleCache(clock.New())
	// Should not panic when slice contains nil element
	c.SetRules([]*cache.CachedRule{nil, newTestCachedRule(newTestRule(1))})
	assertCacheSize(t, c, 1)
}

func TestRuleCache_SetRules_NilInnerRule(t *testing.T) {
	t.Parallel()

	c := cache.NewRuleCache(clock.New())
	// Should not panic when CachedRule has nil inner Rule
	c.SetRules([]*cache.CachedRule{{Rule: nil}, newTestCachedRule(newTestRule(1))})
	assertCacheSize(t, c, 1)
}

func TestRuleCache_SetRules_ReplacesExisting(t *testing.T) {
	t.Parallel()

	c := cache.NewRuleCache(clock.New())
	c.SetRules([]*cache.CachedRule{
		newTestCachedRule(newTestRule(1)),
		newTestCachedRule(newTestRule(2)),
	})
	assertCacheSize(t, c, 2)

	// Replace with different rules
	c.SetRules([]*cache.CachedRule{newTestCachedRule(newTestRule(3))})
	assertCacheSize(t, c, 1)
	assertCacheContains(t, c, testutil.MustDeterministicUUID(3))
}

func TestRuleCache_ApplyChanges_InsertUpdateRemove(t *testing.T) {
	t.Parallel()

	rule1 := newTestCachedRule(newTestRule(1))
	rule2 := newTestCachedRule(newTestRule(2))

	c := cache.NewRuleCache(clock.New())
	c.SetRules([]*cache.CachedRule{rule1, rule2})
	assertCacheSize(t, c, 2)

	// Apply delta: insert rule3, update rule1, remove rule2
	rule3 := newTestCachedRule(newTestRule(3))
	updatedRule1 := newTestCachedRule(newTestRule(1))
	updatedRule1.Rule.Name = "updated-rule-1"

	c.ApplyChanges(
		[]*cache.CachedRule{updatedRule1, rule3},
		[]uuid.UUID{rule2.Rule.ID},
	)

	assertCacheSize(t, c, 2) // rule1 (updated) + rule3
	assertCacheContains(t, c, rule1.Rule.ID, rule3.Rule.ID)

	// Verify the update actually took effect (not just ID presence)
	rules := c.GetActiveRules(nil)
	for _, r := range rules {
		if r.Rule.ID == rule1.Rule.ID {
			assert.Equal(t, "updated-rule-1", r.Rule.Name, "update should have changed rule name")
		}
	}
}

func TestRuleCache_ApplyChanges_NilNil(t *testing.T) {
	t.Parallel()

	c := cache.NewRuleCache(clock.New())
	c.SetRules([]*cache.CachedRule{newTestCachedRule(newTestRule(1))})
	assertCacheSize(t, c, 1)

	// ApplyChanges(nil, nil) should be a no-op
	c.ApplyChanges(nil, nil)
	assertCacheSize(t, c, 1)
}

func TestRuleCache_ApplyChanges_RemoveOnly(t *testing.T) {
	t.Parallel()

	rule1 := newTestCachedRule(newTestRule(1))
	rule2 := newTestCachedRule(newTestRule(2))

	c := cache.NewRuleCache(clock.New())
	c.SetRules([]*cache.CachedRule{rule1, rule2})
	assertCacheSize(t, c, 2)

	c.ApplyChanges(nil, []uuid.UUID{rule1.Rule.ID})
	assertCacheSize(t, c, 1)
	assertCacheContains(t, c, rule2.Rule.ID)
}

func TestRuleCache_ApplyChanges_UpsertAndRemoveSameID(t *testing.T) {
	t.Parallel()

	rule1 := newTestCachedRule(newTestRule(1))
	c := cache.NewRuleCache(clock.New())
	c.SetRules([]*cache.CachedRule{rule1})

	// Upsert rule1 (update) and remove rule1 in same call
	// Upserts processed first, then removes → rule is removed
	updatedRule1 := newTestCachedRule(newTestRule(1))
	updatedRule1.Rule.Name = "updated"
	c.ApplyChanges([]*cache.CachedRule{updatedRule1}, []uuid.UUID{rule1.Rule.ID})

	assertCacheSize(t, c, 0)
}

func TestRuleCache_ApplyChanges_NilUpsertElement(t *testing.T) {
	t.Parallel()

	c := cache.NewRuleCache(clock.New())
	c.SetRules([]*cache.CachedRule{newTestCachedRule(newTestRule(1))})

	// Should skip nil upsert without panic
	c.ApplyChanges([]*cache.CachedRule{nil, newTestCachedRule(newTestRule(2))}, nil)
	assertCacheSize(t, c, 2)
}

func TestRuleCache_ApplyChanges_RemoveNonExistent(t *testing.T) {
	t.Parallel()

	c := cache.NewRuleCache(clock.New())
	c.SetRules([]*cache.CachedRule{newTestCachedRule(newTestRule(1))})
	assertCacheSize(t, c, 1)

	// Removing a non-existent ID should not panic
	c.ApplyChanges(nil, []uuid.UUID{testutil.MustDeterministicUUID(999)})
	assertCacheSize(t, c, 1)
}

func TestRuleCache_ConcurrentReadWrite(t *testing.T) {
	t.Parallel()

	c := cache.NewRuleCache(clock.New())
	c.SetRules([]*cache.CachedRule{newTestCachedRule(newTestRule(1))})
	c.MarkReady()

	var wg sync.WaitGroup

	// Writer goroutine
	wg.Add(1)

	go func() {
		defer wg.Done()

		for i := range 100 {
			rule := newTestCachedRule(newTestRule(int64(i + 10)))
			c.ApplyChanges([]*cache.CachedRule{rule}, nil)
		}
	}()

	// Reader goroutines
	for range 10 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for range 100 {
				rules := c.GetActiveRules(nil)
				assert.NotNil(t, rules, "GetActiveRules must never return nil")
				assert.GreaterOrEqual(t, c.Size(), 1, "size must be at least 1")
				assert.True(t, c.IsReady(), "cache should remain ready during concurrent access")
			}
		}()
	}

	wg.Wait()

	// Final assertions after all goroutines complete
	assert.True(t, c.IsReady(), "cache should remain ready after concurrent access")
	assert.GreaterOrEqual(t, c.Size(), 1, "cache should have at least 1 rule")
}

func TestRuleCache_ReadyFlag(t *testing.T) {
	t.Parallel()

	c := cache.NewRuleCache(clock.New())

	// Not ready initially
	assertCacheNotReady(t, c)

	// Ready after MarkReady
	c.MarkReady()
	assertCacheReady(t, c)
}

// LastSyncTime and Size
func TestRuleCache_LastSyncTime(t *testing.T) {
	t.Parallel()

	mockClk := testutil.NewDefaultMockClock()
	c := cache.NewRuleCache(mockClk)

	// Initially zero
	assert.True(t, c.LastSyncTime().IsZero(), "lastSyncTime should be zero initially")

	// Updates after SetRules
	c.SetRules([]*cache.CachedRule{newTestCachedRule(newTestRule(1))})
	assert.Equal(t, mockClk.Now(), c.LastSyncTime(), "lastSyncTime should update after SetRules")

	assertCacheSize(t, c, 1)
}

func TestRuleCache_Staleness_NeverSynced(t *testing.T) {
	t.Parallel()

	c := cache.NewRuleCache(clock.New())

	// Never-synced cache should return math.MaxInt64
	assert.Equal(t, time.Duration(math.MaxInt64), c.Staleness(),
		"never-synced cache should return max staleness")
}

func TestRuleCache_Staleness_AfterSync(t *testing.T) {
	t.Parallel()

	mockClk := testutil.NewDefaultMockClock()
	c := cache.NewRuleCache(mockClk)

	c.SetRules([]*cache.CachedRule{newTestCachedRule(newTestRule(1))})

	// With mock clock (fixed time), staleness is always 0 immediately after sync
	// because clock.Now().Sub(lastSyncTime) == 0 when both use the same fixed time
	assert.Equal(t, time.Duration(0), c.Staleness(),
		"staleness immediately after sync should be 0 with fixed mock clock")
}

// Staleness tracking tests for circuit breaker degradation

func TestStaleness_StaleCache(t *testing.T) {
	t.Parallel()

	baseTime := testutil.FixedTime()
	// Pointer to MockClock required: test mutates FixedTime to simulate clock advance
	clk := &testutil.MockClock{FixedTime: baseTime}
	ruleCache := cache.NewRuleCache(clk)
	ruleCache.SetRules(nil)
	ruleCache.MarkReady()

	// Advance clock past staleness threshold (50s)
	clk.FixedTime = baseTime.Add(60 * time.Second)

	staleness := ruleCache.Staleness()
	assert.Equal(t, 60*time.Second, staleness,
		"staleness should be exactly 60s (MockClock is deterministic)")
}

func TestStaleness_VeryStaleCache(t *testing.T) {
	t.Parallel()

	baseTime := testutil.FixedTime()
	clk := &testutil.MockClock{FixedTime: baseTime}
	ruleCache := cache.NewRuleCache(clk)
	ruleCache.SetRules(nil)
	ruleCache.MarkReady()

	// Advance clock way past threshold (5 minutes)
	clk.FixedTime = baseTime.Add(5 * time.Minute)

	// Should still be DEGRADED, NOT "NOT_READY" (avoids K8s restart)
	assert.True(t, ruleCache.IsReady(), "very stale cache should still report ready (DEGRADED, not NOT_READY)")
	assert.Equal(t, 5*time.Minute, ruleCache.Staleness())
}

func TestStaleness_Recovery(t *testing.T) {
	t.Parallel()

	baseTime := testutil.FixedTime()
	clk := &testutil.MockClock{FixedTime: baseTime}
	ruleCache := cache.NewRuleCache(clk)
	ruleCache.SetRules(nil)
	ruleCache.MarkReady()

	// Advance clock past staleness threshold
	clk.FixedTime = baseTime.Add(60 * time.Second)
	assert.Equal(t, 60*time.Second, ruleCache.Staleness())

	// Simulate recovery: successful sync touches cache
	ruleCache.ApplyChanges(nil, nil) // updates lastSyncTime

	// Staleness should reset to exactly 0 (MockClock is fixed)
	assert.Equal(t, time.Duration(0), ruleCache.Staleness(),
		"staleness should reset after successful sync")
}

func TestHealth_StaleCacheRemainsReady(t *testing.T) {
	t.Parallel()

	// This test verifies the health checker contract from T-001:
	// DEGRADED state means IsReady()=true AND Staleness()>=threshold.
	// The health handler (T-001) maps this to HTTP 200 (not 503) to avoid K8s restart.

	baseTime := testutil.FixedTime()
	clk := &testutil.MockClock{FixedTime: baseTime}
	ruleCache := cache.NewRuleCache(clk)
	ruleCache.SetRules(nil)
	ruleCache.MarkReady()

	// Advance past threshold
	clk.FixedTime = baseTime.Add(60 * time.Second)

	// Cache is ready (was populated) but stale
	assert.True(t, ruleCache.IsReady(), "cache should report ready even when stale")
	assert.Equal(t, 60*time.Second, ruleCache.Staleness(), "cache should be stale")
}
