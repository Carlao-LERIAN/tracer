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

// Sentinel errors for ActivateRuleService constructor validation.
var (
	ErrActivateNilRepository         = errors.New("repository is required")
	ErrActivateNilExpressionCompiler = errors.New("expressionCompiler is required")
	ErrActivateNilClock              = errors.New("clock is required")
)

// ActivateRuleService handles rule activation (DRAFT/INACTIVE → ACTIVE).
type ActivateRuleService struct {
	repository         RuleRepository
	expressionCompiler ExpressionCompiler
	clock              clock.Clock
	auditWriter        AuditWriter
}

// NewActivateRuleService creates a new ActivateRuleService.
func NewActivateRuleService(repository RuleRepository, expressionCompiler ExpressionCompiler, clk clock.Clock, auditWriter AuditWriter) (*ActivateRuleService, error) {
	if repository == nil {
		return nil, ErrActivateNilRepository
	}

	if expressionCompiler == nil {
		return nil, ErrActivateNilExpressionCompiler
	}

	if clk == nil {
		return nil, ErrActivateNilClock
	}

	return &ActivateRuleService{
		repository:         repository,
		expressionCompiler: expressionCompiler,
		clock:              clk,
		auditWriter:        auditWriter,
	}, nil
}

// Execute activates a rule by validating its expression and updating status to ACTIVE.
// Idempotent: if already ACTIVE, returns the rule without error.
// Returns the updated rule for atomic activate-and-return pattern.
func (s *ActivateRuleService) Execute(ctx context.Context, ruleID uuid.UUID) (*model.Rule, error) {
	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "service.rule.activate")
	defer span.End()

	logger = logging.WithTrace(ctx, logger)

	_ = libOpentelemetry.SetSpanAttributesFromStruct(&span, "activate_input", map[string]any{
		"rule_id":   ruleID.String(),
		"operation": "activate",
	})

	logger.WithFields(
		"operation", "service.rule.activate",
		"rule.id", ruleID.String(),
	).Info("Activating rule")

	rule, err := s.repository.GetByID(ctx, ruleID)
	if err != nil {
		if errors.Is(err, constant.ErrRuleNotFound) {
			libOpentelemetry.HandleSpanBusinessErrorEvent(&span, "Rule not found", err)
			logger.WithFields(
				"operation", "service.rule.activate",
				"rule.id", ruleID.String(),
			).Warn("Rule not found")

			return nil, libCommons.ValidateBusinessError(constant.ErrRuleNotFound, "Rule")
		}

		libOpentelemetry.HandleSpanError(&span, "Failed to get rule from repository", err)
		logger.WithFields(
			"operation", "service.rule.activate",
			"rule.id", ruleID.String(),
			"error.message", err.Error(),
		).Error("Failed to get rule")

		return nil, fmt.Errorf("failed to get rule: %w", err)
	}

	if rule.Expression == "" {
		err := libCommons.ValidateBusinessError(constant.ErrBadRequest, "expression is required to activate rule")
		libOpentelemetry.HandleSpanBusinessErrorEvent(&span, "Empty expression", err)
		logger.WithFields(
			"operation", "service.rule.activate",
			"rule.id", ruleID.String(),
		).Warn("Cannot activate rule with empty expression")

		return nil, err
	}

	// Idempotency: if already active, return the rule (no-op)
	if rule.Status == model.RuleStatusActive {
		logger.WithFields(
			"operation", "service.rule.activate",
			"rule.id", ruleID.String(),
		).Info("Rule already active (idempotent no-op)")

		return rule, nil
	}

	// Capture "before" state for audit (after idempotency check, before mutation)
	beforeState := RuleToMap(rule)

	// Check if transition is valid
	if !rule.Status.CanTransitionTo(model.RuleStatusActive) {
		err := model.NewInvalidTransitionError(rule.Status, model.RuleStatusActive)
		libOpentelemetry.HandleSpanBusinessErrorEvent(&span, "Invalid state transition", err)
		logger.WithFields(
			"operation", "service.rule.activate",
			"rule.id", ruleID.String(),
			"rule.status_from", string(rule.Status),
			"rule.status_to", "ACTIVE",
		).Warn("Invalid transition")

		return nil, err
	}

	logger.WithFields(
		"operation", "service.rule.activate",
		"rule.id", ruleID.String(),
	).Info("Validating expression for rule")

	if _, err := s.expressionCompiler.Compile(ctx, rule.Expression); err != nil {
		businessErr := libCommons.ValidateBusinessError(constant.ErrExpressionSyntax, err.Error())
		libOpentelemetry.HandleSpanBusinessErrorEvent(&span, "Expression compilation failed", businessErr)
		logger.WithFields(
			"operation", "service.rule.activate",
			"rule.id", ruleID.String(),
			"error.message", err.Error(),
		).Warn("Expression validation failed")

		return nil, businessErr
	}

	now := s.clock.Now()
	if err := s.repository.UpdateStatus(ctx, ruleID, model.RuleStatusActive, now, &now, nil); err != nil {
		libOpentelemetry.HandleSpanError(&span, "Failed to update rule status", err)
		logger.WithFields(
			"operation", "service.rule.activate",
			"rule.id", ruleID.String(),
			"error.message", err.Error(),
		).Error("Failed to update rule status")

		return nil, fmt.Errorf("failed to update rule status: %w", err)
	}

	// Update the rule object with new status and timestamps
	rule.Status = model.RuleStatusActive
	rule.UpdatedAt = now
	rule.ActivatedAt = &now
	rule.DeactivatedAt = nil

	logger.WithFields(
		"operation", "service.rule.activate",
		"rule.id", ruleID.String(),
	).Info("Rule activated successfully")

	// Record audit event (best-effort)
	if s.auditWriter != nil {
		clientIP := contextutil.GetClientIP(ctx)

		afterState := RuleToMap(rule)
		if err := s.auditWriter.RecordRuleEvent(
			ctx,
			model.AuditEventRuleActivated,
			model.AuditActionActivate,
			rule.ID,
			beforeState,
			afterState,
			"Rule activated via API",
			clientIP,
		); err != nil {
			logger.WithFields(
				"operation", "service.rule.activate.audit",
				"rule.id", ruleID.String(),
				"error", err.Error(),
			).Warn("Failed to record audit event")
		}
	}

	return rule, nil
}
