# Tracer Makefile

# Component-specific variables
SERVICE_NAME := tracer
BIN_DIR := ./.bin
ARTIFACTS_DIR := ./artifacts
POSTGRES_SERVICE ?= tracer-postgres

# Ensure artifacts directory exists
$(shell mkdir -p $(ARTIFACTS_DIR))

# Define the root directory of the project
ROOT_DIR := $(shell pwd)
DOCKER_CMD := $(shell \
	if [ "$(shell printf '%s\n' "$(DOCKER_MIN_VERSION)" "$(DOCKER_VERSION)" | sort -V | head -n1)" = "$(DOCKER_MIN_VERSION)" ]; then \
		echo "docker compose"; \
	else \
		echo "docker-compose"; \
	fi \
)

# Include shared color definitions and utility functions
include $(ROOT_DIR)/pkg/shell/makefile_colors.mk
include $(ROOT_DIR)/pkg/shell/makefile_utils.mk

#-------------------------------------------------------
# Core Commands
#-------------------------------------------------------

.PHONY: help
help:
	@echo ""
	@echo "$(BOLD)Plugin Service Commands$(NC)"
	@echo ""
	@echo "$(BOLD)Core Commands:$(NC)"
	@echo "  make help                        - Display this help message"
	@echo "  make build                       - Build the binary to .bin/tracer"
	@echo "  make test                        - Run all tests"
	@echo "  make test-unit                   - Run unit tests only"
	@echo "  make test-integration            - Run integration tests (requires running service)"
	@echo "  make clean                       - Clean build artifacts"
	@echo "  make run                         - Run the application with .env config"
	@echo "  make cover                       - Run tests with coverage summary"
	@echo "  make cover-html                  - Generate HTML test coverage report"
	@echo ""
	@echo "$(BOLD)Code Quality Commands:$(NC)"
	@echo "  make lint                        - Run linting tools"
	@echo "  make format                      - Format code with go fmt"
	@echo "  make generate                    - Generate code (mocks, etc.)"
	@echo "  make tidy                        - Update and clean dependencies"
	@echo ""
	@echo "$(BOLD)Docker Commands:$(NC)"
	@echo "  make up                          - Start services with Docker Compose"
	@echo "  make down                        - Stop services with Docker Compose"
	@echo "  make start                       - Start existing containers"
	@echo "  make stop                        - Stop running containers"
	@echo "  make restart                     - Restart all containers"
	@echo "  make logs                        - Show logs for all services"
	@echo "  make logs-api                    - Show logs for plugin service"
	@echo "  make ps                          - List container status"
	@echo "  make rebuild-up                  - Rebuild and restart services during development"
	@echo ""
	@echo "$(BOLD)Database Commands:$(NC)"
	@echo "  make migrate                     - Apply all pending migrations"
	@echo "  make migrate-down                - Rollback last migration"
	@echo "  make migrate-down-all            - Rollback all migrations (5s confirmation, FORCE=1 to skip)"
	@echo "  make migrate-version             - Show current migration version"
	@echo "  make migrate-force VERSION=N     - Force set migration version (use with caution)"
	@echo "  make seed                        - Load development seed data"
	@echo "  make seed-down                   - Remove development seed data"
	@echo ""
	@echo "$(BOLD)Plugin-Specific Commands:$(NC)"
	@echo "  make generate-docs               - Generate Swagger API documentation"
	@echo "  make verify-api-docs             - Verify API documentation coverage"
	@echo "  make validate-api-docs           - Validate API documentation"
	@echo ""
	@echo "$(BOLD)Developer Helper Commands:$(NC)"
	@echo "  make dev-setup                   - Set up development environment"
	@echo ""

#-------------------------------------------------------
# Git Hook Commands
#-------------------------------------------------------

