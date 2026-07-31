# SHC-84 Startup and Termination Qualification

Status: implementation source and candidate EKS behavior are qualified for
existing-v4 reconciliation, fresh formation, forced liveness recovery, and
planned Pod deletion. A supported-version upgrade remains an explicitly
unqualified matrix cell.

## Scope

SHC-84 covers one interaction boundary:

- image-owned first-start and supported-upgrade work must have an explicit,
  bounded Kubernetes startup budget;
- liveness must not be weakened merely because a supported Splunk restart
  briefly closes the management port;
- a kubelet-initiated restart must reach the same bounded, exact-once runtime
  shutdown as every other TERM path; and
- probe-failure termination must not consume the longer grace reserved for a
  planned Pod deletion.

The current campaign uses `enterprise.splunk.com/v4`. It does not add or
qualify a v3-to-v4 conversion or migration path. No Splunkd source change is
part of this work.

## Qualified source

Operator branch:
`codex/shc-84-startup-term-qualification`

| Commit | Purpose |
|---|---|
| `968e19b94` | Extend the startup failure budget and separate startup/liveness restart grace from Pod-deletion grace |
| `3b0e9a2e9` | Add direct-TERM recovery evidence and correct the current readiness contract |
| `d4cc12fc3` | Remove the unsupported SHC readiness-endpoint assumption from the product requirements |
| `4ef6b488c` | Record the measured first-start and direct-TERM baseline |
| `a954dd1b5` | Normalize only the exact persisted v4 legacy startup-probe default |
| `c58ff86cd` | Reject probe-level termination grace on readiness through the v4 CRD and reconciliation validation |
| `8c402b325` | Record the completed local source and runtime validation |
| `301215fc7` | Record the forced-liveness baseline and build the subsequently rejected first Linux candidate |
| `fb9f7b6dc` | Qualify the planned-deletion baseline and harden peer availability checks |
| `67c0d3bd2` | Reconcile probe-grace-only Pod-template changes and add regression coverage |
| `cbaef60af` | Make candidate monitors fail on peer replacement, peer restart, endpoint loss, or extra target restart |

Docker-Splunk branch:
`codex/shc-84-startup-term-qualification`

| Commit | Purpose |
|---|---|
| `1c93f4c` | Record the cross-repository shutdown-budget contract and current-runtime TERM result |

The Docker-Splunk implementation under test already contained the exact-once
shutdown helper and PID-1 exit correction. SHC-84 did not change executable
runtime code.

The accepted Operator binary was built from exact commit
`67c0d3bd28c3d88a72d629ffb1245a139399fc0d` and published at immutable digest
`sha256:d83ae44c825f13cb12117e72d2ca5415b4ffd9b7af36bcab7e81226e11e6cafe`.
Commit `cbaef60af652a17acdefe31a608cc8ced265c4f1` contains later evidence-monitor
hardening only; it is not claimed as the binary source.

## Kubernetes contract

The candidate v4 contract separates four clocks:

| Clock | Candidate default | Meaning |
|---|---:|---|
| Startup failure budget | approximately 30 minutes | Time allowed for image-owned first start and upgrade before kubelet restart |
| Startup/liveness termination grace | 660 seconds | Time allowed after a restart-causing probe fails |
| Runtime local shutdown deadline | 600 seconds | Maximum time the image allows `splunk stop` |
| Planned Pod-deletion grace | 1200 seconds | Time shared by `preStop`, TERM shutdown, and final kubelet cleanup |

Startup uses `initialDelaySeconds: 40`, `periodSeconds: 30`, and
`failureThreshold: 60`. Readiness and liveness retain their existing
thresholds. This is intentional: the baseline did not show consecutive
liveness failure sufficient to kill a healthy member, and readiness must
withdraw traffic promptly.

The v4 `startupProbe` and `livenessProbe` accept
`terminationGracePeriodSeconds` from 1 through 86400. Readiness rejects the
field because a failed readiness probe does not terminate the container.
When `SplunkPodLifecycle` is enabled and the customer omits the probe value,
the Operator renders 660 seconds. An explicit customer value is preserved.

Existing v4 objects can already have the old CRD startup tuple
`40/30/30/12` persisted. With the lifecycle gate enabled, the Operator resolves
only that exact legacy-default tuple to failure threshold 60. Any tuple with a
customer-modified delay, timeout, period, threshold, or probe grace remains
unchanged. This is current-v4 default normalization and does not add a
v3-to-v4 migration contract.

