// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	libCommons "github.com/LerianStudio/lib-commons/v2/commons"
	libLog "github.com/LerianStudio/lib-commons/v2/commons/log"
	libOtel "github.com/LerianStudio/lib-commons/v2/commons/opentelemetry"
	libPostgres "github.com/LerianStudio/lib-commons/v2/commons/postgres"
	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"

	pgdb "tracer/internal/adapters/postgres/db"
	"tracer/internal/services/command"
	"tracer/internal/services/query"
	"tracer/pkg/constant"
	"tracer/pkg/logging"
	"tracer/pkg/model"
	pkgHTTP "tracer/pkg/net/http"
)

// Compile-time interface implementation checks.
var (
	_ query.TransactionValidationRepository   = (*TransactionValidationRepository)(nil)
	_ command.TransactionValidationRepository = (*TransactionValidationRepository)(nil)
)

// sortFieldToColumn maps camelCase API field names to snake_case database column names.
// Add new entries here when supporting additional sort fields.
var sortFieldToColumn = map[string]string{
	"createdAt":        "created_at",
	"processingTimeMs": "processing_time_ms",
}

// validTransactionValidationDBColumns defines the whitelist of valid database column names for sorting.
// Used by buildNextCursor to validate snake_case column names.
var validTransactionValidationDBColumns = map[string]bool{
	"created_at":         true,
	"processing_time_ms": true,
}

// TransactionValidationRepository implements TransactionValidationRepository using PostgreSQL with Squirrel query builder.
// Handles JSONB fields (account, segment, portfolio, merchant, metadata, limit_usage_details) and
// UUID[] arrays (matched_rule_ids, evaluated_rule_ids) for transaction validation persistence.
// NOTE: Only INSERT operations are allowed - transaction validation trail is immutable per SOX/GLBA requirements.
type TransactionValidationRepository struct {
	conn      pgdb.Connection
	tableName string
}

// NewTransactionValidationRepository creates a new PostgreSQL transaction validation repository.
func NewTransactionValidationRepository(conn *libPostgres.PostgresConnection) *TransactionValidationRepository {
	return &TransactionValidationRepository{
		conn:      pgdb.NewPostgresConnectionAdapter(conn),
		tableName: "transaction_validations",
	}
}

// NewTransactionValidationRepositoryWithConnection creates a new PostgreSQL transaction validation repository with a custom pgdb.Connection.
// This is primarily used for testing with mock connections.
func NewTransactionValidationRepositoryWithConnection(conn pgdb.Connection) *TransactionValidationRepository {
	return &TransactionValidationRepository{
		conn:      conn,
		tableName: "transaction_validations",
	}
}

