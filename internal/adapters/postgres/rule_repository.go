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
	"strings"
	"time"

	libCommons "github.com/LerianStudio/lib-commons/v2/commons"
	libOtel "github.com/LerianStudio/lib-commons/v2/commons/opentelemetry"
	libPostgres "github.com/LerianStudio/lib-commons/v2/commons/postgres"
	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"

	pgdb "tracer/internal/adapters/postgres/db"
	"tracer/pkg/constant"
	"tracer/pkg/logging"
	"tracer/pkg/model"
	pkgHTTP "tracer/pkg/net/http"
)

// postgresConnectionAdapter adapts *libPostgres.PostgresConnection to pgdb.Connection.
type postgresConnectionAdapter struct {
	conn *libPostgres.PostgresConnection
}

func (p *postgresConnectionAdapter) GetDB() (pgdb.DB, error) {
	return p.conn.GetDB()
}

const tableName = "rules"

// mapRuleSortFieldToColumn converts a camelCase sort field to its snake_case database column name.
// Returns the column name if valid, otherwise returns empty string.
// Uses a switch instead of a package-level map to prevent runtime mutation.
func mapRuleSortFieldToColumn(sortField string) string {
	switch sortField {
	case "createdAt":
		return "created_at"
	case "updatedAt":
		return "updated_at"
	case "name":
		return "name"
	case "status":
		return "status"
	default:
		return ""
	}
}

// Repository implements RuleRepository using PostgreSQL with Squirrel query builder.
type Repository struct {
	conn pgdb.Connection
}

// NewRepository creates a new PostgreSQL rule repository.
func NewRepository(conn *libPostgres.PostgresConnection) *Repository {
	return &Repository{
		conn: &postgresConnectionAdapter{conn: conn},
	}
}

// NewRepositoryWithConnection creates a new PostgreSQL rule repository with a custom pgdb.Connection.
// This is primarily used for testing with mock connections.
func NewRepositoryWithConnection(conn pgdb.Connection) *Repository {
	return &Repository{
		conn: conn,
	}
}

// Create inserts a new rule into the database.
func (r *Repository) Create(ctx context.Context, rule *model.Rule) (*model.Rule, error) {
	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "repository.rule.create")
	defer span.End()

	logger = logging.WithTrace(ctx, logger)

	db, err := r.conn.GetDB()
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to get database connection", err)
		return nil, fmt.Errorf("failed to get database connection: %w", err)
	}

	scopesJSON, err := json.Marshal(rule.Scopes)
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to marshal scopes", err)
		return nil, fmt.Errorf("failed to marshal scopes: %w", err)
	}

	query := sq.Insert(tableName).
		Columns("id", "name", "description", "expression", "action", "scopes", "status", "created_at", "updated_at").
		Values(rule.ID, rule.Name, rule.Description, rule.Expression, rule.Action, scopesJSON, rule.Status, rule.CreatedAt, rule.UpdatedAt).
		PlaceholderFormat(sq.Dollar)

	sqlStr, args, err := query.ToSql()
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to build query", err)
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	logger.WithFields(
		"operation", "repository.rule.create",
		"rule.id", rule.ID.String(),
		"rule.name", rule.Name,
	).Info("Creating rule")

	_, err = db.ExecContext(ctx, sqlStr, args...)
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to insert rule", err)
		return nil, fmt.Errorf("failed to insert rule: %w", err)
	}

	return rule, nil
}

