// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package postgres

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"tracer/internal/testutil"
	"tracer/pkg/model"
)

// TestUsageCounterPostgreSQLModel_ToEntity tests the conversion from database model to domain entity.
// This test follows the ToEntity/FromEntity pattern from Ring Standards (golang/domain.md).
func TestUsageCounterPostgreSQLModel_ToEntity(t *testing.T) {
	t.Parallel()

	// Deterministic test data following PROJECT_RULES.md
	testID := testutil.MustDeterministicUUID(1)
	testLimitID := testutil.MustDeterministicUUID(2)
	fixedTime := testutil.FixedTime()

	tests := []struct {
		name     string
		dbModel  UsageCounterPostgreSQLModel
		expected *model.UsageCounter
	}{
		{
			name: "converts basic usage counter",
			dbModel: UsageCounterPostgreSQLModel{
				ID:            testID.String(),
				LimitID:       testLimitID.String(),
				ScopeKey:      "acct:123",
				PeriodKey:     "2025-01",
				CurrentUsage:  5000,
				LastUpdatedAt: fixedTime,
			},
			expected: &model.UsageCounter{
				ID:            testID,
				LimitID:       testLimitID,
				ScopeKey:      "acct:123",
				PeriodKey:     "2025-01",
				CurrentUsage:  5000,
				LastUpdatedAt: fixedTime,
			},
		},
		{
			name: "converts usage counter with zero usage",
			dbModel: UsageCounterPostgreSQLModel{
				ID:            testID.String(),
				LimitID:       testLimitID.String(),
				ScopeKey:      "segment:gold",
				PeriodKey:     "2025-01-15",
				CurrentUsage:  0,
				LastUpdatedAt: fixedTime,
			},
			expected: &model.UsageCounter{
				ID:            testID,
				LimitID:       testLimitID,
				ScopeKey:      "segment:gold",
				PeriodKey:     "2025-01-15",
				CurrentUsage:  0,
				LastUpdatedAt: fixedTime,
			},
		},
		{
			name: "converts usage counter with high usage value",
			dbModel: UsageCounterPostgreSQLModel{
				ID:            testID.String(),
				LimitID:       testLimitID.String(),
				ScopeKey:      "portfolio:xyz",
				PeriodKey:     "2025-12",
				CurrentUsage:  9223372036854775807, // Max int64
				LastUpdatedAt: fixedTime,
			},
			expected: &model.UsageCounter{
				ID:            testID,
				LimitID:       testLimitID,
				ScopeKey:      "portfolio:xyz",
				PeriodKey:     "2025-12",
				CurrentUsage:  9223372036854775807,
				LastUpdatedAt: fixedTime,
			},
		},
		{
			name: "converts usage counter with merchant scope",
			dbModel: UsageCounterPostgreSQLModel{
				ID:            testID.String(),
				LimitID:       testLimitID.String(),
				ScopeKey:      "merchant:m-001",
				PeriodKey:     "2025-06-30",
				CurrentUsage:  15000,
				LastUpdatedAt: fixedTime.Add(24 * time.Hour),
			},
			expected: &model.UsageCounter{
				ID:            testID,
				LimitID:       testLimitID,
				ScopeKey:      "merchant:m-001",
				PeriodKey:     "2025-06-30",
				CurrentUsage:  15000,
				LastUpdatedAt: fixedTime.Add(24 * time.Hour),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Act
			result, err := tt.dbModel.ToEntity()

			// Assert
			require.NoError(t, err, "ToEntity should not return error")
			require.NotNil(t, result, "ToEntity should not return nil")
			assert.Equal(t, tt.expected.ID, result.ID, "ID mismatch")
			assert.Equal(t, tt.expected.LimitID, result.LimitID, "LimitID mismatch")
			assert.Equal(t, tt.expected.ScopeKey, result.ScopeKey, "ScopeKey mismatch")
			assert.Equal(t, tt.expected.PeriodKey, result.PeriodKey, "PeriodKey mismatch")
			assert.Equal(t, tt.expected.CurrentUsage, result.CurrentUsage, "CurrentUsage mismatch")
			assert.Equal(t, tt.expected.LastUpdatedAt, result.LastUpdatedAt, "LastUpdatedAt mismatch")
		})
	}
}

