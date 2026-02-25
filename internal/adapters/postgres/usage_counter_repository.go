// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	libCommons "github.com/LerianStudio/lib-commons/v2/commons"
	libOtel "github.com/LerianStudio/lib-commons/v2/commons/opentelemetry"
	libPostgres "github.com/LerianStudio/lib-commons/v2/commons/postgres"
	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	pgdb "tracer/internal/adapters/postgres/db"
	"tracer/pkg/constant"
	"tracer/pkg/logging"
	"tracer/pkg/model"
)

// DefaultDeleteBatchSize is the number of rows to delete per iteration when cleaning up expired counters.
// This prevents long-running locks on large tables by breaking the delete into smaller batches.
const DefaultDeleteBatchSize = 1000

// UsageCounterRepository implements query.UsageCounterRepository using PostgreSQL.
// Provides atomic usage counter operations with row-level locking (SELECT FOR UPDATE).
type UsageCounterRepository struct {
	conn            pgdb.Connection
	tableName       string
	deleteBatchSize int
}

// NewUsageCounterRepository creates a new PostgreSQL usage counter repository.
func NewUsageCounterRepository(conn *libPostgres.PostgresConnection) *UsageCounterRepository {
	return &UsageCounterRepository{
		conn:            pgdb.NewPostgresConnectionAdapter(conn),
		tableName:       "usage_counters",
		deleteBatchSize: DefaultDeleteBatchSize,
	}
}

// NewUsageCounterRepositoryWithConnection creates a new PostgreSQL usage counter repository with a custom pgdb.Connection.
// This is primarily used for testing with mock connections.
func NewUsageCounterRepositoryWithConnection(conn pgdb.Connection) *UsageCounterRepository {
	return &UsageCounterRepository{
		conn:            conn,
		tableName:       "usage_counters",
		deleteBatchSize: DefaultDeleteBatchSize,
	}
}

// GetForUpdate retrieves an existing usage counter with row-level lock.
// Uses SELECT FOR UPDATE to prevent race conditions during concurrent operations.
// Returns sql.ErrNoRows if the counter doesn't exist (does NOT create one).
// Used by rollback operations where creating a counter would be incorrect.
func (r *UsageCounterRepository) GetForUpdate(ctx context.Context, limitID uuid.UUID, scopeKey, periodKey string) (*model.UsageCounter, error) {
	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "repository.usage_counter.get_for_update")
	defer span.End()

	logger = logging.WithTrace(ctx, logger)

	db, err := r.conn.GetDB()
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to get database connection", err)
		return nil, fmt.Errorf("failed to get database connection: %w", err)
	}

	logger.WithFields(
		"operation", "repository.usage_counter.get_for_update",
		"limit_id", limitID.String(),
		"scope_key", scopeKey,
		"period_key", periodKey,
	).Info("Getting usage counter with lock")

	selectQuery := sq.Select("id", "limit_id", "scope_key", "period_key", "current_usage", "last_updated_at").
		From(r.tableName).
		Where(sq.Eq{
			"limit_id":   limitID,
			"scope_key":  scopeKey,
			"period_key": periodKey,
		}).
		Suffix("FOR UPDATE").
		PlaceholderFormat(sq.Dollar)

	sqlStr, args, err := selectQuery.ToSql()
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to build select query", err)
		return nil, fmt.Errorf("failed to build select query: %w", err)
	}

	counter, err := r.scanCounter(ctx, db.QueryRowContext(ctx, sqlStr, args...))
	if err != nil {
		// Return sql.ErrNoRows as-is so caller can distinguish "not found" from other errors
		libOtel.HandleSpanError(&span, "Failed to get usage counter", err)
		return nil, err
	}

	logger.WithFields(
		"operation", "repository.usage_counter.get_for_update",
		"counter_id", counter.ID.String(),
		"current_usage", counter.CurrentUsage,
	).Info("Found usage counter")

	return counter, nil
}

