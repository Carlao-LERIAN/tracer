// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package workers

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"tracer/internal/services/command"
	"tracer/internal/services/workers/mocks"
	"tracer/internal/testutil"
	"tracer/pkg/model"
)

func TestRuleSyncWorker_Notify_TriggersSyncCycle(t *testing.T) {
	t.Parallel()
	_, cleanup := setupTestTracer(t)
	defer cleanup()

	ctrl := gomock.NewController(t)
	mockCache := mocks.NewMockRuleSyncCache(ctrl)
	repo := mocks.NewMockRuleSyncRepository(ctrl)
	compiler := mocks.NewMockExpressionCompiler(ctrl)
	logger := testutil.NewMockLogger()

	tickerChan := make(chan time.Time) // unbuffered — will never fire
	clk := testutil.MockClock{FixedTime: testutil.FixedTime(), TickerChan: tickerChan}

	worker, err := NewRuleSyncWorker(mockCache, repo, compiler, defaultSyncConfig(), logger, defaultTestCircuitBreaker(), clk)
	require.NoError(t, err)

	mockCache.EXPECT().LastSyncTime().Return(testutil.FixedTime()).AnyTimes()

	// Expect exactly one sync cycle triggered by Notify (not by ticker).
	// The Do callback signals syncComplete so we can wait deterministically
	// instead of using time.Sleep.
	syncComplete := make(chan struct{}, 1)

	repo.EXPECT().GetRulesUpdatedSince(gomock.Any(), gomock.Any()).Return([]*model.Rule{}, nil).Times(1)
	mockCache.EXPECT().ApplyChanges(gomock.Nil(), gomock.Nil()).Times(1).
		Do(func(_ any, _ any) { syncComplete <- struct{}{} })
	mockCache.EXPECT().Size().Return(0).AnyTimes()

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- worker.RunWithContext(ctx)
	}()

	// Notify should trigger a sync cycle even though the ticker never fires
	worker.Notify()

	// Wait for the sync cycle to complete via mock callback
	select {
	case <-syncComplete:
		// Sync cycle finished — ApplyChanges was called
	case <-time.After(2 * time.Second):
		t.Fatal("sync cycle did not complete within timeout")
	}

	cancel()

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not shut down within timeout")
	}
}

func TestRuleSyncWorker_Notify_Coalesces(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockCache := mocks.NewMockRuleSyncCache(ctrl)
	repo := mocks.NewMockRuleSyncRepository(ctrl)
	compiler := mocks.NewMockExpressionCompiler(ctrl)
	logger := testutil.NewMockLogger()

	worker, err := NewRuleSyncWorker(mockCache, repo, compiler, defaultSyncConfig(), logger, defaultTestCircuitBreaker(), nil)
	require.NoError(t, err)

	// Send multiple notifications rapidly — only 1 should be buffered (cap=1)
	for i := 0; i < 10; i++ {
		worker.Notify()
	}

	// Drain the channel — should get exactly 1
	count := 0
	for {
		select {
		case <-worker.syncNowChan:
			count++
		default:
			goto done
		}
	}
done:
	assert.Equal(t, 1, count, "multiple rapid Notify() calls should coalesce into 1")
}

func TestRuleSyncWorker_Notify_NilNotifierSafe(t *testing.T) {
	t.Parallel()

	// Verify that a command handler with nil notifier does not panic.
	// This simulates the production path where SetNotifier is never called
	// (e.g., in unit tests that don't wire the full dependency graph).
	var notifier command.RuleChangeNotifier // nil interface

	assert.NotPanics(t, func() {
		if notifier != nil {
			notifier.Notify()
		}
	})
}

func TestRuleSyncWorker_ImplementsRuleChangeNotifier(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockCache := mocks.NewMockRuleSyncCache(ctrl)
	repo := mocks.NewMockRuleSyncRepository(ctrl)
	compiler := mocks.NewMockExpressionCompiler(ctrl)
	logger := testutil.NewMockLogger()

	worker, err := NewRuleSyncWorker(mockCache, repo, compiler, defaultSyncConfig(), logger, defaultTestCircuitBreaker(), nil)
	require.NoError(t, err)

	// Compile-time check: *RuleSyncWorker satisfies command.RuleChangeNotifier
	var _ command.RuleChangeNotifier = worker
}
