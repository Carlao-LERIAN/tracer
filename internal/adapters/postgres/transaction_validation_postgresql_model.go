// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package postgres

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"tracer/pkg/model"
)

// TransactionValidationPostgreSQLModel is the database representation of a TransactionValidation entity.
// It follows the ToEntity/FromEntity pattern from Ring Standards (golang/domain.md).
// This model handles:
// - UUID as string for database storage
// - JSONB fields for complex nested objects (account, segment, portfolio, merchant, metadata, limit_usage_details)
// - UUID arrays as string for PostgreSQL UUID[] type (matched_rule_ids, evaluated_rule_ids)
// - Nullable fields using pointers for optional JSONB columns
type TransactionValidationPostgreSQLModel struct {
	ID                   string    `db:"id"`
	RequestID            string    `db:"request_id"`
	TransactionType      string    `db:"transaction_type"`
	SubType              *string   `db:"sub_type"`
	Amount               int64     `db:"amount"`
	Currency             string    `db:"currency"`
	TransactionTimestamp time.Time `db:"transaction_timestamp"`
	Account              string    `db:"account"`   // JSONB
	Segment              *string   `db:"segment"`   // JSONB (nullable)
	Portfolio            *string   `db:"portfolio"` // JSONB (nullable)
	Merchant             *string   `db:"merchant"`  // JSONB (nullable)
	Metadata             string    `db:"metadata"`  // JSONB
	Decision             string    `db:"decision"`
	Reason               string    `db:"reason"`
	MatchedRuleIds       string    `db:"matched_rule_ids"`    // UUID[] as string
	EvaluatedRuleIds     string    `db:"evaluated_rule_ids"`  // UUID[] as string
	LimitUsageDetails    string    `db:"limit_usage_details"` // JSONB
	ProcessingTimeMs     int64     `db:"processing_time_ms"`
	CreatedAt            time.Time `db:"created_at"`
}

// ToEntity converts the database model to a domain entity.
// This method handles:
// - Parsing UUIDs from strings
// - Unmarshaling JSONB fields to domain types
// - Converting string arrays to UUID slices
// - Converting string enums to typed constants
// Returns an error if JSON unmarshaling fails (e.g., corrupted data in database).
// All JSONB fields use fail-fast approach to catch data corruption early.
func (m *TransactionValidationPostgreSQLModel) ToEntity() (*model.TransactionValidation, error) {
	// Parse UUIDs from strings
	id, _ := uuid.Parse(m.ID)
	requestID, _ := uuid.Parse(m.RequestID)

	// Build entity with basic fields
	validation := &model.TransactionValidation{
		ID:                   id,
		RequestID:            requestID,
		TransactionType:      model.TransactionType(m.TransactionType),
		SubType:              m.SubType,
		Amount:               m.Amount,
		Currency:             m.Currency,
		TransactionTimestamp: m.TransactionTimestamp,
		EvaluationResult: model.EvaluationResult{
			Decision:         model.Decision(m.Decision),
			Reason:           m.Reason,
			MatchedRuleIDs:   []uuid.UUID{},
			EvaluatedRuleIDs: []uuid.UUID{},
		},
		LimitUsageDetails: []model.LimitUsageDetail{},
		ProcessingTimeMs:  m.ProcessingTimeMs,
		CreatedAt:         m.CreatedAt,
	}

	// Unmarshal account JSONB (required field)
	if m.Account != "" {
		if err := json.Unmarshal([]byte(m.Account), &validation.Account); err != nil {
			return nil, fmt.Errorf("failed to unmarshal account: %w", err)
		}
	}

	// Unmarshal optional segment JSONB
	if m.Segment != nil && *m.Segment != "" {
		var segment model.SegmentContext
		if err := json.Unmarshal([]byte(*m.Segment), &segment); err != nil {
			return nil, fmt.Errorf("failed to unmarshal segment: %w", err)
		}

		validation.Segment = &segment
	}

	// Unmarshal optional portfolio JSONB
	if m.Portfolio != nil && *m.Portfolio != "" {
		var portfolio model.PortfolioContext
		if err := json.Unmarshal([]byte(*m.Portfolio), &portfolio); err != nil {
			return nil, fmt.Errorf("failed to unmarshal portfolio: %w", err)
		}

		validation.Portfolio = &portfolio
	}

	// Unmarshal optional merchant JSONB
	if m.Merchant != nil && *m.Merchant != "" {
		var merchant model.MerchantContext
		if err := json.Unmarshal([]byte(*m.Merchant), &merchant); err != nil {
			return nil, fmt.Errorf("failed to unmarshal merchant: %w", err)
		}

		validation.Merchant = &merchant
	}

	// Unmarshal metadata JSONB
	if m.Metadata != "" && m.Metadata != "{}" {
		if err := json.Unmarshal([]byte(m.Metadata), &validation.Metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}
	}

	// Unmarshal limit usage details JSONB
	if m.LimitUsageDetails != "" && m.LimitUsageDetails != "[]" {
		if err := json.Unmarshal([]byte(m.LimitUsageDetails), &validation.LimitUsageDetails); err != nil {
			return nil, fmt.Errorf("failed to unmarshal limit_usage_details: %w", err)
		}
	}

	// Ensure LimitUsageDetails is never nil (return empty slice instead of null in JSON)
	if validation.LimitUsageDetails == nil {
		validation.LimitUsageDetails = []model.LimitUsageDetail{}
	}

	// Parse UUID arrays from PostgreSQL format
	validation.MatchedRuleIDs = parseUUIDArrayString(m.MatchedRuleIds)
	validation.EvaluatedRuleIDs = parseUUIDArrayString(m.EvaluatedRuleIds)

	return validation, nil
}

