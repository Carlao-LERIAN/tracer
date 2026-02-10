// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package command

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"go.opentelemetry.io/otel/trace"

	libCommons "github.com/LerianStudio/lib-commons/v2/commons"
	libLog "github.com/LerianStudio/lib-commons/v2/commons/log"
	libOpentelemetry "github.com/LerianStudio/lib-commons/v2/commons/opentelemetry"

	"tracer/pkg/clock"
	"tracer/pkg/constant"
	"tracer/pkg/contextutil"
	"tracer/pkg/logging"
	"tracer/pkg/model"
)

// UpdateLimitInput defines input for updating a limit.
// Only non-nil fields are updated.
//
// ARCHITECTURE NOTE: This struct intentionally mirrors in.UpdateLimitInput from the HTTP layer.
// The HTTP struct contains validation tags (validate:"...") for request validation,
// while this command struct is a pure DTO without framework dependencies.
// The HTTP layer converts its struct to this one via in.ToUpdateLimitServiceInput()
// before calling command handlers. This separation ensures:
//   - HTTP adapter owns request validation (tags, format checks)
//   - Command layer remains framework-agnostic and testable
//   - Domain model (limit.Update) performs business validation
//
// Note: LimitType and Currency are immutable and cannot be updated.
type UpdateLimitInput struct {
	Name        *string          `json:"name,omitempty"`
	Description *string          `json:"description,omitempty"`
	MaxAmount   *decimal.Decimal `json:"maxAmount,omitempty"`
	Scopes      *[]model.Scope   `json:"scopes,omitempty"`
}

// UpdateLimitCommand handles limit updates.
type UpdateLimitCommand struct {
	repo        LimitRepository
	clock       clock.Clock
	auditWriter AuditWriter
}

// NewUpdateLimitCommand creates a new UpdateLimitCommand with dependencies.
// Returns an error if repo or clk is nil to catch invalid dependency injection at construction time.
func NewUpdateLimitCommand(repo LimitRepository, clk clock.Clock, auditWriter AuditWriter) (*UpdateLimitCommand, error) {
	if repo == nil {
		return nil, ErrNilLimitRepository
	}

	if clk == nil {
		return nil, ErrNilClock
	}

	return &UpdateLimitCommand{
		repo:        repo,
		clock:       clk,
		auditWriter: auditWriter,
	}, nil
}

// Execute updates an existing limit.
// Returns constant.ErrLimitNilInput for nil input.
// Returns constant.ErrLimitInvalidID for nil UUID.
// Returns constant.ErrLimitAlreadyDeleted if attempting to update a deleted limit.
// Only fields with non-nil values in input are updated.
func (c *UpdateLimitCommand) Execute(ctx context.Context, id uuid.UUID, input *UpdateLimitInput) (*model.Limit, error) {
	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "service.limit.update")
	defer span.End()

	logger = logging.WithTrace(ctx, logger)

	if err := c.validateInput(&span, id, input); err != nil {
		return nil, err
	}

	logger.WithFields(
		"operation", "service.limit.update",
		"limit.id", id.String(),
	).Info("Updating limit")

	normalizedInput := c.normalizeInput(input)

	_ = libOpentelemetry.SetSpanAttributesFromStruct(&span, "update_limit_input", map[string]any{
		"limit_id":        id.String(),
		"has_name":        normalizedInput.Name != nil,
		"has_max_amount":  normalizedInput.MaxAmount != nil,
		"has_description": normalizedInput.Description != nil,
		"has_scopes":      normalizedInput.Scopes != nil,
	})

	if ctx.Err() != nil {
		libOpentelemetry.HandleSpanError(&span, "Context cancelled", ctx.Err())
		logger.WithFields(
			"operation", "service.limit.update",
			"limit.id", id.String(),
		).Warn("Context cancelled before fetching limit")

		return nil, ctx.Err()
	}

	limit, err := c.fetchLimit(ctx, &span, logger, id)
	if err != nil {
		return nil, err
	}

	beforeState := LimitToMap(limit)

	if limit.Status == model.LimitStatusDeleted {
		err := constant.ErrLimitAlreadyDeleted
		libOpentelemetry.HandleSpanBusinessErrorEvent(&span, "Cannot update deleted limit", err)
		logger.WithFields(
			"operation", "service.limit.update",
			"limit.id", id.String(),
		).Warn("Attempted to update deleted limit")

		return nil, err
	}

	if !c.hasChanges(normalizedInput) {
		logger.WithFields(
			"operation", "service.limit.update",
			"limit.id", id.String(),
		).Info("No changes requested, returning existing limit")

		return limit, nil
	}

	if err := limit.Update(normalizedInput.Name, normalizedInput.MaxAmount, normalizedInput.Description, normalizedInput.Scopes, c.clock.Now()); err != nil {
		libOpentelemetry.HandleSpanBusinessErrorEvent(&span, "Update validation failed", err)
		logger.WithFields(
			"operation", "service.limit.update",
			"limit.id", id.String(),
			"error.message", err.Error(),
		).Warn("Failed to update limit entity")

		return nil, err
	}

	if ctx.Err() != nil {
		libOpentelemetry.HandleSpanError(&span, "Context cancelled", ctx.Err())
		logger.WithFields(
			"operation", "service.limit.update",
			"limit.id", id.String(),
		).Warn("Context cancelled")

		return nil, ctx.Err()
	}

	if err := c.repo.Update(ctx, limit); err != nil {
		libOpentelemetry.HandleSpanError(&span, "Failed to persist update", err)
		logger.WithFields(
			"operation", "service.limit.update",
			"limit.id", id.String(),
			"error.message", err.Error(),
		).Error("Failed to persist limit update")

		return nil, err
	}

	logger.WithFields(
		"operation", "service.limit.update",
		"limit.id", limit.ID.String(),
		"limit.name", limit.Name,
	).Info("Limit updated successfully")

	c.recordAudit(ctx, logger, limit, beforeState)

	return limit, nil
}

