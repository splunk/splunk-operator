# Qualify stable indexer search addresses during Kubernetes Pod replacement

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to
date as work proceeds.

This document is maintained in accordance with the ExecPlan requirements in
the `execution-plan` skill.

## Purpose / Big Picture

During the qualified SHC-85 and SHC-97 indexer replacement campaigns, every
HEC request and distributed-search request succeeded, but some successful
searches temporarily returned fewer previously acknowledged events than an
earlier successful search. The results later converged exactly. A successful
HTTP response therefore did not prove immediate distributed-search
completeness.

The observed Search Head peer inventory identified each indexer by its current
Pod IP. A StatefulSet preserves a Pod's ordinal and storage identity across
replacement but assigns a new Pod IP. Splunk then had to remove the old peer
address and converge on the replacement address independently on every Search
Head. The worst earlier samples occurred after the Operator lifecycle had
already completed, all indexer Pods were Ready, and Kubernetes had published
their endpoints.

SHC-98 is a bounded experiment, not a pre-declared fix. It uses Splunk
Enterprise's supported `server.conf/[clustering]/register_search_address`
setting so each indexer advertises its stable StatefulSet Pod DNS name rather
than its replaceable Pod IP. The experiment is accepted only if exact source
gates pass and an immutable EKS indexer rollout proves that the Search Heads
retain the stable names, converge correctly, and materially improve the
customer-visible workload record without introducing a new startup,
readiness, DNS, or replacement failure.

Even an accepted SHC-98 result does not add a Splunk partial-result signal.
Explicit signaling that a successful distributed search is incomplete remains
a Splunk Enterprise requirement until that behavior exists and is qualified.

## Progress

- [x] (2026-08-03 00:55Z) Revalidated the live SHC-97 topology and Splunk
  contract before changing source. All three Search Heads listed the four
  indexers as current Pod-IP management endpoints; all were `Up`. The
  indexers had no effective `register_search_address`,
  `register_replication_address`, or `register_forwarder_address` setting.
- [x] (2026-08-03 01:02Z) Verified that all four live indexer containers
  resolve `socket.getfqdn()` to their stable per-Pod name under the existing
  headless Service. The Service is headless and publishes not-ready addresses.
- [x] (2026-08-03 01:07Z) Traced the local Splunk Enterprise source contract.
  The Cluster Manager uses the peer's connection address when the add-peer
  request omits `searchAddress`, overrides it when the request includes the
  supported registered search address, and distributes the resulting search
  host/port to Search Heads.
- [x] (2026-08-03 01:13Z) Created isolated
  `codex/shc-98-stable-indexer-search-address` worktrees for Splunk Ansible,
  Docker-Splunk, and Splunk Operator from their accepted SHC-97 feature tips.
- [x] (2026-08-03 01:16Z) Added opt-in Splunk Ansible support for
  `SPLUNK_IDXC_REGISTER_SEARCH_ADDRESS`. The value `auto` resolves an existing
  `SPLUNK_HOSTNAME` or the system FQDN. The supported setting is written and
  verified before the only Splunk start; it requests no restart.
- [x] (2026-08-03 01:16Z) Added the Operator's clustered-indexer candidate
  default of `auto`, with golden and unit evidence that customer
  `spec.extraEnv` can replace the default with an explicit address.
- [x] (2026-08-03 01:17Z) Pinned Docker-Splunk to exact Splunk Ansible source
  and passed its deterministic four-test dependency-ref Make gate.
- [x] (2026-08-03 01:27Z) Found and corrected a null-default compatibility
  defect before Linux execution. Existing non-opted-in Ansible deployments
  have a defined YAML null value; the include guard now treats undefined,
  null, and empty values as a no-op. Sixty focused Python tests pass.
- [x] (2026-08-03 01:50Z) Passed the complete local Splunk Ansible
  `make shc-check` under the repository's pinned Ansible 5.10 environment on
  Python 3.9: focused legacy lint, full playbook syntax, 60 clustering
  environment tests, five stable-address task tests, and two startup tests.
