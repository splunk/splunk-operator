---
title: Webhook Development
parent: Develop & Contribute
nav_order: 7
---

# Adding Validation for a New CRD

This guide covers how to extend the Splunk Operator's [validation webhook](../reference/ValidationWebhook.md) to support a new Custom Resource Definition.

## 1. Create Validation Functions

Create a new file `pkg/splunk/validation/<crd>_validation.go`:

```go
package validation

import (
    "k8s.io/apimachinery/pkg/util/validation/field"
    enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
)

// Validate<CRD>Create validates a <CRD> on CREATE
func Validate<CRD>Create(obj *enterpriseApi.<CRD>) field.ErrorList {
    var allErrs field.ErrorList
    // Add validation logic
    allErrs = append(allErrs, validateCommonSplunkSpec(&obj.Spec.CommonSplunkSpec, field.NewPath("spec"))...)
    return allErrs
}

// Validate<CRD>CreateWithContext validates with access to Kubernetes API
// Use this for validations that need to check if resources exist (e.g., Secrets)
func Validate<CRD>CreateWithContext(obj *enterpriseApi.<CRD>, vc *ValidationContext) field.ErrorList {
    allErrs := Validate<CRD>Create(obj)
    if len(obj.Spec.ImagePullSecrets) > 0 {
        allErrs = append(allErrs, ValidateImagePullSecretsExistence(
            obj.Spec.ImagePullSecrets, vc, field.NewPath("spec").Child("imagePullSecrets"))...)
    }
    return allErrs
}

// Validate<CRD>Update validates a <CRD> on UPDATE
func Validate<CRD>Update(obj, oldObj *enterpriseApi.<CRD>) field.ErrorList {
    return Validate<CRD>Create(obj)
}

// Validate<CRD>UpdateWithContext validates on UPDATE with Kubernetes API access
func Validate<CRD>UpdateWithContext(obj, oldObj *enterpriseApi.<CRD>, vc *ValidationContext) field.ErrorList {
    return Validate<CRD>CreateWithContext(obj, vc)
}

// Get<CRD>WarningsOnCreate returns warnings for CREATE
func Get<CRD>WarningsOnCreate(obj *enterpriseApi.<CRD>) []string {
    return getCommonWarnings(&obj.Spec.CommonSplunkSpec)
}

// Get<CRD>WarningsOnUpdate returns warnings for UPDATE
func Get<CRD>WarningsOnUpdate(obj, oldObj *enterpriseApi.<CRD>) []string {
    return Get<CRD>WarningsOnCreate(obj)
}
```

## 2. Register the Validator

Add the GVR and validator to `pkg/splunk/validation/registry.go`:

```go
// Add GVR constant
var <CRD>GVR = schema.GroupVersionResource{
    Group:    "enterprise.splunk.com",
    Version:  "v4",
    Resource: "<crd>s",  // plural, lowercase
}

// Add to DefaultValidators map
var DefaultValidators = map[schema.GroupVersionResource]Validator{
    // ... existing validators ...

    <CRD>GVR: &GenericValidator[*enterpriseApi.<CRD>]{
        ValidateCreateFunc:            Validate<CRD>Create,
        ValidateUpdateFunc:            Validate<CRD>Update,
        ValidateCreateWithContextFunc: Validate<CRD>CreateWithContext,  // Optional: for resource lookups
        ValidateUpdateWithContextFunc: Validate<CRD>UpdateWithContext,  // Optional: for resource lookups
        WarningsOnCreateFunc:          Get<CRD>WarningsOnCreate,
        WarningsOnUpdateFunc:          Get<CRD>WarningsOnUpdate,
        GroupKind: schema.GroupKind{
            Group: "enterprise.splunk.com",
            Kind:  "<CRD>",
        },
    },
}
```

## 3. Add Unit Tests

Create `pkg/splunk/validation/<crd>_validation_test.go` with test cases.

## 4. Update ValidatingWebhookConfiguration

Add the new resource to `config/webhook/manifests.yaml`:

```yaml
webhooks:
  - name: validate.enterprise.splunk.com
    rules:
      - apiGroups: ["enterprise.splunk.com"]
        apiVersions: ["v4"]
        operations: ["CREATE", "UPDATE"]
        resources:
          - standalones
          - indexerclusters
          - <crd>s  # Add new resource here
```

## Context-Aware vs Basic Validation

- **Basic validation** (`ValidateCreateFunc`): For validations that only need the CR itself (field formats, required fields, cross-field rules)
- **Context-aware validation** (`ValidateCreateWithContextFunc`): For validations that need to query the Kubernetes API (checking if Secrets, ConfigMaps, or other resources exist)

If your CRD doesn't need context-aware validation, you can omit `ValidateCreateWithContextFunc` and `ValidateUpdateWithContextFunc` — the webhook will automatically fall back to the basic validation functions.

# Cross-Version Field Protection

`IndexerCluster` and `SearchHeadCluster` are stored as `enterprise.splunk.com/v4`, which has fields `v3` cannot represent. A write through `v3` (an update, or a delete that writes deletion metadata back) would silently drop those fields. Cross-version field protection denies such writes. Its logic is in `pkg/splunk/validation/cross_version_guard.go` and is controlled by the `CrossVersionFieldProtection` feature gate (see [Feature Gates](FeatureGates.md)).

## How it is deployed

