# Search Head Cluster Reliability Implementation Planning

This directory turns the product and architecture requirements in
`docs/SearchHeadClusterKubernetesStabilizationRequirements.md` into an
implementation program for Splunk Operator, Docker-Splunk/Splunk Ansible, and
Splunk Enterprise integration.

The planning baseline is the GitLab `sok/develop` branch at commit
`39316c19fb990f1af84966d5269a8f4116550dbb`, observed on 2026-07-24. The commit
is an evidence anchor, not a permanent product claim. Every implementation
milestone must refresh the baseline before design approval and again before
merge.

These documents do not declare the earlier requirements implemented. They
separate:

1. behavior verified in the current development baseline;
2. useful work on unmerged branches that may be reused after review;
3. design decisions that still require agreement; and
4. implementation and qualification work that remains.

## Latest bounded qualification

SHC-89 qualified schema-valid, quiet status for custom resources created
already paused. Exact source `3e1716737` passed 41 Linux suites and 157 specs.
Its immutable Operator image initialized all seven active v4 Splunk resource
kinds to `Pending/Paused`, including SearchHeadCluster `deployerPhase`, wrote
the state once, created no managed workload, and emitted no Reconciler error.
After annotation removal, a LicenseManager and three-member SHC followed
ordinary reconciliation to Ready; the SHC finished with three endpoints, all
members Up, zero restarts, and direct search success on every member.

This remains bounded spike evidence; it is not a declaration that every
scenario in the matrix is complete or that the feature is ready for default
enablement. Queue and ObjectStorage have no active enterprise reconcilers in
this source baseline and are not live qualification targets. The source audit
also registered SHC-91 for deletion-before-pause ordering in five active v4
controllers; SHC-90 remains the separate namespace-termination propagation
guard.
Exact source, image, timing, and remaining-boundary evidence is in
`SHC89PausedStatusQualification.md`, `SHCWorkItemIndex.md`, and
`QualificationObservabilityRolloutPlan.md`.

## Review order

1. `CurrentDevelopBaseline.md` establishes what exists now.
2. `SHCImplementationExecPlan.md` defines milestones, dependencies, gates, and
   acceptance evidence.
3. `SHCWorkItemIndex.md` maps the bounded `SHC-*` execution records to
   immutable commits and stable scenario identifiers.
4. `OperatorLifecycleTechnicalDesign.md` will define the CRD, controller state
   machine, StatefulSet strategy, and durable status contract.
5. `SHCImageUpgradeWorkflowTechnicalDesign.md` defines the OPS-007
   cluster-wide image-upgrade workflow and its composition with per-member
   lifecycle orchestration.
6. `RuntimeLifecycleContract.md` defines the contract between the Operator,
   Pod lifecycle, probe scripts, Docker-Splunk/Splunk Ansible, and splunkd.
7. `SplunkEnterpriseIndexerRollingRestartRequirements.md` records the
   Splunk-managed indexer restart boundary and the remote serving-recovery
   contract that cannot be completed by an Operator readiness probe.
8. `ParallelWorkstreamPlan.md` defines branch ownership, dependency waves,
   integration rules, and conflict prevention.
9. `SHCTestScenarioMatrix.md` defines the complete stable scenario inventory
   and common pass invariants.
10. `QualificationObservabilityRolloutPlan.md` is the executable test,
   evidence, migration, release, and rollback plan.
11. `RuntimeLinuxBuildHandoffManifest.example.yaml` is the source-to-builder
   contract for Docker-Splunk image construction on a supported Linux host.

The Operator design is a Wave 0 spike contract and the runtime design now
contains the startup, shutdown, captain-identity, and dynamic-target decisions
found during implementation. Both still require approval and integrated
qualification before production delivery.

## Branching rule

Implementation branches should be created from the current GitLab
`sok/develop` revision after the relevant design is approved. Planning
documents may be reviewed independently. Existing experimental branches are
inputs to review; they are not the integration base and must not be merged
without rebasing, code review, and current-baseline validation.
