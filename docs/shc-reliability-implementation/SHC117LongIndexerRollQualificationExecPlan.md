# Keep qualification evidence active for the complete indexer roll

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to
date as work proceeds.

This document is maintained in accordance with the ExecPlan requirements in
the `execution-plan` skill.

## Purpose / Big Picture

The SHC-112 exact search-peer gate can legitimately wait about 26 minutes for
each replacement indexer's stale prior Pod-IP entry to disappear from every
Search Head. A four-indexer reverse-ordinal roll plus 60 consecutive complete
final-state observations can therefore exceed the old two-hour monitor
timeout. The old workload stopped after one hour, so it could end after only
the first one or two replacements and could not support a full-roll
availability verdict.

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
- [x] (2026-08-04 18:20Z) The extended monitor completed the entire SHC-116
  EKS `3 -> 2 -> 1 -> 0` roll and 60 consecutive final-state observations.
  It retained 647 full snapshots from `15:58:41Z` through `18:20:20Z` and
  exited zero without timing out.
- [x] (2026-08-04 19:26Z) The independent 10,800-sample workload Job finished
  with Kubernetes `Complete`, zero HEC failures, zero search-request failures,
  exact final count/minimum/maximum/distinct values of
  `10800/1/10800/10800`, and `complete=true`. The runner collected final
  resources, exited zero, and all 25 artifact hashes verify.

## Surprises & Discoveries

- Observation: the previous workload duration was shorter than the measured
  full lifecycle, even though each individual replacement was healthy.
  Consequence: a clean one-hour workload could not prove all ordinal
  transitions.
- Observation: the previous two-hour monitor included 60 stable observations
  with a five-second minimum sleep within its deadline.
  Consequence: four ordinary stale-peer convergence intervals could exhaust
  the timeout before the final acceptance window.
- Observation: the stability setting counts full observations rather than
  promising a fixed wall-clock interval. Each observation performs Kubernetes
  and Splunk REST collection before the configured five-second sleep.
  Evidence: 60 accepted final-state observations ran from `18:07:18Z` through
  `18:20:20Z`, about 13 minutes in the retained cluster.
  Consequence: acceptance and timeout sizing must use measured full-sample
  duration; the phrase "five-minute stable window" describes only the minimum
  sleep budget and is not precise enough for evidence.
- Observation: a successful workload Job proves request success and exact
  eventual delivery, but its `countRegressions` and `maxPending` fields remain
  independent evidence.
  Consequence: the final SHC-117 verdict must report those counters even when
  Kubernetes marks the Job Complete; it must not equate eventual completeness
  with uninterrupted distributed-search completeness.
- Observation: the completed Job recorded 19 count regressions, all while the
  planned roll was active. The last occurred at `17:40:34Z`; maximum pending
  was 1,335 at sequence 5,358 and `17:41:15Z`. The lifecycle monitor remained
  active through `18:20:20Z`, and the Job had no later count regression before
  completion at `19:26:16Z`.
  Consequence: the extended window was long enough to distinguish roll-bound
  regressions from post-roll stability and to retain eventual exact-delivery
  evidence without hiding the intermediate-result gap.

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
- Decision: define the final stability gate as 60 consecutive complete
  observations and retain their first and last timestamps.
  Rationale: collection cost is real and variable, so sample count plus actual
  timestamps is reproducible evidence while a nominal wall-clock label is not.
  Date/Author: 2026-08-04, Codex with Vivek Reddy.
- Decision: keep lifecycle-monitor success, request success, final exact
  delivery, and intermediate search-count regression as separate verdicts.
  Rationale: each answers a different availability question and a passing Job
  alone cannot prove that every HTTP-successful search was complete.
  Date/Author: 2026-08-04, Codex with Vivek Reddy.

## Outcomes & Retrospective

The extended monitor no longer ends before the observed full-roll duration. It
completed all four ordinal transitions and 60 consecutive final-state
observations with exit code zero. The 10,800-sample Job and outer runner also
completed with zero request failures, exact eventual uniqueness, and verified
final artifacts. SHC-117 therefore closes the duration/evidence-window gap. It
does not convert the 19 HTTP-successful count regressions into a success claim;
that separate Splunk distributed-search result contract remains open.

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
- Live long-workload verdict: passed for duration coverage, request success,
  and eventual exact delivery. Submitted 10,800; HEC failures 0; search-request
  failures 0; final count/min/max/distinct `10800/1/10800/10800`; Kubernetes
  Job `Complete`; monitor and outer-runner exit codes 0. Intermediate evidence:
  19 count regressions and maximum pending 1,335 during the planned roll.
- Lifecycle monitor: passed with 647 snapshots from
  `2026-08-04T15:58:41Z` through `2026-08-04T18:20:20Z`, target order
  `[3,2,1,0]`, and 60 consecutive final-state observations from
  `18:07:18Z` through `18:20:20Z`.
- Monitor SHA-256 values: TSV
  `7bb72cc61397ba923fda5645e63146820f76f10b36541ca0eb14c6ba2d186a66`,
  Events
  `43c0ca28a2f3f4df819824e6a441df273490dcb71535c16b7c71c52a8c9e04af`,
  final configuration
  `492e557ab3ae3dc5aa77a2423abcdb871a35fd93a7f60e605cdc37d606d2e971`,
  and stdout
  `16eebe9c61746e9841cac64f4c127c5336bde8efb59bc7341df6f53aeb27a676`.
- Workload log SHA-256:
  `ef779a56ad85c6813bf377aafa3ed5388064a15bcf31c93031504992cbc7c71e`.
  Workload Job SHA-256:
  `7fe374d35f7bc2519a0d1f0d215446a688c67da5e55b9f52753849b433572e77`.
  Artifact-manifest SHA-256:
  `7e46344af722395aa21225adbd48e47242a3c086a66f2dd9be46a29bc956faae`;
  all 25 listed hashes verify from the repository root.

## Interfaces and Dependencies

SHC-117 changes only the SHC-98 qualification fixture and its documentation.
It is used to qualify SHC-116 and the cumulative SHC-112 through SHC-115
behavior. Its Job continues to use the accepted runtime digest and the same
HEC/search script, Secret references, Services, and no-service-account-token
boundary.

## Revision Note

Updated on 2026-08-04 after the extended lifecycle monitor exited zero. This
revision records the completed ordinal and stability evidence while preserving
the long-workload Job and final artifact hashes as explicit open work.

Updated on 2026-08-04 after the extended Job and outer runner completed. This
revision closes the test-duration gap with exact final counters and verified
artifacts while preserving intermediate search-count regressions as a separate
availability finding.
