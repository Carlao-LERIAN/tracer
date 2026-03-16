// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package bootstrap

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	authMiddleware "github.com/LerianStudio/lib-auth/v2/auth/middleware"
	libCommons "github.com/LerianStudio/lib-commons/v2/commons"
	libLog "github.com/LerianStudio/lib-commons/v2/commons/log"
	libOtel "github.com/LerianStudio/lib-commons/v2/commons/opentelemetry"
	libPostgres "github.com/LerianStudio/lib-commons/v2/commons/postgres"
	libZap "github.com/LerianStudio/lib-commons/v2/commons/zap"

	"tracer/internal/adapters/cel"
	"tracer/internal/adapters/http/in"
	httpMiddleware "tracer/internal/adapters/http/in/middleware"
	"tracer/internal/adapters/postgres"
	"tracer/internal/services"
	"tracer/internal/services/cache"
	"tracer/internal/services/command"
	"tracer/internal/services/query"
	"tracer/internal/services/workers"
	"tracer/pkg/clock"
	"tracer/pkg/constant"
	"tracer/pkg/migration"
	"tracer/pkg/model"
	"tracer/pkg/resilience"
)

// Config is the top level configuration struct for the entire application.
type Config struct {
	ServerAddress           string `env:"SERVER_ADDRESS"`
	LogLevel                string `env:"LOG_LEVEL"`
	OtelServiceName         string `env:"OTEL_RESOURCE_SERVICE_NAME"`
	OtelLibraryName         string `env:"OTEL_LIBRARY_NAME"`
	OtelServiceVersion      string `env:"OTEL_RESOURCE_SERVICE_VERSION"`
	OtelDeploymentEnv       string `env:"OTEL_RESOURCE_DEPLOYMENT_ENVIRONMENT"`
	OtelColExporterEndpoint string `env:"OTEL_EXPORTER_OTLP_ENDPOINT"`
	EnableTelemetry         bool   `env:"ENABLE_TELEMETRY"`
	DBHost                  string `env:"DB_HOST"`
	DBUser                  string `env:"DB_USER"`
	DBPassword              string `env:"DB_PASSWORD"`
	DBName                  string `env:"DB_NAME"`
	DBPort                  string `env:"DB_PORT"`
	DBSSLMode               string `env:"DB_SSL_MODE"`
	MigrationPath           string `env:"MIGRATIONS_PATH"`

	// Authentication
	APIKey               string `env:"API_KEY"`
	APIKeyEnabled        bool   `env:"API_KEY_ENABLED"`
	APIKeyOnlyValidation bool   `env:"API_KEY_ENABLED_ONLY_VALIDATION"`
	PluginAuthAddress    string `env:"PLUGIN_AUTH_ADDRESS"`
	PluginAuthEnabled    bool   `env:"PLUGIN_AUTH_ENABLED"`

	// CORS
	CORSAllowedOrigins string `env:"CORS_ALLOWED_ORIGINS"`

	// CEL Expression Engine
	CELCostLimit string `env:"CEL_COST_LIMIT"`

	// Rule Evaluation Feature Flags
	DefaultDecisionWhenNoMatch string `env:"DEFAULT_DECISION_WHEN_NO_MATCH"`
	MaxRulesPerRequest         string `env:"MAX_RULES_PER_REQUEST"`

	// Usage Counter Cleanup Worker
	// CleanupWorkerEnabled enables/disables the background cleanup worker (default: false)
	// Set CLEANUP_WORKER_ENABLED=true in environment to enable the worker
	CleanupWorkerEnabled bool `env:"CLEANUP_WORKER_ENABLED"`
	// CleanupIntervalHours is the interval between cleanup runs in hours (default: 24)
	CleanupIntervalHours string `env:"CLEANUP_INTERVAL_HOURS"`

	// Rule Sync Worker
	// RuleSyncPollIntervalSeconds is how often the worker polls for rule changes (default: 10)
	RuleSyncPollIntervalSeconds string `env:"RULE_SYNC_POLL_INTERVAL_SECONDS"`
	// RuleSyncStalenessThresholdSeconds is when the cache is considered stale for health checks (default: 50)
	RuleSyncStalenessThresholdSeconds string `env:"RULE_SYNC_STALENESS_THRESHOLD_SECONDS"`
	// RuleSyncOverlapBufferSeconds is the overlap buffer for delta queries in seconds (default: 2)
	RuleSyncOverlapBufferSeconds string `env:"RULE_SYNC_OVERLAP_BUFFER_SECONDS"`
}

// minAPIKeyLength is the minimum recommended length for API keys.
const minAPIKeyLength = 32

// celCompilerAdapter wraps cel.Adapter to satisfy command.ExpressionCompiler interface.
type celCompilerAdapter struct {
	adapter *cel.Adapter
}

// Compile wraps cel.Adapter.Compile to return any instead of *cel.CompiledProgram.
func (c *celCompilerAdapter) Compile(ctx context.Context, expression string) (any, error) {
	return c.adapter.Compile(ctx, expression)
}

