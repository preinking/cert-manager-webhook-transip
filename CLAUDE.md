# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a cert-manager webhook for TransIP DNS provider, enabling DNS01 ACME challenges for Let's Encrypt certificate validation. The webhook integrates with cert-manager running in Kubernetes and uses the TransIP API to manage DNS TXT records required for domain validation.

## Architecture

### Core Components

**main.go** - The webhook server implementation:
- `transipDNSProviderSolver` - Implements the cert-manager webhook interface
- `Present()` - Creates TXT records in TransIP DNS for ACME challenges
- `CleanUp()` - Removes TXT records after validation
- `Initialize()` - Sets up Kubernetes client for secret access
- `NewTransipClient()` - Creates TransIP API client with credentials from Kubernetes secrets

The webhook retrieves TransIP API credentials from Kubernetes secrets (referenced in the Issuer config) and uses the gotransip library to interact with the TransIP API.

**Configuration Flow:**
1. Cert-manager calls the webhook with a `ChallengeRequest` containing domain info
2. Webhook loads config from the request, including `accountName` and `privateKeySecretRef`
3. Webhook retrieves the private key from the Kubernetes secret
4. Creates a TransIP API client and adds/removes DNS TXT records
5. Cert-manager verifies the DNS record and proceeds with certificate issuance

**Key Helper Functions:**
- `extractDomainName()` - Extracts the base domain from FQDN using DNS resolution (main.go:262)
- `extractRecordName()` - Extracts the subdomain portion for the DNS record (main.go:255)
- `loadConfig()` - Decodes the Issuer configuration JSON (main.go:242)

### Deployment

The webhook can be deployed via:
- **Helm chart** in `charts/cert-manager-webhook-transip/` - Preferred method
- **kubectl apply** using `deploy/recommended.yaml` - Direct YAML deployment

The Helm chart creates:
- Deployment running the webhook container
- Service exposing the webhook API
- APIService registering the webhook with cert-manager
- RBAC resources for accessing secrets

Configuration in `values.yaml`:
- `groupName: cert-manager.webhook.transip` - Must match the GROUP_NAME env var and Issuer webhook config
- Image version tracks cert-manager compatibility (currently 1.17.2 LTS)

## Development Commands

### Building

Build Docker image:
```bash
make build
# Builds: docker.io/quanby/cert-manager-webhook-transip:<VERSION>
```

The Makefile uses versioning from `.release` file. Version is managed via:
- `make patch-release` - Increment patch version (0.0.X)
- `make minor-release` - Increment minor version (0.X.0)
- `make major-release` - Increment major version (X.0.0)

Each release command builds, tags, and pushes to Docker registry.

### Testing

Run the test suite (requires TransIP credentials):
```bash
TEST_ZONE_NAME=example.com go test .
```

Test configuration:
- Create `testdata/transip/config.json` with your TransIP account name and base64-encoded private key
- Set `TEST_ZONE_NAME` to a domain you own in TransIP
- Tests use the cert-manager conformance test suite (main_test.go:16)

### Local Development

Build webhook binary:
```bash
CGO_ENABLED=0 go build -o webhook -ldflags '-w -extldflags "-static"' .
```

The webhook requires:
- `GROUP_NAME` environment variable (e.g., "cert-manager.webhook.transip")
- Access to Kubernetes API for fetching secrets
- Cert-manager running in the cluster

## Dependencies

- **Go 1.23.0+** (see go.mod)
- **cert-manager v1.17.2 LTS** - Webhook framework and ACME DNS solver interfaces
- **gotransip/v6** - TransIP API client library
- **Kubernetes client-go** - For accessing secrets in the cluster

## Important Notes

- The webhook must be deployed in the same namespace as cert-manager (typically `cert-manager`)
- TransIP private keys are stored as Kubernetes secrets and referenced in the Issuer config
- The `groupName` in values.yaml must match the `webhook.groupName` in Issuer manifests
- TTL for DNS records defaults to what's specified in the Issuer config (typically 300 seconds)
- The webhook tolerates idempotent calls - calling `Present()` multiple times with the same challenge is safe
