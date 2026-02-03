// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package command

//go:generate mockgen -source=limit_repository.go -destination=limit_repository_mock.go -package=command

import (
	"context"
	"time"

	"github.com/google/uuid"

	"tracer/pkg/model"
)

// LimitRepository defines the interface for limit persistence in commands.
// This interface is intentionally separate from query.LimitRepository per CQRS pattern.
// GetByID exists in both interfaces as commands need to read current state for validation
// before performing mutations.
type LimitRepository interface {
	// Create persists a new limit to the database.
	// The limit must have a valid ID (not uuid.Nil) and pass model validation.
	Create(ctx context.Context, lmt *model.Limit) error

	// GetByID retrieves a limit by its unique identifier.
	// Returns constant.ErrLimitNotFound if the limit does not exist.
	GetByID(ctx context.Context, limitID uuid.UUID) (*model.Limit, error)

	// Update persists changes to an existing limit.
	// The limit's UpdatedAt field should be set before calling this method.
	Update(ctx context.Context, lmt *model.Limit) error

	// UpdateStatus updates only the status field and updatedAt timestamp.
	// More efficient than Update() for lifecycle transitions (activate, deactivate, delete).
	// For DELETED status, also sets deleted_at for soft-delete consistency.
	UpdateStatus(ctx context.Context, limitID uuid.UUID, status model.LimitStatus, updatedAt time.Time) error
}
