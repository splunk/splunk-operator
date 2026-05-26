---
title: Validation Webhook
parent: Reference
nav_order: 5
---

# Validation Webhook

The Splunk Operator includes an optional validation webhook that validates Splunk Enterprise Custom Resource (CR) specifications before they are persisted to the Kubernetes API server. This provides immediate feedback when invalid configurations are submitted.

## Overview

The validation webhook intercepts CREATE and UPDATE operations on Splunk Enterprise CRDs and validates the spec fields according to predefined rules. If validation fails, the request is rejected with a descriptive error message.

### Supported CRDs

The webhook validates the following Custom Resource Definitions:

- Standalone
- IndexerCluster
- SearchHeadCluster
- ClusterManager
- LicenseManager
- MonitoringConsole

## Enabling the Validation Webhook

The validation webhook is **disabled by default** and must be explicitly enabled. This is an opt-in feature for the v4 API.

### Prerequisites

Before enabling the webhook, you need TLS certificates for the webhook server. You have two options:

#### Option A: Use cert-manager (Recommended)

Install cert-manager to automatically manage TLS certificates:

```bash
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.14.0/cert-manager.yaml
kubectl wait --for=condition=Available --timeout=300s deployment/cert-manager -n cert-manager
kubectl wait --for=condition=Available --timeout=300s deployment/cert-manager-webhook -n cert-manager
```

#### Option B: Use Your Own Certificates

If you prefer not to use cert-manager, you can provide your own TLS certificates:

1. **Generate certificates** for the webhook service. The certificate must have:
   - **Common Name (CN):** `splunk-operator-webhook-service.splunk-operator.svc`
   - **Subject Alternative Names (SANs):**
     - `splunk-operator-webhook-service.splunk-operator.svc`
     - `splunk-operator-webhook-service.splunk-operator.svc.cluster.local`

   Example using OpenSSL:
   ```bash
   # Generate CA
   openssl genrsa -out ca.key 2048
   openssl req -x509 -new -nodes -key ca.key -days 365 -out ca.crt -subj "/CN=splunk-webhook-ca"

   # Generate server key and CSR
   openssl genrsa -out tls.key 2048
   openssl req -new -key tls.key -out server.csr -subj "/CN=splunk-operator-webhook-service.splunk-operator.svc" \
     -config <(cat /etc/ssl/openssl.cnf <(printf "\n[SAN]\nsubjectAltName=DNS:splunk-operator-webhook-service.splunk-operator.svc,DNS:splunk-operator-webhook-service.splunk-operator.svc.cluster.local"))

   # Sign the certificate
   openssl x509 -req -in server.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out tls.crt -days 365 \
     -extensions SAN -extfile <(cat /etc/ssl/openssl.cnf <(printf "\n[SAN]\nsubjectAltName=DNS:splunk-operator-webhook-service.splunk-operator.svc,DNS:splunk-operator-webhook-service.splunk-operator.svc.cluster.local"))
   ```

2. **Create the webhook certificate Secret:**
   ```bash
   kubectl create secret tls webhook-server-cert \
     --cert=tls.crt \
     --key=tls.key \
     -n splunk-operator
   ```

3. **Inject the CA bundle** into the ValidatingWebhookConfiguration:
   ```bash
   # Get base64-encoded CA certificate
   CA_BUNDLE=$(cat ca.crt | base64 | tr -d '\n')

   # Patch the webhook configuration
   kubectl patch validatingwebhookconfiguration splunk-operator-validating-webhook-configuration \
     --type='json' -p="[{'op': 'replace', 'path': '/webhooks/0/clientConfig/caBundle', 'value': '${CA_BUNDLE}'}]"
   ```

4. **Deploy without cert-manager:** Use the `config/default-with-webhook` overlay but skip the certmanager components, or manually deploy the webhook components.

### Deployment Options

#### Option 1: Enable via Helm Feature Gates

If deploying with Helm, enable the feature gate through the `splunkOperator.featureGates` value:

```bash
helm install splunk-operator splunk/splunk-operator \
  --set splunkOperator.featureGates.ValidationWebhook=true
```

Or in your values file:

```yaml
splunkOperator:
  featureGates:
    ValidationWebhook: true
```

