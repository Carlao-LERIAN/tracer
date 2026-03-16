// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package command

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"tracer/internal/testutil"
	"tracer/pkg/model"
)

// TestUpdateLimit_TimeWindow verifies that time window fields are handled correctly during update.
func TestUpdateLimit_TimeWindow(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRepo := NewMockLimitRepository(ctrl)
	clk := testutil.NewDefaultMockClock()

	cmd, err := NewUpdateLimitCommand(mockRepo, clk, nil)
	require.NoError(t, err)

	limitID := testutil.MustDeterministicUUID(1)
	now := testutil.FixedTime().UTC()

	// Existing limit WITHOUT time window
	existingLimit := &model.Limit{
		ID:              limitID,
		Name:            "Test Limit",
		LimitType:       model.LimitTypeDaily,
		MaxAmount:       decimal.RequireFromString("1000.00"),
		Currency:        "BRL",
		Scopes:          []model.Scope{{AccountID: testutil.UUIDPtr(testutil.MustDeterministicUUID(2))}},
		Status:          model.LimitStatusActive,
		ActiveTimeStart: nil, // NO time window initially
		ActiveTimeEnd:   nil,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	// Mock: GetByID returns existing limit
	mockRepo.EXPECT().
		GetByID(gomock.Any(), limitID).
		Return(existingLimit, nil)

	// Prepare time window values before mock setup so we can verify exact values
	startTime, err := model.NewTimeOfDay("06:00")
	require.NoError(t, err)
	endTime, err := model.NewTimeOfDay("20:00")
	require.NoError(t, err)

	// Mock: Update should be called with exact time window values
	mockRepo.EXPECT().
		Update(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, limit *model.Limit) error {
			require.NotNil(t, limit.ActiveTimeStart)
			require.NotNil(t, limit.ActiveTimeEnd)
			assert.True(t, limit.ActiveTimeStart.Equal(startTime), "ActiveTimeStart should be 06:00")
			assert.True(t, limit.ActiveTimeEnd.Equal(endTime), "ActiveTimeEnd should be 20:00")
			return nil
		})

	input := &UpdateLimitInput{
		ActiveTimeStart: &startTime,
		ActiveTimeEnd:   &endTime,
	}

	updatedLimit, err := cmd.Execute(context.Background(), limitID, input)
	require.NoError(t, err)

	// Assert: Time window fields should be updated (currently they're IGNORED - this will FAIL)
	assert.NotNil(t, updatedLimit.ActiveTimeStart, "ActiveTimeStart should be updated")
	assert.NotNil(t, updatedLimit.ActiveTimeEnd, "ActiveTimeEnd should be updated")

	if updatedLimit.ActiveTimeStart != nil {
		assert.Equal(t, startTime, *updatedLimit.ActiveTimeStart, "ActiveTimeStart should match input")
	}
	if updatedLimit.ActiveTimeEnd != nil {
		assert.Equal(t, endTime, *updatedLimit.ActiveTimeEnd, "ActiveTimeEnd should match input")
	}
}

// TestUpdateLimit_CustomPeriod_RED verifies that custom period fields are currently IGNORED.
// This test should FAIL initially (RED), proving the bug exists.
func TestUpdateLimit_CustomPeriod_RED(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRepo := NewMockLimitRepository(ctrl)
	clk := testutil.NewDefaultMockClock()

	cmd, err := NewUpdateLimitCommand(mockRepo, clk, nil)
	require.NoError(t, err)

	limitID := testutil.MustDeterministicUUID(1)
	now := testutil.FixedTime().UTC()

	// Initial dates: Nov 27-28, 2026 (Black Friday)
	initialStart := time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)
	initialEnd := time.Date(2026, 11, 28, 23, 59, 59, 0, time.UTC)

	existingLimit := &model.Limit{
		ID:              limitID,
		Name:            "Black Friday Limit",
		LimitType:       model.LimitTypeCustom,
		MaxAmount:       decimal.RequireFromString("10000.00"),
		Currency:        "BRL",
		Scopes:          []model.Scope{{AccountID: testutil.UUIDPtr(testutil.MustDeterministicUUID(2))}},
		Status:          model.LimitStatusActive,
		CustomStartDate: &initialStart,
		CustomEndDate:   &initialEnd,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	// Mock: GetByID returns existing limit
	mockRepo.EXPECT().
		GetByID(gomock.Any(), limitID).
		Return(existingLimit, nil)

	// Mock: Update should be called with new custom dates
	mockRepo.EXPECT().
		Update(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, limit *model.Limit) error {
			// Verify custom dates were updated
			assert.NotNil(t, limit.CustomStartDate, "Update should have CustomStartDate set")
			assert.NotNil(t, limit.CustomEndDate, "Update should have CustomEndDate set")
			return nil
		})

	// Act: Try to UPDATE to Cyber Monday (Dec 1-2, 2026)
	newStart := "2026-12-01T00:00:00Z"
	newEnd := "2026-12-02T23:59:59Z"

	input := &UpdateLimitInput{
		CustomStartDate: &newStart,
		CustomEndDate:   &newEnd,
	}

	updatedLimit, err := cmd.Execute(context.Background(), limitID, input)
	require.NoError(t, err)

	// Assert: Custom period should be updated (currently IGNORED - this will FAIL)
	assert.NotNil(t, updatedLimit.CustomStartDate, "CustomStartDate should be updated")
	assert.NotNil(t, updatedLimit.CustomEndDate, "CustomEndDate should be updated")

	if updatedLimit.CustomStartDate != nil {
		expectedStart, parseErr := time.Parse(time.RFC3339, newStart)
		require.NoError(t, parseErr, "newStart should be valid RFC3339")
		assert.Equal(t, expectedStart.UTC(), updatedLimit.CustomStartDate.UTC(), "CustomStartDate should match input")
	}

	if updatedLimit.CustomEndDate != nil {
		expectedEnd, parseErr := time.Parse(time.RFC3339, newEnd)
		require.NoError(t, parseErr, "newEnd should be valid RFC3339")
		assert.Equal(t, expectedEnd.UTC(), updatedLimit.CustomEndDate.UTC(), "CustomEndDate should match input")
	}
}
