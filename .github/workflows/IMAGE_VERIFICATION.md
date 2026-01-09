# Image Verification Workflow

## Overview

The `reusable-verify-image.yml` workflow provides cosign-based image signature verification as a separate, reusable module.

## When to Use

### ✅ DO Use This Workflow:

1. **Before Production Deployments**
   - Verify images before deploying to production environments
   - Ensure only signed images are deployed

2. **In Release Pipelines**
   - Verify images before promoting them to higher environments
   - Add as a gate before critical deployments

3. **When Consuming External Images**
   - Verify images from external registries
   - Validate images before use in your clusters

4. **Compliance Requirements**
   - When your organization requires signature verification
   - For audit trails and security compliance

### ❌ DON'T Use This Workflow:

1. **Immediately After Building**
   - Don't verify in the same workflow that signs the image
   - The build workflow already signs - verification is redundant

2. **In Development/Testing**
   - Skip verification for dev environments to speed up iteration
   - Only verify for production-bound images

## Usage Examples

### Example 1: Verify Before Deployment

```yaml
name: Deploy to Production

on:
  workflow_dispatch:
    inputs:
      image-tag:
        required: true

jobs:
  verify-image:
    uses: ./.github/workflows/reusable-verify-image.yml
    with:
      image-name: ${{ secrets.ECR_REPOSITORY }}/splunk/splunk-operator:${{ inputs.image-tag }}
      cloud-provider: aws
    secrets:
      AWS_ACCESS_KEY_ID: ${{ secrets.AWS_ACCESS_KEY_ID }}
      AWS_SECRET_ACCESS_KEY: ${{ secrets.AWS_SECRET_ACCESS_KEY }}
      AWS_DEFAULT_REGION: ${{ secrets.AWS_DEFAULT_REGION }}
      ECR_REPOSITORY: ${{ secrets.ECR_REPOSITORY }}
      COSIGN_PUBLIC_KEY: ${{ secrets.COSIGN_PUBLIC_KEY }}

  deploy:
    needs: verify-image
    runs-on: ubuntu-latest
    steps:
      - name: Deploy to Production
        run: |
          echo "Deploying verified image..."
          # Your deployment steps here
```

### Example 2: Conditional Deployment Based on Verification

```yaml
jobs:
  verify-image:
    uses: ./.github/workflows/reusable-verify-image.yml
    with:
      image-name: ${{ env.IMAGE_NAME }}
    secrets: inherit

  deploy-if-verified:
    needs: verify-image
    if: needs.verify-image.outputs.verified == 'true'
    runs-on: ubuntu-latest
    steps:
      - name: Deploy
        run: echo "Deploying verified image..."
```

### Example 3: Multi-Cloud Verification

```yaml
jobs:
  verify-aws-image:
    uses: ./.github/workflows/reusable-verify-image.yml
    with:
      image-name: ${{ secrets.ECR_REPOSITORY }}/splunk/splunk-operator:${{ github.sha }}
      cloud-provider: aws
    secrets:
      AWS_ACCESS_KEY_ID: ${{ secrets.AWS_ACCESS_KEY_ID }}
      AWS_SECRET_ACCESS_KEY: ${{ secrets.AWS_SECRET_ACCESS_KEY }}
      AWS_DEFAULT_REGION: ${{ secrets.AWS_DEFAULT_REGION }}
      ECR_REPOSITORY: ${{ secrets.ECR_REPOSITORY }}
      COSIGN_PUBLIC_KEY: ${{ secrets.COSIGN_PUBLIC_KEY }}

  verify-azure-image:
    uses: ./.github/workflows/reusable-verify-image.yml
    with:
      image-name: myregistry.azurecr.io/splunk/splunk-operator:${{ github.sha }}
      cloud-provider: azure
    secrets:
      AZURE_CREDENTIALS: ${{ secrets.AZURE_CREDENTIALS }}
      COSIGN_PUBLIC_KEY: ${{ secrets.COSIGN_PUBLIC_KEY }}
```

## Inputs

| Input | Required | Default | Description |
|-------|----------|---------|-------------|
| `image-name` | Yes | - | Full image name with tag (registry/repo:tag) |
| `cloud-provider` | No | `aws` | Cloud provider: `aws`, `azure`, or `gcp` |

## Secrets

| Secret | Required | Description |
|--------|----------|-------------|
| `COSIGN_PUBLIC_KEY` | Yes | Cosign public key for verification |
| `AWS_ACCESS_KEY_ID` | No | AWS access key (for AWS/ECR) |
| `AWS_SECRET_ACCESS_KEY` | No | AWS secret key (for AWS/ECR) |
| `AWS_DEFAULT_REGION` | No | AWS region (for AWS/ECR) |
| `ECR_REPOSITORY` | No | ECR repository URL (for AWS/ECR) |
| `AZURE_CREDENTIALS` | No | Azure credentials JSON (for Azure) |
| `GCP_SERVICE_ACCOUNT_KEY` | No | GCP service account key (for GCP) |

## Outputs

| Output | Description |
|--------|-------------|
| `verified` | Boolean - `true` if signature verified, `false` otherwise |

## Best Practices

1. **Always verify in production pipelines** - Make verification a required step before production deployments

2. **Use `needs` dependencies** - Ensure verification completes successfully before deployment:
   ```yaml
   deploy:
     needs: verify-image
     if: needs.verify-image.outputs.verified == 'true'
   ```

3. **Store public keys securely** - Keep `COSIGN_PUBLIC_KEY` in GitHub Secrets, not in code

4. **Log verification results** - The workflow automatically creates step summaries for audit trails

5. **Separate from build** - Don't verify immediately after signing in the same workflow

## Security Considerations

- **Public Key Distribution**: Ensure your cosign public key is distributed securely
- **Key Rotation**: Have a process for rotating signing keys periodically
- **Audit Logs**: GitHub Actions provides audit logs for all verification runs
- **Failure Handling**: Verification failures should block deployments (exit code 1)

## Troubleshooting

### Verification Fails with "no signatures found"

**Cause**: The image was not signed or signatures are not in the registry

**Solutions**:
- Ensure the build workflow ran successfully with `sign-image: true`
- Check that the image tag matches exactly (no typos)
- Verify the image exists in the registry

### Authentication Issues

**Cause**: Missing or incorrect cloud credentials

**Solutions**:
- Verify all required secrets are set for your cloud provider
- Check that service accounts have registry read permissions
- Test authentication independently first

### Wrong Public Key

**Cause**: Using a different public key than the one used for signing

**Solutions**:
- Ensure `COSIGN_PUBLIC_KEY` secret matches the private key used for signing
- Verify key format (should include headers like `-----BEGIN PUBLIC KEY-----`)
