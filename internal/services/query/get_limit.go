// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package query

import (
	"context"
	"errors"

	libCommons "github.com/LerianStudio/lib-commons/v2/commons"
	libOpentelemetry "github.com/LerianStudio/lib-commons/v2/commons/opentelemetry"
	"github.com/google/uuid"

	"tracer/pkg/constant"
	"tracer/pkg/logging"
	"tracer/pkg/model"
)

// GetLimitQuery handles retrieving a single limit.
type GetLimitQuery struct {
	repo LimitRepository
}

// NewGetLimitQuery creates a new GetLimitQuery with dependencies.
func NewGetLimitQuery(repo LimitRepository) *GetLimitQuery {
	return &GetLimitQuery{repo: repo}
}

// Execute retrieves a limit by ID.
// Returns constant.ErrLimitNotFound if the limit doesn't exist.
func (q *GetLimitQuery) Execute(ctx context.Context, id uuid.UUID) (*model.Limit, error) {
	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "service.limit.get")
	defer span.End()

	logger = logging.WithTrace(ctx, logger)

	// Validate input
	if id == uuid.Nil {
		err := constant.ErrLimitInvalidID
		libOpentelemetry.HandleSpanBusinessErrorEvent(&span, "Invalid limit ID provided", err)

		return nil, err
	}

	logger.WithFields(
		"operation", "service.limit.get",
		"limit.id", id.String(),
	).Info("Getting limit")

	err := libOpentelemetry.SetSpanAttributesFromStruct(&span, "get_limit_input", map[string]any{
		"limit_id": id.String(),
	})
	if err != nil {
		libOpentelemetry.HandleSpanError(&span, "Failed to set span attributes", err)
	}

	// Retrieve from repository
	limit, err := q.repo.GetByID(ctx, id)
	if err != nil {
		// Distinguish business errors (not found) from infrastructure errors
		if errors.Is(err, constant.ErrLimitNotFound) {
			libOpentelemetry.HandleSpanBusinessErrorEvent(&span, "Limit not found", err)
			logger.WithFields(
				"operation", "service.limit.get",
				"limit.id", id.String(),
			).Warn("Limit not found")
		} else {
			libOpentelemetry.HandleSpanError(&span, "Failed to retrieve limit", err)
			logger.WithFields(
				"operation", "service.limit.get",
				"limit.id", id.String(),
				"error.message", err.Error(),
			).Error("Failed to get limit")
		}

		return nil, err
	}

	logger.WithFields(
		"operation", "service.limit.get",
		"limit.id", limit.ID.String(),
		"limit.name", limit.Name,
		"limit.status", string(limit.Status),
	).Info("Limit retrieved successfully")

	return limit, nil
}
