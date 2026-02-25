// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package middleware

import (
	authMiddleware "github.com/LerianStudio/lib-auth/v2/auth/middleware"
	"github.com/gofiber/fiber/v2"
)

// AuthGuardConfig holds all configuration for the auth guard.
type AuthGuardConfig struct {
	APIKey            string
	APIKeyEnabled     bool
	PluginAuthEnabled bool
	AppName           string
}

// AuthGuard manages authentication middleware based on configuration flags.
//
// Auth priority: Plugin Auth > API Key.
//   - If PluginAuthEnabled, endpoints use plugin auth
//   - Otherwise, endpoints fall back to API key auth
type AuthGuard struct {
	apiKeyAuth fiber.Handler
	authClient *authMiddleware.AuthClient
	cfg        AuthGuardConfig
}

// NewAuthGuard creates a new AuthGuard with the given configuration.
// Returns nil if authClient is nil when PluginAuthEnabled is true, since
// Protect() would dereference it to call Authorize(). Callers must check
// for nil return and handle accordingly.
func NewAuthGuard(cfg AuthGuardConfig, authClient *authMiddleware.AuthClient) *AuthGuard {
	if cfg.PluginAuthEnabled && authClient == nil {
		return nil
	}

	return &AuthGuard{
		apiKeyAuth: APIKeyAuth(APIKeyConfig{
			Key:     cfg.APIKey,
			Enabled: cfg.APIKeyEnabled,
		}),
		authClient: authClient,
		cfg:        cfg,
	}
}

// Protect returns auth middleware with plugin auth priority.
// Returns pluginAuth if enabled, otherwise apiKeyAuth.
func (g *AuthGuard) Protect(resource, method string) fiber.Handler {
	if g.cfg.PluginAuthEnabled {
		return g.authClient.Authorize(g.cfg.AppName, resource, method)
	}

	return g.apiKeyAuth
}

// With returns the appropriate auth middleware for a route.
// When forceAPIKeyAuth is true, returns API key auth directly (bypassing plugin auth).
// Otherwise, returns Protect (plugin auth if enabled, else API key).
//
//	api.Post("/rules", guard.With("rules", "post", false), handler)
//	api.Post("/validations", guard.With("validations", "post", cfg.APIKeyOnlyValidation), handler)
func (g *AuthGuard) With(resource, method string, forceAPIKeyAuth bool) fiber.Handler {
	if forceAPIKeyAuth {
		return g.apiKeyAuth
	}

	return g.Protect(resource, method)
}
