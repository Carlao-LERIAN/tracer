// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package query

//go:generate mockgen -source=limit_checker.go -destination=limit_checker_mock.go -package=query

import (
	"context"
	"fmt"
	"math"
	"strings"

	libCommons "github.com/LerianStudio/lib-commons/v2/commons"
	libOtel "github.com/LerianStudio/lib-commons/v2/commons/opentelemetry"
	"github.com/google/uuid"

	"tracer/pkg/constant"
	"tracer/pkg/logging"
	"tracer/pkg/model"
)

// LimitChecker defines the interface for checking limits against transactions.
type LimitChecker interface {
	// CheckLimits evaluates all applicable limits for a transaction.
	// Returns CheckLimitsOutput with:
	//   - Allowed: true if no limits exceeded (or no active limits found)
	//   - ExceededLimitIDs: IDs of limits that would be exceeded
	//   - LimitUsageDetails: usage information for all checked limits
	//
	// For DAILY/MONTHLY limits: increments usage counters atomically
	// For PER_TRANSACTION limits: checks maxAmount directly without persistent counters
	CheckLimits(ctx context.Context, input *model.CheckLimitsInput) (*model.CheckLimitsOutput, error)

	// RollbackUsage decrements usage counters for limits that were previously incremented.
	// Called when a transaction is denied by rules after limits were already incremented.
	// Only affects DAILY/MONTHLY limits (PER_TRANSACTION has no persistent counters).
	// usageDetails contains the limits to rollback (typically from a previous CheckLimits call).
	RollbackUsage(ctx context.Context, input *model.CheckLimitsInput, usageDetails []model.LimitUsageDetail) error
}

// LimitCheckerService implements LimitChecker using repository pattern.
type LimitCheckerService struct {
	limitRepo        LimitRepository
	usageCounterRepo UsageCounterRepository
}

// limitCheckResult holds the result of checking a single limit.
// Used internally for two-phase check-then-increment logic.
type limitCheckResult struct {
	detail    *model.LimitUsageDetail
	exceeded  bool
	counterID *uuid.UUID // nil for PER_TRANSACTION limits
}

// NewLimitChecker creates a new LimitCheckerService.
// Returns error if either repository is nil.
func NewLimitChecker(limitRepo LimitRepository, usageCounterRepo UsageCounterRepository) (*LimitCheckerService, error) {
	if limitRepo == nil {
		return nil, constant.ErrLimitCheckerNilLimitRepo
	}

	if usageCounterRepo == nil {
		return nil, constant.ErrLimitCheckerNilUsageCounterRepo
	}

	return &LimitCheckerService{
		limitRepo:        limitRepo,
		usageCounterRepo: usageCounterRepo,
	}, nil
}

