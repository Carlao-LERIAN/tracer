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

	libCommons "github.com/LerianStudio/lib-commons/v2/commons"
	libLog "github.com/LerianStudio/lib-commons/v2/commons/log"
	libOtel "github.com/LerianStudio/lib-commons/v2/commons/opentelemetry"
	libPostgres "github.com/LerianStudio/lib-commons/v2/commons/postgres"
	libZap "github.com/LerianStudio/lib-commons/v2/commons/zap"

	"tracer/internal/adapters/cel"
	"tracer/internal/adapters/http/in"
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
	APIKey        string `env:"API_KEY"`
	APIKeyEnabled bool   `env:"API_KEY_ENABLED"`

	// CORS
	CORSAllowedOrigins string `env:"CORS_ALLOWED_ORIGINS"`

	// CEL Expression Engine
	CELCostLimit    string `env:"CEL_COST_LIMIT"`
	CELCacheMaxSize string `env:"CEL_CACHE_MAX_SIZE"`

	// Rule Evaluation Feature Flags
	DefaultDecisionWhenNoMatch string `env:"DEFAULT_DECISION_WHEN_NO_MATCH"`
	MaxRulesPerRequest         string `env:"MAX_RULES_PER_REQUEST"`

	// Usage Counter Cleanup Worker
	// CleanupWorkerEnabled enables/disables the background cleanup worker (default: false)
	// Set CLEANUP_WORKER_ENABLED=true in environment to enable the worker
	CleanupWorkerEnabled bool `env:"CLEANUP_WORKER_ENABLED"`
	// CleanupIntervalHours is the interval between cleanup runs in hours (default: 24)
	CleanupIntervalHours string `env:"CLEANUP_INTERVAL_HOURS"`
	// CleanupRetentionDays is how many days to retain usage counters (default: 90)
	CleanupRetentionDays string `env:"CLEANUP_RETENTION_DAYS"`
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