// GetByID retrieves a rule by its ID.
func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (*model.Rule, error) {
	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "repository.rule.get_by_id")
	defer span.End()

	logger = logging.WithTrace(ctx, logger)

	db, err := r.conn.GetDB()
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to get database connection", err)
		return nil, fmt.Errorf("failed to get database connection: %w", err)
	}

	query := sq.Select("id", "name", "description", "expression", "action", "scopes", "status", "created_at", "updated_at", "activated_at", "deactivated_at", "deleted_at").
		From(tableName).
		Where(sq.Eq{"id": id}).
		Where(sq.Eq{"deleted_at": nil}).
		PlaceholderFormat(sq.Dollar)

	sqlStr, args, err := query.ToSql()
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to build query", err)
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	logger.WithFields(
		"operation", "repository.rule.get_by_id",
		"rule.id", id.String(),
	).Info("Getting rule by ID")

	rule, err := r.scanRule(db.QueryRowContext(ctx, sqlStr, args...))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			libOtel.HandleSpanBusinessErrorEvent(&span, "Rule not found", constant.ErrRuleNotFound)
			return nil, constant.ErrRuleNotFound
		}

		libOtel.HandleSpanError(&span, "Failed to get rule", err)

		return nil, fmt.Errorf("failed to get rule: %w", err)
	}

	return rule, nil
}

// GetByName retrieves a rule by its normalized name.
func (r *Repository) GetByName(ctx context.Context, name string) (*model.Rule, error) {
	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "repository.rule.get_by_name")
	defer span.End()

	logger = logging.WithTrace(ctx, logger)

	db, err := r.conn.GetDB()
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to get database connection", err)
		return nil, fmt.Errorf("failed to get database connection: %w", err)
	}

	query := sq.Select("id", "name", "description", "expression", "action", "scopes", "status", "created_at", "updated_at", "activated_at", "deactivated_at", "deleted_at").
		From(tableName).
		Where(sq.Eq{"name": name}).
		Where(sq.Eq{"deleted_at": nil}).
		PlaceholderFormat(sq.Dollar)

	sqlStr, args, err := query.ToSql()
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to build query", err)
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	logger.WithFields(
		"operation", "repository.rule.get_by_name",
		"rule.name", name,
	).Info("Getting rule by name")

	rule, err := r.scanRule(db.QueryRowContext(ctx, sqlStr, args...))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			libOtel.HandleSpanBusinessErrorEvent(&span, "Rule not found", constant.ErrRuleNotFound)
			return nil, constant.ErrRuleNotFound
		}

		libOtel.HandleSpanError(&span, "Failed to get rule by name", err)

		return nil, fmt.Errorf("failed to get rule by name: %w", err)
	}

	return rule, nil
}

// ListByStatus retrieves rules with optional status filter.
// Used by RuleRepository interface for command operations.
func (r *Repository) ListByStatus(ctx context.Context, status *model.RuleStatus) ([]*model.Rule, error) {
	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "repository.rule.list_by_status")
	defer span.End()

	logger = logging.WithTrace(ctx, logger)

	db, err := r.conn.GetDB()
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to get database connection", err)
		return nil, fmt.Errorf("failed to get database connection: %w", err)
	}

	query := sq.Select("id", "name", "description", "expression", "action", "scopes", "status", "created_at", "updated_at", "activated_at", "deactivated_at", "deleted_at").
		From(tableName).
		Where(sq.Eq{"deleted_at": nil}).
		PlaceholderFormat(sq.Dollar)

	if status != nil {
		query = query.Where(sq.Eq{"status": *status})
	}

	query = query.OrderBy("created_at DESC")

	sqlStr, args, err := query.ToSql()
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to build query", err)
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	logger.WithFields(
		"operation", "repository.rule.list_by_status",
	).Info("Listing rules by status")

	rows, err := db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to list rules", err)
		return nil, fmt.Errorf("failed to list rules: %w", err)
	}
	defer rows.Close()

	var rules []*model.Rule

	for rows.Next() {
		rule, err := r.scanRuleFromRows(rows)
		if err != nil {
			libOtel.HandleSpanError(&span, "Failed to scan rule", err)
			return nil, fmt.Errorf("failed to scan rule: %w", err)
		}

		rules = append(rules, rule)
	}

	if err := rows.Err(); err != nil {
		libOtel.HandleSpanError(&span, "Error iterating rules", err)
		return nil, fmt.Errorf("error iterating rules: %w", err)
	}

	logger.WithFields(
		"operation", "repository.rule.list_by_status",
		"list.count", len(rules),
	).Info("Listed rules")

	return rules, nil
}