// Insert creates a new transaction validation record (insert-only, no updates allowed).
// This maintains the immutability requirement for compliance (SOX/GLBA).
func (r *TransactionValidationRepository) Insert(ctx context.Context, validation *model.TransactionValidation) error {
	if validation == nil {
		return errors.New("validation cannot be nil")
	}

	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "repository.transaction_validation.insert")
	defer span.End()

	logger = logging.WithTrace(ctx, logger)

	db, err := r.conn.GetDB()
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to get database connection", err)
		return fmt.Errorf("failed to get database connection: %w", err)
	}

	// Marshal JSONB fields
	accountJSON, err := json.Marshal(validation.Account)
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to marshal account", err)
		return fmt.Errorf("failed to marshal account: %w", err)
	}

	var segmentJSON []byte
	if validation.Segment != nil {
		segmentJSON, err = json.Marshal(validation.Segment)
		if err != nil {
			libOtel.HandleSpanError(&span, "Failed to marshal segment", err)
			return fmt.Errorf("failed to marshal segment: %w", err)
		}
	}

	var portfolioJSON []byte
	if validation.Portfolio != nil {
		portfolioJSON, err = json.Marshal(validation.Portfolio)
		if err != nil {
			libOtel.HandleSpanError(&span, "Failed to marshal portfolio", err)
			return fmt.Errorf("failed to marshal portfolio: %w", err)
		}
	}

	var merchantJSON []byte
	if validation.Merchant != nil {
		merchantJSON, err = json.Marshal(validation.Merchant)
		if err != nil {
			libOtel.HandleSpanError(&span, "Failed to marshal merchant", err)
			return fmt.Errorf("failed to marshal merchant: %w", err)
		}
	}

	var metadataJSON []byte
	if validation.Metadata != nil {
		metadataJSON, err = json.Marshal(validation.Metadata)
		if err != nil {
			libOtel.HandleSpanError(&span, "Failed to marshal metadata", err)
			return fmt.Errorf("failed to marshal metadata: %w", err)
		}
	} else {
		metadataJSON = []byte("{}")
	}

	limitUsageDetailsJSON, err := json.Marshal(validation.LimitUsageDetails)
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to marshal limit usage details", err)
		return fmt.Errorf("failed to marshal limit usage details: %w", err)
	}

	// Convert UUID slices to StringArray for PostgreSQL UUID[] type
	matchedRuleIDs := uuidSliceToStringArray(validation.MatchedRuleIDs)
	evaluatedRuleIDs := uuidSliceToStringArray(validation.EvaluatedRuleIDs)

	qb := sq.Insert(r.tableName).
		Columns(
			"id",
			"request_id",
			"transaction_type",
			"sub_type",
			"amount",
			"currency",
			"transaction_timestamp",
			"account",
			"segment",
			"portfolio",
			"merchant",
			"metadata",
			"decision",
			"reason",
			"matched_rule_ids",
			"evaluated_rule_ids",
			"limit_usage_details",
			"processing_time_ms",
			"created_at",
		).
		Values(
			validation.ID,
			validation.RequestID,
			string(validation.TransactionType),
			validation.SubType,
			validation.Amount,
			validation.Currency,
			validation.TransactionTimestamp,
			accountJSON,
			segmentJSON,
			portfolioJSON,
			merchantJSON,
			metadataJSON,
			string(validation.Decision),
			validation.Reason,
			matchedRuleIDs,
			evaluatedRuleIDs,
			limitUsageDetailsJSON,
			validation.ProcessingTimeMs,
			validation.CreatedAt,
		).
		PlaceholderFormat(sq.Dollar)

	sqlStr, args, err := qb.ToSql()
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to build query", err)
		return fmt.Errorf("failed to build query: %w", err)
	}

	logger.WithFields(
		"operation", "repository.transaction_validation.insert",
		"validation.id", validation.ID.String(),
		"validation.decision", string(validation.Decision),
	).Info("Inserting transaction validation record")

	_, err = db.ExecContext(ctx, sqlStr, args...)
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to insert transaction validation", err)
		return fmt.Errorf("failed to insert transaction validation: %w", err)
	}

	return nil
}

// GetByID retrieves a specific transaction validation record by its unique identifier.
// Returns constant.ErrTransactionValidationNotFound if the record does not exist.
func (r *TransactionValidationRepository) GetByID(ctx context.Context, id uuid.UUID) (*model.TransactionValidation, error) {
	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "repository.transaction_validation.get_by_id")
	defer span.End()

	logger = logging.WithTrace(ctx, logger)

	db, err := r.conn.GetDB()
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to get database connection", err)

		return nil, fmt.Errorf("failed to get database connection: %w", err)
	}

	qb := sq.Select(
		"id",
		"request_id",
		"transaction_type",
		"sub_type",
		"amount",
		"currency",
		"transaction_timestamp",
		"account",
		"segment",
		"portfolio",
		"merchant",
		"metadata",
		"decision",
		"reason",
		"matched_rule_ids",
		"evaluated_rule_ids",
		"limit_usage_details",
		"processing_time_ms",
		"created_at",
	).
		From(r.tableName).
		Where(sq.Eq{"id": id}).
		PlaceholderFormat(sq.Dollar)

	sqlStr, args, err := qb.ToSql()
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to build query", err)

		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	logger.WithFields(
		"operation", "repository.transaction_validation.get_by_id",
		"validation.id", id.String(),
	).Info("Getting transaction validation by ID")

	validation, err := r.scanValidation(ctx, db.QueryRowContext(ctx, sqlStr, args...))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			libOtel.HandleSpanBusinessErrorEvent(&span, "Transaction validation not found", constant.ErrTransactionValidationNotFound)

			return nil, constant.ErrTransactionValidationNotFound
		}

		libOtel.HandleSpanError(&span, "Failed to get transaction validation", err)

		return nil, fmt.Errorf("failed to get transaction validation: %w", err)
	}

	return validation, nil
}

