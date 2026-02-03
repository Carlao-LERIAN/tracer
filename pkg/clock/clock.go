// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package clock

import "time"

// Clock provides time operations. Allows injection for testing.
type Clock interface {
	Now() time.Time
	// NewTicker returns a ticker that ticks at the specified duration.
	// Returns a channel that receives the current time on each tick.
	// The caller must call the returned stop function to release resources.
	NewTicker(d time.Duration) (c <-chan time.Time, stop func())
}

// RealClock implements Clock using the real system time.
type RealClock struct{}

// Now returns the current UTC time.
func (RealClock) Now() time.Time {
	return time.Now().UTC()
}

// NewTicker returns a real time.Ticker's channel and stop function.
func (RealClock) NewTicker(d time.Duration) (<-chan time.Time, func()) {
	ticker := time.NewTicker(d)
	return ticker.C, ticker.Stop
}

// New returns a new RealClock instance.
func New() Clock {
	return RealClock{}
}
