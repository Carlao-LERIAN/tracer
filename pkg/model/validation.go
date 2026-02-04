// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package model

import (
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

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
// Amount is expressed in the smallest currency unit (e.g., cents for USD/BRL).
// Example: $10.50 should be sent as 1050.
// Use NewValidationRequest() to construct - ensures validation and normalization.
type ValidationRequest struct {
	RequestID            uuid.UUID         `json:"requestId" validate:"required" swaggertype:"string" format:"uuid"`
	TransactionType      TransactionType   `json:"transactionType" validate:"required"`
	SubType              *string           `json:"subType,omitempty"`
	Amount               int64             `json:"amount" validate:"required"`
	Currency             string            `json:"currency" validate:"required"`
	TransactionTimestamp time.Time         `json:"transactionTimestamp" format:"date-time" validate:"required"`
	Account              AccountContext    `json:"account" validate:"required"`
	Segment              *SegmentContext   `json:"segment,omitempty"`
	Portfolio            *PortfolioContext `json:"portfolio,omitempty"`
	Merchant             *MerchantContext  `json:"merchant,omitempty"`
	Metadata             map[string]any    `json:"metadata,omitempty"`
}

// NewValidationRequest creates a new ValidationRequest with validation and normalization.
// Currency is normalized to uppercase and trimmed.
// SubType is trimmed if provided.
// Metadata is deep-copied to prevent external mutation.
// Returns error if validation fails after normalization.
//
// This constructor can be called in two ways:
// 1. After JSON parsing: construct from parsed struct to normalize and validate
// 2. Programmatically: construct from individual fields
func NewValidationRequest(
	requestID uuid.UUID,
	transactionType TransactionType,
	subType *string,
	amount int64,
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

	// Defensive copy of metadata to prevent external mutation
	var metadataCopy map[string]any
	if metadata != nil {
		metadataCopy = make(map[string]any, len(metadata))
		for k, v := range metadata {
			metadataCopy[k] = v
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
		Segment:              segment,
		Portfolio:            portfolio,
		Merchant:             merchant,
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
// SubType is trimmed and Metadata is deep-copied to prevent external mutation.
// Currency is NOT normalized - API enforces strict ISO 4217 uppercase validation.
// Returns error if validation fails after normalization.
func (r *ValidationRequest) NormalizeAndValidate() error {
	// Normalize subType if provided (trim whitespace)
	if r.SubType != nil {
		trimmed := strings.TrimSpace(*r.SubType)
		r.SubType = &trimmed
	}

	// Deep copy metadata to prevent external mutation
	if r.Metadata != nil {
		metadataCopy := make(map[string]any, len(r.Metadata))
		for k, v := range r.Metadata {
			metadataCopy[k] = v
		}

		r.Metadata = metadataCopy
	}

	// Validate (currency will be validated as-is, enforcing uppercase ISO 4217)
	return r.Validate()
}

// LimitUsageDetail contains usage information for a checked limit.
// Amounts are expressed in the smallest currency unit (e.g., cents).
// Note: RemainingAmount is calculated as (LimitAmount - CurrentUsage), not stored.
// Aligned with API Design v1.3.2 section 4.1.1 LimitUsage structure.
type LimitUsageDetail struct {
	LimitID     uuid.UUID `json:"limitId" swaggertype:"string" format:"uuid"`
	LimitAmount int64     `json:"limitAmount"`
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
	CurrentUsage int64 `json:"currentUsage"`
	// AttemptedAmount is the transaction amount being validated.
	// Per API Design v1.3.2 section 4.1.1.
	AttemptedAmount int64 `json:"attemptedAmount"`
	Exceeded        bool  `json:"exceeded"`

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

	if r.Amount <= 0 {
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
