// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package services

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"go.uber.org/mock/gomock"

	commandMocks "tracer/internal/services/command/mocks"
	"tracer/internal/services/mocks"
	"tracer/internal/testutil"
	"tracer/pkg/model"
)

// benchSink prevents compiler optimization of benchmark results.
var benchSink any

func BenchmarkValidationService_Validate(b *testing.B) {
	ctrl := gomock.NewController(b)

	mockRuleEval := mocks.NewMockRuleEvaluator(ctrl)
	mockLimitCheck := mocks.NewMockLimitChecker(ctrl)
	mockAuditRepo := commandMocks.NewMockTransactionValidationRepository(ctrl)

	// Setup mock responses
	evalResult, err := model.NewEvaluationResult(
		model.DecisionAllow,
		[]uuid.UUID{},
		[]uuid.UUID{testutil.MustDeterministicUUID(1)},
		"No blocking rules",
	)
	if err != nil {
		b.Fatal(err)
	}

	limitOutput := &model.CheckLimitsOutput{
		Allowed:           true,
		LimitUsageDetails: []model.LimitUsageDetail{},
	}

	mockRuleEval.EXPECT().
		Execute(gomock.Any(), gomock.Any()).
		Return(evalResult, nil).
		AnyTimes()

	mockLimitCheck.EXPECT().
		CheckLimits(gomock.Any(), gomock.Any()).
		Return(limitOutput, nil).
		AnyTimes()

	mockAuditRepo.EXPECT().
		Insert(gomock.Any(), gomock.Any()).
		Return(nil).
		AnyTimes()

	mockAuditWriter := mocks.NewMockAuditWriter(ctrl)
	mockAuditWriter.EXPECT().
		RecordValidationEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil).
		AnyTimes()

	service, err := NewValidationService(mockRuleEval, mockLimitCheck, mockAuditRepo, mockAuditWriter)
	if err != nil {
		b.Fatal(err)
	}

	accountID := testutil.MustDeterministicUUID(100)
	request := &model.ValidationRequest{
		RequestID:       testutil.MustDeterministicUUID(1),
		TransactionType: model.TransactionTypeCard,
		Amount:          decimal.RequireFromString("100"),
		Currency:        "USD",
		TransactionTimestamp:       time.Now(),
		Account:         model.AccountContext{ID: accountID},
	}

	ctx := context.Background()

	b.ResetTimer()

	for b.Loop() {
		result, err := service.Validate(ctx, request)
		if err != nil {
			b.Fatal(err)
		}

		benchSink = result
	}
}

func BenchmarkValidationService_Validate_WithDenyRule(b *testing.B) {
	ctrl := gomock.NewController(b)

	mockRuleEval := mocks.NewMockRuleEvaluator(ctrl)
	mockLimitCheck := mocks.NewMockLimitChecker(ctrl)
	mockAuditRepo := commandMocks.NewMockTransactionValidationRepository(ctrl)

	// Setup mock to return DENY (should skip limit check)
	evalResult, err := model.NewEvaluationResult(
		model.DecisionDeny,
		[]uuid.UUID{testutil.MustDeterministicUUID(10)},
		[]uuid.UUID{testutil.MustDeterministicUUID(10)},
		"Rule blocked transaction",
	)
	if err != nil {
		b.Fatal(err)
	}

	mockRuleEval.EXPECT().
		Execute(gomock.Any(), gomock.Any()).
		Return(evalResult, nil).
		AnyTimes()

	// LimitChecker should NOT be called when DENY by rule
	mockLimitCheck.EXPECT().
		CheckLimits(gomock.Any(), gomock.Any()).
		Times(0)

	mockAuditRepo.EXPECT().
		Insert(gomock.Any(), gomock.Any()).
		Return(nil).
		AnyTimes()

	mockAuditWriter := mocks.NewMockAuditWriter(ctrl)
	mockAuditWriter.EXPECT().
		RecordValidationEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil).
		AnyTimes()

	service, err := NewValidationService(mockRuleEval, mockLimitCheck, mockAuditRepo, mockAuditWriter)
	if err != nil {
		b.Fatal(err)
	}

	accountID := testutil.MustDeterministicUUID(100)
	request := &model.ValidationRequest{
		RequestID:       testutil.MustDeterministicUUID(1),
		TransactionType: model.TransactionTypeCard,
		Amount:          decimal.RequireFromString("100"),
		Currency:        "USD",
		TransactionTimestamp:       time.Now(),
		Account:         model.AccountContext{ID: accountID},
	}

	ctx := context.Background()

	b.ResetTimer()

	for b.Loop() {
		result, err := service.Validate(ctx, request)
		if err != nil {
			b.Fatal(err)
		}

		benchSink = result
	}
}

func BenchmarkValidationService_Validate_Parallel(b *testing.B) {
	ctrl := gomock.NewController(b)

	mockRuleEval := mocks.NewMockRuleEvaluator(ctrl)
	mockLimitCheck := mocks.NewMockLimitChecker(ctrl)
	mockAuditRepo := commandMocks.NewMockTransactionValidationRepository(ctrl)

	evalResult, err := model.NewEvaluationResult(
		model.DecisionAllow,
		[]uuid.UUID{},
		[]uuid.UUID{testutil.MustDeterministicUUID(1)},
		"No blocking rules",
	)
	if err != nil {
		b.Fatal(err)
	}

	limitOutput := &model.CheckLimitsOutput{
		Allowed:           true,
		LimitUsageDetails: []model.LimitUsageDetail{},
	}

	mockRuleEval.EXPECT().
		Execute(gomock.Any(), gomock.Any()).
		Return(evalResult, nil).
		AnyTimes()

	mockLimitCheck.EXPECT().
		CheckLimits(gomock.Any(), gomock.Any()).
		Return(limitOutput, nil).
		AnyTimes()

	mockAuditRepo.EXPECT().
		Insert(gomock.Any(), gomock.Any()).
		Return(nil).
		AnyTimes()

	mockAuditWriter := mocks.NewMockAuditWriter(ctrl)
	mockAuditWriter.EXPECT().
		RecordValidationEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil).
		AnyTimes()

	service, err := NewValidationService(mockRuleEval, mockLimitCheck, mockAuditRepo, mockAuditWriter)
	if err != nil {
		b.Fatal(err)
	}

	ctx := context.Background()

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		accountID := uuid.New()
		request := &model.ValidationRequest{
			RequestID:       uuid.New(),
			TransactionType: model.TransactionTypeCard,
			Amount:          decimal.RequireFromString("100"),
			Currency:        "USD",
			TransactionTimestamp:       time.Now(),
			Account:         model.AccountContext{ID: accountID},
		}

		var localSink any
		for pb.Next() {
			result, err := service.Validate(ctx, request)
			if err != nil {
				b.Error(err)
				return
			}

			localSink = result
		}
		benchSink = localSink // Single write after loop
	})
}
