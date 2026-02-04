// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

// Package cel provides CEL expression compilation and evaluation.
package cel

import (
	"context"
	"errors"
	"fmt"
	"time"

	libCommons "github.com/LerianStudio/lib-commons/v2/commons"
	libLog "github.com/LerianStudio/lib-commons/v2/commons/log"
	libOtel "github.com/LerianStudio/lib-commons/v2/commons/opentelemetry"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/checker"

	"tracer/pkg/constant"
	"tracer/pkg/logging"
	"tracer/pkg/model"
)

// defaultCostEstimator provides default size estimates for CEL cost calculation.
// It implements checker.CostEstimator interface with conservative estimates.
type defaultCostEstimator struct{}

// EstimateSize returns a default size estimate for variable-sized elements.
// Uses conservative estimates to prevent underestimation of expression cost.
func (e *defaultCostEstimator) EstimateSize(element checker.AstNode) *checker.SizeEstimate {
	// Return a conservative default size for unknown elements
	// This ensures expressions with variable-sized inputs are properly costed
	estimate := checker.SizeEstimate{Min: 0, Max: 100}
	return &estimate
}

// EstimateCallCost returns nil to use default cost estimation for function calls.
func (e *defaultCostEstimator) EstimateCallCost(function, overloadID string, target *checker.AstNode, args []checker.AstNode) *checker.CallEstimate {
	return nil
}

// safePrefix returns the first n characters of s, or the whole string if shorter.
func safePrefix(s string, n int) string {
	if len(s) <= n {
		return s
	}

	return s[:n]
}

// ExpressionEngine compiles and evaluates CEL expressions.
// Interface defined locally per Ring pattern (depend on abstractions, not concretions).
type ExpressionEngine interface {
	// Compile validates, compiles, and caches a CEL expression.
	// Returns a CompiledProgram that can be used for evaluation.
	Compile(ctx context.Context, expression string) (*CompiledProgram, error)

	// Evaluate runs a compiled program against a ValidationRequest.
	// Returns the boolean result of the expression.
	Evaluate(ctx context.Context, program *CompiledProgram, req *model.ValidationRequest) (bool, error)

	// Invalidate removes an expression from the cache by its hash.
	Invalidate(ctx context.Context, expressionHash string) error

	// Stats returns cache statistics for monitoring.
	Stats() CacheStats
}

// AdapterConfig holds configuration for the CEL adapter.
type AdapterConfig struct {
	// CostLimit is the maximum cost for CEL expression evaluation.
	// Read from CEL_COST_LIMIT env var (default: 10000).
	CostLimit uint64

	// CacheMaxSize is the maximum number of compiled expressions to cache.
	// Read from CEL_CACHE_MAX_SIZE env var (default: 1000).
	CacheMaxSize int64
}

// Adapter implements ExpressionEngine using google/cel-go.
type Adapter struct {
	env       *Environment
	cache     *Cache
	logger    libLog.Logger
	costLimit uint64
}

// NewAdapter creates a CEL adapter with the given configuration and logger.
// Returns an error if logger is nil or environment creation fails.
func NewAdapter(cfg AdapterConfig, logger libLog.Logger) (*Adapter, error) {
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}

	env, err := NewEnvironment()
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL environment: %w", err)
	}

	costLimit := cfg.CostLimit
	if costLimit == 0 {
		costLimit = DefaultCostLimit
	}

	cacheMaxSize := cfg.CacheMaxSize
	if cacheMaxSize == 0 {
		cacheMaxSize = DefaultCacheMaxSize
	}

	return &Adapter{
		env:       env,
		cache:     NewCache(cacheMaxSize),
		logger:    logger,
		costLimit: costLimit,
	}, nil
}

