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

func TestDecision_IsValid(t *testing.T) {
	tests := []struct {
		name     string
		decision Decision
		want     bool
	}{
		{
			name:     "ALLOW is valid",
			decision: DecisionAllow,
			want:     true,
		},
		{
			name:     "DENY is valid",
			decision: DecisionDeny,
			want:     true,
		},
		{
			name:     "REVIEW is valid",
			decision: DecisionReview,
			want:     true,
		},
		{
			name:     "empty string is invalid",
			decision: Decision(""),
			want:     false,
		},
		{
			name:     "lowercase allow is invalid",
			decision: Decision("allow"),
			want:     false,
		},
		{
			name:     "random string is invalid",
			decision: Decision("INVALID"),
			want:     false,
		},
		{
			name:     "partial match is invalid",
			decision: Decision("ALLO"),
			want:     false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.decision.IsValid()
			assert.Equal(t, tc.want, result)
		})
	}
}

func TestNewValidationResponse(t *testing.T) {
	tests := []struct {
		name         string
		validationID uuid.UUID
		requestID    uuid.UUID
		decision     Decision
	}{
		{
			name:         "creates response with ALLOW decision",
			validationID: uuid.MustParse("550e8400-e29b-41d4-a716-446655440000"),
			requestID:    uuid.MustParse("550e8400-e29b-41d4-a716-446655440001"),
			decision:     DecisionAllow,
		},
		{
			name:         "creates response with DENY decision",
			validationID: uuid.MustParse("550e8400-e29b-41d4-a716-446655440010"),
			requestID:    uuid.MustParse("550e8400-e29b-41d4-a716-446655440002"),
			decision:     DecisionDeny,
		},
		{
			name:         "creates response with REVIEW decision",
			validationID: uuid.MustParse("550e8400-e29b-41d4-a716-446655440020"),
			requestID:    uuid.MustParse("550e8400-e29b-41d4-a716-446655440003"),
			decision:     DecisionReview,
		},
		{
			name:         "creates response with invalid decision (documents current behavior)",
			validationID: uuid.MustParse("550e8400-e29b-41d4-a716-446655440030"),
			requestID:    uuid.MustParse("550e8400-e29b-41d4-a716-446655440004"),
			decision:     Decision("INVALID"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := NewValidationResponse(tc.validationID, tc.requestID, tc.decision)

			require.NotNil(t, result)
			assert.Equal(t, tc.validationID, result.ValidationID)
			assert.Equal(t, tc.requestID, result.RequestID)
			assert.Equal(t, tc.decision, result.Decision)

			// Verify slices are initialized (not nil) for proper JSON serialization
			assert.NotNil(t, result.MatchedRuleIDs, "MatchedRuleIDs should be initialized")
			assert.NotNil(t, result.EvaluatedRuleIDs, "EvaluatedRuleIDs should be initialized")
			assert.NotNil(t, result.LimitUsageDetails, "LimitUsageDetails should be initialized")

			// Verify slices are empty
			assert.Empty(t, result.MatchedRuleIDs)
			assert.Empty(t, result.EvaluatedRuleIDs)
			assert.Empty(t, result.LimitUsageDetails)

			// Verify optional fields are zero values
			assert.Empty(t, result.Reason)
			assert.Zero(t, result.ProcessingTimeMs)
		})
	}
}

func TestDecision_String(t *testing.T) {
	tests := []struct {
		name     string
		decision Decision
		want     string
	}{
		{
			name:     "ALLOW returns ALLOW string",
			decision: DecisionAllow,
			want:     "ALLOW",
		},
		{
			name:     "DENY returns DENY string",
			decision: DecisionDeny,
			want:     "DENY",
		},
		{
			name:     "REVIEW returns REVIEW string",
			decision: DecisionReview,
			want:     "REVIEW",
		},
		{
			name:     "invalid value returns raw string",
			decision: Decision("INVALID"),
			want:     "INVALID",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.decision.String())
		})
	}
}

