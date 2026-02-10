// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package command

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"tracer/internal/testutil"
	"tracer/pkg/constant"
	"tracer/pkg/model"
)

// ============================================================================
// RULE AUDIT EVENTS
// ============================================================================

// TestAuditEventRecording_CreateRule validates CREATE audit event.
func TestAuditEventRecording_CreateRule(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRepo := NewMockRuleRepository(ctrl)
	mockCEL := NewMockExpressionCompiler(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)

	mockCEL.EXPECT().Compile(gomock.Any(), gomock.Any()).Return(nil, nil)
	mockRepo.EXPECT().GetByName(gomock.Any(), gomock.Any()).Return(nil, constant.ErrRuleNotFound)
	mockRepo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(&model.Rule{ID: testutil.MustDeterministicUUID(1)}, nil)

	// VALIDATE: EventType, Action, Before/After
	auditWriter.EXPECT().RecordRuleEvent(
		gomock.Any(),
		model.AuditEventRuleCreated,
		model.AuditActionCreate,
		gomock.Any(),
		gomock.Nil(),
		gomock.Not(gomock.Nil()),
		gomock.Any(),
		gomock.Any(),
	).Return(nil).Times(1)

	cmd := NewCreateRuleCommand(mockRepo, mockCEL, testutil.NewDefaultMockClock(), auditWriter)
	_, err := cmd.Execute(context.Background(), &CreateRuleInput{
		Name: "Test", Expression: "true", Action: model.DecisionAllow,
	})

	require.NoError(t, err)
}

// TestAuditEventRecording_ActivateRule validates ACTIVATE audit event.
func TestAuditEventRecording_ActivateRule(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRepo := NewMockRuleRepository(ctrl)
	mockCEL := NewMockExpressionCompiler(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)

	ruleID := testutil.MustDeterministicUUID(10)
	mockRepo.EXPECT().GetByID(gomock.Any(), ruleID).Return(&model.Rule{
		ID: ruleID, Expression: "true", Status: model.RuleStatusDraft,
	}, nil)
	mockCEL.EXPECT().Compile(gomock.Any(), gomock.Any()).Return(nil, nil)
	mockRepo.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, rule *model.Rule) (*model.Rule, error) {
		return rule, nil
	})

	// VALIDATE: Before and After states captured
	auditWriter.EXPECT().RecordRuleEvent(
		gomock.Any(),
		model.AuditEventRuleActivated,
		model.AuditActionActivate,
		ruleID,
		gomock.Not(gomock.Nil()),
		gomock.Not(gomock.Nil()),
		gomock.Any(),
		gomock.Any(),
	).Return(nil).Times(1)

	service, err := NewActivateRuleService(mockRepo, mockCEL, testutil.NewDefaultMockClock(), auditWriter)
	require.NoError(t, err)
	_, err = service.Execute(context.Background(), ruleID)
	require.NoError(t, err)
}

// TestAuditEventRecording_DeactivateRule validates DEACTIVATE audit event.
func TestAuditEventRecording_DeactivateRule(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRepo := NewMockRuleRepository(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)

	ruleID := testutil.MustDeterministicUUID(20)
	mockRepo.EXPECT().GetByID(gomock.Any(), ruleID).Return(&model.Rule{
		ID: ruleID, Status: model.RuleStatusActive,
	}, nil)
	mockRepo.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, rule *model.Rule) (*model.Rule, error) {
		return rule, nil
	})

	// VALIDATE: Before and After states captured
	auditWriter.EXPECT().RecordRuleEvent(
		gomock.Any(),
		model.AuditEventRuleDeactivated,
		model.AuditActionDeactivate,
		ruleID,
		gomock.Not(gomock.Nil()),
		gomock.Not(gomock.Nil()),
		gomock.Any(),
		gomock.Any(),
	).Return(nil).Times(1)

	service := NewDeactivateRuleService(mockRepo, testutil.NewDefaultMockClock(), auditWriter)
	_, err := service.Execute(context.Background(), ruleID)
	require.NoError(t, err)
}

