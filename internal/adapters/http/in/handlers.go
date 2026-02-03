// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package in

import (
	libHTTP "github.com/LerianStudio/lib-commons/v2/commons/net/http"
	"github.com/gofiber/fiber/v2"
)

// Health godoc
//
//	@Summary		Health check (liveness probe)
//	@Description	Check if the service is alive. Returns plain text "healthy".
//	@ID				getHealth
//	@Tags			health
//	@Accept			plain
//	@Produce		plain
//	@Success		200	{string}	string	"healthy"
//	@Router			/health [get]
func Health(c *fiber.Ctx) error {
	return libHTTP.Ping(c)
}

// Version godoc
//
//	@Summary		Get service version
//	@Description	Returns the current version of the service
//	@ID				getVersion
//	@Tags			info
//	@Accept			json
//	@Produce		json
//	@Success		200	{object}	api.VersionResponse	"Version information"
//	@Router			/version [get]
func Version(c *fiber.Ctx) error {
	return libHTTP.Version(c)
}
