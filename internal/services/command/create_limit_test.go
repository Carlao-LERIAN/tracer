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

func TestNewCreateLimitCommand(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockRepo := NewMockLimitRepository(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)
	// No audit expected - constructor only
	cmd, err := NewCreateLimitCommand(mockRepo, auditWriter)

	require.NoError(t, err)
	assert.NotNil(t, cmd)
}

func TestNewCreateLimitCommand_NilRepository(t *testing.T) {
	cmd, err := NewCreateLimitCommand(nil, nil)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNilLimitRepository)
	assert.Nil(t, cmd)
}

func TestCreateLimitCommand_Execute(t *testing.T) {
	validScope := model.Scope{
		AccountID: testutil.UUIDPtr(uuid.New()),
	}

	tests := []struct {
		name        string
		input       *CreateLimitInput
		setupMock   func(*MockLimitRepository)
		setupAudit  func(*MockAuditWriter)
		expectError bool
		errorIs     error
		validate    func(*testing.T, *model.Limit)
	}{
		{
			name: "Success - create daily limit",
			input: &CreateLimitInput{
				Name:        "Daily Card Limit",
				Description: testutil.StringPtr("Daily spending limit"),
				LimitType:   model.LimitTypeDaily,
				MaxAmount:   100000,
				Currency:    "USD",
				Scopes:      []model.Scope{validScope},
			},
			setupMock: func(m *MockLimitRepository) {
				m.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
			},
			expectError: false,
			validate: func(t *testing.T, limit *model.Limit) {
				assert.Equal(t, "Daily Card Limit", limit.Name)
				assert.Equal(t, model.LimitTypeDaily, limit.LimitType)
				assert.Equal(t, int64(100000), limit.MaxAmount)
				assert.Equal(t, "USD", limit.Currency)
				assert.Equal(t, model.LimitStatusDraft, limit.Status)
				assert.NotNil(t, limit.ResetAt)
			},
		},
		{
			name: "Success - create monthly limit",
			input: &CreateLimitInput{
				Name:      "Monthly Transfer Limit",
				LimitType: model.LimitTypeMonthly,
				MaxAmount: 500000,
				Currency:  "BRL",
				Scopes:    []model.Scope{validScope},
			},
			setupMock: func(m *MockLimitRepository) {
				m.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
			},
			expectError: false,
			validate: func(t *testing.T, limit *model.Limit) {
				assert.Equal(t, model.LimitTypeMonthly, limit.LimitType)
				assert.NotNil(t, limit.ResetAt)
			},
		},
		{
			name: "Success - create per-transaction limit",
			input: &CreateLimitInput{
				Name:      "Per Transaction Limit",
				LimitType: model.LimitTypePerTransaction,
				MaxAmount: 10000,
				Currency:  "EUR",
				Scopes:    []model.Scope{validScope},
			},
			setupMock: func(m *MockLimitRepository) {
				m.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
			},
			expectError: false,
			validate: func(t *testing.T, limit *model.Limit) {
				assert.Equal(t, model.LimitTypePerTransaction, limit.LimitType)
				assert.Nil(t, limit.ResetAt)
			},
		},
		{
			name: "Success - create limit with multiple scopes",
			input: &CreateLimitInput{
				Name:      "Multi-Scope Limit",
				LimitType: model.LimitTypeDaily,
				MaxAmount: 50000,
				Currency:  "USD",
				Scopes: []model.Scope{
					{AccountID: testutil.UUIDPtr(uuid.New())},
					{PortfolioID: testutil.UUIDPtr(uuid.New())},
				},
			},
			setupMock: func(m *MockLimitRepository) {
				m.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
			},
			expectError: false,
			validate: func(t *testing.T, limit *model.Limit) {
				assert.Len(t, limit.Scopes, 2)
			},
		},
		{
			name: "Success - normalizes lowercase currency to uppercase",
			input: &CreateLimitInput{
				Name:      "Lowercase Currency Test",
				LimitType: model.LimitTypeDaily,
				MaxAmount: 100000,
				Currency:  "usd",
				Scopes:    []model.Scope{validScope},
			},
			setupMock: func(m *MockLimitRepository) {
				m.EXPECT().Create(gomock.Any(), gomock.Cond(func(x any) bool {
					limit, ok := x.(*model.Limit)
					return ok && limit.Currency == "USD"
				})).Return(nil)
			},
			expectError: false,
			validate: func(t *testing.T, limit *model.Limit) {
				assert.Equal(t, "USD", limit.Currency)
			},
		},
		{
			name: "Success - normalizes name by trimming whitespace",
			input: &CreateLimitInput{
				Name:      "  Whitespace Name  ",
				LimitType: model.LimitTypeDaily,
				MaxAmount: 100000,
				Currency:  "USD",
				Scopes:    []model.Scope{validScope},
			},
			setupMock: func(m *MockLimitRepository) {
				m.EXPECT().Create(gomock.Any(), gomock.Cond(func(x any) bool {
					limit, ok := x.(*model.Limit)
					return ok && limit.Name == "Whitespace Name"
				})).Return(nil)
			},
			expectError: false,
			validate: func(t *testing.T, limit *model.Limit) {
				assert.Equal(t, "Whitespace Name", limit.Name)
			},
		},
		{
			name: "Success - normalizes name and currency with surrounding whitespace",
			input: &CreateLimitInput{
				Name:      "  Foo  ",
				LimitType: model.LimitTypeDaily,
				MaxAmount: 100000,
				Currency:  " usd ",
				Scopes:    []model.Scope{validScope},
			},
			setupMock: func(m *MockLimitRepository) {
				m.EXPECT().Create(gomock.Any(), gomock.Cond(func(x any) bool {
					limit, ok := x.(*model.Limit)
					return ok && limit.Name == "Foo" && limit.Currency == "USD"
				})).Return(nil)
			},
			expectError: false,
			validate: func(t *testing.T, limit *model.Limit) {
				assert.Equal(t, "Foo", limit.Name)
				assert.Equal(t, "USD", limit.Currency)
			},
		},
		{
			name: "Failure - whitespace-only name",
			input: &CreateLimitInput{
				Name:      "   ",
				LimitType: model.LimitTypeDaily,
				MaxAmount: 100000,
				Currency:  " usd ",
				Scopes:    []model.Scope{validScope},
			},
			setupMock:   func(m *MockLimitRepository) {},
			expectError: true,
			errorIs:     constant.ErrLimitNameRequired,
		},
		{
			name: "Failure - empty name",
			input: &CreateLimitInput{
				Name:      "",
				LimitType: model.LimitTypeDaily,
				MaxAmount: 100000,
				Currency:  "USD",
				Scopes:    []model.Scope{validScope},
			},
			setupMock:   func(m *MockLimitRepository) {},
			expectError: true,
			errorIs:     constant.ErrLimitNameRequired,
		},
		{
			name: "Failure - invalid limit type",
			input: &CreateLimitInput{
				Name:      "Test Limit",
				LimitType: model.LimitType("INVALID"),
				MaxAmount: 100000,
				Currency:  "USD",
				Scopes:    []model.Scope{validScope},
			},
			setupMock:   func(m *MockLimitRepository) {},
			expectError: true,
			errorIs:     constant.ErrLimitInvalidType,
		},
		{
			name: "Failure - zero maxAmount",
			input: &CreateLimitInput{
				Name:      "Test Limit",
				LimitType: model.LimitTypeDaily,
				MaxAmount: 0,
				Currency:  "USD",
				Scopes:    []model.Scope{validScope},
			},
			setupMock:   func(m *MockLimitRepository) {},
			expectError: true,
			errorIs:     constant.ErrLimitInvalidMaxAmount,
		},
		{
			name: "Failure - negative maxAmount",
			input: &CreateLimitInput{
				Name:      "Test Limit",
				LimitType: model.LimitTypeDaily,
				MaxAmount: -100,
				Currency:  "USD",
				Scopes:    []model.Scope{validScope},
			},
			setupMock:   func(m *MockLimitRepository) {},
			expectError: true,
			errorIs:     constant.ErrLimitInvalidMaxAmount,
		},
		{
			name: "Failure - invalid currency (contains number)",
			input: &CreateLimitInput{
				Name:      "Test Limit",
				LimitType: model.LimitTypeDaily,
				MaxAmount: 100000,
				Currency:  "US1",
				Scopes:    []model.Scope{validScope},
			},
			setupMock:   func(m *MockLimitRepository) {},
			expectError: true,
			errorIs:     constant.ErrLimitInvalidCurrency,
		},
		{
			name: "Failure - currency too short",
			input: &CreateLimitInput{
				Name:      "Test Limit",
				LimitType: model.LimitTypeDaily,
				MaxAmount: 100000,
				Currency:  "US",
				Scopes:    []model.Scope{validScope},
			},
			setupMock:   func(m *MockLimitRepository) {},
			expectError: true,
			errorIs:     constant.ErrLimitInvalidCurrency,
		},
		{
			name: "Failure - empty scopes",
			input: &CreateLimitInput{
				Name:      "Test Limit",
				LimitType: model.LimitTypeDaily,
				MaxAmount: 100000,
				Currency:  "USD",
				Scopes:    []model.Scope{},
			},
			setupMock:   func(m *MockLimitRepository) {},
			expectError: true,
			errorIs:     constant.ErrLimitInvalidScope,
		},
		{
			name: "Failure - nil scopes",
			input: &CreateLimitInput{
				Name:      "Test Limit",
				LimitType: model.LimitTypeDaily,
				MaxAmount: 100000,
				Currency:  "USD",
				Scopes:    nil,
			},
			setupMock:   func(m *MockLimitRepository) {},
			expectError: true,
			errorIs:     constant.ErrLimitInvalidScope,
		},
		{
			name: "Failure - empty scope in array",
			input: &CreateLimitInput{
				Name:      "Test Limit",
				LimitType: model.LimitTypeDaily,
				MaxAmount: 100000,
				Currency:  "USD",
				Scopes:    []model.Scope{{}},
			},
			setupMock:   func(m *MockLimitRepository) {},
			expectError: true,
			errorIs:     constant.ErrLimitInvalidScope,
		},
		{
			name: "Failure - repository error",
			input: &CreateLimitInput{
				Name:      "Test Limit",
				LimitType: model.LimitTypeDaily,
				MaxAmount: 100000,
				Currency:  "USD",
				Scopes:    []model.Scope{validScope},
			},
			setupMock: func(m *MockLimitRepository) {
				m.EXPECT().Create(gomock.Any(), gomock.Any()).Return(errors.New("database error"))
			},
			expectError: true,
			// No errorIs - repository errors are wrapped, not sentinel errors
		},
		{
			name: "Success - create succeeds even when audit write fails",
			input: &CreateLimitInput{
				Name:      "Audit Failure Test",
				LimitType: model.LimitTypeDaily,
				MaxAmount: 100000,
				Currency:  "USD",
				Scopes:    []model.Scope{validScope},
			},
			setupMock: func(m *MockLimitRepository) {
				m.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
			},
			setupAudit: func(m *MockAuditWriter) {
				m.EXPECT().
					RecordLimitEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(errors.New("audit write failed")).
					Times(1)
			},
			expectError: false,
			validate: func(t *testing.T, limit *model.Limit) {
				assert.NotEqual(t, uuid.Nil, limit.ID)
				assert.Equal(t, "Audit Failure Test", limit.Name)
				assert.Equal(t, model.LimitStatusDraft, limit.Status)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)

			mockRepo := NewMockLimitRepository(ctrl)
			auditWriter := NewMockAuditWriter(ctrl)
			tc.setupMock(mockRepo)

			// Setup audit expectations - use custom setupAudit if provided, otherwise auto-configure
			if tc.setupAudit != nil {
				// Custom audit setup (e.g., for testing audit failure scenarios)
				tc.setupAudit(auditWriter)
			} else if tc.expectError {
				// No audit event expected - operation failed
				auditWriter.EXPECT().
					RecordLimitEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Times(0)
			} else {
				// Default: audit event should be called exactly once with specific parameters
				auditWriter.EXPECT().
					RecordLimitEvent(
						gomock.Any(),                 // ctx
						model.AuditEventLimitCreated, // eventType
						model.AuditActionCreate,      // action
						gomock.Any(),                 // limitID (generated)
						nil,                          // beforeState (no before for create)
						gomock.Any(),                 // afterState
						"Limit created via API",      // description
						gomock.Any(),                 // clientIP
					).
					Times(1).
					Return(nil)
			}

			cmd, cmdErr := NewCreateLimitCommand(mockRepo, auditWriter)
			require.NoError(t, cmdErr)
			result, err := cmd.Execute(context.Background(), tc.input)

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

func TestCreateLimitCommand_Execute_NilInput(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockRepo := NewMockLimitRepository(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)

	// No audit event expected - nil input validation failed
	auditWriter.EXPECT().
		RecordLimitEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Times(0)

	cmd, cmdErr := NewCreateLimitCommand(mockRepo, auditWriter)
	require.NoError(t, cmdErr)

	result, err := cmd.Execute(context.Background(), nil)

	require.Error(t, err)
	assert.ErrorIs(t, err, constant.ErrLimitNilInput)
	assert.Nil(t, result)
}

func TestCreateLimitCommand_Execute_ContextCancellation(t *testing.T) {
	ctrl := gomock.NewController(t)

	validScope := model.Scope{
		AccountID: testutil.UUIDPtr(uuid.New()),
	}

	input := &CreateLimitInput{
		Name:      "Test Limit",
		LimitType: model.LimitTypeDaily,
		MaxAmount: 100000,
		Currency:  "USD",
		Scopes:    []model.Scope{validScope},
	}

	mockRepo := NewMockLimitRepository(ctrl)
	auditWriter := NewMockAuditWriter(ctrl)
	// Repository should NOT be called when context is already canceled
	mockRepo.EXPECT().Create(gomock.Any(), gomock.Any()).Times(0)

	// No audit event expected - context cancelled
	auditWriter.EXPECT().
		RecordLimitEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Times(0)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately before Execute

	cmd, cmdErr := NewCreateLimitCommand(mockRepo, auditWriter)
	require.NoError(t, cmdErr)
	result, err := cmd.Execute(ctx, input)

	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, result)
}
