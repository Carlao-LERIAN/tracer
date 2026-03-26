// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package command

//go:generate mockgen -source=create_rule.go -destination=expression_compiler_mock.go -package=command

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	libCommons "github.com/LerianStudio/lib-commons/v4/commons"
	libLog "github.com/LerianStudio/lib-commons/v4/commons/log"
	libOpentelemetry "github.com/LerianStudio/lib-commons/v4/commons/opentelemetry"

	"tracer/pkg/clock"
	"tracer/pkg/constant"
	"tracer/pkg/contextutil"
	"tracer/pkg/logging"
	"tracer/pkg/model"
)

var multipleSpaces = regexp.MustCompile(`\s+`)

// NormalizeName normalizes a rule name for uniqueness comparison.
// Applies: lowercase + trim whitespace + collapse multiple spaces.
// Example: "  mInha    REGRA  xpto " -> "minha regra xpto"
func NormalizeName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	return multipleSpaces.ReplaceAllString(name, " ")
}

// ExpressionCompiler validates CEL expressions.
// Interface defined locally per Ring pattern.
type ExpressionCompiler interface {
	Compile(ctx context.Context, expression string) (any, error)
}

// CreateRuleInput represents the input for creating a new rule.
type CreateRuleInput struct {
	Name        string
	Description string
	Expression  string
	Action      model.Decision
	Scopes      []model.Scope
}

// CreateRuleCommand handles the creation of new rules.
type CreateRuleCommand struct {
	repo        RuleRepository
	cel         ExpressionCompiler
	clock       clock.Clock
	auditWriter AuditWriter
}

// NewCreateRuleCommand creates a new CreateRuleCommand instance.
func NewCreateRuleCommand(repo RuleRepository, cel ExpressionCompiler, clk clock.Clock, auditWriter AuditWriter) *CreateRuleCommand {
	return &CreateRuleCommand{
		repo:        repo,
		cel:         cel,
		clock:       clk,
		auditWriter: auditWriter,
	}
}

// Execute creates a new rule with validation.
func (c *CreateRuleCommand) Execute(ctx context.Context, input *CreateRuleInput) (*model.Rule, error) {
	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "service.rule.create")
	defer span.End()

	logger = logging.WithTrace(ctx, logger)

	// Handle nil input
	if input == nil {
		err := constant.ErrRuleNilInput
		libOpentelemetry.HandleSpanBusinessErrorEvent(span, "Nil input provided", err)
		logger.With(
			libLog.String("operation", "service.rule.create"),
		).Log(ctx, libLog.LevelWarn, "Nil input provided")

		return nil, err
	}

	// Normalize name for storage and uniqueness check
	normalizedName := NormalizeName(input.Name)

	logger.With(
		libLog.String("operation", "service.rule.create"),
		libLog.Any("rule.name", input.Name),
		libLog.Any("rule.name_normalized", normalizedName),
	).Log(ctx, libLog.LevelInfo, "Creating rule")

	// 1. Validate CEL expression syntax
	_, err := c.cel.Compile(ctx, input.Expression)
	if err != nil {
		libOpentelemetry.HandleSpanBusinessErrorEvent(span, "Invalid CEL expression", err)
		return nil, err
	}

	// 2. Build rule entity using validating constructor (store normalized name)
	// model.NewRule normalizes nil scopes to empty slice for proper JSON serialization
	var description *string
	if input.Description != "" {
		description = &input.Description
	}

	now := c.clock.Now()

	rule, err := model.NewRule(normalizedName, input.Expression, input.Action, input.Scopes, description, now)
	if err != nil {
		libOpentelemetry.HandleSpanBusinessErrorEvent(span, "Invalid rule input", err)
		logger.With(
			libLog.String("operation", "service.rule.create"),
			libLog.String("error.message", err.Error()),
		).Log(ctx, libLog.LevelWarn, "Invalid rule input")

		return nil, err
	}

	// 3. Persist rule
	err = libOpentelemetry.SetSpanAttributesFromValue(span, "rule_input", rule, nil)
	if err != nil {
		libOpentelemetry.HandleSpanError(span, "Failed to set span attributes", err)
	}

	result, err := c.repo.Create(ctx, rule)
	if err != nil {
		if errors.Is(err, constant.ErrRuleNameAlreadyExistsInCtx) {
			libOpentelemetry.HandleSpanBusinessErrorEvent(span, "Rule name already exists in this context", err)

			return nil, err
		}

		libOpentelemetry.HandleSpanError(span, "Failed to create rule", err)
		logger.With(
			libLog.String("operation", "service.rule.create"),
			libLog.String("error.message", err.Error()),
		).Log(ctx, libLog.LevelError, "Failed to create rule")

		return nil, fmt.Errorf("failed to create rule: %w", err)
	}

	logger.With(
		libLog.String("operation", "service.rule.create"),
		libLog.String("rule.id", result.ID.String()),
		libLog.Any("rule.name", result.Name),
	).Log(ctx, libLog.LevelInfo, "Rule created")

	// Record audit event (best-effort, failures logged but don't fail the operation)
	if c.auditWriter != nil {
		clientIP := contextutil.GetClientIP(ctx)

		after := RuleToMap(result)
		if err := c.auditWriter.RecordRuleEvent(
			ctx,
			model.AuditEventRuleCreated,
			model.AuditActionCreate,
			result.ID,
			nil,   // no "before" state for create
			after, // "after" state is the created rule
			"Rule created via API",
			clientIP,
		); err != nil {
			logger.With(
				libLog.String("operation", "service.rule.create.audit"),
				libLog.String("rule.id", result.ID.String()),
				libLog.String("error", err.Error()),
			).Log(ctx, libLog.LevelWarn, "Failed to record audit event")
		}
	}

	return result, nil
}
