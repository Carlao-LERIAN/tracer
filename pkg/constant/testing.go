// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package constant

// FaultInjectionHeader is the header used to trigger fault injection in tests.
// Only works when FAULT_INJECTION_ENABLED=true (integration test mode).
const FaultInjectionHeader = "X-Test-Fault-Injection"

// Fault injection types
const (
	FaultTimeout     = "timeout"     // Simulates 504 Gateway Timeout
	FaultUnavailable = "unavailable" // Simulates 503 Service Unavailable
)