// GetActiveRules retrieves all active rules for evaluation.
// If txScope is provided, filters rules by scope at database level using JSONB operators.
// If txScope is nil, returns all active rules (global).
// Implements query.ActiveRulesRepository interface.
func (r *Repository) GetActiveRules(ctx context.Context, txScope *model.Scope) ([]*model.Rule, error) {
	// If no scope provided, return all active rules
	if txScope == nil || txScope.IsEmpty() {
		status := model.RuleStatusActive
		return r.ListByStatus(ctx, &status)
	}

	// Use scope-filtered query for performance optimization
	return r.ListActiveByScopes(ctx, []model.Scope{*txScope})
}

// Update updates an existing rule.
func (r *Repository) Update(ctx context.Context, rule *model.Rule) (*model.Rule, error) {
	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "repository.rule.update")
	defer span.End()

	logger = logging.WithTrace(ctx, logger)

	db, err := r.conn.GetDB()
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to get database connection", err)
		return nil, fmt.Errorf("failed to get database connection: %w", err)
	}

	scopesJSON, err := json.Marshal(rule.Scopes)
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to marshal scopes", err)
		return nil, fmt.Errorf("failed to marshal scopes: %w", err)
	}

	query := sq.Update(tableName).
		Set("name", rule.Name).
		Set("description", rule.Description).
		Set("expression", rule.Expression).
		Set("action", rule.Action).
		Set("scopes", scopesJSON).
		Set("status", rule.Status).
		Set("updated_at", rule.UpdatedAt).
		Where(sq.Eq{"id": rule.ID}).
		Where(sq.Eq{"deleted_at": nil}).
		PlaceholderFormat(sq.Dollar)

	sqlStr, args, err := query.ToSql()
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to build query", err)
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	logger.WithFields(
		"operation", "repository.rule.update",
		"rule.id", rule.ID.String(),
	).Info("Updating rule")

	result, err := db.ExecContext(ctx, sqlStr, args...)
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to update rule", err)
		return nil, fmt.Errorf("failed to update rule: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to get rows affected", err)
		return nil, fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		libOtel.HandleSpanBusinessErrorEvent(&span, "Rule not found", constant.ErrRuleNotFound)
		return nil, constant.ErrRuleNotFound
	}

	return rule, nil
}

// Delete soft-deletes a rule by setting deleted_at and status to DELETED.
func (r *Repository) Delete(ctx context.Context, id uuid.UUID) error {
	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "repository.rule.delete")
	defer span.End()

	logger = logging.WithTrace(ctx, logger)

	db, err := r.conn.GetDB()
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to get database connection", err)
		return fmt.Errorf("failed to get database connection: %w", err)
	}

	now := time.Now().UTC()

	query := sq.Update(tableName).
		Set("status", model.RuleStatusDeleted).
		Set("deleted_at", now).
		Set("updated_at", now).
		Where(sq.Eq{"id": id}).
		Where(sq.Eq{"deleted_at": nil}).
		PlaceholderFormat(sq.Dollar)

	sqlStr, args, err := query.ToSql()
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to build query", err)
		return fmt.Errorf("failed to build query: %w", err)
	}

	logger.WithFields(
		"operation", "repository.rule.delete",
		"rule.id", id.String(),
	).Info("Deleting rule")

	result, err := db.ExecContext(ctx, sqlStr, args...)
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to delete rule", err)
		return fmt.Errorf("failed to delete rule: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to get rows affected", err)
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		libOtel.HandleSpanBusinessErrorEvent(&span, "Rule not found", constant.ErrRuleNotFound)
		return constant.ErrRuleNotFound
	}

	return nil
}

