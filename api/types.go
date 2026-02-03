// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package api

// ErrorResponse represents a standard error response.
// Used in Swagger documentation for error responses.
type ErrorResponse struct {
	Code    string `json:"code" validate:"required" example:"TRC-0001"`
	Title   string `json:"title" validate:"required" example:"Bad Request"`
	Message string `json:"message" validate:"required" example:"Invalid input provided"`
}

// VersionResponse represents the response of the version endpoint.
type VersionResponse struct {
	Version     string `json:"version" example:"1.0.0"`
	RequestDate string `json:"requestDate" example:"2025-01-01T00:00:00Z"`
}

// HealthCheck represents the status of a single component check.
type HealthCheck struct {
	Component string `json:"component" example:"database"`
	Status    string `json:"status" example:"OK"`
	Message   string `json:"message,omitempty" example:""`
}

// ReadinessResponse represents the response of the readiness check.
type ReadinessResponse struct {
	Status string        `json:"status" example:"READY"`
	Checks []HealthCheck `json:"checks" validate:"max=20"`
}
