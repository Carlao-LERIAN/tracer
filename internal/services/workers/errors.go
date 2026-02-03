// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package workers

import "errors"

// Shared sentinel errors for worker components.
// These errors are used across multiple workers for consistent error handling.
var (
	// ErrNilLogger is returned when a required logger dependency is nil.
	ErrNilLogger = errors.New("logger cannot be nil")
	// ErrNilRepository is returned when a required repository dependency is nil.
	ErrNilRepository = errors.New("repository cannot be nil")
	// ErrInvalidCleanupInterval is returned when cleanup interval is not positive.
	ErrInvalidCleanupInterval = errors.New("cleanup interval must be positive")
	// ErrInvalidRetentionPeriod is returned when retention period is not positive.
	ErrInvalidRetentionPeriod = errors.New("retention period must be positive")
)
