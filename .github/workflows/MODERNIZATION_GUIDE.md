# GitHub Workflows Modernization Guide

## Overview

This guide documents the modernization and modularization of Splunk Operator's GitHub Actions workflows. The goal is to reduce duplication, improve maintainability, and standardize on modern GitHub Actions best practices.

## Current State Analysis

### Statistics
- **Total Workflows**: 24
- **Code Duplication**: ~60-70% across workflows
- **Repeated Patterns**: 13+ major code blocks duplicated
- **Outdated Actions**: 15+ workflows using v1/v2 actions

### Major Issues Identified

1. **Operator SDK Installation** - Repeated in 13 workflows (~40 lines each)
2. **Tool Installations** - Helm, eksctl, kubectl duplicated 6-10 times
3. **Cloud Authentication** - AWS/Azure/GCP auth logic duplicated
4. **Integration Tests** - Nearly identical test patterns in separate files
5. **Action Version Inconsistency** - Mixing v1, v2, v3, v4 versions

## New Architecture

### Component Hierarchy

```
.github/
├── actions/                           # Composite Actions (NEW)
│   ├── setup-operator-tools/
│   │   └── action.yml
│   ├── setup-k8s-tools/
│   │   └── action.yml
│   ├── configure-cloud-auth/
│   │   └── action.yml
│   ├── build-and-sign-image/
│   │   └── action.yml
│   └── collect-test-artifacts/
│       └── action.yml
├── workflows/
│   ├── reusable-build-operator.yml    # Reusable Workflows (NEW)
│   ├── reusable-integration-test.yml
│   ├── modernized-build-test-push.yml # Example modernized workflow
│   └── [24 existing workflows]        # To be migrated
```

## Composite Actions

### 1. setup-operator-tools

**Purpose**: Install Operator SDK, Ginkgo, and Go toolchain

**Usage**:
```yaml
- uses: ./.github/actions/setup-operator-tools
  with:
    operator-sdk-version: ${{ steps.dotenv.outputs.OPERATOR_SDK_VERSION }}
    go-version: ${{ steps.dotenv.outputs.GO_VERSION }}
```

**Replaces**: 13 instances of duplicate Operator SDK installation code

### 2. setup-k8s-tools

**Purpose**: Install kubectl, Helm, eksctl, metrics-server, dashboard

**Usage**:
```yaml
- uses: ./.github/actions/setup-k8s-tools
  with:
    kubectl-version: v1.29.1
    helm-version: v3.8.2
    eksctl-version: v0.215.0
    install-metrics-server: 'true'
```

**Replaces**: 10+ instances of tool installation scripts

### 3. configure-cloud-auth

**Purpose**: Unified authentication for AWS, Azure, GCP

**Usage**:
```yaml
- uses: ./.github/actions/configure-cloud-auth
  with:
    cloud-provider: aws  # or azure, gcp
    aws-access-key-id: ${{ secrets.AWS_ACCESS_KEY_ID }}
    aws-secret-access-key: ${{ secrets.AWS_SECRET_ACCESS_KEY }}
    aws-region: ${{ secrets.AWS_DEFAULT_REGION }}
```

**Replaces**: 10+ instances of cloud provider authentication

### 4. build-and-sign-image

**Purpose**: Build Docker images, push to registry, sign with Cosign

**Usage**:
```yaml
- uses: ./.github/actions/build-and-sign-image
  with:
    image-name: ${{ secrets.ECR_REPOSITORY }}/splunk/splunk-operator:${{ github.sha }}
    sign-image: true
    cosign-private-key: ${{ secrets.COSIGN_PRIVATE_KEY }}
    cosign-password: ${{ secrets.COSIGN_PASSWORD }}
```

**Replaces**: 8+ instances of image build and signing logic

### 5. collect-test-artifacts

**Purpose**: Collect pod logs and upload artifacts

**Usage**:
```yaml
- uses: ./.github/actions/collect-test-artifacts
  with:
    artifact-name: test-logs-${{ matrix.test }}
    log-path: ./test
```

**Replaces**: 12+ instances of artifact collection

## Reusable Workflows

### 1. reusable-build-operator.yml

**Purpose**: Build operator image with vulnerability scanning and signing

