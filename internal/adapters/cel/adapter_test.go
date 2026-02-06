// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package cel

import (
	"context"
	"testing"

	"tracer/internal/testutil"
	"tracer/pkg/constant"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)



// TestNewAdapter_Success tests successful adapter creation.
func TestNewAdapter_Success(t *testing.T) {
	t.Parallel()

	logger := testutil.NewMockLogger()
	cfg := AdapterConfig{
		CostLimit:    5000,
		CacheMaxSize: 500,
	}

	adapter, err := NewAdapter(cfg, logger)

	require.NoError(t, err, "NewAdapter should not return error")
	assert.NotNil(t, adapter, "Adapter should not be nil")
	assert.Equal(t, uint64(5000), adapter.costLimit)
}

// TestNewAdapter_DefaultValues tests that zero values use defaults.
func TestNewAdapter_DefaultValues(t *testing.T) {
	t.Parallel()

	logger := testutil.NewMockLogger()
	cfg := AdapterConfig{} // Zero values

	adapter, err := NewAdapter(cfg, logger)

	require.NoError(t, err)
	assert.Equal(t, DefaultCostLimit, adapter.costLimit)
}

// TestNewAdapter_NilLogger tests error for nil logger.
func TestNewAdapter_NilLogger(t *testing.T) {
	t.Parallel()

	cfg := AdapterConfig{}

	adapter, err := NewAdapter(cfg, nil)

	assert.Nil(t, adapter)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "logger")
}

// TestAdapter_Compile_Success tests successful compilation.
func TestAdapter_Compile_Success(t *testing.T) {
	t.Parallel()

	adapter := newTestAdapter(t)
	ctx := context.Background()

	program, err := adapter.Compile(ctx, "amount > 100000")

	require.NoError(t, err)
	assert.NotNil(t, program)
	assert.NotEmpty(t, program.ExpressionHash)
	assert.Equal(t, "amount > 100000", program.SourceExpression)
	assert.NotNil(t, program.Program)
}

// TestAdapter_Compile_CacheHit tests that cached expressions are returned.
func TestAdapter_Compile_CacheHit(t *testing.T) {
	t.Parallel()

	adapter := newTestAdapter(t)
	ctx := context.Background()
	expression := "amount > 100000"

	// First compilation
	program1, err := adapter.Compile(ctx, expression)
	require.NoError(t, err)

	// Second compilation should hit cache
	program2, err := adapter.Compile(ctx, expression)
	require.NoError(t, err)

	// Should be the same cached program
	assert.Equal(t, program1.ExpressionHash, program2.ExpressionHash)

	// Check cache stats
	stats := adapter.Stats()
	assert.Equal(t, int64(1), stats.Hits, "Should have 1 cache hit")
}

// TestAdapter_Compile_SyntaxError tests syntax error handling.
func TestAdapter_Compile_SyntaxError(t *testing.T) {
	t.Parallel()

	adapter := newTestAdapter(t)
	ctx := context.Background()

	program, err := adapter.Compile(ctx, "invalid syntax !!!")

	assert.Nil(t, program)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), constant.ErrExpressionSyntax.Error())
}

// TestAdapter_Compile_EmptyExpression tests empty expression error.
func TestAdapter_Compile_EmptyExpression(t *testing.T) {
	t.Parallel()

	adapter := newTestAdapter(t)
	ctx := context.Background()

	program, err := adapter.Compile(ctx, "")

	assert.Nil(t, program)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), constant.ErrExpressionSyntax.Error())
}

// TestAdapter_Compile_TypeError tests non-boolean expression error.
func TestAdapter_Compile_TypeError(t *testing.T) {
	t.Parallel()

	adapter := newTestAdapter(t)
	ctx := context.Background()

	// Expression that returns string, not bool
	program, err := adapter.Compile(ctx, "transactionType")

	assert.Nil(t, program)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), constant.ErrExpressionType.Error())
}

// TestAdapter_Evaluate_Success tests successful evaluation.
func TestAdapter_Evaluate_Success(t *testing.T) {
	t.Parallel()

	adapter := newTestAdapter(t)
	ctx := context.Background()

	program, err := adapter.Compile(ctx, "amount > 100000")
	require.NoError(t, err)

	req := newTestRequest()
	result, err := adapter.Evaluate(ctx, program, req)

	require.NoError(t, err)
	assert.True(t, result, "150000 > 100000 should be true")
}

