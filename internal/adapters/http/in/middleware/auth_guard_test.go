// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package middleware

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"tracer/internal/testutil"

	authMiddleware "github.com/LerianStudio/lib-auth/v2/auth/middleware"
	libLog "github.com/LerianStudio/lib-commons/v2/commons/log"
	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestAuthGuard creates an AuthGuard with a real AuthClient.
// When pluginAuthEnabled=true, a fake server address is provided so that
// the AuthClient actually enforces auth (instead of pass-through on empty address).
func newTestAuthGuard(t *testing.T, cfg AuthGuardConfig, fakeAuthServerURL string) *AuthGuard {
	t.Helper()

	mockLogger := testutil.NewMockLogger()

	var logger libLog.Logger = mockLogger

	address := ""
	if cfg.PluginAuthEnabled && fakeAuthServerURL != "" {
		address = fakeAuthServerURL
	}

	authClient := authMiddleware.NewAuthClient(address, cfg.PluginAuthEnabled, &logger)

	return NewAuthGuard(cfg, authClient)
}

// newFakeAuthServer creates a test HTTP server that simulates the auth service.
// Returns 403 to any request, ensuring unauthenticated calls are rejected.
func newFakeAuthServer(t *testing.T) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))

	t.Cleanup(server.Close)

	return server
}

// newTestApp creates a Fiber app with a single GET /test route protected by the given handler.
func newTestApp(authHandler fiber.Handler) *fiber.App {
	app := fiber.New()
	app.Get("/test", authHandler, func(c *fiber.Ctx) error {
		return c.SendString("success")
	})

	return app
}

// isPluginAuthResponse checks if the response body is plain text "Missing Token"
// (returned by lib-auth plugin auth when no Bearer token is present).
func isPluginAuthResponse(t *testing.T, body []byte) bool {
	t.Helper()

	return string(body) == "Missing Token"
}

// isAPIKeyAuthResponse checks if the response body is JSON with code "Unauthenticated"
// (returned by our API key middleware when no X-API-Key header is present).
func isAPIKeyAuthResponse(t *testing.T, body []byte) bool {
	t.Helper()

	var errResp errorResponse

	if err := json.Unmarshal(body, &errResp); err != nil {
		return false
	}

	return errResp.Code == "Unauthenticated"
}

func TestNewAuthGuard(t *testing.T) {
	t.Parallel()

	cfg := AuthGuardConfig{
		APIKey:            "test-key",
		APIKeyEnabled:     true,
		PluginAuthEnabled: false,
		AppName:           "tracer",
	}

	guard := newTestAuthGuard(t, cfg, "")

	assert.NotNil(t, guard)
	assert.NotNil(t, guard.apiKeyAuth)
	assert.NotNil(t, guard.authClient)
	assert.Equal(t, cfg, guard.cfg)
}

func TestNewAuthGuard_ReturnsNilWhenPluginEnabledAndClientNil(t *testing.T) {
	t.Parallel()

	cfg := AuthGuardConfig{
		APIKey:            "test-key",
		APIKeyEnabled:     true,
		PluginAuthEnabled: true,
		AppName:           "tracer",
	}

	guard := NewAuthGuard(cfg, nil)

	assert.Nil(t, guard, "Expected nil guard when PluginAuthEnabled=true and authClient is nil")
}

