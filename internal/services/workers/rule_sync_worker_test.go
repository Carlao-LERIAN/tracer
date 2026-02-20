// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package workers

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"tracer/internal/services/cache"
	"tracer/internal/services/workers/mocks"
	"tracer/internal/testutil"
	"tracer/pkg/model"
)

func TestNewRuleSyncWorker_NilCache(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	repo := mocks.NewMockRuleSyncRepository(ctrl)
	compiler := mocks.NewMockExpressionCompiler(ctrl)
	logger := testutil.NewMockLogger()

	_, err := NewRuleSyncWorker(nil, repo, compiler, DefaultRuleSyncWorkerConfig(), logger, defaultTestCircuitBreaker(), nil)

	require.ErrorIs(t, err, ErrNilRuleCache)
}

func TestNewRuleSyncWorker_NilRepository(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockCache := mocks.NewMockRuleSyncCache(ctrl)
	compiler := mocks.NewMockExpressionCompiler(ctrl)
	logger := testutil.NewMockLogger()

	_, err := NewRuleSyncWorker(mockCache, nil, compiler, DefaultRuleSyncWorkerConfig(), logger, defaultTestCircuitBreaker(), nil)

	require.ErrorIs(t, err, ErrNilRepository)
}

func TestNewRuleSyncWorker_NilCompiler(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockCache := mocks.NewMockRuleSyncCache(ctrl)
	repo := mocks.NewMockRuleSyncRepository(ctrl)
	logger := testutil.NewMockLogger()

	_, err := NewRuleSyncWorker(mockCache, repo, nil, DefaultRuleSyncWorkerConfig(), logger, defaultTestCircuitBreaker(), nil)

	require.ErrorIs(t, err, ErrNilExpressionCompiler)
}

func TestNewRuleSyncWorker_InvalidInterval(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		interval time.Duration
	}{
		{name: "zero interval", interval: 0},
		{name: "negative interval", interval: -5 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			mockCache := mocks.NewMockRuleSyncCache(ctrl)
			repo := mocks.NewMockRuleSyncRepository(ctrl)
			compiler := mocks.NewMockExpressionCompiler(ctrl)
			logger := testutil.NewMockLogger()

			cfg := DefaultRuleSyncWorkerConfig()
			cfg.PollInterval = tt.interval

			_, err := NewRuleSyncWorker(mockCache, repo, compiler, cfg, logger, defaultTestCircuitBreaker(), nil)

			require.ErrorIs(t, err, ErrInvalidPollInterval)
		})
	}
}

func TestNewRuleSyncWorker_NilLogger(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockCache := mocks.NewMockRuleSyncCache(ctrl)
	repo := mocks.NewMockRuleSyncRepository(ctrl)
	compiler := mocks.NewMockExpressionCompiler(ctrl)

	_, err := NewRuleSyncWorker(mockCache, repo, compiler, DefaultRuleSyncWorkerConfig(), nil, defaultTestCircuitBreaker(), nil)

	require.ErrorIs(t, err, ErrNilLogger)
}

func TestNewRuleSyncWorker_NilCircuitBreaker(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockCache := mocks.NewMockRuleSyncCache(ctrl)
	repo := mocks.NewMockRuleSyncRepository(ctrl)
	compiler := mocks.NewMockExpressionCompiler(ctrl)
	logger := testutil.NewMockLogger()

	_, err := NewRuleSyncWorker(mockCache, repo, compiler, DefaultRuleSyncWorkerConfig(), logger, nil, nil)

	require.ErrorIs(t, err, ErrNilCircuitBreaker)
}