- [x] (2026-08-03 01:51Z) Ran Docker-Splunk `make ansible`; it cloned and
  detached at the exact pinned source, matching the Makefile and recorded
  version.
- [x] (2026-08-03 01:56Z) Added and source-qualified an explicit `absent`
  rollback value while preserving undefined/null/empty as an unmanaged no-op.
- [x] (2026-08-03 01:27Z) Operator focused generation tests, `make build`, Go
  formatting, vet, and compilation pass. The complete macOS Make test reached
  42 passing suites and one unrelated `pkg/splunk/enterprise` failure.
- [x] (2026-08-03 01:28Z) Isolated that unrelated failure: the existing probe
  test's broad `splunkd.*start` process match sees a local Coder command whose
  hostname contains `splunkd` and whose options contain `autostart`. Record
  and correct this independently; do not attribute it to stable addressing.
- [x] (2026-08-03 02:02Z) Restricted automatic Operator delivery to the
  existing combined `SplunkPodLifecycle` and `IndexerClusterLifecycle` Alpha
  gate. With either gate disabled, the Operator adds no candidate default;
  an explicit customer `spec.extraEnv` value remains available. Ten repeated
  focused runs and `make build` pass at Operator source `2c607d6e2`.
- [x] (2026-08-03 02:03Z) Revalidated the retained EKS control boundary. The
  deployed Operator watches all namespaces, the target namespace contains the
  only IndexerCluster in the cluster, all four managed tiers are Ready, and
  the three lifecycle Alpha gates are enabled. No cluster resource was
  changed. The rollout plan therefore pauses every target CR before changing
  the Operator or desired runtime and converges dependencies before the
  IndexerCluster is unpaused.
- [x] (2026-08-03 02:10Z) Composed exact SHC-98 tip `9e3b24761` with only the
  isolated SHC-99 process-matcher source `184061106` in a disposable detached
  worktree. `make test` passed all 43 suites, including all 192 controller
  specs, with zero failures and 78.3 percent composite coverage. This proves
  local composition; the review branches remain separate and native Linux
  qualification is still required.
- [x] (2026-08-03 02:20Z) Added the read-only SHC-98 peer-convergence monitor
  at `78ff404c7`. Its Make gate passes Bash syntax and ShellCheck. A live
  snapshot queried all three Search Heads and Cluster Manager without a
  cluster mutation: all four peers were `Up` and searchable, but both Cluster
  Manager and every Search Head still exposed Pod-IP search addresses while
  all four indexers resolved the expected stable per-ordinal FQDN.
- [x] (2026-08-03 02:22Z) Corrected the qualification invariant after the live
  snapshot showed current `OnDelete` semantics. All four Pods carried the
  StatefulSet `updateRevision` and the lifecycle was `Completed`, while
  Kubernetes retained the older `currentRevision`. The monitor now requires
  every Pod to carry `updateRevision` for `OnDelete` and requires
  `currentRevision == updateRevision` only for `RollingUpdate`.
- [x] (2026-08-03 02:29Z) Added a separate API-independent SHC-98 workload Job
  at `aa9a566aa`. It is pinned to the retained accepted runtime OCI index,
  uses a unique Pod-derived run ID, mounts no service-account token, and
  passed client and live server-side dry-run validation. The monitor's
  snapshot mode and expected no-roll failure path both execute successfully.
- [x] (2026-08-03 02:40Z) Corrected the configuration ownership boundary before
  Linux execution. `auto` now adopts an empty effective setting but preserves
  an existing unmanaged customer value. Explicit input records persistent
  Ansible ownership, and `absent` removes only a setting carrying that
  ownership marker. A subsequent review corrected empty `SPLUNK_HOSTNAME` so
  `auto` falls back to the system FQDN as specified. The complete Ansible SHC
  Make gate passed with 62 clustering environment tests, seven structural
  stable-address tests, eight executable ownership scenarios, and two startup
  tests. The executable scenarios prove adoption, idempotence, unmanaged
  preservation, owned rollback, explicit override, and customer takeover
  after prior ownership; a truncated empty marker is also treated as unowned.
  A prefix-collision negative proves ownership requires exact `btool` line
  equality. Docker-Splunk's four dependency-ref tests and exact detached
  checkout passed against Ansible `9dff0999c` and Docker source `6ee266c1`.
