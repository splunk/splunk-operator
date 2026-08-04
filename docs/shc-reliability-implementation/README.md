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

## Active bounded investigation

SHC-98 is testing whether Splunk's supported
`server.conf/[clustering]/register_search_address` can replace ephemeral
indexer Pod-IP peer identities with stable StatefulSet Pod FQDNs. The source
candidate is isolated across Operator, Docker-Splunk, and Splunk Ansible. It is
not yet EKS-qualified and is not presented as closure of the existing
successful-but-incomplete distributed-search finding. The plan, acceptance
criteria, and current evidence are in
`SHC98StableIndexerSearchAddressExecPlan.md`.

## Latest bounded qualification

SHC-107 exact test source `3e9f47751` completed two Operator-owned Search Head
`2 -> 1 -> 0` rolls on the no-service-mesh EKS topology. The transport-only
control delivered 1,800 events exactly but recorded 218 searches rejected on
connections pinned to detained members. The response-aware run closed and
retried four explicit HTTP 405 detention responses, completed 1,200 events
with zero logical or count-regression failure, and survived deletion of the
active Operator during durable ordinal-2 work. It finished with three `Up`
members, three serving endpoints, a ready captain, and zero restarts. This is
a bounded client-mitigation and lifecycle result, not a Splunkd fix or final
candidate-image certification. A subsequent full indexer `3 -> 2 -> 1 -> 0`
roll delivered 2,400 events exactly and recovered one explicit HEC 503, but
three HTTP-successful searches regressed by up to 847 events. The controller
also selected ordinal 2 before ordinal 3 had converged on every Search Head;
exact four-peer convergence followed lifecycle completion by 1,583 seconds.
Mesh/ingress, HTTP HEC, candidate-image, immediate-completeness, and
release-soak gates remain open.

SHC-97 qualified the bounded Docker-Splunk single-start contract using exact
Splunk-Ansible source `ae8ecf4a` and Docker-Splunk source `118cae68`. The
Linux-built runtime OCI index
`sha256:49b12103f8444319dcf823eb829d2dfc020410e44d46273461c1b15e52c724fd`
was exercised on EKS 1.31.14 with official Splunk build
`10.5.2605.0/844c593e9c1d`.

Two same-PVC Cluster Manager replacements each issued one initial start,
completed Ansible with `failed=0`, reported no port 8191 conflict, became
Ready, and recorded zero container restarts. The same immutable runtime then
converged across LicenseManager, four indexers, the Deployer, and three Search
Heads. Every managed tier finished Ready with unchanged PVC identities and
zero container restarts. The Search Head rollout retained at least two serving
endpoints and transferred captaincy before each active-captain replacement.

This is a bounded source, packaging, same-PVC startup, and full-topology
startup qualification, not completion of every runtime scenario. The
live initial starts returned zero, so the conditional nonzero status-poll path
is established by executable source tests rather than a deliberately failed
production-style Pod. Immediate distributed-search completeness during
indexer replacement and provider/version breadth remain separate work. Exact
evidence and limits are in
`SHC97DockerSplunkStartupQualification.md`.

## Review order

1. `CurrentDevelopBaseline.md` establishes what exists now.
2. `SHC112IndexerSearchPeerConvergenceGateExecPlan.md` records the durable
   Operator-owned indexer advancement gate and its separate Splunk Enterprise
   boundary.
3. `SHC113SplunkRESTResponseClosureExecPlan.md` records the shared REST-client
   response ownership correction and its immutable resource-soak gate.
4. `SHC114BoundedSearchPeerObservationExecPlan.md` records the bounded,
   cancellation-aware peer-observation batch and its live controller gate.
5. `SHC115ShortLivedRESTTransportOwnershipExecPlan.md` records the
   repository-wide short-lived Splunk REST transport ownership rule and live
   resource-soak gate.
6. `SHC116IndexerEndpointWithdrawalExecPlan.md` records the durable
   EndpointSlice-withdrawal propagation barrier before indexer decommission.
7. `SHC117LongIndexerRollQualificationExecPlan.md` records the extended,
   finite evidence window required to observe every indexer ordinal.
8. `SHC118SearchHeadEndpointWithdrawalExecPlan.md` records the corresponding
   propagation barrier required before Search Head detention.
9. `SHC111ProtocolQualificationExecPlan.md` records explicit HTTP/HTTPS
   persistent-client variants and their distinct network-topology gates.
10. `SHC107PersistentClientQualificationExecPlan.md` defines the bounded
   long-lived Kubernetes Service client evidence and remaining live gates.
11. `SHC106DeployerMemberCoordinationExecPlan.md` records the live
   Deployer/member overlap, exact source correction, completed regression
   gates, and remaining immutable Linux/EKS acceptance boundary.
12. `SHC105AppFrameworkRequeueBoundaryExecPlan.md` records the live
   timing defect, exact source correction, completed regression gates, and the
   remaining immutable Linux/EKS acceptance boundary.
13. `SHCFinalBranchManifest.md` records exact accepted branch tips, merge
   ancestry, superseded branch dispositions, and cumulative repository gates.
14. `SHCFinalIntegrationExecPlan.md` records the assembly of the final Operator
   and runtime feature branches and the exact remaining qualification gates.
15. `SHCImplementationExecPlan.md` defines milestones, dependencies, gates, and
   acceptance evidence.
16. `SHCWorkItemIndex.md` maps the bounded `SHC-*` execution records to
   immutable commits and stable scenario identifiers.
17. `SHC98StableIndexerSearchAddressExecPlan.md` records the active bounded
   distributed-peer convergence experiment.
18. `SHC97DockerSplunkStartupQualification.md` records the latest completed
   runtime startup and full-topology qualification.
19. `SHC93OperatorReadinessQualification.md` and
   `SHC93OperatorReadinessExecPlan.md` record the latest manager-readiness
   contract, exact evidence, rejected candidates, and remaining boundaries.
20. `OperatorLifecycleTechnicalDesign.md` will define the CRD, controller state
   machine, StatefulSet strategy, and durable status contract.
21. `SHCImageUpgradeWorkflowTechnicalDesign.md` defines the OPS-007
   cluster-wide image-upgrade workflow and its composition with per-member
   lifecycle orchestration.
22. `RuntimeLifecycleContract.md` defines the contract between the Operator,
   Pod lifecycle, probe scripts, Docker-Splunk/Splunk Ansible, and splunkd.
23. `SplunkEnterpriseIndexerRollingRestartRequirements.md` records the
   Splunk-managed indexer restart boundary and the remote serving-recovery
   contract that cannot be completed by an Operator readiness probe.
24. `ParallelWorkstreamPlan.md` defines branch ownership, dependency waves,
   integration rules, and conflict prevention.
25. `SHCTestScenarioMatrix.md` defines the complete stable scenario inventory
   and common pass invariants.
26. `QualificationObservabilityRolloutPlan.md` is the executable test,
   evidence, migration, release, and rollback plan.
27. `RuntimeLinuxBuildHandoffManifest.example.yaml` is the source-to-builder
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
