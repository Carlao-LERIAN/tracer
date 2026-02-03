// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package query

//go:generate mockgen -source=list_rules.go -destination=list_rules_repository_mock.go -package=query

import (
	"context"
	"strings"

	libCommons "github.com/LerianStudio/lib-commons/v2/commons"
	libOpentelemetry "github.com/LerianStudio/lib-commons/v2/commons/opentelemetry"

	"tracer/pkg/constant"
	"tracer/pkg/logging"
	"tracer/pkg/model"
)

// ListRulesRepository defines the interface for listing rules.
type ListRulesRepository interface {
	List(ctx context.Context, filter *model.ListRulesFilter) (*model.ListRulesResult, error)
}

// ListRulesQuery handles listing rules with pagination and filtering.
type ListRulesQuery struct {
	repo ListRulesRepository
}

// NewListRulesQuery creates a new ListRulesQuery instance.
func NewListRulesQuery(repo ListRulesRepository) *ListRulesQuery {
	return &ListRulesQuery{
		repo: repo,
	}
}

// Execute lists rules with cursor-based pagination and filtering.
func (q *ListRulesQuery) Execute(ctx context.Context, filter *model.ListRulesFilter) (*model.ListRulesResult, error) {
	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "service.rule.list")
	defer span.End()

	logger = logging.WithTrace(ctx, logger)

	// Create a copy to avoid mutating caller's filter; use empty filter if nil
	var normalizedFilter model.ListRulesFilter
	if filter != nil {
		normalizedFilter = *filter
	}

	// Apply defaults
	if normalizedFilter.Limit == 0 {
		normalizedFilter.Limit = constant.DefaultPaginationLimit
	}

	// SortBy should be camelCase (API contract); repository converts to snake_case
	if normalizedFilter.SortBy == "" {
		normalizedFilter.SortBy = "createdAt"
	}

	if normalizedFilter.SortOrder == "" {
		normalizedFilter.SortOrder = "DESC"
	} else {
		normalizedFilter.SortOrder = strings.ToUpper(normalizedFilter.SortOrder)
	}

	logger.WithFields(
		"operation", "service.rule.list",
		"list.limit", normalizedFilter.Limit,
		"list.cursor", normalizedFilter.Cursor,
		"list.sort_by", normalizedFilter.SortBy,
		"list.sort_order", normalizedFilter.SortOrder,
	).Info("Listing rules")

	result, err := q.repo.List(ctx, &normalizedFilter)
	if err != nil {
		libOpentelemetry.HandleSpanError(&span, "Failed to list rules", err)
		return nil, err
	}

	err = libOpentelemetry.SetSpanAttributesFromStruct(&span, "list_result", map[string]any{
		"rules_count": len(result.Rules),
		"has_more":    result.HasMore,
	})
	if err != nil {
		libOpentelemetry.HandleSpanError(&span, "Failed to set span attributes", err)
	}

	logger.WithFields(
		"operation", "service.rule.list",
		"list.count", len(result.Rules),
		"list.has_more", result.HasMore,
	).Info("Rules listed")

	return result, nil
}
