// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package command

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	libCommons "github.com/LerianStudio/lib-commons/v4/commons"
	libLog "github.com/LerianStudio/lib-commons/v4/commons/log"
	libOpentelemetry "github.com/LerianStudio/lib-commons/v4/commons/opentelemetry"
	"github.com/shopspring/decimal"

	"tracer/pkg/clock"
	"tracer/pkg/constant"
	"tracer/pkg/contextutil"
	"tracer/pkg/logging"
	"tracer/pkg/model"
)

// ErrNilLimitRepository is returned when a nil LimitRepository is passed to a limit command constructor.
var ErrNilLimitRepository = errors.New("nil LimitRepository passed to limit command constructor")

// ErrNilClock is returned when a nil Clock is passed to a command constructor.
var ErrNilClock = errors.New("nil Clock passed to command constructor")

// CreateLimitInput defines input for creating a limit.
//
// ARCHITECTURE NOTE: This struct intentionally mirrors in.CreateLimitInput from the HTTP layer.
// The HTTP struct contains validation tags (validate:"...") for request validation,
// while this command struct is a pure DTO without framework dependencies.
// The HTTP layer converts its struct to this one via in.ToCreateLimitServiceInput()
// before calling command handlers. This separation ensures:
//   - HTTP adapter owns request validation (tags, format checks)
//   - Command layer remains framework-agnostic and testable
//   - Domain model (model.NewLimit) performs business validation
type CreateLimitInput struct {
	Name            string
	Description     *string
	LimitType       model.LimitType
	MaxAmount       decimal.Decimal
	Currency        string
	Scopes          []model.Scope
	ActiveTimeStart *model.TimeOfDay
	ActiveTimeEnd   *model.TimeOfDay
	CustomStartDate *string
	CustomEndDate   *string
}

// CreateLimitCommand handles limit creation.
type CreateLimitCommand struct {
	repo        LimitRepository
	clock       clock.Clock
	auditWriter AuditWriter
}

// NewCreateLimitCommand creates a new CreateLimitCommand with dependencies.
// Returns an error if repo is nil to catch invalid dependency injection at construction time.
func NewCreateLimitCommand(repo LimitRepository, clk clock.Clock, auditWriter AuditWriter) (*CreateLimitCommand, error) {
	if repo == nil {
		return nil, ErrNilLimitRepository
	}

	if clk == nil {
		return nil, ErrNilClock
	}

	return &CreateLimitCommand{
		repo:        repo,
		clock:       clk,
		auditWriter: auditWriter,
	}, nil
}

