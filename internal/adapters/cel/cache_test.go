// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package cel

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createTestProgram creates a test CompiledProgram with the given expression.
func createTestProgram(expression string) *CompiledProgram {
	return &CompiledProgram{
		ExpressionHash:   HashExpression(expression),
		SourceExpression: expression,
		Program:          nil, // Program is not needed for cache tests
		CompiledAt:       time.Now(),
		CompileTimeMs:    1,
	}
}

// TestCache_GetSet tests basic cache get and set operations.
func TestCache_GetSet(t *testing.T) {
	tests := []struct {
		name        string
		expression  string
		description string
	}{
		{
			name:        "Success - store and retrieve program",
			expression:  "amount > 100",
			description: "Should store and retrieve a compiled program",
		},
		{
			name:        "Success - store complex expression",
			expression:  `transactionType == "CARD" && amount > 100`,
			description: "Should store complex expression",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cache := NewCache(DefaultCacheMaxSize)
			program := createTestProgram(tc.expression)

			// Set program
			cache.Set(program)

			// Get program
			retrieved, found := cache.Get(program.ExpressionHash)

			assert.True(t, found, "Program should be found in cache")
			assert.Equal(t, program.ExpressionHash, retrieved.ExpressionHash)
			assert.Equal(t, program.SourceExpression, retrieved.SourceExpression)
		})
	}
}

// TestCache_GetNotFound tests cache miss behavior.
func TestCache_GetNotFound(t *testing.T) {
	cache := NewCache(DefaultCacheMaxSize)

	retrieved, found := cache.Get("nonexistent-hash")

	assert.False(t, found, "Should not find nonexistent program")
	assert.Nil(t, retrieved, "Retrieved program should be nil")
}

// TestCache_MaxSize_Eviction tests FIFO eviction when cache is full.
func TestCache_MaxSize_Eviction(t *testing.T) {
	maxSize := int64(3)
	cache := NewCache(maxSize)

	// Add programs with different times (oldest first)
	programs := make([]*CompiledProgram, 4)
	for i := 0; i < 4; i++ {
		programs[i] = &CompiledProgram{
			ExpressionHash:   HashExpression(fmt.Sprintf("amount > %d", i)),
			SourceExpression: fmt.Sprintf("amount > %d", i),
			CompiledAt:       time.Now().Add(time.Duration(i) * time.Second),
			CompileTimeMs:    1,
		}
	}

	// Add first 3 programs
	for i := 0; i < 3; i++ {
		cache.Set(programs[i])
	}

	stats := cache.Stats()
	assert.Equal(t, maxSize, stats.Size, "Cache should be at max size")

	// Verify first (oldest) program is in cache
	_, found := cache.Get(programs[0].ExpressionHash)
	assert.True(t, found, "Oldest program should be in cache before eviction")

	// Add 4th program - should evict oldest (programs[0])
	cache.Set(programs[3])

	// Size should still be maxSize
	stats = cache.Stats()
	assert.Equal(t, maxSize, stats.Size, "Cache should not exceed max size")

	// Oldest program (programs[0]) should be evicted
	_, found = cache.Get(programs[0].ExpressionHash)
	assert.False(t, found, "Oldest program should be evicted (FIFO)")

	// Newest program should be in cache
	_, found = cache.Get(programs[3].ExpressionHash)
	assert.True(t, found, "Newest program should be in cache")

	// Programs 1 and 2 should still be in cache
	_, found = cache.Get(programs[1].ExpressionHash)
	assert.True(t, found, "Program 1 should still be in cache")
	_, found = cache.Get(programs[2].ExpressionHash)
	assert.True(t, found, "Program 2 should still be in cache")
}

// TestCache_Invalidate tests cache invalidation.
func TestCache_Invalidate(t *testing.T) {
	cache := NewCache(DefaultCacheMaxSize)
	program := createTestProgram("amount > 100")

	// Add and verify
	cache.Set(program)
	_, found := cache.Get(program.ExpressionHash)
	require.True(t, found, "Program should be in cache before invalidation")

	// Invalidate
	cache.Invalidate(program.ExpressionHash)

	// Verify removed
	_, found = cache.Get(program.ExpressionHash)
	assert.False(t, found, "Program should not be in cache after invalidation")

	// Size should be decremented
	stats := cache.Stats()
	assert.Equal(t, int64(0), stats.Size, "Cache size should be 0 after invalidation")
}

// TestCache_InvalidateNonexistent tests invalidating a nonexistent entry.
func TestCache_InvalidateNonexistent(t *testing.T) {
	cache := NewCache(DefaultCacheMaxSize)
	program := createTestProgram("amount > 100")
	cache.Set(program)

	initialStats := cache.Stats()

	// Invalidate nonexistent
	cache.Invalidate("nonexistent-hash")

	// Size should not change
	stats := cache.Stats()
	assert.Equal(t, initialStats.Size, stats.Size, "Cache size should not change")
}