func TestNewRuleSyncWorker_InvalidStalenessThreshold(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		threshold time.Duration
	}{
		{name: "zero threshold", threshold: 0},
		{name: "negative threshold", threshold: -10 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			mockCache := mocks.NewMockRuleSyncCache(ctrl)
			repo := mocks.NewMockRuleSyncRepository(ctrl)
			compiler := mocks.NewMockExpressionCompiler(ctrl)
			logger := testutil.NewMockLogger()

			cfg := DefaultRuleSyncWorkerConfig()
			cfg.StalenessThreshold = tt.threshold

			_, err := NewRuleSyncWorker(mockCache, repo, compiler, cfg, logger, defaultTestCircuitBreaker(), nil)

			require.ErrorIs(t, err, ErrInvalidStalenessThreshold)
		})
	}
}

func TestNewRuleSyncWorker_InvalidOverlapBuffer(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockCache := mocks.NewMockRuleSyncCache(ctrl)
	repo := mocks.NewMockRuleSyncRepository(ctrl)
	compiler := mocks.NewMockExpressionCompiler(ctrl)
	logger := testutil.NewMockLogger()

	cfg := DefaultRuleSyncWorkerConfig()
	cfg.OverlapBuffer = -1 * time.Second

	_, err := NewRuleSyncWorker(mockCache, repo, compiler, cfg, logger, defaultTestCircuitBreaker(), nil)

	require.ErrorIs(t, err, ErrInvalidOverlapBuffer)
}

func TestNewRuleSyncWorker_ValidConfig(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockCache := mocks.NewMockRuleSyncCache(ctrl)
	repo := mocks.NewMockRuleSyncRepository(ctrl)
	compiler := mocks.NewMockExpressionCompiler(ctrl)
	logger := testutil.NewMockLogger()

	worker, err := NewRuleSyncWorker(mockCache, repo, compiler, DefaultRuleSyncWorkerConfig(), logger, defaultTestCircuitBreaker(), nil)

	require.NoError(t, err)
	assert.NotNil(t, worker)
}

func TestRuleSyncWorker_GracefulShutdown(t *testing.T) {
	t.Parallel()
	_, cleanup := setupTestTracer(t)
	defer cleanup()

	ctrl := gomock.NewController(t)
	mockCache := mocks.NewMockRuleSyncCache(ctrl)
	repo := mocks.NewMockRuleSyncRepository(ctrl)
	compiler := mocks.NewMockExpressionCompiler(ctrl)
	logger := testutil.NewMockLogger()
	clk := testutil.MockClock{FixedTime: testutil.FixedTime()}

	// Cache returns initial lastSync time
	mockCache.EXPECT().LastSyncTime().Return(testutil.FixedTime()).AnyTimes()

	worker, err := NewRuleSyncWorker(mockCache, repo, compiler, defaultSyncConfig(), logger, defaultTestCircuitBreaker(), clk)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- worker.RunWithContext(ctx)
	}()

	// Cancel context -> worker should exit
	cancel()

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not shut down within timeout")
	}
}

// Sync cycle tests follow (S-013)

func TestRunSyncCycle_DetectsNewRules(t *testing.T) {
	t.Parallel()
	_, cleanup := setupTestTracer(t)
	defer cleanup()

	ctrl := gomock.NewController(t)
	mockCache := mocks.NewMockRuleSyncCache(ctrl)
	repo := mocks.NewMockRuleSyncRepository(ctrl)
	compiler := mocks.NewMockExpressionCompiler(ctrl)
	logger := testutil.NewMockLogger()
	clk := testutil.MockClock{FixedTime: testutil.FixedTime()}

	worker, err := NewRuleSyncWorker(mockCache, repo, compiler, defaultSyncConfig(), logger, defaultTestCircuitBreaker(), clk)
	require.NoError(t, err)
	worker.lastSync = testutil.FixedTime()

	newRule := newSyncTestActiveRule(1)

	// Cache is empty
	mockCache.EXPECT().GetActiveRules(nil).Return(nil)
	mockCache.EXPECT().Size().Return(1).AnyTimes()

	// Delta query returns a new rule
	repo.EXPECT().GetRulesUpdatedSince(gomock.Any(), gomock.Any()).Return([]*model.Rule{newRule}, nil)

	// CEL compilation succeeds
	compiler.EXPECT().Compile(gomock.Any(), newRule.Expression).Return("compiled-program", nil)

	// Cache updated with new rule
	mockCache.EXPECT().ApplyChanges(gomock.Any(), gomock.Len(0)).Do(
		func(upserts []*cache.CachedRule, _ []uuid.UUID) {
			require.Len(t, upserts, 1)
			assert.Equal(t, newRule.ID, upserts[0].Rule.ID)
			assert.Equal(t, "compiled-program", upserts[0].Program)
		},
	)

	worker.runSyncCycle(context.Background())
}