- [ ] Run the authoritative Splunk Ansible `make shc-check`, Operator
  `make test` and `make build`, and Docker-Splunk dependency/build gates on a
  clean Linux AMD64 vWorkstation. The Coder API currently returns EOF before
  SSH establishment; retry without weakening any gate.
- [ ] Build and publish immutable Linux AMD64 Docker-Splunk and Operator
  images, record their source commits and OCI digests, and verify the runtime
  contains the exact pinned Ansible commit.
- [ ] Deploy one combined desired indexer revision so stable-address
  configuration and runtime image do not cause two separate rolls.
- [ ] Run continuous acknowledged ingest, distributed search, per-Search-Head
  peer inventory, cluster health, endpoint, restart, and lifecycle observation
  through the complete Operator-owned `3 -> 2 -> 1 -> 0` indexer roll.
- [ ] Accept, revise, or reject the candidate from evidence. Update all
  program documents and integrate accepted commits only after that decision.

## Surprises & Discoveries

- Observation: Kubernetes readiness and Cluster Manager RF/SF health did not
  prove that every Search Head had converged its distributed-peer view.
  Evidence: earlier workloads recorded HTTP-successful count regressions after
  the lifecycle was `Completed`, while Search Heads still logged connection or
  authentication attempts to old Pod IPs.
  Consequence: SHC-98 observes `/services/search/distributed/peers` separately
  on every Search Head and treats agreement as a distinct gate.
- Observation: the current live peer inventory contains Pod IPs because the
  registered search address is absent.
  Evidence: all three Search Heads reported the same four `IP:8089` entries;
  indexer `btool server list clustering` returned no registered search,
  replication, or forwarder address.
  Consequence: changing a Kubernetes Service or readiness probe alone cannot
  alter the address Splunk advertises through Cluster Manager membership.
- Observation: the supported Splunk setting already carries a host or FQDN in
  the add-peer protocol; no Splunkd source change is required for this test.
  Evidence: the local specification defines `register_search_address`; the
  peer add request includes it; Cluster Manager otherwise falls back to the
  connection host and distributes the selected search host/port.
  Consequence: SHC-98 changes only Splunk Ansible, Docker-Splunk dependency
  selection, and Operator Pod configuration.
- Observation: the current StatefulSet/headless-Service identity already
  provides a stable candidate address without a service mesh.
  Evidence: every live indexer returned
  `<pod>.<headless-service>.<namespace>.svc.cluster.local` from
  `socket.getfqdn()`.
  Consequence: the experiment depends on Kubernetes cluster DNS and the
  existing StatefulSet subdomain, not on ingress TLS termination or a service
  mesh. Mesh and no-mesh deployments still need separate final qualification.
- Observation: the headless Service publishes a replacement indexer before it
  is Ready.
  Consequence: stable DNS removes address identity churn but does not make the
  replacement process ready sooner. Search Heads may resolve the stable name
  to a starting Pod and must recover through normal connection retry. The EKS
  test must not infer serving readiness from DNS resolution.
- Observation: the retained Operator watches all namespaces rather than only
  the qualification namespace.
  Evidence: its `WATCH_NAMESPACE` value is empty. A cluster-wide inventory
  found one IndexerCluster, the intended `shc-final-qualification/shcfinal-idxc`.
  Consequence: the experimental default must remain behind the existing Alpha
  lifecycle gates, and the live rollout must use tier-specific pause controls
  so deploying the candidate Operator cannot start an unintended revision.
