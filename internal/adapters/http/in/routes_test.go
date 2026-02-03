// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package in

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	libOtel "github.com/LerianStudio/lib-commons/v2/commons/opentelemetry"
	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"tracer/internal/adapters/http/in/mocks"
	"tracer/internal/testutil"
	"tracer/pkg/model"
)

func TestMain(m *testing.M) {
	// Skip telemetry middleware that causes data races in lib-commons ContextWithLogger.
	// The race occurs when multiple goroutines call it concurrently (as happens in Fiber's app.Test).
	os.Setenv("SKIP_LIB_COMMONS_TELEMETRY", "true")
	os.Exit(m.Run())
}

// errorResponse represents the standard error response format from libHTTP.
type errorResponse struct {
	Code   string `json:"code"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

// testRouterDeps holds dependencies for creating test routers.
// Extracted to allow tests to configure mock expectations before router creation.
type testRouterDeps struct {
	RuleService                  *MockRuleService
	LimitService                 *MockLimitService
	ValidationService            *mocks.MockValidationService
	TransactionValidationService *mocks.MockTransactionValidationService
	AuditEventService            *MockAuditEventService
	cfg                          *RouteConfig
	t                            *testing.T
}

// newTestRouterDeps creates test dependencies without any mock expectations.
// Tests should configure expectations on the returned mocks before calling build().
func newTestRouterDeps(t *testing.T, cfg *RouteConfig) *testRouterDeps {
	ctrl := gomock.NewController(t)

	return &testRouterDeps{
		RuleService:                  NewMockRuleService(ctrl),
		LimitService:                 NewMockLimitService(ctrl),
		ValidationService:            mocks.NewMockValidationService(ctrl),
		TransactionValidationService: mocks.NewMockTransactionValidationService(ctrl),
		AuditEventService:            NewMockAuditEventService(ctrl),
		cfg:                          cfg,
		t:                            t,
	}
}

// build creates the Fiber app with the configured dependencies.
func (d *testRouterDeps) build() *fiber.App {
	logger := testutil.NewMockLogger()
	telemetry := &libOtel.Telemetry{
		TelemetryConfig: libOtel.TelemetryConfig{
			ServiceName:     "tracer-test",
			EnableTelemetry: false,
			Logger:          logger,
		},
	}

	return NewRoutes(logger, telemetry, &HealthChecker{}, d.cfg, d.RuleService, d.LimitService, d.ValidationService, d.TransactionValidationService, d.AuditEventService)
}

// createTestRouter creates a test router with the given RouteConfig.
// For auth/route protection tests that don't exercise service handlers.
// No mock expectations are set - requests that reach handlers will fail with gomock errors,
// which helps catch accidental handler invocations during refactors.
func createTestRouter(t *testing.T, cfg *RouteConfig) *fiber.App {
	deps := newTestRouterDeps(t, cfg)
	return deps.build()
}

func TestRoutes_PublicEndpoints_NoAuthRequired(t *testing.T) {
	// Note: SKIP_LIB_COMMONS_TELEMETRY=true is set in TestMain to skip telemetry middleware that causes data races.
	cfg := &RouteConfig{
		APIKey:        "test-secret-key-32-characters-long",
		APIKeyEnabled: true,
	}
	app := createTestRouter(t, cfg)

	testCases := []struct {
		name             string
		path             string
		acceptableStatus []int
	}{
		{"health", "/health", []int{http.StatusOK}},
		{"ready", "/ready", []int{http.StatusOK, http.StatusServiceUnavailable}},
		{"version", "/version", []int{http.StatusOK}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			// Note: NO X-API-Key header

			resp, err := app.Test(req, -1)
			require.NoError(t, err)

			// Public endpoints should NOT require authentication (should NOT return 401)
			assert.NotEqual(t, http.StatusUnauthorized, resp.StatusCode,
				"Public endpoint %s should NOT return 401 Unauthorized", tc.path)

			// Verify status is one of the acceptable statuses
			assert.Contains(t, tc.acceptableStatus, resp.StatusCode,
				"Public endpoint %s returned unexpected status %d", tc.path, resp.StatusCode)

			require.NoError(t, resp.Body.Close())
		})
	}
}

func TestRoutes_ProtectedEndpoints_RequireAuth(t *testing.T) {
	tests := []struct {
		name           string
		method         string
		path           string
		expectedStatus int
		expectedCode   string
	}{
		{
			name:           "GET /v1/test returns 401 without API key",
			method:         http.MethodGet,
			path:           "/v1/test",
			expectedStatus: http.StatusUnauthorized,
			expectedCode:   "Unauthenticated",
		},
		{
			name:           "POST /v1/validations returns 401 without API key",
			method:         http.MethodPost,
			path:           "/v1/validations",
			expectedStatus: http.StatusUnauthorized,
			expectedCode:   "Unauthenticated",
		},
		{
			name:           "GET /v1/rules returns 401 without API key",
			method:         http.MethodGet,
			path:           "/v1/rules",
			expectedStatus: http.StatusUnauthorized,
			expectedCode:   "Unauthenticated",
		},
		{
			name:           "GET /v1/limits returns 401 without API key",
			method:         http.MethodGet,
			path:           "/v1/limits",
			expectedStatus: http.StatusUnauthorized,
			expectedCode:   "Unauthenticated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create router per subtest for proper isolation
			cfg := &RouteConfig{
				APIKey:        "test-secret-key-32-characters-long",
				APIKeyEnabled: true,
			}
			app := createTestRouter(t, cfg)

			req := httptest.NewRequest(tt.method, tt.path, nil)
			// Note: NO X-API-Key header

			resp, err := app.Test(req, -1)
			require.NoError(t, err)
			defer resp.Body.Close()

			// Protected endpoints should require authentication
			assert.Equal(t, tt.expectedStatus, resp.StatusCode,
				"Protected endpoint %s should require API key", tt.path)

			if tt.expectedStatus == http.StatusUnauthorized {
				body, err := io.ReadAll(resp.Body)
				require.NoError(t, err)

				var errResp errorResponse
				err = json.Unmarshal(body, &errResp)
				require.NoError(t, err, "Expected JSON error response, got: %s", string(body))
				assert.Equal(t, tt.expectedCode, errResp.Code,
					"Error code should be 'Unauthenticated'")
			}
		})
	}
}

func TestRoutes_ProtectedEndpoints_ValidKey(t *testing.T) {
	validAPIKey := "test-secret-key-32-characters-long"

	tests := []struct {
		name      string
		method    string
		path      string
		needsMock string // "rules", "limits", or "" for no mock needed
	}{
		{
			name:      "GET /v1/test accessible with valid API key",
			method:    http.MethodGet,
			path:      "/v1/test",
			needsMock: "",
		},
		{
			name:      "POST /v1/validations accessible with valid API key",
			method:    http.MethodPost,
			path:      "/v1/validations",
			needsMock: "",
		},
		{
			name:      "GET /v1/rules accessible with valid API key",
			method:    http.MethodGet,
			path:      "/v1/rules",
			needsMock: "rules",
		},
		{
			name:      "GET /v1/limits accessible with valid API key",
			method:    http.MethodGet,
			path:      "/v1/limits",
			needsMock: "limits",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &RouteConfig{
				APIKey:        validAPIKey,
				APIKeyEnabled: true,
			}
			deps := newTestRouterDeps(t, cfg)

			// Set expectations only for endpoints that actually hit handlers
			switch tt.needsMock {
			case "rules":
				deps.RuleService.EXPECT().ListRules(gomock.Any(), gomock.Any()).
					Return(&model.ListRulesResult{Rules: []model.Rule{}}, nil).Times(1)
			case "limits":
				deps.LimitService.EXPECT().ListLimits(gomock.Any(), gomock.Any()).
					Return(&model.ListLimitsResult{Limits: []model.Limit{}}, nil).Times(1)
			}

			app := deps.build()

			req := httptest.NewRequest(tt.method, tt.path, nil)
			req.Header.Set("X-API-Key", validAPIKey)

			resp, err := app.Test(req, -1)
			require.NoError(t, err)
			defer resp.Body.Close()

			// With valid API key, should NOT get 401 Unauthorized
			// The request might get 404 if no handler exists, but NOT 401
			assert.NotEqual(t, http.StatusUnauthorized, resp.StatusCode,
				"Protected endpoint %s with valid API key should not return 401", tt.path)
		})
	}
}

func TestRoutes_ProtectedEndpoints_AuthDisabled(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
	}{
		{
			name:   "GET /v1/test accessible when auth disabled",
			method: http.MethodGet,
			path:   "/v1/test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create router per subtest for proper isolation
			cfg := &RouteConfig{
				APIKey:        "some-key",
				APIKeyEnabled: false, // Auth disabled
			}
			app := createTestRouter(t, cfg)

			req := httptest.NewRequest(tt.method, tt.path, nil)
			// Note: NO X-API-Key header

			resp, err := app.Test(req, -1)
			require.NoError(t, err)
			defer resp.Body.Close()

			// With auth disabled, should NOT get 401 Unauthorized
			// Might get 404, but not 401
			assert.NotEqual(t, http.StatusUnauthorized, resp.StatusCode,
				"With auth disabled, endpoint %s should not return 401", tt.path)
		})
	}
}

func TestGetCORSAllowedOrigins(t *testing.T) {
	tests := []struct {
		name       string
		configured string
		expected   string
	}{
		{
			name:       "Success - empty returns default (restrictive)",
			configured: "",
			expected:   "",
		},
		{
			name:       "Success - wildcard for development",
			configured: "*",
			expected:   "*",
		},
		{
			name:       "Success - single origin for production",
			configured: "https://app.example.com",
			expected:   "https://app.example.com",
		},
		{
			name:       "Success - multiple origins for production",
			configured: "https://app.example.com,https://admin.example.com",
			expected:   "https://app.example.com,https://admin.example.com",
		},
		// Edge cases - passed through as-is (CORS middleware handles validation)
		{
			name:       "Edge case - whitespace in origins passed through",
			configured: "https://app.example.com, https://admin.example.com",
			expected:   "https://app.example.com, https://admin.example.com",
		},
		{
			name:       "Edge case - trailing comma passed through",
			configured: "https://app.example.com,",
			expected:   "https://app.example.com,",
		},
		{
			name:       "Edge case - whitespace only passed through",
			configured: "   ",
			expected:   "   ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getCORSAllowedOrigins(tt.configured)
			assert.Equal(t, tt.expected, result)
		})
	}
}
