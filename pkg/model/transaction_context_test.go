// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package model

import (
	"testing"

	"github.com/shopspring/decimal"
	"time"

	"tracer/internal/testutil"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTransactionContext_ToMap(t *testing.T) {
	accountID := testutil.MustDeterministicUUID(1)
	merchantID := testutil.MustDeterministicUUID(2)
	segmentID := testutil.MustDeterministicUUID(3)
	portfolioID := testutil.MustDeterministicUUID(4)

	tests := []struct {
		name     string
		ctx      *TransactionContext
		validate func(*testing.T, map[string]any)
	}{
		{
			name: "complete context with all fields",
			ctx: &TransactionContext{
				TransactionType: TransactionTypeCard,
				SubType:         testutil.StringPtr("debit"),
				Amount:          decimal.RequireFromString("1000"),
				Currency:        "USD",
				TransactionTimestamp:       time.Date(2030, 12, 24, 10, 0, 0, 0, time.UTC),
				Account: AccountContext{
					ID:     accountID,
					Type:   "checking",
					Status: "active",
				},
				Merchant: &MerchantContext{
					ID:       merchantID,
					Name:     "Acme Store",
					Category: "5411",
					Country:  "US",
				},
				Segment:   &SegmentContext{ID: segmentID, Name: "retail"},
				Portfolio: &PortfolioContext{ID: portfolioID, Name: "premium"},
				Metadata:  map[string]any{"customField": "value"},
			},
			validate: func(t *testing.T, result map[string]any) {
				// Top-level fields
				assert.Equal(t, "CARD", result["transactionType"])
				assert.Equal(t, "debit", result["subType"])
				assert.Equal(t, float64(1000), result["amount"])
				assert.Equal(t, "USD", result["currency"])
				assert.Equal(t, time.Date(2030, 12, 24, 10, 0, 0, 0, time.UTC), result["transactionTimestamp"])

				// Account nested map
				account, ok := result["account"].(map[string]any)
				require.True(t, ok, "account should be a map")
				assert.Equal(t, accountID.String(), account["accountId"])
				assert.Equal(t, "checking", account["type"])
				assert.Equal(t, "active", account["status"])

				// Merchant nested map
				merchant, ok := result["merchant"].(map[string]any)
				require.True(t, ok, "merchant should be a map")
				assert.Equal(t, merchantID.String(), merchant["merchantId"])
				assert.Equal(t, "Acme Store", merchant["name"])
				assert.Equal(t, "5411", merchant["category"])
				assert.Equal(t, "US", merchant["country"])

				// Segment nested map
				segment, ok := result["segment"].(map[string]any)
				require.True(t, ok, "segment should be a map")
				assert.NotEmpty(t, segment["segmentId"])
				assert.Equal(t, "retail", segment["name"])

				// Portfolio nested map
				portfolio, ok := result["portfolio"].(map[string]any)
				require.True(t, ok, "portfolio should be a map")
				assert.NotEmpty(t, portfolio["portfolioId"])
				assert.Equal(t, "premium", portfolio["name"])

				// Metadata map
				metadata, ok := result["metadata"].(map[string]any)
				require.True(t, ok, "metadata should be a map")
				assert.Equal(t, "value", metadata["customField"])
			},
		},
		{
			name: "minimal context without optional fields",
			ctx: &TransactionContext{
				TransactionType: TransactionTypeWire,
				Amount:          decimal.RequireFromString("500"),
				Currency:        "EUR",
				TransactionTimestamp:       time.Date(2030, 12, 24, 11, 0, 0, 0, time.UTC),
				Account: AccountContext{
					ID:   accountID,
					Type: "savings",
				},
			},
			validate: func(t *testing.T, result map[string]any) {
				// Required fields
				assert.Equal(t, "WIRE", result["transactionType"])
				assert.Equal(t, float64(500), result["amount"])
				assert.Equal(t, "EUR", result["currency"])

				// Account nested map
				account, ok := result["account"].(map[string]any)
				require.True(t, ok, "account should be a map")
				assert.Equal(t, accountID.String(), account["accountId"])
				assert.Equal(t, "savings", account["type"])

				// Optional fields should be nil
				assert.Nil(t, result["subType"])
				assert.Nil(t, result["merchant"])
			},
		},
		// Edge case tests
		{
			name: "zero amount is preserved",
			ctx: &TransactionContext{
				TransactionType: TransactionTypeCard,
				Amount:          decimal.RequireFromString("0"),
				Currency:        "USD",
				TransactionTimestamp:       time.Date(2030, 12, 24, 12, 0, 0, 0, time.UTC),
				Account: AccountContext{
					ID:   accountID,
					Type: "checking",
				},
			},
			validate: func(t *testing.T, result map[string]any) {
				assert.Equal(t, float64(0), result["amount"])
			},
		},
		{
			name: "negative amount is preserved",
			ctx: &TransactionContext{
				TransactionType: TransactionTypeCard,
				Amount:          decimal.RequireFromString("-500"),
				Currency:        "USD",
				TransactionTimestamp:       time.Date(2030, 12, 24, 12, 0, 0, 0, time.UTC),
				Account: AccountContext{
					ID:   accountID,
					Type: "checking",
				},
			},
			validate: func(t *testing.T, result map[string]any) {
				assert.Equal(t, float64(-500), result["amount"])
			},
		},
		{
			name: "empty currency string is preserved",
			ctx: &TransactionContext{
				TransactionType: TransactionTypeCard,
				Amount:          decimal.RequireFromString("100"),
				Currency:        "",
				TransactionTimestamp:       time.Date(2030, 12, 24, 12, 0, 0, 0, time.UTC),
				Account: AccountContext{
					ID:   accountID,
					Type: "checking",
				},
			},
			validate: func(t *testing.T, result map[string]any) {
				assert.Equal(t, "", result["currency"])
			},
		},
		{
			name: "empty account type and status are preserved",
			ctx: &TransactionContext{
				TransactionType: TransactionTypeCard,
				Amount:          decimal.RequireFromString("100"),
				Currency:        "USD",
				TransactionTimestamp:       time.Date(2030, 12, 24, 12, 0, 0, 0, time.UTC),
				Account: AccountContext{
					ID:     accountID,
					Type:   "",
					Status: "",
				},
			},
			validate: func(t *testing.T, result map[string]any) {
				account, ok := result["account"].(map[string]any)
				require.True(t, ok, "account should be a map")
				assert.Equal(t, "", account["type"])
				assert.Equal(t, "", account["status"])
			},
		},
		{
			name: "nil segment yields nil",
			ctx: &TransactionContext{
				TransactionType: TransactionTypeCard,
				Amount:          decimal.RequireFromString("100"),
				Currency:        "USD",
				TransactionTimestamp:       time.Date(2030, 12, 24, 12, 0, 0, 0, time.UTC),
				Account: AccountContext{
					ID:   accountID,
					Type: "checking",
				},
				Segment: nil,
			},
			validate: func(t *testing.T, result map[string]any) {
				assert.Nil(t, result["segment"])
			},
		},
		{
			name: "nil portfolio yields nil",
			ctx: &TransactionContext{
				TransactionType: TransactionTypeCard,
				Amount:          decimal.RequireFromString("100"),
				Currency:        "USD",
				TransactionTimestamp:       time.Date(2030, 12, 24, 12, 0, 0, 0, time.UTC),
				Account: AccountContext{
					ID:   accountID,
					Type: "checking",
				},
				Portfolio: nil,
			},
			validate: func(t *testing.T, result map[string]any) {
				assert.Nil(t, result["portfolio"])
			},
		},
		{
			name: "empty metadata map is preserved as empty",
			ctx: &TransactionContext{
				TransactionType: TransactionTypeCard,
				Amount:          decimal.RequireFromString("100"),
				Currency:        "USD",
				TransactionTimestamp:       time.Date(2030, 12, 24, 12, 0, 0, 0, time.UTC),
				Account: AccountContext{
					ID:   accountID,
					Type: "checking",
				},
				Metadata: map[string]any{},
			},
			validate: func(t *testing.T, result map[string]any) {
				metadata, ok := result["metadata"].(map[string]any)
				require.True(t, ok, "metadata should be a map")
				assert.Empty(t, metadata)
			},
		},
		{
			name: "nil metadata yields empty map",
			ctx: &TransactionContext{
				TransactionType: TransactionTypeCard,
				Amount:          decimal.RequireFromString("100"),
				Currency:        "USD",
				TransactionTimestamp:       time.Date(2030, 12, 24, 12, 0, 0, 0, time.UTC),
				Account: AccountContext{
					ID:   accountID,
					Type: "checking",
				},
				Metadata: nil,
			},
			validate: func(t *testing.T, result map[string]any) {
				metadata, ok := result["metadata"].(map[string]any)
				require.True(t, ok, "metadata should be a map")
				assert.Empty(t, metadata)
			},
		},
		{
			name: "zero-value TransactionContext",
			ctx:  &TransactionContext{},
			validate: func(t *testing.T, result map[string]any) {
				// TransactionType zero-value is empty string
				assert.Equal(t, "", result["transactionType"])
				// Amount zero-value is 0
				assert.Equal(t, float64(0), result["amount"])
				// Currency zero-value is empty string
				assert.Equal(t, "", result["currency"])
				// Timestamp zero-value is zero time
				assert.Equal(t, time.Time{}, result["transactionTimestamp"])

				// Account nested map with zero-value AccountContext
				account, ok := result["account"].(map[string]any)
				require.True(t, ok, "account should be a map")
				assert.Equal(t, uuid.Nil.String(), account["accountId"])
				assert.Equal(t, "", account["type"])
				assert.Equal(t, "", account["status"])
				accountMetadata, ok := account["metadata"].(map[string]any)
				require.True(t, ok, "account metadata should be a map")
				assert.Empty(t, accountMetadata)

				// Optional fields should be nil
				assert.Nil(t, result["subType"])
				assert.Nil(t, result["merchant"])
				assert.Nil(t, result["segment"])
				assert.Nil(t, result["portfolio"])

				// Nil metadata yields empty map
				metadata, ok := result["metadata"].(map[string]any)
				require.True(t, ok, "metadata should be a map")
				assert.Empty(t, metadata)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.ctx.ToMap()
			tt.validate(t, result)
		})
	}
}


