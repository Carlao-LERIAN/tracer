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

	"tracer/pkg/constant"
	"tracer/pkg/model"
)

func TestNewDeleteRuleService_NilRepository(t *testing.T) {
	// No audit expected - constructor validation

	service, err := NewDeleteRuleService(nil, nil)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNilDeleteRuleRepository)
	assert.Nil(t, service)
}

func TestDeleteRule_Success_FromInactive(t *testing.T) {
	ctrl := gomock.NewController(t)

	ctx := context.Background()
	ruleID := uuid.New()

	rule := &model.Rule{
		ID:         ruleID,
		Name:       "Test Rule",
		Status:     model.RuleStatusInactive,
		Expression: "amount > 1000",
	}

	mockRepo := NewMockRuleRepository(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)

	mockRepo.EXPECT().
		GetByID(gomock.Any(), ruleID).
		Return(rule, nil)
	mockRepo.EXPECT().
		Delete(gomock.Any(), ruleID).
		Return(nil)

	// Capture and verify audit event arguments
	auditWriter.EXPECT().
		RecordRuleEvent(
			gomock.Any(),                // ctx
			model.AuditEventRuleDeleted, // eventType
			model.AuditActionDelete,     // action
			ruleID,                      // ruleID
			gomock.Any(),                // beforeState
			gomock.Any(),                // afterState
			"Rule deleted via API",      // description
			gomock.Any(),                // clientIP
		).
		DoAndReturn(func(_ any, _ any, _ any, _ any, beforeState map[string]any, afterState map[string]any, _ any, _ any) error {
			// Verify beforeState contains INACTIVE status
			assert.Equal(t, model.RuleStatusInactive, beforeState["status"], "beforeState should have INACTIVE status")
			assert.Equal(t, rule.Name, beforeState["name"], "beforeState should have rule name")
			// Verify afterState is empty map (deleted)
			assert.Empty(t, afterState, "afterState should be empty for delete")
			return nil
		}).
		Times(1)

	service, err := NewDeleteRuleService(mockRepo, auditWriter)
	require.NoError(t, err)

	err = service.Execute(ctx, ruleID)

	require.NoError(t, err)
}

func TestDeleteRule_Success_FromDraft(t *testing.T) {
	ctrl := gomock.NewController(t)

	ctx := context.Background()
	ruleID := uuid.New()

	rule := &model.Rule{
		ID:         ruleID,
		Name:       "Draft Rule",
		Status:     model.RuleStatusDraft,
		Expression: "amount > 500",
	}

	mockRepo := NewMockRuleRepository(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)

	mockRepo.EXPECT().
		GetByID(gomock.Any(), ruleID).
		Return(rule, nil)
	mockRepo.EXPECT().
		Delete(gomock.Any(), ruleID).
		Return(nil)

	// Capture and verify audit event arguments
	auditWriter.EXPECT().
		RecordRuleEvent(
			gomock.Any(),                // ctx
			model.AuditEventRuleDeleted, // eventType
			model.AuditActionDelete,     // action
			ruleID,                      // ruleID
			gomock.Any(),                // beforeState
			gomock.Any(),                // afterState
			"Rule deleted via API",      // description
			gomock.Any(),                // clientIP
		).
		DoAndReturn(func(_ any, _ any, _ any, _ any, beforeState map[string]any, afterState map[string]any, _ any, _ any) error {
			// Verify beforeState contains DRAFT status
			assert.Equal(t, model.RuleStatusDraft, beforeState["status"], "beforeState should have DRAFT status")
			assert.Equal(t, rule.Name, beforeState["name"], "beforeState should have rule name")
			// Verify afterState is empty map (deleted)
			assert.Empty(t, afterState, "afterState should be empty for delete")
			return nil
		}).
		Times(1)

	service, err := NewDeleteRuleService(mockRepo, auditWriter)
	require.NoError(t, err)

	err = service.Execute(ctx, ruleID)

	require.NoError(t, err)
}

func TestDeleteRule_RuleNotFound(t *testing.T) {
	ctrl := gomock.NewController(t)

	ctx := context.Background()
	ruleID := uuid.New()

	mockRepo := NewMockRuleRepository(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)

	mockRepo.EXPECT().
		GetByID(gomock.Any(), ruleID).
		Return(nil, constant.ErrRuleNotFound)
	// No audit event expected - rule not found
	auditWriter.EXPECT().
		RecordRuleEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Times(0)

	service, err := NewDeleteRuleService(mockRepo, auditWriter)
	require.NoError(t, err)

	err = service.Execute(ctx, ruleID)

	require.Error(t, err)
}

