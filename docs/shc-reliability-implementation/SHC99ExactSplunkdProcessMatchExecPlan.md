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
followed by whitespace or end of input, and either `start` or `restart` must
be a separate argument. A deterministic test supplies synthetic process
tables for the known false positive and both real daemon forms. No readiness,
lifecycle-hold, shutdown, or restart timing behavior changes.

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
- [x] (2026-08-03 16:46Z) Integrated the isolated correction as `9bb94c929`
  into exact cumulative source `aa3792287`, then passed
  `make shc98-monitor-check`, all 43 native-Linux Make suites and 192
  enterprise/controller specs, composite coverage 78.3 percent, and
  `make build` on Linux AMD64.
- [x] (2026-08-03 18:08Z) Deployed initial integrated image index
  `sha256:e9ef83a9a50d90461d13e5634e744c0cded7806cac0bb8194e45aee84c4395b2`
  and stopped qualification when a live initialized Search Head running
  `splunkd -p 8089 restart` correctly exposed that the start-only exact match
  returned `Splunkd not running`. Direct invocation left the Pod UID,
  readiness, and restart count unchanged.
- [x] (2026-08-03 18:15Z) Audited all 20 managed Splunk Pods and the pinned
  Splunk Ansible source. Sixteen live Pods used the exact `restart` form, four
  used exact `start`, and Ansible contains both supported commands. Commit
  `05b7b3ea7` accepts both tokens while rejecting `autostart`, `restartable`,
  and `mysplunkd`; focused repeats, the enterprise package, complete Linux
  Make gates, and `make build` passed.
- [x] (2026-08-03 18:21Z) Reproduced a separate namespace probe-ConfigMap
  race during image restoration: concurrent controllers lost resource-version
  updates and emitted misleading `GetIndexerStatefulSetFailed` Warnings.
  Commit `0b56ec79b` re-reads and retries conflicts; 20 deterministic conflict
  repetitions and the complete Linux gate passed.
- [x] (2026-08-03 18:34Z) Qualified final immutable Operator index
  `sha256:0f2480b1e8e39d6e5a00e014df280c5aa3167abe5e498dd1deaac7399254f0f6`
  on EKS. Real `start` and `restart`, synthetic exact forms, false-positive
  rejection, and lifecycle hold all returned the expected results. The
  selected Pod UIDs, readiness, and restart counts were unchanged; both
  environments stayed 10/10 Ready with zero restarts; concurrent ConfigMap
  propagation produced zero Warning Events and zero controller ERROR/FATAL
  logs.
- [x] (2026-08-03 18:36Z) Restored accepted Operator index
  `sha256:a9f2125097fa823d5182e8729683e5099116a889fdae8e892f0bd3110a8cdf3d`.
  The restoration emitted no Warning or controller error, and both final
  cluster snapshots remained Ready with four searchable indexers and four Up
  distributed peers on every Search Head.

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
- Observation: a healthy long-running Splunk daemon may retain either the
  `start` or `restart` command that launched it.
  Evidence: 16 of 20 live Pods used `splunkd -p 8089 restart`, while four used
  `splunkd -p 8089 start`; the pinned Ansible source has distinct start and
  restart tasks.
  Consequence: exact matching must accept both tokens without falling back to
  substring matching.
- Observation: all Splunk CR controllers reconcile one namespace-scoped probe
  ConfigMap after an Operator image change.
  Evidence: the accepted-image restoration produced simultaneous Kubernetes
  resource-version conflicts, six controller errors, and one misleading
  `GetIndexerStatefulSetFailed` Warning per namespace even though both
  StatefulSets were healthy.
  Consequence: probe data reconciliation retries optimistic-lock conflicts
  from the latest object and logs only a terminal retry failure.

## Decision Log

- Decision: retain the existing `ps` pipeline and tighten only its regular
  expression.
  Rationale: this minimizes container compatibility risk and preserves the
  command availability assumptions already exercised by Docker-Splunk.
  Date/Author: 2026-08-03, Codex with Vivek Reddy.
- Decision: require `splunkd` to be a whitespace- or slash-delimited basename
  and `start` or `restart` to be a separate token.
  Rationale: these are the two real runtime forms observed in the qualified
  topology. Token boundaries still reject `splunkdev`, `mysplunkd`,
  `autostart`, `restartable`, and similar substrings.
  Date/Author: 2026-08-03, Codex with Vivek Reddy.
- Decision: retry shared probe ConfigMap update conflicts from a fresh
  Kubernetes read.
  Rationale: every writer has the same Operator-owned desired scripts, so an
  identical concurrent winner is success; persistent or non-conflict errors
  still return normally.
  Date/Author: 2026-08-03, Codex with Vivek Reddy.