- Observation: `OnDelete` StatefulSet status does not provide revision
  equality as a final convergence signal in the retained environment.
  Evidence: the lifecycle is `Completed`, all four indexer Pods carry
  `splunk-shcfinal-idxc-indexer-7d95fbc54b`, and
  `updatedReplicas=4`, while status retains older `currentRevision`
  `splunk-shcfinal-idxc-indexer-6968767b9b` and `currentReplicas=0`.
  Consequence: current-design qualification must prove that every Pod carries
  `updateRevision`; revision equality remains valid for the intended
  `RollingUpdate` workflow, not for this `OnDelete` compatibility path.
- Observation: Splunk Ansible's default `register_search_address` is defined
  as YAML null rather than undefined.
  Consequence: the role include must use `default("", true)` so existing
  deployments remain unchanged unless a value is explicitly provided.
- Observation: a broad process-name grep can produce a false positive outside
  a Splunk container.
  Evidence: the local Coder/VS Code command contains `splunkd` in a hostname
  and `start` inside `autostart`, causing the existing level-one liveness test
  to report success without a Splunk process.
  Consequence: track the probe matcher as an independent work item and run
  SHC-98's authoritative source gate on a process-clean Linux host.
- Observation: an automatic default must not overwrite a customer-supplied
  effective `register_search_address`, and an unscoped rollback must not
  delete one.
  Consequence: the pre-start role persists the last managed value only when
  this feature writes the setting. `auto` and `absent` act as owners only when
  the effective value still matches that record. A different customer value
  is preserved and stale ownership is relinquished. Six executable Ansible
  scenarios prove these transitions; equivalent EKS negatives remain open.
- Observation: `dict.get(key, fallback)` does not use the fallback when the
  key exists with an empty string.
  Consequence: automatic address selection uses non-empty `SPLUNK_HOSTNAME`
  or `socket.getfqdn()` explicitly, and a regression test covers an empty
  container hostname override.
- Observation: marker existence alone is insufficient proof of configuration
  ownership because the marker can be truncated independently of server.conf.
  Consequence: ownership requires a non-empty recorded value that matches the
  effective setting. An empty marker is treated as unowned, the customer value
  is preserved, and the obsolete marker is removed.
- Observation: substring comparison would misclassify a customer value such as
  `generated.example.attacker` as ownership of `generated.example`.
  Consequence: effective-setting discovery and ownership verification use
  exact `btool` output-line matches. The prefix-collision negative preserves
  the customer value and relinquishes the stale marker.

## Decision Log

- Decision: treat stable search addressing as an evidence-driven hypothesis,
  not as automatic closure of SHC-85 or OPS-011.
  Rationale: the prior records prove correlation with peer-address churn but
  do not prove that address churn is the only source of incomplete successful
  searches.
  Date/Author: 2026-08-03, Codex with Vivek Reddy.
- Decision: use Splunk's supported `register_search_address` setting and make
  no Splunkd modification.
  Rationale: the setting is already read, transmitted, stored, and distributed
  by the current Splunk implementation. The immediate task is to qualify that
  contract on Kubernetes.
  Date/Author: 2026-08-03, Codex with Vivek Reddy.
- Decision: keep Splunk Ansible behavior opt-in and make the Operator choose
  `auto` only for clustered indexer StatefulSets when both
  `SplunkPodLifecycle` and `IndexerClusterLifecycle` are enabled.
  Rationale: non-Kubernetes Ansible users may have DNS and routing assumptions
  that cannot be inferred safely. The Operator owns a stable per-ordinal DNS
  identity, but this remains an Alpha lifecycle experiment and must not alter
  the current default-disabled contract.
  Date/Author: 2026-08-03, Codex with Vivek Reddy.
- Decision: allow `spec.extraEnv` to override the Operator default.
  Rationale: customers with an explicit reachable address or different DNS
  design need a supported escape hatch, and the existing environment merge
  contract already provides that precedence.
  Date/Author: 2026-08-03, Codex with Vivek Reddy.
- Decision: write and verify the setting before Splunk starts, without a
  handler or second `splunk start`.
  Rationale: the peer registers during startup; adding a post-start mutation
  and restart would conflict with the SHC-97 single-start contract and create
  avoidable Pod initialization work.
  Date/Author: 2026-08-03, Codex with Vivek Reddy.
