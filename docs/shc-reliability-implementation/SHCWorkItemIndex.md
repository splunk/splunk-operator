# Search Head Cluster Reliability Work-Item Index

## Purpose

This file is the central registry for the `SHC-*` execution identifiers used
while implementing and qualifying the Search Head Cluster reliability design.
It answers three separate questions:

- which bounded engineering work item changed the integrated feature branch;
- which immutable commits contain that work; and
- which stable requirement scenarios provide acceptance evidence.

`SHC-*` identifiers are implementation and qualification work items. They are
not product requirements and are not substitutes for the stable scenario IDs
in `SHCTestScenarioMatrix.md`. A work item can satisfy several scenarios, and a
scenario can depend on several work items.

The authoritative program status remains in `SHCImplementationExecPlan.md`.
The detailed test evidence remains in
`QualificationObservabilityRolloutPlan.md`. This index links those records
without duplicating their full content.

## Status vocabulary

- **Integrated** means the source commit is in the SHC reliability feature
  history.
- **Source-qualified** means its branch-local unit, controller, generation, or
  build gates passed.
- **EKS-qualified** means the behavior was exercised as part of a recorded EKS
  campaign. It does not mean every scenario associated with the capability is
  complete or that production default enablement is approved.

## Work-item registry

| Work item | Scope | Source commits | Primary scenarios | Recorded status |
|---|---|---|---|---|
| SHC-60 | Parse and retain member management URI needed for dynamic captain operations | `0e3864f1e` | LFC-002, LFC-009 | Integrated; source-qualified; exercised by later EKS captain-transfer campaigns |
| SHC-61 | Wait for local and captain views to converge before destructive progression | `9061027f7` | LFC-008, LFC-009, REJ-006 | Integrated; source-qualified; exercised by later EKS campaigns |
| SHC-62 | Bound replacement Pod startup and classify a replacement that does not start | `fd5d32ed1` | STS-008, REJ-004, REJ-005 | Integrated; source-qualified; remaining fault-injection variants are open |
| SHC-63 | Surface a blocked rollout through durable status and Kubernetes conditions | `8255c818e` | STS-008, OBS-001, OBS-004 | Integrated; source-qualified; blocked status exercised on EKS |
| SHC-64 | Preserve terminal lifecycle reason and diagnostic message | `63cc5cf2f` | OBS-001, OBS-005 | Integrated; source-qualified; terminal-detail behavior exercised by blocked campaigns |
| SHC-65 | Keep healthy non-target peers serving during rollout planning and target withdrawal | `4ff606a57` | HLT-003, HLT-004, LFC-012 | Integrated; source-qualified; serving invariant continuously checked on EKS |
| SHC-66 | Count rollout decision transitions once without polling-driven metric inflation | `dbc80363a` | OBS-002, OBS-003 | Integrated; source-qualified; targeted metric evidence recorded |
| SHC-67 | Continue a verified `OnDelete` lifecycle and recognize a detained owned target | `605e7cb37`, `702eb982a` | LFC-001, STS-001, STS-002 | Integrated; source-qualified; complete three-member `OnDelete` EKS rollout passed |
| SHC-68 | Make detention release, upgrade initialization/control, and uncertain detention requests bounded and retry-safe | `85d86c55e`, `60a32d728`, `8659c63ae`, `c77c3fb86` | LFC-010, LFC-011, OBS-002 | Integrated; source-qualified; exercised in integrated lifecycle campaigns |
| SHC-69 | Require ready KV Store before rollout authorization and recovery advancement | `22ab2ca0c` | REJ-011, STS-006 | Integrated; source-qualified; KV Store gate exercised in EKS happy path and later campaigns |
| SHC-70 | Record the first complete lifecycle qualification evidence | `ed7b1b656` | LFC-001, LFC-002, STS-001, STS-002, STS-005, STS-006 | EKS-qualified for the recorded three-member happy path and controller restart |
| SHC-71 | Rehearse active `RollingUpdate` rollback to `OnDelete` | `1f7dd6041` | STS-010 | EKS-qualified for one active ordinal-two rollback; additional rollback-under-fault variants remain open |
| SHC-72 | Correct and qualify scale lifecycle, cancellation, repeated-operation identity, member observation, and scale observability | `255759009`, `e7b696f5e`, `b4d2af703`, `7e97936df`, `6ebe009ad`, `ccab4fe33`, `89f4aebb4` | OPS-001, OPS-002, OPS-003, OBS-001, OBS-002 | EKS-qualified for cancellation, repeated `4 -> 3`, final `3 -> 4` and `4 -> 3`, storage policy, and 300-second stability |
| SHC-73 | Recover a withdrawn Pod update after drain timeout and refresh search counts during cancellation | `23bdb631b`, `5783e5b69`, `a463e89e6` | LFC-003, LFC-004, LFC-005, LFC-014 | EKS-qualified for real-time fail-closed cancellation and bounded historical drain |
| SHC-74 | Add audited post-timeout continuation with operation/token matching and a durable approval barrier | `54a5aae3c`, `5bfd23b18` | LFC-006, OBS-001, OBS-003, OBS-006 | EKS-qualified for wrong-token, stale-operation, exact approval, reverse-ordinal rollout, and 312-second stability |
| SHC-75 | Qualify failed captain transfer and pre-authorization revision withdrawal; handle ControllerRevision reuse, in-place readiness handoff, and StatefulSet generation observation | `eb6907ee5`, `44ccac31e`, `3e9e735a7` | LFC-007, OBS-001, OBS-002 | EKS-qualified for pre-authorization failure/cancellation, reverse-ordinal rollback, clean Event/log audit, and 321-second stability |
| SHC-76 | Retain an already-authorized target across a superseding desired revision, queue the later Pod template, and release it only after Kubernetes traffic readiness | `24eea3f37`, `243f7a5d2`, `50eb10514` | STS-003, STS-014, OBS-001, OBS-002 | EKS-qualified for post-authorization revision handoff, two distinct target authorizations, complete reverse-ordinal convergence, 127 uninterrupted searches, and 300-second final stability |

