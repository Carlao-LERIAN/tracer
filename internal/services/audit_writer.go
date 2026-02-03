// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package services

//go:generate mockgen -source=audit_writer.go -destination=mocks/audit_writer_mock.go -package=mocks

import (
	"context"

	"github.com/google/uuid"

	"tracer/pkg/model"
)

// AuditWriter defines the interface for recording audit events.
// Implemented by RecordAuditEventCommand.
// Used by ValidationService, RuleService, and LimitService.
// Client IP and Request ID are passed as parameters by the calling services.
type AuditWriter interface {
	RecordValidationEvent(
		ctx context.Context,
		validationID uuid.UUID,
		request map[string]any,
		evalResult model.EvaluationResult,
		responseContext model.ValidationResponseContext,
		clientIP string,
	) error

	RecordRuleEvent(
		ctx context.Context,
		eventType model.AuditEventType,
		action model.AuditAction,
		ruleID uuid.UUID,
		before map[string]any,
		after map[string]any,
		reason string,
		clientIP string,
	) error

	RecordLimitEvent(
		ctx context.Context,
		eventType model.AuditEventType,
		action model.AuditAction,
		limitID uuid.UUID,
		before map[string]any,
		after map[string]any,
		reason string,
		clientIP string,
	) error
}
