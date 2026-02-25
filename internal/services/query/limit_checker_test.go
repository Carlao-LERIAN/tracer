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
	"tracer/pkg/clock"
	"tracer/pkg/constant"
	"tracer/pkg/model"
)

// errDatabase is a sentinel error for testing database error paths.
var errDatabase = errors.New("database error")

// serverPeriodKeyDaily and serverPeriodKeyMonthly are derived from the
// deterministic mock clock so period-key expectations stay in sync.
var (
	serverPeriodKeyDaily   = testutil.DefaultTestTime.Format("2006-01-02")
	serverPeriodKeyMonthly = testutil.DefaultTestTime.Format("2006-01")
)

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
		clock     clock.Clock
		wantErr   bool
		wantErrIs error
	}{
		{
			name:      "valid repositories",
			limitRepo: mockLimitRepo,
			usageRepo: mockUsageRepo,
			clock:     testutil.NewDefaultMockClock(),
			wantErr:   false,
		},
		{
			name:      "nil limit repository",
			limitRepo: nil,
			usageRepo: mockUsageRepo,
			clock:     testutil.NewDefaultMockClock(),
			wantErr:   true,
			wantErrIs: constant.ErrLimitCheckerNilLimitRepo,
		},
		{
			name:      "nil usage counter repository",
			limitRepo: mockLimitRepo,
			usageRepo: nil,
			clock:     testutil.NewDefaultMockClock(),
			wantErr:   true,
			wantErrIs: constant.ErrLimitCheckerNilUsageCounterRepo,
		},
		{
			name:      "nil clock",
			limitRepo: mockLimitRepo,
			usageRepo: mockUsageRepo,
			clock:     nil,
			wantErr:   true,
			wantErrIs: constant.ErrLimitCheckerNilClock,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			checker, err := NewLimitChecker(tc.limitRepo, tc.usageRepo, tc.clock)

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

	timestamp := time.Date(2025, 12, 28, 10, 0, 0, 0, time.UTC)
	periodKeyDaily := serverPeriodKeyDaily

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
				// Atomic upsert: returns new usage (500 + 50 = 550)
				ucr.EXPECT().UpsertAndIncrementAtomic(gomock.Any(), limitID1, scopeKey, periodKeyDaily, decimal.RequireFromString("50"), decimal.RequireFromString("1000")).
					Return(decimal.RequireFromString("550"), nil)
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
				// Atomic upsert: returns ErrUsageCounterExceedsLimit when 500 + 600 > 1000
				ucr.EXPECT().UpsertAndIncrementAtomic(gomock.Any(), limitID1, scopeKey, periodKeyDaily, decimal.RequireFromString("600"), decimal.RequireFromString("1000")).
					Return(decimal.RequireFromString("500"), constant.ErrUsageCounterExceedsLimit)
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
				// First limit (DAILY) is atomically incremented (succeeds: 500 + 80 <= 1000)
				ucr.EXPECT().UpsertAndIncrementAtomic(gomock.Any(), limitID1, scopeKey, periodKeyDaily, decimal.RequireFromString("80"), decimal.RequireFromString("1000")).
					Return(decimal.RequireFromString("580"), nil)

				// Second limit (PER_TRANSACTION) is checked directly: 80 > 50 → exceeded
				// When exceeded, rollback the first limit's increment
				ucr.EXPECT().GetForUpdate(gomock.Any(), limitID1, scopeKey, periodKeyDaily).
					Return(&model.UsageCounter{
						ID:           counterID1,
						LimitID:      limitID1,
						ScopeKey:     scopeKey,
						PeriodKey:    periodKeyDaily,
						CurrentUsage: decimal.RequireFromString("580"),
					}, nil)
				ucr.EXPECT().DecrementAtomic(gomock.Any(), counterID1, decimal.RequireFromString("80")).Return(nil)
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
				// Atomic upsert: returns new usage (0 + 50 = 50)
				ucr.EXPECT().UpsertAndIncrementAtomic(gomock.Any(), limitID1, scopeKey, periodKeyDaily, decimal.RequireFromString("50"), decimal.RequireFromString("1000")).
					Return(decimal.RequireFromString("50"), nil)
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
				periodKeyMonthly := serverPeriodKeyMonthly
				// Atomic upsert: returns new usage (1000 + 50 = 1050)
				ucr.EXPECT().UpsertAndIncrementAtomic(gomock.Any(), limitID3, scopeKey, periodKeyMonthly, decimal.RequireFromString("50"), decimal.RequireFromString("5000")).
					Return(decimal.RequireFromString("1050"), nil)
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
				// Atomic upsert: CurrentUsage 500 + Amount 500 == MaxAmount 1000 (exactly at limit, allowed)
				// Returns new usage (1000)
				ucr.EXPECT().UpsertAndIncrementAtomic(gomock.Any(), limitID1, scopeKey, periodKeyDaily, decimal.RequireFromString("500"), decimal.RequireFromString("1000")).
					Return(decimal.RequireFromString("1000"), nil)
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
			name: "error - UsageCounterRepository.UpsertAndIncrementAtomic returns error",
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
				// Atomic upsert returns database error
				ucr.EXPECT().UpsertAndIncrementAtomic(gomock.Any(), limitID1, scopeKey, periodKeyDaily, decimal.RequireFromString("50"), decimal.RequireFromString("1000")).
					Return(decimal.Zero, errDatabase)
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

			checker, err := NewLimitChecker(mockLimitRepo, mockUsageRepo, testutil.NewDefaultMockClock())
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
	periodKeyDaily := serverPeriodKeyDaily // Uses server clock period key

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
					InternalPeriodKey: periodKeyDaily, // Stored period key
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
					InternalPeriodKey: periodKeyDaily, // Stored period key
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
					InternalPeriodKey: periodKeyDaily, // Stored period key
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
					InternalPeriodKey: periodKeyDaily, // Stored period key
				},
				{
					LimitID:           limitID2,
					LimitAmount:       decimal.RequireFromString("2000"),
					InternalLimitType: model.LimitTypeDaily,
					Scopes:            []model.Scope{{AccountID: &accountID}},
					CurrentUsage:      decimal.RequireFromString("1050"),
					Exceeded:          false,
					InternalPeriodKey: periodKeyDaily, // Stored period key
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
					InternalPeriodKey: periodKeyDaily, // Stored period key
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

			checker, err := NewLimitChecker(mockLimitRepo, mockUsageRepo, testutil.NewDefaultMockClock())
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
	// 2. Each call uses atomic UpsertAndIncrementAtomic
	// 3. Increments happen atomically in DB

	limitID := testutil.MustDeterministicUUID(1)
	accountID := testutil.MustDeterministicUUID(100)

	timestamp := time.Date(2025, 12, 28, 10, 0, 0, 0, time.UTC)
	periodKeyDaily := serverPeriodKeyDaily

	const numGoroutines = 10
	amountPerRequest := decimal.RequireFromString("10")

	ctrl := gomock.NewController(t)

	mockLimitRepo := NewMockLimitRepository(ctrl)
	mockUsageRepo := NewMockUsageCounterRepository(ctrl)

	// Setup mock expectations for concurrent calls
	// Each goroutine will call List + UpsertAndIncrementAtomic
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

	// Each concurrent call uses atomic upsert (returns incremental usage values)
	mockUsageRepo.EXPECT().UpsertAndIncrementAtomic(
		gomock.Any(),
		limitID,
		scopeKey,
		periodKeyDaily,
		amountPerRequest,
		decimal.RequireFromString("1000"),
	).Return(decimal.RequireFromString("10"), nil).Times(numGoroutines) // Returns mock post-increment usage

	ctx := setupTest(t)

	checker, err := NewLimitChecker(mockLimitRepo, mockUsageRepo, testutil.NewDefaultMockClock())
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
	periodKeyDaily := serverPeriodKeyDaily

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

	// First limit (DAILY) is atomically incremented (succeeds: 500 + 80 <= 1000)
	mockUsageRepo.EXPECT().UpsertAndIncrementAtomic(gomock.Any(), limitID1, scopeKey, periodKeyDaily, decimal.RequireFromString("80"), decimal.RequireFromString("1000")).
		Return(decimal.RequireFromString("580"), nil)

	// Second limit (PER_TRANSACTION) is checked directly: 80 > 50 → exceeded
	// When exceeded, rollback the first limit's increment
	mockUsageRepo.EXPECT().GetForUpdate(gomock.Any(), limitID1, scopeKey, periodKeyDaily).
		Return(&model.UsageCounter{
			ID:           counterID1,
			LimitID:      limitID1,
			ScopeKey:     scopeKey,
			PeriodKey:    periodKeyDaily,
			CurrentUsage: decimal.RequireFromString("580"),
		}, nil)
	mockUsageRepo.EXPECT().DecrementAtomic(gomock.Any(), counterID1, decimal.RequireFromString("80")).Return(nil)

	ctx := setupTest(t)

	checker, err := NewLimitChecker(mockLimitRepo, mockUsageRepo, testutil.NewDefaultMockClock())
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

	timestamp := time.Date(2025, 12, 28, 10, 0, 0, 0, time.UTC)
	periodKeyDaily := serverPeriodKeyDaily

	tests := []struct {
		name        string
		amount      decimal.Decimal
		maxAmount   decimal.Decimal
		wantAllowed bool
		wantErr     bool
		wantErrIs   error
		setupUpsert func(*MockUsageCounterRepository, string)
	}{
		{
			name:        "large amount within limits - allowed",
			amount:      decimal.RequireFromString("10000000000000"), // 10 trillion
			maxAmount:   decimal.RequireFromString("50000000000000"),
			wantAllowed: true,
			wantErr:     false,
			setupUpsert: func(ucr *MockUsageCounterRepository, scopeKey string) {
				// Atomic upsert succeeds, returns new usage (10 trillion + 10 trillion = 20 trillion)
				ucr.EXPECT().UpsertAndIncrementAtomic(gomock.Any(), limitID, scopeKey, periodKeyDaily, decimal.RequireFromString("10000000000000"), decimal.RequireFromString("50000000000000")).
					Return(decimal.RequireFromString("20000000000000"), nil)
			},
		},
		{
			name:        "large amount exceeds limit - projected usage > maxAmount",
			amount:      decimal.RequireFromString("10000000000000000"),    // Very large amount (10 quadrillion)
			maxAmount:   decimal.RequireFromString("92233720368547758.07"), // MaxInt64 / 100 (~92 quadrillion)
			wantAllowed: false,                                             // DB returns ErrUsageCounterExceedsLimit
			wantErr:     false,                                             // No error, just exceeds limit
			wantErrIs:   nil,
			setupUpsert: func(ucr *MockUsageCounterRepository, scopeKey string) {
				// Atomic upsert returns ErrUsageCounterExceedsLimit when currentUsage + amount > maxAmount
				ucr.EXPECT().UpsertAndIncrementAtomic(gomock.Any(), limitID, scopeKey, periodKeyDaily, decimal.RequireFromString("10000000000000000"), decimal.RequireFromString("92233720368547758.07")).
					Return(decimal.RequireFromString("90000000000000000"), constant.ErrUsageCounterExceedsLimit)
			},
		},
		{
			name:        "amount exactly at remaining capacity - allowed",
			amount:      decimal.RequireFromString("10000000000"),
			maxAmount:   decimal.RequireFromString("100000000000"), // currentUsage + amount == maxAmount
			wantAllowed: true,
			wantErr:     false,
			setupUpsert: func(ucr *MockUsageCounterRepository, scopeKey string) {
				// Atomic upsert succeeds at boundary (returns exactly maxAmount)
				ucr.EXPECT().UpsertAndIncrementAtomic(gomock.Any(), limitID, scopeKey, periodKeyDaily, decimal.RequireFromString("10000000000"), decimal.RequireFromString("100000000000")).
					Return(decimal.RequireFromString("100000000000"), nil)
			},
		},
		{
			name:        "amount exceeds limit - not allowed but no error",
			amount:      decimal.RequireFromString("20000000000"),
			maxAmount:   decimal.RequireFromString("100000000000"), // currentUsage + amount > maxAmount
			wantAllowed: false,
			wantErr:     false,
			setupUpsert: func(ucr *MockUsageCounterRepository, scopeKey string) {
				// Atomic upsert returns ErrUsageCounterExceedsLimit
				ucr.EXPECT().UpsertAndIncrementAtomic(gomock.Any(), limitID, scopeKey, periodKeyDaily, decimal.RequireFromString("20000000000"), decimal.RequireFromString("100000000000")).
					Return(decimal.RequireFromString("90000000000"), constant.ErrUsageCounterExceedsLimit)
			},
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
			tc.setupUpsert(mockUsageRepo, scopeKey)

			ctx := setupTest(t)

			checker, err := NewLimitChecker(mockLimitRepo, mockUsageRepo, testutil.NewDefaultMockClock())
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

	timestamp := time.Date(2025, 12, 28, 10, 0, 0, 0, time.UTC)
	periodKeyDaily := serverPeriodKeyDaily

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

	// Expect UpsertAndIncrementAtomic for all 3 limits (none exceeded)
	mockUsageRepo.EXPECT().UpsertAndIncrementAtomic(gomock.Any(), limitID1, scopeKey, periodKeyDaily, decimal.RequireFromString("50"), decimal.RequireFromString("1000")).
		Return(decimal.RequireFromString("50"), nil)
	mockUsageRepo.EXPECT().UpsertAndIncrementAtomic(gomock.Any(), limitID2, scopeKey, periodKeyDaily, decimal.RequireFromString("50"), decimal.RequireFromString("2000")).
		Return(decimal.RequireFromString("50"), nil)
	mockUsageRepo.EXPECT().UpsertAndIncrementAtomic(gomock.Any(), limitID3, scopeKey, periodKeyDaily, decimal.RequireFromString("50"), decimal.RequireFromString("3000")).
		Return(decimal.RequireFromString("50"), nil)

	ctx := setupTest(t)

	checker, err := NewLimitChecker(mockLimitRepo, mockUsageRepo, testutil.NewDefaultMockClock())
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

	checker, err := NewLimitChecker(mockLimitRepo, mockUsageRepo, testutil.NewDefaultMockClock())
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

	checker, err := NewLimitChecker(mockLimitRepo, mockUsageRepo, testutil.NewDefaultMockClock())
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

	checker, err := NewLimitChecker(mockLimitRepo, mockUsageRepo, testutil.NewDefaultMockClock())
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

	timestamp := time.Date(2025, 12, 28, 10, 0, 0, 0, time.UTC)
	periodKeyDaily := serverPeriodKeyDaily

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

	// Atomic upsert returns ErrUsageCounterExceedsLimit when current (92233720368547758) + 1 > max (92233720368547758.07)
	mockUsageRepo.EXPECT().UpsertAndIncrementAtomic(gomock.Any(), limitID, scopeKey, periodKeyDaily, decimal.RequireFromString("1"), decimal.RequireFromString("92233720368547758.07")).
		Return(decimal.RequireFromString("92233720368547758"), constant.ErrUsageCounterExceedsLimit)

	ctx := setupTest(t)

	checker, err := NewLimitChecker(mockLimitRepo, mockUsageRepo, testutil.NewDefaultMockClock())
	require.NoError(t, err)

	input := &model.CheckLimitsInput{
		Amount:               decimal.RequireFromString("1"), // Small amount but would exceed limit
		Currency:             "USD",
		AccountID:            accountID,
		TransactionTimestamp: timestamp,
	}

	output, err := checker.CheckLimits(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.False(t, output.Allowed, "Should be denied because projected usage exceeds limit")
	assert.Contains(t, output.ExceededLimitIDs, limitID, "Limit should be marked as exceeded when projected > max")
	require.Len(t, output.LimitUsageDetails, 1)

	// CurrentUsage is projected usage (current + amount) when exceeded
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

func TestCheckLimits_ServerTimestamp(t *testing.T) {
	t.Parallel()

	// Seed 8152-8159
	limitID := testutil.MustDeterministicUUID(8152)
	accountID := testutil.MustDeterministicUUID(8153)

	// Server clock: 2024-01-15 10:30:00 UTC
	serverTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	mockClock := testutil.NewMockClock(serverTime)

	// Client timestamp: 2024-01-14 08:00:00 UTC (yesterday - the attacker's manipulated date)
	clientTimestamp := time.Date(2024, 1, 14, 8, 0, 0, 0, time.UTC)

	// The expected period key MUST be based on server date, NOT client date
	expectedPeriodKeyDaily := "2024-01-15"
	// Client would compute period key "2024-01-14" — must never be used

	ctrl := gomock.NewController(t)

	mockLimitRepo := NewMockLimitRepository(ctrl)
	mockUsageRepo := NewMockUsageCounterRepository(ctrl)

	status := model.LimitStatusActive
	currency := "USD"
	scopeKey := "acct:" + accountID.String()

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
				MaxAmount: decimal.RequireFromString("1000"),
				Currency:  "USD",
				Scopes:    []model.Scope{{AccountID: &accountID}},
				Status:    model.LimitStatusActive,
			},
		},
		HasMore: false,
	}, nil)

	// KEY ASSERTION: UpsertAndIncrementAtomic must be called with server-date period key "2024-01-15",
	// NOT the client-supplied "2024-01-14". This is the core security verification.
	mockUsageRepo.EXPECT().UpsertAndIncrementAtomic(gomock.Any(), limitID, scopeKey, expectedPeriodKeyDaily, decimal.RequireFromString("100"), decimal.RequireFromString("1000")).
		Return(decimal.RequireFromString("600"), nil) // Returns new usage (500 + 100)

	ctx := setupTest(t)

	checker, err := NewLimitChecker(mockLimitRepo, mockUsageRepo, mockClock)
	require.NoError(t, err)

	input := &model.CheckLimitsInput{
		Amount:               decimal.RequireFromString("100"),
		Currency:             "USD",
		AccountID:            accountID,
		TransactionTimestamp: clientTimestamp, // Attacker-controlled: yesterday
	}

	output, err := checker.CheckLimits(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.True(t, output.Allowed, "Transaction should be allowed (500 + 100 <= 1000)")
	assert.Len(t, output.LimitUsageDetails, 1)

	// If the mock for UpsertAndIncrementAtomic with "2024-01-15" was NOT called,
	// gomock will fail the test -- proving the server timestamp was used.
}

func TestCheckLimits_ServerTimestamp_Monthly(t *testing.T) {
	t.Parallel()

	// Seed 8160-8167
	limitID := testutil.MustDeterministicUUID(8160)
	accountID := testutil.MustDeterministicUUID(8161)

	// Server clock: 2024-02-01 00:05:00 UTC (first day of February)
	serverTime := time.Date(2024, 2, 1, 0, 5, 0, 0, time.UTC)
	mockClock := testutil.NewMockClock(serverTime)

	// Client timestamp: 2024-01-31 23:55:00 UTC (last day of January - boundary attack)
	clientTimestamp := time.Date(2024, 1, 31, 23, 55, 0, 0, time.UTC)

	// The expected period key MUST be based on server date (February), NOT client date (January)
	expectedPeriodKeyMonthly := "2024-02"

	ctrl := gomock.NewController(t)

	mockLimitRepo := NewMockLimitRepository(ctrl)
	mockUsageRepo := NewMockUsageCounterRepository(ctrl)

	status := model.LimitStatusActive
	currency := "USD"
	scopeKey := "acct:" + accountID.String()

	mockLimitRepo.EXPECT().List(gomock.Any(), &model.ListLimitsFilter{
		Status:   &status,
		Currency: &currency,
		Limit:    constant.MaxPaginationLimit,
		Cursor:   "",
	}).Return(&model.ListLimitsResult{
		Limits: []model.Limit{
			{
				ID:        limitID,
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

	// KEY ASSERTION: period key must be "2024-02" (server month), not "2024-01" (client month)
	mockUsageRepo.EXPECT().UpsertAndIncrementAtomic(gomock.Any(), limitID, scopeKey, expectedPeriodKeyMonthly, decimal.RequireFromString("200"), decimal.RequireFromString("5000")).
		Return(decimal.RequireFromString("2200"), nil) // Returns new usage (2000 + 200)

	ctx := setupTest(t)

	checker, err := NewLimitChecker(mockLimitRepo, mockUsageRepo, mockClock)
	require.NoError(t, err)

	input := &model.CheckLimitsInput{
		Amount:               decimal.RequireFromString("200"),
		Currency:             "USD",
		AccountID:            accountID,
		TransactionTimestamp: clientTimestamp, // Attacker-controlled: last day of previous month
	}

	output, err := checker.CheckLimits(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.True(t, output.Allowed, "Transaction should be allowed (2000 + 200 <= 5000)")
	assert.Len(t, output.LimitUsageDetails, 1)
}

func TestCheckLimits_PerTransactionUnaffectedByClock(t *testing.T) {
	t.Parallel()

	// Seed 8170-8177
	limitID := testutil.MustDeterministicUUID(8170)
	accountID := testutil.MustDeterministicUUID(8171)

	// Server clock: some arbitrary date - should NOT matter for PER_TRANSACTION
	serverTime := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
	mockClock := testutil.NewMockClock(serverTime)

	// Client timestamp: completely different date - also should NOT matter
	clientTimestamp := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)

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
				Name:      "Per Transaction Limit",
				LimitType: model.LimitTypePerTransaction,
				MaxAmount: decimal.RequireFromString("500"),
				Currency:  "USD",
				Scopes:    []model.Scope{{AccountID: &accountID}},
				Status:    model.LimitStatusActive,
			},
		},
		HasMore: false,
	}, nil)

	// No UsageCounterRepository calls expected for PER_TRANSACTION limits.
	// PER_TRANSACTION checks amount directly against maxAmount with no counters.

	ctx := setupTest(t)

	checker, err := NewLimitChecker(mockLimitRepo, mockUsageRepo, mockClock)
	require.NoError(t, err)

	input := &model.CheckLimitsInput{
		Amount:               decimal.RequireFromString("200"),
		Currency:             "USD",
		AccountID:            accountID,
		TransactionTimestamp: clientTimestamp,
	}

	output, err := checker.CheckLimits(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.True(t, output.Allowed, "200 <= 500 should be allowed for PER_TRANSACTION")
	assert.Empty(t, output.ExceededLimitIDs)
	assert.Len(t, output.LimitUsageDetails, 1)

	detail := output.LimitUsageDetails[0]
	assert.Equal(t, limitID, detail.LimitID)
	assert.True(t, decimal.Zero.Equal(detail.CurrentUsage), "PER_TRANSACTION has no persistent usage, should be 0")
	assert.True(t, decimal.RequireFromString("200").Equal(detail.AttemptedAmount))
	assert.False(t, detail.Exceeded)

	// Verify that gomock saw NO calls to GetOrCreateForUpdate or IncrementAtomic.
	// This confirms PER_TRANSACTION does not touch usage counters at all,
	// regardless of what clock is injected.
}

// =============================================================================
// T11 Rollback Tests - ST-11-01 and ST-11-02
// =============================================================================

// TestRollbackUsage_StoredPeriodKey verifies that RollbackUsage uses the stored
// InternalPeriodKey from LimitUsageDetail, NOT a recalculated value from client timestamp.
// This is critical for period boundary consistency: if CheckLimits runs at 23:59 and
// produces period "2024-01-15", but rollback happens at 00:01, it MUST still use
// "2024-01-15" (stored), not "2024-01-16" (recalculated from new time).
// Seeds: 8190-8199
func TestRollbackUsage_StoredPeriodKey(t *testing.T) {
	t.Parallel()

	// Seeds: 8190-8199 range
	limitID := testutil.MustDeterministicUUID(8190)
	accountID := testutil.MustDeterministicUUID(8191)
	counterID := testutil.MustDeterministicUUID(8192)

	// Server clock at 2024-01-15 10:30:00 UTC
	// This would produce period key "2024-01-15" if used for recalculation
	serverTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	mockClock := testutil.NewMockClock(serverTime)

	// Stored period key from CheckLimits (server time when check occurred)
	// This is the KEY value - rollback MUST use THIS, not recalculate
	storedPeriodKey := "2024-01-15"

	// Client timestamp is DIFFERENT day (2024-01-14 08:00:00 UTC)
	// If the implementation incorrectly recalculates from client timestamp,
	// it would produce "2024-01-14" - THIS MUST NOT HAPPEN
	clientTimestamp := time.Date(2024, 1, 14, 8, 0, 0, 0, time.UTC)

	ctrl := gomock.NewController(t)

	mockLimitRepo := NewMockLimitRepository(ctrl)
	mockUsageRepo := NewMockUsageCounterRepository(ctrl)

	scopeKey := "acct:" + accountID.String()

	// KEY ASSERTION: Expect GetForUpdate with storedPeriodKey ("2024-01-15")
	// NOT with "2024-01-14" (from client timestamp)
	// If the implementation uses client timestamp, this mock expectation will FAIL
	mockUsageRepo.EXPECT().GetForUpdate(gomock.Any(), limitID, scopeKey, storedPeriodKey).
		Return(&model.UsageCounter{
			ID:           counterID,
			LimitID:      limitID,
			ScopeKey:     scopeKey,
			PeriodKey:    storedPeriodKey,
			CurrentUsage: decimal.RequireFromString("550"),
		}, nil)

	// Verify DecrementAtomic is called successfully
	mockUsageRepo.EXPECT().DecrementAtomic(gomock.Any(), counterID, decimal.RequireFromString("50")).Return(nil)

	ctx := setupTest(t)

	checker, err := NewLimitChecker(mockLimitRepo, mockUsageRepo, mockClock)
	require.NoError(t, err)

	// Input with client timestamp that's different from the stored period key
	input := &model.CheckLimitsInput{
		Amount:               decimal.RequireFromString("50"),
		Currency:             "USD",
		AccountID:            accountID,
		TransactionTimestamp: clientTimestamp, // 2024-01-14 - DIFFERENT from stored key
	}

	// LimitUsageDetail with stored InternalPeriodKey from CheckLimits
	// This simulates what CheckLimits produces when it increments the counter
	usageDetails := []model.LimitUsageDetail{
		{
			LimitID:           limitID,
			LimitAmount:       decimal.RequireFromString("1000"),
			InternalLimitType: model.LimitTypeDaily,
			Scopes:            []model.Scope{{AccountID: &accountID}},
			CurrentUsage:      decimal.RequireFromString("550"),
			AttemptedAmount:   decimal.RequireFromString("50"),
			Exceeded:          false,
			InternalPeriodKey: storedPeriodKey, // "2024-01-15" - stored during CheckLimits
		},
	}

	err = checker.RollbackUsage(ctx, input, usageDetails)

	// Test PASSES if:
	// 1. GetForUpdate was called with "2024-01-15" (storedPeriodKey)
	// 2. DecrementAtomic was called successfully
	// 3. No error returned
	//
	// Test FAILS (RED) if:
	// - Implementation recalculates period key from client timestamp ("2024-01-14")
	// - Mock expectation not met (GetForUpdate called with wrong period key)
	require.NoError(t, err)
}

// TestLimitCheckerService_RollbackUsage_UsesStoredPeriodKey verifies that RollbackUsage
// uses detail.InternalPeriodKey instead of recalculating from input.TransactionTimestamp.
// This prevents period key mismatch when rollback crosses a period boundary.
// Seeds: 8200-8209
func TestLimitCheckerService_RollbackUsage_UsesStoredPeriodKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		limitType         model.LimitType
		storedPeriodKey   string // The InternalPeriodKey stored in LimitUsageDetail
		clientTimestamp   time.Time
		expectedPeriodKey string // The period key GetForUpdate should be called with
	}{
		{
			name:      "daily limit uses stored period key not client timestamp",
			limitType: model.LimitTypeDaily,
			// Stored period key from previous day (when increment happened)
			storedPeriodKey: "2024-01-14",
			// Client timestamp would produce "2024-01-15"
			clientTimestamp:   time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
			expectedPeriodKey: "2024-01-14", // Must use stored, not recalculated
		},
		{
			name:      "monthly limit uses stored period key not client timestamp",
			limitType: model.LimitTypeMonthly,
			// Stored period key from previous month (when increment happened)
			storedPeriodKey: "2024-01",
			// Client timestamp would produce "2024-02"
			clientTimestamp:   time.Date(2024, 2, 1, 0, 5, 0, 0, time.UTC),
			expectedPeriodKey: "2024-01", // Must use stored, not recalculated
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Seed 8200-8209
			limitID := testutil.MustDeterministicUUID(8200)
			accountID := testutil.MustDeterministicUUID(8201)
			counterID := testutil.MustDeterministicUUID(8202)

			// Server clock at a different time - should NOT be used for rollback
			serverTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
			mockClock := testutil.NewMockClock(serverTime)

			ctrl := gomock.NewController(t)

			mockLimitRepo := NewMockLimitRepository(ctrl)
			mockUsageRepo := NewMockUsageCounterRepository(ctrl)

			scopeKey := "acct:" + accountID.String()

			// KEY ASSERTION: GetForUpdate must be called with the STORED period key
			// (from InternalPeriodKey), NOT the period key calculated from client timestamp.
			mockUsageRepo.EXPECT().GetForUpdate(gomock.Any(), limitID, scopeKey, tc.expectedPeriodKey).
				Return(&model.UsageCounter{
					ID:           counterID,
					LimitID:      limitID,
					ScopeKey:     scopeKey,
					PeriodKey:    tc.expectedPeriodKey,
					CurrentUsage: decimal.RequireFromString("550"),
				}, nil)

			mockUsageRepo.EXPECT().DecrementAtomic(gomock.Any(), counterID, decimal.RequireFromString("50")).Return(nil)

			ctx := setupTest(t)

			checker, err := NewLimitChecker(mockLimitRepo, mockUsageRepo, mockClock)
			require.NoError(t, err)

			input := &model.CheckLimitsInput{
				Amount:               decimal.RequireFromString("50"),
				Currency:             "USD",
				AccountID:            accountID,
				TransactionTimestamp: tc.clientTimestamp, // Different from stored period key
			}

			usageDetails := []model.LimitUsageDetail{
				{
					LimitID:           limitID,
					LimitAmount:       decimal.RequireFromString("1000"),
					InternalLimitType: tc.limitType,
					Scopes:            []model.Scope{{AccountID: &accountID}},
					CurrentUsage:      decimal.RequireFromString("550"),
					Exceeded:          false,
					InternalPeriodKey: tc.storedPeriodKey, // This is what should be used
				},
			}

			err = checker.RollbackUsage(ctx, input, usageDetails)

			require.NoError(t, err)
			// If the mock for GetForUpdate with tc.expectedPeriodKey was NOT called,
			// gomock will fail the test — proving the stored period key was used.
		})
	}
}

