// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

//go:build e2e

package steps

import (
	"github.com/cucumber/godog"

	"tracer/tests/end2end/support"
)

// InitializeScenario registers all step definitions for a Godog scenario.
// Called once per scenario — each scenario gets a fresh ScenarioContext.
func InitializeScenario(ctx *godog.ScenarioContext) {
	sc := support.NewScenarioContext()

	// NOTE: Per-scenario cleanup is intentionally disabled.
	// E2E scenarios within a Feature are sequential journeys — each scenario
	// depends on resources created by the previous one. Cleanup between
	// scenarios would break cross-scenario state. The docker-compose setup
	// provides a fresh database for each full test run.

	// Register step definitions by category
	registerAuthSteps(ctx, sc)
	registerRuleSteps(ctx, sc)
	registerValidationSteps(ctx, sc)
	registerLimitSteps(ctx, sc)
	registerAuditSteps(ctx, sc)
}