**Note:** This requires the webhook Kubernetes resources (Service, ValidatingWebhookConfiguration, TLS certificates) to be deployed separately.

#### Option 2: Use the Webhook-Enabled Kustomize Overlay

Deploy using the `config/default-with-webhook` overlay which includes all necessary webhook components and enables the `ValidationWebhook` feature gate automatically:

```bash
make deploy IMG=<your-image> ENVIRONMENT=default-with-webhook \
  SPLUNK_GENERAL_TERMS="--accept-sgt-current-at-splunk-com"
```

This uses the same `make deploy` target as the standard deployment, which substitutes the `WATCH_NAMESPACE`, `SPLUNK_ENTERPRISE_IMAGE`, and `SPLUNK_GENERAL_TERMS` placeholder values before running `kustomize build`.

#### Option 3: Enable via Feature Gate on Existing Deployment

If you already have the operator deployed with the webhook Kubernetes resources (Service, ValidatingWebhookConfiguration, TLS certificates), enable the feature gate by patching the container args:

```bash
kubectl patch deployment splunk-operator-controller-manager -n splunk-operator \
  --type='json' -p='[{"op": "add", "path": "/spec/template/spec/containers/0/args/-", "value": "--feature-gates=ValidationWebhook=true"}]'
```

**Note:** This requires the webhook service, ValidatingWebhookConfiguration, and TLS certificates to already be deployed. Use Option 1 for a complete deployment.

#### Option 4: Modify Default Kustomization

Edit `config/default/kustomization.yaml` to uncomment the webhook-related sections:

1. Uncomment `- ../webhook` in the `bases` section
2. Uncomment `- ../certmanager` in the `bases` section
3. Uncomment `- manager_webhook_patch.yaml` in `patchesStrategicMerge`
4. Uncomment `- webhookcainjection_patch.yaml` in `patchesStrategicMerge`
5. Uncomment the `vars` section for certificate injection

Then deploy:

```bash
make deploy IMG=<your-image> SPLUNK_GENERAL_TERMS="--accept-sgt-current-at-splunk-com"
```

### Legacy: ENABLE_VALIDATION_WEBHOOK Environment Variable

> **Deprecated:** The `ENABLE_VALIDATION_WEBHOOK` environment variable is deprecated and will be removed in a future release. Use the `--feature-gates=ValidationWebhook=true` flag instead.

For backwards compatibility, setting `ENABLE_VALIDATION_WEBHOOK=true` as an environment variable on the operator container will still enable the validation webhook. The operator logs a deprecation warning when this method is used.

When both the `--feature-gates=ValidationWebhook=...` CLI flag and the `ENABLE_VALIDATION_WEBHOOK` env var are set, the **CLI flag takes precedence**. The env var is applied at startup before flag parsing, so the CLI value overwrites it.

## Validated Fields

The webhook validates the following spec fields:

### Common Fields (All CRDs)

| Field | Validation Rule | Error Message |
|-------|-----------------|---------------|
| `spec.etcVolumeStorageConfig.storageCapacity` | Must match format `^[0-9]+Gi$` (e.g., "10Gi", "100Gi") | must be in Gi format (e.g., '10Gi', '100Gi') |
| `spec.varVolumeStorageConfig.storageCapacity` | Must match format `^[0-9]+Gi$` | must be in Gi format (e.g., '10Gi', '100Gi') |
| `spec.etcVolumeStorageConfig.storageClassName` | Required when `ephemeralStorage=false` and `storageCapacity` is set | storageClassName is required when using persistent storage |
| `spec.varVolumeStorageConfig.storageClassName` | Required when `ephemeralStorage=false` and `storageCapacity` is set | storageClassName is required when using persistent storage |
| `spec.etcVolumeStorageConfig.ephemeralStorage` | Mutually exclusive with `storageClassName` and `storageCapacity` | storageClassName/storageCapacity cannot be set when ephemeralStorage is true |
| `spec.varVolumeStorageConfig.ephemeralStorage` | Mutually exclusive with `storageClassName` and `storageCapacity` | storageClassName/storageCapacity cannot be set when ephemeralStorage is true |
| `spec.extraEnv[*].name` | Must be unique across all entries | duplicate environment variable |
| `spec.imagePullSecrets[*].name` | Must be unique across all entries | duplicate secret reference |
| `spec.imagePullSecrets[*].name` | Must reference an existing Secret in the namespace | not found |
| `spec.livenessProbe.initialDelaySeconds` | Must be ≥ 0 | must be non-negative |
| `spec.readinessProbe.initialDelaySeconds` | Must be ≥ 0 | must be non-negative |
| `spec.startupProbe.initialDelaySeconds` | Must be ≥ 0 | must be non-negative |
| `spec.resources.requests.cpu` | Must be ≤ `limits.cpu` | request must be less than or equal to limit |
| `spec.resources.requests.memory` | Must be ≤ `limits.memory` | request must be less than or equal to limit |