- Decision: combine the runtime-image and Pod-environment change into one
  indexer StatefulSet revision.
  Rationale: two revisions would introduce a second destructive roll and make
  the availability comparison ambiguous.
  Date/Author: 2026-08-03, Codex with Vivek Reddy.
- Decision: keep the availability workload API-independent and the peer/K8s
  monitor read-only as separate evidence streams.
  Rationale: the workload must continue when Kubernetes observation is slow or
  unavailable, while the monitor needs Kubernetes identity and direct REST
  evidence from every Search Head and Cluster Manager. Combining them would
  make API availability a hidden dependency of the customer-visible test.
  Date/Author: 2026-08-03, Codex with Vivek Reddy.
- Decision: make StatefulSet convergence strategy-aware.
  Rationale: `OnDelete` can retain an older `currentRevision` after every Pod
  is manually replaced. Require all Pods on `updateRevision` for `OnDelete`;
  additionally require current/update equality for `RollingUpdate`.
  Date/Author: 2026-08-03, Codex with Vivek Reddy.
- Decision: pause the retained LicenseManager, ClusterManager,
  SearchHeadCluster, and IndexerCluster before deploying the candidate
  Operator or changing desired runtime images, then unpause and converge them
  in dependency order with the IndexerCluster last.
  Rationale: the Operator is cluster-wide, and the IndexerCluster dependency
  check does not wait for Search Head convergence. This ordering keeps the
  distributed-search front end stable and ensures the indexer image plus
  stable-address environment appear in one desired revision before the
  Operator can authorize replacement.
  Date/Author: 2026-08-03, Codex with Vivek Reddy.
- Decision: reserve `SPLUNK_IDXC_REGISTER_SEARCH_ADDRESS=absent` for
  controlled rollback.
  Rationale: the setting is persisted on the indexer PVC. Reverting images
  alone would leave it active, while treating null or empty as removal would
  unexpectedly mutate existing non-opted-in Ansible deployments. `absent`
  acts only when the persistent feature-ownership marker exists and its
  recorded value still matches the effective setting, so an unmanaged or
  subsequently changed customer value is neither removed nor claimed.
  Date/Author: 2026-08-03, Codex with Vivek Reddy.
- Decision: keep explicit partial-result semantics as a Splunk Enterprise
  requirement even if stable addresses improve the workload.
  Rationale: a client must not interpret an incomplete aggregate as complete
  merely because transport succeeded.
  Date/Author: 2026-08-03, Codex with Vivek Reddy.

## Outcomes & Retrospective

In progress. No production recommendation or EKS qualification is claimed.
The source candidate is isolated and pushed. Splunk Ansible's complete local
SHC Make gate, Docker-Splunk's dependency checkout/ref gates, Operator focused
tests, ten repeated feature-gate/override runs, and Operator `make build` pass.
A disposable composition with the independent SHC-99 probe correction also
passed all 43 local suites and 192 controller specs. Pre-EKS compatibility,
customer-ownership, and rollout-scope defects have been corrected. Acceptance
still requires clean Linux full gates,
immutable images, an exact-image EKS rollout, and evidence from every Search
Head.

## Context and Orientation

The Splunk Operator creates the clustered indexer StatefulSet in
`pkg/splunk/enterprise/indexercluster.go`. Environment variables provided by
the Operator are merged with `CommonSplunkSpec.ExtraEnv`; customer values take
precedence. SHC-98 adds `SPLUNK_IDXC_REGISTER_SEARCH_ADDRESS=auto` only to
clustered indexer Pods when the existing combined Pod and Indexer lifecycle
Alpha gate is enabled. With those gates off, no new value is injected.

