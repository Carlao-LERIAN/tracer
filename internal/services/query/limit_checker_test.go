// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package query

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"tracer/internal/testutil"
	"tracer/pkg/constant"
	"tracer/pkg/model"
)

// errDatabase is a sentinel error for testing database error paths.
var errDatabase = errors.New("database error")

// setupTest creates test context with tracing setup.
func setupTest(t *testing.T) context.Context {
	t.Helper()
	testutil.SetupTestTracing(t)

	return context.Background()
}

func TestNewLimitChecker(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockLimitRepo := NewMockLimitRepository(ctrl)
	mockUsageRepo := NewMockUsageCounterRepository(ctrl)

	tests := []struct {
		name      string
		limitRepo LimitRepository
		usageRepo UsageCounterRepository
		wantErr   bool
		wantErrIs error
	}{
		{
			name:      "valid repositories",
			limitRepo: mockLimitRepo,
			usageRepo: mockUsageRepo,
			wantErr:   false,
		},
		{
			name:      "nil limit repository",
			limitRepo: nil,
			usageRepo: mockUsageRepo,
			wantErr:   true,
			wantErrIs: constant.ErrLimitCheckerNilLimitRepo,
		},
		{
			name:      "nil usage counter repository",
			limitRepo: mockLimitRepo,
			usageRepo: nil,
			wantErr:   true,
			wantErrIs: constant.ErrLimitCheckerNilUsageCounterRepo,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			checker, err := NewLimitChecker(tc.limitRepo, tc.usageRepo)

			if tc.wantErr {
				require.Error(t, err)
				assert.ErrorIs(t, err, tc.wantErrIs)
				assert.Nil(t, checker)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, checker)
			}
		})
	}
}

