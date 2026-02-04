// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package model

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestScope_ToMap(t *testing.T) {
	t.Parallel()

	t.Run("Success - empty scope returns empty map", func(t *testing.T) {
		scope := &Scope{}
		result := scope.ToMap()

		assert.NotNil(t, result, "Result should not be nil")
		assert.Empty(t, result, "Result should be empty map")
	})

	t.Run("Success - scope with only SegmentID", func(t *testing.T) {
		segmentID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440001")
		scope := &Scope{
			SegmentID: &segmentID,
		}

		result := scope.ToMap()

		assert.Len(t, result, 1)
		assert.Equal(t, segmentID.String(), result["segmentId"])
	})

	t.Run("Success - scope with only PortfolioID", func(t *testing.T) {
		portfolioID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440002")
		scope := &Scope{
			PortfolioID: &portfolioID,
		}

		result := scope.ToMap()

		assert.Len(t, result, 1)
		assert.Equal(t, portfolioID.String(), result["portfolioId"])
	})

	t.Run("Success - scope with only AccountID", func(t *testing.T) {
		accountID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440003")
		scope := &Scope{
			AccountID: &accountID,
		}

		result := scope.ToMap()

		assert.Len(t, result, 1)
		assert.Equal(t, accountID.String(), result["accountId"])
	})

	t.Run("Success - scope with only MerchantID", func(t *testing.T) {
		merchantID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440004")
		scope := &Scope{
			MerchantID: &merchantID,
		}

		result := scope.ToMap()

		assert.Len(t, result, 1)
		assert.Equal(t, merchantID.String(), result["merchantId"])
	})

	t.Run("Success - scope with only TransactionType", func(t *testing.T) {
		txType := TransactionTypeCard
		scope := &Scope{
			TransactionType: &txType,
		}

		result := scope.ToMap()

		assert.Len(t, result, 1)
		assert.Equal(t, txType.String(), result["transactionType"])
	})

	t.Run("Success - scope with only SubType", func(t *testing.T) {
		subType := "CREDIT"
		scope := &Scope{
			SubType: &subType,
		}

		result := scope.ToMap()

		assert.Len(t, result, 1)
		assert.Equal(t, subType, result["subType"])
	})

	t.Run("Success - scope with nil SubType pointer is excluded", func(t *testing.T) {
		accountID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440003")
		scope := &Scope{
			AccountID: &accountID,
			SubType:   nil,
		}

		result := scope.ToMap()

		assert.Len(t, result, 1)
		assert.Equal(t, accountID.String(), result["accountId"])
		assert.NotContains(t, result, "subType", "nil SubType should not be included")
	})

	t.Run("Success - scope with non-nil SubType pointer is included", func(t *testing.T) {
		accountID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440003")
		subType := "INSTANT"
		scope := &Scope{
			AccountID: &accountID,
			SubType:   &subType,
		}

		result := scope.ToMap()

		assert.Len(t, result, 2)
		assert.Equal(t, accountID.String(), result["accountId"])
		assert.Equal(t, "INSTANT", result["subType"])
	})

	t.Run("Success - scope with nil TransactionType pointer is excluded", func(t *testing.T) {
		accountID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440003")
		scope := &Scope{
			AccountID:       &accountID,
			TransactionType: nil,
		}

		result := scope.ToMap()

		assert.Len(t, result, 1)
		assert.Equal(t, accountID.String(), result["accountId"])
		assert.NotContains(t, result, "transactionType", "nil TransactionType should not be included")
	})

	t.Run("Success - scope with non-nil TransactionType pointer is included", func(t *testing.T) {
		accountID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440003")
		txType := TransactionTypePix
		scope := &Scope{
			AccountID:       &accountID,
			TransactionType: &txType,
		}

		result := scope.ToMap()

		assert.Len(t, result, 2)
		assert.Equal(t, accountID.String(), result["accountId"])
		assert.Equal(t, "PIX", result["transactionType"])
	})

	t.Run("Success - scope with all fields populated", func(t *testing.T) {
		segmentID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440001")
		portfolioID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440002")
		accountID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440003")
		merchantID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440004")
		txType := TransactionTypeCard
		subType := "CREDIT"

		scope := &Scope{
			SegmentID:       &segmentID,
			PortfolioID:     &portfolioID,
			AccountID:       &accountID,
			MerchantID:      &merchantID,
			TransactionType: &txType,
			SubType:         &subType,
		}

		result := scope.ToMap()

		assert.Len(t, result, 6, "All 6 fields should be included")
		assert.Equal(t, segmentID.String(), result["segmentId"])
		assert.Equal(t, portfolioID.String(), result["portfolioId"])
		assert.Equal(t, accountID.String(), result["accountId"])
		assert.Equal(t, merchantID.String(), result["merchantId"])
		assert.Equal(t, txType.String(), result["transactionType"])
		assert.Equal(t, subType, result["subType"])
	})

	t.Run("Success - scope with mixed populated fields", func(t *testing.T) {
		accountID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440003")
		txType := TransactionTypeWire
		subType := "INTERNATIONAL"

		scope := &Scope{
			AccountID:       &accountID,
			TransactionType: &txType,
			SubType:         &subType,
		}

		result := scope.ToMap()

		assert.Len(t, result, 3)
		assert.Equal(t, accountID.String(), result["accountId"])
		assert.Equal(t, "WIRE", result["transactionType"])
		assert.Equal(t, "INTERNATIONAL", result["subType"])
		assert.NotContains(t, result, "segmentId")
		assert.NotContains(t, result, "portfolioId")
		assert.NotContains(t, result, "merchantId")
	})

	t.Run("Success - map keys use camelCase", func(t *testing.T) {
		segmentID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440001")
		portfolioID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440002")
		accountID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440003")
		merchantID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440004")
		txType := TransactionTypeCard
		subType := "CREDIT"

		scope := &Scope{
			SegmentID:       &segmentID,
			PortfolioID:     &portfolioID,
			AccountID:       &accountID,
			MerchantID:      &merchantID,
			TransactionType: &txType,
			SubType:         &subType,
		}

		result := scope.ToMap()

		// Verify camelCase keys
		assert.Contains(t, result, "segmentId")
		assert.Contains(t, result, "portfolioId")
		assert.Contains(t, result, "accountId")
		assert.Contains(t, result, "merchantId")
		assert.Contains(t, result, "transactionType")
		assert.Contains(t, result, "subType")

		// Verify PascalCase keys do NOT exist
		assert.NotContains(t, result, "SegmentID")
		assert.NotContains(t, result, "PortfolioID")
		assert.NotContains(t, result, "AccountID")
		assert.NotContains(t, result, "MerchantID")
		assert.NotContains(t, result, "TransactionType")
		assert.NotContains(t, result, "SubType")
	})

	t.Run("Success - UUID values are converted to strings", func(t *testing.T) {
		accountID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440003")
		scope := &Scope{
			AccountID: &accountID,
		}

		result := scope.ToMap()

		value, ok := result["accountId"]
		assert.True(t, ok, "accountId key should exist")
		assert.IsType(t, "", value, "UUID should be converted to string")
		assert.Equal(t, "550e8400-e29b-41d4-a716-446655440003", value)
	})

	t.Run("Success - TransactionType values use String() method", func(t *testing.T) {
		testCases := []struct {
			txType   TransactionType
			expected string
		}{
			{TransactionTypeCard, "CARD"},
			{TransactionTypePix, "PIX"},
			{TransactionTypeWire, "WIRE"},
		}

		for _, tc := range testCases {
			t.Run(tc.expected, func(t *testing.T) {
				scope := &Scope{
					TransactionType: &tc.txType,
				}

				result := scope.ToMap()

				assert.Equal(t, tc.expected, result["transactionType"])
			})
		}
	})
}
