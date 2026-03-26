// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package db

import (
	"context"
	"database/sql"
	"errors"

	libPostgres "github.com/LerianStudio/lib-commons/v4/commons/postgres"
	"github.com/bxcodec/dbresolver/v2"
)

// ErrNilConnection is returned when attempting to use a nil database connection.
var ErrNilConnection = errors.New("database connection is nil")

// PostgresConnectionAdapter adapts *libPostgres.Client to Connection interface.
// This allows repositories to use a common interface for database connections,
// enabling easier testing with mocks while maintaining compatibility with production connections.
type PostgresConnectionAdapter struct {
	conn *libPostgres.Client
}

// NewPostgresConnectionAdapter creates a new adapter for a postgres Client.
// Returns nil if conn is nil. Callers should check for nil before use.
func NewPostgresConnectionAdapter(conn *libPostgres.Client) *PostgresConnectionAdapter {
	if conn == nil {
		return nil
	}

	return &PostgresConnectionAdapter{conn: conn}
}

// GetDB returns the underlying database connection using the provided context.
// The context is propagated to the connection resolver, enabling deadline,
// cancellation, and trace correlation through the connection lifecycle.
// Returns ErrNilConnection if the adapter was created with a nil connection.
func (p *PostgresConnectionAdapter) GetDB(ctx context.Context) (DB, error) {
	if p == nil || p.conn == nil {
		return nil, ErrNilConnection
	}

	return p.conn.Resolver(ctx)
}

// TxBeginnerAdapter adapts dbresolver.DB to our TxBeginner interface.
// This is necessary because dbresolver.DB.BeginTx returns dbresolver.Tx,
// while our TxBeginner interface expects Tx (our interface).
// Both interfaces are structurally compatible, but Go requires explicit adaptation.
type TxBeginnerAdapter struct {
	db dbresolver.DB
}

// NewTxBeginnerAdapter creates a new TxBeginnerAdapter.
// Returns nil if db is nil. Callers should check for nil before use.
func NewTxBeginnerAdapter(db dbresolver.DB) *TxBeginnerAdapter {
	if db == nil {
		return nil
	}

	return &TxBeginnerAdapter{db: db}
}

// BeginTx starts a new database transaction.
// The returned Tx is a wrapper around dbresolver.Tx that satisfies our Tx interface.
func (t *TxBeginnerAdapter) BeginTx(ctx context.Context, opts *sql.TxOptions) (Tx, error) {
	if t == nil || t.db == nil {
		return nil, ErrNilConnection
	}

	tx, err := t.db.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}

	// dbresolver.Tx satisfies our Tx interface (structurally compatible)
	return tx, nil
}

// Compile-time interface satisfaction checks.
var _ TxBeginner = (*TxBeginnerAdapter)(nil)