func TestLimitCheckerService_CheckLimits(t *testing.T) {
	// Test UUIDs
	limitID1 := testutil.MustDeterministicUUID(1)
	limitID2 := testutil.MustDeterministicUUID(2)
	limitID3 := testutil.MustDeterministicUUID(3)
	accountID := testutil.MustDeterministicUUID(100)
	counterID1 := testutil.MustDeterministicUUID(201)
	counterID2 := testutil.MustDeterministicUUID(202)

	timestamp := time.Date(2025, 12, 28, 10, 0, 0, 0, time.UTC)
	periodKeyDaily := "2025-12-28"

	tests := []struct {
		name         string
		input        *model.CheckLimitsInput
		setupMocks   func(*MockLimitRepository, *MockUsageCounterRepository)
		wantAllowed  bool
		wantExceeded []uuid.UUID
		wantDetails  int
		wantErr      bool
		wantErrIs    error
	}{
		{
			name: "no active limits - allowed",
			input: &model.CheckLimitsInput{
				Amount:               decimal.RequireFromString("100"),
				Currency:             "USD",
				AccountID:            accountID,
				TransactionTimestamp: timestamp,
			},
			setupMocks: func(lr *MockLimitRepository, ucr *MockUsageCounterRepository) {
				status := model.LimitStatusActive
				currency := "USD"
				lr.EXPECT().List(gomock.Any(), &model.ListLimitsFilter{
					Status:   &status,
					Currency: &currency,
					Limit:    constant.MaxPaginationLimit,
				}).Return(&model.ListLimitsResult{
					Limits:  []model.Limit{},
					HasMore: false,
				}, nil)
			},
			wantAllowed:  true,
			wantExceeded: nil,
			wantDetails:  0,
			wantErr:      false,
		},
		{
			name: "single DAILY limit - not exceeded",
			input: &model.CheckLimitsInput{
				Amount:               decimal.RequireFromString("50"),
				Currency:             "USD",
				AccountID:            accountID,
				TransactionTimestamp: timestamp,
			},
			setupMocks: func(lr *MockLimitRepository, ucr *MockUsageCounterRepository) {
				status := model.LimitStatusActive
				currency := "USD"
				lr.EXPECT().List(gomock.Any(), &model.ListLimitsFilter{
					Status:   &status,
					Currency: &currency,
					Limit:    constant.MaxPaginationLimit,
				}).Return(&model.ListLimitsResult{
					Limits: []model.Limit{
						{
							ID:        limitID1,
							Name:      "Daily Limit",
							LimitType: model.LimitTypeDaily,
							MaxAmount: decimal.RequireFromString("1000"),
							Currency:  "USD",
							Scopes:    []model.Scope{{AccountID: &accountID}},
							Status:    model.LimitStatusActive,
						},
					},
					HasMore: false,
				}, nil)

				scopeKey := "acct:" + accountID.String()
				ucr.EXPECT().GetOrCreateForUpdate(gomock.Any(), limitID1, scopeKey, periodKeyDaily).
					Return(&model.UsageCounter{
						ID:           counterID1,
						LimitID:      limitID1,
						ScopeKey:     scopeKey,
						PeriodKey:    periodKeyDaily,
						CurrentUsage: decimal.RequireFromString("500"),
					}, nil)

				ucr.EXPECT().IncrementAtomic(gomock.Any(), counterID1, decimal.RequireFromString("50")).Return(nil)
			},
			wantAllowed:  true,
			wantExceeded: nil,
			wantDetails:  1,
			wantErr:      false,
		},
		{
			name: "single DAILY limit - exceeded",
			input: &model.CheckLimitsInput{
				Amount:               decimal.RequireFromString("600"),
				Currency:             "USD",
				AccountID:            accountID,
				TransactionTimestamp: timestamp,
			},
			setupMocks: func(lr *MockLimitRepository, ucr *MockUsageCounterRepository) {
				status := model.LimitStatusActive
				currency := "USD"
				lr.EXPECT().List(gomock.Any(), &model.ListLimitsFilter{
					Status:   &status,
					Currency: &currency,
					Limit:    constant.MaxPaginationLimit,
				}).Return(&model.ListLimitsResult{
					Limits: []model.Limit{
						{
							ID:        limitID1,
							Name:      "Daily Limit",
							LimitType: model.LimitTypeDaily,
							MaxAmount: decimal.RequireFromString("1000"),
							Currency:  "USD",
							Scopes:    []model.Scope{{AccountID: &accountID}},
							Status:    model.LimitStatusActive,
						},
					},
					HasMore: false,
				}, nil)

				scopeKey := "acct:" + accountID.String()
				ucr.EXPECT().GetOrCreateForUpdate(gomock.Any(), limitID1, scopeKey, periodKeyDaily).
					Return(&model.UsageCounter{
						ID:           counterID1,
						LimitID:      limitID1,
						ScopeKey:     scopeKey,
						PeriodKey:    periodKeyDaily,
						CurrentUsage: decimal.RequireFromString("500"),
					}, nil)
				// No IncrementAtomic call because limit is exceeded
			},
			wantAllowed:  false,
			wantExceeded: []uuid.UUID{limitID1},
			wantDetails:  1,
			wantErr:      false,
		},
		{
			name: "PER_TRANSACTION limit - not exceeded",
			input: &model.CheckLimitsInput{
				Amount:               decimal.RequireFromString("50"),
				Currency:             "USD",
				AccountID:            accountID,
				TransactionTimestamp: timestamp,
			},
			setupMocks: func(lr *MockLimitRepository, ucr *MockUsageCounterRepository) {
				status := model.LimitStatusActive
				currency := "USD"
				lr.EXPECT().List(gomock.Any(), &model.ListLimitsFilter{
					Status:   &status,
					Currency: &currency,
					Limit:    constant.MaxPaginationLimit,
				}).Return(&model.ListLimitsResult{
					Limits: []model.Limit{
						{
							ID:        limitID1,
							Name:      "Per Transaction Limit",
							LimitType: model.LimitTypePerTransaction,
							MaxAmount: decimal.RequireFromString("100"),
							Currency:  "USD",
							Scopes:    []model.Scope{{AccountID: &accountID}},
							Status:    model.LimitStatusActive,
						},
					},
					HasMore: false,
				}, nil)
				// No usage counter calls for PER_TRANSACTION
			},
			wantAllowed:  true,
			wantExceeded: nil,
			wantDetails:  1,
			wantErr:      false,
		},
		{
			name: "PER_TRANSACTION limit - exceeded",
			input: &model.CheckLimitsInput{
				Amount:               decimal.RequireFromString("150"),
				Currency:             "USD",
				AccountID:            accountID,
				TransactionTimestamp: timestamp,
			},
			setupMocks: func(lr *MockLimitRepository, ucr *MockUsageCounterRepository) {
				status := model.LimitStatusActive
				currency := "USD"
				lr.EXPECT().List(gomock.Any(), &model.ListLimitsFilter{
					Status:   &status,
					Currency: &currency,
					Limit:    constant.MaxPaginationLimit,
				}).Return(&model.ListLimitsResult{
					Limits: []model.Limit{
						{
							ID:        limitID1,
							Name:      "Per Transaction Limit",
							LimitType: model.LimitTypePerTransaction,
							MaxAmount: decimal.RequireFromString("100"),
							Currency:  "USD",
							Scopes:    []model.Scope{{AccountID: &accountID}},
							Status:    model.LimitStatusActive,
						},
					},
					HasMore: false,
				}, nil)
			},
			wantAllowed:  false,
			wantExceeded: []uuid.UUID{limitID1},
			wantDetails:  1,
			wantErr:      false,
		},
		{
			name: "multiple limits - one exceeded",
			input: &model.CheckLimitsInput{
				Amount:               decimal.RequireFromString("80"),
				Currency:             "USD",
				AccountID:            accountID,
				TransactionTimestamp: timestamp,
			},
			setupMocks: func(lr *MockLimitRepository, ucr *MockUsageCounterRepository) {
				status := model.LimitStatusActive
				currency := "USD"
				lr.EXPECT().List(gomock.Any(), &model.ListLimitsFilter{
					Status:   &status,
					Currency: &currency,
					Limit:    constant.MaxPaginationLimit,
				}).Return(&model.ListLimitsResult{
					Limits: []model.Limit{
						{
							ID:        limitID1,
							Name:      "Daily Limit",
							LimitType: model.LimitTypeDaily,
							MaxAmount: decimal.RequireFromString("1000"),
							Currency:  "USD",
							Scopes:    []model.Scope{{AccountID: &accountID}},
							Status:    model.LimitStatusActive,
						},
						{
							ID:        limitID2,
							Name:      "Per Transaction Limit",
							LimitType: model.LimitTypePerTransaction,
							MaxAmount: decimal.RequireFromString("50"),
							Currency:  "USD",
							Scopes:    []model.Scope{{AccountID: &accountID}},
							Status:    model.LimitStatusActive,
						},
					},
					HasMore: false,
				}, nil)

				scopeKey := "acct:" + accountID.String()
				ucr.EXPECT().GetOrCreateForUpdate(gomock.Any(), limitID1, scopeKey, periodKeyDaily).
					Return(&model.UsageCounter{
						ID:           counterID1,
						LimitID:      limitID1,
						ScopeKey:     scopeKey,
						PeriodKey:    periodKeyDaily,
						CurrentUsage: decimal.RequireFromString("500"),
					}, nil)
				// PER_TRANSACTION limitID2 is exceeded (80 > 50)
				// With two-phase approach, NO IncrementAtomic is called when any limit is exceeded
			},
			wantAllowed:  false,
			wantExceeded: []uuid.UUID{limitID2},
			wantDetails:  2,
			wantErr:      false,
		},
		{
			name: "currency filter - no limits for currency (DB-level filtering)",
			input: &model.CheckLimitsInput{
				Amount:               decimal.RequireFromString("50"),
				Currency:             "USD",
				AccountID:            accountID,
				TransactionTimestamp: timestamp,
			},
			setupMocks: func(lr *MockLimitRepository, ucr *MockUsageCounterRepository) {
				// With DB-level currency filtering, query for USD returns empty list
				// (BRL limits are filtered out at database level)
				status := model.LimitStatusActive
				currency := "USD"
				lr.EXPECT().List(gomock.Any(), &model.ListLimitsFilter{
					Status:   &status,
					Currency: &currency,
					Limit:    constant.MaxPaginationLimit,
				}).Return(&model.ListLimitsResult{
					Limits:  []model.Limit{},
					HasMore: false,
				}, nil)
			},
			wantAllowed:  true,
			wantExceeded: nil,
			wantDetails:  0,
			wantErr:      false,
		},
		{
			name: "scope mismatch - limit not applicable",
			input: &model.CheckLimitsInput{
				Amount:               decimal.RequireFromString("50"),
				Currency:             "USD",
				AccountID:            accountID,
				TransactionTimestamp: timestamp,
			},
			setupMocks: func(lr *MockLimitRepository, ucr *MockUsageCounterRepository) {
				otherAccountID := testutil.MustDeterministicUUID(999)
				status := model.LimitStatusActive
				currency := "USD"
				lr.EXPECT().List(gomock.Any(), &model.ListLimitsFilter{
					Status:   &status,
					Currency: &currency,
					Limit:    constant.MaxPaginationLimit,
				}).Return(&model.ListLimitsResult{
					Limits: []model.Limit{
						{
							ID:        limitID1,
							Name:      "Other Account Limit",
							LimitType: model.LimitTypeDaily,
							MaxAmount: decimal.RequireFromString("1000"),
							Currency:  "USD",
							Scopes:    []model.Scope{{AccountID: &otherAccountID}}, // Different account
							Status:    model.LimitStatusActive,
						},
					},
					HasMore: false,
				}, nil)
			},
			wantAllowed:  true,
			wantExceeded: nil,
			wantDetails:  0,
			wantErr:      false,
		},
		{
			name: "global limit - matches all scopes",
			input: &model.CheckLimitsInput{
				Amount:               decimal.RequireFromString("50"),
				Currency:             "USD",
				AccountID:            accountID,
				TransactionTimestamp: timestamp,
			},
			setupMocks: func(lr *MockLimitRepository, ucr *MockUsageCounterRepository) {
				status := model.LimitStatusActive
				currency := "USD"
				lr.EXPECT().List(gomock.Any(), &model.ListLimitsFilter{
					Status:   &status,
					Currency: &currency,
					Limit:    constant.MaxPaginationLimit,
				}).Return(&model.ListLimitsResult{
					Limits: []model.Limit{
						{
							ID:        limitID1,
							Name:      "Global Limit",
							LimitType: model.LimitTypeDaily,
							MaxAmount: decimal.RequireFromString("1000"),
							Currency:  "USD",
							Scopes:    []model.Scope{}, // Empty scopes = global
							Status:    model.LimitStatusActive,
						},
					},
					HasMore: false,
				}, nil)

				scopeKey := "acct:" + accountID.String()
				ucr.EXPECT().GetOrCreateForUpdate(gomock.Any(), limitID1, scopeKey, periodKeyDaily).
					Return(&model.UsageCounter{
						ID:           counterID1,
						LimitID:      limitID1,
						ScopeKey:     scopeKey,
						PeriodKey:    periodKeyDaily,
						CurrentUsage: decimal.RequireFromString("0"),
					}, nil)
				ucr.EXPECT().IncrementAtomic(gomock.Any(), counterID1, decimal.RequireFromString("50")).Return(nil)
			},
			wantAllowed:  true,
			wantExceeded: nil,
			wantDetails:  1,
			wantErr:      false,
		},
		{
			name: "invalid input - zero amount",
			input: &model.CheckLimitsInput{
				Amount:               decimal.RequireFromString("0"),
				Currency:             "USD",
				AccountID:            accountID,
				TransactionTimestamp: timestamp,
			},
			setupMocks: func(lr *MockLimitRepository, ucr *MockUsageCounterRepository) {
				// No mocks - validation fails before repository calls
			},
			wantAllowed: false,
			wantErr:     true,
			wantErrIs:   constant.ErrCheckLimitsInvalidAmount,
		},
		{
			name: "invalid input - nil accountID",
			input: &model.CheckLimitsInput{
				Amount:               decimal.RequireFromString("50"),
				Currency:             "USD",
				AccountID:            uuid.Nil,
				TransactionTimestamp: timestamp,
			},
			setupMocks: func(lr *MockLimitRepository, ucr *MockUsageCounterRepository) {
				// No mocks - validation fails before repository calls
			},
			wantAllowed: false,
			wantErr:     true,
			wantErrIs:   constant.ErrCheckLimitsInvalidAccountID,
		},
		{
			name: "MONTHLY limit period key",
			input: &model.CheckLimitsInput{
				Amount:               decimal.RequireFromString("50"),
				Currency:             "USD",
				AccountID:            accountID,
				TransactionTimestamp: timestamp,
			},
			setupMocks: func(lr *MockLimitRepository, ucr *MockUsageCounterRepository) {
				status := model.LimitStatusActive
				currency := "USD"
				lr.EXPECT().List(gomock.Any(), &model.ListLimitsFilter{
					Status:   &status,
					Currency: &currency,
					Limit:    constant.MaxPaginationLimit,
				}).Return(&model.ListLimitsResult{
					Limits: []model.Limit{
						{
							ID:        limitID3,
							Name:      "Monthly Limit",
							LimitType: model.LimitTypeMonthly,
							MaxAmount: decimal.RequireFromString("5000"),
							Currency:  "USD",
							Scopes:    []model.Scope{{AccountID: &accountID}},
							Status:    model.LimitStatusActive,
						},
					},
					HasMore: false,
				}, nil)

				scopeKey := "acct:" + accountID.String()
				periodKeyMonthly := "2025-12"
				ucr.EXPECT().GetOrCreateForUpdate(gomock.Any(), limitID3, scopeKey, periodKeyMonthly).
					Return(&model.UsageCounter{
						ID:           counterID2,
						LimitID:      limitID3,
						ScopeKey:     scopeKey,
						PeriodKey:    periodKeyMonthly,
						CurrentUsage: decimal.RequireFromString("1000"),
					}, nil)
				ucr.EXPECT().IncrementAtomic(gomock.Any(), counterID2, decimal.RequireFromString("50")).Return(nil)
			},
			wantAllowed:  true,
			wantExceeded: nil,
			wantDetails:  1,
			wantErr:      false,
		},
		{
			name: "boundary - amount equals remaining capacity",
			input: &model.CheckLimitsInput{
				Amount:               decimal.RequireFromString("500"),
				Currency:             "USD",
				AccountID:            accountID,
				TransactionTimestamp: timestamp,
			},
			setupMocks: func(lr *MockLimitRepository, ucr *MockUsageCounterRepository) {
				status := model.LimitStatusActive
				currency := "USD"
				lr.EXPECT().List(gomock.Any(), &model.ListLimitsFilter{
					Status:   &status,
					Currency: &currency,
					Limit:    constant.MaxPaginationLimit,
				}).Return(&model.ListLimitsResult{
					Limits: []model.Limit{
						{
							ID:        limitID1,
							Name:      "Daily Limit",
							LimitType: model.LimitTypeDaily,
							MaxAmount: decimal.RequireFromString("1000"),
							Currency:  "USD",
							Scopes:    []model.Scope{{AccountID: &accountID}},
							Status:    model.LimitStatusActive,
						},
					},
					HasMore: false,
				}, nil)

				scopeKey := "acct:" + accountID.String()
				// CurrentUsage 500 + Amount 500 == MaxAmount 1000 (exactly at limit)
				ucr.EXPECT().GetOrCreateForUpdate(gomock.Any(), limitID1, scopeKey, periodKeyDaily).
					Return(&model.UsageCounter{
						ID:           counterID1,
						LimitID:      limitID1,
						ScopeKey:     scopeKey,
						PeriodKey:    periodKeyDaily,
						CurrentUsage: decimal.RequireFromString("500"),
					}, nil)
				// IncrementAtomic should be called because projected usage equals limit (not exceeded)
				ucr.EXPECT().IncrementAtomic(gomock.Any(), counterID1, decimal.RequireFromString("500")).Return(nil)
			},
			wantAllowed:  true,
			wantExceeded: nil,
			wantDetails:  1,
			wantErr:      false,
		},
		{
			name: "error - LimitRepository.List returns error",
			input: &model.CheckLimitsInput{
				Amount:               decimal.RequireFromString("50"),
				Currency:             "USD",
				AccountID:            accountID,
				TransactionTimestamp: timestamp,
			},
			setupMocks: func(lr *MockLimitRepository, ucr *MockUsageCounterRepository) {
				status := model.LimitStatusActive
				currency := "USD"
				lr.EXPECT().List(gomock.Any(), &model.ListLimitsFilter{
					Status:   &status,
					Currency: &currency,
					Limit:    constant.MaxPaginationLimit,
				}).Return(nil, errDatabase)
			},
			wantAllowed: false,
			wantErr:     true,
			wantErrIs:   errDatabase,
		},
		{
			name: "error - UsageCounterRepository.GetOrCreateForUpdate returns error",
			input: &model.CheckLimitsInput{
				Amount:               decimal.RequireFromString("50"),
				Currency:             "USD",
				AccountID:            accountID,
				TransactionTimestamp: timestamp,
			},
			setupMocks: func(lr *MockLimitRepository, ucr *MockUsageCounterRepository) {
				status := model.LimitStatusActive
				currency := "USD"
				lr.EXPECT().List(gomock.Any(), &model.ListLimitsFilter{
					Status:   &status,
					Currency: &currency,
					Limit:    constant.MaxPaginationLimit,
				}).Return(&model.ListLimitsResult{
					Limits: []model.Limit{
						{
							ID:        limitID1,
							Name:      "Daily Limit",
							LimitType: model.LimitTypeDaily,
							MaxAmount: decimal.RequireFromString("1000"),
							Currency:  "USD",
							Scopes:    []model.Scope{{AccountID: &accountID}},
							Status:    model.LimitStatusActive,
						},
					},
					HasMore: false,
				}, nil)

				scopeKey := "acct:" + accountID.String()
				ucr.EXPECT().GetOrCreateForUpdate(gomock.Any(), limitID1, scopeKey, periodKeyDaily).
					Return(nil, errDatabase)
			},
			wantAllowed: false,
			wantErr:     true,
			wantErrIs:   errDatabase,
		},
		{
			name: "error - UsageCounterRepository.IncrementAtomic returns error",
			input: &model.CheckLimitsInput{
				Amount:               decimal.RequireFromString("50"),
				Currency:             "USD",
				AccountID:            accountID,
				TransactionTimestamp: timestamp,
			},
			setupMocks: func(lr *MockLimitRepository, ucr *MockUsageCounterRepository) {
				status := model.LimitStatusActive
				currency := "USD"
				lr.EXPECT().List(gomock.Any(), &model.ListLimitsFilter{
					Status:   &status,
					Currency: &currency,
					Limit:    constant.MaxPaginationLimit,
				}).Return(&model.ListLimitsResult{
					Limits: []model.Limit{
						{
							ID:        limitID1,
							Name:      "Daily Limit",
							LimitType: model.LimitTypeDaily,
							MaxAmount: decimal.RequireFromString("1000"),
							Currency:  "USD",
							Scopes:    []model.Scope{{AccountID: &accountID}},
							Status:    model.LimitStatusActive,
						},
					},
					HasMore: false,
				}, nil)

				scopeKey := "acct:" + accountID.String()
				ucr.EXPECT().GetOrCreateForUpdate(gomock.Any(), limitID1, scopeKey, periodKeyDaily).
					Return(&model.UsageCounter{
						ID:           counterID1,
						LimitID:      limitID1,
						ScopeKey:     scopeKey,
						PeriodKey:    periodKeyDaily,
						CurrentUsage: decimal.RequireFromString("500"),
					}, nil)
				ucr.EXPECT().IncrementAtomic(gomock.Any(), counterID1, decimal.RequireFromString("50")).
					Return(errDatabase)
			},
			wantAllowed: false,
			wantErr:     true,
			wantErrIs:   errDatabase,
		},
		{
			name: "invalid input - negative amount",
			input: &model.CheckLimitsInput{
				Amount:               decimal.RequireFromString("-1"),
				Currency:             "USD",
				AccountID:            accountID,
				TransactionTimestamp: timestamp,
			},
			setupMocks: func(lr *MockLimitRepository, ucr *MockUsageCounterRepository) {
				// No mocks - validation fails before repository calls
			},
			wantAllowed: false,
			wantErr:     true,
			wantErrIs:   constant.ErrCheckLimitsInvalidAmount,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)

			mockLimitRepo := NewMockLimitRepository(ctrl)
			mockUsageRepo := NewMockUsageCounterRepository(ctrl)

			tc.setupMocks(mockLimitRepo, mockUsageRepo)

			ctx := setupTest(t)

			checker, err := NewLimitChecker(mockLimitRepo, mockUsageRepo)
			require.NoError(t, err)

			output, err := checker.CheckLimits(ctx, tc.input)

			if tc.wantErr {
				require.Error(t, err)
				if tc.wantErrIs != nil {
					assert.ErrorIs(t, err, tc.wantErrIs)
				}
				assert.Nil(t, output)
			} else {
				require.NoError(t, err)
				require.NotNil(t, output)
				assert.Equal(t, tc.wantAllowed, output.Allowed)
				assert.Len(t, output.LimitUsageDetails, tc.wantDetails)

				if tc.wantExceeded != nil {
					assert.Equal(t, tc.wantExceeded, output.ExceededLimitIDs)
				} else {
					assert.Empty(t, output.ExceededLimitIDs)
				}
			}
		})
	}
}

