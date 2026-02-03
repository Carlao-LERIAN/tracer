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

	"tracer/internal/testutil"
	"tracer/pkg/constant"
	"tracer/pkg/model"
)

func TestGetRuleQuery_Execute(t *testing.T) {
	ruleID := uuid.New()
	testAccountID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440001")
	existingRule := &model.Rule{
		ID:         ruleID,
		Name:       "test rule",
		Expression: "amount > 1000",
		Action:     model.DecisionDeny,
		Status:     model.RuleStatusActive,
		Scopes: []model.Scope{
			{AccountID: testutil.UUIDPtr(testAccountID)},
		},
		CreatedAt: time.Now().Add(-time.Hour),
		UpdatedAt: time.Now(),
	}

	tests := []struct {
		name      string
		ruleID    uuid.UUID
		mockSetup func(ctrl *gomock.Controller) *MockRuleRepository
		wantErr   bool
		errIs     error
	}{
		{
			name:   "success - returns rule",
			ruleID: ruleID,
			mockSetup: func(ctrl *gomock.Controller) *MockRuleRepository {
				mockRepo := NewMockRuleRepository(ctrl)
				mockRepo.EXPECT().
					GetByID(gomock.Any(), ruleID).
					Return(existingRule, nil)
				return mockRepo
			},
			wantErr: false,
		},
		{
			name:   "error - rule not found",
			ruleID: ruleID,
			mockSetup: func(ctrl *gomock.Controller) *MockRuleRepository {
				mockRepo := NewMockRuleRepository(ctrl)
				mockRepo.EXPECT().
					GetByID(gomock.Any(), ruleID).
					Return(nil, constant.ErrRuleNotFound)
				return mockRepo
			},
			wantErr: true,
			errIs:   constant.ErrRuleNotFound,
		},
		{
			name:   "error - repository error",
			ruleID: ruleID,
			mockSetup: func(ctrl *gomock.Controller) *MockRuleRepository {
				mockRepo := NewMockRuleRepository(ctrl)
				mockRepo.EXPECT().
					GetByID(gomock.Any(), ruleID).
					Return(nil, errors.New("database error"))
				return mockRepo
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)

			mockRepo := tt.mockSetup(ctrl)

			query := NewGetRuleQuery(mockRepo)

			ctx := context.Background()
			result, err := query.Execute(ctx, tt.ruleID)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errIs != nil {
					assert.True(t, errors.Is(err, tt.errIs), "expected error %v, got %v", tt.errIs, err)
				}
				assert.Nil(t, result)
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
				assert.Equal(t, tt.ruleID, result.ID)
				assert.Equal(t, existingRule.Name, result.Name)
				assert.Equal(t, existingRule.Expression, result.Expression)
				assert.Equal(t, existingRule.Action, result.Action)
				assert.Equal(t, existingRule.Status, result.Status)
			}
		})
	}
}