func TestRunSyncCycle_DetectsUpdatedRules(t *testing.T) {
	t.Parallel()
	_, cleanup := setupTestTracer(t)
	defer cleanup()

	ctrl := gomock.NewController(t)
	mockCache := mocks.NewMockRuleSyncCache(ctrl)
	repo := mocks.NewMockRuleSyncRepository(ctrl)
	compiler := mocks.NewMockExpressionCompiler(ctrl)
	logger := testutil.NewMockLogger()
	clk := testutil.MockClock{FixedTime: testutil.FixedTime()}

	worker, err := NewRuleSyncWorker(mockCache, repo, compiler, defaultSyncConfig(), logger, defaultTestCircuitBreaker(), clk)
	require.NoError(t, err)
	worker.lastSync = testutil.FixedTime()

	existingRule := newSyncTestActiveRule(1)
	cachedRule := newSyncTestCachedRule(existingRule)

	mockCache.EXPECT().GetActiveRules(nil).Return([]*cache.CachedRule{cachedRule})
	mockCache.EXPECT().Size().Return(1).AnyTimes()

	// Delta returns same rule with newer UpdatedAt
	updatedRule := newSyncTestActiveRule(1)
	updatedRule.UpdatedAt = existingRule.UpdatedAt.Add(5 * time.Second)
	updatedRule.Expression = "amount > 5000"
	repo.EXPECT().GetRulesUpdatedSince(gomock.Any(), gomock.Any()).Return([]*model.Rule{updatedRule}, nil)

	// CEL recompilation for updated expression
	compiler.EXPECT().Compile(gomock.Any(), updatedRule.Expression).Return("new-compiled", nil)

	// Cache updated
	mockCache.EXPECT().ApplyChanges(gomock.Any(), gomock.Len(0)).Do(
		func(upserts []*cache.CachedRule, _ []uuid.UUID) {
			require.Len(t, upserts, 1)
			assert.Equal(t, updatedRule.ID, upserts[0].Rule.ID)
			assert.Equal(t, "new-compiled", upserts[0].Program)
		},
	)

	worker.runSyncCycle(context.Background())
}

func TestRunSyncCycle_DetectsDeletedRules(t *testing.T) {
	t.Parallel()
	_, cleanup := setupTestTracer(t)
	defer cleanup()

	ctrl := gomock.NewController(t)
	mockCache := mocks.NewMockRuleSyncCache(ctrl)
	repo := mocks.NewMockRuleSyncRepository(ctrl)
	compiler := mocks.NewMockExpressionCompiler(ctrl)
	logger := testutil.NewMockLogger()
	clk := testutil.MockClock{FixedTime: testutil.FixedTime()}

	worker, err := NewRuleSyncWorker(mockCache, repo, compiler, defaultSyncConfig(), logger, defaultTestCircuitBreaker(), clk)
	require.NoError(t, err)
	worker.lastSync = testutil.FixedTime()

	existingRule := newSyncTestActiveRule(1)
	cachedRule := newSyncTestCachedRule(existingRule)

	mockCache.EXPECT().GetActiveRules(nil).Return([]*cache.CachedRule{cachedRule})
	mockCache.EXPECT().Size().Return(0).AnyTimes()

	// Delta returns rule as INACTIVE
	deactivatedRule := newSyncTestRule(1, model.RuleStatusInactive)
	repo.EXPECT().GetRulesUpdatedSince(gomock.Any(), gomock.Any()).Return([]*model.Rule{deactivatedRule}, nil)

	// Cache updated — verify deleted rule identity
	mockCache.EXPECT().ApplyChanges(gomock.Len(0), gomock.Any()).Do(
		func(_ []*cache.CachedRule, removeIDs []uuid.UUID) {
			require.Len(t, removeIDs, 1)
			assert.Equal(t, existingRule.ID, removeIDs[0])
		},
	)

	worker.runSyncCycle(context.Background())
}