func TestAuthGuard_Protect(t *testing.T) {
	t.Parallel()

	fakeServer := newFakeAuthServer(t)

	tests := []struct {
		name           string
		cfg            AuthGuardConfig
		apiKey         string
		setHeader      bool
		expectedStatus int
		expectedBody   string
	}{
		{
			name: "API key mode - valid key returns 200",
			cfg: AuthGuardConfig{
				APIKey:            "valid-key",
				APIKeyEnabled:     true,
				PluginAuthEnabled: false,
				AppName:           "tracer",
			},
			apiKey:         "valid-key",
			setHeader:      true,
			expectedStatus: http.StatusOK,
			expectedBody:   "success",
		},
		{
			name: "API key mode - missing key returns 401",
			cfg: AuthGuardConfig{
				APIKey:            "valid-key",
				APIKeyEnabled:     true,
				PluginAuthEnabled: false,
				AppName:           "tracer",
			},
			setHeader:      false,
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name: "API key mode - invalid key returns 401",
			cfg: AuthGuardConfig{
				APIKey:            "valid-key",
				APIKeyEnabled:     true,
				PluginAuthEnabled: false,
				AppName:           "tracer",
			},
			apiKey:         "wrong-key",
			setHeader:      true,
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name: "Plugin auth mode - missing Bearer token returns 401",
			cfg: AuthGuardConfig{
				APIKey:            "valid-key",
				APIKeyEnabled:     true,
				PluginAuthEnabled: true,
				AppName:           "tracer",
			},
			setHeader:      false,
			expectedStatus: http.StatusUnauthorized,
			expectedBody:   "Missing Token",
		},
		{
			name: "All auth disabled - passes through without key",
			cfg: AuthGuardConfig{
				APIKey:            "valid-key",
				APIKeyEnabled:     false,
				PluginAuthEnabled: false,
				AppName:           "tracer",
			},
			setHeader:      false,
			expectedStatus: http.StatusOK,
			expectedBody:   "success",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			serverURL := ""
			if tt.cfg.PluginAuthEnabled {
				serverURL = fakeServer.URL
			}

			guard := newTestAuthGuard(t, tt.cfg, serverURL)
			app := newTestApp(guard.Protect("rules", "get"))

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			if tt.setHeader {
				req.Header.Set(HeaderAPIKey, tt.apiKey)
			}

			resp, err := app.Test(req, -1)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, tt.expectedStatus, resp.StatusCode)

			if tt.expectedBody != "" {
				body, err := io.ReadAll(resp.Body)
				require.NoError(t, err)
				assert.Contains(t, string(body), tt.expectedBody)
			}
		})
	}
}

func TestAuthGuard_With(t *testing.T) {
	t.Parallel()

	fakeServer := newFakeAuthServer(t)

	tests := []struct {
		name           string
		cfg            AuthGuardConfig
		apiKeyParam    bool
		apiKey         string
		setHeader      bool
		expectedStatus int
		expectedBody   string
	}{
		{
			name: "apiKey=false, plugin disabled - valid API key returns 200",
			cfg: AuthGuardConfig{
				APIKey:            "valid-key",
				APIKeyEnabled:     true,
				PluginAuthEnabled: false,
				AppName:           "tracer",
			},
			apiKeyParam:    false,
			apiKey:         "valid-key",
			setHeader:      true,
			expectedStatus: http.StatusOK,
			expectedBody:   "success",
		},
		{
			name: "apiKey=false, plugin disabled - missing key returns 401",
			cfg: AuthGuardConfig{
				APIKey:            "valid-key",
				APIKeyEnabled:     true,
				PluginAuthEnabled: false,
				AppName:           "tracer",
			},
			apiKeyParam:    false,
			setHeader:      false,
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name: "apiKey=false, plugin enabled - uses plugin auth (Missing Token)",
			cfg: AuthGuardConfig{
				APIKey:            "valid-key",
				APIKeyEnabled:     true,
				PluginAuthEnabled: true,
				AppName:           "tracer",
			},
			apiKeyParam:    false,
			setHeader:      false,
			expectedStatus: http.StatusUnauthorized,
			expectedBody:   "Missing Token",
		},
		{
			name: "apiKey=true, plugin enabled - bypasses plugin auth, valid API key returns 200",
			cfg: AuthGuardConfig{
				APIKey:            "valid-key",
				APIKeyEnabled:     true,
				PluginAuthEnabled: true,
				AppName:           "tracer",
			},
			apiKeyParam:    true,
			apiKey:         "valid-key",
			setHeader:      true,
			expectedStatus: http.StatusOK,
			expectedBody:   "success",
		},
		{
			name: "apiKey=true, plugin enabled - bypasses plugin auth, missing key returns API key 401",
			cfg: AuthGuardConfig{
				APIKey:            "valid-key",
				APIKeyEnabled:     true,
				PluginAuthEnabled: true,
				AppName:           "tracer",
			},
			apiKeyParam:    true,
			setHeader:      false,
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name: "apiKey=true, all auth disabled - passes through",
			cfg: AuthGuardConfig{
				APIKey:            "valid-key",
				APIKeyEnabled:     false,
				PluginAuthEnabled: false,
				AppName:           "tracer",
			},
			apiKeyParam:    true,
			setHeader:      false,
			expectedStatus: http.StatusOK,
			expectedBody:   "success",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			serverURL := ""
			if tt.cfg.PluginAuthEnabled {
				serverURL = fakeServer.URL
			}

			guard := newTestAuthGuard(t, tt.cfg, serverURL)
			app := newTestApp(guard.With("validations", "post", tt.apiKeyParam))

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			if tt.setHeader {
				req.Header.Set(HeaderAPIKey, tt.apiKey)
			}

			resp, err := app.Test(req, -1)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, tt.expectedStatus, resp.StatusCode)

			if tt.expectedBody != "" {
				body, err := io.ReadAll(resp.Body)
				require.NoError(t, err)
				assert.Contains(t, string(body), tt.expectedBody)
			}
		})
	}
}

