// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package in

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"

	"tracer/internal/services/command"
	"tracer/pkg/constant"
	"tracer/pkg/model"
)

// Validation constants define the limits for limit input fields.
// Note: These values must match the validation tags in the structs below.
const (
	MaxLimitNameLength        = 255
	MaxLimitDescriptionLength = 1000
	MaxLimitScopesCount       = 100
	MaxLimitSubTypeLength     = 50
	// MaxUsageCountersPerLimit bounds the number of usage counters returned for a single limit.
	// Calculated as: max_scopes (100) × reasonable_period_history (~10 months).
	// Provides DoS protection and documents API expectations for tooling validation.
	MaxUsageCountersPerLimit = 1000
)

// registerLimitValidations registers limit-specific validation functions.
// Returns an error if any validator registration fails.
func registerLimitValidations(v *validator.Validate) error {
	// limittype validates that LimitType is a valid enum value
	if err := v.RegisterValidation("limittype", validateLimitType); err != nil {
		return fmt.Errorf("failed to register limittype validator: %w", err)
	}

	// limitstatus validates that LimitStatus is a valid enum value
	if err := v.RegisterValidation("limitstatus", validateLimitStatus); err != nil {
		return fmt.Errorf("failed to register limitstatus validator: %w", err)
	}

	return nil
}

// validateLimitType validates that the LimitType is a valid enum value.
func validateLimitType(fl validator.FieldLevel) bool {
	field := fl.Field()

	if field.Kind() == reflect.Ptr {
		if field.IsNil() {
			return true
		}

		field = field.Elem()
	}

	limitType := model.LimitType(field.String())

	return limitType.IsValid()
}

// validateLimitStatus validates that the LimitStatus is a valid enum value.
func validateLimitStatus(fl validator.FieldLevel) bool {
	field := fl.Field()

	if field.Kind() == reflect.Ptr {
		if field.IsNil() {
			return true
		}

		field = field.Elem()
	}

	status := model.LimitStatus(field.String())

	return status.IsValid()
}

// CreateLimitInput represents the HTTP request body for creating a limit.
type CreateLimitInput struct {
	Name        string          `json:"name" validate:"required,min=1,max=255"`
	Description *string         `json:"description,omitempty" validate:"omitempty,max=1000"`
	LimitType   model.LimitType `json:"limitType" validate:"required,limittype"`
	MaxAmount   int64           `json:"maxAmount" validate:"required,gt=0" minimum:"1"`
	Currency    string          `json:"currency" validate:"required,len=3,uppercase" minLength:"3" maxLength:"3" example:"USD"`
	Scopes      []model.Scope   `json:"scopes" validate:"required,min=1,max=100,dive,scopenotempty"`
}

// Validate validates the CreateLimitInput struct using validator/v10.
func (i *CreateLimitInput) Validate() error {
	v, err := getValidator()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrValidatorInit, err)
	}

	if err := v.Struct(i); err != nil {
		return formatLimitValidationError(err)
	}

	return nil
}

// UpdateLimitInput represents the HTTP request body for updating a limit.
type UpdateLimitInput struct {
	Name        *string        `json:"name,omitempty" validate:"omitempty,min=1,max=255"`
	Description *string        `json:"description,omitempty" validate:"omitempty,max=1000"`
	MaxAmount   *int64         `json:"maxAmount,omitempty" validate:"omitempty,gt=0" minimum:"1"`
	Scopes      *[]model.Scope `json:"scopes,omitempty" validate:"omitempty,min=1,max=100,dive,scopenotempty"`
}

// Validate validates the UpdateLimitInput struct using validator/v10.
func (i *UpdateLimitInput) Validate() error {
	v, err := getValidator()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrValidatorInit, err)
	}

	if err := v.Struct(i); err != nil {
		return formatLimitValidationError(err)
	}

	return nil
}

// IsEmpty returns true if no fields are set for update.
func (i *UpdateLimitInput) IsEmpty() bool {
	return i.Name == nil && i.MaxAmount == nil && i.Description == nil && i.Scopes == nil
}

