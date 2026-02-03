// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package testutil

import (
	"database/sql"
	"net/url"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"

	pgdb "tracer/internal/adapters/postgres/db"
)

// GetTestDSN builds the PostgreSQL connection string from environment variables.
// Uses URL-format DSN with proper escaping for special characters in credentials.
// Uses same defaults as docker-compose for local development.
// Environment variables: DB_HOST, DB_PORT, DB_USER, DB_PASSWORD, DB_NAME, DB_SSL_MODE.
func GetTestDSN() string {
	host := os.Getenv("DB_HOST")
	if host == "" {
		host = "localhost"
	}

	port := os.Getenv("DB_PORT")
	if port == "" {
		port = "5432"
	}

	user := os.Getenv("DB_USER")
	if user == "" {
		user = "tracer"
	}

	password := os.Getenv("DB_PASSWORD")
	if password == "" {
		password = "tracer"
	}

	dbName := os.Getenv("DB_NAME")
	if dbName == "" {
		dbName = "tracer"
	}

	sslMode := os.Getenv("DB_SSL_MODE")
	if sslMode == "" {
		sslMode = "disable"
	}

	// Build URL-format DSN with proper escaping for special characters
	dsn := &url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(user, password),
		Host:     host + ":" + port,
		Path:     "/" + dbName,
		RawQuery: "sslmode=" + url.QueryEscape(sslMode),
	}

	return dsn.String()
}

// SetupIntegrationDB creates a database connection for integration tests.
// Uses t.Cleanup for automatic teardown after all test cleanups.
func SetupIntegrationDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn := GetTestDSN()
	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err, "Failed to open database connection")

	// Verify connection
	err = db.Ping()
	require.NoError(t, err, "Failed to ping database - ensure PostgreSQL is running")

	// Use t.Cleanup for automatic teardown (runs after all other t.Cleanup calls in LIFO order)
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Logf("failed to close db: %v", err)
		}
	})

	return db
}

// IntegrationDBAdapter wraps *sql.DB to implement the pgdb.Connection interface for integration tests.
type IntegrationDBAdapter struct {
	DB *sql.DB
}

// GetDB returns the underlying *sql.DB which implements pgdb.DB interface.
func (a *IntegrationDBAdapter) GetDB() (pgdb.DB, error) {
	return a.DB, nil
}
