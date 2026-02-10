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
		libOpentelemetry.HandleSpanError(&span, "Context cancelled", ctx.Err())
		logger.WithFields(
			"operation", "service.limit.activate",
			"limit.id", id.String(),
		).Warn("Context cancelled")

		return nil, ctx.Err()
	}

	// Validate input
	if id == uuid.Nil {
		libOpentelemetry.HandleSpanBusinessErrorEvent(&span, "Invalid input: nil UUID", constant.ErrLimitInvalidID)
		logger.WithFields(
			"operation", "service.limit.activate",
		).Warn("Invalid input: nil UUID")

		return nil, constant.ErrLimitInvalidID
	}

	_ = libOpentelemetry.SetSpanAttributesFromStruct(&span, "activate_input", map[string]any{
		"limit_id":  id.String(),
		"operation": "activate",
	})

	logger.WithFields(
		"operation", "service.limit.activate",
		"limit.id", id.String(),
	).Info("Activating limit")

	limit, err := c.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, constant.ErrLimitNotFound) {
			libOpentelemetry.HandleSpanBusinessErrorEvent(&span, "Limit not found", err)
			logger.WithFields(
				"operation", "service.limit.activate",
				"limit.id", id.String(),
			).Warn("Limit not found")

			return nil, err
		}

		libOpentelemetry.HandleSpanError(&span, "Failed to get limit from repository", err)
		logger.WithFields(
			"operation", "service.limit.activate",
			"limit.id", id.String(),
			"error.message", err.Error(),
		).Error("Failed to get limit")

		return nil, fmt.Errorf("failed to get limit: %w", err)
	}

	// Defensive check: treat nil limit as not found (guards against repo returning nil, nil)
	if limit == nil {
		libOpentelemetry.HandleSpanBusinessErrorEvent(&span, "Limit not found", constant.ErrLimitNotFound)
		logger.WithFields(
			"operation", "service.limit.activate",
			"limit.id", id.String(),
		).Warn("Limit not found")

		return nil, constant.ErrLimitNotFound
	}

	// Idempotency: if already active, return the limit (no-op)
	if limit.Status == model.LimitStatusActive {
		logger.WithFields(
			"operation", "service.limit.activate",
			"limit.id", id.String(),
		).Info("Limit already active (idempotent no-op)")

		return limit, nil
	}

	// Capture "before" state for audit (after idempotency check, before mutation)
	beforeState := LimitToMap(limit)

	// Capture original status before mutation for accurate logging
	originalStatus := limit.Status

	// Use domain model's SetStatus for transition validation
	if err := limit.SetStatus(model.LimitStatusActive, c.clock.Now()); err != nil {
		libOpentelemetry.HandleSpanBusinessErrorEvent(&span, "Invalid state transition", err)
		logger.WithFields(
			"operation", "service.limit.activate",
			"limit.id", id.String(),
			"limit.status_from", string(originalStatus),
			"limit.status_to", "ACTIVE",
		).Warn("Invalid transition")

		return nil, libCommons.ValidateBusinessError(constant.ErrLimitInvalidStatusChange, err.Error())
	}

	if err := c.repo.UpdateStatus(ctx, id, model.LimitStatusActive, limit.UpdatedAt); err != nil {
		libOpentelemetry.HandleSpanError(&span, "Failed to update limit status", err)
		logger.WithFields(
			"operation", "service.limit.activate",
			"limit.id", id.String(),
			"error.message", err.Error(),
		).Error("Failed to update limit status")

		return nil, fmt.Errorf("failed to update limit status: %w", err)
	}

	logger.WithFields(
		"operation", "service.limit.activate",
		"limit.id", id.String(),
		"limit.status", string(limit.Status),
	).Info("Limit activated successfully")

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
			logger.WithFields(
				"operation", "service.limit.activate.audit",
				"limit.id", limit.ID.String(),
				"error", err.Error(),
			).Warn("Failed to record audit event")
		}
	}

	return limit, nil
}
