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

func TestUpdateRuleCommand_Execute(t *testing.T) {
	ruleID := testutil.MustDeterministicUUID(1)
	// Use fixed times for deterministic tests
	baseTime := time.Date(2024, 1, 15, 9, 0, 0, 0, time.UTC)
	existingRule := &model.Rule{
		ID:         ruleID,
		Name:       "existing rule",
		Expression: "amount > 1000",
		Action:     model.DecisionDeny,
		Status:     model.RuleStatusDraft,
		CreatedAt:  baseTime,
		UpdatedAt:  baseTime,
	}

	tests := []struct {
		name      string
		ruleID    uuid.UUID
		input     *UpdateRuleInput
		mockSetup func(ctrl *gomock.Controller) (*MockRuleRepository, *MockExpressionCompiler)
		wantErr   bool
		errIs     error
	}{
		{
			name:   "success - update name only",
			ruleID: ruleID,
			input: &UpdateRuleInput{
				Name: testutil.StringPtr("Updated Rule Name"),
			},
			mockSetup: func(ctrl *gomock.Controller) (*MockRuleRepository, *MockExpressionCompiler) {
				mockRepo := NewMockRuleRepository(ctrl)
				mockCEL := NewMockExpressionCompiler(ctrl)

				mockRepo.EXPECT().
					GetByID(gomock.Any(), ruleID).
					Return(copyRule(existingRule), nil)
				mockRepo.EXPECT().
					GetByName(gomock.Any(), "updated rule name").
					Return(nil, constant.ErrRuleNotFound)
				mockRepo.EXPECT().
					Update(gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, rule *model.Rule) (*model.Rule, error) {
						return rule, nil
					})

				return mockRepo, mockCEL
			},
			wantErr: false,
		},
		{
			name:   "success - update expression only",
			ruleID: ruleID,
			input: &UpdateRuleInput{
				Expression: testutil.StringPtr("amount > 5000"),
			},
			mockSetup: func(ctrl *gomock.Controller) (*MockRuleRepository, *MockExpressionCompiler) {
				mockRepo := NewMockRuleRepository(ctrl)
				mockCEL := NewMockExpressionCompiler(ctrl)

				mockRepo.EXPECT().
					GetByID(gomock.Any(), ruleID).
					Return(copyRule(existingRule), nil)
				mockCEL.EXPECT().
					Compile(gomock.Any(), "amount > 5000").
					Return(nil, nil)
				mockRepo.EXPECT().
					Update(gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, rule *model.Rule) (*model.Rule, error) {
						return rule, nil
					})

				return mockRepo, mockCEL
			},
			wantErr: false,
		},
		{
			name:   "success - update action only",
			ruleID: ruleID,
			input: &UpdateRuleInput{
				Action: testutil.Ptr(model.DecisionReview),
			},
			mockSetup: func(ctrl *gomock.Controller) (*MockRuleRepository, *MockExpressionCompiler) {
				mockRepo := NewMockRuleRepository(ctrl)
				mockCEL := NewMockExpressionCompiler(ctrl)

				mockRepo.EXPECT().
					GetByID(gomock.Any(), ruleID).
					Return(copyRule(existingRule), nil)
				mockRepo.EXPECT().
					Update(gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, rule *model.Rule) (*model.Rule, error) {
						return rule, nil
					})

				return mockRepo, mockCEL
			},
			wantErr: false,
		},
		{
			name:   "success - update scopes only",
			ruleID: ruleID,
			input: &UpdateRuleInput{
				Scopes: testutil.Ptr([]model.Scope{
					{AccountID: testutil.UUIDPtr(uuid.MustParse("550e8400-e29b-41d4-a716-446655440001"))},
				}),
			},
			mockSetup: func(ctrl *gomock.Controller) (*MockRuleRepository, *MockExpressionCompiler) {
				mockRepo := NewMockRuleRepository(ctrl)
				mockCEL := NewMockExpressionCompiler(ctrl)

				mockRepo.EXPECT().
					GetByID(gomock.Any(), ruleID).
					Return(copyRule(existingRule), nil)
				mockRepo.EXPECT().
					Update(gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, rule *model.Rule) (*model.Rule, error) {
						return rule, nil
					})

				return mockRepo, mockCEL
			},
			wantErr: false,
		},
		{
			name:   "success - update multiple fields",
			ruleID: ruleID,
			input: &UpdateRuleInput{
				Name:        testutil.StringPtr("New Name"),
				Description: testutil.StringPtr("New Description"),
				Expression:  testutil.StringPtr("amount > 10000"),
				Action:      testutil.Ptr(model.DecisionAllow),
			},
			mockSetup: func(ctrl *gomock.Controller) (*MockRuleRepository, *MockExpressionCompiler) {
				mockRepo := NewMockRuleRepository(ctrl)
				mockCEL := NewMockExpressionCompiler(ctrl)

				mockRepo.EXPECT().
					GetByID(gomock.Any(), ruleID).
					Return(copyRule(existingRule), nil)
				mockCEL.EXPECT().
					Compile(gomock.Any(), "amount > 10000").
					Return(nil, nil)
				mockRepo.EXPECT().
					GetByName(gomock.Any(), "new name").
					Return(nil, constant.ErrRuleNotFound)
				mockRepo.EXPECT().
					Update(gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, rule *model.Rule) (*model.Rule, error) {
						return rule, nil
					})

				return mockRepo, mockCEL
			},
			wantErr: false,
		},
		{
			name:   "success - update name to same normalized value (no uniqueness check)",
			ruleID: ruleID,
			input: &UpdateRuleInput{
				Name: testutil.StringPtr("  EXISTING  RULE  "), // normalizes to "existing rule"
			},
			mockSetup: func(ctrl *gomock.Controller) (*MockRuleRepository, *MockExpressionCompiler) {
				mockRepo := NewMockRuleRepository(ctrl)
				mockCEL := NewMockExpressionCompiler(ctrl)

				mockRepo.EXPECT().
					GetByID(gomock.Any(), ruleID).
					Return(copyRule(existingRule), nil)
				// No GetByName call - same normalized name
				mockRepo.EXPECT().
					Update(gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, rule *model.Rule) (*model.Rule, error) {
						return rule, nil
					})

				return mockRepo, mockCEL
			},
			wantErr: false,
		},
		{
			name:   "error - rule not found",
			ruleID: ruleID,
			input: &UpdateRuleInput{
				Name: testutil.StringPtr("New Name"),
			},
			mockSetup: func(ctrl *gomock.Controller) (*MockRuleRepository, *MockExpressionCompiler) {
				mockRepo := NewMockRuleRepository(ctrl)
				mockCEL := NewMockExpressionCompiler(ctrl)

				mockRepo.EXPECT().
					GetByID(gomock.Any(), ruleID).
					Return(nil, constant.ErrRuleNotFound)

				return mockRepo, mockCEL
			},
			wantErr: true,
			errIs:   constant.ErrRuleNotFound,
		},
		{
			name:   "error - invalid CEL expression",
			ruleID: ruleID,
			input: &UpdateRuleInput{
				Expression: testutil.StringPtr("invalid cel >>>"),
			},
			mockSetup: func(ctrl *gomock.Controller) (*MockRuleRepository, *MockExpressionCompiler) {
				mockRepo := NewMockRuleRepository(ctrl)
				mockCEL := NewMockExpressionCompiler(ctrl)

				mockRepo.EXPECT().
					GetByID(gomock.Any(), ruleID).
					Return(copyRule(existingRule), nil)
				mockCEL.EXPECT().
					Compile(gomock.Any(), "invalid cel >>>").
					Return(nil, constant.ErrExpressionSyntax)

				return mockRepo, mockCEL
			},
			wantErr: true,
			errIs:   constant.ErrExpressionSyntax,
		},
		{
			name:   "error - name already exists",
			ruleID: ruleID,
			input: &UpdateRuleInput{
				Name: testutil.StringPtr("Another Rule"),
			},
			mockSetup: func(ctrl *gomock.Controller) (*MockRuleRepository, *MockExpressionCompiler) {
				mockRepo := NewMockRuleRepository(ctrl)
				mockCEL := NewMockExpressionCompiler(ctrl)

				mockRepo.EXPECT().
					GetByID(gomock.Any(), ruleID).
					Return(copyRule(existingRule), nil)
				mockRepo.EXPECT().
					GetByName(gomock.Any(), "another rule").
					Return(&model.Rule{Name: "another rule"}, nil)

				return mockRepo, mockCEL
			},
			wantErr: true,
			errIs:   constant.ErrRuleNameAlreadyExists,
		},
		{
			name:   "error - repository update fails",
			ruleID: ruleID,
			input: &UpdateRuleInput{
				Action: testutil.Ptr(model.DecisionReview),
			},
			mockSetup: func(ctrl *gomock.Controller) (*MockRuleRepository, *MockExpressionCompiler) {
				mockRepo := NewMockRuleRepository(ctrl)
				mockCEL := NewMockExpressionCompiler(ctrl)

				mockRepo.EXPECT().
					GetByID(gomock.Any(), ruleID).
					Return(copyRule(existingRule), nil)
				mockRepo.EXPECT().
					Update(gomock.Any(), gomock.Any()).
					Return(nil, errors.New("database error"))

				return mockRepo, mockCEL
			},
			wantErr: true,
		},
		{
			name:   "error - GetByID fails with unexpected error",
			ruleID: ruleID,
			input: &UpdateRuleInput{
				Name: testutil.StringPtr("New Name"),
			},
			mockSetup: func(ctrl *gomock.Controller) (*MockRuleRepository, *MockExpressionCompiler) {
				mockRepo := NewMockRuleRepository(ctrl)
				mockCEL := NewMockExpressionCompiler(ctrl)

				mockRepo.EXPECT().
					GetByID(gomock.Any(), ruleID).
					Return(nil, errors.New("database connection error"))

				return mockRepo, mockCEL
			},
			wantErr: true,
		},
		{
			name:   "error - GetByName fails with unexpected error during name change",
			ruleID: ruleID,
			input: &UpdateRuleInput{
				Name: testutil.StringPtr("Different Name"),
			},
			mockSetup: func(ctrl *gomock.Controller) (*MockRuleRepository, *MockExpressionCompiler) {
				mockRepo := NewMockRuleRepository(ctrl)
				mockCEL := NewMockExpressionCompiler(ctrl)

				mockRepo.EXPECT().
					GetByID(gomock.Any(), ruleID).
					Return(copyRule(existingRule), nil)
				mockRepo.EXPECT().
					GetByName(gomock.Any(), "different name").
					Return(nil, errors.New("database connection error"))

				return mockRepo, mockCEL
			},
			wantErr: true,
		},
		{
			name:   "error - invalid action decision",
			ruleID: ruleID,
			input: &UpdateRuleInput{
				Action: testutil.Ptr(model.Decision("INVALID")),
			},
			mockSetup: func(ctrl *gomock.Controller) (*MockRuleRepository, *MockExpressionCompiler) {
				mockRepo := NewMockRuleRepository(ctrl)
				mockCEL := NewMockExpressionCompiler(ctrl)

				mockRepo.EXPECT().
					GetByID(gomock.Any(), ruleID).
					Return(copyRule(existingRule), nil)

				return mockRepo, mockCEL
			},
			wantErr: true,
			errIs:   constant.ErrRuleInvalidAction,
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
						gomock.Any(),                // ctx
						model.AuditEventRuleUpdated, // eventType
						model.AuditActionUpdate,     // action
						tt.ruleID,                   // ruleID
						gomock.Any(),                // beforeState
						gomock.Any(),                // afterState
						"Rule updated via API",      // description
						gomock.Any(),                // clientIP
					).
					Times(1).
					Return(nil)
			}

			cmd := NewUpdateRuleCommand(mockRepo, mockCEL, testutil.NewDefaultMockClock(), auditWriter)

			ctx := context.Background()
			result, err := cmd.Execute(ctx, tt.ruleID, tt.input)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errIs != nil {
					assert.True(t, errors.Is(err, tt.errIs), "expected error %v, got %v", tt.errIs, err)
				}
				assert.Nil(t, result)
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
			}
		})
	}
}