// CheckLimits evaluates all applicable limits for a transaction.
func (s *LimitCheckerService) CheckLimits(ctx context.Context, input *model.CheckLimitsInput) (*model.CheckLimitsOutput, error) {
	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "service.limit_checker.check_limits")
	defer span.End()

	logger = logging.WithTrace(ctx, logger)

	if input == nil {
		libOtel.HandleSpanBusinessErrorEvent(&span, "Nil input", constant.ErrCheckLimitsNilInput)
		return nil, constant.ErrCheckLimitsNilInput
	}

	if err := input.Validate(); err != nil {
		libOtel.HandleSpanBusinessErrorEvent(&span, "Invalid input", err)
		return nil, err
	}

	if err := libOtel.SetSpanAttributesFromStruct(&span, "input", input); err != nil {
		span.RecordError(err)

		logger.WithFields(
			"operation", "service.limit_checker.check_limits",
			"error", err.Error(),
		).Warn("Failed to set span attributes for input")
	}

	// Get applicable limits (active limits matching currency and scopes)
	limits, err := s.getApplicableLimits(ctx, input)
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to get applicable limits", err)
		return nil, err
	}

	if len(limits) == 0 {
		logger.WithFields(
			"operation", "service.limit_checker.check_limits",
			"currency", input.Currency,
		).Info("No active limits found for criteria")

		output := model.NewCheckLimitsOutput(true)

		return output, nil
	}

	logger.WithFields(
		"operation", "service.limit_checker.check_limits",
		"applicable_limits_count", len(limits),
	).Info("Found applicable limits")

	// Phase 1: Check all limits WITHOUT incrementing counters
	// This prevents partial increments if a later limit would be exceeded
	checkResults := make([]limitCheckResult, 0, len(limits))
	exceededIDs := make([]uuid.UUID, 0, len(limits))

	for i := range limits {
		limit := &limits[i]

		result, err := s.checkSingleLimitWithoutIncrement(ctx, limit, input)
		if err != nil {
			libOtel.HandleSpanError(&span, "Failed to check limit", err)
			return nil, err
		}

		checkResults = append(checkResults, *result)

		if result.exceeded {
			exceededIDs = append(exceededIDs, limit.ID)
		}
	}

	// Build usage details from check results
	usageDetails := make([]model.LimitUsageDetail, 0, len(checkResults))
	for _, result := range checkResults {
		usageDetails = append(usageDetails, *result.detail)
	}

	output := model.NewCheckLimitsOutput(true).WithLimitUsageDetails(usageDetails)

	if len(exceededIDs) > 0 {
		// Some limits exceeded - do NOT increment any counters
		output = output.WithExceededLimits(exceededIDs)
		output.Allowed = false

		logger.WithFields(
			"operation", "service.limit_checker.check_limits",
			"exceeded_count", len(exceededIDs),
			"exceeded_limit_ids", exceededIDs,
		).Info("Limits exceeded")
	} else {
		// Phase 2: All limits passed - now increment all counters
		if err := s.incrementAllCounters(ctx, checkResults, input.Amount); err != nil {
			libOtel.HandleSpanError(&span, "Failed to increment counters", err)
			return nil, err
		}

		logger.WithFields(
			"operation", "service.limit_checker.check_limits",
			"checked_count", len(usageDetails),
		).Info("All limits passed")
	}

	return output, nil
}