// List retrieves rules with cursor-based pagination and filtering.
// Implements both RuleRepository.List and ListRulesRepository interface for the query service.
// Uses keyset pagination with created_at + id for consistent results.
func (r *Repository) List(ctx context.Context, filter *model.ListRulesFilter) (*model.ListRulesResult, error) {
	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "repository.rule.list_with_filter")
	defer span.End()

	logger = logging.WithTrace(ctx, logger)

	db, err := r.conn.GetDB()
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to get database connection", err)
		return nil, fmt.Errorf("failed to get database connection: %w", err)
	}

	query := sq.Select("id", "name", "description", "expression", "action", "scopes", "status", "created_at", "updated_at", "activated_at", "deactivated_at", "deleted_at").
		From(tableName).
		Where(sq.Eq{"deleted_at": nil}).
		PlaceholderFormat(sq.Dollar)

	query = r.applyFilters(query, filter)

	orderDir := "DESC"
	if filter.SortOrder == "ASC" {
		orderDir = "ASC"
	}

	// Validate sort field and convert to database column name
	sortBy := filter.SortBy
	if sortBy == "" {
		sortBy = "createdAt" // Default API field name
	}

	// Convert camelCase API field to snake_case database column
	sortColumn := mapRuleSortFieldToColumn(sortBy)
	if sortColumn == "" {
		return nil, fmt.Errorf("%w: %s", constant.ErrInvalidSortColumn, sortBy)
	}

	// Apply cursor filter for keyset pagination
	// When cursor is present, sort params come from cursor (handler rejects sortBy/sortOrder with cursor)
	if filter.Cursor != "" {
		query, sortColumn, sortBy, orderDir, err = r.applyCursorFilter(query, filter.Cursor, &span)
		if err != nil {
			return nil, err
		}
	}

	// Order by sort column + created_at + id for consistent keyset pagination
	query = r.applyOrderBy(query, sortColumn, orderDir)

	// Fetch one extra to determine if there are more results
	// Defense-in-depth: ensure fetchLimit is positive before uint64 conversion
	// to prevent integer overflow (gosec G115). Validation at upstream layers
	// already ensures Limit >= 0, but we add local protection.
	fetchLimit := filter.Limit + 1
	if fetchLimit <= 0 {
		// This should never happen due to upstream validation, but protect against
		// potential bypass or refactoring. Use default limit + 1 as safe fallback.
		fetchLimit = constant.DefaultPaginationLimit + 1
	}
	query = query.Limit(uint64(fetchLimit)) // #nosec G115 - fetchLimit validated positive above

	sqlStr, args, err := query.ToSql()
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to build query", err)
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	logger.WithFields(
		"operation", "repository.rule.list_with_filter",
		"list.limit", filter.Limit,
		"list.cursor", filter.Cursor,
	).Info("Listing rules with filter")

	rows, err := db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to list rules", err)
		return nil, fmt.Errorf("failed to list rules: %w", err)
	}
	defer rows.Close()

	rules := []model.Rule{}

	for rows.Next() {
		rule, err := r.scanRuleFromRows(rows)
		if err != nil {
			libOtel.HandleSpanError(&span, "Failed to scan rule", err)
			return nil, fmt.Errorf("failed to scan rule: %w", err)
		}

		rules = append(rules, *rule)
	}

	if err := rows.Err(); err != nil {
		libOtel.HandleSpanError(&span, "Error iterating rules", err)
		return nil, fmt.Errorf("error iterating rules: %w", err)
	}

	hasMore := len(rules) > filter.Limit
	if hasMore {
		rules = rules[:filter.Limit]
	}

	// Generate next cursor from the last item
	var nextCursor string

	if hasMore && len(rules) > 0 {
		lastRule := rules[len(rules)-1]
		// Use camelCase sortBy for cursor (API format), not snake_case sortColumn
		sortValue := getSortValueFromRule(&lastRule, sortBy)

		cursor := pkgHTTP.Cursor{
			ID:         lastRule.ID.String(),
			SortValue:  sortValue,
			SortBy:     sortBy, // Store camelCase API field name in cursor
			SortOrder:  orderDir,
			PointsNext: true,
		}

		nextCursor, err = pkgHTTP.EncodeCursor(cursor)
		if err != nil {
			libOtel.HandleSpanError(&span, "Failed to encode cursor", err)
			return nil, fmt.Errorf("failed to encode cursor: %w", err)
		}
	}

	result := &model.ListRulesResult{
		Rules:      rules,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	}

	logger.WithFields(
		"operation", "repository.rule.list_with_filter",
		"list.count", len(rules),
		"list.has_more", hasMore,
	).Info("Listed rules")

	return result, nil
}

