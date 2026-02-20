// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package command

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"tracer/internal/testutil"
	"tracer/pkg/constant"
	"tracer/pkg/model"
)

func TestNewDeactivateRuleService_NilRepository(t *testing.T) {
	service, err := NewDeactivateRuleService(nil, testutil.NewDefaultMockClock(), nil, nil)

	require.Nil(t, service)
	require.ErrorIs(t, err, ErrDeactivateNilRepository)
}

func TestNewDeactivateRuleService_NilClock(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRepo := NewMockRuleRepository(ctrl)

	service, err := NewDeactivateRuleService(mockRepo, nil, nil, nil)

	require.Nil(t, service)
	require.ErrorIs(t, err, ErrDeactivateNilClock)
}

func TestDeactivateRule_Success(t *testing.T) {
	ctrl := gomock.NewController(t)

	ctx := context.Background()
	ruleID := testutil.MustDeterministicUUID(1)

	inputRule := &model.Rule{
		ID:         ruleID,
		Name:       "Test Rule",
		Status:     model.RuleStatusActive,
		Expression: "amount > 1000",
	}

	mockRepo := NewMockRuleRepository(ctrl)

	mockRepo.EXPECT().
		GetByID(gomock.Any(), ruleID).
		Return(inputRule, nil)
	mockRepo.EXPECT().
		Update(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, rule *model.Rule) (*model.Rule, error) {
			assert.Equal(t, ruleID, rule.ID)
			assert.Equal(t, model.RuleStatusInactive, rule.Status)
			assert.NotNil(t, rule.DeactivatedAt, "deactivatedAt should be set")
			assert.False(t, rule.UpdatedAt.IsZero(), "updatedAt should be set")
			return rule, nil
		})

	auditWriter := NewMockAuditWriter(ctrl)
	// Audit event should be called exactly once with specific parameters
	auditWriter.EXPECT().RecordRuleEvent(
		gomock.Any(),                    // ctx (may have trace info)
		model.AuditEventRuleDeactivated, // eventType
		model.AuditActionDeactivate,     // action
		ruleID,                          // ruleID
		gomock.AssignableToTypeOf(map[string]any{}), // beforeState (ACTIVE)
		gomock.AssignableToTypeOf(map[string]any{}), // afterState (INACTIVE)
		"Rule deactivated via API",                  // description
		gomock.Any(),                                // clientIP (may be 0.0.0.0 from context)
	).Return(nil).Times(1)

	service, err := NewDeactivateRuleService(mockRepo, testutil.NewDefaultMockClock(), auditWriter, nil)
	require.NoError(t, err)

	result, err := service.Execute(ctx, ruleID)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, ruleID, result.ID)
	assert.Equal(t, model.RuleStatusInactive, result.Status)
	assert.False(t, result.UpdatedAt.IsZero(), "UpdatedAt should be set")
	assert.NotNil(t, result.DeactivatedAt, "DeactivatedAt should be set after deactivation")
	assert.Nil(t, result.ActivatedAt, "ActivatedAt should be nil after deactivation from ACTIVE")
}

func TestDeactivateRule_FromDraft_InvalidTransition(t *testing.T) {
	ctrl := gomock.NewController(t)

	ctx := context.Background()
	ruleID := testutil.MustDeterministicUUID(1)

	inputRule := &model.Rule{
		ID:         ruleID,
		Name:       "Test Rule",
		Status:     model.RuleStatusDraft,
		Expression: "amount > 1000",
	}

	mockRepo := NewMockRuleRepository(ctrl)

	mockRepo.EXPECT().
		GetByID(gomock.Any(), ruleID).
		Return(inputRule, nil)
	// No UpdateStatus call expected - invalid transition

	auditWriter := NewMockAuditWriter(ctrl)
	// No audit event expected - invalid transition error
	auditWriter.EXPECT().RecordRuleEvent(
		gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
		gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
	).Times(0)

	service, err := NewDeactivateRuleService(mockRepo, testutil.NewDefaultMockClock(), auditWriter, nil)
	require.NoError(t, err)

	_, err = service.Execute(ctx, ruleID)

	// DRAFT → INACTIVE is not a valid transition (DRAFT can only go to ACTIVE or DELETED)
	require.Error(t, err)
	var transitionErr *model.InvalidTransitionError
	require.True(t, errors.As(err, &transitionErr), "should be an InvalidTransitionError")
	assert.Equal(t, model.RuleStatusDraft, transitionErr.From)
	assert.Equal(t, model.RuleStatusInactive, transitionErr.To)
}

