// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package postgres

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUsageCounterPostgreSQLModel_FromEntity_NilEntity(t *testing.T) {
	model := &UsageCounterPostgreSQLModel{}

	err := model.FromEntity(nil)

	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot be nil")
}