// List retrieves transaction validation records matching the provided filters using cursor-based pagination.
// If filters is nil, defaults are applied (last 90 days, limit 100).
// Results are ordered by the specified sortBy field (default: created_at DESC).
func (r *TransactionValidationRepository) List(ctx context.Context, filters *model.TransactionValidationFilters) (*model.ListTransactionValidationsResult, error) {
	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "repository.transaction_validation.list")
	defer span.End()

	logger = logging.WithTrace(ctx, logger)

	// Apply defaults if filters is nil
	if filters == nil {
		filters = &model.TransactionValidationFilters{}
	}

	filters.SetDefaults()

	// Validate filters
	if err := filters.Validate(); err != nil {
		libOtel.HandleSpanBusinessErrorEvent(&span, "Invalid transaction validation filters", err)
		return nil, fmt.Errorf("%w: %w", constant.ErrInvalidTransactionValidationFilters, err)
	}

	// Validate and normalize sort parameters
	sortBy, sortOrder, err := r.validateAndNormalizeSort(filters)
	if err != nil {
		libOtel.HandleSpanBusinessErrorEvent(&span, "Invalid sort column", err)
		return nil, err
	}

	db, err := r.conn.GetDB()
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to get database connection", err)
		return nil, fmt.Errorf("failed to get database connection: %w", err)
	}

	qb := sq.Select(
		"id",
		"request_id",
		"transaction_type",
		"sub_type",
		"amount",
		"currency",
		"transaction_timestamp",
		"account",
		"segment",
		"portfolio",
		"merchant",
		"metadata",
		"decision",
		"reason",
		"matched_rule_ids",
		"evaluated_rule_ids",
		"limit_usage_details",
		"processing_time_ms",
		"created_at",
	).
		From(r.tableName).
		PlaceholderFormat(sq.Dollar)

	// Apply business filters
	qb = r.applyFilters(qb, filters)

	// Apply cursor filter for keyset pagination
	qb, sortBy, sortOrder, err = r.applyCursorFilter(qb, filters.Cursor, sortBy, sortOrder, &span)
	if err != nil {
		libOtel.HandleSpanBusinessErrorEvent(&span, "Invalid cursor", err)
		return nil, err
	}

	// Apply ordering
	qb = r.applyOrderBy(qb, sortBy, sortOrder)

	// Fetch Limit+1 to determine if more pages exist
	// Defense-in-depth: ensure fetchLimit is positive before uint64 conversion
	// to prevent integer overflow (gosec G115). Validation at model layer
	// already ensures Limit >= 0 && Limit <= 1000, but we add local protection.
	fetchLimit := filters.Limit + 1
	if fetchLimit <= 0 {
		// This should never happen due to upstream validation, but protect against
		// potential bypass or refactoring. Use default limit + 1 as safe fallback.
		fetchLimit = model.DefaultTransactionValidationFilterLimit + 1
	}

	qb = qb.Limit(uint64(fetchLimit)) // #nosec G115 - fetchLimit is validated positive above

	sqlStr, args, err := qb.ToSql()
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to build query", err)
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	logger.WithFields(
		"operation", "repository.transaction_validation.list",
		"filter.limit", filters.Limit,
		"filter.has_cursor", filters.Cursor != "",
	).Info("Listing transaction validations")

	rows, err := db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to list transaction validations", err)
		return nil, fmt.Errorf("failed to list transaction validations: %w", err)
	}
	defer rows.Close()

	var validations []*model.TransactionValidation

	for rows.Next() {
		validation, err := r.scanValidationFromRows(ctx, rows)
		if err != nil {
			libOtel.HandleSpanError(&span, "Failed to scan transaction validation", err)
			return nil, fmt.Errorf("failed to scan transaction validation: %w", err)
		}

		validations = append(validations, validation)
	}

	if err := rows.Err(); err != nil {
		libOtel.HandleSpanError(&span, "Error iterating transaction validations", err)
		return nil, fmt.Errorf("error iterating transaction validations: %w", err)
	}

	// Determine if there are more results
	hasMore := len(validations) > filters.Limit

	if hasMore {
		validations = validations[:filters.Limit]
	}

	// Generate next cursor from the last item
	var nextCursor string

	if hasMore && len(validations) > 0 {
		lastValidation := validations[len(validations)-1]

		nextCursor, err = r.buildNextCursor(lastValidation, sortBy, sortOrder)
		if err != nil {
			libOtel.HandleSpanError(&span, "Failed to encode cursor", err)
			return nil, fmt.Errorf("failed to encode cursor: %w", err)
		}
	}

	// Ensure we return empty slice, not nil
	if validations == nil {
		validations = []*model.TransactionValidation{}
	}

	result := &model.ListTransactionValidationsResult{
		TransactionValidations: validations,
		NextCursor:             nextCursor,
		HasMore:                hasMore,
	}

	logger.WithFields(
		"operation", "repository.transaction_validation.list",
		"result.count", len(validations),
		"result.has_more", hasMore,
	).Info("Listed transaction validations")

	return result, nil
}

