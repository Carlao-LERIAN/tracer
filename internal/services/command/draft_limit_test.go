// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package command

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/shopspring/decimal"

	"tracer/internal/testutil"
	"tracer/pkg/constant"
	"tracer/pkg/model"
)

func TestNewDraftLimitCommand(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockRepo := NewMockLimitRepository(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)
	// No audit expected - constructor only
	cmd := NewDraftLimitCommand(mockRepo, testutil.NewDefaultMockClock(), auditWriter)

	assert.NotNil(t, cmd)
	assert.Equal(t, mockRepo, cmd.repo)
	assert.NotNil(t, cmd.clock)
	assert.Equal(t, auditWriter, cmd.auditWriter)
}

func TestDraftLimitCommand_Execute_Success(t *testing.T) {
	ctrl := gomock.NewController(t)

	ctx := context.Background()
	limitID := testutil.MustDeterministicUUID(100)
	testStartTime := testutil.FixedTime()

	inactiveLimit := &model.Limit{
		ID:        limitID,
		Name:      "Test Limit",
		LimitType: model.LimitTypeDaily,
		MaxAmount: decimal.RequireFromString("1000"),
		Currency:  "USD",
		Scopes:    []model.Scope{{AccountID: testutil.UUIDPtr(testutil.MustDeterministicUUID(101))}},
		Status:    model.LimitStatusInactive,
		CreatedAt: testStartTime.Add(-time.Hour), // Created an hour ago
		UpdatedAt: testStartTime.Add(-time.Hour), // Last updated an hour ago
	}

	mockRepo := NewMockLimitRepository(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)

	// Capture the timestamp passed to UpdateStatus to verify it's sensible
	var capturedTimestamp time.Time

	mockRepo.EXPECT().
		GetByID(gomock.Any(), limitID).
		Return(inactiveLimit, nil)
	mockRepo.EXPECT().
		UpdateStatus(gomock.Any(), limitID, model.LimitStatusDraft, gomock.AssignableToTypeOf(time.Time{})).
		Do(func(_ context.Context, _ uuid.UUID, _ model.LimitStatus, ts time.Time) {
			capturedTimestamp = ts
		}).
		Return(nil)

	// Audit event should be called exactly once with specific parameters
	auditWriter.EXPECT().
		RecordLimitEvent(
			gomock.Any(),                          // ctx
			model.AuditEventLimitDrafted,          // eventType
			model.AuditActionDraft,                // action
			limitID,                               // limitID
			gomock.Any(),                          // beforeState
			gomock.Any(),                          // afterState
			"Limit transitioned to draft via API", // description
			gomock.Any(),                          // clientIP
		).
		Times(1).
		Return(nil)

	cmd := NewDraftLimitCommand(mockRepo, testutil.NewDefaultMockClock(), auditWriter)

	result, err := cmd.Execute(ctx, limitID)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, limitID, result.ID)
	assert.Equal(t, model.LimitStatusDraft, result.Status)
	assert.False(t, result.UpdatedAt.IsZero(), "UpdatedAt should be set")

	// Verify the timestamp passed to repository is sensible (>= test start time)
	require.False(t, capturedTimestamp.IsZero(), "Captured timestamp should not be zero")
	require.True(t, !capturedTimestamp.Before(testStartTime),
		"Timestamp passed to UpdateStatus should be >= test start time, got %v (test started at %v)",
		capturedTimestamp, testStartTime)

	// Verify result.UpdatedAt matches what was passed to repository
	assert.Equal(t, capturedTimestamp, result.UpdatedAt,
		"result.UpdatedAt should match timestamp passed to repository")
}

func TestDraftLimitCommand_Execute_AlreadyDraft_Idempotent(t *testing.T) {
	ctrl := gomock.NewController(t)

	ctx := context.Background()
	limitID := testutil.MustDeterministicUUID(102)
	now := testutil.FixedTime()

	draftLimit := &model.Limit{
		ID:        limitID,
		Name:      "Test Limit",
		LimitType: model.LimitTypeDaily,
		MaxAmount: decimal.RequireFromString("1000"),
		Currency:  "USD",
		Scopes:    []model.Scope{{AccountID: testutil.UUIDPtr(testutil.MustDeterministicUUID(103))}},
		Status:    model.LimitStatusDraft,
		CreatedAt: now,
		UpdatedAt: now,
	}

	mockRepo := NewMockLimitRepository(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)

	mockRepo.EXPECT().
		GetByID(gomock.Any(), limitID).
		Return(draftLimit, nil)
	// No UpdateStatus call expected for idempotent operation
	// No audit event expected - already draft (idempotent no-op)
	auditWriter.EXPECT().
		RecordLimitEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Times(0)

	cmd := NewDraftLimitCommand(mockRepo, testutil.NewDefaultMockClock(), auditWriter)

	result, err := cmd.Execute(ctx, limitID)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, limitID, result.ID)
	assert.Equal(t, model.LimitStatusDraft, result.Status, "Status should remain DRAFT")
}