// RollbackUsage decrements usage counters for limits that were previously incremented.
//
// EVENTUAL CONSISTENCY DESIGN:
// This method intentionally returns nil even when some rollbacks fail. This is by design:
//   - The primary operation (transaction denial) has already succeeded
//   - Failing the rollback should not cause the overall denial to fail
//   - Usage counters self-correct at period boundaries (daily/monthly resets)
//   - Slightly over-counted limits are acceptable and conservative (may deny borderline transactions)
//
// Observability for failed rollbacks:
//   - Span event recorded via libOtel.HandleSpanBusinessErrorEvent (for tracing)
//   - Metric emitted via MetricRollbackFailures (for alerting dashboards)
//   - Structured logging with failed limit IDs (for investigation)
//
// Operators should set up alerts on tracer_limit_rollback_failures_total > 0 to investigate
// persistent rollback failures that may indicate infrastructure issues.
func (s *LimitCheckerService) RollbackUsage(ctx context.Context, input *model.CheckLimitsInput, usageDetails []model.LimitUsageDetail) error {
	logger, tracer, _, metricsFactory := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "service.limit_checker.rollback_usage")
	defer span.End()

	logger = logging.WithTrace(ctx, logger)

	if input == nil {
		libOtel.HandleSpanBusinessErrorEvent(&span, "Invalid input", constant.ErrCheckLimitsNilInput)
		return constant.ErrCheckLimitsNilInput
	}

	if len(usageDetails) == 0 {
		logger.WithFields(
			"operation", "service.limit_checker.rollback_usage",
		).Info("No usage details to rollback")

		return nil
	}

	logger.WithFields(
		"operation", "service.limit_checker.rollback_usage",
		"details_count", len(usageDetails),
	).Info("Rolling back usage")

	// Track limits that failed to rollback for alerting
	// Note: PER_TRANSACTION limits don't have persistent counters and are skipped
	var failedLimits []uuid.UUID

	for _, detail := range usageDetails {
		// PER_TRANSACTION limits don't have persistent counters - skip without DB lookup
		// InternalLimitType is now included in LimitUsageDetail to avoid N+1 queries
		if detail.InternalLimitType == model.LimitTypePerTransaction {
			continue
		}

		// Calculate scope key using transaction context from input
		txScope := buildTransactionScope(input)
		scopeKey := model.CalculateScopeKey(txScope)

		periodKey, err := model.CalculatePeriodKey(detail.InternalLimitType, input.TransactionTimestamp)
		if err != nil {
			logger.WithFields(
				"operation", "service.limit_checker.rollback_usage",
				"limit_id", detail.LimitID.String(),
				"error", err.Error(),
			).Warn("Failed to calculate period key for rollback, skipping")

			failedLimits = append(failedLimits, detail.LimitID)

			continue
		}

		// Get existing counter (do NOT create one - rollback should only affect existing counters)
		counter, err := s.usageCounterRepo.GetForUpdate(ctx, detail.LimitID, scopeKey, periodKey)
		if err != nil {
			logger.WithFields(
				"operation", "service.limit_checker.rollback_usage",
				"limit_id", detail.LimitID.String(),
				"error", err.Error(),
			).Warn("Failed to get counter for rollback, skipping")

			failedLimits = append(failedLimits, detail.LimitID)

			continue
		}

		if err := s.usageCounterRepo.DecrementAtomic(ctx, counter.ID, input.Amount); err != nil {
			logger.WithFields(
				"operation", "service.limit_checker.rollback_usage",
				"limit_id", detail.LimitID.String(),
				"counter_id", counter.ID.String(),
				"amount", input.Amount,
				"error", err.Error(),
			).Warn("Failed to decrement counter for rollback, skipping")

			failedLimits = append(failedLimits, detail.LimitID)

			continue
		}

		logger.WithFields(
			"operation", "service.limit_checker.rollback_usage",
			"limit_id", detail.LimitID.String(),
			"counter_id", counter.ID.String(),
			"amount", input.Amount,
		).Info("Rolled back usage for limit")
	}

	if len(failedLimits) > 0 {
		libOtel.HandleSpanBusinessErrorEvent(&span, "Some limits failed to rollback", fmt.Errorf("failed limits: %v", failedLimits))

		// Emit metric for alerting (see EVENTUAL CONSISTENCY DESIGN comment above)
		if metricsFactory != nil {
			metricsFactory.Counter(MetricRollbackFailures).Add(ctx, int64(len(failedLimits)))
		}

		logger.WithFields(
			"operation", "service.limit_checker.rollback_usage",
			"failed_count", len(failedLimits),
			"failed_limit_ids", failedLimits,
		).Warn("Some limits failed to rollback")
	}

	return nil
}

// getApplicableLimits fetches active limits matching currency and scopes.
// Handles pagination to retrieve all matching limits beyond MaxPaginationLimit.
func (s *LimitCheckerService) getApplicableLimits(ctx context.Context, input *model.CheckLimitsInput) ([]model.Limit, error) {
	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "service.limit_checker.get_applicable_limits")
	defer span.End()

	logger = logging.WithTrace(ctx, logger)

	// Fetch active limits matching currency (filtered at DB level)
	// Use pagination loop to handle cases where more limits exist than MaxPaginationLimit
	status := model.LimitStatusActive
	currency := input.Currency

	var allLimits []model.Limit

	var cursor string

	for {
		filter := &model.ListLimitsFilter{
			Status:   &status,
			Currency: &currency,
			Limit:    constant.MaxPaginationLimit,
			Cursor:   cursor,
		}

		result, err := s.limitRepo.List(ctx, filter)
		if err != nil {
			libOtel.HandleSpanError(&span, "Failed to list limits", err)
			return nil, err
		}

		allLimits = append(allLimits, result.Limits...)

		// Break if no more pages
		if !result.HasMore || result.NextCursor == "" {
			break
		}

		cursor = result.NextCursor
	}

	// Filter by scope matching (scopes require in-memory evaluation)
	applicable := make([]model.Limit, 0, len(allLimits))

	// Build transaction scope from input for matching
	txScope := buildTransactionScope(input)

	for _, limit := range allLimits {
		// Scopes must match (ANY limit scope matches the transaction scope)
		if !scopeMatchesLimit(limit.Scopes, txScope) {
			continue
		}

		applicable = append(applicable, limit)
	}

	logger.WithFields(
		"operation", "service.limit_checker.get_applicable_limits",
		"limits_for_currency", len(allLimits),
		"applicable_limits", len(applicable),
		"currency", input.Currency,
	).Info("Filtered applicable limits")

	return applicable, nil
}

