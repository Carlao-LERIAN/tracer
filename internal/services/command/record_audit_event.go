// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package command

import (
	"context"
	"fmt"
	"net"

	"github.com/google/uuid"

	pgdb "tracer/internal/adapters/postgres/db"
	"tracer/pkg/model"
)

// RecordAuditEventCommand handles recording audit events.
type RecordAuditEventCommand struct {
	repo AuditEventRepository
}

// NewRecordAuditEventCommand creates a new RecordAuditEventCommand.
func NewRecordAuditEventCommand(repo AuditEventRepository) *RecordAuditEventCommand {
	return &RecordAuditEventCommand{repo: repo}
}

// RecordValidationEvent records an audit event for a transaction validation.
// NOTE: evalResult is passed separately from responseContext to avoid embedding redundancy.
// The decision is extracted from evalResult and stored in AuditEvent.Result field.
func (c *RecordAuditEventCommand) RecordValidationEvent(
	ctx context.Context,
	validationID uuid.UUID,
	request map[string]any,
	evalResult model.EvaluationResult,
	responseContext model.ValidationResponseContext,
	clientIP string,
) error {
	result := model.DecisionToAuditResult(evalResult.Decision)

	event, err := model.NewAuditEvent(
		model.AuditEventTransactionValidated,
		model.AuditActionValidate,
		result,
		validationID.String(),
		model.ResourceTypeTransaction,
		model.Actor{
			ActorType: model.ActorTypeSystem,
			ID:        "svc_tracer",
			Name:      "Tracer Validation Engine",
			IPAddress: normalizeIP(clientIP),
		},
	)
	if err != nil {
		return fmt.Errorf("failed to create audit event: %w", err)
	}

	event.WithValidationContext(request, evalResult, responseContext)

	return c.repo.Insert(ctx, event)
}

// RecordValidationEventWithTx records an audit event for a transaction validation using the provided database connection.
// The db parameter accepts either a regular DB connection or a transaction (*sql.Tx via TxAdapter).
// Atomicity with other database changes is only guaranteed when a transaction handle is passed;
// a plain DB connection will execute the insert independently.
// NOTE: evalResult is passed separately from responseContext to avoid embedding redundancy.
// The decision is extracted from evalResult and stored in AuditEvent.Result field.
func (c *RecordAuditEventCommand) RecordValidationEventWithTx(
	ctx context.Context,
	db pgdb.DB,
	validationID uuid.UUID,
	request map[string]any,
	evalResult model.EvaluationResult,
	responseContext model.ValidationResponseContext,
	clientIP string,
) error {
	result := model.DecisionToAuditResult(evalResult.Decision)

	event, err := model.NewAuditEvent(
		model.AuditEventTransactionValidated,
		model.AuditActionValidate,
		result,
		validationID.String(),
		model.ResourceTypeTransaction,
		model.Actor{
			ActorType: model.ActorTypeSystem,
			ID:        "svc_tracer",
			Name:      "Tracer Validation Engine",
			IPAddress: normalizeIP(clientIP),
		},
	)
	if err != nil {
		return fmt.Errorf("failed to create audit event: %w", err)
	}

	event.WithValidationContext(request, evalResult, responseContext)

	return c.repo.InsertWithTx(ctx, db, event)
}

// RecordRuleEvent records an audit event for a rule operation.
func (c *RecordAuditEventCommand) RecordRuleEvent(
	ctx context.Context,
	eventType model.AuditEventType,
	action model.AuditAction,
	ruleID uuid.UUID,
	before map[string]any,
	after map[string]any,
	reason string,
	clientIP string,
) error {
	event, err := model.NewAuditEvent(
		eventType,
		action,
		model.AuditResultSuccess,
		ruleID.String(),
		model.ResourceTypeRule,
		model.Actor{
			ActorType: model.ActorTypeSystem,
			ID:        "svc_tracer",
			Name:      "Tracer Rule Manager",
			IPAddress: normalizeIP(clientIP),
		},
	)
	if err != nil {
		return fmt.Errorf("failed to create audit event: %w", err)
	}

	event.WithCRUDContext(before, after, reason)

	return c.repo.Insert(ctx, event)
}

// RecordLimitEvent records an audit event for a limit operation.
func (c *RecordAuditEventCommand) RecordLimitEvent(
	ctx context.Context,
	eventType model.AuditEventType,
	action model.AuditAction,
	limitID uuid.UUID,
	before map[string]any,
	after map[string]any,
	reason string,
	clientIP string,
) error {
	event, err := model.NewAuditEvent(
		eventType,
		action,
		model.AuditResultSuccess,
		limitID.String(),
		model.ResourceTypeLimit,
		model.Actor{
			ActorType: model.ActorTypeSystem,
			ID:        "svc_tracer",
			Name:      "Tracer Limit Manager",
			IPAddress: normalizeIP(clientIP),
		},
	)
	if err != nil {
		return fmt.Errorf("failed to create audit event: %w", err)
	}

	event.WithCRUDContext(before, after, reason)

	return c.repo.Insert(ctx, event)
}

// normalizeIP normalizes IP address for storage.
func normalizeIP(ip string) string {
	if ip == "" {
		return "0.0.0.0"
	}

	host, _, err := net.SplitHostPort(ip)
	if err == nil {
		ip = host
	}

	if net.ParseIP(ip) == nil {
		return "0.0.0.0"
	}

	return ip
}
