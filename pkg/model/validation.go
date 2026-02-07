// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package model

import (
	"maps"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"tracer/pkg"
	"tracer/pkg/constant"
)

// metadataKeyPattern allows only alphanumeric characters and underscores
var metadataKeyPattern = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

// Valid account types per API design
var validAccountTypes = map[string]bool{
	"checking": true, "savings": true, "credit": true,
}

// Valid account statuses per API design
var validAccountStatuses = map[string]bool{
	"active": true, "suspended": true, "closed": true,
}

// mccPattern validates 4-digit MCC codes (ISO 18245)
var mccPattern = regexp.MustCompile(`^\d{4}$`)

// countryCodePattern validates ISO 3166-1 alpha-2 codes (2 uppercase letters)
var countryCodePattern = regexp.MustCompile(`^[A-Z]{2}$`)

// DefaultClockSkewTolerance is the default maximum allowed time difference between
// the transaction timestamp and the server's current time to account for clock drift.
const DefaultClockSkewTolerance = 1 * time.Minute

// ClockSkewTolerance is the configurable maximum allowed time difference between
// the transaction timestamp and the server's current time. Callers can override
// this value at startup to adjust tolerance (e.g., 100-500ms for stricter checks).
var ClockSkewTolerance = DefaultClockSkewTolerance

// Decision represents the validation decision
type Decision string

const (
	DecisionAllow  Decision = "ALLOW"
	DecisionDeny   Decision = "DENY"
	DecisionReview Decision = "REVIEW"
)

// ValidationRequest is the input for transaction validation.
// Amount is expressed as a decimal value (e.g., 1000.00 for USD/BRL).
// Use NewValidationRequest() to construct - ensures validation and normalization.
type ValidationRequest struct {
	RequestID            uuid.UUID         `json:"requestId" validate:"required" swaggertype:"string" format:"uuid"`
	TransactionType      TransactionType   `json:"transactionType" validate:"required"`
	SubType              *string           `json:"subType,omitempty"`
	Amount               decimal.Decimal   `json:"amount" validate:"required" swaggertype:"string" example:"100.00"`
	Currency             string            `json:"currency" validate:"required"`
	TransactionTimestamp time.Time         `json:"transactionTimestamp" format:"date-time" validate:"required"`
	Account              AccountContext    `json:"account" validate:"required"`
	Segment              *SegmentContext   `json:"segment,omitempty"`
	Portfolio            *PortfolioContext `json:"portfolio,omitempty"`
	Merchant             *MerchantContext  `json:"merchant,omitempty"`
	Metadata             map[string]any    `json:"metadata,omitempty"`
}

// NewValidationRequest creates a new ValidationRequest with validation and normalization.
// Currency is normalized to uppercase and trimmed (auto-corrects case).
// SubType is trimmed if provided.
// Metadata is shallow-copied (top-level keys only) to detach from the original map.
// Note: nested maps/slices within metadata values remain shared references.
// Returns error if validation fails after normalization.
//
// Use this constructor when:
// - Building requests programmatically where currency normalization is desired
// - You want automatic currency case correction (e.g., "usd" → "USD")
//
// For strict post-JSON-parse validation without currency normalization, use NormalizeAndValidate() instead.
func NewValidationRequest(
	requestID uuid.UUID,
	transactionType TransactionType,
	subType *string,
	amount decimal.Decimal,
	currency string,
	transactionTimestamp time.Time,
	account AccountContext,
	segment *SegmentContext,
	portfolio *PortfolioContext,
	merchant *MerchantContext,
	metadata map[string]any,
) (*ValidationRequest, error) {
	// Normalize currency (uppercase and trim)
	normalizedCurrency := strings.ToUpper(strings.TrimSpace(currency))

	// Normalize subType if provided
	var normalizedSubType *string

	if subType != nil {
		trimmed := strings.TrimSpace(*subType)
		normalizedSubType = &trimmed
	}

	// Shallow copy of metadata to detach top-level map entries
	// Note: nested maps/slices share references with original (acceptable trade-off)
	var metadataCopy map[string]any
	if metadata != nil {
		metadataCopy = make(map[string]any, len(metadata))
		maps.Copy(metadataCopy, metadata)
	}

	// Defensive copy of nested context metadata maps
	var segmentCopy *SegmentContext
	if segment != nil {
		segmentCopy = &SegmentContext{
			ID:   segment.ID,
			Name: segment.Name,
		}
		if segment.Metadata != nil {
			segmentCopy.Metadata = make(map[string]any, len(segment.Metadata))
			maps.Copy(segmentCopy.Metadata, segment.Metadata)
		}
	}

	var portfolioCopy *PortfolioContext
	if portfolio != nil {
		portfolioCopy = &PortfolioContext{
			ID:   portfolio.ID,
			Name: portfolio.Name,
		}
		if portfolio.Metadata != nil {
			portfolioCopy.Metadata = make(map[string]any, len(portfolio.Metadata))
			maps.Copy(portfolioCopy.Metadata, portfolio.Metadata)
		}
	}

	var merchantCopy *MerchantContext
	if merchant != nil {
		merchantCopy = &MerchantContext{
			ID:       merchant.ID,
			Name:     merchant.Name,
			Category: merchant.Category,
			Country:  merchant.Country,
		}
		if merchant.Metadata != nil {
			merchantCopy.Metadata = make(map[string]any, len(merchant.Metadata))
			maps.Copy(merchantCopy.Metadata, merchant.Metadata)
		}
	}

	req := &ValidationRequest{
		RequestID:            requestID,
		TransactionType:      transactionType,
		SubType:              normalizedSubType,
		Amount:               amount,
		Currency:             normalizedCurrency,
		TransactionTimestamp: transactionTimestamp,
		Account:              account,
		Segment:              segmentCopy,
		Portfolio:            portfolioCopy,
		Merchant:             merchantCopy,
		Metadata:             metadataCopy,
	}

	// Validate after construction
	if err := req.Validate(); err != nil {
		return nil, err
	}

	return req, nil
}