// applyFilters adds WHERE clauses for name, status, and action filters.
func (r *Repository) applyFilters(query sq.SelectBuilder, filter *model.ListRulesFilter) sq.SelectBuilder {
	if filter.Name != nil && *filter.Name != "" {
		// Case-insensitive partial match using ILIKE with % wildcards
		// Escape LIKE special characters to prevent unintended pattern matching
		escapedName := escapeLikePattern(*filter.Name)
		query = query.Where(sq.ILike{"name": "%" + escapedName + "%"})
	}

	if filter.Status != nil {
		query = query.Where(sq.Eq{"status": *filter.Status})
	}

	if filter.Action != nil {
		query = query.Where(sq.Eq{"action": *filter.Action})
	}

	return query
}

// applyOrderBy applies ORDER BY clause for keyset pagination.
// Uses 2-level ordering: sort_column + id for consistent cursor-based pagination.
func (r *Repository) applyOrderBy(query sq.SelectBuilder, sortColumn string, orderDir string) sq.SelectBuilder {
	return query.OrderBy(fmt.Sprintf("%s %s, id %s", sortColumn, orderDir, orderDir))
}

// applyCursorFilter adds keyset pagination WHERE clause to the query.
// Supports custom sort columns with id as tiebreaker.
// Returns: updated query, snake_case sortColumn, camelCase sortBy, sortOrder, and error.
// Note: Handler validation ensures sortBy/sortOrder are never provided with cursor (TRC-0045).
func (r *Repository) applyCursorFilter(query sq.SelectBuilder, cursorStr string, span *trace.Span) (sq.SelectBuilder, string, string, string, error) {
	if cursorStr == "" {
		return query, "", "", "", nil
	}

	cursor, err := pkgHTTP.DecodeCursor(cursorStr)
	if err != nil {
		libOtel.HandleSpanBusinessErrorEvent(span, "Invalid cursor", err)
		return query, "", "", "", fmt.Errorf("%w: %v", constant.ErrInvalidCursor, err)
	}

	// Use sort field from cursor (in camelCase)
	sortField := cursor.SortBy
	if sortField == "" {
		sortField = "createdAt" // Default API field name
	}

	// Convert camelCase API field to snake_case database column
	sortColumn := mapRuleSortFieldToColumn(sortField)
	if sortColumn == "" {
		libOtel.HandleSpanBusinessErrorEvent(span, "Invalid sort column in cursor", fmt.Errorf("%w: %s", constant.ErrInvalidSortColumn, sortField))
		return query, "", "", "", fmt.Errorf("%w in cursor: %s", constant.ErrInvalidSortColumn, sortField)
	}

	orderDir := strings.ToUpper(cursor.SortOrder)
	if orderDir != "ASC" && orderDir != "DESC" {
		libOtel.HandleSpanBusinessErrorEvent(span, "Invalid sort order in cursor", fmt.Errorf("%w: %s", constant.ErrInvalidCursor, cursor.SortOrder))
		return query, "", "", "", fmt.Errorf("%w: invalid sort order %s", constant.ErrInvalidCursor, cursor.SortOrder)
	}

	// Build WHERE clause based on sort column
	query = r.buildCursorCondition(query, &cursor, sortColumn, orderDir)

	// Return both snake_case sortColumn (for SQL) and camelCase sortField (for next cursor)
	return query, sortColumn, sortField, orderDir, nil
}

