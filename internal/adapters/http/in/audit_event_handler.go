// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package in

//go:generate mockgen -source=audit_event_handler.go -destination=audit_event_service_mock.go -package=in

import (
	"context"
	"errors"

	libCommons "github.com/LerianStudio/lib-commons/v2/commons"
	libHTTP "github.com/LerianStudio/lib-commons/v2/commons/net/http"
	libOpentelemetry "github.com/LerianStudio/lib-commons/v2/commons/opentelemetry"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"

	"tracer/pkg/constant"
	"tracer/pkg/logging"
	"tracer/pkg/model"
	pkgHTTP "tracer/pkg/net/http"
)

// AuditEventService defines the interface for audit event query operations.
// Interface defined locally per Ring pattern.
type AuditEventService interface {
	GetAuditEvent(ctx context.Context, eventID uuid.UUID) (*model.AuditEvent, error)
	ListAuditEvents(ctx context.Context, filters *model.AuditEventFilters) (*model.ListAuditEventsResult, error)
	VerifyHashChain(ctx context.Context, eventID uuid.UUID) (*model.HashChainVerificationResult, error)
}

// AuditEventHandler handles HTTP requests for audit event operations.
type AuditEventHandler struct {
	service AuditEventService
}

// NewAuditEventHandler creates a new audit event handler.
func NewAuditEventHandler(service AuditEventService) *AuditEventHandler {
	return &AuditEventHandler{
		service: service,
	}
}