// Count returns the total number of records matching the filters.
// Useful for pagination metadata without fetching all records.
func (r *TransactionValidationRepository) Count(ctx context.Context, filters *model.TransactionValidationFilters) (int64, error) {
	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "repository.transaction_validation.count")
	defer span.End()

	logger = logging.WithTrace(ctx, logger)

	// Apply defaults if filters is nil
	if filters == nil {
		filters = &model.TransactionValidationFilters{}
	}

	filters.SetDefaults()

	// Validate filters
	if err := filters.Validate(); err != nil {
		libOtel.HandleSpanBusinessErrorEvent(&span, "Invalid transaction validation filters", err)

		return 0, fmt.Errorf("%w: %w", constant.ErrInvalidTransactionValidationFilters, err)
	}

	db, err := r.conn.GetDB()
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to get database connection", err)

		return 0, fmt.Errorf("failed to get database connection: %w", err)
	}

	qb := sq.Select("count(*)").
		From(r.tableName).
		PlaceholderFormat(sq.Dollar)

	// Apply filters (excluding pagination for count)
	qb = r.applyFilters(qb, filters)

	sqlStr, args, err := qb.ToSql()
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to build query", err)

		return 0, fmt.Errorf("failed to build query: %w", err)
	}

	logger.WithFields(
		"operation", "repository.transaction_validation.count",
	).Info("Counting transaction validations")

	var count int64

	err = db.QueryRowContext(ctx, sqlStr, args...).Scan(&count)
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to count transaction validations", err)

		return 0, fmt.Errorf("failed to count transaction validations: %w", err)
	}

	logger.WithFields(
		"operation", "repository.transaction_validation.count",
		"result.count", count,
	).Info("Counted transaction validations")

	return count, nil
}

// applyFilters adds WHERE clauses based on the provided filters.
func (r *TransactionValidationRepository) applyFilters(qb sq.SelectBuilder, filters *model.TransactionValidationFilters) sq.SelectBuilder {
	// Date range filter
	if !filters.StartDate.IsZero() {
		qb = qb.Where(sq.GtOrEq{"created_at": filters.StartDate})
	}

	if !filters.EndDate.IsZero() {
		qb = qb.Where(sq.LtOrEq{"created_at": filters.EndDate})
	}

	// Decision filter
	if filters.Decision != nil {
		qb = qb.Where(sq.Eq{"decision": string(*filters.Decision)})
	}

	// AccountID filter (JSONB path query on account)
	if filters.AccountID != nil {
		qb = qb.Where("account->>'accountId' = ?", filters.AccountID.String())
	}

	// MatchedRuleID filter (ANY on UUID[] array)
	if filters.MatchedRuleID != nil {
		qb = qb.Where("? = ANY(matched_rule_ids)", filters.MatchedRuleID.String())
	}

	// ExceededLimitID filter (JSONB path query on limit_usage_details array)
	if filters.ExceededLimitID != nil {
		qb = qb.Where(
			"EXISTS (SELECT 1 FROM jsonb_array_elements(limit_usage_details) AS lud WHERE lud->>'limitId' = ? AND (lud->>'exceeded')::boolean = true)",
			filters.ExceededLimitID.String(),
		)
	}

	// SegmentID filter (JSONB path query on segment)
	if filters.SegmentID != nil {
		qb = qb.Where("segment->>'segmentId' = ?", filters.SegmentID.String())
	}

	// PortfolioID filter (JSONB path query on portfolio)
	if filters.PortfolioID != nil {
		qb = qb.Where("portfolio->>'portfolioId' = ?", filters.PortfolioID.String())
	}

	// TransactionType filter
	if filters.TransactionType != nil {
		qb = qb.Where(sq.Eq{"transaction_type": string(*filters.TransactionType)})
	}

	return qb
}