// buildCursorCondition builds WHERE clause for keyset pagination.
// Uses 2-level comparison: sort_column, then id as tiebreaker.
func (r *Repository) buildCursorCondition(query sq.SelectBuilder, cursor *pkgHTTP.Cursor, sortColumn string, orderDir string) sq.SelectBuilder {
	lt := sq.Lt{}
	gt := sq.Gt{}
	eq := sq.Eq{}

	lt[sortColumn] = cursor.SortValue
	gt[sortColumn] = cursor.SortValue
	eq[sortColumn] = cursor.SortValue

	if orderDir == "DESC" {
		return query.Where(
			sq.Or{
				lt, // sort_value < cursor
				sq.And{
					eq,                     // sort_value = cursor
					sq.Lt{"id": cursor.ID}, // AND id < cursor
				},
			},
		)
	}

	return query.Where(
		sq.Or{
			gt, // sort_value > cursor
			sq.And{
				eq,                     // sort_value = cursor
				sq.Gt{"id": cursor.ID}, // AND id > cursor
			},
		},
	)
}

// getSortValueFromRule extracts the sort value from a rule based on the sort field.
// sortBy is expected in camelCase (API format).
func getSortValueFromRule(rule *model.Rule, sortBy string) string {
	switch sortBy {
	case "name":
		return rule.Name
	case "status":
		return string(rule.Status)
	case "updatedAt":
		return rule.UpdatedAt.Format(time.RFC3339Nano)
	case "createdAt":
		return rule.CreatedAt.Format(time.RFC3339Nano)
	default:
		return rule.CreatedAt.Format(time.RFC3339Nano)
	}
}

// ListActiveByScopes retrieves active rules that match any of the given scopes.
// Filtering is done in the database using JSONB operators for optimal performance.
// A rule matches if:
// - It has no scopes (global rule), OR
// - Any of its scopes match any of the filter scopes
// A scope matches when all non-null fields in the rule scope equal the corresponding filter values.
func (r *Repository) ListActiveByScopes(ctx context.Context, scopes []model.Scope) ([]*model.Rule, error) {
	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "repository.rule.list_active_by_scopes")
	defer span.End()

	logger = logging.WithTrace(ctx, logger)

	db, err := r.conn.GetDB()
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to get database connection", err)
		return nil, fmt.Errorf("failed to get database connection: %w", err)
	}

	query := sq.Select("id", "name", "description", "expression", "action", "scopes", "status", "created_at", "updated_at", "activated_at", "deactivated_at", "deleted_at").
		From(tableName).
		Where(sq.Eq{"status": model.RuleStatusActive}).
		Where(sq.Eq{"deleted_at": nil}).
		OrderBy("created_at DESC").
		PlaceholderFormat(sq.Dollar)

	// Apply JSONB scope filter if scopes are provided
	if len(scopes) > 0 {
		scopeFilter, filterArgs := r.buildScopeFilter(scopes)
		query = query.Where(scopeFilter, filterArgs...)
	}

	sqlStr, args, err := query.ToSql()
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to build query", err)
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	logger.WithFields(
		"operation", "repository.rule.list_active_by_scopes",
		"scope.filter_count", len(scopes),
	).Info("Listing active rules by scopes")

	rows, err := db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to list active rules", err)
		return nil, fmt.Errorf("failed to list active rules: %w", err)
	}
	defer rows.Close()

	var rules []*model.Rule

	for rows.Next() {
		rule, err := r.scanRuleFromRows(rows)
		if err != nil {
			libOtel.HandleSpanError(&span, "Failed to scan rule", err)
			return nil, fmt.Errorf("failed to scan rule: %w", err)
		}

		rules = append(rules, rule)
	}

	if err := rows.Err(); err != nil {
		libOtel.HandleSpanError(&span, "Error iterating rules", err)
		return nil, fmt.Errorf("error iterating rules: %w", err)
	}

	logger.WithFields(
		"operation", "repository.rule.list_active_by_scopes",
		"list.count", len(rules),
	).Info("Found active rules matching scopes")

	return rules, nil
}

