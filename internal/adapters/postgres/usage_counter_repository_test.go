// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/shopspring/decimal"

	"tracer/internal/adapters/postgres/db/mocks"
	"tracer/internal/testutil"
	"tracer/pkg/constant"
	"tracer/pkg/model"
)

// setupUsageCounterRepositoryMockDB creates a gomock controller, mock DBConnection, and sqlmock for testing.
func setupUsageCounterRepositoryMockDB(t *testing.T) (*UsageCounterRepository, sqlmock.Sqlmock, func()) {
	t.Helper()

	ctrl := gomock.NewController(t)
	db, sqlMock, err := sqlmock.New()
	require.NoError(t, err)

	mockConn := mocks.NewMockConnection(ctrl)
	mockConn.EXPECT().GetDB().Return(db, nil).AnyTimes()

	repo := NewUsageCounterRepositoryWithConnection(mockConn)

	cleanup := func() {
		if err := db.Close(); err != nil {
			t.Logf("failed to close mock db: %v", err)
		}
	}

	return repo, sqlMock, cleanup
}

// usageCounterColumns returns the column names for usage counter queries.
func usageCounterColumns() []string {
	return []string{"id", "limit_id", "scope_key", "period_key", "current_usage", "last_updated_at"}
}

// testUsageCounter creates a test usage counter with default values.
func testUsageCounter(limitID uuid.UUID) *model.UsageCounter {
	return &model.UsageCounter{
		ID:            testutil.MustDeterministicUUID(1),
		LimitID:       limitID,
		ScopeKey:      "acct:123",
		PeriodKey:     "2025-01",
		CurrentUsage:  decimal.RequireFromString("50"),
		LastUpdatedAt: testutil.DefaultTestTime,
	}
}

func TestUsageCounterRepository_GetOrCreateForUpdate_ConnectionError(t *testing.T) {
	testutil.SetupTestTracing(t)

	ctrl := gomock.NewController(t)

	mockConn := mocks.NewMockConnection(ctrl)
	mockConn.EXPECT().GetDB().Return(nil, errors.New("connection refused"))

	repo := NewUsageCounterRepositoryWithConnection(mockConn)

	ctx := context.Background()
	limitID := testutil.MustDeterministicUUID(999)

	_, err := repo.GetOrCreateForUpdate(ctx, limitID, "acct:123", "2025-01")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get database connection")
}

func TestUsageCounterRepository_GetOrCreateForUpdate(t *testing.T) {
	testutil.SetupTestTracing(t)

	limitID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")

	tests := []struct {
		name      string
		limitID   uuid.UUID
		scopeKey  string
		periodKey string
		mockSetup func(mock sqlmock.Sqlmock)
		wantErr   bool
		errMsg    string
		validate  func(t *testing.T, counter *model.UsageCounter)
	}{
		{
			name:      "Success - finds existing counter",
			limitID:   limitID,
			scopeKey:  "acct:123",
			periodKey: "2025-01",
			mockSetup: func(mock sqlmock.Sqlmock) {
				counter := testUsageCounter(limitID)
				rows := sqlmock.NewRows(usageCounterColumns()).
					AddRow(counter.ID, counter.LimitID, counter.ScopeKey, counter.PeriodKey, counter.CurrentUsage, counter.LastUpdatedAt)

				mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, limit_id, scope_key, period_key, current_usage, last_updated_at FROM usage_counters WHERE limit_id = $1 AND period_key = $2 AND scope_key = $3 FOR UPDATE`)).
					WithArgs(limitID, "2025-01", "acct:123").
					WillReturnRows(rows)
			},
			validate: func(t *testing.T, counter *model.UsageCounter) {
				assert.Equal(t, limitID, counter.LimitID)
				assert.Equal(t, "acct:123", counter.ScopeKey)
				assert.Equal(t, "2025-01", counter.PeriodKey)
				assert.True(t, decimal.RequireFromString("50").Equal(counter.CurrentUsage))
			},
		},
		{
			name:      "Success - creates new counter when not found",
			limitID:   limitID,
			scopeKey:  "acct:456",
			periodKey: "2025-02",
			mockSetup: func(mock sqlmock.Sqlmock) {
				// First query returns no rows
				mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, limit_id, scope_key, period_key, current_usage, last_updated_at FROM usage_counters WHERE limit_id = $1 AND period_key = $2 AND scope_key = $3 FOR UPDATE`)).
					WithArgs(limitID, "2025-02", "acct:456").
					WillReturnError(sql.ErrNoRows)

				// Insert succeeds
				mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO usage_counters`)).
					WithArgs(sqlmock.AnyArg(), limitID, "acct:456", "2025-02", decimal.RequireFromString("0"), sqlmock.AnyArg()).
					WillReturnResult(sqlmock.NewResult(1, 1))

				// Post-insert SELECT to acquire FOR UPDATE lock and return the inserted row
				rows := sqlmock.NewRows(usageCounterColumns()).
					AddRow(testutil.MustDeterministicUUID(10), limitID, "acct:456", "2025-02", decimal.RequireFromString("0"), testutil.DefaultTestTime)
				mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, limit_id, scope_key, period_key, current_usage, last_updated_at FROM usage_counters WHERE id = $1 FOR UPDATE`)).
					WithArgs(sqlmock.AnyArg()).
					WillReturnRows(rows)
			},
			validate: func(t *testing.T, counter *model.UsageCounter) {
				assert.Equal(t, limitID, counter.LimitID)
				assert.Equal(t, "acct:456", counter.ScopeKey)
				assert.Equal(t, "2025-02", counter.PeriodKey)
				assert.True(t, decimal.RequireFromString("0").Equal(counter.CurrentUsage))
			},
		},
		{
			name:      "Success - handles concurrent insert by retrying select",
			limitID:   limitID,
			scopeKey:  "acct:789",
			periodKey: "2025-03",
			mockSetup: func(mock sqlmock.Sqlmock) {
				// First query returns no rows
				mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, limit_id, scope_key, period_key, current_usage, last_updated_at FROM usage_counters WHERE limit_id = $1 AND period_key = $2 AND scope_key = $3 FOR UPDATE`)).
					WithArgs(limitID, "2025-03", "acct:789").
					WillReturnError(sql.ErrNoRows)

				// Insert fails due to concurrent insert (unique constraint violation - SQLSTATE 23505)
				mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO usage_counters`)).
					WithArgs(sqlmock.AnyArg(), limitID, "acct:789", "2025-03", decimal.RequireFromString("0"), sqlmock.AnyArg()).
					WillReturnError(&pgconn.PgError{Code: "23505", Message: "duplicate key value violates unique constraint"})

				// Retry select succeeds
				counter := &model.UsageCounter{
					ID:            testutil.MustDeterministicUUID(2),
					LimitID:       limitID,
					ScopeKey:      "acct:789",
					PeriodKey:     "2025-03",
					CurrentUsage:  decimal.RequireFromString("1"),
					LastUpdatedAt: testutil.DefaultTestTime,
				}
				rows := sqlmock.NewRows(usageCounterColumns()).
					AddRow(counter.ID, counter.LimitID, counter.ScopeKey, counter.PeriodKey, counter.CurrentUsage, counter.LastUpdatedAt)

				mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, limit_id, scope_key, period_key, current_usage, last_updated_at FROM usage_counters WHERE limit_id = $1 AND period_key = $2 AND scope_key = $3 FOR UPDATE`)).
					WithArgs(limitID, "2025-03", "acct:789").
					WillReturnRows(rows)
			},
			validate: func(t *testing.T, counter *model.UsageCounter) {
				assert.Equal(t, limitID, counter.LimitID)
				assert.Equal(t, "acct:789", counter.ScopeKey)
				assert.Equal(t, "2025-03", counter.PeriodKey)
				assert.True(t, decimal.RequireFromString("1").Equal(counter.CurrentUsage))
			},
		},
		{
			name:      "Error - insert fails with non-unique-constraint error",
			limitID:   limitID,
			scopeKey:  "acct:fail",
			periodKey: "2025-04",
			mockSetup: func(mock sqlmock.Sqlmock) {
				// First query returns no rows
				mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, limit_id, scope_key, period_key, current_usage, last_updated_at FROM usage_counters WHERE limit_id = $1 AND period_key = $2 AND scope_key = $3 FOR UPDATE`)).
					WithArgs(limitID, "2025-04", "acct:fail").
					WillReturnError(sql.ErrNoRows)

				// Insert fails with non-unique-constraint error (should NOT retry)
				mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO usage_counters`)).
					WithArgs(sqlmock.AnyArg(), limitID, "acct:fail", "2025-04", decimal.RequireFromString("0"), sqlmock.AnyArg()).
					WillReturnError(errors.New("disk full"))
			},
			wantErr: true,
			errMsg:  "failed to insert usage counter",
		},
		{
			name:      "Error - query fails",
			limitID:   limitID,
			scopeKey:  "acct:123",
			periodKey: "2025-01",
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, limit_id, scope_key, period_key, current_usage, last_updated_at FROM usage_counters`)).
					WithArgs(limitID, "2025-01", "acct:123").
					WillReturnError(errors.New("database error"))
			},
			wantErr: true,
			errMsg:  "failed to get usage counter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, sqlMock, cleanup := setupUsageCounterRepositoryMockDB(t)
			defer cleanup()

			tt.mockSetup(sqlMock)

			ctx := context.Background()
			counter, err := repo.GetOrCreateForUpdate(ctx, tt.limitID, tt.scopeKey, tt.periodKey)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, counter)

			if tt.validate != nil {
				tt.validate(t, counter)
			}
		})
	}
}

