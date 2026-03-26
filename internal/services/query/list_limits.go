// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package query

import (
	"context"
	"errors"

	libCommons "github.com/LerianStudio/lib-commons/v4/commons"
	libLog "github.com/LerianStudio/lib-commons/v4/commons/log"
	libOpentelemetry "github.com/LerianStudio/lib-commons/v4/commons/opentelemetry"

	"tracer/pkg/constant"
	"tracer/pkg/logging"
	"tracer/pkg/model"
)

// ErrNilListLimitsRepository is returned when the LimitRepository is nil.
var ErrNilListLimitsRepository = errors.New("limit repository is nil")

// ListLimitsQuery handles listing limits with filters.
type ListLimitsQuery struct {
	repo LimitRepository
}

// NewListLimitsQuery creates a new ListLimitsQuery with dependencies.
// Returns ErrNilListLimitsRepository if the repository is nil.
func NewListLimitsQuery(repo LimitRepository) (*ListLimitsQuery, error) {
	if repo == nil {
		return nil, ErrNilListLimitsRepository
	}

	return &ListLimitsQuery{repo: repo}, nil
}

// Execute retrieves limits with cursor-based pagination and filtering.
//
// Pagination behavior:
// - Limit defaults to constant.DefaultPaginationLimit (10) if not specified
// - Limit is capped at constant.MaxPaginationLimit (100)
// - Cursor is a base64-encoded cursor for keyset pagination
// - NextCursor is returned when more results exist
// - HasMore indicates if additional pages are available
func (q *ListLimitsQuery) Execute(ctx context.Context, filter *model.ListLimitsFilter) (*model.ListLimitsResult, error) {
	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "service.limit.list")
	defer span.End()

	logger = logging.WithTrace(ctx, logger)

	// Handle nil filter by creating empty filter
	if filter == nil {
		filter = &model.ListLimitsFilter{}
	}

	// Apply defaults first to normalize values (limit, sortBy, sortOrder)
	filter.ApplyDefaults()

	// Extract filter values for span attributes (used in both error and success paths)
	filterStatus := ""
	if filter.Status != nil {
		filterStatus = string(*filter.Status)
	}

	filterLimitType := ""
	if filter.LimitType != nil {
		filterLimitType = string(*filter.LimitType)
	}

	// Validate filter values after defaults are applied
	if err := filter.Validate(); err != nil {
		_ = libOpentelemetry.SetSpanAttributesFromValue(span, "list_limits_filter", map[string]any{
			"limit":           filter.Limit,
			"has_cursor":      filter.Cursor != "",
			"sort_by":         filter.SortBy,
			"sort_order":      filter.SortOrder,
			"status":          filterStatus,
			"limit_type":      filterLimitType,
			"service.success": false,
		}, nil)

		libOpentelemetry.HandleSpanBusinessErrorEvent(span, "Invalid filter", err)
		logger.With(
			libLog.String("operation", "service.limit.list"),
			libLog.String("error.message", err.Error()),
			libLog.Any("filter.status", filterStatus),
			libLog.Any("filter.limit_type", filterLimitType),
			libLog.Any("filter.sort_by", filter.SortBy),
			libLog.Any("filter.sort_order", filter.SortOrder),
		).Log(ctx, libLog.LevelWarn, "Invalid filter provided")

		return nil, err
	}

	logger.With(
		libLog.String("operation", "service.limit.list"),
		libLog.Any("filter.limit", filter.Limit),
		libLog.Any("filter.has_cursor", filter.Cursor != ""),
		libLog.Any("filter.sort_by", filter.SortBy),
		libLog.Any("filter.sort_order", filter.SortOrder),
	).Log(ctx, libLog.LevelInfo, "Listing limits")

	// Check context cancellation before repository call
	if ctx.Err() != nil {
		libOpentelemetry.HandleSpanError(span, "Context cancelled", ctx.Err())
		logger.With(
			libLog.String("operation", "service.limit.list"),
		).Log(ctx, libLog.LevelWarn, "Context cancelled before repository call")

		return nil, ctx.Err()
	}

	// Retrieve from repository
	result, err := q.repo.List(ctx, filter)
	if err != nil {
		// Distinguish business errors (invalid cursor) from infrastructure errors
		if errors.Is(err, constant.ErrInvalidCursor) {
			libOpentelemetry.HandleSpanBusinessErrorEvent(span, "Invalid pagination cursor", err)
			logger.With(
				libLog.String("operation", "service.limit.list"),
				libLog.String("error.message", err.Error()),
			).Log(ctx, libLog.LevelWarn, "Invalid cursor provided")
		} else {
			libOpentelemetry.HandleSpanError(span, "Failed to list limits", err)
			logger.With(
				libLog.String("operation", "service.limit.list"),
				libLog.String("error.message", err.Error()),
			).Log(ctx, libLog.LevelError, "Failed to list limits")
		}

		return nil, err
	}

	// Set span attributes after successful repository call
	err = libOpentelemetry.SetSpanAttributesFromValue(span, "list_limits_filter", map[string]any{
		"limit":           filter.Limit,
		"has_cursor":      filter.Cursor != "",
		"sort_by":         filter.SortBy,
		"sort_order":      filter.SortOrder,
		"status":          filterStatus,
		"limit_type":      filterLimitType,
		"service.success": true,
	}, nil)
	if err != nil {
		libOpentelemetry.HandleSpanError(span, "Failed to set span attributes", err)
	}

	logger.With(
		libLog.String("operation", "service.limit.list"),
		libLog.Int("result.count", len(result.Limits)),
		libLog.Any("result.has_more", result.HasMore),
	).Log(ctx, libLog.LevelInfo, "Limits listed successfully")

	// Add span attributes for result (consistent with list_rules.go)
	err = libOpentelemetry.SetSpanAttributesFromValue(span, "list_limits_result", map[string]any{
		"limits_count": len(result.Limits),
		"has_more":     result.HasMore,
		"has_cursor":   result.NextCursor != "",
	}, nil)
	if err != nil {
		libOpentelemetry.HandleSpanError(span, "Failed to set result span attributes", err)
	}

	return result, nil
}