// parseCELCostLimit parses the CEL cost limit from string to uint64.
// Returns default value (10000) if empty.
// Returns error if value is invalid or zero.
func parseCELCostLimit(s string) (uint64, error) {
	const defaultValue uint64 = 10000

	if s == "" {
		return defaultValue, nil
	}

	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid CEL_COST_LIMIT value '%s': %w", s, err)
	}

	if v == 0 {
		return 0, fmt.Errorf("CEL_COST_LIMIT must be positive, got 0")
	}

	return v, nil
}

// parseDefaultDecision parses the default decision from string.
// Returns model.DecisionAllow if empty or "ALLOW".
// Returns model.DecisionDeny if "DENY".
// Returns error if value is invalid.
//
// NOTE: REVIEW is intentionally excluded as a default decision because
// defaulting to manual review would overwhelm human reviewers when no
// rules match, which is the common case for legitimate transactions.
func parseDefaultDecision(s string) (model.Decision, error) {
	switch s {
	case "", "ALLOW":
		return model.DecisionAllow, nil
	case "DENY":
		return model.DecisionDeny, nil
	default:
		return "", fmt.Errorf("invalid DEFAULT_DECISION_WHEN_NO_MATCH value '%s': must be ALLOW or DENY", s)
	}
}

// parseMaxRulesPerRequest parses the max rules per request from string to int.
// Returns default value (1000) if empty.
// Returns error if value is invalid, non-positive, or exceeds maximum.
func parseMaxRulesPerRequest(s string) (int, error) {
	const defaultValue = 1000

	// maxAllowed limits MaxRulesPerRequest to prevent resource exhaustion.
	// 100,000 rules is a reasonable upper bound that balances flexibility
	// with DoS protection. At this limit, memory and CPU usage remain bounded.
	const maxAllowed = 100000

	if s == "" {
		return defaultValue, nil
	}

	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid MAX_RULES_PER_REQUEST value '%s': %w", s, err)
	}

	if v <= 0 {
		return 0, fmt.Errorf("MAX_RULES_PER_REQUEST must be positive, got %d", v)
	}

	if v > maxAllowed {
		return 0, fmt.Errorf("MAX_RULES_PER_REQUEST exceeds maximum allowed (%d), got %d", maxAllowed, v)
	}

	return v, nil
}

// parseCleanupIntervalHours parses the cleanup interval from string to time.Duration.
// Returns default value (24 hours) if empty.
// Returns error if value is invalid, non-positive, or exceeds maximum.
func parseCleanupIntervalHours(s string) (time.Duration, error) {
	const defaultHours = 24

	// maxAllowedHours limits cleanup interval to 1 year (8760 hours).
	// This prevents misconfiguration that could effectively disable cleanup.
	const maxAllowedHours = 8760

	if s == "" {
		return time.Duration(defaultHours) * time.Hour, nil
	}

	hours, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid CLEANUP_INTERVAL_HOURS value '%s': %w", s, err)
	}

	if hours <= 0 {
		return 0, fmt.Errorf("CLEANUP_INTERVAL_HOURS must be positive, got %d", hours)
	}

	if hours > maxAllowedHours {
		return 0, fmt.Errorf("CLEANUP_INTERVAL_HOURS exceeds maximum allowed (%d hours = 1 year), got %d", maxAllowedHours, hours)
	}

	return time.Duration(hours) * time.Hour, nil
}

// parseRuleSyncPollInterval parses the poll interval from string to time.Duration.
// Returns default value (10 seconds) if empty.
// Returns error if value is invalid, non-positive, or exceeds maximum.
func parseRuleSyncPollInterval(s string) (time.Duration, error) {
	const (
		defaultSeconds    = 10
		maxAllowedSeconds = 3600
	)

	if s == "" {
		return time.Duration(defaultSeconds) * time.Second, nil
	}

	seconds, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid RULE_SYNC_POLL_INTERVAL_SECONDS value '%s': %w", s, err)
	}

	if seconds <= 0 {
		return 0, fmt.Errorf("RULE_SYNC_POLL_INTERVAL_SECONDS must be positive, got %d", seconds)
	}

	if seconds > maxAllowedSeconds {
		return 0, fmt.Errorf("RULE_SYNC_POLL_INTERVAL_SECONDS exceeds maximum allowed (%d seconds = 1 hour), got %d", maxAllowedSeconds, seconds)
	}

	return time.Duration(seconds) * time.Second, nil
}

// parseRuleSyncStalenessThreshold parses the staleness threshold from string to time.Duration.
// Returns default value (50 seconds) if empty.
// Returns error if value is invalid, non-positive, or exceeds maximum.
func parseRuleSyncStalenessThreshold(s string) (time.Duration, error) {
	const (
		defaultSeconds    = 50
		maxAllowedSeconds = 3600
	)

	if s == "" {
		return time.Duration(defaultSeconds) * time.Second, nil
	}

	seconds, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid RULE_SYNC_STALENESS_THRESHOLD_SECONDS value '%s': %w", s, err)
	}

	if seconds <= 0 {
		return 0, fmt.Errorf("RULE_SYNC_STALENESS_THRESHOLD_SECONDS must be positive, got %d", seconds)
	}

	if seconds > maxAllowedSeconds {
		return 0, fmt.Errorf("RULE_SYNC_STALENESS_THRESHOLD_SECONDS exceeds maximum allowed (%d seconds = 1 hour), got %d", maxAllowedSeconds, seconds)
	}

	return time.Duration(seconds) * time.Second, nil
}