// GetOrCreateForUpdate retrieves or creates a usage counter with row-level lock.
// Uses SELECT FOR UPDATE to prevent race conditions during concurrent increments.
// If the counter doesn't exist, it creates one with currentUsage=0.
func (r *UsageCounterRepository) GetOrCreateForUpdate(ctx context.Context, limitID uuid.UUID, scopeKey, periodKey string) (*model.UsageCounter, error) {
	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "repository.usage_counter.get_or_create_for_update")
	defer span.End()

	logger = logging.WithTrace(ctx, logger)

	db, err := r.conn.GetDB()
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to get database connection", err)
		return nil, fmt.Errorf("failed to get database connection: %w", err)
	}

	logger.WithFields(
		"operation", "repository.usage_counter.get_or_create_for_update",
		"limit_id", limitID.String(),
		"scope_key", scopeKey,
		"period_key", periodKey,
	).Info("Getting or creating usage counter with lock")

	// Try to get existing counter with FOR UPDATE lock
	selectQuery := sq.Select("id", "limit_id", "scope_key", "period_key", "current_usage", "last_updated_at").
		From(r.tableName).
		Where(sq.Eq{
			"limit_id":   limitID,
			"scope_key":  scopeKey,
			"period_key": periodKey,
		}).
		Suffix("FOR UPDATE").
		PlaceholderFormat(sq.Dollar)

	sqlStr, args, err := selectQuery.ToSql()
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to build select query", err)
		return nil, fmt.Errorf("failed to build select query: %w", err)
	}

	counter, err := r.scanCounter(ctx, db.QueryRowContext(ctx, sqlStr, args...))
	if err == nil {
		logger.WithFields(
			"operation", "repository.usage_counter.get_or_create_for_update",
			"counter_id", counter.ID.String(),
			"current_usage", counter.CurrentUsage,
		).Info("Found existing usage counter")

		return counter, nil
	}

	if !errors.Is(err, sql.ErrNoRows) {
		libOtel.HandleSpanError(&span, "Failed to get usage counter", err)
		return nil, fmt.Errorf("failed to get usage counter: %w", err)
	}

	// Counter doesn't exist, create new one
	newCounter, err := model.NewUsageCounter(limitID, scopeKey, periodKey, time.Now())
	if err != nil {
		libOtel.HandleSpanBusinessErrorEvent(&span, "Failed to create usage counter model", err)
		return nil, err
	}

	// Convert entity to database model using ToEntity/FromEntity pattern
	var dbModel UsageCounterPostgreSQLModel
	if err := dbModel.FromEntity(newCounter); err != nil {
		return nil, fmt.Errorf("failed to convert entity to database model: %w", err)
	}

	insertQuery := sq.Insert(r.tableName).
		Columns("id", "limit_id", "scope_key", "period_key", "current_usage", "last_updated_at").
		Values(dbModel.ID, dbModel.LimitID, dbModel.ScopeKey, dbModel.PeriodKey, dbModel.CurrentUsage, dbModel.LastUpdatedAt).
		PlaceholderFormat(sq.Dollar)

	sqlStr, args, err = insertQuery.ToSql()
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to build insert query", err)
		return nil, fmt.Errorf("failed to build insert query: %w", err)
	}

	_, err = db.ExecContext(ctx, sqlStr, args...)
	if err != nil {
		// Only retry on unique constraint violation (SQLSTATE 23505)
		// This handles the race condition where another transaction inserted the counter
		if !IsUniqueViolation(err) {
			// Not a unique constraint violation - return the original error
			libOtel.HandleSpanError(&span, "Failed to insert usage counter", err)
			return nil, fmt.Errorf("failed to insert usage counter: %w", err)
		}

		// Handle concurrent insert race condition
		// Another transaction inserted the counter, try to select it again
		selectQuery = sq.Select("id", "limit_id", "scope_key", "period_key", "current_usage", "last_updated_at").
			From(r.tableName).
			Where(sq.Eq{
				"limit_id":   limitID,
				"scope_key":  scopeKey,
				"period_key": periodKey,
			}).
			Suffix("FOR UPDATE").
			PlaceholderFormat(sq.Dollar)

		sqlStr, args, err = selectQuery.ToSql()
		if err != nil {
			libOtel.HandleSpanError(&span, "Failed to build retry select query", err)
			return nil, fmt.Errorf("failed to build retry select query: %w", err)
		}

		var retryErr error

		counter, retryErr = r.scanCounter(ctx, db.QueryRowContext(ctx, sqlStr, args...))
		if retryErr != nil {
			libOtel.HandleSpanError(&span, "Failed to insert or get usage counter", retryErr)
			return nil, fmt.Errorf("failed to insert or get usage counter: %w", retryErr)
		}

		logger.WithFields(
			"operation", "repository.usage_counter.get_or_create_for_update",
			"counter_id", counter.ID.String(),
			"note", "found after concurrent insert",
		).Info("Found usage counter after retry")

		return counter, nil
	}

	// Re-select the inserted row with FOR UPDATE to acquire the row-level lock
	// This ensures the returned counter has the lock, matching the existing row path
	selectInserted := sq.Select("id", "limit_id", "scope_key", "period_key", "current_usage", "last_updated_at").
		From(r.tableName).
		Where(sq.Eq{"id": newCounter.ID}).
		Suffix("FOR UPDATE").
		PlaceholderFormat(sq.Dollar)

	sqlStr, args, err = selectInserted.ToSql()
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to build post-insert select query", err)
		return nil, fmt.Errorf("failed to build post-insert select query: %w", err)
	}

	counter, err = r.scanCounter(ctx, db.QueryRowContext(ctx, sqlStr, args...))
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to select inserted usage counter", err)
		return nil, fmt.Errorf("failed to select inserted usage counter: %w", err)
	}

	logger.WithFields(
		"operation", "repository.usage_counter.get_or_create_for_update",
		"counter_id", counter.ID.String(),
	).Info("Created new usage counter")

	return counter, nil
}

