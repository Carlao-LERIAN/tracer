# ------------------------------------------------------
# Test configuration for Tracer project
# ------------------------------------------------------
TEST_TRACER_URL ?= http://localhost:8080
TEST_HEALTH_WAIT ?= 60

# Integration test filter
# RUN: specific test name pattern (e.g., TestIntegration_PostgresRepo_Create)
# PKG: specific package to test (e.g., ./internal/...)
# Usage: make test-integration RUN=TestIntegration_PostgresRepo_Create
#        make test-integration PKG=./internal/...
#        make test-integration RUN=TestIntegration_Chaos_Database PKG=./internal/... CHAOS=1
RUN ?=
PKG ?=

# Computed run pattern: uses RUN if set, otherwise defaults to '^TestIntegration'
ifeq ($(RUN),)
  RUN_PATTERN := ^TestIntegration
else
  RUN_PATTERN := $(RUN)
endif

# Low-resource mode for limited machines (sets -p=1 -parallel=1, disables -race)
# Usage: make test-integration LOW_RESOURCE=1
#        make coverage-integration LOW_RESOURCE=1
LOW_RESOURCE ?= 0

# Computed flags for low-resource mode
ifeq ($(LOW_RESOURCE),1)
  LOW_RES_P_FLAG := -p 1
  LOW_RES_PARALLEL_FLAG := -parallel 1
  LOW_RES_RACE_FLAG :=
else
  LOW_RES_P_FLAG :=
  LOW_RES_PARALLEL_FLAG :=
  LOW_RES_RACE_FLAG := -race
endif

# macOS ld64 workaround: newer ld emits noisy LC_DYSYMTAB warnings when linking test binaries with -race.
# If available, prefer Apple's classic linker to silence them.
UNAME_S := $(shell uname -s)
ifeq ($(UNAME_S),Darwin)
  # Prefer classic mode to suppress LC_DYSYMTAB warnings on macOS.
  # Set DISABLE_OSX_LINKER_WORKAROUND=1 to disable this behavior.
  ifneq ($(DISABLE_OSX_LINKER_WORKAROUND),1)
    GO_TEST_LDFLAGS := -ldflags="-linkmode=external -extldflags=-ld_classic"
  else
    GO_TEST_LDFLAGS :=
  endif
else
  GO_TEST_LDFLAGS :=
endif

define wait_for_services
	echo "Waiting for services to become healthy..."
	bash -c 'trap "pkill -P $$; exit 1" INT TERM; \
	for i in $$(seq 1 $(TEST_HEALTH_WAIT)); do \
	  if curl -fsS $(TEST_TRACER_URL)/health >/dev/null 2>&1; then \
	    echo "Services are up"; exit 0; \
	  fi; \
	  sleep 1; \
	done; echo "[error] Services not healthy after $(TEST_HEALTH_WAIT)s"; exit 1'
endef

.PHONY: wait-for-services
wait-for-services:
	$(call wait_for_services)


# ------------------------------------------------------
# Test tooling configuration
# ------------------------------------------------------

TEST_REPORTS_DIR ?= ./reports
GOTESTSUM := $(shell command -v gotestsum 2>/dev/null)
RETRY_ON_FAIL ?= 0

.PHONY: tools tools-gotestsum
tools: tools-gotestsum ## Install helpful dev/test tools

tools-gotestsum:
	@if [ -z "$(GOTESTSUM)" ]; then \
		echo "Installing gotestsum..."; \
		GO111MODULE=on go install gotest.tools/gotestsum@latest; \
	else \
		echo "gotestsum already installed: $(GOTESTSUM)"; \
	fi


#-------------------------------------------------------
# Core Commands
#-------------------------------------------------------

.PHONY: test
test:
	$(call title1,"Running all tests")
	@go test -v ./...
	@echo "$(GREEN)$(BOLD)[ok]$(NC) Tests completed successfully$(GREEN) ✔️$(NC)"


#-------------------------------------------------------
# Test Suite Aliases
#-------------------------------------------------------

