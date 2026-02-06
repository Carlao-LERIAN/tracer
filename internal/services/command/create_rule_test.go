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

func TestNormalizeName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"MiNhA REGRA", "minha regra"},
		{"  spaces around  ", "spaces around"},
		{"UPPERCASE", "uppercase"},
		{"lowercase", "lowercase"},
		{"  Mixed CASE with Spaces  ", "mixed case with spaces"},
		{"", ""},
		{"   ", ""},
		{"  mInha    regra  xpto ", "minha regra xpto"},
		{"minha reGra xpto", "minha regra xpto"},
		{"a   b    c     d", "a b c d"},
		{"tab\there", "tab here"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := NormalizeName(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCreateRuleCommand_Execute(t *testing.T) {
	tests := []struct {
		name      string
		input     *CreateRuleInput
		mockSetup func(ctrl *gomock.Controller) (*MockRuleRepository, *MockExpressionCompiler)
		wantErr   bool
		errIs     error
	}{
		{
			name: "success - creates rule with valid input",
			input: &CreateRuleInput{
				Name:        "High Value Transaction Rule",
				Description: "Blocks transactions over $10,000",
				Expression:  "amount > 1000000",
				Action:      model.DecisionDeny,
				Scopes: []model.Scope{
					{AccountID: testutil.UUIDPtr(testutil.MustDeterministicUUID(1))},
				},
			},
			mockSetup: func(ctrl *gomock.Controller) (*MockRuleRepository, *MockExpressionCompiler) {
				mockRepo := NewMockRuleRepository(ctrl)
				mockCEL := NewMockExpressionCompiler(ctrl)

				mockCEL.EXPECT().
					Compile(gomock.Any(), "amount > 1000000").
					Return(nil, nil)
				mockRepo.EXPECT().
					GetByName(gomock.Any(), "high value transaction rule").
					Return(nil, constant.ErrRuleNotFound)
				mockRepo.EXPECT().
					Create(gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, rule *model.Rule) (*model.Rule, error) {
						return rule, nil
					})

				return mockRepo, mockCEL
			},
			wantErr: false,
		},
		{
			name: "success - creates rule without scopes (global rule)",
			input: &CreateRuleInput{
				Name:       "Global Fraud Rule",
				Expression: "amount > 5000000",
				Action:     model.DecisionReview,
				Scopes:     []model.Scope{},
			},
			mockSetup: func(ctrl *gomock.Controller) (*MockRuleRepository, *MockExpressionCompiler) {
				mockRepo := NewMockRuleRepository(ctrl)
				mockCEL := NewMockExpressionCompiler(ctrl)

				mockCEL.EXPECT().
					Compile(gomock.Any(), "amount > 5000000").
					Return(nil, nil)
				mockRepo.EXPECT().
					GetByName(gomock.Any(), "global fraud rule").
					Return(nil, constant.ErrRuleNotFound)
				mockRepo.EXPECT().
					Create(gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, rule *model.Rule) (*model.Rule, error) {
						return rule, nil
					})

				return mockRepo, mockCEL
			},
			wantErr: false,
		},
		{
			name: "error - invalid CEL expression",
			input: &CreateRuleInput{
				Name:       "Bad Expression Rule",
				Expression: "invalid cel >>>",
				Action:     model.DecisionDeny,
			},
			mockSetup: func(ctrl *gomock.Controller) (*MockRuleRepository, *MockExpressionCompiler) {
				mockRepo := NewMockRuleRepository(ctrl)
				mockCEL := NewMockExpressionCompiler(ctrl)

				mockCEL.EXPECT().
					Compile(gomock.Any(), "invalid cel >>>").
					Return(nil, constant.ErrExpressionSyntax)

				return mockRepo, mockCEL
			},
			wantErr: true,
			errIs:   constant.ErrExpressionSyntax,
		},
		{
			name: "error - rule name already exists (case insensitive)",
			input: &CreateRuleInput{
				Name:       "  EXISTING Rule  ",
				Expression: "amount > 100",
				Action:     model.DecisionAllow,
			},
			mockSetup: func(ctrl *gomock.Controller) (*MockRuleRepository, *MockExpressionCompiler) {
				mockRepo := NewMockRuleRepository(ctrl)
				mockCEL := NewMockExpressionCompiler(ctrl)

				mockCEL.EXPECT().
					Compile(gomock.Any(), "amount > 100").
					Return(nil, nil)
				mockRepo.EXPECT().
					GetByName(gomock.Any(), "existing rule").
					Return(&model.Rule{Name: "existing rule"}, nil)

				return mockRepo, mockCEL
			},
			wantErr: true,
			errIs:   constant.ErrRuleNameAlreadyExists,
		},
		{
			name: "error - repository create fails",
			input: &CreateRuleInput{
				Name:       "New Rule",
				Expression: "amount > 100",
				Action:     model.DecisionDeny,
			},
			mockSetup: func(ctrl *gomock.Controller) (*MockRuleRepository, *MockExpressionCompiler) {
				mockRepo := NewMockRuleRepository(ctrl)
				mockCEL := NewMockExpressionCompiler(ctrl)

				mockCEL.EXPECT().
					Compile(gomock.Any(), "amount > 100").
					Return(nil, nil)
				mockRepo.EXPECT().
					GetByName(gomock.Any(), "new rule").
					Return(nil, constant.ErrRuleNotFound)
				mockRepo.EXPECT().
					Create(gomock.Any(), gomock.Any()).
					Return(nil, errors.New("database error"))

				return mockRepo, mockCEL
			},
			wantErr: true,
		},
		{
			name: "error - GetByName fails with unexpected error",
			input: &CreateRuleInput{
				Name:       "Test Rule",
				Expression: "amount > 100",
				Action:     model.DecisionDeny,
			},
			mockSetup: func(ctrl *gomock.Controller) (*MockRuleRepository, *MockExpressionCompiler) {
				mockRepo := NewMockRuleRepository(ctrl)
				mockCEL := NewMockExpressionCompiler(ctrl)

				mockCEL.EXPECT().
					Compile(gomock.Any(), "amount > 100").
					Return(nil, nil)
				mockRepo.EXPECT().
					GetByName(gomock.Any(), "test rule").
					Return(nil, errors.New("database connection error"))

				return mockRepo, mockCEL
			},
			wantErr: true,
		},
		{
			name:  "error - nil input",
			input: nil,
			mockSetup: func(ctrl *gomock.Controller) (*MockRuleRepository, *MockExpressionCompiler) {
				// No mock expectations since we should fail before calling repo or CEL
				mockRepo := NewMockRuleRepository(ctrl)
				mockCEL := NewMockExpressionCompiler(ctrl)
				return mockRepo, mockCEL
			},
			wantErr: true,
			errIs:   constant.ErrRuleNilInput,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)

			mockRepo, mockCEL := tt.mockSetup(ctrl)
			auditWriter := NewMockAuditWriter(ctrl)

			// Setup audit expectations based on success/error
			if tt.wantErr {
				// No audit event expected - operation failed
				auditWriter.EXPECT().
					RecordRuleEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Times(0)
			} else {
				// Audit event should be called exactly once with specific parameters
				auditWriter.EXPECT().
					RecordRuleEvent(
						gomock.Any(),                   // ctx
						model.AuditEventRuleCreated,    // eventType
						model.AuditActionCreate,        // action
						gomock.Any(),                   // ruleID (generated)
						nil,                            // beforeState (no before for create)
						gomock.Any(),                   // afterState
						"Rule created via API",         // description
						gomock.Any(),                   // clientIP
					).
					Times(1).
					Return(nil)
			}

			cmd := NewCreateRuleCommand(mockRepo, mockCEL, testutil.NewDefaultMockClock(), auditWriter)

			ctx := context.Background()
			result, err := cmd.Execute(ctx, tt.input)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errIs != nil {
					assert.True(t, errors.Is(err, tt.errIs), "expected error %v, got %v", tt.errIs, err)
				}
				assert.Nil(t, result)
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
				assert.NotEqual(t, uuid.Nil, result.ID)
				assert.Equal(t, NormalizeName(tt.input.Name), result.Name)
				assert.Equal(t, tt.input.Action, result.Action)
				assert.Equal(t, model.RuleStatusDraft, result.Status)
			}
		})
	}
}