// Execute creates a new limit with validation, tracing, and logging.
func (c *CreateLimitCommand) Execute(ctx context.Context, input *CreateLimitInput) (*model.Limit, error) {
	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "service.limit.create")
	defer span.End()

	logger = logging.WithTrace(ctx, logger)

	// Handle nil input
	if input == nil {
		err := constant.ErrLimitNilInput
		libOpentelemetry.HandleSpanBusinessErrorEvent(span, "Nil input provided", err)

		return nil, err
	}

	// Create normalized copy to avoid mutating caller's input
	normalizedInput := *input
	normalizedInput.Name = strings.TrimSpace(normalizedInput.Name)
	normalizedInput.Currency = strings.ToUpper(strings.TrimSpace(normalizedInput.Currency))

	logger.With(
		libLog.String("operation", "service.limit.create"),
		libLog.Any("limit.name", normalizedInput.Name),
		libLog.String("limit.type", string(normalizedInput.LimitType)),
		libLog.Any("limit.amount", normalizedInput.MaxAmount),
		libLog.Any("limit.currency", normalizedInput.Currency),
	).Log(ctx, libLog.LevelInfo, "Creating limit")

	err := libOpentelemetry.SetSpanAttributesFromValue(span, "create_limit_input", map[string]any{
		"name":       normalizedInput.Name,
		"limit_type": string(normalizedInput.LimitType),
		"max_amount": normalizedInput.MaxAmount,
		"currency":   normalizedInput.Currency,
	}, nil)
	if err != nil {
		libOpentelemetry.HandleSpanError(span, "Failed to set span attributes", err)
	}

	// Create domain entity via appropriate NewLimit* function
	now := c.clock.Now()

	var limit *model.Limit

	// Determine which constructor to use based on provided fields
	hasTimeWindow := normalizedInput.ActiveTimeStart != nil && normalizedInput.ActiveTimeEnd != nil
	hasCustomPeriod := normalizedInput.CustomStartDate != nil && normalizedInput.CustomEndDate != nil

	hasPartialTimeWindow := (normalizedInput.ActiveTimeStart != nil) != (normalizedInput.ActiveTimeEnd != nil)
	if hasPartialTimeWindow {
		libOpentelemetry.HandleSpanBusinessErrorEvent(span, "Partial time window", constant.ErrLimitTimeWindowMismatch)
		return nil, constant.ErrLimitTimeWindowMismatch
	}

	hasPartialCustomPeriod := (normalizedInput.CustomStartDate != nil) != (normalizedInput.CustomEndDate != nil)
	if hasPartialCustomPeriod {
		libOpentelemetry.HandleSpanBusinessErrorEvent(span, "Partial custom period", constant.ErrLimitCustomDatesRequired)
		return nil, constant.ErrLimitCustomDatesRequired
	}

	if hasCustomPeriod {
		// Parse custom period dates
		customStart, parseErr := time.Parse(time.RFC3339, *normalizedInput.CustomStartDate)
		if parseErr != nil {
			libOpentelemetry.HandleSpanBusinessErrorEvent(span, "Failed to parse customStartDate", constant.ErrLimitInvalidCustomStartFormat)
			return nil, fmt.Errorf("%w: %w", constant.ErrLimitInvalidCustomStartFormat, parseErr)
		}

		customEnd, parseErr := time.Parse(time.RFC3339, *normalizedInput.CustomEndDate)
		if parseErr != nil {
			libOpentelemetry.HandleSpanBusinessErrorEvent(span, "Failed to parse customEndDate", constant.ErrLimitInvalidCustomEndFormat)
			return nil, fmt.Errorf("%w: %w", constant.ErrLimitInvalidCustomEndFormat, parseErr)
		}

		if hasTimeWindow {
			// AC-09: CUSTOM period combined with time window
			limit, err = model.NewLimitWithCustomPeriodAndTimeWindow(
				normalizedInput.Name,
				normalizedInput.LimitType,
				normalizedInput.MaxAmount,
				normalizedInput.Currency,
				normalizedInput.Scopes,
				normalizedInput.Description,
				customStart,
				customEnd,
				normalizedInput.ActiveTimeStart.String(),
				normalizedInput.ActiveTimeEnd.String(),
				now,
			)
		} else {
			limit, err = model.NewLimitWithCustomPeriod(
				normalizedInput.Name,
				normalizedInput.LimitType,
				normalizedInput.MaxAmount,
				normalizedInput.Currency,
				normalizedInput.Scopes,
				normalizedInput.Description,
				customStart,
				customEnd,
				now,
			)
		}
	} else if hasTimeWindow {
		// Create limit with time window
		limit, err = model.NewLimitWithTimeWindow(
			normalizedInput.Name,
			normalizedInput.LimitType,
			normalizedInput.MaxAmount,
			normalizedInput.Currency,
			normalizedInput.Scopes,
			normalizedInput.Description,
			normalizedInput.ActiveTimeStart.String(),
			normalizedInput.ActiveTimeEnd.String(),
			now,
		)
	} else {
		// Standard limit (no time window, no custom period)
		limit, err = model.NewLimit(
			normalizedInput.Name,
			normalizedInput.LimitType,
			normalizedInput.MaxAmount,
			normalizedInput.Currency,
			normalizedInput.Scopes,
			normalizedInput.Description,
			now,
		)
	}

	if err != nil {
		libOpentelemetry.HandleSpanBusinessErrorEvent(span, "Failed to create limit entity", err)
		logger.With(
			libLog.String("operation", "service.limit.create"),
			libLog.String("error.message", err.Error()),
		).Log(ctx, libLog.LevelWarn, "Failed to create limit entity")

		return nil, err
	}

	// Check for context cancellation before repository call
	if err := ctx.Err(); err != nil {
		libOpentelemetry.HandleSpanError(span, "Context canceled before persist", err)
		logger.With(
			libLog.String("operation", "service.limit.create"),
			libLog.String("error.message", err.Error()),
		).Log(ctx, libLog.LevelWarn, "Context canceled before persisting limit")

		return nil, err
	}

	// Persist to repository
	if err := c.repo.Create(ctx, limit); err != nil {
		libOpentelemetry.HandleSpanError(span, "Failed to persist limit", err)
		logger.With(
			libLog.String("operation", "service.limit.create"),
			libLog.String("error.message", err.Error()),
		).Log(ctx, libLog.LevelError, "Failed to persist limit")

		return nil, err
	}

	logger.With(
		libLog.String("operation", "service.limit.create"),
		libLog.String("limit.id", limit.ID.String()),
		libLog.Any("limit.name", limit.Name),
		libLog.String("limit.status", string(limit.Status)),
	).Log(ctx, libLog.LevelInfo, "Limit created successfully")

	// Record audit event (best-effort)
	if c.auditWriter != nil {
		clientIP := contextutil.GetClientIP(ctx)

		after := LimitToMap(limit)
		if err := c.auditWriter.RecordLimitEvent(
			ctx,
			model.AuditEventLimitCreated,
			model.AuditActionCreate,
			limit.ID,
			nil,
			after,
			"Limit created via API",
			clientIP,
		); err != nil {
			logger.With(
				libLog.String("operation", "service.limit.create.audit"),
				libLog.String("limit.id", limit.ID.String()),
				libLog.String("error", err.Error()),
			).Log(ctx, libLog.LevelWarn, "Failed to record audit event")
		}
	}

	return limit, nil
}