**Usage**:
```yaml
jobs:
  build:
    uses: ./.github/workflows/reusable-build-operator.yml
    with:
      image-tag: ${{ github.sha }}
      sign-image: true
      run-vulnerability-scan: true
    secrets:
      ECR_REPOSITORY: ${{ secrets.ECR_REPOSITORY }}
      AWS_ACCESS_KEY_ID: ${{ secrets.AWS_ACCESS_KEY_ID }}
      AWS_SECRET_ACCESS_KEY: ${{ secrets.AWS_SECRET_ACCESS_KEY }}
      AWS_DEFAULT_REGION: ${{ secrets.AWS_DEFAULT_REGION }}
      COSIGN_PRIVATE_KEY: ${{ secrets.COSIGN_PRIVATE_KEY }}
      COSIGN_PASSWORD: ${{ secrets.COSIGN_PASSWORD }}
```

**Outputs**:
- `image-name`: Full image name with tag
- `image-digest`: Image SHA digest

**Replaces**: Duplicate build logic in 10+ workflows

### 2. reusable-integration-test.yml

**Purpose**: Run integration tests on any cloud platform (EKS, AKS, GKE)

**Usage**:
```yaml
jobs:
  test:
    uses: ./.github/workflows/reusable-integration-test.yml
    with:
      cluster-platform: eks
      test-focus: appframeworksS1
      cluster-name: eks-test-${{ github.run_id }}
      cluster-workers: 5
      cluster-nodes: 2
      splunk-operator-image: ${{ needs.build.outputs.image-name }}
      splunk-enterprise-image: ${{ secrets.SPLUNK_ENTERPRISE_IMAGE }}
      cluster-wide: true
    secrets:
      AWS_ACCESS_KEY_ID: ${{ secrets.AWS_ACCESS_KEY_ID }}
      AWS_SECRET_ACCESS_KEY: ${{ secrets.AWS_SECRET_ACCESS_KEY }}
      AWS_DEFAULT_REGION: ${{ secrets.AWS_DEFAULT_REGION }}
```

**Replaces**: 10+ separate integration test workflows

## Migration Plan

### Phase 1: Foundation (Week 1)
- ✅ Create composite actions directory structure
- ✅ Implement 5 core composite actions
- ✅ Create 2 reusable workflows
- ✅ Document usage and examples
- ⏳ Test composite actions in isolation

### Phase 2: Pilot Migration (Week 2)
- ⏳ Migrate `build-test-push-workflow.yml` to use new actions
- ⏳ Run pilot workflow in parallel with existing
- ⏳ Compare results and performance
- ⏳ Fix any issues discovered

### Phase 3: Batch Migration (Weeks 3-4)

**Group A - Build Workflows** (3 workflows):
- arm-AL2023-build-test-push-workflow-AL2023.yml
- arm-RHEL-build-test-push-workflow.yml
- arm-Ubuntu-build-test-push-workflow.yml
- distroless-build-test-push-workflow.yml

**Group B - Integration Tests** (8 workflows):
- int-test-workflow.yml
- int-test-azure-workflow.yml
- int-test-gcp-workflow.yml
- arm-AL2023-int-test-workflow.yml
- arm-RHEL-int-test-workflow.yml
- arm-Ubuntu-int-test-workflow.yml
- distroless-int-test-workflow.yml
- manual-int-test-workflow.yml

**Group C - Specialized Tests** (4 workflows):
- helm-test-workflow.yml
- namespace-scope-int-workflow.yml
- nightly-int-test-workflow.yml
- kubectl-splunk-workflow.yml

**Group D - Release Workflows** (4 workflows):
- Keep mostly as-is, minimal changes
- Update action versions only

**Group E - Security Workflows** (2 workflows):
- Keep as-is, update action versions

### Phase 4: Cleanup (Week 5)
- Remove old duplicate code
- Update documentation
- Archive old workflows for reference

## Action Version Standardization

### Updated Action Versions

| Action | Old Version | New Version | Status |
|--------|-------------|-------------|--------|
| actions/checkout | @v2, @v3 | @v4 | ✅ Updated in new workflows |
| actions/setup-go | @v2 | @v5 | ✅ Updated in new workflows |
| actions/setup-python | @v2 | @v5 | ⏳ To update |
| actions/upload-artifact | @v4.4.0 | @v4 | ✅ Standardized |
| docker/setup-buildx-action | @v2.5.0 | @v3 | ✅ Updated |
| docker/login-action | @v1, @v3 | @v3 | ✅ Standardized |
| aws-actions/configure-aws-credentials | @v1, @v4 | @v4 | ✅ Standardized |
| aws-actions/amazon-ecr-login | @v1 | @v2 | ✅ Updated |
| Azure/setup-kubectl | @v3 | @v4 | ✅ Updated |
| azure/login | @v1 | @v2 | ✅ Updated |
| google-github-actions/auth | @v1 | @v2 | ✅ Updated |
| google-github-actions/setup-gcloud | @v1 | @v2 | ✅ Updated |

