// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package command

//go:generate mockgen -destination=audit_writer_mock_test.go -package=command -source=audit_writer.go AuditWriter

import (
	"context"

	"github.com/google/uuid"

	"tracer/pkg/model"
)

// AuditWriter defines the interface for recording audit events from commands.
// This interface mirrors services.AuditWriter but exists in the command package
// to avoid circular dependencies (commands can't import services).
// The actual implementation is RecordAuditEventCommand, which is injected into commands.
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
