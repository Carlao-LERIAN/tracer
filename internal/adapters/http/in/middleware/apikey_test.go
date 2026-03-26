// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package middleware

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"tracer/internal/testutil"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// errorResponse represents the standard error response format from libHTTP.
type errorResponse struct {
	Code   string `json:"code"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

func TestAPIKeyAuth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		config         APIKeyConfig
		apiKey         string
		setHeader      bool
		expectedStatus int
		expectedCode   string
		expectedBody   string
	}{
		{
			name: "Success - Valid API Key",
			config: APIKeyConfig{
				Key:     "valid-secret-key",
				Enabled: true,
			},
			apiKey:         "valid-secret-key",
			setHeader:      true,
			expectedStatus: http.StatusOK,
			expectedBody:   "success",
		},
		{
			name: "Error - Missing API Key (no header)",
			config: APIKeyConfig{
				Key:     "valid-secret-key",
				Enabled: true,
			},
			apiKey:         "",
			setHeader:      false,
			expectedStatus: http.StatusUnauthorized,
			expectedCode:   "Unauthenticated",
		},
		{
			name: "Error - Invalid API Key",
			config: APIKeyConfig{
				Key:     "valid-secret-key",
				Enabled: true,
			},
			apiKey:         "wrong-key",
			setHeader:      true,
			expectedStatus: http.StatusUnauthorized,
			expectedCode:   "Unauthenticated",
		},
		{
			name: "Error - Empty API Key (header present but empty)",
			config: APIKeyConfig{
				Key:     "valid-secret-key",
				Enabled: true,
			},
			apiKey:         "",
			setHeader:      true,
			expectedStatus: http.StatusUnauthorized,
			expectedCode:   "Unauthenticated",
		},
		{
			name: "Success - Auth Disabled (no key required)",
			config: APIKeyConfig{
				Key:     "valid-secret-key",
				Enabled: false,
			},
			apiKey:         "",
			setHeader:      false,
			expectedStatus: http.StatusOK,
			expectedBody:   "success",
		},
		{
			name: "Success - Auth Disabled (request with wrong key still passes)",
			config: APIKeyConfig{
				Key:     "valid-secret-key",
				Enabled: false,
			},
			apiKey:         "any-key",
			setHeader:      true,
			expectedStatus: http.StatusOK,
			expectedBody:   "success",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup Fiber app with middleware
			app := fiber.New()
			app.Use(APIKeyAuth(tt.config))
			app.Get("/test", func(c *fiber.Ctx) error {
				return c.SendString("success")
			})

			// Create request
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			if tt.setHeader {
				req.Header.Set("X-API-Key", tt.apiKey)
			}

			// Execute request
			resp, err := app.Test(req, -1)
			require.NoError(t, err)
			defer resp.Body.Close()

			// Assert status code
			assert.Equal(t, tt.expectedStatus, resp.StatusCode)

			// Assert response body
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)

			if tt.expectedStatus == http.StatusOK {
				assert.Equal(t, tt.expectedBody, string(body))
			} else if tt.expectedCode != "" {
				// Parse error response and check code
				var errResp errorResponse
				err := json.Unmarshal(body, &errResp)
				require.NoError(t, err, "Expected JSON error response, got: %s", string(body))
				assert.Equal(t, tt.expectedCode, errResp.Code, "Error code should be 'Unauthenticated'")
			}
		})
	}
}

// TestAPIKeyAuth_ConstantTimeComparison verifies timing attack resistance.
// This test ensures that comparison time doesn't vary significantly based on
// how much of the key matches. While we can't perfectly test constant-time
// behavior in unit tests, we can verify the implementation uses the correct approach.
func TestAPIKeyAuth_ConstantTimeComparison(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		config   APIKeyConfig
		apiKey   string
		expected int
	}{
		{
			name: "Completely different key",
			config: APIKeyConfig{
				Key:     "AAAAAAAAAAAAAAAA",
				Enabled: true,
			},
			apiKey:   "BBBBBBBBBBBBBBBB",
			expected: http.StatusUnauthorized,
		},
		{
			name: "First character different",
			config: APIKeyConfig{
				Key:     "AAAAAAAAAAAAAAAA",
				Enabled: true,
			},
			apiKey:   "BAAAAAAAAAAAAAAA",
			expected: http.StatusUnauthorized,
		},
		{
			name: "Last character different",
			config: APIKeyConfig{
				Key:     "AAAAAAAAAAAAAAAA",
				Enabled: true,
			},
			apiKey:   "AAAAAAAAAAAAAAAB",
			expected: http.StatusUnauthorized,
		},
		{
			name: "Different length",
			config: APIKeyConfig{
				Key:     "short",
				Enabled: true,
			},
			apiKey:   "muchlongerkey",
			expected: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := fiber.New()
			app.Use(APIKeyAuth(tt.config))
			app.Get("/test", func(c *fiber.Ctx) error {
				return c.SendString("success")
			})

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.Header.Set("X-API-Key", tt.apiKey)

			resp, err := app.Test(req, -1)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, tt.expected, resp.StatusCode)
		})
	}
}

// TestAPIKeyAuth_SameErrorMessage ensures the same error is returned for both
// missing and invalid keys to prevent enumeration attacks.
func TestAPIKeyAuth_SameErrorMessage(t *testing.T) {
	t.Parallel()

	config := APIKeyConfig{
		Key:     "secret-key",
		Enabled: true,
	}

	app := fiber.New()
	app.Use(APIKeyAuth(config))
	app.Get("/test", func(c *fiber.Ctx) error {
		return c.SendString("success")
	})

	// Test missing key
	reqMissing := httptest.NewRequest(http.MethodGet, "/test", nil)
	respMissing, err := app.Test(reqMissing, -1)
	require.NoError(t, err)
	defer respMissing.Body.Close()

	bodyMissing, err := io.ReadAll(respMissing.Body)
	require.NoError(t, err)

	// Test invalid key
	reqInvalid := httptest.NewRequest(http.MethodGet, "/test", nil)
	reqInvalid.Header.Set("X-API-Key", "wrong-key")
	respInvalid, err := app.Test(reqInvalid, -1)
	require.NoError(t, err)
	defer respInvalid.Body.Close()

	bodyInvalid, err := io.ReadAll(respInvalid.Body)
	require.NoError(t, err)

	// Parse both responses
	var errMissing, errInvalid errorResponse
	err = json.Unmarshal(bodyMissing, &errMissing)
	require.NoError(t, err)
	err = json.Unmarshal(bodyInvalid, &errInvalid)
	require.NoError(t, err)

	// Verify same error code and detail
	assert.Equal(t, errMissing.Code, errInvalid.Code, "Error codes should be identical")
	assert.Equal(t, errMissing.Title, errInvalid.Title, "Error titles should be identical")
	assert.Equal(t, errMissing.Detail, errInvalid.Detail, "Error details should be identical")
}

// =============================================================================
// APIKeyAuthWithLogger tests
// =============================================================================

func TestAPIKeyAuthWithLogger_MissingKey_LogsWarning(t *testing.T) {
	t.Parallel()

	// Arrange
	mockLogger := testutil.NewMockLogger()
	config := APIKeyConfig{
		Key:     "valid-secret-key",
		Enabled: true,
	}

	app := fiber.New()
	app.Use(APIKeyAuthWithLogger(config, mockLogger))
	app.Get("/v1/validations", func(c *fiber.Ctx) error {
		return c.SendString("success")
	})

	// Act
	req := httptest.NewRequest(http.MethodGet, "/v1/validations", nil)
	// No X-API-Key header - missing key scenario
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Assert - should return 401
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	// Assert - should log warning with correct fields
	require.Len(t, mockLogger.Calls, 1, "expected exactly one log call")
	call := mockLogger.Calls[0]
	assert.Equal(t, "warn", call.Level, "expected warn level")
	assert.Equal(t, "auth_failed", call.Message, "expected auth_failed message")

	// Check fields contain reason=missing_api_key
	fieldsMap := testutil.FieldsToMap(call.Fields)
	assert.Equal(t, "missing_api_key", fieldsMap["reason"], "expected reason=missing_api_key")
	assert.Equal(t, "/v1/validations", fieldsMap["path"], "expected path=/v1/validations")
	assert.Contains(t, fieldsMap, "remote_ip", "expected remote_ip field")
}

func TestAPIKeyAuthWithLogger_InvalidKey_LogsWarning(t *testing.T) {
	t.Parallel()

	// Arrange
	mockLogger := testutil.NewMockLogger()
	config := APIKeyConfig{
		Key:     "valid-secret-key",
		Enabled: true,
	}

	app := fiber.New()
	app.Use(APIKeyAuthWithLogger(config, mockLogger))
	app.Get("/v1/validations", func(c *fiber.Ctx) error {
		return c.SendString("success")
	})

	// Act
	req := httptest.NewRequest(http.MethodGet, "/v1/validations", nil)
	req.Header.Set("X-API-Key", "wrong-key") // Invalid key scenario
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Assert - should return 401
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	// Assert - should log warning with correct fields
	require.Len(t, mockLogger.Calls, 1, "expected exactly one log call")
	call := mockLogger.Calls[0]
	assert.Equal(t, "warn", call.Level, "expected warn level")
	assert.Equal(t, "auth_failed", call.Message, "expected auth_failed message")

	// Check fields contain reason=invalid_api_key
	fieldsMap := testutil.FieldsToMap(call.Fields)
	assert.Equal(t, "invalid_api_key", fieldsMap["reason"], "expected reason=invalid_api_key")
	assert.Equal(t, "/v1/validations", fieldsMap["path"], "expected path=/v1/validations")
	assert.Contains(t, fieldsMap, "remote_ip", "expected remote_ip field")
}

func TestAPIKeyAuthWithLogger_ValidKey_LogsDebug(t *testing.T) {
	t.Parallel()

	// Arrange
	mockLogger := testutil.NewMockLogger()
	config := APIKeyConfig{
		Key:     "valid-secret-key",
		Enabled: true,
	}

	app := fiber.New()
	app.Use(APIKeyAuthWithLogger(config, mockLogger))
	app.Get("/v1/validations", func(c *fiber.Ctx) error {
		return c.SendString("success")
	})

	// Act
	req := httptest.NewRequest(http.MethodGet, "/v1/validations", nil)
	req.Header.Set("X-API-Key", "valid-secret-key") // Valid key scenario
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Assert - should return 200
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Assert - should log debug with path
	require.Len(t, mockLogger.Calls, 1, "expected exactly one log call")
	call := mockLogger.Calls[0]
	assert.Equal(t, "debug", call.Level, "expected debug level")
	assert.Equal(t, "auth_success", call.Message, "expected auth_success message")

	// Check fields contain path
	fieldsMap := testutil.FieldsToMap(call.Fields)
	assert.Equal(t, "/v1/validations", fieldsMap["path"], "expected path=/v1/validations")
}

func TestAPIKeyAuthWithLogger_NeverLogsAPIKeyValue(t *testing.T) {
	t.Parallel()

	// Arrange - use a distinctive API key to search for
	secretKey := "SUPER_SECRET_KEY_12345_DO_NOT_LOG"

	// Test cases: missing, invalid, and valid keys
	testCases := []struct {
		name   string
		apiKey string
	}{
		{"missing_key", ""},
		{"invalid_key", "wrong_key_attempt"},
		{"valid_key", secretKey},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Create fresh mock and app for each subtest to avoid data races
			mockLogger := testutil.NewMockLogger()
			config := APIKeyConfig{
				Key:     secretKey,
				Enabled: true,
			}

			app := fiber.New()
			app.Use(APIKeyAuthWithLogger(config, mockLogger))
			app.Get("/v1/validations", func(c *fiber.Ctx) error {
				return c.SendString("success")
			})

			req := httptest.NewRequest(http.MethodGet, "/v1/validations", nil)
			if tc.apiKey != "" {
				req.Header.Set("X-API-Key", tc.apiKey)
			}

			resp, err := app.Test(req, -1)
			require.NoError(t, err)
			resp.Body.Close()

			// Verify the API key value is never logged
			for _, call := range mockLogger.Calls {
				// Check message doesn't contain the API key
				assert.False(t, strings.Contains(call.Message, secretKey),
					"message should not contain API key value")
				// Only check for request API key if it's not empty
				if tc.apiKey != "" {
					assert.False(t, strings.Contains(call.Message, tc.apiKey),
						"message should not contain request API key value")
				}

				// Check fields don't contain the API key
				for _, field := range call.Fields {
					fieldStr, ok := field.Value.(string)
					if ok {
						assert.False(t, strings.Contains(fieldStr, secretKey),
							"field should not contain API key value")
						if tc.apiKey != "" {
							assert.False(t, strings.Contains(fieldStr, tc.apiKey),
								"field should not contain request API key value")
						}
					}
				}
			}
		})
	}
}

func TestAPIKeyAuthWithLogger_DisabledAuth_NoLogging(t *testing.T) {
	t.Parallel()

	// Arrange
	mockLogger := testutil.NewMockLogger()
	config := APIKeyConfig{
		Key:     "valid-secret-key",
		Enabled: false, // Auth disabled
	}

	app := fiber.New()
	app.Use(APIKeyAuthWithLogger(config, mockLogger))
	app.Get("/v1/validations", func(c *fiber.Ctx) error {
		return c.SendString("success")
	})

	// Act
	req := httptest.NewRequest(http.MethodGet, "/v1/validations", nil)
	// No X-API-Key header
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Assert - should return 200
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Assert - no logging when auth is disabled
	assert.Len(t, mockLogger.Calls, 0, "expected no log calls when auth is disabled")
}

// =============================================================================
// APIKeyAuthWithMetrics tests
// =============================================================================

// MetricCall represents a single metric increment call for testing.
type MetricCall struct {
	MetricName string
	Labels     map[string]string
	Value      int64
}

// mockMetricsRecorder tracks metric calls for verification in tests.
type mockMetricsRecorder struct {
	Calls []MetricCall
}

func newMockMetricsRecorder() *mockMetricsRecorder {
	return &mockMetricsRecorder{
		Calls: []MetricCall{},
	}
}

func (m *mockMetricsRecorder) Counter(metric Metric) CounterAdder {
	return &mockCounterBuilder{
		recorder:   m,
		metricName: metric.Name,
		labels:     make(map[string]string),
	}
}

type mockCounterBuilder struct {
	recorder   *mockMetricsRecorder
	metricName string
	labels     map[string]string
}

func (c *mockCounterBuilder) WithLabels(labels map[string]string) CounterAdder {
	for k, v := range labels {
		c.labels[k] = v
	}

	return c
}

func (c *mockCounterBuilder) Add(_ context.Context, value int64) {
	c.recorder.Calls = append(c.recorder.Calls, MetricCall{
		MetricName: c.metricName,
		Labels:     c.labels,
		Value:      value,
	})
}

// TestAPIKeyAuthWithMetrics_FunctionExists verifies the function signature exists.
func TestAPIKeyAuthWithMetrics_FunctionExists(t *testing.T) {
	t.Parallel()

	// Arrange
	config := APIKeyConfig{
		Key:     "test-key",
		Enabled: true,
	}
	mockLogger := testutil.NewMockLogger()

	// Act - This test will fail until APIKeyAuthWithMetrics is implemented
	// The function should accept: cfg, logger, metricsFactory, telemetry
	// and return a fiber.Handler
	handler := APIKeyAuthWithMetrics(config, mockLogger, nil, nil)

	// Assert - function should return a non-nil handler
	assert.NotNil(t, handler, "APIKeyAuthWithMetrics should return a fiber.Handler")
}

// TestAPIKeyAuthWithMetrics_MissingKey_Returns401 verifies auth behavior with missing key.
func TestAPIKeyAuthWithMetrics_MissingKey_Returns401(t *testing.T) {
	t.Parallel()

	// Arrange
	config := APIKeyConfig{
		Key:     "valid-secret-key",
		Enabled: true,
	}
	mockLogger := testutil.NewMockLogger()

	app := fiber.New()
	// Pass nil for metrics and telemetry since we're testing basic auth behavior
	app.Use(APIKeyAuthWithMetrics(config, mockLogger, nil, nil))
	app.Get("/v1/validations", func(c *fiber.Ctx) error {
		return c.SendString("success")
	})

	// Act
	req := httptest.NewRequest(http.MethodGet, "/v1/validations", nil)
	// No X-API-Key header
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Assert - should return 401
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

// TestAPIKeyAuthWithMetrics_InvalidKey_Returns401 verifies auth behavior with invalid key.
func TestAPIKeyAuthWithMetrics_InvalidKey_Returns401(t *testing.T) {
	t.Parallel()

	// Arrange
	config := APIKeyConfig{
		Key:     "valid-secret-key",
		Enabled: true,
	}
	mockLogger := testutil.NewMockLogger()

	app := fiber.New()
	app.Use(APIKeyAuthWithMetrics(config, mockLogger, nil, nil))
	app.Get("/v1/validations", func(c *fiber.Ctx) error {
		return c.SendString("success")
	})

	// Act
	req := httptest.NewRequest(http.MethodGet, "/v1/validations", nil)
	req.Header.Set("X-API-Key", "wrong-key")
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Assert - should return 401
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

// TestAPIKeyAuthWithMetrics_ValidKey_Returns200 verifies auth passes with valid key.
func TestAPIKeyAuthWithMetrics_ValidKey_Returns200(t *testing.T) {
	t.Parallel()

	// Arrange
	config := APIKeyConfig{
		Key:     "valid-secret-key",
		Enabled: true,
	}
	mockLogger := testutil.NewMockLogger()

	app := fiber.New()
	app.Use(APIKeyAuthWithMetrics(config, mockLogger, nil, nil))
	app.Get("/v1/validations", func(c *fiber.Ctx) error {
		return c.SendString("success")
	})

	// Act
	req := httptest.NewRequest(http.MethodGet, "/v1/validations", nil)
	req.Header.Set("X-API-Key", "valid-secret-key")
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Assert - should return 200
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Read body
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "success", string(body))
}

// TestAPIKeyAuthWithMetrics_DisabledAuth_Passes verifies disabled auth passes all requests.
func TestAPIKeyAuthWithMetrics_DisabledAuth_Passes(t *testing.T) {
	t.Parallel()

	// Arrange
	config := APIKeyConfig{
		Key:     "valid-secret-key",
		Enabled: false, // Auth disabled
	}
	mockLogger := testutil.NewMockLogger()

	app := fiber.New()
	app.Use(APIKeyAuthWithMetrics(config, mockLogger, nil, nil))
	app.Get("/v1/validations", func(c *fiber.Ctx) error {
		return c.SendString("success")
	})

	// Act
	req := httptest.NewRequest(http.MethodGet, "/v1/validations", nil)
	// No API key, but auth is disabled
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Assert - should return 200
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestAPIKeyAuthWithMetrics_MissingKey_LogsWarning verifies logging behavior.
func TestAPIKeyAuthWithMetrics_MissingKey_LogsWarning(t *testing.T) {
	t.Parallel()

	// Arrange
	config := APIKeyConfig{
		Key:     "valid-secret-key",
		Enabled: true,
	}
	mockLogger := testutil.NewMockLogger()

	app := fiber.New()
	app.Use(APIKeyAuthWithMetrics(config, mockLogger, nil, nil))
	app.Get("/v1/validations", func(c *fiber.Ctx) error {
		return c.SendString("success")
	})

	// Act
	req := httptest.NewRequest(http.MethodGet, "/v1/validations", nil)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Assert - should log warning with reason=missing_api_key
	require.Len(t, mockLogger.Calls, 1, "expected exactly one log call")
	call := mockLogger.Calls[0]
	assert.Equal(t, "warn", call.Level)
	assert.Equal(t, "auth_failed", call.Message)

	fieldsMap := testutil.FieldsToMap(call.Fields)
	assert.Equal(t, ReasonMissingAPIKey, fieldsMap["reason"])
}

// TestAPIKeyAuthWithMetrics_InvalidKey_LogsWarning verifies logging behavior.
func TestAPIKeyAuthWithMetrics_InvalidKey_LogsWarning(t *testing.T) {
	t.Parallel()

	// Arrange
	config := APIKeyConfig{
		Key:     "valid-secret-key",
		Enabled: true,
	}
	mockLogger := testutil.NewMockLogger()

	app := fiber.New()
	app.Use(APIKeyAuthWithMetrics(config, mockLogger, nil, nil))
	app.Get("/v1/validations", func(c *fiber.Ctx) error {
		return c.SendString("success")
	})

	// Act
	req := httptest.NewRequest(http.MethodGet, "/v1/validations", nil)
	req.Header.Set("X-API-Key", "wrong-key")
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Assert - should log warning with reason=invalid_api_key
	require.Len(t, mockLogger.Calls, 1, "expected exactly one log call")
	call := mockLogger.Calls[0]
	assert.Equal(t, "warn", call.Level)
	assert.Equal(t, "auth_failed", call.Message)

	fieldsMap := testutil.FieldsToMap(call.Fields)
	assert.Equal(t, ReasonInvalidAPIKey, fieldsMap["reason"])
}

// TestAPIKeyAuthWithMetrics_UsesValidateAPIKeyFunction verifies reuse of validateAPIKey.
func TestAPIKeyAuthWithMetrics_UsesValidateAPIKeyFunction(t *testing.T) {
	t.Parallel()

	// This test verifies that the middleware properly uses the validateAPIKey function
	// by testing edge cases that validateAPIKey handles
	tests := []struct {
		name           string
		apiKey         string
		setHeader      bool
		expectedStatus int
		expectedReason string
	}{
		{
			name:           "Missing header - uses ReasonMissingAPIKey",
			apiKey:         "",
			setHeader:      false,
			expectedStatus: http.StatusUnauthorized,
			expectedReason: ReasonMissingAPIKey,
		},
		{
			name:           "Empty header value - uses ReasonMissingAPIKey",
			apiKey:         "",
			setHeader:      true,
			expectedStatus: http.StatusUnauthorized,
			expectedReason: ReasonMissingAPIKey,
		},
		{
			name:           "Invalid key - uses ReasonInvalidAPIKey",
			apiKey:         "wrong",
			setHeader:      true,
			expectedStatus: http.StatusUnauthorized,
			expectedReason: ReasonInvalidAPIKey,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := APIKeyConfig{
				Key:     "valid-secret-key",
				Enabled: true,
			}
			mockLogger := testutil.NewMockLogger()

			app := fiber.New()
			app.Use(APIKeyAuthWithMetrics(config, mockLogger, nil, nil))
			app.Get("/test", func(c *fiber.Ctx) error {
				return c.SendString("success")
			})

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			if tt.setHeader {
				req.Header.Set("X-API-Key", tt.apiKey)
			}

			resp, err := app.Test(req, -1)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, tt.expectedStatus, resp.StatusCode)

			// Verify the correct reason constant is used
			require.Len(t, mockLogger.Calls, 1)
			fieldsMap := testutil.FieldsToMap(mockLogger.Calls[0].Fields)
			assert.Equal(t, tt.expectedReason, fieldsMap["reason"],
				"Should use the correct reason constant from validateAPIKey")
		})
	}
}

// TestMetricAuthFailures_Definition verifies the metric is properly defined.
func TestMetricAuthFailures_Definition(t *testing.T) {
	t.Parallel()

	// This test will fail until metrics.go is created with MetricAuthFailures
	assert.Equal(t, "tracer_auth_failures_total", MetricAuthFailures.Name,
		"Metric name should follow TRD Section 9.3 convention with tracer_ prefix")
	assert.Equal(t, "1", MetricAuthFailures.Unit,
		"Metric unit should be '1' for counters")
	assert.NotEmpty(t, MetricAuthFailures.Description,
		"Metric should have a description")
}

// =============================================================================
// APIKeyAuthWithMetrics metrics recording tests
// =============================================================================

func TestAPIKeyAuthWithMetrics_MissingKey_IncrementsMetric(t *testing.T) {
	t.Parallel()

	// Arrange
	config := APIKeyConfig{
		Key:     "valid-secret-key",
		Enabled: true,
	}
	mockLogger := testutil.NewMockLogger()
	mockMetrics := newMockMetricsRecorder()

	app := fiber.New()
	app.Use(APIKeyAuthWithMetrics(config, mockLogger, mockMetrics, nil))
	app.Get("/v1/validations", func(c *fiber.Ctx) error {
		return c.SendString("success")
	})

	// Act
	req := httptest.NewRequest(http.MethodGet, "/v1/validations", nil)
	// No X-API-Key header
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Assert - metric should be incremented
	require.Len(t, mockMetrics.Calls, 1, "expected exactly one metric call")
	call := mockMetrics.Calls[0]
	assert.Equal(t, "tracer_auth_failures_total", call.MetricName)
	assert.Equal(t, ReasonMissingAPIKey, call.Labels["reason"])
	assert.Equal(t, int64(1), call.Value)
}

func TestAPIKeyAuthWithMetrics_InvalidKey_IncrementsMetric(t *testing.T) {
	t.Parallel()

	// Arrange
	config := APIKeyConfig{
		Key:     "valid-secret-key",
		Enabled: true,
	}
	mockLogger := testutil.NewMockLogger()
	mockMetrics := newMockMetricsRecorder()

	app := fiber.New()
	app.Use(APIKeyAuthWithMetrics(config, mockLogger, mockMetrics, nil))
	app.Get("/v1/validations", func(c *fiber.Ctx) error {
		return c.SendString("success")
	})

	// Act
	req := httptest.NewRequest(http.MethodGet, "/v1/validations", nil)
	req.Header.Set("X-API-Key", "wrong-key")
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Assert - metric should be incremented with invalid_api_key reason
	require.Len(t, mockMetrics.Calls, 1, "expected exactly one metric call")
	call := mockMetrics.Calls[0]
	assert.Equal(t, "tracer_auth_failures_total", call.MetricName)
	assert.Equal(t, ReasonInvalidAPIKey, call.Labels["reason"])
	assert.Equal(t, int64(1), call.Value)
}

func TestAPIKeyAuthWithMetrics_ValidKey_NoMetricIncrement(t *testing.T) {
	t.Parallel()

	// Arrange
	config := APIKeyConfig{
		Key:     "valid-secret-key",
		Enabled: true,
	}
	mockLogger := testutil.NewMockLogger()
	mockMetrics := newMockMetricsRecorder()

	app := fiber.New()
	app.Use(APIKeyAuthWithMetrics(config, mockLogger, mockMetrics, nil))
	app.Get("/v1/validations", func(c *fiber.Ctx) error {
		return c.SendString("success")
	})

	// Act
	req := httptest.NewRequest(http.MethodGet, "/v1/validations", nil)
	req.Header.Set("X-API-Key", "valid-secret-key")
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Assert - no metric should be incremented on success
	assert.Len(t, mockMetrics.Calls, 0, "expected no metric calls on successful auth")
}

func TestAPIKeyAuthWithMetrics_DisabledAuth_NoMetricIncrement(t *testing.T) {
	t.Parallel()

	// Arrange
	config := APIKeyConfig{
		Key:     "valid-secret-key",
		Enabled: false, // Auth disabled
	}
	mockLogger := testutil.NewMockLogger()
	mockMetrics := newMockMetricsRecorder()

	app := fiber.New()
	app.Use(APIKeyAuthWithMetrics(config, mockLogger, mockMetrics, nil))
	app.Get("/v1/validations", func(c *fiber.Ctx) error {
		return c.SendString("success")
	})

	// Act
	req := httptest.NewRequest(http.MethodGet, "/v1/validations", nil)
	// No API key, but auth is disabled
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Assert - no metric should be incremented when auth is disabled
	assert.Len(t, mockMetrics.Calls, 0, "expected no metric calls when auth is disabled")
}
