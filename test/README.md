# Integration testing for Splunk Operator

The full integration test guide lives in [docs/develop/IntegrationTesting.md](../docs/develop/IntegrationTesting.md), including local k3s workstation setup.

When modifying the test framework (`test/testenv/`, `test/run-tests.sh`, `test/env.sh`), please update that document to keep it accurate.

## SCS sanity gate (`test/scs_sanity/`)

Attaches to an **already-running** per-environment SOK instance on an SCS cluster (no
namespace/CR provisioning) to gate promotion in the `scs_deploy` Loki pipeline lane
(`gitlab-ci/scs-deploy-loki.sh`, via `gitlab-ci/scs-sanity-gate.sh`). All specs are
labeled `tier:scs-sanity` plus a phase label, since the real Helm upgrade happens
between two separate invocations of the precompiled ginkgo binary:

- `phase:pre-upgrade` — run before the upgrade; captures a tenant `IngestorCluster`
  baseline (spec, restart counts, a HEC marker) to `SCS_SANITY_BASELINE_FILE`.
- `phase:post-upgrade-operator` — run after `verify_operator`, unconditionally
  (including on a brand-new environment with no pre-existing Helm release): checks
  operator Deployment health and leader election only, since those never require an
  existing tenant.
- `phase:post-upgrade-tenant` — run after `phase:post-upgrade-operator`, but only when
  an existing tenant was present before the upgrade (`RELEASE_PRESENT=true`): re-checks
  CR readiness and HEC ingest, then diffs against the baseline file for disruption.

Env-var contract (set by `scs-sanity-gate.sh`, with `SCS_INGESTOR_NAME`/
`SCS_INGESTOR_NAMESPACE` as optional discovery overrides): `SCS_OPERATOR_NAMESPACE`,
`TARGET_OPERATOR_IMAGE`, `SCS_SANITY_BASELINE_FILE`.
