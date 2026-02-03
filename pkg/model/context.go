// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package model

import "github.com/google/uuid"

// AccountContext contains account information for validation.
// Type should be one of: "checking", "savings", "credit"
// Status should be one of: "active", "suspended", "closed"
type AccountContext struct {
	ID       uuid.UUID      `json:"accountId" swaggertype:"string" format:"uuid"`
	Type     string         `json:"type"`
	Status   string         `json:"status"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// ToMap converts AccountContext to map[string]any for CEL evaluation.
// Returns nil if the receiver is nil.
func (a *AccountContext) ToMap() map[string]any {
	if a == nil {
		return nil
	}

	metadata := a.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}

	return map[string]any{
		"accountId": a.ID.String(),
		"type":      a.Type,
		"status":    a.Status,
		"metadata":  metadata,
	}
}

// MerchantContext contains merchant information for validation.
type MerchantContext struct {
	ID       uuid.UUID      `json:"merchantId" swaggertype:"string" format:"uuid"`
	Name     string         `json:"name"`
	Category string         `json:"category"` // e.g., ISO 18245 MCC code
	Country  string         `json:"country"`  // ISO 3166-1 alpha-2 code
	Metadata map[string]any `json:"metadata,omitempty"`
}

// ToMap converts MerchantContext to map[string]any for CEL evaluation.
// Returns nil if the receiver is nil.
func (m *MerchantContext) ToMap() map[string]any {
	if m == nil {
		return nil
	}

	metadata := m.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}

	return map[string]any{
		"merchantId": m.ID.String(),
		"name":       m.Name,
		"category":   m.Category,
		"country":    m.Country,
		"metadata":   metadata,
	}
}

// SegmentContext contains segment information for transaction categorization.
type SegmentContext struct {
	ID       uuid.UUID      `json:"segmentId" swaggertype:"string" format:"uuid"`
	Name     string         `json:"name,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// ToMap converts SegmentContext to map[string]any for CEL evaluation.
// Returns nil if the receiver is nil.
func (s *SegmentContext) ToMap() map[string]any {
	if s == nil {
		return nil
	}

	metadata := s.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}

	return map[string]any{
		"segmentId": s.ID.String(),
		"name":      s.Name,
		"metadata":  metadata,
	}
}

// PortfolioContext contains portfolio information for transaction categorization.
type PortfolioContext struct {
	ID       uuid.UUID      `json:"portfolioId" swaggertype:"string" format:"uuid"`
	Name     string         `json:"name,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// ToMap converts PortfolioContext to map[string]any for CEL evaluation.
// Returns nil if the receiver is nil.
func (p *PortfolioContext) ToMap() map[string]any {
	if p == nil {
		return nil
	}

	metadata := p.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}

	return map[string]any{
		"portfolioId": p.ID.String(),
		"name":        p.Name,
		"metadata":    metadata,
	}
}