// Compile validates, compiles, and caches a CEL expression.
// Uses OpenTelemetry tracing with span name: adapter.cel.compile
func (a *Adapter) Compile(ctx context.Context, expression string) (*CompiledProgram, error) {
	start := time.Now()

	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)
	logger = logging.WithTrace(ctx, logger)

	_, span := tracer.Start(ctx, "adapter.cel.compile")
	defer span.End()

	// Validate expression is not empty (fail fast before any processing)
	if expression == "" {
		err := fmt.Errorf("%w: expression cannot be empty", constant.ErrExpressionSyntax)
		libOtel.HandleSpanError(&span, "empty expression", err)

		return nil, err
	}

	// Compute expression hash
	hash := HashExpression(expression)

	if err := libOtel.SetSpanAttributesFromStruct(&span, "compile_input", map[string]any{
		"expression_hash":   hash,
		"expression_length": len(expression),
	}); err != nil {
		libOtel.HandleSpanError(&span, "Failed to set span attributes", err)
	}

	// Check cache first
	if prog, ok := a.cache.Get(hash); ok {
		if err := libOtel.SetSpanAttributesFromStruct(&span, "cache", map[string]any{"hit": true}); err != nil {
			libOtel.HandleSpanError(&span, "Failed to set span attributes", err)
		}

		return prog, nil
	}

	if err := libOtel.SetSpanAttributesFromStruct(&span, "cache", map[string]any{"hit": false}); err != nil {
		libOtel.HandleSpanError(&span, "Failed to set span attributes", err)
	}

	// Compile to AST
	ast, err := a.env.Compile(expression)
	if err != nil {
		// Use structured error classification from CompileError
		var (
			wrappedErr error
			compileErr *CompileError
		)

		if errors.As(err, &compileErr) {
			// Use the structured IsTypeError flag for deterministic classification
			if compileErr.IsTypeError {
				wrappedErr = fmt.Errorf("%w: %w", constant.ErrExpressionType, err)
				libOtel.HandleSpanError(&span, "type error", wrappedErr)
			} else {
				wrappedErr = fmt.Errorf("%w: %w", constant.ErrExpressionSyntax, err)
				libOtel.HandleSpanError(&span, "compilation failed", wrappedErr)
			}
		} else {
			// Fallback for unexpected error types (shouldn't happen with our Environment)
			wrappedErr = fmt.Errorf("%w: %w", constant.ErrExpressionSyntax, err)
			libOtel.HandleSpanError(&span, "compilation failed", wrappedErr)
		}

		return nil, wrappedErr
	}

	// Validate boolean return type
	if ast.OutputType() != cel.BoolType {
		err := fmt.Errorf("%w: expression returns %v, expected bool", constant.ErrExpressionType, ast.OutputType())
		libOtel.HandleSpanError(&span, "type validation failed", err)

		return nil, err
	}

	// Estimate expression cost and reject if it exceeds the limit
	// This prevents creation of expensive rules that would cause DoS at evaluation time
	costEstimate, err := checker.Cost(ast.NativeRep(), &defaultCostEstimator{})
	if err != nil {
		// Use distinct error for estimation failures vs actual cost exceeded
		costErr := fmt.Errorf("%w: %w", constant.ErrExpressionCostEstimation, err)
		libOtel.HandleSpanError(&span, "cost estimation failed", costErr)

		return nil, costErr
	}

	// Use the maximum estimated cost (worst case) for validation
	// costEstimate.Max is uint64, so we compare with costLimit
	if costEstimate.Max > a.costLimit {
		costErr := fmt.Errorf("%w: estimated cost %d exceeds limit %d", constant.ErrExpressionCostExceeded, costEstimate.Max, a.costLimit)
		libOtel.HandleSpanError(&span, "expression cost exceeds limit", costErr)

		if err := libOtel.SetSpanAttributesFromStruct(&span, "cost_validation", map[string]any{
			"estimated_cost_min": costEstimate.Min,
			"estimated_cost_max": costEstimate.Max,
			"cost_limit":         a.costLimit,
			"exceeded":           true,
		}); err != nil {
			libOtel.HandleSpanError(&span, "Failed to set span attributes", err)
		}

		return nil, costErr
	}

	if err := libOtel.SetSpanAttributesFromStruct(&span, "cost_validation", map[string]any{
		"estimated_cost_min": costEstimate.Min,
		"estimated_cost_max": costEstimate.Max,
		"cost_limit":         a.costLimit,
		"exceeded":           false,
	}); err != nil {
		libOtel.HandleSpanError(&span, "Failed to set span attributes", err)
	}

	// Create program (compile-time cost validation already done above via checker.Cost)
	program, err := a.env.Program(ast)
	if err != nil {
		progErr := fmt.Errorf("%w: %w", constant.ErrExpressionProgram, err)
		libOtel.HandleSpanError(&span, "program creation failed", progErr)

		return nil, progErr
	}

	// Build compiled program
	compiledAt := time.Now()
	compileTimeMs := compiledAt.Sub(start).Milliseconds()

	compiled := &CompiledProgram{
		ExpressionHash:   hash,
		SourceExpression: expression,
		Program:          program,
		CompiledAt:       compiledAt,
		CompileTimeMs:    compileTimeMs,
	}

	// Cache the compiled program
	a.cache.Set(compiled)

	if err := libOtel.SetSpanAttributesFromStruct(&span, "compile_result", map[string]any{
		"compile_time_ms": compileTimeMs,
	}); err != nil {
		libOtel.HandleSpanError(&span, "Failed to set span attributes", err)
	}

	logger.WithFields(
		"operation", "adapter.cel.compile",
		"expression.hash", safePrefix(hash, 8),
		"compile.time_ms", compileTimeMs,
	).Info("CEL expression compiled")

	return compiled, nil
}

