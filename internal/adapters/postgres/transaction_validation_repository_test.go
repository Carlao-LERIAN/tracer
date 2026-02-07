// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/shopspring/decimal"

	"tracer/internal/adapters/postgres/db/mocks"
	"tracer/internal/testutil"
	"tracer/pkg/constant"
	"tracer/pkg/model"
	pkgHTTP "tracer/pkg/net/http"
)

// setupTransactionValidationRepositoryMockDB creates a gomock controller, mock DBConnection, and sqlmock for testing.
// Returns the repository, sqlmock for query expectations, and a cleanup function.
func setupTransactionValidationRepositoryMockDB(t *testing.T) (*TransactionValidationRepository, sqlmock.Sqlmock, func()) {
	t.Helper()

	ctrl := gomock.NewController(t)
	db, sqlMock, err := sqlmock.New()
	require.NoError(t, err)

	mockConn := mocks.NewMockConnection(ctrl)
	mockConn.EXPECT().GetDB().Return(db, nil).AnyTimes()

	repo := NewTransactionValidationRepositoryWithConnection(mockConn)

	cleanup := func() {
		sqlMock.ExpectClose()
		err := db.Close()
		require.NoError(t, err)
	}

	return repo, sqlMock, cleanup
}

// testTransactionValidation creates a test transaction validation with default values.
func testTransactionValidation() *model.TransactionValidation {
	return &model.TransactionValidation{
		ID:                   testutil.MustDeterministicUUID(1),
		RequestID:            testutil.MustDeterministicUUID(100),
		TransactionType:      model.TransactionTypeCard,
		SubType:              nil,
		Amount:               decimal.RequireFromString("500"),
		Currency:             "USD",
		TransactionTimestamp: time.Date(2024, 1, 15, 9, 0, 0, 0, time.UTC),
		Account: model.AccountContext{
			ID:     testutil.MustDeterministicUUID(200),
			Type:   "checking",
			Status: "active",
		},
		Segment:   nil,
		Portfolio: nil,
		Merchant:  nil,
		Metadata:  nil,
		EvaluationResult: model.EvaluationResult{
			Decision:         model.DecisionAllow,
			Reason:           "No matching rules",
			MatchedRuleIDs:   []uuid.UUID{},
			EvaluatedRuleIDs: []uuid.UUID{testutil.MustDeterministicUUID(10), testutil.MustDeterministicUUID(11)},
		},
		LimitUsageDetails: []model.LimitUsageDetail{},
		ProcessingTimeMs:  42,
		CreatedAt:         time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
	}
}

// testTransactionValidationWithArrays creates a test transaction validation with populated arrays.
func testTransactionValidationWithArrays() *model.TransactionValidation {
	return &model.TransactionValidation{
		ID:                   testutil.MustDeterministicUUID(2),
		RequestID:            testutil.MustDeterministicUUID(101),
		TransactionType:      model.TransactionTypeWire,
		SubType:              nil,
		Amount:               decimal.RequireFromString("1000"),
		Currency:             "USD",
		TransactionTimestamp: time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
		Account: model.AccountContext{
			ID:     testutil.MustDeterministicUUID(201),
			Type:   "savings",
			Status: "active",
		},
		Segment:   nil,
		Portfolio: nil,
		Merchant:  nil,
		Metadata:  nil,
		EvaluationResult: model.EvaluationResult{
			Decision:         model.DecisionDeny,
			Reason:           "Multiple rules matched",
			MatchedRuleIDs:   []uuid.UUID{testutil.MustDeterministicUUID(20), testutil.MustDeterministicUUID(21)},
			EvaluatedRuleIDs: []uuid.UUID{testutil.MustDeterministicUUID(22), testutil.MustDeterministicUUID(23), testutil.MustDeterministicUUID(24)},
		},
		LimitUsageDetails: []model.LimitUsageDetail{
			{
				LimitID:      testutil.MustDeterministicUUID(30),
				LimitAmount:  decimal.RequireFromString("10000"),
				CurrentUsage: decimal.RequireFromString("9500"),
				Exceeded:     false,
			},
			{
				LimitID:      testutil.MustDeterministicUUID(31),
				LimitAmount:  decimal.RequireFromString("5000"),
				CurrentUsage: decimal.RequireFromString("5000.01"),
				Exceeded:     true,
			},
		},
		ProcessingTimeMs: 85,
		CreatedAt:        time.Date(2024, 1, 15, 11, 0, 0, 0, time.UTC),
	}
}

// transactionValidationColumns returns the column names for transaction validation queries.
func transactionValidationColumns() []string {
	return []string{
		"id",
		"request_id",
		"transaction_type",
		"sub_type",
		"amount",
		"currency",
		"transaction_timestamp",
		"account",
		"segment",
		"portfolio",
		"merchant",
		"metadata",
		"decision",
		"reason",
		"matched_rule_ids",
		"evaluated_rule_ids",
		"limit_usage_details",
		"processing_time_ms",
		"created_at",
	}
}

