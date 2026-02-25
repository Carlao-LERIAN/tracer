// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package cache_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"tracer/internal/services/cache"
	"tracer/pkg/clock"
	"tracer/pkg/constant"
	"tracer/pkg/model"
)

func TestCacheAdapter_ImplementsActiveRulesRepository(t *testing.T) {
	t.Parallel()

	c := cache.NewRuleCache(clock.New())
	adapter, err := cache.NewCacheAdapter(c)
	require.NoError(t, err)

	// Compile-time interface check
	var _ interface {
		GetActiveRules(ctx context.Context, txScope *model.Scope) ([]*model.Rule, error)
	} = adapter
}

func TestCacheAdapter_DelegatesGetActiveRules(t *testing.T) {
	t.Parallel()

	rule := newTestRule(1)
	cachedRule := newTestCachedRule(rule)

	c := cache.NewRuleCache(clock.New())
	c.SetRules([]*cache.CachedRule{cachedRule})
	c.MarkReady()

	adapter, err := cache.NewCacheAdapter(c)
	require.NoError(t, err)

	ctx := context.Background()
	rules, err := adapter.GetActiveRules(ctx, nil)

	require.NoError(t, err)
	require.Len(t, rules, 1)
	assert.Equal(t, rule.ID, rules[0].ID)
	assert.Equal(t, rule.Name, rules[0].Name)
}

func TestCacheAdapter_NotReady_ReturnsError(t *testing.T) {
	t.Parallel()

	c := cache.NewRuleCache(clock.New())
	adapter, adapterErr := cache.NewCacheAdapter(c)
	require.NoError(t, adapterErr)

	// Cache not ready — adapter should return specific error
	ctx := context.Background()
	rules, err := adapter.GetActiveRules(ctx, nil)

	require.Error(t, err, "should return error when cache is not ready")
	assert.ErrorIs(t, err, constant.ErrRuleCacheNotReady)
	assert.Nil(t, rules)
}

func TestHealthProvider_NotReady(t *testing.T) {
	t.Parallel()

	c := cache.NewRuleCache(clock.New())

	assert.False(t, c.IsReady())
}

func TestHealthProvider_Ready(t *testing.T) {
	t.Parallel()

	c := cache.NewRuleCache(clock.New())
	c.SetRules([]*cache.CachedRule{newTestCachedRule(newTestRule(1))})
	c.MarkReady()

	assert.True(t, c.IsReady())
	assert.Equal(t, 1, c.Size())
}

func TestCacheAdapter_SetsCompiledProgram(t *testing.T) {
	t.Parallel()

	rule := newTestRule(1)
	program := "compiled-program-stub"
	cachedRule := &cache.CachedRule{Rule: rule, Program: program}

	c := cache.NewRuleCache(clock.New())
	c.SetRules([]*cache.CachedRule{cachedRule})
	c.MarkReady()

	adapter, err := cache.NewCacheAdapter(c)
	require.NoError(t, err)

	ctx := context.Background()
	rules, err := adapter.GetActiveRules(ctx, nil)

	require.NoError(t, err)
	require.Len(t, rules, 1)
	assert.Equal(t, program, rules[0].CompiledProgram,
		"CacheAdapter should set CompiledProgram from CachedRule.Program")
}

func TestNewCacheAdapter_NilCache_ReturnsError(t *testing.T) {
	t.Parallel()

	adapter, err := cache.NewCacheAdapter(nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, cache.ErrNilCache)
	assert.Nil(t, adapter)
}