// TestAdapter_Evaluate_False tests evaluation returning false.
func TestAdapter_Evaluate_False(t *testing.T) {
	t.Parallel()

	adapter := newTestAdapter(t)
	ctx := context.Background()

	program, err := adapter.Compile(ctx, "amount > 200000")
	require.NoError(t, err)

	req := newTestRequest()
	result, err := adapter.Evaluate(ctx, program, req)

	require.NoError(t, err)
	assert.False(t, result, "150000 > 200000 should be false")
}

// TestAdapter_Evaluate_ComplexExpression tests complex expression evaluation.
func TestAdapter_Evaluate_ComplexExpression(t *testing.T) {
	t.Parallel()

	adapter := newTestAdapter(t)
	ctx := context.Background()

	program, err := adapter.Compile(ctx, `transactionType == "PIX" && amount > 100000 && account["status"] == "active"`)
	require.NoError(t, err)

	req := newTestRequest()
	result, err := adapter.Evaluate(ctx, program, req)

	require.NoError(t, err)
	assert.True(t, result)
}

// TestAdapter_Evaluate_NilProgram tests error for nil program.
func TestAdapter_Evaluate_NilProgram(t *testing.T) {
	t.Parallel()

	adapter := newTestAdapter(t)
	ctx := context.Background()

	result, err := adapter.Evaluate(ctx, nil, newTestRequest())

	assert.False(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "program")
}

// TestAdapter_Evaluate_NilRequest tests error for nil request.
func TestAdapter_Evaluate_NilRequest(t *testing.T) {
	t.Parallel()

	adapter := newTestAdapter(t)
	ctx := context.Background()

	program, err := adapter.Compile(ctx, "amount > 100000")
	require.NoError(t, err)

	result, err := adapter.Evaluate(ctx, program, nil)

	assert.False(t, result)
	assert.Error(t, err)
}

// TestAdapter_Invalidate tests cache invalidation.
func TestAdapter_Invalidate(t *testing.T) {
	t.Parallel()

	adapter := newTestAdapter(t)
	ctx := context.Background()

	// Compile and cache
	program, err := adapter.Compile(ctx, "amount > 100000")
	require.NoError(t, err)

	// Verify in cache
	stats := adapter.Stats()
	assert.Equal(t, int64(1), stats.Size)

	// Invalidate
	err = adapter.Invalidate(ctx, program.ExpressionHash)
	require.NoError(t, err)

	// Verify removed from cache
	stats = adapter.Stats()
	assert.Equal(t, int64(0), stats.Size)
}

// TestAdapter_Stats tests cache statistics.
func TestAdapter_Stats(t *testing.T) {
	t.Parallel()

	adapter := newTestAdapter(t)
	ctx := context.Background()

	// Initial stats
	stats := adapter.Stats()
	assert.Equal(t, int64(0), stats.Size)
	assert.Equal(t, int64(0), stats.Hits)
	assert.Equal(t, int64(0), stats.Misses)

	// Compile (cache miss)
	_, err := adapter.Compile(ctx, "amount > 100000")
	require.NoError(t, err)

	stats = adapter.Stats()
	assert.Equal(t, int64(1), stats.Size)
	assert.Equal(t, int64(1), stats.Misses) // First compilation is a miss

	// Compile same (cache hit)
	_, err = adapter.Compile(ctx, "amount > 100000")
	require.NoError(t, err)

	stats = adapter.Stats()
	assert.Equal(t, int64(1), stats.Size)
	assert.Equal(t, int64(1), stats.Hits)
}

// TestAdapter_TracingSpans tests that spans are created.
func TestAdapter_TracingSpans(t *testing.T) {
	t.Parallel()

	tt := testutil.SetupTestTracing(t)

	ctx := context.Background()

	// Create adapter and perform operations
	adapter := newTestAdapter(t)

	program, err := adapter.Compile(ctx, "amount > 100000")
	require.NoError(t, err)

	_, err = adapter.Evaluate(ctx, program, newTestRequest())
	require.NoError(t, err)

	// Verify spans
	spans := tt.GetSpans()

	var compileSpan, evalSpan bool

	for _, s := range spans {
		if s.Name == "adapter.cel.compile" {
			compileSpan = true
		}

		if s.Name == "adapter.cel.evaluate" {
			evalSpan = true
		}
	}

	assert.True(t, compileSpan, "Should have adapter.cel.compile span")
	assert.True(t, evalSpan, "Should have adapter.cel.evaluate span")
}

// TestAdapter_ImplementsInterface tests that Adapter implements ExpressionEngine.
func TestAdapter_ImplementsInterface(t *testing.T) {
	t.Parallel()

	var _ ExpressionEngine = (*Adapter)(nil)
}
