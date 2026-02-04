// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package model

import "github.com/google/uuid"

// UUIDPtr returns a pointer to the given UUID.
// Helper function for tests that need *uuid.UUID values.
func UUIDPtr(u uuid.UUID) *uuid.UUID {
	return &u
}

// StringPtr returns a pointer to the given string.
// Helper function for tests that need *string values.
func StringPtr(s string) *string {
	return &s
}