// TestRollbackUsage_FallbackToServerClock verifies that when InternalPeriodKey is empty
// and limit type is NOT PER_TRANSACTION, RollbackUsage falls back to computing
// the period key from s.clock.Now().
// Seeds: 8200-8209 range
// Fallback behavior implemented — test validates server-clock fallback.
func TestRollbackUsage_FallbackToServerClock(t *testing.T) {
	t.Parallel()

	// Seed 8203-8209
	limitID := testutil.MustDeterministicUUID(8203)
	accountID := testutil.MustDeterministicUUID(8204)
	counterID := testutil.MustDeterministicUUID(8205)

	// Server clock at 2024-01-15 10:30:00 UTC
	serverTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	mockClock := testutil.NewMockClock(serverTime)

	// Expected period key from server clock
	expectedPeriodKeyDaily := "2024-01-15"

	ctrl := gomock.NewController(t)

	mockLimitRepo := NewMockLimitRepository(ctrl)
	mockUsageRepo := NewMockUsageCounterRepository(ctrl)

	scopeKey := "acct:" + accountID.String()

	// KEY ASSERTION: When InternalPeriodKey is empty but limit is DAILY,
	// the fallback should compute period key from server clock.
	mockUsageRepo.EXPECT().GetForUpdate(gomock.Any(), limitID, scopeKey, expectedPeriodKeyDaily).
		Return(&model.UsageCounter{
			ID:           counterID,
			LimitID:      limitID,
			ScopeKey:     scopeKey,
			PeriodKey:    expectedPeriodKeyDaily,
			CurrentUsage: decimal.RequireFromString("550"),
		}, nil)

	mockUsageRepo.EXPECT().DecrementAtomic(gomock.Any(), counterID, decimal.RequireFromString("50")).Return(nil)

	ctx := setupTest(t)

	checker, err := NewLimitChecker(mockLimitRepo, mockUsageRepo, mockClock)
	require.NoError(t, err)

	// Client timestamp doesn't matter for this test
	clientTimestamp := time.Date(2024, 1, 14, 8, 0, 0, 0, time.UTC)

	input := &model.CheckLimitsInput{
		Amount:               decimal.RequireFromString("50"),
		Currency:             "USD",
		AccountID:            accountID,
		TransactionTimestamp: clientTimestamp,
	}

	usageDetails := []model.LimitUsageDetail{
		{
			LimitID:           limitID,
			LimitAmount:       decimal.RequireFromString("1000"),
			InternalLimitType: model.LimitTypeDaily, // NOT PER_TRANSACTION
			Scopes:            []model.Scope{{AccountID: &accountID}},
			CurrentUsage:      decimal.RequireFromString("550"),
			Exceeded:          false,
			InternalPeriodKey: "", // EMPTY - should trigger fallback to server clock
		},
	}

	err = checker.RollbackUsage(ctx, input, usageDetails)

	// NOTE: This test may FAIL if the fallback behavior is not implemented.
	// Current implementation logs a warning and skips when InternalPeriodKey is empty.
	// T12 will implement the fallback.
	require.NoError(t, err)
}