func TestRunSyncCycle_NoChanges(t *testing.T) {
	t.Parallel()
	_, cleanup := setupTestTracer(t)
	defer cleanup()

	ctrl := gomock.NewController(t)
	mockCache := mocks.NewMockRuleSyncCache(ctrl)
	repo := mocks.NewMockRuleSyncRepository(ctrl)
	compiler := mocks.NewMockExpressionCompiler(ctrl)
	logger := testutil.NewMockLogger()
	clk := testutil.MockClock{FixedTime: testutil.FixedTime()}

	worker, err := NewRuleSyncWorker(mockCache, repo, compiler, defaultSyncConfig(), logger, defaultTestCircuitBreaker(), clk)
	require.NoError(t, err)
	worker.lastSync = testutil.FixedTime().Add(-10 * time.Second)

	// Delta returns nothing — touches cache staleness and returns before GetActiveRules
	repo.EXPECT().GetRulesUpdatedSince(gomock.Any(), gomock.Any()).Return([]*model.Rule{}, nil)
	mockCache.EXPECT().Size().Return(0).AnyTimes()

	// Cache staleness must be touched even on empty fetch
	mockCache.EXPECT().ApplyChanges(gomock.Nil(), gomock.Nil())

	worker.runSyncCycle(context.Background())

	// lastSync should advance to clock.Now() on empty fetch
	assert.Equal(t, testutil.FixedTime(), worker.lastSync)
}

func TestRunSyncCycle_OverlapBuffer(t *testing.T) {
	t.Parallel()
	_, cleanup := setupTestTracer(t)
	defer cleanup()

	ctrl := gomock.NewController(t)
	mockCache := mocks.NewMockRuleSyncCache(ctrl)
	repo := mocks.NewMockRuleSyncRepository(ctrl)
	compiler := mocks.NewMockExpressionCompiler(ctrl)
	logger := testutil.NewMockLogger()
	fixedNow := testutil.FixedTime()
	clk := testutil.MockClock{FixedTime: fixedNow}

	cfg := defaultSyncConfig()
	cfg.OverlapBuffer = 2 * time.Second

	worker, err := NewRuleSyncWorker(mockCache, repo, compiler, cfg, logger, defaultTestCircuitBreaker(), clk)
	require.NoError(t, err)

	lastSync := fixedNow.Add(-10 * time.Second)
	worker.lastSync = lastSync

	// Key assertion: query uses lastSync MINUS overlapBuffer
	expectedSince := lastSync.Add(-2 * time.Second)
	repo.EXPECT().GetRulesUpdatedSince(gomock.Any(), expectedSince).Return([]*model.Rule{}, nil)
	mockCache.EXPECT().Size().Return(0).AnyTimes()

	// Cache staleness must be touched even on empty fetch
	mockCache.EXPECT().ApplyChanges(gomock.Nil(), gomock.Nil())

	worker.runSyncCycle(context.Background())

	assert.Equal(t, fixedNow, worker.lastSync,
		"empty fetch should advance lastSync to clock.Now()")
}

