// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

// Package cel provides CEL expression compilation and evaluation.
package cel

import (
	"sync"
	"sync/atomic"
	"time"
)

// DefaultCacheMaxSize is the default maximum number of cached programs.
const DefaultCacheMaxSize int64 = 1000

// Cache stores compiled CEL programs with thread-safe access.
//
// Eviction Strategy (MVP):
// When the cache reaches capacity, the oldest entry (FIFO) is evicted.
//
// Alternatives considered:
//   - LRU (Least Recently Used): More complex, requires linked list + map structure.
//     Better for access pattern optimization but adds significant complexity.
//   - Random eviction: Simple O(1) but may evict frequently used entries.
//   - No eviction (reject new): Simple but causes cache misses for new expressions.
//
// FIFO was chosen for MVP because:
//   - Simple to implement with sync.Map iteration
//   - Predictable behavior for debugging
//   - Sufficient for initial use case where expressions are relatively stable
//   - Can be upgraded to LRU if access patterns show need for optimization
type Cache struct {
	programs sync.Map
	size     atomic.Int64
	maxSize  int64
	hits     atomic.Int64
	misses   atomic.Int64
	mu       sync.Mutex // protects eviction to prevent concurrent evictions
}

// NewCache creates a new cache with the specified maximum size.
// Use DefaultCacheMaxSize if no specific limit is needed.
func NewCache(maxSize int64) *Cache {
	if maxSize <= 0 {
		maxSize = DefaultCacheMaxSize
	}

	return &Cache{
		maxSize: maxSize,
	}
}

// Get retrieves a compiled program by expression hash.
// Returns the program and true if found, nil and false otherwise.
func (c *Cache) Get(hash string) (*CompiledProgram, bool) {
	value, ok := c.programs.Load(hash)
	if !ok {
		c.misses.Add(1)

		return nil, false
	}

	program, ok := value.(*CompiledProgram)
	if !ok {
		// Type assertion failed - count as cache miss
		c.misses.Add(1)

		return nil, false
	}

	c.hits.Add(1)

	return program, true
}

// Set stores a compiled program in the cache.
// If the cache is at capacity, the oldest entry (by CompiledAt) is evicted (FIFO).
// The entire operation is atomic to prevent race conditions.
func (c *Cache) Set(program *CompiledProgram) {
	if program == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if already exists (update case - no size change)
	if _, exists := c.programs.Load(program.ExpressionHash); exists {
		c.programs.Store(program.ExpressionHash, program)

		return
	}

	// Check capacity and evict if needed (while holding lock)
	if c.size.Load() >= c.maxSize {
		c.evictOldestLocked()
	}

	// Store and increment size
	c.programs.Store(program.ExpressionHash, program)
	c.size.Add(1)
}

// evictOldestLocked removes the oldest entry from the cache (FIFO eviction).
// Caller must hold c.mu lock.
func (c *Cache) evictOldestLocked() {
	var oldestHash string

	var oldestTime time.Time

	c.programs.Range(func(key, value any) bool {
		program, ok := value.(*CompiledProgram)
		if !ok {
			return true
		}

		if oldestHash == "" || program.CompiledAt.Before(oldestTime) {
			oldestHash = program.ExpressionHash
			oldestTime = program.CompiledAt
		}

		return true
	})

	if oldestHash != "" {
		c.programs.Delete(oldestHash)
		c.size.Add(-1)
	}
}

// Invalidate removes a program from the cache.
// The operation is atomic to prevent race conditions with Set.
func (c *Cache) Invalidate(hash string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, existed := c.programs.LoadAndDelete(hash); existed {
		c.size.Add(-1)
	}
}

// CacheStats contains cache statistics for metrics.
type CacheStats struct {
	Size   int64
	Hits   int64
	Misses int64
}

// Stats returns current cache statistics.
func (c *Cache) Stats() CacheStats {
	return CacheStats{
		Size:   c.size.Load(),
		Hits:   c.hits.Load(),
		Misses: c.misses.Load(),
	}
}

// Clear removes all entries from the cache.
// The operation is atomic to prevent race conditions with Set.
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.programs.Range(func(key, _ any) bool {
		c.programs.Delete(key)

		return true
	})

	c.size.Store(0)
}
