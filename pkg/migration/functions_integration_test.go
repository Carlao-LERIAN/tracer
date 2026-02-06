// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

//go:build integration

package migration

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"tracer/internal/testutil"
)

func TestMigratorIntegration(t *testing.T) {
	// Use the testcontainers database URL (automatically configured by test suite)
	dbURL := testutil.GetTestDSN()

	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("failed to ping database: %v", err)
	}

	// Cleanup any previous test state (functions_migrations table may exist from previous runs)
	_, _ = db.ExecContext(ctx, "DROP TABLE IF EXISTS "+functionsMigrationsTable)
	_, _ = db.ExecContext(ctx, "DROP FUNCTION IF EXISTS test_func()")

	tempDir := t.TempDir()

	if err := os.WriteFile(
		filepath.Join(tempDir, "000001_test_function.up.sql"),
		[]byte("CREATE OR REPLACE FUNCTION test_func() RETURNS INTEGER AS $$ BEGIN RETURN 42; END; $$ LANGUAGE plpgsql;"),
		0644,
	); err != nil {
		t.Fatalf("failed to write migration: %v", err)
	}

	migrator := NewFunctionMigrator(db, tempDir, nil)

	defer func() {
		_, _ = db.ExecContext(ctx, "DROP TABLE IF EXISTS "+functionsMigrationsTable)
		_, _ = db.ExecContext(ctx, "DROP FUNCTION IF EXISTS test_func()")
	}()

	version, dirty, err := migrator.Version(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, version, "initial version")
	assert.False(t, dirty, "initial dirty")

	if err := migrator.Up(ctx); err != nil {
		t.Fatalf("Up() error = %v", err)
	}

	version, dirty, err = migrator.Version(ctx)
	if err != nil {
		t.Fatalf("Version() after up error = %v", err)
	}

	if version != 1 {
		t.Errorf("version after up = %d, want 1", version)
	}

	if dirty {
		t.Errorf("dirty after up = true, want false")
	}

	var result int
	err = db.QueryRowContext(ctx, "SELECT test_func()").Scan(&result)
	if err != nil {
		t.Errorf("failed to call test function: %v", err)
	}

	if result != 42 {
		t.Errorf("test_func() = %d, want 42", result)
	}

	if err := migrator.Up(ctx); err != nil {
		t.Fatalf("Second Up() error = %v", err)
	}

	version, dirty, err = migrator.Version(ctx)
	if err != nil {
		t.Fatalf("Version() after second up error = %v", err)
	}

	if version != 1 {
		t.Errorf("version after second up = %d, want 1 (idempotent)", version)
	}

	if dirty {
		t.Errorf("dirty after second up = true, want false")
	}
}
