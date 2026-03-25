// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package services

//go:generate mockgen -source=validation_service.go -destination=mocks/validation_service_mock.go -package=mocks

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	libCommons "github.com/LerianStudio/lib-commons/v2/commons"
	libLog "github.com/LerianStudio/lib-commons/v2/commons/log"
	libOpentelemetry "github.com/LerianStudio/lib-commons/v2/commons/opentelemetry"

	pgdb "tracer/internal/adapters/postgres/db"
	"tracer/internal/services/command"
	"tracer/internal/services/query"
	"tracer/pkg/clock"
	"tracer/pkg/contextutil"
	"tracer/pkg/logging"
	"tracer/pkg/model"
	"tracer/pkg/sanitize"
)

// validationPersistTimeout is the maximum duration for transaction validation record persistence.
// 5 seconds is sufficient for DB operations in normal conditions.
const validationPersistTimeout = 5 * time.Second

// validationTxTimeout bounds the entire transaction lifecycle (BeginTx through Commit/Rollback).
// Prevents connection pool exhaustion from stalled queries or deadlocks.
const validationTxTimeout = 10 * time.Second

// Sentinel errors for ValidationService constructor validation.
var (
	ErrNilRuleEvaluator                  = errors.New("rule evaluator cannot be nil")
	ErrNilLimitChecker                   = errors.New("limit checker cannot be nil")
	ErrNilTransactionValidationRepo      = errors.New("transaction validation repository cannot be nil")
	ErrNilTransactionValidationQueryRepo = errors.New("transaction validation query repository cannot be nil")
	ErrNilAuditWriter                    = errors.New("auditWriter cannot be nil")
	ErrNilConn                           = errors.New("database connection cannot be nil")
)

// ValidateResult is the result of transaction validation including idempotency information.
// IsDuplicate is true when the same request_id was already processed - the handler uses
// this to return HTTP 200 (duplicate) vs HTTP 201 (new).
type ValidateResult struct {
	Response    *model.ValidationResponse
	IsDuplicate bool
}

// RuleEvaluator evaluates transaction rules.
type RuleEvaluator interface {
	Execute(ctx context.Context, req *model.ValidationRequest) (*model.EvaluationResult, error)
}

// LimitChecker checks transaction limits.
type LimitChecker interface {
	CheckLimits(ctx context.Context, db pgdb.DB, input *model.CheckLimitsInput) (*model.CheckLimitsOutput, error)
}

// ValidationService orchestrates transaction validation.
type ValidationService struct {
	conn                           pgdb.TxBeginner
	ruleEvaluator                  RuleEvaluator
	limitChecker                   LimitChecker
	transactionValidationRepo      command.TransactionValidationRepository
	transactionValidationQueryRepo query.TransactionValidationRepository
	auditWriter                    AuditWriter
	clock                          clock.Clock
}

// NewValidationService creates a new ValidationService with dependency validation.
func NewValidationService(
	conn pgdb.TxBeginner,
	ruleEval RuleEvaluator,
	limitCheck LimitChecker,
	transactionValidationRepo command.TransactionValidationRepository,
	transactionValidationQueryRepo query.TransactionValidationRepository,
	auditWriter AuditWriter,
	clk clock.Clock,
) (*ValidationService, error) {
	if conn == nil {
		return nil, ErrNilConn
	}

	if ruleEval == nil {
		return nil, ErrNilRuleEvaluator
	}

	if limitCheck == nil {
		return nil, ErrNilLimitChecker
	}

	if transactionValidationRepo == nil {
		return nil, ErrNilTransactionValidationRepo
	}

	if transactionValidationQueryRepo == nil {
		return nil, ErrNilTransactionValidationQueryRepo
	}

	if auditWriter == nil {
		return nil, ErrNilAuditWriter
	}

	if clk == nil {
		clk = clock.RealClock{}
	}

	return &ValidationService{
		conn:                           conn,
		ruleEvaluator:                  ruleEval,
		limitChecker:                   limitCheck,
		transactionValidationRepo:      transactionValidationRepo,
		transactionValidationQueryRepo: transactionValidationQueryRepo,
		auditWriter:                    auditWriter,
		clock:                          clk,
	}, nil
}