Docker-Splunk consumes an exact Splunk Ansible source through the ref pinned in
its `Makefile`. Splunk Ansible reads container environment in
`inventory/environ.py`, stores the resolved value as
`splunk.idxc.register_search_address`, and runs common-role tasks before
`start_splunk.yml`. SHC-98 writes
`$SPLUNK_HOME/etc/system/local/server.conf` stanza `[clustering]` option
`register_search_address`, then verifies the effective value with `btool`.
It records ownership beside the persistent `etc` tree when it writes the key.
Automatic mode leaves a pre-existing unowned effective value unchanged, and
controlled rollback removes only an owned system-local value.

For `auto`, Splunk Ansible uses `SPLUNK_HOSTNAME` when explicitly available and
otherwise uses `socket.getfqdn()`. Under the Operator's indexer StatefulSet,
the Kubernetes hostname and subdomain yield a per-ordinal FQDN under the
headless Service. The address remains the same when Kubernetes replaces that
ordinal and assigns a new Pod IP.

In the current Splunk implementation, an indexer add-peer request sends the
configured search address to Cluster Manager. When absent, Cluster Manager
uses the connection host, which is the observed Pod IP. Cluster Manager sends
the selected search host/port to Search Heads as part of the peer generation.
SHC-98 must prove whether Search Heads retain the FQDN and re-resolve it during
replacement; that behavior is not assumed from source configuration alone.

## Plan of Work

First, preserve the three repositories as separate review units. Splunk
Ansible owns parsing, pre-start configuration, validation, and effective-value
verification. Docker-Splunk owns only the immutable Ansible dependency pin and
runtime build. Splunk Operator owns only the clustered-indexer default and
customer override behavior.

Second, run deterministic source gates on native Linux AMD64. Start from clean
worktrees, fetch the pushed SHC-98 branches, and run Splunk Ansible
`make shc-check`, Docker-Splunk `make test_ansible_ref` plus its supported
runtime build, and Splunk Operator `make test` and `make build`. Record exact
commits, suite totals, and any failure without classifying an unrelated
failure as SHC-98 success.

Third, build immutable images. The Docker-Splunk image must use the approved
Splunk Enterprise package already selected for the retained qualification
environment and must record the exact Ansible commit inside the build. The
Operator image must be built from the exact SHC-98 source. Push content-addressed
images to the existing ECR repository and capture OCI index plus linux/amd64
manifest digests.

Fourth, prepare a single indexer desired revision. Keep dependent tiers on a
consistent runtime image according to the existing desired-image dependency
contract. Prevent the Operator from beginning indexer replacement until both
the image and stable-address environment are present in the same desired Pod
template. Verify the StatefulSet update revision before permitting lifecycle
progress.

Fifth, start the API-independent workload and read-only SHC-98 monitor before
the first replacement. Each
sample writes a unique acknowledged HEC event, runs a distributed search from
the Search Head service, records result count and maximum sequence, and
preserves all transport and Splunk messages. At the same cadence, query every
Search Head's distributed-peer inventory and record address, status, GUID,
and last-error fields. Also record IndexerCluster lifecycle stage, target
ordinal and UID, StatefulSet revisions/partition, Pod UIDs/IPs/readiness,
EndpointSlices, container restarts, Cluster Manager RF/SF/all-searchable
health, and relevant Splunk/operator/Kubernetes events.

Sixth, allow the Operator-owned indexer roll to proceed in its normal ordinal
order `3 -> 2 -> 1 -> 0`. Do not manually advance durable lifecycle status or
delete a second Pod. For each ordinal, prove that only one indexer is expected
unavailable, the previous peer's remote serving path recovered before the next
selection, the FQDN remains unchanged while the Pod UID and IP change, and all
three Search Heads converge on the same four stable names.

Seventh, retain a stable observation window after lifecycle completion. Stop
the workload only after every tier is Ready, every indexer Pod carries the
StatefulSet `updateRevision`, the strategy-specific revision invariant holds,
Cluster Manager reports RF/SF/all searchable, each Search Head reports the
same four `Up` peers, old Pod IPs are absent from peer inventories and
relevant new logs, and the final distributed search returns the exact
acknowledged event set.

