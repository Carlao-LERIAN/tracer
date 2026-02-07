// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package command

import (
	"context"
	"errors"
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

func TestNewUpdateLimitCommand(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockRepo := NewMockLimitRepository(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)
	// No audit expected - constructor only
	cmd := NewUpdateLimitCommand(mockRepo, auditWriter)

	assert.NotNil(t, cmd)
	assert.Equal(t, mockRepo, cmd.repo)
}

func TestUpdateLimitCommand_Execute(t *testing.T) {
	limitID := testutil.MustDeterministicUUID(1)
	now := testutil.FixedTime().UTC()

	// newExistingLimit creates a fresh limit instance per test to avoid mutation side effects.
	// Each test gets its own copy, preventing test interdependencies.
	newExistingLimit := func() *model.Limit {
		return &model.Limit{
			ID:        limitID,
			Name:      "Original Limit",
			LimitType: model.LimitTypeDaily,
			MaxAmount: decimal.RequireFromString("1000"),
			Currency:  "USD",
			Scopes:    []model.Scope{{AccountID: testutil.UUIDPtr(testutil.MustDeterministicUUID(2))}},
			Status:    model.LimitStatusActive,
			CreatedAt: now,
			UpdatedAt: now,
		}
	}

	tests := []struct {
		name        string
		limitID     uuid.UUID
		input       *UpdateLimitInput
		setupMock   func(*MockLimitRepository)
		expectError bool
		errorIs     error
		validate    func(*testing.T, *model.Limit)
	}{
		{
			name:    "Success - update name only",
			limitID: limitID,
			input: &UpdateLimitInput{
				Name: testutil.StringPtr("Updated Limit Name"),
			},
			setupMock: func(m *MockLimitRepository) {
				m.EXPECT().GetByID(gomock.Any(), limitID).Return(newExistingLimit(), nil)
				m.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)
			},
			expectError: false,
			validate: func(t *testing.T, limit *model.Limit) {
				assert.Equal(t, "Updated Limit Name", limit.Name)
			},
		},
		{
			name:    "Success - update maxAmount only",
			limitID: limitID,
			input: &UpdateLimitInput{
				MaxAmount: testutil.Ptr(decimal.RequireFromString("2000")),
			},
			setupMock: func(m *MockLimitRepository) {
				m.EXPECT().GetByID(gomock.Any(), limitID).Return(newExistingLimit(), nil)
				m.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)
			},
			expectError: false,
			validate: func(t *testing.T, limit *model.Limit) {
				assert.True(t, decimal.RequireFromString("2000").Equal(limit.MaxAmount))
			},
		},
		{
			name:    "Success - update description",
			limitID: limitID,
			input: &UpdateLimitInput{
				Description: testutil.StringPtr("Updated description"),
			},
			setupMock: func(m *MockLimitRepository) {
				m.EXPECT().GetByID(gomock.Any(), limitID).Return(newExistingLimit(), nil)
				m.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)
			},
			expectError: false,
			validate: func(t *testing.T, limit *model.Limit) {
				require.NotNil(t, limit.Description)
				assert.Equal(t, "Updated description", *limit.Description)
			},
		},
		{
			name:    "Success - update scopes",
			limitID: limitID,
			input: &UpdateLimitInput{
				Scopes: &[]model.Scope{{PortfolioID: testutil.UUIDPtr(testutil.MustDeterministicUUID(10))}},
			},
			setupMock: func(m *MockLimitRepository) {
				m.EXPECT().GetByID(gomock.Any(), limitID).Return(newExistingLimit(), nil)
				m.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)
			},
			expectError: false,
			validate: func(t *testing.T, limit *model.Limit) {
				assert.Len(t, limit.Scopes, 1)
				assert.NotNil(t, limit.Scopes[0].PortfolioID)
			},
		},
		{
			name:    "Success - update multiple fields",
			limitID: limitID,
			input: &UpdateLimitInput{
				Name:        testutil.StringPtr("Multi-Update Limit"),
				MaxAmount:   testutil.Ptr(decimal.RequireFromString("3000")),
				Description: testutil.StringPtr("New description"),
			},
			setupMock: func(m *MockLimitRepository) {
				m.EXPECT().GetByID(gomock.Any(), limitID).Return(newExistingLimit(), nil)
				m.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)
			},
			expectError: false,
			validate: func(t *testing.T, limit *model.Limit) {
				assert.Equal(t, "Multi-Update Limit", limit.Name)
				assert.True(t, decimal.RequireFromString("3000").Equal(limit.MaxAmount))
			},
		},
		{
			name:    "Success - no changes (all nil)",
			limitID: limitID,
			input:   &UpdateLimitInput{},
			setupMock: func(m *MockLimitRepository) {
				m.EXPECT().GetByID(gomock.Any(), limitID).Return(newExistingLimit(), nil)
				// No Update call expected when no fields are modified
			},
			expectError: false,
		},
		{
			name:    "Failure - limit not found",
			limitID: testutil.MustDeterministicUUID(20),
			input: &UpdateLimitInput{
				Name: testutil.StringPtr("New Name"),
			},
			setupMock: func(m *MockLimitRepository) {
				m.EXPECT().GetByID(gomock.Any(), gomock.Any()).Return(nil, constant.ErrLimitNotFound)
			},
			expectError: true,
			errorIs:     constant.ErrLimitNotFound,
		},
		{
			name:    "Failure - empty name",
			limitID: limitID,
			input: &UpdateLimitInput{
				Name: testutil.StringPtr(""),
			},
			setupMock: func(m *MockLimitRepository) {
				m.EXPECT().GetByID(gomock.Any(), limitID).Return(newExistingLimit(), nil)
			},
			expectError: true,
		},
		{
			name:    "Failure - zero maxAmount",
			limitID: limitID,
			input: &UpdateLimitInput{
				MaxAmount: testutil.Ptr(decimal.RequireFromString("0")),
			},
			setupMock: func(m *MockLimitRepository) {
				m.EXPECT().GetByID(gomock.Any(), limitID).Return(newExistingLimit(), nil)
			},
			expectError: true,
		},
		{
			name:    "Failure - negative maxAmount",
			limitID: limitID,
			input: &UpdateLimitInput{
				MaxAmount: testutil.Ptr(decimal.RequireFromString("-1")),
			},
			setupMock: func(m *MockLimitRepository) {
				m.EXPECT().GetByID(gomock.Any(), limitID).Return(newExistingLimit(), nil)
			},
			expectError: true,
		},
		{
			name:    "Failure - empty scopes array",
			limitID: limitID,
			input: &UpdateLimitInput{
				Scopes: &[]model.Scope{},
			},
			setupMock: func(m *MockLimitRepository) {
				m.EXPECT().GetByID(gomock.Any(), limitID).Return(newExistingLimit(), nil)
			},
			expectError: true,
		},
		{
			name:    "Failure - invalid scope in array",
			limitID: limitID,
			input: &UpdateLimitInput{
				Scopes: &[]model.Scope{{}},
			},
			setupMock: func(m *MockLimitRepository) {
				m.EXPECT().GetByID(gomock.Any(), limitID).Return(newExistingLimit(), nil)
			},
			expectError: true,
		},
		{
			name:    "Failure - repository GetByID error",
			limitID: limitID,
			input: &UpdateLimitInput{
				Name: testutil.StringPtr("New Name"),
			},
			setupMock: func(m *MockLimitRepository) {
				m.EXPECT().GetByID(gomock.Any(), limitID).Return(nil, errors.New("database error"))
			},
			expectError: true,
		},
		{
			name:    "Failure - repository Update error",
			limitID: limitID,
			input: &UpdateLimitInput{
				Name: testutil.StringPtr("New Name"),
			},
			setupMock: func(m *MockLimitRepository) {
				m.EXPECT().GetByID(gomock.Any(), limitID).Return(newExistingLimit(), nil)
				m.EXPECT().Update(gomock.Any(), gomock.Any()).Return(errors.New("database error"))
			},
			expectError: true,
		},
		{
			name:    "Failure - update deleted limit",
			limitID: limitID,
			input: &UpdateLimitInput{
				Name: testutil.StringPtr("New Name"),
			},
			setupMock: func(m *MockLimitRepository) {
				deletedLimit := &model.Limit{
					ID:        limitID,
					Name:      "Deleted Limit",
					LimitType: model.LimitTypeDaily,
					MaxAmount: decimal.RequireFromString("1000"),
					Currency:  "USD",
					Scopes:    []model.Scope{{AccountID: testutil.UUIDPtr(testutil.MustDeterministicUUID(30))}},
					Status:    model.LimitStatusDeleted,
					CreatedAt: now,
					UpdatedAt: now,
				}
				m.EXPECT().GetByID(gomock.Any(), limitID).Return(deletedLimit, nil)
			},
			expectError: true,
			errorIs:     constant.ErrLimitAlreadyDeleted,
		},
		{
			name:    "Failure - description with XSS content",
			limitID: limitID,
			input: &UpdateLimitInput{
				Description: testutil.StringPtr("<script>alert('xss')</script>"),
			},
			setupMock: func(m *MockLimitRepository) {
				m.EXPECT().GetByID(gomock.Any(), limitID).Return(newExistingLimit(), nil)
			},
			expectError: true,
			errorIs:     constant.ErrLimitDescriptionInvalidChars,
		},
		{
			name:        "Failure - nil input",
			limitID:     limitID,
			input:       nil,
			setupMock:   func(m *MockLimitRepository) {},
			expectError: true,
			errorIs:     constant.ErrLimitNilInput,
		},
		{
			name:    "Failure - nil UUID",
			limitID: uuid.Nil,
			input: &UpdateLimitInput{
				Name: testutil.StringPtr("New Name"),
			},
			setupMock:   func(m *MockLimitRepository) {},
			expectError: true,
			errorIs:     constant.ErrLimitInvalidID,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)

			mockRepo := NewMockLimitRepository(ctrl)
			auditWriter := NewMockAuditWriter(ctrl)
			tc.setupMock(mockRepo)

			// Setup audit expectations based on success/error and changes
			if tc.expectError {
				// No audit event expected - operation failed
				auditWriter.EXPECT().
					RecordLimitEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Times(0)
			} else if tc.name == "Success - no changes (all nil)" {
				// No audit event expected - no changes to persist
				auditWriter.EXPECT().
					RecordLimitEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Times(0)
			} else {
				// Audit event should be called exactly once with specific parameters
				auditWriter.EXPECT().
					RecordLimitEvent(
						gomock.Any(),                 // ctx
						model.AuditEventLimitUpdated, // eventType
						model.AuditActionUpdate,      // action
						tc.limitID,                   // limitID
						gomock.Any(),                 // beforeState
						gomock.Any(),                 // afterState
						"Limit updated via API",      // description
						gomock.Any(),                 // clientIP
					).
					Times(1).
					Return(nil)
			}

			cmd := NewUpdateLimitCommand(mockRepo, auditWriter)
			result, err := cmd.Execute(context.Background(), tc.limitID, tc.input)

			if tc.expectError {
				require.Error(t, err)
				if tc.errorIs != nil {
					assert.ErrorIs(t, err, tc.errorIs)
				}
				assert.Nil(t, result)
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
				if tc.validate != nil {
					tc.validate(t, result)
				}
			}
		})
	}
}
