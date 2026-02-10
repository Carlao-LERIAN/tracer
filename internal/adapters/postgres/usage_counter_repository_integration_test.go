// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"tracer/internal/testutil"
	"tracer/pkg/constant"
	"tracer/pkg/model"
)

// createTestLimit creates a test limit in the database and returns its ID.
// This is required because usage_counters has a foreign key constraint to limits.
func createTestLimit(t *testing.T, db *sql.DB, base int64) uuid.UUID {
	t.Helper()

	limitID := testutil.MustDeterministicUUID(base)

	_, err := db.Exec(`
		INSERT INTO limits (id, name, limit_type, max_amount, currency, scopes, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, limitID, "Test Limit "+limitID.String()[:8], "DAILY", decimal.NewFromInt(10000), "USD", "[]", "ACTIVE")
	require.NoError(t, err, "Failed to create test limit")

	return limitID
}

// cleanupTestLimit removes a test limit from the database.
// Due to ON DELETE CASCADE, this also removes associated usage_counters.
func cleanupTestLimit(t *testing.T, db *sql.DB, limitID uuid.UUID) {
	t.Helper()

	_, err := db.Exec("DELETE FROM limits WHERE id = $1", limitID)
	if err != nil {
		t.Logf("Cleanup: failed to delete limit %s: %v", limitID, err)
	}
}

// TestUsageCounterRepository_IncrementAtomic_Concurrent_Integration tests that
// concurrent IncrementAtomic calls do not lose updates when using SELECT FOR UPDATE.
//
// This is a real integration test that:
// 1. Uses a real PostgreSQL database (from docker-compose)
// 2. Creates a usage counter with currentUsage=0
// 3. Runs 10 goroutines in parallel, each calling IncrementAtomic with amount=10
// 4. Verifies final currentUsage equals exactly 100 (10 * 10)
//
// If SELECT FOR UPDATE is working correctly, no updates should be lost due to race conditions.
func TestUsageCounterRepository_IncrementAtomic_Concurrent_Integration(t *testing.T) {
	// Setup test tracing (required for lib-commons context extraction)
	testutil.SetupTestTracing(t)

	// Connect to real PostgreSQL (cleanup registered via t.Cleanup)
	db := testutil.SetupIntegrationDB(t)

	// Create repository with real database connection
	adapter := &testutil.IntegrationDBAdapter{DB: db}
	repo := NewUsageCounterRepositoryWithConnection(adapter)

	// Test parameters
	const numGoroutines = 10
	incrementAmount := decimal.RequireFromString("10")
	expectedFinalUsage := decimal.RequireFromString("100") // numGoroutines(10) * incrementAmount(10)

	// Create a test limit first (required for FK constraint)
	limitID := createTestLimit(t, db, 9001)
	scopeKey := "test:concurrent-" + testutil.MustDeterministicUUID(9101).String()[:8]
	periodKey := "2025-01"

	// Cleanup: remove test limit (cascades to counters)
	t.Cleanup(func() {
		cleanupTestLimit(t, db, limitID)
	})

	// Create initial counter with currentUsage=0
	ctx := context.Background()
	counter, err := repo.GetOrCreateForUpdate(ctx, limitID, scopeKey, periodKey)
	require.NoError(t, err, "Failed to create initial counter")
	require.NotNil(t, counter)
	require.True(t, decimal.Zero.Equal(counter.CurrentUsage), "Initial counter should have 0 usage")

	counterID := counter.ID
	t.Logf("Created counter %s with initial usage 0", counterID)

	// Run concurrent increments
	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines)

	for i := range numGoroutines {
		wg.Add(1)

		go func(goroutineID int) {
			defer wg.Done()

			// Each goroutine gets its own context
			goroutineCtx := context.Background()

			err := repo.IncrementAtomic(goroutineCtx, counterID, incrementAmount)
			if err != nil {
				errors <- fmt.Errorf("goroutine %d: %w", goroutineID, err)
			}
		}(i)
	}

	// Wait for all goroutines to complete
	wg.Wait()
	close(errors)

	// Check for errors from goroutines
	var goroutineErrors []error
	for err := range errors {
		goroutineErrors = append(goroutineErrors, err)
	}

	require.Empty(t, goroutineErrors, "Goroutines should not have errors: %v", goroutineErrors)

	// Verify final usage equals expected value (no lost updates)
	var finalUsage decimal.Decimal
	err = db.QueryRowContext(ctx, "SELECT current_usage FROM usage_counters WHERE id = $1", counterID).Scan(&finalUsage)
	require.NoError(t, err, "Failed to query final usage")

	assert.True(t, expectedFinalUsage.Equal(finalUsage),
		"Final usage should be exactly %s (no lost updates), but got %s",
		expectedFinalUsage.String(), finalUsage.String())

	t.Logf("SUCCESS: %d goroutines each incremented by %s, final usage = %s (expected %s)",
		numGoroutines, incrementAmount.String(), finalUsage.String(), expectedFinalUsage.String())
}

// TestUsageCounterRepository_GetOrCreateForUpdate_Concurrent_Integration tests that
// concurrent GetOrCreateForUpdate calls correctly handle the race condition where
// multiple goroutines try to create the same counter simultaneously.
//
// This verifies the retry logic in GetOrCreateForUpdate handles concurrent inserts.
func TestUsageCounterRepository_GetOrCreateForUpdate_Concurrent_Integration(t *testing.T) {
	testutil.SetupTestTracing(t)

	db := testutil.SetupIntegrationDB(t)

	adapter := &testutil.IntegrationDBAdapter{DB: db}
	repo := NewUsageCounterRepositoryWithConnection(adapter)

	const numGoroutines = 10

	// Create a test limit first (required for FK constraint)
	limitID := createTestLimit(t, db, 9002)
	scopeKey := "test:concurrent-create-" + testutil.MustDeterministicUUID(9102).String()[:8]
	periodKey := "2025-01"

	// Cleanup: remove test limit (cascades to counters)
	t.Cleanup(func() {
		cleanupTestLimit(t, db, limitID)
	})

	// Run concurrent GetOrCreateForUpdate calls
	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines)
	counters := make(chan *model.UsageCounter, numGoroutines)

	for i := range numGoroutines {
		wg.Add(1)

		go func(goroutineID int) {
			defer wg.Done()

			ctx := context.Background()

			counter, err := repo.GetOrCreateForUpdate(ctx, limitID, scopeKey, periodKey)
			if err != nil {
				errors <- fmt.Errorf("goroutine %d: %w", goroutineID, err)
				return
			}

			counters <- counter
		}(i)
	}

	wg.Wait()
	close(errors)
	close(counters)

	// Check for errors
	var goroutineErrors []error
	for err := range errors {
		goroutineErrors = append(goroutineErrors, err)
	}

	require.Empty(t, goroutineErrors, "Goroutines should not have errors: %v", goroutineErrors)

	// All goroutines should return the same counter (same ID)
	var collectedCounters []*model.UsageCounter
	for counter := range counters {
		collectedCounters = append(collectedCounters, counter)
	}

	require.Len(t, collectedCounters, numGoroutines, "Should have %d counters", numGoroutines)

	// Verify all counters have the same ID (only one was created)
	firstID := collectedCounters[0].ID
	for i, counter := range collectedCounters {
		assert.Equal(t, firstID, counter.ID,
			"All goroutines should return the same counter ID, but goroutine %d got different ID", i)
		assert.Equal(t, limitID, counter.LimitID)
		assert.Equal(t, scopeKey, counter.ScopeKey)
		assert.Equal(t, periodKey, counter.PeriodKey)
	}

	// Verify only one counter exists in the database
	var count int
	err := db.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM usage_counters WHERE limit_id = $1 AND scope_key = $2 AND period_key = $3",
		limitID, scopeKey, periodKey).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "Only one counter should exist in the database")

	t.Logf("SUCCESS: %d goroutines all returned the same counter ID %s", numGoroutines, firstID)
}

// TestUsageCounterRepository_DecrementAtomic_Concurrent_Integration tests that
// concurrent DecrementAtomic calls do not under-decrement when using conditional UPDATE.
func TestUsageCounterRepository_DecrementAtomic_Concurrent_Integration(t *testing.T) {
	testutil.SetupTestTracing(t)

	db := testutil.SetupIntegrationDB(t)

	adapter := &testutil.IntegrationDBAdapter{DB: db}
	repo := NewUsageCounterRepositoryWithConnection(adapter)

	const numGoroutines = 10
	decrementAmount := decimal.RequireFromString("5")
	initialUsage := decimal.RequireFromString("100")
	expectedFinalUsage := decimal.RequireFromString("50") // 100 - numGoroutines(10) * decrementAmount(5)

	// Create a test limit first (required for FK constraint)
	limitID := createTestLimit(t, db, 9003)
	scopeKey := "test:concurrent-decrement-" + testutil.MustDeterministicUUID(9103).String()[:8]
	periodKey := "2025-01"

	// Cleanup: remove test limit (cascades to counters)
	t.Cleanup(func() {
		cleanupTestLimit(t, db, limitID)
	})

	// Create counter with initial usage
	ctx := context.Background()
	counter, err := repo.GetOrCreateForUpdate(ctx, limitID, scopeKey, periodKey)
	require.NoError(t, err)

	counterID := counter.ID

	// Set initial usage directly in database
	_, err = db.ExecContext(ctx, "UPDATE usage_counters SET current_usage = $1 WHERE id = $2", initialUsage, counterID)
	require.NoError(t, err)

	// Run concurrent decrements
	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines)

	for i := range numGoroutines {
		wg.Add(1)

		go func(goroutineID int) {
			defer wg.Done()

			err := repo.DecrementAtomic(context.Background(), counterID, decrementAmount)
			if err != nil {
				errors <- fmt.Errorf("goroutine %d: %w", goroutineID, err)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	var goroutineErrors []error
	for err := range errors {
		goroutineErrors = append(goroutineErrors, err)
	}

	require.Empty(t, goroutineErrors, "Goroutines should not have errors: %v", goroutineErrors)

	// Verify final usage
	var finalUsage decimal.Decimal
	err = db.QueryRowContext(ctx, "SELECT current_usage FROM usage_counters WHERE id = $1", counterID).Scan(&finalUsage)
	require.NoError(t, err)

	assert.True(t, expectedFinalUsage.Equal(finalUsage),
		"Final usage should be exactly %s, but got %s",
		expectedFinalUsage.String(), finalUsage.String())

	t.Logf("SUCCESS: %d goroutines each decremented by %s from %s, final usage = %s",
		numGoroutines, decrementAmount.String(), initialUsage.String(), finalUsage.String())
}

// TestUsageCounterRepository_DecrementAtomic_UnderflowProtection_Integration tests that
// DecrementAtomic returns an error and does not modify current_usage when the decrement
// amount exceeds the current balance.
func TestUsageCounterRepository_DecrementAtomic_UnderflowProtection_Integration(t *testing.T) {
	testutil.SetupTestTracing(t)

	db := testutil.SetupIntegrationDB(t)
	adapter := &testutil.IntegrationDBAdapter{DB: db}
	repo := NewUsageCounterRepositoryWithConnection(adapter)

	limitID := createTestLimit(t, db, 9004)
	scopeKey := "test:decrement-underflow-" + testutil.MustDeterministicUUID(9104).String()[:8]
	periodKey := "2025-01"

	t.Cleanup(func() {
		cleanupTestLimit(t, db, limitID)
	})

	ctx := context.Background()
	counter, err := repo.GetOrCreateForUpdate(ctx, limitID, scopeKey, periodKey)
	require.NoError(t, err)

	// Set initial usage to 10
	initialUsage := decimal.RequireFromString("10")
	_, err = db.ExecContext(ctx, "UPDATE usage_counters SET current_usage = $1 WHERE id = $2", initialUsage, counter.ID)
	require.NoError(t, err)

	// Attempt to decrement by 15 (exceeds current_usage of 10)
	decrementAmount := decimal.RequireFromString("15")
	err = repo.DecrementAtomic(ctx, counter.ID, decrementAmount)
	require.ErrorIs(t, err, constant.ErrUsageCounterCurrentUsageNegative)

	// Verify usage was NOT modified
	var finalUsage decimal.Decimal
	err = db.QueryRowContext(ctx, "SELECT current_usage FROM usage_counters WHERE id = $1", counter.ID).Scan(&finalUsage)
	require.NoError(t, err)
	assert.True(t, initialUsage.Equal(finalUsage),
		"Usage should remain %s after failed underflow decrement, got %s",
		initialUsage.String(), finalUsage.String())
}

// TestUsageCounterRepository_MixedOperations_Concurrent_Integration tests concurrent
// increment and decrement operations to verify atomicity under mixed workloads.
func TestUsageCounterRepository_MixedOperations_Concurrent_Integration(t *testing.T) {
	testutil.SetupTestTracing(t)

	db := testutil.SetupIntegrationDB(t)

	adapter := &testutil.IntegrationDBAdapter{DB: db}
	repo := NewUsageCounterRepositoryWithConnection(adapter)

	const numIncrements = 5
	const numDecrements = 5
	incrementAmount := decimal.RequireFromString("10")
	decrementAmount := decimal.RequireFromString("5")
	initialUsage := decimal.RequireFromString("50")

	// Expected: 50 + (5 * 10) - (5 * 5) = 50 + 50 - 25 = 75
	expectedFinalUsage := decimal.RequireFromString("75")

	// Create a test limit first (required for FK constraint)
	limitID := createTestLimit(t, db, 9005)
	scopeKey := "test:mixed-ops-" + testutil.MustDeterministicUUID(9105).String()[:8]
	periodKey := "2025-01"

	// Cleanup: remove test limit (cascades to counters)
	t.Cleanup(func() {
		cleanupTestLimit(t, db, limitID)
	})

	ctx := context.Background()
	counter, err := repo.GetOrCreateForUpdate(ctx, limitID, scopeKey, periodKey)
	require.NoError(t, err)

	counterID := counter.ID

	// Set initial usage
	_, err = db.ExecContext(ctx, "UPDATE usage_counters SET current_usage = $1 WHERE id = $2", initialUsage, counterID)
	require.NoError(t, err)

	var wg sync.WaitGroup
	errors := make(chan error, numIncrements+numDecrements)

	// Launch increment goroutines
	for i := range numIncrements {
		wg.Add(1)

		go func(id int) {
			defer wg.Done()

			if err := repo.IncrementAtomic(context.Background(), counterID, incrementAmount); err != nil {
				errors <- fmt.Errorf("increment %d: %w", id, err)
			}
		}(i)
	}

	// Launch decrement goroutines
	for i := range numDecrements {
		wg.Add(1)

		go func(id int) {
			defer wg.Done()

			if err := repo.DecrementAtomic(context.Background(), counterID, decrementAmount); err != nil {
				errors <- fmt.Errorf("decrement %d: %w", id, err)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	var goroutineErrors []error
	for err := range errors {
		goroutineErrors = append(goroutineErrors, err)
	}

	require.Empty(t, goroutineErrors, "Operations should not have errors: %v", goroutineErrors)

	var finalUsage decimal.Decimal
	err = db.QueryRowContext(ctx, "SELECT current_usage FROM usage_counters WHERE id = $1", counterID).Scan(&finalUsage)
	require.NoError(t, err)

	assert.True(t, expectedFinalUsage.Equal(finalUsage),
		"Final usage should be exactly %s, but got %s",
		expectedFinalUsage.String(), finalUsage.String())

	t.Logf("SUCCESS: Mixed ops (5 increments of %s, 5 decrements of %s) from %s, final = %s (expected %s)",
		incrementAmount.String(), decrementAmount.String(), initialUsage.String(), finalUsage.String(), expectedFinalUsage.String())
}