### CRD-Specific Fields

| CRD | Field | Validation Rule |
|-----|-------|-----------------|
| Standalone | `spec.replicas` | Must be ≥ 0 |
| IndexerCluster | `spec.replicas` | Must be ≥ 3 |
| SearchHeadCluster | `spec.replicas` | Must be ≥ 3 |

### SmartStore Validation (Standalone, ClusterManager)

SmartStore configuration is validated only when provided:

| Field | Validation Rule |
|-------|-----------------|
| `spec.smartstore.volumes[*].name` | Required (non-empty) |
| `spec.smartstore.volumes[*]` | Either `endpoint` or `path` must be specified |
| `spec.smartstore.indexes[*].name` | Required (non-empty) |
| `spec.smartstore.indexes[*].volumeName` | Required (non-empty) |

### AppFramework Validation (Standalone, ClusterManager, SearchHeadCluster)

AppFramework configuration is validated only when provided:

| Field | Validation Rule |
|-------|-----------------|
| `spec.appRepo.appSources[*].name` | Required (non-empty) |
| `spec.appRepo.appSources[*].location` | Required (non-empty) |
| `spec.appRepo.appSources[*]` | Combination of `location` + `scope` must be unique across all appSources |
| `spec.appRepo.appSources[*].premiumAppsProps` | Required when `scope=premiumApps` |
| `spec.appRepo.appsRepoPollIntervalSeconds` | Must be ≥ 0 |
| `spec.appRepo.volumes[*].name` | Required (non-empty) |

## Example Validation Errors

### Invalid Replicas

```yaml
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: example
spec:
  replicas: -1  # Invalid: negative value
```

Error:
```
The Standalone "example" is invalid: .spec.replicas: Invalid value: -1: should be a non-negative integer
```

### Invalid Storage Configuration

```yaml
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: example
spec:
  etcVolumeStorageConfig:
    storageCapacity: "10GB"  # Invalid: must use Gi suffix
```

Error:
```
The Standalone "example" is invalid: spec.etcVolumeStorageConfig.storageCapacity: Invalid value: "10GB": must be in Gi format (e.g., '10Gi', '100Gi')
```

### Missing SmartStore Volume Name

```yaml
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: example
spec:
  smartstore:
    volumes:
      - name: ""  # Invalid: empty name
        endpoint: "s3://bucket"
```

Error:
```
The Standalone "example" is invalid: spec.smartstore.volumes[0].name: Required value: volume name is required
```

### Non-Existent ImagePullSecret

```yaml
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: example
  namespace: splunk
spec:
  imagePullSecrets:
    - name: my-registry-secret  # Invalid: secret does not exist in namespace
```

Error:
```
The Standalone "example" is invalid: spec.imagePullSecrets[0].name: Not found: "my-registry-secret"
```

### Ephemeral Storage with StorageClassName

```yaml
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: example
spec:
  etcVolumeStorageConfig:
    ephemeralStorage: true
    storageClassName: "standard"  # Invalid: cannot set with ephemeralStorage=true
```

Error:
```
The Standalone "example" is invalid: spec.etcVolumeStorageConfig.storageClassName: Invalid value: "standard": storageClassName cannot be set when ephemeralStorage is true
```

## Verifying Webhook Deployment

### Check Webhook Pod is Running

```bash
kubectl get pods -n splunk-operator
# Expected: splunk-operator-controller-manager-xxx   1/1   Running
```

### Check Certificate is Ready

