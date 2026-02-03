// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

// Package cel provides CEL environment setup for transaction validation.
// This adapter creates the CEL environment with all required variables
// aligned with model.ValidationRequest structure.
package cel

import (
	"fmt"
	"strings"

	"tracer/pkg/model"

	"github.com/google/cel-go/cel"
)

// CompileError wraps CEL compilation errors with structured issue information.
// It provides deterministic error classification without string matching.
type CompileError struct {
	Issues      *cel.Issues
	IsTypeError bool // True if any issue is a type-related error
	Message     string
}

// Error implements the error interface.
func (e *CompileError) Error() string {
	return e.Message
}

// Unwrap returns the underlying error for errors.Is/As support.
func (e *CompileError) Unwrap() error {
	if e.Issues != nil {
		return e.Issues.Err()
	}

	return nil
}

// classifyIssues analyzes CEL issues to determine if they are type-related.
// Type errors include: type mismatches, overload resolution failures, undeclared references.
// Syntax errors include: parsing failures, invalid tokens.
func classifyIssues(issues *cel.Issues) bool {
	if issues == nil {
		return false
	}

	for _, issue := range issues.Errors() {
		msg := issue.Message
		// Type-related error patterns from cel-go:
		// - "no matching overload" - function called with wrong argument types
		// - "type mismatch" - incompatible types in operation
		// - "undeclared reference" - using undefined variable (type resolution failure)
		// - "found no matching overload" - alternative phrasing
		if strings.Contains(msg, "no matching overload") ||
			strings.Contains(msg, "type mismatch") ||
			strings.Contains(msg, "undeclared reference") ||
			strings.Contains(msg, "found no matching overload") {
			return true
		}
	}

	return false
}

// Environment wraps cel.Env to provide a cleaner API for expression compilation.
// It encapsulates the CEL environment configured with all transaction context variables.
type Environment struct {
	env *cel.Env
}

// Compile compiles a CEL expression and returns the AST.
// Returns a *CompileError if the expression has syntax or type errors.
// The CompileError contains structured issue information for deterministic error classification.
func (e *Environment) Compile(expression string) (*cel.Ast, error) {
	ast, issues := e.env.Compile(expression)
	if issues != nil && issues.Err() != nil {
		return nil, &CompileError{
			Issues:      issues,
			IsTypeError: classifyIssues(issues),
			Message:     issues.Err().Error(),
		}
	}

	return ast, nil
}

// Program creates an evaluable program from a compiled AST.
func (e *Environment) Program(ast *cel.Ast, opts ...cel.ProgramOption) (cel.Program, error) {
	return e.env.Program(ast, opts...)
}

// CELEnv returns the underlying cel.Env for advanced usage.
func (e *Environment) CELEnv() *cel.Env {
	return e.env
}

// NewEnvironment creates a CEL environment with all required transaction context variables.
// Variables are aligned with model.ValidationRequest structure:
//   - transactionType (string): CARD, WIRE, PIX, CRYPTO
//   - subType (string): debit, credit, instant, etc. (optional, empty string if nil)
//   - amount (int): Amount in smallest currency unit (e.g., cents)
//   - currency (string): ISO 4217 currency code
//   - account (map[string]dyn): Account context with id, type, status, metadata
//   - segment (map[string]dyn): Segment context (optional, empty map if nil)
//   - portfolio (map[string]dyn): Portfolio context (optional, empty map if nil)
//   - merchant (map[string]dyn): Merchant context (optional, empty map if nil)
//   - metadata (map[string]dyn): Custom metadata fields
//   - transactionTimestamp (int): Unix timestamp in nanoseconds
func NewEnvironment() (*Environment, error) {
	env, err := cel.NewEnv(
		// Transaction fields (from ValidationRequest)
		cel.Variable("transactionType", cel.StringType),
		cel.Variable("subType", cel.StringType),
		cel.Variable("amount", cel.IntType),
		cel.Variable("currency", cel.StringType),

		// Context objects (maps for flexible field access)
		cel.Variable("account", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("segment", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("portfolio", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("merchant", cel.MapType(cel.StringType, cel.DynType)),

		// Metadata (custom fields)
		cel.Variable("metadata", cel.MapType(cel.StringType, cel.DynType)),

		// Timestamp (Unix timestamp in nanoseconds for precise time-based expressions)
		cel.Variable("transactionTimestamp", cel.IntType),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL environment: %w", err)
	}

	return &Environment{env: env}, nil
}

// BuildActivation converts a model.ValidationRequest to a CEL activation map.
// All fields are mapped to their corresponding CEL variable types.
// Optional fields (subType, merchant) are converted to empty values when nil.
// Amount is expected in smallest currency unit (e.g., cents).
// TransactionTimestamp is in Unix nanoseconds (use transactionTimestamp / 1000000000 in expressions for seconds).
func BuildActivation(req *model.ValidationRequest) (map[string]any, error) {
	if req == nil {
		return nil, fmt.Errorf("validation request is required")
	}

	activation := make(map[string]any)

	// Transaction type (enum converted to string)
	activation["transactionType"] = string(req.TransactionType)

	// SubType (optional - empty string if nil)
	if req.SubType != nil {
		activation["subType"] = *req.SubType
	} else {
		activation["subType"] = ""
	}

	// Amount (int64, in smallest currency unit)
	activation["amount"] = req.Amount

	// Currency (ISO 4217 string)
	activation["currency"] = req.Currency

	// Account context (map with id, type, status, metadata)
	activation["account"] = req.Account.ToMap()

	// Segment context (optional - empty map if nil)
	if req.Segment != nil {
		activation["segment"] = req.Segment.ToMap()
	} else {
		activation["segment"] = emptyMap()
	}

	// Portfolio context (optional - empty map if nil)
	if req.Portfolio != nil {
		activation["portfolio"] = req.Portfolio.ToMap()
	} else {
		activation["portfolio"] = emptyMap()
	}

	// Merchant context (optional - empty map if nil)
	if req.Merchant != nil {
		activation["merchant"] = req.Merchant.ToMap()
	} else {
		activation["merchant"] = emptyMap()
	}

	// Metadata (optional - empty map if nil)
	activation["metadata"] = safeMetadata(req.Metadata)

	// TransactionTimestamp (Unix timestamp in nanoseconds)
	activation["transactionTimestamp"] = req.TransactionTimestamp.UnixNano()

	return activation, nil
}

// emptyMap returns an empty map for CEL activation.
// Used for optional fields when the source is nil.
func emptyMap() map[string]any {
	return map[string]any{}
}

// safeMetadata returns metadata or empty map if nil.
// Ensures CEL expressions can safely access metadata fields.
func safeMetadata(metadata map[string]any) map[string]any {
	if metadata != nil {
		return metadata
	}

	return emptyMap()
}