func TestRunSyncCycle_CELCompilationFailure(t *testing.T) {
	t.Parallel()
	_, cleanup := setupTestTracer(t)
	defer cleanup()

	ctrl := gomock.NewController(t)
	mockCache := mocks.NewMockRuleSyncCache(ctrl)
	repo := mocks.NewMockRuleSyncRepository(ctrl)
	compiler := mocks.NewMockExpressionCompiler(ctrl)
	logger := testutil.NewMockLogger()
	clk := testutil.MockClock{FixedTime: testutil.FixedTime()}

	worker, err := NewRuleSyncWorker(mockCache, repo, compiler, defaultSyncConfig(), logger, defaultTestCircuitBreaker(), clk)
	require.NoError(t, err)
	worker.lastSync = testutil.FixedTime()

	r1 := newSyncTestActiveRule(1) // will fail compilation
	r1.Expression = "bad_syntax >"
	r2 := newSyncTestActiveRule(2) // will succeed
	r2.Expression = "amount > 5000"

	mockCache.EXPECT().GetActiveRules(nil).Return(nil)
	mockCache.EXPECT().Size().Return(2).AnyTimes()
	repo.EXPECT().GetRulesUpdatedSince(gomock.Any(), gomock.Any()).Return([]*model.Rule{r1, r2}, nil)

	// r1 fails compilation, r2 succeeds
	compiler.EXPECT().Compile(gomock.Any(), "bad_syntax >").Return(nil, assert.AnError)
	compiler.EXPECT().Compile(gomock.Any(), "amount > 5000").Return("compiled-r2", nil)

	// Both rules applied — verify r1 has nil program, r2 has compiled program
	mockCache.EXPECT().ApplyChanges(gomock.Any(), gomock.Len(0)).Do(
		func(upserts []*cache.CachedRule, _ []uuid.UUID) {
			require.Len(t, upserts, 2)
			for _, u := range upserts {
				switch u.Rule.ID {
				case r1.ID:
					assert.Nil(t, u.Program, "failed compilation should store nil program")
				case r2.ID:
					assert.Equal(t, "compiled-r2", u.Program, "successful compilation should store program")
				default:
					t.Errorf("unexpected rule ID in upserts: %s", u.Rule.ID)
				}
			}
		},
	)

	worker.runSyncCycle(context.Background())
}

func TestRunSyncCycle_RepoError(t *testing.T) {
	t.Parallel()
	_, cleanup := setupTestTracer(t)
	defer cleanup()

	ctrl := gomock.NewController(t)
	mockCache := mocks.NewMockRuleSyncCache(ctrl)
	repo := mocks.NewMockRuleSyncRepository(ctrl)
	compiler := mocks.NewMockExpressionCompiler(ctrl)
	logger := testutil.NewMockLogger()
	clk := testutil.MockClock{FixedTime: testutil.FixedTime()}

	worker, err := NewRuleSyncWorker(mockCache, repo, compiler, defaultSyncConfig(), logger, defaultTestCircuitBreaker(), clk)
	require.NoError(t, err)

	worker.lastSync = testutil.FixedTime()

	// Repository error — cycle continues (no panic)
	repo.EXPECT().GetRulesUpdatedSince(gomock.Any(), gomock.Any()).Return(nil, assert.AnError)

	worker.runSyncCycle(context.Background())
}

func TestRunSyncCycle_LastSyncUpdatedOnSuccess(t *testing.T) {
	t.Parallel()
	_, cleanup := setupTestTracer(t)
	defer cleanup()

	ctrl := gomock.NewController(t)
	mockCache := mocks.NewMockRuleSyncCache(ctrl)
	repo := mocks.NewMockRuleSyncRepository(ctrl)
	compiler := mocks.NewMockExpressionCompiler(ctrl)
	logger := testutil.NewMockLogger()
	initialTime := testutil.FixedTime()
	clk := testutil.MockClock{FixedTime: initialTime}

	worker, err := NewRuleSyncWorker(mockCache, repo, compiler, defaultSyncConfig(), logger, defaultTestCircuitBreaker(), clk)
	require.NoError(t, err)
	worker.lastSync = initialTime

	newRule := newSyncTestActiveRule(1)
	ruleUpdatedAt := initialTime.Add(3 * time.Second)
	newRule.UpdatedAt = ruleUpdatedAt

	mockCache.EXPECT().GetActiveRules(nil).Return(nil)
	mockCache.EXPECT().Size().Return(1).AnyTimes()
	repo.EXPECT().GetRulesUpdatedSince(gomock.Any(), gomock.Any()).Return([]*model.Rule{newRule}, nil)
	compiler.EXPECT().Compile(gomock.Any(), gomock.Any()).Return("compiled", nil)
	mockCache.EXPECT().ApplyChanges(gomock.Any(), gomock.Any())

	worker.runSyncCycle(context.Background())

	// lastSync should be updated to max(updated_at) from results
	assert.Equal(t, ruleUpdatedAt, worker.lastSync)
}