```bash
kubectl get certificate -n splunk-operator
# Expected: splunk-operator-serving-cert   True   webhook-server-cert
```

### Check Webhook is Registered

```bash
kubectl get validatingwebhookconfiguration splunk-operator-validating-webhook-configuration
```

### Check Operator Logs

```bash
kubectl logs -n splunk-operator deployment/splunk-operator-controller-manager | grep -i webhook
# Look for: "Validation webhook enabled"
# Look for: "Starting webhook server" {"port": 9443}
```

## Troubleshooting

### Webhook Not Being Called

1. Verify the ValidatingWebhookConfiguration exists:
   ```bash
   kubectl get validatingwebhookconfiguration splunk-operator-validating-webhook-configuration -o yaml
   ```

2. Check that the CA bundle is injected:
   ```bash
   kubectl get validatingwebhookconfiguration splunk-operator-validating-webhook-configuration \
     -o jsonpath='{.webhooks[0].clientConfig.caBundle}' | base64 -d | head -1
   # Should show: -----BEGIN CERTIFICATE-----
   ```

3. Verify webhook service endpoints:
   ```bash
   kubectl get endpoints -n splunk-operator splunk-operator-webhook-service
   # Should show an IP address
   ```

### Certificate Issues

#### If using cert-manager:

1. Check cert-manager logs:
   ```bash
   kubectl logs -n cert-manager deployment/cert-manager
   ```

2. Check certificate status:
   ```bash
   kubectl describe certificate -n splunk-operator splunk-operator-serving-cert
   ```

3. Check issuer:
   ```bash
   kubectl get issuer -n splunk-operator
   ```

#### If using custom certificates:

1. Verify the Secret exists and contains valid data:
   ```bash
   kubectl get secret webhook-server-cert -n splunk-operator -o yaml
   ```

2. Verify the certificate is valid and not expired:
   ```bash
   kubectl get secret webhook-server-cert -n splunk-operator -o jsonpath='{.data.tls\.crt}' | base64 -d | openssl x509 -text -noout
   ```

3. Verify the CA bundle in the webhook configuration matches your CA:
   ```bash
   kubectl get validatingwebhookconfiguration splunk-operator-validating-webhook-configuration \
     -o jsonpath='{.webhooks[0].clientConfig.caBundle}' | base64 -d | openssl x509 -text -noout
   ```

4. Ensure the certificate SANs include the webhook service DNS name:
   ```
   splunk-operator-webhook-service.splunk-operator.svc
   ```

### Webhook Disabled

If you see "Validation webhook disabled" in the logs, ensure:

1. The `--feature-gates=ValidationWebhook=true` flag is set on the operator container args (or the legacy `ENABLE_VALIDATION_WEBHOOK=true` env var is set)
2. You're using the correct kustomize overlay (`config/default-with-webhook`)

## Architecture

The validation webhook consists of:

| Component | Description |
|-----------|-------------|
| **Webhook Server** | HTTP server listening on port 9443 with TLS |
| **Validator Registry** | Maps CRD types to their validation functions |
| **ValidatingWebhookConfiguration** | Kubernetes resource that registers the webhook |
| **Certificate** | TLS certificate (managed by cert-manager or provided manually) |
| **Service** | Kubernetes service exposing the webhook endpoint |

### Request Flow

1. User submits a CREATE/UPDATE request for a Splunk CRD
2. Kubernetes API server intercepts the request
3. API server sends an AdmissionReview to the webhook service
4. Webhook server validates the spec fields
5. Webhook returns Allowed/Denied response
6. If allowed, the resource is persisted; if denied, user receives error

## Adding a New CRD to the Webhook

For a step-by-step guide on extending the webhook to support a new CRD, see the [Webhook Development](../develop/WebhookDevelopment.md) guide.

## Disabling the Webhook

To disable the webhook after it has been enabled, remove the `--feature-gates=ValidationWebhook=true` flag from the container args (or remove the `ENABLE_VALIDATION_WEBHOOK` env var if using the legacy method).

Or redeploy using the default kustomization (without webhook):

```bash
make deploy IMG=<your-image> SPLUNK_GENERAL_TERMS="--accept-sgt-current-at-splunk-com"
```