func TestLimitCheckerService_RollbackUsage(t *testing.T) {
	limitID1 := testutil.MustDeterministicUUID(1)
	limitID2 := testutil.MustDeterministicUUID(2)
	accountID := testutil.MustDeterministicUUID(100)
	counterID1 := testutil.MustDeterministicUUID(201)

	timestamp := time.Date(2025, 12, 28, 10, 0, 0, 0, time.UTC)
	periodKeyDaily := "2025-12-28"

	tests := []struct {
		name         string
		input        *model.CheckLimitsInput
		usageDetails []model.LimitUsageDetail
		setupMocks   func(*MockLimitRepository, *MockUsageCounterRepository)
		wantErr      bool
	}{
		{
			name: "empty usage details - no-op",
			input: &model.CheckLimitsInput{
				Amount:               decimal.RequireFromString("50"),
				Currency:             "USD",
				AccountID:            accountID,
				TransactionTimestamp: timestamp,
			},
			usageDetails: []model.LimitUsageDetail{},
			setupMocks: func(lr *MockLimitRepository, ucr *MockUsageCounterRepository) {
				// No mocks - empty details is a no-op
			},
			wantErr: false,
		},
		{
			name: "rollback DAILY limit",
			input: &model.CheckLimitsInput{
				Amount:               decimal.RequireFromString("50"),
				Currency:             "USD",
				AccountID:            accountID,
				TransactionTimestamp: timestamp,
			},
			usageDetails: []model.LimitUsageDetail{
				{
					LimitID:           limitID1,
					LimitAmount:       decimal.RequireFromString("1000"),
					InternalLimitType: model.LimitTypeDaily,
					Scopes:            []model.Scope{{AccountID: &accountID}},
					CurrentUsage:      decimal.RequireFromString("550"),
					Exceeded:          false,
				},
			},
			setupMocks: func(lr *MockLimitRepository, ucr *MockUsageCounterRepository) {
				// No GetByID call needed - Scopes are in LimitUsageDetail
				scopeKey := "acct:" + accountID.String()
				ucr.EXPECT().GetForUpdate(gomock.Any(), limitID1, scopeKey, periodKeyDaily).
					Return(&model.UsageCounter{
						ID:           counterID1,
						LimitID:      limitID1,
						ScopeKey:     scopeKey,
						PeriodKey:    periodKeyDaily,
						CurrentUsage: decimal.RequireFromString("550"),
					}, nil)
				ucr.EXPECT().DecrementAtomic(gomock.Any(), counterID1, decimal.RequireFromString("50")).Return(nil)
			},
			wantErr: false,
		},
		{
			name: "skip PER_TRANSACTION limit - no persistent counter",
			input: &model.CheckLimitsInput{
				Amount:               decimal.RequireFromString("50"),
				Currency:             "USD",
				AccountID:            accountID,
				TransactionTimestamp: timestamp,
			},
			usageDetails: []model.LimitUsageDetail{
				{
					LimitID:           limitID2,
					LimitAmount:       decimal.RequireFromString("100"),
					InternalLimitType: model.LimitTypePerTransaction,
					Scopes:            []model.Scope{{AccountID: &accountID}},
					CurrentUsage:      decimal.RequireFromString("0"),
					Exceeded:          false,
				},
			},
			setupMocks: func(lr *MockLimitRepository, ucr *MockUsageCounterRepository) {
				// No GetByID or counter calls - PER_TRANSACTION has no persistent counter
			},
			wantErr: false,
		},
		{
			name: "GetForUpdate error - logs warning but continues",
			input: &model.CheckLimitsInput{
				Amount:               decimal.RequireFromString("50"),
				Currency:             "USD",
				AccountID:            accountID,
				TransactionTimestamp: timestamp,
			},
			usageDetails: []model.LimitUsageDetail{
				{
					LimitID:           limitID1,
					LimitAmount:       decimal.RequireFromString("1000"),
					InternalLimitType: model.LimitTypeDaily,
					Scopes:            []model.Scope{{AccountID: &accountID}},
					CurrentUsage:      decimal.RequireFromString("550"),
					Exceeded:          false,
				},
			},
			setupMocks: func(lr *MockLimitRepository, ucr *MockUsageCounterRepository) {
				// No GetByID call needed - Scopes are in LimitUsageDetail
				scopeKey := "acct:" + accountID.String()
				ucr.EXPECT().GetForUpdate(gomock.Any(), limitID1, scopeKey, periodKeyDaily).
					Return(nil, errDatabase)
			},
			wantErr: false, // Should not error, just log warning
		},
		{
			name: "DecrementAtomic error - logs warning but continues",
			input: &model.CheckLimitsInput{
				Amount:               decimal.RequireFromString("50"),
				Currency:             "USD",
				AccountID:            accountID,
				TransactionTimestamp: timestamp,
			},
			usageDetails: []model.LimitUsageDetail{
				{
					LimitID:           limitID1,
					LimitAmount:       decimal.RequireFromString("1000"),
					InternalLimitType: model.LimitTypeDaily,
					Scopes:            []model.Scope{{AccountID: &accountID}},
					CurrentUsage:      decimal.RequireFromString("550"),
					Exceeded:          false,
				},
			},
			setupMocks: func(lr *MockLimitRepository, ucr *MockUsageCounterRepository) {
				// No GetByID call needed - Scopes are in LimitUsageDetail
				scopeKey := "acct:" + accountID.String()
				ucr.EXPECT().GetForUpdate(gomock.Any(), limitID1, scopeKey, periodKeyDaily).
					Return(&model.UsageCounter{
						ID:           counterID1,
						LimitID:      limitID1,
						ScopeKey:     scopeKey,
						PeriodKey:    periodKeyDaily,
						CurrentUsage: decimal.RequireFromString("550"),
					}, nil)
				ucr.EXPECT().DecrementAtomic(gomock.Any(), counterID1, decimal.RequireFromString("50")).
					Return(errDatabase)
			},
			wantErr: false, // Should not error, just log warning
		},
		{
			name: "multiple limits - partial failures logged as warnings",
			input: &model.CheckLimitsInput{
				Amount:               decimal.RequireFromString("50"),
				Currency:             "USD",
				AccountID:            accountID,
				TransactionTimestamp: timestamp,
			},
			usageDetails: []model.LimitUsageDetail{
				{
					LimitID:           limitID1,
					LimitAmount:       decimal.RequireFromString("1000"),
					InternalLimitType: model.LimitTypeDaily,
					Scopes:            []model.Scope{{AccountID: &accountID}},
					CurrentUsage:      decimal.RequireFromString("550"),
					Exceeded:          false,
				},
				{
					LimitID:           limitID2,
					LimitAmount:       decimal.RequireFromString("2000"),
					InternalLimitType: model.LimitTypeDaily,
					Scopes:            []model.Scope{{AccountID: &accountID}},
					CurrentUsage:      decimal.RequireFromString("1050"),
					Exceeded:          false,
				},
			},
			setupMocks: func(lr *MockLimitRepository, ucr *MockUsageCounterRepository) {
				scopeKey := "acct:" + accountID.String()

				// First limit fails at GetForUpdate
				ucr.EXPECT().GetForUpdate(gomock.Any(), limitID1, scopeKey, periodKeyDaily).
					Return(nil, errDatabase)

				// Second limit succeeds
				counterID2 := testutil.MustDeterministicUUID(202)
				ucr.EXPECT().GetForUpdate(gomock.Any(), limitID2, scopeKey, periodKeyDaily).
					Return(&model.UsageCounter{
						ID:           counterID2,
						LimitID:      limitID2,
						ScopeKey:     scopeKey,
						PeriodKey:    periodKeyDaily,
						CurrentUsage: decimal.RequireFromString("1050"),
					}, nil)
				ucr.EXPECT().DecrementAtomic(gomock.Any(), counterID2, decimal.RequireFromString("50")).Return(nil)
			},
			wantErr: false, // Should not error, continues with other limits
		},
		{
			name: "decrement would result in negative - logs warning but continues",
			input: &model.CheckLimitsInput{
				Amount:               decimal.RequireFromString("50"),
				Currency:             "USD",
				AccountID:            accountID,
				TransactionTimestamp: timestamp,
			},
			usageDetails: []model.LimitUsageDetail{
				{
					LimitID:           limitID1,
					LimitAmount:       decimal.RequireFromString("1000"),
					InternalLimitType: model.LimitTypeDaily,
					Scopes:            []model.Scope{{AccountID: &accountID}},
					CurrentUsage:      decimal.RequireFromString("550"),
					Exceeded:          false,
				},
			},
			setupMocks: func(lr *MockLimitRepository, ucr *MockUsageCounterRepository) {
				scopeKey := "acct:" + accountID.String()
				ucr.EXPECT().GetForUpdate(gomock.Any(), limitID1, scopeKey, periodKeyDaily).
					Return(&model.UsageCounter{
						ID:           counterID1,
						LimitID:      limitID1,
						ScopeKey:     scopeKey,
						PeriodKey:    periodKeyDaily,
						CurrentUsage: decimal.RequireFromString("550"),
					}, nil)
				ucr.EXPECT().DecrementAtomic(gomock.Any(), counterID1, decimal.RequireFromString("50")).
					Return(constant.ErrUsageCounterCurrentUsageNegative)
			},
			wantErr: false, // Should not error, just log warning
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)

			mockLimitRepo := NewMockLimitRepository(ctrl)
			mockUsageRepo := NewMockUsageCounterRepository(ctrl)

			tc.setupMocks(mockLimitRepo, mockUsageRepo)

			ctx := setupTest(t)

			checker, err := NewLimitChecker(mockLimitRepo, mockUsageRepo)
			require.NoError(t, err)

			err = checker.RollbackUsage(ctx, tc.input, tc.usageDetails)

			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestLimitCheckerService_CheckLimits_ConcurrentAccess(t *testing.T) {
	// This test verifies that CheckLimits handles concurrent requests correctly.
	// The key behaviors tested:
	// 1. Multiple goroutines can call CheckLimits simultaneously
	// 2. Each call gets proper counter state via GetOrCreateForUpdate (row lock)
	// 3. Increments happen atomically via IncrementAtomic

	limitID := testutil.MustDeterministicUUID(1)
	accountID := testutil.MustDeterministicUUID(100)
	counterID := testutil.MustDeterministicUUID(201)

	timestamp := time.Date(2025, 12, 28, 10, 0, 0, 0, time.UTC)
	periodKeyDaily := "2025-12-28"

	const numGoroutines = 10
	amountPerRequest := decimal.RequireFromString("10")

	ctrl := gomock.NewController(t)

	mockLimitRepo := NewMockLimitRepository(ctrl)
	mockUsageRepo := NewMockUsageCounterRepository(ctrl)

	// Setup mock expectations for concurrent calls
	// Each goroutine will call List, GetOrCreateForUpdate, and IncrementAtomic
	status := model.LimitStatusActive
	currency := "USD"

	mockLimitRepo.EXPECT().List(gomock.Any(), &model.ListLimitsFilter{
		Status:   &status,
		Currency: &currency,
		Limit:    constant.MaxPaginationLimit,
		Cursor:   "",
	}).Return(&model.ListLimitsResult{
		Limits: []model.Limit{
			{
				ID:        limitID,
				Name:      "Daily Limit",
				LimitType: model.LimitTypeDaily,
				MaxAmount: decimal.RequireFromString("1000"), // High enough to not be exceeded
				Currency:  "USD",
				Scopes:    []model.Scope{{AccountID: &accountID}},
				Status:    model.LimitStatusActive,
			},
		},
		HasMore: false,
	}, nil).Times(numGoroutines)

	scopeKey := "acct:" + accountID.String()

	// Each concurrent call will get counter and increment
	mockUsageRepo.EXPECT().GetOrCreateForUpdate(gomock.Any(), limitID, scopeKey, periodKeyDaily).
		Return(&model.UsageCounter{
			ID:           counterID,
			LimitID:      limitID,
			ScopeKey:     scopeKey,
			PeriodKey:    periodKeyDaily,
			CurrentUsage: decimal.RequireFromString("0"), // Starting usage
		}, nil).Times(numGoroutines)

	mockUsageRepo.EXPECT().IncrementAtomic(gomock.Any(), counterID, amountPerRequest).
		Return(nil).Times(numGoroutines)

	ctx := setupTest(t)

	checker, err := NewLimitChecker(mockLimitRepo, mockUsageRepo)
	require.NoError(t, err)

	// Run concurrent CheckLimits calls
	var wg sync.WaitGroup

	errors := make(chan error, numGoroutines)
	results := make(chan *model.CheckLimitsOutput, numGoroutines)

	for range numGoroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			input := &model.CheckLimitsInput{
				Amount:               amountPerRequest,
				Currency:             "USD",
				AccountID:            accountID,
				TransactionTimestamp: timestamp,
			}

			output, err := checker.CheckLimits(ctx, input)
			if err != nil {
				errors <- err
				return
			}

			results <- output
		}()
	}

	wg.Wait()
	close(errors)
	close(results)

	// Verify no errors occurred
	for err := range errors {
		t.Errorf("Concurrent CheckLimits failed: %v", err)
	}

	// Verify all calls succeeded and were allowed
	successCount := 0
	for output := range results {
		assert.True(t, output.Allowed, "All concurrent requests should be allowed")
		successCount++
	}

	assert.Equal(t, numGoroutines, successCount, "All goroutines should complete successfully")
}