Finally, compare this campaign with the existing SHC-85 and SHC-97 evidence.
Accept stable addressing only if it preserves all prior invariants and removes
or materially reduces the peer-convergence window in a repeatable way. A
single better sample is evidence for further repetition, not enough to close
OPS-011. Reject or revise the candidate if Search Heads still store resolved
Pod IPs, DNS publication creates a worse failure, results remain silently
incomplete without improvement, or any source/runtime compatibility gate
fails.

## Concrete Steps

On the Linux vWorkstation, create clean worktrees for the pushed branch and
run the repository-owned commands:

    cd /home/vivekr/splunk-complete/splunk-ansible-shc-98
    make shc-check

    cd /home/vivekr/splunk-complete/docker-splunk-shc-98
    make test_ansible_ref

    cd /home/vivekr/splunk-complete/splunk-operator-shc-98
    make test
    make build

Use the Docker-Splunk Makefile's supported Linux build target with the exact
approved Splunk package, then use the Operator Makefile's supported image
target. Record source and image digests before changing the cluster.

Before rollout, capture the live baseline:

    kubectl -n shc-final-qualification get indexercluster,searchheadcluster,statefulset,pod,endpointslice -o wide
    kubectl -n shc-final-qualification get statefulset splunk-shcfinal-idxc-indexer -o yaml

Validate and start the two independent evidence streams before unpausing the
IndexerCluster:

    make shc98-monitor-check shc98-workload-check
    make shc98-incluster-workload SHC98_KUBECTL='kubectl --context shc85-vivek-spl-301372'
    SHC98_KUBE_CONTEXT=shc85-vivek-spl-301372 test/fixtures/shc-reliability/shc98_stable_address_monitor.sh

The management endpoint requests run inside each Search Head with the
namespace admin credential supplied through the existing safe test harness;
credentials must never be printed. Save normalized peer records containing
only address, status, GUID, and non-secret diagnostic fields.

## Validation and Acceptance

Source acceptance requires all repository-owned focused and full Make gates
to pass on Linux AMD64, a clean `git diff --check`, clean worktree status, and
an immutable relationship from Docker-Splunk to the exact Ansible commit.

Runtime acceptance requires:

- one desired indexer revision and one `3 -> 2 -> 1 -> 0` replacement pass;
- at most one lifecycle-owned unavailable indexer at any time;
- unchanged PVC and per-ordinal FQDN identity across each replacement;
- new Pod UID and IP observed without an old peer identity remaining;
- all Search Heads agreeing on four stable FQDN peer entries and `Up` status;
- final RF met, SF met, all searchable, every Pod carrying the StatefulSet
  `updateRevision`, current/update revision equality when the strategy is
  `RollingUpdate`, all Pods Ready, and zero unexpected container restarts;
- zero HEC request failures and zero distributed-search request failures;
- exact final equality between acknowledged events and distributed-search
  results; and
- a complete record of every successful-search count regression, maximum
  pending gap, Splunk message, peer-inventory transition, and timing boundary.

The candidate is not sufficient to close OPS-011 unless repeated evidence
also proves immediate completeness or Splunk explicitly signals partial
results so clients can retry/fail safely. Mesh/no-mesh, TLS variants,
insufficient redundancy, API partitions, leader failover, persistent clients,
App Framework-controlled restarts, and soak remain separate gates.

## Idempotence and Recovery

The Ansible configuration is idempotent: the same managed
`register_search_address` produces no change and no restart. Undefined, null,
or empty input skips the task. Automatic mode preserves an existing unowned
effective value. Explicit and newly adopted automatic values carry a
persistent record of the last managed value. Automatic reconciliation and
`absent` treat it as owned only while the effective setting still matches that
record; a different customer value is preserved and stale ownership is
removed. Re-running the Operator reconcile preserves the same generated
environment and StatefulSet revision after convergence.

If image construction fails, do not patch the cluster. If the Operator image
is deployed before the runtime is ready, keep the IndexerCluster paused using
the repository's verified pause contract and do not begin replacement. If a
roll is interrupted, preserve the durable lifecycle record and resume with
the same desired revision; do not edit status or delete another Pod.