## SHC-75 immutable qualification inputs

- source branch:
  `codex/shc-75-captain-transfer-timeout-qualification`;
- final source before this documentation commit:
  `3e9e735a776eb90957a0d0d2722b28ce0da5baff`;
- Operator image:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk-operator:shc-reliability-3e9e735a7`;
- Operator image digest:
  `sha256:98b71dbbb394d51abea5e79a9f63e4423f43ae3f623d5ed3d28cb9d55c0b6f72`;
- EKS cluster: `vivek-spl-301372` in `us-west-2`;
- qualification namespace: `shc75-captain-timeout`;
- runtime image:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk:shc-prestart-7951d69-ansible-9954434-splunk-10.6.0.0-d9be152689b7`;
- Linux gate: `make fmt vet build test`, 41 Ginkgo suites, 154 controller
  specifications, zero failures, 78.5 percent composite coverage; and
- EKS result: forward `2 -> 1`, captain timeout failed closed, original
  captain UID preserved, revision withdrawal restored it in place, rollback
  `2 -> 1`, maximum unavailable `1/1`, expected Event deltas only, zero
  container restarts, and 321 continuous seconds of final stability.

## SHC-76 immutable qualification inputs

- source branch:
  `codex/shc-76-post-authorization-revision-withdrawal`;
- final source before this documentation commit:
  `50eb10514a550d67652663cd7ab6644313681dcc`;
- Operator source commits:
  `24eea3f37ddb95032cb495dc0b422e8ca3cf9116`,
  `243f7a5d295196e1003ea70a37947bb04bed681c`, and
  `50eb10514a550d67652663cd7ab6644313681dcc`;
- Operator image:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk-operator:shc-reliability-50eb10514`;
- Operator image digest:
  `sha256:62e450584a9788cd9b0f2959164bdcef2c75608c66bb468cc572e887712d7624`;
- EKS cluster: `vivek-spl-301372` in `us-west-2`;
- accepted qualification namespace: `shc76-revision-withdrawal`;
- runtime image:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk:shc-prestart-7951d69-ansible-9954434-splunk-10.6.0.0-d9be152689b7`;
- Linux gate: `make fmt vet build test`, 41 Ginkgo suites, 154 controller
  specifications, zero failures, 78.5 percent composite coverage;
- pre-action gate: 180 seconds, 25 healthy samples, three Ready and serving
  members, matching StatefulSet revisions, partition three, an authoritative
  dynamic captain, and zero restarts;
- STS-014 result: revision A was authorized for ordinal two; revision B was
  submitted during its replacement; the StatefulSet retained revision A and
  partition two until the first replacement was Ready, serving, registered,
  and `Up`; revision B then received a separate operation and authorization;
  and the rollout completed ordinals `2 -> 1 -> 0`;
- availability result: 127 successful service searches, zero failures,
  minimum two Ready endpoints, maximum one unavailable Pod, zero container
  restarts, and zero conflicting rollout Events in the run window; and
- final result: dynamic captain on ordinal one, all members `Up`,
  `service_ready_flag=1`, no Splunk rolling restart, KV Store `ready` with
  three members and no upgrade or backup, followed by a 300-second gate with
  37 successful samples and three Ready endpoints throughout.

The first destructive run exposed a real boundary error: Splunk-side lifecycle
`Completed` could precede the replacement Pod's Kubernetes Ready and serving
conditions, allowing the queued template to be released early. Commit
`50eb10514` closes that gap. A second run was intentionally excluded because
the just-formed baseline transiently lost member readiness before any
lifecycle operation; the Operator kept partition three and reported
`ExistingUnavailablePod` without authorizing disruption. The accepted third
run began only after the sustained pre-action gate.

## Next execution records

The next work item must be assigned before implementation and added here in the
same change that creates its branch.

Other remaining scenarios continue to be selected from
`SHCTestScenarioMatrix.md`; the absence of a new `SHC-*` number does not make a
scenario complete.

## Revision Note

2026-07-28: Created the central SHC-60 through SHC-76 execution registry,
linked implementation commits to stable scenario identifiers, recorded
qualification scope without claiming production readiness, and recorded the
post-authorization revision-withdrawal handoff.
