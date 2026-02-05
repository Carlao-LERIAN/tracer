// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package command

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"tracer/internal/testutil"
	"tracer/pkg/constant"
	"tracer/pkg/model"
)

func TestNewActivateRuleService_NilRepository(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockExprCompiler := NewMockExpressionCompiler(ctrl)

	service, err := NewActivateRuleService(nil, mockExprCompiler, testutil.NewDefaultMockClock(), nil)

	require.Nil(t, service)
	require.ErrorIs(t, err, ErrActivateNilRepository)
}

func TestNewActivateRuleService_NilExpressionCompiler(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRepo := NewMockRuleRepository(ctrl)

	service, err := NewActivateRuleService(mockRepo, nil, testutil.NewDefaultMockClock(), nil)

	require.Nil(t, service)
	require.ErrorIs(t, err, ErrActivateNilExpressionCompiler)
}

func TestNewActivateRuleService_NilClock(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRepo := NewMockRuleRepository(ctrl)
	mockExprCompiler := NewMockExpressionCompiler(ctrl)

	service, err := NewActivateRuleService(mockRepo, mockExprCompiler, nil, nil)

	require.Nil(t, service)
	require.ErrorIs(t, err, ErrActivateNilClock)
}

func TestActivateRule_Success(t *testing.T) {
	ctrl := gomock.NewController(t)

	ctx := context.Background()
	ruleID := uuid.New()

	inputRule := &model.Rule{
		ID:         ruleID,
		Name:       "Test Rule",
		Status:     model.RuleStatusDraft,
		Expression: "amount > 1000",
	}

	mockRepo := NewMockRuleRepository(ctrl)
	mockExprCompiler := NewMockExpressionCompiler(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)

	mockRepo.EXPECT().
		GetByID(gomock.Any(), ruleID).
		Return(inputRule, nil)
	mockExprCompiler.EXPECT().
		Compile(gomock.Any(), inputRule.Expression).
		Return(nil, nil)
	mockRepo.EXPECT().
		Update(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, rule *model.Rule) (*model.Rule, error) {
			assert.Equal(t, ruleID, rule.ID)
			assert.Equal(t, model.RuleStatusActive, rule.Status)
			assert.NotNil(t, rule.ActivatedAt, "activatedAt should be set")
			assert.Nil(t, rule.DeactivatedAt, "deactivatedAt should be nil for activate")
			assert.False(t, rule.UpdatedAt.IsZero(), "updatedAt should be set")
			return rule, nil
		})

	// Audit event should be called exactly once with specific parameters
	auditWriter.EXPECT().
		RecordRuleEvent(
			gomock.Any(),
			model.AuditEventRuleActivated,
			model.AuditActionActivate,
			ruleID,
			gomock.Any(),
			gomock.Any(),
			"Rule activated via API",
			gomock.Any(),
		).
		Times(1).
		Return(nil)

	service, err := NewActivateRuleService(mockRepo, mockExprCompiler, testutil.NewDefaultMockClock(), auditWriter)
	require.NoError(t, err)

	result, err := service.Execute(ctx, ruleID)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, ruleID, result.ID)
	assert.Equal(t, model.RuleStatusActive, result.Status)
	assert.False(t, result.UpdatedAt.IsZero(), "UpdatedAt should be set")
}

func TestActivateRule_RuleNotFound(t *testing.T) {
	ctrl := gomock.NewController(t)

	ctx := context.Background()
	ruleID := uuid.New()

	mockRepo := NewMockRuleRepository(ctrl)
	mockExprCompiler := NewMockExpressionCompiler(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)

	mockRepo.EXPECT().
		GetByID(gomock.Any(), ruleID).
		Return(nil, constant.ErrRuleNotFound)
	// No audit event expected - operation failed
	auditWriter.EXPECT().
		RecordRuleEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Times(0)

	service, err := NewActivateRuleService(mockRepo, mockExprCompiler, testutil.NewDefaultMockClock(), auditWriter)
	require.NoError(t, err)

	_, err = service.Execute(ctx, ruleID)

	require.Error(t, err)
}

func TestActivateRule_AlreadyActive_Idempotent(t *testing.T) {
	ctrl := gomock.NewController(t)

	ctx := context.Background()
	ruleID := uuid.New()

	inputRule := &model.Rule{
		ID:         ruleID,
		Name:       "Test Rule",
		Status:     model.RuleStatusActive,
		Expression: "amount > 1000",
	}

	mockRepo := NewMockRuleRepository(ctrl)
	mockExprCompiler := NewMockExpressionCompiler(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)

	mockRepo.EXPECT().
		GetByID(gomock.Any(), ruleID).
		Return(inputRule, nil)
	// No audit event expected - already active (idempotent no-op)
	auditWriter.EXPECT().
		RecordRuleEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Times(0)

	service, err := NewActivateRuleService(mockRepo, mockExprCompiler, testutil.NewDefaultMockClock(), auditWriter)
	require.NoError(t, err)

	result, err := service.Execute(ctx, ruleID)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, ruleID, result.ID)
	assert.Equal(t, model.RuleStatusActive, result.Status, "Status should remain ACTIVE")
}

