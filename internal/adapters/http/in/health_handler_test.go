// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package in

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	libCommons "github.com/LerianStudio/lib-commons/v2/commons"
	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.uber.org/mock/gomock"

	"tracer/api"
	"tracer/internal/testutil"
)

// Test configuration constants.
const concurrentRequestCount = 10

// createTestFiberApp creates a Fiber app with telemetry context for testing.
// Uses lib-commons context functions to inject tracer, matching production behavior.
func createTestFiberApp(hc *HealthChecker) *fiber.App {
	app := fiber.New()

	app.Use(func(c *fiber.Ctx) error {
		ctx := c.UserContext()
		ctx = libCommons.ContextWithTracer(ctx, otel.Tracer("tracer-test"))
		c.SetUserContext(ctx)

		return c.Next()
	})
	app.Get("/ready", hc.ReadinessHandler())

	return app
}

// TestReadinessHandler_WithMockDB tests the ReadinessHandler with a mockable database.
func TestReadinessHandler_WithMockDB(t *testing.T) {
	tests := []struct {
		name           string
		connected      bool
		pingError      error
		expectedStatus int
		expectedBody   func(t *testing.T, body []byte)
	}{
		{
			name:           "database healthy",
			connected:      true,
			pingError:      nil,
			expectedStatus: http.StatusOK,
			expectedBody: func(t *testing.T, body []byte) {
				var response api.ReadinessResponse
				err := json.Unmarshal(body, &response)
				require.NoError(t, err, "failed to unmarshal response")

				assert.Equal(t, StatusReady, response.Status, "status should be READY")
				require.Len(t, response.Checks, 2, "should have 2 checks (database + rule_cache)")
				assert.Equal(t, ComponentDatabase, response.Checks[0].Component)
				assert.Equal(t, StatusOK, response.Checks[0].Status)
			},
		},
		{
			name:           "returns 503 when connection not established",
			connected:      false,
			pingError:      nil,
			expectedStatus: http.StatusServiceUnavailable,
			expectedBody: func(t *testing.T, body []byte) {
				var response api.ReadinessResponse
				err := json.Unmarshal(body, &response)
				require.NoError(t, err, "failed to unmarshal response")

				assert.Equal(t, StatusNotReady, response.Status)
				require.Len(t, response.Checks, 2, "should have 2 checks (database + rule_cache)")
				assert.Equal(t, StatusFailed, response.Checks[0].Status)
				assert.Equal(t, ErrConnectionNotEstablished.Error(), response.Checks[0].Message)
			},
		},
		{
			name:           "returns 503 when ping fails",
			connected:      true,
			pingError:      errors.New("connection refused"),
			expectedStatus: http.StatusServiceUnavailable,
			expectedBody: func(t *testing.T, body []byte) {
				var response api.ReadinessResponse
				err := json.Unmarshal(body, &response)
				require.NoError(t, err, "failed to unmarshal response")

				assert.Equal(t, StatusNotReady, response.Status)
				require.Len(t, response.Checks, 2, "should have 2 checks (database + rule_cache)")
				assert.Equal(t, StatusFailed, response.Checks[0].Status)
				assert.Equal(t, ErrPingFailed.Error(), response.Checks[0].Message)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test tracing
			testutil.SetupTestTracing(t)

			// Create gomock controller
			ctrl := gomock.NewController(t)

			// Create sqlmock database
			db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
			require.NoError(t, err)
			defer db.Close()

			// Setup ping expectation
			if tt.connected {
				if tt.pingError != nil {
					mock.ExpectPing().WillReturnError(tt.pingError)
				} else {
					mock.ExpectPing()
				}
			}

			// Create mock provider with gomock expectations
			provider := NewMockPostgresDBProvider(ctrl)
			provider.EXPECT().IsConnected().Return(tt.connected)
			if tt.connected {
				provider.EXPECT().GetDB().Return(db, nil)
			}

			hc := NewTestableHealthChecker(provider)
			app := createTestFiberApp(hc)

			req := httptest.NewRequest(http.MethodGet, "/ready", nil)

			resp, err := app.Test(req, -1)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, tt.expectedStatus, resp.StatusCode)
			assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)

			if tt.expectedBody != nil {
				tt.expectedBody(t, body)
			}

			// Verify all expectations were met
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestReadinessHandler_GetDBError(t *testing.T) {
	t.Run("returns 503 when GetDB returns error", func(t *testing.T) {
		testutil.SetupTestTracing(t)

		ctrl := gomock.NewController(t)

		provider := NewMockPostgresDBProvider(ctrl)
		provider.EXPECT().IsConnected().Return(true)
		provider.EXPECT().GetDB().Return(nil, errors.New("failed to get database connection"))

		hc := NewTestableHealthChecker(provider)
		app := createTestFiberApp(hc)

		req := httptest.NewRequest(http.MethodGet, "/ready", nil)
		resp, err := app.Test(req, -1)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		var response api.ReadinessResponse
		err = json.Unmarshal(body, &response)
		require.NoError(t, err)

		assert.Equal(t, StatusNotReady, response.Status)
		require.Len(t, response.Checks, 2)
		assert.Equal(t, StatusFailed, response.Checks[0].Status)
		assert.Equal(t, ErrConnectionFailed.Error(), response.Checks[0].Message)
	})
}

func TestReadinessHandler_NilProvider(t *testing.T) {
	t.Run("returns 503 when provider is nil", func(t *testing.T) {
		testutil.SetupTestTracing(t)

		hc := NewTestableHealthChecker(nil)
		app := createTestFiberApp(hc)

		req := httptest.NewRequest(http.MethodGet, "/ready", nil)
		resp, err := app.Test(req, -1)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		var response api.ReadinessResponse
		err = json.Unmarshal(body, &response)
		require.NoError(t, err)

		assert.Equal(t, StatusNotReady, response.Status)
		require.Len(t, response.Checks, 2)
		assert.Equal(t, StatusFailed, response.Checks[0].Status)
		assert.Equal(t, ErrConnectionNotEstablished.Error(), response.Checks[0].Message)
	})
}

func TestReadinessHandler_ConcurrentRequests(t *testing.T) {
	t.Run("handles concurrent health check requests", func(t *testing.T) {
		testutil.SetupTestTracing(t)

		ctrl := gomock.NewController(t)

		db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
		require.NoError(t, err)
		defer db.Close()

		// Expect multiple pings from concurrent requests
		for range concurrentRequestCount {
			mock.ExpectPing()
		}

		// Use AnyTimes() for concurrent calls
		provider := NewMockPostgresDBProvider(ctrl)
		provider.EXPECT().IsConnected().Return(true).AnyTimes()
		provider.EXPECT().GetDB().Return(db, nil).AnyTimes()

		hc := NewTestableHealthChecker(provider)
		app := createTestFiberApp(hc)

		var wg sync.WaitGroup
		results := make(chan int, concurrentRequestCount)

		// Launch concurrent requests
		for range concurrentRequestCount {
			wg.Add(1)

			go func() {
				defer wg.Done()

				req := httptest.NewRequest(http.MethodGet, "/ready", nil)

				resp, err := app.Test(req, -1)
				if err != nil {
					results <- -1

					return
				}
				defer resp.Body.Close()

				results <- resp.StatusCode
			}()
		}

		wg.Wait()
		close(results)

		// All requests should complete successfully
		successCount := 0

		for status := range results {
			if status == http.StatusOK {
				successCount++
			}
		}

		assert.Equal(t, concurrentRequestCount, successCount, "all concurrent requests should succeed")
	})
}

func TestReadinessHandler_Timeout(t *testing.T) {
	t.Run("returns error when health check exceeds timeout", func(t *testing.T) {
		testutil.SetupTestTracing(t)

		ctrl := gomock.NewController(t)

		// Create a provider with a slow ping that will exceed the timeout
		db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
		require.NoError(t, err)
		defer db.Close()

		// Configure ping to delay longer than the timeout
		mock.ExpectPing().WillDelayFor(5 * time.Second)

		provider := NewMockPostgresDBProvider(ctrl)
		provider.EXPECT().IsConnected().Return(true)
		provider.EXPECT().GetDB().Return(db, nil)

		// Create a health checker with a very short timeout
		hc := &HealthChecker{
			dbProvider: provider,
			timeout:    50 * time.Millisecond, // Very short timeout
		}

		app := createTestFiberApp(hc)

		req := httptest.NewRequest(http.MethodGet, "/ready", nil)

		resp, err := app.Test(req, 10000) // 10 second test timeout
		require.NoError(t, err)
		defer resp.Body.Close()

		// Should return 503 because the ping exceeded the context timeout
		assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		var response api.ReadinessResponse
		err = json.Unmarshal(body, &response)
		require.NoError(t, err)

		assert.Equal(t, StatusNotReady, response.Status)
	})
}

func TestDefaultHealthCheckTimeout(t *testing.T) {
	t.Run("default timeout is 3 seconds", func(t *testing.T) {
		assert.Equal(t, 3*time.Second, DefaultHealthCheckTimeout)
	})

	t.Run("NewHealthChecker uses default timeout", func(t *testing.T) {
		hc := NewHealthChecker(nil)
		assert.Equal(t, DefaultHealthCheckTimeout, hc.timeout)
	})

	t.Run("NewTestableHealthChecker uses default timeout", func(t *testing.T) {
		hc := NewTestableHealthChecker(nil)
		assert.Equal(t, DefaultHealthCheckTimeout, hc.timeout)
	})
}

func TestReadiness_CacheNotReady_ReturnsDegraded(t *testing.T) {
	testutil.SetupTestTracing(t)

	ctrl := gomock.NewController(t)
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectPing()

	provider := NewMockPostgresDBProvider(ctrl)
	provider.EXPECT().IsConnected().Return(true)
	provider.EXPECT().GetDB().Return(db, nil)

	hc := NewTestableHealthChecker(provider)
	mockCache := &mockCacheHealth{ready: false, staleness: time.Duration(math.MaxInt64), size: 0}
	hc.SetCacheHealthProvider(mockCache)

	app := createTestFiberApp(hc)
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	resp, err := app.Test(req, -1)

	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode, "DEGRADED should return 200")

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var response api.ReadinessResponse
	err = json.Unmarshal(body, &response)
	require.NoError(t, err)
	assert.Equal(t, StatusDegraded, response.Status)

	// Verify cache component appears in checks
	require.GreaterOrEqual(t, len(response.Checks), 1, "should include cache check")
	var cacheCheck *api.HealthCheck
	for i := range response.Checks {
		if response.Checks[i].Component == ComponentRuleCache {
			cacheCheck = &response.Checks[i]
			break
		}
	}
	require.NotNil(t, cacheCheck, "should have rule_cache check")
	assert.Equal(t, StatusFailed, cacheCheck.Status)
	assert.Equal(t, "cache not ready", cacheCheck.Message)
}

func TestReadiness_CacheReady_ReturnsUp(t *testing.T) {
	testutil.SetupTestTracing(t)

	ctrl := gomock.NewController(t)
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectPing()

	provider := NewMockPostgresDBProvider(ctrl)
	provider.EXPECT().IsConnected().Return(true)
	provider.EXPECT().GetDB().Return(db, nil)

	hc := NewTestableHealthChecker(provider)
	mockCache := &mockCacheHealth{ready: true, staleness: 5 * time.Second, size: 10}
	hc.SetCacheHealthProvider(mockCache)

	app := createTestFiberApp(hc)
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	resp, err := app.Test(req, -1)

	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var response api.ReadinessResponse
	err = json.Unmarshal(body, &response)
	require.NoError(t, err)
	assert.Equal(t, StatusReady, response.Status)

	// Verify cache component shows OK
	var cacheCheck *api.HealthCheck
	for i := range response.Checks {
		if response.Checks[i].Component == ComponentRuleCache {
			cacheCheck = &response.Checks[i]
			break
		}
	}
	require.NotNil(t, cacheCheck, "should have rule_cache check")
	assert.Equal(t, StatusOK, cacheCheck.Status)
}

func TestReadiness_CacheStalenessExceeded_ReturnsDegraded(t *testing.T) {
	testutil.SetupTestTracing(t)

	ctrl := gomock.NewController(t)
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectPing()

	provider := NewMockPostgresDBProvider(ctrl)
	provider.EXPECT().IsConnected().Return(true)
	provider.EXPECT().GetDB().Return(db, nil)

	hc := NewTestableHealthChecker(provider)
	// Cache is ready but staleness exceeds threshold
	mockCache := &mockCacheHealth{ready: true, staleness: 10 * time.Minute, size: 5}
	hc.SetCacheHealthProvider(mockCache)

	app := createTestFiberApp(hc)
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	resp, err := app.Test(req, -1)

	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode, "DEGRADED should return 200")

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var response api.ReadinessResponse
	err = json.Unmarshal(body, &response)
	require.NoError(t, err)
	assert.Equal(t, StatusDegraded, response.Status)

	// Verify cache component shows stale status
	var cacheCheck *api.HealthCheck
	for i := range response.Checks {
		if response.Checks[i].Component == ComponentRuleCache {
			cacheCheck = &response.Checks[i]
			break
		}
	}
	require.NotNil(t, cacheCheck, "should have rule_cache check")
	assert.Equal(t, StatusFailed, cacheCheck.Status)
	assert.Equal(t, "cache data stale", cacheCheck.Message)
}

// mockCacheHealth implements RuleCacheHealthProvider for testing.
type mockCacheHealth struct {
	ready     bool
	staleness time.Duration
	size      int
}

func (m *mockCacheHealth) IsReady() bool            { return m.ready }
func (m *mockCacheHealth) Staleness() time.Duration { return m.staleness }
func (m *mockCacheHealth) Size() int                { return m.size }
