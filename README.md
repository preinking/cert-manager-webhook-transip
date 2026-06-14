# cert-manager-webhook-transip

A [cert-manager](https://cert-manager.io/) DNS01 solver webhook that uses
[TransIP](https://www.transip.nl/) as the DNS provider. It lets cert-manager
issue and renew certificates — including wildcards — for domains whose DNS is
hosted at TransIP, by automatically creating and removing the `_acme-challenge`
`TXT` records that ACME DNS01 validation requires.

## Features

- **DNS01 challenge solving** for any domain hosted in your TransIP account.
- **Wildcard certificates** (`*.example.com`) — only possible via DNS01.
- **Idempotent and concurrency-safe** — tolerates retries and parallel
  validations for the same domain.
- **Per-issuer credentials** — reference a Kubernetes `Secret` per `Issuer` /
  `ClusterIssuer`.
- **Self-bootstrapping TLS** — the Helm chart provisions the webhook's own
  serving certificates via cert-manager.
- Ships as a small static container and a Helm chart.

## Contents

- [Prerequisites](#prerequisites)
- [Installation](#installation)
- [Quick start](#quick-start)
- [Configuration reference](#configuration-reference)
- [Verifying it works](#verifying-it-works)
- [Troubleshooting](#troubleshooting)
- [Running tests](#running-tests)
- [Building and releasing](#building-and-releasing)
- [Documentation](#documentation)
- [Contributing](#contributing)
- [License](#license)

## Prerequisites

| Requirement | Notes |
|-------------|-------|
| A Kubernetes cluster | Tested against cert-manager `v1.20.x`. |
| [cert-manager](https://cert-manager.io/docs/installation/) installed | Must be running and healthy **before** installing this webhook. |
| A TransIP account | With the domain(s) you want certificates for hosted there. |
| A TransIP API private key | Generated in the TransIP control panel (Account → API). Note the **account name** (your TransIP username). |
| Helm 3 or `kubectl` | For installation. |

> The webhook is installed into the `cert-manager` namespace by default and
> assumes cert-manager runs there too.

## Installation

### Helm

```bash
git clone https://github.com/preinking/cert-manager-webhook-transip.git
helm install cert-manager-webhook-transip \
  --namespace cert-manager \
  ./cert-manager-webhook-transip/charts/cert-manager-webhook-transip
```

### kubectl

```bash
kubectl -n cert-manager apply \
  -f https://raw.githubusercontent.com/preinking/cert-manager-webhook-transip/main/deploy/recommended.yaml
```

Verify the webhook registered itself with the Kubernetes API aggregation layer:

```bash
kubectl get apiservice v1alpha1.cert-manager.webhook.transip
# NAME                                     SERVICE                                       AVAILABLE   AGE
# v1alpha1.cert-manager.webhook.transip    cert-manager/...-cert-manager-webhook-transip True        1m
```

`AVAILABLE` must be `True` before issuance will work.

## Quick start

This is the most common path: a `ClusterIssuer` backed by Let's Encrypt
production, then a wildcard certificate.

### 1. Store your TransIP API key in a Secret

```bash
# Your TransIP API private key is in a local file named "privateKey"
kubectl -n cert-manager create secret generic transip-credentials \
  --from-file=privateKey
```

The webhook reads this Secret from the **challenge's resource namespace**. For a
`ClusterIssuer` that is the `cert-manager` namespace (as above). See
[Configuration reference](#configuration-reference) for the namespaced-`Issuer`
case.

### 2. Create a ClusterIssuer

```yaml
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt-production
spec:
  acme:
    email: user@example.com
    server: https://acme-v02.api.letsencrypt.org/directory
    privateKeySecretRef:
      name: letsencrypt-production-issuer-key
    solvers:
      - dns01:
          webhook:
            groupName: cert-manager.webhook.transip
            solverName: transip
            config:
              accountName: your-transip-username
              ttl: 300
              privateKeySecretRef:
                name: transip-credentials
                key: privateKey
```

> **Tip:** while testing, point `server` at the Let's Encrypt **staging**
> endpoint (`https://acme-staging-v02.api.letsencrypt.org/directory`) to avoid
> hitting production rate limits. Switch to production once issuance succeeds.

### 3. Request a certificate

```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: example-wildcard
  namespace: default
spec:
  secretName: example-wildcard-tls
  issuerRef:
    name: letsencrypt-production
    kind: ClusterIssuer
  dnsNames:
    - "*.example.com"
    - example.com
```

## Configuration reference

These keys go in the issuer's `solvers[].dns01.webhook.config` block:

| Key | Type | Required | Description |
|-----|------|----------|-------------|
| `accountName` | string | yes | Your TransIP username. |
| `privateKeySecretRef` | object | yes | `{ name, key }` referencing the Secret that holds the TransIP API private key. **Recommended** over inlining. |
| `privateKey` | string | no | The API private key inline. Avoid — keep keys in a Secret. |
| `ttl` | integer | no | TTL (seconds) for the challenge `TXT` record. Defaults to `0`; set explicitly, e.g. `300`. |

And the top-level `webhook` fields cert-manager requires:

| Key | Value |
|-----|-------|
| `groupName` | `cert-manager.webhook.transip` (must match the chart's `groupName`). |
| `solverName` | `transip` |

### Credentials and namespaces

The webhook fetches the key Secret from the challenge's **resource namespace**:

- **`ClusterIssuer`** → the cluster resource namespace
  (`--cluster-resource-namespace`), which the standard cert-manager Helm install
  sets to the `cert-manager` namespace. The chart grants the webhook permission
  to read Secrets there. This is the common case.
- **Namespaced `Issuer`** → the `Issuer`'s own namespace. The chart does **not**
  grant Secret-read access outside `cert-manager` by default, so place the Secret
  in `cert-manager`, or grant the webhook's ServiceAccount `get secrets` in that
  namespace.

### Chart values

Common `values.yaml` overrides:

| Value | Default | Purpose |
|-------|---------|---------|
| `groupName` | `cert-manager.webhook.transip` | API group; must match the issuer `groupName`. |
| `replicaCount` | `1` | Webhook replicas (stateless). |
| `certManager.namespace` | `cert-manager` | Namespace cert-manager runs in. |
| `certManager.serviceAccountName` | `cert-manager` | cert-manager's ServiceAccount (granted permission to call the solver). |
| `image.repository` / `image.tag` | `preinking/cert-manager-webhook-transip` / `v1.20.4` | Container image. |
| `image.privateRegistrySecretName` | `""` | `imagePullSecret` name for a private registry. |
| `resources`, `nodeSelector`, `tolerations`, `affinity` | `{}` / `[]` | Standard scheduling/limits knobs. |

## Verifying it works

```bash
# Watch the certificate become Ready
kubectl -n default get certificate example-wildcard -w

# If it stalls, inspect the challenge
kubectl get challenges -A
kubectl describe challenge -n default <challenge-name>
```

The resulting TLS material lands in the `secretName` you specified
(`example-wildcard-tls`).

## Troubleshooting

| Symptom | Likely cause | What to check |
|---------|--------------|---------------|
| `apiservice ... AVAILABLE: False` | Webhook pod not ready, or serving cert / CA injection incomplete | Pod status; the `*-webhook-tls` Secret exists; cert-manager + ca-injector are healthy. |
| Challenge stays `pending`, webhook never logs | cert-manager not authorized to call the solver | `*-domain-solver` ClusterRoleBinding targets the correct cert-manager ServiceAccount/namespace. |
| `no private key for <key> in secret ...` | Wrong Secret name/key, or wrong namespace | Secret exists in the resource namespace (`cert-manager` for ClusterIssuer); `key` matches. |
| Stuck "propagating" / DNS self-check fails | DNS not yet visible (this is cert-manager waiting, not the webhook) | The `TXT` record exists at TransIP; the cluster can resolve public DNS. |
| TransIP API auth or zone errors | Bad `accountName`/key, or domain not at TransIP | Credentials valid; the domain is actually hosted in this TransIP account. |

Raise webhook log verbosity to see per-step traces:

```bash
kubectl -n cert-manager logs deploy/<release>-cert-manager-webhook-transip
```

For internals and the full request flow, see the
[architecture documentation](docs/architecture.md).

## Running tests

The conformance suite runs the real solver against your TransIP account, so it
needs live credentials. Put a valid config snippet in
`testdata/transip/config.json`, then:

```bash
TEST_ZONE_NAME=example.com. go test .
```

> `TEST_ZONE_NAME` must be a zone you control at TransIP, with a trailing dot.
> The suite creates and deletes real `TXT` records.

## Building and releasing

```bash
# Build the Docker image
make build

# Cut a patch release (bump version, build, push)
make patch-release      # or: make minor-release / make major-release
```

Override the registry and image owner:

```bash
make build REGISTRY_HOST=ghcr.io USERNAME=yourname
```

Versioning is tracked in `.release` and `charts/.../Chart.yaml`.

## Documentation

- [Architecture](docs/architecture.md) — how the webhook integrates with
  cert-manager and the Kubernetes aggregation layer, the request flow, the RBAC
  and TLS model, and failure modes.
- [cert-manager DNS01 webhook docs](https://cert-manager.io/docs/configuration/acme/dns01/webhook/)
- [TransIP API](https://api.transip.nl/)

## Contributing

Issues and pull requests are welcome.

1. Fork and create a feature branch.
2. Make your change. If it touches solver logic, add or update tests.
3. Run `go build ./...` and `go vet ./...`; run `go test .` against a test zone
   if your change affects DNS handling.
4. Keep the README and `docs/architecture.md` in sync with behavioral changes.
5. Open a pull request describing the change and how you verified it.

## License

Licensed under the [Apache License 2.0](LICENSE).