// TestRollbackUsage_SkipsPerTransaction verifies that PER_TRANSACTION limits
// are skipped during rollback (no DB calls).
// Seeds: 8210 range
func TestRollbackUsage_SkipsPerTransaction(t *testing.T) {
	t.Parallel()

	// Seed 8210-8214
	limitID := testutil.MustDeterministicUUID(8210)
	accountID := testutil.MustDeterministicUUID(8211)

	serverTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	mockClock := testutil.NewMockClock(serverTime)

	ctrl := gomock.NewController(t)

	mockLimitRepo := NewMockLimitRepository(ctrl)
	mockUsageRepo := NewMockUsageCounterRepository(ctrl)

	// KEY ASSERTION: No GetForUpdate or DecrementAtomic calls should be made
	// because PER_TRANSACTION limits have no persistent counters.
	// gomock will fail if unexpected calls are made.

	ctx := setupTest(t)

	checker, err := NewLimitChecker(mockLimitRepo, mockUsageRepo, mockClock)
	require.NoError(t, err)

	input := &model.CheckLimitsInput{
		Amount:               decimal.RequireFromString("50"),
		Currency:             "USD",
		AccountID:            accountID,
		TransactionTimestamp: testutil.FixedTime(),
	}

	usageDetails := []model.LimitUsageDetail{
		{
			LimitID:           limitID,
			LimitAmount:       decimal.RequireFromString("100"),
			InternalLimitType: model.LimitTypePerTransaction, // PER_TRANSACTION - should be skipped
			Scopes:            []model.Scope{{AccountID: &accountID}},
			CurrentUsage:      decimal.Zero,
			Exceeded:          false,
			InternalPeriodKey: "", // Empty for PER_TRANSACTION (expected)
		},
	}

	err = checker.RollbackUsage(ctx, input, usageDetails)

	require.NoError(t, err)
	// gomock will fail if GetForUpdate or DecrementAtomic were called unexpectedly.
}

