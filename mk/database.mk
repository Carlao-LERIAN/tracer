# ------------------------------------------------------
# Database migration and seed management commands
# ------------------------------------------------------
# This file contains all database operations including
# migrations, rollbacks, versioning, and seed data management.
#
# Requirements:
#   - golang-migrate/migrate CLI (auto-installed if missing)
#   - PostgreSQL database accessible via DATABASE_URL
#   - Docker Compose for seed operations (uses POSTGRES_SERVICE)
#
# Variables:
#   - DATABASE_URL: Connection string (default: localhost:5432/tracer)
#   - POSTGRES_USER: PostgreSQL username for seed operations (default: tracer)
#   - POSTGRES_DB: PostgreSQL database name for seed operations (default: tracer)
#   - MIGRATIONS_PATH: Path to migration files (default: ./migrations)
#   - MIGRATIONS_FUNCTIONS_PATH: Path to function migrations (default: ./migrations/functions)
#   - MIGRATIONS_FUNCTIONS_DB_URL: Auto-constructed URL with x-migrations-table parameter (internal)
#   - POSTGRES_SERVICE: Docker service name (from main Makefile)
#   - DOCKER_CMD: Docker compose command (from main Makefile)
#   - FORCE: Skip confirmation prompts (0|1, default: 0)
#   - VERSION: Migration version for migrate-force command
#
# Usage:
#   make migrate                     # Apply all pending migrations
#   make migrate-down                # Rollback last migration
#   make migrate-down-all FORCE=1    # Rollback all (with confirmation)
#   make migrate-force VERSION=5     # Force set version to 5
#   make seed                        # Load development seed data
# ------------------------------------------------------

# Database configuration
DATABASE_URL ?= postgres://tracer:tracer@localhost:5432/tracer?sslmode=disable
POSTGRES_USER ?= tracer
POSTGRES_DB ?= tracer
MIGRATIONS_PATH ?= ./migrations
MIGRATIONS_FUNCTIONS_PATH ?= ${MIGRATIONS_PATH}/functions

# Dynamically construct the migrations functions DB URL with the correct separator
# If DATABASE_URL contains '?', use '&', otherwise use '?'
MIGRATIONS_FUNCTIONS_DB_URL := $(if $(findstring ?,$(DATABASE_URL)),$(DATABASE_URL)&x-migrations-table=schema_migrations_functions,$(DATABASE_URL)?x-migrations-table=schema_migrations_functions)

# Path to the installed migrate binary
MIGRATE_BIN := $(or $(GOBIN),$(shell go env GOPATH)/bin)/migrate

#-------------------------------------------------------
# Commands (alphabetically ordered)
#-------------------------------------------------------

# Apply all pending migrations
# Runs function migrations first, then regular migrations
.PHONY: migrate
migrate:
	$(call title1,"Applying database migrations")
	@if [ ! -x "$(MIGRATE_BIN)" ]; then \
		echo "$(YELLOW)Installing golang-migrate...$(NC)"; \
		go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest; \
	fi
	@$(MIGRATE_BIN) -database "$(MIGRATIONS_FUNCTIONS_DB_URL)" -path $(MIGRATIONS_FUNCTIONS_PATH) up
	@$(MIGRATE_BIN) -database "$(DATABASE_URL)" -path $(MIGRATIONS_PATH) up
	@echo "$(GREEN)$(BOLD)[ok]$(NC) Migrations applied successfully$(GREEN) ✔️$(NC)"

# Rollback the last migration
# Affects only regular migrations, not function migrations
.PHONY: migrate-down
migrate-down:
	$(call title1,"Rolling back last migration")
	@if [ ! -x "$(MIGRATE_BIN)" ]; then \
		echo "$(YELLOW)Installing golang-migrate...$(NC)"; \
		go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest; \
	fi
	@$(MIGRATE_BIN) -database "$(DATABASE_URL)" -path $(MIGRATIONS_PATH) down 1
	@echo "$(GREEN)$(BOLD)[ok]$(NC) Last migration rolled back successfully$(GREEN) ✔️$(NC)"