// FromEntity converts a domain entity to a database model.
// This method handles:
// - Converting UUIDs to strings
// - Marshaling domain types to JSONB
// - Converting UUID slices to string arrays
// - Converting typed constants to strings
func (m *TransactionValidationPostgreSQLModel) FromEntity(entity *model.TransactionValidation) {
	m.ID = entity.ID.String()
	m.RequestID = entity.RequestID.String()
	m.TransactionType = string(entity.TransactionType)
	m.SubType = entity.SubType
	m.Amount = entity.Amount
	m.Currency = entity.Currency
	m.TransactionTimestamp = entity.TransactionTimestamp
	m.Decision = string(entity.Decision)
	m.Reason = entity.Reason
	m.ProcessingTimeMs = entity.ProcessingTimeMs
	m.CreatedAt = entity.CreatedAt

	// Marshal account to JSONB
	accountJSON, err := json.Marshal(entity.Account)
	if err != nil {
		m.Account = "{}"
	} else {
		m.Account = string(accountJSON)
	}

	// Marshal optional segment to JSONB
	if entity.Segment != nil {
		segmentJSON, err := json.Marshal(entity.Segment)
		if err == nil {
			segmentStr := string(segmentJSON)
			m.Segment = &segmentStr
		}
	} else {
		m.Segment = nil
	}

	// Marshal optional portfolio to JSONB
	if entity.Portfolio != nil {
		portfolioJSON, err := json.Marshal(entity.Portfolio)
		if err == nil {
			portfolioStr := string(portfolioJSON)
			m.Portfolio = &portfolioStr
		}
	} else {
		m.Portfolio = nil
	}

	// Marshal optional merchant to JSONB
	if entity.Merchant != nil {
		merchantJSON, err := json.Marshal(entity.Merchant)
		if err == nil {
			merchantStr := string(merchantJSON)
			m.Merchant = &merchantStr
		}
	} else {
		m.Merchant = nil
	}

	// Marshal metadata to JSONB, defaulting to empty object for nil
	metadata := entity.Metadata
	if metadata == nil {
		m.Metadata = "{}"
	} else {
		metadataJSON, err := json.Marshal(metadata)
		if err != nil {
			m.Metadata = "{}"
		} else {
			m.Metadata = string(metadataJSON)
		}
	}

	// Marshal limit usage details to JSONB, defaulting to empty array for nil
	limitUsageDetails := entity.LimitUsageDetails
	if limitUsageDetails == nil {
		limitUsageDetails = []model.LimitUsageDetail{}
	}

	limitUsageDetailsJSON, err := json.Marshal(limitUsageDetails)
	if err != nil {
		m.LimitUsageDetails = "[]"
	} else {
		m.LimitUsageDetails = string(limitUsageDetailsJSON)
	}

	// Convert UUID slices to PostgreSQL array format
	m.MatchedRuleIds = formatUUIDArrayString(entity.MatchedRuleIDs)
	m.EvaluatedRuleIds = formatUUIDArrayString(entity.EvaluatedRuleIDs)
}

// parseUUIDArrayString parses a PostgreSQL UUID array string format to []uuid.UUID.
// Format: "{uuid1,uuid2,...}" or empty string for empty array.
// Invalid UUIDs are skipped silently.
func parseUUIDArrayString(arrayStr string) []uuid.UUID {
	if arrayStr == "" || arrayStr == "{}" {
		return []uuid.UUID{}
	}

	// Remove curly braces
	trimmed := arrayStr
	if len(trimmed) >= 2 && trimmed[0] == '{' && trimmed[len(trimmed)-1] == '}' {
		trimmed = trimmed[1 : len(trimmed)-1]
	}

	if trimmed == "" {
		return []uuid.UUID{}
	}

	// Split by comma and parse UUIDs
	parts := splitUUIDArray(trimmed)
	result := make([]uuid.UUID, 0, len(parts))

	for _, part := range parts {
		id, err := uuid.Parse(part)
		if err == nil {
			result = append(result, id)
		}
	}

	return result
}

// splitUUIDArray splits a comma-separated UUID string.
func splitUUIDArray(s string) []string {
	if s == "" {
		return []string{}
	}

	var result []string

	start := 0

	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			part := s[start:i]
			if part != "" {
				result = append(result, part)
			}

			start = i + 1
		}
	}

	return result
}

// formatUUIDArrayString formats a []uuid.UUID to PostgreSQL array string format.
// Format: "{uuid1,uuid2,...}" for non-empty arrays, "{}" for empty/nil arrays.
func formatUUIDArrayString(uuids []uuid.UUID) string {
	if len(uuids) == 0 {
		return "{}"
	}

	result := "{"

	for i, id := range uuids {
		if i > 0 {
			result += ","
		}

		result += id.String()
	}

	result += "}"

	return result
}
