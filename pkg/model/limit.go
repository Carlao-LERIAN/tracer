// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package model

import (
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"tracer/pkg"
	"tracer/pkg/constant"
)

// LimitType represents the period type of a limit
type LimitType string

const (
	LimitTypeDaily          LimitType = "DAILY"
	LimitTypeMonthly        LimitType = "MONTHLY"
	LimitTypePerTransaction LimitType = "PER_TRANSACTION"
)

// LimitStatus represents the lifecycle status of a limit
type LimitStatus string

const (
	LimitStatusDraft    LimitStatus = "DRAFT"
	LimitStatusActive   LimitStatus = "ACTIVE"
	LimitStatusInactive LimitStatus = "INACTIVE"
	LimitStatusDeleted  LimitStatus = "DELETED"
)

// safeNameRegex validates limit names contain only safe ASCII characters.
// Allows: alphanumeric, literal spaces, hyphens, underscores, periods, parentheses.
// Prevents: XSS vectors like <script>, SQL injection attempts, and control whitespace.
// Note: Uses literal space instead of \s to reject tabs, newlines, and other control chars.
// ASCII-only by design: accented and unicode characters are rejected. This restriction is
// enforced at the API/DB boundary; callers should normalize input accordingly.
// Length is validated separately against MaxNameLength (255 bytes, ASCII-safe).
var safeNameRegex = regexp.MustCompile(`^[a-zA-Z0-9 \-_.()]+$`)

// String length constraints (aligned with database schema and HTTP validation)
const (
	MaxNameLength        = 255  // VARCHAR(255) in database
	MaxDescriptionLength = 1000 // TEXT in database, but limited for practical use
	MaxSubTypeLength     = 50   // Maximum length for transaction subType field
)

// safeDescriptionRegex validates description contains no script/HTML tags.
// More permissive than name regex but prevents XSS vectors.
// Allows: most ASCII characters except < and > which could form HTML tags.
// ASCII-only by design: while the regex doesn't explicitly block unicode, the API/DB boundary
// enforces ASCII input; callers should normalize accordingly.
// Length is validated separately against MaxDescriptionLength (1000 bytes, ASCII-safe).
var safeDescriptionRegex = regexp.MustCompile(`^[^<>]*$`)

// Limit represents a transaction limit.
// MaxAmount is expressed as a decimal value (e.g., 1000.00 for USD/BRL).
// ResetAt is calculated based on LimitType:
//   - DAILY: next midnight UTC
//   - MONTHLY: next 1st of month at midnight UTC
//   - PER_TRANSACTION: null (no reset)
type Limit struct {
	ID          uuid.UUID       `json:"limitId" swaggertype:"string" format:"uuid"`
	Name        string          `json:"name"`
	Description *string         `json:"description,omitempty"`
	LimitType   LimitType       `json:"limitType"`
	MaxAmount   decimal.Decimal `json:"maxAmount" swaggertype:"string" example:"1000.00"`
	Currency    string          `json:"currency"`
	Scopes      []Scope         `json:"scopes"`
	Status      LimitStatus     `json:"status"`
	ResetAt     *time.Time      `json:"resetAt,omitempty" format:"date-time"`
	CreatedAt   time.Time       `json:"createdAt" format:"date-time"`
	UpdatedAt   time.Time       `json:"updatedAt" format:"date-time"`
	DeletedAt   *time.Time      `json:"deletedAt,omitempty" format:"date-time"`
}

// UsageCounter tracks current usage for a limit within a specific scope and period.
// CurrentUsage is expressed as a decimal value.
// Note: Remaining amount is calculated as (Limit.MaxAmount - CurrentUsage), not stored.
// ScopeKey format: "acct:abc-123", "segment:gold", "portfolio:xyz"
// PeriodKey format: "2025-12-28" for DAILY, "2025-12" for MONTHLY
type UsageCounter struct {
	ID            uuid.UUID       `json:"usageCounterId" swaggertype:"string" format:"uuid"`
	LimitID       uuid.UUID       `json:"limitId" swaggertype:"string" format:"uuid"`
	ScopeKey      string          `json:"scopeKey"`
	PeriodKey     string          `json:"periodKey"`
	CurrentUsage  decimal.Decimal `json:"currentUsage" swaggertype:"string" example:"500.00" minimum:"0"`
	LastUpdatedAt time.Time       `json:"lastUpdatedAt" format:"date-time"`
}

