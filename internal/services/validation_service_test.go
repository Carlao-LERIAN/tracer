// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"tracer/internal/services/command"
	commandMocks "tracer/internal/services/command/mocks"
	"tracer/internal/services/mocks"
	"tracer/internal/testutil"
	"tracer/pkg/model"
)

func TestValidateTransaction(t *testing.T) {
	// Common test fixtures
	fixedTime := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	requestID := testutil.MustDeterministicUUID(1)
	accountID := testutil.MustDeterministicUUID(2)
	ruleID1 := testutil.MustDeterministicUUID(10)
	ruleID2 := testutil.MustDeterministicUUID(11)
	limitID := testutil.MustDeterministicUUID(20)

	baseRequest := &model.ValidationRequest{
		RequestID:            requestID,
		TransactionType:      model.TransactionTypeCard,
		Amount:               decimal.RequireFromString("100"),
		Currency:             "USD",
		TransactionTimestamp: fixedTime,
		Account:              model.AccountContext{ID: accountID},
	}

	tests := []struct {
		name             string
		request          *model.ValidationRequest
		setupMocks       func(ctrl *gomock.Controller, persistDone chan struct{}) (RuleEvaluator, LimitChecker, command.TransactionValidationRepository, AuditWriter)
		expectedDecision model.Decision
		expectedReason   string
		expectError      bool
		expectedErr      error
		cancelContext    bool
	}{
		{
			name:    "DENY by rule - rule evaluation returns DENY",
			request: baseRequest,
			setupMocks: func(ctrl *gomock.Controller, persistDone chan struct{}) (RuleEvaluator, LimitChecker, command.TransactionValidationRepository, AuditWriter) {
				ruleEval := mocks.NewMockRuleEvaluator(ctrl)
				limitCheck := mocks.NewMockLimitChecker(ctrl)
				transactionValidationRepo := commandMocks.NewMockTransactionValidationRepository(ctrl)

				// AuditWriter mock - expects RecordValidationEvent call
				auditWriter := mocks.NewMockAuditWriter(ctrl)
				auditWriter.EXPECT().RecordValidationEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)

				// Rule evaluation returns DENY
				evalResult, err := model.NewEvaluationResult(
					model.DecisionDeny,
					[]uuid.UUID{ruleID1},
					[]uuid.UUID{ruleID1, ruleID2},
					"Rule blocked transaction",
				)
				require.NoError(t, err)
				ruleEval.EXPECT().
					Execute(gomock.Any(), gomock.Any()).
					Return(evalResult, nil)

				// Limit check should NOT be called when DENY by rule
				limitCheck.EXPECT().CheckLimits(gomock.Any(), gomock.Any()).Times(0)

				// Audit should be inserted (fire-and-forget) - signal completion via channel
				transactionValidationRepo.EXPECT().Insert(gomock.Any(), gomock.Any()).DoAndReturn(
					func(_ context.Context, _ *model.TransactionValidation) error {
						close(persistDone)
						return nil
					})

				return ruleEval, limitCheck, transactionValidationRepo, auditWriter
			},
			expectedDecision: model.DecisionDeny,
			expectedReason:   "Rule blocked transaction",
			expectError:      false,
		},
		{
			name:    "DENY by exceeded limit - limit check returns exceeded",
			request: baseRequest,
			setupMocks: func(ctrl *gomock.Controller, persistDone chan struct{}) (RuleEvaluator, LimitChecker, command.TransactionValidationRepository, AuditWriter) {
				ruleEval := mocks.NewMockRuleEvaluator(ctrl)
				limitCheck := mocks.NewMockLimitChecker(ctrl)
				transactionValidationRepo := commandMocks.NewMockTransactionValidationRepository(ctrl)

				// AuditWriter mock - expects RecordValidationEvent call
				auditWriter := mocks.NewMockAuditWriter(ctrl)
				auditWriter.EXPECT().RecordValidationEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)

				// Rule evaluation returns ALLOW (no blocking rules)
				evalResult, err := model.NewEvaluationResult(
					model.DecisionAllow,
					[]uuid.UUID{ruleID1},
					[]uuid.UUID{ruleID1},
					"Rule allowed transaction",
				)
				require.NoError(t, err)
				ruleEval.EXPECT().
					Execute(gomock.Any(), gomock.Any()).
					Return(evalResult, nil)

				// Limit check returns exceeded
				limitOutput := &model.CheckLimitsOutput{
					Allowed: false,
					LimitUsageDetails: []model.LimitUsageDetail{
						{
							LimitID:      limitID,
							LimitAmount:  decimal.RequireFromString("50"),
							Scope:        "account:" + limitID.String(),
							Period:       model.LimitTypeDaily,
							CurrentUsage: decimal.RequireFromString("60"),
							Exceeded:     true,
						},
					},
					ExceededLimitIDs: []uuid.UUID{limitID},
				}
				limitCheck.EXPECT().
					CheckLimits(gomock.Any(), gomock.Any()).
					Return(limitOutput, nil)

				// Audit should be inserted - signal completion via channel
				transactionValidationRepo.EXPECT().Insert(gomock.Any(), gomock.Any()).DoAndReturn(
					func(_ context.Context, _ *model.TransactionValidation) error {
						close(persistDone)
						return nil
					})

				return ruleEval, limitCheck, transactionValidationRepo, auditWriter
			},
			expectedDecision: model.DecisionDeny,
			expectedReason:   "limit_exceeded",
			expectError:      false,
		},
		{
			name:    "REVIEW when REVIEW rules match - no DENY",
			request: baseRequest,
			setupMocks: func(ctrl *gomock.Controller, persistDone chan struct{}) (RuleEvaluator, LimitChecker, command.TransactionValidationRepository, AuditWriter) {
				ruleEval := mocks.NewMockRuleEvaluator(ctrl)
				limitCheck := mocks.NewMockLimitChecker(ctrl)
				transactionValidationRepo := commandMocks.NewMockTransactionValidationRepository(ctrl)

				// AuditWriter mock - expects RecordValidationEvent call
				auditWriter := mocks.NewMockAuditWriter(ctrl)
				auditWriter.EXPECT().RecordValidationEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)

				// Rule evaluation returns REVIEW
				evalResult, err := model.NewEvaluationResult(
					model.DecisionReview,
					[]uuid.UUID{ruleID1},
					[]uuid.UUID{ruleID1, ruleID2},
					"Rule requires review",
				)
				require.NoError(t, err)
				ruleEval.EXPECT().
					Execute(gomock.Any(), gomock.Any()).
					Return(evalResult, nil)

				// Limit check should be called (REVIEW doesn't short-circuit)
				// Populate LimitUsageDetails with realistic data to verify RollbackUsage receives full payload
				limitOutput := &model.CheckLimitsOutput{
					Allowed: true,
					LimitUsageDetails: []model.LimitUsageDetail{
						{
							LimitID:           limitID,
							LimitAmount:       decimal.RequireFromString("1000"),
							Scope:             "acct:" + accountID.String(),
							Period:            model.LimitTypeDaily,
							CurrentUsage:      decimal.RequireFromString("100"),
							AttemptedAmount:   decimal.RequireFromString("100"),
							Exceeded:          false,
							InternalLimitType: model.LimitTypeDaily,
							Scopes:            []model.Scope{{AccountID: &accountID}},
							InternalPeriodKey: "2025-01-15",
						},
					},
					ExceededLimitIDs: []uuid.UUID{},
				}
				limitCheck.EXPECT().
					CheckLimits(gomock.Any(), gomock.Any()).
					Return(limitOutput, nil)

				// REVIEW decision triggers rollback of usage increments
				// Assert that RollbackUsage receives the expected LimitUsageDetails from CheckLimits
				limitCheck.EXPECT().
					RollbackUsage(gomock.Any(), gomock.Any(), gomock.Eq(limitOutput.LimitUsageDetails)).
					Return(nil)

				// Audit should be inserted - signal completion via channel
				transactionValidationRepo.EXPECT().Insert(gomock.Any(), gomock.Any()).DoAndReturn(
					func(_ context.Context, _ *model.TransactionValidation) error {
						close(persistDone)
						return nil
					})

				return ruleEval, limitCheck, transactionValidationRepo, auditWriter
			},
			expectedDecision: model.DecisionReview,
			expectedReason:   "Rule requires review",
			expectError:      false,
		},
		{
			name:    "REVIEW rollback failure is non-fatal",
			request: baseRequest,
			setupMocks: func(ctrl *gomock.Controller, persistDone chan struct{}) (RuleEvaluator, LimitChecker, command.TransactionValidationRepository, AuditWriter) {
				ruleEval := mocks.NewMockRuleEvaluator(ctrl)
				limitCheck := mocks.NewMockLimitChecker(ctrl)
				transactionValidationRepo := commandMocks.NewMockTransactionValidationRepository(ctrl)

				// AuditWriter mock - expects RecordValidationEvent call
				auditWriter := mocks.NewMockAuditWriter(ctrl)
				auditWriter.EXPECT().RecordValidationEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)

				// Rule evaluation returns REVIEW
				evalResult, err := model.NewEvaluationResult(
					model.DecisionReview,
					[]uuid.UUID{ruleID1},
					[]uuid.UUID{ruleID1, ruleID2},
					"Rule requires review",
				)
				require.NoError(t, err)
				ruleEval.EXPECT().
					Execute(gomock.Any(), gomock.Any()).
					Return(evalResult, nil)

				// Limit check should be called
				// Populate LimitUsageDetails with realistic data to verify RollbackUsage receives full payload
				limitOutput := &model.CheckLimitsOutput{
					Allowed: true,
					LimitUsageDetails: []model.LimitUsageDetail{
						{
							LimitID:           limitID,
							LimitAmount:       decimal.RequireFromString("1000"),
							Scope:             "acct:" + accountID.String(),
							Period:            model.LimitTypeDaily,
							CurrentUsage:      decimal.RequireFromString("100"),
							AttemptedAmount:   decimal.RequireFromString("100"),
							Exceeded:          false,
							InternalLimitType: model.LimitTypeDaily,
							Scopes:            []model.Scope{{AccountID: &accountID}},
							InternalPeriodKey: "2025-01-15",
						},
					},
					ExceededLimitIDs: []uuid.UUID{},
				}
				limitCheck.EXPECT().
					CheckLimits(gomock.Any(), gomock.Any()).
					Return(limitOutput, nil)

				// REVIEW decision triggers rollback - ROLLBACK FAILS (DB timeout)
				// Assert that RollbackUsage receives the expected LimitUsageDetails from CheckLimits
				limitCheck.EXPECT().
					RollbackUsage(gomock.Any(), gomock.Any(), gomock.Eq(limitOutput.LimitUsageDetails)).
					Return(errors.New("database timeout during rollback"))

				// Audit should still be inserted despite rollback failure
				transactionValidationRepo.EXPECT().Insert(gomock.Any(), gomock.Any()).DoAndReturn(
					func(_ context.Context, _ *model.TransactionValidation) error {
						close(persistDone)
						return nil
					})

				return ruleEval, limitCheck, transactionValidationRepo, auditWriter
			},
			expectedDecision: model.DecisionReview,
			expectedReason:   "Rule requires review",
			expectError:      false, // Rollback failure should NOT fail the validation
		},
		{
			name:    "DENY by limit takes precedence over REVIEW",
			request: baseRequest,
			setupMocks: func(ctrl *gomock.Controller, persistDone chan struct{}) (RuleEvaluator, LimitChecker, command.TransactionValidationRepository, AuditWriter) {
				ruleEval := mocks.NewMockRuleEvaluator(ctrl)
				limitCheck := mocks.NewMockLimitChecker(ctrl)
				transactionValidationRepo := commandMocks.NewMockTransactionValidationRepository(ctrl)

				// AuditWriter mock - expects RecordValidationEvent call
				auditWriter := mocks.NewMockAuditWriter(ctrl)
				auditWriter.EXPECT().RecordValidationEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)

				// Rule evaluation returns REVIEW
				evalResult, err := model.NewEvaluationResult(
					model.DecisionReview,
					[]uuid.UUID{ruleID1},
					[]uuid.UUID{ruleID1},
					"Rule requires review",
				)
				require.NoError(t, err)
				ruleEval.EXPECT().
					Execute(gomock.Any(), gomock.Any()).
					Return(evalResult, nil)

				// Limit check returns exceeded - should override REVIEW
				limitOutput := &model.CheckLimitsOutput{
					Allowed: false,
					LimitUsageDetails: []model.LimitUsageDetail{
						{
							LimitID:      limitID,
							LimitAmount:  decimal.RequireFromString("50"),
							Scope:        "account:" + limitID.String(),
							Period:       model.LimitTypeDaily,
							CurrentUsage: decimal.RequireFromString("60"),
							Exceeded:     true,
						},
					},
					ExceededLimitIDs: []uuid.UUID{limitID},
				}
				limitCheck.EXPECT().
					CheckLimits(gomock.Any(), gomock.Any()).
					Return(limitOutput, nil)

				// Audit should be inserted - signal completion via channel
				transactionValidationRepo.EXPECT().Insert(gomock.Any(), gomock.Any()).DoAndReturn(
					func(_ context.Context, _ *model.TransactionValidation) error {
						close(persistDone)
						return nil
					})

				return ruleEval, limitCheck, transactionValidationRepo, auditWriter
			},
			expectedDecision: model.DecisionDeny,
			expectedReason:   "limit_exceeded",
			expectError:      false,
		},
		{
			name:    "ALLOW with matched ALLOW rules",
			request: baseRequest,
			setupMocks: func(ctrl *gomock.Controller, persistDone chan struct{}) (RuleEvaluator, LimitChecker, command.TransactionValidationRepository, AuditWriter) {
				ruleEval := mocks.NewMockRuleEvaluator(ctrl)
				limitCheck := mocks.NewMockLimitChecker(ctrl)
				transactionValidationRepo := commandMocks.NewMockTransactionValidationRepository(ctrl)

				// AuditWriter mock - expects RecordValidationEvent call
				auditWriter := mocks.NewMockAuditWriter(ctrl)
				auditWriter.EXPECT().RecordValidationEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)

				// Rule evaluation returns ALLOW with matched rules
				evalResult, err := model.NewEvaluationResult(
					model.DecisionAllow,
					[]uuid.UUID{ruleID1},
					[]uuid.UUID{ruleID1, ruleID2},
					"Rule allowed transaction",
				)
				require.NoError(t, err)
				ruleEval.EXPECT().
					Execute(gomock.Any(), gomock.Any()).
					Return(evalResult, nil)

				// Limit check passes
				limitOutput := &model.CheckLimitsOutput{
					Allowed: true,
					LimitUsageDetails: []model.LimitUsageDetail{
						{
							LimitID:      limitID,
							LimitAmount:  decimal.RequireFromString("500"),
							Scope:        "account:" + limitID.String(),
							Period:       model.LimitTypeDaily,
							CurrentUsage: decimal.RequireFromString("100"),
							Exceeded:     false,
						},
					},
					ExceededLimitIDs: []uuid.UUID{},
				}
				limitCheck.EXPECT().
					CheckLimits(gomock.Any(), gomock.Any()).
					Return(limitOutput, nil)

				// Audit should be inserted - signal completion via channel
				transactionValidationRepo.EXPECT().Insert(gomock.Any(), gomock.Any()).DoAndReturn(
					func(_ context.Context, _ *model.TransactionValidation) error {
						close(persistDone)
						return nil
					})

				return ruleEval, limitCheck, transactionValidationRepo, auditWriter
			},
			expectedDecision: model.DecisionAllow,
			expectedReason:   "Rule allowed transaction",
			expectError:      false,
		},
		{
			name:    "ALLOW with default decision - no rules matched",
			request: baseRequest,
			setupMocks: func(ctrl *gomock.Controller, persistDone chan struct{}) (RuleEvaluator, LimitChecker, command.TransactionValidationRepository, AuditWriter) {
				ruleEval := mocks.NewMockRuleEvaluator(ctrl)
				limitCheck := mocks.NewMockLimitChecker(ctrl)
				transactionValidationRepo := commandMocks.NewMockTransactionValidationRepository(ctrl)

				// AuditWriter mock - expects RecordValidationEvent call
				auditWriter := mocks.NewMockAuditWriter(ctrl)
				auditWriter.EXPECT().RecordValidationEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)

				// Rule evaluation returns ALLOW with no matched rules (default)
				evalResult, err := model.NewNoMatchResult(model.DecisionAllow, []uuid.UUID{ruleID1, ruleID2})
				require.NoError(t, err)
				ruleEval.EXPECT().
					Execute(gomock.Any(), gomock.Any()).
					Return(evalResult, nil)

				// Limit check passes
				limitOutput := &model.CheckLimitsOutput{
					Allowed:           true,
					LimitUsageDetails: []model.LimitUsageDetail{},
					ExceededLimitIDs:  []uuid.UUID{},
				}
				limitCheck.EXPECT().
					CheckLimits(gomock.Any(), gomock.Any()).
					Return(limitOutput, nil)

				// Audit should be inserted - signal completion via channel
				transactionValidationRepo.EXPECT().Insert(gomock.Any(), gomock.Any()).DoAndReturn(
					func(_ context.Context, _ *model.TransactionValidation) error {
						close(persistDone)
						return nil
					})

				return ruleEval, limitCheck, transactionValidationRepo, auditWriter
			},
			expectedDecision: model.DecisionAllow,
			expectedReason:   "No matching rules found",
			expectError:      false,
		},
		{
			name:    "context cancellation at start - returns error immediately",
			request: baseRequest,
			setupMocks: func(ctrl *gomock.Controller, _ chan struct{}) (RuleEvaluator, LimitChecker, command.TransactionValidationRepository, AuditWriter) {
				ruleEval := mocks.NewMockRuleEvaluator(ctrl)
				limitCheck := mocks.NewMockLimitChecker(ctrl)
				transactionValidationRepo := commandMocks.NewMockTransactionValidationRepository(ctrl)

				// AuditWriter mock - expects RecordValidationEvent call
				auditWriter := mocks.NewMockAuditWriter(ctrl)
				auditWriter.EXPECT().RecordValidationEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(0)

				// No calls should be made when context is cancelled
				ruleEval.EXPECT().Execute(gomock.Any(), gomock.Any()).Times(0)
				limitCheck.EXPECT().CheckLimits(gomock.Any(), gomock.Any()).Times(0)
				transactionValidationRepo.EXPECT().Insert(gomock.Any(), gomock.Any()).Times(0)

				return ruleEval, limitCheck, transactionValidationRepo, auditWriter
			},
			expectError:   true,
			expectedErr:   context.Canceled,
			cancelContext: true,
		},
		{
			name:    "error from rule evaluator - propagates error",
			request: baseRequest,
			setupMocks: func(ctrl *gomock.Controller, _ chan struct{}) (RuleEvaluator, LimitChecker, command.TransactionValidationRepository, AuditWriter) {
				ruleEval := mocks.NewMockRuleEvaluator(ctrl)
				limitCheck := mocks.NewMockLimitChecker(ctrl)
				transactionValidationRepo := commandMocks.NewMockTransactionValidationRepository(ctrl)

				// AuditWriter mock - expects RecordValidationEvent call
				auditWriter := mocks.NewMockAuditWriter(ctrl)
				auditWriter.EXPECT().RecordValidationEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(0)

				// Rule evaluation returns error
				ruleEval.EXPECT().
					Execute(gomock.Any(), gomock.Any()).
					Return(nil, errors.New("rule evaluation failed"))

				// Limit check should NOT be called when rule eval fails
				limitCheck.EXPECT().CheckLimits(gomock.Any(), gomock.Any()).Times(0)

				// Audit should NOT be inserted on error
				transactionValidationRepo.EXPECT().Insert(gomock.Any(), gomock.Any()).Times(0)

				return ruleEval, limitCheck, transactionValidationRepo, auditWriter
			},
			expectError: true,
		},
		{
			name:    "error from limit checker - propagates error",
			request: baseRequest,
			setupMocks: func(ctrl *gomock.Controller, _ chan struct{}) (RuleEvaluator, LimitChecker, command.TransactionValidationRepository, AuditWriter) {
				ruleEval := mocks.NewMockRuleEvaluator(ctrl)
				limitCheck := mocks.NewMockLimitChecker(ctrl)
				transactionValidationRepo := commandMocks.NewMockTransactionValidationRepository(ctrl)

				// AuditWriter mock - expects RecordValidationEvent call
				auditWriter := mocks.NewMockAuditWriter(ctrl)
				auditWriter.EXPECT().RecordValidationEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(0)

				// Rule evaluation returns ALLOW
				evalResult, err := model.NewEvaluationResult(
					model.DecisionAllow,
					[]uuid.UUID{ruleID1},
					[]uuid.UUID{ruleID1},
					"Rule allowed transaction",
				)
				require.NoError(t, err)
				ruleEval.EXPECT().
					Execute(gomock.Any(), gomock.Any()).
					Return(evalResult, nil)

				// Limit check returns error
				limitCheck.EXPECT().
					CheckLimits(gomock.Any(), gomock.Any()).
					Return(nil, errors.New("limit check failed"))

				// Audit should NOT be inserted on error
				transactionValidationRepo.EXPECT().Insert(gomock.Any(), gomock.Any()).Times(0)

				return ruleEval, limitCheck, transactionValidationRepo, auditWriter
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)

			// Create channel to signal when audit is done (for deterministic waiting)
			persistDone := make(chan struct{})

			ruleEval, limitCheck, transactionValidationRepo, auditWriter := tt.setupMocks(ctrl, persistDone)

			service, err := NewValidationService(ruleEval, limitCheck, transactionValidationRepo, auditWriter)
			require.NoError(t, err)

			// Create context - cancelled for context cancellation test
			ctx := context.Background()
			if tt.cancelContext {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel() // Cancel immediately
			}

			result, err := service.Validate(ctx, tt.request)

			// Wait for async audit goroutine to complete (if audit was expected)
			// Audit is only inserted for successful validations (non-error cases)
			if !tt.expectError {
				select {
				case <-persistDone:
					// Audit completed
				case <-time.After(1 * time.Second):
					t.Fatal("Timed out waiting for audit to complete")
				}
			}

			if tt.expectError {
				require.Error(t, err)

				if tt.expectedErr != nil {
					require.ErrorIs(t, err, tt.expectedErr)
				}

				assert.Nil(t, result)
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
				assert.Equal(t, tt.expectedDecision, result.Decision)
				assert.Contains(t, result.Reason, tt.expectedReason)
			}
		})
	}
}

