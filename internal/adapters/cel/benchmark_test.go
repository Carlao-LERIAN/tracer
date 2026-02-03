// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package cel

import (
	"context"
	"testing"
)

// BenchmarkCompile benchmarks expression compilation (cache miss).
// Each iteration creates a new adapter to avoid cache hits.
func BenchmarkCompile(b *testing.B) {
	ctx := context.Background()
	expression := "amount > 100000"

	for b.Loop() {
		adapter := newTestAdapter(b)
		_, err := adapter.Compile(ctx, expression)

		if err != nil {
			b.Fatalf("Compile failed: %v", err)
		}
	}
}

// BenchmarkCompile_Cached benchmarks expression compilation with cache hit.
func BenchmarkCompile_Cached(b *testing.B) {
	adapter := newTestAdapter(b)
	ctx := context.Background()
	expression := "amount > 100000"

	// Warm up cache
	_, err := adapter.Compile(ctx, expression)
	if err != nil {
		b.Fatalf("Initial compile failed: %v", err)
	}

	for b.Loop() {
		_, err := adapter.Compile(ctx, expression)
		if err != nil {
			b.Fatalf("Compile failed: %v", err)
		}
	}
}

// BenchmarkCompile_ComplexExpression benchmarks compilation of complex expressions.
func BenchmarkCompile_ComplexExpression(b *testing.B) {
	ctx := context.Background()
	expression := `transactionType == "PIX" && amount > 100000 && account["status"] == "active" && currency == "BRL"`

	for b.Loop() {
		adapter := newTestAdapter(b)
		_, err := adapter.Compile(ctx, expression)

		if err != nil {
			b.Fatalf("Compile failed: %v", err)
		}
	}
}

// BenchmarkEvaluate benchmarks expression evaluation.
func BenchmarkEvaluate(b *testing.B) {
	adapter := newTestAdapter(b)
	ctx := context.Background()
	expression := "amount > 100000"

	program, err := adapter.Compile(ctx, expression)
	if err != nil {
		b.Fatalf("Compile failed: %v", err)
	}

	req := newTestRequest()

	for b.Loop() {
		_, err := adapter.Evaluate(ctx, program, req)
		if err != nil {
			b.Fatalf("Evaluate failed: %v", err)
		}
	}
}

// BenchmarkEvaluate_ComplexExpression benchmarks evaluation of complex expressions.
func BenchmarkEvaluate_ComplexExpression(b *testing.B) {
	adapter := newTestAdapter(b)
	ctx := context.Background()
	expression := `transactionType == "PIX" && amount > 100000 && account["status"] == "active" && currency == "BRL"`

	program, err := adapter.Compile(ctx, expression)
	if err != nil {
		b.Fatalf("Compile failed: %v", err)
	}

	req := newTestRequest()

	for b.Loop() {
		_, err := adapter.Evaluate(ctx, program, req)
		if err != nil {
			b.Fatalf("Evaluate failed: %v", err)
		}
	}
}

// BenchmarkCompileAndEvaluate benchmarks full compile + evaluate cycle (cache miss).
func BenchmarkCompileAndEvaluate(b *testing.B) {
	ctx := context.Background()
	expression := "amount > 100000"
	req := newTestRequest()

	for b.Loop() {
		adapter := newTestAdapter(b)

		program, err := adapter.Compile(ctx, expression)
		if err != nil {
			b.Fatalf("Compile failed: %v", err)
		}

		_, err = adapter.Evaluate(ctx, program, req)
		if err != nil {
			b.Fatalf("Evaluate failed: %v", err)
		}
	}
}

// BenchmarkCompileAndEvaluate_Cached benchmarks compile (cached) + evaluate cycle.
func BenchmarkCompileAndEvaluate_Cached(b *testing.B) {
	adapter := newTestAdapter(b)
	ctx := context.Background()
	expression := "amount > 100000"
	req := newTestRequest()

	// Warm up cache
	_, err := adapter.Compile(ctx, expression)
	if err != nil {
		b.Fatalf("Initial compile failed: %v", err)
	}

	for b.Loop() {
		program, err := adapter.Compile(ctx, expression)
		if err != nil {
			b.Fatalf("Compile failed: %v", err)
		}

		_, err = adapter.Evaluate(ctx, program, req)
		if err != nil {
			b.Fatalf("Evaluate failed: %v", err)
		}
	}
}

// BenchmarkBuildActivation benchmarks building activation from ValidationRequest.
func BenchmarkBuildActivation(b *testing.B) {
	req := newTestRequest()

	for b.Loop() {
		_, err := BuildActivation(req)
		if err != nil {
			b.Fatalf("BuildActivation failed: %v", err)
		}
	}
}

// BenchmarkHashExpression benchmarks expression hashing.
func BenchmarkHashExpression(b *testing.B) {
	expression := `transactionType == "PIX" && amount > 100000 && account["status"] == "active"`

	for b.Loop() {
		HashExpression(expression)
	}
}
