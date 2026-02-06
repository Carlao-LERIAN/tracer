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

	"tracer/internal/testutil"
	"tracer/pkg/constant"
	"tracer/pkg/model"
)

func TestNewDeleteLimitCommand(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockRepo := NewMockLimitRepository(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)
	// No audit expected - constructor only
	cmd := NewDeleteLimitCommand(mockRepo, auditWriter)

	assert.NotNil(t, cmd)
	assert.Equal(t, mockRepo, cmd.repo)
	assert.Equal(t, auditWriter, cmd.auditWriter)
}

func TestDeleteLimitCommand_Execute_InvalidTransition_FromActive(t *testing.T) {
	ctrl := gomock.NewController(t)

	ctx := context.Background()
	limitID := testutil.MustDeterministicUUID(1)
	now := testutil.FixedTime()

	activeLimit := &model.Limit{
		ID:        limitID,
		Name:      "Test Limit",
		LimitType: model.LimitTypeDaily,
		MaxAmount: 100000,
		Currency:  "USD",
		Scopes:    []model.Scope{{AccountID: testutil.UUIDPtr(testutil.MustDeterministicUUID(2))}},
		Status:    model.LimitStatusActive,
		CreatedAt: now,
		UpdatedAt: now,
	}

	mockRepo := NewMockLimitRepository(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)

	mockRepo.EXPECT().
		GetByID(gomock.Any(), limitID).
		Return(activeLimit, nil)
	// No UpdateStatus call expected - invalid transition

	// No audit event expected - invalid transition error
	auditWriter.EXPECT().
		RecordLimitEvent(
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
		).
		Times(0)

	cmd := NewDeleteLimitCommand(mockRepo, auditWriter)

	err := cmd.Execute(ctx, limitID)

	// ACTIVE → DELETED is not a valid transition (ACTIVE can only go to INACTIVE)
	require.Error(t, err)
	assert.ErrorIs(t, err, constant.ErrLimitInvalidStatusChange)
}