// IncrementAtomic atomically increments the usage counter.
// Uses a single UPDATE statement for atomic increment.
func (r *UsageCounterRepository) IncrementAtomic(ctx context.Context, counterID uuid.UUID, amount decimal.Decimal) error {
	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "repository.usage_counter.increment_atomic")
	defer span.End()

	logger = logging.WithTrace(ctx, logger)

	if amount.IsNegative() {
		libOtel.HandleSpanBusinessErrorEvent(&span, "Invalid increment amount", constant.ErrUsageCounterIncrementNonNegative)
		return constant.ErrUsageCounterIncrementNonNegative
	}

	if amount.IsZero() {
		return nil
	}

	db, err := r.conn.GetDB()
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to get database connection", err)
		return fmt.Errorf("failed to get database connection: %w", err)
	}

	updateQuery := sq.Update(r.tableName).
		Set("current_usage", sq.Expr("current_usage + ?", amount)).
		Set("last_updated_at", time.Now().UTC()).
		Where(sq.Eq{"id": counterID}).
		PlaceholderFormat(sq.Dollar)

	sqlStr, args, err := updateQuery.ToSql()
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to build update query", err)
		return fmt.Errorf("failed to build update query: %w", err)
	}

	result, err := db.ExecContext(ctx, sqlStr, args...)
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to increment counter", err)
		return fmt.Errorf("failed to increment counter: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to get rows affected", err)
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		libOtel.HandleSpanBusinessErrorEvent(&span, "Usage counter not found", constant.ErrUsageCounterNotFound)
		return constant.ErrUsageCounterNotFound
	}

	logger.WithFields(
		"operation", "repository.usage_counter.increment_atomic",
		"counter_id", counterID.String(),
		"amount", amount.String(),
	).Info("Incremented usage counter")

	return nil
}

