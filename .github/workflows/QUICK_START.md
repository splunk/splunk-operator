# Quick Start: Using Modernized Workflows

## For Developers

### I want to add a new integration test

**Use the reusable integration test workflow:**

```yaml
name: My New Test

on:
  push:
    branches: [my-feature]

jobs:
  my-test:
    uses: ./.github/workflows/reusable-integration-test.yml
    with:
      cluster-platform: eks  # or azure, gcp
      test-focus: my-new-test
      cluster-name: eks-test-${{ github.run_id }}
      cluster-workers: 3
      splunk-operator-image: ${{ secrets.ECR_REPOSITORY }}/splunk/splunk-operator:latest
      splunk-enterprise-image: ${{ secrets.SPLUNK_ENTERPRISE_IMAGE }}
    secrets:
      AWS_ACCESS_KEY_ID: ${{ secrets.AWS_ACCESS_KEY_ID }}
      AWS_SECRET_ACCESS_KEY: ${{ secrets.AWS_SECRET_ACCESS_KEY }}
      AWS_DEFAULT_REGION: ${{ secrets.AWS_DEFAULT_REGION }}
      ECR_REPOSITORY: ${{ secrets.ECR_REPOSITORY }}
      TEST_BUCKET: ${{ secrets.TEST_BUCKET }}
      TEST_INDEXES_S3_BUCKET: ${{ secrets.TEST_INDEXES_S3_BUCKET }}
```

### I want to build and test a custom operator image

**Use the reusable build workflow:**

```yaml
name: Build Custom Image

on:
  workflow_dispatch:
    inputs:
      base-image:
        description: 'Custom base image'
        required: true

jobs:
  build:
    uses: ./.github/workflows/reusable-build-operator.yml
    with:
      base-image: ${{ inputs.base-image }}
      image-tag: custom-${{ github.sha }}
      sign-image: true
    secrets: inherit
```

### I need to setup Operator SDK in my workflow

**Use the composite action:**

```yaml
steps:
  - uses: actions/checkout@v4

  - name: Load .env
    id: dotenv
    uses: falti/dotenv-action@d4d12eaa0e1dd06d5bdc3d7af3bf4c8c93cb5359

  - uses: ./.github/actions/setup-operator-tools
    with:
      operator-sdk-version: ${{ steps.dotenv.outputs.OPERATOR_SDK_VERSION }}
      go-version: ${{ steps.dotenv.outputs.GO_VERSION }}

  - name: Use Operator SDK
    run: operator-sdk version
```

### I need to install Kubernetes tools

**Use the composite action:**

```yaml
steps:
  - name: Load .env
    id: dotenv
    uses: falti/dotenv-action@d4d12eaa0e1dd06d5bdc3d7af3bf4c8c93cb5359

  - uses: ./.github/actions/setup-k8s-tools
    with:
      kubectl-version: ${{ steps.dotenv.outputs.KUBECTL_VERSION }}
      helm-version: v3.8.2
      eksctl-version: ${{ steps.dotenv.outputs.EKSCTL_VERSION }}
      install-metrics-server: 'true'  # Optional

  - name: Use tools
    run: |
      kubectl version --client
      helm version
      eksctl version
```

### I need to authenticate to AWS/Azure/GCP

**Use the composite action:**

```yaml
steps:
  # For AWS
  - uses: ./.github/actions/configure-cloud-auth
    with:
      cloud-provider: aws
      aws-access-key-id: ${{ secrets.AWS_ACCESS_KEY_ID }}
      aws-secret-access-key: ${{ secrets.AWS_SECRET_ACCESS_KEY }}
      aws-region: ${{ secrets.AWS_DEFAULT_REGION }}

  # For Azure
  - uses: ./.github/actions/configure-cloud-auth
    with:
      cloud-provider: azure
      azure-credentials: ${{ secrets.AZURE_CREDENTIALS }}

  # For GCP
  - uses: ./.github/actions/configure-cloud-auth
    with:
      cloud-provider: gcp
      gcp-service-account-key: ${{ secrets.GCP_SERVICE_ACCOUNT_KEY }}
```

### I need to build and sign an image

**Use the composite action:**

```yaml
steps:
  - uses: actions/checkout@v4

  - name: Setup authentication (AWS, Azure, or GCP)
    uses: ./.github/actions/configure-cloud-auth
    with:
      cloud-provider: aws
      # ... auth params ...

  - uses: ./.github/actions/build-and-sign-image
    with:
      image-name: ${{ secrets.ECR_REPOSITORY }}/my-image:${{ github.sha }}
      dockerfile: ./Dockerfile
      sign-image: true
      cosign-private-key: ${{ secrets.COSIGN_PRIVATE_KEY }}
      cosign-password: ${{ secrets.COSIGN_PASSWORD }}
```

