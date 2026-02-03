// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package in

//go:generate mockgen -source=validation_handler.go -destination=mocks/validation_service_mock.go -package=mocks

import (
	"context"
	"errors"
	"net/http"
	"strings"

	libCommons "github.com/LerianStudio/lib-commons/v2/commons"
	libHTTP "github.com/LerianStudio/lib-commons/v2/commons/net/http"
	libOpentelemetry "github.com/LerianStudio/lib-commons/v2/commons/opentelemetry"
	"github.com/gofiber/fiber/v2"
	"go.opentelemetry.io/otel/trace"

	"tracer/pkg/constant"
	"tracer/pkg/logging"
	"tracer/pkg/model"
	pkgHTTP "tracer/pkg/net/http"
)

// maxPayloadSize is the maximum allowed request body size in bytes (100KB).
const maxPayloadSize = 100 * 1024

// ValidationService defines the interface for validation operations.
// Interface defined locally per Ring pattern.
type ValidationService interface {
	Validate(ctx context.Context, request *model.ValidationRequest) (*model.ValidationResponse, error)
}

// ValidationHandler handles HTTP requests for transaction validation.
type ValidationHandler struct {
	service ValidationService
}

// NewValidationHandler creates a new validation handler.
func NewValidationHandler(service ValidationService) *ValidationHandler {
	return &ValidationHandler{
		service: service,
	}
}

// Validate godoc
//
//	@Summary		Validate a transaction
//	@Description	Validates a transaction against configured rules and limits.
//	@ID				validateTransaction
//	@Tags			validations
//	@Accept			json
//	@Produce		json
//	@Security		ApiKeyAuth
//	@Param			request		body		model.ValidationRequest	true	"Validation request"
//	@Success		200			{object}	model.ValidationResponse	"Validation result"
//	@Failure		400			{object}	api.ErrorResponse	"Invalid input"
//	@Failure		401			{object}	api.ErrorResponse	"Unauthorized"
//	@Failure		413			{object}	api.ErrorResponse	"Payload too large (exceeds 100KB)"
//	@Failure		500			{object}	api.ErrorResponse	"Internal server error"
//	@Failure		503			{object}	api.ErrorResponse	"Service unavailable"
//	@Failure		504			{object}	api.ErrorResponse	"Gateway timeout"
//	@Router			/v1/validations [post]
func (h *ValidationHandler) Validate(c *fiber.Ctx) error {
	ctx := c.UserContext()
	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "handler.validations.validate")
	defer span.End()

	logger = logging.WithTrace(ctx, logger)

	// Check payload size (technical error - use HandleSpanError)
	if len(c.Body()) > maxPayloadSize {
		logger.WithFields(
			"operation", "handler.validations.validate",
			"payload_size", len(c.Body()),
			"max_size", maxPayloadSize,
		).Warn("Payload too large")

		libOpentelemetry.HandleSpanError(&span, "Payload exceeds size limit", constant.ErrPayloadTooLarge)

		return libHTTP.JSONResponse(c, http.StatusRequestEntityTooLarge, libCommons.Response{
			Code:    constant.CodePayloadTooLarge,
			Title:   "Payload Too Large",
			Message: "payload too large: exceeds 100KB limit",
		})
	}

	var request model.ValidationRequest
	if err := c.BodyParser(&request); err != nil {
		logger.WithFields(
			"operation", "handler.validations.validate",
			"error.message", err.Error(),
		).Warn("Failed to parse request body")

		libOpentelemetry.HandleSpanError(&span, "Failed to parse request body", err)

		return pkgHTTP.BadRequestWithMessage(c, "TRC-0003", "Bad Request", h.parseErrorToUserMessage(err))
	}

	// Validate request (business error - use HandleSpanBusinessErrorEvent)
	if err := request.Validate(); err != nil {
		logger.WithFields(
			"operation", "handler.validations.validate",
			"error.message", err.Error(),
		).Warn("Request validation failed")

		libOpentelemetry.HandleSpanBusinessErrorEvent(&span, "Request validation failed", err)

		return h.handleValidationInputError(c, err)
	}

	logger.WithFields(
		"operation", "handler.validations.validate",
		"request.id", request.RequestID.String(),
		"request.amount", request.Amount,
		"request.currency", request.Currency,
		"request.transaction_type", string(request.TransactionType),
	).Info("Processing validation request")

	err := libOpentelemetry.SetSpanAttributesFromStruct(&span, "validation_request", map[string]any{
		"request_id":       request.RequestID.String(),
		"transaction_type": string(request.TransactionType),
		"amount":           request.Amount,
		"currency":         request.Currency,
	})
	if err != nil {
		libOpentelemetry.HandleSpanError(&span, "Failed to set span attributes", err)
	}

	// Call validation service
	response, err := h.service.Validate(ctx, &request)
	if err != nil {
		return h.handleValidationError(c, &span, err)
	}

	logger.WithFields(
		"operation", "handler.validations.validate",
		"request.id", request.RequestID.String(),
		"decision", string(response.Decision),
		"processing_time_ms", response.ProcessingTimeMs,
	).Info("Validation completed")

	return libHTTP.OK(c, response)
}

// validationErrorMapping maps validation errors to their specific error codes and messages.
type validationErrorMapping struct {
	code    string
	message string
}