func TestUsageCounterRepository_IncrementAtomic_ConnectionError(t *testing.T) {
	testutil.SetupTestTracing(t)

	ctrl := gomock.NewController(t)

	mockConn := mocks.NewMockConnection(ctrl)
	mockConn.EXPECT().GetDB().Return(nil, errors.New("connection refused"))

	repo := NewUsageCounterRepositoryWithConnection(mockConn)

	ctx := context.Background()
	err := repo.IncrementAtomic(ctx, testutil.MustDeterministicUUID(998), decimal.RequireFromString("1"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get database connection")
}

func TestUsageCounterRepository_IncrementAtomic(t *testing.T) {
	testutil.SetupTestTracing(t)

	counterID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440001")

	tests := []struct {
		name      string
		counterID uuid.UUID
		amount    decimal.Decimal
		mockSetup func(mock sqlmock.Sqlmock)
		wantErr   bool
		errVal    error
		errMsg    string
	}{
		{
			name:      "Success - increments counter",
			counterID: counterID,
			amount:    decimal.RequireFromString("5"),
			mockSetup: func(mock sqlmock.Sqlmock) {
				// Simple UPDATE: current_usage = current_usage + amount
				mock.ExpectExec(regexp.QuoteMeta(`UPDATE usage_counters SET`)).
					WithArgs(decimal.RequireFromString("5"), sqlmock.AnyArg(), counterID).
					WillReturnResult(sqlmock.NewResult(0, 1))
			},
		},
		{
			name:      "Success - zero amount is no-op",
			counterID: counterID,
			amount:    decimal.RequireFromString("0"),
			mockSetup: func(mock sqlmock.Sqlmock) {
				// No database calls expected
			},
		},
		{
			name:      "Error - negative amount",
			counterID: counterID,
			amount:    decimal.RequireFromString("-1"),
			mockSetup: func(mock sqlmock.Sqlmock) {
				// No database calls expected
			},
			wantErr: true,
			errVal:  constant.ErrUsageCounterIncrementNonNegative,
		},
		{
			name:      "Error - counter not found (0 rows on UPDATE)",
			counterID: counterID,
			amount:    decimal.RequireFromString("1"),
			mockSetup: func(mock sqlmock.Sqlmock) {
				// UPDATE returns 0 rows (counter doesn't exist)
				mock.ExpectExec(regexp.QuoteMeta(`UPDATE usage_counters SET`)).
					WithArgs(decimal.RequireFromString("1"), sqlmock.AnyArg(), counterID).
					WillReturnResult(sqlmock.NewResult(0, 0))
			},
			wantErr: true,
			errVal:  constant.ErrUsageCounterNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, sqlMock, cleanup := setupUsageCounterRepositoryMockDB(t)
			defer cleanup()

			tt.mockSetup(sqlMock)

			ctx := context.Background()
			err := repo.IncrementAtomic(ctx, tt.counterID, tt.amount)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errVal != nil {
					assert.ErrorIs(t, err, tt.errVal)
				}
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestUsageCounterRepository_DecrementAtomic_ConnectionError(t *testing.T) {
	testutil.SetupTestTracing(t)

	ctrl := gomock.NewController(t)

	mockConn := mocks.NewMockConnection(ctrl)
	mockConn.EXPECT().GetDB().Return(nil, errors.New("connection refused"))

	repo := NewUsageCounterRepositoryWithConnection(mockConn)

	ctx := context.Background()
	err := repo.DecrementAtomic(ctx, testutil.MustDeterministicUUID(997), decimal.RequireFromString("1"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get database connection")
}

func TestUsageCounterRepository_DecrementAtomic(t *testing.T) {
	testutil.SetupTestTracing(t)

	counterID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440001")

	tests := []struct {
		name      string
		counterID uuid.UUID
		amount    decimal.Decimal
		mockSetup func(mock sqlmock.Sqlmock)
		wantErr   bool
		errVal    error
	}{
		{
			name:      "Success - decrements counter",
			counterID: counterID,
			amount:    decimal.RequireFromString("5"),
			mockSetup: func(mock sqlmock.Sqlmock) {
				// Atomic conditional UPDATE with WHERE current_usage >= amount
				mock.ExpectExec(regexp.QuoteMeta(`UPDATE usage_counters SET`)).
					WithArgs(decimal.RequireFromString("5"), sqlmock.AnyArg(), counterID, decimal.RequireFromString("5")).
					WillReturnResult(sqlmock.NewResult(0, 1))
			},
		},
		{
			name:      "Success - zero amount is no-op",
			counterID: counterID,
			amount:    decimal.RequireFromString("0"),
			mockSetup: func(mock sqlmock.Sqlmock) {
				// No database calls expected
			},
		},
		{
			name:      "Error - negative amount",
			counterID: counterID,
			amount:    decimal.RequireFromString("-1"),
			mockSetup: func(mock sqlmock.Sqlmock) {
				// No database calls expected
			},
			wantErr: true,
			errVal:  constant.ErrUsageCounterDecrementNonNegative,
		},
		{
			name:      "Error - counter not found",
			counterID: counterID,
			amount:    decimal.RequireFromString("1"),
			mockSetup: func(mock sqlmock.Sqlmock) {
				// UPDATE returns 0 rows (counter not found or insufficient balance)
				mock.ExpectExec(regexp.QuoteMeta(`UPDATE usage_counters SET`)).
					WithArgs(decimal.RequireFromString("1"), sqlmock.AnyArg(), counterID, decimal.RequireFromString("1")).
					WillReturnResult(sqlmock.NewResult(0, 0))

				// SELECT to distinguish: counter not found
				mock.ExpectQuery(regexp.QuoteMeta(`SELECT current_usage FROM usage_counters WHERE id = $1`)).
					WithArgs(counterID).
					WillReturnError(sql.ErrNoRows)
			},
			wantErr: true,
			errVal:  constant.ErrUsageCounterNotFound,
		},
		{
			name:      "Error - would result in negative usage",
			counterID: counterID,
			amount:    decimal.RequireFromString("10"),
			mockSetup: func(mock sqlmock.Sqlmock) {
				// UPDATE returns 0 rows (current_usage < amount)
				mock.ExpectExec(regexp.QuoteMeta(`UPDATE usage_counters SET`)).
					WithArgs(decimal.RequireFromString("10"), sqlmock.AnyArg(), counterID, decimal.RequireFromString("10")).
					WillReturnResult(sqlmock.NewResult(0, 0))

				// SELECT to distinguish: counter exists but insufficient balance
				rows := sqlmock.NewRows([]string{"current_usage"}).AddRow(decimal.RequireFromString("5"))
				mock.ExpectQuery(regexp.QuoteMeta(`SELECT current_usage FROM usage_counters WHERE id = $1`)).
					WithArgs(counterID).
					WillReturnRows(rows)
			},
			wantErr: true,
			errVal:  constant.ErrUsageCounterCurrentUsageNegative,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, sqlMock, cleanup := setupUsageCounterRepositoryMockDB(t)
			defer cleanup()

			tt.mockSetup(sqlMock)

			ctx := context.Background()
			err := repo.DecrementAtomic(ctx, tt.counterID, tt.amount)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errVal != nil {
					assert.ErrorIs(t, err, tt.errVal)
				}
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestUsageCounterRepository_GetByLimitID_ConnectionError(t *testing.T) {
	testutil.SetupTestTracing(t)

	ctrl := gomock.NewController(t)

	mockConn := mocks.NewMockConnection(ctrl)
	mockConn.EXPECT().GetDB().Return(nil, errors.New("connection refused"))

	repo := NewUsageCounterRepositoryWithConnection(mockConn)

	ctx := context.Background()
	_, err := repo.GetByLimitID(ctx, testutil.MustDeterministicUUID(996))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get database connection")
}

func TestUsageCounterRepository_GetByLimitID(t *testing.T) {
	testutil.SetupTestTracing(t)

	limitID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")

	tests := []struct {
		name      string
		limitID   uuid.UUID
		mockSetup func(mock sqlmock.Sqlmock)
		wantErr   bool
		errMsg    string
		wantCount int
	}{
		{
			name:    "Success - returns multiple counters",
			limitID: limitID,
			mockSetup: func(mock sqlmock.Sqlmock) {
				counter1 := testUsageCounter(limitID)
				counter2 := &model.UsageCounter{
					ID:            testutil.MustDeterministicUUID(3),
					LimitID:       limitID,
					ScopeKey:      "acct:456",
					PeriodKey:     "2025-01",
					CurrentUsage:  decimal.RequireFromString("25"),
					LastUpdatedAt: testutil.DefaultTestTime,
				}

				rows := sqlmock.NewRows(usageCounterColumns()).
					AddRow(counter1.ID, counter1.LimitID, counter1.ScopeKey, counter1.PeriodKey, counter1.CurrentUsage, counter1.LastUpdatedAt).
					AddRow(counter2.ID, counter2.LimitID, counter2.ScopeKey, counter2.PeriodKey, counter2.CurrentUsage, counter2.LastUpdatedAt)

				mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, limit_id, scope_key, period_key, current_usage, last_updated_at FROM usage_counters WHERE limit_id = $1 ORDER BY period_key DESC, scope_key ASC`)).
					WithArgs(limitID).
					WillReturnRows(rows)
			},
			wantCount: 2,
		},
		{
			name:    "Success - returns empty slice when no counters",
			limitID: limitID,
			mockSetup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows(usageCounterColumns())

				mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, limit_id, scope_key, period_key, current_usage, last_updated_at FROM usage_counters WHERE limit_id = $1 ORDER BY period_key DESC, scope_key ASC`)).
					WithArgs(limitID).
					WillReturnRows(rows)
			},
			wantCount: 0,
		},
		{
			name:    "Error - query fails",
			limitID: limitID,
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, limit_id, scope_key, period_key, current_usage, last_updated_at FROM usage_counters`)).
					WithArgs(limitID).
					WillReturnError(errors.New("database error"))
			},
			wantErr: true,
			errMsg:  "failed to get usage counters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, sqlMock, cleanup := setupUsageCounterRepositoryMockDB(t)
			defer cleanup()

			tt.mockSetup(sqlMock)

			ctx := context.Background()
			counters, err := repo.GetByLimitID(ctx, tt.limitID)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				return
			}

			require.NoError(t, err)
			assert.Len(t, counters, tt.wantCount)
		})
	}
}

func TestUsageCounterRepository_GetUsageForLimits_ConnectionError(t *testing.T) {
	testutil.SetupTestTracing(t)

	ctrl := gomock.NewController(t)

	mockConn := mocks.NewMockConnection(ctrl)
	mockConn.EXPECT().GetDB().Return(nil, errors.New("connection refused"))

	repo := NewUsageCounterRepositoryWithConnection(mockConn)

	ctx := context.Background()
	_, err := repo.GetUsageForLimits(ctx, []uuid.UUID{testutil.MustDeterministicUUID(995)}, "acct:123", "2025-01")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get database connection")
}

func TestUsageCounterRepository_GetUsageForLimits(t *testing.T) {
	testutil.SetupTestTracing(t)

	limitID1 := uuid.MustParse("550e8400-e29b-41d4-a716-446655440001")
	limitID2 := uuid.MustParse("550e8400-e29b-41d4-a716-446655440002")

	tests := []struct {
		name      string
		limitIDs  []uuid.UUID
		scopeKey  string
		periodKey string
		mockSetup func(mock sqlmock.Sqlmock)
		wantErr   bool
		errMsg    string
		validate  func(t *testing.T, result map[uuid.UUID]decimal.Decimal)
	}{
		{
			name:      "Success - returns usage for multiple limits",
			limitIDs:  []uuid.UUID{limitID1, limitID2},
			scopeKey:  "acct:123",
			periodKey: "2025-01",
			mockSetup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{"limit_id", "current_usage"}).
					AddRow(limitID1, decimal.RequireFromString("50")).
					AddRow(limitID2, decimal.RequireFromString("25"))

				mock.ExpectQuery(regexp.QuoteMeta(`SELECT limit_id, current_usage FROM usage_counters WHERE limit_id IN ($1,$2) AND period_key = $3 AND scope_key = $4`)).
					WithArgs(limitID1, limitID2, "2025-01", "acct:123").
					WillReturnRows(rows)
			},
			validate: func(t *testing.T, result map[uuid.UUID]decimal.Decimal) {
				assert.Len(t, result, 2)
				assert.True(t, decimal.RequireFromString("50").Equal(result[limitID1]))
				assert.True(t, decimal.RequireFromString("25").Equal(result[limitID2]))
			},
		},
		{
			name:      "Success - empty limit IDs returns empty map",
			limitIDs:  []uuid.UUID{},
			scopeKey:  "acct:123",
			periodKey: "2025-01",
			mockSetup: func(mock sqlmock.Sqlmock) {
				// No database calls expected
			},
			validate: func(t *testing.T, result map[uuid.UUID]decimal.Decimal) {
				assert.Len(t, result, 0)
			},
		},
		{
			name:      "Success - returns partial results (some limits have no counters)",
			limitIDs:  []uuid.UUID{limitID1, limitID2},
			scopeKey:  "acct:123",
			periodKey: "2025-01",
			mockSetup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{"limit_id", "current_usage"}).
					AddRow(limitID1, decimal.RequireFromString("50"))

				mock.ExpectQuery(regexp.QuoteMeta(`SELECT limit_id, current_usage FROM usage_counters WHERE limit_id IN ($1,$2) AND period_key = $3 AND scope_key = $4`)).
					WithArgs(limitID1, limitID2, "2025-01", "acct:123").
					WillReturnRows(rows)
			},
			validate: func(t *testing.T, result map[uuid.UUID]decimal.Decimal) {
				assert.Len(t, result, 1)
				assert.True(t, decimal.RequireFromString("50").Equal(result[limitID1]))
				_, exists := result[limitID2]
				assert.False(t, exists)
			},
		},
		{
			name:      "Error - query fails",
			limitIDs:  []uuid.UUID{limitID1},
			scopeKey:  "acct:123",
			periodKey: "2025-01",
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(regexp.QuoteMeta(`SELECT limit_id, current_usage FROM usage_counters`)).
					WithArgs(limitID1, "2025-01", "acct:123").
					WillReturnError(errors.New("database error"))
			},
			wantErr: true,
			errMsg:  "failed to get usage",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, sqlMock, cleanup := setupUsageCounterRepositoryMockDB(t)
			defer cleanup()

			tt.mockSetup(sqlMock)

			ctx := context.Background()
			result, err := repo.GetUsageForLimits(ctx, tt.limitIDs, tt.scopeKey, tt.periodKey)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, result)

			if tt.validate != nil {
				tt.validate(t, result)
			}
		})
	}
}

func TestUsageCounterRepository_DeleteExpiredCounters_ConnectionError(t *testing.T) {
	testutil.SetupTestTracing(t)

	ctrl := gomock.NewController(t)

	mockConn := mocks.NewMockConnection(ctrl)
	mockConn.EXPECT().GetDB().Return(nil, errors.New("connection refused"))

	repo := NewUsageCounterRepositoryWithConnection(mockConn)

	ctx := context.Background()
	_, err := repo.DeleteExpiredCounters(ctx, testutil.FixedTime())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get database connection")
}

func TestUsageCounterRepository_DeleteExpiredCounters(t *testing.T) {
	testutil.SetupTestTracing(t)

	// The batched delete query pattern used by the repository
	batchedDeleteQuery := `DELETE FROM usage_counters WHERE id IN (SELECT id FROM usage_counters WHERE last_updated_at < $1 LIMIT $2)`

	tests := []struct {
		name      string
		olderThan time.Time
		mockSetup func(mock sqlmock.Sqlmock)
		wantErr   bool
		errMsg    string
		wantCount int64
	}{
		{
			name:      "Success - deletes expired counters",
			olderThan: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			mockSetup: func(mock sqlmock.Sqlmock) {
				// First batch: delete 5 rows
				mock.ExpectExec(regexp.QuoteMeta(batchedDeleteQuery)).
					WithArgs(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), DefaultDeleteBatchSize).
					WillReturnResult(sqlmock.NewResult(0, 5))
				// Second batch: no more rows to delete, loop terminates
				mock.ExpectExec(regexp.QuoteMeta(batchedDeleteQuery)).
					WithArgs(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), DefaultDeleteBatchSize).
					WillReturnResult(sqlmock.NewResult(0, 0))
			},
			wantCount: 5,
		},
		{
			name:      "Success - no counters to delete",
			olderThan: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
			mockSetup: func(mock sqlmock.Sqlmock) {
				// First batch returns 0, loop terminates immediately
				mock.ExpectExec(regexp.QuoteMeta(batchedDeleteQuery)).
					WithArgs(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), DefaultDeleteBatchSize).
					WillReturnResult(sqlmock.NewResult(0, 0))
			},
			wantCount: 0,
		},
		{
			name:      "Error - delete fails",
			olderThan: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec(regexp.QuoteMeta(batchedDeleteQuery)).
					WithArgs(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), DefaultDeleteBatchSize).
					WillReturnError(errors.New("database error"))
			},
			wantErr: true,
			errMsg:  "failed to delete expired counters",
		},
		{
			name:      "Error - rows affected fails",
			olderThan: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec(regexp.QuoteMeta(batchedDeleteQuery)).
					WithArgs(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), DefaultDeleteBatchSize).
					WillReturnResult(sqlmock.NewErrorResult(errors.New("rows affected error")))
			},
			wantErr: true,
			errMsg:  "failed to get rows affected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, sqlMock, cleanup := setupUsageCounterRepositoryMockDB(t)
			defer cleanup()

			tt.mockSetup(sqlMock)

			ctx := context.Background()
			count, err := repo.DeleteExpiredCounters(ctx, tt.olderThan)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantCount, count)
		})
	}
}

func TestUsageCounterRepository_UpsertAndIncrementAtomic_WithinLimit(t *testing.T) {
	t.Parallel()
	testutil.SetupTestTracing(t)

	limitID := testutil.MustDeterministicUUID(8001)
	scopeKey := "acct:8001"
	periodKey := "2025-06"

	tests := []struct {
		name      string
		limitID   uuid.UUID
		scopeKey  string
		periodKey string
		amount    decimal.Decimal
		maxAmount decimal.Decimal
		mockSetup func(mock sqlmock.Sqlmock)
		wantUsage decimal.Decimal
		// wantErr is always false here; error paths are covered in ST-04-03 (exceeds) and ST-04-05 (propagation)
		wantErr bool
	}{
		{
			name:      "Success - existing counter ON CONFLICT UPDATE path returns new usage",
			limitID:   limitID,
			scopeKey:  scopeKey,
			periodKey: periodKey,
			amount:    decimal.RequireFromString("200"),
			maxAmount: decimal.RequireFromString("1000"),
			mockSetup: func(mock sqlmock.Sqlmock) {
				// The upsert query uses INSERT ... ON CONFLICT ... DO UPDATE ... WHERE ... RETURNING current_usage
				// When the counter exists and current_usage + amount <= maxAmount, it returns the new usage.
				rows := sqlmock.NewRows([]string{"current_usage"}).
					AddRow(decimal.RequireFromString("700"))

				mock.ExpectQuery(regexp.QuoteMeta(
					`INSERT INTO usage_counters (id,limit_id,scope_key,period_key,current_usage,last_updated_at) VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (limit_id, scope_key, period_key) DO UPDATE SET current_usage = usage_counters.current_usage + $7, last_updated_at = $8 WHERE usage_counters.current_usage + $9 <= $10 RETURNING current_usage`,
				)).
					WithArgs(
						sqlmock.AnyArg(), // $1 id (generated UUID)
						limitID.String(), // $2 limit_id
						scopeKey,         // $3 scope_key
						periodKey,        // $4 period_key
						sqlmock.AnyArg(), // $5 current_usage (initial = amount for INSERT)
						sqlmock.AnyArg(), // $6 last_updated_at
						sqlmock.AnyArg(), // $7 amount (for DO UPDATE SET)
						sqlmock.AnyArg(), // $8 last_updated_at (for DO UPDATE SET)
						sqlmock.AnyArg(), // $9 amount (for WHERE guard)
						sqlmock.AnyArg(), // $10 maxAmount (for WHERE guard)
					).
					WillReturnRows(rows)
			},
			wantUsage: decimal.RequireFromString("700"),
		},
		{
			name:      "Success - new counter INSERT path returns initial amount",
			limitID:   testutil.MustDeterministicUUID(8002),
			scopeKey:  "acct:8002",
			periodKey: "2025-07",
			amount:    decimal.RequireFromString("300"),
			maxAmount: decimal.RequireFromString("1000"),
			mockSetup: func(mock sqlmock.Sqlmock) {
				// INSERT succeeds (no conflict), RETURNING returns the inserted current_usage.
				rows := sqlmock.NewRows([]string{"current_usage"}).
					AddRow(decimal.RequireFromString("300"))

				mock.ExpectQuery(regexp.QuoteMeta(
					`INSERT INTO usage_counters (id,limit_id,scope_key,period_key,current_usage,last_updated_at) VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (limit_id, scope_key, period_key) DO UPDATE SET current_usage = usage_counters.current_usage + $7, last_updated_at = $8 WHERE usage_counters.current_usage + $9 <= $10 RETURNING current_usage`,
				)).
					WithArgs(
						sqlmock.AnyArg(), // $1 id (generated UUID)
						sqlmock.AnyArg(), // $2 limit_id
						sqlmock.AnyArg(), // $3 scope_key
						sqlmock.AnyArg(), // $4 period_key
						sqlmock.AnyArg(), // $5 current_usage (initial = amount)
						sqlmock.AnyArg(), // $6 last_updated_at
						sqlmock.AnyArg(), // $7 amount
						sqlmock.AnyArg(), // $8 last_updated_at
						sqlmock.AnyArg(), // $9 amount
						sqlmock.AnyArg(), // $10 maxAmount
					).
					WillReturnRows(rows)
			},
			wantUsage: decimal.RequireFromString("300"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, sqlMock, cleanup := setupUsageCounterRepositoryMockDB(t)
			defer cleanup()

			tt.mockSetup(sqlMock)

			ctx := context.Background()
			usage, err := repo.UpsertAndIncrementAtomic(ctx, tt.limitID, tt.scopeKey, tt.periodKey, tt.amount, tt.maxAmount)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.True(t, tt.wantUsage.Equal(usage), "expected usage %s, got %s", tt.wantUsage, usage)
		})
	}
}

func TestUsageCounterRepository_UpsertAndIncrementAtomic_ExceedsLimit(t *testing.T) {
	t.Parallel()
	testutil.SetupTestTracing(t)

	limitID := testutil.MustDeterministicUUID(8010)
	scopeKey := "acct:8010"
	periodKey := "2025-06"

	tests := []struct {
		name      string
		limitID   uuid.UUID
		scopeKey  string
		periodKey string
		amount    decimal.Decimal
		maxAmount decimal.Decimal
		mockSetup func(mock sqlmock.Sqlmock)
		wantErr   error
	}{
		{
			name:      "Error - increment would exceed limit (0 rows from RETURNING)",
			limitID:   limitID,
			scopeKey:  scopeKey,
			periodKey: periodKey,
			amount:    decimal.RequireFromString("600"),
			maxAmount: decimal.RequireFromString("1000"),
			mockSetup: func(mock sqlmock.Sqlmock) {
				// The ON CONFLICT UPDATE WHERE guard rejects the update because
				// current_usage (500) + amount (600) = 1100 > maxAmount (1000).
				// RETURNING returns 0 rows because the WHERE clause prevented the UPDATE.
				// sql.ErrNoRows is returned by QueryRowContext.Scan when no rows returned.
				rows := sqlmock.NewRows([]string{"current_usage"})

				mock.ExpectQuery(regexp.QuoteMeta(
					`INSERT INTO usage_counters (id,limit_id,scope_key,period_key,current_usage,last_updated_at) VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (limit_id, scope_key, period_key) DO UPDATE SET current_usage = usage_counters.current_usage + $7, last_updated_at = $8 WHERE usage_counters.current_usage + $9 <= $10 RETURNING current_usage`,
				)).
					WithArgs(
						sqlmock.AnyArg(), // $1 id (generated UUID)
						sqlmock.AnyArg(), // $2 limit_id
						sqlmock.AnyArg(), // $3 scope_key
						sqlmock.AnyArg(), // $4 period_key
						sqlmock.AnyArg(), // $5 current_usage (initial = amount for INSERT)
						sqlmock.AnyArg(), // $6 last_updated_at
						sqlmock.AnyArg(), // $7 amount (for DO UPDATE SET)
						sqlmock.AnyArg(), // $8 last_updated_at (for DO UPDATE SET)
						sqlmock.AnyArg(), // $9 amount (for WHERE guard)
						sqlmock.AnyArg(), // $10 maxAmount (for WHERE guard)
					).
					WillReturnRows(rows)
			},
			wantErr: constant.ErrUsageCounterExceedsLimit,
		},
		{
			name:      "Boundary - amount exactly at boundary still succeeds",
			limitID:   testutil.MustDeterministicUUID(8011),
			scopeKey:  "acct:8011",
			periodKey: "2025-06",
			amount:    decimal.RequireFromString("500"),
			maxAmount: decimal.RequireFromString("1000"),
			mockSetup: func(mock sqlmock.Sqlmock) {
				// current_usage (500) + amount (500) = 1000 <= maxAmount (1000)
				// This is a boundary success case: the WHERE guard allows the update.
				rows := sqlmock.NewRows([]string{"current_usage"}).
					AddRow(decimal.RequireFromString("1000"))

				mock.ExpectQuery(regexp.QuoteMeta(
					`INSERT INTO usage_counters (id,limit_id,scope_key,period_key,current_usage,last_updated_at) VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (limit_id, scope_key, period_key) DO UPDATE SET current_usage = usage_counters.current_usage + $7, last_updated_at = $8 WHERE usage_counters.current_usage + $9 <= $10 RETURNING current_usage`,
				)).
					WithArgs(
						sqlmock.AnyArg(), // $1 id (generated UUID)
						sqlmock.AnyArg(), // $2 limit_id
						sqlmock.AnyArg(), // $3 scope_key
						sqlmock.AnyArg(), // $4 period_key
						sqlmock.AnyArg(), // $5 current_usage
						sqlmock.AnyArg(), // $6 last_updated_at
						sqlmock.AnyArg(), // $7 amount
						sqlmock.AnyArg(), // $8 last_updated_at
						sqlmock.AnyArg(), // $9 amount
						sqlmock.AnyArg(), // $10 maxAmount
					).
					WillReturnRows(rows)
			},
			wantErr: nil, // Should succeed, not error
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, sqlMock, cleanup := setupUsageCounterRepositoryMockDB(t)
			defer cleanup()

			tt.mockSetup(sqlMock)

			ctx := context.Background()
			usage, err := repo.UpsertAndIncrementAtomic(ctx, tt.limitID, tt.scopeKey, tt.periodKey, tt.amount, tt.maxAmount)

			if tt.wantErr != nil {
				require.Error(t, err)
				assert.ErrorIs(t, err, tt.wantErr)
				assert.True(t, usage.IsZero(), "expected zero usage on error, got %s", usage)
				return
			}

			require.NoError(t, err)
			assert.False(t, usage.IsZero(), "expected non-zero usage on success")
		})
	}
}

func TestUsageCounterRepository_UpsertAndIncrementAtomic_PreCheck(t *testing.T) {
	t.Parallel()
	testutil.SetupTestTracing(t)

	limitID := testutil.MustDeterministicUUID(8020)

	tests := []struct {
		name      string
		limitID   uuid.UUID
		scopeKey  string
		periodKey string
		amount    decimal.Decimal
		maxAmount decimal.Decimal
		mockSetup func(mock sqlmock.Sqlmock)
		wantErr   error
	}{
		{
			name:      "Error - amount exceeds maxAmount pre-check rejects before SQL",
			limitID:   limitID,
			scopeKey:  "acct:8020",
			periodKey: "2025-06",
			amount:    decimal.RequireFromString("1500"),
			maxAmount: decimal.RequireFromString("1000"),
			mockSetup: func(mock sqlmock.Sqlmock) {
				// No SQL should be executed because the Go pre-check catches this.
				// If any SQL is executed, sqlmock will fail the test with
				// "call to Query/Exec was not expected".
			},
			wantErr: constant.ErrUsageCounterExceedsLimit,
		},
		{
			name:      "Success - amount equals maxAmount on fresh counter is allowed (boundary)",
			limitID:   testutil.MustDeterministicUUID(8021),
			scopeKey:  "acct:8021",
			periodKey: "2025-06",
			amount:    decimal.RequireFromString("1000"),
			maxAmount: decimal.RequireFromString("1000"),
			mockSetup: func(mock sqlmock.Sqlmock) {
				// amount == maxAmount: the pre-check should NOT reject this because
				// for a fresh INSERT (current_usage=0), 0 + 1000 = 1000 <= 1000 is valid.
				// The pre-check only rejects when amount > maxAmount (strictly greater).
				rows := sqlmock.NewRows([]string{"current_usage"}).
					AddRow(decimal.RequireFromString("1000"))

				mock.ExpectQuery(regexp.QuoteMeta(
					`INSERT INTO usage_counters (id,limit_id,scope_key,period_key,current_usage,last_updated_at) VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (limit_id, scope_key, period_key) DO UPDATE SET current_usage = usage_counters.current_usage + $7, last_updated_at = $8 WHERE usage_counters.current_usage + $9 <= $10 RETURNING current_usage`,
				)).
					WithArgs(
						sqlmock.AnyArg(), // $1 id (generated UUID)
						sqlmock.AnyArg(), // $2 limit_id
						sqlmock.AnyArg(), // $3 scope_key
						sqlmock.AnyArg(), // $4 period_key
						sqlmock.AnyArg(), // $5 current_usage
						sqlmock.AnyArg(), // $6 last_updated_at
						sqlmock.AnyArg(), // $7 amount
						sqlmock.AnyArg(), // $8 last_updated_at
						sqlmock.AnyArg(), // $9 amount
						sqlmock.AnyArg(), // $10 maxAmount
					).
					WillReturnRows(rows)
			},
			wantErr: nil, // Should succeed; pre-check only rejects amount > maxAmount
		},
		{
			name:      "Zero amount is a no-op (returns zero usage)",
			limitID:   testutil.MustDeterministicUUID(8022),
			scopeKey:  "acct:8022",
			periodKey: "2025-06",
			amount:    decimal.RequireFromString("0"),
			maxAmount: decimal.RequireFromString("1000"),
			mockSetup: func(mock sqlmock.Sqlmock) {
				// Zero amount should be a no-op; no SQL executed.
			},
			wantErr: nil,
		},
		{
			name:      "Error - negative amount returns increment-non-negative error",
			limitID:   testutil.MustDeterministicUUID(8023),
			scopeKey:  "acct:8023",
			periodKey: "2025-06",
			amount:    decimal.RequireFromString("-10"),
			maxAmount: decimal.RequireFromString("1000"),
			mockSetup: func(mock sqlmock.Sqlmock) {
				// Negative amount should be rejected before SQL execution.
			},
			wantErr: constant.ErrUsageCounterIncrementNonNegative,
		},
		{
			// maxAmount=0 means the limit is configured to deny everything.
			// Any positive amount satisfies amount.GreaterThan(decimal.Zero) == true,
			// so the pre-check must catch it before SQL is executed.
			name:      "zero maxAmount rejects any positive amount",
			limitID:   testutil.MustDeterministicUUID(8041),
			scopeKey:  "acct:test-zero-max",
			periodKey: "2024-01-15",
			amount:    decimal.RequireFromString("1"),
			maxAmount: decimal.Zero,
			mockSetup: func(mock sqlmock.Sqlmock) {
				// No SQL should be executed: the Go pre-check (amount > maxAmount)
				// fires because 1 > 0. If any query is issued, sqlmock will fail
				// the test with "call to Query/Exec was not expected".
			},
			wantErr: constant.ErrUsageCounterExceedsLimit,
		},
		{
			// zero amount against zero maxAmount: amount.IsZero() short-circuits
			// before any DB call, returning (decimal.Zero, nil) immediately.
			name:      "zero amount with zero maxAmount passes pre-check and succeeds",
			limitID:   testutil.MustDeterministicUUID(8042),
			scopeKey:  "acct:test-zero-max-zero-amount",
			periodKey: "2024-01-15",
			amount:    decimal.Zero,
			maxAmount: decimal.Zero,
			mockSetup: func(mock sqlmock.Sqlmock) {
				// No SQL should be executed: amount.IsZero() short-circuits
				// before any DB call. If any query is issued, sqlmock will
				// fail with "call to Query/Exec was not expected".
			},
			wantErr: nil, // Should succeed; amount.IsZero() returns early
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, sqlMock, cleanup := setupUsageCounterRepositoryMockDB(t)
			defer cleanup()

			tt.mockSetup(sqlMock)

			ctx := context.Background()
			usage, err := repo.UpsertAndIncrementAtomic(ctx, tt.limitID, tt.scopeKey, tt.periodKey, tt.amount, tt.maxAmount)

			if tt.wantErr != nil {
				require.Error(t, err)
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			// Contract: when amount.IsZero(), UpsertAndIncrementAtomic must short-circuit and return (decimal.Zero, nil)
			if tt.amount.IsZero() {
				assert.True(t, usage.IsZero(), "expected zero usage for zero amount, got %s", usage)
			}
		})
	}
}

func TestUsageCounterRepository_UpsertAndIncrementAtomic_ConnectionError(t *testing.T) {
	t.Parallel()
	testutil.SetupTestTracing(t)

	ctrl := gomock.NewController(t)

	mockConn := mocks.NewMockConnection(ctrl)
	mockConn.EXPECT().GetDB().Return(nil, errors.New("connection refused"))

	repo := NewUsageCounterRepositoryWithConnection(mockConn)

	ctx := context.Background()
	_, err := repo.UpsertAndIncrementAtomic(ctx, testutil.MustDeterministicUUID(8029), "acct:8029", "2025-06", decimal.RequireFromString("100"), decimal.RequireFromString("1000"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get database connection")
}

func TestUsageCounterRepository_UpsertAndIncrementAtomic_ErrorPropagation(t *testing.T) {
	t.Parallel()
	testutil.SetupTestTracing(t)

	limitID := testutil.MustDeterministicUUID(8030)

	tests := []struct {
		name      string
		limitID   uuid.UUID
		scopeKey  string
		periodKey string
		amount    decimal.Decimal
		maxAmount decimal.Decimal
		mockSetup func(mock sqlmock.Sqlmock)
		wantErr   bool
		errMsg    string
	}{
		{
			name:      "Error - database error is propagated with wrapping",
			limitID:   limitID,
			scopeKey:  "acct:8030",
			periodKey: "2025-06",
			amount:    decimal.RequireFromString("100"),
			maxAmount: decimal.RequireFromString("1000"),
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(regexp.QuoteMeta(
					`INSERT INTO usage_counters (id,limit_id,scope_key,period_key,current_usage,last_updated_at) VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (limit_id, scope_key, period_key) DO UPDATE SET current_usage = usage_counters.current_usage + $7, last_updated_at = $8 WHERE usage_counters.current_usage + $9 <= $10 RETURNING current_usage`,
				)).
					WithArgs(
						sqlmock.AnyArg(), // $1 id (generated UUID)
						sqlmock.AnyArg(), // $2 limit_id
						sqlmock.AnyArg(), // $3 scope_key
						sqlmock.AnyArg(), // $4 period_key
						sqlmock.AnyArg(), // $5 current_usage
						sqlmock.AnyArg(), // $6 last_updated_at
						sqlmock.AnyArg(), // $7 amount
						sqlmock.AnyArg(), // $8 last_updated_at
						sqlmock.AnyArg(), // $9 amount
						sqlmock.AnyArg(), // $10 maxAmount
					).
					WillReturnError(errors.New("disk full"))
			},
			wantErr: true,
			errMsg:  "disk full",
		},
		{
			name:      "Error - scan error is propagated",
			limitID:   testutil.MustDeterministicUUID(8031),
			scopeKey:  "acct:8031",
			periodKey: "2025-06",
			amount:    decimal.RequireFromString("100"),
			maxAmount: decimal.RequireFromString("1000"),
			mockSetup: func(mock sqlmock.Sqlmock) {
				// Return a row with an incompatible type to trigger a scan error
				rows := sqlmock.NewRows([]string{"current_usage"}).
					AddRow("not-a-decimal")

				mock.ExpectQuery(regexp.QuoteMeta(
					`INSERT INTO usage_counters (id,limit_id,scope_key,period_key,current_usage,last_updated_at) VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (limit_id, scope_key, period_key) DO UPDATE SET current_usage = usage_counters.current_usage + $7, last_updated_at = $8 WHERE usage_counters.current_usage + $9 <= $10 RETURNING current_usage`,
				)).
					WithArgs(
						sqlmock.AnyArg(), // $1 id (generated UUID)
						sqlmock.AnyArg(), // $2 limit_id
						sqlmock.AnyArg(), // $3 scope_key
						sqlmock.AnyArg(), // $4 period_key
						sqlmock.AnyArg(), // $5 current_usage
						sqlmock.AnyArg(), // $6 last_updated_at
						sqlmock.AnyArg(), // $7 amount
						sqlmock.AnyArg(), // $8 last_updated_at
						sqlmock.AnyArg(), // $9 amount
						sqlmock.AnyArg(), // $10 maxAmount
					).
					WillReturnRows(rows)
			},
			wantErr: true,
			errMsg:  "failed to scan",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, sqlMock, cleanup := setupUsageCounterRepositoryMockDB(t)
			defer cleanup()

			tt.mockSetup(sqlMock)

			ctx := context.Background()
			usage, err := repo.UpsertAndIncrementAtomic(ctx, tt.limitID, tt.scopeKey, tt.periodKey, tt.amount, tt.maxAmount)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errMsg)
			assert.True(t, usage.IsZero(), "expected zero usage on error, got %s", usage)
		})
	}
}

func TestUsageCounterRepository_UpsertAndIncrementAtomic_ContextCancellation(t *testing.T) {
	t.Parallel()
	testutil.SetupTestTracing(t)

	limitID := testutil.MustDeterministicUUID(8040)

	repo, sqlMock, cleanup := setupUsageCounterRepositoryMockDB(t)
	defer cleanup()

	// When context is cancelled, the database driver returns context.Canceled
	sqlMock.ExpectQuery(regexp.QuoteMeta(
		`INSERT INTO usage_counters (id,limit_id,scope_key,period_key,current_usage,last_updated_at) VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (limit_id, scope_key, period_key) DO UPDATE SET current_usage = usage_counters.current_usage + $7, last_updated_at = $8 WHERE usage_counters.current_usage + $9 <= $10 RETURNING current_usage`,
	)).
		WithArgs(
			sqlmock.AnyArg(), // $1 id (generated UUID)
			sqlmock.AnyArg(), // $2 limit_id
			sqlmock.AnyArg(), // $3 scope_key
			sqlmock.AnyArg(), // $4 period_key
			sqlmock.AnyArg(), // $5 current_usage
			sqlmock.AnyArg(), // $6 last_updated_at
			sqlmock.AnyArg(), // $7 amount
			sqlmock.AnyArg(), // $8 last_updated_at
			sqlmock.AnyArg(), // $9 amount
			sqlmock.AnyArg(), // $10 maxAmount
		).
		WillReturnError(context.Canceled)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	usage, err := repo.UpsertAndIncrementAtomic(ctx, limitID, "acct:8040", "2025-06", decimal.RequireFromString("100"), decimal.RequireFromString("1000"))

	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled), "expected context.Canceled, got: %v", err)
	assert.True(t, usage.IsZero(), "expected zero usage on cancellation, got %s", usage)
}
