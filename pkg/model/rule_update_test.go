// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package model

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"tracer/internal/testutil"
	"tracer/pkg/constant"
)

// newTestRule creates a valid Rule for testing purposes.
// Returns a Rule with valid fields and a sample scope.
// Fails the test immediately if NewRule returns an error.
func newTestRule(t *testing.T) *Rule {
	t.Helper()

	rule, err := NewRule(
		"Test Rule",
		"amount > 1000",
		DecisionDeny,
		[]Scope{{AccountID: testutil.UUIDPtr(uuid.New())}},
		nil,
	)
	require.NoError(t, err, "newTestRule: NewRule failed")

	return rule
}

func TestRule_Update_ScopeValidation(t *testing.T) {
	t.Parallel()

	t.Run("Error - rejects scope with all nil fields (empty scope)", func(t *testing.T) {
		rule := newTestRule(t)
		emptyScope := Scope{} // All fields nil

		scopesWithEmpty := &[]Scope{emptyScope}

		err := rule.Update(nil, nil, nil, scopesWithEmpty)

		require.Error(t, err, "Update should reject empty scope")
		assert.ErrorIs(t, err, constant.ErrRuleInvalidScope)
	})

	t.Run("Error - rejects multiple scopes where one is empty", func(t *testing.T) {
		rule := newTestRule(t)
		validScope := Scope{AccountID: testutil.UUIDPtr(uuid.New())}
		emptyScope := Scope{} // All fields nil

		scopesWithOneEmpty := &[]Scope{validScope, emptyScope}

		err := rule.Update(nil, nil, nil, scopesWithOneEmpty)

		require.Error(t, err, "Update should reject when any scope is empty")
		assert.ErrorIs(t, err, constant.ErrRuleInvalidScope)
	})

	t.Run("Error - empty scope in first position", func(t *testing.T) {
		rule := newTestRule(t)
		emptyScope := Scope{}
		validScope := Scope{AccountID: testutil.UUIDPtr(uuid.New())}

		scopesWithFirstEmpty := &[]Scope{emptyScope, validScope}

		err := rule.Update(nil, nil, nil, scopesWithFirstEmpty)

		require.Error(t, err, "Update should reject when first scope is empty")
		assert.ErrorIs(t, err, constant.ErrRuleInvalidScope)
	})

	t.Run("Success - accepts valid scopes", func(t *testing.T) {
		rule := newTestRule(t)
		originalScopes := make([]Scope, len(rule.Scopes))
		copy(originalScopes, rule.Scopes)

		validScopes := &[]Scope{
			{AccountID: testutil.UUIDPtr(uuid.New())},
			{PortfolioID: testutil.UUIDPtr(uuid.New())},
		}

		err := rule.Update(nil, nil, nil, validScopes)

		require.NoError(t, err)
		assert.Len(t, rule.Scopes, 2)
	})

	t.Run("Success - accepts empty slice (removes all scopes)", func(t *testing.T) {
		rule := newTestRule(t)

		emptySlice := &[]Scope{}

		err := rule.Update(nil, nil, nil, emptySlice)

		require.NoError(t, err)
		assert.Empty(t, rule.Scopes)
	})

	t.Run("Success - nil scopes parameter keeps existing scopes", func(t *testing.T) {
		rule := newTestRule(t)
		originalScopes := make([]Scope, len(rule.Scopes))
		copy(originalScopes, rule.Scopes)

		err := rule.Update(nil, nil, nil, nil)

		require.NoError(t, err)
		assert.Equal(t, originalScopes, rule.Scopes, "Scopes should remain unchanged when nil is passed")
	})

	t.Run("Atomicity - does not mutate scopes on validation failure", func(t *testing.T) {
		rule := newTestRule(t)
		originalScopes := make([]Scope, len(rule.Scopes))
		copy(originalScopes, rule.Scopes)

		emptyScope := Scope{}
		invalidScopes := &[]Scope{emptyScope}

		err := rule.Update(nil, nil, nil, invalidScopes)

		require.Error(t, err)
		assert.Equal(t, originalScopes, rule.Scopes, "Scopes should not be mutated on validation failure")
	})
}
