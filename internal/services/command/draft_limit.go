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

const opDraftLimit = "service.limit.draft"

// DraftLimitCommand handles limit draft transition (INACTIVE → DRAFT).
type DraftLimitCommand struct {
	repo        LimitRepository
	clock       clock.Clock
	auditWriter AuditWriter
}

// NewDraftLimitCommand creates a new DraftLimitCommand with dependencies.
func NewDraftLimitCommand(repo LimitRepository, clk clock.Clock, auditWriter AuditWriter) *DraftLimitCommand {
	return &DraftLimitCommand{
		repo:        repo,
		clock:       clk,
		auditWriter: auditWriter,
	}
}

// Execute transitions a limit to DRAFT status.
// Idempotent: if already DRAFT, returns the limit without error.
// Returns error if limit is not found or transition is invalid (e.g., from ACTIVE or DELETED).
func (c *DraftLimitCommand) Execute(ctx context.Context, id uuid.UUID) (*model.Limit, error) {
	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, opDraftLimit)
	defer span.End()

	logger = logging.WithTrace(ctx, logger)

	// Check context cancellation first
	if ctx.Err() != nil {
		libOpentelemetry.HandleSpanError(&span, "Context cancelled", ctx.Err())
		logger.WithFields(
			"operation", opDraftLimit,
		).Warn("Context cancelled")

		return nil, ctx.Err()
	}

	// Validate input
	if id == uuid.Nil {
		libOpentelemetry.HandleSpanBusinessErrorEvent(&span, "Invalid input: nil UUID", constant.ErrLimitInvalidID)
		logger.WithFields(
			"operation", opDraftLimit,
		).Warn("Invalid input: nil UUID")

		return nil, constant.ErrLimitInvalidID
	}

	_ = libOpentelemetry.SetSpanAttributesFromStruct(&span, "draft_input", map[string]any{
		"limit_id":  id.String(),
		"operation": "draft",
	})

	logger.WithFields(
		"operation", opDraftLimit,
		"limit.id", id.String(),
	).Info("Transitioning limit to draft")

	limit, err := c.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, constant.ErrLimitNotFound) {
			libOpentelemetry.HandleSpanBusinessErrorEvent(&span, "Limit not found", err)
			logger.WithFields(
				"operation", opDraftLimit,
				"limit.id", id.String(),
			).Warn("Limit not found")

			return nil, err
		}

		libOpentelemetry.HandleSpanError(&span, "Failed to get limit from repository", err)
		logger.WithFields(
			"operation", opDraftLimit,
			"limit.id", id.String(),
			"error.message", err.Error(),
		).Error("Failed to get limit")

		return nil, fmt.Errorf("failed to get limit: %w", err)
	}

	// Defensive check: treat nil limit as not found (guards against repo returning nil, nil)
	if limit == nil {
		libOpentelemetry.HandleSpanBusinessErrorEvent(&span, "Limit not found", constant.ErrLimitNotFound)
		logger.WithFields(
			"operation", opDraftLimit,
			"limit.id", id.String(),
		).Warn("Limit not found")

		return nil, constant.ErrLimitNotFound
	}

	// Idempotency: if already draft, return the limit (no-op)
	if limit.Status == model.LimitStatusDraft {
		logger.WithFields(
			"operation", opDraftLimit,
			"limit.id", id.String(),
		).Info("Limit already in draft (idempotent no-op)")

		return limit, nil
	}

	// Capture "before" state for audit (after idempotency check to avoid unnecessary work)
	beforeState := LimitToMap(limit)

	// Capture original status before mutation for accurate logging
	originalStatus := limit.Status

	// Use domain model's SetStatus for transition validation
	if err := limit.SetStatus(model.LimitStatusDraft, c.clock.Now()); err != nil {
		libOpentelemetry.HandleSpanBusinessErrorEvent(&span, "Invalid state transition", err)
		logger.WithFields(
			"operation", opDraftLimit,
			"limit.id", id.String(),
			"limit.status_from", string(originalStatus),
			"limit.status_to", "DRAFT",
		).Warn("Invalid transition")

		return nil, libCommons.ValidateBusinessError(constant.ErrLimitInvalidStatusChange, err.Error())
	}

	if err := c.repo.UpdateStatus(ctx, id, model.LimitStatusDraft, limit.UpdatedAt); err != nil {
		libOpentelemetry.HandleSpanError(&span, "Failed to update limit status", err)
		logger.WithFields(
			"operation", opDraftLimit,
			"limit.id", id.String(),
			"error.message", err.Error(),
		).Error("Failed to update limit status")

		return nil, fmt.Errorf("failed to update limit status: %w", err)
	}

	logger.WithFields(
		"operation", opDraftLimit,
		"limit.id", id.String(),
		"limit.status", string(limit.Status),
	).Info("Limit transitioned to draft successfully")

	// Record audit event (best-effort)
	if c.auditWriter != nil {
		clientIP := contextutil.GetClientIP(ctx)

		afterState := LimitToMap(limit)
		if err := c.auditWriter.RecordLimitEvent(
			ctx,
			model.AuditEventLimitDrafted,
			model.AuditActionDraft,
			limit.ID,
			beforeState,
			afterState,
			"Limit transitioned to draft via API",
			clientIP,
		); err != nil {
			logger.WithFields(
				"operation", opDraftLimit+".audit",
				"limit.id", limit.ID.String(),
				"error", err.Error(),
			).Warn("Failed to record audit event")
		}
	}

	return limit, nil
}