# Rollback ALL migrations (DESTRUCTIVE - requires confirmation)
# Set FORCE=1 to skip the 5-second confirmation prompt
# Usage: make migrate-down-all FORCE=1
.PHONY: migrate-down-all
migrate-down-all:
	$(call title1,"Rolling back all migrations")
	@if [ "$(FORCE)" != "1" ]; then \
		echo "$(RED)$(BOLD)WARNING: This will rollback ALL migrations and may cause data loss!$(NC)"; \
		echo "$(YELLOW)Press Ctrl+C to cancel, or wait 5 seconds to continue...$(NC)"; \
		echo "$(CYAN)Tip: Use FORCE=1 to skip this warning$(NC)"; \
		sleep 5; \
	fi
	@if [ ! -x "$(MIGRATE_BIN)" ]; then \
		echo "$(YELLOW)Installing golang-migrate...$(NC)"; \
		go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest; \
	fi
	@$(MIGRATE_BIN) -database "$(DATABASE_URL)" -path $(MIGRATIONS_PATH) down -all
	@$(MIGRATE_BIN) -database "$(MIGRATIONS_FUNCTIONS_DB_URL)" -path $(MIGRATIONS_FUNCTIONS_PATH) down -all
	@echo "$(GREEN)$(BOLD)[ok]$(NC) All migrations rolled back successfully$(GREEN) ✔️$(NC)"

# Force set migration version (use with caution)
# Required parameter: VERSION=N
# Usage: make migrate-force VERSION=5
.PHONY: migrate-force
migrate-force:
	$(call title1,"Force setting migration version to $(VERSION)")
	@if [ -z "$(VERSION)" ]; then \
		echo "$(RED)$(BOLD)[error]$(NC) VERSION is required. Usage: make migrate-force VERSION=1$(RED) ❌$(NC)"; \
		exit 1; \
	fi
	@if [ ! -x "$(MIGRATE_BIN)" ]; then \
		echo "$(YELLOW)Installing golang-migrate...$(NC)"; \
		go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest; \
	fi
	@$(MIGRATE_BIN) -database "$(DATABASE_URL)" -path $(MIGRATIONS_PATH) force $(VERSION)
	@echo "$(GREEN)$(BOLD)[ok]$(NC) Migration version forced to $(VERSION)$(GREEN) ✔️$(NC)"

# Show current migration version
# Displays version for both function and regular migrations
.PHONY: migrate-version
migrate-version:
	$(call title1,"Showing current migration version")
	@if [ ! -x "$(MIGRATE_BIN)" ]; then \
		echo "$(YELLOW)Installing golang-migrate...$(NC)"; \
		go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest; \
	fi
	@$(MIGRATE_BIN) -database "$(MIGRATIONS_FUNCTIONS_DB_URL)" -path $(MIGRATIONS_FUNCTIONS_PATH) version
	@$(MIGRATE_BIN) -database "$(DATABASE_URL)" -path $(MIGRATIONS_PATH) version

# Load development seed data into database
# Requires: ./migrations/seeds/001_dev_data.sql
# Uses Docker Compose to execute SQL against PostgreSQL service
.PHONY: seed
seed:
	$(call title1,"Loading development seed data")
	@if [ ! -f "$(MIGRATIONS_PATH)/seeds/001_dev_data.sql" ]; then \
		echo "$(YELLOW)No seed file found at $(MIGRATIONS_PATH)/seeds/001_dev_data.sql$(NC)"; \
		echo "$(YELLOW)Skipping seed data loading$(NC)"; \
	else \
		$(DOCKER_CMD) -f docker-compose.yml exec -T $(POSTGRES_SERVICE) psql -U $(POSTGRES_USER) -d $(POSTGRES_DB) < $(MIGRATIONS_PATH)/seeds/001_dev_data.sql; \
		echo "$(GREEN)$(BOLD)[ok]$(NC) Seed data loaded successfully$(GREEN) ✔️$(NC)"; \
	fi

# Remove development seed data from database
# Requires: ./migrations/seeds/001_dev_data.down.sql
# Uses Docker Compose to execute SQL against PostgreSQL service
.PHONY: seed-down
seed-down:
	$(call title1,"Removing development seed data")
	@if [ ! -f "$(MIGRATIONS_PATH)/seeds/001_dev_data.down.sql" ]; then \
		echo "$(YELLOW)No seed rollback file found at $(MIGRATIONS_PATH)/seeds/001_dev_data.down.sql$(NC)"; \
		echo "$(YELLOW)Skipping seed data removal$(NC)"; \
	else \
		$(DOCKER_CMD) -f docker-compose.yml exec -T $(POSTGRES_SERVICE) psql -U $(POSTGRES_USER) -d $(POSTGRES_DB) < $(MIGRATIONS_PATH)/seeds/001_dev_data.down.sql; \
		echo "$(GREEN)$(BOLD)[ok]$(NC) Seed data removed successfully$(GREEN) ✔️$(NC)"; \
	fi