// Validate orchestrates the transaction validation flow with idempotency support.
// Returns ValidateResult with IsDuplicate=true for duplicate requests (DD-3: Stripe model).
// Decision precedence: DENY > Limit Exceeded > REVIEW > ALLOW > Default.
//
// # Transactional Flow
//
// The validation uses database transactions to ensure atomicity:
//   - Dedup check and rule evaluation happen OUTSIDE a transaction
//   - DENY-by-rule: persists validation+audit in separate transaction (no counters involved)
//   - Limit checks, validation persistence, and audit recording happen INSIDE a transaction
//   - If limit exceeded or REVIEW: tx.Rollback() atomically undoes counter increments
//   - If ALLOW: COMMIT saves counters, validation record, and audit event atomically
//
// This eliminates the need for compensating rollbacks and their associated failure modes.
func (s *ValidationService) Validate(ctx context.Context, req *model.ValidationRequest) (*ValidateResult, error) {
	// Check context cancellation FIRST
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if req == nil {
		return nil, errors.New("validation request cannot be nil")
	}

	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "service.validation.orchestrate")
	defer span.End()

	logger = logging.WithTrace(ctx, logger)

	// Step 0: Check for duplicate request (DD-3: Stripe model deduplication)
	// This is done BEFORE any processing to avoid double-counting limits.
	existingValidation, err := s.transactionValidationQueryRepo.FindByRequestID(ctx, req.RequestID)
	if err != nil {
		libOpentelemetry.HandleSpanError(&span, "failed to check for duplicate request", err)

		return nil, fmt.Errorf("failed to check for duplicate request: %w", err)
	}

	if existingValidation != nil {
		// Duplicate detected - return cached response without processing
		logger.WithFields(
			"operation", "service.validation.orchestrate",
			"request.id", req.RequestID,
			"existing.validation.id", existingValidation.ID,
		).Info("Duplicate request detected - returning cached response")

		span.AddEvent("duplicate_request_detected")

		return &ValidateResult{
			Response:    existingValidation.ToValidationResponse(),
			IsDuplicate: true,
		}, nil
	}

	startTime := time.Now() // Wall clock for latency measurement only
	evaluatedAt := s.clock.Now().UTC()

	// Generate validationId for audit record (used in both response and persistence)
	validationID := uuid.New()

	logger.WithFields(
		"operation", "service.validation.orchestrate",
		"validation.id", validationID,
		"request.id", req.RequestID,
		"transaction.type", req.TransactionType,
		"transaction.amount", req.Amount,
	).Info("Starting validation")

	// Build response
	response := model.NewValidationResponse(validationID, req.RequestID, model.DecisionAllow, evaluatedAt)

	// Step 1: Evaluate rules (OUTSIDE transaction)
	evalResult, err := s.ruleEvaluator.Execute(ctx, req)
	if err != nil {
		libOpentelemetry.HandleSpanError(&span, "rule evaluation failed", err)

		return nil, fmt.Errorf("rule evaluation failed: %w", err)
	}

	if evalResult == nil {
		libOpentelemetry.HandleSpanError(&span, "rule evaluation returned nil", nil)

		return nil, fmt.Errorf("rule evaluation returned nil result")
	}

	// Copy evaluation result to response
	response.EvaluationResult = *evalResult
	response.Decision = evalResult.Decision

	// If DENY by rule, return immediately (don't check limits)
	// Persist using non-transactional helpers since no counters are involved
	if evalResult.Decision == model.DecisionDeny {
		response.ProcessingTimeMs = time.Since(startTime).Milliseconds()
		s.persistTransactionValidation(ctx, req, response, logger)
		s.persistAuditEvent(ctx, req, response, logger)

		logger.WithFields(
			"operation", "service.validation.orchestrate",
			"request.id", req.RequestID,
			"decision", "DENY",
		).Info("Validation completed (by rule)")

		return &ValidateResult{
			Response:    response,
			IsDuplicate: false,
		}, nil
	}

	// Step 2: Begin transaction for limit checks + persistence
	// This ensures atomicity: counter increments, validation record, and audit event
	// are either ALL committed (ALLOW) or ALL rolled back (DENY/REVIEW).
	// txCtx bounds the entire transaction lifecycle to prevent connection pool exhaustion.
	txCtx, txCancel := context.WithTimeout(ctx, validationTxTimeout)
	defer txCancel()

	tx, err := s.conn.BeginTx(txCtx, nil) // nil = default isolation level (typically READ COMMITTED)
	if err != nil {
		libOpentelemetry.HandleSpanError(&span, "failed to begin transaction", err)

		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}

	// Ensure tx is rolled back on panic or early return
	// The defer will be a no-op if we explicitly commit
	defer func() {
		if tx != nil {
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				logger.WithFields(
					"operation", "service.validation.orchestrate",
					"error", rollbackErr.Error(),
				).Warn("Failed to rollback transaction in defer cleanup")
			}
		}
	}()

	// Step 3: Check limits (INSIDE transaction)
	limitInput := req.ToCheckLimitsInput()

	limitOutput, err := s.limitChecker.CheckLimits(txCtx, tx, limitInput)
	if err != nil {
		// tx.Rollback() will be called by defer
		libOpentelemetry.HandleSpanError(&span, "limit check failed", err)

		return nil, fmt.Errorf("limit check failed: %w", err)
	}

	if limitOutput == nil {
		// tx.Rollback() will be called by defer
		libOpentelemetry.HandleSpanError(&span, "limit check returned nil", nil)

		return nil, fmt.Errorf("limit check returned nil result")
	}

	response.LimitUsageDetails = limitOutput.LimitUsageDetails
	response.EvaluatedAt = limitOutput.EvaluatedAt

	// If limit exceeded, rollback counters and persist DENY outside tx
	if !limitOutput.Allowed {
		response.Decision = model.DecisionDeny
		response.Reason = "limit_exceeded"
		response.ProcessingTimeMs = time.Since(startTime).Milliseconds()
		s.rollbackAndPersist(ctx, tx, req, response, logger, "limit exceeded")
		tx = nil

		return &ValidateResult{Response: response, IsDuplicate: false}, nil
	}

	// Step 4: If rules returned REVIEW, rollback counters and persist outside tx
	// REVIEW means "manual review required" - don't count transaction against limits
	if evalResult.Decision == model.DecisionReview {
		response.ProcessingTimeMs = time.Since(startTime).Milliseconds()
		s.rollbackAndPersist(ctx, tx, req, response, logger, "REVIEW decision")
		tx = nil

		return &ValidateResult{Response: response, IsDuplicate: false}, nil
	}

	// Step 5: ALLOW path - persist validation and audit inside transaction, then COMMIT
	response.ProcessingTimeMs = time.Since(startTime).Milliseconds()

	// Persist transaction validation inside tx
	if err := s.persistTransactionValidationWithTx(txCtx, tx, req, response, logger); err != nil {
		if result := s.handleConcurrentDuplicate(ctx, err, req, logger); result != nil {
			return result, nil
		}

		// tx.Rollback() will be called by defer
		libOpentelemetry.HandleSpanError(&span, "failed to persist transaction validation", err)

		return nil, fmt.Errorf("failed to persist transaction validation: %w", err)
	}

	// Persist audit event inside tx
	if err := s.persistAuditEventWithTx(txCtx, tx, req, response, logger); err != nil {
		// tx.Rollback() will be called by defer
		libOpentelemetry.HandleSpanError(&span, "failed to persist audit event", err)

		return nil, fmt.Errorf("failed to persist audit event: %w", err)
	}

	// COMMIT the transaction
	if err := tx.Commit(); err != nil {
		libOpentelemetry.HandleSpanError(&span, "failed to commit transaction", err)

		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	tx = nil // Prevent defer from rolling back after successful commit

	logger.WithFields(
		"operation", "service.validation.orchestrate",
		"request.id", req.RequestID,
		"decision", response.Decision,
	).Info("Validation completed")

	return &ValidateResult{
		Response:    response,
		IsDuplicate: false,
	}, nil
}

