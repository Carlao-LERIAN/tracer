// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package command

//go:generate mockgen -source=repository.go -destination=repository_mock.go -package=command

import (
	"context"
	"time"

	"github.com/google/uuid"

	"tracer/pkg/model"
)

// RuleRepository defines the interface for rule persistence.
type RuleRepository interface {
	Create(ctx context.Context, rule *model.Rule) (*model.Rule, error)
	GetByID(ctx context.Context, id uuid.UUID) (*model.Rule, error)
	GetByName(ctx context.Context, name string) (*model.Rule, error)
	ListByStatus(ctx context.Context, status *model.RuleStatus) ([]*model.Rule, error)
	Update(ctx context.Context, rule *model.Rule) (*model.Rule, error)
	Delete(ctx context.Context, id uuid.UUID) error
	ListActiveByScopes(ctx context.Context, scopes []model.Scope) ([]*model.Rule, error)
	// UpdateStatus updates the status field and related timestamps.
	// activatedAt is set when transitioning to ACTIVE, deactivatedAt when transitioning to INACTIVE.
	UpdateStatus(ctx context.Context, id uuid.UUID, status model.RuleStatus, updatedAt time.Time, activatedAt *time.Time, deactivatedAt *time.Time) error
}
