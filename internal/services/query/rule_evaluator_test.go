// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package query

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"tracer/internal/adapters/cel"
	"tracer/internal/testutil"
	"tracer/pkg/model"
)

func TestRuleEvaluator_Evaluate(t *testing.T) {
	testRuleID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	testAccountID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440001")
	testRequestID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440002")
	testRequestNoMatchID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440003")
	now := time.Date(2030, 1, 1, 12, 0, 0, 0, time.UTC)

	testRule := &model.Rule{
		ID:         testRuleID,
		Name:       "High amount fraud rule",
		Expression: "amount > 100000",
		Action:     model.DecisionDeny,
		Status:     model.RuleStatusActive,
		Scopes:     []model.Scope{},
		CreatedAt:  now.Add(-24 * time.Hour),
		UpdatedAt:  now.Add(-1 * time.Hour),
	}

	testRequest := &model.ValidationRequest{
		RequestID:       testRequestID,
		TransactionType: model.TransactionTypeCard,
		Amount:          150000, // $1500.00 in cents, should trigger rule
		Currency:        "USD",
		TransactionTimestamp: now,
		Account: model.AccountContext{
			ID: testAccountID,
		},
		Metadata: map[string]any{},
	}

	testRequestNoMatch := &model.ValidationRequest{
		RequestID:       testRequestNoMatchID,
		TransactionType: model.TransactionTypeCard,
		Amount:          5000, // $50.00 in cents, should NOT trigger rule
		Currency:        "USD",
		TransactionTimestamp: now,
		Account: model.AccountContext{
			ID: testAccountID,
		},
		Metadata: map[string]any{},
	}

	mockCompiledProgram := &cel.CompiledProgram{
		ExpressionHash:   "test-hash",
		SourceExpression: testRule.Expression,
		CompiledAt:       now,
		CompileTimeMs:    1,
	}

	tests := []struct {
		name           string
		rule           *model.Rule
		request        *model.ValidationRequest
		mockSetup      func(ctrl *gomock.Controller) *MockExpressionEvaluator
		expectedResult bool
		wantErr        bool
		expectedErr    error
		expectedErrMsg string
	}{
		{
			name:    "rule matches when expression evaluates to true",
			rule:    testRule,
			request: testRequest,
			mockSetup: func(ctrl *gomock.Controller) *MockExpressionEvaluator {
				mockEval := NewMockExpressionEvaluator(ctrl)
				mockEval.EXPECT().
					Compile(gomock.Any(), testRule.Expression).
					Return(mockCompiledProgram, nil)
				mockEval.EXPECT().
					Evaluate(gomock.Any(), mockCompiledProgram, testRequest).
					Return(true, nil)
				return mockEval
			},
			expectedResult: true,
			wantErr:        false,
		},
		{
			name:    "rule does not match when expression evaluates to false",
			rule:    testRule,
			request: testRequestNoMatch,
			mockSetup: func(ctrl *gomock.Controller) *MockExpressionEvaluator {
				mockEval := NewMockExpressionEvaluator(ctrl)
				mockEval.EXPECT().
					Compile(gomock.Any(), testRule.Expression).
					Return(mockCompiledProgram, nil)
				mockEval.EXPECT().
					Evaluate(gomock.Any(), mockCompiledProgram, testRequestNoMatch).
					Return(false, nil)
				return mockEval
			},
			expectedResult: false,
			wantErr:        false,
		},
		{
			name:    "returns error when CEL compilation fails",
			rule:    testRule,
			request: testRequest,
			mockSetup: func(ctrl *gomock.Controller) *MockExpressionEvaluator {
				mockEval := NewMockExpressionEvaluator(ctrl)
				mockEval.EXPECT().
					Compile(gomock.Any(), testRule.Expression).
					Return(nil, errors.New("syntax error in expression"))
				return mockEval
			},
			expectedResult: false,
			wantErr:        true,
			expectedErrMsg: "failed to compile expression",
		},
		{
			name:    "returns error when CEL evaluation fails",
			rule:    testRule,
			request: testRequest,
			mockSetup: func(ctrl *gomock.Controller) *MockExpressionEvaluator {
				mockEval := NewMockExpressionEvaluator(ctrl)
				mockEval.EXPECT().
					Compile(gomock.Any(), testRule.Expression).
					Return(mockCompiledProgram, nil)
				mockEval.EXPECT().
					Evaluate(gomock.Any(), mockCompiledProgram, testRequest).
					Return(false, errors.New("evaluation error"))
				return mockEval
			},
			expectedResult: false,
			wantErr:        true,
			expectedErrMsg: "failed to evaluate expression",
		},
		{
			name:    "returns error when rule is nil",
			rule:    nil,
			request: testRequest,
			mockSetup: func(ctrl *gomock.Controller) *MockExpressionEvaluator {
				return NewMockExpressionEvaluator(ctrl)
			},
			expectedResult: false,
			wantErr:        true,
			expectedErr:    ErrNilRule,
		},
		{
			name:    "returns error when request is nil",
			rule:    testRule,
			request: nil,
			mockSetup: func(ctrl *gomock.Controller) *MockExpressionEvaluator {
				return NewMockExpressionEvaluator(ctrl)
			},
			expectedResult: false,
			wantErr:        true,
			expectedErr:    ErrNilRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testutil.SetupTestTracing(t)

			ctrl := gomock.NewController(t)

			mockEval := tt.mockSetup(ctrl)

			evaluator, err := NewRuleEvaluator(mockEval)
			require.NoError(t, err)

			ctx := context.Background()
			result, err := evaluator.Evaluate(ctx, tt.rule, tt.request)

			if tt.wantErr {
				require.Error(t, err)
				if tt.expectedErr != nil {
					assert.ErrorIs(t, err, tt.expectedErr)
				} else {
					assert.Contains(t, err.Error(), tt.expectedErrMsg)
				}
				assert.False(t, result)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedResult, result)
			}
		})
	}
}

func TestNewRuleEvaluator_NilExpressionEvaluator(t *testing.T) {
	evaluator, err := NewRuleEvaluator(nil)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNilExpressionEvaluator)
	assert.Nil(t, evaluator)
}