// scanValidation scans a single row into a TransactionValidation struct.
func (r *TransactionValidationRepository) scanValidation(ctx context.Context, row *sql.Row) (*model.TransactionValidation, error) {
	var (
		validation            model.TransactionValidation
		transactionType       string
		accountJSON           []byte
		segmentJSON           []byte
		portfolioJSON         []byte
		merchantJSON          []byte
		metadataJSON          []byte
		limitUsageDetailsJSON []byte
		matchedRuleIDs        StringArray
		evaluatedRuleIDs      StringArray
		decision              string
	)

	err := row.Scan(
		&validation.ID,
		&validation.RequestID,
		&transactionType,
		&validation.SubType,
		&validation.Amount,
		&validation.Currency,
		&validation.TransactionTimestamp,
		&accountJSON,
		&segmentJSON,
		&portfolioJSON,
		&merchantJSON,
		&metadataJSON,
		&decision,
		&validation.Reason,
		&matchedRuleIDs,
		&evaluatedRuleIDs,
		&limitUsageDetailsJSON,
		&validation.ProcessingTimeMs,
		&validation.CreatedAt,
	)
	if err != nil {
		return nil, err
	}

	return r.hydrateValidation(ctx, &validation, transactionType, accountJSON, segmentJSON, portfolioJSON, merchantJSON, metadataJSON, limitUsageDetailsJSON, []string(matchedRuleIDs), []string(evaluatedRuleIDs), decision)
}

// scanValidationFromRows scans a row from sql.Rows into a TransactionValidation struct.
func (r *TransactionValidationRepository) scanValidationFromRows(ctx context.Context, rows *sql.Rows) (*model.TransactionValidation, error) {
	var (
		validation            model.TransactionValidation
		transactionType       string
		accountJSON           []byte
		segmentJSON           []byte
		portfolioJSON         []byte
		merchantJSON          []byte
		metadataJSON          []byte
		limitUsageDetailsJSON []byte
		matchedRuleIDs        StringArray
		evaluatedRuleIDs      StringArray
		decision              string
	)

	err := rows.Scan(
		&validation.ID,
		&validation.RequestID,
		&transactionType,
		&validation.SubType,
		&validation.Amount,
		&validation.Currency,
		&validation.TransactionTimestamp,
		&accountJSON,
		&segmentJSON,
		&portfolioJSON,
		&merchantJSON,
		&metadataJSON,
		&decision,
		&validation.Reason,
		&matchedRuleIDs,
		&evaluatedRuleIDs,
		&limitUsageDetailsJSON,
		&validation.ProcessingTimeMs,
		&validation.CreatedAt,
	)
	if err != nil {
		return nil, err
	}

	return r.hydrateValidation(ctx, &validation, transactionType, accountJSON, segmentJSON, portfolioJSON, merchantJSON, metadataJSON, limitUsageDetailsJSON, []string(matchedRuleIDs), []string(evaluatedRuleIDs), decision)
}