func TestValidationRequest_ToTransactionScope(t *testing.T) {
	accountID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440001")
	segmentID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440002")
	portfolioID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440003")
	merchantID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440004")
	txTypeCard := TransactionTypeCard
	txTypePix := TransactionTypePix
	subType := "debit"

	tests := []struct {
		name    string
		input   ValidationRequest
		want    *Scope
		wantErr bool
	}{
		{
			name: "minimal request with account only",
			input: ValidationRequest{
				TransactionType: TransactionTypePix,
				Account:         AccountContext{ID: accountID},
			},
			want: &Scope{
				AccountID:       &accountID,
				TransactionType: &txTypePix,
			},
		},
		{
			name: "request with all optional fields",
			input: ValidationRequest{
				TransactionType: txTypeCard,
				SubType:         &subType,
				Account:         AccountContext{ID: accountID},
				Segment:         &SegmentContext{ID: segmentID},
				Portfolio:       &PortfolioContext{ID: portfolioID},
				Merchant:        &MerchantContext{ID: merchantID},
			},
			want: &Scope{
				AccountID:       &accountID,
				SegmentID:       &segmentID,
				PortfolioID:     &portfolioID,
				MerchantID:      &merchantID,
				TransactionType: &txTypeCard,
				SubType:         &subType,
			},
		},
		{
			name: "request with segment only",
			input: ValidationRequest{
				TransactionType: txTypeCard,
				Account:         AccountContext{ID: accountID},
				Segment:         &SegmentContext{ID: segmentID},
			},
			want: &Scope{
				AccountID:       &accountID,
				SegmentID:       &segmentID,
				TransactionType: &txTypeCard,
			},
		},
		{
			name: "request with portfolio only",
			input: ValidationRequest{
				TransactionType: txTypeCard,
				Account:         AccountContext{ID: accountID},
				Portfolio:       &PortfolioContext{ID: portfolioID},
			},
			want: &Scope{
				AccountID:       &accountID,
				PortfolioID:     &portfolioID,
				TransactionType: &txTypeCard,
			},
		},
		{
			name: "request with merchant only",
			input: ValidationRequest{
				TransactionType: txTypeCard,
				Account:         AccountContext{ID: accountID},
				Merchant:        &MerchantContext{ID: merchantID},
			},
			want: &Scope{
				AccountID:       &accountID,
				MerchantID:      &merchantID,
				TransactionType: &txTypeCard,
			},
		},
		{
			name: "request with subtype",
			input: ValidationRequest{
				TransactionType: txTypeCard,
				SubType:         &subType,
				Account:         AccountContext{ID: accountID},
			},
			want: &Scope{
				AccountID:       &accountID,
				TransactionType: &txTypeCard,
				SubType:         &subType,
			},
		},
		{
			name: "request with zero account ID",
			input: ValidationRequest{
				TransactionType: TransactionTypePix,
				Account:         AccountContext{ID: uuid.UUID{}},
			},
			want: &Scope{
				AccountID:       func() *uuid.UUID { id := uuid.UUID{}; return &id }(),
				TransactionType: &txTypePix,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.input.ToTransactionScope()

			require.NotNil(t, result, "ToTransactionScope should not return nil")
			assert.Equal(t, tc.want.AccountID, result.AccountID, "AccountID mismatch")
			assert.Equal(t, tc.want.SegmentID, result.SegmentID, "SegmentID mismatch")
			assert.Equal(t, tc.want.PortfolioID, result.PortfolioID, "PortfolioID mismatch")
			assert.Equal(t, tc.want.MerchantID, result.MerchantID, "MerchantID mismatch")
			assert.Equal(t, tc.want.TransactionType, result.TransactionType, "TransactionType mismatch")
			assert.Equal(t, tc.want.SubType, result.SubType, "SubType mismatch")
		})
	}
}

func TestValidationRequest_Validate_MerchantID(t *testing.T) {
	validAccountID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440001")
	validMerchantID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440002")
	validRequestID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440003")
	fixedTimestamp := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)

	baseRequest := func() ValidationRequest {
		return ValidationRequest{
			RequestID:            validRequestID,
			TransactionType:      TransactionTypeCard,
			Amount:               1000,
			Currency:             "BRL",
			TransactionTimestamp: fixedTimestamp,
			Account:              AccountContext{ID: validAccountID},
		}
	}

	tests := []struct {
		name    string
		modify  func(*ValidationRequest)
		wantErr error
	}{
		{
			name:    "valid request without merchant",
			modify:  func(r *ValidationRequest) {},
			wantErr: nil,
		},
		{
			name: "valid request with merchant ID",
			modify: func(r *ValidationRequest) {
				r.Merchant = &MerchantContext{ID: validMerchantID, Category: "5411", Country: "BR"}
			},
			wantErr: nil,
		},
		{
			name: "invalid - merchant provided with nil ID",
			modify: func(r *ValidationRequest) {
				r.Merchant = &MerchantContext{ID: uuid.Nil, Category: "5411", Country: "BR"}
			},
			wantErr: constant.ErrValidationMerchantIDRequired,
		},
		{
			name: "invalid - merchant provided with zero UUID",
			modify: func(r *ValidationRequest) {
				r.Merchant = &MerchantContext{Category: "5411"}
			},
			wantErr: constant.ErrValidationMerchantIDRequired,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := baseRequest()
			tc.modify(&req)

			err := req.Validate()

			if tc.wantErr != nil {
				assert.ErrorIs(t, err, tc.wantErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