func (c *UpdateLimitCommand) validateInput(span *trace.Span, id uuid.UUID, input *UpdateLimitInput) error {
	if input == nil {
		err := constant.ErrLimitNilInput
		libOpentelemetry.HandleSpanBusinessErrorEvent(span, "Nil input provided", err)

		return err
	}

	if id == uuid.Nil {
		err := constant.ErrLimitInvalidID
		libOpentelemetry.HandleSpanBusinessErrorEvent(span, "Nil limit ID provided", err)

		return err
	}

	return nil
}

func (c *UpdateLimitCommand) normalizeInput(input *UpdateLimitInput) *UpdateLimitInput {
	normalizedInput := &UpdateLimitInput{
		MaxAmount: input.MaxAmount,
		Scopes:    input.Scopes,
	}

	if input.Name != nil {
		trimmed := strings.TrimSpace(*input.Name)
		normalizedInput.Name = &trimmed
	}

	if input.Description != nil {
		trimmed := strings.TrimSpace(*input.Description)
		normalizedInput.Description = &trimmed
	}

	return normalizedInput
}

func (c *UpdateLimitCommand) fetchLimit(ctx context.Context, span *trace.Span, logger libLog.Logger, id uuid.UUID) (*model.Limit, error) {
	limit, err := c.repo.GetByID(ctx, id)
	if err != nil {
		c.handleFetchError(span, logger, id, err)
		return nil, err
	}

	if limit == nil {
		libOpentelemetry.HandleSpanBusinessErrorEvent(span, "Limit not found", constant.ErrLimitNotFound)
		logger.WithFields(
			"operation", "service.limit.update",
			"limit.id", id.String(),
		).Warn("Limit not found")

		return nil, constant.ErrLimitNotFound
	}

	return limit, nil
}

func (c *UpdateLimitCommand) handleFetchError(span *trace.Span, logger libLog.Logger, id uuid.UUID, err error) {
	if errors.Is(err, constant.ErrLimitNotFound) {
		libOpentelemetry.HandleSpanBusinessErrorEvent(span, "Limit not found", err)
		logger.WithFields(
			"operation", "service.limit.update",
			"limit.id", id.String(),
		).Warn("Limit not found")
	} else {
		libOpentelemetry.HandleSpanError(span, "Failed to fetch limit", err)
		logger.WithFields(
			"operation", "service.limit.update",
			"limit.id", id.String(),
			"error.message", err.Error(),
		).Error("Failed to fetch limit")
	}
}

func (c *UpdateLimitCommand) hasChanges(input *UpdateLimitInput) bool {
	return input.Name != nil || input.MaxAmount != nil || input.Description != nil || input.Scopes != nil
}

func (c *UpdateLimitCommand) recordAudit(ctx context.Context, logger libLog.Logger, limit *model.Limit, beforeState map[string]any) {
	if c.auditWriter == nil {
		return
	}

	clientIP := contextutil.GetClientIP(ctx)
	afterState := LimitToMap(limit)

	if err := c.auditWriter.RecordLimitEvent(
		ctx,
		model.AuditEventLimitUpdated,
		model.AuditActionUpdate,
		limit.ID,
		beforeState,
		afterState,
		"Limit updated via API",
		clientIP,
	); err != nil {
		logger.WithFields(
			"operation", "service.limit.update.audit",
			"limit.id", limit.ID.String(),
			"error", err.Error(),
		).Warn("Failed to record audit event")
	}
}
