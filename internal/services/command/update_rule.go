// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package command

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	libCommons "github.com/LerianStudio/lib-commons/v2/commons"
	libLog "github.com/LerianStudio/lib-commons/v2/commons/log"
	libOpentelemetry "github.com/LerianStudio/lib-commons/v2/commons/opentelemetry"

	"tracer/pkg/clock"
	"tracer/pkg/constant"
	"tracer/pkg/contextutil"
	"tracer/pkg/logging"
	"tracer/pkg/model"
)

// UpdateRuleInput represents the input for updating an existing rule.
// All fields are optional (pointers) to support partial updates.
type UpdateRuleInput struct {
	Name        *string
	Description *string
	Expression  *string
	Action      *model.Decision
	Scopes      *[]model.Scope
}

// UpdateRuleCommand handles the update of existing rules.
type UpdateRuleCommand struct {
	repo        RuleRepository
	cel         ExpressionCompiler
	clock       clock.Clock
	auditWriter AuditWriter
}

// NewUpdateRuleCommand creates a new UpdateRuleCommand instance.
func NewUpdateRuleCommand(repo RuleRepository, cel ExpressionCompiler, clk clock.Clock, auditWriter AuditWriter) *UpdateRuleCommand {
	return &UpdateRuleCommand{
		repo:        repo,
		cel:         cel,
		clock:       clk,
		auditWriter: auditWriter,
	}
}

// Execute updates an existing rule with validation.
func (c *UpdateRuleCommand) Execute(ctx context.Context, id uuid.UUID, input *UpdateRuleInput) (*model.Rule, error) {
	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "service.rule.update")
	defer span.End()

	logger = logging.WithTrace(ctx, logger)

	logger.WithFields(
		"operation", "service.rule.update",
		"rule.id", id.String(),
	).Info("Updating rule")

	rule, err := c.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, constant.ErrRuleNotFound) {
			libOpentelemetry.HandleSpanBusinessErrorEvent(&span, "Rule not found", err)
			return nil, err
		}

		libOpentelemetry.HandleSpanError(&span, "Failed to get rule", err)

		return nil, fmt.Errorf("failed to get rule: %w", err)
	}

	beforeState := RuleToMap(rule)

	// Validate expression update (only for DRAFT rules)
	if input.Expression != nil {
		if rule.Status != model.RuleStatusDraft {
			libOpentelemetry.HandleSpanBusinessErrorEvent(&span, "Expression cannot be modified for non-DRAFT rules", constant.ErrExpressionNotModifiable)
			return nil, constant.ErrExpressionNotModifiable
		}

		_, err := c.cel.Compile(ctx, *input.Expression)
		if err != nil {
			libOpentelemetry.HandleSpanBusinessErrorEvent(&span, "Invalid CEL expression", err)
			return nil, err
		}
	}

	// Validate name uniqueness if changing and prepare normalized name
	var normalizedName *string

	if input.Name != nil {
		normalized := NormalizeName(*input.Name)
		normalizedName = &normalized

		if normalized != rule.Name {
			existing, err := c.repo.GetByName(ctx, normalized)
			if err != nil && !errors.Is(err, constant.ErrRuleNotFound) {
				libOpentelemetry.HandleSpanError(&span, "Failed to check name uniqueness", err)
				return nil, fmt.Errorf("failed to check name uniqueness: %w", err)
			}

			if existing != nil {
				libOpentelemetry.HandleSpanBusinessErrorEvent(&span, "Rule name already exists", constant.ErrRuleNameAlreadyExists)
				return nil, constant.ErrRuleNameAlreadyExists
			}
		}
	}

	// Validate action FIRST (before any mutations) to ensure atomicity
	if input.Action != nil {
		if err := rule.SetAction(*input.Action, c.clock.Now()); err != nil {
			libOpentelemetry.HandleSpanBusinessErrorEvent(&span, "Invalid decision value", err)
			return nil, err
		}
	}

	// Use domain model Update method with normalized name (validates all before mutating any)
	if err := rule.Update(normalizedName, input.Expression, input.Description, input.Scopes, c.clock.Now()); err != nil {
		libOpentelemetry.HandleSpanBusinessErrorEvent(&span, "Failed to update rule", err)
		return nil, err
	}

	err = libOpentelemetry.SetSpanAttributesFromStruct(&span, "rule_update", rule)
	if err != nil {
		libOpentelemetry.HandleSpanError(&span, "Failed to set span attributes", err)
	}

	result, err := c.repo.Update(ctx, rule)
	if err != nil {
		libOpentelemetry.HandleSpanError(&span, "Failed to update rule", err)
		logger.WithFields(
			"operation", "service.rule.update",
			"rule.id", id.String(),
			"error.message", err.Error(),
		).Error("Failed to update rule")

		return nil, fmt.Errorf("failed to update rule: %w", err)
	}

	logger.WithFields(
		"operation", "service.rule.update",
		"rule.id", result.ID.String(),
		"rule.name", result.Name,
	).Info("Rule updated successfully")

	c.recordAuditEvent(ctx, logger, result, beforeState)

	return result, nil
}

func (c *UpdateRuleCommand) recordAuditEvent(ctx context.Context, logger libLog.Logger, result *model.Rule, beforeState map[string]any) {
	if c.auditWriter == nil {
		return
	}

	clientIP := contextutil.GetClientIP(ctx)
	afterState := RuleToMap(result)

	if err := c.auditWriter.RecordRuleEvent(
		ctx,
		model.AuditEventRuleUpdated,
		model.AuditActionUpdate,
		result.ID,
		beforeState,
		afterState,
		"Rule updated via API",
		clientIP,
	); err != nil {
		logger.WithFields(
			"operation", "service.rule.update.audit",
			"rule.id", result.ID.String(),
			"error", err.Error(),
		).Warn("Failed to record audit event")
	}
}
