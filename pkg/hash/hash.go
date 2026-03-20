// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

// Package hash provides utility functions for hashing UUIDs to integer values.
// Used for PostgreSQL advisory locks in the idempotency/deduplication flow.
package hash

import (
	"hash/fnv"

	"github.com/google/uuid"
)

// HashUUIDToInt32 converts a UUID to an int32 hash using FNV-1a algorithm.
// This is used for PostgreSQL advisory lock keys (pg_advisory_xact_lock).
// The hash is deterministic - the same UUID always produces the same int32.
//
// FNV-1a is chosen because:
// - Fast computation (critical for high-throughput validation)
// - Good distribution across the int32 range
// - Deterministic (no external state)
// - Built into Go standard library
func HashUUIDToInt32(id uuid.UUID) int32 {
	h := fnv.New32a()
	// Write UUID bytes directly - uuid.UUID is a [16]byte array
	h.Write(id[:])

	return int32(h.Sum32())
}
