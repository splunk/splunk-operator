# Keep qualification evidence active for the complete indexer roll

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to
date as work proceeds.

This document is maintained in accordance with the ExecPlan requirements in
the `execution-plan` skill.

## Purpose / Big Picture

The SHC-112 exact search-peer gate can legitimately wait about 26 minutes for
each replacement indexer's stale prior Pod-IP entry to disappear from every
Search Head. A four-indexer reverse-ordinal roll plus the required five-minute
stable window can therefore exceed the old two-hour monitor timeout. The old
workload stopped after one hour, so it could end after only the first one or two
replacements and could not support a full-roll availability verdict.

SHC-117 is a test-only correction. It keeps the monitor and API-independent HEC
and distributed-search workload active for three hours and gives the Job a
four-hour Kubernetes deadline. It changes no controller, CRD, workload object,
runtime image, Docker-Splunk behavior, or Splunk Enterprise behavior.

## Progress

- [x] (2026-08-04 UTC) Measured the baseline per-ordinal convergence interval
  and compared it with the existing one-hour workload and two-hour monitor.
- [x] Created isolated branch
  `codex/shc-117-long-indexer-roll-qualification` from exact SHC-116 source.
- [x] Set the complete-roll workload to 10,800 one-second samples, its active
  deadline to 14,400 seconds, and the monitor default timeout to 10,800
  seconds.
- [x] The Makefile monitor syntax/ShellCheck target and workload manifest
  dry-run passed on macOS. Linux bash syntax and manifest dry-run passed; the
  vWorkstation does not currently provide the separate ShellCheck executable.
- [ ] Run the complete SHC-116 EKS roll and retain all evidence artifacts.

## Surprises & Discoveries

- Observation: the previous workload duration was shorter than the measured
  full lifecycle, even though each individual replacement was healthy.
  Consequence: a clean one-hour workload could not prove all ordinal
  transitions.
- Observation: the previous two-hour monitor included a five-minute stable
  requirement within its deadline.
  Consequence: four ordinary stale-peer convergence intervals could exhaust
  the timeout before the final acceptance window.

## Decision Log

- Decision: use a three-hour evidence duration and a four-hour Job deadline.
  Rationale: this covers the measured four-ordinal lifecycle with substantial
  margin while retaining a finite failure boundary.
  Date/Author: 2026-08-04, Codex with Vivek Reddy.
- Decision: keep this correction on a test-only branch stacked after the exact
  SHC-116 production source.
  Rationale: the immutable manager image must remain attributable to the
  production commit while evidence-harness changes remain independently
  reviewable.
  Date/Author: 2026-08-04, Codex with Vivek Reddy.

## Outcomes & Retrospective

The harness no longer ends before the observed full-roll duration. Its live
outcome remains open until the SHC-116 candidate completes every ordinal and
the final stable window.

## Plan of Work

Run the existing Make validation targets on the exact harness source. On the
vWorkstation, launch the extended Job and monitor before introducing a new
IndexerCluster template revision. Keep the accepted runtime image unchanged
and use `pod-ip` expected-address mode for the retained cluster. Preserve the
complete Job log, monitor TSV, Events, configuration snapshot, hashes, and
start/completion timestamps.

## Validation and Acceptance

- the monitor script passes bash syntax and ShellCheck where the executable is
  installed;
- the Job passes the repository's client-side Kubernetes manifest dry-run;
- the Job remains active through all four replacement UIDs and the final
  stable window;
- the monitor records target order `3,2,1,0` and does not time out during an
  otherwise progressing exact-peer gate;
- the final evidence states the exact number of samples actually completed and
  does not infer coverage beyond that interval; and
- the production Operator image digest remains the exact SHC-116 artifact.

## Idempotence and Recovery

The Make target deletes only the named prior qualification Job before creating
the next one. The workload has no Kubernetes API credential and uses a unique
Pod-hostname run identifier, so a recreated Job produces a distinct Splunk
event stream. Evidence filenames must also be unique. Repeating the harness
does not mutate controller source or the Splunk runtime image.

## Artifacts and Notes

- Parent production source:
  `96c83dcadc25e6034ba2a41898c84ed1b255b570`.
- Harness branch: `codex/shc-117-long-indexer-roll-qualification`.
- Exact harness source: `cd522e119ef7113b4605c42e5a9624febce3ca49`.
- Workload samples: 10,800 at one-second intervals.
- Monitor timeout: 10,800 seconds.
- Job active deadline: 14,400 seconds.
- Live evidence: pending.

## Interfaces and Dependencies

SHC-117 changes only the SHC-98 qualification fixture and its documentation.
It is used to qualify SHC-116 and the cumulative SHC-112 through SHC-115
behavior. Its Job continues to use the accepted runtime digest and the same
HEC/search script, Secret references, Services, and no-service-account-token
boundary.
