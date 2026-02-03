// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package command

import (
	"context"

	"tracer/pkg/model"
)

//go:generate mockgen -source=audit_event_repository.go -destination=mocks/audit_event_repository_mock.go -package=mocks

// AuditEventRepository defines the write interface for audit event persistence.
type AuditEventRepository interface {
	Insert(ctx context.Context, event *model.AuditEvent) error
}