// checkSingleLimitWithoutIncrement checks a single limit and returns usage details.
// Does NOT increment counters - that happens in incrementAllCounters after all limits pass.
func (s *LimitCheckerService) checkSingleLimitWithoutIncrement(ctx context.Context, limit *model.Limit, input *model.CheckLimitsInput) (*limitCheckResult, error) {
	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "service.limit_checker.check_single_limit")
	defer span.End()

	logger = logging.WithTrace(ctx, logger)

	if err := libOtel.SetSpanAttributesFromStruct(&span, "limit", map[string]any{
		"id":        limit.ID.String(),
		"name":      limit.Name,
		"type":      string(limit.LimitType),
		"maxAmount": limit.MaxAmount,
	}); err != nil {
		span.RecordError(err)

		logger.WithFields(
			"operation", "service.limit_checker.check_single_limit",
			"limit_id", limit.ID.String(),
			"limit_name", limit.Name,
			"error", err.Error(),
		).Warn("Failed to set span attributes for limit")
	}

	// For PER_TRANSACTION limits, check directly against maxAmount
	if limit.LimitType == model.LimitTypePerTransaction {
		exceeded := input.Amount > limit.MaxAmount
		detail := &model.LimitUsageDetail{
			LimitID:         limit.ID,
			LimitAmount:     limit.MaxAmount,
			Scope:           formatScopeString(limit.Scopes),
			Period:          limit.LimitType,
			CurrentUsage:    0, // PER_TRANSACTION has no persistent usage
			AttemptedAmount: input.Amount,
			Exceeded:        exceeded,
			// Internal fields for rollback
			InternalLimitType: limit.LimitType,
			Scopes:            append([]model.Scope(nil), limit.Scopes...),
		}

		logger.WithFields(
			"operation", "service.limit_checker.check_single_limit",
			"limit_id", limit.ID.String(),
			"limit_type", "PER_TRANSACTION",
			"max_amount", limit.MaxAmount,
			"transaction_amount", input.Amount,
			"exceeded", exceeded,
		).Info("Checked PER_TRANSACTION limit")

		return &limitCheckResult{
			detail:    detail,
			exceeded:  exceeded,
			counterID: nil, // PER_TRANSACTION has no counter
		}, nil
	}

	// For DAILY/MONTHLY limits, get/create counter and check projected usage
	txScope := buildTransactionScope(input)
	scopeKey := model.CalculateScopeKey(txScope)

	periodKey, err := model.CalculatePeriodKey(limit.LimitType, input.TransactionTimestamp)
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to calculate period key", err)
		return nil, err
	}

	// Get or create the counter with row-level lock
	counter, err := s.usageCounterRepo.GetOrCreateForUpdate(ctx, limit.ID, scopeKey, periodKey)
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to get usage counter", err)
		return nil, err
	}

	// Calculate projected usage with overflow protection
	var projectedUsage int64

	var exceeded bool

	if counter.CurrentUsage > math.MaxInt64-input.Amount {
		// Overflow would occur - treat as exceeded and cap at MaxInt64
		exceeded = true
		projectedUsage = math.MaxInt64
	} else {
		projectedUsage = counter.CurrentUsage + input.Amount
		exceeded = projectedUsage > limit.MaxAmount
	}

	logger.WithFields(
		"operation", "service.limit_checker.check_single_limit",
		"limit_id", limit.ID.String(),
		"limit_type", string(limit.LimitType),
		"max_amount", limit.MaxAmount,
		"current_usage", counter.CurrentUsage,
		"transaction_amount", input.Amount,
		"projected_usage", projectedUsage,
		"exceeded", exceeded,
	).Info("Checked limit")

	// CurrentUsage represents the projected usage after applying input.Amount,
	// not the persisted/actual counter value. This projected value is returned
	// regardless of whether the limit was exceeded or the increment was applied.
	// When exceeded=true, the counter was NOT incremented but CurrentUsage still
	// shows what the usage would have been if the transaction were allowed.
	// When overflow would occur, CurrentUsage is capped at MaxInt64.
	detail := &model.LimitUsageDetail{
		LimitID:         limit.ID,
		LimitAmount:     limit.MaxAmount,
		Scope:           formatScopeString(limit.Scopes),
		Period:          limit.LimitType,
		CurrentUsage:    projectedUsage,
		AttemptedAmount: input.Amount,
		Exceeded:        exceeded,
		// Internal fields for rollback
		InternalLimitType: limit.LimitType,
		Scopes:            append([]model.Scope(nil), limit.Scopes...),
	}

	return &limitCheckResult{
		detail:    detail,
		exceeded:  exceeded,
		counterID: &counter.ID,
	}, nil
}