// transactionValidationRow creates a sqlmock row from a TransactionValidation.
// Uses helper functions mustMarshalJSON, mustMarshalJSONOrEmpty, and uuidSliceToStrings
// for consistent JSON marshaling and UUID conversion across all tests.
func transactionValidationRow(t *testing.T, tv *model.TransactionValidation) *sqlmock.Rows {
	t.Helper()

	return sqlmock.NewRows(transactionValidationColumns()).
		AddRow(
			tv.ID,
			tv.RequestID,
			string(tv.TransactionType),
			tv.SubType,
			tv.Amount,
			tv.Currency,
			tv.TransactionTimestamp,
			mustMarshalJSON(t, tv.Account),
			mustMarshalJSONOrNil(t, tv.Segment),
			mustMarshalJSONOrNil(t, tv.Portfolio),
			mustMarshalJSONOrNil(t, tv.Merchant),
			mustMarshalJSONOrEmpty(t, tv.Metadata),
			string(tv.Decision),
			tv.Reason,
			uuidSliceToStrings(tv.MatchedRuleIDs),
			uuidSliceToStrings(tv.EvaluatedRuleIDs),
			mustMarshalJSON(t, tv.LimitUsageDetails),
			tv.ProcessingTimeMs,
			tv.CreatedAt,
		)
}

// mustMarshalJSON marshals a value to JSON and fails the test on error.
func mustMarshalJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err, "failed to marshal JSON")
	return b
}

// mustMarshalJSONOrEmpty marshals a value to JSON, returning "{}" for nil values.
func mustMarshalJSONOrEmpty(t *testing.T, v any) []byte {
	t.Helper()
	if v == nil {
		return []byte("{}")
	}
	return mustMarshalJSON(t, v)
}

// mustMarshalJSONOrNil marshals a value to JSON, returning nil for nil values.
// Used for nullable JSONB columns (segment, portfolio, merchant).
func mustMarshalJSONOrNil(t *testing.T, v any) []byte {
	t.Helper()
	if v == nil {
		return nil
	}

	// Handle typed nil pointers (e.g., (*SegmentContext)(nil))
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Ptr && rv.IsNil() {
		return nil
	}

	return mustMarshalJSON(t, v)
}

// uuidSliceToStrings converts a slice of UUIDs to StringArray for sqlmock.
func uuidSliceToStrings(ids []uuid.UUID) StringArray {
	result := make(StringArray, len(ids))
	for i, id := range ids {
		result[i] = id.String()
	}
	return result
}

func TestTransactionValidationPostgresRepository_Insert_ConnectionError(t *testing.T) {
	testutil.SetupTestTracing(t)

	ctrl := gomock.NewController(t)

	mockConn := mocks.NewMockConnection(ctrl)
	mockConn.EXPECT().GetDB().Return(nil, errors.New("connection refused"))

	repo := NewTransactionValidationRepositoryWithConnection(mockConn)

	ctx := context.Background()
	err := repo.Insert(ctx, testTransactionValidation())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get database connection")
}

