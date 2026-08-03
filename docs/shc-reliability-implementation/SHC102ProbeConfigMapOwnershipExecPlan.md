# Preserve customer probe scripts while upgrading managed defaults

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to
date as work proceeds.

This document is maintained in accordance with the ExecPlan requirements in
the `execution-plan` skill.

## Purpose / Big Picture

The Splunk Operator supports one namespace-scoped ConfigMap containing the
liveness, readiness, and startup scripts used by every Splunk Pod in that
namespace. Customers may create this ConfigMap before creating a Splunk custom
resource or may edit the generated scripts later. The published contract says
the Operator preserves those custom scripts.

SHC lifecycle work also needs revised default scripts to reach namespaces that
continue to use Operator defaults. The Operator therefore needs a factual way
to distinguish an unchanged generated default from customer-owned data. It
must not infer ownership from the ConfigMap name, field manager, current
script text, or timing.

After SHC-102, a newly generated probe ConfigMap records the exact hash of the
data written by the Operator. A later Operator may update the scripts only
when that recorded hash still matches the complete current data. A pre-created
ConfigMap has no marker and is preserved. Editing any generated data makes its
current hash differ from the marker, so it is also preserved. Conflict retries
repeat the ownership check against the latest API object and stop safely if a
customer edits during the race.

## Progress

- [x] (2026-08-03 18:42Z) Revalidated the published custom-probe contract in
  `docs/reference/Examples.md` and the original CSPL-3912 implementation. Both
  pre-created and later-edited probe ConfigMaps were intentionally returned
  unchanged.
- [x] (2026-08-03 18:45Z) Confirmed that SHC-98's unconditional stale-script
  reconciliation contradicted that contract and that SHC-101 made only its
  concurrent update safe; neither change established ownership.
- [x] (2026-08-03 18:50Z) Implemented content-integrity ownership, direct
  create/AlreadyExists handling, latest-object ownership checks inside
  conflict retry, and defensive deep copies before mutation.
- [x] (2026-08-03 18:54Z) Added deterministic tests for pre-created custom
  data, new generated defaults, untouched managed-default upgrade, edit of a
  generated ConfigMap, ordinary update conflict, edit during conflict retry,
  and customer creation during the create race. Twenty repeated focused runs,
  the complete enterprise package, and `make build` passed on macOS.
- [x] (2026-08-03 18:55Z) Committed the isolated correction as
  `e887d4becb5a3b99e7aa545deaa40ac94ea0a2df`, pushed
  `codex/shc-102-probe-configmap-ownership`, and fast-forwarded the GitLab
  integration branch to the same source.
- [x] (2026-08-03 UTC) Completed the exact native-Linux Make gate at
  `e887d4bec`: 43 suites, 192/192 enterprise/controller specs, 78.3 percent
  composite coverage, `make build`, a clean source tree, and manager SHA-256
  `e506bc6e40baa13c32e44fe7fb62b33304b794ab20254db9a4b6a57329553d31`.
- [x] (2026-08-03 UTC) Built immutable Operator OCI index
  `sha256:2680a7fee145458e6d70355f25e1dfd4d8b19ca15cf11e0ed8793ff43fcf8a7e`
  and qualified pre-created custom data, unchanged marked-data upgrade, a
  later customer edit, controller replacement, zero workload restart, and
  preserved Pod identity on EKS. Candidate reconciliation produced zero
  controller ERROR/FATAL logs and zero workload Warning Events.
- [x] (2026-08-03 UTC) Restored both active namespace ConfigMaps exactly,
  restored the accepted Operator image, and passed retained- and
  fresh-cluster health snapshots with zero workload restart.
- [x] (2026-08-03 20:35Z) Completed SHC-103's cumulative native-Linux,
  immutable-image, live new-ConfigMap creation, cleanup, and accepted-
  restoration gates at `070ca5f59`; the final ownership source is complete.

## Surprises & Discoveries

- Observation: a ConfigMap name does not establish ownership.
  Evidence: both the Operator-generated default and the documented customer-
  created override use `splunk-<namespace>-probe-configmap`.
  Consequence: name-based reconciliation is unsafe.
- Observation: unmarked existing data cannot be classified safely after the
  fact.
  Evidence: an old generated ConfigMap and a customer ConfigMap can have the
  same keys and may even contain the same release's defaults. Existing objects
  have no owner reference or durable content-origin marker.
  Consequence: every unmarked existing ConfigMap is treated as customer-owned.
  Customers who want automatic default upgrades can delete an unmodified
  legacy ConfigMap once and let the Operator recreate it with the marker.