// rollbackAndPersist rolls back the transaction to undo counter increments,
// then persists validation and audit records outside the transaction (best-effort).
// Used by DENY-by-limit and REVIEW paths.
func (s *ValidationService) rollbackAndPersist(ctx context.Context, tx pgdb.Tx, req *model.ValidationRequest, resp *model.ValidationResponse, logger libLog.Logger, reason string) {
	if tx != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			logger.WithFields(
				"operation", "service.validation.orchestrate",
				"request.id", req.RequestID,
				"error", rollbackErr.Error(),
			).Warn("Failed to rollback transaction for " + reason)
		}
	}

	s.persistTransactionValidation(ctx, req, resp, logger)
	s.persistAuditEvent(ctx, req, resp, logger)

	logger.WithFields(
		"operation", "service.validation.orchestrate",
		"request.id", req.RequestID,
		"decision", resp.Decision,
	).Info("Validation completed")
}

// handleConcurrentDuplicate checks if a persist error is a concurrent duplicate (TOCTOU race)
// and returns the cached response if so. Returns nil if the error is not a duplicate.
func (s *ValidationService) handleConcurrentDuplicate(ctx context.Context, err error, req *model.ValidationRequest, logger libLog.Logger) *ValidateResult {
	if !errors.Is(err, command.ErrDuplicateValidation) {
		return nil
	}

	logger.WithFields(
		"operation", "service.validation.orchestrate",
		"request.id", req.RequestID,
	).Info("Concurrent duplicate detected - fetching cached response")

	existing, findErr := s.transactionValidationQueryRepo.FindByRequestID(ctx, req.RequestID)
	if findErr == nil && existing != nil {
		return &ValidateResult{
			Response:    existing.ToValidationResponse(),
			IsDuplicate: true,
		}
	}

	return nil
}