func TestTransactionValidationPostgresRepository_Insert(t *testing.T) {
	testutil.SetupTestTracing(t)

	tests := []struct {
		name      string
		tv        *model.TransactionValidation
		mockSetup func(mock sqlmock.Sqlmock, tv *model.TransactionValidation)
		wantErr   bool
		errMsg    string
	}{
		{
			name: "Success - inserts transaction validation with empty arrays",
			tv:   testTransactionValidation(),
			mockSetup: func(mock sqlmock.Sqlmock, tv *model.TransactionValidation) {
				mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO transaction_validations`)).
					WithArgs(
						tv.ID,
						tv.RequestID,
						string(tv.TransactionType),
						tv.SubType,
						tv.Amount,
						tv.Currency,
						tv.TransactionTimestamp,
						sqlmock.AnyArg(), // account (JSONB)
						sqlmock.AnyArg(), // segment (JSONB)
						sqlmock.AnyArg(), // portfolio (JSONB)
						sqlmock.AnyArg(), // merchant (JSONB)
						sqlmock.AnyArg(), // metadata (JSONB)
						string(tv.Decision),
						tv.Reason,
						sqlmock.AnyArg(), // matched_rule_ids (UUID[])
						sqlmock.AnyArg(), // evaluated_rule_ids (UUID[])
						sqlmock.AnyArg(), // limit_usage_details (JSONB)
						tv.ProcessingTimeMs,
						tv.CreatedAt,
					).
					WillReturnResult(sqlmock.NewResult(1, 1))
			},
			wantErr: false,
		},
		{
			name: "Success - inserts transaction validation with populated arrays",
			tv:   testTransactionValidationWithArrays(),
			mockSetup: func(mock sqlmock.Sqlmock, tv *model.TransactionValidation) {
				mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO transaction_validations`)).
					WithArgs(
						tv.ID,
						tv.RequestID,
						string(tv.TransactionType),
						tv.SubType,
						tv.Amount,
						tv.Currency,
						tv.TransactionTimestamp,
						sqlmock.AnyArg(), // account (JSONB)
						sqlmock.AnyArg(), // segment (JSONB)
						sqlmock.AnyArg(), // portfolio (JSONB)
						sqlmock.AnyArg(), // merchant (JSONB)
						sqlmock.AnyArg(), // metadata (JSONB)
						string(tv.Decision),
						tv.Reason,
						sqlmock.AnyArg(), // matched_rule_ids (UUID[])
						sqlmock.AnyArg(), // evaluated_rule_ids (UUID[])
						sqlmock.AnyArg(), // limit_usage_details (JSONB)
						tv.ProcessingTimeMs,
						tv.CreatedAt,
					).
					WillReturnResult(sqlmock.NewResult(1, 1))
			},
			wantErr: false,
		},
		{
			name: "Error - database insert fails",
			tv:   testTransactionValidation(),
			mockSetup: func(mock sqlmock.Sqlmock, tv *model.TransactionValidation) {
				mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO transaction_validations`)).
					WillReturnError(errors.New("database error"))
			},
			wantErr: true,
			errMsg:  "failed to insert transaction validation",
		},
		{
			name:      "Error - nil transaction validation",
			tv:        nil,
			mockSetup: func(mock sqlmock.Sqlmock, tv *model.TransactionValidation) {},
			wantErr:   true,
			errMsg:    "validation cannot be nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, sqlMock, cleanup := setupTransactionValidationRepositoryMockDB(t)
			defer cleanup()

			tt.mockSetup(sqlMock, tt.tv)

			ctx := context.Background()
			err := repo.Insert(ctx, tt.tv)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}

			require.NoError(t, sqlMock.ExpectationsWereMet())
		})
	}
}

func TestTransactionValidationPostgresRepository_GetByID_ConnectionError(t *testing.T) {
	testutil.SetupTestTracing(t)

	ctrl := gomock.NewController(t)

	mockConn := mocks.NewMockConnection(ctrl)
	mockConn.EXPECT().GetDB().Return(nil, errors.New("connection refused"))

	repo := NewTransactionValidationRepositoryWithConnection(mockConn)

	ctx := context.Background()
	result, err := repo.GetByID(ctx, testutil.MustDeterministicUUID(999))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get database connection")
	assert.Nil(t, result)
}

func TestTransactionValidationPostgresRepository_GetByID(t *testing.T) {
	testutil.SetupTestTracing(t)

	tests := []struct {
		name      string
		tvID      uuid.UUID
		mockSetup func(mock sqlmock.Sqlmock)
		want      *model.TransactionValidation
		wantErr   bool
		errType   error
	}{
		{
			name: "Success - finds transaction validation with empty arrays",
			tvID: testutil.MustDeterministicUUID(1),
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
					WithArgs(testutil.MustDeterministicUUID(1)).
					WillReturnRows(transactionValidationRow(t, testTransactionValidation()))
			},
			want:    testTransactionValidation(),
			wantErr: false,
		},
		{
			name: "Success - finds transaction validation with populated arrays",
			tvID: testutil.MustDeterministicUUID(2),
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
					WithArgs(testutil.MustDeterministicUUID(2)).
					WillReturnRows(transactionValidationRow(t, testTransactionValidationWithArrays()))
			},
			want:    testTransactionValidationWithArrays(),
			wantErr: false,
		},
		{
			name: "Error - transaction validation not found",
			tvID: testutil.MustDeterministicUUID(99),
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
					WithArgs(testutil.MustDeterministicUUID(99)).
					WillReturnError(sql.ErrNoRows)
			},
			wantErr: true,
			errType: constant.ErrTransactionValidationNotFound,
		},
		{
			name: "Error - database query fails",
			tvID: testutil.MustDeterministicUUID(1),
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
					WithArgs(testutil.MustDeterministicUUID(1)).
					WillReturnError(errors.New("database error"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, sqlMock, cleanup := setupTransactionValidationRepositoryMockDB(t)
			defer cleanup()

			tt.mockSetup(sqlMock)

			ctx := context.Background()
			result, err := repo.GetByID(ctx, tt.tvID)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errType != nil {
					assert.ErrorIs(t, err, tt.errType)
				}
				assert.Nil(t, result)
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
				assert.Equal(t, tt.want.ID, result.ID)
				assert.Equal(t, tt.want.Decision, result.Decision)
				assert.Equal(t, tt.want.Reason, result.Reason)
				assert.Equal(t, len(tt.want.MatchedRuleIDs), len(result.MatchedRuleIDs))
				assert.Equal(t, len(tt.want.EvaluatedRuleIDs), len(result.EvaluatedRuleIDs))
				assert.Equal(t, len(tt.want.LimitUsageDetails), len(result.LimitUsageDetails))
			}

			require.NoError(t, sqlMock.ExpectationsWereMet())
		})
	}
}

func TestTransactionValidationPostgresRepository_List_ConnectionError(t *testing.T) {
	testutil.SetupTestTracing(t)

	ctrl := gomock.NewController(t)

	mockConn := mocks.NewMockConnection(ctrl)
	mockConn.EXPECT().GetDB().Return(nil, errors.New("connection refused"))

	repo := NewTransactionValidationRepositoryWithConnection(mockConn)

	ctx := context.Background()
	result, err := repo.List(ctx, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get database connection")
	assert.Nil(t, result)
}

func TestTransactionValidationPostgresRepository_List(t *testing.T) {
	testutil.SetupTestTracing(t)

	tests := []struct {
		name      string
		filters   *model.TransactionValidationFilters
		mockSetup func(mock sqlmock.Sqlmock)
		wantLen   int
		wantErr   bool
		errMsg    string
		errType   error
	}{
		{
			name:    "Success - lists transaction validations with default filters",
			filters: nil,
			mockSetup: func(mock sqlmock.Sqlmock) {
				// Add two rows using the standard transactionValidationRow helper
				tv1 := testTransactionValidation()
				tv2 := testTransactionValidationWithArrays()

				rows := transactionValidationRow(t, tv1)
				rows.AddRow(
					tv2.ID,
					tv2.RequestID,
					string(tv2.TransactionType),
					tv2.SubType,
					tv2.Amount,
					tv2.Currency,
					tv2.TransactionTimestamp,
					mustMarshalJSON(t, tv2.Account),
					mustMarshalJSONOrNil(t, tv2.Segment),
					mustMarshalJSONOrNil(t, tv2.Portfolio),
					mustMarshalJSONOrNil(t, tv2.Merchant),
					mustMarshalJSONOrEmpty(t, tv2.Metadata),
					string(tv2.Decision),
					tv2.Reason,
					uuidSliceToStrings(tv2.MatchedRuleIDs),
					uuidSliceToStrings(tv2.EvaluatedRuleIDs),
					mustMarshalJSON(t, tv2.LimitUsageDetails),
					tv2.ProcessingTimeMs,
					tv2.CreatedAt,
				)

				mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
					WillReturnRows(rows)
			},
			wantLen: 2,
			wantErr: false,
		},
		{
			name: "Success - lists transaction validations with decision filter",
			filters: &model.TransactionValidationFilters{
				Decision: testutil.Ptr(model.DecisionDeny),
				Limit:    10,
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				tv := testTransactionValidationWithArrays()
				rows := transactionValidationRow(t, tv)

				mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
					WillReturnRows(rows)
			},
			wantLen: 1,
			wantErr: false,
		},
		{
			name: "Success - lists transaction validations with combined filters",
			filters: &model.TransactionValidationFilters{
				Decision:      testutil.Ptr(model.DecisionDeny),
				StartDate:     time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndDate:       time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
				MatchedRuleID: testutil.UUIDPtr(testutil.MustDeterministicUUID(20)),
				Limit:         10,
				SortBy:        "createdAt",
				SortOrder:     "DESC",
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				tv := testTransactionValidationWithArrays()
				rows := transactionValidationRow(t, tv)

				mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
					WillReturnRows(rows)
			},
			wantLen: 1,
			wantErr: false,
		},
		{
			name: "Success - lists transaction validations with segmentId filter",
			filters: &model.TransactionValidationFilters{
				SegmentID: testutil.UUIDPtr(testutil.MustDeterministicUUID(300)),
				Limit:     10,
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				tv := testTransactionValidation()
				rows := transactionValidationRow(t, tv)
				mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
					WillReturnRows(rows)
			},
			wantLen: 1,
			wantErr: false,
		},
		{
			name: "Success - lists transaction validations with portfolioId filter",
			filters: &model.TransactionValidationFilters{
				PortfolioID: testutil.UUIDPtr(testutil.MustDeterministicUUID(400)),
				Limit:       10,
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				tv := testTransactionValidation()
				rows := transactionValidationRow(t, tv)
				mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
					WillReturnRows(rows)
			},
			wantLen: 1,
			wantErr: false,
		},
		{
			name: "Success - lists transaction validations with transactionType filter",
			filters: &model.TransactionValidationFilters{
				TransactionType: testutil.Ptr(model.TransactionTypeCard),
				Limit:           10,
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				tv := testTransactionValidation()
				rows := transactionValidationRow(t, tv)
				mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
					WillReturnRows(rows)
			},
			wantLen: 1,
			wantErr: false,
		},
		{
			name:    "Success - returns empty list",
			filters: nil,
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
					WillReturnRows(sqlmock.NewRows(transactionValidationColumns()))
			},
			wantLen: 0,
			wantErr: false,
		},
		{
			name:    "Error - database query fails",
			filters: nil,
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
					WillReturnError(errors.New("database error"))
			},
			wantErr: true,
			errMsg:  "failed to list transaction validations",
		},
		{
			name: "Error - invalid filters (negative limit)",
			filters: &model.TransactionValidationFilters{
				Limit: -1,
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				// No DB call expected - validation should fail first
			},
			wantErr: true,
			errType: constant.ErrInvalidTransactionValidationFilters,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, sqlMock, cleanup := setupTransactionValidationRepositoryMockDB(t)
			defer cleanup()

			tt.mockSetup(sqlMock)

			ctx := context.Background()
			result, err := repo.List(ctx, tt.filters)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errType != nil {
					require.ErrorIs(t, err, tt.errType)
				} else if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
				assert.Len(t, result.TransactionValidations, tt.wantLen)
			}

			require.NoError(t, sqlMock.ExpectationsWereMet())
		})
	}
}

func TestTransactionValidationPostgresRepository_Count_ConnectionError(t *testing.T) {
	testutil.SetupTestTracing(t)

	ctrl := gomock.NewController(t)

	mockConn := mocks.NewMockConnection(ctrl)
	mockConn.EXPECT().GetDB().Return(nil, errors.New("connection refused"))

	repo := NewTransactionValidationRepositoryWithConnection(mockConn)

	ctx := context.Background()
	result, err := repo.Count(ctx, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get database connection")
	assert.Equal(t, int64(0), result)
}

func TestTransactionValidationPostgresRepository_Count(t *testing.T) {
	testutil.SetupTestTracing(t)

	tests := []struct {
		name      string
		filters   *model.TransactionValidationFilters
		mockSetup func(mock sqlmock.Sqlmock)
		want      int64
		wantErr   bool
		errMsg    string
	}{
		{
			name:    "Success - counts all transaction validations",
			filters: nil,
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*)`)).
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(42))
			},
			want:    42,
			wantErr: false,
		},
		{
			name: "Success - counts with decision filter",
			filters: &model.TransactionValidationFilters{
				Decision: testutil.Ptr(model.DecisionDeny),
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*)`)).
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(10))
			},
			want:    10,
			wantErr: false,
		},
		{
			name:    "Success - returns zero count",
			filters: nil,
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*)`)).
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
			},
			want:    0,
			wantErr: false,
		},
		{
			name:    "Error - database query fails",
			filters: nil,
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*)`)).
					WillReturnError(errors.New("database error"))
			},
			wantErr: true,
			errMsg:  "failed to count transaction validations",
		},
		{
			name: "Error - invalid filters (negative limit)",
			filters: &model.TransactionValidationFilters{
				Limit: -1,
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				// No DB call expected - validation should fail first
			},
			wantErr: true,
			errMsg:  "limit cannot be negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, sqlMock, cleanup := setupTransactionValidationRepositoryMockDB(t)
			defer cleanup()

			if tt.mockSetup != nil {
				tt.mockSetup(sqlMock)
			}

			ctx := context.Background()
			result, err := repo.Count(ctx, tt.filters)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, result)
			}

			require.NoError(t, sqlMock.ExpectationsWereMet())
		})
	}
}

// Test JSONB serialization/deserialization for limit usage details
func TestTransactionValidationPostgresRepository_LimitUsageDetails_JSONB(t *testing.T) {
	testutil.SetupTestTracing(t)

	repo, sqlMock, cleanup := setupTransactionValidationRepositoryMockDB(t)
	defer cleanup()

	tv := testTransactionValidationWithArrays()
	rows := transactionValidationRow(t, tv)

	sqlMock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(tv.ID).
		WillReturnRows(rows)

	ctx := context.Background()
	result, err := repo.GetByID(ctx, tv.ID)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.LimitUsageDetails, 2)

	// Verify first limit usage detail
	assert.Equal(t, tv.LimitUsageDetails[0].LimitID, result.LimitUsageDetails[0].LimitID)
	assert.Equal(t, tv.LimitUsageDetails[0].LimitAmount, result.LimitUsageDetails[0].LimitAmount)
	assert.Equal(t, tv.LimitUsageDetails[0].CurrentUsage, result.LimitUsageDetails[0].CurrentUsage)
	assert.Equal(t, tv.LimitUsageDetails[0].Exceeded, result.LimitUsageDetails[0].Exceeded)

	// Verify second limit usage detail (exceeded)
	assert.Equal(t, tv.LimitUsageDetails[1].LimitID, result.LimitUsageDetails[1].LimitID)
	assert.Equal(t, tv.LimitUsageDetails[1].LimitAmount, result.LimitUsageDetails[1].LimitAmount)
	assert.Equal(t, tv.LimitUsageDetails[1].CurrentUsage, result.LimitUsageDetails[1].CurrentUsage)
	assert.True(t, result.LimitUsageDetails[1].Exceeded)

	require.NoError(t, sqlMock.ExpectationsWereMet())
}

// Test UUID[] array handling for matched/evaluated rule IDs
func TestTransactionValidationPostgresRepository_UUIDArrays(t *testing.T) {
	testutil.SetupTestTracing(t)

	repo, sqlMock, cleanup := setupTransactionValidationRepositoryMockDB(t)
	defer cleanup()

	tv := testTransactionValidationWithArrays()
	rows := transactionValidationRow(t, tv)

	sqlMock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(tv.ID).
		WillReturnRows(rows)

	ctx := context.Background()
	result, err := repo.GetByID(ctx, tv.ID)

	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify matched rule IDs
	require.Len(t, result.MatchedRuleIDs, 2)
	assert.Equal(t, tv.MatchedRuleIDs[0], result.MatchedRuleIDs[0])
	assert.Equal(t, tv.MatchedRuleIDs[1], result.MatchedRuleIDs[1])

	// Verify evaluated rule IDs
	require.Len(t, result.EvaluatedRuleIDs, 3)
	assert.Equal(t, tv.EvaluatedRuleIDs[0], result.EvaluatedRuleIDs[0])
	assert.Equal(t, tv.EvaluatedRuleIDs[1], result.EvaluatedRuleIDs[1])
	assert.Equal(t, tv.EvaluatedRuleIDs[2], result.EvaluatedRuleIDs[2])

	require.NoError(t, sqlMock.ExpectationsWereMet())
}

func TestGetSortValueFromValidation(t *testing.T) {
	testutil.SetupTestTracing(t)

	fixedTime := time.Date(2024, 6, 15, 10, 30, 0, 123456789, time.UTC)
	validation := &model.TransactionValidation{
		ID:               testutil.MustDeterministicUUID(1),
		CreatedAt:        fixedTime,
		ProcessingTimeMs: 42,
	}

	tests := []struct {
		name      string
		sortBy    string
		wantValue string
		wantErr   bool
		errMsg    string
	}{
		{
			name:      "Success - created_at returns RFC3339Nano formatted time",
			sortBy:    "created_at",
			wantValue: fixedTime.Format(time.RFC3339Nano),
			wantErr:   false,
		},
		{
			name:      "Success - processing_time_ms returns string formatted int64",
			sortBy:    "processing_time_ms",
			wantValue: "42",
			wantErr:   false,
		},
		{
			name:    "Error - unknown sortBy returns error",
			sortBy:  "unknown_column",
			wantErr: true,
			errMsg:  "unsupported sort column: unknown_column",
		},
		{
			name:    "Error - empty sortBy returns error",
			sortBy:  "",
			wantErr: true,
			errMsg:  "unsupported sort column:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value, err := getSortValueFromValidation(validation, tt.sortBy)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				assert.Empty(t, value)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantValue, value)
			}
		})
	}
}

func TestValidateAndNormalizeSort(t *testing.T) {
	testutil.SetupTestTracing(t)

	repo, _, cleanup := setupTransactionValidationRepositoryMockDB(t)
	defer cleanup()

	tests := []struct {
		name          string
		filters       *model.TransactionValidationFilters
		wantSortBy    string
		wantSortOrder string
		wantErr       bool
		errIs         error
	}{
		{
			name: "Success - camelCase createdAt maps to snake_case",
			filters: &model.TransactionValidationFilters{
				SortBy:    "createdAt",
				SortOrder: "DESC",
			},
			wantSortBy:    "created_at",
			wantSortOrder: "DESC",
			wantErr:       false,
		},
		{
			name: "Error - snake_case created_at rejected (use camelCase)",
			filters: &model.TransactionValidationFilters{
				SortBy:    "created_at",
				SortOrder: "ASC",
			},
			wantErr: true,
			errIs:   constant.ErrInvalidSortColumn,
		},
		{
			name: "Success - processingTimeMs maps to processing_time_ms",
			filters: &model.TransactionValidationFilters{
				SortBy:    "processingTimeMs",
				SortOrder: "DESC",
			},
			wantSortBy:    "processing_time_ms",
			wantSortOrder: "DESC",
			wantErr:       false,
		},
		{
			name: "Error - snake_case processing_time_ms rejected (use camelCase)",
			filters: &model.TransactionValidationFilters{
				SortBy:    "processing_time_ms",
				SortOrder: "ASC",
			},
			wantErr: true,
			errIs:   constant.ErrInvalidSortColumn,
		},
		{
			name: "Success - empty sortBy defaults to createdAt mapped to created_at",
			filters: &model.TransactionValidationFilters{
				SortBy:    "",
				SortOrder: "DESC",
			},
			wantSortBy:    "created_at",
			wantSortOrder: "DESC",
			wantErr:       false,
		},
		{
			name: "Success - lowercase sortOrder normalized to uppercase",
			filters: &model.TransactionValidationFilters{
				SortBy:    "createdAt",
				SortOrder: "desc",
			},
			wantSortBy:    "created_at",
			wantSortOrder: "DESC",
			wantErr:       false,
		},
		{
			name: "Success - mixed case sortOrder normalized to uppercase",
			filters: &model.TransactionValidationFilters{
				SortBy:    "createdAt",
				SortOrder: "AsC",
			},
			wantSortBy:    "created_at",
			wantSortOrder: "ASC",
			wantErr:       false,
		},
		{
			name: "Success - empty sortOrder defaults to DESC",
			filters: &model.TransactionValidationFilters{
				SortBy:    "createdAt",
				SortOrder: "",
			},
			wantSortBy:    "created_at",
			wantSortOrder: "DESC",
			wantErr:       false,
		},
		{
			name: "Error - invalid sortBy rejected by allowlist",
			filters: &model.TransactionValidationFilters{
				SortBy:    "invalid_column",
				SortOrder: "DESC",
			},
			wantErr: true,
			errIs:   constant.ErrInvalidSortColumn,
		},
		{
			name: "Error - SQL injection attempt rejected",
			filters: &model.TransactionValidationFilters{
				SortBy:    "created_at; DROP TABLE users;--",
				SortOrder: "DESC",
			},
			wantErr: true,
			errIs:   constant.ErrInvalidSortColumn,
		},
		{
			name: "Success - invalid sortOrder defaults to DESC (defense-in-depth)",
			filters: &model.TransactionValidationFilters{
				SortBy:    "createdAt",
				SortOrder: "INVALID",
			},
			wantSortBy:    "created_at",
			wantSortOrder: "DESC",
			wantErr:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sortBy, sortOrder, err := repo.validateAndNormalizeSort(tt.filters)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errIs != nil {
					assert.ErrorIs(t, err, tt.errIs)
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantSortBy, sortBy, "sortBy mismatch")
				assert.Equal(t, tt.wantSortOrder, sortOrder, "sortOrder mismatch")
			}
		})
	}
}

func TestBuildNextCursor_Integration(t *testing.T) {
	testutil.SetupTestTracing(t)

	repo, _, cleanup := setupTransactionValidationRepositoryMockDB(t)
	defer cleanup()

	fixedTime := time.Date(2024, 6, 15, 10, 30, 0, 123456789, time.UTC)
	validation := &model.TransactionValidation{
		ID:        testutil.MustDeterministicUUID(1),
		CreatedAt: fixedTime,
	}

	tests := []struct {
		name              string
		sortBy            string
		sortOrder         string
		wantCursorSortBy  string
		wantCursorOrder   string
		wantCursorValue   string
		wantErr           bool
		errIs             error
	}{
		{
			name:              "Success - snake_case created_at with DESC",
			sortBy:            "created_at",
			sortOrder:         "DESC",
			wantCursorSortBy:  "created_at",
			wantCursorOrder:   "DESC",
			wantCursorValue:   fixedTime.Format(time.RFC3339Nano),
			wantErr:           false,
		},
		{
			name:              "Success - snake_case created_at with ASC",
			sortBy:            "created_at",
			sortOrder:         "ASC",
			wantCursorSortBy:  "created_at",
			wantCursorOrder:   "ASC",
			wantCursorValue:   fixedTime.Format(time.RFC3339Nano),
			wantErr:           false,
		},
		{
			name:              "Success - lowercase sortOrder normalized to uppercase in cursor",
			sortBy:            "created_at",
			sortOrder:         "desc",
			wantCursorSortBy:  "created_at",
			wantCursorOrder:   "DESC",
			wantCursorValue:   fixedTime.Format(time.RFC3339Nano),
			wantErr:           false,
		},
		{
			name:              "Success - mixed case sortOrder normalized in cursor",
			sortBy:            "created_at",
			sortOrder:         "AsC",
			wantCursorSortBy:  "created_at",
			wantCursorOrder:   "ASC",
			wantCursorValue:   fixedTime.Format(time.RFC3339Nano),
			wantErr:           false,
		},
		{
			name:              "Success - invalid sortOrder defaults to DESC in cursor",
			sortBy:            "created_at",
			sortOrder:         "INVALID",
			wantCursorSortBy:  "created_at",
			wantCursorOrder:   "DESC",
			wantCursorValue:   fixedTime.Format(time.RFC3339Nano),
			wantErr:           false,
		},
		{
			name:      "Error - invalid sortBy rejected",
			sortBy:    "invalid_column",
			sortOrder: "DESC",
			wantErr:   true,
			errIs:     constant.ErrInvalidSortColumn,
		},
		{
			name:      "Error - camelCase sortBy rejected (buildNextCursor expects snake_case)",
			sortBy:    "createdAt",
			sortOrder: "DESC",
			wantErr:   true,
			errIs:     constant.ErrInvalidSortColumn,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cursor, err := repo.buildNextCursor(validation, tt.sortBy, tt.sortOrder)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errIs != nil {
					assert.ErrorIs(t, err, tt.errIs)
				}
			} else {
				require.NoError(t, err)
				require.NotEmpty(t, cursor)

				// Decode and verify cursor contents
				decoded, decodeErr := pkgHTTP.DecodeCursor(cursor)
				require.NoError(t, decodeErr)

				assert.Equal(t, validation.ID.String(), decoded.ID)
				assert.Equal(t, tt.wantCursorSortBy, decoded.SortBy)
				assert.Equal(t, tt.wantCursorOrder, decoded.SortOrder)
				assert.Equal(t, tt.wantCursorValue, decoded.SortValue)
				assert.True(t, decoded.PointsNext)
			}
		})
	}
}

func TestValidateAndNormalizeSort_FullPipeline(t *testing.T) {
	testutil.SetupTestTracing(t)

	repo, _, cleanup := setupTransactionValidationRepositoryMockDB(t)
	defer cleanup()

	fixedTime := time.Date(2024, 6, 15, 10, 30, 0, 123456789, time.UTC)
	validation := &model.TransactionValidation{
		ID:        testutil.MustDeterministicUUID(1),
		CreatedAt: fixedTime,
	}

	tests := []struct {
		name             string
		inputSortBy      string
		inputSortOrder   string
		wantCursorSortBy string
		wantCursorOrder  string
		wantCursorValue  string
	}{
		{
			name:             "Full pipeline - camelCase input produces correct cursor",
			inputSortBy:      "createdAt",
			inputSortOrder:   "desc",
			wantCursorSortBy: "created_at",
			wantCursorOrder:  "DESC",
			wantCursorValue:  fixedTime.Format(time.RFC3339Nano),
		},
		{
			name:             "Full pipeline - empty sortBy defaults correctly",
			inputSortBy:      "",
			inputSortOrder:   "",
			wantCursorSortBy: "created_at",
			wantCursorOrder:  "DESC",
			wantCursorValue:  fixedTime.Format(time.RFC3339Nano),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filters := &model.TransactionValidationFilters{
				SortBy:    tt.inputSortBy,
				SortOrder: tt.inputSortOrder,
			}

			// Step 1: Validate and normalize sort
			normalizedSortBy, normalizedSortOrder, err := repo.validateAndNormalizeSort(filters)
			require.NoError(t, err)

			// Step 2: Get sort value from validation
			sortValue, err := getSortValueFromValidation(validation, normalizedSortBy)
			require.NoError(t, err)
			assert.Equal(t, tt.wantCursorValue, sortValue)

			// Step 3: Build cursor with normalized values
			cursor, err := repo.buildNextCursor(validation, normalizedSortBy, normalizedSortOrder)
			require.NoError(t, err)

			// Step 4: Decode and verify cursor
			decoded, err := pkgHTTP.DecodeCursor(cursor)
			require.NoError(t, err)

			assert.Equal(t, validation.ID.String(), decoded.ID)
			assert.Equal(t, tt.wantCursorSortBy, decoded.SortBy)
			assert.Equal(t, tt.wantCursorOrder, decoded.SortOrder)
			assert.Equal(t, tt.wantCursorValue, decoded.SortValue)
		})
	}
}

// TestTransactionValidationRepository_List_JSONBFilterKeys verifies that JSONB
// filters use the correct JSON key names (accountId, segmentId, portfolioId)
// as defined in the model JSON tags, instead of generic 'id'.
func TestTransactionValidationRepository_List_JSONBFilterKeys(t *testing.T) {
	testutil.SetupTestTracing(t)

	tests := []struct {
		name            string
		filters         *model.TransactionValidationFilters
		expectedPattern string // Regex pattern that MUST match the SQL query
		description     string
	}{
		{
			name: "AccountID filter must use accountId JSON key",
			filters: &model.TransactionValidationFilters{
				AccountID: testutil.UUIDPtr(testutil.MustDeterministicUUID(200)),
				Limit:     10,
			},
			expectedPattern: `account->>'accountId'`,
			description:     "AccountContext serializes ID as 'accountId' per json tag",
		},
		{
			name: "SegmentID filter must use segmentId JSON key",
			filters: &model.TransactionValidationFilters{
				SegmentID: testutil.UUIDPtr(testutil.MustDeterministicUUID(300)),
				Limit:     10,
			},
			expectedPattern: `segment->>'segmentId'`,
			description:     "SegmentContext serializes ID as 'segmentId' per json tag",
		},
		{
			name: "PortfolioID filter must use portfolioId JSON key",
			filters: &model.TransactionValidationFilters{
				PortfolioID: testutil.UUIDPtr(testutil.MustDeterministicUUID(400)),
				Limit:       10,
			},
			expectedPattern: `portfolio->>'portfolioId'`,
			description:     "PortfolioContext serializes ID as 'portfolioId' per json tag",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, sqlMock, cleanup := setupTransactionValidationRepositoryMockDB(t)
			defer cleanup()

			tv := testTransactionValidation()
			rows := transactionValidationRow(t, tv)

			// Expect the query to contain the correct JSONB key
			// If the implementation uses wrong key (e.g., account->>'id'),
			// this expectation will NOT match and the test will fail
			sqlMock.ExpectQuery(tt.expectedPattern).
				WillReturnRows(rows)

			ctx := context.Background()
			result, err := repo.List(ctx, tt.filters)

			require.NoError(t, err, "List should succeed: %s", tt.description)
			require.NotNil(t, result)

			// Verify all expectations were met - this is the key assertion
			// If the SQL doesn't contain the expected pattern, ExpectationsWereMet will fail
			err = sqlMock.ExpectationsWereMet()
			require.NoError(t, err, "SQL query must contain %s because %s",
				tt.expectedPattern, tt.description)
		})
	}
}
