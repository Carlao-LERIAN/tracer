// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

//go:build unit

package model

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"tracer/pkg/constant"
)

func TestNewAuditEvent_Validation(t *testing.T) {
	t.Parallel()

	validActor := Actor{
		ActorType: ActorTypeSystem,
		ID:        "test_actor",
		Name:      "Test Actor",
	}

	t.Run("Error - invalid event type", func(t *testing.T) {
		event, err := NewAuditEvent(
			AuditEventType("INVALID"),
			AuditActionCreate,
			AuditResultSuccess,
			uuid.NewString(),
			ResourceTypeRule,
			validActor,
		)

		require.Error(t, err)
		assert.Nil(t, event)
		assert.ErrorIs(t, err, constant.ErrAuditEventInvalidType)
	})

	t.Run("Error - invalid action", func(t *testing.T) {
		event, err := NewAuditEvent(
			AuditEventRuleCreated,
			AuditAction("INVALID"),
			AuditResultSuccess,
			uuid.NewString(),
			ResourceTypeRule,
			validActor,
		)

		require.Error(t, err)
		assert.Nil(t, event)
		assert.ErrorIs(t, err, constant.ErrAuditEventInvalidAction)
	})

	t.Run("Error - invalid result", func(t *testing.T) {
		event, err := NewAuditEvent(
			AuditEventRuleCreated,
			AuditActionCreate,
			AuditResult("INVALID"),
			uuid.NewString(),
			ResourceTypeRule,
			validActor,
		)

		require.Error(t, err)
		assert.Nil(t, event)
		assert.ErrorIs(t, err, constant.ErrAuditEventInvalidResult)
	})

	t.Run("Error - empty resource ID", func(t *testing.T) {
		event, err := NewAuditEvent(
			AuditEventRuleCreated,
			AuditActionCreate,
			AuditResultSuccess,
			"",
			ResourceTypeRule,
			validActor,
		)

		require.Error(t, err)
		assert.Nil(t, event)
		assert.ErrorIs(t, err, constant.ErrAuditEventResourceIDRequired)
	})

	t.Run("Error - invalid resource type", func(t *testing.T) {
		event, err := NewAuditEvent(
			AuditEventRuleCreated,
			AuditActionCreate,
			AuditResultSuccess,
			uuid.NewString(),
			ResourceType("INVALID"),
			validActor,
		)

		require.Error(t, err)
		assert.Nil(t, event)
		assert.ErrorIs(t, err, constant.ErrAuditEventInvalidResourceType)
	})

	t.Run("Error - empty actor ID", func(t *testing.T) {
		invalidActor := Actor{
			ActorType: ActorTypeUser,
			ID:        "",
			Name:      "Test Actor",
		}

		event, err := NewAuditEvent(
			AuditEventRuleCreated,
			AuditActionCreate,
			AuditResultSuccess,
			uuid.NewString(),
			ResourceTypeRule,
			invalidActor,
		)

		require.Error(t, err)
		assert.Nil(t, event)
		assert.ErrorIs(t, err, constant.ErrAuditEventActorIDRequired)
	})

	t.Run("Error - invalid actor type", func(t *testing.T) {
		invalidActor := Actor{
			ActorType: ActorType("INVALID"),
			ID:        "test_actor",
			Name:      "Test Actor",
		}

		event, err := NewAuditEvent(
			AuditEventRuleCreated,
			AuditActionCreate,
			AuditResultSuccess,
			uuid.NewString(),
			ResourceTypeRule,
			invalidActor,
		)

		require.Error(t, err)
		assert.Nil(t, event)
		assert.ErrorIs(t, err, constant.ErrAuditEventActorTypeInvalid)
	})

	t.Run("Success - all valid enums", func(t *testing.T) {
		testCases := []struct {
			name         string
			eventType    AuditEventType
			action       AuditAction
			result       AuditResult
			resourceType ResourceType
		}{
			{
				name:         "Rule created",
				eventType:    AuditEventRuleCreated,
				action:       AuditActionCreate,
				result:       AuditResultSuccess,
				resourceType: ResourceTypeRule,
			},
			{
				name:         "Limit activated",
				eventType:    AuditEventLimitActivated,
				action:       AuditActionActivate,
				result:       AuditResultSuccess,
				resourceType: ResourceTypeLimit,
			},
			{
				name:         "Transaction validated",
				eventType:    AuditEventTransactionValidated,
				action:       AuditActionValidate,
				result:       AuditResultAllow,
				resourceType: ResourceTypeTransaction,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				event, err := NewAuditEvent(
					tc.eventType,
					tc.action,
					tc.result,
					uuid.NewString(),
					tc.resourceType,
					validActor,
				)

				require.NoError(t, err)
				require.NotNil(t, event)
				assert.Equal(t, tc.eventType, event.EventType)
				assert.Equal(t, tc.action, event.Action)
				assert.Equal(t, tc.result, event.Result)
				assert.Equal(t, tc.resourceType, event.ResourceType)
			})
		}
	})

	t.Run("Success - context and metadata initialized as empty maps", func(t *testing.T) {
		event, err := NewAuditEvent(
			AuditEventRuleCreated,
			AuditActionCreate,
			AuditResultSuccess,
			uuid.NewString(),
			ResourceTypeRule,
			validActor,
		)

		require.NoError(t, err)
		require.NotNil(t, event)
		assert.NotNil(t, event.Context, "Context should be initialized")
		assert.NotNil(t, event.Metadata, "Metadata should be initialized")
		assert.Empty(t, event.Context, "Context should be empty map")
		assert.Empty(t, event.Metadata, "Metadata should be empty map")
	})

	t.Run("Success - normalizes resourceID with whitespace", func(t *testing.T) {
		testCases := []struct {
			name       string
			resourceID string
			expected   string
		}{
			{
				name:       "leading spaces",
				resourceID: "  resource-123",
				expected:   "resource-123",
			},
			{
				name:       "trailing spaces",
				resourceID: "resource-123  ",
				expected:   "resource-123",
			},
			{
				name:       "leading and trailing spaces",
				resourceID: "  resource-123  ",
				expected:   "resource-123",
			},
			{
				name:       "tabs and newlines",
				resourceID: "\t resource-123 \n",
				expected:   "resource-123",
			},
			{
				name:       "no whitespace",
				resourceID: "resource-123",
				expected:   "resource-123",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				event, err := NewAuditEvent(
					AuditEventRuleCreated,
					AuditActionCreate,
					AuditResultSuccess,
					tc.resourceID,
					ResourceTypeRule,
					validActor,
				)

				require.NoError(t, err)
				require.NotNil(t, event)
				assert.Equal(t, tc.expected, event.ResourceID, "ResourceID should be trimmed")
			})
		}
	})

	t.Run("Success - normalizes actor.ID with whitespace", func(t *testing.T) {
		testCases := []struct {
			name     string
			actorID  string
			expected string
		}{
			{
				name:     "leading spaces",
				actorID:  "  actor-123",
				expected: "actor-123",
			},
			{
				name:     "trailing spaces",
				actorID:  "actor-123  ",
				expected: "actor-123",
			},
			{
				name:     "leading and trailing spaces",
				actorID:  "  actor-123  ",
				expected: "actor-123",
			},
			{
				name:     "tabs and newlines",
				actorID:  "\t actor-123 \n",
				expected: "actor-123",
			},
			{
				name:     "no whitespace",
				actorID:  "actor-123",
				expected: "actor-123",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				actorWithWhitespace := Actor{
					ActorType: ActorTypeSystem,
					ID:        tc.actorID,
					Name:      "Test Actor",
				}

				event, err := NewAuditEvent(
					AuditEventRuleCreated,
					AuditActionCreate,
					AuditResultSuccess,
					uuid.NewString(),
					ResourceTypeRule,
					actorWithWhitespace,
				)

				require.NoError(t, err)
				require.NotNil(t, event)
				assert.Equal(t, tc.expected, event.Actor.ID, "Actor.ID should be trimmed")
			})
		}
	})

	t.Run("Error - whitespace-only resourceID", func(t *testing.T) {
		event, err := NewAuditEvent(
			AuditEventRuleCreated,
			AuditActionCreate,
			AuditResultSuccess,
			"   ",
			ResourceTypeRule,
			validActor,
		)

		require.Error(t, err)
		assert.Nil(t, event)
		assert.ErrorIs(t, err, constant.ErrAuditEventResourceIDRequired)
	})

	t.Run("Error - whitespace-only actor.ID", func(t *testing.T) {
		invalidActor := Actor{
			ActorType: ActorTypeUser,
			ID:        "   ",
			Name:      "Test Actor",
		}

		event, err := NewAuditEvent(
			AuditEventRuleCreated,
			AuditActionCreate,
			AuditResultSuccess,
			uuid.NewString(),
			ResourceTypeRule,
			invalidActor,
		)

		require.Error(t, err)
		assert.Nil(t, event)
		assert.ErrorIs(t, err, constant.ErrAuditEventActorIDRequired)
	})
}

