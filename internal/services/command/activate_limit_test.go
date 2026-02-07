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

func TestNewActivateLimitCommand(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockRepo := NewMockLimitRepository(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)
	// No audit expected - constructor only

	cmd := NewActivateLimitCommand(mockRepo, auditWriter)

	assert.NotNil(t, cmd)
}

func TestActivateLimitCommand_Execute_Success(t *testing.T) {
	ctrl := gomock.NewController(t)

	ctx := context.Background()
	limitID := testutil.MustDeterministicUUID(1)
	now := testutil.FixedTime()

	inactiveLimit := &model.Limit{
		ID:        limitID,
		Name:      "Test Limit",
		LimitType: model.LimitTypeDaily,
		MaxAmount: decimal.RequireFromString("1000"),
		Currency:  "USD",
		Scopes:    []model.Scope{{AccountID: testutil.UUIDPtr(testutil.MustDeterministicUUID(2))}},
		Status:    model.LimitStatusInactive,
		CreatedAt: now,
		UpdatedAt: now,
	}

	mockRepo := NewMockLimitRepository(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)

	mockRepo.EXPECT().
		GetByID(gomock.Any(), limitID).
		Return(inactiveLimit, nil)
	mockRepo.EXPECT().
		UpdateStatus(gomock.Any(), limitID, model.LimitStatusActive, gomock.Any()).
		Return(nil)

	// Audit event should be called exactly once with specific parameters
	auditWriter.EXPECT().
		RecordLimitEvent(
			gomock.Any(),                   // ctx
			model.AuditEventLimitActivated, // eventType
			model.AuditActionActivate,      // action
			limitID,                        // limitID
			gomock.Any(),                   // beforeState (map with INACTIVE status)
			gomock.Any(),                   // afterState (map with ACTIVE status)
			"Limit activated via API",      // description
			gomock.Any(),                   // clientIP
		).
		Times(1).
		Return(nil)

	cmd := NewActivateLimitCommand(mockRepo, auditWriter)

	result, err := cmd.Execute(ctx, limitID)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, limitID, result.ID)
	assert.Equal(t, model.LimitStatusActive, result.Status)
	assert.False(t, result.UpdatedAt.IsZero(), "UpdatedAt should be set")
}

func TestActivateLimitCommand_Execute_AlreadyActive_Idempotent(t *testing.T) {
	ctrl := gomock.NewController(t)

	ctx := context.Background()
	limitID := testutil.MustDeterministicUUID(10)
	now := testutil.FixedTime()

	activeLimit := &model.Limit{
		ID:        limitID,
		Name:      "Test Limit",
		LimitType: model.LimitTypeDaily,
		MaxAmount: decimal.RequireFromString("1000"),
		Currency:  "USD",
		Scopes:    []model.Scope{{AccountID: testutil.UUIDPtr(testutil.MustDeterministicUUID(11))}},
		Status:    model.LimitStatusActive,
		CreatedAt: now,
		UpdatedAt: now,
	}

	mockRepo := NewMockLimitRepository(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)

	mockRepo.EXPECT().
		GetByID(gomock.Any(), limitID).
		Return(activeLimit, nil)

	// No audit event expected - already active (idempotent no-op)
	auditWriter.EXPECT().
		RecordLimitEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Times(0)

	cmd := NewActivateLimitCommand(mockRepo, auditWriter)

	result, err := cmd.Execute(ctx, limitID)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, limitID, result.ID)
	assert.Equal(t, model.LimitStatusActive, result.Status, "Status should remain ACTIVE")
}

func TestActivateLimitCommand_Execute_LimitNotFound(t *testing.T) {
	ctrl := gomock.NewController(t)

	ctx := context.Background()
	limitID := testutil.MustDeterministicUUID(20)

	mockRepo := NewMockLimitRepository(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)

	mockRepo.EXPECT().
		GetByID(gomock.Any(), limitID).
		Return(nil, constant.ErrLimitNotFound)

	// No audit event expected - operation failed
	auditWriter.EXPECT().
		RecordLimitEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Times(0)

	cmd := NewActivateLimitCommand(mockRepo, auditWriter)

	result, err := cmd.Execute(ctx, limitID)

	require.Error(t, err)
	assert.ErrorIs(t, err, constant.ErrLimitNotFound)
	assert.Nil(t, result)
}

