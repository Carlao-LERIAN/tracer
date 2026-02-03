// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package model

import (
	"time"

	"github.com/google/uuid"
)

// TransactionValidation is the immutable audit record for compliance (SOX/GLBA).
// Stores individual fields for explicit traceability and queryability.
// Embeds EvaluationResult to avoid field duplication.
type TransactionValidation struct {
	ID                   uuid.UUID          `json:"validationId" swaggertype:"string" format:"uuid"`
	RequestID            uuid.UUID          `json:"requestId" swaggertype:"string" format:"uuid"`
	TransactionType      TransactionType    `json:"transactionType"`
	SubType              *string            `json:"subType,omitempty"`
	Amount               int64              `json:"amount"`
	Currency             string             `json:"currency"`
	TransactionTimestamp time.Time          `json:"transactionTimestamp" format:"date-time"`
	Account              AccountContext     `json:"account"`
	Segment              *SegmentContext    `json:"segment,omitempty"`
	Portfolio            *PortfolioContext  `json:"portfolio,omitempty"`
	Merchant             *MerchantContext   `json:"merchant,omitempty"`
	Metadata             map[string]any     `json:"metadata,omitempty"`
	EvaluationResult
	LimitUsageDetails []LimitUsageDetail `json:"limitUsageDetails"`
	ProcessingTimeMs  int64              `json:"processingTimeMs"`
	CreatedAt         time.Time          `json:"createdAt" format:"date-time"`
}

// NewTransactionValidation creates a TransactionValidation with initialized slices.
// Ensures JSON serialization produces [] instead of null for empty arrays.
// The createdAt parameter allows deterministic testing; use time.Now().UTC() in production.
func NewTransactionValidation(id uuid.UUID, decision Decision, createdAt time.Time) *TransactionValidation {
	return &TransactionValidation{
		ID: id,
		EvaluationResult: EvaluationResult{
			Decision:         decision,
			MatchedRuleIDs:   []uuid.UUID{},
			EvaluatedRuleIDs: []uuid.UUID{},
			Reason:           "",
		},
		LimitUsageDetails: []LimitUsageDetail{},
		CreatedAt:         createdAt,
	}
}