// hydrateValidation unmarshals JSON fields and converts string arrays to UUIDs.
func (r *TransactionValidationRepository) hydrateValidation(
	ctx context.Context,
	validation *model.TransactionValidation,
	transactionType string,
	accountJSON []byte,
	segmentJSON []byte,
	portfolioJSON []byte,
	merchantJSON []byte,
	metadataJSON []byte,
	limitUsageDetailsJSON []byte,
	matchedRuleIDs []string,
	evaluatedRuleIDs []string,
	decision string,
) (*model.TransactionValidation, error) {
	//nolint:dogsled // only logger needed for UUID parsing warnings; tracer/headerID/metrics unused here
	logger, _, _, _ := libCommons.NewTrackingFromContext(ctx)
	logger = logging.WithTrace(ctx, logger)

	// Set transaction type and decision
	validation.TransactionType = model.TransactionType(transactionType)
	validation.Decision = model.Decision(decision)

	// Unmarshal JSONB fields
	if len(accountJSON) > 0 {
		if err := json.Unmarshal(accountJSON, &validation.Account); err != nil {
			return nil, fmt.Errorf("failed to unmarshal account: %w", err)
		}
	}

	if len(segmentJSON) > 0 {
		validation.Segment = &model.SegmentContext{}
		if err := json.Unmarshal(segmentJSON, validation.Segment); err != nil {
			return nil, fmt.Errorf("failed to unmarshal segment: %w", err)
		}
	}

	if len(portfolioJSON) > 0 {
		validation.Portfolio = &model.PortfolioContext{}
		if err := json.Unmarshal(portfolioJSON, validation.Portfolio); err != nil {
			return nil, fmt.Errorf("failed to unmarshal portfolio: %w", err)
		}
	}

	if len(merchantJSON) > 0 {
		validation.Merchant = &model.MerchantContext{}
		if err := json.Unmarshal(merchantJSON, validation.Merchant); err != nil {
			return nil, fmt.Errorf("failed to unmarshal merchant: %w", err)
		}
	}

	if len(metadataJSON) > 0 {
		if err := json.Unmarshal(metadataJSON, &validation.Metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}
	}

	if len(limitUsageDetailsJSON) > 0 {
		if err := json.Unmarshal(limitUsageDetailsJSON, &validation.LimitUsageDetails); err != nil {
			return nil, fmt.Errorf("failed to unmarshal limit usage details: %w", err)
		}
	}

	// Ensure slices are initialized (not nil)
	if validation.LimitUsageDetails == nil {
		validation.LimitUsageDetails = []model.LimitUsageDetail{}
	}

	// Convert string arrays to UUID slices with warning logging for invalid UUIDs
	validation.MatchedRuleIDs = stringArrayToUUIDSliceWithWarning(matchedRuleIDs, logger, "matched_rule_ids", validation.ID)
	validation.EvaluatedRuleIDs = stringArrayToUUIDSliceWithWarning(evaluatedRuleIDs, logger, "evaluated_rule_ids", validation.ID)

	return validation, nil
}

// validateAndNormalizeSort validates and normalizes sort parameters.
// Returns the validated sortBy, sortOrder values, and any validation error.
func (r *TransactionValidationRepository) validateAndNormalizeSort(filters *model.TransactionValidationFilters) (string, string, error) {
	sortBy := filters.SortBy
	if sortBy == "" {
		sortBy = "createdAt"
	}

	if !model.IsValidTransactionValidationSortField(sortBy) {
		return "", "", constant.ErrInvalidSortColumn
	}

	// Map camelCase API field names to snake_case database column names
	if col, ok := sortFieldToColumn[sortBy]; ok {
		sortBy = col
	}

	sortOrder := strings.ToUpper(filters.SortOrder)
	if sortOrder == "" {
		sortOrder = "DESC"
	}

	// Defense-in-depth: default invalid sortOrder to "DESC" rather than returning an error,
	// since sortOrder is already validated at the API layer and this provides safe fallback.
	if sortOrder != "ASC" && sortOrder != "DESC" {
		sortOrder = "DESC"
	}

	return sortBy, sortOrder, nil
}