// ScanFields returns pointers to all fields for use with sql.Row.Scan or sql.Rows.Scan.
// Field order matches: id, limit_id, scope_key, period_key, current_usage, last_updated_at.
func (c *UsageCounter) ScanFields() []any {
	return []any{&c.ID, &c.LimitID, &c.ScopeKey, &c.PeriodKey, &c.CurrentUsage, &c.LastUpdatedAt}
}

// IsValid validates LimitType enum
func (t LimitType) IsValid() bool {
	switch t {
	case LimitTypeDaily, LimitTypeMonthly, LimitTypePerTransaction:
		return true
	}

	return false
}

// IsValid validates LimitStatus enum
func (s LimitStatus) IsValid() bool {
	switch s {
	case LimitStatusDraft, LimitStatusActive, LimitStatusInactive, LimitStatusDeleted:
		return true
	}

	return false
}

// CalculateResetAt computes next reset time based on limit type
func CalculateResetAt(limitType LimitType, now time.Time) *time.Time {
	switch limitType {
	case LimitTypeDaily:
		nextDay := now.UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)

		return &nextDay
	case LimitTypeMonthly:
		year, month, _ := now.UTC().Date()
		nextMonth := time.Date(year, month+1, 1, 0, 0, 0, 0, time.UTC)

		return &nextMonth
	case LimitTypePerTransaction:
		return nil
	default:
		return nil
	}
}

// validateCurrency checks if currency is a valid ISO 4217 code (3 uppercase letters)
func validateCurrency(currency string) error {
	if !pkg.IsValidCurrency(currency) {
		return constant.ErrLimitInvalidCurrency
	}

	return nil
}

// validateScopes checks if scopes array is valid
func validateScopes(scopes []Scope) error {
	if len(scopes) == 0 {
		return constant.ErrLimitInvalidScope
	}

	for _, scope := range scopes {
		if scope.IsEmpty() {
			return constant.ErrLimitInvalidScope
		}

		if scope.TransactionType != nil && !scope.TransactionType.IsValid() {
			return constant.ErrLimitInvalidScope
		}
	}

	return nil
}