// Evaluate runs a compiled program against a ValidationRequest.
// Uses OpenTelemetry tracing with span name: adapter.cel.evaluate
func (a *Adapter) Evaluate(ctx context.Context, program *CompiledProgram, req *model.ValidationRequest) (bool, error) {
	start := time.Now()

	_, tracer, _, _ := libCommons.NewTrackingFromContext(ctx) //nolint:dogsled // only tracer is needed from tracking context

	_, span := tracer.Start(ctx, "adapter.cel.evaluate")
	defer span.End()

	// Validate inputs
	if program == nil {
		err := fmt.Errorf("program is required")
		libOtel.HandleSpanError(&span, "nil program", err)

		return false, err
	}

	if program.Program == nil {
		err := fmt.Errorf("compiled program is nil")
		libOtel.HandleSpanError(&span, "nil compiled program", err)

		return false, err
	}

	if err := libOtel.SetSpanAttributesFromStruct(&span, "evaluate_input", map[string]any{
		"expression_hash": program.ExpressionHash,
	}); err != nil {
		libOtel.HandleSpanError(&span, "Failed to set span attributes", err)
	}

	// Validate request is not nil
	if req == nil {
		err := fmt.Errorf("validation request is required")
		libOtel.HandleSpanError(&span, "nil request", err)

		return false, err
	}

	// Build activation from request
	activation, err := BuildActivation(req)
	if err != nil {
		wrappedErr := fmt.Errorf("%w: failed to build activation: %w", constant.ErrExpressionEvaluation, err)
		libOtel.HandleSpanError(&span, "failed to build activation", wrappedErr)

		return false, wrappedErr
	}

	// Evaluate
	out, _, err := program.Program.Eval(activation)
	if err != nil {
		evalErr := fmt.Errorf("%w: %w", constant.ErrExpressionEvaluation, err)
		libOtel.HandleSpanError(&span, "evaluation failed", evalErr)

		return false, evalErr
	}

	// Extract boolean result
	result, ok := out.Value().(bool)
	if !ok {
		err := fmt.Errorf("%w: expected bool, got %T", constant.ErrExpressionType, out.Value())
		libOtel.HandleSpanError(&span, "type assertion failed", err)

		return false, err
	}

	// Record span attributes
	durationMs := time.Since(start).Milliseconds()

	if err := libOtel.SetSpanAttributesFromStruct(&span, "evaluate_result", map[string]any{
		"duration_ms": durationMs,
		"result":      result,
	}); err != nil {
		libOtel.HandleSpanError(&span, "Failed to set span attributes", err)
	}

	return result, nil
}

// Invalidate removes an expression from the cache by its hash.
// Uses OpenTelemetry tracing with span name: adapter.cel.invalidate
// Propagates ctx through logging and tracing for observability.
func (a *Adapter) Invalidate(ctx context.Context, expressionHash string) error {
	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)
	logger = logging.WithTrace(ctx, logger)

	_, span := tracer.Start(ctx, "adapter.cel.invalidate")
	defer span.End()

	// Set span attributes for the invalidation operation
	if err := libOtel.SetSpanAttributesFromStruct(&span, "invalidate_input", map[string]any{
		"expression_hash": expressionHash,
	}); err != nil {
		libOtel.HandleSpanError(&span, "Failed to set span attributes", err)
	}

	// Invalidate from cache
	a.cache.Invalidate(expressionHash)

	// Log with trace context
	logger.WithFields(
		"operation", "adapter.cel.invalidate",
		"expression.hash", safePrefix(expressionHash, 8),
	).Info("CEL expression invalidated")

	return nil
}

// Stats returns cache statistics for monitoring.
func (a *Adapter) Stats() CacheStats {
	return a.cache.Stats()
}

// Ensure Adapter implements ExpressionEngine at compile time.
var _ ExpressionEngine = (*Adapter)(nil)