// applyCursorFilter adds keyset pagination WHERE clause to the query.
// Supports custom sort columns with id as tiebreaker.
// Returns the updated query, sort column, and sort order from the cursor (for consistency).
func (r *TransactionValidationRepository) applyCursorFilter(qb sq.SelectBuilder, cursorStr string, requestedSortBy string, requestedOrderDir string, span *trace.Span) (sq.SelectBuilder, string, string, error) {
	if cursorStr == "" {
		return qb, requestedSortBy, requestedOrderDir, nil
	}

	cursor, err := pkgHTTP.DecodeCursor(cursorStr)
	if err != nil {
		libOtel.HandleSpanBusinessErrorEvent(span, "Invalid cursor", err)
		return qb, requestedSortBy, requestedOrderDir, fmt.Errorf("%w: %w", constant.ErrInvalidCursor, err)
	}

	// Use sort column and order from cursor for consistency across pages
	// Cursor stores snake_case column names (already normalized)
	sortColumn := cursor.SortBy
	if sortColumn == "" {
		sortColumn = "created_at"
	} else if !validTransactionValidationDBColumns[sortColumn] {
		libOtel.HandleSpanBusinessErrorEvent(span, "Invalid sort column in cursor", constant.ErrInvalidSortColumn)
		return qb, requestedSortBy, requestedOrderDir, constant.ErrInvalidSortColumn
	}

	orderDir := cursor.SortOrder
	if orderDir != "ASC" && orderDir != "DESC" {
		orderDir = "DESC"
	}

	// Validate cursor sort parameters match request parameters
	// This prevents clients from changing sort mid-pagination which could cause inconsistent results
	if sortColumn != requestedSortBy {
		libOtel.HandleSpanBusinessErrorEvent(span, "Cursor sort mismatch", constant.ErrInvalidCursor)
		return qb, requestedSortBy, requestedOrderDir, fmt.Errorf("%w: cursor sortBy does not match request", constant.ErrInvalidCursor)
	}

	if orderDir != strings.ToUpper(requestedOrderDir) {
		libOtel.HandleSpanBusinessErrorEvent(span, "Cursor sort order mismatch", constant.ErrInvalidCursor)
		return qb, requestedSortBy, requestedOrderDir, fmt.Errorf("%w: cursor sortOrder does not match request", constant.ErrInvalidCursor)
	}

	// Validate cursor sort value type matches expected column type
	if err := validateCursorSortValueTransactionValidation(sortColumn, cursor.SortValue); err != nil {
		libOtel.HandleSpanBusinessErrorEvent(span, "Invalid cursor sort value type", constant.ErrInvalidCursor)
		return qb, requestedSortBy, requestedOrderDir, fmt.Errorf("%w: %w", constant.ErrInvalidCursor, err)
	}

	// Build WHERE clause based on sort column
	qb = r.buildCursorCondition(qb, &cursor, sortColumn, orderDir)

	return qb, sortColumn, orderDir, nil
}

// buildCursorCondition builds WHERE clause for keyset pagination.
// Uses sort value + ID as tiebreaker for consistent pagination.
func (r *TransactionValidationRepository) buildCursorCondition(qb sq.SelectBuilder, cursor *pkgHTTP.Cursor, sortBy, sortOrder string) sq.SelectBuilder {
	lt := sq.Lt{}
	gt := sq.Gt{}
	eq := sq.Eq{}

	lt[sortBy] = cursor.SortValue
	gt[sortBy] = cursor.SortValue
	eq[sortBy] = cursor.SortValue

	if sortOrder == "DESC" {
		return qb.Where(
			sq.Or{
				lt, // sort_value < cursor
				sq.And{
					eq,                     // sort_value = cursor
					sq.Lt{"id": cursor.ID}, // AND id < cursor
				},
			},
		)
	}

	return qb.Where(
		sq.Or{
			gt, // sort_value > cursor
			sq.And{
				eq,                     // sort_value = cursor
				sq.Gt{"id": cursor.ID}, // AND id > cursor
			},
		},
	)
}

// applyOrderBy applies ORDER BY clause for keyset pagination.
// Always adds id as secondary sort for stable pagination.
// sortBy is validated against validSortFields whitelist before calling this method.
// sortOrder is constrained to "ASC" or "DESC" before calling this method.
func (r *TransactionValidationRepository) applyOrderBy(qb sq.SelectBuilder, sortBy, sortOrder string) sq.SelectBuilder {
	// Use string concatenation instead of fmt.Sprintf
	// sortBy and sortOrder are pre-validated by whitelist and constraint checks
	return qb.OrderBy(sortBy + " " + sortOrder + ", id " + sortOrder)
}