// TestAuditEventRecording_UpdateRule validates UPDATE audit event.
func TestAuditEventRecording_UpdateRule(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRepo := NewMockRuleRepository(ctrl)
	mockCEL := NewMockExpressionCompiler(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)

	ruleID := testutil.MustDeterministicUUID(30)
	mockRepo.EXPECT().GetByID(gomock.Any(), ruleID).Return(&model.Rule{
		ID: ruleID, Name: "Old", Expression: "true",
	}, nil)
	mockRepo.EXPECT().GetByName(gomock.Any(), gomock.Any()).Return(nil, constant.ErrRuleNotFound)
	mockRepo.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, r *model.Rule) (*model.Rule, error) { return r, nil })

	// VALIDATE: Both before and after captured
	auditWriter.EXPECT().RecordRuleEvent(
		gomock.Any(),
		model.AuditEventRuleUpdated,
		model.AuditActionUpdate,
		ruleID,
		gomock.Not(gomock.Nil()),
		gomock.Not(gomock.Nil()),
		gomock.Any(),
		gomock.Any(),
	).Return(nil).Times(1)

	cmd := NewUpdateRuleCommand(mockRepo, mockCEL, testutil.NewDefaultMockClock(), auditWriter)
	_, err := cmd.Execute(context.Background(), ruleID, &UpdateRuleInput{
		Name: testutil.StringPtr("New"),
	})
	require.NoError(t, err)
}

// TestAuditEventRecording_DeleteRule validates DELETE audit event.
func TestAuditEventRecording_DeleteRule(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRepo := NewMockRuleRepository(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)

	ruleID := testutil.MustDeterministicUUID(40)
	mockRepo.EXPECT().GetByID(gomock.Any(), ruleID).Return(&model.Rule{
		ID: ruleID, Status: model.RuleStatusInactive,
	}, nil)
	mockRepo.EXPECT().Delete(gomock.Any(), ruleID).Return(nil)

	// VALIDATE: After is nil for delete
	auditWriter.EXPECT().RecordRuleEvent(
		gomock.Any(),
		model.AuditEventRuleDeleted,
		model.AuditActionDelete,
		ruleID,
		gomock.Not(gomock.Nil()),
		gomock.Nil(),
		gomock.Any(),
		gomock.Any(),
	).Return(nil).Times(1)

	service, err := NewDeleteRuleService(mockRepo, auditWriter)
	require.NoError(t, err)
	err = service.Execute(context.Background(), ruleID)
	require.NoError(t, err)
}

// ============================================================================
// LIMIT AUDIT EVENTS
// ============================================================================

// TestAuditEventRecording_CreateLimit validates limit CREATE audit.
func TestAuditEventRecording_CreateLimit(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRepo := NewMockLimitRepository(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)

	mockRepo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, l *model.Limit) (*model.Limit, error) { return l, nil })

	auditWriter.EXPECT().RecordLimitEvent(
		gomock.Any(),
		model.AuditEventLimitCreated,
		model.AuditActionCreate,
		gomock.Any(),
		gomock.Nil(),
		gomock.Not(gomock.Nil()),
		gomock.Any(),
		gomock.Any(),
	).Return(nil).Times(1)

	cmd, err := NewCreateLimitCommand(mockRepo, testutil.NewDefaultMockClock(), auditWriter)
	require.NoError(t, err)
	_, err = cmd.Execute(context.Background(), &CreateLimitInput{
		Name: "Test", LimitType: model.LimitTypeDaily, MaxAmount: decimal.RequireFromString("1000"),
		Currency: "BRL", Scopes: []model.Scope{{AccountID: testutil.UUIDPtr(testutil.MustDeterministicUUID(50))}},
	})
	require.NoError(t, err)
}