// buildScopeFilter builds a JSONB filter clause for scope matching.
// Returns a SQL fragment and its arguments for use in WHERE clause.
//
// The filter matches rules where:
// - scopes is empty array '[]' (global rules match everything), OR
// - any scope in the array matches any of the filter scopes
//
// A scope matches when all non-null fields in the rule scope equal the filter values.
// Null fields in rule scope act as wildcards (match any value).
//
// IMPORTANT: Uses '?' placeholders (not $1, $2) so Squirrel can renumber them
// correctly when combined with other WHERE clauses.
func (r *Repository) buildScopeFilter(filterScopes []model.Scope) (string, []any) {
	if len(filterScopes) == 0 {
		return "1=1", nil
	}

	// Build OR conditions for each filter scope
	var (
		scopeConditions []string
		args            []any
	)

	for _, scope := range filterScopes {
		condition, scopeArgs := r.buildSingleScopeCondition(scope)
		if condition != "" {
			scopeConditions = append(scopeConditions, condition)
			args = append(args, scopeArgs...)
		}
	}

	if len(scopeConditions) == 0 {
		// No valid filter conditions, match all (including global rules)
		return "1=1", nil
	}

	// Global rules (empty scopes array) always match
	// OR any scope in the array matches any filter scope
	filter := fmt.Sprintf(
		"(scopes = '[]'::jsonb OR EXISTS (SELECT 1 FROM jsonb_array_elements(scopes) AS scope WHERE %s))",
		"("+joinOr(scopeConditions)+")",
	)

	return filter, args
}

// buildSingleScopeCondition builds the WHERE conditions for a single filter scope.
// Returns SQL fragment and arguments.
// Uses '?' placeholders so Squirrel can renumber them correctly.
// UUID fields are converted to strings for JSONB comparison.
func (r *Repository) buildSingleScopeCondition(scope model.Scope) (string, []any) {
	var (
		conditions []string
		args       []any
	)

	// For each field: if filter has a value, rule scope field must be NULL or equal to filter value
	// This implements the "rule scope field is wildcard if NULL" logic
	// UUID fields are converted to string for JSONB comparison

	if scope.SegmentID != nil {
		conditions = append(conditions, "(scope->>'segmentId' IS NULL OR scope->>'segmentId' = ?)")
		args = append(args, scope.SegmentID.String())
	}

	if scope.PortfolioID != nil {
		conditions = append(conditions, "(scope->>'portfolioId' IS NULL OR scope->>'portfolioId' = ?)")
		args = append(args, scope.PortfolioID.String())
	}

	if scope.AccountID != nil {
		conditions = append(conditions, "(scope->>'accountId' IS NULL OR scope->>'accountId' = ?)")
		args = append(args, scope.AccountID.String())
	}

	if scope.MerchantID != nil {
		conditions = append(conditions, "(scope->>'merchantId' IS NULL OR scope->>'merchantId' = ?)")
		args = append(args, scope.MerchantID.String())
	}

	if scope.TransactionType != nil {
		conditions = append(conditions, "(scope->>'transactionType' IS NULL OR scope->>'transactionType' = ?)")
		args = append(args, string(*scope.TransactionType))
	}

	if scope.SubType != nil {
		conditions = append(conditions, "(scope->>'subType' IS NULL OR scope->>'subType' = ?)")
		args = append(args, *scope.SubType)
	}

	if len(conditions) == 0 {
		// Empty filter scope matches everything
		return "1=1", nil
	}

	return "(" + joinAnd(conditions) + ")", args
}

// joinAnd joins conditions with AND.
func joinAnd(conditions []string) string {
	return strings.Join(conditions, " AND ")
}

// joinOr joins conditions with OR.
func joinOr(conditions []string) string {
	return strings.Join(conditions, " OR ")
}

