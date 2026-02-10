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

// DeactivateLimitCommand handles limit deactivation (ACTIVE → INACTIVE).
type DeactivateLimitCommand struct {
	repo        LimitRepository
	clock       clock.Clock
	auditWriter AuditWriter
}

// NewDeactivateLimitCommand creates a new DeactivateLimitCommand with dependencies.
func NewDeactivateLimitCommand(repo LimitRepository, clk clock.Clock, auditWriter AuditWriter) *DeactivateLimitCommand {
	return &DeactivateLimitCommand{
		repo:        repo,
		clock:       clk,
		auditWriter: auditWriter,
	}
}

// Execute deactivates an active limit.
// Idempotent: if already INACTIVE, returns the limit without error.
// Returns error if limit is not found or transition is invalid (e.g., from DELETED).
func (c *DeactivateLimitCommand) Execute(ctx context.Context, id uuid.UUID) (*model.Limit, error) {
	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "service.limit.deactivate")
	defer span.End()

	logger = logging.WithTrace(ctx, logger)

	// Check context cancellation first
	if ctx.Err() != nil {
		libOpentelemetry.HandleSpanError(&span, "Context cancelled", ctx.Err())
		logger.WithFields(
			"operation", "service.limit.deactivate",
		).Warn("Context cancelled")

		return nil, ctx.Err()
	}

	// Validate input
	if id == uuid.Nil {
		libOpentelemetry.HandleSpanBusinessErrorEvent(&span, "Invalid input: nil UUID", constant.ErrLimitInvalidID)
		logger.WithFields(
			"operation", "service.limit.deactivate",
		).Warn("Invalid input: nil UUID")

		return nil, constant.ErrLimitInvalidID
	}

	_ = libOpentelemetry.SetSpanAttributesFromStruct(&span, "deactivate_input", map[string]any{
		"limit_id":  id.String(),
		"operation": "deactivate",
	})

	logger.WithFields(
		"operation", "service.limit.deactivate",
		"limit.id", id.String(),
	).Info("Deactivating limit")

	limit, err := c.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, constant.ErrLimitNotFound) {
			libOpentelemetry.HandleSpanBusinessErrorEvent(&span, "Limit not found", err)
			logger.WithFields(
				"operation", "service.limit.deactivate",
				"limit.id", id.String(),
			).Warn("Limit not found")

			return nil, err
		}

		libOpentelemetry.HandleSpanError(&span, "Failed to get limit from repository", err)
		logger.WithFields(
			"operation", "service.limit.deactivate",
			"limit.id", id.String(),
			"error.message", err.Error(),
		).Error("Failed to get limit")

		return nil, fmt.Errorf("failed to get limit: %w", err)
	}

	// Defensive check: treat nil limit as not found (guards against repo returning nil, nil)
	if limit == nil {
		libOpentelemetry.HandleSpanBusinessErrorEvent(&span, "Limit not found", constant.ErrLimitNotFound)
		logger.WithFields(
			"operation", "service.limit.deactivate",
			"limit.id", id.String(),
		).Warn("Limit not found")

		return nil, constant.ErrLimitNotFound
	}

	// Capture "before" state for audit
	beforeState := LimitToMap(limit)

	// Idempotency: if already inactive, return the limit (no-op)
	if limit.Status == model.LimitStatusInactive {
		logger.WithFields(
			"operation", "service.limit.deactivate",
			"limit.id", id.String(),
		).Info("Limit already inactive (idempotent no-op)")

		return limit, nil
	}

	// Capture original status before mutation for accurate logging
	originalStatus := limit.Status

	// Use domain model's SetStatus for transition validation
	if err := limit.SetStatus(model.LimitStatusInactive, c.clock.Now()); err != nil {
		libOpentelemetry.HandleSpanBusinessErrorEvent(&span, "Invalid state transition", err)
		logger.WithFields(
			"operation", "service.limit.deactivate",
			"limit.id", id.String(),
			"limit.status_from", string(originalStatus),
			"limit.status_to", "INACTIVE",
		).Warn("Invalid transition")

		return nil, libCommons.ValidateBusinessError(constant.ErrLimitInvalidStatusChange, err.Error())
	}

	if err := c.repo.UpdateStatus(ctx, id, model.LimitStatusInactive, limit.UpdatedAt); err != nil {
		libOpentelemetry.HandleSpanError(&span, "Failed to update limit status", err)
		logger.WithFields(
			"operation", "service.limit.deactivate",
			"limit.id", id.String(),
			"error.message", err.Error(),
		).Error("Failed to update limit status")

		return nil, fmt.Errorf("failed to update limit status: %w", err)
	}

	logger.WithFields(
		"operation", "service.limit.deactivate",
		"limit.id", id.String(),
		"limit.status", string(limit.Status),
	).Info("Limit deactivated successfully")

	// Record audit event (best-effort)
	if c.auditWriter != nil {
		clientIP := contextutil.GetClientIP(ctx)

		afterState := LimitToMap(limit)
		if err := c.auditWriter.RecordLimitEvent(
			ctx,
			model.AuditEventLimitDeactivated,
			model.AuditActionDeactivate,
			limit.ID,
			beforeState,
			afterState,
			"Limit deactivated via API",
			clientIP,
		); err != nil {
			logger.WithFields(
				"operation", "service.limit.deactivate.audit",
				"limit.id", limit.ID.String(),
				"error", err.Error(),
			).Warn("Failed to record audit event")
		}
	}

	return limit, nil
}