// parseRuleSyncOverlapBuffer parses the overlap buffer from string to time.Duration.
// Returns default value (2 seconds) if empty.
// Returns error if value is invalid, negative, or exceeds maximum.
// Zero is allowed (no overlap buffer).
func parseRuleSyncOverlapBuffer(s string) (time.Duration, error) {
	const (
		defaultSeconds    = 2
		maxAllowedSeconds = 60
	)

	if s == "" {
		return time.Duration(defaultSeconds) * time.Second, nil
	}

	seconds, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid RULE_SYNC_OVERLAP_BUFFER_SECONDS value '%s': %w", s, err)
	}

	if seconds < 0 {
		return 0, fmt.Errorf("RULE_SYNC_OVERLAP_BUFFER_SECONDS must be non-negative, got %d", seconds)
	}

	if seconds > maxAllowedSeconds {
		return 0, fmt.Errorf("RULE_SYNC_OVERLAP_BUFFER_SECONDS exceeds maximum allowed (%d seconds), got %d", maxAllowedSeconds, seconds)
	}

	return time.Duration(seconds) * time.Second, nil
}

// LoadCleanupWorkerConfig creates a UsageCleanupWorkerConfig from environment configuration.
// Returns nil config if cleanup worker is disabled.
// Returns error if config or logger is nil, or if config values are invalid.
func LoadCleanupWorkerConfig(cfg *Config, logger libLog.Logger) (*workers.UsageCleanupWorkerConfig, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	if logger == nil {
		return nil, fmt.Errorf("logger cannot be nil")
	}

	// Check if cleanup worker is disabled
	// Note: default value for bool is false, so cleanup worker is disabled by default
	// Set CLEANUP_WORKER_ENABLED=true to enable the background cleanup worker
	if !cfg.CleanupWorkerEnabled {
		logger.WithFields(
			"config", "CLEANUP_WORKER_ENABLED",
		).Info("Usage counter cleanup worker is DISABLED")

		return nil, nil
	}

	cleanupInterval, err := parseCleanupIntervalHours(cfg.CleanupIntervalHours)
	if err != nil {
		return nil, fmt.Errorf("invalid CLEANUP_INTERVAL_HOURS: %w", err)
	}

	logger.WithFields(
		"cleanup_interval", cleanupInterval.String(),
	).Info("Usage counter cleanup worker configuration loaded")

	return &workers.UsageCleanupWorkerConfig{
		CleanupInterval: cleanupInterval,
	}, nil
}

// LoadRuleSyncWorkerConfig creates a RuleSyncWorkerConfig from environment configuration.
// Returns error if config or logger is nil, or if config values are invalid.
func LoadRuleSyncWorkerConfig(cfg *Config, logger libLog.Logger) (*workers.RuleSyncWorkerConfig, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	if logger == nil {
		return nil, fmt.Errorf("logger cannot be nil")
	}

	pollInterval, err := parseRuleSyncPollInterval(cfg.RuleSyncPollIntervalSeconds)
	if err != nil {
		return nil, fmt.Errorf("invalid RULE_SYNC_POLL_INTERVAL_SECONDS: %w", err)
	}

	stalenessThreshold, err := parseRuleSyncStalenessThreshold(cfg.RuleSyncStalenessThresholdSeconds)
	if err != nil {
		return nil, fmt.Errorf("invalid RULE_SYNC_STALENESS_THRESHOLD_SECONDS: %w", err)
	}

	overlapBuffer, err := parseRuleSyncOverlapBuffer(cfg.RuleSyncOverlapBufferSeconds)
	if err != nil {
		return nil, fmt.Errorf("invalid RULE_SYNC_OVERLAP_BUFFER_SECONDS: %w", err)
	}

	if stalenessThreshold < pollInterval {
		return nil, fmt.Errorf("invalid configuration: RULE_SYNC_STALENESS_THRESHOLD_SECONDS (%s) must be >= RULE_SYNC_POLL_INTERVAL_SECONDS (%s)",
			stalenessThreshold, pollInterval)
	}

	logger.WithFields(
		"poll_interval", pollInterval.String(),
		"staleness_threshold", stalenessThreshold.String(),
		"overlap_buffer", overlapBuffer.String(),
	).Info("Rule sync worker configuration loaded")

	return &workers.RuleSyncWorkerConfig{
		PollInterval:       pollInterval,
		StalenessThreshold: stalenessThreshold,
		OverlapBuffer:      overlapBuffer,
	}, nil
}