func TestRunSyncCycle_LastSyncNotUpdatedOnError(t *testing.T) {
	t.Parallel()
	_, cleanup := setupTestTracer(t)
	defer cleanup()

	ctrl := gomock.NewController(t)
	mockCache := mocks.NewMockRuleSyncCache(ctrl)
	repo := mocks.NewMockRuleSyncRepository(ctrl)
	compiler := mocks.NewMockExpressionCompiler(ctrl)
	logger := testutil.NewMockLogger()
	initialTime := testutil.FixedTime()
	clk := testutil.MockClock{FixedTime: initialTime}

	worker, err := NewRuleSyncWorker(mockCache, repo, compiler, defaultSyncConfig(), logger, defaultTestCircuitBreaker(), clk)
	require.NoError(t, err)
	worker.lastSync = initialTime

	repo.EXPECT().GetRulesUpdatedSince(gomock.Any(), gomock.Any()).Return(nil, assert.AnError)

	worker.runSyncCycle(context.Background())

	// lastSync unchanged on error
	assert.Equal(t, initialTime, worker.lastSync)
}

func TestRunSyncCycle_StagnationPrevention(t *testing.T) {
	t.Parallel()
	_, cleanup := setupTestTracer(t)
	defer cleanup()

	ctrl := gomock.NewController(t)
	mockCache := mocks.NewMockRuleSyncCache(ctrl)
	repo := mocks.NewMockRuleSyncRepository(ctrl)
	compiler := mocks.NewMockExpressionCompiler(ctrl)
	logger := testutil.NewMockLogger()
	advancedTime := testutil.FixedTime().Add(10 * time.Second)
	clk := testutil.MockClock{FixedTime: advancedTime}

	worker, err := NewRuleSyncWorker(mockCache, repo, compiler, defaultSyncConfig(), logger, defaultTestCircuitBreaker(), clk)
	require.NoError(t, err)
	worker.lastSync = testutil.FixedTime() // 10s behind clock.Now()

	existingRule := newSyncTestActiveRule(1)
	existingRule.UpdatedAt = testutil.FixedTime()
	cachedRule := newSyncTestCachedRule(existingRule)

	mockCache.EXPECT().GetActiveRules(nil).Return([]*cache.CachedRule{cachedRule})
	mockCache.EXPECT().Size().Return(1).AnyTimes()

	// Delta returns same rule (overlap re-fetch) — UpdatedAt == lastSync
	repo.EXPECT().GetRulesUpdatedSince(gomock.Any(), gomock.Any()).Return([]*model.Rule{existingRule}, nil)

	// Cache staleness must be touched even when changes are empty
	mockCache.EXPECT().ApplyChanges(gomock.Nil(), gomock.Nil())

	worker.runSyncCycle(context.Background())

	// Stagnation prevention: lastSync should advance to clock.Now()
	assert.Equal(t, advancedTime, worker.lastSync,
		"stagnation prevention should advance lastSync to clock.Now()")
}