// DecrementAtomic atomically decrements the usage counter for rollback operations.
// Uses a conditional UPDATE to prevent TOCTOU race conditions.
func (r *UsageCounterRepository) DecrementAtomic(ctx context.Context, counterID uuid.UUID, amount decimal.Decimal) error {
	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "repository.usage_counter.decrement_atomic")
	defer span.End()

	logger = logging.WithTrace(ctx, logger)

	if amount.IsNegative() {
		libOtel.HandleSpanBusinessErrorEvent(&span, "Invalid decrement amount", constant.ErrUsageCounterDecrementNonNegative)
		return constant.ErrUsageCounterDecrementNonNegative
	}

	if amount.IsZero() {
		return nil
	}

	db, err := r.conn.GetDB()
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to get database connection", err)
		return fmt.Errorf("failed to get database connection: %w", err)
	}

	// Perform atomic conditional decrement in a single UPDATE.
	// The WHERE clause ensures we only decrement if current_usage >= amount,
	// preventing negative values and TOCTOU race conditions.
	updateQuery := sq.Update(r.tableName).
		Set("current_usage", sq.Expr("current_usage - ?", amount)).
		Set("last_updated_at", time.Now().UTC()).
		Where(sq.Eq{"id": counterID}).
		Where(sq.GtOrEq{"current_usage": amount}).
		PlaceholderFormat(sq.Dollar)

	sqlStr, args, err := updateQuery.ToSql()
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to build update query", err)
		return fmt.Errorf("failed to build update query: %w", err)
	}

	result, err := db.ExecContext(ctx, sqlStr, args...)
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to decrement counter", err)
		return fmt.Errorf("failed to decrement counter: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to get rows affected", err)
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		// No rows affected: either counter doesn't exist or insufficient balance.
		// Run a minimal SELECT to distinguish between the two cases.
		selectQuery := sq.Select("current_usage").
			From(r.tableName).
			Where(sq.Eq{"id": counterID}).
			PlaceholderFormat(sq.Dollar)

		sqlStr, args, err = selectQuery.ToSql()
		if err != nil {
			libOtel.HandleSpanError(&span, "Failed to build select query", err)
			return fmt.Errorf("failed to build select query: %w", err)
		}

		var currentUsage decimal.Decimal

		err = db.QueryRowContext(ctx, sqlStr, args...).Scan(&currentUsage)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				libOtel.HandleSpanBusinessErrorEvent(&span, "Usage counter not found", constant.ErrUsageCounterNotFound)
				return constant.ErrUsageCounterNotFound
			}

			libOtel.HandleSpanError(&span, "Failed to check counter existence", err)

			return fmt.Errorf("failed to check counter existence: %w", err)
		}

		// Counter exists but current_usage < amount (insufficient balance)
		libOtel.HandleSpanBusinessErrorEvent(&span, "Decrement would result in negative usage", constant.ErrUsageCounterCurrentUsageNegative)

		return constant.ErrUsageCounterCurrentUsageNegative
	}

	logger.WithFields(
		"operation", "repository.usage_counter.decrement_atomic",
		"counter_id", counterID.String(),
		"amount", amount.String(),
	).Info("Decremented usage counter")

	return nil
}