// TestAuthGuard_PluginAuthPriority verifies that plugin auth takes priority over API key.
// When PluginAuthEnabled=true, Protect() should return plugin auth middleware.
// Sending only an API key (no Bearer token) should trigger plugin auth's "Missing Token",
// proving that plugin auth was chosen instead of API key auth.
func TestAuthGuard_PluginAuthPriority(t *testing.T) {
	t.Parallel()

	fakeServer := newFakeAuthServer(t)

	guard := newTestAuthGuard(t, AuthGuardConfig{
		APIKey:            "valid-key",
		APIKeyEnabled:     true,
		PluginAuthEnabled: true,
		AppName:           "tracer",
	}, fakeServer.URL)

	app := newTestApp(guard.Protect("rules", "get"))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set(HeaderAPIKey, "valid-key") // Valid API key, but no Bearer token

	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	// Plugin auth should be enforced: "Missing Token" instead of API key auth passing
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	assert.True(t, isPluginAuthResponse(t, body),
		"Expected plugin auth response 'Missing Token', got: %s", string(body))
}

// TestAuthGuard_DualMode verifies the dual auth mode behavior:
// When PluginAuthEnabled=true, apiKey=true should use API key only,
// while apiKey=false should use plugin auth.
func TestAuthGuard_DualMode(t *testing.T) {
	t.Parallel()

	fakeServer := newFakeAuthServer(t)

	guard := newTestAuthGuard(t, AuthGuardConfig{
		APIKey:            "valid-key",
		APIKeyEnabled:     true,
		PluginAuthEnabled: true,
		AppName:           "tracer",
	}, fakeServer.URL)

	t.Run("validation endpoint with apiKey=true uses API key auth", func(t *testing.T) {
		t.Parallel()

		app := newTestApp(guard.With("validations", "post", true))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set(HeaderAPIKey, "valid-key")

		resp, err := app.Test(req, -1)
		require.NoError(t, err)
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		assert.Equal(t, http.StatusOK, resp.StatusCode,
			"With apiKey=true + valid API key, should return 200")
		assert.Equal(t, "success", string(body))
	})

	t.Run("validation endpoint with apiKey=true rejects missing key", func(t *testing.T) {
		t.Parallel()

		app := newTestApp(guard.With("validations", "post", true))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		// No API key and no Bearer token

		resp, err := app.Test(req, -1)
		require.NoError(t, err)
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		// Should get API key 401 (not plugin auth's "Missing Token")
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
		assert.True(t, isAPIKeyAuthResponse(t, body),
			"Expected API key auth JSON response, got: %s", string(body))
	})

	t.Run("other endpoint with apiKey=false uses plugin auth", func(t *testing.T) {
		t.Parallel()

		app := newTestApp(guard.With("rules", "get", false))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set(HeaderAPIKey, "valid-key") // Valid API key, but no Bearer token

		resp, err := app.Test(req, -1)
		require.NoError(t, err)
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		// Should get plugin auth response, NOT API key auth passing
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
		assert.True(t, isPluginAuthResponse(t, body),
			"Expected plugin auth response 'Missing Token', got: %s", string(body))
	})
}
