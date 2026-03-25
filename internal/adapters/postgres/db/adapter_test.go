// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package db

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/bxcodec/dbresolver/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubTx is a minimal dbresolver.Tx implementation for testing.
type stubTx struct {
	dbresolver.Tx
}

// stubDB is a test double for dbresolver.DB that controls BeginTx behavior.
type stubDB struct {
	dbresolver.DB
	tx           dbresolver.Tx
	err          error
	receivedOpts *sql.TxOptions
}

func (s *stubDB) BeginTx(_ context.Context, opts *sql.TxOptions) (dbresolver.Tx, error) {
	s.receivedOpts = opts

	return s.tx, s.err
}

func TestNewTxBeginnerAdapter_NilDB(t *testing.T) {
	adapter := NewTxBeginnerAdapter(nil)
	assert.Nil(t, adapter)
}

func TestNewTxBeginnerAdapter_ValidDB(t *testing.T) {
	adapter := NewTxBeginnerAdapter(&stubDB{})
	require.NotNil(t, adapter)
}

func TestTxBeginnerAdapter_BeginTx_NilAdapter(t *testing.T) {
	var adapter *TxBeginnerAdapter

	tx, err := adapter.BeginTx(context.Background(), nil)

	assert.Nil(t, tx)
	assert.ErrorIs(t, err, ErrNilConnection)
}

func TestTxBeginnerAdapter_BeginTx_NilDB(t *testing.T) {
	adapter := &TxBeginnerAdapter{db: nil}

	tx, err := adapter.BeginTx(context.Background(), nil)

	assert.Nil(t, tx)
	assert.ErrorIs(t, err, ErrNilConnection)
}

func TestTxBeginnerAdapter_BeginTx_ErrorPropagation(t *testing.T) {
	dbErr := errors.New("connection refused")
	adapter := NewTxBeginnerAdapter(&stubDB{err: dbErr})

	tx, err := adapter.BeginTx(context.Background(), nil)

	assert.Nil(t, tx)
	assert.ErrorIs(t, err, dbErr)
}

func TestTxBeginnerAdapter_BeginTx_Success(t *testing.T) {
	expectedTx := &stubTx{}
	adapter := NewTxBeginnerAdapter(&stubDB{tx: expectedTx})

	tx, err := adapter.BeginTx(context.Background(), nil)

	require.NoError(t, err)
	assert.Equal(t, expectedTx, tx)
}

func TestTxBeginnerAdapter_BeginTx_ForwardsOptions(t *testing.T) {
	stub := &stubDB{tx: &stubTx{}}
	adapter := NewTxBeginnerAdapter(stub)

	opts := &sql.TxOptions{Isolation: sql.LevelSerializable, ReadOnly: true}
	tx, err := adapter.BeginTx(context.Background(), opts)

	require.NoError(t, err)
	require.NotNil(t, tx)
	assert.Equal(t, opts, stub.receivedOpts)
}
