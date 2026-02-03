// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package model

import "github.com/google/uuid"

// ptrMatches checks if a pattern pointer matches a value pointer.
// If pattern is nil, returns true (nil means "match any").
// If pattern is not nil but value is nil, returns false.
// If both are not nil, returns *pattern == *value.
func ptrMatches[T comparable](pattern, value *T) bool {
	if pattern == nil {
		return true
	}

	if value == nil {
		return false
	}

	return *pattern == *value
}

// Scope represents a hierarchical scope for rules and limits.
// ID fields are uuid.UUID pointers (validated at input layer).
// At least one field must be set for a scope to be valid.
// Validation tags are used by the HTTP layer for input validation.
type Scope struct {
	SegmentID       *uuid.UUID       `json:"segmentId,omitempty" swaggertype:"string" format:"uuid"`
	PortfolioID     *uuid.UUID       `json:"portfolioId,omitempty" swaggertype:"string" format:"uuid"`
	AccountID       *uuid.UUID       `json:"accountId,omitempty" swaggertype:"string" format:"uuid"`
	MerchantID      *uuid.UUID       `json:"merchantId,omitempty" swaggertype:"string" format:"uuid"`
	TransactionType *TransactionType `json:"transactionType,omitempty" validate:"omitempty,transactiontype"`
	SubType         *string          `json:"subType,omitempty" validate:"omitempty,max=50"`
}

// IsEmpty returns true if all scope fields are nil.
func (s *Scope) IsEmpty() bool {
	return s.SegmentID == nil &&
		s.PortfolioID == nil &&
		s.AccountID == nil &&
		s.MerchantID == nil &&
		s.TransactionType == nil &&
		s.SubType == nil
}

// Matches checks if this scope matches another scope.
// All non-nil fields in this scope must match corresponding fields in other.
// A nil field in this scope means "match any value" for that field.
func (s *Scope) Matches(other *Scope) bool {
	return ptrMatches(s.AccountID, other.AccountID) &&
		ptrMatches(s.SegmentID, other.SegmentID) &&
		ptrMatches(s.PortfolioID, other.PortfolioID) &&
		ptrMatches(s.MerchantID, other.MerchantID) &&
		ptrMatches(s.TransactionType, other.TransactionType) &&
		ptrMatches(s.SubType, other.SubType)
}

// ToMap converts Scope to map[string]any for CEL evaluation.
// Only non-nil fields are included in the result.
func (s *Scope) ToMap() map[string]any {
	result := make(map[string]any)

	if s.SegmentID != nil {
		result["segmentId"] = s.SegmentID.String()
	}

	if s.PortfolioID != nil {
		result["portfolioId"] = s.PortfolioID.String()
	}

	if s.AccountID != nil {
		result["accountId"] = s.AccountID.String()
	}

	if s.MerchantID != nil {
		result["merchantId"] = s.MerchantID.String()
	}

	if s.TransactionType != nil {
		result["transactionType"] = s.TransactionType.String()
	}

	if s.SubType != nil {
		result["subType"] = *s.SubType
	}

	return result
}