func TestActivateLimitCommand_Execute_InvalidTransition_FromDeleted(t *testing.T) {
	ctrl := gomock.NewController(t)

	ctx := context.Background()
	limitID := testutil.MustDeterministicUUID(30)
	now := testutil.FixedTime()

	deletedLimit := &model.Limit{
		ID:        limitID,
		Name:      "Test Limit",
		LimitType: model.LimitTypeDaily,
		MaxAmount: decimal.RequireFromString("1000"),
		Currency:  "USD",
		Scopes:    []model.Scope{{AccountID: testutil.UUIDPtr(testutil.MustDeterministicUUID(31))}},
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

	cmd := NewActivateLimitCommand(mockRepo, auditWriter)

	result, err := cmd.Execute(ctx, limitID)

	require.Error(t, err)
	assert.Nil(t, result)
}

func TestActivateLimitCommand_Execute_GetByIDError(t *testing.T) {
	ctrl := gomock.NewController(t)

	ctx := context.Background()
	limitID := testutil.MustDeterministicUUID(40)

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

	cmd := NewActivateLimitCommand(mockRepo, auditWriter)

	result, err := cmd.Execute(ctx, limitID)

	require.Error(t, err)
	// Verify the original error is wrapped and preserved
	assert.ErrorIs(t, err, dbErr, "should wrap the original database error")
	assert.Nil(t, result)
}

func TestActivateLimitCommand_Execute_UpdateStatusError(t *testing.T) {
	ctrl := gomock.NewController(t)

	ctx := context.Background()
	limitID := testutil.MustDeterministicUUID(50)
	now := testutil.FixedTime()

	inactiveLimit := &model.Limit{
		ID:        limitID,
		Name:      "Test Limit",
		LimitType: model.LimitTypeDaily,
		MaxAmount: decimal.RequireFromString("1000"),
		Currency:  "USD",
		Scopes:    []model.Scope{{AccountID: testutil.UUIDPtr(testutil.MustDeterministicUUID(51))}},
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
		UpdateStatus(gomock.Any(), limitID, model.LimitStatusActive, gomock.AssignableToTypeOf(time.Time{})).
		Return(dbErr)

	// No audit event expected - UpdateStatus failed before audit
	auditWriter.EXPECT().
		RecordLimitEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Times(0)

	cmd := NewActivateLimitCommand(mockRepo, auditWriter)

	result, err := cmd.Execute(ctx, limitID)

	require.Error(t, err)
	// Verify the original error is wrapped and preserved
	assert.ErrorIs(t, err, dbErr, "should wrap the original database error")
	assert.Nil(t, result)
}

func TestActivateLimitCommand_Execute_NilUUID(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockRepo := NewMockLimitRepository(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)

	// No audit event expected - validation failed
	auditWriter.EXPECT().
		RecordLimitEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Times(0)

	cmd := NewActivateLimitCommand(mockRepo, auditWriter)

	result, err := cmd.Execute(context.Background(), uuid.Nil)

	require.Error(t, err)
	assert.ErrorIs(t, err, constant.ErrLimitInvalidID)
	assert.Nil(t, result)
}

func TestActivateLimitCommand_Execute_ContextCancellation(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockRepo := NewMockLimitRepository(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)
	// Explicitly assert repository methods are NOT called when context is cancelled
	mockRepo.EXPECT().GetByID(gomock.Any(), gomock.Any()).Times(0)
	mockRepo.EXPECT().UpdateStatus(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)

	// No audit event expected - context cancelled early
	auditWriter.EXPECT().
		RecordLimitEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Times(0)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	cmd := NewActivateLimitCommand(mockRepo, auditWriter)
	result, err := cmd.Execute(ctx, testutil.MustDeterministicUUID(60))

	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, result)
}
