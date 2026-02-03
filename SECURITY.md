# Security Policy

## Reporting a Vulnerability

We take security vulnerabilities seriously. If you discover a security issue, please report it responsibly.

### How to Report

You can report security vulnerabilities through:

1. **GitHub Security Advisory** (Preferred)
   - Go to the **Security** tab of this repository
   - Click **"Report a vulnerability"**
   - This allows private discussion until a fix is ready

2. **Email**
   - Send details to: [security@lerian.studio](mailto:security@lerian.studio)
   - PGP key available for encrypted communications
   - We recommend [Mailvelope](https://mailvelope.com/en) for email encryption

**Please do NOT disclose the vulnerability publicly until we have addressed it.**

## Response Timeline

| Action | Timeframe |
|--------|-----------|
| Acknowledgment | Within 24 hours |
| Initial assessment | Within 72 hours |
| Status update | Within 7 days |
| Resolution target | Within 90 days (severity dependent) |

## Disclosure Process

1. **Initial Contact**: You submit vulnerability via GitHub Advisory or email
2. **Acknowledgment**: We confirm receipt within 24 hours
3. **Verification**: Our security team verifies the vulnerability
4. **Assessment**: We determine severity and potential impact
5. **Resolution**: We develop and deploy a fix
6. **Notification**: We inform you of the resolution
7. **Public Disclosure**: We coordinate with you to disclose responsibly

## Supported Versions

| Version | Supported |
|---------|-----------|
| Latest release | Yes |
| Previous minor | Security fixes only |
| Older versions | No |

We recommend always running the latest version.

## Scope

### In Scope
- Authentication and authorization vulnerabilities
- Data exposure or leakage
- Injection vulnerabilities (SQL, command, etc.)
- Cryptographic issues
- Business logic flaws

### Out of Scope
- Denial of service (DoS) attacks
- Social engineering
- Physical security
- Issues in dependencies (report to upstream)

## Security Best Practices

When deploying this application:

- **Never hardcode secrets** - Use environment variables or secrets management (e.g., HashiCorp Vault)
- **Keep updated** - Regularly update to the latest version
- **Secure configuration** - Follow our documentation for secure setup
- **Network security** - Use TLS, firewalls, and network segmentation
- **Access control** - Apply principle of least privilege

## Secrets Management

### Development Environment

For local development, use the `.env` file (copied from `.env.example`). This file is gitignored and should never be committed.

### Production Environment

**⚠️ NEVER use plain text secrets in production.**

Use an external secrets manager to inject secrets at runtime:

| Solution | Use Case | Documentation |
|----------|----------|---------------|
| **HashiCorp Vault** | Self-hosted, multi-cloud | <https://www.vaultproject.io/> |
| **AWS Secrets Manager** | AWS-native workloads | <https://aws.amazon.com/secrets-manager/> |
| **Azure Key Vault** | Azure-native workloads | <https://azure.microsoft.com/services/key-vault/> |
| **GCP Secret Manager** | GCP-native workloads | <https://cloud.google.com/secret-manager> |
| **External Secrets Operator** | Kubernetes (any cloud) | <https://external-secrets.io/> |

### Kubernetes Deployment

For Kubernetes deployments, we recommend using [External Secrets Operator](https://external-secrets.io/) to sync secrets from your cloud provider's secret manager:

```yaml
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: tracer-secrets
spec:
  refreshInterval: 1h
  secretStoreRef:
    name: vault-backend  # or aws-secretsmanager, azure-keyvault, etc.
    kind: ClusterSecretStore
  target:
    name: tracer-secrets
  data:
    - secretKey: API_KEY
      remoteRef:
        key: tracer/production
        property: api_key
    - secretKey: DB_PASSWORD
      remoteRef:
        key: tracer/production
        property: db_password
```

### Secrets Checklist

Before deploying to production, verify:

- [ ] No secrets in source code or git history
- [ ] `.env` file is in `.gitignore`
- [ ] `API_KEY` is at least 32 characters and randomly generated
- [ ] `DB_PASSWORD` is strong and unique
- [ ] Secrets are rotated regularly (recommended: 90 days)
- [ ] Access to secrets is audited and logged

## Recognition

We appreciate security researchers who help keep our project secure. With your permission, we'll acknowledge your contribution in our release notes.

## Contact

- **Security Email**: [security@lerian.studio](mailto:security@lerian.studio)
- **General Issues**: Use GitHub Issues for non-security bugs
- **Discussions**: [GitHub Discussions](https://github.com/LerianStudio/tracer/discussions)