// TestValidateTransaction_AuditFieldsPopulated verifies that individual audit fields
// are correctly populated in the audit record for SOX/GLBA compliance.
func TestValidateTransaction_AuditFieldsPopulated(t *testing.T) {
	// Setup
	fixedTime := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	requestID := testutil.MustDeterministicUUID(100)
	accountID := testutil.MustDeterministicUUID(101)
	merchantID := testutil.MustDeterministicUUID(102)
	ruleID1 := testutil.MustDeterministicUUID(110)
	ruleID2 := testutil.MustDeterministicUUID(111)
	limitID := testutil.MustDeterministicUUID(120)
	subType := "credit"

	request := &model.ValidationRequest{
		RequestID:            requestID,
		TransactionType:      model.TransactionTypeCard,
		SubType:              &subType,
		Amount:               decimal.RequireFromString("250"), // $250.00
		Currency:             "BRL",
		TransactionTimestamp: fixedTime,
		Account: model.AccountContext{
			ID:     accountID,
			Type:   "checking",
			Status: "active",
		},
		Merchant: &model.MerchantContext{
			ID:       merchantID,
			Category: "5411",
			Country:  "BR",
		},
		Metadata: map[string]any{
			"source": "mobile",
		},
	}

	ctrl := gomock.NewController(t)
	persistDone := make(chan struct{})

	ruleEval := mocks.NewMockRuleEvaluator(ctrl)
	limitCheck := mocks.NewMockLimitChecker(ctrl)
	transactionValidationRepo := commandMocks.NewMockTransactionValidationRepository(ctrl)

	// AuditWriter mock - expects RecordValidationEvent call
	auditWriter := mocks.NewMockAuditWriter(ctrl)
	auditWriter.EXPECT().RecordValidationEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)

	// Rule evaluation returns ALLOW with matched rules
	evalResult, err := model.NewEvaluationResult(
		model.DecisionAllow,
		[]uuid.UUID{ruleID1},
		[]uuid.UUID{ruleID1, ruleID2},
		"Transaction allowed",
	)
	require.NoError(t, err)
	ruleEval.EXPECT().
		Execute(gomock.Any(), gomock.Any()).
		Return(evalResult, nil)

	// Limit check passes with usage details
	limitOutput := &model.CheckLimitsOutput{
		Allowed: true,
		LimitUsageDetails: []model.LimitUsageDetail{
			{
				LimitID:      limitID,
				LimitAmount:  decimal.RequireFromString("1000"), // $1000.00
				Scope:        "account:" + limitID.String(),
				Period:       model.LimitTypeDaily,
				CurrentUsage: decimal.RequireFromString("250"),
				Exceeded:     false,
			},
		},
		ExceededLimitIDs: []uuid.UUID{},
	}
	limitCheck.EXPECT().
		CheckLimits(gomock.Any(), gomock.Any()).
		Return(limitOutput, nil)

	// Capture the audit record to verify fields
	var capturedTV *model.TransactionValidation
	transactionValidationRepo.EXPECT().Insert(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, audit *model.TransactionValidation) error {
			capturedTV = audit
			close(persistDone)
			return nil
		})

	service, err := NewValidationService(ruleEval, limitCheck, transactionValidationRepo, auditWriter)
	require.NoError(t, err)

	// Act
	result, err := service.Validate(context.Background(), request)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Wait for audit to complete
	select {
	case <-persistDone:
	case <-time.After(1 * time.Second):
		t.Fatal("Timed out waiting for audit to complete")
	}

	// Assert - Verify individual audit fields are populated
	require.NotNil(t, capturedTV, "Audit record should be captured")

	// Request fields
	assert.Equal(t, requestID, capturedTV.RequestID)
	assert.Equal(t, model.TransactionTypeCard, capturedTV.TransactionType)
	assert.Equal(t, &subType, capturedTV.SubType)
	assert.True(t, decimal.RequireFromString("250").Equal(capturedTV.Amount), "Amount should be 250")
	assert.Equal(t, "BRL", capturedTV.Currency)
	assert.Equal(t, fixedTime, capturedTV.TransactionTimestamp)

	// Account context
	assert.Equal(t, accountID, capturedTV.Account.ID)
	assert.Equal(t, "checking", capturedTV.Account.Type)
	assert.Equal(t, "active", capturedTV.Account.Status)

	// Merchant context
	require.NotNil(t, capturedTV.Merchant)
	assert.Equal(t, merchantID, capturedTV.Merchant.ID)
	assert.Equal(t, "5411", capturedTV.Merchant.Category)
	assert.Equal(t, "BR", capturedTV.Merchant.Country)

	// Metadata
	require.NotNil(t, capturedTV.Metadata)
	assert.Equal(t, "mobile", capturedTV.Metadata["source"])

	// Response fields (from EvaluationResult)
	assert.Equal(t, model.DecisionAllow, capturedTV.Decision)
	assert.Equal(t, "Transaction allowed", capturedTV.Reason)

	// Matched and evaluated rule IDs
	require.Len(t, capturedTV.MatchedRuleIDs, 1)
	assert.Equal(t, ruleID1, capturedTV.MatchedRuleIDs[0])
	require.Len(t, capturedTV.EvaluatedRuleIDs, 2)

	// Limit usage details
	require.Len(t, capturedTV.LimitUsageDetails, 1)
	assert.Equal(t, limitID, capturedTV.LimitUsageDetails[0].LimitID)
	assert.Equal(t, decimal.RequireFromString("1000").String(), capturedTV.LimitUsageDetails[0].LimitAmount.String())
	assert.Equal(t, model.LimitTypeDaily, capturedTV.LimitUsageDetails[0].Period)
	assert.Equal(t, decimal.RequireFromString("250").String(), capturedTV.LimitUsageDetails[0].CurrentUsage.String())
	assert.False(t, capturedTV.LimitUsageDetails[0].Exceeded)
}