func TestDraftLimitCommand_Execute_LimitNotFound(t *testing.T) {
	ctrl := gomock.NewController(t)

	ctx := context.Background()
	limitID := testutil.MustDeterministicUUID(104)

	mockRepo := NewMockLimitRepository(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)

	mockRepo.EXPECT().
		GetByID(gomock.Any(), limitID).
		Return(nil, constant.ErrLimitNotFound)
	// No audit event expected - limit not found
	auditWriter.EXPECT().
		RecordLimitEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Times(0)

	cmd := NewDraftLimitCommand(mockRepo, testutil.NewDefaultMockClock(), auditWriter)

	result, err := cmd.Execute(ctx, limitID)

	require.Error(t, err)
	assert.ErrorIs(t, err, constant.ErrLimitNotFound)
	assert.Nil(t, result)
}

func TestDraftLimitCommand_Execute_InvalidTransition_FromActive(t *testing.T) {
	ctrl := gomock.NewController(t)

	ctx := context.Background()
	limitID := testutil.MustDeterministicUUID(105)
	now := testutil.FixedTime()

	activeLimit := &model.Limit{
		ID:        limitID,
		Name:      "Test Limit",
		LimitType: model.LimitTypeDaily,
		MaxAmount: decimal.RequireFromString("1000"),
		Currency:  "USD",
		Scopes:    []model.Scope{{AccountID: testutil.UUIDPtr(testutil.MustDeterministicUUID(106))}},
		Status:    model.LimitStatusActive,
		CreatedAt: now,
		UpdatedAt: now,
	}

	mockRepo := NewMockLimitRepository(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)

	mockRepo.EXPECT().
		GetByID(gomock.Any(), limitID).
		Return(activeLimit, nil)
	// No audit event expected - invalid transition error
	auditWriter.EXPECT().
		RecordLimitEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Times(0)

	cmd := NewDraftLimitCommand(mockRepo, testutil.NewDefaultMockClock(), auditWriter)

	result, err := cmd.Execute(ctx, limitID)

	require.Error(t, err)
	assert.ErrorIs(t, err, constant.ErrLimitInvalidStatusChange)
	assert.Nil(t, result)
}

func TestDraftLimitCommand_Execute_InvalidTransition_FromDeleted(t *testing.T) {
	ctrl := gomock.NewController(t)

	ctx := context.Background()
	limitID := testutil.MustDeterministicUUID(107)
	now := testutil.FixedTime()

	deletedLimit := &model.Limit{
		ID:        limitID,
		Name:      "Test Limit",
		LimitType: model.LimitTypeDaily,
		MaxAmount: decimal.RequireFromString("1000"),
		Currency:  "USD",
		Scopes:    []model.Scope{{AccountID: testutil.UUIDPtr(testutil.MustDeterministicUUID(108))}},
		Status:    model.LimitStatusDeleted,
		CreatedAt: now,
		UpdatedAt: now,
	}

	mockRepo := NewMockLimitRepository(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)

	mockRepo.EXPECT().
		GetByID(gomock.Any(), limitID).
		Return(deletedLimit, nil)
	// No audit event expected - invalid transition error
	auditWriter.EXPECT().
		RecordLimitEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Times(0)

	cmd := NewDraftLimitCommand(mockRepo, testutil.NewDefaultMockClock(), auditWriter)

	result, err := cmd.Execute(ctx, limitID)

	require.Error(t, err)
	assert.ErrorIs(t, err, constant.ErrLimitInvalidStatusChange)
	assert.Nil(t, result)
}

func TestDraftLimitCommand_Execute_GetByIDError(t *testing.T) {
	ctrl := gomock.NewController(t)

	ctx := context.Background()
	limitID := testutil.MustDeterministicUUID(109)

	mockRepo := NewMockLimitRepository(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)

	dbErr := errors.New("database error")
	mockRepo.EXPECT().
		GetByID(gomock.Any(), limitID).
		Return(nil, dbErr)
	// No audit event expected - GetByID failed
	auditWriter.EXPECT().
		RecordLimitEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Times(0)

	cmd := NewDraftLimitCommand(mockRepo, testutil.NewDefaultMockClock(), auditWriter)

	result, err := cmd.Execute(ctx, limitID)

	require.Error(t, err)
	// Verify the original error is wrapped and preserved
	assert.ErrorIs(t, err, dbErr, "should wrap the original database error")
	assert.Nil(t, result)
}

