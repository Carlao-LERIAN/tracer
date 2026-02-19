// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package workers

import "time"

// RuleSyncWorkerConfig holds configuration for the rule sync worker.
type RuleSyncWorkerConfig struct {
	// PollInterval is how often the worker polls for rule changes (default: 10s).
	PollInterval time.Duration
	// StalenessThreshold is the duration after which the cache is considered stale (default: 50s).
	// Used by health checker to report DEGRADED state when cache has not been refreshed within this duration.
	StalenessThreshold time.Duration
	// OverlapBuffer is subtracted from lastSync when querying deltas (default: 2s).
	// Ensures no changes are missed at poll boundaries due to clock skew or transaction lag.
	OverlapBuffer time.Duration
}

// DefaultRuleSyncWorkerConfig returns default configuration values.
func DefaultRuleSyncWorkerConfig() RuleSyncWorkerConfig {
	return RuleSyncWorkerConfig{
		PollInterval:       10 * time.Second,
		StalenessThreshold: 50 * time.Second,
		OverlapBuffer:      2 * time.Second,
	}
}