func TestDeactivateRule_RuleNotFound(t *testing.T) {
	ctrl := gomock.NewController(t)

	ctx := context.Background()
	ruleID := testutil.MustDeterministicUUID(1)

	mockRepo := NewMockRuleRepository(ctrl)

	mockRepo.EXPECT().
		GetByID(gomock.Any(), ruleID).
		Return(nil, constant.ErrRuleNotFound)

	auditWriter := NewMockAuditWriter(ctrl)
	// No audit event expected - rule not found
	auditWriter.EXPECT().RecordRuleEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)

	service, err := NewDeactivateRuleService(mockRepo, testutil.NewDefaultMockClock(), auditWriter, nil)
	require.NoError(t, err)

	_, err = service.Execute(ctx, ruleID)

	require.Error(t, err)
}

func TestDeactivateRule_AlreadyInactive_Idempotent(t *testing.T) {
	ctrl := gomock.NewController(t)

	ctx := context.Background()
	ruleID := testutil.MustDeterministicUUID(1)

	inputRule := &model.Rule{
		ID:         ruleID,
		Name:       "Test Rule",
		Status:     model.RuleStatusInactive,
		Expression: "amount > 1000",
	}

	mockRepo := NewMockRuleRepository(ctrl)

	mockRepo.EXPECT().
		GetByID(gomock.Any(), ruleID).
		Return(inputRule, nil)

	auditWriter := NewMockAuditWriter(ctrl)
	// No audit event expected - already inactive (idempotent no-op)
	auditWriter.EXPECT().RecordRuleEvent(
		gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
		gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
	).Times(0)

	service, err := NewDeactivateRuleService(mockRepo, testutil.NewDefaultMockClock(), auditWriter, nil)
	require.NoError(t, err)

	result, err := service.Execute(ctx, ruleID)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, ruleID, result.ID)
	assert.Equal(t, model.RuleStatusInactive, result.Status, "Status should remain INACTIVE")
}

func TestDeactivateRule_InvalidTransition(t *testing.T) {
	ctrl := gomock.NewController(t)

	ctx := context.Background()
	ruleID := testutil.MustDeterministicUUID(1)

	inputRule := &model.Rule{
		ID:         ruleID,
		Name:       "Test Rule",
		Status:     model.RuleStatusDeleted,
		Expression: "amount > 1000",
	}

	mockRepo := NewMockRuleRepository(ctrl)

	mockRepo.EXPECT().
		GetByID(gomock.Any(), ruleID).
		Return(inputRule, nil)

	auditWriter := NewMockAuditWriter(ctrl)
	// No audit event expected - invalid transition error
	auditWriter.EXPECT().RecordRuleEvent(
		gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
		gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
	).Times(0)

	service, err := NewDeactivateRuleService(mockRepo, testutil.NewDefaultMockClock(), auditWriter, nil)
	require.NoError(t, err)

	_, err = service.Execute(ctx, ruleID)

	require.Error(t, err)
	// Verify the error is the typed InvalidTransitionError
	var transitionErr *model.InvalidTransitionError
	assert.True(t, errors.As(err, &transitionErr), "should be an InvalidTransitionError")
}

func TestDeactivateRule_GetByIDError(t *testing.T) {
	ctrl := gomock.NewController(t)

	ctx := context.Background()
	ruleID := testutil.MustDeterministicUUID(1)

	mockRepo := NewMockRuleRepository(ctrl)

	mockRepo.EXPECT().
		GetByID(gomock.Any(), ruleID).
		Return(nil, errors.New("database error"))

	auditWriter := NewMockAuditWriter(ctrl)
	// No audit event expected - GetByID failed
	auditWriter.EXPECT().RecordRuleEvent(
		gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
		gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
	).Times(0)

	service, err := NewDeactivateRuleService(mockRepo, testutil.NewDefaultMockClock(), auditWriter, nil)
	require.NoError(t, err)

	_, err = service.Execute(ctx, ruleID)

	require.Error(t, err)
}

func TestDeactivateRule_UpdateStatusError(t *testing.T) {
	ctrl := gomock.NewController(t)

	ctx := context.Background()
	ruleID := testutil.MustDeterministicUUID(1)

	inputRule := &model.Rule{
		ID:         ruleID,
		Name:       "Test Rule",
		Status:     model.RuleStatusActive,
		Expression: "amount > 1000",
	}

	mockRepo := NewMockRuleRepository(ctrl)

	mockRepo.EXPECT().
		GetByID(gomock.Any(), ruleID).
		Return(inputRule, nil)
	mockRepo.EXPECT().
		Update(gomock.Any(), gomock.Any()).
		Return(nil, errors.New("database error"))

	auditWriter := NewMockAuditWriter(ctrl)
	// No audit event expected - Update failed before audit
	auditWriter.EXPECT().RecordRuleEvent(
		gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
		gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
	).Times(0)

	service, err := NewDeactivateRuleService(mockRepo, testutil.NewDefaultMockClock(), auditWriter, nil)
	require.NoError(t, err)

	_, err = service.Execute(ctx, ruleID)

	require.Error(t, err)
	// Note: inputRule in memory IS mutated by SetStatus() before persistence fails
	assert.Equal(t, model.RuleStatusInactive, inputRule.Status, "Status is mutated in memory by SetStatus()")
}