// incrementAllCounters increments all counters after all limits have passed.
// Only called when no limits are exceeded.
func (s *LimitCheckerService) incrementAllCounters(ctx context.Context, results []limitCheckResult, amount int64) error {
	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "service.limit_checker.increment_all_counters")
	defer span.End()

	logger = logging.WithTrace(ctx, logger)

	for _, result := range results {
		// Skip PER_TRANSACTION limits (no persistent counter)
		if result.counterID == nil {
			continue
		}

		if err := s.usageCounterRepo.IncrementAtomic(ctx, *result.counterID, amount); err != nil {
			libOtel.HandleSpanError(&span, "Failed to increment counter", err)
			return err
		}

		logger.WithFields(
			"operation", "service.limit_checker.increment_all_counters",
			"limit_id", result.detail.LimitID.String(),
			"counter_id", result.counterID.String(),
			"increment_amount", amount,
		).Info("Incremented usage counter")
	}

	return nil
}

// buildTransactionScope creates a Scope from CheckLimitsInput fields.
// This scope is used for matching against limit scopes and for scopeKey generation.
func buildTransactionScope(input *model.CheckLimitsInput) *model.Scope {
	if input == nil {
		return nil
	}

	return &model.Scope{
		AccountID:       &input.AccountID,
		SegmentID:       input.SegmentID,
		PortfolioID:     input.PortfolioID,
		TransactionType: input.TransactionType,
		SubType:         input.SubType,
	}
}

// scopeMatchesLimit checks if any of the limit's scopes match the transaction scope.
// Global limits (empty scopes) match all transactions.
func scopeMatchesLimit(limitScopes []model.Scope, txScope *model.Scope) bool {
	// Global limit (empty scopes) matches all transactions
	if len(limitScopes) == 0 {
		return true
	}

	if txScope == nil {
		return false
	}

	// Check if ANY limit scope matches the transaction scope
	for i := range limitScopes {
		if limitScopes[i].Matches(txScope) {
			return true
		}
	}

	return false
}

// formatScopeString creates a human-readable string representation of scopes.
// Format examples: "global", "(account:uuid)", "(account:uuid,segment:uuid)", "(account:a,segment:b) OR (account:c)"
// Per API Design v1.3.2 section 4.1.1 LimitUsage.scope field.
// Each scope is wrapped in parentheses; multiple scopes (OR alternatives) are joined with " OR ".
func formatScopeString(scopes []model.Scope) string {
	if len(scopes) == 0 {
		return "global"
	}

	var scopeGroups []string

	for _, scope := range scopes {
		var fields []string

		if scope.AccountID != nil {
			fields = append(fields, "account:"+scope.AccountID.String())
		}

		if scope.SegmentID != nil {
			fields = append(fields, "segment:"+scope.SegmentID.String())
		}

		if scope.PortfolioID != nil {
			fields = append(fields, "portfolio:"+scope.PortfolioID.String())
		}

		if scope.MerchantID != nil {
			fields = append(fields, "merchant:"+scope.MerchantID.String())
		}

		if scope.TransactionType != nil {
			fields = append(fields, "transactionType:"+string(*scope.TransactionType))
		}

		if scope.SubType != nil {
			fields = append(fields, "subType:"+*scope.SubType)
		}

		if len(fields) > 0 {
			scopeGroups = append(scopeGroups, "("+strings.Join(fields, ",")+")")
		}
	}

	if len(scopeGroups) == 0 {
		return "global"
	}

	return strings.Join(scopeGroups, " OR ")
}
