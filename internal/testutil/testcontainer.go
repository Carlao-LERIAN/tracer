// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

//go:build integration

package testutil

import (
	"context"
	"fmt"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// TestPostgresContainer holds the postgres testcontainer instance.
type TestPostgresContainer struct {
	*postgres.PostgresContainer
	ConnectionString string
	Host             string
	Port             string
}

// NewTestPostgresContainer creates a new postgres container for integration tests.
// The container starts empty - the application will run migrations on startup.
func NewTestPostgresContainer(ctx context.Context) (*TestPostgresContainer, error) {
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
		return nil, fmt.Errorf("failed to start postgres container: %w", err)
	}

	// Helper to terminate container on error (prevents resource leak)
	// Uses a fresh background context to ensure cleanup runs even if original ctx is canceled
	terminateOnError := func(err error) (*TestPostgresContainer, error) {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if termErr := container.Terminate(cleanupCtx); termErr != nil {
			return nil, fmt.Errorf("%w (also failed to terminate container: %v)", err, termErr)
		}

		return nil, err
	}

	host, err := container.Host(ctx)
	if err != nil {
		return terminateOnError(fmt.Errorf("failed to get container host: %w", err))
	}

	port, err := container.MappedPort(ctx, "5432")
	if err != nil {
		return terminateOnError(fmt.Errorf("failed to get container port: %w", err))
	}

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return terminateOnError(fmt.Errorf("failed to get connection string: %w", err))
	}

	return &TestPostgresContainer{
		PostgresContainer: container,
		ConnectionString:  connStr,
		Host:              host,
		Port:              port.Port(),
	}, nil
}

// Terminate stops and removes the container.
func (c *TestPostgresContainer) Terminate(ctx context.Context) error {
	return c.PostgresContainer.Terminate(ctx)
}