// TestUsageCounterPostgreSQLModel_FromEntity tests the conversion from domain entity to database model.
// This test follows the ToEntity/FromEntity pattern from Ring Standards (golang/domain.md).
func TestUsageCounterPostgreSQLModel_FromEntity(t *testing.T) {
	t.Parallel()

	// Deterministic test data following PROJECT_RULES.md
	testID := testutil.MustDeterministicUUID(10)
	testLimitID := testutil.MustDeterministicUUID(11)
	fixedTime := testutil.FixedTime()

	tests := []struct {
		name     string
		entity   *model.UsageCounter
		assertFn func(t *testing.T, dbModel *UsageCounterPostgreSQLModel)
	}{
		{
			name: "converts basic usage counter entity",
			entity: &model.UsageCounter{
				ID:            testID,
				LimitID:       testLimitID,
				ScopeKey:      "acct:456",
				PeriodKey:     "2025-02",
				CurrentUsage:  3000,
				LastUpdatedAt: fixedTime,
			},
			assertFn: func(t *testing.T, dbModel *UsageCounterPostgreSQLModel) {
				t.Helper()
				assert.Equal(t, testID.String(), dbModel.ID, "ID should be string representation of UUID")
				assert.Equal(t, testLimitID.String(), dbModel.LimitID, "LimitID should be string representation of UUID")
				assert.Equal(t, "acct:456", dbModel.ScopeKey)
				assert.Equal(t, "2025-02", dbModel.PeriodKey)
				assert.Equal(t, int64(3000), dbModel.CurrentUsage)
				assert.Equal(t, fixedTime, dbModel.LastUpdatedAt)
			},
		},
		{
			name: "converts entity with zero usage",
			entity: &model.UsageCounter{
				ID:            testID,
				LimitID:       testLimitID,
				ScopeKey:      "segment:silver",
				PeriodKey:     "2025-03-01",
				CurrentUsage:  0,
				LastUpdatedAt: fixedTime,
			},
			assertFn: func(t *testing.T, dbModel *UsageCounterPostgreSQLModel) {
				t.Helper()
				assert.Equal(t, testID.String(), dbModel.ID)
				assert.Equal(t, testLimitID.String(), dbModel.LimitID)
				assert.Equal(t, "segment:silver", dbModel.ScopeKey)
				assert.Equal(t, "2025-03-01", dbModel.PeriodKey)
				assert.Equal(t, int64(0), dbModel.CurrentUsage)
				assert.Equal(t, fixedTime, dbModel.LastUpdatedAt)
			},
		},
		{
			name: "converts entity with max int64 usage",
			entity: &model.UsageCounter{
				ID:            testID,
				LimitID:       testLimitID,
				ScopeKey:      "portfolio:abc",
				PeriodKey:     "2025-12",
				CurrentUsage:  9223372036854775807, // Max int64
				LastUpdatedAt: fixedTime.Add(48 * time.Hour),
			},
			assertFn: func(t *testing.T, dbModel *UsageCounterPostgreSQLModel) {
				t.Helper()
				assert.Equal(t, testID.String(), dbModel.ID)
				assert.Equal(t, testLimitID.String(), dbModel.LimitID)
				assert.Equal(t, "portfolio:abc", dbModel.ScopeKey)
				assert.Equal(t, "2025-12", dbModel.PeriodKey)
				assert.Equal(t, int64(9223372036854775807), dbModel.CurrentUsage)
				assert.Equal(t, fixedTime.Add(48*time.Hour), dbModel.LastUpdatedAt)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Act
			var dbModel UsageCounterPostgreSQLModel
			dbModel.FromEntity(tt.entity)

			// Assert
			tt.assertFn(t, &dbModel)
		})
	}
}

// TestUsageCounterPostgreSQLModel_RoundTrip tests that ToEntity and FromEntity are inverses.
// entity -> FromEntity -> dbModel -> ToEntity -> entity (should be equal)
func TestUsageCounterPostgreSQLModel_RoundTrip(t *testing.T) {
	t.Parallel()

	testID := testutil.MustDeterministicUUID(20)
	testLimitID := testutil.MustDeterministicUUID(21)
	fixedTime := testutil.FixedTime()

	original := &model.UsageCounter{
		ID:            testID,
		LimitID:       testLimitID,
		ScopeKey:      "acct:round-trip-123",
		PeriodKey:     "2025-07",
		CurrentUsage:  12345,
		LastUpdatedAt: fixedTime.Add(30 * time.Minute),
	}

	// entity -> dbModel
	var dbModel UsageCounterPostgreSQLModel
	dbModel.FromEntity(original)

	// dbModel -> entity
	result, err := dbModel.ToEntity()

	// Assert equality
	require.NoError(t, err, "ToEntity should not return error")
	require.NotNil(t, result, "ToEntity should not return nil")
	assert.Equal(t, original.ID, result.ID, "Round-trip ID mismatch")
	assert.Equal(t, original.LimitID, result.LimitID, "Round-trip LimitID mismatch")
	assert.Equal(t, original.ScopeKey, result.ScopeKey, "Round-trip ScopeKey mismatch")
	assert.Equal(t, original.PeriodKey, result.PeriodKey, "Round-trip PeriodKey mismatch")
	assert.Equal(t, original.CurrentUsage, result.CurrentUsage, "Round-trip CurrentUsage mismatch")
	assert.Equal(t, original.LastUpdatedAt, result.LastUpdatedAt, "Round-trip LastUpdatedAt mismatch")
}