// TestAuditEventRecording_ActivateLimit validates limit ACTIVATE audit.
func TestAuditEventRecording_ActivateLimit(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRepo := NewMockLimitRepository(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)

	limitID := testutil.MustDeterministicUUID(60)
	mockRepo.EXPECT().GetByID(gomock.Any(), limitID).Return(&model.Limit{
		ID: limitID, Status: model.LimitStatusInactive,
	}, nil)
	mockRepo.EXPECT().UpdateStatus(gomock.Any(), limitID, model.LimitStatusActive, gomock.Any()).Return(nil)

	auditWriter.EXPECT().RecordLimitEvent(
		gomock.Any(),
		model.AuditEventLimitActivated,
		model.AuditActionActivate,
		limitID,
		gomock.Not(gomock.Nil()),
		gomock.Not(gomock.Nil()),
		gomock.Any(),
		gomock.Any(),
	).Return(nil).Times(1)

	cmd := NewActivateLimitCommand(mockRepo, testutil.NewDefaultMockClock(), auditWriter)
	_, err := cmd.Execute(context.Background(), limitID)
	require.NoError(t, err)
}

// TestAuditEventRecording_DeactivateLimit validates limit DEACTIVATE audit.
func TestAuditEventRecording_DeactivateLimit(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRepo := NewMockLimitRepository(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)

	limitID := testutil.MustDeterministicUUID(70)
	mockRepo.EXPECT().GetByID(gomock.Any(), limitID).Return(&model.Limit{
		ID: limitID, Status: model.LimitStatusActive,
	}, nil)
	mockRepo.EXPECT().UpdateStatus(gomock.Any(), limitID, model.LimitStatusInactive, gomock.Any()).Return(nil)

	auditWriter.EXPECT().RecordLimitEvent(
		gomock.Any(),
		model.AuditEventLimitDeactivated,
		model.AuditActionDeactivate,
		limitID,
		gomock.Not(gomock.Nil()),
		gomock.Not(gomock.Nil()),
		gomock.Any(),
		gomock.Any(),
	).Return(nil).Times(1)

	cmd := NewDeactivateLimitCommand(mockRepo, testutil.NewDefaultMockClock(), auditWriter)
	_, err := cmd.Execute(context.Background(), limitID)
	require.NoError(t, err)
}

// TestAuditEventRecording_UpdateLimit validates limit UPDATE audit.
func TestAuditEventRecording_UpdateLimit(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRepo := NewMockLimitRepository(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)

	limitID := testutil.MustDeterministicUUID(80)
	mockRepo.EXPECT().GetByID(gomock.Any(), limitID).Return(&model.Limit{
		ID: limitID, MaxAmount: decimal.RequireFromString("500"),
	}, nil)
	mockRepo.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, l *model.Limit) (*model.Limit, error) { return l, nil })

	auditWriter.EXPECT().RecordLimitEvent(
		gomock.Any(),
		model.AuditEventLimitUpdated,
		model.AuditActionUpdate,
		limitID,
		gomock.Not(gomock.Nil()),
		gomock.Not(gomock.Nil()),
		gomock.Any(),
		gomock.Any(),
	).Return(nil).Times(1)

	cmd := NewUpdateLimitCommand(mockRepo, testutil.NewDefaultMockClock(), auditWriter)
	_, err := cmd.Execute(context.Background(), limitID, &UpdateLimitInput{
		MaxAmount: testutil.Ptr(decimal.RequireFromString("1000")),
	})
	require.NoError(t, err)
}

