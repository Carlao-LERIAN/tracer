// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package model

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRuleStatus_IsValid(t *testing.T) {
	tests := []struct {
		name     string
		status   RuleStatus
		expected bool
	}{
		{
			name:     "Success - DRAFT is valid",
			status:   RuleStatusDraft,
			expected: true,
		},
		{
			name:     "Success - ACTIVE is valid",
			status:   RuleStatusActive,
			expected: true,
		},
		{
			name:     "Success - INACTIVE is valid",
			status:   RuleStatusInactive,
			expected: true,
		},
		{
			name:     "Success - DELETED is valid",
			status:   RuleStatusDeleted,
			expected: true,
		},
		{
			name:     "Error - empty string is invalid",
			status:   RuleStatus(""),
			expected: false,
		},
		{
			name:     "Error - lowercase draft is invalid",
			status:   RuleStatus("draft"),
			expected: false,
		},
		{
			name:     "Error - random string is invalid",
			status:   RuleStatus("INVALID"),
			expected: false,
		},
		{
			name:     "Error - partial match is invalid",
			status:   RuleStatus("DRAF"),
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.status.IsValid()
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestRule_JSONSerialization(t *testing.T) {
	t.Run("Success - rule serializes to JSON correctly", func(t *testing.T) {
		ruleID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440001")
		description := "Test rule description"
		createdAt := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
		updatedAt := time.Date(2024, 1, 15, 11, 0, 0, 0, time.UTC)

		rule := Rule{
			ID:          ruleID,
			Name:        "test rule",
			Description: &description,
			Expression:  "amount > 1000",
			Action:      DecisionDeny,
			Scopes:      []Scope{},
			Status:      RuleStatusDraft,
			CreatedAt:   createdAt,
			UpdatedAt:   updatedAt,
			DeletedAt:   nil,
		}

		data, err := json.Marshal(rule)
		require.NoError(t, err)

		var result map[string]any
		err = json.Unmarshal(data, &result)
		require.NoError(t, err)

		assert.Equal(t, ruleID.String(), result["ruleId"])
		assert.Equal(t, "test rule", result["name"])
		assert.Equal(t, description, result["description"])
		assert.Equal(t, "amount > 1000", result["expression"])
		assert.Equal(t, "DENY", result["action"])
		assert.Equal(t, "DRAFT", result["status"])
		assert.NotNil(t, result["scopes"])
		assert.NotNil(t, result["createdAt"])
		assert.NotNil(t, result["updatedAt"])
	})

	t.Run("Success - rule without description serializes correctly", func(t *testing.T) {
		rule := Rule{
			ID:          uuid.New(),
			Name:        "test rule",
			Description: nil,
			Expression:  "amount > 1000",
			Action:      DecisionAllow,
			Scopes:      []Scope{},
			Status:      RuleStatusActive,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}

		data, err := json.Marshal(rule)
		require.NoError(t, err)

		var result map[string]any
		err = json.Unmarshal(data, &result)
		require.NoError(t, err)

		_, hasDescription := result["description"]
		assert.False(t, hasDescription, "description should be omitted when nil")
	})

	t.Run("Success - rule deserializes from JSON correctly", func(t *testing.T) {
		jsonData := `{
			"ruleId": "550e8400-e29b-41d4-a716-446655440001",
			"name": "test rule",
			"description": "Test description",
			"expression": "amount > 1000",
			"action": "DENY",
			"scopes": [],
			"status": "DRAFT",
			"createdAt": "2024-01-15T10:00:00Z",
			"updatedAt": "2024-01-15T11:00:00Z"
		}`

		var rule Rule
		err := json.Unmarshal([]byte(jsonData), &rule)
		require.NoError(t, err)

		assert.Equal(t, uuid.MustParse("550e8400-e29b-41d4-a716-446655440001"), rule.ID)
		assert.Equal(t, "test rule", rule.Name)
		require.NotNil(t, rule.Description)
		assert.Equal(t, "Test description", *rule.Description)
		assert.Equal(t, "amount > 1000", rule.Expression)
		assert.Equal(t, DecisionDeny, rule.Action)
		assert.Equal(t, RuleStatusDraft, rule.Status)
		assert.Empty(t, rule.Scopes)
	})
}

func TestListRulesFilter_Defaults(t *testing.T) {
	t.Run("Success - filter with zero values", func(t *testing.T) {
		filter := ListRulesFilter{}

		assert.Nil(t, filter.Status)
		assert.Nil(t, filter.Action)
		assert.Zero(t, filter.Limit)
		assert.Empty(t, filter.Cursor)
		assert.Empty(t, filter.SortBy)
		assert.Empty(t, filter.SortOrder)
	})

	t.Run("Success - filter with all values set", func(t *testing.T) {
		status := RuleStatusActive
		action := DecisionDeny

		filter := ListRulesFilter{
			Status:    &status,
			Action:    &action,
			Limit:     10,
			Cursor:    "abc123",
			SortBy:    "createdAt",
			SortOrder: "DESC",
		}

		require.NotNil(t, filter.Status)
		assert.Equal(t, RuleStatusActive, *filter.Status)
		require.NotNil(t, filter.Action)
		assert.Equal(t, DecisionDeny, *filter.Action)
		assert.Equal(t, 10, filter.Limit)
		assert.Equal(t, "abc123", filter.Cursor)
		assert.Equal(t, "createdAt", filter.SortBy)
		assert.Equal(t, "DESC", filter.SortOrder)
	})
}

func TestListRulesResult_Fields(t *testing.T) {
	t.Run("Success - result with rules and pagination", func(t *testing.T) {
		rules := []Rule{
			{ID: uuid.New(), Name: "rule1", Status: RuleStatusDraft},
			{ID: uuid.New(), Name: "rule2", Status: RuleStatusActive},
		}

		result := ListRulesResult{
			Rules:      rules,
			NextCursor: "next_cursor_value",
			HasMore:    true,
		}

		assert.Len(t, result.Rules, 2)
		assert.Equal(t, "next_cursor_value", result.NextCursor)
		assert.True(t, result.HasMore)
	})

	t.Run("Success - result with empty rules", func(t *testing.T) {
		result := ListRulesResult{
			Rules:      []Rule{},
			NextCursor: "",
			HasMore:    false,
		}

		assert.Empty(t, result.Rules)
		assert.Empty(t, result.NextCursor)
		assert.False(t, result.HasMore)
	})
}
