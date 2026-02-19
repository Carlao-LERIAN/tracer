// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package constant

import (
	"errors"
)

// Error codes organized by category with spacing of 20 between groups
// to allow inserting new errors within each category.

var (
	// =============================================================================
	// General/Common Errors (TRC-0001 to TRC-0019)
	// =============================================================================
	ErrUnexpectedFieldsInTheRequest = errors.New("TRC-0001") // unexpected fields in request
	ErrMissingFieldsInRequest       = errors.New("TRC-0002") // missing required fields
	ErrBadRequest                   = errors.New("TRC-0003") // generic bad request
	ErrInternalServer               = errors.New("TRC-0004") // internal server error
	ErrCalculationFieldType         = errors.New("TRC-0005") // invalid calculation field type
	ErrInvalidQueryParameter        = errors.New("TRC-0006") // invalid query parameter
	ErrInvalidPathParameter         = errors.New("TRC-0007") // invalid path parameter
	ErrParentIDNotFound             = errors.New("TRC-0010") // parent ID not found
	ErrPayloadTooLarge              = errors.New("TRC-0011") // payload too large
	ErrContextCancelled             = errors.New("TRC-0012") // context cancelled / service unavailable

	// =============================================================================
	// Date/Time Errors (TRC-0020 to TRC-0039)
	// =============================================================================
	ErrInvalidDateFormat     = errors.New("TRC-0020") // invalid date format
	ErrInvalidFinalDate      = errors.New("TRC-0021") // invalid final date
	ErrDateRangeExceedsLimit = errors.New("TRC-0022") // date range exceeds limit
	ErrInvalidDateRange      = errors.New("TRC-0023") // invalid date range

	// =============================================================================
	// Pagination Errors (TRC-0040 to TRC-0059)
	// =============================================================================
	ErrPaginationLimitExceeded = errors.New("TRC-0040") // pagination limit exceeded
	ErrPaginationLimitInvalid  = errors.New("TRC-0041") // pagination limit must be positive
	ErrInvalidSortOrder        = errors.New("TRC-0042") // invalid sort order
	ErrInvalidSortColumn       = errors.New("TRC-0043") // sort column not in allowed list
	ErrInvalidCursor           = errors.New("TRC-0044") // invalid or corrupted pagination cursor
	ErrCursorWithSortParams    = errors.New("TRC-0045") // cursor and sort parameters are mutually exclusive

	// =============================================================================
	// Metadata Errors (TRC-0060 to TRC-0079)
	// =============================================================================
	ErrMetadataKeyLengthExceeded   = errors.New("TRC-0060") // metadata key length exceeded
	ErrMetadataValueLengthExceeded = errors.New("TRC-0061") // metadata value length exceeded
	ErrInvalidMetadataNesting      = errors.New("TRC-0062") // invalid metadata nesting
	ErrMetadataEntriesExceeded     = errors.New("TRC-0063") // metadata entries exceed maximum of 50
	ErrMetadataKeyInvalidChars     = errors.New("TRC-0064") // metadata key contains invalid characters

	// =============================================================================
	// Decision & CEL Expression Errors (TRC-0080 to TRC-0099)
	// =============================================================================
	ErrInvalidDecision          = errors.New("TRC-0080") // invalid decision value
	ErrReasonRequired           = errors.New("TRC-0081") // reason is required
	ErrInvalidDefaultDecision   = errors.New("TRC-0082") // invalid default decision value
	ErrExpressionSyntax         = errors.New("TRC-0083") // invalid CEL syntax
	ErrExpressionType           = errors.New("TRC-0084") // expression must return boolean
	ErrExpressionCostExceeded   = errors.New("TRC-0085") // cost limit exceeded (cost computed and above threshold)
	ErrExpressionEvaluation     = errors.New("TRC-0086") // runtime evaluation error
	ErrExpressionProgram        = errors.New("TRC-0087") // program creation failed (compilation phase)
	ErrExpressionCostEstimation = errors.New("TRC-0088") // failed to estimate expression cost

	// =============================================================================
	// Rule Errors (TRC-0100 to TRC-0119)
	// =============================================================================
	ErrRuleNotFound            = errors.New("TRC-0100") // rule not found by ID
	ErrRuleNameAlreadyExists   = errors.New("TRC-0101") // rule name must be unique
	ErrRuleInvalidStatus       = errors.New("TRC-0102") // invalid rule status transition
	ErrRuleEvaluationFailed    = errors.New("TRC-0103") // rule evaluation failed
	ErrExpressionNotModifiable = errors.New("TRC-0104") // expression cannot be modified for non-DRAFT rules
	ErrRuleNilInput            = errors.New("TRC-0105") // rule input cannot be nil
	ErrRuleNameRequired        = errors.New("TRC-0106") // rule name is required
	ErrRuleNameTooLong         = errors.New("TRC-0107") // rule name exceeds max length (255)
	ErrRuleExpressionRequired  = errors.New("TRC-0108") // rule expression is required
	ErrRuleExpressionTooLong   = errors.New("TRC-0109") // rule expression exceeds max length (5000)
	ErrRuleInvalidAction       = errors.New("TRC-0110") // action must be one of [ALLOW, DENY, REVIEW]
	ErrRuleInvalidScope        = errors.New("TRC-0111") // scope must have at least one field set
	ErrRuleDescriptionTooLong  = errors.New("TRC-0112") // rule description exceeds max length (1000)
	ErrRuleScopesTooMany       = errors.New("TRC-0113") // rule scopes exceed maximum (100)
	ErrRuleInvalidTransition   = errors.New("TRC-0114") // status transition not allowed

	// =============================================================================
	// Limit Errors (TRC-0120 to TRC-0139)
	// =============================================================================
	ErrLimitNotFound                = errors.New("TRC-0120") // limit not found by ID
	ErrLimitInvalidStatusChange     = errors.New("TRC-0121") // invalid limit status transition
	ErrLimitInvalidType             = errors.New("TRC-0122") // invalid limit type
	ErrLimitInvalidMaxAmount        = errors.New("TRC-0123") // maxAmount must be positive
	ErrLimitInvalidCurrency         = errors.New("TRC-0124") // currency must be valid ISO 4217
	ErrLimitInvalidScope            = errors.New("TRC-0125") // scope validation failed
	ErrLimitNameRequired            = errors.New("TRC-0126") // limit name is required
	ErrLimitNameTooLong             = errors.New("TRC-0127") // limit name exceeds max length
	ErrLimitAlreadyDeleted          = errors.New("TRC-0128") // limit is already in DELETED state
	ErrLimitNameInvalidChars        = errors.New("TRC-0129") // limit name contains invalid characters
	ErrLimitDescriptionInvalidChars = errors.New("TRC-0130") // limit description contains invalid characters
	ErrLimitInvalidID               = errors.New("TRC-0131") // limit ID is invalid or nil
	ErrLimitDescriptionTooLong      = errors.New("TRC-0132") // limit description exceeds max length
	ErrLimitInvalidStatusFilter     = errors.New("TRC-0133") // invalid status filter value
	ErrLimitInvalidTypeFilter       = errors.New("TRC-0134") // invalid limitType filter value
	ErrLimitDeletedAtInvariant      = errors.New("TRC-0135") // DeletedAt must be set iff status is DELETED
	ErrLimitCheckFailed             = errors.New("TRC-0136") // limit check failed
	ErrLimitNilInput                = errors.New("TRC-0137") // limit input cannot be nil
	ErrLimitImmutableField          = errors.New("TRC-0138") // cannot modify immutable field (limitType, currency)

	// =============================================================================
	// Audit Event Errors (TRC-0140 to TRC-0159)
	// =============================================================================
	ErrAuditEventNotFound            = errors.New("TRC-0140") // audit event not found
	ErrInvalidAuditEventFilters      = errors.New("TRC-0141") // invalid audit event filter parameters
	ErrAuditEventInvalidType         = errors.New("TRC-0142") // invalid audit event type
	ErrAuditEventInvalidAction       = errors.New("TRC-0143") // invalid audit action
	ErrAuditEventInvalidResult       = errors.New("TRC-0144") // invalid audit result
	ErrAuditEventResourceIDRequired  = errors.New("TRC-0145") // resource ID is required
	ErrAuditEventInvalidResourceType = errors.New("TRC-0146") // invalid resource type
	ErrAuditEventActorIDRequired     = errors.New("TRC-0147") // actor ID is required
	ErrAuditEventActorTypeInvalid    = errors.New("TRC-0148") // actor type must be 'user' or 'system'

	// =============================================================================
	// UsageCounter Errors (TRC-0160 to TRC-0179)
	// =============================================================================
	ErrUsageCounterOverflow             = errors.New("TRC-0160") // usage counter would overflow
	ErrUsageCounterLimitIDRequired      = errors.New("TRC-0161") // usage counter limitID is required
	ErrUsageCounterScopeKeyRequired     = errors.New("TRC-0162") // usage counter scopeKey is required
	ErrUsageCounterPeriodKeyRequired    = errors.New("TRC-0163") // usage counter periodKey is required
	ErrUsageCounterCurrentUsageNegative = errors.New("TRC-0164") // usage counter currentUsage must be non-negative
	ErrUsageCounterIncrementNonNegative = errors.New("TRC-0165") // increment amount must be non-negative
	ErrUsageCounterNotFound             = errors.New("TRC-0166") // usage counter not found
	ErrUsageCounterDecrementNonNegative = errors.New("TRC-0168") // decrement amount must be non-negative

	// =============================================================================
	// CheckLimits Errors (TRC-0180 to TRC-0199)
	// =============================================================================
	ErrCheckLimitsInvalidAmount          = errors.New("TRC-0180") // check limits amount must be positive
	ErrCheckLimitsInvalidCurrency        = errors.New("TRC-0181") // check limits currency must be valid ISO 4217
	ErrCheckLimitsUnknownLimitType       = errors.New("TRC-0183") // unknown limit type for period key calculation
	ErrCheckLimitsInvalidTimestamp       = errors.New("TRC-0184") // check limits timestamp must not be zero
	ErrCheckLimitsNilInput               = errors.New("TRC-0185") // check limits input cannot be nil
	ErrCheckLimitsInvalidAccountID       = errors.New("TRC-0186") // check limits accountId is required
	ErrCheckLimitsInvalidTransactionType = errors.New("TRC-0187") // check limits transactionType must be valid
	ErrCheckLimitsInvalidSubType         = errors.New("TRC-0188") // check limits subType exceeds maximum length

	// =============================================================================
	// Constructor/Dependency Injection Errors (TRC-0200 to TRC-0219)
	// =============================================================================
	ErrLimitCheckerNilLimitRepo        = errors.New("TRC-0200") // limit checker: limit repository cannot be nil
	ErrLimitCheckerNilUsageCounterRepo = errors.New("TRC-0201") // limit checker: usage counter repository cannot be nil

	// =============================================================================
	// Validation Request Errors (TRC-0220 to TRC-0249)
	// =============================================================================
	ErrValidationRequestIDRequired       = errors.New("TRC-0220") // requestId is required
	ErrValidationInvalidTransactionType  = errors.New("TRC-0221") // invalid transactionType
	ErrValidationAmountNonPositive       = errors.New("TRC-0222") // amount must be positive
	ErrValidationCurrencyRequired        = errors.New("TRC-0223") // currency is required
	ErrValidationInvalidCurrency         = errors.New("TRC-0224") // currency must be valid ISO 4217
	ErrValidationTimestampRequired       = errors.New("TRC-0225") // timestamp is required
	ErrValidationTimestampFuture         = errors.New("TRC-0226") // timestamp cannot be in the future
	ErrValidationAccountRequired         = errors.New("TRC-0227") // account is required
	ErrValidationTimeout                 = errors.New("TRC-0229") // validation timeout
	ErrValidationSegmentIDRequired       = errors.New("TRC-0230") // segmentId is required when segment is provided
	ErrValidationPortfolioIDRequired     = errors.New("TRC-0231") // portfolioId is required when portfolio is provided
	ErrValidationSubTypeTooLong          = errors.New("TRC-0232") // subType exceeds maximum length of 50 characters
	ErrValidationInvalidAccountType      = errors.New("TRC-0233") // account.type must be checking, savings, or credit
	ErrValidationInvalidAccountStatus    = errors.New("TRC-0234") // account.status must be active, suspended, or closed
	ErrValidationInvalidMerchantCategory = errors.New("TRC-0235") // merchant.category must be 4-digit MCC code
	ErrValidationInvalidMerchantCountry  = errors.New("TRC-0236") // merchant.country must be ISO 3166-1 alpha-2
	ErrValidationMerchantIDRequired      = errors.New("TRC-0237") // merchant.id is required when merchant is provided

	// =============================================================================
	// Audit / Transaction Validation Errors (TRC-0250 to TRC-0269)
	// =============================================================================
	ErrInvalidTransactionValidationFilters = errors.New("TRC-0250") // invalid transaction validation filter parameters
	ErrTransactionValidationNotFound       = errors.New("TRC-0251") // transaction validation record not found
	ErrListValidationsTimeout              = errors.New("TRC-0252") // list validations query timeout (deadline exceeded)

	// =============================================================================
	// Transaction Validation Constructor Errors (TRC-0270 to TRC-0279)
	// =============================================================================
	ErrTransactionValidationIDRequired        = errors.New("TRC-0270") // validation ID is required
	ErrTransactionValidationCreatedAtRequired = errors.New("TRC-0271") // createdAt is required

	// =============================================================================
	// Cache Errors (TRC-0280 to TRC-0299)
	// =============================================================================
	ErrRuleCacheWarmUpFailed = errors.New("TRC-0280") // rule cache warm-up failed
	ErrRuleCacheNotReady     = errors.New("TRC-0281") // rule cache is not ready
)

// Error code constants for HTTP responses.
const (
	CodeBadRequest             = "TRC-0003"
	CodeInternalServer         = "TRC-0004"
	CodePayloadTooLarge        = "TRC-0011"
	CodeContextCancelled       = "TRC-0012"
	CodeRuleEvaluationError    = "TRC-0103"
	CodeLimitCheckError        = "TRC-0136"
	CodeValidationTimeout      = "TRC-0229"
	CodeListValidationsTimeout = "TRC-0252"
	CodeCacheNotReady          = "TRC-0281"
)