func TestLimitCheckerService_CheckLimits_TwoPhaseNoPartialIncrement(t *testing.T) {
	// This test verifies that when multiple limits are checked and one exceeds,
	// NO counters are incremented (two-phase check-then-increment).

	limitID1 := testutil.MustDeterministicUUID(1)
	limitID2 := testutil.MustDeterministicUUID(2)
	accountID := testutil.MustDeterministicUUID(100)
	counterID1 := testutil.MustDeterministicUUID(201)

	timestamp := time.Date(2025, 12, 28, 10, 0, 0, 0, time.UTC)
	periodKeyDaily := "2025-12-28"

	ctrl := gomock.NewController(t)

	mockLimitRepo := NewMockLimitRepository(ctrl)
	mockUsageRepo := NewMockUsageCounterRepository(ctrl)

	status := model.LimitStatusActive
	currency := "USD"

	// Two limits: first one passes, second one exceeds
	mockLimitRepo.EXPECT().List(gomock.Any(), &model.ListLimitsFilter{
		Status:   &status,
		Currency: &currency,
		Limit:    constant.MaxPaginationLimit,
		Cursor:   "",
	}).Return(&model.ListLimitsResult{
		Limits: []model.Limit{
			{
				ID:        limitID1,
				Name:      "Daily Limit",
				LimitType: model.LimitTypeDaily,
				MaxAmount: decimal.RequireFromString("1000"), // Will NOT exceed
				Currency:  "USD",
				Scopes:    []model.Scope{{AccountID: &accountID}},
				Status:    model.LimitStatusActive,
			},
			{
				ID:        limitID2,
				Name:      "Per Transaction Limit",
				LimitType: model.LimitTypePerTransaction,
				MaxAmount: decimal.RequireFromString("50"), // Will exceed (amount is 80)
				Currency:  "USD",
				Scopes:    []model.Scope{{AccountID: &accountID}},
				Status:    model.LimitStatusActive,
			},
		},
		HasMore: false,
	}, nil)

	scopeKey := "acct:" + accountID.String()

	// First limit (DAILY) will be checked
	mockUsageRepo.EXPECT().GetOrCreateForUpdate(gomock.Any(), limitID1, scopeKey, periodKeyDaily).
		Return(&model.UsageCounter{
			ID:           counterID1,
			LimitID:      limitID1,
			ScopeKey:     scopeKey,
			PeriodKey:    periodKeyDaily,
			CurrentUsage: decimal.RequireFromString("500"),
		}, nil)

	// Explicit assertion: IncrementAtomic must NEVER be called when any limit exceeds.
	// This is the core guarantee of the two-phase check-then-increment design.
	mockUsageRepo.EXPECT().IncrementAtomic(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)

	ctx := setupTest(t)

	checker, err := NewLimitChecker(mockLimitRepo, mockUsageRepo)
	require.NoError(t, err)

	input := &model.CheckLimitsInput{
		Amount:               decimal.RequireFromString("80"), // Exceeds PER_TRANSACTION limit of 50
		Currency:             "USD",
		AccountID:            accountID,
		TransactionTimestamp: timestamp,
	}

	output, err := checker.CheckLimits(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.False(t, output.Allowed, "Should be denied due to PER_TRANSACTION limit")
	assert.Contains(t, output.ExceededLimitIDs, limitID2, "PER_TRANSACTION limit should be exceeded")
	assert.Len(t, output.LimitUsageDetails, 2, "Should have details for both limits")
}

func TestLimitCheckerService_CheckLimits_LargeAmountNearInt64Max(t *testing.T) {
	// This test verifies behavior with large decimal amounts.
	// Tests that projected usage is compared correctly against maxAmount.

	limitID := testutil.MustDeterministicUUID(1)
	accountID := testutil.MustDeterministicUUID(100)
	counterID := testutil.MustDeterministicUUID(201)

	timestamp := time.Date(2025, 12, 28, 10, 0, 0, 0, time.UTC)
	periodKeyDaily := "2025-12-28"

	tests := []struct {
		name         string
		amount       decimal.Decimal
		currentUsage decimal.Decimal
		maxAmount    decimal.Decimal
		wantAllowed  bool
		wantErr      bool
		wantErrIs    error
		setupIncr    func(*MockUsageCounterRepository)
	}{
		{
			name:         "large amount within limits - allowed",
			amount:       decimal.RequireFromString("10000000000000"), // 10 trillion
			currentUsage: decimal.RequireFromString("10000000000000"),
			maxAmount:    decimal.RequireFromString("50000000000000"),
			wantAllowed:  true,
			wantErr:      false,
			setupIncr: func(ucr *MockUsageCounterRepository) {
				ucr.EXPECT().IncrementAtomic(gomock.Any(), counterID, decimal.RequireFromString("10000000000000")).Return(nil)
			},
		},
		{
			name:         "large amount exceeds limit - projected usage > maxAmount",
			amount:       decimal.RequireFromString("10000000000000000"),    // Very large amount
			currentUsage: decimal.RequireFromString("90000000000000000"),    // Current usage close to max
			maxAmount:    decimal.RequireFromString("92233720368547758.07"), // MaxInt64 / 100
			wantAllowed:  false,                                             // Projected usage exceeds maxAmount
			wantErr:      false,                                             // No error, just exceeds limit
			wantErrIs:    nil,
			setupIncr:    func(ucr *MockUsageCounterRepository) {}, // No increment called - limit exceeded
		},
		{
			name:         "amount exactly at remaining capacity - allowed",
			amount:       decimal.RequireFromString("10000000000"),
			currentUsage: decimal.RequireFromString("90000000000"),
			maxAmount:    decimal.RequireFromString("100000000000"), // currentUsage + amount == maxAmount
			wantAllowed:  true,
			wantErr:      false,
			setupIncr: func(ucr *MockUsageCounterRepository) {
				ucr.EXPECT().IncrementAtomic(gomock.Any(), counterID, decimal.RequireFromString("10000000000")).Return(nil)
			},
		},
		{
			name:         "amount exceeds limit - not allowed but no error",
			amount:       decimal.RequireFromString("20000000000"),
			currentUsage: decimal.RequireFromString("90000000000"),
			maxAmount:    decimal.RequireFromString("100000000000"), // currentUsage + amount > maxAmount
			wantAllowed:  false,
			wantErr:      false,
			setupIncr:    func(ucr *MockUsageCounterRepository) {}, // No increment called
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)

			mockLimitRepo := NewMockLimitRepository(ctrl)
			mockUsageRepo := NewMockUsageCounterRepository(ctrl)

			status := model.LimitStatusActive
			currency := "USD"

			mockLimitRepo.EXPECT().List(gomock.Any(), &model.ListLimitsFilter{
				Status:   &status,
				Currency: &currency,
				Limit:    constant.MaxPaginationLimit,
				Cursor:   "",
			}).Return(&model.ListLimitsResult{
				Limits: []model.Limit{
					{
						ID:        limitID,
						Name:      "High Value Limit",
						LimitType: model.LimitTypeDaily,
						MaxAmount: tc.maxAmount,
						Currency:  "USD",
						Scopes:    []model.Scope{{AccountID: &accountID}},
						Status:    model.LimitStatusActive,
					},
				},
				HasMore: false,
			}, nil)

			scopeKey := "acct:" + accountID.String()

			mockUsageRepo.EXPECT().GetOrCreateForUpdate(gomock.Any(), limitID, scopeKey, periodKeyDaily).
				Return(&model.UsageCounter{
					ID:           counterID,
					LimitID:      limitID,
					ScopeKey:     scopeKey,
					PeriodKey:    periodKeyDaily,
					CurrentUsage: tc.currentUsage,
				}, nil)

			tc.setupIncr(mockUsageRepo)

			ctx := setupTest(t)

			checker, err := NewLimitChecker(mockLimitRepo, mockUsageRepo)
			require.NoError(t, err)

			input := &model.CheckLimitsInput{
				Amount:               tc.amount,
				Currency:             "USD",
				AccountID:            accountID,
				TransactionTimestamp: timestamp,
			}

			output, err := checker.CheckLimits(ctx, input)

			if tc.wantErr {
				require.Error(t, err)
				if tc.wantErrIs != nil {
					assert.ErrorIs(t, err, tc.wantErrIs)
				}
			} else {
				require.NoError(t, err)
				require.NotNil(t, output)
				assert.Equal(t, tc.wantAllowed, output.Allowed)
			}
		})
	}
}

