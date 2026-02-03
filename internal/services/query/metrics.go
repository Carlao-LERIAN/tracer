// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

// Package query provides query services for the Tracer application.
package query

import (
	libMetrics "github.com/LerianStudio/lib-commons/v2/commons/opentelemetry/metrics"
)

// Metric is an alias for libMetrics.Metric to allow local usage without importing.
type Metric = libMetrics.Metric

// MetricRollbackFailures tracks limit rollback failures.
// Name follows TRD Section 9.3 convention with tracer_ prefix.
// This metric signals when usage counters could not be decremented after a transaction
// was denied by rules. Non-zero values indicate eventual consistency gaps that will
// self-correct at period boundaries (daily/monthly resets).
// Labels: none (limit_id available in logs/spans for investigation)
var MetricRollbackFailures = Metric{
	Name:        "tracer_limit_rollback_failures_total",
	Unit:        "1",
	Description: "Total limit rollback failures (eventual consistency gaps)",
}
