// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package clock

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRealClock_Now(t *testing.T) {
	c := New()

	before := time.Now().UTC()
	got := c.Now()
	after := time.Now().UTC()

	assert.True(t, !got.Before(before), "Clock.Now() should not be before the test start time")
	assert.True(t, !got.After(after), "Clock.Now() should not be after the test end time")
	assert.Equal(t, time.UTC, got.Location(), "Clock.Now() should return UTC time")
}

// MockClock is a test double that returns a fixed time.
type MockClock struct {
	FixedTime time.Time
}

// Now returns the fixed time.
func (m MockClock) Now() time.Time {
	return m.FixedTime
}

func TestMockClock_Now(t *testing.T) {
	fixedTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	c := MockClock{FixedTime: fixedTime}

	got := c.Now()

	assert.Equal(t, fixedTime, got)
}
