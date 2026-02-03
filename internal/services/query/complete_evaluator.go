// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package query

//go:generate mockgen -source=complete_evaluator.go -destination=complete_evaluator_mock.go -package=query

import (
	"context"
	"errors"
	"fmt"

	libCommons "github.com/LerianStudio/lib-commons/v2/commons"
	libOpentelemetry "github.com/LerianStudio/lib-commons/v2/commons/opentelemetry"
	"github.com/google/uuid"

	"tracer/pkg/logging"
	"tracer/pkg/model"
)

// ErrNilSingleRuleEvaluator is returned when NewCompleteEvaluator is called with a nil SingleRuleEvaluator.
var ErrNilSingleRuleEvaluator = errors.New("single rule evaluator is nil")

// ErrNilRequest is returned when EvaluateAll is called with a nil request.
var ErrNilRequest = errors.New("request cannot be nil")

// SingleRuleEvaluator evaluates a single rule against a validation request.
// Interface defined in the package that USES it (per PROJECT_RULES.md).
type SingleRuleEvaluator interface {
	Evaluate(ctx context.Context, rule *model.Rule, req *model.ValidationRequest) (bool, error)
}

// EvaluationCollector holds categorized rule matches from complete evaluation.
// All rules are evaluated without short-circuiting, and results are grouped by action type.
type EvaluationCollector struct {
	DenyRuleIDs      []uuid.UUID
	AllowRuleIDs     []uuid.UUID
	ReviewRuleIDs    []uuid.UUID
	EvaluatedRuleIDs []uuid.UUID
}

// CompleteEvaluator evaluates ALL rules against a validation request without short-circuiting.
// Results are categorized by action type (DENY, REVIEW, ALLOW).
type CompleteEvaluator struct {
	ruleEval SingleRuleEvaluator
}

// NewCompleteEvaluator creates a new CompleteEvaluator instance.
// Returns ErrNilSingleRuleEvaluator if the ruleEval is nil.
func NewCompleteEvaluator(ruleEval SingleRuleEvaluator) (*CompleteEvaluator, error) {
	if ruleEval == nil {
		return nil, ErrNilSingleRuleEvaluator
	}

	return &CompleteEvaluator{
		ruleEval: ruleEval,
	}, nil
}

// EvaluateAll evaluates ALL rules against the validation request, categorizing by action type.
// Does NOT short-circuit - all rules are evaluated regardless of matches found.
// Returns an EvaluationCollector with rules grouped by their action type.
//
// Telemetry:
// - Span name: "service.rules.evaluate_all"
// - Attributes: rules.evaluated_count, rules.deny_count, rules.allow_count, rules.review_count
func (e *CompleteEvaluator) EvaluateAll(
	ctx context.Context,
	rules []*model.Rule,
	req *model.ValidationRequest,
) (*EvaluationCollector, error) {
	// 1. Get logger and tracer from context
	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	// 2. Start span "service.rules.evaluate_all"
	ctx, span := tracer.Start(ctx, "service.rules.evaluate_all")
	defer span.End()

	// Enrich logger with trace context
	logger = logging.WithTrace(ctx, logger)

	// Validate request is not nil
	if req == nil {
		libOpentelemetry.HandleSpanError(&span, "Nil request", ErrNilRequest)

		logger.WithFields(
			"operation", "service.rules.evaluate_all",
			"error.message", ErrNilRequest.Error(),
		).Error("Request validation failed")

		return nil, ErrNilRequest
	}

	logger.WithFields(
		"operation", "service.rules.evaluate_all",
		"rules.total_count", len(rules),
	).Info("Evaluating all rules")

	// 3. Initialize collector with pre-allocated slices for better performance
	// EvaluatedRuleIDs will contain all rules, others estimated at ~25% match rate
	rulesCount := len(rules)

	estimatedMatches := rulesCount / 4
	if estimatedMatches < 1 {
		estimatedMatches = 1
	}

	collector := &EvaluationCollector{
		DenyRuleIDs:      make([]uuid.UUID, 0, estimatedMatches),
		AllowRuleIDs:     make([]uuid.UUID, 0, estimatedMatches),
		ReviewRuleIDs:    make([]uuid.UUID, 0, estimatedMatches),
		EvaluatedRuleIDs: make([]uuid.UUID, 0, rulesCount),
	}

	// 4. For each rule, evaluate and categorize
	for _, rule := range rules {
		// Nil guard - skip nil rules to avoid panics
		if rule == nil {
			logger.WithFields(
				"operation", "service.rules.evaluate_all",
			).Warn("Nil rule encountered in rules slice, skipping")

			continue
		}

		// a. Check context cancellation
		select {
		case <-ctx.Done():
			libOpentelemetry.HandleSpanError(&span, "Context cancelled during evaluation", ctx.Err())

			logger.WithFields(
				"operation", "service.rules.evaluate_all",
				"error.message", ctx.Err().Error(),
			).Error("Context cancelled during rule evaluation")

			return nil, fmt.Errorf("context cancelled during evaluation: %w", ctx.Err())
		default:
			// Continue processing
		}

		// b. Call ruleEval.Evaluate(ctx, rule, req)
		matched, err := e.ruleEval.Evaluate(ctx, rule, req)
		if err != nil {
			// e. If error, handle with telemetry and return
			libOpentelemetry.HandleSpanError(&span, "Failed to evaluate rule", err)

			logger.WithFields(
				"operation", "service.rules.evaluate_all",
				"rule.id", rule.ID.String(),
				"error.message", err.Error(),
			).Error("Failed to evaluate rule")

			return nil, fmt.Errorf("failed to evaluate rule %s: %w", rule.ID.String(), err)
		}

		// c. Track in EvaluatedRuleIDs
		collector.EvaluatedRuleIDs = append(collector.EvaluatedRuleIDs, rule.ID)

		// d. If matched, add to appropriate category (DenyRuleIDs, AllowRuleIDs, ReviewRuleIDs)
		if matched {
			switch rule.Action {
			case model.DecisionDeny:
				collector.DenyRuleIDs = append(collector.DenyRuleIDs, rule.ID)
			case model.DecisionAllow:
				collector.AllowRuleIDs = append(collector.AllowRuleIDs, rule.ID)
			case model.DecisionReview:
				collector.ReviewRuleIDs = append(collector.ReviewRuleIDs, rule.ID)
			default:
				logger.WithFields(
					"operation", "service.rules.evaluate_all",
					"rule.id", rule.ID.String(),
					"rule.action", string(rule.Action),
				).Warn("Unknown rule action type encountered")
			}
		}
	}

	// 5. Set span attributes with counts
	if err := libOpentelemetry.SetSpanAttributesFromStruct(&span, "rules", map[string]any{
		"evaluated_count": len(collector.EvaluatedRuleIDs),
		"deny_count":      len(collector.DenyRuleIDs),
		"allow_count":     len(collector.AllowRuleIDs),
		"review_count":    len(collector.ReviewRuleIDs),
	}); err != nil {
		libOpentelemetry.HandleSpanError(&span, "Failed to set span attributes", err)
	}

	logger.WithFields(
		"operation", "service.rules.evaluate_all",
		"rules.evaluated_count", len(collector.EvaluatedRuleIDs),
		"rules.deny_count", len(collector.DenyRuleIDs),
		"rules.allow_count", len(collector.AllowRuleIDs),
		"rules.review_count", len(collector.ReviewRuleIDs),
	).Info("All rules evaluated successfully")

	// 6. Return collector
	return collector, nil
}