// NormalizeAndValidate normalizes non-critical fields and validates the request in-place.
// This method is useful after JSON parsing where the struct is already constructed.
// SubType is trimmed and Metadata maps are defensively copied at all levels:
// - Top-level Metadata map is shallow-copied
// - Nested context metadata (Segment.Metadata, Portfolio.Metadata, Merchant.Metadata) are also shallow-copied
// Note: Values within metadata maps remain shared references if they are maps/slices themselves.
// Currency is NOT normalized - API enforces strict ISO 4217 uppercase validation (e.g., "usd" will fail).
// Returns error if validation fails after normalization.
//
// Atomicity: If validation fails, the receiver is NOT modified. Normalization is only applied
// after successful validation. This allows callers to safely retry or inspect the original values.
//
// Use this method when:
// - Validating after JSON deserialization where strict ISO 4217 uppercase currency is required
// - You want to enforce that clients send properly formatted currency codes
//
// For programmatic construction with automatic currency normalization, use NewValidationRequest() instead.
func (r *ValidationRequest) NormalizeAndValidate() error {
	// Prepare normalized values without mutating the receiver yet
	var normalizedSubType *string

	if r.SubType != nil {
		trimmed := strings.TrimSpace(*r.SubType)
		normalizedSubType = &trimmed
	}

	// Prepare shallow copy of top-level metadata
	var metadataCopy map[string]any
	if r.Metadata != nil {
		metadataCopy = make(map[string]any, len(r.Metadata))
		maps.Copy(metadataCopy, r.Metadata)
	}

	// Create temporary copy with normalized values for validation
	temp := *r
	temp.SubType = normalizedSubType
	temp.Metadata = metadataCopy

	// Deep copy nested context metadata to prevent shared references
	if temp.Segment != nil && temp.Segment.Metadata != nil {
		segmentMetaCopy := make(map[string]any, len(temp.Segment.Metadata))
		maps.Copy(segmentMetaCopy, temp.Segment.Metadata)

		segmentCopy := *temp.Segment
		segmentCopy.Metadata = segmentMetaCopy
		temp.Segment = &segmentCopy
	}

	if temp.Portfolio != nil && temp.Portfolio.Metadata != nil {
		portfolioMetaCopy := make(map[string]any, len(temp.Portfolio.Metadata))
		maps.Copy(portfolioMetaCopy, temp.Portfolio.Metadata)

		portfolioCopy := *temp.Portfolio
		portfolioCopy.Metadata = portfolioMetaCopy
		temp.Portfolio = &portfolioCopy
	}

	if temp.Merchant != nil && temp.Merchant.Metadata != nil {
		merchantMetaCopy := make(map[string]any, len(temp.Merchant.Metadata))
		maps.Copy(merchantMetaCopy, temp.Merchant.Metadata)

		merchantCopy := *temp.Merchant
		merchantCopy.Metadata = merchantMetaCopy
		temp.Merchant = &merchantCopy
	}

	// Validate on temp - if error, original r remains unchanged
	if err := temp.Validate(); err != nil {
		return err
	}

	// Only apply changes if validation succeeded (atomic commit)
	r.SubType = normalizedSubType
	r.Metadata = metadataCopy
	r.Segment = temp.Segment
	r.Portfolio = temp.Portfolio
	r.Merchant = temp.Merchant

	return nil
}