// ListLimitsInput represents query parameters for listing limits.
type ListLimitsInput struct {
	Limit     *int   `query:"limit"` // Changed to *int to distinguish nil (use default) from 0 (invalid)
	Cursor    string `query:"cursor"`
	Status    string `query:"status" enums:"DRAFT,ACTIVE,INACTIVE"`
	LimitType string `query:"limitType" enums:"DAILY,MONTHLY,PER_TRANSACTION"`
	SortBy    string `query:"sortBy" enums:"createdAt,updatedAt,name,maxAmount"`
	SortOrder string `query:"sortOrder" enums:"ASC,DESC"`
}

// SetDefaults applies default values.
// Note: SortBy and SortOrder defaults are only applied when cursor is not present,
// because cursor already contains sort configuration (TRC-0045).
func (i *ListLimitsInput) SetDefaults() {
	if i.Limit == nil {
		defaultLimit := constant.DefaultPaginationLimit
		i.Limit = &defaultLimit
	}

	// Only apply sort defaults when not using cursor pagination
	// Cursor already contains sort configuration from the original request (TRC-0045)
	if i.Cursor == "" {
		if i.SortBy == "" {
			i.SortBy = "createdAt"
		}

		// Normalize sortOrder to uppercase and apply default if empty
		i.SortOrder = NormalizeSortOrder(i.SortOrder, "DESC")
	}
}

// Validate validates the ListLimitsInput struct.
func (i *ListLimitsInput) Validate() error {
	// Validate pagination limit with specific error codes (TRC-0040, TRC-0041)
	if err := ValidatePaginationLimit(i.Limit, 100); err != nil {
		return err
	}

	// Validate cursor consistency (TRC-0045)
	if err := ValidateCursorConsistency(i.Cursor, i.SortBy, i.SortOrder); err != nil {
		return err
	}

	// Validate sortBy whitelist (TRC-0043)
	allowedSortFields := []string{"createdAt", "updatedAt", "name", "maxAmount"}
	if err := ValidateSortBy(i.SortBy, allowedSortFields); err != nil {
		return err
	}

	// Validate sortOrder enum (TRC-0042)
	if err := ValidateSortOrder(i.SortOrder); err != nil {
		return err
	}

	// Validate status enum using model's IsValid method
	if i.Status != "" {
		status := model.LimitStatus(i.Status)
		if !status.IsValid() {
			return &ValidationError{
				Code:    "TRC-0006",
				Message: "status must be one of [DRAFT ACTIVE INACTIVE]",
			}
		}
		// Prevent DELETED status in list filters (soft-deleted records should not be queried)
		if status == model.LimitStatusDeleted {
			return &ValidationError{
				Code:    "TRC-0006",
				Message: "status filter does not allow DELETED",
			}
		}
	}

	// Validate limitType enum using model's IsValid method
	if i.LimitType != "" {
		limitType := model.LimitType(i.LimitType)
		if !limitType.IsValid() {
			return &ValidationError{
				Code:    "TRC-0006",
				Message: "limitType must be one of [DAILY MONTHLY PER_TRANSACTION]",
			}
		}
	}

	return nil
}

// ListLimitsResponse represents the HTTP response for listing limits.
type ListLimitsResponse struct {
	Limits     []model.Limit `json:"limits"`
	NextCursor string        `json:"nextCursor,omitempty"`
	HasMore    bool          `json:"hasMore"`
}

// Note: UsageSnapshot is now defined in pkg/model/limit.go
// The HTTP response uses model.UsageSnapshot directly.

// ToCreateLimitServiceInput converts HTTP CreateLimitInput to service CreateLimitInput.
func ToCreateLimitServiceInput(input *CreateLimitInput) *command.CreateLimitInput {
	scopes := make([]model.Scope, len(input.Scopes))
	copy(scopes, input.Scopes)

	return &command.CreateLimitInput{
		Name:        input.Name,
		Description: input.Description,
		LimitType:   input.LimitType,
		MaxAmount:   input.MaxAmount,
		Currency:    input.Currency,
		Scopes:      scopes,
	}
}