# Quick test coverage verification
# Runs a fast coverage check without generating detailed reports
# Used primarily in CI/CD pipelines and pre-commit hooks
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

# Unit tests
.PHONY: test-unit
test-unit:
	$(call title1,"Running Go unit tests")
	@if ! command -v go >/dev/null 2>&1; then \
		echo "$(RED)Error: go is not installed$(NC)"; \
		exit 1; \
	fi
	@set -e; mkdir -p $(TEST_REPORTS_DIR); \
	pkgs=$$(go list ./... | awk '!/\/tests($|\/)/' | awk '!/\/api($|\/)/'); \
	if [ -z "$$pkgs" ]; then \
	  echo "No unit test packages found (outside ./tests)"; \
	else \
	  if [ -n "$(GOTESTSUM)" ]; then \
	    echo "Running unit tests with gotestsum"; \
	    gotestsum --format testname -- -v -race -count=1 $(GO_TEST_LDFLAGS) $$pkgs || { \
	      if [ "$(RETRY_ON_FAIL)" = "1" ]; then \
	        echo "Retrying unit tests once..."; \
	        gotestsum --format testname -- -v -race -count=1 $(GO_TEST_LDFLAGS) $$pkgs; \
	      else \
	        exit 1; \
	      fi; \
	    }; \
	  else \
	    go test -v -race -count=1 $(GO_TEST_LDFLAGS) $$pkgs; \
	  fi; \
	fi
	@echo "$(GREEN)$(BOLD)[ok]$(NC) Unit tests completed successfully$(GREEN) ✔️$(NC)"

# Unit tests with coverage (uses covermode=atomic)
# Supports PKG parameter to filter packages (e.g., PKG=./internal/...)
# Supports .ignorecoverunit file to exclude patterns from coverage stats
.PHONY: coverage-unit
coverage-unit:
	$(call title1,"Running Go unit tests with coverage")
	@if ! command -v go >/dev/null 2>&1; then \
		echo "$(RED)Error: go is not installed$(NC)"; \
		exit 1; \
	fi
	@set -e; mkdir -p $(TEST_REPORTS_DIR); \
	if [ -n "$(PKG)" ]; then \
	  echo "Using specified package: $(PKG)"; \
	  pkgs=$$(go list $(PKG) 2>/dev/null | awk '!/\/tests($|\/)/' | awk '!/\/api($|\/)/' | tr '\n' ' '); \
	else \
	  pkgs=$$(go list ./... | awk '!/\/tests($|\/)/' | awk '!/\/api($|\/)/'); \
	fi; \
	if [ -z "$$pkgs" ]; then \
	  echo "No unit test packages found (outside ./tests)"; \
	else \
	  echo "Packages: $$pkgs"; \
	  if [ -n "$(GOTESTSUM)" ]; then \
	    echo "Running unit tests with gotestsum (coverage enabled)"; \
	    gotestsum --format testname -- -v -race -count=1 $(GO_TEST_LDFLAGS) -covermode=atomic -coverprofile=$(TEST_REPORTS_DIR)/unit_coverage.out $$pkgs || { \
	      if [ "$(RETRY_ON_FAIL)" = "1" ]; then \
	        echo "Retrying unit tests once..."; \
	        gotestsum --format testname -- -v -race -count=1 $(GO_TEST_LDFLAGS) -covermode=atomic -coverprofile=$(TEST_REPORTS_DIR)/unit_coverage.out $$pkgs; \
	      else \
	        exit 1; \
	      fi; \
	    }; \
	  else \
	    go test -v -race -count=1 $(GO_TEST_LDFLAGS) -covermode=atomic -coverprofile=$(TEST_REPORTS_DIR)/unit_coverage.out $$pkgs; \
	  fi; \
	  if [ -f .ignorecoverunit ]; then \
	    echo "Filtering coverage with .ignorecoverunit patterns..."; \
	    patterns=$$(grep -v '^#' .ignorecoverunit | grep -v '^$$' | tr '\n' '|' | sed 's/|$$//'); \
	    if [ -n "$$patterns" ]; then \
	      regex_patterns=$$(echo "$$patterns" | sed 's/\./\\./g' | sed 's/\*/.*/g'); \
	      head -1 $(TEST_REPORTS_DIR)/unit_coverage.out > $(TEST_REPORTS_DIR)/unit_coverage_filtered.out; \
	      tail -n +2 $(TEST_REPORTS_DIR)/unit_coverage.out | grep -vE "$$regex_patterns" >> $(TEST_REPORTS_DIR)/unit_coverage_filtered.out || true; \
	      mv $(TEST_REPORTS_DIR)/unit_coverage_filtered.out $(TEST_REPORTS_DIR)/unit_coverage.out; \
	      echo "Excluded patterns: $$patterns"; \
	    fi; \
	  fi; \
	  echo "----------------------------------------"; \
	  go tool cover -func=$(TEST_REPORTS_DIR)/unit_coverage.out | grep total | awk '{print "Total coverage: " $$3}'; \
	  echo "----------------------------------------"; \
	fi
	@echo "$(GREEN)$(BOLD)[ok]$(NC) Coverage report generated$(GREEN) ✔️$(NC)"