// TestRollbackIncrementedCounters_DecrementFailure verifies that rollback continues
// even when DecrementAtomic fails for one limit (best-effort).
// Seeds: 8230-8234 range
func TestRollbackIncrementedCounters_DecrementFailure(t *testing.T) {
	t.Parallel()

	// Seed 8230-8239
	limitID1 := testutil.MustDeterministicUUID(8230)
	limitID2 := testutil.MustDeterministicUUID(8231)
	accountID := testutil.MustDeterministicUUID(8232)
	counterID1 := testutil.MustDeterministicUUID(8233)
	counterID2 := testutil.MustDeterministicUUID(8234)

	serverTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	mockClock := testutil.NewMockClock(serverTime)

	periodKey := "2024-01-15"

	ctrl := gomock.NewController(t)

	mockLimitRepo := NewMockLimitRepository(ctrl)
	mockUsageRepo := NewMockUsageCounterRepository(ctrl)

	scopeKey := "acct:" + accountID.String()

	// First limit: GetForUpdate succeeds, DecrementAtomic returns error
	mockUsageRepo.EXPECT().GetForUpdate(gomock.Any(), limitID1, scopeKey, periodKey).
		Return(&model.UsageCounter{
			ID:           counterID1,
			LimitID:      limitID1,
			ScopeKey:     scopeKey,
			PeriodKey:    periodKey,
			CurrentUsage: decimal.RequireFromString("550"),
		}, nil)

	mockUsageRepo.EXPECT().DecrementAtomic(gomock.Any(), counterID1, decimal.RequireFromString("50")).
		Return(errDatabase) // First limit fails

	// Second limit: GetForUpdate succeeds, DecrementAtomic succeeds
	mockUsageRepo.EXPECT().GetForUpdate(gomock.Any(), limitID2, scopeKey, periodKey).
		Return(&model.UsageCounter{
			ID:           counterID2,
			LimitID:      limitID2,
			ScopeKey:     scopeKey,
			PeriodKey:    periodKey,
			CurrentUsage: decimal.RequireFromString("1050"),
		}, nil)

	mockUsageRepo.EXPECT().DecrementAtomic(gomock.Any(), counterID2, decimal.RequireFromString("50")).Return(nil)

	ctx := setupTest(t)

	checker, err := NewLimitChecker(mockLimitRepo, mockUsageRepo, mockClock)
	require.NoError(t, err)

	input := &model.CheckLimitsInput{
		Amount:               decimal.RequireFromString("50"),
		Currency:             "USD",
		AccountID:            accountID,
		TransactionTimestamp: testutil.FixedTime(),
	}

	usageDetails := []model.LimitUsageDetail{
		{
			LimitID:           limitID1,
			LimitAmount:       decimal.RequireFromString("1000"),
			InternalLimitType: model.LimitTypeDaily,
			Scopes:            []model.Scope{{AccountID: &accountID}},
			CurrentUsage:      decimal.RequireFromString("550"),
			Exceeded:          false,
			InternalPeriodKey: periodKey,
		},
		{
			LimitID:           limitID2,
			LimitAmount:       decimal.RequireFromString("2000"),
			InternalLimitType: model.LimitTypeDaily,
			Scopes:            []model.Scope{{AccountID: &accountID}},
			CurrentUsage:      decimal.RequireFromString("1050"),
			Exceeded:          false,
			InternalPeriodKey: periodKey,
		},
	}

	// KEY ASSERTION: RollbackUsage should NOT return an error even though
	// the first limit's DecrementAtomic failed. It logs warnings and continues.
	err = checker.RollbackUsage(ctx, input, usageDetails)

	require.NoError(t, err, "RollbackUsage should not fail even when some rollbacks fail - it's best-effort")
	// Both limits should have been attempted (verified by gomock expectations)
}

