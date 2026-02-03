// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package model

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"tracer/pkg/constant"
)

func TestValidationRequest_Validate(t *testing.T) {
	validRequest := func() *ValidationRequest {
		accountID := uuid.New()
		return &ValidationRequest{
			RequestID:       uuid.New(),
			TransactionType: TransactionTypeCard,
			Amount:          10000, // $100.00 in cents
			Currency:        "USD",
			TransactionTimestamp:       time.Now(),
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
				r.Amount = 0
			},
			expectedErr: constant.ErrValidationAmountNonPositive,
		},
		{
			name: "negative amount fails",
			modify: func(r *ValidationRequest) {
				r.Amount = -100
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
				r.TransactionTimestamp = time.Now().Add(2 * time.Minute)
			},
			expectedErr: constant.ErrValidationTimestampFuture,
		},
		{
			name: "timestamp within clock skew tolerance passes",
			modify: func(r *ValidationRequest) {
				// Set timestamp 30 seconds in the future (within 1 minute clock skew allowance)
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
				r.Segment = &SegmentContext{ID: uuid.New(), Name: "retail"}
			},
			expectedErr: nil,
		},
		{
			name: "valid portfolio passes",
			modify: func(r *ValidationRequest) {
				r.Portfolio = &PortfolioContext{ID: uuid.New(), Name: "premium"}
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

func TestValidationRequest_ToTransactionContext(t *testing.T) {
	subType := "Credit"
	segmentID := uuid.New()
	portfolioID := uuid.New()
	req := &ValidationRequest{
		RequestID:       uuid.New(),
		TransactionType: TransactionTypeCard,
		SubType:         &subType,
		Amount:          50000,
		Currency:        "BRL",
		TransactionTimestamp:       time.Now(),
		Account: AccountContext{
			ID:     uuid.New(),
			Type:   "checking",
			Status: "active",
		},
		Merchant: &MerchantContext{
			ID:       uuid.New(),
			Category: "RETAIL",
			Country:  "BR",
		},
		Segment:   &SegmentContext{ID: segmentID, Name: "retail"},
		Portfolio: &PortfolioContext{ID: portfolioID, Name: "premium"},
		Metadata:  map[string]any{"channel": "mobile"},
	}

	ctx := req.ToTransactionContext()

	require.NotNil(t, ctx)
	assert.Equal(t, req.TransactionType, ctx.TransactionType)
	assert.Equal(t, req.SubType, ctx.SubType)
	assert.Equal(t, req.Amount, ctx.Amount)
	assert.Equal(t, req.Currency, ctx.Currency)
	assert.Equal(t, req.TransactionTimestamp, ctx.TransactionTimestamp)
	assert.Equal(t, req.Account, ctx.Account)
	assert.Equal(t, req.Merchant, ctx.Merchant)
	assert.Equal(t, req.Segment, ctx.Segment)
	assert.Equal(t, req.Portfolio, ctx.Portfolio)
	assert.Equal(t, req.Metadata, ctx.Metadata)
}

func TestValidationRequest_ToTransactionContext_NilOptionalFields(t *testing.T) {
	req := &ValidationRequest{
		RequestID:       uuid.New(),
		TransactionType: TransactionTypePix,
		SubType:         nil,
		Amount:          10000,
		Currency:        "BRL",
		TransactionTimestamp:       time.Now(),
		Account: AccountContext{
			ID: uuid.New(),
		},
		Merchant:  nil,
		Segment:   nil,
		Portfolio: nil,
		Metadata:  nil,
	}

	ctx := req.ToTransactionContext()

	require.NotNil(t, ctx)
	assert.Nil(t, ctx.SubType)
	assert.Nil(t, ctx.Merchant)
	assert.Nil(t, ctx.Segment)
	assert.Nil(t, ctx.Portfolio)
	assert.Nil(t, ctx.Metadata)
}

func TestValidationRequest_ToCheckLimitsInput(t *testing.T) {
	t.Run("converts required fields correctly", func(t *testing.T) {
		subType := "Credit"
		accountID := uuid.New()
		segmentID := uuid.New()
		portfolioID := uuid.New()
		timestamp := time.Now()
		req := &ValidationRequest{
			RequestID:       uuid.New(),
			TransactionType: TransactionTypeCard,
			SubType:         &subType,
			Amount:          50000,
			Currency:        "USD",
			TransactionTimestamp:       timestamp,
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
		accountID := uuid.New()
		req := &ValidationRequest{
			RequestID:       uuid.New(),
			TransactionType: TransactionTypePix,
			SubType:         nil,
			Amount:          10000,
			Currency:        "BRL",
			TransactionTimestamp:       time.Now(),
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