// buildNextCursor creates a base64-encoded cursor from the last validation in the result set.
// Validates sortBy against allowed database columns and normalizes sortOrder to uppercase.
// Note: sortBy is expected to be snake_case (already normalized by validateAndNormalizeSort).
func (r *TransactionValidationRepository) buildNextCursor(validation *model.TransactionValidation, sortBy, sortOrder string) (string, error) {
	// Validate sortBy against database column whitelist (snake_case)
	if !validTransactionValidationDBColumns[sortBy] {
		return "", constant.ErrInvalidSortColumn
	}

	// Normalize sortOrder to uppercase
	normalizedSortOrder := strings.ToUpper(sortOrder)
	if normalizedSortOrder != "ASC" && normalizedSortOrder != "DESC" {
		normalizedSortOrder = "DESC"
	}

	sortValue, err := getSortValueFromValidation(validation, sortBy)
	if err != nil {
		return "", fmt.Errorf("failed to get sort value: %w", err)
	}

	cursor := pkgHTTP.Cursor{
		ID:         validation.ID.String(),
		SortValue:  sortValue,
		SortBy:     sortBy,
		SortOrder:  normalizedSortOrder,
		PointsNext: true,
	}

	return pkgHTTP.EncodeCursor(cursor)
}

// validateCursorSortValueTransactionValidation validates that the cursor sort value has the correct type
// for the given sort column. This prevents database type coercion errors and
// unexpected query results.
func validateCursorSortValueTransactionValidation(sortBy, sortValue string) error {
	switch sortBy {
	case "created_at":
		// Timestamp columns expect RFC3339Nano format
		if _, err := time.Parse(time.RFC3339Nano, sortValue); err != nil {
			return fmt.Errorf("invalid timestamp format for %s", sortBy)
		}
	case "processing_time_ms":
		// Integer column - parse as int64
		if _, err := strconv.ParseInt(sortValue, 10, 64); err != nil {
			return fmt.Errorf("invalid integer format for %s", sortBy)
		}
	default:
		return fmt.Errorf("unsupported sort column: %s", sortBy)
	}

	return nil
}

// getSortValueFromValidation extracts the value of the sort column from a validation.
// Returns an error if sortBy is not a supported column.
func getSortValueFromValidation(validation *model.TransactionValidation, sortBy string) (string, error) {
	switch sortBy {
	case "created_at":
		return validation.CreatedAt.Format(time.RFC3339Nano), nil
	case "processing_time_ms":
		return strconv.FormatInt(validation.ProcessingTimeMs, 10), nil
	default:
		return "", fmt.Errorf("unsupported sort column: %s", sortBy)
	}
}

// uuidSliceToStringArray converts a slice of UUIDs to StringArray for PostgreSQL UUID[] type.
// StringArray implements driver.Value interface required for database/sql compatibility.
func uuidSliceToStringArray(uuids []uuid.UUID) StringArray {
	if uuids == nil {
		return StringArray{}
	}

	result := make(StringArray, len(uuids))
	for i, id := range uuids {
		result[i] = id.String()
	}

	return result
}

// stringArrayToUUIDSliceWithWarning converts a []string to a slice of UUIDs.
// Invalid UUIDs are skipped with a warning log indicating possible data corruption.
func stringArrayToUUIDSliceWithWarning(strs []string, logger libLog.Logger, fieldName string, validationID uuid.UUID) []uuid.UUID {
	if strs == nil {
		return []uuid.UUID{}
	}

	result := make([]uuid.UUID, 0, len(strs))

	for _, s := range strs {
		id, err := uuid.Parse(s)
		if err == nil {
			result = append(result, id)
		} else {
			// Invalid UUID encountered - possible data corruption
			logger.WithFields(
				"operation", "repository.transaction_validation.uuid_conversion",
				"field", fieldName,
				"validation_id", validationID.String(),
				"invalid_value", s,
				"error", err.Error(),
			).Warn("Skipped invalid UUID in transaction validation record - possible data corruption")
		}
	}

	return result
}
