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

// DeleteLimitCommand handles limit deletion (soft-delete).
type DeleteLimitCommand struct {
	repo        LimitRepository
	clock       clock.Clock
	auditWriter AuditWriter
}

// NewDeleteLimitCommand creates a new DeleteLimitCommand with dependencies.
func NewDeleteLimitCommand(repo LimitRepository, clk clock.Clock, auditWriter AuditWriter) *DeleteLimitCommand {
	return &DeleteLimitCommand{
		repo:        repo,
		clock:       clk,
		auditWriter: auditWriter,
	}
}

// Execute soft-deletes a limit by setting status to DELETED.
// DELETED is a terminal state - the limit cannot be reactivated.
// Idempotent: if already DELETED, returns success without error.
// Returns error if limit is not found.
func (c *DeleteLimitCommand) Execute(ctx context.Context, id uuid.UUID) error {
	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "service.limit.delete")
	defer span.End()

	logger = logging.WithTrace(ctx, logger)

	_ = libOpentelemetry.SetSpanAttributesFromStruct(&span, "delete_input", map[string]any{
		"limit_id":  id.String(),
		"operation": "delete",
	})

	logger.WithFields(
		"operation", "service.limit.delete",
		"limit.id", id.String(),
	).Info("Deleting limit")

	// Validate input
	if id == uuid.Nil {
		libOpentelemetry.HandleSpanBusinessErrorEvent(&span, "Invalid input: nil UUID", constant.ErrLimitInvalidID)
		logger.WithFields(
			"operation", "service.limit.delete",
		).Warn("Invalid input: nil UUID")

		return constant.ErrLimitInvalidID
	}

	// Check context cancellation
	if ctx.Err() != nil {
		libOpentelemetry.HandleSpanError(&span, "Context cancelled", ctx.Err())
		logger.WithFields(
			"operation", "service.limit.delete",
			"limit.id", id.String(),
		).Warn("Context cancelled")

		return ctx.Err()
	}

	limit, err := c.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, constant.ErrLimitNotFound) {
			libOpentelemetry.HandleSpanBusinessErrorEvent(&span, "Limit not found", err)
			logger.WithFields(
				"operation", "service.limit.delete",
				"limit.id", id.String(),
			).Warn("Limit not found")

			return err
		}

		libOpentelemetry.HandleSpanError(&span, "Failed to get limit from repository", err)
		logger.WithFields(
			"operation", "service.limit.delete",
			"limit.id", id.String(),
			"error.message", err.Error(),
		).Error("Failed to get limit")

		return fmt.Errorf("failed to get limit: %w", err)
	}

	// Defensive check: treat nil limit as not found (guards against repo returning nil, nil)
	if limit == nil {
		libOpentelemetry.HandleSpanBusinessErrorEvent(&span, "Limit not found", constant.ErrLimitNotFound)
		logger.WithFields(
			"operation", "service.limit.delete",
			"limit.id", id.String(),
		).Warn("Limit not found")

		return constant.ErrLimitNotFound
	}

	// Capture "before" state for audit
	beforeState := LimitToMap(limit)

	// Idempotency: if already deleted, return success (no-op)
	if limit.Status == model.LimitStatusDeleted {
		logger.WithFields(
			"operation", "service.limit.delete",
			"limit.id", id.String(),
		).Info("Limit already deleted (idempotent no-op)")

		return nil
	}

	// Capture original status before mutation for accurate logging
	originalStatus := limit.Status

	// Validate transition via model.Limit.SetStatus which enforces allowed transitions:
	// ACTIVE → DELETED and INACTIVE → DELETED are valid; DELETED → DELETED is handled
	// above as idempotent. See model.LimitStatus and model.Limit.SetStatus for rules.
	if err := limit.SetStatus(model.LimitStatusDeleted, c.clock.Now()); err != nil {
		libOpentelemetry.HandleSpanBusinessErrorEvent(&span, "Invalid state transition", err)
		logger.WithFields(
			"operation", "service.limit.delete",
			"limit.id", id.String(),
			"limit.status_from", string(originalStatus),
			"limit.status_to", "DELETED",
		).Warn("Invalid transition")

		return libCommons.ValidateBusinessError(constant.ErrLimitInvalidStatusChange, err.Error())
	}

	// Use limit.UpdatedAt from SetStatus() for timestamp consistency
	if err := c.repo.UpdateStatus(ctx, id, model.LimitStatusDeleted, limit.UpdatedAt); err != nil {
		libOpentelemetry.HandleSpanError(&span, "Failed to delete limit", err)
		logger.WithFields(
			"operation", "service.limit.delete",
			"limit.id", id.String(),
			"error.message", err.Error(),
		).Error("Failed to delete limit")

		return fmt.Errorf("failed to delete limit: %w", err)
	}

	logger.WithFields(
		"operation", "service.limit.delete",
		"limit.id", id.String(),
		"limit.status", string(limit.Status),
	).Info("Limit deleted successfully")

	// Record audit event (best-effort)
	if c.auditWriter != nil {
		clientIP := contextutil.GetClientIP(ctx)

		// For delete, "after" is nil since the resource no longer exists logically
		if err := c.auditWriter.RecordLimitEvent(
			ctx,
			model.AuditEventLimitDeleted,
			model.AuditActionDelete,
			id,
			beforeState,
			nil, // no "after" state for delete
			"Limit deleted via API",
			clientIP,
		); err != nil {
			logger.WithFields(
				"operation", "service.limit.delete.audit",
				"limit.id", id.String(),
				"error", err.Error(),
			).Warn("Failed to record audit event")
		}
	}

	return nil
}