// LimitUsageDetail contains usage information for a checked limit.
// Amounts are expressed as decimal values.
// Note: RemainingAmount is calculated as (LimitAmount - CurrentUsage), not stored.
// Aligned with API Design v1.3.2 section 4.1.1 LimitUsage structure.
type LimitUsageDetail struct {
	LimitID     uuid.UUID       `json:"limitId" swaggertype:"string" format:"uuid"`
	LimitAmount decimal.Decimal `json:"limitAmount" swaggertype:"string" example:"1000.00"`
	// Scope is a human-readable string representation of the limit's scope
	// (e.g., "account:uuid" or "segment:uuid" or "global").
	// Per API Design v1.3.2 section 4.1.1.
	Scope string `json:"scope"`
	// Period indicates the type of limit (DAILY, MONTHLY, PER_TRANSACTION).
	// Named "period" per API Design v1.3.2 section 4.1.1.
	Period LimitType `json:"period" swaggertype:"string"`
	// CurrentUsage represents the PROJECTED usage after applying the transaction amount,
	// not the actual persisted counter value. This is calculated as:
	// (counter.CurrentUsage + input.Amount) for DAILY/MONTHLY limits, or 0 for PER_TRANSACTION.
	// When Exceeded=true, the counter was NOT incremented, but CurrentUsage still shows
	// what the usage would have been if the transaction were allowed.
	CurrentUsage decimal.Decimal `json:"currentUsage" swaggertype:"string" example:"500.00"`
	// AttemptedAmount is the transaction amount being validated.
	// Per API Design v1.3.2 section 4.1.1.
	AttemptedAmount decimal.Decimal `json:"attemptedAmount" swaggertype:"string" example:"100.00"`
	Exceeded        bool            `json:"exceeded"`

	// Internal fields for rollback operations - not serialized to JSON.
	// InternalLimitType stores the persistent limit type for rollback logic.
	// Used by RollbackUsage to skip PER_TRANSACTION limits (no persistent counters)
	// without needing to re-fetch the limit from the database.
	// Note: Period (above) is the API-facing field; this is for internal use only.
	InternalLimitType LimitType `json:"-"`
	// Scopes contains the limit's scopes, used by RollbackUsage to calculate
	// scopeKey without needing to re-fetch the limit from the database.
	// This eliminates N+1 queries during rollback operations.
	Scopes []Scope `json:"-"`
}

// ValidationResponse is the output of transaction validation.
// Embeds EvaluationResult to avoid field duplication.
// Aligned with TRD v1.2.4: arrays for matched/evaluated rules and limit details.
type ValidationResponse struct {
	ValidationID uuid.UUID `json:"validationId" swaggertype:"string" format:"uuid"`
	RequestID    uuid.UUID `json:"requestId" swaggertype:"string" format:"uuid"`
	EvaluationResult
	LimitUsageDetails []LimitUsageDetail `json:"limitUsageDetails"`
	ProcessingTimeMs  int64              `json:"processingTimeMs"`
}

// NewValidationResponse creates a ValidationResponse with initialized slices.
// Ensures JSON serialization produces [] instead of null for empty arrays.
// validationID is the server-generated unique identifier for the audit record.
func NewValidationResponse(validationID, requestID uuid.UUID, decision Decision) *ValidationResponse {
	return &ValidationResponse{
		ValidationID: validationID,
		RequestID:    requestID,
		EvaluationResult: EvaluationResult{
			Decision:         decision,
			MatchedRuleIDs:   []uuid.UUID{},
			EvaluatedRuleIDs: []uuid.UUID{},
			Reason:           "",
		},
		LimitUsageDetails: []LimitUsageDetail{},
	}
}

// IsValid checks if the decision is valid
func (d Decision) IsValid() bool {
	switch d {
	case DecisionAllow, DecisionDeny, DecisionReview:
		return true
	default:
		return false
	}
}

// String returns the string representation of the decision
func (d Decision) String() string {
	return string(d)
}

// Validate checks that all required fields in ValidationRequest are present and valid.
// Returns specific error constants for each validation failure.
func (r *ValidationRequest) Validate() error {
	if err := r.validateRequiredFields(); err != nil {
		return err
	}

	if err := r.validateOptionalFields(); err != nil {
		return err
	}

	if err := r.validateMerchant(); err != nil {
		return err
	}

	return r.validateMetadata()
}