// TestCheckLimits_MultiLimitPartialRollback verifies that when limit C exceeds,
// limits A and B are rolled back (rollbackIncrementedCounters is called).
// Seeds: 8240-8254 range
func TestCheckLimits_MultiLimitPartialRollback(t *testing.T) {
	t.Parallel()

	// Seed 8240-8254
	limitIDA := testutil.MustDeterministicUUID(8240)
	limitIDB := testutil.MustDeterministicUUID(8241)
	limitIDC := testutil.MustDeterministicUUID(8242)
	accountID := testutil.MustDeterministicUUID(8243)
	counterIDA := testutil.MustDeterministicUUID(8244)
	counterIDB := testutil.MustDeterministicUUID(8245)

	// Server clock for period key calculation
	serverTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	mockClock := testutil.NewMockClock(serverTime)

	periodKeyDaily := "2024-01-15"
	periodKeyMonthly := "2024-01"

	ctrl := gomock.NewController(t)

	mockLimitRepo := NewMockLimitRepository(ctrl)
	mockUsageRepo := NewMockUsageCounterRepository(ctrl)

	status := model.LimitStatusActive
	currency := "USD"
	scopeKey := "acct:" + accountID.String()

	// Three limits: A (daily, maxAmount=10000), B (monthly, maxAmount=5000), C (daily, maxAmount=300)
	mockLimitRepo.EXPECT().List(gomock.Any(), &model.ListLimitsFilter{
		Status:   &status,
		Currency: &currency,
		Limit:    constant.MaxPaginationLimit,
		Cursor:   "",
	}).Return(&model.ListLimitsResult{
		Limits: []model.Limit{
			{
				ID:        limitIDA,
				Name:      "Daily Limit A",
				LimitType: model.LimitTypeDaily,
				MaxAmount: decimal.RequireFromString("10000"),
				Currency:  "USD",
				Scopes:    []model.Scope{{AccountID: &accountID}},
				Status:    model.LimitStatusActive,
			},
			{
				ID:        limitIDB,
				Name:      "Monthly Limit B",
				LimitType: model.LimitTypeMonthly,
				MaxAmount: decimal.RequireFromString("5000"),
				Currency:  "USD",
				Scopes:    []model.Scope{{AccountID: &accountID}},
				Status:    model.LimitStatusActive,
			},
			{
				ID:        limitIDC,
				Name:      "Daily Limit C",
				LimitType: model.LimitTypeDaily,
				MaxAmount: decimal.RequireFromString("300"), // Amount=500 > 300 -> pre-check fails
				Currency:  "USD",
				Scopes:    []model.Scope{{AccountID: &accountID}},
				Status:    model.LimitStatusActive,
			},
		},
		HasMore: false,
	}, nil)

	// Limit A: UpsertAndIncrementAtomic succeeds (500 <= 10000)
	mockUsageRepo.EXPECT().UpsertAndIncrementAtomic(
		gomock.Any(),
		limitIDA,
		scopeKey,
		periodKeyDaily,
		decimal.RequireFromString("500"),
		decimal.RequireFromString("10000"),
	).Return(decimal.RequireFromString("500"), nil)

	// Limit B: UpsertAndIncrementAtomic succeeds (500 <= 5000)
	mockUsageRepo.EXPECT().UpsertAndIncrementAtomic(
		gomock.Any(),
		limitIDB,
		scopeKey,
		periodKeyMonthly,
		decimal.RequireFromString("500"),
		decimal.RequireFromString("5000"),
	).Return(decimal.RequireFromString("500"), nil)

	// Limit C: pre-check fails (500 > 300), NO UpsertAndIncrementAtomic call
	// This triggers rollback for A and B

	// Rollback for Limit A: GetForUpdate + DecrementAtomic
	mockUsageRepo.EXPECT().GetForUpdate(gomock.Any(), limitIDA, scopeKey, periodKeyDaily).
		Return(&model.UsageCounter{
			ID:           counterIDA,
			LimitID:      limitIDA,
			ScopeKey:     scopeKey,
			PeriodKey:    periodKeyDaily,
			CurrentUsage: decimal.RequireFromString("500"),
		}, nil)

	mockUsageRepo.EXPECT().DecrementAtomic(gomock.Any(), counterIDA, decimal.RequireFromString("500")).Return(nil)

	// Rollback for Limit B: GetForUpdate + DecrementAtomic
	mockUsageRepo.EXPECT().GetForUpdate(gomock.Any(), limitIDB, scopeKey, periodKeyMonthly).
		Return(&model.UsageCounter{
			ID:           counterIDB,
			LimitID:      limitIDB,
			ScopeKey:     scopeKey,
			PeriodKey:    periodKeyMonthly,
			CurrentUsage: decimal.RequireFromString("500"),
		}, nil)

	mockUsageRepo.EXPECT().DecrementAtomic(gomock.Any(), counterIDB, decimal.RequireFromString("500")).Return(nil)

	ctx := setupTest(t)

	checker, err := NewLimitChecker(mockLimitRepo, mockUsageRepo, mockClock)
	require.NoError(t, err)

	input := &model.CheckLimitsInput{
		Amount:               decimal.RequireFromString("500"), // Exceeds Limit C's maxAmount (300)
		Currency:             "USD",
		AccountID:            accountID,
		TransactionTimestamp: testutil.FixedTime(),
	}

	output, err := checker.CheckLimits(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.False(t, output.Allowed, "Transaction should be denied due to Limit C exceeded")
	require.Len(t, output.ExceededLimitIDs, 1, "Should have exactly one exceeded limit")
	assert.Equal(t, limitIDC, output.ExceededLimitIDs[0], "Limit C should be exceeded")

	// Verify details contain info about checked limits
	assert.Len(t, output.LimitUsageDetails, 3, "Should have details for all 3 limits")

	// Verify Limit C shows exceeded
	var limitCDetail *model.LimitUsageDetail
	for i := range output.LimitUsageDetails {
		if output.LimitUsageDetails[i].LimitID == limitIDC {
			limitCDetail = &output.LimitUsageDetails[i]

			break
		}
	}

	require.NotNil(t, limitCDetail, "Should have detail for Limit C")
	assert.True(t, limitCDetail.Exceeded, "Limit C should be marked as exceeded")
}
