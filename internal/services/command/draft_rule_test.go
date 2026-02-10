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

func TestNewDraftRuleService_NilRepository(t *testing.T) {
	svc, err := NewDraftRuleService(nil, testutil.NewDefaultMockClock(), nil)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNilRuleRepository)
	assert.Nil(t, svc)
}

func TestNewDraftRuleService_NilClock(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockRepo := NewMockRuleRepository(ctrl)
	svc, err := NewDraftRuleService(mockRepo, nil, nil)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNilClock)
	assert.Nil(t, svc)
}

func TestDraftRule_Success(t *testing.T) {
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
	mockRepo.EXPECT().
		Update(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, rule *model.Rule) (*model.Rule, error) {
			assert.Equal(t, ruleID, rule.ID)
			assert.Equal(t, model.RuleStatusDraft, rule.Status)
			assert.Nil(t, rule.ActivatedAt, "activatedAt should be nil")
			assert.Nil(t, rule.DeactivatedAt, "deactivatedAt should be nil after draft")
			assert.False(t, rule.UpdatedAt.IsZero(), "updatedAt should be set")
			return rule, nil
		})

	auditWriter := NewMockAuditWriter(ctrl)
	// Audit event should be called exactly once with specific parameters
	auditWriter.EXPECT().RecordRuleEvent(
		gomock.Any(),                // ctx (may have trace info)
		model.AuditEventRuleDrafted, // eventType
		model.AuditActionDraft,      // action
		ruleID,                      // ruleID
		gomock.AssignableToTypeOf(map[string]any{}), // beforeState (INACTIVE)
		gomock.AssignableToTypeOf(map[string]any{}), // afterState (DRAFT)
		"Rule transitioned to draft via API",        // description
		gomock.Any(),                                // clientIP (may be 0.0.0.0 from context)
	).Return(nil).Times(1)

	service, svcErr := NewDraftRuleService(mockRepo, testutil.NewDefaultMockClock(), auditWriter)
	require.NoError(t, svcErr)

	result, err := service.Execute(ctx, ruleID)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, ruleID, result.ID)
	assert.Equal(t, model.RuleStatusDraft, result.Status)
	assert.False(t, result.UpdatedAt.IsZero(), "UpdatedAt should be set")
	assert.Nil(t, result.ActivatedAt, "ActivatedAt should be nil after draft")
	assert.Nil(t, result.DeactivatedAt, "DeactivatedAt should be nil after draft")
}

func TestDraftRule_Success_AuditWriteFails(t *testing.T) {
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
	mockRepo.EXPECT().
		Update(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, rule *model.Rule) (*model.Rule, error) {
			return rule, nil
		})

	auditWriter := NewMockAuditWriter(ctrl)
	// Audit write fails - operation should still succeed (best-effort audit)
	auditWriter.EXPECT().RecordRuleEvent(
		gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
		gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
	).Return(errors.New("audit write failed")).Times(1)

	service, svcErr := NewDraftRuleService(mockRepo, testutil.NewDefaultMockClock(), auditWriter)
	require.NoError(t, svcErr)

	result, err := service.Execute(ctx, ruleID)

	// Operation succeeds despite audit failure (best-effort pattern)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, ruleID, result.ID)
	assert.Equal(t, model.RuleStatusDraft, result.Status)
}

func TestDraftRule_FromActive_InvalidTransition(t *testing.T) {
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
	// No Update call expected - invalid transition

	auditWriter := NewMockAuditWriter(ctrl)
	// No audit event expected - invalid transition error
	auditWriter.EXPECT().RecordRuleEvent(
		gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
		gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
	).Times(0)

	service, svcErr := NewDraftRuleService(mockRepo, testutil.NewDefaultMockClock(), auditWriter)
	require.NoError(t, svcErr)

	_, err := service.Execute(ctx, ruleID)

	// ACTIVE → DRAFT is not a valid transition (ACTIVE can only go to INACTIVE)
	require.Error(t, err)
	var transitionErr *model.InvalidTransitionError
	require.True(t, errors.As(err, &transitionErr), "should be an InvalidTransitionError")
	assert.Equal(t, model.RuleStatusActive, transitionErr.From)
	assert.Equal(t, model.RuleStatusDraft, transitionErr.To)
}

func TestDraftRule_RuleNotFound(t *testing.T) {
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

	service, svcErr := NewDraftRuleService(mockRepo, testutil.NewDefaultMockClock(), auditWriter)
	require.NoError(t, svcErr)

	_, err := service.Execute(ctx, ruleID)

	require.Error(t, err)
}

func TestDraftRule_AlreadyDraft_Idempotent(t *testing.T) {
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

	auditWriter := NewMockAuditWriter(ctrl)
	// No audit event expected - already draft (idempotent no-op)
	auditWriter.EXPECT().RecordRuleEvent(
		gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
		gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
	).Times(0)

	service, svcErr := NewDraftRuleService(mockRepo, testutil.NewDefaultMockClock(), auditWriter)
	require.NoError(t, svcErr)

	result, err := service.Execute(ctx, ruleID)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, ruleID, result.ID)
	assert.Equal(t, model.RuleStatusDraft, result.Status, "Status should remain DRAFT")
}

func TestDraftRule_InvalidTransition(t *testing.T) {
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

	service, svcErr := NewDraftRuleService(mockRepo, testutil.NewDefaultMockClock(), auditWriter)
	require.NoError(t, svcErr)

	_, err := service.Execute(ctx, ruleID)

	require.Error(t, err)
	// Verify the error is the typed InvalidTransitionError with correct From/To
	var transitionErr *model.InvalidTransitionError
	require.True(t, errors.As(err, &transitionErr), "should be an InvalidTransitionError")
	assert.Equal(t, model.RuleStatusDeleted, transitionErr.From)
	assert.Equal(t, model.RuleStatusDraft, transitionErr.To)
}

func TestDraftRule_GetByIDError(t *testing.T) {
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

	service, svcErr := NewDraftRuleService(mockRepo, testutil.NewDefaultMockClock(), auditWriter)
	require.NoError(t, svcErr)

	_, err := service.Execute(ctx, ruleID)

	require.Error(t, err)
}

func TestDraftRule_UpdateError(t *testing.T) {
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
	mockRepo.EXPECT().
		Update(gomock.Any(), gomock.Any()).
		Return(nil, errors.New("database error"))

	auditWriter := NewMockAuditWriter(ctrl)
	// No audit event expected - Update failed before audit
	auditWriter.EXPECT().RecordRuleEvent(
		gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
		gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
	).Times(0)

	service, svcErr := NewDraftRuleService(mockRepo, testutil.NewDefaultMockClock(), auditWriter)
	require.NoError(t, svcErr)

	_, err := service.Execute(ctx, ruleID)

	require.Error(t, err)
	// Note: inputRule in memory IS mutated by SetStatus() before persistence fails
	assert.Equal(t, model.RuleStatusDraft, inputRule.Status, "Status is mutated in memory by SetStatus()")
}