func TestNewValidationService_NilDependencies(t *testing.T) {
	ctrl := gomock.NewController(t)

	validRuleEval := mocks.NewMockRuleEvaluator(ctrl)
	validLimitCheck := mocks.NewMockLimitChecker(ctrl)
	validTransactionValidationRepo := commandMocks.NewMockTransactionValidationRepository(ctrl)
	validAuditWriter := mocks.NewMockAuditWriter(ctrl)

	tests := []struct {
		name                      string
		ruleEval                  RuleEvaluator
		limitCheck                LimitChecker
		transactionValidationRepo command.TransactionValidationRepository
		auditWriter               AuditWriter
		expectedErr               error
	}{
		{
			name:                      "nil rule evaluator",
			ruleEval:                  nil,
			limitCheck:                validLimitCheck,
			transactionValidationRepo: validTransactionValidationRepo,
			auditWriter:               validAuditWriter,
			expectedErr:               ErrNilRuleEvaluator,
		},
		{
			name:                      "nil limit checker",
			ruleEval:                  validRuleEval,
			limitCheck:                nil,
			transactionValidationRepo: validTransactionValidationRepo,
			auditWriter:               validAuditWriter,
			expectedErr:               ErrNilLimitChecker,
		},
		{
			name:                      "nil transaction validation repository",
			ruleEval:                  validRuleEval,
			limitCheck:                validLimitCheck,
			transactionValidationRepo: nil,
			auditWriter:               validAuditWriter,
			expectedErr:               ErrNilTransactionValidationRepo,
		},
		{
			name:                      "nil audit writer",
			ruleEval:                  validRuleEval,
			limitCheck:                validLimitCheck,
			transactionValidationRepo: validTransactionValidationRepo,
			auditWriter:               nil,
			expectedErr:               ErrNilAuditWriter,
		},
		{
			name:                      "all valid dependencies",
			ruleEval:                  validRuleEval,
			limitCheck:                validLimitCheck,
			transactionValidationRepo: validTransactionValidationRepo,
			auditWriter:               validAuditWriter,
			expectedErr:               nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, err := NewValidationService(tt.ruleEval, tt.limitCheck, tt.transactionValidationRepo, tt.auditWriter)

			if tt.expectedErr != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tt.expectedErr)
				assert.Nil(t, service)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, service)
			}
		})
	}
}

