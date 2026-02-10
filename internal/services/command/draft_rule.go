// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package command

import (
	"context"
	"errors"
	"fmt"

	libCommons "github.com/LerianStudio/lib-commons/v2/commons"
	libOpentelemetry "github.com/LerianStudio/lib-commons/v2/commons/opentelemetry"
	"github.com/google/uuid"

	"tracer/pkg/clock"
	"tracer/pkg/constant"
	"tracer/pkg/contextutil"
	"tracer/pkg/logging"
	"tracer/pkg/model"
)

// DraftRuleService handles rule draft transition (INACTIVE → DRAFT).
type DraftRuleService struct {
	repository  RuleRepository
	clock       clock.Clock
	auditWriter AuditWriter
}

// NewDraftRuleService creates a new DraftRuleService.
func NewDraftRuleService(repository RuleRepository, clk clock.Clock, auditWriter AuditWriter) *DraftRuleService {
	return &DraftRuleService{
		repository:  repository,
		clock:       clk,
		auditWriter: auditWriter,
	}
}

// Execute transitions a rule to DRAFT status.
// Idempotent: if already DRAFT, returns the rule without error.
// Returns the updated rule for atomic draft-and-return pattern.
func (s *DraftRuleService) Execute(ctx context.Context, ruleID uuid.UUID) (*model.Rule, error) {
	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "service.rule.draft")
	defer span.End()

	logger = logging.WithTrace(ctx, logger)

	_ = libOpentelemetry.SetSpanAttributesFromStruct(&span, "draft_input", map[string]any{
		"rule_id":   ruleID.String(),
		"operation": "draft",
	})

	logger.WithFields(
		"operation", "service.rule.draft",
		"rule.id", ruleID.String(),
	).Info("Transitioning rule to draft")

	rule, err := s.repository.GetByID(ctx, ruleID)
	if err != nil {
		if errors.Is(err, constant.ErrRuleNotFound) {
			libOpentelemetry.HandleSpanBusinessErrorEvent(&span, "Rule not found", err)
			logger.WithFields(
				"operation", "service.rule.draft",
				"rule.id", ruleID.String(),
			).Warn("Rule not found")

			return nil, libCommons.ValidateBusinessError(constant.ErrRuleNotFound, "Rule")
		}

		libOpentelemetry.HandleSpanError(&span, "Failed to get rule from repository", err)
		logger.WithFields(
			"operation", "service.rule.draft",
			"rule.id", ruleID.String(),
			"error.message", err.Error(),
		).Error("Failed to get rule")

		return nil, fmt.Errorf("failed to get rule: %w", err)
	}

	// Idempotency: if already draft, return the rule (no-op)
	// Check before audit capture to avoid unnecessary state snapshots
	if rule.Status == model.RuleStatusDraft {
		logger.WithFields(
			"operation", "service.rule.draft",
			"rule.id", ruleID.String(),
		).Info("Rule already in draft (idempotent no-op)")

		return rule, nil
	}

	// Capture "before" state for audit
	beforeState := RuleToMap(rule)

	// Use domain model method for status transition (validates and maintains invariants)
	if err := rule.SetStatus(model.RuleStatusDraft, s.clock.Now()); err != nil {
		// Check for invalid transition (business error)
		var transitionErr *model.InvalidTransitionError
		if errors.As(err, &transitionErr) {
			libOpentelemetry.HandleSpanBusinessErrorEvent(&span, "Invalid state transition", transitionErr)
			logger.WithFields(
				"operation", "service.rule.draft",
				"rule.id", ruleID.String(),
				"rule.status_from", string(transitionErr.From),
				"rule.status_to", string(transitionErr.To),
			).Warn("Invalid transition")

			return nil, transitionErr
		}

		// Technical error (invalid status value or other)
		libOpentelemetry.HandleSpanError(&span, "Failed to set rule status", err)
		logger.WithFields(
			"operation", "service.rule.draft",
			"rule.id", ruleID.String(),
			"error.message", err.Error(),
		).Error("Failed to set rule status")

		return nil, fmt.Errorf("failed to set rule status: %w", err)
	}

	// Persist updated rule
	updatedRule, err := s.repository.Update(ctx, rule)
	if err != nil {
		libOpentelemetry.HandleSpanError(&span, "Failed to update rule", err)
		logger.WithFields(
			"operation", "service.rule.draft",
			"rule.id", ruleID.String(),
			"error.message", err.Error(),
		).Error("Failed to update rule")

		return nil, fmt.Errorf("failed to update rule: %w", err)
	}

	logger.WithFields(
		"operation", "service.rule.draft",
		"rule.id", updatedRule.ID.String(),
	).Info("Rule transitioned to draft successfully")

	// Record audit event (best-effort)
	if s.auditWriter != nil {
		clientIP := contextutil.GetClientIP(ctx)

		afterState := RuleToMap(updatedRule)
		if err := s.auditWriter.RecordRuleEvent(
			ctx,
			model.AuditEventRuleDrafted,
			model.AuditActionDraft,
			updatedRule.ID,
			beforeState,
			afterState,
			"Rule transitioned to draft via API",
			clientIP,
		); err != nil {
			logger.WithFields(
				"operation", "service.rule.draft.audit",
				"rule.id", updatedRule.ID.String(),
				"error", err.Error(),
			).Warn("Failed to record audit event")
		}
	}

	return updatedRule, nil
}