// ListAuditEvents godoc
//
//	@Summary		List audit events
//	@Description	Lists audit events with filters and cursor-based pagination. SOX/GLBA compliance endpoint.
//	@ID				listAuditEvents
//	@Tags			audit
//	@Accept			json
//	@Produce		json
//	@Security		ApiKeyAuth
//	@Param			startDate		query		string	false	"Start date (RFC3339 format)	Format(date-time)"
//	@Param			endDate			query		string	false	"End date (RFC3339 format)		Format(date-time)"
//	@Param			eventType		query		string	false	"Filter by event type"	Enums(TRANSACTION_VALIDATED, RULE_CREATED, RULE_UPDATED, RULE_ACTIVATED, RULE_DEACTIVATED, RULE_DRAFTED, RULE_DELETED, LIMIT_CREATED, LIMIT_UPDATED, LIMIT_ACTIVATED, LIMIT_DEACTIVATED, LIMIT_DRAFTED, LIMIT_DELETED)
//	@Param			action			query		string	false	"Filter by action"	Enums(VALIDATE, CREATE, UPDATE, DELETE, ACTIVATE, DEACTIVATE, DRAFT)
//	@Param			result			query		string	false	"Filter by result"	Enums(SUCCESS, FAILED, ALLOW, DENY, REVIEW)
//	@Param			resourceType	query		string	false	"Filter by resource type"	Enums(transaction, rule, limit)
//	@Param			resourceId		query		string	false	"Filter by resource ID (UUID)"	Format(uuid)
//	@Param			actorType		query		string	false	"Filter by actor type"	Enums(user, system)
//	@Param			actorId			query		string	false	"Filter by actor ID"
//	@Param			accountId		query		string	false	"Filter by account ID (UUID)"	Format(uuid)
//	@Param			segmentId		query		string	false	"Filter by segment ID (UUID)"	Format(uuid)
//	@Param			portfolioId		query		string	false	"Filter by portfolio ID (UUID)"	Format(uuid)
//	@Param			transactionType	query		string	false	"Filter by transaction type"
//	@Param			matchedRuleId	query		string	false	"Filter by matched rule ID (UUID)"	Format(uuid)
//	@Param			limit		    query		int		false	"Max items per page (1-1000, default: 100)"	minimum(1)	maximum(1000)
//	@Param			cursor  		query		string	false	"Pagination token (empty for first page)"
//	@Param			sortBy			query		string	false	"Sort field"	Enums(createdAt, eventType)
//	@Param			sortOrder		query		string	false	"Sort direction"	Enums(ASC, DESC)
//	@Success		200				{object}	ListAuditEventsResponse	"Audit events listed successfully"
//	@Failure		400				{object}	api.ErrorResponse	"Invalid parameters"
//	@Failure		401				{object}	api.ErrorResponse	"Unauthorized"
//	@Failure		500				{object}	api.ErrorResponse	"Internal server error"
//	@Router			/v1/audit-events [get]
func (h *AuditEventHandler) ListAuditEvents(c *fiber.Ctx) error {
	ctx := c.UserContext()
	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "handler.audit_event.list")
	defer span.End()

	logger = logging.WithTrace(ctx, logger)

	// Parse query parameters into input struct
	var input ListAuditEventsInput

	if err := c.QueryParser(&input); err != nil {
		libOpentelemetry.HandleSpanError(&span, "Failed to parse query parameters", err)
		return pkgHTTP.BadRequestWithMessage(c, "TRC-0006", "Invalid Query Parameter", "Invalid query parameters")
	}

	// Validate before applying defaults to ensure fail-fast behavior
	if err := input.Validate(); err != nil {
		libOpentelemetry.HandleSpanBusinessErrorEvent(&span, "Validation failed", err)
		logger.WithFields(
			"operation", "handler.audit_event.list",
			"error", err.Error(),
		).Error("Failed to validate request parameters")

		// Check if it's a ValidationError with a specific code (e.g., TRC-0043)
		var valErr *ValidationError
		if errors.As(err, &valErr) && valErr.Code != "" {
			return pkgHTTP.BadRequestWithMessage(c, valErr.Code, "Validation Error", valErr.Message)
		}

		return pkgHTTP.BadRequestWithMessage(c, "TRC-0001", "Validation Error", err.Error())
	}

	// Apply defaults after validation
	input.SetDefaults()

	logger.WithFields(
		"operation", "handler.audit_event.list",
		"list.limit", input.Limit,
		"list.cursor", input.Cursor,
		"list.sort_by", input.SortBy,
		"list.sort_order", input.SortOrder,
	).Info("Listing audit events")

	// Convert to service filters
	filters, err := toAuditEventFilters(&input)
	if err != nil {
		libOpentelemetry.HandleSpanBusinessErrorEvent(&span, "Invalid filters", err)
		logger.WithFields(
			"operation", "handler.audit_event.list",
			"error", err.Error(),
		).Error("Failed to convert filter parameters")

		var valErr *ValidationError
		if errors.As(err, &valErr) && valErr.Code != "" {
			return pkgHTTP.BadRequestWithMessage(c, valErr.Code, "Validation Error", valErr.Message)
		}

		return pkgHTTP.BadRequestWithMessage(c, "TRC-0006", "Invalid Query Parameter", err.Error())
	}

	result, err := h.service.ListAuditEvents(ctx, filters)
	if err != nil {
		return handleAuditEventServiceError(c, &span, err)
	}

	// Convert to response
	response := toListAuditEventsResponse(result)

	logger.WithFields(
		"operation", "handler.audit_event.list",
		"list.count", len(response.AuditEvents),
		"list.has_more", response.HasMore,
	).Info("Audit events listed")

	return libHTTP.OK(c, response)
}

// GetAuditEvent godoc
//
//	@Summary		Get an audit event by ID
//	@Description	Retrieves a single audit event by its unique identifier. SOX/GLBA compliance endpoint.
//	@ID				getAuditEvent
//	@Tags			audit
//	@Accept			json
//	@Produce		json
//	@Security		ApiKeyAuth
//	@Param			id			path		string	true	"Event ID (UUID)"	Format(uuid)
//	@Success		200			{object}	model.AuditEvent		"Audit event retrieved successfully"
//	@Failure		400			{object}	api.ErrorResponse	"Invalid event ID"
//	@Failure		401			{object}	api.ErrorResponse	"Unauthorized"
//	@Failure		404			{object}	api.ErrorResponse	"Audit event not found"
//	@Failure		500			{object}	api.ErrorResponse	"Internal server error"
//	@Router			/v1/audit-events/{id} [get]
func (h *AuditEventHandler) GetAuditEvent(c *fiber.Ctx) error {
	ctx := c.UserContext()
	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "handler.audit_event.get")
	defer span.End()

	logger = logging.WithTrace(ctx, logger)

	// Parse event ID from path
	idParam := c.Params("id")

	eventID, err := uuid.Parse(idParam)
	if err != nil {
		libOpentelemetry.HandleSpanBusinessErrorEvent(&span, "Invalid event ID", err)
		return pkgHTTP.BadRequestWithMessage(c, "TRC-0007", "Invalid Path Parameter", "Invalid event ID format")
	}

	logger.WithFields(
		"operation", "handler.audit_event.get",
		"audit_event.id", eventID.String(),
	).Info("Getting audit event")

	result, err := h.service.GetAuditEvent(ctx, eventID)
	if err != nil {
		return handleAuditEventServiceError(c, &span, err)
	}

	logger.WithFields(
		"operation", "handler.audit_event.get",
		"audit_event.id", result.EventID.String(),
		"audit_event.type", result.EventType,
	).Info("Audit event retrieved")

	return libHTTP.OK(c, result)
}