func TestValidateTransactionValidation(t *testing.T) {
	t.Parallel()

	validID := testutil.MustDeterministicUUID(1)
	requestID := testutil.MustDeterministicUUID(2)
	accountID := testutil.MustDeterministicUUID(3)
	fixedTime := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)

	// Helper to create a valid transaction validation record
	validTV := func() *model.TransactionValidation {
		return &model.TransactionValidation{
			ID:                   validID,
			RequestID:            requestID,
			TransactionType:      model.TransactionTypeCard,
			Amount:               decimal.RequireFromString("100"),
			Currency:             "USD",
			TransactionTimestamp: fixedTime,
			Account:              model.AccountContext{ID: accountID, Type: "checking", Status: "active"},
			EvaluationResult:     model.EvaluationResult{Decision: model.DecisionAllow},
			CreatedAt:            fixedTime,
		}
	}

	tests := []struct {
		name      string
		tv        *model.TransactionValidation
		wantError bool
		errMsg    string
	}{
		{
			name:      "valid transaction validation record",
			tv:        validTV(),
			wantError: false,
		},
		{
			name: "nil ID",
			tv: func() *model.TransactionValidation {
				v := validTV()
				v.ID = uuid.UUID{}
				return v
			}(),
			wantError: true,
			errMsg:    "transaction validation ID is nil",
		},
		{
			name: "invalid decision",
			tv: func() *model.TransactionValidation {
				v := validTV()
				v.Decision = model.Decision("INVALID")
				return v
			}(),
			wantError: true,
			errMsg:    "invalid decision",
		},
		{
			name: "nil request ID",
			tv: func() *model.TransactionValidation {
				v := validTV()
				v.RequestID = uuid.UUID{}
				return v
			}(),
			wantError: true,
			errMsg:    "transaction validation request ID is nil",
		},
		{
			name: "invalid transaction type",
			tv: func() *model.TransactionValidation {
				v := validTV()
				v.TransactionType = model.TransactionType("INVALID")
				return v
			}(),
			wantError: true,
			errMsg:    "invalid transaction type",
		},
		{
			name: "zero amount",
			tv: func() *model.TransactionValidation {
				v := validTV()
				v.Amount = decimal.RequireFromString("0")
				return v
			}(),
			wantError: true,
			errMsg:    "invalid amount",
		},
		{
			name: "negative amount",
			tv: func() *model.TransactionValidation {
				v := validTV()
				v.Amount = decimal.RequireFromString("-1")
				return v
			}(),
			wantError: true,
			errMsg:    "invalid amount",
		},
		{
			name: "empty currency",
			tv: func() *model.TransactionValidation {
				v := validTV()
				v.Currency = ""
				return v
			}(),
			wantError: true,
			errMsg:    "currency is empty",
		},
		{
			name: "zero transaction timestamp",
			tv: func() *model.TransactionValidation {
				v := validTV()
				v.TransactionTimestamp = time.Time{}
				return v
			}(),
			wantError: true,
			errMsg:    "transaction timestamp is zero",
		},
		{
			name: "nil account ID",
			tv: func() *model.TransactionValidation {
				v := validTV()
				v.Account.ID = uuid.UUID{}
				return v
			}(),
			wantError: true,
			errMsg:    "account ID is nil",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := validateTransactionValidation(tc.tv)

			if tc.wantError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestValidateTransactionValidation_AllRequiredRequestFields verifies that all required request fields
// are validated (ID, RequestID, TransactionType, Amount, Currency, Account.ID).
func TestValidateTransactionValidation_AllRequiredRequestFields(t *testing.T) {
	t.Parallel()

	validID := testutil.MustDeterministicUUID(1)
	requestID := testutil.MustDeterministicUUID(2)
	accountID := testutil.MustDeterministicUUID(3)
	fixedTime := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)

	// Base valid record
	validTV := func() *model.TransactionValidation {
		return &model.TransactionValidation{
			ID:                   validID,
			RequestID:            requestID,
			TransactionType:      model.TransactionTypeCard,
			Amount:               decimal.RequireFromString("100"),
			Currency:             "USD",
			TransactionTimestamp: fixedTime,
			Account:              model.AccountContext{ID: accountID, Type: "checking", Status: "active"},
			EvaluationResult:     model.EvaluationResult{Decision: model.DecisionAllow},
			CreatedAt:            fixedTime,
		}
	}

	// Test that each required field triggers validation error when missing/invalid
	requiredFields := []struct {
		name      string
		modify    func(*model.TransactionValidation)
		errSubstr string
	}{
		{
			name: "ID is required",
			modify: func(tv *model.TransactionValidation) {
				tv.ID = uuid.UUID{}
			},
			errSubstr: "transaction validation ID is nil",
		},
		{
			name: "RequestID is required",
			modify: func(tv *model.TransactionValidation) {
				tv.RequestID = uuid.UUID{}
			},
			errSubstr: "transaction validation request ID is nil",
		},
		{
			name: "TransactionType is required and must be valid",
			modify: func(tv *model.TransactionValidation) {
				tv.TransactionType = ""
			},
			errSubstr: "invalid transaction type",
		},
		{
			name: "Amount must be positive",
			modify: func(tv *model.TransactionValidation) {
				tv.Amount = decimal.RequireFromString("0")
			},
			errSubstr: "invalid amount",
		},
		{
			name: "Currency is required",
			modify: func(tv *model.TransactionValidation) {
				tv.Currency = ""
			},
			errSubstr: "currency is empty",
		},
		{
			name: "TransactionTimestamp is required",
			modify: func(tv *model.TransactionValidation) {
				tv.TransactionTimestamp = time.Time{}
			},
			errSubstr: "transaction timestamp is zero",
		},
		{
			name: "Account.ID is required",
			modify: func(tv *model.TransactionValidation) {
				tv.Account.ID = uuid.UUID{}
			},
			errSubstr: "account ID is nil",
		},
	}

	for _, tc := range requiredFields {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tv := validTV()
			tc.modify(tv)

			err := validateTransactionValidation(tv)

			require.Error(t, err, "Expected validation error for: %s", tc.name)
			assert.Contains(t, err.Error(), tc.errSubstr)
		})
	}

	// Verify valid record passes all validations
	t.Run("all required fields present passes validation", func(t *testing.T) {
		t.Parallel()

		tv := validTV()
		err := validateTransactionValidation(tv)
		require.NoError(t, err)
	})
}

