// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package query

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestMetricRollbackFailures_Definition verifies the metric is properly defined.
func TestMetricRollbackFailures_Definition(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "tracer_limit_rollback_failures_total", MetricRollbackFailures.Name,
		"Metric name should follow TRD Section 9.3 convention with tracer_ prefix")
	assert.Equal(t, "1", MetricRollbackFailures.Unit,
		"Metric unit should be 1 for counters")
	assert.NotEmpty(t, MetricRollbackFailures.Description,
		"Metric should have a description")
	assert.Contains(t, MetricRollbackFailures.Description, "rollback",
		"Description should mention rollback")
}