func TestUpdateRuleCommand_Execute_AppliesChangesCorrectly(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockRepo := NewMockRuleRepository(ctrl)
	mockCEL := NewMockExpressionCompiler(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)
	cmd := NewUpdateRuleCommand(mockRepo, mockCEL, testutil.NewDefaultMockClock(), auditWriter)

	ruleID := testutil.MustDeterministicUUID(1)
	// Use a fixed time that is before the mock clock time (2024-01-15 10:30:00 UTC)
	originalUpdatedAt := time.Date(2024, 1, 15, 9, 30, 0, 0, time.UTC)
	existingRule := &model.Rule{
		ID:         ruleID,
		Name:       "old name",
		Expression: "amount > 100",
		Action:     model.DecisionDeny,
		Status:     model.RuleStatusDraft,
		CreatedAt:  time.Date(2024, 1, 15, 9, 0, 0, 0, time.UTC),
		UpdatedAt:  originalUpdatedAt,
	}

	testAccountID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440002")

	input := &UpdateRuleInput{
		Name:        testutil.StringPtr("  NEW  NAME  "),
		Description: testutil.StringPtr("Updated description"),
		Expression:  testutil.StringPtr("amount > 999"),
		Action:      testutil.Ptr(model.DecisionAllow),
		Scopes: testutil.Ptr([]model.Scope{
			{AccountID: testutil.UUIDPtr(testAccountID)},
		}),
	}

	mockRepo.EXPECT().
		GetByID(gomock.Any(), ruleID).
		Return(existingRule, nil)
	mockCEL.EXPECT().
		Compile(gomock.Any(), "amount > 999").
		Return(nil, nil)
	mockRepo.EXPECT().
		GetByName(gomock.Any(), "new name").
		Return(nil, constant.ErrRuleNotFound)
	mockRepo.EXPECT().
		Update(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, rule *model.Rule) (*model.Rule, error) {
			// Verify all changes were applied
			assert.Equal(t, ruleID, rule.ID)
			assert.Equal(t, "new name", rule.Name) // normalized
			assert.Equal(t, "Updated description", *rule.Description)
			assert.Equal(t, "amount > 999", rule.Expression)
			assert.Equal(t, model.DecisionAllow, rule.Action)
			assert.Len(t, rule.Scopes, 1)
			assert.Equal(t, testAccountID, *rule.Scopes[0].AccountID)
			assert.True(t, rule.UpdatedAt.After(originalUpdatedAt))
			return rule, nil
		})

	// Audit event should be called exactly once with specific parameters
	auditWriter.EXPECT().
		RecordRuleEvent(
			gomock.Any(),                // ctx
			model.AuditEventRuleUpdated, // eventType
			model.AuditActionUpdate,     // action
			ruleID,                      // ruleID
			gomock.Any(),                // beforeState
			gomock.Any(),                // afterState
			"Rule updated via API",      // description
			gomock.Any(),                // clientIP
		).
		Times(1).
		Return(nil)

	ctx := context.Background()
	result, err := cmd.Execute(ctx, ruleID, input)

	require.NoError(t, err)
	require.NotNil(t, result)
}

// copyRule creates a copy of a rule to avoid mutation issues in tests
func copyRule(r *model.Rule) *model.Rule {
	if r == nil {
		return nil
	}
	copy := *r
	return &copy
}
