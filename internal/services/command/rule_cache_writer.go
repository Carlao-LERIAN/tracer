// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package command

//go:generate mockgen -source=rule_cache_writer.go -destination=rule_cache_writer_mock.go -package=command

import (
	"github.com/google/uuid"

	"tracer/pkg/model"
)

// RuleCacheWriter is implemented by components that can directly update the
// in-memory rule cache. Used by command services to synchronously update the
// cache after persisting rule state changes, ensuring the local instance is
// immediately consistent without waiting for the background sync worker.
type RuleCacheWriter interface {
	UpsertRule(rule *model.Rule, program any)
	RemoveRule(id uuid.UUID)
}