// TestUsageCounterPostgreSQLModel_ToEntity_EdgeCases tests edge cases for ToEntity conversion.
func TestUsageCounterPostgreSQLModel_ToEntity_EdgeCases(t *testing.T) {
	t.Parallel()

	fixedTime := testutil.FixedTime()

	tests := []struct {
		name        string
		dbModel     UsageCounterPostgreSQLModel
		validate    func(t *testing.T, result *model.UsageCounter)
		expectError bool
	}{
		{
			name: "returns error for invalid ID UUID",
			dbModel: UsageCounterPostgreSQLModel{
				ID:            "invalid-uuid",
				LimitID:       testutil.MustDeterministicUUID(1).String(),
				ScopeKey:      "acct:test",
				PeriodKey:     "2025-01",
				CurrentUsage:  100,
				LastUpdatedAt: fixedTime,
			},
			validate: func(t *testing.T, result *model.UsageCounter) {
				t.Helper()
				// Should fail - tested via expectError in table driver
			},
			expectError: true,
		},
		{
			name: "returns error for invalid LimitID UUID",
			dbModel: UsageCounterPostgreSQLModel{
				ID:            testutil.MustDeterministicUUID(1).String(),
				LimitID:       "also-invalid",
				ScopeKey:      "acct:test",
				PeriodKey:     "2025-01",
				CurrentUsage:  100,
				LastUpdatedAt: fixedTime,
			},
			validate: func(t *testing.T, result *model.UsageCounter) {
				t.Helper()
				// Should fail - tested via expectError in table driver
			},
			expectError: true,
		},
		{
			name: "handles empty scope key",
			dbModel: UsageCounterPostgreSQLModel{
				ID:            testutil.MustDeterministicUUID(30).String(),
				LimitID:       testutil.MustDeterministicUUID(31).String(),
				ScopeKey:      "",
				PeriodKey:     "2025-01",
				CurrentUsage:  0,
				LastUpdatedAt: fixedTime,
			},
			validate: func(t *testing.T, result *model.UsageCounter) {
				t.Helper()
				require.NotNil(t, result)
				assert.Empty(t, result.ScopeKey, "Empty scope key should be preserved")
			},
		},
		{
			name: "handles empty period key",
			dbModel: UsageCounterPostgreSQLModel{
				ID:            testutil.MustDeterministicUUID(32).String(),
				LimitID:       testutil.MustDeterministicUUID(33).String(),
				ScopeKey:      "acct:test",
				PeriodKey:     "",
				CurrentUsage:  0,
				LastUpdatedAt: fixedTime,
			},
			validate: func(t *testing.T, result *model.UsageCounter) {
				t.Helper()
				require.NotNil(t, result)
				assert.Empty(t, result.PeriodKey, "Empty period key should be preserved")
			},
		},
		{
			name: "handles special characters in scope key",
			dbModel: UsageCounterPostgreSQLModel{
				ID:            testutil.MustDeterministicUUID(34).String(),
				LimitID:       testutil.MustDeterministicUUID(35).String(),
				ScopeKey:      "acct:abc-123_xyz:special",
				PeriodKey:     "2025-01-15T10:30:00Z",
				CurrentUsage:  5000,
				LastUpdatedAt: fixedTime,
			},
			validate: func(t *testing.T, result *model.UsageCounter) {
				t.Helper()
				require.NotNil(t, result)
				assert.Equal(t, "acct:abc-123_xyz:special", result.ScopeKey, "Special characters should be preserved")
				assert.Equal(t, "2025-01-15T10:30:00Z", result.PeriodKey, "ISO timestamp period key should be preserved")
			},
		},
		{
			name: "handles different scope key formats",
			dbModel: UsageCounterPostgreSQLModel{
				ID:            testutil.MustDeterministicUUID(36).String(),
				LimitID:       testutil.MustDeterministicUUID(37).String(),
				ScopeKey:      "global", // No prefix
				PeriodKey:     "2025",   // Year only
				CurrentUsage:  999999,
				LastUpdatedAt: fixedTime,
			},
			validate: func(t *testing.T, result *model.UsageCounter) {
				t.Helper()
				require.NotNil(t, result)
				assert.Equal(t, "global", result.ScopeKey, "Scope key without prefix should be preserved")
				assert.Equal(t, "2025", result.PeriodKey, "Year-only period key should be preserved")
			},
		},
		{
			name: "handles zero time value",
			dbModel: UsageCounterPostgreSQLModel{
				ID:            testutil.MustDeterministicUUID(38).String(),
				LimitID:       testutil.MustDeterministicUUID(39).String(),
				ScopeKey:      "acct:test",
				PeriodKey:     "2025-01",
				CurrentUsage:  100,
				LastUpdatedAt: time.Time{}, // Zero time
			},
			validate: func(t *testing.T, result *model.UsageCounter) {
				t.Helper()
				require.NotNil(t, result)
				assert.True(t, result.LastUpdatedAt.IsZero(), "Zero time should be preserved")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := tt.dbModel.ToEntity()

			if tt.expectError {
				require.Error(t, err, "ToEntity should return error")
				require.Nil(t, result, "Result should be nil when error occurs")
			} else {
				require.NoError(t, err, "ToEntity should not return error for edge cases")
				tt.validate(t, result)
			}
		})
	}
}