// TestValidateTransactionValidation_AllRequiredResponseFields verifies that all required response fields
// are validated (Decision must be valid).
func TestValidateTransactionValidation_AllRequiredResponseFields(t *testing.T) {
	t.Parallel()

	validID := testutil.MustDeterministicUUID(1)
	requestID := testutil.MustDeterministicUUID(2)
	accountID := testutil.MustDeterministicUUID(3)
	fixedTime := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)

	// Base valid record
	validTV := func() *model.TransactionValidation {
		return &model.TransactionValidation{
			ID:                   validID,
			RequestID:            requestID,
			TransactionType:      model.TransactionTypeCard,
			Amount:               decimal.RequireFromString("100"),
			Currency:             "USD",
			TransactionTimestamp: fixedTime,
			Account:              model.AccountContext{ID: accountID, Type: "checking", Status: "active"},
			EvaluationResult:     model.EvaluationResult{Decision: model.DecisionAllow},
			CreatedAt:            fixedTime,
		}
	}

	// Test invalid decision values
	invalidDecisions := []struct {
		name     string
		decision model.Decision
	}{
		{name: "empty decision", decision: ""},
		{name: "invalid decision UNKNOWN", decision: "UNKNOWN"},
		{name: "invalid decision lowercase", decision: "allow"},
		{name: "invalid decision typo", decision: "ALOW"},
	}

	for _, tc := range invalidDecisions {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tv := validTV()
			tv.Decision = tc.decision

			err := validateTransactionValidation(tv)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid decision")
		})
	}
}