The 660-second default is the image's current 600-second shutdown deadline plus
a 60-second kubelet margin. A customer who increases
`SPLUNK_SHUTDOWN_TIMEOUT_SECONDS` must increase both restart-causing probe
grace values as well.

## Default-policy baseline

The baseline used:

- EKS Kubernetes `v1.31.14-eks-8f14419`;
- the official fixed Splunk runtime build
  `splunkcloud-10.5.2605.0-844c593e9c1d-linux-amd64`;
- runtime digest
  `sha256:2b6d0f3b316eca90f061bfc22be2f6fc59c960fcfaa6791a871c0a5d4ee0b2c2`;
- three Search Heads with persistent `etc` and `var` volumes; and
- the pre-candidate rendered defaults.

The Search Heads rendered:

```text
startup:   initial=40 timeout=30 period=30 failure=12 probeGrace=unset
liveness:  initial=30 timeout=30 period=30 failure=3  probeGrace=unset
Pod grace: 1200
```

Observed first-start startup-probe failures were six on ordinal zero and seven
each on ordinals one and two. All containers completed image-owned
initialization with zero Kubernetes restarts. The supported first-formation
rolling restart produced two non-consecutive liveness failures per member over
the complete formation window and did not restart a container. One
`SHCInitialFormationRestartStarted` Event was emitted. The SHC reached
`Ready`, formation stage `Complete`, three registered `Up` members, and three
client endpoints at `2026-07-30T23:48:44Z`.

This proves the fixed runtime can form under the old default in this
environment. It does not make that approximately six-minute startup budget
acceptable: an earlier supported first start took about 7 minutes 24 seconds
and was killed by that budget, and supported upgrade duration has not yet been
qualified against the candidate.

## Direct TERM result

At `2026-07-31T00:05:56Z`, the test sent TERM directly to PID 1 in the
established non-captain ordinal one. This bypassed `preStop` and exercised the
image TERM path.

Observed result:

- `/sbin/splunk-shutdown` recorded `source=term`;
- the local stop completed with result zero;
- PID 1 exited at `2026-07-31T00:06:39Z`, approximately 42 seconds after the
  trigger and far below the configured grace;
- Kubernetes restarted the container exactly once;
- the Pod UID remained
  `04eb85ef-131e-472b-894b-05f59e4765ba`;
- the two unaffected Search Heads remained client endpoints;
- the same-version persistent member rejoined without Pod replacement; and
- the SHC returned to `Ready`, `Complete`, and three endpoints, with the first
  stable observation at `2026-07-31T00:07:57Z`.

The previous container log retained both:

```text
splunk-shutdown: stop started source=term timeout_seconds=600
splunk-shutdown: stop completed source=term result=0
```

The reusable evidence monitor is
`test/fixtures/shc-reliability/shc84_term_exit_monitor.sh`.

## Forced-liveness current-runtime result

At `2026-07-31T00:34:23Z`, the test replaced the established non-captain
ordinal two's container state marker with an unhealthy value. This exercised
the current kubelet liveness-failure path with the pre-candidate probe policy.

Observed result:

- readiness withdrew the member from the client Service by
  `2026-07-31T00:34:37Z`, 14 seconds after the trigger;
- the other two Search Heads remained client endpoints throughout the run;
- after the configured consecutive liveness failures, `preStop` completed the
  runtime shutdown helper and TERM observed
  `shutdown already completed result=0 source=term`;
- the old container exited with result zero at `2026-07-31T00:36:31Z`;
- Kubernetes restarted the container exactly once without changing Pod UID
  `4705f7ab-7b38-4440-850a-6cbeba8d03fb`;
- the captain remained ordinal zero and all members returned registered and
  `Up`; and
- the first `Ready`, `Complete`, three-endpoint observation was
  `2026-07-31T00:37:55Z`.

The Pod did not retain a `Killing` Event for this restart. Qualification and
support procedures therefore cannot use that Event as their only restart
evidence. They must correlate readiness and liveness `Unhealthy` Events with
the container restart count, `lastState.terminated`, the unchanged Pod UID,
the previous container shutdown log, and the client EndpointSlice.

This baseline proves the current runtime and hook converge on one stop when
liveness kills a container. It does not qualify the candidate's 660-second
probe-level grace, because the pre-candidate Pod had no probe-level override
and used the 1200-second Pod grace.

## Planned-deletion current-runtime result

At `2026-07-31T02:30:10Z`, the test deleted established non-captain ordinal
one through the Kubernetes API. This exercised the current Pod-level
1200-second grace and runtime `preStop` path; it did not claim an
Operator-controlled rolling revision.