func TestDeleteLimitCommand_Execute_Success_FromInactive(t *testing.T) {
	ctrl := gomock.NewController(t)

	ctx := context.Background()
	limitID := testutil.MustDeterministicUUID(10)
	now := testutil.FixedTime()

	inactiveLimit := &model.Limit{
		ID:        limitID,
		Name:      "Test Limit",
		LimitType: model.LimitTypeDaily,
		MaxAmount: 100000,
		Currency:  "USD",
		Scopes:    []model.Scope{{AccountID: testutil.UUIDPtr(testutil.MustDeterministicUUID(11))}},
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
		UpdateStatus(gomock.Any(), limitID, model.LimitStatusDeleted, gomock.AssignableToTypeOf(time.Time{})).
		Return(nil)

	// Audit event should be called exactly once with specific parameters
	auditWriter.EXPECT().
		RecordLimitEvent(
			gomock.Any(),                                // ctx (may have trace info)
			model.AuditEventLimitDeleted,                // eventType
			model.AuditActionDelete,                     // action
			limitID,                                     // limitID
			gomock.AssignableToTypeOf(map[string]any{}), // beforeState (INACTIVE)
			gomock.AssignableToTypeOf(map[string]any{}), // afterState (empty map for delete)
			"Limit deleted via API",                     // description
			gomock.Any(),                                // clientIP (may be 0.0.0.0 from context)
		).
		Times(1).
		Return(nil)

	cmd := NewDeleteLimitCommand(mockRepo, auditWriter)

	err := cmd.Execute(ctx, limitID)

	require.NoError(t, err)
}

func TestDeleteLimitCommand_Execute_AlreadyDeleted_Idempotent(t *testing.T) {
	ctrl := gomock.NewController(t)

	ctx := context.Background()
	limitID := testutil.MustDeterministicUUID(20)
	now := testutil.FixedTime()

	deletedLimit := &model.Limit{
		ID:        limitID,
		Name:      "Test Limit",
		LimitType: model.LimitTypeDaily,
		MaxAmount: 100000,
		Currency:  "USD",
		Scopes:    []model.Scope{{AccountID: testutil.UUIDPtr(testutil.MustDeterministicUUID(21))}},
		Status:    model.LimitStatusDeleted,
		CreatedAt: now,
		UpdatedAt: now,
	}

	mockRepo := NewMockLimitRepository(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)

	mockRepo.EXPECT().
		GetByID(gomock.Any(), limitID).
		Return(deletedLimit, nil)
	// No UpdateStatus call expected for idempotent operation
	// No audit event expected - already deleted (idempotent no-op)
	auditWriter.EXPECT().
		RecordLimitEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Times(0)

	cmd := NewDeleteLimitCommand(mockRepo, auditWriter)

	err := cmd.Execute(ctx, limitID)

	require.NoError(t, err)
}

func TestDeleteLimitCommand_Execute_LimitNotFound(t *testing.T) {
	ctrl := gomock.NewController(t)

	ctx := context.Background()
	limitID := testutil.MustDeterministicUUID(30)

	mockRepo := NewMockLimitRepository(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)

	mockRepo.EXPECT().
		GetByID(gomock.Any(), limitID).
		Return(nil, constant.ErrLimitNotFound)
	// No audit event expected - limit not found
	auditWriter.EXPECT().
		RecordLimitEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Times(0)

	cmd := NewDeleteLimitCommand(mockRepo, auditWriter)

	err := cmd.Execute(ctx, limitID)

	require.Error(t, err)
	assert.ErrorIs(t, err, constant.ErrLimitNotFound)
}

func TestDeleteLimitCommand_Execute_GetByIDError(t *testing.T) {
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

	cmd := NewDeleteLimitCommand(mockRepo, auditWriter)

	err := cmd.Execute(ctx, limitID)

	require.Error(t, err)
	// Verify the original error is wrapped and preserved
	assert.ErrorIs(t, err, dbErr, "should wrap the original database error")
}

func TestDeleteLimitCommand_Execute_UpdateStatusError(t *testing.T) {
	ctrl := gomock.NewController(t)

	ctx := context.Background()
	limitID := testutil.MustDeterministicUUID(50)
	now := testutil.FixedTime()

	// Use INACTIVE status since ACTIVE → DELETED is not allowed
	inactiveLimit := &model.Limit{
		ID:        limitID,
		Name:      "Test Limit",
		LimitType: model.LimitTypeDaily,
		MaxAmount: 100000,
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
		UpdateStatus(gomock.Any(), limitID, model.LimitStatusDeleted, gomock.AssignableToTypeOf(time.Time{})).
		Return(dbErr)
	// No audit event expected - UpdateStatus failed before audit
	auditWriter.EXPECT().
		RecordLimitEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Times(0)

	cmd := NewDeleteLimitCommand(mockRepo, auditWriter)

	err := cmd.Execute(ctx, limitID)

	require.Error(t, err)
	// Verify the original error is wrapped and preserved
	assert.ErrorIs(t, err, dbErr, "should wrap the original database error")
}

func TestDeleteLimitCommand_Execute_NilUUID(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockRepo := NewMockLimitRepository(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)
	// No mock expectations - should fail before repository call
	// No audit event expected - validation failed
	auditWriter.EXPECT().
		RecordLimitEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Times(0)

	cmd := NewDeleteLimitCommand(mockRepo, auditWriter)

	err := cmd.Execute(context.Background(), uuid.Nil)

	require.Error(t, err)
	assert.ErrorIs(t, err, constant.ErrLimitInvalidID)
}

func TestDeleteLimitCommand_Execute_ContextCancellation(t *testing.T) {
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

	cmd := NewDeleteLimitCommand(mockRepo, auditWriter)
	err := cmd.Execute(ctx, testutil.MustDeterministicUUID(60))

	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}
