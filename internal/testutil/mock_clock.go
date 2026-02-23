// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package testutil

import (
	"sync"
	"time"

	"tracer/pkg/clock"
)

// DefaultTestTime is the standard fixed time used in tests.
// Set to 1 minute ago to stay within the 24-hour validation window
// while remaining constant for the entire test process lifetime.
var DefaultTestTime = time.Now().Add(-1 * time.Minute).UTC()

// FixedTime returns the default fixed time for deterministic tests.
// Use this instead of time.Now().UTC() in tests to ensure reproducibility.
func FixedTime() time.Time {
	return DefaultTestTime
}

// MockClock is a test double for clock.Clock that returns a fixed time.
type MockClock struct {
	FixedTime  time.Time
	TickerChan chan time.Time // Optional: set to control ticker behavior in tests
}

// Now returns the fixed time.
func (m MockClock) Now() time.Time {
	return m.FixedTime
}

// NewTicker returns a controllable ticker for testing.
// If TickerChan is set, it returns that channel; otherwise returns a channel that never fires.
// The stop function closes the channel only if it was created internally.
// The stop function is idempotent and safe to call multiple times.
func (m MockClock) NewTicker(_ time.Duration) (<-chan time.Time, func()) {
	if m.TickerChan != nil {
		return m.TickerChan, func() {} // caller controls the channel
	}
	// Return a channel that never fires for tests that don't need ticker
	ch := make(chan time.Time)

	var once sync.Once

	return ch, func() { once.Do(func() { close(ch) }) }
}

// NewMockClock creates a new MockClock with the given fixed time.
func NewMockClock(fixedTime time.Time) clock.Clock {
	return MockClock{FixedTime: fixedTime}
}

// NewDefaultMockClock creates a MockClock with the default test time (2024-01-15 10:30:00 UTC).
func NewDefaultMockClock() clock.Clock {
	return MockClock{FixedTime: DefaultTestTime}
}
