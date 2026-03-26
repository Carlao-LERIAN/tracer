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

// ActivateLimitCommand handles limit activation (INACTIVE → ACTIVE).
type ActivateLimitCommand struct {
	repo        LimitRepository
	clock       clock.Clock
	auditWriter AuditWriter
}

// NewActivateLimitCommand creates a new ActivateLimitCommand with dependencies.
// Returns an error if repo or clk is nil to catch invalid dependency injection at construction time.
func NewActivateLimitCommand(repo LimitRepository, clk clock.Clock, auditWriter AuditWriter) (*ActivateLimitCommand, error) {
	if repo == nil {
		return nil, ErrNilLimitRepository
	}

	if clk == nil {
		return nil, ErrNilClock
	}

	return &ActivateLimitCommand{
		repo:        repo,
		clock:       clk,
		auditWriter: auditWriter,
	}, nil
}

// Execute activates an inactive limit.
// Idempotent: if already ACTIVE, returns the limit without error.
// Returns error if limit is not found or transition is invalid (e.g., from DELETED).
func (c *ActivateLimitCommand) Execute(ctx context.Context, id uuid.UUID) (*model.Limit, error) {
	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "service.limit.activate")
	defer span.End()

	logger = logging.WithTrace(ctx, logger)

	// Check context cancellation first to avoid unnecessary work
	if ctx.Err() != nil {
		libOpentelemetry.HandleSpanError(span, "Context cancelled", ctx.Err())
		logger.With(
			libLog.String("operation", "service.limit.activate"),
			libLog.String("limit.id", id.String()),
		).Log(ctx, libLog.LevelWarn, "Context cancelled")

		return nil, ctx.Err()
	}

	// Validate input
	if id == uuid.Nil {
		libOpentelemetry.HandleSpanBusinessErrorEvent(span, "Invalid input: nil UUID", constant.ErrLimitInvalidID)
		logger.With(
			libLog.String("operation", "service.limit.activate"),
		).Log(ctx, libLog.LevelWarn, "Invalid input: nil UUID")

		return nil, constant.ErrLimitInvalidID
	}

	_ = libOpentelemetry.SetSpanAttributesFromValue(span, "activate_input", map[string]any{
		"limit_id":  id.String(),
		"operation": "activate",
	}, nil)

	logger.With(
		libLog.String("operation", "service.limit.activate"),
		libLog.String("limit.id", id.String()),
	).Log(ctx, libLog.LevelInfo, "Activating limit")

	limit, err := c.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, constant.ErrLimitNotFound) {
			libOpentelemetry.HandleSpanBusinessErrorEvent(span, "Limit not found", err)
			logger.With(
				libLog.String("operation", "service.limit.activate"),
				libLog.String("limit.id", id.String()),
			).Log(ctx, libLog.LevelWarn, "Limit not found")

			return nil, err
		}

		libOpentelemetry.HandleSpanError(span, "Failed to get limit from repository", err)
		logger.With(
			libLog.String("operation", "service.limit.activate"),
			libLog.String("limit.id", id.String()),
			libLog.String("error.message", err.Error()),
		).Log(ctx, libLog.LevelError, "Failed to get limit")

		return nil, fmt.Errorf("failed to get limit: %w", err)
	}

	// Defensive check: treat nil limit as not found (guards against repo returning nil, nil)
	if limit == nil {
		libOpentelemetry.HandleSpanBusinessErrorEvent(span, "Limit not found", constant.ErrLimitNotFound)
		logger.With(
			libLog.String("operation", "service.limit.activate"),
			libLog.String("limit.id", id.String()),
		).Log(ctx, libLog.LevelWarn, "Limit not found")

		return nil, constant.ErrLimitNotFound
	}

	// Idempotency: if already active, return the limit (no-op)
	if limit.Status == model.LimitStatusActive {
		logger.With(
			libLog.String("operation", "service.limit.activate"),
			libLog.String("limit.id", id.String()),
		).Log(ctx, libLog.LevelInfo, "Limit already active (idempotent no-op)")

		return limit, nil
	}

	// Capture "before" state for audit (after idempotency check, before mutation)
	beforeState := LimitToMap(limit)

	// Capture original status before mutation for accurate logging
	originalStatus := limit.Status

	// Use domain model's SetStatus for transition validation
	if err := limit.SetStatus(model.LimitStatusActive, c.clock.Now()); err != nil {
		libOpentelemetry.HandleSpanBusinessErrorEvent(span, "Invalid state transition", err)
		logger.With(
			libLog.String("operation", "service.limit.activate"),
			libLog.String("limit.id", id.String()),
			libLog.String("limit.status_from", string(originalStatus)),
			libLog.String("limit.status_to", "ACTIVE"),
		).Log(ctx, libLog.LevelWarn, "Invalid transition")

		return nil, libCommons.ValidateBusinessError(constant.ErrLimitInvalidStatusChange, err.Error())
	}

	if err := c.repo.UpdateStatus(ctx, id, model.LimitStatusActive, limit.UpdatedAt); err != nil {
		libOpentelemetry.HandleSpanError(span, "Failed to update limit status", err)
		logger.With(
			libLog.String("operation", "service.limit.activate"),
			libLog.String("limit.id", id.String()),
			libLog.String("error.message", err.Error()),
		).Log(ctx, libLog.LevelError, "Failed to update limit status")

		return nil, fmt.Errorf("failed to update limit status: %w", err)
	}

	logger.With(
		libLog.String("operation", "service.limit.activate"),
		libLog.String("limit.id", id.String()),
		libLog.String("limit.status", string(limit.Status)),
	).Log(ctx, libLog.LevelInfo, "Limit activated successfully")

	// Record audit event (best-effort)
	if c.auditWriter != nil {
		clientIP := contextutil.GetClientIP(ctx)

		afterState := LimitToMap(limit)
		if err := c.auditWriter.RecordLimitEvent(
			ctx,
			model.AuditEventLimitActivated,
			model.AuditActionActivate,
			limit.ID,
			beforeState,
			afterState,
			"Limit activated via API",
			clientIP,
		); err != nil {
			logger.With(
				libLog.String("operation", "service.limit.activate.audit"),
				libLog.String("limit.id", limit.ID.String()),
				libLog.String("error", err.Error()),
			).Log(ctx, libLog.LevelWarn, "Failed to record audit event")
		}
	}

	return limit, nil
}