Observed result:

- the deleting member's serving readiness became false and the client Service
  retained the two unaffected endpoints by `2026-07-31T02:30:15Z`;
- the unaffected Pod UIDs and their captured restart counts did not change;
- StatefulSet replacement was first observed with new Pod UID
  `8bbc528e-ef13-460c-8048-db77e4e74ffc` and zero restarts at
  `2026-07-31T02:30:53Z`;
- the captain remained ordinal zero;
- the replacement rejoined registered and `Up`; and
- the first `Ready`, `Complete`, three-endpoint observation was
  `2026-07-31T02:31:58Z`.

The client endpoint count never fell below two. The established-recovery
monitor was corrected to compare each member with its captured restart-count
baseline, rather than assuming every campaign starts with zero restarts. It
also now rejects replacement of either unaffected peer.

The attempted live log stream ended with a connection reset as the old Pod
disappeared and did not durably retain the hook output. The replacement Pod
cannot provide the old Pod's container or hook log. Planned-deletion
qualification therefore still requires a durable shutdown result outside the
ephemeral Pod log stream. Until that is implemented, support must correlate
the deletion timestamp, serving-readiness withdrawal, old and new Pod UIDs,
replacement timing, unaffected endpoints, and SHC rejoin state; those facts
prove availability and recovery but do not independently prove exact-once
shutdown ownership.

## Source validation

The following completed successfully on macOS:

- `make generate manifests`;
- `make fmt vet`;
- `make build`;
- `make test`: 41 suites, 156 specs, zero failures, 78.6% composite coverage;
- focused API, probe-rendering, validation, and Search Head StatefulSet tests;
- shell syntax and ShellCheck for both SHC-84 monitors; and
- Docker-Splunk `make test_shutdown`: seven tests, zero failures.

The exact accepted binary source also completed the canonical Linux workstation
path:

- `make generate manifests`;
- `make fmt vet build`;
- `git diff --exit-code`; and
- `make test`: 41 suites, 156 specs, zero failures, 78.6% composite coverage.

The replacement image was built with `make docker-buildx` for
`linux/amd64`. Generated CRDs were installed through `make install`, and the
API server rejected a v4 readiness probe carrying
`terminationGracePeriodSeconds`. The Operator Deployment used the immutable
digest above, retained the three lifecycle feature gates, became Available,
and recorded zero container restarts during qualification.

## Rejected first candidate

The first Linux candidate was built from Operator commit `301215fc7` and
published as immutable digest
`sha256:21b0c301f91005ac4d89f7dcd4c08b222c4e7f047d8acf77bfb5315014095c19`.
The image and generated CRDs deployed successfully, and the API server rejected
probe-level termination grace on readiness as required.

Live reconciliation of an existing v4 SHC then found a compatibility defect.
The desired StatefulSet template resolved startup to failure threshold 60 and
probe grace 660, but liveness retained no probe-level grace. StatefulSet merge
logic compared only the four existing probe timing fields. Startup happened to
converge because its threshold also changed, causing the complete revised probe
to be copied. Liveness differed only by the newly introduced grace field, so
that update was not detected.

This image is rejected and must not be used as qualification evidence. The
merge comparison now treats addition, removal, or value change of
`terminationGracePeriodSeconds` as a Pod-template change and has direct and
merge-level regression coverage.

## Existing-v4 reconciliation result

The accepted image and generated CRDs were first applied to an existing v4
three-member SHC whose persisted startup probe still contained the exact
legacy tuple. Reconciliation produced:

```text
startup:   initial=40 timeout=30 period=30 failure=60 probeGrace=660
liveness:  initial=30 timeout=30 period=30 failure=3  probeGrace=660
readiness: initial=10 timeout=5  period=5  failure=3  probeGrace=unset
Pod grace: 1200
```

The current `OnDelete` lifecycle converged all three Pods to StatefulSet
revision `splunk-shc84-shc-search-head-666c586686`. Every Pod retained zero
container restarts, all three members returned registered and `Up`, and the
SHC returned to `Ready`, formation stage `Complete`, and three client
endpoints. Captaincy was observed on both ordinal one and ordinal zero while
the rollout progressed; the final ordinal-zero target was handled only after
captain transfer. This qualifies current-v4 default normalization and the
probe-grace-only merge fix. It does not add a v3 conversion contract.

## Fresh candidate formation result

The accepted candidate formed a new three-member v4 SHC in namespace
`shc84-startup-term-candidate` using the official fixed runtime digest
`sha256:2b6d0f3b316eca90f061bfc22be2f6fc59c960fcfaa6791a871c0a5d4ee0b2c2`.
The fresh StatefulSet rendered the four-clock contract exactly as shown above.

