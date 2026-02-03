// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package migration

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestParseMigrationFileName(t *testing.T) {
	tests := []struct {
		name        string
		filename    string
		wantVersion int
		wantName    string
		wantErr     bool
	}{
		{
			name:        "valid up migration",
			filename:    "000001_create_function.up.sql",
			wantVersion: 1,
			wantName:    "create_function",
			wantErr:     false,
		},
		{
			name:        "multi-word name",
			filename:    "000003_calculate_audit_event_hash.up.sql",
			wantVersion: 3,
			wantName:    "calculate_audit_event_hash",
			wantErr:     false,
		},
		{
			name:     "missing direction (plain sql)",
			filename: "000001_create_function.sql",
			wantErr:  true,
		},
		{
			name:     "down migration not supported",
			filename: "000002_verify_hash_chain.down.sql",
			wantErr:  true,
		},
		{
			name:     "invalid version",
			filename: "abc_create_function.up.sql",
			wantErr:  true,
		},
		{
			name:     "no underscore",
			filename: "000001.up.sql",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version, name, err := parseMigrationFileName(tt.filename)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if version != tt.wantVersion {
				t.Errorf("version = %d, want %d", version, tt.wantVersion)
			}

			if name != tt.wantName {
				t.Errorf("name = %q, want %q", name, tt.wantName)
			}
		})
	}
}

func TestLoadMigrations(t *testing.T) {
	tempDir := t.TempDir()

	testMigrations := map[string]string{
		"000001_first_migration.up.sql":  "CREATE FUNCTION test1();",
		"000002_second_migration.up.sql": "CREATE FUNCTION test2();",
	}

	for filename, content := range testMigrations {
		filePath := filepath.Join(tempDir, filename)
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}
	}

	migrator := NewFunctionMigrator(nil, tempDir, nil)
	result, err := migrator.loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations() error = %v", err)
	}

	if len(result.Migrations) != 2 {
		t.Errorf("got %d migrations, want 2", len(result.Migrations))
	}

	if result.Migrations[0].Version != 1 {
		t.Errorf("first migration version = %d, want 1", result.Migrations[0].Version)
	}

	if result.Migrations[1].Version != 2 {
		t.Errorf("second migration version = %d, want 2", result.Migrations[1].Version)
	}

	if result.Migrations[0].UpSQL != "CREATE FUNCTION test1();" {
		t.Errorf("first migration up SQL incorrect")
	}
}

func TestLoadMigrations_IgnoresNonUpFiles(t *testing.T) {
	tempDir := t.TempDir()

	files := map[string]string{
		"000001_test.up.sql":   "CREATE FUNCTION test();",
		"000001_test.down.sql": "DROP FUNCTION test();",
		"README.md":            "Documentation",
	}

	for filename, content := range files {
		filePath := filepath.Join(tempDir, filename)
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}
	}

	migrator := NewFunctionMigrator(nil, tempDir, nil)
	result, err := migrator.loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations() error = %v", err)
	}

	if len(result.Migrations) != 1 {
		t.Errorf("got %d migrations, want 1 (should ignore .down.sql and README.md)", len(result.Migrations))
	}
}

func TestMigratorIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration test")
	}

	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("failed to ping database: %v", err)
	}

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
	if err != nil {
		t.Fatalf("Version() error = %v", err)
	}

	if version != 0 {
		t.Errorf("initial version = %d, want 0", version)
	}

	if dirty {
		t.Errorf("initial dirty = true, want false")
	}

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
