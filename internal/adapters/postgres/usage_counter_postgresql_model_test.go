// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package postgres

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"tracer/internal/testutil"
	"tracer/pkg/model"
)

func TestUsageCounterPostgreSQLModel_FromEntity_NilEntity(t *testing.T) {
	t.Parallel()

	dbModel := &UsageCounterPostgreSQLModel{}

	err := dbModel.FromEntity(nil)

	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot be nil")
}

func TestUsageCounterPostgreSQLModel_FromEntity_Valid(t *testing.T) {
	t.Parallel()

	testID := testutil.MustDeterministicUUID(1)
	testLimitID := testutil.MustDeterministicUUID(2)
	fixedTime := testutil.FixedTime()

	entity := &model.UsageCounter{
		ID:            testID,
		LimitID:       testLimitID,
		ScopeKey:      "acct:abc-123",
		PeriodKey:     "2025-12-28",
		CurrentUsage:  500,
		LastUpdatedAt: fixedTime,
	}

	dbModel := &UsageCounterPostgreSQLModel{}
	err := dbModel.FromEntity(entity)

	require.NoError(t, err)
	assert.Equal(t, testID.String(), dbModel.ID)
	assert.Equal(t, testLimitID.String(), dbModel.LimitID)
	assert.Equal(t, "acct:abc-123", dbModel.ScopeKey)
	assert.Equal(t, "2025-12-28", dbModel.PeriodKey)
	assert.Equal(t, int64(500), dbModel.CurrentUsage)
	assert.Equal(t, fixedTime, dbModel.LastUpdatedAt)
}

func TestUsageCounterPostgreSQLModel_ToEntity_Valid(t *testing.T) {
	t.Parallel()

	testID := testutil.MustDeterministicUUID(3)
	testLimitID := testutil.MustDeterministicUUID(4)
	fixedTime := testutil.FixedTime()

	dbModel := &UsageCounterPostgreSQLModel{
		ID:            testID.String(),
		LimitID:       testLimitID.String(),
		ScopeKey:      "segment:gold",
		PeriodKey:     "2025-12",
		CurrentUsage:  1000,
		LastUpdatedAt: fixedTime,
	}

	entity, err := dbModel.ToEntity()

	require.NoError(t, err)
	assert.Equal(t, testID, entity.ID)
	assert.Equal(t, testLimitID, entity.LimitID)
	assert.Equal(t, "segment:gold", entity.ScopeKey)
	assert.Equal(t, "2025-12", entity.PeriodKey)
	assert.Equal(t, int64(1000), entity.CurrentUsage)
	assert.Equal(t, fixedTime, entity.LastUpdatedAt)
}

func TestUsageCounterPostgreSQLModel_ToEntity_InvalidID(t *testing.T) {
	t.Parallel()

	dbModel := &UsageCounterPostgreSQLModel{
		ID:      "not-a-uuid",
		LimitID: testutil.MustDeterministicUUID(5).String(),
	}

	_, err := dbModel.ToEntity()

	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid UsageCounter ID")
}

func TestUsageCounterPostgreSQLModel_ToEntity_InvalidLimitID(t *testing.T) {
	t.Parallel()

	dbModel := &UsageCounterPostgreSQLModel{
		ID:      testutil.MustDeterministicUUID(6).String(),
		LimitID: "not-a-uuid",
	}

	_, err := dbModel.ToEntity()

	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid LimitID")
}

func TestUsageCounterPostgreSQLModel_RoundTrip(t *testing.T) {
	t.Parallel()

	testID := testutil.MustDeterministicUUID(7)
	testLimitID := testutil.MustDeterministicUUID(8)
	fixedTime := testutil.FixedTime()

	original := &model.UsageCounter{
		ID:            testID,
		LimitID:       testLimitID,
		ScopeKey:      "portfolio:xyz",
		PeriodKey:     "2025-12-28",
		CurrentUsage:  750,
		LastUpdatedAt: fixedTime,
	}

	dbModel := &UsageCounterPostgreSQLModel{}
	err := dbModel.FromEntity(original)
	require.NoError(t, err)

	restored, err := dbModel.ToEntity()
	require.NoError(t, err)

	assert.Equal(t, original.ID, restored.ID)
	assert.Equal(t, original.LimitID, restored.LimitID)
	assert.Equal(t, original.ScopeKey, restored.ScopeKey)
	assert.Equal(t, original.PeriodKey, restored.PeriodKey)
	assert.Equal(t, original.CurrentUsage, restored.CurrentUsage)
	assert.Equal(t, original.LastUpdatedAt, restored.LastUpdatedAt)
}