Measured observations were:

- the monitor began before the CR existed at `2026-07-31T03:22:24Z`;
- the three Search Head Pods completed image-owned startup and the supported
  first-formation restart with zero Kubernetes container restarts;
- formation advanced to `TelemetryApplied` at
  `2026-07-31T03:32:20Z`;
- the first `Ready`, `Complete`, three-Pod and three-endpoint sample was
  `2026-07-31T03:37:14Z`; and
- twelve final stable samples retained three endpoints and zero restarts.

The candidate therefore qualifies the approximately 30-minute fresh-start
budget without weakening readiness or liveness thresholds.

The run also exposed a separate status-semantics gap. The SHC initially
reported `Error` while its referenced LicenseManager was still starting.
Controller validation described the dependency as not ready or not yet on the
required image and attached an upgrade-path validation failure. Without user
action, the same SHC changed to `Pending` once the LicenseManager became Ready
and then completed formation. A retryable dependency wait must be represented
as Pending/Progressing with a bounded dependency reason; `Error` must be
reserved for terminal or incompatible dependency state. This observation did
not cause a workload restart or formation failure, but it is a supportability
requirement rather than an accepted status contract.

## Forced-liveness candidate result

At `2026-07-31T03:42:09Z`, the established non-captain ordinal two was made
unhealthy through the runtime state marker. This exercised the accepted
660-second liveness-probe termination grace.

Observed result:

- readiness removed ordinal two from client endpoints by
  `2026-07-31T03:42:25Z`;
- ordinals zero and one remained endpoints and retained their UIDs and restart
  counts throughout;
- the runtime artifact recorded one shutdown owner,
  `pid=10409 source=prestop`, at `2026-07-31T03:43:16Z`;
- TERM reused that result, with the previous log reporting
  `shutdown already completed result=0 source=term`;
- the successful stop took approximately 43 seconds from the first observed
  `preStop` owner to old-container exit, longer than Kubernetes' ordinary
  30-second default and well within the rendered 660-second probe grace;
- Kubernetes restarted only the target container, exactly once, by the
  `2026-07-31T03:44:02Z` sample;
- target Pod UID
  `4d2ce22a-ebe5-4d8e-a2f3-10af24494851` did not change; and
- the first recovered `Ready`, `Complete`, three-endpoint sample was
  `2026-07-31T03:45:22Z`, followed by twelve stable samples.

The accepted probe override therefore provides a bounded kubelet restart,
keeps two healthy members serving, and converges `preStop` and TERM on one
runtime stop.

## Planned-deletion candidate result

At `2026-07-31T03:48:11Z`, the established non-captain ordinal one was deleted
through the Kubernetes API. The deleting Pod carried the separate
1200-second Pod grace.

Observed result:

- serving readiness and the client endpoint were withdrawn by
  `2026-07-31T03:48:13Z`;
- ordinals zero and two remained endpoints with unchanged UIDs and restart
  counts;
- the original Pod UID
  `c409ee5b-63c3-4ba0-bcec-2e9e9ba121d3` exited, and the replacement UID
  `a4b7fe61-da12-43c1-8024-9455873b1b62` was observed at
  `2026-07-31T03:48:54Z`;
- the replacement became registered and `Up` before it was returned to client
  traffic;
- all three endpoints were present at `2026-07-31T03:50:03Z`; and
- the first `Ready`, `Complete` sample was `2026-07-31T03:50:10Z`, followed by
  twelve stable samples.

The client endpoint count never fell below two. As in the baseline, the old
Pod's hook output was not durably available after replacement. Availability,
identity, and rejoin behavior are qualified; durable per-Pod shutdown-stage
retention remains a separate observability requirement.

## Remaining acceptance gates

| Acceptance cell | Result |
|---|---|
| Exact Linux Make source gate | Passed |
| Immutable Operator image and generated CRDs | Passed |
| Existing-v4 normalization and reconciliation | Passed |
| Fresh v4 formation | Passed |
| Same-version persistent-container restart | Passed |
| Forced liveness failure with probe grace 660 | Passed |
| Planned Pod deletion with Pod grace 1200 | Passed |
| Supported-version upgrade exercising image-owned upgrade work | Open |

No older supported source image was available in this campaign for the final
upgrade comparison. That cell remains unqualified; it is not converted into an
inferred pass. The candidate policy is qualified for the measured current-v4
paths above, not for every future Splunk upgrade duration.
