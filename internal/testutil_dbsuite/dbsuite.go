// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

//go:build integration

// Package testutil_dbsuite provides a lightweight TestMain helper that starts a
// PostgreSQL testcontainer and optionally applies migrations.
//
// Unlike testutil_integration.SetupTestSuite, this package does NOT import
// bootstrap or start an HTTP server — it only provides a database. This avoids
// import cycles when used from packages that bootstrap itself depends on
// (e.g. internal/adapters/postgres).
package testutil_dbsuite

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	migratePostgres "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// dbEnvVars lists environment variables managed by the suite.
var dbEnvVars = []string{
	"DB_HOST", "DB_PORT", "DB_USER", "DB_PASSWORD", "DB_NAME",
}

// suiteConfig holds configuration for SetupTestDBSuite.
type suiteConfig struct {
	migrationsPath string
}

// Option configures SetupTestDBSuite behavior.
type Option func(*suiteConfig)

// WithMigrations enables function + schema migrations from the given directory.
// The path should point to the top-level migrations/ directory containing
// numbered schema files and a functions/ subdirectory.
func WithMigrations(path string) Option {
	return func(cfg *suiteConfig) {
		cfg.migrationsPath = path
	}
}

// SetupTestDBSuite starts a PostgreSQL testcontainer, optionally applies
// migrations, runs m.Run(), then tears down. Returns the exit code for os.Exit.
func SetupTestDBSuite(m *testing.M, opts ...Option) int {
	var cfg suiteConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	ctx := context.Background()

	saved := saveEnv()

	container, connStr, host, port, err := startPostgres(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start postgres container: %v\n", err)
		restoreEnv(saved)
		return 1
	}

	os.Setenv("DB_HOST", host)
	os.Setenv("DB_PORT", port)
	os.Setenv("DB_USER", "tracer")
	os.Setenv("DB_PASSWORD", "tracer")
	os.Setenv("DB_NAME", "tracer_test")

	if cfg.migrationsPath != "" {
		if err := applyMigrations(ctx, connStr, cfg.migrationsPath); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to apply migrations: %v\n", err)
			container.Terminate(ctx)
			restoreEnv(saved)
			return 1
		}
	}

	code := m.Run()

	container.Terminate(ctx)
	restoreEnv(saved)

	return code
}

// startPostgres creates a throwaway PostgreSQL container.
func startPostgres(ctx context.Context) (*postgres.PostgresContainer, string, string, string, error) {
	container, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("tracer_test"),
		postgres.WithUsername("tracer"),
		postgres.WithPassword("tracer"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		return nil, "", "", "", fmt.Errorf("start container: %w", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		container.Terminate(ctx)
		return nil, "", "", "", fmt.Errorf("get host: %w", err)
	}

	mappedPort, err := container.MappedPort(ctx, "5432")
	if err != nil {
		container.Terminate(ctx)
		return nil, "", "", "", fmt.Errorf("get port: %w", err)
	}

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		container.Terminate(ctx)
		return nil, "", "", "", fmt.Errorf("get connstr: %w", err)
	}

	return container, connStr, host, mappedPort.Port(), nil
}

// applyMigrations applies function migrations first, then schema migrations.
// Functions must run first because the initial schema references them
// (e.g. prevent_truncate, calculate_audit_event_hash).
func applyMigrations(ctx context.Context, connectionString, migrationsPath string) error {
	db, err := sql.Open("pgx", connectionString)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping db: %w", err)
	}

	// 1. Function migrations (functions/ subdirectory) — execute *.up.sql files directly.
	// We avoid importing pkg/migration here to prevent an import cycle
	// (pkg/migration tests import this package).
	functionsPath := migrationsPath + "/functions"
	if _, statErr := os.Stat(functionsPath); statErr == nil {
		if err := execSQLFiles(ctx, db, functionsPath); err != nil {
			return fmt.Errorf("function migrations: %w", err)
		}
	}

	// 2. Schema migrations via golang-migrate
	driver, err := migratePostgres.WithInstance(db, &migratePostgres.Config{})
	if err != nil {
		return fmt.Errorf("create migrate driver: %w", err)
	}

	m, err := migrate.NewWithDatabaseInstance(
		"file://"+migrationsPath,
		"postgres",
		driver,
	)
	if err != nil {
		return fmt.Errorf("create migrate instance: %w", err)
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("schema migrations: %w", err)
	}

	return nil
}

// saveEnv captures current values of DB env vars for later restoration.
func saveEnv() map[string]*string {
	saved := make(map[string]*string, len(dbEnvVars))
	for _, name := range dbEnvVars {
		if val, ok := os.LookupEnv(name); ok {
			saved[name] = &val
		} else {
			saved[name] = nil
		}
	}
	return saved
}

// restoreEnv restores env vars to their original values.
func restoreEnv(saved map[string]*string) {
	for _, name := range dbEnvVars {
		ptr, existed := saved[name]
		if !existed || ptr == nil {
			os.Unsetenv(name)
		} else {
			os.Setenv(name, *ptr)
		}
	}
}

// execSQLFiles reads and executes all *.up.sql files in dir, sorted by name.
// This is a lightweight alternative to FunctionMigrator for test setup on
// fresh containers where version tracking is unnecessary.
func execSQLFiles(ctx context.Context, db *sql.DB, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read dir %s: %w", dir, err)
	}

	// Collect and sort *.up.sql files
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".up.sql") {
			files = append(files, e.Name())
		}
	}

	sort.Strings(files)

	for _, name := range files {
		content, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}

		if _, err := db.ExecContext(ctx, string(content)); err != nil {
			return fmt.Errorf("exec %s: %w", name, err)
		}
	}

	return nil
}