// validationErrorMappings maps validation errors to specific TRC codes and messages.
var validationErrorMappings = map[error]validationErrorMapping{
	constant.ErrValidationRequestIDRequired:       {code: "TRC-0220", message: "requestId is required"},
	constant.ErrValidationInvalidTransactionType:  {code: "TRC-0221", message: "transactionType must be one of [CARD, WIRE, PIX, CRYPTO]"},
	constant.ErrValidationAmountNonPositive:       {code: "TRC-0222", message: "amount must be positive"},
	constant.ErrValidationCurrencyRequired:        {code: "TRC-0223", message: "currency is required"},
	constant.ErrValidationInvalidCurrency:         {code: "TRC-0224", message: "currency must be valid ISO 4217 code (e.g., BRL, USD)"},
	constant.ErrValidationTimestampRequired:       {code: "TRC-0225", message: "transactionTimestamp is required"},
	constant.ErrValidationTimestampFuture:         {code: "TRC-0226", message: "transactionTimestamp cannot be in the future"},
	constant.ErrValidationAccountRequired:         {code: "TRC-0227", message: "account is required"},
	constant.ErrValidationSegmentIDRequired:       {code: "TRC-0230", message: "segment.id is required when segment is provided"},
	constant.ErrValidationPortfolioIDRequired:     {code: "TRC-0231", message: "portfolio.id is required when portfolio is provided"},
	constant.ErrValidationSubTypeTooLong:          {code: "TRC-0232", message: "subType exceeds maximum length of 50 characters"},
	constant.ErrValidationInvalidAccountType:      {code: "TRC-0233", message: "account.type must be one of: checking, savings, credit"},
	constant.ErrValidationInvalidAccountStatus:    {code: "TRC-0234", message: "account.status must be one of: active, suspended, closed"},
	constant.ErrValidationInvalidMerchantCategory: {code: "TRC-0235", message: "merchant.category must be a 4-digit MCC code"},
	constant.ErrValidationInvalidMerchantCountry:  {code: "TRC-0236", message: "merchant.country must be ISO 3166-1 alpha-2 code (e.g., BR, US)"},
	constant.ErrValidationMerchantIDRequired:      {code: "TRC-0237", message: "merchant.id is required when merchant object is provided"},
	constant.ErrMetadataEntriesExceeded:           {code: "TRC-0063", message: "metadata exceeds maximum of 50 entries"},
	constant.ErrMetadataKeyLengthExceeded:         {code: "TRC-0060", message: "metadata key exceeds maximum length of 64 characters"},
	constant.ErrMetadataKeyInvalidChars:           {code: "TRC-0064", message: "metadata key contains invalid characters (only alphanumeric and underscore allowed)"},
}

// handleValidationInputError converts input validation errors to appropriate HTTP responses.
// Maps error codes to human-readable messages with field names for better debugging.
func (h *ValidationHandler) handleValidationInputError(c *fiber.Ctx, err error) error {
	for knownErr, mapping := range validationErrorMappings {
		if errors.Is(err, knownErr) {
			return pkgHTTP.BadRequestWithMessage(c, mapping.code, "Validation Error", mapping.message)
		}
	}

	logger, _, _, _ := libCommons.NewTrackingFromContext(c.UserContext()) //nolint:dogsled // only logger needed
	logger.WithFields("error.message", err.Error()).Warn("Unhandled validation input error")

	return pkgHTTP.BadRequestWithMessage(c, "TRC-0001", "Validation Error", "invalid request")
}

// parseErrorToUserMessage converts JSON parsing errors to user-friendly messages.
// Identifies the field that failed parsing based on error content.
func (h *ValidationHandler) parseErrorToUserMessage(err error) string {
	errMsg := err.Error()

	// UUID parsing errors - generic message since Fiber doesn't include field name
	if strings.Contains(errMsg, "invalid UUID") {
		return "invalid UUID format in request (check requestId, account.id, segment.id, portfolio.id, merchant.id)"
	}

	// Time parsing errors
	if strings.Contains(errMsg, "parsing time") || strings.Contains(errMsg, "cannot parse") {
		return "timestamp: invalid format (expected RFC3339)"
	}

	// Return generic message for other cases to avoid leaking parser details
	return "invalid request body"
}

// handleValidationError converts service errors to appropriate HTTP responses.
func (h *ValidationHandler) handleValidationError(c *fiber.Ctx, span *trace.Span, err error) error {
	switch {
	case errors.Is(err, constant.ErrValidationTimeout):
		libOpentelemetry.HandleSpanError(span, "Validation timeout", err)

		return libHTTP.JSONResponse(c, http.StatusGatewayTimeout, libCommons.Response{
			Code:    constant.CodeValidationTimeout,
			Title:   "Gateway Timeout",
			Message: "validation timeout",
		})
	case errors.Is(err, context.Canceled):
		libOpentelemetry.HandleSpanError(span, "Context cancelled", err)

		return libHTTP.JSONResponse(c, http.StatusServiceUnavailable, libCommons.Response{
			Code:    constant.CodeContextCancelled,
			Title:   "Service Unavailable",
			Message: "request cancelled",
		})
	case errors.Is(err, constant.ErrRuleEvaluationFailed):
		libOpentelemetry.HandleSpanError(span, "Rule evaluation failed", err)

		return libHTTP.InternalServerError(c, constant.CodeRuleEvaluationError, "Internal Server Error", "rule evaluation failed")
	case errors.Is(err, constant.ErrLimitCheckFailed):
		libOpentelemetry.HandleSpanError(span, "Limit check failed", err)

		return libHTTP.InternalServerError(c, constant.CodeLimitCheckError, "Internal Server Error", "limit check failed")
	default:
		libOpentelemetry.HandleSpanError(span, "Validation failed", err)

		return libHTTP.InternalServerError(c, constant.CodeInternalServer, "Internal Server Error", "validation processing failed")
	}
}
