// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package db

import (
	"errors"

	libPostgres "github.com/LerianStudio/lib-commons/v2/commons/postgres"
)

// ErrNilConnection is returned when attempting to use a nil database connection.
var ErrNilConnection = errors.New("database connection is nil")

// PostgresConnectionAdapter adapts *libPostgres.PostgresConnection to Connection interface.
// This allows repositories to use a common interface for database connections,
// enabling easier testing with mocks while maintaining compatibility with production connections.
type PostgresConnectionAdapter struct {
	conn *libPostgres.PostgresConnection
}

// NewPostgresConnectionAdapter creates a new adapter for a PostgresConnection.
// Returns nil if conn is nil. Callers should check for nil before use.
func NewPostgresConnectionAdapter(conn *libPostgres.PostgresConnection) *PostgresConnectionAdapter {
	if conn == nil {
		return nil
	}

	return &PostgresConnectionAdapter{conn: conn}
}

// GetDB returns the underlying database connection.
// Returns ErrNilConnection if the adapter was created with a nil connection.
func (p *PostgresConnectionAdapter) GetDB() (DB, error) {
	if p == nil || p.conn == nil {
		return nil, ErrNilConnection
	}

	return p.conn.GetDB()
}
