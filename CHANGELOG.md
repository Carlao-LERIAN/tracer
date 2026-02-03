# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2026-01-30

This is the first public release of Tracer, a real-time transaction validation and fraud prevention API for financial systems.

### Added

- **Transaction Validation API** - Real-time ALLOW/DENY/REVIEW decisions with <80ms p99 latency
- **Expression-based Rules** - Type-safe rule engine using CEL (Common Expression Language)
- **Flexible Spending Limits** - Daily, monthly, and per-transaction limits with scope-based application (account, segment, portfolio, merchant, transaction type)
- **Complete Audit Trail** - SOX/GLBA compliant with hash chain integrity and 7-year retention policy
- **Rule Management** - Create, activate, deactivate, update, and delete validation rules with draft/active status workflow
- **Limit Management** - Configure and manage spending limits with real-time usage tracking
- **REST API** - Comprehensive OpenAPI 3.0 documented endpoints for all operations
- **Authentication** - API key-based authentication with secure key management
- **PostgreSQL Persistence** - Robust data storage with database migrations and connection pooling
- **Observability** - Structured logging with slog, OpenTelemetry tracing, and Prometheus metrics
- **Health Checks** - Liveness and readiness endpoints for orchestration integration
- **Docker Support** - Production-ready containerization with distroless base images

### Security

- Input validation on all API endpoints with detailed error messages (TRC-* error codes)
- SQL injection protection via parameterized queries
- TLS 1.2+ enforcement for encrypted communications
- Rate limiting and request size constraints (100KB max payload)
- Audit trail immutability with PostgreSQL rules preventing UPDATE/DELETE operations
- Hash chain integrity for tamper detection in audit logs