func TestAuditEventType_IsValid(t *testing.T) {
	t.Parallel()

	t.Run("Valid event types", func(t *testing.T) {
		t.Parallel()

		validTypes := []AuditEventType{
			AuditEventRuleCreated,
			AuditEventRuleUpdated,
			AuditEventRuleActivated,
			AuditEventRuleDeactivated,
			AuditEventRuleDeleted,
			AuditEventLimitCreated,
			AuditEventLimitUpdated,
			AuditEventLimitActivated,
			AuditEventLimitDeactivated,
			AuditEventLimitDeleted,
			AuditEventTransactionValidated,
		}

		for _, eventType := range validTypes {
			assert.True(t, eventType.IsValid(), "Event type %s should be valid", eventType)
		}
	})

	t.Run("Invalid event types", func(t *testing.T) {
		t.Parallel()

		invalidTypes := []AuditEventType{
			AuditEventType(""),
			AuditEventType("INVALID"),
			AuditEventType("rule_created"),
			AuditEventType("RuleDeleted"),
		}

		for _, eventType := range invalidTypes {
			assert.False(t, eventType.IsValid(), "Event type %s should be invalid", eventType)
		}
	})
}

func TestAuditAction_IsValid(t *testing.T) {
	t.Parallel()

	t.Run("Valid actions", func(t *testing.T) {
		t.Parallel()

		validActions := []AuditAction{
			AuditActionCreate,
			AuditActionUpdate,
			AuditActionDelete,
			AuditActionActivate,
			AuditActionDeactivate,
			AuditActionValidate,
		}

		for _, action := range validActions {
			assert.True(t, action.IsValid(), "Action %s should be valid", action)
		}
	})

	t.Run("Invalid actions", func(t *testing.T) {
		t.Parallel()

		invalidActions := []AuditAction{
			AuditAction(""),
			AuditAction("INVALID"),
			AuditAction("create"),
			AuditAction("READ"),
		}

		for _, action := range invalidActions {
			assert.False(t, action.IsValid(), "Action %s should be invalid", action)
		}
	})
}

