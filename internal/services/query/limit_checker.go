// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package query

//go:generate mockgen -source=limit_checker.go -destination=limit_checker_mock.go -package=query

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	libCommons "github.com/LerianStudio/lib-commons/v2/commons"
	libOtel "github.com/LerianStudio/lib-commons/v2/commons/opentelemetry"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"go.opentelemetry.io/otel/trace"

	"tracer/pkg/clock"
	"tracer/pkg/constant"
	"tracer/pkg/logging"
	"tracer/pkg/model"
)

// rollbackTimeout bounds rollback compensation operations to prevent unbounded resource consumption.
// 5 seconds is sufficient for typical database operations under normal conditions.
// Follows the pattern established by validationPersistTimeout in validation_service.go.
const rollbackTimeout = 5 * time.Second

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
	clock            clock.Clock
}

// NewLimitChecker creates a new LimitCheckerService.
// Returns error if any dependency is nil.
func NewLimitChecker(limitRepo LimitRepository, usageCounterRepo UsageCounterRepository, clk clock.Clock) (*LimitCheckerService, error) {
	if limitRepo == nil {
		return nil, constant.ErrLimitCheckerNilLimitRepo
	}

	if usageCounterRepo == nil {
		return nil, constant.ErrLimitCheckerNilUsageCounterRepo
	}

	if clk == nil {
		return nil, constant.ErrLimitCheckerNilClock
	}

	return &LimitCheckerService{
		limitRepo:        limitRepo,
		usageCounterRepo: usageCounterRepo,
		clock:            clk,
	}, nil
}

// CheckLimits evaluates all applicable limits for a transaction.
// Uses atomic upsert to prevent TOCTOU race conditions.
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

	// Build transaction scope and compute server timestamp once for all limits
	txScope := buildTransactionScope(input)
	scopeKey := model.CalculateScopeKey(txScope)
	serverNow := s.clock.Now()

	// Process each limit with atomic upsert (increment happens in DB)
	usageDetails := make([]model.LimitUsageDetail, 0, len(limits))
	incrementedDetails := make([]model.LimitUsageDetail, 0, len(limits))

	var exceededLimitID *uuid.UUID

	for i := range limits {
		limit := &limits[i]

		detail, exceeded, err := s.processLimitAtomic(ctx, limit, input, scopeKey, serverNow)
		if err != nil {
			// DB error - rollback already-incremented counters and return error
			// Use detached context for rollback - request context may be canceled,
			// but compensation MUST complete to maintain data integrity.
			if len(incrementedDetails) > 0 {
				rollbackCtx, cancel := context.WithTimeout(context.Background(), rollbackTimeout)
				// Preserve trace context for observability
				rollbackCtx = trace.ContextWithSpan(rollbackCtx, trace.SpanFromContext(ctx))
				s.rollbackIncrementedCounters(rollbackCtx, input, incrementedDetails, scopeKey)
				cancel()
			}

			libOtel.HandleSpanError(&span, "Failed to process limit atomically", err)

			return nil, err
		}

		usageDetails = append(usageDetails, *detail)

		if exceeded {
			// Limit exceeded - rollback already-incremented counters and break
			exceededLimitID = &limit.ID

			// Rollback with detached context - compensation must complete regardless
			// of request cancellation to prevent false limit exhaustion.
			if len(incrementedDetails) > 0 {
				rollbackCtx, cancel := context.WithTimeout(context.Background(), rollbackTimeout)
				// Preserve trace context for observability
				rollbackCtx = trace.ContextWithSpan(rollbackCtx, trace.SpanFromContext(ctx))
				s.rollbackIncrementedCounters(rollbackCtx, input, incrementedDetails, scopeKey)
				cancel()
			}

			break
		}

		// Only track DAILY/MONTHLY limits for potential rollback (PER_TRANSACTION has no counters)
		if limit.LimitType != model.LimitTypePerTransaction {
			incrementedDetails = append(incrementedDetails, *detail)
		}
	}

	output := model.NewCheckLimitsOutput(true).WithLimitUsageDetails(usageDetails)

	if exceededLimitID != nil {
		output = output.WithExceededLimits([]uuid.UUID{*exceededLimitID})
		output.Allowed = false

		logger.WithFields(
			"operation", "service.limit_checker.check_limits",
			"exceeded_limit_id", exceededLimitID.String(),
		).Info("Limit exceeded")
	} else {
		logger.WithFields(
			"operation", "service.limit_checker.check_limits",
			"checked_count", len(usageDetails),
		).Info("All limits passed")
	}

	return output, nil
}