func TestActivateRule_InvalidTransition(t *testing.T) {
	ctrl := gomock.NewController(t)

	ctx := context.Background()
	ruleID := uuid.New()

	inputRule := &model.Rule{
		ID:         ruleID,
		Name:       "Test Rule",
		Status:     model.RuleStatusDeleted,
		Expression: "amount > 1000",
	}

	mockRepo := NewMockRuleRepository(ctrl)
	mockExprCompiler := NewMockExpressionCompiler(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)

	mockRepo.EXPECT().
		GetByID(gomock.Any(), ruleID).
		Return(inputRule, nil)
	mockExprCompiler.EXPECT().
		Compile(gomock.Any(), inputRule.Expression).
		Return(nil, nil)
	// No audit event expected - invalid transition error
	auditWriter.EXPECT().
		RecordRuleEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Times(0)

	service, err := NewActivateRuleService(mockRepo, mockExprCompiler, testutil.NewDefaultMockClock(), auditWriter)
	require.NoError(t, err)

	_, err = service.Execute(ctx, ruleID)

	require.Error(t, err)
	// Verify the error is the typed InvalidTransitionError
	var transitionErr *model.InvalidTransitionError
	assert.True(t, errors.As(err, &transitionErr), "should be an InvalidTransitionError")
}

func TestActivateRule_EmptyExpression(t *testing.T) {
	ctrl := gomock.NewController(t)

	ctx := context.Background()
	ruleID := uuid.New()

	inputRule := &model.Rule{
		ID:         ruleID,
		Name:       "Test Rule",
		Status:     model.RuleStatusDraft,
		Expression: "",
	}

	mockRepo := NewMockRuleRepository(ctrl)
	mockExprCompiler := NewMockExpressionCompiler(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)

	mockRepo.EXPECT().
		GetByID(gomock.Any(), ruleID).
		Return(inputRule, nil)
	// No audit event expected - empty expression validation failed
	auditWriter.EXPECT().
		RecordRuleEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Times(0)

	service, err := NewActivateRuleService(mockRepo, mockExprCompiler, testutil.NewDefaultMockClock(), auditWriter)
	require.NoError(t, err)

	_, err = service.Execute(ctx, ruleID)

	require.Error(t, err)
}

func TestActivateRule_ExpressionCompilationFailed(t *testing.T) {
	ctrl := gomock.NewController(t)

	ctx := context.Background()
	ruleID := uuid.New()

	inputRule := &model.Rule{
		ID:         ruleID,
		Name:       "Test Rule",
		Status:     model.RuleStatusDraft,
		Expression: "invalid expression syntax",
	}

	mockRepo := NewMockRuleRepository(ctrl)
	mockExprCompiler := NewMockExpressionCompiler(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)

	mockRepo.EXPECT().
		GetByID(gomock.Any(), ruleID).
		Return(inputRule, nil)
	mockExprCompiler.EXPECT().
		Compile(gomock.Any(), inputRule.Expression).
		Return(nil, errors.New("syntax error"))
	// No audit event expected - expression compilation failed
	auditWriter.EXPECT().
		RecordRuleEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Times(0)

	service, err := NewActivateRuleService(mockRepo, mockExprCompiler, testutil.NewDefaultMockClock(), auditWriter)
	require.NoError(t, err)

	_, err = service.Execute(ctx, ruleID)

	require.Error(t, err)
}

func TestActivateRule_GetByIDError(t *testing.T) {
	ctrl := gomock.NewController(t)

	ctx := context.Background()
	ruleID := uuid.New()

	mockRepo := NewMockRuleRepository(ctrl)
	mockExprCompiler := NewMockExpressionCompiler(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)

	mockRepo.EXPECT().
		GetByID(gomock.Any(), ruleID).
		Return(nil, errors.New("database error"))
	// No audit event expected - GetByID failed
	auditWriter.EXPECT().
		RecordRuleEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Times(0)

	service, err := NewActivateRuleService(mockRepo, mockExprCompiler, testutil.NewDefaultMockClock(), auditWriter)
	require.NoError(t, err)

	_, err = service.Execute(ctx, ruleID)

	require.Error(t, err)
}

func TestActivateRule_UpdateStatusError(t *testing.T) {
	ctrl := gomock.NewController(t)

	ctx := context.Background()
	ruleID := uuid.New()

	inputRule := &model.Rule{
		ID:         ruleID,
		Name:       "Test Rule",
		Status:     model.RuleStatusDraft,
		Expression: "amount > 1000",
	}

	// Capture original state to verify immutable fields don't change
	originalName := inputRule.Name
	originalExpression := inputRule.Expression

	mockRepo := NewMockRuleRepository(ctrl)
	mockExprCompiler := NewMockExpressionCompiler(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)

	mockRepo.EXPECT().
		GetByID(gomock.Any(), ruleID).
		Return(inputRule, nil)
	mockExprCompiler.EXPECT().
		Compile(gomock.Any(), inputRule.Expression).
		Return(nil, nil)
	mockRepo.EXPECT().
		Update(gomock.Any(), gomock.Any()).
		Return(nil, errors.New("database error"))
	// No audit event expected - Update failed before audit
	auditWriter.EXPECT().
		RecordRuleEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Times(0)

	service, err := NewActivateRuleService(mockRepo, mockExprCompiler, testutil.NewDefaultMockClock(), auditWriter)
	require.NoError(t, err)

	_, err = service.Execute(ctx, ruleID)

	require.Error(t, err)

	// Note: inputRule in memory IS mutated by SetStatus() before persistence fails
	// This is current behavior - domain method mutates object, then persistence may fail
	// The rule is not persisted to database on error (repository.Update fails)
	assert.Equal(t, model.RuleStatusActive, inputRule.Status, "Status is mutated in memory by SetStatus()")
	assert.Equal(t, originalName, inputRule.Name, "Name should not change")
	assert.Equal(t, originalExpression, inputRule.Expression, "Expression should not change")
}
