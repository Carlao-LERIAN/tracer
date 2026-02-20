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

	"tracer/pkg/constant"
	"tracer/pkg/contextutil"
	"tracer/pkg/logging"
	"tracer/pkg/model"
)

// DeleteRuleService handles rule deletion (INACTIVE → DELETED).
type DeleteRuleService struct {
	repository  RuleRepository
	auditWriter AuditWriter
	notifier    RuleChangeNotifier
}

var ErrNilDeleteRuleRepository = errors.New("delete rule repository is nil")
var ErrNilAuditWriter = errors.New("audit writer is nil")

// NewDeleteRuleService creates a new DeleteRuleService.
// The notifier parameter is optional (nil-safe); when set, it triggers an
// immediate cache sync after a successful deletion.
func NewDeleteRuleService(repository RuleRepository, auditWriter AuditWriter, notifier RuleChangeNotifier) (*DeleteRuleService, error) {
	if repository == nil {
		return nil, ErrNilDeleteRuleRepository
	}

	if auditWriter == nil {
		return nil, ErrNilAuditWriter
	}

	return &DeleteRuleService{
		repository:  repository,
		auditWriter: auditWriter,
		notifier:    notifier,
	}, nil
}

// Execute soft-deletes a rule by updating status to DELETED.
// Only INACTIVE rules can be deleted per state machine.
// Idempotent: if already DELETED, returns success without error.
func (s *DeleteRuleService) Execute(ctx context.Context, ruleID uuid.UUID) error {
	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "service.rule.delete")
	defer span.End()

	logger = logging.WithTrace(ctx, logger)

	_ = libOpentelemetry.SetSpanAttributesFromStruct(&span, "delete_input", map[string]any{
		"rule_id":   ruleID.String(),
		"operation": "delete",
	})

	logger.WithFields(
		"operation", "service.rule.delete",
		"rule.id", ruleID.String(),
	).Info("Deleting rule")

	rule, err := s.repository.GetByID(ctx, ruleID)
	if err != nil {
		if errors.Is(err, constant.ErrRuleNotFound) {
			libOpentelemetry.HandleSpanBusinessErrorEvent(&span, "Rule not found", err)
			logger.WithFields(
				"operation", "service.rule.delete",
				"rule.id", ruleID.String(),
			).Warn("Rule not found")

			return libCommons.ValidateBusinessError(constant.ErrRuleNotFound, "Rule")
		}

		libOpentelemetry.HandleSpanError(&span, "Failed to get rule from repository", err)
		logger.WithFields(
			"operation", "service.rule.delete",
			"rule.id", ruleID.String(),
			"error.message", err.Error(),
		).Error("Failed to get rule")

		return fmt.Errorf("failed to get rule: %w", err)
	}

	// Idempotency: if already deleted, return success (no-op)
	// Check before audit capture to avoid unnecessary state snapshots
	if rule.Status == model.RuleStatusDeleted {
		logger.WithFields(
			"operation", "service.rule.delete",
			"rule.id", ruleID.String(),
		).Info("Rule already deleted (idempotent no-op)")

		return nil
	}

	// Capture "before" state for audit
	beforeState := RuleToMap(rule)

	// Check if transition is valid (only DRAFT/INACTIVE → DELETED allowed)
	if !rule.Status.CanTransitionTo(model.RuleStatusDeleted) {
		err := model.NewInvalidTransitionError(rule.Status, model.RuleStatusDeleted)
		libOpentelemetry.HandleSpanBusinessErrorEvent(&span, "Invalid state transition", err)
		logger.WithFields(
			"operation", "service.rule.delete",
			"rule.id", ruleID.String(),
			"rule.status_from", string(rule.Status),
			"rule.status_to", "DELETED",
		).Warn("Invalid transition")

		return err
	}

	// Persist deletion (repository.Delete handles status update to DELETED)
	// Do NOT call rule.SetStatus() before this - would mutate object before persistence succeeds
	if err := s.repository.Delete(ctx, ruleID); err != nil {
		libOpentelemetry.HandleSpanError(&span, "Failed to delete rule", err)
		logger.WithFields(
			"operation", "service.rule.delete",
			"rule.id", ruleID.String(),
			"error.message", err.Error(),
		).Error("Failed to delete rule")

		return fmt.Errorf("failed to delete rule: %w", err)
	}

	logger.WithFields(
		"operation", "service.rule.delete",
		"rule.id", ruleID.String(),
	).Info("Rule deleted successfully")

	// Record audit event (best-effort)
	if s.auditWriter != nil {
		clientIP := contextutil.GetClientIP(ctx)

		// For delete, "after" is nil since the resource no longer exists logically
		if err := s.auditWriter.RecordRuleEvent(
			ctx,
			model.AuditEventRuleDeleted,
			model.AuditActionDelete,
			ruleID,
			beforeState,
			nil, // no "after" state for delete
			"Rule deleted via API",
			clientIP,
		); err != nil {
			logger.WithFields(
				"operation", "service.rule.delete.audit",
				"rule.id", ruleID.String(),
				"error", err.Error(),
			).Warn("Failed to record audit event")
		}
	}

	if s.notifier != nil {
		s.notifier.Notify()
	}

	return nil
}