func TestRunSyncCycle_OverlapClassifyPath(t *testing.T) {
	t.Parallel()
	_, cleanup := setupTestTracer(t)
	defer cleanup()

	ctrl := gomock.NewController(t)
	mockCache := mocks.NewMockRuleSyncCache(ctrl)
	repo := mocks.NewMockRuleSyncRepository(ctrl)
	compiler := mocks.NewMockExpressionCompiler(ctrl)
	logger := testutil.NewMockLogger()
	clk := testutil.MockClock{FixedTime: testutil.FixedTime()}

	worker, err := NewRuleSyncWorker(mockCache, repo, compiler, defaultSyncConfig(), logger, defaultTestCircuitBreaker(), clk)
	require.NoError(t, err)
	worker.lastSync = testutil.FixedTime()

	existingRule := newSyncTestActiveRule(1)
	cachedRule := newSyncTestCachedRule(existingRule)

	mockCache.EXPECT().GetActiveRules(nil).Return([]*cache.CachedRule{cachedRule})
	mockCache.EXPECT().Size().Return(1).AnyTimes()

	// Delta returns same rule (overlap buffer re-fetch) — same UpdatedAt
	repo.EXPECT().GetRulesUpdatedSince(gomock.Any(), gomock.Any()).Return([]*model.Rule{existingRule}, nil)

	// ClassifyChanges returns empty ChangeSet — no Compile, but cache staleness touched
	mockCache.EXPECT().ApplyChanges(gomock.Nil(), gomock.Nil())

	worker.runSyncCycle(context.Background())
}

func TestRunSyncCycle_EmptyFetch_TouchesCacheStaleness(t *testing.T) {
	t.Parallel()
	_, cleanup := setupTestTracer(t)
	defer cleanup()

	ctrl := gomock.NewController(t)
	mockCache := mocks.NewMockRuleSyncCache(ctrl)
	repo := mocks.NewMockRuleSyncRepository(ctrl)
	compiler := mocks.NewMockExpressionCompiler(ctrl)
	logger := testutil.NewMockLogger()
	clk := testutil.MockClock{FixedTime: testutil.FixedTime()}

	worker, err := NewRuleSyncWorker(mockCache, repo, compiler, defaultSyncConfig(), logger, defaultTestCircuitBreaker(), clk)
	require.NoError(t, err)
	worker.lastSync = testutil.FixedTime().Add(-10 * time.Second)

	// Delta returns nothing
	repo.EXPECT().GetRulesUpdatedSince(gomock.Any(), gomock.Any()).Return([]*model.Rule{}, nil)
	mockCache.EXPECT().Size().Return(0).AnyTimes()

	// Key assertion: cache staleness must be touched even on empty fetch
	mockCache.EXPECT().ApplyChanges(gomock.Nil(), gomock.Nil())

	worker.runSyncCycle(context.Background())
}

func TestRunSyncCycle_EmptyChangeSet_TouchesCacheStaleness(t *testing.T) {
	t.Parallel()
	_, cleanup := setupTestTracer(t)
	defer cleanup()

	ctrl := gomock.NewController(t)
	mockCache := mocks.NewMockRuleSyncCache(ctrl)
	repo := mocks.NewMockRuleSyncRepository(ctrl)
	compiler := mocks.NewMockExpressionCompiler(ctrl)
	logger := testutil.NewMockLogger()
	clk := testutil.MockClock{FixedTime: testutil.FixedTime()}

	worker, err := NewRuleSyncWorker(mockCache, repo, compiler, defaultSyncConfig(), logger, defaultTestCircuitBreaker(), clk)
	require.NoError(t, err)
	worker.lastSync = testutil.FixedTime()

	existingRule := newSyncTestActiveRule(1)
	cachedRule := newSyncTestCachedRule(existingRule)

	mockCache.EXPECT().GetActiveRules(nil).Return([]*cache.CachedRule{cachedRule})

	// Delta returns same rule (overlap re-fetch) — ClassifyChanges returns empty
	repo.EXPECT().GetRulesUpdatedSince(gomock.Any(), gomock.Any()).Return([]*model.Rule{existingRule}, nil)
	mockCache.EXPECT().Size().Return(1).AnyTimes()

	// Key assertion: cache staleness must be touched even when changes are empty
	mockCache.EXPECT().ApplyChanges(gomock.Nil(), gomock.Nil())

	worker.runSyncCycle(context.Background())
}