### I need to collect test artifacts

**Use the composite action:**

```yaml
steps:
  # ... run your tests ...

  - name: Collect artifacts
    if: always()
    uses: ./.github/actions/collect-test-artifacts
    with:
      artifact-name: my-test-logs-${{ github.run_id }}
      log-path: ./test
```

---

## For Maintainers

### Updating Tool Versions

**Update `.env` file:**

```bash
# Edit .env
OPERATOR_SDK_VERSION=v1.40.0  # Update version
GO_VERSION=1.23.1
KUBECTL_VERSION=v1.30.0
EKSCTL_VERSION=v0.216.0
```

**That's it!** All workflows automatically use the new versions.

### Adding a New Composite Action

1. Create directory: `.github/actions/my-action/`
2. Create `action.yml`:

```yaml
name: 'My Action'
description: 'What it does'

inputs:
  my-input:
    description: 'Input description'
    required: true

runs:
  using: 'composite'
  steps:
    - name: Do something
      shell: bash
      run: |
        echo "Hello ${{ inputs.my-input }}"

outputs:
  my-output:
    description: 'Output description'
    value: ${{ steps.some-step.outputs.value }}
```

3. Use it in workflows:

```yaml
- uses: ./.github/actions/my-action
  with:
    my-input: value
```

### Adding a New Reusable Workflow

1. Create `.github/workflows/reusable-my-workflow.yml`:

```yaml
name: 'Reusable My Workflow'

on:
  workflow_call:
    inputs:
      my-param:
        description: 'Parameter description'
        required: true
        type: string
    secrets:
      MY_SECRET:
        required: true
    outputs:
      my-output:
        description: 'Output description'
        value: ${{ jobs.my-job.outputs.output-value }}

jobs:
  my-job:
    runs-on: ubuntu-latest
    outputs:
      output-value: ${{ steps.my-step.outputs.value }}
    steps:
      - name: Do work
        run: echo "Working"
```

2. Call it from other workflows:

```yaml
jobs:
  call-reusable:
    uses: ./.github/workflows/reusable-my-workflow.yml
    with:
      my-param: value
    secrets:
      MY_SECRET: ${{ secrets.MY_SECRET }}
```

### Testing Changes Locally

**Use `act` to test workflows locally:**

```bash
# Install act
brew install act

# Test a workflow
act -W .github/workflows/modernized-build-test-push.yml

# Test with secrets
act -W .github/workflows/my-workflow.yml --secret-file .secrets

# Test specific job
act -W .github/workflows/my-workflow.yml -j my-job
```

### Validating Workflow Syntax

**Use `actionlint`:**

```bash
# Install
brew install actionlint

# Validate all workflows
actionlint .github/workflows/*.yml

# Validate specific workflow
actionlint .github/workflows/my-workflow.yml
```

---

## Composite Actions Reference

| Action | Purpose | Common Inputs |
|--------|---------|---------------|
| **setup-operator-tools** | Install Operator SDK, Ginkgo, Go | `operator-sdk-version`, `go-version` |
| **setup-k8s-tools** | Install kubectl, Helm, eksctl | `kubectl-version`, `helm-version`, `eksctl-version` |
| **configure-cloud-auth** | Authenticate to cloud providers | `cloud-provider`, auth credentials |
| **build-and-sign-image** | Build Docker image, sign with Cosign | `image-name`, `dockerfile`, `sign-image` |
| **collect-test-artifacts** | Collect and upload test logs | `artifact-name`, `log-path` |

---

## Reusable Workflows Reference

| Workflow | Purpose | Common Inputs |
|----------|---------|---------------|
| **reusable-build-operator.yml** | Build operator image with scanning | `image-tag`, `sign-image`, `run-vulnerability-scan` |
| **reusable-integration-test.yml** | Run integration tests on any cloud | `cluster-platform`, `test-focus`, `cluster-name` |

---

## Common Patterns

### Pattern: Build → Test → Deploy

```yaml
jobs:
  build:
    uses: ./.github/workflows/reusable-build-operator.yml
    with:
      image-tag: ${{ github.sha }}
    secrets: inherit

  test:
    needs: build
    uses: ./.github/workflows/reusable-integration-test.yml
    with:
      cluster-platform: eks
      splunk-operator-image: ${{ needs.build.outputs.image-name }}
      # ... test params ...
    secrets: inherit

  deploy:
    needs: [build, test]
    runs-on: ubuntu-latest
    steps:
      - name: Deploy
        run: |
          echo "Deploying ${{ needs.build.outputs.image-name }}"
```

