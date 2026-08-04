# Bound App Framework requeue at the repository-poll boundary

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to
date as work proceeds.

This document is maintained in accordance with the ExecPlan requirements in
the `execution-plan` skill.

## Purpose / Big Picture

During the final SHC-94 EKS rollout qualification, the Operator emitted an
`invalid requeue time` error while App Framework repository polling and the
Search Head lifecycle were both active. The rollout itself continued because
the lifecycle supplied a separate five-second requeue, but a normal timer
boundary must not be reported as an Operator failure. False errors make a
healthy rollout appear failed to support and can hide a real error in the same
time window.

SHC-105 makes the repository-poll timer return the existing bounded five-second
retry at both the exact boundary and after the timer is overdue. It does not
change App Framework ownership, polling frequency, bundle behavior, or Search
Head rollout sequencing.

## Progress

- [x] (2026-08-03 22:45Z) Reproduced one live false error during the SHC-94
  EKS rollout. The message reported a zero requeue duration at the App
  Framework repository-poll boundary. The same reconcile also had a valid
  five-second Search Head lifecycle requeue, and rollout progress continued.
- [x] (2026-08-03 23:09Z) Implemented the isolated correction at Operator
  commit `0e638dac4` on branch
  `codex/shc-105-appframework-requeue-boundary`.
- [x] (2026-08-03 23:14Z) Added positive-delay, exact-boundary, and overdue
  tests. The focused boundary test passed 1,000 repetitions.
- [x] (2026-08-03 23:14Z) Passed `make fmt vet`, `make build`, all 43 Make test
  suites with zero failures and 78.3 percent composite coverage, Helm lint,
  and all 150 Helm unit tests.
- [x] (2026-08-03 23:27Z) Passed the repository linter in new-change mode
  relative to the SHC-105 base with zero issues. The complete repository lint
  still reports the separately recorded 24 pre-existing issues.
- [x] (2026-08-03 23:29Z) Passed 100 race-enabled exact-boundary repetitions
  and 20 race-enabled repetitions of the three SHC-94 App Framework ownership
  tests.
- [x] (2026-08-03 23:31Z) Cross-compiled the complete manager through the
  repository Make target for `linux/amd64`; the resulting binary is an x86-64
  ELF executable. Native Linux container-image construction and execution are
  still required.
- [x] (2026-08-04 00:55Z) Closed the independent 240-sample accepted-image
  conflict campaign. Its Operator log contained exactly one ERROR entry: the
  same `invalid requeue time` signature with `timeValue=0`, at `00:01:06Z`.
  The rollout and all 240 workload requests still completed, confirming the
  bounded observability defect without expanding it into an outage claim.
- [ ] Build an immutable Linux/AMD64 Operator image from exact source
  `0e638dac4`, deploy it to the EKS qualification cluster, and observe at least
  two App Framework poll boundaries during an active lifecycle without an
  invalid-requeue error.
- [ ] Confirm the candidate produces no new Warning Events, no Operator
  ERROR/FATAL entries, no workload restart, and no change to the SHC-94
  conflict-serialization behavior.
- [ ] Restore or promote the accepted immutable Operator image only after the
  live acceptance gate passes.

## Surprises & Discoveries

- Observation: repository polling can be due when first inspected and exactly
  on the boundary when the next delay is calculated.
  Evidence: the timer-expiry decision and the delay calculation use separate
  current-time reads with second resolution. The live error contained an
  exact zero delay rather than a negative overdue delay.
  Consequence: handling only negative durations leaves a valid boundary value
  to the shared requeue validator, where it is rejected.
- Observation: the false error did not stop this particular rollout.
  Evidence: the Search Head lifecycle independently requested a five-second
  requeue, and the controller continued through the remaining reverse-ordinal
  replacements to a stable three-member SHC.
  Consequence: the EKS evidence establishes an observability defect, not proof
  of an availability outage or a stalled rollout.
- Observation: repository polling was frequent during the qualification
  window while durable App Framework work remained empty.
  Evidence: the Operator logged normal poll-expiry decisions repeatedly, but
  only one exact-boundary invalid-requeue message.
  Consequence: live acceptance must cross multiple real boundaries; a single
  successful reconcile is insufficient.
- Observation: repository-wide lint currently reports 24 pre-existing issues
  outside the SHC-105 diff.
  Evidence: the issues are in PostgreSQL, telemetry, unrelated integration
  tests, configuration, and utility files. `git diff --check`, formatting,
  vet, build, the complete test suite, and chart gates pass for this change.
  Consequence: unrelated lint cleanup remains separate and must not be mixed
  into the reliability fix.

## Decision Log

- Decision: reuse the existing five-second overdue fallback for an exact-zero
  delay.
  Rationale: zero means the poll is due now, and the code already defines five
  seconds as the bounded retry for an overdue calculation. This removes the
  false error without changing the configured repository poll interval.
  Date/Author: 2026-08-03, Codex with Vivek Reddy.