func TestAuditResult_IsValid(t *testing.T) {
	t.Parallel()

	t.Run("Valid results", func(t *testing.T) {
		t.Parallel()

		validResults := []AuditResult{
			AuditResultSuccess,
			AuditResultFailed,
			AuditResultAllow,
			AuditResultDeny,
			AuditResultReview,
		}

		for _, result := range validResults {
			assert.True(t, result.IsValid(), "Result %s should be valid", result)
		}
	})

	t.Run("Invalid results", func(t *testing.T) {
		t.Parallel()

		invalidResults := []AuditResult{
			AuditResult(""),
			AuditResult("INVALID"),
			AuditResult("success"),
			AuditResult("PENDING"),
		}

		for _, result := range invalidResults {
			assert.False(t, result.IsValid(), "Result %s should be invalid", result)
		}
	})
}

func TestResourceType_IsValid(t *testing.T) {
	t.Parallel()

	t.Run("Valid resource types", func(t *testing.T) {
		t.Parallel()

		validTypes := []ResourceType{
			ResourceTypeRule,
			ResourceTypeLimit,
			ResourceTypeTransaction,
		}

		for _, resourceType := range validTypes {
			assert.True(t, resourceType.IsValid(), "Resource type %s should be valid", resourceType)
		}
	})

	t.Run("Invalid resource types", func(t *testing.T) {
		t.Parallel()

		invalidTypes := []ResourceType{
			ResourceType(""),
			ResourceType("INVALID"),
			ResourceType("RULE"),
			ResourceType("USER"),
		}

		for _, resourceType := range invalidTypes {
			assert.False(t, resourceType.IsValid(), "Resource type %s should be invalid", resourceType)
		}
	})
}