// NewLimit creates a new Limit entity with validation.
// maxAmount is a decimal value (e.g., 1000.00).
// Scopes ordering is preserved: the returned Limit.Scopes maintains the same order as the input.
// Name and description are trimmed of leading/trailing whitespace before storage.
// The caller provides the current timestamp (createdAt) to enable deterministic testing via clock injection.
func NewLimit(
	name string,
	limitType LimitType,
	maxAmount decimal.Decimal,
	currency string,
	scopes []Scope,
	description *string,
	createdAt time.Time,
) (*Limit, error) {
	now := createdAt.UTC()
	resetAt := CalculateResetAt(limitType, now)

	// Normalize textual inputs
	normalizedName := strings.TrimSpace(name)
	normalizedCurrency := strings.ToUpper(strings.TrimSpace(currency))

	var normalizedDescription *string

	if description != nil {
		trimmed := strings.TrimSpace(*description)
		normalizedDescription = &trimmed
	}

	// Defensive copy of scopes to prevent external mutation
	scopesCopy := append([]Scope(nil), scopes...)

	limit := &Limit{
		ID:          uuid.New(),
		Name:        normalizedName,
		Description: normalizedDescription,
		LimitType:   limitType,
		MaxAmount:   maxAmount,
		Currency:    normalizedCurrency,
		Scopes:      scopesCopy,
		Status:      LimitStatusDraft,
		ResetAt:     resetAt,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := limit.Validate(); err != nil {
		return nil, err
	}

	return limit, nil
}

// validateName checks if name is valid
func validateName(name string) error {
	if strings.TrimSpace(name) == "" {
		return constant.ErrLimitNameRequired
	}

	if len(name) > MaxNameLength {
		return constant.ErrLimitNameTooLong
	}

	// Validate name contains only safe characters (XSS/injection prevention)
	if !safeNameRegex.MatchString(name) {
		return constant.ErrLimitNameInvalidChars
	}

	return nil
}

// validateMaxAmount checks if maxAmount is valid
func validateMaxAmount(maxAmount decimal.Decimal) error {
	if maxAmount.LessThanOrEqual(decimal.Zero) {
		return constant.ErrLimitInvalidMaxAmount
	}

	return nil
}

// validateDescription checks if description is valid (length and XSS prevention).
// Returns nil if description is nil (optional field).
func validateDescription(description *string) error {
	if description == nil {
		return nil
	}

	// Validate description length
	if len(*description) > MaxDescriptionLength {
		return constant.ErrLimitDescriptionTooLong
	}

	// Validate description contains no HTML/script tags (XSS prevention)
	if !safeDescriptionRegex.MatchString(*description) {
		return constant.ErrLimitDescriptionInvalidChars
	}

	return nil
}

// Update modifies limit fields. Only non-nil parameters are updated.
// maxAmount is a decimal value (e.g., 1000.00).
// Name and description are trimmed of leading/trailing whitespace before storage.
// The caller provides the current timestamp (now) to enable deterministic testing via clock injection.
func (l *Limit) Update(
	name *string,
	maxAmount *decimal.Decimal,
	description *string,
	scopes *[]Scope,
	now time.Time,
) error {
	updated := false

	if name != nil {
		normalizedName := strings.TrimSpace(*name)
		if err := validateName(normalizedName); err != nil {
			return err
		}

		l.Name = normalizedName
		updated = true
	}

	if maxAmount != nil {
		if err := validateMaxAmount(*maxAmount); err != nil {
			return err
		}

		l.MaxAmount = *maxAmount
		updated = true
	}

	if description != nil {
		normalizedDescription := strings.TrimSpace(*description)
		if err := validateDescription(&normalizedDescription); err != nil {
			return err
		}

		l.Description = &normalizedDescription
		updated = true
	}

	if scopes != nil {
		if err := validateScopes(*scopes); err != nil {
			return err
		}

		// Defensive copy to prevent external mutation
		l.Scopes = append([]Scope(nil), *scopes...)
		updated = true
	}

	if updated {
		l.UpdatedAt = now.UTC()
	}

	return nil
}

// validStatusTransitions defines allowed state transitions.
// DELETED is a terminal state - no transitions allowed from it.
// State machine (aligned with Rules):
// - DRAFT → ACTIVE (activate), DRAFT → DELETED (delete)
// - ACTIVE → INACTIVE (deactivate) - ACTIVE limits CANNOT be deleted directly
// - INACTIVE → ACTIVE (reactivate), INACTIVE → DRAFT (recovery), INACTIVE → DELETED (delete)
var validStatusTransitions = map[LimitStatus][]LimitStatus{
	LimitStatusDraft:    {LimitStatusActive, LimitStatusDeleted},
	LimitStatusActive:   {LimitStatusInactive},
	LimitStatusInactive: {LimitStatusActive, LimitStatusDraft, LimitStatusDeleted},
	LimitStatusDeleted:  {}, // Terminal state
}

// SetStatus changes the limit status with transition validation.
// Idempotent: same-status transitions are no-ops (return nil without updating timestamp).
// DELETED is a terminal state and cannot be transitioned from.
// The caller provides the current timestamp (now) to enable deterministic testing via clock injection.
func (l *Limit) SetStatus(status LimitStatus, now time.Time) error {
	if !status.IsValid() {
		return constant.ErrLimitInvalidStatusChange
	}

	// Idempotency: same status is a no-op
	if l.Status == status {
		return nil
	}

	// Check if transition is allowed
	allowedTransitions := validStatusTransitions[l.Status]
	isValidTransition := false

	for _, allowed := range allowedTransitions {
		if status == allowed {
			isValidTransition = true

			break
		}
	}

	if !isValidTransition {
		return constant.ErrLimitInvalidStatusChange
	}

	l.Status = status
	l.UpdatedAt = now.UTC()

	// Maintain DeletedAt invariant: set when DELETED, clear otherwise
	if status == LimitStatusDeleted {
		utcNow := now.UTC()
		l.DeletedAt = &utcNow
	} else {
		l.DeletedAt = nil
	}

	return nil
}

// IsActive checks if limit is currently active.
func (l *Limit) IsActive() bool {
	return l.Status == LimitStatusActive
}

// Validate ensures Limit entity is valid.
func (l *Limit) Validate() error {
	if err := validateName(l.Name); err != nil {
		return err
	}

	if err := validateDescription(l.Description); err != nil {
		return err
	}

	if !l.LimitType.IsValid() {
		return constant.ErrLimitInvalidType
	}

	if err := validateMaxAmount(l.MaxAmount); err != nil {
		return err
	}

	if err := validateCurrency(l.Currency); err != nil {
		return err
	}

	if err := validateScopes(l.Scopes); err != nil {
		return err
	}

	if !l.Status.IsValid() {
		return constant.ErrLimitInvalidStatusChange
	}

	// Enforce DeletedAt invariant: must be set iff status is DELETED
	if l.Status == LimitStatusDeleted && l.DeletedAt == nil {
		return constant.ErrLimitDeletedAtInvariant
	}

	if l.Status != LimitStatusDeleted && l.DeletedAt != nil {
		return constant.ErrLimitDeletedAtInvariant
	}

	return nil
}

// NewUsageCounter creates a new UsageCounter entity.
// Returns constant.ErrUsageCounterLimitIDRequired if limitID is uuid.Nil.
// Returns constant.ErrUsageCounterScopeKeyRequired if scopeKey is empty or whitespace-only.
// Returns constant.ErrUsageCounterPeriodKeyRequired if periodKey is empty or whitespace-only.
// ScopeKey and periodKey are trimmed of leading/trailing whitespace before storage.
func NewUsageCounter(
	limitID uuid.UUID,
	scopeKey string,
	periodKey string,
	createdAt time.Time,
) (*UsageCounter, error) {
	if limitID == uuid.Nil {
		return nil, constant.ErrUsageCounterLimitIDRequired
	}

	normalizedScopeKey := strings.TrimSpace(scopeKey)
	if normalizedScopeKey == "" {
		return nil, constant.ErrUsageCounterScopeKeyRequired
	}

	normalizedPeriodKey := strings.TrimSpace(periodKey)
	if normalizedPeriodKey == "" {
		return nil, constant.ErrUsageCounterPeriodKeyRequired
	}

	return &UsageCounter{
		ID:            uuid.New(),
		LimitID:       limitID,
		ScopeKey:      normalizedScopeKey,
		PeriodKey:     normalizedPeriodKey,
		CurrentUsage:  decimal.Zero,
		LastUpdatedAt: createdAt.UTC(),
	}, nil
}

// Increment adds amount to current usage.
// amount is a decimal value.
// Returns constant.ErrUsageCounterIncrementNonNegative if amount < 0.
func (u *UsageCounter) Increment(amount decimal.Decimal, now time.Time) error {
	if amount.IsNegative() {
		return constant.ErrUsageCounterIncrementNonNegative
	}

	if amount.IsZero() {
		return nil
	}

	u.CurrentUsage = u.CurrentUsage.Add(amount)
	u.LastUpdatedAt = now.UTC()

	return nil
}

// Validate ensures UsageCounter is valid.
// ScopeKey and PeriodKey are invalid if empty or whitespace-only.
func (u *UsageCounter) Validate() error {
	if u.LimitID == uuid.Nil {
		return constant.ErrUsageCounterLimitIDRequired
	}

	if strings.TrimSpace(u.ScopeKey) == "" {
		return constant.ErrUsageCounterScopeKeyRequired
	}

	if strings.TrimSpace(u.PeriodKey) == "" {
		return constant.ErrUsageCounterPeriodKeyRequired
	}

	if u.CurrentUsage.IsNegative() {
		return constant.ErrUsageCounterCurrentUsageNegative
	}

	return nil
}

// ListLimitsFilter defines filters for listing limits.
// Cursor-based pagination: Cursor contains base64-encoded cursor with sort info.
type ListLimitsFilter struct {
	Status    *LimitStatus `json:"status,omitempty"`
	LimitType *LimitType   `json:"limitType,omitempty"`
	Currency  *string      `json:"currency,omitempty"`
	Limit     int          `json:"limit"`
	Cursor    string       `json:"cursor,omitempty"`
	SortBy    string       `json:"sortBy,omitempty"`
	SortOrder string       `json:"sortOrder,omitempty"`
}

// DefaultLimitSortField is the default sort column for limit queries.
const DefaultLimitSortField = "createdAt"

// validLimitSortFields defines the whitelist of valid sort fields for limits.
// This is the single source of truth - used by both model validation and repository.
// Unexported with read-only access via IsValidLimitSortField() to prevent external mutation.
var validLimitSortFields = map[string]bool{
	"name":                true,
	DefaultLimitSortField: true,
	"updatedAt":           true,
	"maxAmount":           true,
}

// IsValidLimitSortField checks if the given field is a valid sort field for limits.
func IsValidLimitSortField(field string) bool {
	return validLimitSortFields[field]
}

// ApplyDefaults sets default values for Limit, SortBy, and SortOrder fields.
// This method mutates the filter in-place.
// - Limit defaults to constant.DefaultPaginationLimit if <= 0
// - Limit is capped at constant.MaxPaginationLimit
// - SortBy defaults to "createdAt" if empty (camelCase, case-sensitive)
// - SortOrder defaults to "desc" if empty, normalized to lowercase
func (f *ListLimitsFilter) ApplyDefaults() {
	if f.Limit <= 0 {
		f.Limit = constant.DefaultPaginationLimit
	} else if f.Limit > constant.MaxPaginationLimit {
		f.Limit = constant.MaxPaginationLimit
	}

	if f.SortBy == "" {
		f.SortBy = DefaultLimitSortField
	}
	// Note: SortBy is NOT lowercased because it uses camelCase (e.g., "createdAt")

	if f.SortOrder == "" {
		f.SortOrder = string(constant.Desc)
	} else {
		f.SortOrder = strings.ToLower(f.SortOrder)
	}
}

// Validate ensures ListLimitsFilter has valid values.
// Call ApplyDefaults() before Validate() if you want defaults applied.
// Returns error if Status, LimitType, Limit, SortBy, or SortOrder are invalid.
func (f *ListLimitsFilter) Validate() error {
	if f.Status != nil && !f.Status.IsValid() {
		return constant.ErrLimitInvalidStatusFilter
	}

	if f.LimitType != nil && !f.LimitType.IsValid() {
		return constant.ErrLimitInvalidTypeFilter
	}

	if f.Limit <= 0 {
		return constant.ErrPaginationLimitInvalid
	}

	if f.Limit > constant.MaxPaginationLimit {
		return constant.ErrPaginationLimitExceeded
	}

	if f.SortBy != "" && !IsValidLimitSortField(f.SortBy) {
		return constant.ErrInvalidSortColumn
	}

	if f.SortOrder != "" {
		sortOrder := strings.ToLower(f.SortOrder)
		if sortOrder != string(constant.Asc) && sortOrder != string(constant.Desc) {
			return constant.ErrInvalidSortOrder
		}
	}

	return nil
}

// ListLimitsResult defines the result of listing limits.
type ListLimitsResult struct {
	Limits     []Limit `json:"limits"`
	NextCursor string  `json:"nextCursor,omitempty"`
	HasMore    bool    `json:"hasMore"`
}

// UsageSnapshot represents aggregated usage information for a limit.
// This is the response structure for GetLimitUsage as defined in api-design.md section 4.3.3.
// For PER_TRANSACTION limits, CurrentUsage is always 0 and ResetAt is nil.
type UsageSnapshot struct {
	// Limit identifier
	LimitID uuid.UUID `json:"limitId" swaggertype:"string" format:"uuid"`
	// Current usage amount (sum of all counters)
	CurrentUsage decimal.Decimal `json:"currentUsage" swaggertype:"string" example:"500.00"`
	// Total limit amount (from Limit.MaxAmount)
	LimitAmount decimal.Decimal `json:"limitAmount" swaggertype:"string" example:"1000.00"`
	// Usage percentage (currentUsage / limitAmount * 100)
	UtilizationPercent float64 `json:"utilizationPercent" example:"50.0"`
	// True if usage > 80%
	NearLimit bool `json:"nearLimit" example:"false"`
	// When counter resets (nil for PER_TRANSACTION)
	ResetAt *time.Time `json:"resetAt,omitempty" format:"date-time"`
}

// NearLimitThreshold is the threshold percentage (80%) above which nearLimit is true.
const NearLimitThreshold = 80.0

// NewUsageSnapshot creates a UsageSnapshot from a Limit and its usage counters.
// For PER_TRANSACTION limits, currentUsage is always 0 and resetAt is nil.
func NewUsageSnapshot(limit *Limit, counters []UsageCounter) *UsageSnapshot {
	currentUsage := decimal.Zero

	// For PER_TRANSACTION limits, currentUsage is always 0
	if limit.LimitType != LimitTypePerTransaction {
		for _, counter := range counters {
			currentUsage = currentUsage.Add(counter.CurrentUsage)
		}
	}

	// Calculate utilization percentage
	var utilizationPercent float64
	if limit.MaxAmount.IsPositive() {
		// (currentUsage / maxAmount) * 100
		utilizationPercent, _ = currentUsage.Div(limit.MaxAmount).Mul(decimal.NewFromInt(100)).Float64()
	}

	// nearLimit is true if usage > 80% (strictly greater, not >=)
	nearLimit := utilizationPercent > NearLimitThreshold

	// ResetAt is nil for PER_TRANSACTION limits
	var resetAt *time.Time
	if limit.LimitType != LimitTypePerTransaction {
		resetAt = limit.ResetAt
	}

	return &UsageSnapshot{
		LimitID:            limit.ID,
		CurrentUsage:       currentUsage,
		LimitAmount:        limit.MaxAmount,
		UtilizationPercent: utilizationPercent,
		NearLimit:          nearLimit,
		ResetAt:            resetAt,
	}
}
