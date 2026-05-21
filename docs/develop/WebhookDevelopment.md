---
title: Webhook Development
parent: Develop & Contribute
nav_order: 7
---

# Adding Validation for a New CRD

This guide covers how to extend the Splunk Operator's [validation webhook](../reference/ValidationWebhook.html) to support a new Custom Resource Definition.

## 1. Create Validation Functions

Create a new file `pkg/splunk/enterprise/validation/<crd>_validation.go`:

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

Add the GVR and validator to `pkg/splunk/enterprise/validation/registry.go`:

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

Create `pkg/splunk/enterprise/validation/<crd>_validation_test.go` with test cases.

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