// LoadEvaluationConfig creates an EvaluationConfig from environment configuration.
// Returns error if config or logger is nil, or if any value is invalid.
// Logs a warning if using default ALLOW decision (fail-open behavior).
func LoadEvaluationConfig(cfg *Config, logger libLog.Logger) (*query.EvaluationConfig, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	if logger == nil {
		return nil, fmt.Errorf("logger cannot be nil")
	}

	defaultDecision, err := parseDefaultDecision(cfg.DefaultDecisionWhenNoMatch)
	if err != nil {
		return nil, fmt.Errorf("invalid DEFAULT_DECISION_WHEN_NO_MATCH: %w", err)
	}

	// Warn if using default ALLOW (fail-open) - operator should be aware
	if cfg.DefaultDecisionWhenNoMatch == "" {
		logger.WithFields(
			"config", "DEFAULT_DECISION_WHEN_NO_MATCH",
			"default_value", "ALLOW",
		).Warn("Using default ALLOW decision when no rules match (fail-open)")
	}

	maxRules, err := parseMaxRulesPerRequest(cfg.MaxRulesPerRequest)
	if err != nil {
		return nil, fmt.Errorf("invalid MAX_RULES_PER_REQUEST: %w", err)
	}

	return &query.EvaluationConfig{
		DefaultDecisionWhenNoMatch: defaultDecision,
		MaxRulesPerRequest:         maxRules,
	}, nil
}

// initCELAdapter initializes the CEL expression engine with configuration.
func initCELAdapter(cfg *Config, logger libLog.Logger) (*cel.Adapter, error) {
	celCostLimit, err := parseCELCostLimit(cfg.CELCostLimit)
	if err != nil {
		return nil, fmt.Errorf("invalid CEL cost limit configuration: %w", err)
	}

	adapter, err := cel.NewAdapter(cel.AdapterConfig{
		CostLimit: celCostLimit,
	}, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL adapter: %w", err)
	}

	return adapter, nil
}

// ValidateAuthConfig validates the authentication configuration.
// It warns when auth is disabled (operator should be aware).
// It fails if auth is enabled but key is missing.
// It warns if the key is too short (security best practice).
func ValidateAuthConfig(cfg *Config, logger libLog.Logger) error {
	// Warn if auth is disabled (operator should be aware)
	if !cfg.APIKeyEnabled {
		logger.WithFields("config", "API_KEY_ENABLED").Warn("API Key authentication is DISABLED")
		return nil
	}

	// Fail if auth is enabled but key is missing
	if cfg.APIKey == "" {
		return fmt.Errorf("API_KEY must be set when API_KEY_ENABLED=true")
	}

	// Warn if key is too short (security best practice)
	if len(cfg.APIKey) < minAPIKeyLength {
		logger.WithFields("min_length", minAPIKeyLength, "actual_length", len(cfg.APIKey)).Warn("API_KEY should be at least 32 characters")
	}

	return nil
}

// ValidateAccessManagerConfig validates the Access Manager plugin configuration.
// It warns when plugin auth is disabled (operator should be aware).
// It fails if plugin auth is enabled but the address is missing.
func ValidateAccessManagerConfig(cfg *Config, logger libLog.Logger) error {
	if !cfg.PluginAuthEnabled {
		logger.WithFields("config", "PLUGIN_AUTH_ENABLED").Warn("Access Manager plugin authentication is DISABLED")
		return nil
	}

	if cfg.PluginAuthAddress == "" {
		return fmt.Errorf("PLUGIN_AUTH_ADDRESS must be set when PLUGIN_AUTH_ENABLED=true")
	}

	return nil
}

// initPostgresConnection creates and connects a PostgreSQL connection pool.
func initPostgresConnection(cfg *Config, logger libLog.Logger) (*libPostgres.PostgresConnection, error) {
	sslMode := cfg.DBSSLMode
	if sslMode == "" {
		sslMode = "disable" // Default for local development; use "require" in production
	}

	postgresSQLSource := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=%s",
		cfg.DBHost, cfg.DBUser, cfg.DBPassword, cfg.DBName, cfg.DBPort, sslMode)

	postgresConn := &libPostgres.PostgresConnection{
		ConnectionStringPrimary: postgresSQLSource,
		ConnectionStringReplica: postgresSQLSource,
		PrimaryDBName:           cfg.DBName,
		ReplicaDBName:           cfg.DBName,
		Component:               constant.ApplicationName,
		MigrationsPath:          cfg.MigrationPath,
		Logger:                  logger,
	}

	if err := runFunctionMigrations(postgresSQLSource, cfg.MigrationPath, logger); err != nil {
		return nil, fmt.Errorf("failed to run function migrations: %w", err)
	}

	if err := postgresConn.Connect(); err != nil {
		return nil, fmt.Errorf("failed to connect to PostgreSQL: %w", err)
	}

	return postgresConn, nil
}

