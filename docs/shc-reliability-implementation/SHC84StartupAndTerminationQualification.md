# SHC-84 Startup and Termination Qualification

Status: implementation source and current-runtime baseline qualified; candidate
Linux image and EKS policy qualification remain open.

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

Docker-Splunk branch:
`codex/shc-84-startup-term-qualification`

| Commit | Purpose |
|---|---|
| `1c93f4c` | Record the cross-repository shutdown-budget contract and current-runtime TERM result |

The Docker-Splunk implementation under test already contained the exact-once
shutdown helper and PID-1 exit correction. SHC-84 did not change executable
runtime code.

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

## Source validation

The following completed successfully on macOS:

- `make generate manifests`;
- `make fmt vet`;
- `make build`;
- `make test`: 41 suites, 156 specs, zero failures, 78.6% composite coverage;
- focused API, probe-rendering, validation, and Search Head StatefulSet tests;
- shell syntax and ShellCheck for both SHC-84 monitors; and
- Docker-Splunk `make test_shutdown`: seven tests, zero failures.

## Remaining acceptance gates

SHC-84 is not complete until the exact pushed source is:

1. built through the Linux workstation Make path;
2. published as an immutable Operator image;
3. deployed with its generated CRDs;
4. proven to render startup failure threshold 60, startup/liveness grace 660,
   readiness grace unset, and Pod grace 1200;
5. qualified through fresh v4 SHC formation;
6. qualified through a same-version persistent-member restart;
7. forced through a startup- or liveness-probe failure to prove the 660-second
   override, exact-once shutdown, and bounded container exit;
8. qualified through planned Pod deletion to prove `preStop` and TERM converge
   on one shutdown while using the independent Pod grace; and
9. compared with a supported upgrade whose image-owned work is capable of
   exercising the longer startup window.

Absence of an available supported source image for the last upgrade comparison
must be recorded as an unqualified matrix cell; it must not be converted into
an inferred pass.