// persistTransactionValidation persists a transaction validation record synchronously.
//
// # Error Contract: BEST-EFFORT (errors logged, NOT returned)
//
// This method is used outside transactions (DENY-by-rule, DENY-by-limit, REVIEW paths).
// Errors are logged and metrics emitted, but NOT returned to the caller. The validation
// response is always delivered to the client regardless of persistence outcome.
// This differs from persistTransactionValidationWithTx which returns errors for tx rollback.
//
// # Design Decision: Synchronous Persistence
//
// Persistence is synchronous to ensure that validation records are immediately
// available for retrieval via GET /v1/validations/{id}. This design prioritizes:
//
//  1. Consistency: Validation records are available immediately after POST returns
//  2. Compliance: SOX/GLBA audit trail is guaranteed before response is sent
//  3. Simplicity: No race conditions between validation response and queries
//
// Trade-offs accepted:
//   - Slightly higher latency (~5-10ms for DB write)
//   - Validation response blocked on DB availability
//
// The timeout (validationPersistTimeout) bounds the maximum wait time.
func (s *ValidationService) persistTransactionValidation(ctx context.Context, req *model.ValidationRequest, resp *model.ValidationResponse, logger libLog.Logger) {
	tv, err := buildTransactionValidation(req, resp, s.clock.Now().UTC())
	if err != nil {
		logger.WithFields(
			"request.id", resp.RequestID,
			"error.message", err.Error(),
		).Error("failed to build transaction validation record")

		return
	}

	// Extract tracer and metricsFactory from context for observability
	_, tracer, _, metricsFactory := libCommons.NewTrackingFromContext(ctx)

	// Create context with timeout for DB operation.
	// Use context.Background() instead of ctx to ensure persistence has full timeout budget
	// even if request context is cancelled. This ensures SOX/GLBA compliance persistence
	// completes regardless of client-side cancellation.
	persistCtx, cancel := context.WithTimeout(context.Background(), validationPersistTimeout)
	defer cancel()

	// Create span for tracing
	persistCtx, span := tracer.Start(persistCtx, "transaction-validation.persist")
	defer span.End()

	if err := s.transactionValidationRepo.Insert(persistCtx, tv); err != nil { //nolint:contextcheck // persistCtx intentionally from Background()
		libOpentelemetry.HandleSpanError(&span, "failed to persist transaction validation record", err)

		// Emit metric for alerting (compliance risk: audit trail gap)
		// Note: persistCtx is intentionally derived from Background(), not parent ctx
		if metricsFactory != nil {
			metricsFactory.Counter(MetricAuditPersistFailures).Add(persistCtx, 1) //nolint:contextcheck // persistCtx intentionally independent
		}

		logger.WithFields(
			"request.id", resp.RequestID,
			"error.message", err.Error(),
		).Error("failed to persist transaction validation record")
	}
}