// VerifyHashChain godoc
//
//	@Summary		Verify audit event hash chain integrity
//	@Description	Verifies the integrity of the audit event hash chain up to a specific event. Detects tampering attempts. SOX/GLBA compliance endpoint.
//	@ID				verifyAuditEvent
//	@Tags			audit
//	@Accept			json
//	@Produce		json
//	@Security		ApiKeyAuth
//	@Param			id			path		string	true	"Event ID to verify up to (UUID)"	Format(uuid)
//	@Success		200			{object}	model.HashChainVerificationResult	"Hash chain verification completed"
//	@Failure		400			{object}	api.ErrorResponse	"Invalid event ID"
//	@Failure		401			{object}	api.ErrorResponse	"Unauthorized"
//	@Failure		404			{object}	api.ErrorResponse	"Event not found"
//	@Failure		500			{object}	api.ErrorResponse	"Internal server error"
//	@Router			/v1/audit-events/{id}/verify [get]
func (h *AuditEventHandler) VerifyHashChain(c *fiber.Ctx) error {
	ctx := c.UserContext()
	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "handler.audit_event.verify_chain")
	defer span.End()

	logger = logging.WithTrace(ctx, logger)

	// Parse event ID from path parameter
	eventIDParam := c.Params("id")

	eventID, err := uuid.Parse(eventIDParam)
	if err != nil {
		libOpentelemetry.HandleSpanBusinessErrorEvent(&span, "Invalid event ID", err)
		return pkgHTTP.BadRequestWithMessage(c, "TRC-0007", "Invalid Path Parameter", "Invalid event ID format")
	}

	logger.WithFields(
		"operation", "handler.audit_event.verify_chain",
		"event_id", eventID.String(),
	).Info("Verifying hash chain")

	result, err := h.service.VerifyHashChain(ctx, eventID)
	if err != nil {
		return handleAuditEventServiceError(c, &span, err)
	}

	logger.WithFields(
		"operation", "handler.audit_event.verify_chain",
		"is_valid", result.IsValid,
		"total_checked", result.TotalChecked,
	).Info("Hash chain verification completed")

	return libHTTP.OK(c, result)
}

// handleAuditEventServiceError converts service errors to appropriate HTTP responses.
func handleAuditEventServiceError(c *fiber.Ctx, span *trace.Span, err error) error {
	switch {
	case errors.Is(err, constant.ErrAuditEventNotFound):
		libOpentelemetry.HandleSpanBusinessErrorEvent(span, "Audit event not found", err)
		return libHTTP.NotFound(c, "TRC-0140", "Not Found", "Audit event not found")
	case errors.Is(err, constant.ErrInvalidAuditEventFilters):
		libOpentelemetry.HandleSpanBusinessErrorEvent(span, "Invalid filters", err)

		return pkgHTTP.BadRequestWithMessage(c, "TRC-0141", "Bad Request", "Invalid audit event filters")
	case errors.Is(err, constant.ErrInvalidCursor):
		libOpentelemetry.HandleSpanBusinessErrorEvent(span, "Invalid cursor", err)

		return pkgHTTP.BadRequestWithMessage(c, "TRC-0044", "Bad Request", "Invalid pagination cursor")
	case errors.Is(err, constant.ErrInvalidSortColumn):
		libOpentelemetry.HandleSpanBusinessErrorEvent(span, "Invalid sort column", err)

		return pkgHTTP.BadRequestWithMessage(c, "TRC-0043", "Bad Request", "Invalid sort column")
	default:
		libOpentelemetry.HandleSpanError(span, "Operation failed", err)
		return libHTTP.InternalServerError(c, "TRC-0004", "Internal Server Error", "An unexpected error occurred")
	}
}