// initRuleService creates the rule service with all its dependencies.
// The cacheWriter parameter is optional (nil-safe); when provided, activate and
// deactivate commands will synchronously update the in-memory cache.
func initRuleService(ruleRepo *postgres.Repository, celAdapter *cel.Adapter, auditWriter command.AuditWriter, cacheWriter command.RuleCacheWriter, clk clock.Clock) (*services.RuleService, error) {
	celCompiler := &celCompilerAdapter{adapter: celAdapter}

	// Inject audit writer and cache writer into Rule commands
	createRuleCmd := command.NewCreateRuleCommand(ruleRepo, celCompiler, clk, auditWriter)
	updateRuleCmd := command.NewUpdateRuleCommand(ruleRepo, celCompiler, clk, auditWriter)

	activateRuleCmd, err := command.NewActivateRuleService(ruleRepo, celCompiler, clk, auditWriter, cacheWriter)
	if err != nil {
		return nil, fmt.Errorf("failed to create activate rule service: %w", err)
	}

	deactivateRuleCmd, err := command.NewDeactivateRuleService(ruleRepo, clk, auditWriter, cacheWriter)
	if err != nil {
		return nil, fmt.Errorf("failed to create deactivate rule service: %w", err)
	}

	draftRuleCmd, err := command.NewDraftRuleService(ruleRepo, clk, auditWriter)
	if err != nil {
		return nil, fmt.Errorf("failed to create draft rule service: %w", err)
	}

	deleteRuleCmd, err := command.NewDeleteRuleService(ruleRepo, auditWriter)
	if err != nil {
		return nil, fmt.Errorf("failed to create delete rule service: %w", err)
	}

	getRuleQuery := query.NewGetRuleQuery(ruleRepo)
	listRulesQuery := query.NewListRulesQuery(ruleRepo)

	return services.NewRuleService(createRuleCmd, updateRuleCmd, activateRuleCmd, deactivateRuleCmd, draftRuleCmd, deleteRuleCmd, getRuleQuery, listRulesQuery), nil
}

// initEvaluateRulesQuery creates the rule evaluation query with all its dependencies.
// The activeRulesRepo parameter accepts any ActiveRulesRepository implementation
// (e.g., *postgres.Repository for direct DB reads, or *cache.CacheAdapter for in-memory reads).
func initEvaluateRulesQuery(activeRulesRepo query.ActiveRulesRepository, celAdapter *cel.Adapter, evalConfig *query.EvaluationConfig) (*query.EvaluateRulesQuery, error) {
	ruleEvaluator, err := query.NewRuleEvaluator(celAdapter)
	if err != nil {
		return nil, fmt.Errorf("failed to create rule evaluator: %w", err)
	}

	completeEvaluator, err := query.NewCompleteEvaluator(ruleEvaluator)
	if err != nil {
		return nil, fmt.Errorf("failed to create complete evaluator: %w", err)
	}

	getActiveRulesQuery, err := query.NewGetActiveRulesQuery(activeRulesRepo)
	if err != nil {
		return nil, fmt.Errorf("failed to create get active rules query: %w", err)
	}

	evaluateRulesQuery, err := query.NewEvaluateRulesQuery(getActiveRulesQuery, completeEvaluator, evalConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create evaluate rules query: %w", err)
	}

	return evaluateRulesQuery, nil
}

// limitServiceDeps holds the dependencies created during limit service initialization.
type limitServiceDeps struct {
	service          *services.LimitService
	usageCounterRepo *postgres.UsageCounterRepository
	limitRepo        *postgres.LimitRepository
}

// initLimitService creates the limit service with all its dependencies.
func initLimitService(postgresConn *libPostgres.PostgresConnection, auditWriter command.AuditWriter, clk clock.Clock) (*limitServiceDeps, error) {
	limitRepo := postgres.NewLimitRepository(postgresConn)
	usageCounterRepo := postgres.NewUsageCounterRepository(postgresConn)

	createLimitCmd, err := command.NewCreateLimitCommand(limitRepo, clk, auditWriter)
	if err != nil {
		return nil, fmt.Errorf("failed to create limit command: %w", err)
	}

	updateLimitCmd, err := command.NewUpdateLimitCommand(limitRepo, clk, auditWriter)
	if err != nil {
		return nil, fmt.Errorf("failed to create update limit command: %w", err)
	}

	activateLimitCmd, err := command.NewActivateLimitCommand(limitRepo, clk, auditWriter)
	if err != nil {
		return nil, fmt.Errorf("failed to create activate limit command: %w", err)
	}

	deactivateLimitCmd, err := command.NewDeactivateLimitCommand(limitRepo, clk, auditWriter)
	if err != nil {
		return nil, fmt.Errorf("failed to create deactivate limit command: %w", err)
	}

	draftLimitCmd, err := command.NewDraftLimitCommand(limitRepo, clk, auditWriter)
	if err != nil {
		return nil, fmt.Errorf("failed to create draft limit command: %w", err)
	}

	deleteLimitCmd, err := command.NewDeleteLimitCommand(limitRepo, clk, auditWriter)
	if err != nil {
		return nil, fmt.Errorf("failed to create delete limit command: %w", err)
	}

	getLimitQuery := query.NewGetLimitQuery(limitRepo)

	listLimitsQuery, err := query.NewListLimitsQuery(limitRepo)
	if err != nil {
		return nil, fmt.Errorf("failed to create list limits query: %w", err)
	}

	service := services.NewLimitService(createLimitCmd, updateLimitCmd, activateLimitCmd, deactivateLimitCmd, draftLimitCmd, deleteLimitCmd, getLimitQuery, listLimitsQuery, usageCounterRepo)

	return &limitServiceDeps{
		service:          service,
		usageCounterRepo: usageCounterRepo,
		limitRepo:        limitRepo,
	}, nil
}