func TestDeleteRule_AlreadyDeleted_Idempotent(t *testing.T) {
	ctrl := gomock.NewController(t)

	ctx := context.Background()
	ruleID := uuid.New()

	rule := &model.Rule{
		ID:         ruleID,
		Name:       "Test Rule",
		Status:     model.RuleStatusDeleted,
		Expression: "amount > 1000",
	}

	mockRepo := NewMockRuleRepository(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)

	mockRepo.EXPECT().
		GetByID(gomock.Any(), ruleID).
		Return(rule, nil)
	// No audit event expected - already deleted (idempotent no-op)
	auditWriter.EXPECT().
		RecordRuleEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Times(0)

	service, err := NewDeleteRuleService(mockRepo, auditWriter)
	require.NoError(t, err)

	err = service.Execute(ctx, ruleID)

	require.NoError(t, err)
}

func TestDeleteRule_InvalidTransition(t *testing.T) {
	// Only ACTIVE → DELETED is invalid (ACTIVE must go to INACTIVE first)
	// DRAFT → DELETED is now valid, so removed from this test
	tests := []struct {
		name       string
		fromStatus model.RuleStatus
	}{
		{"FromActive", model.RuleStatusActive},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)

			ctx := context.Background()
			ruleID := uuid.New()

			rule := &model.Rule{
				ID:         ruleID,
				Name:       "Test Rule",
				Status:     tc.fromStatus,
				Expression: "amount > 1000",
			}

			// Record original state before Execute
			originalStatus := rule.Status
			originalName := rule.Name
			originalExpression := rule.Expression

			mockRepo := NewMockRuleRepository(ctrl)
			auditWriter := NewMockAuditWriter(ctrl)

			mockRepo.EXPECT().
				GetByID(gomock.Any(), ruleID).
				Return(rule, nil)
			// No audit event expected - invalid transition error
			auditWriter.EXPECT().
				RecordRuleEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Times(0)

			service, err := NewDeleteRuleService(mockRepo, auditWriter)
			require.NoError(t, err)

			err = service.Execute(ctx, ruleID)

			require.Error(t, err)
			// Verify the error is the typed InvalidTransitionError
			var transitionErr *model.InvalidTransitionError
			assert.True(t, errors.As(err, &transitionErr), "should be an InvalidTransitionError")

			// Verify rule was not mutated on error
			assert.Equal(t, originalStatus, rule.Status, "rule status should not be mutated on error")
			assert.Equal(t, originalName, rule.Name, "rule name should not be mutated on error")
			assert.Equal(t, originalExpression, rule.Expression, "rule expression should not be mutated on error")
		})
	}
}

func TestDeleteRule_GetByIDError(t *testing.T) {
	ctrl := gomock.NewController(t)

	ctx := context.Background()
	ruleID := uuid.New()

	mockRepo := NewMockRuleRepository(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)

	mockRepo.EXPECT().
		GetByID(gomock.Any(), ruleID).
		Return(nil, errors.New("database error"))
	// No audit event expected - GetByID failed
	auditWriter.EXPECT().
		RecordRuleEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Times(0)

	service, err := NewDeleteRuleService(mockRepo, auditWriter)
	require.NoError(t, err)

	err = service.Execute(ctx, ruleID)

	require.Error(t, err)
}

func TestDeleteRule_DeleteError(t *testing.T) {
	ctrl := gomock.NewController(t)

	ctx := context.Background()
	ruleID := uuid.New()

	rule := &model.Rule{
		ID:         ruleID,
		Name:       "Test Rule",
		Status:     model.RuleStatusInactive,
		Expression: "amount > 1000",
	}

	// Record original state before Execute
	originalStatus := rule.Status
	originalName := rule.Name
	originalExpression := rule.Expression

	mockRepo := NewMockRuleRepository(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)

	mockRepo.EXPECT().
		GetByID(gomock.Any(), ruleID).
		Return(rule, nil)
	mockRepo.EXPECT().
		Delete(gomock.Any(), ruleID).
		Return(errors.New("database error"))
	// No audit event expected - Delete failed before audit
	auditWriter.EXPECT().
		RecordRuleEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Times(0)

	service, err := NewDeleteRuleService(mockRepo, auditWriter)
	require.NoError(t, err)

	err = service.Execute(ctx, ruleID)

	require.Error(t, err)

	// Verify rule was not mutated on error
	assert.Equal(t, originalStatus, rule.Status, "rule status should not be mutated on error")
	assert.Equal(t, originalName, rule.Name, "rule name should not be mutated on error")
	assert.Equal(t, originalExpression, rule.Expression, "rule expression should not be mutated on error")
}
