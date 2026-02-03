// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package db

//go:generate mockgen -source=interfaces.go -destination=mocks/interfaces_mock.go -package=mocks

import (
	"context"
	"database/sql"
)

// DB defines the minimal database interface required by repositories.
// This interface is satisfied by both *sql.DB and dbresolver.DB.
type DB interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Connection defines the interface for database connection providers.
// This allows for easy mocking in tests while maintaining compatibility
// with *libPostgres.PostgresConnection in production.
type Connection interface {
	GetDB() (DB, error)
}