## Benefits

### Reduced Maintenance
- **Before**: Update Operator SDK version in 13 files
- **After**: Update in 1 composite action + `.env` file

### Consistency
- All workflows use same tool installation logic
- Standardized authentication patterns
- Consistent error handling

### Code Reduction
- **Before**: ~5000+ lines across all workflows
- **After**: ~2000 lines (60% reduction estimated)

### Improved Readability
- Workflows focus on business logic, not boilerplate
- Clear separation of concerns
- Self-documenting workflow names

### Easier Testing
- Composite actions can be tested independently
- Reusable workflows reduce test matrix

## Best Practices

### When to Use Composite Actions
- ✅ Repeated setup steps (tool installation, auth)
- ✅ Common cleanup patterns
- ✅ Standard artifact collection
- ❌ Complex logic with many conditionals
- ❌ Workflow-specific business logic

### When to Use Reusable Workflows
- ✅ Complete job patterns (build, test, deploy)
- ✅ Multi-step processes with dependencies
- ✅ Jobs that need to run on specific runners
- ❌ Simple 1-2 step operations
- ❌ Highly customized one-off workflows

### Composite Action Guidelines
1. **Single Responsibility**: Each action should do one thing well
2. **Parameterization**: Make actions flexible with inputs
3. **Default Values**: Provide sensible defaults
4. **Documentation**: Document all inputs, outputs, and usage
5. **Error Handling**: Use `shell: bash` with proper error codes

### Reusable Workflow Guidelines
1. **Explicit Inputs**: Define all required inputs clearly
2. **Secret Inheritance**: Pass secrets explicitly, don't rely on inheritance
3. **Outputs**: Expose useful outputs for downstream jobs
4. **Matrix Support**: Design for matrix strategy when applicable
5. **Platform Agnostic**: Don't hardcode platform-specific details

## Testing Strategy

### Testing Composite Actions
1. Create minimal test workflow
2. Test each input parameter
3. Verify outputs are correct
4. Test error conditions
5. Run on multiple runners (ubuntu, macos)

### Testing Reusable Workflows
1. Call from test workflow with various inputs
2. Verify job outputs
3. Test secret passing
4. Validate error handling
5. Confirm artifact uploads

### Validation Checklist
- [ ] Workflow syntax is valid (use `actionlint`)
- [ ] All secrets are properly passed
- [ ] Environment variables are set correctly
- [ ] Error handling works as expected
- [ ] Artifacts are uploaded correctly
- [ ] Cleanup runs on failure (`if: always()`)

## Rollback Plan

If issues are discovered after migration:

1. **Immediate Rollback**: Keep old workflows with `-legacy` suffix
2. **Gradual Migration**: Enable new workflows with `workflow_dispatch` first
3. **Parallel Running**: Run both old and new for 1-2 weeks
4. **Monitoring**: Compare execution times and failure rates

## Monitoring & Metrics

Track these metrics before and after migration:

- Workflow execution time
- Failure rate
- Lines of code in workflows
- Time to update tool versions
- Developer satisfaction (survey)

## FAQ

### Q: Will this break existing workflows?
**A**: No, the migration is opt-in. Old workflows continue to work until migrated.

### Q: How do I test a composite action locally?
**A**: Use `act` (https://github.com/nektos/act) to run workflows locally.

### Q: Can I use composite actions from another repo?
**A**: Yes, but it's recommended to keep them in the same repo for versioning.

### Q: What if I need to customize a composite action?
**A**: Use inputs to parameterize behavior. If that's not enough, create a specialized version.

### Q: How do I version composite actions?
**A**: Use git tags or branches. Reference them like: `uses: org/repo/.github/actions/my-action@v1`

## Resources

- [GitHub Actions: Composite Actions](https://docs.github.com/en/actions/creating-actions/creating-a-composite-action)
- [GitHub Actions: Reusable Workflows](https://docs.github.com/en/actions/using-workflows/reusing-workflows)
- [Action Lint](https://github.com/rhysd/actionlint)
- [Act - Run workflows locally](https://github.com/nektos/act)

## Changelog

### 2025-11-16
- Initial architecture design
- Created 5 composite actions
- Created 2 reusable workflows
- Created example modernized workflow
- Documentation created

### Next Steps
- [ ] Test composite actions in isolated workflow
- [ ] Migrate pilot workflow (build-test-push)
- [ ] Gather feedback from team
- [ ] Begin batch migration

---

**Maintained by**: Platform Engineering Team
**Last Updated**: 2025-11-16
**Version**: 1.0.0