// initHTTPServer creates the HTTP server with all services wired together.
// Extracted from InitServers to reduce cyclomatic complexity.
func initHTTPServer(
	cfg *Config,
	postgresConn *libPostgres.PostgresConnection,
	limitDeps *limitServiceDeps,
	evaluateRulesQuery *query.EvaluateRulesQuery,
	auditWriter *command.RecordAuditEventCommand,
	auditEventRepo *postgres.AuditEventRepository,
	ruleService *services.RuleService,
	healthChecker *in.HealthChecker,
	logger libLog.Logger,
	telemetry *libOtel.Telemetry,
	clk clock.Clock,
) (*HTTPServer, error) {
	// Init Transaction Validation repository and queries
	transactionValidationRepo := postgres.NewTransactionValidationRepository(postgresConn)
	getTransactionValidationQuery := query.NewGetTransactionValidationQuery(transactionValidationRepo)
	listTransactionValidationsQuery := query.NewListTransactionValidationsQuery(transactionValidationRepo)

	// Init LimitChecker for ValidationService
	limitChecker, err := query.NewLimitChecker(limitDeps.limitRepo, limitDeps.usageCounterRepo, clk)
	if err != nil {
		return nil, fmt.Errorf("failed to create limit checker: %w", err)
	}

	// Init ValidationService with audit writer for SOX/GLBA compliance
	validationService, err := services.NewValidationService(evaluateRulesQuery, limitChecker, transactionValidationRepo, auditWriter, clk)
	if err != nil {
		return nil, err
	}

	// Init Transaction Validation service facade
	transactionValidationService, err := services.NewTransactionValidationService(getTransactionValidationQuery, listTransactionValidationsQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to create transaction validation service: %w", err)
	}

	// Init Audit Event service (read-only per SOX/GLBA requirements)
	auditEventService, err := initAuditEventService(auditEventRepo)
	if err != nil {
		return nil, err
	}

	// Route configuration with CORS settings
	routeConfig := &in.RouteConfig{
		CORSAllowedOrigins:   cfg.CORSAllowedOrigins,
		APIKeyOnlyValidation: cfg.APIKeyOnlyValidation,
	}

	// Create auth guard with all authentication configuration
	authClient := authMiddleware.NewAuthClient(cfg.PluginAuthAddress, cfg.PluginAuthEnabled, &logger)
	authGuard := httpMiddleware.NewAuthGuard(httpMiddleware.AuthGuardConfig{
		APIKey:            cfg.APIKey,
		APIKeyEnabled:     cfg.APIKeyEnabled,
		PluginAuthEnabled: cfg.PluginAuthEnabled,
		AppName:           constant.ApplicationName,
	}, authClient)

	httpApp, err := in.NewRoutes(logger, telemetry, healthChecker, routeConfig, ruleService, limitDeps.service, validationService, transactionValidationService, auditEventService, authGuard, clk)
	if err != nil {
		return nil, fmt.Errorf("failed to create routes: %w", err)
	}

	return NewHTTPServer(cfg, httpApp, logger, telemetry)
}

// initCleanupWorker creates the usage cleanup worker if enabled.
func initCleanupWorker(cfg *Config, usageCounterRepo *postgres.UsageCounterRepository, logger libLog.Logger, clk clock.Clock) (*workers.UsageCleanupWorker, error) {
	cleanupWorkerConfig, err := LoadCleanupWorkerConfig(cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("invalid cleanup worker configuration: %w", err)
	}

	if cleanupWorkerConfig == nil {
		return nil, nil
	}

	cleanupWorker, err := workers.NewUsageCleanupWorker(usageCounterRepo, *cleanupWorkerConfig, logger, clk)
	if err != nil {
		return nil, fmt.Errorf("failed to create cleanup worker: %w", err)
	}

	logger.WithFields(
		"component", "cleanup_worker",
		"cleanup_interval", cleanupWorkerConfig.CleanupInterval.String(),
	).Info("Usage cleanup worker initialized")

	return cleanupWorker, nil
}

// initWorkers initializes all background workers and assembles the Service.
// Extracted from InitServers to reduce cyclomatic complexity.
func initWorkers(
	cfg *Config,
	limitDeps *limitServiceDeps,
	syncWorker *workers.RuleSyncWorker,
	serverAPI *HTTPServer,
	logger libLog.Logger,
	clk clock.Clock,
) (*Service, error) {
	cleanupWorker, err := initCleanupWorker(cfg, limitDeps.usageCounterRepo, logger, clk)
	if err != nil {
		return nil, err
	}

	return &Service{
		HTTPServer:    serverAPI,
		Logger:        logger,
		cleanupWorker: cleanupWorker,
		syncWorker:    syncWorker,
	}, nil
}