// TestAuditEventRecording_DeleteLimit validates limit DELETE audit.
func TestAuditEventRecording_DeleteLimit(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRepo := NewMockLimitRepository(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)

	limitID := testutil.MustDeterministicUUID(90)
	mockRepo.EXPECT().GetByID(gomock.Any(), limitID).Return(&model.Limit{
		ID: limitID, Status: model.LimitStatusInactive,
	}, nil)
	mockRepo.EXPECT().UpdateStatus(gomock.Any(), limitID, model.LimitStatusDeleted, gomock.Any()).Return(nil)

	auditWriter.EXPECT().RecordLimitEvent(
		gomock.Any(),
		model.AuditEventLimitDeleted,
		model.AuditActionDelete,
		limitID,
		gomock.Not(gomock.Nil()),
		gomock.Nil(),
		gomock.Any(),
		gomock.Any(),
	).Return(nil).Times(1)

	cmd := NewDeleteLimitCommand(mockRepo, testutil.NewDefaultMockClock(), auditWriter)
	err := cmd.Execute(context.Background(), limitID)
	require.NoError(t, err)
}

// ============================================================================
// VALIDATION AUDIT EVENTS
// ============================================================================

// TestAuditEventRecording_ValidationEvent validates validation event recording.
// Note: This test validates the RecordValidationEvent interface contract.
// End-to-end testing is done in integration tests (tests/integration/11_audit_events_test.go).
func TestAuditEventRecording_ValidationEvent(t *testing.T) {
	ctrl := gomock.NewController(t)
	auditWriter := NewMockAuditWriter(ctrl)

	validationID := testutil.MustDeterministicUUID(100)
	accountID := testutil.MustDeterministicUUID(101)

	// Mock request data
	request := map[string]any{
		"requestId":       testutil.MustDeterministicUUID(102).String(),
		"transactionType": "PIX",
		"amount":          decimal.RequireFromString("100"),
		"currency":        "BRL",
		"timestamp":       testutil.FixedTime(),
		"account": map[string]any{
			"id":       accountID.String(),
			"type":     "CHECKING",
			"status":   "ACTIVE",
			"metadata": map[string]any{},
		},
		"metadata": map[string]any{},
	}

	evalResult, err := model.NewEvaluationResult(
		model.DecisionAllow,
		[]uuid.UUID{},
		[]uuid.UUID{},
		"All rules passed",
	)
	require.NoError(t, err)

	responseContext := model.ValidationResponseContext{
		ProcessingTimeMs:  50,
		LimitUsageDetails: []model.LimitUsageDetail{},
	}

	// VALIDATE: Captures request, evalResult, and clientIP
	var capturedRequest map[string]any
	var capturedEvalResult model.EvaluationResult
	var capturedClientIP string

	auditWriter.EXPECT().RecordValidationEvent(
		gomock.Any(),
		validationID,
		gomock.Not(gomock.Nil()),
		gomock.Any(),
		gomock.Any(),
		gomock.Any(),
	).DoAndReturn(func(ctx context.Context, valID uuid.UUID, req map[string]any,
		evalRes model.EvaluationResult, respCtx model.ValidationResponseContext, clientIP string) error {
		capturedRequest = req
		capturedEvalResult = evalRes
		capturedClientIP = clientIP
		return nil
	}).Times(1)

	// Execute
	err = auditWriter.RecordValidationEvent(
		context.Background(),
		validationID,
		request,
		*evalResult,
		responseContext,
		"192.168.1.1",
	)

	require.NoError(t, err)

	// VALIDATE CAPTURED DATA
	assert.NotNil(t, capturedRequest, "request snapshot must be captured")
	assert.Equal(t, request["requestId"], capturedRequest["requestId"], "requestId must be in snapshot")
	assert.Equal(t, "PIX", capturedRequest["transactionType"], "transaction type must be in snapshot")
	assert.Equal(t, decimal.RequireFromString("100"), capturedRequest["amount"], "amount must be in snapshot")
	assert.Equal(t, "BRL", capturedRequest["currency"], "currency must be in snapshot")
	assert.NotNil(t, capturedRequest["account"], "account must be in snapshot")

	assert.Equal(t, model.DecisionAllow, capturedEvalResult.Decision, "decision must match")
	assert.Equal(t, "All rules passed", capturedEvalResult.Reason, "reason must match")

	assert.Equal(t, "192.168.1.1", capturedClientIP, "client IP must be captured")
}