// buildTransactionValidation creates and populates a TransactionValidation record from the request and response.
// Populates all compliance fields (SOX/GLBA) and validates the record before returning.
// createdAt should come from the injected clock for testability.
func buildTransactionValidation(req *model.ValidationRequest, resp *model.ValidationResponse, createdAt time.Time) (*model.TransactionValidation, error) {
	tv, err := model.NewTransactionValidation(resp.ValidationID, resp.Decision, createdAt)
	if err != nil {
		return nil, fmt.Errorf("failed to create transaction validation record: %w", err)
	}

	tv.RequestID = req.RequestID
	tv.TransactionType = req.TransactionType
	tv.SubType = req.SubType
	tv.Amount = req.Amount
	tv.Currency = req.Currency
	tv.TransactionTimestamp = req.TransactionTimestamp
	tv.Account = req.Account
	tv.Segment = req.Segment
	tv.Portfolio = req.Portfolio
	tv.Merchant = req.Merchant
	tv.Metadata = sanitize.SanitizeMetadata(req.Metadata)
	tv.EvaluationResult = resp.EvaluationResult
	tv.LimitUsageDetails = resp.LimitUsageDetails
	tv.ProcessingTimeMs = resp.ProcessingTimeMs

	if err := validateTransactionValidation(tv); err != nil {
		return nil, fmt.Errorf("transaction validation record validation failed: %w", err)
	}

	return tv, nil
}

// validateTransactionValidation ensures compliance-critical fields are present before persistence.
// Returns an error if any required field for SOX/GLBA compliance is missing or invalid.
// This is a defensive check - these fields should always be populated by the validation flow.
func validateTransactionValidation(tv *model.TransactionValidation) error {
	// ID must be valid (not nil UUID)
	if tv.ID == (uuid.UUID{}) {
		return errors.New("transaction validation ID is nil")
	}

	// Decision must be valid
	if !tv.Decision.IsValid() {
		return fmt.Errorf("invalid decision: %s", tv.Decision)
	}

	// RequestID must be valid
	if tv.RequestID == (uuid.UUID{}) {
		return errors.New("transaction validation request ID is nil")
	}

	// TransactionType must be valid
	if !tv.TransactionType.IsValid() {
		return fmt.Errorf("invalid transaction type: %s", tv.TransactionType)
	}

	// Amount must be positive
	if tv.Amount.LessThanOrEqual(decimal.Zero) {
		return fmt.Errorf("invalid amount: %s", tv.Amount.String())
	}

	// Currency must not be empty
	if tv.Currency == "" {
		return errors.New("currency is empty")
	}

	// TransactionTimestamp must be valid for compliance audit trail
	if tv.TransactionTimestamp.IsZero() {
		return errors.New("transaction timestamp is zero")
	}

	// Account ID must be valid
	if tv.Account.ID == (uuid.UUID{}) {
		return errors.New("account ID is nil")
	}

	return nil
}

// buildRequestSnapshot creates the request snapshot map used for audit event persistence.
func buildRequestSnapshot(req *model.ValidationRequest) map[string]any {
	requestSnapshot := map[string]any{
		"requestId":       req.RequestID.String(),
		"transactionType": req.TransactionType,
		"subType":         req.SubType,
		"amount":          req.Amount,
		"currency":        req.Currency,
		"timestamp":       req.TransactionTimestamp,
		"account": map[string]any{
			"id":       req.Account.ID.String(),
			"type":     req.Account.Type,
			"status":   req.Account.Status,
			"metadata": req.Account.Metadata,
		},
		"metadata": req.Metadata,
	}

	if req.Segment != nil {
		requestSnapshot["account"].(map[string]any)["segmentId"] = req.Segment.ID.String()
		requestSnapshot["segment"] = map[string]any{
			"segmentId": req.Segment.ID.String(),
			"name":      req.Segment.Name,
			"metadata":  req.Segment.Metadata,
		}
	}

	if req.Portfolio != nil {
		requestSnapshot["account"].(map[string]any)["portfolioId"] = req.Portfolio.ID.String()
		requestSnapshot["portfolio"] = map[string]any{
			"portfolioId": req.Portfolio.ID.String(),
			"name":        req.Portfolio.Name,
			"metadata":    req.Portfolio.Metadata,
		}
	}

	if req.Merchant != nil {
		requestSnapshot["merchant"] = map[string]any{
			"merchantId": req.Merchant.ID,
			"name":       req.Merchant.Name,
			"category":   req.Merchant.Category,
			"country":    req.Merchant.Country,
			"metadata":   req.Merchant.Metadata,
		}
	}

	return requestSnapshot
}