// TestCache_ConcurrentAccess tests thread-safe concurrent access.
func TestCache_ConcurrentAccess(t *testing.T) {
	cache := NewCache(1000)
	var wg sync.WaitGroup

	// Concurrent writes
	for i := 0; i < 100; i++ {
		wg.Add(1)

		go func(idx int) {
			defer wg.Done()
			program := createTestProgram(fmt.Sprintf("amount > %d", idx))
			cache.Set(program)
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 100; i++ {
		wg.Add(1)

		go func(idx int) {
			defer wg.Done()
			hash := HashExpression(fmt.Sprintf("amount > %d", idx))
			cache.Get(hash)
		}(i)
	}

	wg.Wait()

	stats := cache.Stats()
	assert.True(t, stats.Size > 0, "Cache should have entries after concurrent access")
	assert.True(t, stats.Size <= 100, "Cache should not exceed 100 entries")
}

// TestHashExpression_Deterministic tests that hash is deterministic.
func TestHashExpression_Deterministic(t *testing.T) {
	expression := "amount > 100 && transactionType == \"CARD\""

	hash1 := HashExpression(expression)
	hash2 := HashExpression(expression)
	hash3 := HashExpression(expression)

	assert.Equal(t, hash1, hash2, "Hash should be deterministic")
	assert.Equal(t, hash2, hash3, "Hash should be deterministic")
	assert.Len(t, hash1, 64, "SHA-256 hash should be 64 hex characters")
}

// TestHashExpression_Different tests that different expressions produce different hashes.
func TestHashExpression_Different(t *testing.T) {
	hash1 := HashExpression("amount > 100")
	hash2 := HashExpression("amount > 10001")

	assert.NotEqual(t, hash1, hash2, "Different expressions should produce different hashes")
}

// TestCache_Stats tests cache statistics tracking.
func TestCache_Stats(t *testing.T) {
	cache := NewCache(DefaultCacheMaxSize)

	// Initial stats
	stats := cache.Stats()
	assert.Equal(t, int64(0), stats.Size, "Initial size should be 0")
	assert.Equal(t, int64(0), stats.Hits, "Initial hits should be 0")
	assert.Equal(t, int64(0), stats.Misses, "Initial misses should be 0")

	// Add a program
	program := createTestProgram("amount > 100")
	cache.Set(program)

	stats = cache.Stats()
	assert.Equal(t, int64(1), stats.Size, "Size should be 1 after adding")

	// Cache hit
	cache.Get(program.ExpressionHash)
	stats = cache.Stats()
	assert.Equal(t, int64(1), stats.Hits, "Hits should be 1 after cache hit")

	// Cache miss
	cache.Get("nonexistent")
	stats = cache.Stats()
	assert.Equal(t, int64(1), stats.Misses, "Misses should be 1 after cache miss")
}

// TestCache_SetNil tests that setting nil program is handled gracefully.
func TestCache_SetNil(t *testing.T) {
	cache := NewCache(DefaultCacheMaxSize)

	// Should not panic
	cache.Set(nil)

	stats := cache.Stats()
	assert.Equal(t, int64(0), stats.Size, "Cache should be empty after setting nil")
}

// TestCache_Update tests updating an existing entry.
func TestCache_Update(t *testing.T) {
	cache := NewCache(DefaultCacheMaxSize)

	expression := "amount > 100"
	program1 := createTestProgram(expression)
	program1.CompileTimeMs = 10

	cache.Set(program1)

	// Update with new compile time
	program2 := createTestProgram(expression)
	program2.CompileTimeMs = 20

	cache.Set(program2)

	// Size should still be 1 (update, not new entry)
	stats := cache.Stats()
	assert.Equal(t, int64(1), stats.Size, "Size should be 1 after update")

	// Retrieved program should have new compile time
	retrieved, _ := cache.Get(program2.ExpressionHash)
	assert.Equal(t, int64(20), retrieved.CompileTimeMs, "Should have updated compile time")
}

// TestCache_Clear tests clearing the cache.
func TestCache_Clear(t *testing.T) {
	cache := NewCache(DefaultCacheMaxSize)

	// Add some programs
	for i := 0; i < 10; i++ {
		program := createTestProgram(fmt.Sprintf("amount > %d", i))
		cache.Set(program)
	}

	stats := cache.Stats()
	require.Equal(t, int64(10), stats.Size, "Cache should have 10 entries")

	// Clear
	cache.Clear()

	stats = cache.Stats()
	assert.Equal(t, int64(0), stats.Size, "Cache should be empty after clear")
}

// TestNewCache_DefaultMaxSize tests default max size when zero or negative.
func TestNewCache_DefaultMaxSize(t *testing.T) {
	tests := []struct {
		name     string
		maxSize  int64
		expected int64
	}{
		{
			name:     "Zero uses default",
			maxSize:  0,
			expected: DefaultCacheMaxSize,
		},
		{
			name:     "Negative uses default",
			maxSize:  -1,
			expected: DefaultCacheMaxSize,
		},
		{
			name:     "Custom max size",
			maxSize:  500,
			expected: 500,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cache := NewCache(tc.maxSize)
			assert.Equal(t, tc.expected, cache.maxSize, "Max size should match expected")
		})
	}
}
