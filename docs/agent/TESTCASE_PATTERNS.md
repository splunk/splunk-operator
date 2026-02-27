# Testcase Patterns

This doc maps common Splunk Validated Architectures (SVA) and features to test helpers.

## Integration Helpers (test/testenv/deployment.go)

S1 (Standalone)
- `DeployStandalone` (basic)
- `DeployStandaloneWithGivenSmartStoreSpec` (smartstore)

C3 (Single-site cluster, SHC optional)
- `DeploySingleSiteCluster` (basic)
- `DeploySingleSiteClusterWithGivenAppFrameworkSpec` (app framework)
- SmartStore for C3 requires manual flow: use `DeployClusterManagerWithSmartStoreIndexes` plus `DeployIndexerCluster` and optional SHC.

M4 (Multisite with SHC)
- `DeployMultisiteClusterWithSearchHead` (basic)
- `DeployMultisiteClusterWithSearchHeadAndAppFramework` (app framework)
- `DeployMultisiteClusterWithSearchHeadAndIndexes` (smartstore)

M1 (Multisite, no SHC)
- `DeployMultisiteCluster` (basic)
- For app framework, use `DeployMultisiteClusterWithSearchHeadAndAppFramework` with `shc=false`
- SmartStore without SHC requires manual flow or a custom helper

## Readiness Helpers (test/testenv/verificationutils.go)
- `StandaloneReady`
- `ClusterManagerReady` or `ClusterMasterReady`
- `SearchHeadClusterReady`
- `SingleSiteIndexersReady` or `IndexersReady`
- `IndexerClusterMultisiteStatus`
- `VerifyRFSFMet`
- `LicenseManagerReady` or `LicenseMasterReady`
- `VerifyMonitoringConsoleReady`

## SVA Validation (Integration)
Use `validations` in `docs/agent/TESTCASE_SPEC.yaml` to auto-add readiness checks.
- For C3 SVA: `validations.sva: C3` (adds Monitoring Console + License Manager checks unless disabled)
- Ensure a license file/configmap is configured when enabling License Manager checks

## App Framework Helpers
- `GenerateAppFrameworkSpec` (test/testenv/appframework_utils.go)
- `DeploySingleSiteClusterWithGivenAppFrameworkSpec`
- `DeployMultisiteClusterWithSearchHeadAndAppFramework`

## SmartStore Helpers
- `DeployStandaloneWithGivenSmartStoreSpec`
- `DeployClusterManagerWithSmartStoreIndexes`
- `DeployMultisiteClusterWithSearchHeadAndIndexes`

## Operator Upgrade (KUTTL)
- Use `upgrade` in `docs/agent/TESTCASE_SPEC.yaml` to generate helm install/upgrade steps.
- Example suite: `kuttl/tests/upgrade/c3-with-operator` (checks operator deployment and image).