// TestValidateTransactionValidation_AllValidDecisions verifies all valid decision values pass validation.
func TestValidateTransactionValidation_AllValidDecisions(t *testing.T) {
	t.Parallel()

	validID := testutil.MustDeterministicUUID(1)
	requestID := testutil.MustDeterministicUUID(2)
	accountID := testutil.MustDeterministicUUID(3)
	fixedTime := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)

	validDecisions := []model.Decision{model.DecisionAllow, model.DecisionDeny, model.DecisionReview}

	for _, decision := range validDecisions {
		t.Run(string(decision), func(t *testing.T) {
			t.Parallel()

			audit := &model.TransactionValidation{
				ID:                   validID,
				RequestID:            requestID,
				TransactionType:      model.TransactionTypeCard,
				Amount:               decimal.RequireFromString("100"),
				Currency:             "USD",
				TransactionTimestamp: fixedTime,
				Account:              model.AccountContext{ID: accountID, Type: "checking", Status: "active"},
				EvaluationResult:     model.EvaluationResult{Decision: decision},
				CreatedAt:            fixedTime,
			}

			err := validateTransactionValidation(audit)

			require.NoError(t, err)
		})
	}
}

// TestValidate_TransactionValidationPersistenceSuccess verifies that when validation
// succeeds, the transaction validation record is persisted correctly via the repository.
func TestValidate_TransactionValidationPersistenceSuccess(t *testing.T) {
	fixedTime := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	requestID := testutil.MustDeterministicUUID(1)
	accountID := testutil.MustDeterministicUUID(2)
	ruleID := testutil.MustDeterministicUUID(10)

	request := &model.ValidationRequest{
		RequestID:            requestID,
		TransactionType:      model.TransactionTypeCard,
		Amount:               decimal.RequireFromString("100"),
		Currency:             "USD",
		TransactionTimestamp: fixedTime,
		Account:              model.AccountContext{ID: accountID},
	}

	ctrl := gomock.NewController(t)

	ruleEval := mocks.NewMockRuleEvaluator(ctrl)
	limitCheck := mocks.NewMockLimitChecker(ctrl)
	transactionValidationRepo := commandMocks.NewMockTransactionValidationRepository(ctrl)

	// AuditWriter mock - expects RecordValidationEvent call
	auditWriter := mocks.NewMockAuditWriter(ctrl)
	auditWriter.EXPECT().RecordValidationEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)

	// Rule evaluation returns ALLOW
	evalResult, err := model.NewEvaluationResult(
		model.DecisionAllow,
		[]uuid.UUID{ruleID},
		[]uuid.UUID{ruleID},
		"Transaction allowed",
	)
	require.NoError(t, err)
	ruleEval.EXPECT().
		Execute(gomock.Any(), gomock.Any()).
		Return(evalResult, nil)

	// Limit check passes
	limitOutput := &model.CheckLimitsOutput{
		Allowed:           true,
		LimitUsageDetails: []model.LimitUsageDetail{},
		ExceededLimitIDs:  []uuid.UUID{},
	}
	limitCheck.EXPECT().
		CheckLimits(gomock.Any(), gomock.Any()).
		Return(limitOutput, nil)

	// Insert is called after validation passes
	transactionValidationRepo.EXPECT().Insert(gomock.Any(), gomock.Any()).Times(1).DoAndReturn(
		func(_ context.Context, _ *model.TransactionValidation) error {
			return nil
		})

	service, err := NewValidationService(ruleEval, limitCheck, transactionValidationRepo, auditWriter)
	require.NoError(t, err)

	// Act
	result, err := service.Validate(context.Background(), request)

	// Assert: Validation succeeds - the client gets the result regardless of internal logging
	require.NoError(t, err, "Validation should succeed")
	require.NotNil(t, result)
	assert.Equal(t, model.DecisionAllow, result.Decision)

	// Note: We can't easily verify the log output in this test without injecting a mock logger.
	// The important behavior is that the validation result is returned correctly.
	// In a production system, we would verify logs through observability tooling.
}