- Observation: ownership can change during an optimistic-lock retry.
  Evidence: a customer edit after the first `Get` causes the candidate update
  to conflict. Retrying only the write would overwrite the newer edit.
  Consequence: every retry revalidates that the marker equals the latest data
  hash before writing.
- Observation: create is also an ownership race.
  Evidence: a customer can create the fixed-name ConfigMap after the
  Operator's NotFound read but before the Operator's create.
  Consequence: AlreadyExists is followed by a read and preserve, never an
  update of the winning object.
- Observation: successful API creation does not imply immediate visibility
  through a controller-runtime informer cache.
  Evidence: the first implementation performed a required cached `Get`
  immediately after `Create`; the API-server write could already be durable
  while that read still returned NotFound.
  Consequence: SHC-103 makes successful `Create` authoritative and bounds the
  separate AlreadyExists-winner visibility window. The cumulative source must
  pass a real new-namespace creation test before final acceptance.

## Decision Log

- Decision: use a full deterministic SHA-256 of the ConfigMap `Data` as the
  ownership-integrity marker.
  Rationale: the existing package already has a sorted-key data hash. Comparing
  the recorded value with current data proves only that the data has not
  changed since the Operator wrote it; it does not claim ownership based on
  mutable Kubernetes metadata.
  Date/Author: 2026-08-03, Codex with Vivek Reddy.
- Decision: treat an absent or mismatched marker as customer ownership.
  Rationale: false preservation leaves a legacy namespace on its existing
  scripts and is recoverable; false Operator ownership destroys a supported
  customer customization.
  Date/Author: 2026-08-03, Codex with Vivek Reddy.
- Decision: preserve the stale marker after a customer edit.
  Rationale: no metadata write is required to respect the edit. The mismatch
  remains a durable fail-closed signal and avoids a new race merely to record
  relinquishment.
  Date/Author: 2026-08-03, Codex with Vivek Reddy.
- Decision: retain SHC-101 `RetryOnConflict` only after ownership validation.
  Rationale: multiple controllers still update an unchanged managed default
  concurrently on an Operator upgrade. The retry is correct for that case and
  must terminate successfully if the newest object is now customer-owned.
  Date/Author: 2026-08-03, Codex with Vivek Reddy.

## Outcomes & Retrospective

The ownership behavior is implemented and qualified at `e887d4bec`, and its
final cumulative creation path is qualified at `070ca5f59`. It is
backward compatible for both documented customization paths and creates a
forward-safe upgrade path for newly generated defaults. No CRD, Splunk
Enterprise, Docker-Splunk, Splunk Ansible, Pod template, persistent volume, or
customer secret changes are involved. The cumulative Linux, immutable-image,
live new-object, cleanup, and accepted-restoration gates passed.

## Context and Orientation

`pkg/splunk/enterprise/configuration.go` contains `getProbeConfigMap`, called
while rendering StatefulSets for every Splunk tier. It reads the three scripts
packaged in the Operator image and obtains the shared ConfigMap named by
`GetProbeConfigMapName`.

`pkg/splunk/enterprise/configuration_test.go` contains the direct behavior and
race tests. The injected clients reproduce Kubernetes Conflict and
AlreadyExists responses without timing-dependent goroutines.

`docs/reference/Examples.md` is the customer-facing custom-probe contract.
SHC-101 documents the separate resource-version conflict correction.

## Plan of Work

On creation, build the deterministic three-script map, record its data hash in
`enterprise.splunk.com/probe-configmap-content-hash`, perform a final NotFound
check, and create directly. If another actor wins, read and preserve the
winner.

On an existing object, compare the recorded hash with the complete current
`Data`. Return immediately when the marker is absent or mismatched. When it
matches, compare with the desired packaged scripts. Update data and marker
together under SHC-101's bounded conflict retry. Re-read and repeat the
ownership comparison on every retry.

Qualify on EKS with separate managed and custom namespaces or with disposable
ConfigMaps whose exact before/after content and metadata are captured. Trigger
several tier controllers concurrently, scope Events and logs from the
candidate controller creation time, and retain Pod UID/restart/readiness
snapshots before and after each transition.

## Validation and Acceptance

Source acceptance requires:

- 20 consecutive focused ownership/race test runs;
- the complete enterprise package;
- `make shc98-monitor-check`, `make test`, and `make build` on native Linux;
- a clean generated source tree; and
- exact source and manager hashes.

EKS acceptance requires:

- an unmarked pre-created custom ConfigMap remains byte-for-byte unchanged;
- a newly generated ConfigMap has a marker equal to its data hash;
- an unchanged marked ConfigMap upgrades to candidate defaults and advances
  its marker;
- a customer edit makes marker and data differ and remains unchanged through
  reconciliation and controller restart;
- concurrent tier reconciliation produces no Warning Event and no controller
  ERROR/FATAL log;
- all managed Pods retain their UIDs, readiness, and restart counts; and
- the accepted Operator image is restored with healthy Splunk snapshots.

## Idempotence and Recovery

Reconciliation of a customer-owned ConfigMap is read-only. Reconciliation of
an unchanged managed default writes one deterministic data-and-marker pair and
then becomes read-only. Conflict retry always starts from current API state.
Deleting a legacy unmodified ConfigMap is an explicit customer migration to a
new managed default; the Operator does not delete it automatically.

The EKS rollback is the accepted immutable Operator digest. ConfigMap test
objects must be backed up before mutation, and customer-data tests use
synthetic scripts rather than changing real supported content without a
restore record.

## Artifacts and Notes

- Isolated source branch: `codex/shc-102-probe-configmap-ownership`.
- Integration branch: `codex/shc-kubernetes-reliability-final-integration`.
- Exact source: `e887d4becb5a3b99e7aa545deaa40ac94ea0a2df`.
- Cumulative source after the SHC-103 create/cache correction:
  `070ca5f59a5a995839fb56e4832873222613d58e`.
- Cumulative Linux manager SHA-256:
  `d9afa7444e5ed64256ae3e4c724847a6ea05a5c92eee6a3047a19fd5d5f98f5c`.
- Cumulative Operator OCI index:
  `sha256:2ae4db4155427ade5361f8a4d71f71d7ea0b4bdbf447a40e2dc1434815074308`.
- Qualified candidate Operator OCI index:
  `sha256:2680a7fee145458e6d70355f25e1dfd4d8b19ca15cf11e0ed8793ff43fcf8a7e`.
- Candidate Linux manager SHA-256:
  `e506bc6e40baa13c32e44fe7fb62b33304b794ab20254db9a4b6a57329553d31`.
- Accepted Operator OCI index:
  `sha256:a9f2125097fa823d5182e8729683e5099116a889fdae8e892f0bd3110a8cdf3d`.
- Restored namespace ConfigMap Data SHA-256:
  `a1e2d849d13d1f72f55ca6f50577578774d4706152ab538786002647f06e6458`.
- Managed candidate data and marker SHA-256 before the synthetic customer
  edit: `ddbc90fba32858eb497c2d2ca947ee38f793869c13162d72cbaf2947edfafe43`.
- Synthetic customer-edited data SHA-256 preserved through controller
  replacement:
  `69eac046ade004366168af4e273381d44089b367635657a59d15759ccaf925d6`.

## Interfaces and Dependencies

The correction adds one internal ConfigMap annotation and changes no Custom
Resource API. It uses the existing deterministic `configDataHash`,
controller-runtime client, Kubernetes AlreadyExists and Conflict semantics,
and client-go `RetryOnConflict`. The annotation is an internal integrity
record, not a customer opt-in requirement.

Revision note (2026-08-03 19:05Z): Created this plan after final integration
review found the unconditional probe update contradicted CSPL-3912 and current
customer documentation. Recorded the source design, isolated commit, tests,
remaining Linux/image/live gates, and the compatibility boundary.

Revision note (2026-08-03 UTC): Recorded the exact SHC-102 Linux and EKS
ownership evidence, accepted restoration, and the separate post-create cache
visibility issue carried by SHC-103. Final completion remains deliberately
open until the cumulative source passes a live new-ConfigMap test.

Revision note (2026-08-03 20:35Z): Closed final cumulative acceptance at
`070ca5f59`. The real EKS create produced complete Data hash and marker
`ddbc90fba32858eb497c2d2ca947ee38f793869c13162d72cbaf2947edfafe43`,
zero candidate manager errors, exact retained-object preservation, complete
disposable storage cleanup, and healthy accepted restoration.