// UpsertAndIncrementAtomic atomically creates or increments a usage counter.
// Uses INSERT ... ON CONFLICT DO UPDATE ... WHERE current_usage + amount <= maxAmount RETURNING current_usage.
//
// IMPORTANT: The WHERE guard only applies to the DO UPDATE (conflict) path.
// The INSERT path creates a new counter with current_usage = amount without a WHERE guard.
// Therefore, the caller MUST pre-check amount > maxAmount before calling this method.
func (r *UsageCounterRepository) UpsertAndIncrementAtomic(
	ctx context.Context,
	limitID uuid.UUID,
	scopeKey string,
	periodKey string,
	amount decimal.Decimal,
	maxAmount decimal.Decimal,
) (decimal.Decimal, error) {
	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "repository.usage_counter.upsert_and_increment_atomic")
	defer span.End()

	logger = logging.WithTrace(ctx, logger)

	// Early return for zero amount: no-op, avoids unnecessary DB round-trip.
	if amount.IsZero() {
		return decimal.Zero, nil
	}

	// Pre-check: reject negative amounts before any SQL.
	if amount.IsNegative() {
		return decimal.Zero, constant.ErrUsageCounterIncrementNonNegative
	}

	// Pre-check: amount > maxAmount means even a brand-new counter would exceed the limit.
	// This is MANDATORY because the INSERT path has no WHERE guard.
	if amount.GreaterThan(maxAmount) {
		logger.WithFields(
			"operation", "repository.usage_counter.upsert_and_increment_atomic",
			"amount", amount.String(),
			"max_amount", maxAmount.String(),
			"limit_id", limitID.String(),
		).Info("Amount exceeds maxAmount (pre-check)")
		libOtel.HandleSpanBusinessErrorEvent(&span, "Amount exceeds limit (pre-check)", constant.ErrUsageCounterExceedsLimit)

		return decimal.Zero, constant.ErrUsageCounterExceedsLimit
	}

	now := time.Now().UTC()
	counterID := uuid.New()

	// Build the atomic upsert query using raw SQL via Squirrel Expr.
	// INSERT with ON CONFLICT DO UPDATE + WHERE guard + RETURNING.
	query, args, err := sq.Insert(r.tableName).
		Columns("id", "limit_id", "scope_key", "period_key", "current_usage", "last_updated_at").
		Values(counterID.String(), limitID.String(), scopeKey, periodKey, amount, now).
		Suffix(
			"ON CONFLICT (limit_id, scope_key, period_key) DO UPDATE SET "+
				"current_usage = "+r.tableName+".current_usage + ?, "+
				"last_updated_at = ? "+
				"WHERE "+r.tableName+".current_usage + ? <= ? "+
				"RETURNING current_usage",
			amount, now, amount, maxAmount,
		).
		PlaceholderFormat(sq.Dollar).
		ToSql()
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to build upsert query", err)
		return decimal.Zero, fmt.Errorf("failed to build upsert query: %w", err)
	}

	db, err := r.conn.GetDB()
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to get database connection", err)
		return decimal.Zero, fmt.Errorf("failed to get database connection: %w", err)
	}

	var newUsage decimal.Decimal

	err = db.QueryRowContext(ctx, query, args...).Scan(&newUsage)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// WHERE guard failed: current_usage + amount > maxAmount
			logger.WithFields(
				"operation", "repository.usage_counter.upsert_and_increment_atomic",
				"limit_id", limitID.String(),
				"scope_key", scopeKey,
				"period_key", periodKey,
			).Info("Limit exceeded (WHERE guard)")
			libOtel.HandleSpanBusinessErrorEvent(&span, "Limit exceeded", constant.ErrUsageCounterExceedsLimit)

			return decimal.Zero, constant.ErrUsageCounterExceedsLimit
		}

		libOtel.HandleSpanError(&span, "Database error in upsert", err)

		return decimal.Zero, fmt.Errorf("failed to scan upsert result: %w", err)
	}

	logger.WithFields(
		"operation", "repository.usage_counter.upsert_and_increment_atomic",
		"limit_id", limitID.String(),
		"scope_key", scopeKey,
		"period_key", periodKey,
		"new_usage", newUsage.String(),
	).Info("Upsert and increment completed")

	return newUsage, nil
}