// processLimitAtomic processes a single limit using atomic upsert for DAILY/MONTHLY limits.
// Returns the usage detail, whether the limit was exceeded, and any error.
func (s *LimitCheckerService) processLimitAtomic(
	ctx context.Context,
	limit *model.Limit,
	input *model.CheckLimitsInput,
	scopeKey string,
	serverNow time.Time,
) (*model.LimitUsageDetail, bool, error) {
	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "service.limit_checker.process_limit_atomic")
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
			"operation", "service.limit_checker.process_limit_atomic",
			"limit_id", limit.ID.String(),
			"limit_name", limit.Name,
			"error", err.Error(),
		).Warn("Failed to set span attributes for limit")
	}

	// For PER_TRANSACTION limits, check directly against maxAmount (no counter needed)
	if limit.LimitType == model.LimitTypePerTransaction {
		exceeded := input.Amount.GreaterThan(limit.MaxAmount)
		detail := &model.LimitUsageDetail{
			LimitID:           limit.ID,
			LimitAmount:       limit.MaxAmount,
			Scope:             formatScopeString(limit.Scopes),
			Period:            limit.LimitType,
			CurrentUsage:      decimal.Zero, // PER_TRANSACTION has no persistent usage
			AttemptedAmount:   input.Amount,
			Exceeded:          exceeded,
			InternalLimitType: limit.LimitType,
			Scopes:            append([]model.Scope(nil), limit.Scopes...),
			InternalPeriodKey: "", // PER_TRANSACTION has no period key
		}

		logger.WithFields(
			"operation", "service.limit_checker.process_limit_atomic",
			"limit_id", limit.ID.String(),
			"limit_type", "PER_TRANSACTION",
			"max_amount", limit.MaxAmount.String(),
			"transaction_amount", input.Amount.String(),
			"exceeded", exceeded,
		).Info("Checked PER_TRANSACTION limit")

		return detail, exceeded, nil
	}

	// For DAILY/MONTHLY limits, use atomic upsert
	periodKey, err := model.CalculatePeriodKey(limit.LimitType, serverNow)
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to calculate period key", err)
		return nil, false, err
	}

	// Pre-check: amount > maxAmount would always fail (INSERT path has no WHERE guard)
	if input.Amount.GreaterThan(limit.MaxAmount) {
		detail := &model.LimitUsageDetail{
			LimitID:           limit.ID,
			LimitAmount:       limit.MaxAmount,
			Scope:             formatScopeString(limit.Scopes),
			Period:            limit.LimitType,
			CurrentUsage:      input.Amount, // Would-be usage if allowed
			AttemptedAmount:   input.Amount,
			Exceeded:          true,
			InternalLimitType: limit.LimitType,
			Scopes:            append([]model.Scope(nil), limit.Scopes...),
			InternalPeriodKey: periodKey,
		}

		logger.WithFields(
			"operation", "service.limit_checker.process_limit_atomic",
			"limit_id", limit.ID.String(),
			"limit_type", string(limit.LimitType),
			"max_amount", limit.MaxAmount.String(),
			"transaction_amount", input.Amount.String(),
			"exceeded", true,
		).Info("Amount exceeds limit maximum (pre-check)")

		return detail, true, nil
	}

	// Atomic upsert: create or increment counter, enforcing maxAmount in DB
	newUsage, err := s.usageCounterRepo.UpsertAndIncrementAtomic(
		ctx,
		limit.ID,
		scopeKey,
		periodKey,
		input.Amount,
		limit.MaxAmount,
	)

	if errors.Is(err, constant.ErrUsageCounterExceedsLimit) {
		// Limit exceeded - the counter was NOT incremented
		libOtel.HandleSpanBusinessErrorEvent(&span, "Limit exceeded", err)

		detail := &model.LimitUsageDetail{
			LimitID:           limit.ID,
			LimitAmount:       limit.MaxAmount,
			Scope:             formatScopeString(limit.Scopes),
			Period:            limit.LimitType,
			CurrentUsage:      newUsage.Add(input.Amount), // Projected usage (what it would be)
			AttemptedAmount:   input.Amount,
			Exceeded:          true,
			InternalLimitType: limit.LimitType,
			Scopes:            append([]model.Scope(nil), limit.Scopes...),
			InternalPeriodKey: periodKey,
		}

		logger.WithFields(
			"operation", "service.limit_checker.process_limit_atomic",
			"limit_id", limit.ID.String(),
			"limit_type", string(limit.LimitType),
			"max_amount", limit.MaxAmount.String(),
			"transaction_amount", input.Amount.String(),
			"exceeded", true,
		).Info("Limit exceeded (atomic check)")

		return detail, true, nil
	}

	if err != nil {
		// DB error
		libOtel.HandleSpanError(&span, "Failed to upsert and increment counter", err)
		return nil, false, fmt.Errorf("failed to upsert and increment counter: %w", err)
	}

	// Success: counter was incremented
	detail := &model.LimitUsageDetail{
		LimitID:           limit.ID,
		LimitAmount:       limit.MaxAmount,
		Scope:             formatScopeString(limit.Scopes),
		Period:            limit.LimitType,
		CurrentUsage:      newUsage, // Actual new usage from DB
		AttemptedAmount:   input.Amount,
		Exceeded:          false,
		InternalLimitType: limit.LimitType,
		Scopes:            append([]model.Scope(nil), limit.Scopes...),
		InternalPeriodKey: periodKey,
	}

	logger.WithFields(
		"operation", "service.limit_checker.process_limit_atomic",
		"limit_id", limit.ID.String(),
		"limit_type", string(limit.LimitType),
		"max_amount", limit.MaxAmount.String(),
		"new_usage", newUsage.String(),
		"transaction_amount", input.Amount.String(),
		"period_key", periodKey,
	).Info("Limit check passed (atomic upsert)")

	return detail, false, nil
}