// ToUpdateLimitServiceInput converts HTTP UpdateLimitInput to service UpdateLimitInput.
func ToUpdateLimitServiceInput(input *UpdateLimitInput) *command.UpdateLimitInput {
	result := &command.UpdateLimitInput{
		Name:        input.Name,
		MaxAmount:   input.MaxAmount,
		Description: input.Description,
	}

	if input.Scopes != nil {
		scopes := make([]model.Scope, len(*input.Scopes))
		copy(scopes, *input.Scopes)

		result.Scopes = &scopes
	}

	return result
}

// ToListLimitsFilter converts HTTP ListLimitsInput to model ListLimitsFilter.
// Safely handles nil input.Limit by defaulting to constant.DefaultPaginationLimit.
// SortBy is passed as camelCase; the repository converts to snake_case for DB queries.
func ToListLimitsFilter(input *ListLimitsInput) *model.ListLimitsFilter {
	limit := constant.DefaultPaginationLimit
	if input.Limit != nil {
		limit = *input.Limit
	}

	filter := &model.ListLimitsFilter{
		Limit:     limit,
		Cursor:    input.Cursor,
		SortBy:    input.SortBy, // Pass camelCase directly; repository converts to snake_case
		SortOrder: strings.ToUpper(input.SortOrder),
	}

	if input.Status != "" {
		status := model.LimitStatus(input.Status)
		filter.Status = &status
	}

	if input.LimitType != "" {
		limitType := model.LimitType(input.LimitType)
		filter.LimitType = &limitType
	}

	return filter
}

// ToListLimitsResponse converts model ListLimitsResult to HTTP ListLimitsResponse.
func ToListLimitsResponse(result *model.ListLimitsResult) *ListLimitsResponse {
	// Ensure Limits is never nil to avoid "limits": null in JSON response
	limits := result.Limits
	if limits == nil {
		limits = []model.Limit{}
	}

	return &ListLimitsResponse{
		Limits:     limits,
		NextCursor: result.NextCursor,
		HasMore:    result.HasMore,
	}
}

// formatLimitValidationError formats validator errors into user-friendly messages for limits.
func formatLimitValidationError(err error) error {
	validationErrors, ok := err.(validator.ValidationErrors)
	if !ok {
		return err
	}

	if len(validationErrors) == 0 {
		return nil
	}

	// Process first error only
	fieldError := validationErrors[0]
	fieldName := fieldError.Field()
	tag := fieldError.Tag()
	namespace := fieldError.Namespace()

	// Check if this is a scope field error (from dive validation)
	if isLimitScopeFieldError(namespace) {
		return formatLimitScopeFieldError(fieldError)
	}

	switch tag {
	case "required":
		return fmt.Errorf("%s is a required field", toLimitJSONFieldName(fieldName))
	case "min":
		jsonFieldName := toLimitJSONFieldName(fieldName)
		if jsonFieldName == "scopes" {
			return fmt.Errorf("%s must have at least %s item(s)", jsonFieldName, fieldError.Param())
		}

		return fmt.Errorf("%s must be at least %s characters", jsonFieldName, fieldError.Param())
	case "max":
		jsonFieldName := toLimitJSONFieldName(fieldName)
		if jsonFieldName == "scopes" {
			return fmt.Errorf("%s must have a maximum of %s items", jsonFieldName, fieldError.Param())
		}

		return fmt.Errorf("%s must be a maximum of %s characters", jsonFieldName, fieldError.Param())
	case "len":
		return fmt.Errorf("%s must be exactly %s characters", toLimitJSONFieldName(fieldName), fieldError.Param())
	case "uppercase":
		return fmt.Errorf("%s must be uppercase", toLimitJSONFieldName(fieldName))
	case "gt":
		return fmt.Errorf("%s must be greater than %s", toLimitJSONFieldName(fieldName), fieldError.Param())
	case "oneof":
		return fmt.Errorf("%s must be one of [%s]", toLimitJSONFieldName(fieldName), fieldError.Param())
	case "limittype":
		return fmt.Errorf("%s must be one of [DAILY MONTHLY PER_TRANSACTION]", toLimitJSONFieldName(fieldName))
	case "limitstatus":
		return fmt.Errorf("%s must be one of [DRAFT ACTIVE INACTIVE]", toLimitJSONFieldName(fieldName))
	default:
		return fmt.Errorf("%s validation failed: %s", toLimitJSONFieldName(fieldName), tag)
	}
}