// GetByLimitID retrieves all usage counters for a specific limit.
func (r *UsageCounterRepository) GetByLimitID(ctx context.Context, limitID uuid.UUID) ([]model.UsageCounter, error) {
	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "repository.usage_counter.get_by_limit_id")
	defer span.End()

	logger = logging.WithTrace(ctx, logger)

	db, err := r.conn.GetDB()
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to get database connection", err)
		return nil, fmt.Errorf("failed to get database connection: %w", err)
	}

	query := sq.Select("id", "limit_id", "scope_key", "period_key", "current_usage", "last_updated_at").
		From(r.tableName).
		Where(sq.Eq{"limit_id": limitID}).
		OrderBy("period_key DESC", "scope_key ASC").
		PlaceholderFormat(sq.Dollar)

	sqlStr, args, err := query.ToSql()
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to build query", err)
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	logger.WithFields(
		"operation", "repository.usage_counter.get_by_limit_id",
		"limit_id", limitID.String(),
	).Info("Getting usage counters by limit ID")

	rows, err := db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to get usage counters", err)
		return nil, fmt.Errorf("failed to get usage counters: %w", err)
	}
	defer rows.Close()

	var counters []model.UsageCounter

	for rows.Next() {
		counter, err := r.scanCounterFromRows(ctx, rows)
		if err != nil {
			libOtel.HandleSpanError(&span, "Failed to scan usage counter", err)
			return nil, fmt.Errorf("failed to scan usage counter: %w", err)
		}

		counters = append(counters, *counter)
	}

	if err := rows.Err(); err != nil {
		libOtel.HandleSpanError(&span, "Error iterating usage counters", err)
		return nil, fmt.Errorf("error iterating usage counters: %w", err)
	}

	logger.WithFields(
		"operation", "repository.usage_counter.get_by_limit_id",
		"limit_id", limitID.String(),
		"count", len(counters),
	).Info("Retrieved usage counters")

	return counters, nil
}

// GetUsageForLimits retrieves current usage for multiple limits in a single query.
func (r *UsageCounterRepository) GetUsageForLimits(ctx context.Context, limitIDs []uuid.UUID, scopeKey, periodKey string) (map[uuid.UUID]decimal.Decimal, error) {
	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "repository.usage_counter.get_usage_for_limits")
	defer span.End()

	logger = logging.WithTrace(ctx, logger)

	if len(limitIDs) == 0 {
		return make(map[uuid.UUID]decimal.Decimal), nil
	}

	db, err := r.conn.GetDB()
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to get database connection", err)
		return nil, fmt.Errorf("failed to get database connection: %w", err)
	}

	query := sq.Select("limit_id", "current_usage").
		From(r.tableName).
		Where(sq.Eq{
			"limit_id":   limitIDs,
			"scope_key":  scopeKey,
			"period_key": periodKey,
		}).
		PlaceholderFormat(sq.Dollar)

	sqlStr, args, err := query.ToSql()
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to build query", err)
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	logger.WithFields(
		"operation", "repository.usage_counter.get_usage_for_limits",
		"limit_ids_count", len(limitIDs),
		"scope_key", scopeKey,
		"period_key", periodKey,
	).Info("Getting usage for limits")

	rows, err := db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		libOtel.HandleSpanError(&span, "Failed to get usage", err)
		return nil, fmt.Errorf("failed to get usage: %w", err)
	}
	defer rows.Close()

	result := make(map[uuid.UUID]decimal.Decimal)

	for rows.Next() {
		var limitID uuid.UUID

		var currentUsage decimal.Decimal

		if err := rows.Scan(&limitID, &currentUsage); err != nil {
			libOtel.HandleSpanError(&span, "Failed to scan usage", err)
			return nil, fmt.Errorf("failed to scan usage: %w", err)
		}

		result[limitID] = currentUsage
	}

	if err := rows.Err(); err != nil {
		libOtel.HandleSpanError(&span, "Error iterating usage", err)
		return nil, fmt.Errorf("error iterating usage: %w", err)
	}

	logger.WithFields(
		"operation", "repository.usage_counter.get_usage_for_limits",
		"found_count", len(result),
	).Info("Retrieved usage for limits")

	return result, nil
}

