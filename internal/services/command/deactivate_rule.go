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

// DeactivateRuleService handles rule deactivation (ACTIVE/DRAFT → INACTIVE).
type DeactivateRuleService struct {
	repository  RuleRepository
	clock       clock.Clock
	auditWriter AuditWriter
}

// NewDeactivateRuleService creates a new DeactivateRuleService.
func NewDeactivateRuleService(repository RuleRepository, clk clock.Clock, auditWriter AuditWriter) *DeactivateRuleService {
	return &DeactivateRuleService{
		repository:  repository,
		clock:       clk,
		auditWriter: auditWriter,
	}
}

// Execute deactivates a rule by updating status to INACTIVE.
// Idempotent: if already INACTIVE, returns the rule without error.
// Returns the updated rule for atomic deactivate-and-return pattern.
func (s *DeactivateRuleService) Execute(ctx context.Context, ruleID uuid.UUID) (*model.Rule, error) {
	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "service.rule.deactivate")
	defer span.End()

	logger = logging.WithTrace(ctx, logger)

	_ = libOpentelemetry.SetSpanAttributesFromStruct(&span, "deactivate_input", map[string]any{
		"rule_id":   ruleID.String(),
		"operation": "deactivate",
	})

	logger.WithFields(
		"operation", "service.rule.deactivate",
		"rule.id", ruleID.String(),
	).Info("Deactivating rule")

	rule, err := s.repository.GetByID(ctx, ruleID)
	if err != nil {
		if errors.Is(err, constant.ErrRuleNotFound) {
			libOpentelemetry.HandleSpanBusinessErrorEvent(&span, "Rule not found", err)
			logger.WithFields(
				"operation", "service.rule.deactivate",
				"rule.id", ruleID.String(),
			).Warn("Rule not found")

			return nil, libCommons.ValidateBusinessError(constant.ErrRuleNotFound, "Rule")
		}

		libOpentelemetry.HandleSpanError(&span, "Failed to get rule from repository", err)
		logger.WithFields(
			"operation", "service.rule.deactivate",
			"rule.id", ruleID.String(),
			"error.message", err.Error(),
		).Error("Failed to get rule")

		return nil, fmt.Errorf("failed to get rule: %w", err)
	}

	// Capture "before" state for audit
	beforeState := RuleToMap(rule)

	// Idempotency: if already inactive, return the rule (no-op)
	if rule.Status == model.RuleStatusInactive {
		logger.WithFields(
			"operation", "service.rule.deactivate",
			"rule.id", ruleID.String(),
		).Info("Rule already inactive (idempotent no-op)")

		return rule, nil
	}

	// Check if transition is valid
	if !rule.Status.CanTransitionTo(model.RuleStatusInactive) {
		err := model.NewInvalidTransitionError(rule.Status, model.RuleStatusInactive)
		libOpentelemetry.HandleSpanBusinessErrorEvent(&span, "Invalid state transition", err)
		logger.WithFields(
			"operation", "service.rule.deactivate",
			"rule.id", ruleID.String(),
			"rule.status_from", string(rule.Status),
			"rule.status_to", "INACTIVE",
		).Warn("Invalid transition")

		return nil, err
	}

	now := s.clock.Now()
	if err := s.repository.UpdateStatus(ctx, ruleID, model.RuleStatusInactive, now, nil, &now); err != nil {
		libOpentelemetry.HandleSpanError(&span, "Failed to update rule status", err)
		logger.WithFields(
			"operation", "service.rule.deactivate",
			"rule.id", ruleID.String(),
			"error.message", err.Error(),
		).Error("Failed to update rule status")

		return nil, fmt.Errorf("failed to update rule status: %w", err)
	}

	// Update the rule object with new status and timestamps
	rule.Status = model.RuleStatusInactive
	rule.UpdatedAt = now
	rule.DeactivatedAt = &now

	logger.WithFields(
		"operation", "service.rule.deactivate",
		"rule.id", ruleID.String(),
	).Info("Rule deactivated successfully")

	// Record audit event (best-effort)
	if s.auditWriter != nil {
		clientIP := contextutil.GetClientIP(ctx)

		afterState := RuleToMap(rule)
		if err := s.auditWriter.RecordRuleEvent(
			ctx,
			model.AuditEventRuleDeactivated,
			model.AuditActionDeactivate,
			rule.ID,
			beforeState,
			afterState,
			"Rule deactivated via API",
			clientIP,
		); err != nil {
			logger.WithFields(
				"operation", "service.rule.deactivate.audit",
				"rule.id", ruleID.String(),
				"error", err.Error(),
			).Warn("Failed to record audit event")
		}
	}

	return rule, nil
}