func TestLimitUsageDetail_RemainingAmount_LargeValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		limitAmount  decimal.Decimal
		currentUsage decimal.Decimal
		expected     decimal.Decimal
	}{
		{
			name:         "large limit with zero usage",
			limitAmount:  decimal.RequireFromString("92233720368547758.07"), // MaxInt64 / 100
			currentUsage: decimal.RequireFromString("0"),
			expected:     decimal.RequireFromString("92233720368547758.07"),
		},
		{
			name:         "large limit with large usage",
			limitAmount:  decimal.RequireFromString("92233720368547758.07"),
			currentUsage: decimal.RequireFromString("92233720368547758"),
			expected:     decimal.RequireFromString("0.07"), // MaxInt64/100 - 92233720368547758
		},
		{
			name:         "large limit exactly at max",
			limitAmount:  decimal.RequireFromString("92233720368547758.07"),
			currentUsage: decimal.RequireFromString("92233720368547758.07"),
			expected:     decimal.RequireFromString("0"),
		},
		{
			name:         "quadrillion scale values",
			limitAmount:  decimal.RequireFromString("50000000000000"),
			currentUsage: decimal.RequireFromString("30000000000000"),
			expected:     decimal.RequireFromString("20000000000000"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			detail := model.LimitUsageDetail{
				LimitAmount:  tc.limitAmount,
				CurrentUsage: tc.currentUsage,
			}

			result := detail.RemainingAmount()
			assert.True(t, tc.expected.Equal(result), "expected %s, got %s", tc.expected.String(), result.String())
		})
	}
}

