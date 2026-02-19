// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package query

//go:generate mockgen -source=get_active_rules.go -destination=get_active_rules_mock.go -package=query

import (
	"context"
	"errors"
	"fmt"

	libCommons "github.com/LerianStudio/lib-commons/v2/commons"
	libOpentelemetry "github.com/LerianStudio/lib-commons/v2/commons/opentelemetry"

	"tracer/pkg/logging"
	"tracer/pkg/model"
)

// ErrNilQuery is returned when Execute is called on a nil query receiver.
var ErrNilQuery = errors.New("query is nil")

// ErrNilActiveRulesRepository is returned when the ActiveRulesRepository is nil.
var ErrNilActiveRulesRepository = errors.New("active rules repository is nil")

// ActiveRulesRepository defines the interface for loading active rules.
// Interface defined in the package that USES it (per PROJECT_RULES.md).
// Supports optional scope filtering for performance optimization.
// Implementations MAY return a superset of matching rules; callers MUST apply their own scope filtering.
type ActiveRulesRepository interface {
	GetActiveRules(ctx context.Context, txScope *model.Scope) ([]*model.Rule, error)
}

// GetActiveRulesQuery handles retrieving all active rules for evaluation.
type GetActiveRulesQuery struct {
	repo ActiveRulesRepository
}

// NewGetActiveRulesQuery creates a new GetActiveRulesQuery instance.
// Returns ErrNilActiveRulesRepository if the repository is nil.
func NewGetActiveRulesQuery(repo ActiveRulesRepository) (*GetActiveRulesQuery, error) {
	if repo == nil {
		return nil, ErrNilActiveRulesRepository
	}

	return &GetActiveRulesQuery{
		repo: repo,
	}, nil
}

// Execute retrieves all active rules for evaluation.
// If txScope is provided, only returns rules matching that scope (database-level filtering).
// If txScope is nil, returns all active rules (global).
// Returns ErrNilQuery if the query is nil, or ErrNilActiveRulesRepository if the repository is nil.
func (q *GetActiveRulesQuery) Execute(ctx context.Context, txScope *model.Scope) ([]*model.Rule, error) {
	if q == nil {
		return nil, ErrNilQuery
	}

	if q.repo == nil {
		return nil, ErrNilActiveRulesRepository
	}

	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "service.rules.get_active")
	defer span.End()

	logger = logging.WithTrace(ctx, logger)

	logger.WithFields(
		"operation", "service.rules.get_active",
		"scope.provided", txScope != nil,
	).Info("Getting active rules")

	rules, err := q.repo.GetActiveRules(ctx, txScope)
	if err != nil {
		libOpentelemetry.HandleSpanError(&span, "Failed to get active rules", err)

		logger.WithFields(
			"operation", "service.rules.get_active",
			"error.message", err.Error(),
		).Error("Failed to get active rules")

		return nil, fmt.Errorf("failed to get active rules: %w", err)
	}

	err = libOpentelemetry.SetSpanAttributesFromStruct(&span, "result", map[string]any{
		"rules.count": len(rules),
	})
	if err != nil {
		libOpentelemetry.HandleSpanError(&span, "Failed to set span attributes", err)
	}

	logger.WithFields(
		"operation", "service.rules.get_active",
		"rules.count", len(rules),
	).Info("Active rules retrieved successfully")

	// Normalize nil to empty slice for consistent behavior
	if rules == nil {
		rules = []*model.Rule{}
	}

	return rules, nil
}
