# Current GitLab Develop Baseline

## Purpose

This document records implementation facts that affect sequencing. It is not a
requirements restatement and does not claim that any reliability capability is
complete.

Baseline observed:

- Repository: GitLab `sok/splunk-operator`
- Branch: `develop`
- Commit: `39316c19fb990f1af84966d5269a8f4116550dbb`
- Observation date: 2026-07-24

## Verified capability map

| Capability | Current baseline | Planning classification |
|---|---|---|
| StatefulSet rollout | `OnDelete`; the Operator detects revision mismatch and directly deletes one Pod at a time | Replace only after lifecycle gates are qualified |
| Pod management | `Parallel` | Retain initially, but qualify bootstrap, scale, and disruption behavior explicitly |
| Search Head detention | `PrepareRecycle` enables manual detention | Reuse and harden |
| Search drain | Historical and real-time counts are polled until zero | Add durable timing, timeout policy, outcome, and recovery behavior |
| Captain observation | The controller queries captain information and records captain readiness | Reuse as an observation source; strengthen consistency and staleness rules |
| Planned captain transfer | No verified controller workflow transfers captaincy before replacing the active captain | Design and add |
| Recycle completion | Detention is released after the restarted member reports `Up` | Strengthen with registered, synchronized, identity, and bounded rejoin gates |
| Permanent scale-down | Detains/drains and removes SHC membership before reducing replicas | Separate explicitly from ordinary restart and add captain-aware safety |
| Pod readiness | Probe checks container state and the local management root | Replace for Search Heads with the supported member-readiness contract |
| Pod liveness | Checks local process or management-port reachability depending on probe level | Revalidate so cluster conditions never cause destructive restart loops |
| Pod startup | Checks local management-root reachability | Keep separate from full SHC rejoin validation |
| Termination grace | Splunk workload Pod spec does not set it; Kubernetes default applies | Add API, validated default, migration behavior, and qualification |
| `preStop` | No verified Splunk workload hook | Add only with a single, idempotent runtime shutdown contract |
| Durable lifecycle state | Existing phase/status fields do not identify every rollout stage and timeout | Add an operation-oriented status and condition contract |
| Events and logs | Some scale, terminal failure, captain change, detention, and upgrade logging exists | Normalize around operation ID, stage, reason, target, and duration |
| Metrics | Search counts and upgrade timing exist | Redesign/extend with bounded labels and stage-duration outcomes |
| Workflow package | Package boundaries exist for future SHC workflows, but the SHC package currently contains documentation only | Use as a candidate home after an architecture decision |
| App/bundle target | Existing behavior can depend on ordinal zero or a static captain URL | Replace with dynamic reachable-member selection |
| Startup/bootstrap | Container automation may repeat cluster-forming commands on persistent restart | Define an explicit bootstrap/rejoin intent contract across repositories |

## Existing work that requires reassessment

### Configurable termination grace branch

Local branch `codex/gitlab-termination-grace-period` contains a draft API and
StatefulSet implementation. Its merge base predates the current `develop`
baseline and the repository layout has changed since that base. Treat the
branch as a source of tests and design ideas. Rebase or selectively reimplement
only after the API and migration decisions in the technical design are
approved.

### Per-Pod rolling restart branch

Remote branch `sok/don-ross-pod-rolling-restart` currently exposes only a
build/image commit relative to its old base in this checkout. The associated
Confluence design remains a design input, but the branch is not evidence that
partition-gated `RollingUpdate` is implemented.

### SHC stall and stuck-member branches

The `sok/SHC-stall` and `sok/fix/CSPL-4966-stuck-shc-members` branches contain
useful error handling, detention timeout, events, and tests. They are based on
older development revisions. Compare their behavior with current `develop`
before reuse; do not infer delivery from branch existence.

## Baseline refresh procedure

Before approving a design or starting a milestone:

1. fetch `sok/develop`;
2. record the new commit in the design decision log;
3. rerun repository searches for the capability being changed;
4. compare relevant unmerged branches against the new merge base;
5. update this matrix if current behavior changed; and
6. resolve any conflict between current code, the requirements page, and the
   proposed technical design before implementation begins.