// rollbackIncrementedCounters decrements counters that were already incremented when a later limit fails.
// Uses the InternalPeriodKey stored in each detail to target the exact counter.
// Best-effort: logs errors but continues (see EVENTUAL CONSISTENCY DESIGN in RollbackUsage).
func (s *LimitCheckerService) rollbackIncrementedCounters(
	ctx context.Context,
	input *model.CheckLimitsInput,
	incrementedDetails []model.LimitUsageDetail,
	scopeKey string,
) {
	logger, tracer, _, metricsFactory := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "service.limit_checker.rollback_incremented_counters")
	defer span.End()

	logger = logging.WithTrace(ctx, logger)

	logger.WithFields(
		"operation", "service.limit_checker.rollback_incremented_counters",
		"details_count", len(incrementedDetails),
	).Info("Rolling back incremented counters")

	var failedLimits []uuid.UUID

	for _, detail := range incrementedDetails {
		// Use the stored period key (computed at increment time)
		periodKey := detail.InternalPeriodKey

		if periodKey == "" {
			logger.WithFields(
				"operation", "service.limit_checker.rollback_incremented_counters",
				"limit_id", detail.LimitID.String(),
			).Warn("No period key stored for rollback, skipping")

			failedLimits = append(failedLimits, detail.LimitID)

			continue
		}

		// Get existing counter (do NOT create one - rollback should only affect existing counters)
		counter, err := s.usageCounterRepo.GetForUpdate(ctx, detail.LimitID, scopeKey, periodKey)
		if err != nil {
			logger.WithFields(
				"operation", "service.limit_checker.rollback_incremented_counters",
				"limit_id", detail.LimitID.String(),
				"period_key", periodKey,
				"error", err.Error(),
			).Warn("Failed to get counter for rollback, skipping")

			failedLimits = append(failedLimits, detail.LimitID)

			continue
		}

		if err := s.usageCounterRepo.DecrementAtomic(ctx, counter.ID, input.Amount); err != nil {
			logger.WithFields(
				"operation", "service.limit_checker.rollback_incremented_counters",
				"limit_id", detail.LimitID.String(),
				"counter_id", counter.ID.String(),
				"amount", input.Amount.String(),
				"error", err.Error(),
			).Warn("Failed to decrement counter for rollback, skipping")

			failedLimits = append(failedLimits, detail.LimitID)

			continue
		}

		logger.WithFields(
			"operation", "service.limit_checker.rollback_incremented_counters",
			"limit_id", detail.LimitID.String(),
			"counter_id", counter.ID.String(),
			"amount", input.Amount.String(),
		).Info("Rolled back counter")
	}

	if len(failedLimits) > 0 {
		libOtel.HandleSpanBusinessErrorEvent(&span, "Some counters failed to rollback", fmt.Errorf("failed limits: %v", failedLimits))

		// Emit metric for alerting
		if metricsFactory != nil {
			metricsFactory.Counter(MetricRollbackFailures).Add(ctx, int64(len(failedLimits)))
		}

		logger.WithFields(
			"operation", "service.limit_checker.rollback_incremented_counters",
			"failed_count", len(failedLimits),
			"failed_limit_ids", failedLimits,
		).Warn("Some counters failed to rollback")
	}
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

	// Calculate scope key once for all limits
	txScope := buildTransactionScope(input)
	scopeKey := model.CalculateScopeKey(txScope)

	// Calculate server time once for consistent period key fallback
	// Prevents period key mismatch when rollback crosses period boundary
	serverTime := s.clock.Now()

	for _, detail := range usageDetails {
		// PER_TRANSACTION limits don't have persistent counters - skip without DB lookup
		// InternalLimitType is now included in LimitUsageDetail to avoid N+1 queries
		if detail.InternalLimitType == model.LimitTypePerTransaction {
			continue
		}

		// Use the stored period key (computed at increment time) to target exact counter
		// This prevents period key mismatch when rollback crosses a period boundary
		periodKey := detail.InternalPeriodKey
		if periodKey == "" {
			// Fallback for legacy callers or external rollback calls without stored period key
			var calcErr error

			periodKey, calcErr = model.CalculatePeriodKey(detail.InternalLimitType, serverTime)
			if calcErr != nil {
				logger.WithFields(
					"operation", "service.limit_checker.rollback_usage",
					"limit_id", detail.LimitID.String(),
					"limit_type", string(detail.InternalLimitType),
					"error", calcErr.Error(),
				).Warn("Failed to calculate fallback period key, skipping")

				failedLimits = append(failedLimits, detail.LimitID)

				continue
			}

			logger.WithFields(
				"operation", "service.limit_checker.rollback_usage",
				"limit_id", detail.LimitID.String(),
				"period_key", periodKey,
			).Info("Using server clock fallback for period key")
		}

		// Get existing counter (do NOT create one - rollback should only affect existing counters)
		counter, err := s.usageCounterRepo.GetForUpdate(ctx, detail.LimitID, scopeKey, periodKey)
		if err != nil {
			logger.WithFields(
				"operation", "service.limit_checker.rollback_usage",
				"limit_id", detail.LimitID.String(),
				"period_key", periodKey,
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
				"amount", input.Amount.String(),
				"error", err.Error(),
			).Warn("Failed to decrement counter for rollback, skipping")

			failedLimits = append(failedLimits, detail.LimitID)

			continue
		}

		logger.WithFields(
			"operation", "service.limit_checker.rollback_usage",
			"limit_id", detail.LimitID.String(),
			"counter_id", counter.ID.String(),
			"period_key", periodKey,
			"amount", input.Amount.String(),
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