- Decision: keep SHC-99 separate from SHC-98.
  Rationale: stable distributed-peer addressing did not cause the probe bug;
  separate commits preserve review, rollback, and qualification boundaries.
  Date/Author: 2026-08-03, Codex with Vivek Reddy.

## Outcomes & Retrospective

SHC-99 is complete at cumulative source `0b56ec79b`. The known false-positive
commands are rejected; real and synthetic exact `start` and `restart` forms
are accepted; lifecycle hold remains live without inspecting the process
table; and the test no longer depends on unrelated host processes. The exact
source passed macOS and native-Linux gates, its immutable image passed EKS,
and final accepted-image restoration preserved both healthy clusters. The
qualification also found and closed the shared probe ConfigMap conflict in
SHC-101 rather than hiding its false Warning Events.

## Context and Orientation

`tools/k8_probes/livenessProbe.sh` is mounted into Splunk Pods by the Operator.
When `NO_HEALTHCHECK` is empty and
`SPLUNK_OPERATOR_LIFECYCLE_HOLD` is not true, liveness level one calls
`liveness_probe_check_splunkd_process`. That function reads
`splunk-container.state` and requires both an initialized state and a matching
Splunk daemon process. Qualified containers retain either a `start` or
`restart` daemon argument depending on whether startup automation most
recently started or restarted Splunk.

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
- `/opt/splunk/bin/splunkd -p 8089 start` and
  `splunkd -p 8089 restart` return success;
- `autostart`, `restartable`, `mysplunkd`, and the observed Coder command
  return `Splunkd not running` and a nonzero exit;
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
change creates no durable state. The EKS fixture only replaced `ps` through a
temporary `PATH` for direct script invocation and removed its `/tmp` files on
exit. If another real daemon form is found, add that exact process table as a
regression fixture before broadening the token expression. The accepted
Operator digest is the tested environment rollback; reverting `05b7b3ea7`
would restore the rejected start-only behavior.

## Artifacts and Notes

- Branch: `codex/shc-99-exact-splunkd-process-match`.
- Exact source: `184061106`.
- Final-integration functional commit: `9bb94c929`.
- Start/restart correction: `05b7b3ea7`.
- Concurrent probe ConfigMap correction: `0b56ec79b`.
- Final-integration cumulative source:
  `0b56ec79b99cc4d58aa36eb1e8bb7f9ebf7e6932`.
- Focused repeats: 20 passes.
- Native-Linux Make gate: 43 suites, 192/192 specs, zero failures.
- Composite coverage: 78.3 percent.
- Linux manager SHA-256:
  `0064e7fd7372a59669be01ed7a906ec6e37cbe904e870571ddc9d255b3922758`.
- Final probe SHA-256:
  `8faf8fac6bb133db53f4c6b9190495885f97bd494d9afd499d5e1b0a5fc98d66`.
- Final Operator OCI index:
  `sha256:0f2480b1e8e39d6e5a00e014df280c5aa3167abe5e498dd1deaac7399254f0f6`.
- Candidate fresh/retained snapshot SHA-256:
  `2c5736f4aa527e431e364726155b84a8a42bf9e3e51173f9479b9ba87ac06f6`
  and `e05c08b2b6e68584cc0a77f075f844dccb3b1e2d43b4f0231613ff8b87bf22ab`.
- Accepted-restoration fresh/retained snapshot SHA-256:
  `2cbf54d6b7d5e7f192775aa641aa5bd48a58c8148acaa598f9b6778f9ab5fa5b`
  and `35ab757d3b5ef457c15bddedd012f02977240009e7be55d7239961989c309bd7`.
- No Splunkd, Docker-Splunk, Ansible, CRD, or persistent-data change.

## Interfaces and Dependencies

The accepted process forms are equivalent to:

    splunkd ... start
    splunkd ... restart
    /some/path/splunkd ... start
    /some/path/splunkd ... restart

Both the executable basename and action are tokens; `splunkdev`, `mysplunkd`,
`autostart`, `restartable`, and `starter` do not qualify. The script continues
to depend on `ps`, extended `grep`, `head`, and `awk`, which were already
runtime dependencies before SHC-99.

Revision note (2026-08-03 18:36Z): Replaced the source-only start assumption
with the two process forms observed across 20 live Pods, recorded the rejected
initial EKS image, added the start/restart correction and bounded runtime
matrix, registered the shared ConfigMap conflict as SHC-101, and closed the
plan with exact Linux, OCI, EKS, and accepted-restoration evidence.
