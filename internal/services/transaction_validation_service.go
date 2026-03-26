// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package services

import (
	"context"
	"errors"
	"fmt"

	libCommons "github.com/LerianStudio/lib-commons/v4/commons"
	libLog "github.com/LerianStudio/lib-commons/v4/commons/log"
	libOtel "github.com/LerianStudio/lib-commons/v4/commons/opentelemetry"
	"github.com/google/uuid"

	"tracer/internal/services/query"
	"tracer/pkg/constant"
	"tracer/pkg/logging"
	"tracer/pkg/model"
)

// Sentinel errors for TransactionValidationService.
var (
	// ErrNilGetQuery is returned when NewTransactionValidationService receives a nil getQuery.
	ErrNilGetQuery = errors.New("NewTransactionValidationService: getQuery cannot be nil")
	// ErrNilListQuery is returned when NewTransactionValidationService receives a nil listQuery.
	ErrNilListQuery = errors.New("NewTransactionValidationService: listQuery cannot be nil")
)

// TransactionValidationService is a facade that combines transaction validation queries.
// It implements the TransactionValidationService interface expected by the HTTP handler.
// NOTE: Transaction validations are immutable per SOX/GLBA requirements - only read operations.
type TransactionValidationService struct {
	getQuery  *query.GetTransactionValidationQuery
	listQuery *query.ListTransactionValidationsQuery
}

// NewTransactionValidationService creates a new transaction validation service facade.
// Returns an error if required dependencies are nil.
func NewTransactionValidationService(
	getQuery *query.GetTransactionValidationQuery,
	listQuery *query.ListTransactionValidationsQuery,
) (*TransactionValidationService, error) {
	if getQuery == nil {
		return nil, ErrNilGetQuery
	}

	if listQuery == nil {
		return nil, ErrNilListQuery
	}

	return &TransactionValidationService{
		getQuery:  getQuery,
		listQuery: listQuery,
	}, nil
}

// GetTransactionValidation retrieves a transaction validation record by ID.
func (s *TransactionValidationService) GetTransactionValidation(ctx context.Context, id uuid.UUID) (*model.TransactionValidation, error) {
	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "service.transaction_validation.get")
	defer span.End()

	traceLogger := logging.WithTrace(ctx, logger)

	// Check for context cancellation before query execution
	if err := ctx.Err(); err != nil {
		libOtel.HandleSpanError(span, "Context cancelled", err)

		traceLogger.With(
			libLog.String("operation", "service.transaction_validation.get"),
			libLog.String("validation.id", id.String()),
			libLog.String("error.message", err.Error()),
		).Log(ctx, libLog.LevelError, "Context cancelled before query execution")

		return nil, fmt.Errorf("get transaction validation: %w", err)
	}

	result, err := s.getQuery.Execute(ctx, id)
	if err != nil {
		libOtel.HandleSpanError(span, "Failed to get transaction validation", err)

		traceLogger.With(
			libLog.String("operation", "service.transaction_validation.get"),
			libLog.String("validation.id", id.String()),
			libLog.String("error.message", err.Error()),
		).Log(ctx, libLog.LevelError, "Failed to get transaction validation")

		return nil, fmt.Errorf("get transaction validation: %w", err)
	}

	if result == nil {
		return nil, constant.ErrTransactionValidationNotFound
	}

	return result, nil
}

// ListTransactionValidations retrieves transaction validation records with filters.
func (s *TransactionValidationService) ListTransactionValidations(ctx context.Context, filters *model.TransactionValidationFilters) (*query.ListTransactionValidationsResult, error) {
	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "service.transaction_validation.list")
	defer span.End()

	traceLogger := logging.WithTrace(ctx, logger)

	// Check for context cancellation before query execution
	if err := ctx.Err(); err != nil {
		libOtel.HandleSpanError(span, "Context cancelled", err)

		traceLogger.With(
			libLog.String("operation", "service.transaction_validation.list"),
			libLog.String("error.message", err.Error()),
		).Log(ctx, libLog.LevelError, "Context cancelled before query execution")

		return nil, fmt.Errorf("list transaction validations: %w", err)
	}

	result, err := s.listQuery.Execute(ctx, filters)
	if err != nil {
		libOtel.HandleSpanError(span, "Failed to list transaction validations", err)

		traceLogger.With(
			libLog.String("operation", "service.transaction_validation.list"),
			libLog.String("error.message", err.Error()),
		).Log(ctx, libLog.LevelError, "Failed to list transaction validations")

		return nil, fmt.Errorf("list transaction validations: %w", err)
	}

	return result, nil
}