.PHONY: setup-git-hooks
setup-git-hooks:
	$(call title1,"Installing and configuring git hooks")
	@sh ./scripts/setup-git-hooks.sh
	@echo "$(GREEN)$(BOLD)[ok]$(NC) Git hooks installed successfully$(GREEN) ✔️$(NC)"


.PHONY: check-hooks
check-hooks:
	$(call title1,"Verifying git hooks installation status")
	@err=0; \
	for hook_dir in .githooks/*; do \
		hook_name=$$(basename $$hook_dir); \
		if [ ! -f ".git/hooks/$$hook_name" ]; then \
			echo "$(RED)Git hook $$hook_name is not installed$(NC)"; \
			err=1; \
		else \
			echo "$(GREEN)Git hook $$hook_name is installed$(NC)"; \
		fi; \
	done; \
	if [ $$err -eq 0 ]; then \
		echo "$(GREEN)$(BOLD)[ok]$(NC) All git hooks are properly installed$(GREEN) ✔️$(NC)"; \
	else \
		echo "$(RED)$(BOLD)[error]$(NC) Some git hooks are missing. Run 'make setup-git-hooks' to fix.$(RED) ❌$(NC)"; \
		exit 1; \
	fi

.PHONY: check-envs
check-envs:
	$(call title1,"Checking if github hooks are installed and secret env files are not exposed")
	@sh ./scripts/check-envs.sh
	@echo "$(GREEN)$(BOLD)[ok]$(NC) Environment check completed$(GREEN) ✔️$(NC)"

#-------------------------------------------------------
# Setup Commands
#-------------------------------------------------------

.PHONY: set-env
set-env:
	$(call title1,"Setting up environment files")

	@if [ -f ".env.example" ] && [ ! -f ".env" ]; then \
		echo "$(CYAN)Creating .env in plugin from .env.example$(NC)"; \
		cp ".env.example" ".env"; \
	elif [ ! -f ".env.example" ]; then \
		echo "$(YELLOW)Warning: No .env.example found in plugin$(NC)"; \
	else \
		echo "$(GREEN).env already exists in plugin$(NC)"; \
	fi

	@echo "$(GREEN)$(BOLD)[ok]$(NC) Environment files set up successfully$(GREEN) ✔️$(NC)"

#-------------------------------------------------------
# Build Commands
#-------------------------------------------------------

.PHONY: build
build:
	$(call title1,"Building component")
	@mkdir -p $(BIN_DIR)
	@CGO_ENABLED=0 go build -ldflags="-s -w" -o $(BIN_DIR)/$(SERVICE_NAME) ./cmd/app
	@echo "$(GREEN)$(BOLD)[ok]$(NC) Build completed successfully - binary at $(BIN_DIR)/$(SERVICE_NAME)$(GREEN) ✔️$(NC)"

#-------------------------------------------------------
# Test Commands
#-------------------------------------------------------

.PHONY: test
test:
	$(call title1,"Running tests")
	@go test -v ./...
	@echo "$(GREEN)$(BOLD)[ok]$(NC) Tests completed successfully$(GREEN) ✔️$(NC)"

.PHONY: test-unit
test-unit:
	$(call title1,"Running unit tests")
	@go test -v ./... -count=1
	@echo "$(GREEN)$(BOLD)[ok]$(NC) Unit tests completed successfully$(GREEN) ✔️$(NC)"

.PHONY: test-integration
test-integration:
	$(call title1,"Running integration tests")
	@echo "$(CYAN)Note: Tests use testcontainers (auto-starts PostgreSQL and app server)$(NC)"
	@echo "$(CYAN)      Set DISABLE_TESTCONTAINERS=true to use external server instead$(NC)"
	@go test -tags=integration -v ./tests/integration/...
	@echo "$(GREEN)$(BOLD)[ok]$(NC) Integration tests completed successfully$(GREEN) ✔️$(NC)"

.PHONY: cover
cover:
	$(call title1,"Running tests with coverage")
	@PACKAGES=$$(go list ./... | grep -v -f ./scripts/coverage_ignore.txt); \
	go test -coverprofile=$(ARTIFACTS_DIR)/coverage.out $$PACKAGES
	@echo ""
	@echo "$(CYAN)Coverage Summary:$(NC)"
	@echo "$(CYAN)----------------------------------------$(NC)"
	@go tool cover -func=$(ARTIFACTS_DIR)/coverage.out | grep total | awk '{print "Total coverage: " $$3}'
	@echo "$(CYAN)----------------------------------------$(NC)"
	@echo "$(GREEN)$(BOLD)[ok]$(NC) Coverage report generated at $(ARTIFACTS_DIR)/coverage.out$(GREEN) ✔️$(NC)"

.PHONY: cover-html
cover-html:
	$(call title1,"Generating HTML test coverage report")
	@PACKAGES=$$(go list ./... | grep -v -f ./scripts/coverage_ignore.txt); \
	go test -coverprofile=$(ARTIFACTS_DIR)/coverage.out $$PACKAGES
	@go tool cover -html=$(ARTIFACTS_DIR)/coverage.out -o $(ARTIFACTS_DIR)/coverage.html
	@echo "$(GREEN)Coverage report generated at $(ARTIFACTS_DIR)/coverage.html$(NC)"
	@echo ""
	@echo "$(CYAN)Coverage Summary:$(NC)"
	@echo "$(CYAN)----------------------------------------$(NC)"
	@go tool cover -func=$(ARTIFACTS_DIR)/coverage.out | grep total | awk '{print "Total coverage: " $$3}'
	@echo "$(CYAN)----------------------------------------$(NC)"
	@echo "$(YELLOW)Open $(ARTIFACTS_DIR)/coverage.html in your browser to view detailed coverage report$(NC)"

#-------------------------------------------------------
# Test Coverage Commands
#-------------------------------------------------------

.PHONY: check-tests
check-tests:
	$(call title1,"Verifying test coverage")
	@if find . -name "*.go" -type f | grep -q .; then \
		echo "$(CYAN)Running test coverage check...$(NC)"; \
		go test -coverprofile=coverage.tmp ./... > /dev/null 2>&1; \
		if [ -f coverage.tmp ]; then \
			coverage=$$(go tool cover -func=coverage.tmp | grep total | awk '{print $$3}'); \
			echo "$(CYAN)Test coverage: $(GREEN)$$coverage$(NC)"; \
			rm coverage.tmp; \
		else \
			echo "$(YELLOW)No coverage data generated$(NC)"; \
		fi; \
	else \
		echo "$(YELLOW)No Go files found, skipping test coverage check$(NC)"; \
	fi

#-------------------------------------------------------
# Code Quality Commands
#-------------------------------------------------------

.PHONY: lint
lint:
	$(call title1,"Running linters")
	@if find . -name "*.go" -type f | grep -q .; then \
		if ! command -v golangci-lint >/dev/null 2>&1; then \
			echo "$(YELLOW)Installing golangci-lint v2...$(NC)"; \
			go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.8.0; \
		fi; \
		golangci-lint run --fix ./... --verbose; \
		echo "$(GREEN)$(BOLD)[ok]$(NC) Linting completed successfully$(GREEN) ✔️$(NC)"; \
	else \
		echo "$(YELLOW)No Go files found, skipping linting$(NC)"; \
	fi

.PHONY: quality
quality: lint test
	$(call title1,"Quality checks complete")
	@echo "$(GREEN)$(BOLD)[ok]$(NC) All quality checks passed$(GREEN) ✔️$(NC)"
	@echo ""
	@echo "Checks passed:"
	@echo "  ✅ Linting (errorlint, contextcheck)"
	@echo "  ✅ Unit tests"
	@echo ""
	@echo "$(GREEN)Ready to commit and push!$(NC)"

.PHONY: format
format:
	$(call title1,"Formatting code")
	@go fmt ./...
	@echo "$(GREEN)$(BOLD)[ok]$(NC) Formatting completed successfully$(GREEN) ✔️$(NC)"

.PHONY: generate
generate:
	$(call title1,"Generating code (mocks, etc.)")
	@if ! command -v mockgen >/dev/null 2>&1; then \
		echo "$(YELLOW)Installing mockgen...$(NC)"; \
		go install go.uber.org/mock/mockgen@v0.5.2; \
	fi
	@go generate ./...
	@echo "$(GREEN)$(BOLD)[ok]$(NC) Code generation completed successfully$(GREEN) ✔️$(NC)"

.PHONY: tidy
tidy:
	$(call title1,"Update and Cleaning dependencies")
	@go get -u ./...
	@go mod tidy
	@echo "$(GREEN)$(BOLD)[ok]$(NC) Dependencies updated and cleaned successfully$(GREEN) ✔️$(NC)"

#-------------------------------------------------------
# Security Commands
#-------------------------------------------------------

.PHONY: sec
sec:
	$(call title1,"Running security checks using gosec")
	@if ! command -v gosec >/dev/null 2>&1; then \
		echo "$(YELLOW)Installing gosec...$(NC)"; \
		go install github.com/securego/gosec/v2/cmd/gosec@latest; \
	fi
	@if find . -name "*.go" -type f | grep -q .; then \
		echo "$(CYAN)Running security checks...$(NC)"; \
		gosec -quiet ./...; \
		echo "$(GREEN)$(BOLD)[ok]$(NC) Security checks completed$(GREEN) ✔️$(NC)"; \
	else \
		echo "$(YELLOW)No Go files found, skipping security checks$(NC)"; \
	fi

#-------------------------------------------------------
# Clean Commands
#-------------------------------------------------------

.PHONY: clean
clean:
	$(call title1,"Cleaning build artifacts")
	@rm -rf $(BIN_DIR)/* $(ARTIFACTS_DIR)/*
	@echo "$(GREEN)$(BOLD)[ok]$(NC) Artifacts cleaned successfully$(GREEN) ✔️$(NC)"

#-------------------------------------------------------
# Docker Commands
#-------------------------------------------------------

.PHONY: run
run:
	$(call title1,"Running the application with .env config")
	@go run cmd/app/main.go .env
	@echo "$(GREEN)$(BOLD)[ok]$(NC) Application started successfully$(GREEN) ✔️$(NC)"

.PHONY: build-docker
build-docker:
	$(call title1,"Building Docker images")
	@$(DOCKER_CMD) -f docker-compose.yml build $(c)
	@echo "$(GREEN)$(BOLD)[ok]$(NC) Docker images built successfully$(GREEN) ✔️$(NC)"

.PHONY: up
up:
	$(call title1,"Starting all services in detached mode")
	@$(DOCKER_CMD) -f docker-compose.yml up $(c) -d
	@echo "$(GREEN)$(BOLD)[ok]$(NC) Services started successfully$(GREEN) ✔️$(NC)"

.PHONY: start
start:
	$(call title1,"Starting existing containers")
	@$(DOCKER_CMD) -f docker-compose.yml start $(c)
	@echo "$(GREEN)$(BOLD)[ok]$(NC) Containers started successfully$(GREEN) ✔️$(NC)"

.PHONY: down
down:
	$(call title1,"Stopping and removing containers|networks|volumes")
	@if [ -f "docker-compose.yml" ]; then \
		$(DOCKER_CMD) -f docker-compose.yml down $(c); \
	else \
		echo "$(YELLOW)No docker-compose.yml file found. Skipping down command.$(NC)"; \
	fi
	@echo "$(GREEN)$(BOLD)[ok]$(NC) Services stopped successfully$(GREEN) ✔️$(NC)"

.PHONY: stop
stop:
	$(call title1,"Stopping running containers")
	@$(DOCKER_CMD) -f docker-compose.yml stop $(c)
	@echo "$(GREEN)$(BOLD)[ok]$(NC) Containers stopped successfully$(GREEN) ✔️$(NC)"

.PHONY: restart
restart:
	$(call title1,"Restarting all services")
	@make stop && make up
	@echo "$(GREEN)$(BOLD)[ok]$(NC) Services restarted successfully$(GREEN) ✔️$(NC)"

.PHONY: rebuild-up
rebuild-up:
	$(call title1,"Rebuilding and restarting services")
	@$(DOCKER_CMD) -f docker-compose.yml down
	@$(DOCKER_CMD) -f docker-compose.yml build
	@$(DOCKER_CMD) -f docker-compose.yml up -d
	@echo "$(GREEN)$(BOLD)[ok]$(NC) Services rebuilt and restarted successfully$(GREEN) ✔️$(NC)"

.PHONY: logs
logs:
	$(call title1,"Showing logs for all services")
	@if [ -f "docker-compose.yml" ]; then \
		echo "$(CYAN)Logs for component: $(BOLD)$(SERVICE_NAME)$(NC)"; \
		docker compose -f docker-compose.yml logs --tail=100 -f $(c) 2>/dev/null || docker-compose -f docker-compose.yml logs --tail=100 -f $(c); \
	else \
		echo "$(YELLOW)No docker-compose.yml file found. Skipping logs command.$(NC)"; \
	fi

.PHONY: logs-api
logs-api:
	$(call title1,"Showing logs for $(SERVICE_NAME) service")
	@$(DOCKER_CMD) -f docker-compose.yml logs --tail=100 -f $(SERVICE_NAME)

.PHONY: ps
ps:
	$(call title1,"Listing container status")
	@$(DOCKER_CMD) -f docker-compose.yml ps

#-------------------------------------------------------
# Database Commands
#-------------------------------------------------------

# Database URL for migrations (uses docker-compose postgres service)
DATABASE_URL ?= postgres://tracer:tracer@localhost:5432/tracer?sslmode=disable
# Migrations path (override with: MIGRATIONS_PATH=/custom/path make migrate)
MIGRATIONS_PATH ?= ./migrations
MIGRATIONS_FUNCTIONS_PATH ?= ${MIGRATIONS_PATH}/functions

.PHONY: migrate
migrate:
	$(call title1,"Applying database migrations")
	@if ! command -v migrate >/dev/null 2>&1; then \
		echo "$(YELLOW)Installing golang-migrate...$(NC)"; \
		go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest; \
	fi
	@migrate -database "$(DATABASE_URL)&x-migrations-table=schema_migrations_functions" -path $(MIGRATIONS_FUNCTIONS_PATH) up
	@migrate -database "$(DATABASE_URL)" -path $(MIGRATIONS_PATH) up
	@echo "$(GREEN)$(BOLD)[ok]$(NC) Migrations applied successfully$(GREEN) ✔️$(NC)"

.PHONY: migrate-down
migrate-down:
	$(call title1,"Rolling back last migration")
	@if ! command -v migrate >/dev/null 2>&1; then \
		echo "$(YELLOW)Installing golang-migrate...$(NC)"; \
		go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest; \
	fi
	@migrate -database "$(DATABASE_URL)" -path $(MIGRATIONS_PATH) down 1
	@echo "$(GREEN)$(BOLD)[ok]$(NC) Last migration rolled back successfully$(GREEN) ✔️$(NC)"

.PHONY: migrate-down-all
migrate-down-all:
	$(call title1,"Rolling back all migrations")
	@if [ "$(FORCE)" != "1" ]; then \
		echo "$(RED)$(BOLD)WARNING: This will rollback ALL migrations and may cause data loss!$(NC)"; \
		echo "$(YELLOW)Press Ctrl+C to cancel, or wait 5 seconds to continue...$(NC)"; \
		echo "$(CYAN)Tip: Use FORCE=1 to skip this warning$(NC)"; \
		sleep 5; \
	fi
	@if ! command -v migrate >/dev/null 2>&1; then \
		echo "$(YELLOW)Installing golang-migrate...$(NC)"; \
		go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest; \
	fi
	@migrate -database "$(DATABASE_URL)" -path $(MIGRATIONS_PATH) down -all
	@migrate -database "$(DATABASE_URL)&x-migrations-table=schema_migrations_functions" -path $(MIGRATIONS_FUNCTIONS_PATH) down -all
	@echo "$(GREEN)$(BOLD)[ok]$(NC) All migrations rolled back successfully$(GREEN) ✔️$(NC)"

.PHONY: migrate-version
migrate-version:
	$(call title1,"Showing current migration version")
	@if ! command -v migrate >/dev/null 2>&1; then \
		echo "$(YELLOW)Installing golang-migrate...$(NC)"; \
		go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest; \
	fi
	@migrate -database "$(DATABASE_URL)&x-migrations-table=schema_migrations_functions" -path $(MIGRATIONS_FUNCTIONS_PATH) version
	@migrate -database "$(DATABASE_URL)" -path $(MIGRATIONS_PATH) version

.PHONY: migrate-force
migrate-force:
	$(call title1,"Force setting migration version to $(VERSION)")
	@if [ -z "$(VERSION)" ]; then \
		echo "$(RED)$(BOLD)[error]$(NC) VERSION is required. Usage: make migrate-force VERSION=1$(RED) ❌$(NC)"; \
		exit 1; \
	fi
	@if ! command -v migrate >/dev/null 2>&1; then \
		echo "$(YELLOW)Installing golang-migrate...$(NC)"; \
		go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest; \
	fi
	@migrate -database "$(DATABASE_URL)" -path $(MIGRATIONS_PATH) force $(VERSION)
	@echo "$(GREEN)$(BOLD)[ok]$(NC) Migration version forced to $(VERSION)$(GREEN) ✔️$(NC)"

.PHONY: seed
seed:
	$(call title1,"Loading development seed data")
	@if [ ! -f "$(MIGRATIONS_PATH)/seeds/001_dev_data.sql" ]; then \
		echo "$(YELLOW)No seed file found at $(MIGRATIONS_PATH)/seeds/001_dev_data.sql$(NC)"; \
		echo "$(YELLOW)Skipping seed data loading$(NC)"; \
	else \
		$(DOCKER_CMD) -f docker-compose.yml exec -T $(POSTGRES_SERVICE) psql -U tracer -d tracer < $(MIGRATIONS_PATH)/seeds/001_dev_data.sql; \
		echo "$(GREEN)$(BOLD)[ok]$(NC) Seed data loaded successfully$(GREEN) ✔️$(NC)"; \
	fi

.PHONY: seed-down
seed-down:
	$(call title1,"Removing development seed data")
	@if [ ! -f "$(MIGRATIONS_PATH)/seeds/001_dev_data.down.sql" ]; then \
		echo "$(YELLOW)No seed rollback file found at $(MIGRATIONS_PATH)/seeds/001_dev_data.down.sql$(NC)"; \
		echo "$(YELLOW)Skipping seed data removal$(NC)"; \
	else \
		$(DOCKER_CMD) -f docker-compose.yml exec -T $(POSTGRES_SERVICE) psql -U tracer -d tracer < $(MIGRATIONS_PATH)/seeds/001_dev_data.down.sql; \
		echo "$(GREEN)$(BOLD)[ok]$(NC) Seed data removed successfully$(GREEN) ✔️$(NC)"; \
	fi

#-------------------------------------------------------
# Docs Commands
#-------------------------------------------------------

.PHONY: generate-docs-all
generate-docs-all:
	$(call title1,"Generating Swagger documentation for all services")
	$(call check_command,swag,"go install github.com/swaggo/swag/cmd/swag@latest")
	@echo "$(CYAN)Verifying API documentation coverage...$(NC)"
	@sh ./scripts/verify-api-docs.sh 2>/dev/null || echo "$(YELLOW)Warning: Some API endpoints may not be properly documented. Continuing with documentation generation...$(NC)"
	@echo "$(CYAN)Generating documentation for plugin component...$(NC)"
	$(MAKE) generate-docs 2>&1 | grep -v "warning: "
	@echo "$(GREEN)$(BOLD)[ok]$(NC) Swagger documentation generated successfully$(GREEN) ✔️$(NC)"

.PHONY: verify-api-docs
verify-api-docs:
	$(call title1,"Verifying API documentation coverage")
	@sh ./scripts/verify-api-docs.sh
	@echo "$(GREEN)$(BOLD)[ok]$(NC) API documentation verification completed$(GREEN) ✔️$(NC)"

.PHONY: generate-docs
generate-docs:
	$(call title1,"Generating Swagger API documentation")
	@if ! command -v swag >/dev/null 2>&1; then \
		echo "$(YELLOW)Installing swag...$(NC)"; \
		go install github.com/swaggo/swag/cmd/swag@latest; \
	fi
	@cd $(ROOT_DIR) && swag init -g cmd/app/main.go -o api --parseDependency --parseInternal
	@docker run --rm -v ./:/local --user $(shell id -u):$(shell id -g) openapitools/openapi-generator-cli:v7.10.0 generate -i /local/api/swagger.json -g openapi-yaml -o /local/api
	@mv ./api/openapi/openapi.yaml ./api/openapi.yaml
	@rm -rf ./api/README.md ./api/.openapi-generator* ./api/openapi
	@echo "$(GREEN)$(BOLD)[ok]$(NC) Swagger API documentation generated successfully$(GREEN) ✔️$(NC)"

.PHONY: validate-api-docs
validate-api-docs: generate-docs
	$(call title1,"Validating API documentation")
	@docker run --rm -v ./:/local openapitools/openapi-generator-cli:v7.10.0 validate -i /local/api/swagger.json
	@echo "$(GREEN)$(BOLD)[ok]$(NC) API documentation validation completed$(GREEN) ✔️$(NC)"

#-------------------------------------------------------
# Developer Helper Commands
#-------------------------------------------------------

.PHONY: dev-setup
dev-setup:
	$(call title1,"Setting up development environment")
	@echo "$(CYAN)Installing development tools...$(NC)"
	@if ! command -v golangci-lint >/dev/null 2>&1; then \
		echo "$(YELLOW)Installing golangci-lint v2...$(NC)"; \
		go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.8.0; \
	fi
	@if ! command -v swag >/dev/null 2>&1; then \
		echo "$(YELLOW)Installing swag...$(NC)"; \
		go install github.com/swaggo/swag/cmd/swag@latest; \
	fi
	@if ! command -v mockgen >/dev/null 2>&1; then \
		echo "$(YELLOW)Installing mockgen...$(NC)"; \
		go install go.uber.org/mock/mockgen@v0.5.2; \
	fi
	@if ! command -v gosec >/dev/null 2>&1; then \
		echo "$(YELLOW)Installing gosec...$(NC)"; \
		go install github.com/securego/gosec/v2/cmd/gosec@latest; \
	fi
	@echo "$(CYAN)Setting up environment...$(NC)"
	@if [ -f .env.example ] && [ ! -f .env ]; then \
		cp .env.example .env; \
		echo "$(GREEN)Created .env file from template$(NC)"; \
	fi
	@make tidy
	@make check-tests
	@make sec
	@echo "$(GREEN)$(BOLD)[ok]$(NC) Development environment set up successfully$(GREEN) ✔️$(NC)"
	@echo "$(CYAN)You're ready to start developing! Here are some useful commands:$(NC)"
	@echo "  make build         - Build the component"
	@echo "  make test          - Run tests"
	@echo "  make up            - Start services"
	@echo "  make rebuild-up    - Rebuild and restart services during development"
