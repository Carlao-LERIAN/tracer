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
				Amount:    10000,
				Currency:  "USD",
				AccountID: accountID,
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
				Amount:    5000,
				Currency:  "USD",
				AccountID: accountID,
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
							MaxAmount: 100000,
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
						CurrentUsage: 50000,
					}, nil)

				ucr.EXPECT().IncrementAtomic(gomock.Any(), counterID1, int64(5000)).Return(nil)
			},
			wantAllowed:  true,
			wantExceeded: nil,
			wantDetails:  1,
			wantErr:      false,
		},
		{
			name: "single DAILY limit - exceeded",
			input: &model.CheckLimitsInput{
				Amount:    60000,
				Currency:  "USD",
				AccountID: accountID,
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
							MaxAmount: 100000,
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
						CurrentUsage: 50000,
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
				Amount:    5000,
				Currency:  "USD",
				AccountID: accountID,
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
							MaxAmount: 10000,
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
				Amount:    15000,
				Currency:  "USD",
				AccountID: accountID,
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
							MaxAmount: 10000,
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
				Amount:    8000,
				Currency:  "USD",
				AccountID: accountID,
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
							MaxAmount: 100000,
							Currency:  "USD",
							Scopes:    []model.Scope{{AccountID: &accountID}},
							Status:    model.LimitStatusActive,
						},
						{
							ID:        limitID2,
							Name:      "Per Transaction Limit",
							LimitType: model.LimitTypePerTransaction,
							MaxAmount: 5000,
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
						CurrentUsage: 50000,
					}, nil)
				// PER_TRANSACTION limitID2 is exceeded (8000 > 5000)
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
				Amount:    5000,
				Currency:  "USD",
				AccountID: accountID,
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
				Amount:    5000,
				Currency:  "USD",
				AccountID: accountID,
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
							MaxAmount: 100000,
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
				Amount:    5000,
				Currency:  "USD",
				AccountID: accountID,
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
							MaxAmount: 100000,
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
						CurrentUsage: 0,
					}, nil)
				ucr.EXPECT().IncrementAtomic(gomock.Any(), counterID1, int64(5000)).Return(nil)
			},
			wantAllowed:  true,
			wantExceeded: nil,
			wantDetails:  1,
			wantErr:      false,
		},
		{
			name: "invalid input - zero amount",
			input: &model.CheckLimitsInput{
				Amount:    0,
				Currency:  "USD",
				AccountID: accountID,
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
				Amount:    5000,
				Currency:  "USD",
				AccountID: uuid.Nil,
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
				Amount:    5000,
				Currency:  "USD",
				AccountID: accountID,
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
							MaxAmount: 500000,
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
						CurrentUsage: 100000,
					}, nil)
				ucr.EXPECT().IncrementAtomic(gomock.Any(), counterID2, int64(5000)).Return(nil)
			},
			wantAllowed:  true,
			wantExceeded: nil,
			wantDetails:  1,
			wantErr:      false,
		},
		{
			name: "boundary - amount equals remaining capacity",
			input: &model.CheckLimitsInput{
				Amount:    50000,
				Currency:  "USD",
				AccountID: accountID,
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
							MaxAmount: 100000,
							Currency:  "USD",
							Scopes:    []model.Scope{{AccountID: &accountID}},
							Status:    model.LimitStatusActive,
						},
					},
					HasMore: false,
				}, nil)

				scopeKey := "acct:" + accountID.String()
				// CurrentUsage 50000 + Amount 50000 == MaxAmount 100000 (exactly at limit)
				ucr.EXPECT().GetOrCreateForUpdate(gomock.Any(), limitID1, scopeKey, periodKeyDaily).
					Return(&model.UsageCounter{
						ID:           counterID1,
						LimitID:      limitID1,
						ScopeKey:     scopeKey,
						PeriodKey:    periodKeyDaily,
						CurrentUsage: 50000,
					}, nil)
				// IncrementAtomic should be called because projected usage equals limit (not exceeded)
				ucr.EXPECT().IncrementAtomic(gomock.Any(), counterID1, int64(50000)).Return(nil)
			},
			wantAllowed:  true,
			wantExceeded: nil,
			wantDetails:  1,
			wantErr:      false,
		},
		{
			name: "error - LimitRepository.List returns error",
			input: &model.CheckLimitsInput{
				Amount:    5000,
				Currency:  "USD",
				AccountID: accountID,
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
				Amount:    5000,
				Currency:  "USD",
				AccountID: accountID,
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
							MaxAmount: 100000,
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
				Amount:    5000,
				Currency:  "USD",
				AccountID: accountID,
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
							MaxAmount: 100000,
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
						CurrentUsage: 50000,
					}, nil)
				ucr.EXPECT().IncrementAtomic(gomock.Any(), counterID1, int64(5000)).
					Return(errDatabase)
			},
			wantAllowed: false,
			wantErr:     true,
			wantErrIs:   errDatabase,
		},
		{
			name: "invalid input - negative amount",
			input: &model.CheckLimitsInput{
				Amount:    -100,
				Currency:  "USD",
				AccountID: accountID,
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
				Amount:    5000,
				Currency:  "USD",
				AccountID: accountID,
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
				Amount:    5000,
				Currency:  "USD",
				AccountID: accountID,
				TransactionTimestamp: timestamp,
			},
			usageDetails: []model.LimitUsageDetail{
				{
					LimitID:           limitID1,
					LimitAmount:       100000,
					InternalLimitType: model.LimitTypeDaily,
					Scopes:            []model.Scope{{AccountID: &accountID}},
					CurrentUsage:      55000,
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
						CurrentUsage: 55000,
					}, nil)
				ucr.EXPECT().DecrementAtomic(gomock.Any(), counterID1, int64(5000)).Return(nil)
			},
			wantErr: false,
		},
		{
			name: "skip PER_TRANSACTION limit - no persistent counter",
			input: &model.CheckLimitsInput{
				Amount:    5000,
				Currency:  "USD",
				AccountID: accountID,
				TransactionTimestamp: timestamp,
			},
			usageDetails: []model.LimitUsageDetail{
				{
					LimitID:           limitID2,
					LimitAmount:       10000,
					InternalLimitType: model.LimitTypePerTransaction,
					Scopes:            []model.Scope{{AccountID: &accountID}},
					CurrentUsage:      0,
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
				Amount:    5000,
				Currency:  "USD",
				AccountID: accountID,
				TransactionTimestamp: timestamp,
			},
			usageDetails: []model.LimitUsageDetail{
				{
					LimitID:           limitID1,
					LimitAmount:       100000,
					InternalLimitType: model.LimitTypeDaily,
					Scopes:            []model.Scope{{AccountID: &accountID}},
					CurrentUsage:      55000,
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
				Amount:    5000,
				Currency:  "USD",
				AccountID: accountID,
				TransactionTimestamp: timestamp,
			},
			usageDetails: []model.LimitUsageDetail{
				{
					LimitID:           limitID1,
					LimitAmount:       100000,
					InternalLimitType: model.LimitTypeDaily,
					Scopes:            []model.Scope{{AccountID: &accountID}},
					CurrentUsage:      55000,
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
						CurrentUsage: 55000,
					}, nil)
				ucr.EXPECT().DecrementAtomic(gomock.Any(), counterID1, int64(5000)).
					Return(errDatabase)
			},
			wantErr: false, // Should not error, just log warning
		},
		{
			name: "multiple limits - partial failures logged as warnings",
			input: &model.CheckLimitsInput{
				Amount:    5000,
				Currency:  "USD",
				AccountID: accountID,
				TransactionTimestamp: timestamp,
			},
			usageDetails: []model.LimitUsageDetail{
				{
					LimitID:           limitID1,
					LimitAmount:       100000,
					InternalLimitType: model.LimitTypeDaily,
					Scopes:            []model.Scope{{AccountID: &accountID}},
					CurrentUsage:      55000,
					Exceeded:          false,
				},
				{
					LimitID:           limitID2,
					LimitAmount:       200000,
					InternalLimitType: model.LimitTypeDaily,
					Scopes:            []model.Scope{{AccountID: &accountID}},
					CurrentUsage:      105000,
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
						CurrentUsage: 105000,
					}, nil)
				ucr.EXPECT().DecrementAtomic(gomock.Any(), counterID2, int64(5000)).Return(nil)
			},
			wantErr: false, // Should not error, continues with other limits
		},
		{
			name: "decrement would result in negative - logs warning but continues",
			input: &model.CheckLimitsInput{
				Amount:    5000,
				Currency:  "USD",
				AccountID: accountID,
				TransactionTimestamp: timestamp,
			},
			usageDetails: []model.LimitUsageDetail{
				{
					LimitID:           limitID1,
					LimitAmount:       100000,
					InternalLimitType: model.LimitTypeDaily,
					Scopes:            []model.Scope{{AccountID: &accountID}},
					CurrentUsage:      55000,
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
						CurrentUsage: 55000,
					}, nil)
				ucr.EXPECT().DecrementAtomic(gomock.Any(), counterID1, int64(5000)).
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
	const amountPerRequest = int64(1000)

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
				MaxAmount: 100000, // High enough to not be exceeded
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
			CurrentUsage: 0, // Starting usage
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
				Amount:    amountPerRequest,
				Currency:  "USD",
				AccountID: accountID,
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
				MaxAmount: 100000, // Will NOT exceed
				Currency:  "USD",
				Scopes:    []model.Scope{{AccountID: &accountID}},
				Status:    model.LimitStatusActive,
			},
			{
				ID:        limitID2,
				Name:      "Per Transaction Limit",
				LimitType: model.LimitTypePerTransaction,
				MaxAmount: 5000, // Will exceed (amount is 8000)
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
			CurrentUsage: 50000,
		}, nil)

	// Explicit assertion: IncrementAtomic must NEVER be called when any limit exceeds.
	// This is the core guarantee of the two-phase check-then-increment design.
	mockUsageRepo.EXPECT().IncrementAtomic(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)

	ctx := setupTest(t)

	checker, err := NewLimitChecker(mockLimitRepo, mockUsageRepo)
	require.NoError(t, err)

	input := &model.CheckLimitsInput{
		Amount:    8000, // Exceeds PER_TRANSACTION limit of 5000
		Currency:  "USD",
		AccountID: accountID,
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
	// This test verifies behavior with large amounts near int64 max.
	// Tests overflow detection in the increment path.

	limitID := testutil.MustDeterministicUUID(1)
	accountID := testutil.MustDeterministicUUID(100)
	counterID := testutil.MustDeterministicUUID(201)

	timestamp := time.Date(2025, 12, 28, 10, 0, 0, 0, time.UTC)
	periodKeyDaily := "2025-12-28"

	tests := []struct {
		name         string
		amount       int64
		currentUsage int64
		maxAmount    int64
		wantAllowed  bool
		wantErr      bool
		wantErrIs    error
		setupIncr    func(*MockUsageCounterRepository)
	}{
		{
			name:         "large amount within limits - allowed",
			amount:       int64(1e15), // 1 quadrillion (in smallest unit)
			currentUsage: int64(1e15),
			maxAmount:    int64(5e15),
			wantAllowed:  true,
			wantErr:      false,
			setupIncr: func(ucr *MockUsageCounterRepository) {
				ucr.EXPECT().IncrementAtomic(gomock.Any(), counterID, int64(1e15)).Return(nil)
			},
		},
		{
			name:         "amount causes overflow - detected before increment",
			amount:       int64(1e18),                // Very large amount
			currentUsage: int64(9e18),                // Current usage close to max
			maxAmount:    int64(9223372036854775807), // MaxInt64
			wantAllowed:  false,                      // Overflow detected, treated as exceeded
			wantErr:      false,                      // No error, just exceeds limit
			wantErrIs:    nil,
			setupIncr:    func(ucr *MockUsageCounterRepository) {}, // No increment called - overflow detected early
		},
		{
			name:         "amount exactly at remaining capacity - allowed",
			amount:       int64(1e12),
			currentUsage: int64(9e12),
			maxAmount:    int64(10e12), // currentUsage + amount == maxAmount
			wantAllowed:  true,
			wantErr:      false,
			setupIncr: func(ucr *MockUsageCounterRepository) {
				ucr.EXPECT().IncrementAtomic(gomock.Any(), counterID, int64(1e12)).Return(nil)
			},
		},
		{
			name:         "amount exceeds limit - not allowed but no error",
			amount:       int64(2e12),
			currentUsage: int64(9e12),
			maxAmount:    int64(10e12), // currentUsage + amount > maxAmount
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
				Amount:    tc.amount,
				Currency:  "USD",
				AccountID: accountID,
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
		limitAmount  int64
		currentUsage int64
		expected     int64
	}{
		{
			name:         "large limit with zero usage",
			limitAmount:  int64(9223372036854775807), // MaxInt64
			currentUsage: 0,
			expected:     int64(9223372036854775807),
		},
		{
			name:         "large limit with large usage",
			limitAmount:  int64(9223372036854775807),
			currentUsage: int64(9223372036854775800),
			expected:     7, // MaxInt64 - 9223372036854775800
		},
		{
			name:         "large limit exactly at max",
			limitAmount:  int64(9223372036854775807),
			currentUsage: int64(9223372036854775807),
			expected:     0,
		},
		{
			name:         "quadrillion scale values",
			limitAmount:  int64(5e15),
			currentUsage: int64(3e15),
			expected:     int64(2e15),
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
			assert.Equal(t, tc.expected, result)
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
				MaxAmount: 100000,
				Currency:  "USD",
				Scopes:    []model.Scope{{AccountID: &accountID}},
				Status:    model.LimitStatusActive,
			},
			{
				ID:        limitID2,
				Name:      "Daily Limit 2",
				LimitType: model.LimitTypeDaily,
				MaxAmount: 200000,
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
				MaxAmount: 300000,
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
			CurrentUsage: 0,
		}, nil)
	mockUsageRepo.EXPECT().GetOrCreateForUpdate(gomock.Any(), limitID2, scopeKey, periodKeyDaily).
		Return(&model.UsageCounter{
			ID:           counterID2,
			LimitID:      limitID2,
			ScopeKey:     scopeKey,
			PeriodKey:    periodKeyDaily,
			CurrentUsage: 0,
		}, nil)
	mockUsageRepo.EXPECT().GetOrCreateForUpdate(gomock.Any(), limitID3, scopeKey, periodKeyDaily).
		Return(&model.UsageCounter{
			ID:           counterID3,
			LimitID:      limitID3,
			ScopeKey:     scopeKey,
			PeriodKey:    periodKeyDaily,
			CurrentUsage: 0,
		}, nil)

	// Expect IncrementAtomic for all 3 limits (none exceeded)
	mockUsageRepo.EXPECT().IncrementAtomic(gomock.Any(), counterID1, int64(5000)).Return(nil)
	mockUsageRepo.EXPECT().IncrementAtomic(gomock.Any(), counterID2, int64(5000)).Return(nil)
	mockUsageRepo.EXPECT().IncrementAtomic(gomock.Any(), counterID3, int64(5000)).Return(nil)

	ctx := setupTest(t)

	checker, err := NewLimitChecker(mockLimitRepo, mockUsageRepo)
	require.NoError(t, err)

	input := &model.CheckLimitsInput{
		Amount:    5000,
		Currency:  "USD",
		AccountID: accountID,
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
		Amount:    5000,
		Currency:  "USD",
		AccountID: accountID,
		TransactionTimestamp: timestamp,
	}

	// Use an unknown limit type in usageDetails - this simulates a scenario where
	// a new limit type is added but not yet supported in CalculatePeriodKey
	usageDetails := []model.LimitUsageDetail{
		{
			LimitID:           limitID,
			LimitAmount:       100000,
			InternalLimitType: model.LimitType("UNKNOWN_TYPE"), // Invalid limit type
			Scopes:            []model.Scope{{AccountID: &accountID}},
			CurrentUsage:      55000,
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
			LimitAmount:       100000,
			InternalLimitType: model.LimitTypeDaily,
			CurrentUsage:      55000,
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

func TestLimitCheckerService_CheckLimits_OverflowProtection(t *testing.T) {
	// This test verifies that overflow protection works correctly when
	// counter.CurrentUsage + input.Amount would overflow int64.

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
				MaxAmount: int64(9223372036854775807), // MaxInt64
				Currency:  "USD",
				Scopes:    []model.Scope{{AccountID: &accountID}},
				Status:    model.LimitStatusActive,
			},
		},
		HasMore: false,
	}, nil)

	scopeKey := "acct:" + accountID.String()

	// CurrentUsage is near MaxInt64, adding amount would overflow
	mockUsageRepo.EXPECT().GetOrCreateForUpdate(gomock.Any(), limitID, scopeKey, periodKeyDaily).
		Return(&model.UsageCounter{
			ID:           counterID,
			LimitID:      limitID,
			ScopeKey:     scopeKey,
			PeriodKey:    periodKeyDaily,
			CurrentUsage: int64(9223372036854775800), // Near MaxInt64
		}, nil)

	// No IncrementAtomic should be called because overflow is detected
	// and the limit is marked as exceeded

	ctx := setupTest(t)

	checker, err := NewLimitChecker(mockLimitRepo, mockUsageRepo)
	require.NoError(t, err)

	input := &model.CheckLimitsInput{
		Amount:    int64(100), // Small amount but would cause overflow
		Currency:  "USD",
		AccountID: accountID,
		TransactionTimestamp: timestamp,
	}

	output, err := checker.CheckLimits(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.False(t, output.Allowed, "Should be denied due to overflow protection")
	assert.Contains(t, output.ExceededLimitIDs, limitID, "Limit should be marked as exceeded")
	assert.Len(t, output.LimitUsageDetails, 1)

	// Verify that CurrentUsage is capped at MaxInt64
	assert.Equal(t, int64(9223372036854775807), output.LimitUsageDetails[0].CurrentUsage,
		"CurrentUsage should be capped at MaxInt64 when overflow would occur")
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