# Benchmark tests
# Run performance benchmarks for critical code paths.
# Usage:
#   make test-bench                          # Run all benchmarks
#   make test-bench BENCH=ProcessSpan        # Run specific benchmark pattern
#   make test-bench BENCH_PKG=./internal/... # Run benchmarks in specific package
BENCH ?= .
BENCH_PKG ?= ./...

.PHONY: test-bench
test-bench:
	$(call title1,"Running Go benchmark tests")
	@if ! command -v go >/dev/null 2>&1; then \
		echo "$(RED)Error: go is not installed$(NC)"; \
		exit 1; \
	fi
	@echo "Benchmark pattern: $(BENCH)"
	@echo "Package: $(BENCH_PKG)"
	@go test -bench=$(BENCH) -benchmem -run=^$$ $(BENCH_PKG)
	@echo "$(GREEN)$(BOLD)[ok]$(NC) Benchmark tests completed$(GREEN) ✔️$(NC)"

# Integration tests with testcontainers (no coverage)
# These tests use the `integration` build tag and testcontainers-go to spin up
# ephemeral containers. No external Docker stack is required.
#
# NOTE: Integration tests always run with -p=1 (packages sequentially) because
# testcontainers can overwhelm Docker when creating many containers in parallel.
# This prevents transient failures like "port not found" or container timeouts.
#
# Requirements:
#   - Test files must follow the naming convention: *_integration_test.go
#   - Test functions must start with TestIntegration_ (e.g., TestIntegration_MyFeature_Works)
#   - Chaos tests use TestIntegration_Chaos_ prefix (e.g., TestIntegration_Chaos_Database_NetworkPartition)
#
# Chaos tests (CHAOS=1):
#   Chaos tests are included in integration test files but skip themselves by default.
#   To run chaos tests alongside integration tests, set CHAOS=1:
#     make test-integration CHAOS=1
#   This enables network chaos injection, container restarts, and other failure scenarios.
.PHONY: test-integration
test-integration:
	$(call title1,"Running integration tests with testcontainers")
	@if ! command -v go >/dev/null 2>&1; then \
		echo "$(RED)Error: go is not installed$(NC)"; \
		exit 1; \
	fi
	@if ! command -v docker >/dev/null 2>&1; then \
		echo "$(RED)Error: docker is not installed$(NC)"; \
		exit 1; \
	fi
	@set -e; mkdir -p $(TEST_REPORTS_DIR); \
	if [ -n "$(PKG)" ]; then \
	  echo "Using specified package: $(PKG)"; \
	  pkgs=$$(go list $(PKG) 2>/dev/null | tr '\n' ' '); \
	else \
	  echo "Finding packages with *_integration_test.go files..."; \
	  dirs=$$(find ./internal ./pkg -name '*_integration_test.go' 2>/dev/null | xargs -n1 dirname 2>/dev/null | sort -u | tr '\n' ' '); \
	  pkgs=$$(if [ -n "$$dirs" ]; then go list $$dirs 2>/dev/null | tr '\n' ' '; fi); \
	fi; \
	if [ -z "$$pkgs" ]; then \
	  echo "No integration test packages found"; \
	else \
	  echo "Packages: $$pkgs"; \
	  echo "Running packages sequentially (-p=1) to avoid Docker container conflicts"; \
	  if [ "$(LOW_RESOURCE)" = "1" ]; then \
	    echo "LOW_RESOURCE mode: -parallel=1, race detector disabled"; \
	  fi; \
	  if [ "$(CHAOS)" = "1" ]; then \
	    echo "CHAOS=1: Chaos tests (TestIntegration_Chaos_*) will run"; \
	  else \
	    echo "Chaos tests will be skipped (set CHAOS=1 to include them)"; \
	  fi; \
	  if [ -n "$(GOTESTSUM)" ]; then \
	    echo "Running testcontainers integration tests with gotestsum"; \
	    CHAOS=$(CHAOS) gotestsum --format testname -- \
	      -tags=integration -v $(LOW_RES_RACE_FLAG) -count=1 -timeout 600s $(GO_TEST_LDFLAGS) \
	      -p 1 $(LOW_RES_PARALLEL_FLAG) \
	      -run '$(RUN_PATTERN)' $$pkgs || { \
	      if [ "$(RETRY_ON_FAIL)" = "1" ]; then \
	        echo "Retrying integ tests once..."; \
	        CHAOS=$(CHAOS) gotestsum --format testname -- \
	          -tags=integration -v $(LOW_RES_RACE_FLAG) -count=1 -timeout 600s $(GO_TEST_LDFLAGS) \
	          -p 1 $(LOW_RES_PARALLEL_FLAG) \
	          -run '$(RUN_PATTERN)' $$pkgs; \
	      else \
	        exit 1; \
	      fi; \
	    }; \
	  else \
	    CHAOS=$(CHAOS) go test -tags=integration -v $(LOW_RES_RACE_FLAG) -count=1 -timeout 600s $(GO_TEST_LDFLAGS) \
	      -p 1 $(LOW_RES_PARALLEL_FLAG) \
	      -run '$(RUN_PATTERN)' $$pkgs; \
	  fi; \
	fi
	@echo "$(GREEN)$(BOLD)[ok]$(NC) Integration tests completed successfully$(GREEN) ✔️$(NC)"