func TestCreateRuleCommand_Execute_SetsCorrectFields(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockRepo := NewMockRuleRepository(ctrl)
	mockCEL := NewMockExpressionCompiler(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)

	cmd := NewCreateRuleCommand(mockRepo, mockCEL, testutil.NewDefaultMockClock(), auditWriter)

	testAccountID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440001")

	input := &CreateRuleInput{
		Name:        "Test Rule",
		Description: "Test Description",
		Expression:  "amount > 100",
		Action:      model.DecisionReview,
		Scopes: []model.Scope{
			{
				AccountID:       testutil.UUIDPtr(testAccountID),
				TransactionType: testutil.Ptr(model.TransactionTypeCard),
			},
		},
	}

	normalizedName := NormalizeName(input.Name)

	mockCEL.EXPECT().
		Compile(gomock.Any(), input.Expression).
		Return(nil, nil)
	mockRepo.EXPECT().
		GetByName(gomock.Any(), normalizedName).
		Return(nil, constant.ErrRuleNotFound)
	mockRepo.EXPECT().
		Create(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, rule *model.Rule) (*model.Rule, error) {
			assert.NotEqual(t, uuid.Nil, rule.ID)
			assert.Equal(t, normalizedName, rule.Name)
			assert.Equal(t, input.Description, *rule.Description)
			assert.Equal(t, input.Expression, rule.Expression)
			assert.Equal(t, input.Action, rule.Action)
			assert.Equal(t, model.RuleStatusDraft, rule.Status)
			assert.Len(t, rule.Scopes, 1)
			assert.Equal(t, testAccountID, *rule.Scopes[0].AccountID)
			assert.Equal(t, testutil.FixedTime(), rule.CreatedAt)
			assert.Equal(t, testutil.FixedTime(), rule.UpdatedAt)
			assert.Nil(t, rule.DeletedAt)
			return rule, nil
		})

	// Audit event should be called exactly once with specific parameters
	auditWriter.EXPECT().
		RecordRuleEvent(
			gomock.Any(),                // ctx
			model.AuditEventRuleCreated, // eventType
			model.AuditActionCreate,     // action
			gomock.Any(),                // ruleID
			nil,                         // beforeState (no before for create)
			gomock.Any(),                // afterState
			"Rule created via API",      // description
			gomock.Any(),                // clientIP
		).
		Times(1).
		Return(nil)

	ctx := context.Background()
	result, err := cmd.Execute(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, result)
}