func TestDraftLimitCommand_Execute_UpdateStatusError(t *testing.T) {
	ctrl := gomock.NewController(t)

	ctx := context.Background()
	limitID := testutil.MustDeterministicUUID(110)
	now := testutil.FixedTime()

	inactiveLimit := &model.Limit{
		ID:        limitID,
		Name:      "Test Limit",
		LimitType: model.LimitTypeDaily,
		MaxAmount: decimal.RequireFromString("1000"),
		Currency:  "USD",
		Scopes:    []model.Scope{{AccountID: testutil.UUIDPtr(testutil.MustDeterministicUUID(111))}},
		Status:    model.LimitStatusInactive,
		CreatedAt: now,
		UpdatedAt: now,
	}

	mockRepo := NewMockLimitRepository(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)

	dbErr := errors.New("database error")
	mockRepo.EXPECT().
		GetByID(gomock.Any(), limitID).
		Return(inactiveLimit, nil)
	mockRepo.EXPECT().
		UpdateStatus(gomock.Any(), limitID, model.LimitStatusDraft, gomock.AssignableToTypeOf(time.Time{})).
		Return(dbErr)
	// No audit event expected - UpdateStatus failed before audit
	auditWriter.EXPECT().
		RecordLimitEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Times(0)

	cmd := NewDraftLimitCommand(mockRepo, testutil.NewDefaultMockClock(), auditWriter)

	result, err := cmd.Execute(ctx, limitID)

	require.Error(t, err)
	// Verify the original error is wrapped and preserved
	assert.ErrorIs(t, err, dbErr, "should wrap the original database error")
	assert.Nil(t, result)
}

func TestDraftLimitCommand_Execute_NilUUID(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockRepo := NewMockLimitRepository(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)
	// No mock expectations - should fail before repository call
	// No audit event expected - validation failed
	auditWriter.EXPECT().
		RecordLimitEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Times(0)

	cmd := NewDraftLimitCommand(mockRepo, testutil.NewDefaultMockClock(), auditWriter)

	result, err := cmd.Execute(context.Background(), uuid.Nil)

	require.Error(t, err)
	assert.ErrorIs(t, err, constant.ErrLimitInvalidID)
	assert.Nil(t, result)
}

func TestDraftLimitCommand_Execute_AuditWriteFailure(t *testing.T) {
	ctrl := gomock.NewController(t)

	ctx := context.Background()
	limitID := testutil.MustDeterministicUUID(112)

	inactiveLimit := &model.Limit{
		ID:        limitID,
		Name:      "Test Limit",
		LimitType: model.LimitTypeDaily,
		MaxAmount: decimal.RequireFromString("1000"),
		Currency:  "USD",
		Scopes:    []model.Scope{{AccountID: testutil.UUIDPtr(testutil.MustDeterministicUUID(113))}},
		Status:    model.LimitStatusInactive,
		CreatedAt: testutil.FixedTime(),
		UpdatedAt: testutil.FixedTime(),
	}

	mockRepo := NewMockLimitRepository(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)

	mockRepo.EXPECT().
		GetByID(gomock.Any(), limitID).
		Return(inactiveLimit, nil)
	mockRepo.EXPECT().
		UpdateStatus(gomock.Any(), limitID, model.LimitStatusDraft, gomock.AssignableToTypeOf(time.Time{})).
		Return(nil)

	// Audit fails but operation should still succeed (best-effort audit)
	auditWriter.EXPECT().
		RecordLimitEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Times(1).
		Return(errors.New("audit write failed"))

	cmd := NewDraftLimitCommand(mockRepo, testutil.NewDefaultMockClock(), auditWriter)

	result, err := cmd.Execute(ctx, limitID)

	require.NoError(t, err, "operation should succeed even when audit write fails")
	require.NotNil(t, result)
	assert.Equal(t, limitID, result.ID)
	assert.Equal(t, model.LimitStatusDraft, result.Status)
}

func TestDraftLimitCommand_Execute_NilLimitFromRepo(t *testing.T) {
	ctrl := gomock.NewController(t)

	ctx := context.Background()
	limitID := testutil.MustDeterministicUUID(114)

	mockRepo := NewMockLimitRepository(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)

	// Repo returns (nil, nil) — defensive guard should catch this
	mockRepo.EXPECT().
		GetByID(gomock.Any(), limitID).
		Return(nil, nil)
	// No audit event expected - nil limit treated as not found
	auditWriter.EXPECT().
		RecordLimitEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Times(0)

	cmd := NewDraftLimitCommand(mockRepo, testutil.NewDefaultMockClock(), auditWriter)

	result, err := cmd.Execute(ctx, limitID)

	require.Error(t, err)
	assert.ErrorIs(t, err, constant.ErrLimitNotFound, "nil limit should be treated as not found")
	assert.Nil(t, result)
}

func TestDraftLimitCommand_Execute_ContextCancellation(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockRepo := NewMockLimitRepository(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)
	// No mock expectations - should fail before repository call
	// No audit event expected - context cancelled
	auditWriter.EXPECT().
		RecordLimitEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Times(0)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	cmd := NewDraftLimitCommand(mockRepo, testutil.NewDefaultMockClock(), auditWriter)
	result, err := cmd.Execute(ctx, testutil.MustDeterministicUUID(115))

	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, result)
}