If the EKS candidate is rejected, do not revert the runtime first. Override
the indexer environment with
`SPLUNK_IDXC_REGISTER_SEARCH_ADDRESS=absent`, run one controlled lifecycle
pass with the candidate runtime, and verify `btool` plus every Search Head peer
inventory no longer contains the registered FQDN. Then restore the prior
immutable runtime and Operator desired images through the same
dependency-ordered lifecycle path. Do not remove PVCs or reset Splunk
membership. Preserve the rejected image digests and evidence so the result is
reproducible.

## Artifacts and Notes

- Operator branch: `codex/shc-98-stable-indexer-search-address`.
- Initial Operator source: `5faeeb0e7a5c97750d6006d7d43da168996aab8e`.
- Current gated Operator candidate:
  `2c607d6e295e164d4661dd832294d666c5a1d270`.
- Splunk Ansible branch: `codex/shc-98-stable-indexer-search-address`.
- Customer-safe reversible Ansible source:
  `9dff0999c93fd129d31ba08609423ac2bd600aeb`.
- Docker-Splunk branch: `codex/shc-98-stable-indexer-search-address`.
- Customer-safe dependency-pin source:
  `6ee266c14e25a1d5849a3d5b96cdaf155b09c696`.
- Read-only peer monitor source:
  `78ff404c727f562bd85656f0c65696393bf0cb7d`.
- API-independent workload source:
  `aa9a566aaf8f1a6fb1993776ba1909f2cfb68b71`.
- Pre-candidate snapshot SHA-256:
  `e0524b714a311cb0da7d651f90e787d5e1c86091daf7cb808430edf53444d353`.
- Pre-candidate effective-config snapshot SHA-256:
  `6a421a55cb4637a1470e5505df4ed6eeeb5a663579c4f7fc421034e72efe46c0`.
- EKS context:
  `arn:aws:eks:us-west-2:667741767953:cluster/vivek-spl-301372`.
- Qualification namespace: `shc-final-qualification`.
- Existing accepted runtime OCI index:
  `sha256:49b12103f8444319dcf823eb829d2dfc020410e44d46273461c1b15e52c724fd`.
- Existing accepted Operator OCI index:
  `sha256:a9f2125097fa823d5182e8729683e5099116a889fdae8e892f0bd3110a8cdf3d`.

Do not put admin credentials, HEC tokens, registry credentials, or raw Secret
content in this document or evidence bundles.

## Interfaces and Dependencies

Splunk Ansible exposes the optional environment input:

    SPLUNK_IDXC_REGISTER_SEARCH_ADDRESS=<host-or-IP|auto|absent>

It maps to:

    splunk.idxc.register_search_address
    server.conf/[clustering]/register_search_address

`auto` resolves to `SPLUNK_HOSTNAME` when non-empty, otherwise
`socket.getfqdn()`, but writes it only when no unowned effective value exists
or the feature already owns the setting. Explicit input becomes managed.
`absent` removes the option before start only when the feature's persistent
record exists and the effective value still matches its last managed value.
A different value is preserved and stale ownership is relinquished.
Undefined, null, and empty inputs remain unmanaged.
The Operator supplies `auto` for clustered indexers only under the combined
Pod and Indexer lifecycle Alpha gate and retains the existing
`CommonSplunkSpec.ExtraEnv` override contract. Customers can explicitly supply
the environment value when the gates are disabled.

The candidate depends on StatefulSet hostname/subdomain identity, the
indexer's existing headless Service, Kubernetes cluster DNS, Cluster Manager
peer membership, Search Head distributed-peer updates, and the SHC-85
lifecycle state machine. It does not depend on ingress, external load
balancers, TLS termination at ingress, or a service mesh.

The observed customer path still depends on Splunk Enterprise's distributed
search behavior. If Splunk accepts an unreachable or incomplete peer set and
returns an unmarked successful partial aggregate, Operator readiness cannot
repair that semantic gap; it can only avoid progressing another planned
disruption and expose the evidence.