- Decision: keep SHC-105 separate from SHC-94 rollout ownership.
  Rationale: SHC-94 decides whether durable App Framework work owns the
  disruption slot. SHC-105 only ensures a normal timer boundary produces a
  valid requeue duration.
  Date/Author: 2026-08-03, Codex with Vivek Reddy.
- Decision: do not mark the item complete from macOS tests alone.
  Rationale: the defect was found under live EKS timing and must be closed by
  an immutable Linux image across multiple real polling boundaries.
  Date/Author: 2026-08-03, Codex with Vivek Reddy.

## Outcomes & Retrospective

SHC-105 is source-qualified but not yet complete. Exact source `0e638dac4`
passes the full local regression gate, including 1,000 focused boundary
repetitions. Native Linux image construction and live EKS qualification remain
blocked while the dedicated vWorkstation endpoint fails below authentication
during TLS/API connection setup.

## Context and Orientation

`pkg/splunk/enterprise/util.go` calculates the next App Framework repository
poll requeue from the configured interval and last-check epoch. The caller
combines that delay with other reconciliation work through the shared requeue
helper. The shared helper correctly rejects a non-positive duration; the App
Framework calculator is responsible for converting a due or overdue timer to
its bounded retry.

The triggering EKS topology is namespace `shc-final-qualification` on context
`shc85-vivek-spl-301372`. It contains three Search Heads, four indexers, a
Cluster Manager, deployer, and License Manager. The deployed accepted Operator
contains SHC-94 but not SHC-105.

## Plan of Work

Build the exact SHC-105 source on the Linux vWorkstation and publish an
immutable Operator image. Record the source commit, architecture, tag, and OCI
digest. Deploy by digest without changing the runtime image or the retained
Splunk data.

Run the enhanced SHC-82 monitor, create a harmless Search Head Pod-template
revision, and cross at least two 60-second App Framework repository-poll
boundaries while the lifecycle is active. Count invalid-requeue, ERROR/FATAL,
and Warning Event signatures without recording credentials. Verify workload
requests, endpoints, captain transfer, Pod order, revisions, and final exact
search convergence.

If the false error reappears, preserve the exact timestamp and reconcile
context, reject the candidate, and do not amend the live evidence. If it does
not reappear, record the bounded observation as live qualification rather than
claiming that all possible scheduler timing has been proven.

## Validation and Acceptance

Acceptance requires:

- exact Linux/AMD64 source and immutable Operator digest are recorded;
- focused exact-boundary tests pass repeatedly;
- the complete Make test and build gates pass;
- the candidate crosses at least two live repository-poll boundaries during
  active Search Head lifecycle work;
- zero `invalid requeue time` messages occur in the candidate window;
- zero candidate-caused Warning Events or ERROR/FATAL logs occur;
- Search Head lifecycle ordering, partition control, endpoints, captain
  handling, and final revisions remain correct; and
- numbered HEC and distributed-search workload requests finish exactly
  complete with no request failure.

## Idempotence and Recovery

The source change is deterministic and has no persisted-state migration. If
the candidate fails, redeploy the previously accepted Operator image by its
recorded digest. Existing App Framework status and Search Head lifecycle status
remain the recovery authority; do not edit either status to force progress.

## Artifacts and Notes

- Production fix branch: `codex/shc-105-appframework-requeue-boundary`.
- Exact source: `0e638dac45458519e7daae10235527b85af1be6f`.
- Triggering accepted Operator source: `14d8853908292422b679da46c89c1a15c14c2bf4`.
- Triggering accepted Operator OCI index:
  `sha256:a9f2125097fa823d5182e8729683e5099116a889fdae8e892f0bd3110a8cdf3d`.
- Complete accepted-image Operator log:
  `shc94-real-app-conflict-operator-20260803T2348Z.log`, SHA-256
  `ed0c727359e0368c0e30652cbf1d9991a0db0a8bca1b57795ea1b87a4db1635c`.
- Complete companion workload log:
  `shc94-real-app-conflict-20260803T2348Z.log`, SHA-256
  `238ff88035e37fc58d270a907e5c04f7e87142ec62b3896de6d22e6422b8c621`.
- Full Make test result: 43 suites passed, zero failures, 78.3 percent
  composite coverage.
- Additional gates: `make fmt vet`, `make build`, Helm lint, 150 Helm unit
  tests, and 1,000 focused timer repetitions passed.
- Live-image qualification: pending native Linux builder availability.

## Interfaces and Dependencies

SHC-105 changes only App Framework repository-poll requeue calculation. It
depends on the existing five-second overdue fallback and the shared requeue
validator. It does not alter Custom Resource schemas, StatefulSet strategy,
Splunk Enterprise, Docker-Splunk, Splunk Ansible, S3 contents, or customer poll
configuration.