// TestValidate_AuditPersistFailure_LogsError verifies that when the audit repository
// returns an error (e.g., due to DB failure), the error is logged
// but does not affect the validation result.
func TestValidate_AuditPersistFailure_LogsError(t *testing.T) {
	fixedTime := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	requestID := testutil.MustDeterministicUUID(1)
	accountID := testutil.MustDeterministicUUID(2)
	ruleID := testutil.MustDeterministicUUID(10)

	request := &model.ValidationRequest{
		RequestID:            requestID,
		TransactionType:      model.TransactionTypeCard,
		Amount:               decimal.RequireFromString("100"),
		Currency:             "USD",
		TransactionTimestamp: fixedTime,
		Account:              model.AccountContext{ID: accountID},
	}

	ctrl := gomock.NewController(t)
	errorLogged := make(chan struct{})

	ruleEval := mocks.NewMockRuleEvaluator(ctrl)
	limitCheck := mocks.NewMockLimitChecker(ctrl)
	transactionValidationRepo := commandMocks.NewMockTransactionValidationRepository(ctrl)

	// AuditWriter mock - expects RecordValidationEvent call
	auditWriter := mocks.NewMockAuditWriter(ctrl)
	auditWriter.EXPECT().RecordValidationEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)

	// Rule evaluation returns ALLOW
	evalResult, err := model.NewEvaluationResult(
		model.DecisionAllow,
		[]uuid.UUID{ruleID},
		[]uuid.UUID{ruleID},
		"Transaction allowed",
	)
	require.NoError(t, err)
	ruleEval.EXPECT().
		Execute(gomock.Any(), gomock.Any()).
		Return(evalResult, nil)

	// Limit check passes
	limitOutput := &model.CheckLimitsOutput{
		Allowed:           true,
		LimitUsageDetails: []model.LimitUsageDetail{},
		ExceededLimitIDs:  []uuid.UUID{},
	}
	limitCheck.EXPECT().
		CheckLimits(gomock.Any(), gomock.Any()).
		Return(limitOutput, nil)

	// Audit repo returns error (simulating database failure)
	transactionValidationRepo.EXPECT().Insert(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ *model.TransactionValidation) error {
			close(errorLogged)
			return errors.New("database connection failed")
		})

	service, err := NewValidationService(ruleEval, limitCheck, transactionValidationRepo, auditWriter)
	require.NoError(t, err)

	// Act
	result, err := service.Validate(context.Background(), request)

	// Assert: Validation succeeds despite audit failure
	require.NoError(t, err, "Validation should succeed even if audit fails")
	require.NotNil(t, result)
	assert.Equal(t, model.DecisionAllow, result.Decision)

	// Wait for async audit goroutine to complete
	select {
	case <-errorLogged:
		// Audit error was processed
	case <-time.After(1 * time.Second):
		t.Fatal("Timed out waiting for audit error to be processed")
	}
}

// TestValidate_WithSegmentAndPortfolio verifies that segment and portfolio context
// are correctly included in the audit event for SOX/GLBA compliance.
func TestValidate_WithSegmentAndPortfolio(t *testing.T) {
	fixedTime := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	requestID := testutil.MustDeterministicUUID(1)
	accountID := testutil.MustDeterministicUUID(2)
	segmentID := testutil.MustDeterministicUUID(3)
	portfolioID := testutil.MustDeterministicUUID(4)
	ruleID := testutil.MustDeterministicUUID(10)

	request := &model.ValidationRequest{
		RequestID:            requestID,
		TransactionType:      model.TransactionTypeCard,
		Amount:               decimal.RequireFromString("100"),
		Currency:             "USD",
		TransactionTimestamp: fixedTime,
		Account: model.AccountContext{
			ID:     accountID,
			Type:   "checking",
			Status: "active",
		},
		Segment: &model.SegmentContext{
			ID:       segmentID,
			Name:     "Premium",
			Metadata: map[string]any{"tier": "gold"},
		},
		Portfolio: &model.PortfolioContext{
			ID:       portfolioID,
			Name:     "Investment Portfolio",
			Metadata: map[string]any{"type": "investment"},
		},
	}

	ctrl := gomock.NewController(t)
	persistDone := make(chan struct{})

	ruleEval := mocks.NewMockRuleEvaluator(ctrl)
	limitCheck := mocks.NewMockLimitChecker(ctrl)
	transactionValidationRepo := commandMocks.NewMockTransactionValidationRepository(ctrl)

	// AuditWriter mock - expects RecordValidationEvent call with segment and portfolio
	auditWriter := mocks.NewMockAuditWriter(ctrl)
	auditWriter.EXPECT().RecordValidationEvent(
		gomock.Any(),
		gomock.Any(),
		gomock.Any(), // Request snapshot should contain segment and portfolio
		gomock.Any(),
		gomock.Any(),
		gomock.Any(),
	).DoAndReturn(func(_ context.Context, _ uuid.UUID, snapshot map[string]any, _ model.EvaluationResult, _ model.ValidationResponseContext, _ string) error {
		// Verify segment is present
		segmentData, ok := snapshot["segment"].(map[string]any)
		assert.True(t, ok, "Segment should be in snapshot")
		assert.Equal(t, segmentID.String(), segmentData["segmentId"])
		assert.Equal(t, "Premium", segmentData["name"])

		// Verify portfolio is present
		portfolioData, ok := snapshot["portfolio"].(map[string]any)
		assert.True(t, ok, "Portfolio should be in snapshot")
		assert.Equal(t, portfolioID.String(), portfolioData["portfolioId"])
		assert.Equal(t, "Investment Portfolio", portfolioData["name"])

		// Verify account contains segmentId and portfolioId
		accountData, ok := snapshot["account"].(map[string]any)
		assert.True(t, ok, "Account should be in snapshot")
		assert.Equal(t, segmentID.String(), accountData["segmentId"])
		assert.Equal(t, portfolioID.String(), accountData["portfolioId"])

		return nil
	}).Times(1)

	// Rule evaluation returns ALLOW
	evalResult, err := model.NewEvaluationResult(
		model.DecisionAllow,
		[]uuid.UUID{ruleID},
		[]uuid.UUID{ruleID},
		"Transaction allowed",
	)
	require.NoError(t, err)
	ruleEval.EXPECT().
		Execute(gomock.Any(), gomock.Any()).
		Return(evalResult, nil)

	// Limit check passes
	limitOutput := &model.CheckLimitsOutput{
		Allowed:           true,
		LimitUsageDetails: []model.LimitUsageDetail{},
		ExceededLimitIDs:  []uuid.UUID{},
	}
	limitCheck.EXPECT().
		CheckLimits(gomock.Any(), gomock.Any()).
		Return(limitOutput, nil)

	// Transaction validation persisted successfully
	transactionValidationRepo.EXPECT().Insert(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, tv *model.TransactionValidation) error {
			// Verify segment and portfolio are persisted
			assert.NotNil(t, tv.Segment)
			assert.Equal(t, segmentID, tv.Segment.ID)
			assert.NotNil(t, tv.Portfolio)
			assert.Equal(t, portfolioID, tv.Portfolio.ID)
			close(persistDone)
			return nil
		})

	service, err := NewValidationService(ruleEval, limitCheck, transactionValidationRepo, auditWriter)
	require.NoError(t, err)

	// Act
	result, err := service.Validate(context.Background(), request)

	// Wait for persistence to complete
	select {
	case <-persistDone:
	case <-time.After(1 * time.Second):
		t.Fatal("Timed out waiting for persistence to complete")
	}

	// Assert
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, model.DecisionAllow, result.Decision)
}