# Integration tests with testcontainers (with coverage, uses covermode=atomic)
#
# NOTE: Integration tests always run with -p=1 (packages sequentially) because
# testcontainers can overwhelm Docker when creating many containers in parallel.
# This prevents transient failures like "port not found" or container timeouts.
#
# Chaos tests (CHAOS=1):
#   Chaos tests (TestIntegration_Chaos_*) skip themselves by default.
#   To include chaos tests in coverage, set CHAOS=1:
#     make coverage-integration CHAOS=1
.PHONY: coverage-integration
coverage-integration:
	$(call title1,"Running integration tests with testcontainers (coverage enabled)")
	@if ! command -v go >/dev/null 2>&1; then \
		echo "$(RED)Error: go is not installed$(NC)"; \
		exit 1; \
	fi
	@if ! command -v docker >/dev/null 2>&1; then \
		echo "$(RED)Error: docker is not installed$(NC)"; \
		exit 1; \
	fi
	@set -e; mkdir -p $(TEST_REPORTS_DIR); \
	if [ -n "$(PKG)" ]; then \
	  echo "Using specified package: $(PKG)"; \
	  pkgs=$$(go list $(PKG) 2>/dev/null | tr '\n' ' '); \
	else \
	  echo "Finding packages with *_integration_test.go files..."; \
	  dirs=$$(find ./internal ./pkg -name '*_integration_test.go' 2>/dev/null | xargs -n1 dirname 2>/dev/null | sort -u | tr '\n' ' '); \
	  pkgs=$$(if [ -n "$$dirs" ]; then go list $$dirs 2>/dev/null | tr '\n' ' '; fi); \
	fi; \
	if [ -z "$$pkgs" ]; then \
	  echo "No integration test packages found"; \
	else \
	  echo "Packages: $$pkgs"; \
	  echo "Running packages sequentially (-p=1) to avoid Docker container conflicts"; \
	  if [ "$(LOW_RESOURCE)" = "1" ]; then \
	    echo "LOW_RESOURCE mode: -parallel=1, race detector disabled"; \
	  fi; \
	  if [ "$(CHAOS)" = "1" ]; then \
	    echo "CHAOS=1: Chaos tests (TestIntegration_Chaos_*) will run"; \
	  else \
	    echo "Chaos tests will be skipped (set CHAOS=1 to include them)"; \
	  fi; \
	  if [ -n "$(GOTESTSUM)" ]; then \
	    echo "Running testcontainers integration tests with gotestsum (coverage enabled)"; \
	    CHAOS=$(CHAOS) gotestsum --format testname -- \
	      -tags=integration -v $(LOW_RES_RACE_FLAG) -count=1 -timeout 600s $(GO_TEST_LDFLAGS) \
	      -p 1 $(LOW_RES_PARALLEL_FLAG) \
	      -run '$(RUN_PATTERN)' -covermode=atomic -coverprofile=$(TEST_REPORTS_DIR)/integration_coverage.out \
	      $$pkgs || { \
	      if [ "$(RETRY_ON_FAIL)" = "1" ]; then \
	        echo "Retrying integ tests once..."; \
	        CHAOS=$(CHAOS) gotestsum --format testname -- \
	          -tags=integration -v $(LOW_RES_RACE_FLAG) -count=1 -timeout 600s $(GO_TEST_LDFLAGS) \
	          -p 1 $(LOW_RES_PARALLEL_FLAG) \
	          -run '$(RUN_PATTERN)' -covermode=atomic -coverprofile=$(TEST_REPORTS_DIR)/integration_coverage.out \
	          $$pkgs; \
	      else \
	        exit 1; \
	      fi; \
	    }; \
	  else \
	    CHAOS=$(CHAOS) go test -tags=integration -v $(LOW_RES_RACE_FLAG) -count=1 -timeout 600s $(GO_TEST_LDFLAGS) \
	      -p 1 $(LOW_RES_PARALLEL_FLAG) \
	      -run '$(RUN_PATTERN)' -covermode=atomic -coverprofile=$(TEST_REPORTS_DIR)/integration_coverage.out \
	      $$pkgs; \
	  fi; \
	  echo "----------------------------------------"; \
	  go tool cover -func=$(TEST_REPORTS_DIR)/integration_coverage.out | grep total | awk '{print "Total coverage: " $$3}'; \
	  echo "----------------------------------------"; \
	fi
	@echo "$(GREEN)$(BOLD)[ok]$(NC) Integration coverage report generated$(GREEN) ✔️$(NC)"

# Run all coverage targets
.PHONY: coverage
coverage:
	$(call title1,"Running all coverage targets")
	$(MAKE) coverage-unit
	$(MAKE) coverage-integration
	@echo "$(GREEN)$(BOLD)[ok]$(NC) All coverage reports generated$(GREEN) ✔️$(NC)"

# Run all tests (excludes native fuzz engine which runs indefinitely)
# To include chaos tests: make test-all CHAOS=1
.PHONY: test-all
test-all:
	$(call title1,"Running all tests")
	$(call title1,"Running unit tests")
	$(MAKE) test-unit
	$(call title1,"Running integration tests")
	$(MAKE) test-integration
	@echo "$(GREEN)$(BOLD)[ok]$(NC) All tests completed successfully$(GREEN) ✔️$(NC)"
