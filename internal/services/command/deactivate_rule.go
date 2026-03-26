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

// Sentinel errors for DeactivateRuleService constructor validation.
var (
	ErrDeactivateNilRepository = errors.New("repository is required")
	ErrDeactivateNilClock      = errors.New("clock is required")
)

// DeactivateRuleService handles rule deactivation (ACTIVE/DRAFT → INACTIVE).
type DeactivateRuleService struct {
	repository  RuleRepository
	clock       clock.Clock
	auditWriter AuditWriter
	cacheWriter RuleCacheWriter
}

// NewDeactivateRuleService creates a new DeactivateRuleService.
// The cacheWriter parameter is optional (nil-safe); when set, it synchronously
// removes the rule from the in-memory cache after a successful deactivation.
func NewDeactivateRuleService(repository RuleRepository, clk clock.Clock, auditWriter AuditWriter, cacheWriter RuleCacheWriter) (*DeactivateRuleService, error) {
	if repository == nil {
		return nil, ErrDeactivateNilRepository
	}

	if clk == nil {
		return nil, ErrDeactivateNilClock
	}

	return &DeactivateRuleService{
		repository:  repository,
		clock:       clk,
		auditWriter: auditWriter,
		cacheWriter: cacheWriter,
	}, nil
}

// Execute deactivates a rule by updating status to INACTIVE.
// Idempotent: if already INACTIVE, returns the rule without error.
// Returns the updated rule for atomic deactivate-and-return pattern.
func (s *DeactivateRuleService) Execute(ctx context.Context, ruleID uuid.UUID) (*model.Rule, error) {
	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "service.rule.deactivate")
	defer span.End()

	logger = logging.WithTrace(ctx, logger)

	_ = libOpentelemetry.SetSpanAttributesFromValue(span, "deactivate_input", map[string]any{
		"rule_id":   ruleID.String(),
		"operation": "deactivate",
	}, nil)

	logger.With(
		libLog.String("operation", "service.rule.deactivate"),
		libLog.String("rule.id", ruleID.String()),
	).Log(ctx, libLog.LevelInfo, "Deactivating rule")

	rule, err := s.repository.GetByID(ctx, ruleID)
	if err != nil {
		if errors.Is(err, constant.ErrRuleNotFound) {
			libOpentelemetry.HandleSpanBusinessErrorEvent(span, "Rule not found", err)
			logger.With(
				libLog.String("operation", "service.rule.deactivate"),
				libLog.String("rule.id", ruleID.String()),
			).Log(ctx, libLog.LevelWarn, "Rule not found")

			return nil, libCommons.ValidateBusinessError(constant.ErrRuleNotFound, "Rule")
		}

		libOpentelemetry.HandleSpanError(span, "Failed to get rule from repository", err)
		logger.With(
			libLog.String("operation", "service.rule.deactivate"),
			libLog.String("rule.id", ruleID.String()),
			libLog.String("error.message", err.Error()),
		).Log(ctx, libLog.LevelError, "Failed to get rule")

		return nil, fmt.Errorf("failed to get rule: %w", err)
	}

	// Idempotency: if already inactive, return the rule (no-op)
	// Check before audit capture to avoid unnecessary state snapshots
	if rule.Status == model.RuleStatusInactive {
		logger.With(
			libLog.String("operation", "service.rule.deactivate"),
			libLog.String("rule.id", ruleID.String()),
		).Log(ctx, libLog.LevelInfo, "Rule already inactive (idempotent no-op)")

		return rule, nil
	}

	// Capture "before" state for audit
	beforeState := RuleToMap(rule)

	// Use domain model method for status transition (validates and maintains invariants)
	if err := rule.SetStatus(model.RuleStatusInactive, s.clock.Now()); err != nil {
		// Check for invalid transition (business error)
		var transitionErr *model.InvalidTransitionError
		if errors.As(err, &transitionErr) {
			libOpentelemetry.HandleSpanBusinessErrorEvent(span, "Invalid state transition", transitionErr)
			logger.With(
				libLog.String("operation", "service.rule.deactivate"),
				libLog.String("rule.id", ruleID.String()),
				libLog.String("rule.status_from", string(transitionErr.From)),
				libLog.String("rule.status_to", string(transitionErr.To)),
			).Log(ctx, libLog.LevelWarn, "Invalid transition")

			return nil, transitionErr
		}

		// Technical error (invalid status value or other)
		libOpentelemetry.HandleSpanError(span, "Failed to set rule status", err)
		logger.With(
			libLog.String("operation", "service.rule.deactivate"),
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
			libLog.String("operation", "service.rule.deactivate"),
			libLog.String("rule.id", ruleID.String()),
			libLog.String("error.message", err.Error()),
		).Log(ctx, libLog.LevelError, "Failed to update rule")

		return nil, fmt.Errorf("failed to update rule: %w", err)
	}

	logger.With(
		libLog.String("operation", "service.rule.deactivate"),
		libLog.String("rule.id", updatedRule.ID.String()),
	).Log(ctx, libLog.LevelInfo, "Rule deactivated successfully")

	// Record audit event (best-effort)
	if s.auditWriter != nil {
		clientIP := contextutil.GetClientIP(ctx)

		afterState := RuleToMap(updatedRule)
		if err := s.auditWriter.RecordRuleEvent(
			ctx,
			model.AuditEventRuleDeactivated,
			model.AuditActionDeactivate,
			updatedRule.ID,
			beforeState,
			afterState,
			"Rule deactivated via API",
			clientIP,
		); err != nil {
			logger.With(
				libLog.String("operation", "service.rule.deactivate.audit"),
				libLog.String("rule.id", updatedRule.ID.String()),
				libLog.String("error", err.Error()),
			).Log(ctx, libLog.LevelWarn, "Failed to record audit event")
		}
	}

	if s.cacheWriter != nil {
		s.cacheWriter.RemoveRule(updatedRule.ID)
	}

	return updatedRule, nil
}
