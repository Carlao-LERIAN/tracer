// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package model

import (
	"time"

	"github.com/google/uuid"
)

// RuleStatus represents the lifecycle status of a rule
type RuleStatus string

const (
	RuleStatusDraft    RuleStatus = "DRAFT"
	RuleStatusActive   RuleStatus = "ACTIVE"
	RuleStatusInactive RuleStatus = "INACTIVE"
	RuleStatusDeleted  RuleStatus = "DELETED"
)

// IsValid checks if the RuleStatus is a valid enum value.
func (s RuleStatus) IsValid() bool {
	switch s {
	case RuleStatusDraft, RuleStatusActive, RuleStatusInactive, RuleStatusDeleted:
		return true
	default:
		return false
	}
}

// String returns the string representation of the rule status
func (s RuleStatus) String() string {
	return string(s)
}

// Rule represents a validation rule with CEL expression.
// Note: priority field removed from MVP (TRD v1.2.4) - all rules evaluated, DENY takes precedence.
type Rule struct {
	ID            uuid.UUID  `json:"ruleId" swaggertype:"string" format:"uuid"`
	Name          string     `json:"name"`
	Description   *string    `json:"description,omitempty"`
	Expression    string     `json:"expression"`
	Action        Decision   `json:"action"`
	Scopes        []Scope    `json:"scopes"`
	Status        RuleStatus `json:"status"`
	CreatedAt     time.Time  `json:"createdAt" format:"date-time"`
	UpdatedAt     time.Time  `json:"updatedAt" format:"date-time"`
	ActivatedAt   *time.Time `json:"activatedAt,omitempty" format:"date-time"`
	DeactivatedAt *time.Time `json:"deactivatedAt,omitempty" format:"date-time"`
	DeletedAt     *time.Time `json:"deletedAt,omitempty" format:"date-time"`
}

// ListRulesFilter represents the filter criteria for listing rules.
// Uses cursor-based pagination for consistent results during navigation.
type ListRulesFilter struct {
	Name      *string // Filter by name (case-insensitive partial match / contains)
	Status    *RuleStatus
	Action    *Decision
	Limit     int
	Cursor    string // Base64 encoded cursor for pagination
	SortBy    string
	SortOrder string
}

// ListRulesResult represents the result of listing rules.
// Uses cursor-based pagination per PROJECT_RULES.md.
type ListRulesResult struct {
	Rules      []Rule
	NextCursor string // Base64 encoded cursor for next page (empty if no more results)
	HasMore    bool   // Indicates if there are more results
}
