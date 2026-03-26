// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package command

import (
	"context"
	"errors"
	"fmt"

	libCommons "github.com/LerianStudio/lib-commons/v4/commons"
	libLog "github.com/LerianStudio/lib-commons/v4/commons/log"
	libOpentelemetry "github.com/LerianStudio/lib-commons/v4/commons/opentelemetry"
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
	cacheWriter        RuleCacheWriter
}

// NewActivateRuleService creates a new ActivateRuleService.
// The cacheWriter parameter is optional (nil-safe); when set, it synchronously
// updates the in-memory cache after a successful activation.
func NewActivateRuleService(repository RuleRepository, expressionCompiler ExpressionCompiler, clk clock.Clock, auditWriter AuditWriter, cacheWriter RuleCacheWriter) (*ActivateRuleService, error) {
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
		cacheWriter:        cacheWriter,
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

	_ = libOpentelemetry.SetSpanAttributesFromValue(span, "activate_input", map[string]any{
		"rule_id":   ruleID.String(),
		"operation": "activate",
	}, nil)

	logger.With(
		libLog.String("operation", "service.rule.activate"),
		libLog.String("rule.id", ruleID.String()),
	).Log(ctx, libLog.LevelInfo, "Activating rule")

	rule, err := s.repository.GetByID(ctx, ruleID)
	if err != nil {
		if errors.Is(err, constant.ErrRuleNotFound) {
			libOpentelemetry.HandleSpanBusinessErrorEvent(span, "Rule not found", err)
			logger.With(
				libLog.String("operation", "service.rule.activate"),
				libLog.String("rule.id", ruleID.String()),
			).Log(ctx, libLog.LevelWarn, "Rule not found")

			return nil, libCommons.ValidateBusinessError(constant.ErrRuleNotFound, "Rule")
		}

		libOpentelemetry.HandleSpanError(span, "Failed to get rule from repository", err)
		logger.With(
			libLog.String("operation", "service.rule.activate"),
			libLog.String("rule.id", ruleID.String()),
			libLog.String("error.message", err.Error()),
		).Log(ctx, libLog.LevelError, "Failed to get rule")

		return nil, fmt.Errorf("failed to get rule: %w", err)
	}

	if rule.Expression == "" {
		err := libCommons.ValidateBusinessError(constant.ErrBadRequest, "expression is required to activate rule")
		libOpentelemetry.HandleSpanBusinessErrorEvent(span, "Empty expression", err)
		logger.With(
			libLog.String("operation", "service.rule.activate"),
			libLog.String("rule.id", ruleID.String()),
		).Log(ctx, libLog.LevelWarn, "Cannot activate rule with empty expression")

		return nil, err
	}

	// Idempotency: if already active, return the rule (no-op)
	// Check before audit capture to avoid unnecessary state snapshots
	if rule.Status == model.RuleStatusActive {
		logger.With(
			libLog.String("operation", "service.rule.activate"),
			libLog.String("rule.id", ruleID.String()),
		).Log(ctx, libLog.LevelInfo, "Rule already active (idempotent no-op)")

		return rule, nil
	}

	// Capture "before" state for audit
	beforeState := RuleToMap(rule)

	logger.With(
		libLog.String("operation", "service.rule.activate"),
		libLog.String("rule.id", ruleID.String()),
	).Log(ctx, libLog.LevelInfo, "Validating expression for rule")

	program, err := s.expressionCompiler.Compile(ctx, rule.Expression)
	if err != nil {
		businessErr := libCommons.ValidateBusinessError(constant.ErrExpressionSyntax, err.Error())
		libOpentelemetry.HandleSpanBusinessErrorEvent(span, "Expression compilation failed", businessErr)
		logger.With(
			libLog.String("operation", "service.rule.activate"),
			libLog.String("rule.id", ruleID.String()),
			libLog.String("error.message", err.Error()),
		).Log(ctx, libLog.LevelWarn, "Expression validation failed")

		return nil, businessErr
	}

	// Use domain model method for status transition (validates and maintains invariants)
	if err := rule.SetStatus(model.RuleStatusActive, s.clock.Now()); err != nil {
		// Check for invalid transition (business error)
		var transitionErr *model.InvalidTransitionError
		if errors.As(err, &transitionErr) {
			libOpentelemetry.HandleSpanBusinessErrorEvent(span, "Invalid state transition", transitionErr)
			logger.With(
				libLog.String("operation", "service.rule.activate"),
				libLog.String("rule.id", ruleID.String()),
				libLog.String("rule.status_from", string(transitionErr.From)),
				libLog.String("rule.status_to", string(transitionErr.To)),
			).Log(ctx, libLog.LevelWarn, "Invalid transition")

			return nil, transitionErr
		}

		// Technical error (invalid status value or other)
		libOpentelemetry.HandleSpanError(span, "Failed to set rule status", err)
		logger.With(
			libLog.String("operation", "service.rule.activate"),
			libLog.String("rule.id", ruleID.String()),
			libLog.String("error.message", err.Error()),
		).Log(ctx, libLog.LevelError, "Failed to set rule status")

		return nil, fmt.Errorf("failed to set rule status: %w", err)
	}

	// Persist updated rule
	updatedRule, err := s.repository.Update(ctx, rule)
	if err != nil {
		libOpentelemetry.HandleSpanError(span, "Failed to update rule", err)
		logger.With(
			libLog.String("operation", "service.rule.activate"),
			libLog.String("rule.id", ruleID.String()),
			libLog.String("error.message", err.Error()),
		).Log(ctx, libLog.LevelError, "Failed to update rule")

		return nil, fmt.Errorf("failed to update rule: %w", err)
	}

	logger.With(
		libLog.String("operation", "service.rule.activate"),
		libLog.String("rule.id", updatedRule.ID.String()),
	).Log(ctx, libLog.LevelInfo, "Rule activated successfully")

	// Record audit event (best-effort)
	if s.auditWriter != nil {
		clientIP := contextutil.GetClientIP(ctx)

		afterState := RuleToMap(updatedRule)
		if err := s.auditWriter.RecordRuleEvent(
			ctx,
			model.AuditEventRuleActivated,
			model.AuditActionActivate,
			updatedRule.ID,
			beforeState,
			afterState,
			"Rule activated via API",
			clientIP,
		); err != nil {
			logger.With(
				libLog.String("operation", "service.rule.activate.audit"),
				libLog.String("rule.id", updatedRule.ID.String()),
				libLog.String("error", err.Error()),
			).Log(ctx, libLog.LevelWarn, "Failed to record audit event")
		}
	}

	if s.cacheWriter != nil {
		s.cacheWriter.UpsertRule(updatedRule, program)
	}

	return updatedRule, nil
}
