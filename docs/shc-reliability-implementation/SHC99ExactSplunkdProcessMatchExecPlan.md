# Make level-one liveness identify the Splunk daemon exactly

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to
date as work proceeds.

This document is maintained in accordance with the ExecPlan requirements in
the `execution-plan` skill.

## Purpose / Big Picture

The level-one Splunk Pod liveness probe is used during Operator-owned
lifecycle work. Outside an explicit lifecycle hold, it must remain successful
only while the real Splunk daemon command is running. Its former process
search accepted any process text containing `splunkd`, followed anywhere by
`start`. A local Coder/VS Code process therefore passed the check because its
hostname contained `splunkd` and a later option contained `autostart`.

SHC-99 makes the match token-aware: `splunkd` must be a command/path basename
followed by whitespace or end of input, and `start` must be a separate
argument. A deterministic test supplies synthetic process tables for the
known false positive and the real `/opt/splunk/bin/splunkd ... start` command.
No readiness, lifecycle-hold, shutdown, or restart timing behavior changes.

## Progress

- [x] (2026-08-03 01:23Z) Reproduced the existing test failure repeatedly on
  macOS and identified the exact host process responsible.
- [x] (2026-08-03 01:31Z) Added a token-aware process match and deterministic
  fake-process-table coverage for both rejection and acceptance paths.
- [x] (2026-08-03 01:32Z) Passed ShellCheck and 20 consecutive focused test
  runs.
- [x] (2026-08-03 01:34Z) Passed the full `pkg/splunk/enterprise` package.
- [x] (2026-08-03 01:42Z) Passed `make build` and the complete `make test`
  gate. The final controller report contains 194 test nodes, zero failures;
  composite coverage is 78.3 percent.
- [x] (2026-08-03 01:43Z) Committed and pushed exact source `184061106` on
  isolated branch `codex/shc-99-exact-splunkd-process-match`.
- [ ] Reproduce the focused test and complete Make gates on Linux AMD64 before
  final integration.
- [ ] Exercise level-one liveness in the immutable EKS runtime during a normal
  lifecycle stage and prove the Pod becomes unhealthy if splunkd is absent
  without an explicit lifecycle hold.

## Surprises & Discoveries

- Observation: the false positive did not require a Splunk process.
  Evidence: the matching process was a Coder command containing
  `vworkstation.splunkdev.net` and `--disable-autostart`.
  Consequence: the original regex tested arbitrary substrings across a full
  command line rather than process identity and an argument.
- Observation: the pre-existing test depended on the host process table.
  Evidence: it expected no host command to match the broad expression; the
  same source could pass or fail depending on unrelated developer tooling.
  Consequence: SHC-99 replaces that environmental assumption with a fake `ps`
  executable and exact synthetic process tables.
- Observation: the explicit lifecycle-hold path does not run the process
  search.
  Consequence: the accepted SHC-85 behavior that keeps an initialized,
  responsive container live after an Operator-owned Splunk stop remains
  unchanged.

## Decision Log

- Decision: retain the existing `ps` pipeline and tighten only its regular
  expression.
  Rationale: this minimizes container compatibility risk and preserves the
  command availability assumptions already exercised by Docker-Splunk.
  Date/Author: 2026-08-03, Codex with Vivek Reddy.
- Decision: require `splunkd` to be a whitespace- or slash-delimited basename
  and `start` to be a separate token.
  Rationale: this accepts the real absolute-path and basename command forms
  while rejecting `splunkdev`, `autostart`, and similar substrings.
  Date/Author: 2026-08-03, Codex with Vivek Reddy.
- Decision: keep SHC-99 separate from SHC-98.
  Rationale: stable distributed-peer addressing did not cause the probe bug;
  separate commits preserve review, rollback, and qualification boundaries.
  Date/Author: 2026-08-03, Codex with Vivek Reddy.

## Outcomes & Retrospective

The source correction is complete and passes the macOS source gates. The
known false-positive command is rejected, the real Splunk daemon command is
accepted, and the test no longer inspects unrelated host processes. Linux and
immutable EKS evidence remain open, so SHC-99 is source-qualified locally but
not yet integrated or EKS-qualified.

## Context and Orientation

`tools/k8_probes/livenessProbe.sh` is mounted into Splunk Pods by the Operator.
When `NO_HEALTHCHECK` is empty and
`SPLUNK_OPERATOR_LIFECYCLE_HOLD` is not true, liveness level one calls
`liveness_probe_check_splunkd_process`. That function reads
`splunk-container.state` and requires both an initialized state and a matching
Splunk daemon process.

`pkg/splunk/enterprise/probe_lifecycle_hold_test.go` executes the real Bash
script. SHC-99 places a test-owned `ps` executable first in `PATH`, while
leaving `grep`, `head`, and `awk` available from system directories. This
makes the test portable and proves only the intended process matching.

## Plan of Work

Keep the source change limited to the process expression and deterministic
tests. Run formatting, ShellCheck, repeated focused tests, the full enterprise
package, `make build`, and `make test`. Repeat the Make gates on a clean Linux
AMD64 worktree.

After SHC-99 is integrated into an immutable Operator image, observe a normal
level-one lifecycle transition on EKS. The positive path must remain live with
the real daemon. A bounded negative fixture must remove or replace only the
process-table input in a disposable test context; it must not kill production
Splunk, mutate persistent data, or weaken lifecycle hold. Record probe output,
Pod conditions, restart count, and lifecycle state.

## Validation and Acceptance

Source acceptance requires:

- the synthetic Coder command containing `splunkd` and `autostart` returns
  `Splunkd not running` and a nonzero probe exit;
- `/opt/splunk/bin/splunkd -p 8089 start` returns success;
- lifecycle-hold initialized, failed-initialization, and missing-state tests
  retain their prior results;
- ShellCheck, focused repeat tests, enterprise package tests, `make build`,
  and `make test` pass; and
- generated files are unchanged and the worktree is clean after commit.

EKS acceptance requires an immutable source/image relationship, correct
positive behavior during real lifecycle work, fail-closed behavior without a
real daemon and without hold, no unexpected restart during explicit hold, and
no change to readiness or termination orchestration.

## Idempotence and Recovery

The probe is read-only and every invocation recomputes process state. The
change creates no durable state. Reverting exact commit `184061106` restores
the old matcher, although that rollback also restores the known false-positive
risk. If a Linux/container process format is not accepted, revise the bounded
token expression and add that exact process table as a regression fixture
before integration.

## Artifacts and Notes

- Branch: `codex/shc-99-exact-splunkd-process-match`.
- Exact source: `184061106`.
- Focused repeats: 20 passes.
- Full controller JUnit: 194 nodes, zero failures.
- Composite coverage: 78.3 percent.
- No Splunkd, Docker-Splunk, Ansible, CRD, or persistent-data change.

## Interfaces and Dependencies

The accepted process forms are equivalent to:

    splunkd ... start
    /some/path/splunkd ... start

Both `splunkd` and `start` are tokens; `splunkdev`, `mysplunkd`, `autostart`,
and `starter` do not qualify. The script continues to depend on `ps`, extended
`grep`, `head`, and `awk`, which were already runtime dependencies before
SHC-99.