// initSyncWorker creates the rule sync worker.
func initSyncWorker(
	cfg *Config,
	ruleCache *cache.RuleCache,
	syncRepo *postgres.RuleSyncRepository,
	celAdapter *cel.Adapter,
	logger libLog.Logger,
) (*workers.RuleSyncWorker, error) {
	syncWorkerConfig, err := LoadRuleSyncWorkerConfig(cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("invalid rule sync worker configuration: %w", err)
	}

	// celCompilerAdapter satisfies workers.ExpressionCompiler (Compile returns (any, error))
	compiler := &celCompilerAdapter{adapter: celAdapter}

	// Configure circuit breaker for DB poll resilience
	cbConfig := workers.DefaultSyncCircuitBreakerConfig()
	cb := resilience.NewCircuitBreaker(cbConfig, logger)

	syncWorker, err := workers.NewRuleSyncWorker(ruleCache, syncRepo, compiler, *syncWorkerConfig, logger, cb, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create rule sync worker: %w", err)
	}

	logger.WithFields(
		"component", "rule_sync_worker",
		"poll_interval", syncWorkerConfig.PollInterval.String(),
		"staleness_threshold", syncWorkerConfig.StalenessThreshold.String(),
		"overlap_buffer", syncWorkerConfig.OverlapBuffer.String(),
		"circuit_breaker.failure_threshold", cbConfig.FailureThresh,
		"circuit_breaker.timeout", cbConfig.Timeout.String(),
	).Info("Rule sync worker initialized with circuit breaker")

	return syncWorker, nil
}

// initAuditEventService initializes the audit event service with all required queries.
// Extracted to reduce cyclomatic complexity of InitServers.
func initAuditEventService(auditEventRepo *postgres.AuditEventRepository) (*services.AuditEventService, error) {
	getAuditEventQuery, err := query.NewGetAuditEventQuery(auditEventRepo)
	if err != nil {
		return nil, fmt.Errorf("failed to create get audit event query: %w", err)
	}

	listAuditEventsQuery, err := query.NewListAuditEventsQuery(auditEventRepo)
	if err != nil {
		return nil, fmt.Errorf("failed to create list audit events query: %w", err)
	}

	verifyAuditEventQuery, err := query.NewVerifyAuditEventQuery(auditEventRepo)
	if err != nil {
		return nil, fmt.Errorf("failed to create verify audit event query: %w", err)
	}

	auditEventService, err := services.NewAuditEventService(getAuditEventQuery, listAuditEventsQuery, verifyAuditEventQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to create audit event service: %w", err)
	}

	return auditEventService, nil
}

// initCoreInfra initializes logger, validates auth config, and sets up OpenTelemetry.
func initCoreInfra(cfg *Config) (libLog.Logger, *libOtel.Telemetry, error) {
	logger, err := libZap.InitializeLoggerWithError()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize logger: %w", err)
	}

	// Validate authentication configuration (fail-fast if misconfigured)
	if err := ValidateAuthConfig(cfg, logger); err != nil {
		return nil, nil, fmt.Errorf("invalid auth configuration: %w", err)
	}

	// Validate Access Manager plugin configuration (fail-fast if misconfigured)
	if err := ValidateAccessManagerConfig(cfg, logger); err != nil {
		return nil, nil, fmt.Errorf("invalid access manager configuration: %w", err)
	}

	// Init OpenTelemetry via lib-commons helper (per Ring standards)
	telemetry, err := libOtel.InitializeTelemetryWithError(&libOtel.TelemetryConfig{
		LibraryName:               cfg.OtelLibraryName,
		ServiceName:               cfg.OtelServiceName,
		ServiceVersion:            cfg.OtelServiceVersion,
		DeploymentEnv:             cfg.OtelDeploymentEnv,
		CollectorExporterEndpoint: cfg.OtelColExporterEndpoint,
		EnableTelemetry:           cfg.EnableTelemetry,
		Logger:                    logger,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize telemetry: %w", err)
	}

	return logger, telemetry, nil
}

// initClock creates a clock instance based on environment configuration.
// If MOCK_TIME env var is set to a valid RFC3339 timestamp, returns a MockClock
// with that fixed time (for integration tests). Otherwise, returns a RealClock.
//
// This allows integration tests to simulate specific times (e.g., 22:00 for nighttime
// PIX limits, Black Friday dates for custom periods) without restarting the server
// multiple times or waiting for real time to pass.
//
// SECURITY: MOCK_TIME is read once at server boot. It cannot be modified via HTTP
// requests, preventing timestamp injection attacks. In production, MOCK_TIME should
// never be set, ensuring the system always uses real time.
func initClock() clock.Clock {
	mockTime := os.Getenv("MOCK_TIME")
	if mockTime == "" {
		return clock.New()
	}

	t, err := time.Parse(time.RFC3339, mockTime)
	if err != nil {
		// Invalid format: fall back to real clock and log warning
		// Don't fail server startup due to misconfigured test env var
		fmt.Fprintf(os.Stderr, "WARNING: Invalid MOCK_TIME format '%s' (expected RFC3339), using real clock\n", mockTime)
		return clock.New()
	}

	fmt.Fprintf(os.Stderr, "INFO: Using MOCK_TIME=%s (test mode)\n", mockTime)

	return clock.NewFixedClock(t)
}

