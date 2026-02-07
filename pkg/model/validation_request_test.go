// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package model

import (
	"testing"

	"time"

	"github.com/shopspring/decimal"

	"tracer/internal/testutil"
	"tracer/pkg/constant"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidationRequest_Validate(t *testing.T) {
	validRequest := func() *ValidationRequest {
		accountID := testutil.MustDeterministicUUID(1)
		return &ValidationRequest{
			RequestID:            testutil.MustDeterministicUUID(2),
			TransactionType:      TransactionTypeCard,
			Amount:               decimal.RequireFromString("100"), // $100.00
			Currency:             "USD",
			TransactionTimestamp: testutil.FixedTime(),
			Account: AccountContext{
				ID:     accountID,
				Type:   "checking",
				Status: "active",
			},
		}
	}

	tests := []struct {
		name        string
		modify      func(*ValidationRequest)
		expectedErr error
	}{
		{
			name:        "valid request passes validation",
			modify:      func(r *ValidationRequest) {},
			expectedErr: nil,
		},
		{
			name: "missing requestId fails",
			modify: func(r *ValidationRequest) {
				r.RequestID = uuid.Nil
			},
			expectedErr: constant.ErrValidationRequestIDRequired,
		},
		{
			name: "invalid transactionType fails",
			modify: func(r *ValidationRequest) {
				r.TransactionType = TransactionType("INVALID")
			},
			expectedErr: constant.ErrValidationInvalidTransactionType,
		},
		{
			name: "empty transactionType fails",
			modify: func(r *ValidationRequest) {
				r.TransactionType = TransactionType("")
			},
			expectedErr: constant.ErrValidationInvalidTransactionType,
		},
		{
			name: "zero amount fails",
			modify: func(r *ValidationRequest) {
				r.Amount = decimal.RequireFromString("0")
			},
			expectedErr: constant.ErrValidationAmountNonPositive,
		},
		{
			name: "negative amount fails",
			modify: func(r *ValidationRequest) {
				r.Amount = decimal.RequireFromString("-1")
			},
			expectedErr: constant.ErrValidationAmountNonPositive,
		},
		{
			name: "empty currency fails",
			modify: func(r *ValidationRequest) {
				r.Currency = ""
			},
			expectedErr: constant.ErrValidationCurrencyRequired,
		},
		{
			name: "invalid currency format fails",
			modify: func(r *ValidationRequest) {
				r.Currency = "INVALID"
			},
			expectedErr: constant.ErrValidationInvalidCurrency,
		},
		{
			name: "too short currency fails",
			modify: func(r *ValidationRequest) {
				r.Currency = "US"
			},
			expectedErr: constant.ErrValidationInvalidCurrency,
		},
		{
			name: "too long currency fails",
			modify: func(r *ValidationRequest) {
				r.Currency = "USDD"
			},
			expectedErr: constant.ErrValidationInvalidCurrency,
		},
		{
			name: "zero timestamp fails",
			modify: func(r *ValidationRequest) {
				r.TransactionTimestamp = time.Time{}
			},
			expectedErr: constant.ErrValidationTimestampRequired,
		},
		{
			name: "future timestamp fails",
			modify: func(r *ValidationRequest) {
				// Set timestamp 2 minutes in the future (beyond 1 minute clock skew allowance)
				// Note: Must use time.Now() as the validation logic compares against actual current time
				r.TransactionTimestamp = time.Now().Add(2 * time.Minute)
			},
			expectedErr: constant.ErrValidationTimestampFuture,
		},
		{
			name: "timestamp within clock skew tolerance passes",
			modify: func(r *ValidationRequest) {
				// Set timestamp 30 seconds in the future (within 1 minute clock skew allowance)
				// Note: Must use time.Now() as the validation logic compares against actual current time
				r.TransactionTimestamp = time.Now().Add(30 * time.Second)
			},
			expectedErr: nil,
		},
		{
			name: "missing account ID fails",
			modify: func(r *ValidationRequest) {
				r.Account.ID = uuid.Nil
			},
			expectedErr: constant.ErrValidationAccountRequired,
		},
		{
			name: "segment with nil ID fails",
			modify: func(r *ValidationRequest) {
				r.Segment = &SegmentContext{ID: uuid.Nil}
			},
			expectedErr: constant.ErrValidationSegmentIDRequired,
		},
		{
			name: "portfolio with nil ID fails",
			modify: func(r *ValidationRequest) {
				r.Portfolio = &PortfolioContext{ID: uuid.Nil}
			},
			expectedErr: constant.ErrValidationPortfolioIDRequired,
		},
		{
			name: "valid segment passes",
			modify: func(r *ValidationRequest) {
				r.Segment = &SegmentContext{ID: testutil.MustDeterministicUUID(3), Name: "retail"}
			},
			expectedErr: nil,
		},
		{
			name: "valid portfolio passes",
			modify: func(r *ValidationRequest) {
				r.Portfolio = &PortfolioContext{ID: testutil.MustDeterministicUUID(4), Name: "premium"}
			},
			expectedErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := validRequest()
			tt.modify(req)

			err := req.Validate()

			if tt.expectedErr == nil {
				assert.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, tt.expectedErr)
			}
		})
	}
}

func TestValidationRequest_ToCheckLimitsInput(t *testing.T) {
	t.Run("converts required fields correctly", func(t *testing.T) {
		subType := "Credit"
		accountID := testutil.MustDeterministicUUID(30)
		segmentID := testutil.MustDeterministicUUID(31)
		portfolioID := testutil.MustDeterministicUUID(32)
		timestamp := testutil.FixedTime()
		req := &ValidationRequest{
			RequestID:            testutil.MustDeterministicUUID(33),
			TransactionType:      TransactionTypeCard,
			SubType:              &subType,
			Amount:               decimal.RequireFromString("500"),
			Currency:             "USD",
			TransactionTimestamp: timestamp,
			Account: AccountContext{
				ID: accountID,
			},
			Segment:   &SegmentContext{ID: segmentID},
			Portfolio: &PortfolioContext{ID: portfolioID},
		}

		input := req.ToCheckLimitsInput()

		require.NotNil(t, input)
		assert.Equal(t, req.Amount, input.Amount)
		assert.Equal(t, req.Currency, input.Currency)
		assert.Equal(t, accountID, input.AccountID)
		assert.Equal(t, &segmentID, input.SegmentID)
		assert.Equal(t, &portfolioID, input.PortfolioID)
		assert.Equal(t, timestamp, input.TransactionTimestamp)
	})

	t.Run("handles nil segment and portfolio", func(t *testing.T) {
		accountID := testutil.MustDeterministicUUID(40)
		req := &ValidationRequest{
			RequestID:            testutil.MustDeterministicUUID(41),
			TransactionType:      TransactionTypePix,
			SubType:              nil,
			Amount:               decimal.RequireFromString("100"),
			Currency:             "BRL",
			TransactionTimestamp: testutil.FixedTime(),
			Account: AccountContext{
				ID: accountID,
			},
			Segment:   nil,
			Portfolio: nil,
		}

		input := req.ToCheckLimitsInput()

		require.NotNil(t, input)
		assert.Equal(t, accountID, input.AccountID)
		assert.Nil(t, input.SegmentID)
		assert.Nil(t, input.PortfolioID)
	})
}