// persistAuditEvent persists an audit event for the transaction validation.
//
// # Error Contract: BEST-EFFORT (errors logged, NOT returned)
//
// See persistTransactionValidation for rationale.
// Note: clientIP is extracted from context metadata (set by HTTP handler).
func (s *ValidationService) persistAuditEvent(ctx context.Context, req *model.ValidationRequest, resp *model.ValidationResponse, logger libLog.Logger) {
	clientIP := contextutil.GetClientIP(ctx)
	requestSnapshot := buildRequestSnapshot(req)

	responseContext := model.ValidationResponseContext{
		ProcessingTimeMs:  resp.ProcessingTimeMs,
		LimitUsageDetails: resp.LimitUsageDetails,
	}

	if err := s.auditWriter.RecordValidationEvent(
		ctx,
		resp.ValidationID, // validationID (used as resource_id)
		requestSnapshot,
		resp.EvaluationResult,
		responseContext,
		clientIP,
	); err != nil {
		logger.WithFields(
			"request.id", req.RequestID,
			"error", err.Error(),
		).Error("failed to persist audit event")
	}
}

// persistTransactionValidationWithTx persists a transaction validation record using the provided transaction.
//
// # Error Contract: STRICT (errors returned to caller)
//
// This method is used inside transactions (ALLOW path). Errors are returned so the caller
// can let the deferred tx.Rollback() undo all changes atomically (counters + validation + audit).
// This differs from persistTransactionValidation which uses best-effort semantics.
func (s *ValidationService) persistTransactionValidationWithTx(ctx context.Context, tx pgdb.DB, req *model.ValidationRequest, resp *model.ValidationResponse, logger libLog.Logger) error {
	tv, err := buildTransactionValidation(req, resp, s.clock.Now().UTC())
	if err != nil {
		logger.WithFields(
			"request.id", resp.RequestID,
			"error.message", err.Error(),
		).Error("failed to build transaction validation record")

		return fmt.Errorf("failed to build transaction validation record: %w", err)
	}

	if err := s.transactionValidationRepo.InsertWithTx(ctx, tx, tv); err != nil {
		logger.WithFields(
			"request.id", resp.RequestID,
			"error.message", err.Error(),
		).Error("failed to persist transaction validation record")

		return fmt.Errorf("failed to persist transaction validation record: %w", err)
	}

	return nil
}

// persistAuditEventWithTx persists an audit event using the provided transaction.
//
// # Error Contract: STRICT (errors returned to caller)
//
// See persistTransactionValidationWithTx for rationale.
func (s *ValidationService) persistAuditEventWithTx(ctx context.Context, tx pgdb.DB, req *model.ValidationRequest, resp *model.ValidationResponse, logger libLog.Logger) error {
	clientIP := contextutil.GetClientIP(ctx)
	requestSnapshot := buildRequestSnapshot(req)

	responseContext := model.ValidationResponseContext{
		ProcessingTimeMs:  resp.ProcessingTimeMs,
		LimitUsageDetails: resp.LimitUsageDetails,
	}

	if err := s.auditWriter.RecordValidationEventWithTx(
		ctx,
		tx,
		resp.ValidationID, // validationID (used as resource_id)
		requestSnapshot,
		resp.EvaluationResult,
		responseContext,
		clientIP,
	); err != nil {
		logger.WithFields(
			"request.id", req.RequestID,
			"error", err.Error(),
		).Error("failed to persist audit event")

		return fmt.Errorf("failed to persist audit event: %w", err)
	}

	return nil
}