- The operator starts the webhook server on port 9443 when `CrossVersionFieldProtection` or `ValidationWebhook` is enabled. `CrossVersionFieldProtection` is on by default, so the server runs by default. A `webhook-server` readiness check passes only once the server accepts TLS connections, so a pod that isn't serving stays out of the webhook Service's endpoints.
- `config/webhook-server` is a kustomize component that owns the webhook Service (443 → 9443) and the Deployment's mount of the `webhook-server-cert` Secret. Every overlay includes it.
- `config/components/cross-version-field-protection` is a kustomize component holding a `ValidatingWebhookConfiguration` with only the guard entry, covering the older served versions and the resources listed in its rules, with `matchPolicy: Exact`, `failurePolicy: Fail` and `timeoutSeconds: 5`.
- `config/components/cert-manager` is a kustomize component holding the cert-manager `Issuer` and `Certificate` that write `webhook-server-cert`, the `CERTIFICATE_*` vars, and a patch that adds the `cert-manager.io/inject-ca-from` annotation to every `ValidatingWebhookConfiguration` in the build. It must be listed after the components that add `ValidatingWebhookConfiguration`s. To supply its own certificate, an overlay replaces it with `config/components/ca-bundle`, which writes the CA from its `ca.crt` (gitignored) into the `caBundle` of every `ValidatingWebhookConfiguration` in the build (Option B in [Validation Webhook](../reference/ValidationWebhook.md#option-b-use-your-own-certificates)).
- `config/default` (and `kustomization-cluster.yaml` / `kustomization-namespace.yaml`), and `config/default-with-webhook/kustomization.yaml` include all three components; `config/debug` includes all but the guard. The namespace-scoped variant of `config/default` sets the guard's `namespaceSelector` to the operator's namespace.
- The v4 validation entry (`config/webhook/manifests.yaml`) is only in `config/default-with-webhook`.
- The Helm chart renders the same two entries in `helm-chart/splunk-operator/templates/validating_webhook_configuration.yaml`, each gated on its feature gate in `splunkOperator.featureGates`; keep its rules in sync with the kustomize manifests. See [Helm](../deploy/Helm.md#validation-webhooks).
- The OLM bundle (`make bundle`) builds `config/manifests`, which takes the guard from `config/default` and sets its `failurePolicy` to `Ignore`. `operator-sdk generate bundle` turns it into a `ValidatingAdmissionWebhook` entry under `webhookdefinitions` in the CSV. OLM sets the webhook's `namespaceSelector` to the `OperatorGroup`'s target namespaces, so the bundle supports all four install modes. See [Turning it off under OLM](#turning-it-off-under-olm).

For kustomize and Helm installs, because `failurePolicy` is `Fail`, a `cross-version-field-protection` `ValidatingWebhookConfiguration` that is installed with no webhook server answering it makes every matching write fail. Install or remove it together with the `CrossVersionFieldProtection` gate. `ValidationWebhook` is independent of both.

## Turning it off with `make deploy`

1. If the `ValidatingWebhookConfiguration` is already installed, delete it first. `kubectl apply` never removes it.

   ```bash
   kubectl delete validatingwebhookconfiguration splunk-operator-cross-version-field-protection
   ```

2. Comment out `- ../components/cross-version-field-protection` under `components:` in the kustomization you deploy: `config/default/kustomization.yaml`, or `config/default-with-webhook/kustomization.yaml` with `ENVIRONMENT=default-with-webhook`.

3. Deploy with the gate off:

   ```bash
   make deploy IMG=<image> MANAGER_EXTRA_ARG="--feature-gates=CrossVersionFieldProtection=false"
   ```

   `MANAGER_EXTRA_ARG` takes one flag, so combine gates in it if you need more than one, for example `--feature-gates=PostgresController=true,CrossVersionFieldProtection=false`.

Don't turn the gate off without steps 1 and 2. The `ValidatingWebhookConfiguration` would stay installed with no webhook server answering it, and every matching write would fail.

## Turning it off in a published release YAML

For an install applied from `splunk-operator-cluster.yaml` or `splunk-operator-namespace.yaml`:

1. Remove the `ValidatingWebhookConfiguration` named `splunk-operator-cross-version-field-protection` from the YAML. If it is already installed, delete it first:

   ```bash
   kubectl delete validatingwebhookconfiguration splunk-operator-cross-version-field-protection
   ```

2. Add `--feature-gates=CrossVersionFieldProtection=false` to the manager container's `args`.

Do step 1 before step 2. If the webhook server stops answering while the `ValidatingWebhookConfiguration` is still installed, matching writes fail until it is removed.

## Turning it off under OLM

OLM installs the guard's webhook from the CSV, and recreates or overwrites it on every install or upgrade, so it can't be removed. Turn the gate off with the `FEATURE_GATES` env var in the `Subscription` instead:

```yaml
spec:
  config:
    env:
      - name: FEATURE_GATES
        value: CrossVersionFieldProtection=false
```

With `ValidationWebhook` also off, which is its default, the webhook server doesn't start. The webhook's `failurePolicy` is `Ignore` in the bundle, so the API server then allows v3 writes. If `FEATURE_GATES` already sets other gates, add `CrossVersionFieldProtection=false` to the same comma-separated value.

## Under OLM the guard fails open

Because the bundle's `failurePolicy` is `Ignore`, the guard protects only while the API server can reach the operator on port 9443. While the operator restarts, or if a `NetworkPolicy` blocks that traffic, v3 writes go through unguarded and can drop v4-only fields with no error. If your cluster uses default-deny network policies, allow ingress from the API server to the operator pod on port 9443.