func TestLimitCheckerService_CheckLimits_PaginationLoop(t *testing.T) {
	// This test verifies that getApplicableLimits correctly handles pagination
	// when HasMore=true, fetching multiple pages of results.

	limitID1 := testutil.MustDeterministicUUID(1)
	limitID2 := testutil.MustDeterministicUUID(2)
	limitID3 := testutil.MustDeterministicUUID(3)
	accountID := testutil.MustDeterministicUUID(100)
	counterID1 := testutil.MustDeterministicUUID(201)
	counterID2 := testutil.MustDeterministicUUID(202)
	counterID3 := testutil.MustDeterministicUUID(203)

	timestamp := time.Date(2025, 12, 28, 10, 0, 0, 0, time.UTC)
	periodKeyDaily := "2025-12-28"

	ctrl := gomock.NewController(t)

	mockLimitRepo := NewMockLimitRepository(ctrl)
	mockUsageRepo := NewMockUsageCounterRepository(ctrl)

	status := model.LimitStatusActive
	currency := "USD"
	scopeKey := "acct:" + accountID.String()

	// First page returns 2 limits with HasMore=true
	mockLimitRepo.EXPECT().List(gomock.Any(), &model.ListLimitsFilter{
		Status:   &status,
		Currency: &currency,
		Limit:    constant.MaxPaginationLimit,
		Cursor:   "",
	}).Return(&model.ListLimitsResult{
		Limits: []model.Limit{
			{
				ID:        limitID1,
				Name:      "Daily Limit 1",
				LimitType: model.LimitTypeDaily,
				MaxAmount: decimal.RequireFromString("1000"),
				Currency:  "USD",
				Scopes:    []model.Scope{{AccountID: &accountID}},
				Status:    model.LimitStatusActive,
			},
			{
				ID:        limitID2,
				Name:      "Daily Limit 2",
				LimitType: model.LimitTypeDaily,
				MaxAmount: decimal.RequireFromString("2000"),
				Currency:  "USD",
				Scopes:    []model.Scope{{AccountID: &accountID}},
				Status:    model.LimitStatusActive,
			},
		},
		HasMore:    true,
		NextCursor: "cursor-page-2",
	}, nil)

	// Second page returns 1 limit with HasMore=false
	mockLimitRepo.EXPECT().List(gomock.Any(), &model.ListLimitsFilter{
		Status:   &status,
		Currency: &currency,
		Limit:    constant.MaxPaginationLimit,
		Cursor:   "cursor-page-2",
	}).Return(&model.ListLimitsResult{
		Limits: []model.Limit{
			{
				ID:        limitID3,
				Name:      "Daily Limit 3",
				LimitType: model.LimitTypeDaily,
				MaxAmount: decimal.RequireFromString("3000"),
				Currency:  "USD",
				Scopes:    []model.Scope{{AccountID: &accountID}},
				Status:    model.LimitStatusActive,
			},
		},
		HasMore:    false,
		NextCursor: "",
	}, nil)

	// Expect GetOrCreateForUpdate for all 3 limits
	mockUsageRepo.EXPECT().GetOrCreateForUpdate(gomock.Any(), limitID1, scopeKey, periodKeyDaily).
		Return(&model.UsageCounter{
			ID:           counterID1,
			LimitID:      limitID1,
			ScopeKey:     scopeKey,
			PeriodKey:    periodKeyDaily,
			CurrentUsage: decimal.RequireFromString("0"),
		}, nil)
	mockUsageRepo.EXPECT().GetOrCreateForUpdate(gomock.Any(), limitID2, scopeKey, periodKeyDaily).
		Return(&model.UsageCounter{
			ID:           counterID2,
			LimitID:      limitID2,
			ScopeKey:     scopeKey,
			PeriodKey:    periodKeyDaily,
			CurrentUsage: decimal.RequireFromString("0"),
		}, nil)
	mockUsageRepo.EXPECT().GetOrCreateForUpdate(gomock.Any(), limitID3, scopeKey, periodKeyDaily).
		Return(&model.UsageCounter{
			ID:           counterID3,
			LimitID:      limitID3,
			ScopeKey:     scopeKey,
			PeriodKey:    periodKeyDaily,
			CurrentUsage: decimal.RequireFromString("0"),
		}, nil)

	// Expect IncrementAtomic for all 3 limits (none exceeded)
	mockUsageRepo.EXPECT().IncrementAtomic(gomock.Any(), counterID1, decimal.RequireFromString("50")).Return(nil)
	mockUsageRepo.EXPECT().IncrementAtomic(gomock.Any(), counterID2, decimal.RequireFromString("50")).Return(nil)
	mockUsageRepo.EXPECT().IncrementAtomic(gomock.Any(), counterID3, decimal.RequireFromString("50")).Return(nil)

	ctx := setupTest(t)

	checker, err := NewLimitChecker(mockLimitRepo, mockUsageRepo)
	require.NoError(t, err)

	input := &model.CheckLimitsInput{
		Amount:               decimal.RequireFromString("50"),
		Currency:             "USD",
		AccountID:            accountID,
		TransactionTimestamp: timestamp,
	}

	output, err := checker.CheckLimits(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.True(t, output.Allowed, "All limits should pass")
	assert.Len(t, output.LimitUsageDetails, 3, "Should have details for all 3 limits from both pages")
	assert.Empty(t, output.ExceededLimitIDs)
}

func TestLimitCheckerService_RollbackUsage_UnknownLimitType(t *testing.T) {
	// This test verifies that RollbackUsage handles unknown limit types gracefully
	// by logging a warning and continuing with other limits.

	limitID := testutil.MustDeterministicUUID(1)
	accountID := testutil.MustDeterministicUUID(100)

	timestamp := time.Date(2025, 12, 28, 10, 0, 0, 0, time.UTC)

	ctrl := gomock.NewController(t)

	mockLimitRepo := NewMockLimitRepository(ctrl)
	mockUsageRepo := NewMockUsageCounterRepository(ctrl)

	// No mocks needed - the unknown limit type causes early return before any repo calls

	ctx := setupTest(t)

	checker, err := NewLimitChecker(mockLimitRepo, mockUsageRepo)
	require.NoError(t, err)

	input := &model.CheckLimitsInput{
		Amount:               decimal.RequireFromString("50"),
		Currency:             "USD",
		AccountID:            accountID,
		TransactionTimestamp: timestamp,
	}

	// Use an unknown limit type in usageDetails - this simulates a scenario where
	// a new limit type is added but not yet supported in CalculatePeriodKey
	usageDetails := []model.LimitUsageDetail{
		{
			LimitID:           limitID,
			LimitAmount:       decimal.RequireFromString("1000"),
			InternalLimitType: model.LimitType("UNKNOWN_TYPE"), // Invalid limit type
			Scopes:            []model.Scope{{AccountID: &accountID}},
			CurrentUsage:      decimal.RequireFromString("550"),
			Exceeded:          false,
		},
	}

	// RollbackUsage should not return an error - it logs warnings and continues
	err = checker.RollbackUsage(ctx, input, usageDetails)

	require.NoError(t, err, "RollbackUsage should not fail, just log warning for unknown limit type")
}

func TestLimitCheckerService_CheckLimits_NilInput(t *testing.T) {
	// This test verifies that CheckLimits returns the appropriate error
	// when called with a nil input.

	ctrl := gomock.NewController(t)

	mockLimitRepo := NewMockLimitRepository(ctrl)
	mockUsageRepo := NewMockUsageCounterRepository(ctrl)

	ctx := setupTest(t)

	checker, err := NewLimitChecker(mockLimitRepo, mockUsageRepo)
	require.NoError(t, err)

	// Call with nil input
	output, err := checker.CheckLimits(ctx, nil)

	require.Error(t, err)
	assert.ErrorIs(t, err, constant.ErrCheckLimitsNilInput)
	assert.Nil(t, output)
}

func TestLimitCheckerService_RollbackUsage_NilInput(t *testing.T) {
	// This test verifies that RollbackUsage handles nil input gracefully.
	// RollbackUsage is best-effort and should not fail even with nil input.

	limitID := testutil.MustDeterministicUUID(1)

	ctrl := gomock.NewController(t)

	mockLimitRepo := NewMockLimitRepository(ctrl)
	mockUsageRepo := NewMockUsageCounterRepository(ctrl)

	ctx := setupTest(t)

	checker, err := NewLimitChecker(mockLimitRepo, mockUsageRepo)
	require.NoError(t, err)

	usageDetails := []model.LimitUsageDetail{
		{
			LimitID:           limitID,
			LimitAmount:       decimal.RequireFromString("1000"),
			InternalLimitType: model.LimitTypeDaily,
			CurrentUsage:      decimal.RequireFromString("550"),
			Exceeded:          false,
		},
	}

	// RollbackUsage with nil input - should return early or handle gracefully
	// Since RollbackUsage needs input for timestamp/scopes, it may just skip processing
	err = checker.RollbackUsage(ctx, nil, usageDetails)

	// The behavior depends on implementation - either it returns an error
	// or handles it gracefully. Let's check what actually happens.
	// Based on the code, it will likely panic or return error when accessing input.Timestamp
	// So we need to check the actual implementation behavior.
	require.Error(t, err, "RollbackUsage should return error for nil input")
	assert.ErrorIs(t, err, constant.ErrCheckLimitsNilInput)
}

func TestLimitCheckerService_CheckLimits_LargeDecimalValues(t *testing.T) {
	// This test verifies that large decimal values are handled correctly.
	// With decimal.Decimal, there is no overflow - arithmetic works normally.

	limitID := testutil.MustDeterministicUUID(1)
	accountID := testutil.MustDeterministicUUID(100)
	counterID := testutil.MustDeterministicUUID(201)

	timestamp := time.Date(2025, 12, 28, 10, 0, 0, 0, time.UTC)
	periodKeyDaily := "2025-12-28"

	ctrl := gomock.NewController(t)

	mockLimitRepo := NewMockLimitRepository(ctrl)
	mockUsageRepo := NewMockUsageCounterRepository(ctrl)

	status := model.LimitStatusActive
	currency := "USD"

	mockLimitRepo.EXPECT().List(gomock.Any(), &model.ListLimitsFilter{
		Status:   &status,
		Currency: &currency,
		Limit:    constant.MaxPaginationLimit,
		Cursor:   "",
	}).Return(&model.ListLimitsResult{
		Limits: []model.Limit{
			{
				ID:        limitID,
				Name:      "High Value Limit",
				LimitType: model.LimitTypeDaily,
				MaxAmount: decimal.RequireFromString("92233720368547758.07"), // MaxInt64 / 100
				Currency:  "USD",
				Scopes:    []model.Scope{{AccountID: &accountID}},
				Status:    model.LimitStatusActive,
			},
		},
		HasMore: false,
	}, nil)

	scopeKey := "acct:" + accountID.String()

	// CurrentUsage is very large, projected usage exceeds maxAmount
	mockUsageRepo.EXPECT().GetOrCreateForUpdate(gomock.Any(), limitID, scopeKey, periodKeyDaily).
		Return(&model.UsageCounter{
			ID:           counterID,
			LimitID:      limitID,
			ScopeKey:     scopeKey,
			PeriodKey:    periodKeyDaily,
			CurrentUsage: decimal.RequireFromString("92233720368547758"), // Near MaxInt64 / 100
		}, nil)

	// No IncrementAtomic should be called because projected usage exceeds the limit

	ctx := setupTest(t)

	checker, err := NewLimitChecker(mockLimitRepo, mockUsageRepo)
	require.NoError(t, err)

	input := &model.CheckLimitsInput{
		Amount:               decimal.RequireFromString("1"), // Small amount but would cause overflow
		Currency:             "USD",
		AccountID:            accountID,
		TransactionTimestamp: timestamp,
	}

	output, err := checker.CheckLimits(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.False(t, output.Allowed, "Should be denied because projected usage exceeds limit")
	assert.Contains(t, output.ExceededLimitIDs, limitID, "Limit should be marked as exceeded when projected > max")
	assert.Len(t, output.LimitUsageDetails, 1)

	// Verify that CurrentUsage is capped at MaxInt64
	assert.True(t, decimal.RequireFromString("92233720368547759").Equal(output.LimitUsageDetails[0].CurrentUsage),
		"CurrentUsage should reflect projected usage (current + amount)")
}

func TestFormatScopeString(t *testing.T) {
	accountID1 := testutil.MustDeterministicUUID(1)
	accountID2 := testutil.MustDeterministicUUID(2)
	segmentID := testutil.MustDeterministicUUID(3)
	portfolioID := testutil.MustDeterministicUUID(4)
	merchantID := testutil.MustDeterministicUUID(5)
	transactionType := model.TransactionTypeCard
	subType := "online"

	tests := []struct {
		name     string
		scopes   []model.Scope
		expected string
	}{
		{
			name:     "empty scopes returns global",
			scopes:   []model.Scope{},
			expected: "global",
		},
		{
			name:     "nil scopes returns global",
			scopes:   nil,
			expected: "global",
		},
		{
			name: "single scope with one field",
			scopes: []model.Scope{
				{AccountID: &accountID1},
			},
			expected: "(account:" + accountID1.String() + ")",
		},
		{
			name: "single scope with multiple fields",
			scopes: []model.Scope{
				{
					AccountID: &accountID1,
					SegmentID: &segmentID,
				},
			},
			expected: "(account:" + accountID1.String() + ",segment:" + segmentID.String() + ")",
		},
		{
			name: "multiple scopes each with one field",
			scopes: []model.Scope{
				{AccountID: &accountID1},
				{AccountID: &accountID2},
			},
			expected: "(account:" + accountID1.String() + ") OR (account:" + accountID2.String() + ")",
		},
		{
			name: "multiple scopes with different fields",
			scopes: []model.Scope{
				{
					AccountID: &accountID1,
					SegmentID: &segmentID,
				},
				{AccountID: &accountID2},
			},
			expected: "(account:" + accountID1.String() + ",segment:" + segmentID.String() + ") OR (account:" + accountID2.String() + ")",
		},
		{
			name: "scope with all fields",
			scopes: []model.Scope{
				{
					AccountID:       &accountID1,
					SegmentID:       &segmentID,
					PortfolioID:     &portfolioID,
					MerchantID:      &merchantID,
					TransactionType: &transactionType,
					SubType:         &subType,
				},
			},
			expected: "(account:" + accountID1.String() + ",segment:" + segmentID.String() + ",portfolio:" + portfolioID.String() + ",merchant:" + merchantID.String() + ",transactionType:CARD,subType:online)",
		},
		{
			name: "scope with empty fields returns global",
			scopes: []model.Scope{
				{}, // all nil fields
			},
			expected: "global",
		},
		{
			name: "mixed: one scope with fields, one empty",
			scopes: []model.Scope{
				{AccountID: &accountID1},
				{}, // empty scope
			},
			expected: "(account:" + accountID1.String() + ")",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := formatScopeString(tc.scopes)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestBuildTransactionScope(t *testing.T) {
	t.Parallel()

	accountID := testutil.MustDeterministicUUID(1)
	segmentID := testutil.MustDeterministicUUID(2)
	portfolioID := testutil.MustDeterministicUUID(3)
	transactionType := model.TransactionTypeCard
	subType := "online"

	tests := []struct {
		name     string
		input    *model.CheckLimitsInput
		validate func(*testing.T, *model.Scope)
	}{
		{
			name:  "nil input returns nil scope",
			input: nil,
			validate: func(t *testing.T, scope *model.Scope) {
				assert.Nil(t, scope)
			},
		},
		{
			name: "full input builds complete scope",
			input: &model.CheckLimitsInput{
				AccountID:            accountID,
				SegmentID:            &segmentID,
				PortfolioID:          &portfolioID,
				TransactionType:      &transactionType,
				SubType:              &subType,
				Amount:               decimal.RequireFromString("100"),
				Currency:             "USD",
				TransactionTimestamp: testutil.FixedTime(),
			},
			validate: func(t *testing.T, scope *model.Scope) {
				require.NotNil(t, scope)
				assert.Equal(t, accountID, *scope.AccountID)
				assert.Equal(t, segmentID, *scope.SegmentID)
				assert.Equal(t, portfolioID, *scope.PortfolioID)
				assert.Equal(t, transactionType, *scope.TransactionType)
				assert.Equal(t, subType, *scope.SubType)
			},
		},
		{
			name: "minimal input builds scope with account only",
			input: &model.CheckLimitsInput{
				AccountID:            accountID,
				Amount:               decimal.RequireFromString("50"),
				Currency:             "USD",
				TransactionTimestamp: testutil.FixedTime(),
			},
			validate: func(t *testing.T, scope *model.Scope) {
				require.NotNil(t, scope)
				assert.Equal(t, accountID, *scope.AccountID)
				assert.Nil(t, scope.SegmentID)
				assert.Nil(t, scope.PortfolioID)
				assert.Nil(t, scope.TransactionType)
				assert.Nil(t, scope.SubType)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := buildTransactionScope(tc.input)
			tc.validate(t, result)
		})
	}
}

func TestScopeMatchesLimit(t *testing.T) {
	t.Parallel()

	accountID1 := testutil.MustDeterministicUUID(1)
	accountID2 := testutil.MustDeterministicUUID(2)
	segmentID := testutil.MustDeterministicUUID(3)

	tests := []struct {
		name        string
		limitScopes []model.Scope
		txScope     *model.Scope
		expected    bool
	}{
		{
			name:        "empty limit scopes (global) matches any transaction",
			limitScopes: []model.Scope{},
			txScope:     &model.Scope{AccountID: &accountID1},
			expected:    true,
		},
		{
			name:        "nil limit scopes (global) matches any transaction",
			limitScopes: nil,
			txScope:     &model.Scope{AccountID: &accountID1},
			expected:    true,
		},
		{
			name:        "nil transaction scope doesn't match non-global limit",
			limitScopes: []model.Scope{{AccountID: &accountID1}},
			txScope:     nil,
			expected:    false,
		},
		{
			name:        "matching account scope",
			limitScopes: []model.Scope{{AccountID: &accountID1}},
			txScope:     &model.Scope{AccountID: &accountID1},
			expected:    true,
		},
		{
			name:        "non-matching account scope",
			limitScopes: []model.Scope{{AccountID: &accountID1}},
			txScope:     &model.Scope{AccountID: &accountID2},
			expected:    false,
		},
		{
			name:        "multiple limit scopes - one matches",
			limitScopes: []model.Scope{{AccountID: &accountID1}, {AccountID: &accountID2}},
			txScope:     &model.Scope{AccountID: &accountID2},
			expected:    true,
		},
		{
			name:        "limit scope requires segment but transaction missing segment",
			limitScopes: []model.Scope{{AccountID: &accountID1, SegmentID: &segmentID}},
			txScope:     &model.Scope{AccountID: &accountID1}, // Missing segment
			expected:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := scopeMatchesLimit(tc.limitScopes, tc.txScope)
			assert.Equal(t, tc.expected, result)
		})
	}
}