// RunLoop tests (S-015)

func TestRunLoop_TickerDriven(t *testing.T) {
	t.Parallel()
	_, cleanup := setupTestTracer(t)
	defer cleanup()

	ctrl := gomock.NewController(t)
	mockCache := mocks.NewMockRuleSyncCache(ctrl)
	repo := mocks.NewMockRuleSyncRepository(ctrl)
	compiler := mocks.NewMockExpressionCompiler(ctrl)
	logger := testutil.NewMockLogger()

	tickerChan := make(chan time.Time, 1)
	clk := testutil.MockClock{FixedTime: testutil.FixedTime(), TickerChan: tickerChan}

	worker, err := NewRuleSyncWorker(mockCache, repo, compiler, defaultSyncConfig(), logger, defaultTestCircuitBreaker(), clk)
	require.NoError(t, err)

	warmupTime := testutil.FixedTime().Add(-5 * time.Minute)
	mockCache.EXPECT().LastSyncTime().Return(warmupTime).AnyTimes()

	// One tick -> one sync cycle; verify since uses warmupTime
	expectedSince := warmupTime.Add(-defaultSyncConfig().OverlapBuffer)
	repo.EXPECT().GetRulesUpdatedSince(gomock.Any(), expectedSince).Return([]*model.Rule{}, nil).Times(1)
	cycleDone := make(chan struct{}, 1)
	mockCache.EXPECT().ApplyChanges(gomock.Nil(), gomock.Nil()).Times(1).Do(func(_, _ any) {
		cycleDone <- struct{}{}
	})
	mockCache.EXPECT().Size().Return(0).AnyTimes()

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- worker.RunWithContext(ctx)
	}()

	// Send one tick
	tickerChan <- testutil.FixedTime()

	// Wait for sync cycle to complete deterministically
	<-cycleDone

	cancel()

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not shut down within timeout")
	}
}

func TestRunLoop_MultipleTicksProcessed(t *testing.T) {
	t.Parallel()
	_, cleanup := setupTestTracer(t)
	defer cleanup()

	ctrl := gomock.NewController(t)
	mockCache := mocks.NewMockRuleSyncCache(ctrl)
	repo := mocks.NewMockRuleSyncRepository(ctrl)
	compiler := mocks.NewMockExpressionCompiler(ctrl)
	logger := testutil.NewMockLogger()

	tickerChan := make(chan time.Time, 3)
	clk := testutil.MockClock{FixedTime: testutil.FixedTime(), TickerChan: tickerChan}

	worker, err := NewRuleSyncWorker(mockCache, repo, compiler, defaultSyncConfig(), logger, defaultTestCircuitBreaker(), clk)
	require.NoError(t, err)

	mockCache.EXPECT().LastSyncTime().Return(testutil.FixedTime()).AnyTimes()

	// Expect 3 sync cycles
	repo.EXPECT().GetRulesUpdatedSince(gomock.Any(), gomock.Any()).Return([]*model.Rule{}, nil).Times(3)
	cycleDone := make(chan struct{}, 3)
	mockCache.EXPECT().ApplyChanges(gomock.Nil(), gomock.Nil()).Times(3).Do(func(_, _ any) {
		cycleDone <- struct{}{}
	})
	mockCache.EXPECT().Size().Return(0).AnyTimes()

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- worker.RunWithContext(ctx)
	}()

	// Send 3 ticks
	for i := 0; i < 3; i++ {
		tickerChan <- testutil.FixedTime()
		<-cycleDone
	}

	cancel()

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not shut down within timeout")
	}
}
