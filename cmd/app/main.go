// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package main

import (
	"fmt"

	"tracer/internal/bootstrap"
	"tracer/pkg"
)

// @title						Tracer API
// @version						0.1.0
// @description					Transaction validation service with rules and limits
// @termsOfService				http://swagger.io/terms/
// @host						localhost:8080
// @schemes						http https
// @BasePath					/
// @securityDefinitions.apikey	ApiKeyAuth
// @in							header
// @name						X-API-Key
// @description					API Key for authentication
func main() {
	pkg.InitLocalEnvConfig()

	service, err := bootstrap.InitServers()
	if err != nil {
		// Use panic in bootstrap only - allows deferred functions to run
		// and is caught by recovery middleware if configured
		panic(fmt.Errorf("failed to initialize servers: %w", err))
	}

	service.Run()
}

