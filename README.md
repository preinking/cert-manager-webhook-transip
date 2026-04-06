# cert-manager-webhook-transip

A [cert-manager](https://cert-manager.io/) webhook for DNS01 challenge verification with [TransIP](https://www.transip.nl/) as DNS provider.

## Installation

### Helm

```bash
git clone https://github.com/preinking/cert-manager-webhook-transip.git
helm install cert-manager-webhook-transip --namespace=cert-manager ./charts/cert-manager-webhook-transip
```

### kubectl

```bash
kubectl -n cert-manager apply -f https://raw.githubusercontent.com/preinking/cert-manager-webhook-transip/main/deploy/recommended.yaml
```

## Configuration

The webhook needs your TransIP account name and API private key.

### 1. Create the secret

```bash
# Given your private key is in the file privateKey
kubectl -n cert-manager create secret generic transip-credentials --from-file=privateKey
```

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

## Running tests

Configure your TransIP credentials in `testdata/transip/config.json`, then:

```bash
TEST_ZONE_NAME=example.com go test .
```

## Building

```bash
# Build Docker image
make build

# Create a patch release (bump version, build, push)
make patch-release
```

The `REGISTRY_HOST` and `USERNAME` variables in `Makefile.mk` can be overridden:

```bash
make build REGISTRY_HOST=ghcr.io USERNAME=yourname
```