### Pattern: Multi-Cloud Testing

```yaml
jobs:
  build:
    uses: ./.github/workflows/reusable-build-operator.yml
    secrets: inherit

  test-aws:
    needs: build
    uses: ./.github/workflows/reusable-integration-test.yml
    with:
      cluster-platform: eks
      splunk-operator-image: ${{ needs.build.outputs.image-name }}
    secrets: inherit

  test-azure:
    needs: build
    uses: ./.github/workflows/reusable-integration-test.yml
    with:
      cluster-platform: azure
      splunk-operator-image: ${{ needs.build.outputs.image-name }}
    secrets: inherit

  test-gcp:
    needs: build
    uses: ./.github/workflows/reusable-integration-test.yml
    with:
      cluster-platform: gcp
      splunk-operator-image: ${{ needs.build.outputs.image-name }}
    secrets: inherit
```

### Pattern: Matrix Testing

```yaml
jobs:
  test:
    strategy:
      matrix:
        platform: [eks, azure, gcp]
        test: [basic, advanced]
    uses: ./.github/workflows/reusable-integration-test.yml
    with:
      cluster-platform: ${{ matrix.platform }}
      test-focus: ${{ matrix.test }}
      cluster-name: ${{ matrix.platform }}-${{ matrix.test }}-${{ github.run_id }}
    secrets: inherit
```

---

## Troubleshooting

### Issue: Composite action not found

**Error**: `Unable to resolve action ./.github/actions/my-action`

**Fix**: Ensure you've checked out the repository first:

```yaml
steps:
  - uses: actions/checkout@v4  # Required!
  - uses: ./.github/actions/my-action
```

### Issue: Reusable workflow secrets not available

**Error**: `Secret MY_SECRET is not available`

**Fix**: Pass secrets explicitly or use `secrets: inherit`:

```yaml
jobs:
  my-job:
    uses: ./.github/workflows/reusable-workflow.yml
    secrets: inherit  # Or pass individually
```

### Issue: Output from reusable workflow not available

**Error**: `${{ needs.build.outputs.my-output }}` is empty

**Fix**: Ensure the reusable workflow declares outputs:

```yaml
# In reusable workflow
on:
  workflow_call:
    outputs:
      my-output:
        description: 'My output'
        value: ${{ jobs.my-job.outputs.my-value }}

# In caller workflow
jobs:
  build:
    uses: ./.github/workflows/reusable-workflow.yml

  use-output:
    needs: build
    runs-on: ubuntu-latest
    steps:
      - run: echo "${{ needs.build.outputs.my-output }}"
```

### Issue: Composite action fails silently

**Fix**: Add error handling in composite action:

```yaml
runs:
  using: 'composite'
  steps:
    - shell: bash
      run: |
        set -e  # Exit on error
        set -o pipefail  # Catch errors in pipes
        # Your commands here
```

---

## Best Practices

### ✅ DO

- Use composite actions for repeated setup steps
- Use reusable workflows for complete job patterns
- Pass secrets explicitly or use `secrets: inherit`
- Version your composite actions (via git tags/branches)
- Document inputs and outputs
- Test locally with `act` before pushing
- Use semantic versioning for reusable components

### ❌ DON'T

- Don't use composite actions for complex business logic
- Don't hardcode values that should be parameters
- Don't forget `uses: actions/checkout@v4` before local actions
- Don't mix reusable workflows with job-level matrix (use input parameters)
- Don't skip testing after changes
- Don't forget to update documentation

---

## Getting Help

### Resources

- **GitHub Docs**: [Composite Actions](https://docs.github.com/en/actions/creating-actions/creating-a-composite-action)
- **GitHub Docs**: [Reusable Workflows](https://docs.github.com/en/actions/using-workflows/reusing-workflows)
- **Modernization Guide**: [MODERNIZATION_GUIDE.md](./MODERNIZATION_GUIDE.md)
- **Before/After Comparison**: [BEFORE_AFTER_COMPARISON.md](./BEFORE_AFTER_COMPARISON.md)

### Internal Support

- **Slack**: #platform-engineering
- **Office Hours**: Tuesdays 2pm PT
- **Documentation**: [Confluence Link]

---

**Last Updated**: 2025-11-16
**Version**: 1.0.0
**Maintained by**: Platform Engineering Team