func (r *ValidationRequest) validateRequiredFields() error {
	if r.RequestID == uuid.Nil {
		return constant.ErrValidationRequestIDRequired
	}

	if !r.TransactionType.IsValid() {
		return constant.ErrValidationInvalidTransactionType
	}

	if r.Amount.LessThanOrEqual(decimal.Zero) {
		return constant.ErrValidationAmountNonPositive
	}

	if r.Currency == "" {
		return constant.ErrValidationCurrencyRequired
	}

	if !pkg.IsValidCurrency(r.Currency) {
		return constant.ErrValidationInvalidCurrency
	}

	if r.TransactionTimestamp.IsZero() {
		return constant.ErrValidationTimestampRequired
	}

	maxAllowedTime := time.Now().Add(ClockSkewTolerance)
	if r.TransactionTimestamp.After(maxAllowedTime) {
		return constant.ErrValidationTimestampFuture
	}

	if r.Account.ID == uuid.Nil {
		return constant.ErrValidationAccountRequired
	}

	return nil
}

func (r *ValidationRequest) validateOptionalFields() error {
	if r.SubType != nil && len(*r.SubType) > MaxSubTypeLength {
		return constant.ErrValidationSubTypeTooLong
	}

	if r.Segment != nil && r.Segment.ID == uuid.Nil {
		return constant.ErrValidationSegmentIDRequired
	}

	if r.Portfolio != nil && r.Portfolio.ID == uuid.Nil {
		return constant.ErrValidationPortfolioIDRequired
	}

	if r.Account.Type != "" && !validAccountTypes[r.Account.Type] {
		return constant.ErrValidationInvalidAccountType
	}

	if r.Account.Status != "" && !validAccountStatuses[r.Account.Status] {
		return constant.ErrValidationInvalidAccountStatus
	}

	return nil
}

func (r *ValidationRequest) validateMerchant() error {
	if r.Merchant == nil {
		return nil
	}

	if r.Merchant.ID == uuid.Nil {
		return constant.ErrValidationMerchantIDRequired
	}

	if r.Merchant.Category != "" && !mccPattern.MatchString(r.Merchant.Category) {
		return constant.ErrValidationInvalidMerchantCategory
	}

	if r.Merchant.Country != "" && !countryCodePattern.MatchString(r.Merchant.Country) {
		return constant.ErrValidationInvalidMerchantCountry
	}

	return nil
}

func (r *ValidationRequest) validateMetadata() error {
	if r.Metadata == nil {
		return nil
	}

	if len(r.Metadata) > 50 {
		return constant.ErrMetadataEntriesExceeded
	}

	for key := range r.Metadata {
		if len(key) > 64 {
			return constant.ErrMetadataKeyLengthExceeded
		}

		if !metadataKeyPattern.MatchString(key) {
			return constant.ErrMetadataKeyInvalidChars
		}
	}

	return nil
}

// ToTransactionContext converts ValidationRequest to TransactionContext for rule evaluation.
// Used by T-011 (Validation Orchestration) to prepare input for T-008 (Rule Evaluation).
func (r *ValidationRequest) ToTransactionContext() *TransactionContext {
	return &TransactionContext{
		TransactionType:      r.TransactionType,
		SubType:              r.SubType,
		Amount:               r.Amount,
		Currency:             r.Currency,
		TransactionTimestamp: r.TransactionTimestamp,
		Account:              r.Account,
		Segment:              r.Segment,
		Portfolio:            r.Portfolio,
		Merchant:             r.Merchant,
		Metadata:             r.Metadata,
	}
}

// ToCheckLimitsInput converts ValidationRequest to CheckLimitsInput for limit checking.
// Used by T-011 (Validation Orchestration) to prepare input for T-010 (Limit Checking).
func (r *ValidationRequest) ToCheckLimitsInput() *CheckLimitsInput {
	input := &CheckLimitsInput{
		Amount:               r.Amount,
		Currency:             r.Currency,
		AccountID:            r.Account.ID,
		TransactionType:      &r.TransactionType,
		SubType:              r.SubType,
		TransactionTimestamp: r.TransactionTimestamp,
	}

	if r.Segment != nil {
		input.SegmentID = &r.Segment.ID
	}

	if r.Portfolio != nil {
		input.PortfolioID = &r.Portfolio.ID
	}

	return input
}

// ToTransactionScope builds a single Scope from the ValidationRequest context fields.
// This is used for scope matching in rule evaluation - rules with specific scopes
// should only evaluate against transactions that have matching scopes.
// Per API Design v1.3.1: a transaction has exactly one scope derived from its context
// objects (Account, Segment, Portfolio, Merchant, TransactionType).
func (r *ValidationRequest) ToTransactionScope() *Scope {
	scope := &Scope{
		AccountID:       &r.Account.ID,
		TransactionType: &r.TransactionType,
		SubType:         r.SubType,
	}

	if r.Segment != nil {
		scope.SegmentID = &r.Segment.ID
	}

	if r.Portfolio != nil {
		scope.PortfolioID = &r.Portfolio.ID
	}

	if r.Merchant != nil {
		scope.MerchantID = &r.Merchant.ID
	}

	return scope
}