// scanCounter scans a single row into a UsageCounter model using the ToEntity/FromEntity pattern.
func (r *UsageCounterRepository) scanCounter(ctx context.Context, row *sql.Row) (*model.UsageCounter, error) {
	var dbModel UsageCounterPostgreSQLModel

	// Check for context cancellation before processing
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	err := row.Scan(
		&dbModel.ID,
		&dbModel.LimitID,
		&dbModel.ScopeKey,
		&dbModel.PeriodKey,
		&dbModel.CurrentUsage,
		&dbModel.LastUpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	// Convert database model to domain entity
	counter, err := dbModel.ToEntity()
	if err != nil {
		return nil, fmt.Errorf("failed to convert to entity: %w", err)
	}

	return counter, nil
}

// scanCounterFromRows scans a row from Rows into a UsageCounter model using the ToEntity/FromEntity pattern.
func (r *UsageCounterRepository) scanCounterFromRows(ctx context.Context, rows *sql.Rows) (*model.UsageCounter, error) {
	var dbModel UsageCounterPostgreSQLModel

	// Check for context cancellation before processing
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	err := rows.Scan(
		&dbModel.ID,
		&dbModel.LimitID,
		&dbModel.ScopeKey,
		&dbModel.PeriodKey,
		&dbModel.CurrentUsage,
		&dbModel.LastUpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	// Convert database model to domain entity
	counter, err := dbModel.ToEntity()
	if err != nil {
		return nil, fmt.Errorf("failed to convert to entity: %w", err)
	}

	return counter, nil
}

// DeleteExpiredCounters removes usage counters that haven't been updated since the specified time.
// This is used for cleanup of old period counters that are no longer relevant.
// Deletes are performed in batches to prevent long-running locks on large tables.
// Returns the total number of deleted counters.
func (r *UsageCounterRepository) DeleteExpiredCounters(ctx context.Context, olderThan time.Time) (int64, error) {
	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)

	ctx, span := tracer.Start(ctx, "repository.usage_counter.delete_expired_counters")
	defer span.End()

	logger = logging.WithTrace(ctx, logger)

	logger.WithFields(
		"operation", "repository.usage_counter.delete_expired_counters",
		"older_than", olderThan.Format(time.RFC3339),
		"batch_size", r.deleteBatchSize,
	).Info("Deleting expired usage counters in batches")

	var totalDeleted int64

	for {
		// Check for context cancellation before each batch to allow graceful shutdown
		if err := ctx.Err(); err != nil {
			logger.WithFields(
				"operation", "repository.usage_counter.delete_expired_counters",
				"total_deleted", totalDeleted,
				"reason", err.Error(),
			).Info("Stopping batch deletion due to context cancellation")
			libOtel.HandleSpanError(&span, "Context cancelled during batch deletion", err)

			return totalDeleted, fmt.Errorf("context cancelled during batch deletion: %w", err)
		}

		// Get a fresh connection for each batch to avoid holding transactions across iterations
		db, err := r.conn.GetDB()
		if err != nil {
			libOtel.HandleSpanError(&span, "Failed to get database connection", err)
			return totalDeleted, fmt.Errorf("failed to get database connection: %w", err)
		}

		// Build batched delete query using subquery:
		// DELETE FROM usage_counters WHERE id IN (SELECT id FROM usage_counters WHERE last_updated_at < $1 LIMIT $2)
		// PostgreSQL doesn't support LIMIT directly on DELETE, so we use a subquery approach.
		deleteQuery := fmt.Sprintf(
			"DELETE FROM %s WHERE id IN (SELECT id FROM %s WHERE last_updated_at < $1 LIMIT $2)",
			r.tableName, r.tableName,
		)

		result, err := db.ExecContext(ctx, deleteQuery, olderThan, r.deleteBatchSize)
		if err != nil {
			libOtel.HandleSpanError(&span, "Failed to delete expired counters batch", err)
			return totalDeleted, fmt.Errorf("failed to delete expired counters: %w", err)
		}

		rowsAffected, err := result.RowsAffected()
		if err != nil {
			libOtel.HandleSpanError(&span, "Failed to get rows affected", err)
			return totalDeleted, fmt.Errorf("failed to get rows affected: %w", err)
		}

		totalDeleted += rowsAffected

		logger.WithFields(
			"operation", "repository.usage_counter.delete_expired_counters",
			"batch_deleted", rowsAffected,
			"total_deleted", totalDeleted,
		).Debug("Deleted batch of expired usage counters")

		// Stop when no more rows to delete
		if rowsAffected == 0 {
			break
		}
	}

	logger.WithFields(
		"operation", "repository.usage_counter.delete_expired_counters",
		"deleted_count", totalDeleted,
	).Info("Deleted expired usage counters")

	return totalDeleted, nil
}