// parseCELCacheMaxSize parses the CEL cache max size from string to int64.
// Returns default value (1000) if empty.
// Returns error if value is invalid or non-positive.
func parseCELCacheMaxSize(s string) (int64, error) {
	const defaultValue int64 = 1000

	if s == "" {
		return defaultValue, nil
	}

	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid CEL_CACHE_MAX_SIZE value '%s': %w", s, err)
	}

	if v <= 0 {
		return 0, fmt.Errorf("CEL_CACHE_MAX_SIZE must be positive, got %d", v)
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

// parseCleanupRetentionDays parses the retention period from string to time.Duration.
// Returns default value (90 days) if empty.
// Returns error if value is invalid, non-positive, or exceeds maximum.
func parseCleanupRetentionDays(s string) (time.Duration, error) {
	const defaultDays = 90

	// maxAllowedDays limits retention period to 10 years (3650 days).
	// This prevents unbounded data growth while allowing long retention for compliance.
	const maxAllowedDays = 3650

	if s == "" {
		return time.Duration(defaultDays) * 24 * time.Hour, nil
	}

	days, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid CLEANUP_RETENTION_DAYS value '%s': %w", s, err)
	}

	if days <= 0 {
		return 0, fmt.Errorf("CLEANUP_RETENTION_DAYS must be positive, got %d", days)
	}

	if days > maxAllowedDays {
		return 0, fmt.Errorf("CLEANUP_RETENTION_DAYS exceeds maximum allowed (%d days = 10 years), got %d", maxAllowedDays, days)
	}

	return time.Duration(days) * 24 * time.Hour, nil
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

	retentionPeriod, err := parseCleanupRetentionDays(cfg.CleanupRetentionDays)
	if err != nil {
		return nil, fmt.Errorf("invalid CLEANUP_RETENTION_DAYS: %w", err)
	}

	logger.WithFields(
		"cleanup_interval", cleanupInterval.String(),
		"retention_period", retentionPeriod.String(),
	).Info("Usage counter cleanup worker configuration loaded")

	return &workers.UsageCleanupWorkerConfig{
		CleanupInterval: cleanupInterval,
		RetentionPeriod: retentionPeriod,
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

	celCacheMaxSize, err := parseCELCacheMaxSize(cfg.CELCacheMaxSize)
	if err != nil {
		return nil, fmt.Errorf("invalid cache configuration: %w", err)
	}

	adapter, err := cel.NewAdapter(cel.AdapterConfig{
		CostLimit:    celCostLimit,
		CacheMaxSize: celCacheMaxSize,
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
func initRuleService(ruleRepo *postgres.Repository, celAdapter *cel.Adapter, auditWriter command.AuditWriter) (*services.RuleService, error) {
	celCompiler := &celCompilerAdapter{adapter: celAdapter}
	clk := clock.New()

	// Inject audit writer into all Rule commands for SOX/GLBA compliance
	createRuleCmd := command.NewCreateRuleCommand(ruleRepo, celCompiler, clk, auditWriter)
	updateRuleCmd := command.NewUpdateRuleCommand(ruleRepo, celCompiler, clk, auditWriter)

	activateRuleCmd, err := command.NewActivateRuleService(ruleRepo, celCompiler, clk, auditWriter)
	if err != nil {
		return nil, fmt.Errorf("failed to create activate rule service: %w", err)
	}

	deactivateRuleCmd := command.NewDeactivateRuleService(ruleRepo, clk, auditWriter)

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
func initLimitService(postgresConn *libPostgres.PostgresConnection, auditWriter command.AuditWriter) (*limitServiceDeps, error) {
	limitRepo := postgres.NewLimitRepository(postgresConn)
	usageCounterRepo := postgres.NewUsageCounterRepository(postgresConn)

	// Inject audit writer into all Limit commands for SOX/GLBA compliance
	clk := clock.New()

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

// initCleanupWorker creates the usage cleanup worker if enabled.
func initCleanupWorker(cfg *Config, usageCounterRepo *postgres.UsageCounterRepository, logger libLog.Logger) (*workers.UsageCleanupWorker, error) {
	cleanupWorkerConfig, err := LoadCleanupWorkerConfig(cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("invalid cleanup worker configuration: %w", err)
	}

	if cleanupWorkerConfig == nil {
		return nil, nil
	}

	cleanupWorker, err := workers.NewUsageCleanupWorker(usageCounterRepo, *cleanupWorkerConfig, logger, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create cleanup worker: %w", err)
	}

	logger.WithFields(
		"component", "cleanup_worker",
		"cleanup_interval", cleanupWorkerConfig.CleanupInterval.String(),
		"retention_period", cleanupWorkerConfig.RetentionPeriod.String(),
	).Info("Usage cleanup worker initialized")

	return cleanupWorker, nil
}

// initWorkers initializes all background workers and assembles the Service.
// Extracted from InitServers to reduce cyclomatic complexity.
func initWorkers(
	cfg *Config,
	limitDeps *limitServiceDeps,
	ruleCache *cache.RuleCache,
	ruleSyncRepo *postgres.RuleSyncRepository,
	celAdapter *cel.Adapter,
	serverAPI *HTTPServer,
	logger libLog.Logger,
) (*Service, error) {
	cleanupWorker, err := initCleanupWorker(cfg, limitDeps.usageCounterRepo, logger)
	if err != nil {
		return nil, err
	}

	syncWorker, err := initSyncWorker(ruleCache, ruleSyncRepo, celAdapter, logger)
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
// TODO: read sync worker settings from env vars (PollInterval, StalenessThreshold, OverlapBuffer).
func initSyncWorker(
	ruleCache *cache.RuleCache,
	syncRepo *postgres.RuleSyncRepository,
	celAdapter *cel.Adapter,
	logger libLog.Logger,
) (*workers.RuleSyncWorker, error) {
	syncWorkerConfig := workers.DefaultRuleSyncWorkerConfig()

	// celCompilerAdapter satisfies workers.ExpressionCompiler (Compile returns (any, error))
	compiler := &celCompilerAdapter{adapter: celAdapter}

	syncWorker, err := workers.NewRuleSyncWorker(ruleCache, syncRepo, compiler, syncWorkerConfig, logger, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create rule sync worker: %w", err)
	}

	logger.WithFields(
		"component", "rule_sync_worker",
		"poll_interval", syncWorkerConfig.PollInterval.String(),
		"staleness_threshold", syncWorkerConfig.StalenessThreshold.String(),
		"overlap_buffer", syncWorkerConfig.OverlapBuffer.String(),
	).Info("Rule sync worker initialized")

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

// InitServers initiate http and grpc servers.
func InitServers() (*Service, error) {
	cfg := &Config{}

	if err := libCommons.SetConfigFromEnvVars(cfg); err != nil {
		return nil, err
	}

	logger := libZap.InitializeLogger()

	// Validate authentication configuration (fail-fast if misconfigured)
	if err := ValidateAuthConfig(cfg, logger); err != nil {
		return nil, fmt.Errorf("invalid auth configuration: %w", err)
	}

	// Init OpenTelemetry via lib-commons helper (per Ring standards)
	telemetry := libOtel.InitializeTelemetry(&libOtel.TelemetryConfig{
		LibraryName:               cfg.OtelLibraryName,
		ServiceName:               cfg.OtelServiceName,
		ServiceVersion:            cfg.OtelServiceVersion,
		DeploymentEnv:             cfg.OtelDeploymentEnv,
		CollectorExporterEndpoint: cfg.OtelColExporterEndpoint,
		EnableTelemetry:           cfg.EnableTelemetry,
		Logger:                    logger,
	})

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

	// Init Rule service with audit writer for SOX/GLBA compliance
	ruleService, err := initRuleService(ruleRepo, celAdapter, auditWriter)
	if err != nil {
		return nil, err
	}

	// Init Rule Cache: warm up from database, compile CEL expressions, wire into evaluation path
	clk := clock.New()
	ruleCache := cache.NewRuleCache(clk)
	ruleSyncRepo := postgres.NewRuleSyncRepository(postgresConn)

	ctx := context.Background()

	cacheCompiler := &celCompilerAdapter{adapter: celAdapter}

	rulesLoaded, warmUpDuration, err := cache.WarmUp(ctx, ruleCache, ruleSyncRepo, cacheCompiler, logger, clk)
	if err != nil {
		return nil, fmt.Errorf("failed to warm up rule cache: %w", err)
	}

	logger.Infof("Rule cache warmed up: %d rules in %v", rulesLoaded, warmUpDuration)

	cacheAdapter := cache.NewCacheAdapter(ruleCache)
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
	limitDeps, err := initLimitService(postgresConn, auditWriter)
	if err != nil {
		return nil, err
	}

	// Init Transaction Validation repository and queries
	transactionValidationRepo := postgres.NewTransactionValidationRepository(postgresConn)
	getTransactionValidationQuery := query.NewGetTransactionValidationQuery(transactionValidationRepo)
	listTransactionValidationsQuery := query.NewListTransactionValidationsQuery(transactionValidationRepo)

	// Init LimitChecker for ValidationService
	limitChecker, err := query.NewLimitChecker(limitDeps.limitRepo, limitDeps.usageCounterRepo)
	if err != nil {
		return nil, fmt.Errorf("failed to create limit checker: %w", err)
	}

	// Init ValidationService with audit writer for SOX/GLBA compliance
	validationService, err := services.NewValidationService(evaluateRulesQuery, limitChecker, transactionValidationRepo, auditWriter)
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

	// Route configuration with API key authentication and CORS settings
	routeConfig := &in.RouteConfig{
		APIKey:             cfg.APIKey,
		APIKeyEnabled:      cfg.APIKeyEnabled,
		CORSAllowedOrigins: cfg.CORSAllowedOrigins,
	}

	httpApp := in.NewRoutes(logger, telemetry, healthChecker, routeConfig, ruleService, limitDeps.service, validationService, transactionValidationService, auditEventService)

	serverAPI, err := NewHTTPServer(cfg, httpApp, logger, telemetry)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP server: %w", err)
	}

	// Init background workers
	svc, err := initWorkers(cfg, limitDeps, ruleCache, ruleSyncRepo, celAdapter, serverAPI, logger)
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