// UpdateStatus updates the status and related timestamps for lifecycle transitions.
func (r *Repository) UpdateStatus(ctx context.Context, id uuid.UUID, status model.RuleStatus, updatedAt time.Time, activatedAt *time.Time, deactivatedAt *time.Time) error {
	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "repository.rule.update_status")
	defer span.End()

	logger = logging.WithTrace(ctx, logger)

	db, err := r.conn.GetDB()
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to get database connection", err)
		return fmt.Errorf("failed to get database connection: %w", err)
	}

	query := sq.Update(tableName).
		Set("status", status).
		Set("updated_at", updatedAt)

	if activatedAt != nil {
		query = query.Set("activated_at", *activatedAt)
	}

	if deactivatedAt != nil {
		query = query.Set("deactivated_at", *deactivatedAt)
	}

	query = query.
		Where(sq.Eq{"id": id}).
		Where(sq.Eq{"deleted_at": nil}).
		PlaceholderFormat(sq.Dollar)

	sqlStr, args, err := query.ToSql()
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to build query", err)
		return fmt.Errorf("failed to build query: %w", err)
	}

	logger.WithFields(
		"operation", "repository.rule.update_status",
		"rule.id", id.String(),
		"rule.status", string(status),
	).Info("Updating rule status")

	result, err := db.ExecContext(ctx, sqlStr, args...)
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to update rule status", err)
		return fmt.Errorf("failed to update rule status: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to get rows affected", err)
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		libOtel.HandleSpanBusinessErrorEvent(&span, "Rule not found", constant.ErrRuleNotFound)
		return constant.ErrRuleNotFound
	}

	return nil
}

// scanRule scans a single row into a Rule struct.
func (r *Repository) scanRule(row *sql.Row) (*model.Rule, error) {
	var rule model.Rule

	var scopesJSON []byte

	var activatedAt, deactivatedAt, deletedAt sql.NullTime

	err := row.Scan(
		&rule.ID,
		&rule.Name,
		&rule.Description,
		&rule.Expression,
		&rule.Action,
		&scopesJSON,
		&rule.Status,
		&rule.CreatedAt,
		&rule.UpdatedAt,
		&activatedAt,
		&deactivatedAt,
		&deletedAt,
	)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(scopesJSON, &rule.Scopes); err != nil {
		return nil, fmt.Errorf("failed to unmarshal scopes: %w", err)
	}

	// Ensure scopes is never nil (return empty array instead of null in JSON)
	if rule.Scopes == nil {
		rule.Scopes = []model.Scope{}
	}

	if activatedAt.Valid {
		rule.ActivatedAt = &activatedAt.Time
	}

	if deactivatedAt.Valid {
		rule.DeactivatedAt = &deactivatedAt.Time
	}

	if deletedAt.Valid {
		rule.DeletedAt = &deletedAt.Time
	}

	return &rule, nil
}

// scanRuleFromRows scans a row from sql.Rows into a Rule struct.
func (r *Repository) scanRuleFromRows(rows *sql.Rows) (*model.Rule, error) {
	var rule model.Rule

	var scopesJSON []byte

	var activatedAt, deactivatedAt, deletedAt sql.NullTime

	err := rows.Scan(
		&rule.ID,
		&rule.Name,
		&rule.Description,
		&rule.Expression,
		&rule.Action,
		&scopesJSON,
		&rule.Status,
		&rule.CreatedAt,
		&rule.UpdatedAt,
		&activatedAt,
		&deactivatedAt,
		&deletedAt,
	)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(scopesJSON, &rule.Scopes); err != nil {
		return nil, fmt.Errorf("failed to unmarshal scopes: %w", err)
	}

	// Ensure scopes is never nil (return empty array instead of null in JSON)
	if rule.Scopes == nil {
		rule.Scopes = []model.Scope{}
	}

	if activatedAt.Valid {
		rule.ActivatedAt = &activatedAt.Time
	}

	if deactivatedAt.Valid {
		rule.DeactivatedAt = &deactivatedAt.Time
	}

	if deletedAt.Valid {
		rule.DeletedAt = &deletedAt.Time
	}

	return &rule, nil
}

// escapeLikePattern escapes special LIKE/ILIKE characters (%, _, \) in the input
// to prevent unintended pattern matching behavior when used in SQL LIKE clauses.
func escapeLikePattern(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)

	return s
}
