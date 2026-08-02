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

SHC-93 qualified the bounded Operator reconciliation-readiness contract at
exact source `90103bef5`. `/healthz` remains a process-local signal;
`/readyz` now requires the complete initial controller informer barrier and a
current exact authorization review for the leader Lease. Current leadership
remains a separate metric, so a synchronized and authorized HA standby is
Ready. The final source passed 43 macOS and Linux suites, all 185 enterprise
controller specs, build, focused race, Kustomize, and Helm gates. Immutable
Operator OCI index
`sha256:b5a022a788c7cacf8b7ee33e7132eae56d82b14eb631809ddd116c8b816e9d63`
and chart SHA-256
`008abda67d13775ce6cd7e0f8e77365edce01af82f6ad9c12ecf34911a2f6925`
were exercised on EKS 1.31.14.

Cold informer and Lease denials kept the manager process healthy but the
Deployment unavailable, and restored access recovered the same Pod without a
restart. Two Ready contenders completed leader takeover in 35 seconds. During
an active-leader API interruption, controller-runtime exited after loss of
Lease renewal as designed; the API-isolated restart served health, remained
NotReady behind the informer barrier, and did not CrashLoop. The secure
manager metrics Service retained the NotReady endpoint for diagnosis. Normal
cleanup removed every SHC-93 fixture while the retained SHC stayed 3/3 Ready
with zero restarts.

This is bounded manager qualification, not completion of every scenario in
the program. Live evidence covers one EKS 1.31.14 cluster. Other providers and
versions, productized manager replica/rollout configuration, ongoing
post-start per-informer health, and production alert delivery remain separate
work. Exact evidence and rejected intermediate designs are in
`SHC93OperatorReadinessQualification.md` and
`SHC93OperatorReadinessExecPlan.md`.

## Review order

1. `CurrentDevelopBaseline.md` establishes what exists now.
2. `SHCImplementationExecPlan.md` defines milestones, dependencies, gates, and
   acceptance evidence.
3. `SHCWorkItemIndex.md` maps the bounded `SHC-*` execution records to
   immutable commits and stable scenario identifiers.
4. `SHC93OperatorReadinessQualification.md` and
   `SHC93OperatorReadinessExecPlan.md` record the latest manager-readiness
   contract, exact evidence, rejected candidates, and remaining boundaries.
5. `OperatorLifecycleTechnicalDesign.md` will define the CRD, controller state
   machine, StatefulSet strategy, and durable status contract.
6. `SHCImageUpgradeWorkflowTechnicalDesign.md` defines the OPS-007
   cluster-wide image-upgrade workflow and its composition with per-member
   lifecycle orchestration.
7. `RuntimeLifecycleContract.md` defines the contract between the Operator,
   Pod lifecycle, probe scripts, Docker-Splunk/Splunk Ansible, and splunkd.
8. `SplunkEnterpriseIndexerRollingRestartRequirements.md` records the
   Splunk-managed indexer restart boundary and the remote serving-recovery
   contract that cannot be completed by an Operator readiness probe.
9. `ParallelWorkstreamPlan.md` defines branch ownership, dependency waves,
   integration rules, and conflict prevention.
10. `SHCTestScenarioMatrix.md` defines the complete stable scenario inventory
   and common pass invariants.
11. `QualificationObservabilityRolloutPlan.md` is the executable test,
   evidence, migration, release, and rollback plan.
12. `RuntimeLinuxBuildHandoffManifest.example.yaml` is the source-to-builder
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
