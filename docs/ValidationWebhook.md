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

Before enabling the webhook, ensure you have:

1. **cert-manager** installed in your cluster (required for TLS certificate management)

```bash
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.14.0/cert-manager.yaml
kubectl wait --for=condition=Available --timeout=300s deployment/cert-manager -n cert-manager
kubectl wait --for=condition=Available --timeout=300s deployment/cert-manager-webhook -n cert-manager
```

### Deployment Options

#### Option 1: Use the Webhook-Enabled Kustomize Overlay

Deploy using the `config/default-with-webhook` overlay which includes all necessary webhook components:

```bash
# Build and apply the webhook-enabled configuration
kustomize build config/default-with-webhook | kubectl apply -f -
```

#### Option 2: Enable Webhook on Existing Deployment

If you already have the operator deployed, you can enable the webhook by setting the `ENABLE_VALIDATION_WEBHOOK` environment variable:

```bash
kubectl set env deployment/splunk-operator-controller-manager \
  ENABLE_VALIDATION_WEBHOOK=true -n splunk-operator
```

**Note:** This option also requires the webhook service, ValidatingWebhookConfiguration, and TLS certificates to be deployed. Use Option 1 for a complete deployment.

#### Option 3: Modify Default Kustomization

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

## Validated Fields

The webhook validates the following spec fields:

### Common Fields (All CRDs)

| Field | Validation Rule | Error Message |
|-------|-----------------|---------------|
| `spec.imagePullPolicy` | Must be `Always`, `Never`, or `IfNotPresent` | Unsupported value: supported values: "Always", "Never", "IfNotPresent" |
| `spec.livenessInitialDelaySeconds` | Must be ≥ 0 | must be non-negative |
| `spec.readinessInitialDelaySeconds` | Must be ≥ 0 | must be non-negative |
| `spec.etcVolumeStorageConfig.storageCapacity` | Must match format `^[0-9]+Gi$` (e.g., "10Gi", "100Gi") | must be in Gi format (e.g., '10Gi', '100Gi') |
| `spec.varVolumeStorageConfig.storageCapacity` | Must match format `^[0-9]+Gi$` | must be in Gi format (e.g., '10Gi', '100Gi') |
| `spec.etcVolumeStorageConfig.storageClassName` | Required when `ephemeralStorage=false` and `storageCapacity` is set | storageClassName is required when using persistent storage |
| `spec.varVolumeStorageConfig.storageClassName` | Required when `ephemeralStorage=false` and `storageCapacity` is set | storageClassName is required when using persistent storage |

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

### Invalid ImagePullPolicy

```yaml
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: example
spec:
  imagePullPolicy: "InvalidPolicy"  # Invalid: not a valid policy
```

Error:
```
The Standalone "example" is invalid: spec.imagePullPolicy: Unsupported value: "InvalidPolicy": supported values: "Always", "IfNotPresent"
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
# Look for: "Validation webhook enabled via ENABLE_VALIDATION_WEBHOOK=true"
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

### Webhook Disabled

If you see "Validation webhook disabled" in the logs, ensure:

1. The `ENABLE_VALIDATION_WEBHOOK` environment variable is set to `true`
2. You're using the correct kustomize overlay (`config/default-with-webhook`)

## Architecture

The validation webhook consists of:

| Component | Description |
|-----------|-------------|
| **Webhook Server** | HTTP server listening on port 9443 with TLS |
| **Validator Registry** | Maps CRD types to their validation functions |
| **ValidatingWebhookConfiguration** | Kubernetes resource that registers the webhook |
| **Certificate** | TLS certificate managed by cert-manager |
| **Service** | Kubernetes service exposing the webhook endpoint |

### Request Flow

1. User submits a CREATE/UPDATE request for a Splunk CRD
2. Kubernetes API server intercepts the request
3. API server sends an AdmissionReview to the webhook service
4. Webhook server validates the spec fields
5. Webhook returns Allowed/Denied response
6. If allowed, the resource is persisted; if denied, user receives error

## Disabling the Webhook

To disable the webhook after it has been enabled:

```bash
kubectl set env deployment/splunk-operator-controller-manager \
  ENABLE_VALIDATION_WEBHOOK=false -n splunk-operator
```

Or redeploy using the default kustomization (without webhook):

```bash
make deploy IMG=<your-image> SPLUNK_GENERAL_TERMS="--accept-sgt-current-at-splunk-com"
```