// isLimitScopeFieldError checks if the error is from a scope field (via dive validation).
func isLimitScopeFieldError(namespace string) bool {
	return strings.Contains(namespace, "Scopes[")
}

// formatLimitScopeFieldError formats a scope field validation error for limits.
func formatLimitScopeFieldError(fieldError validator.FieldError) error {
	tag := fieldError.Tag()

	// Handle scopenotempty validation (applies to the whole scope, not a field)
	if tag == "scopenotempty" {
		index := extractLimitScopeIndex(fieldError.Namespace())
		if index == -1 {
			return fmt.Errorf("scope must have at least one field set")
		}

		return fmt.Errorf("scope at index %d must have at least one field set", index)
	}

	fieldName := toLimitScopeJSONFieldName(fieldError.Field())

	var msg string

	switch tag {
	case "uuid":
		msg = fmt.Sprintf("%s must be a valid UUID", fieldName)
	case "oneof":
		msg = fmt.Sprintf("%s must be one of [%s]", fieldName, fieldError.Param())
	case "transactiontype":
		msg = fmt.Sprintf("%s must be one of [CARD WIRE PIX CRYPTO]", fieldName)
	case "max":
		msg = fmt.Sprintf("%s must be a maximum of %s characters", fieldName, fieldError.Param())
	default:
		msg = fmt.Sprintf("%s validation failed: %s", fieldName, tag)
	}

	// Extract index from namespace (e.g., "CreateLimitInput.Scopes[0].SegmentID")
	index := extractLimitScopeIndex(fieldError.Namespace())
	if index == -1 {
		return fmt.Errorf("scope %s", msg)
	}

	return fmt.Errorf("scope at index %d: %s", index, msg)
}

// extractLimitScopeIndex extracts the scope index from the namespace.
// Returns -1 if no index is found, allowing 0 to be a valid index.
func extractLimitScopeIndex(namespace string) int {
	start := strings.Index(namespace, "Scopes[")
	if start == -1 {
		return -1
	}

	start += len("Scopes[")
	end := strings.Index(namespace[start:], "]")

	if end == -1 {
		return -1
	}

	var index int

	n, _ := fmt.Sscanf(namespace[start:start+end], "%d", &index)
	if n != 1 {
		return -1
	}

	return index
}

// toLimitJSONFieldName converts struct field name to JSON field name for limits.
func toLimitJSONFieldName(fieldName string) string {
	switch fieldName {
	case "Name":
		return "name"
	case "Description":
		return "description"
	case "LimitType":
		return "limitType"
	case "MaxAmount":
		return "maxAmount"
	case "Currency":
		return "currency"
	case "Scopes":
		return "scopes"
	case "Status":
		return "status"
	case "Limit":
		return "limit"
	case "Cursor":
		return "cursor"
	case "SortBy":
		return "sortBy"
	case "SortOrder":
		return "sortOrder"
	default:
		return fieldName
	}
}

// toLimitScopeJSONFieldName converts LimitScopeInput field name to JSON field name.
func toLimitScopeJSONFieldName(fieldName string) string {
	switch fieldName {
	case "SegmentID":
		return "segmentId"
	case "PortfolioID":
		return "portfolioId"
	case "AccountID":
		return "accountId"
	case "MerchantID":
		return "merchantId"
	case "TransactionType":
		return "transactionType"
	case "SubType":
		return "subType"
	default:
		return fieldName
	}
}
