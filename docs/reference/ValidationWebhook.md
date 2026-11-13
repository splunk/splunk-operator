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

3. **Deploy with your CA.** Each install path below writes your CA into the `caBundle` of every `ValidatingWebhookConfiguration` it creates, so the API server trusts the serving certificate from the start. Because the webhooks use `failurePolicy: Fail`, a webhook whose CA does not match the serving certificate rejects every write it matches.
   - **Helm:**
     ```bash
     helm install splunk-operator splunk/splunk-operator -n splunk-operator \
       --set splunkOperator.webhook.useCertManager=false \
       --set-file splunkOperator.webhook.caBundle=ca.crt
     ```
     The chart then creates no cert-manager resources and mounts the Secret named by `splunkOperator.webhook.certSecretName` (default `webhook-server-cert`). The install fails if `caBundle` is missing or is not a PEM certificate. The certificate names in step 1 assume the release namespace `splunk-operator` and no `splunkOperator.nameOverride`; otherwise use `<fullname>-webhook-service.<namespace>.svc`.
   - **Kustomize:** copy `ca.crt` to `config/components/ca-bundle/ca.crt`. In your kustomization, replace `- ../components/cert-manager` with `- ../components/ca-bundle` under `components:` (`config/default` and `config/default-with-webhook` both list it), then apply it:
     ```bash
     kubectl apply --server-side -k <kustomization-dir>
     ```
     `<kustomization-dir>` is the kustomization you deploy, with your own values for the `WATCH_NAMESPACE_VALUE`, `SPLUNK_ENTERPRISE_IMAGE`, `SPLUNK_GENERAL_TERMS_VALUE` and `MANAGER_EXTRA_ARG_VALUE` placeholders in `config/default` (or `config/default-with-webhook`); don't apply those directories with the placeholders in place. The build fails if `ca.crt` is missing.
   - **Published release YAML** (`splunk-operator-cluster.yaml` or `splunk-operator-namespace.yaml`): before applying, delete the `Issuer` and `Certificate` objects and the `cert-manager.io/inject-ca-from` annotations, and set `caBundle` in each webhook's `clientConfig` to the output of `base64 < ca.crt | tr -d '\n'`.

#### Rotating your own certificates

- **New serving certificate from the same CA:** update the `webhook-server-cert` Secret. The operator picks up the new files without a restart once the kubelet refreshes the mounted Secret, which can take a minute or two. Nothing else changes.
- **New CA:** the API server uses a new `caBundle` within seconds, but the operator serves a new certificate only after that kubelet refresh. To avoid rejected writes in between:
  1. Update the CA bundle (see below) to hold both CAs, for example `cat old-ca.crt new-ca.crt > ca.crt`.
  2. Update the `webhook-server-cert` Secret to the certificate signed by the new CA, and wait until the operator serves it.
  3. Update the CA bundle to hold only the new CA.

To update the CA bundle:

- **Helm:**
  ```bash
  helm upgrade splunk-operator splunk/splunk-operator -n splunk-operator --reuse-values \
    --set-file splunkOperator.webhook.caBundle=ca.crt
  ```
- **Published release YAML:** set each webhook's `caBundle` in your edited YAML to the output of `base64 < ca.crt | tr -d '\n'`, then apply the file again:
  ```bash
  kubectl apply --server-side -f splunk-operator-cluster.yaml
  ```
- **Kustomize:** copy `ca.crt` to `config/components/ca-bundle/ca.crt`, then apply the same kustomization you deployed under "Deploy with your CA" above again:
  ```bash
  kubectl apply --server-side -k <kustomization-dir>
  ```

Each of these changes only the `ValidatingWebhookConfiguration`s, so the operator pod is not restarted.

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

**Note:** The chart creates the webhook Service and the `ValidatingWebhookConfiguration` itself, and by default a cert-manager `Issuer` and `Certificate` for the serving certificate; don't deploy them separately. By default cert-manager must already be installed (Option A above). To use your own certificate instead, follow Option B above: `--set splunkOperator.webhook.useCertManager=false --set-file splunkOperator.webhook.caBundle=ca.crt`. See [Helm: Validation webhooks](../deploy/Helm.md#validation-webhooks).

#### Option 2: Use the Webhook-Enabled Kustomize Overlay

To learn more about the \<required value\> for the `SPLUNK_GENERAL_TERMS` variable, please see the [README](../README.md#splunk-general-terms-acceptance) or Splunk Enterprise 10.x image’s README.

Deploy using the `config/default-with-webhook` overlay which includes all necessary webhook components and enables the `ValidationWebhook` feature gate automatically:

```bash
make deploy IMG=<your-image> ENVIRONMENT=default-with-webhook \
  SPLUNK_GENERAL_TERMS="<required value>"
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

To learn more about the \<required value\> for the `SPLUNK_GENERAL_TERMS` variable, please see the [README](../README.md#splunk-general-terms-acceptance) or Splunk Enterprise 10.x image’s README.

Edit `config/default/kustomization.yaml` and uncomment `- ../webhook` in the `bases` section. The webhook Service, the certificate mount, the serving certificate and its CA injection already come from the `../webhook-server` and `../components/cert-manager` components that the file lists.

Then deploy with the feature gate on:

```bash
make deploy IMG=<your-image> SPLUNK_GENERAL_TERMS="<required value>" \
  MANAGER_EXTRA_ARG="--feature-gates=ValidationWebhook=true"
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
| `spec.etcVolumeStorageConfig.ephemeralStorage` | Mutually exclusive with `storageClassName`, `volumeAttributesClassName`, and `storageCapacity` | storageClassName/volumeAttributesClassName/storageCapacity cannot be set when ephemeralStorage is true |
| `spec.varVolumeStorageConfig.ephemeralStorage` | Mutually exclusive with `storageClassName`, `volumeAttributesClassName`, and `storageCapacity` | storageClassName/volumeAttributesClassName/storageCapacity cannot be set when ephemeralStorage is true |
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

To learn more about the \<required value\> for the `SPLUNK_GENERAL_TERMS` variable, please see the [README](../README.md#splunk-general-terms-acceptance) or Splunk Enterprise 10.x image’s README.

Or redeploy using the default kustomization (without webhook):

```bash
make deploy IMG=<your-image> SPLUNK_GENERAL_TERMS="<required value>"
```