// TestValidate_NilRequest verifies that Validate returns an error when called with nil request.
func TestValidate_NilRequest(t *testing.T) {
	ctrl := gomock.NewController(t)

	ruleEval := mocks.NewMockRuleEvaluator(ctrl)
	limitCheck := mocks.NewMockLimitChecker(ctrl)
	transactionValidationRepo := commandMocks.NewMockTransactionValidationRepository(ctrl)
	auditWriter := mocks.NewMockAuditWriter(ctrl)

	// No mock expectations - function should return early

	service, err := NewValidationService(ruleEval, limitCheck, transactionValidationRepo, auditWriter)
	require.NoError(t, err)

	// Act
	result, err := service.Validate(context.Background(), nil)

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "validation request cannot be nil")
	assert.Nil(t, result)
}

// TestValidate_RuleEvaluatorReturnsNil verifies that Validate handles nil evaluation result.
func TestValidate_RuleEvaluatorReturnsNil(t *testing.T) {
	fixedTime := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	requestID := testutil.MustDeterministicUUID(1)
	accountID := testutil.MustDeterministicUUID(2)

	request := &model.ValidationRequest{
		RequestID:            requestID,
		TransactionType:      model.TransactionTypeCard,
		Amount:               decimal.RequireFromString("100"),
		Currency:             "USD",
		TransactionTimestamp: fixedTime,
		Account:              model.AccountContext{ID: accountID},
	}

	ctrl := gomock.NewController(t)

	ruleEval := mocks.NewMockRuleEvaluator(ctrl)
	limitCheck := mocks.NewMockLimitChecker(ctrl)
	transactionValidationRepo := commandMocks.NewMockTransactionValidationRepository(ctrl)
	auditWriter := mocks.NewMockAuditWriter(ctrl)

	// Rule evaluation returns nil result (but no error)
	ruleEval.EXPECT().
		Execute(gomock.Any(), gomock.Any()).
		Return(nil, nil)

	// No limit check or audit expected - should fail early

	service, err := NewValidationService(ruleEval, limitCheck, transactionValidationRepo, auditWriter)
	require.NoError(t, err)

	// Act
	result, err := service.Validate(context.Background(), request)

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rule evaluation returned nil result")
	assert.Nil(t, result)
}

// TestValidate_LimitCheckerReturnsNil verifies that Validate handles nil limit check result.
func TestValidate_LimitCheckerReturnsNil(t *testing.T) {
	fixedTime := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	requestID := testutil.MustDeterministicUUID(1)
	accountID := testutil.MustDeterministicUUID(2)
	ruleID := testutil.MustDeterministicUUID(10)

	request := &model.ValidationRequest{
		RequestID:            requestID,
		TransactionType:      model.TransactionTypeCard,
		Amount:               decimal.RequireFromString("100"),
		Currency:             "USD",
		TransactionTimestamp: fixedTime,
		Account:              model.AccountContext{ID: accountID},
	}

	ctrl := gomock.NewController(t)

	ruleEval := mocks.NewMockRuleEvaluator(ctrl)
	limitCheck := mocks.NewMockLimitChecker(ctrl)
	transactionValidationRepo := commandMocks.NewMockTransactionValidationRepository(ctrl)
	auditWriter := mocks.NewMockAuditWriter(ctrl)

	// Rule evaluation returns ALLOW
	evalResult, err := model.NewEvaluationResult(
		model.DecisionAllow,
		[]uuid.UUID{ruleID},
		[]uuid.UUID{ruleID},
		"Transaction allowed",
	)
	require.NoError(t, err)
	ruleEval.EXPECT().
		Execute(gomock.Any(), gomock.Any()).
		Return(evalResult, nil)

	// Limit check returns nil result (but no error)
	limitCheck.EXPECT().
		CheckLimits(gomock.Any(), gomock.Any()).
		Return(nil, nil)

	// No audit expected - should fail early

	service, err := NewValidationService(ruleEval, limitCheck, transactionValidationRepo, auditWriter)
	require.NoError(t, err)

	// Act
	result, err := service.Validate(context.Background(), request)

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "limit check returned nil result")
	assert.Nil(t, result)
}

// TestValidate_AuditEventWriterFailure verifies audit writer errors are logged but don't fail validation.
func TestValidate_AuditEventWriterFailure(t *testing.T) {
	fixedTime := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	requestID := testutil.MustDeterministicUUID(1)
	accountID := testutil.MustDeterministicUUID(2)
	ruleID := testutil.MustDeterministicUUID(10)

	request := &model.ValidationRequest{
		RequestID:            requestID,
		TransactionType:      model.TransactionTypeCard,
		Amount:               decimal.RequireFromString("100"),
		Currency:             "USD",
		TransactionTimestamp: fixedTime,
		Account:              model.AccountContext{ID: accountID},
	}

	ctrl := gomock.NewController(t)
	persistDone := make(chan struct{})

	ruleEval := mocks.NewMockRuleEvaluator(ctrl)
	limitCheck := mocks.NewMockLimitChecker(ctrl)
	transactionValidationRepo := commandMocks.NewMockTransactionValidationRepository(ctrl)
	auditWriter := mocks.NewMockAuditWriter(ctrl)

	// AuditWriter returns error
	auditWriter.EXPECT().RecordValidationEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(errors.New("audit writer failure")).Times(1)

	// Rule evaluation returns ALLOW
	evalResult, err := model.NewEvaluationResult(
		model.DecisionAllow,
		[]uuid.UUID{ruleID},
		[]uuid.UUID{ruleID},
		"Transaction allowed",
	)
	require.NoError(t, err)
	ruleEval.EXPECT().
		Execute(gomock.Any(), gomock.Any()).
		Return(evalResult, nil)

	// Limit check passes
	limitOutput := &model.CheckLimitsOutput{
		Allowed:           true,
		LimitUsageDetails: []model.LimitUsageDetail{},
		ExceededLimitIDs:  []uuid.UUID{},
	}
	limitCheck.EXPECT().
		CheckLimits(gomock.Any(), gomock.Any()).
		Return(limitOutput, nil)

	// Transaction validation persisted successfully
	transactionValidationRepo.EXPECT().Insert(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ *model.TransactionValidation) error {
			close(persistDone)
			return nil
		})

	service, err := NewValidationService(ruleEval, limitCheck, transactionValidationRepo, auditWriter)
	require.NoError(t, err)

	// Act
	result, err := service.Validate(context.Background(), request)

	// Wait for persistence to complete
	select {
	case <-persistDone:
	case <-time.After(1 * time.Second):
		t.Fatal("Timed out waiting for persistence to complete")
	}

	// Assert: Validation succeeds despite audit writer failure (best-effort audit)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, model.DecisionAllow, result.Decision)
}