// InitServers initiate http and grpc servers.
func InitServers() (*Service, error) {
	cfg := &Config{}

	if err := libCommons.SetConfigFromEnvVars(cfg); err != nil {
		return nil, err
	}

	logger, telemetry, err := initCoreInfra(cfg)
	if err != nil {
		return nil, err
	}

	// Init PostgreSQL connection pool
	postgresConn, err := initPostgresConnection(cfg, logger)
	if err != nil {
		return nil, err
	}

	// Track initialization success; if initialization fails after Connect(),
	// the deferred cleanup will mark the connection as disconnected to prevent
	// partial resource leaks and avoid using a partially-initialized connection.
	initSuccess := false

	defer func() {
		if !initSuccess && postgresConn != nil {
			postgresConn.Connected = false
		}
	}()

	// Init health checker for readiness probe
	healthChecker := in.NewHealthChecker(postgresConn)

	// Init CEL expression engine
	celAdapter, err := initCELAdapter(cfg, logger)
	if err != nil {
		return nil, err
	}

	// Init Rule repository (shared by rule service and evaluation)
	ruleRepo := postgres.NewRepository(postgresConn)

	// Init Audit Event repository and command (needed by Rule/Limit commands and ValidationService)
	auditEventRepo := postgres.NewAuditEventRepository(postgresConn)
	auditWriter := command.NewRecordAuditEventCommand(auditEventRepo)

	// Init Clock (supports MOCK_TIME for integration tests)
	clk := initClock()

	// Init Rule Cache: warm up from database, compile CEL expressions, wire into evaluation path
	ruleCache := cache.NewRuleCache(clk)
	ruleSyncRepo := postgres.NewRuleSyncRepository(postgresConn)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cacheCompiler := &celCompilerAdapter{adapter: celAdapter}

	rulesLoaded, warmUpDuration, err := cache.WarmUp(ctx, ruleCache, ruleSyncRepo, cacheCompiler, logger, clk)
	if err != nil {
		return nil, fmt.Errorf("failed to warm up rule cache: %w", err)
	}

	logger.Infof("Rule cache warmed up: %d rules in %v", rulesLoaded, warmUpDuration)

	// Init sync worker for background polling (cross-instance consistency)
	syncWorker, err := initSyncWorker(cfg, ruleCache, ruleSyncRepo, celAdapter, logger)
	if err != nil {
		return nil, err
	}

	// Init Rule service with audit writer and rule cache for synchronous cache updates
	ruleService, err := initRuleService(ruleRepo, celAdapter, auditWriter, ruleCache, clk)
	if err != nil {
		return nil, err
	}

	cacheAdapter, err := cache.NewCacheAdapter(ruleCache)
	if err != nil {
		return nil, fmt.Errorf("failed to create cache adapter: %w", err)
	}

	healthChecker.SetCacheHealthProvider(ruleCache)

	// Init Rule Evaluation components
	evalConfig, err := LoadEvaluationConfig(cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("invalid evaluation configuration: %w", err)
	}

	evaluateRulesQuery, err := initEvaluateRulesQuery(cacheAdapter, celAdapter, evalConfig)
	if err != nil {
		return nil, err
	}

	// Init Limit service with audit writer for SOX/GLBA compliance
	limitDeps, err := initLimitService(postgresConn, auditWriter, clk)
	if err != nil {
		return nil, err
	}

	// Init HTTP server with all services
	serverAPI, err := initHTTPServer(cfg, postgresConn, limitDeps, evaluateRulesQuery, auditWriter, auditEventRepo, ruleService, healthChecker, logger, telemetry, clk)
	if err != nil {
		return nil, err
	}

	// Init background workers
	svc, err := initWorkers(cfg, limitDeps, syncWorker, serverAPI, logger, clk)
	if err != nil {
		return nil, err
	}

	// Mark initialization as successful; defer cleanup will not close the connection.
	initSuccess = true

	return svc, nil
}

// runFunctionMigrations executes PostgreSQL function migrations before schema migrations.
// This ensures functions are available for triggers and constraints in the main schema.
func runFunctionMigrations(connectionString string, migrationsPath string, logger libLog.Logger) error {
	if migrationsPath == "" {
		return nil
	}

	functionsPath := fmt.Sprintf("%s/functions", migrationsPath)

	if _, err := os.Stat(functionsPath); os.IsNotExist(err) {
		logger.WithFields("functions_path", functionsPath).Info("No function migrations directory found; skipping function migrations")
		return nil
	}

	logger.WithFields("functions_path", functionsPath).Info("Applying function migrations")

	db, err := sql.Open("pgx", connectionString)
	if err != nil {
		return fmt.Errorf("failed to open database for function migrations: %w", err)
	}
	defer db.Close()

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	migrator := migration.NewFunctionMigrator(db, functionsPath, logger)

	if err := migrator.Up(ctx); err != nil {
		return fmt.Errorf("failed to apply function migrations: %w", err)
	}

	version, _, err := migrator.Version(ctx)
	if err != nil {
		return fmt.Errorf("failed to get migration version: %w", err)
	}

	logger.WithFields("version", version).Info("Function migrations applied successfully")

	return nil
}
