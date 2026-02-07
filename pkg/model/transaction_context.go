// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package model

import (
	"time"

	"github.com/shopspring/decimal"
)

// TransactionContext contains all data needed for rule evaluation.
// Aligned with API Design v1.3.1 for CEL expression evaluation.
type TransactionContext struct {
	TransactionType      TransactionType   `json:"transactionType"`
	SubType              *string           `json:"subType,omitempty"`
	Amount               decimal.Decimal   `json:"amount" swaggertype:"string" example:"100.00"`
	Currency             string            `json:"currency"`
	TransactionTimestamp time.Time         `json:"transactionTimestamp"`
	Account              AccountContext    `json:"account"`
	Segment              *SegmentContext   `json:"segment,omitempty"`
	Portfolio            *PortfolioContext `json:"portfolio,omitempty"`
	Merchant             *MerchantContext  `json:"merchant,omitempty"`
	Metadata             map[string]any    `json:"metadata,omitempty"`
}

// ToMap converts TransactionContext to map[string]any for CEL evaluation.
// This method provides the transaction data in a format suitable for CEL expressions.
//
// Note on nil handling (intentional for CEL compatibility):
// - segment/portfolio: nil → nil (allows `segment == nil` checks in CEL)
// - metadata: nil → empty map (allows `metadata.field` access without nil errors)
func (tc *TransactionContext) ToMap() map[string]any {
	// Convert decimal to float64 for CEL evaluation compatibility
	amountFloat := tc.Amount.InexactFloat64()
	result := map[string]any{
		"transactionType":      tc.TransactionType.String(),
		"amount":               amountFloat,
		"currency":             tc.Currency,
		"transactionTimestamp": tc.TransactionTimestamp,
		"account":              tc.Account.ToMap(),
	}

	if tc.SubType != nil {
		result["subType"] = *tc.SubType
	} else {
		result["subType"] = nil
	}

	if tc.Segment != nil {
		result["segment"] = tc.Segment.ToMap()
	} else {
		result["segment"] = nil
	}

	if tc.Portfolio != nil {
		result["portfolio"] = tc.Portfolio.ToMap()
	} else {
		result["portfolio"] = nil
	}

	if tc.Merchant != nil {
		result["merchant"] = tc.Merchant.ToMap()
	} else {
		result["merchant"] = nil
	}

	if tc.Metadata != nil {
		result["metadata"] = tc.Metadata
	} else {
		result["metadata"] = map[string]any{}
	}

	return result
}
