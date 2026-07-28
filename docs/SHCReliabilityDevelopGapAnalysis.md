# SHC Reliability on Kubernetes: Gap Analysis Against `sok/develop`

**Scope:** This document looks only at the *current* behavior of `sok/develop` (Splunk Operator), the current `splunkd/develop` SHC implementation, and the current `docker-splunk`/`splunk-ansible` `develop` branches — and lists what needs to be **added** to make Search Head Cluster (SHC) lifecycle reliable on Kubernetes. It intentionally excludes the multi-container redesign, the distroless image direction, Ansible removal, and other longer-term splunkd changes; those are tracked separately as future work. Everything below is either a direct code citation from the `develop` branch of the relevant repo, or a citation from a Confluence requirements page.

This is a research/planning document, not a proposal — it exists to give the near-term engineering work a single, code-verified punch list.

**Supported Kubernetes version:** `docs/ChangeLog.md` lists every release from 2.6.0 through 3.0.0 as supporting **1.27+**, open-ended (no upper bound). `docs/GettingStarted.md:48` adds "should work with any CNCF certified distribution." All Kubernetes-primitive claims below (StatefulSet update strategy, PDB, finalizers, preStop) are checked against this floor; where behavior is version-gated within the 1.27+ range, it's called out explicitly.

---

## 1. What `sok/develop` actually does today (verified)

| Area | Current `develop` behavior | Source |
|---|---|---|
| StatefulSet update strategy | `OnDelete` — Kubernetes never auto-replaces a Pod on spec change; the Operator drives all replacement itself | `pkg/splunk/enterprise/configuration.go:747-749` |
| Pod management policy | `Parallel` — all Pods in a SHC StatefulSet are created/started concurrently, no ordinal-0-first guarantee | `pkg/splunk/enterprise/configuration.go:746` |
| Termination grace period | **Not set at all** on the Pod spec → falls back to the Kubernetes built-in default of **30 seconds** | `pkg/splunk/enterprise/configuration.go:750-770` (no `TerminationGracePeriodSeconds` field present) |
| Lifecycle hooks | No `preStop`/`postStart` hook anywhere in the codebase | repo-wide grep, zero hits for `Lifecycle{` |
| Readiness probe | Bare curl to management root `https://localhost:8089/`, returns 0 on socket success. No SHC-specific endpoint (`/services/shcluster/member/ready`) is consulted | `tools/k8_probes/readinessProbe.sh`, `pkg/splunk/enterprise/configuration.go:1187-1190` |
| Liveness probe | Same mechanism plus a container-state-file/`K8_OPERATOR_LIVENESS_LEVEL` downgrade path | `tools/k8_probes/*`, `configuration.go:51-64` |
| Recycle (rolling update path) | `PrepareRecycle()`: sets liveness level, calls `InitiateUpgrade()`, sets `SetSearchHeadDetention(true)`; then polls `ActiveHistoricalSearchCount + ActiveRealtimeSearchCount == 0` **with no timeout at all** | `pkg/splunk/enterprise/searchheadclusterpodmanager.go:140-209` |
| Scale-down | Calls `PrepareRecycle()` then explicitly `RemoveSearchHeadClusterMember()` — this is the only path that removes RAFT membership; ordinary recycle never does | `searchheadclusterpodmanager.go:115-137` |
| Pod deletion mechanism | Direct `client.Delete()` on the Pod/PVC via the StatefulSet controller path, not the Eviction API | `pkg/splunk/splkcontroller/statefulset.go:180-270` |
| Captain awareness | `updateStatus()` queries `/services/shcluster/captain/info` every reconcile and populates `Status.Captain`, `Status.CaptainReady`, `Status.Initialized`, `Status.MinPeersJoined`, `Status.MaintenanceMode` — **this already works** | `searchheadclusterpodmanager.go:283-341` |
| Captain-transfer-before-delete | **Does not exist.** Nothing in `PrepareRecycle`/`PrepareScaleDown` checks whether ordinal `n` is the current captain, and nothing calls the native `transfer_captaincy` REST endpoint before quarantining/deleting a member | repo-wide grep for `transfer\|StepDown\|demote_captain`, zero hits in `pkg/splunk/enterprise/*.go` or `pkg/splunk/client/*.go` |
| App Framework bundle-push target | Hardcoded to search-head ordinal 0 via `SPLUNK_SEARCH_HEAD_CAPTAIN_URL` | `pkg/splunk/enterprise/util.go` (`getSearchHeadExtraEnv`) |
| PodDisruptionBudget | Not implemented anywhere | repo-wide grep, zero hits for `PodDisruptionBudget` |
| Terminal-failure detection | Exists and is fairly recent — `CheckPodsForTerminalFailures` surfaces stuck-image/config errors as `PhaseError` instead of waiting out the full reconcile timeout | `searchheadclusterpodmanager.go:70-74`, `splkcontroller/statefulset.go` |

**Bottom line:** the Operator today treats every SHC member — captain or not — identically: detain, poll search counts forever, delete. It has no concept of "this member currently holds the RAFT captaincy" during that sequence, even though it already fetches that exact fact every reconcile for status reporting.

## 2. What `docker-splunk` / `splunk-ansible` `develop` does today (verified)

| Area | Current `develop` behavior | Source |
|---|---|---|
| TERM handling | `entrypoint.sh` traps `SIGINT`/`SIGTERM` and runs `splunk stop`; this is a standalone code path with no external coordination marker/lock | `splunk/common-files/entrypoint.sh:33-36` |
| Captaincy fact | `splunk_search_head_captain` is derived fresh on **every** container start by comparing hostname/FQDN against `splunk.search_head_captain_url` (i.e. the Operator's static ordinal-0 env var) | `roles/splunk_search_head/tasks/main.yml:1-12` |
| SHC cluster-forming tasks | `init shcluster-config`, `bootstrap shcluster-captain`, preferred-captaincy POST, and `add shcluster-member` all run unconditionally on every start of `search_head_clustering.yml`, gated only by `splunk_search_head_captain` (a re-derived boolean) — **not** by any check of persisted state or runtime SHC APIs | `roles/splunk_search_head/tasks/search_head_clustering.yml:1-95` |
| Idempotency | Relies entirely on splunkd's own error strings (`"node seems to have already joined another cluster"`, `"is already part of cluster"`) to no-op re-run attempts | same file, `until`/`failed_when` clauses |
| Preferred captaincy | Defaults to `true` unless `SPLUNK_PREFERRED_CAPTAINCY=false` is explicitly set | `inventory/environ.py:139-141` |
| Persisted-state / runtime-API guard before cluster-forming tasks | **Does not exist** — no check of `/services/shcluster/member/info` or `/services/shcluster/captain/info` anywhere in `roles/splunk_search_head/` | repo-wide grep, zero hits |
| `first_run` fact | Exists (`true` when `/opt/splunk/etc/auth/splunk.secret` is absent) and is used to gate other first-boot-only tasks (e.g. `enable_admin_auth`, `install`), but is **not used** to gate any of the SHC cluster-forming tasks in `search_head_clustering.yml` | `roles/splunk_common/tasks/get_facts.yml:49-51` |
| Container state file (`splunk-container.state`) | Written only by `entrypoint.sh`, values are `starting`, `started`, `restarting` — **no `stopping`/`terminating` value is ever written.** During SIGTERM/`teardown()`, the file keeps its last value (`started`) for the entire shutdown, so anything reading it (including the Operator's own liveness-downgrade path) cannot distinguish "healthy and running" from "mid-shutdown" | `splunk/common-files/entrypoint.sh:54,85,108`; consumed by `splunk/common-files/checkstate.sh:32-38` |
| `splunk offline` (graceful cluster detach) | Never called anywhere in `docker-splunk` — the TERM trap only calls `splunk stop`, with no cluster-aware detach step | repo-wide grep for `splunk offline`, zero hits in `splunk/` or `uf/` |
| Double-stop / lock guard | No lock file, PID file, or any serialization primitive around `splunk stop`/`teardown()` anywhere in the shell scripts | repo-wide grep for `flock\|lockfile`, zero hits |

## 3. What `splunkd/develop` provides today that the layers above don't yet use (verified)

| Native capability | Source |
|---|---|
| `/services/shcluster/member/ready` — dedicated readiness endpoint checking SHC-enabled + not-in-any-detention | `SHPoolReadinessHandler.cpp:19`, `member_isInAnyKindDetention()` |
| `transfer_captaincy` REST / `splunk transfer shcluster-captain` — deliberate captain handoff (old captain steps down, target forces an election via `runForElectionNow()`) | `SHPCaptain.cpp:2478, 2956-3031`; `SHPRaftConsensus.cpp:1085-1091` |
| Native searchable-rolling-restart drain timeout (default 180s) — the **only** place native Splunk has a built-in search-drain timeout; ordinary manual detention has none | `SHPRollingRestartHelper` |
| Election timeout defaults: `election_timeout_ms=60000`, heartbeat = timeout/`election_timeout_2_hb_ratio`(12) ≈ 5s | splunkd defaults, confirmed against Confluence 1080087511222 |
| `InstallSnapshot` (leader-driven log catch-up for a far-behind/rejoining peer) | **stubbed out**, `#if 0`, marked "appendSnaphsot-NYI" — `SHPRaftConsensus.cpp:1870-1897` |

## 3b. Kubernetes primitive behavior relevant to the gaps below (verified against kubernetes.io docs, 1.27+ floor)

These aren't gaps in Splunk-side code — they're constraints the fixes in Section 4 must design around, confirmed directly against the official Kubernetes documentation (`concepts/workloads/controllers/statefulset`, `concepts/containers/container-lifecycle-hooks`, `concepts/workloads/pods/pod-lifecycle`, `concepts/workloads/pods/disruptions`, `concepts/overview/working-with-objects/finalizers`):

- **PodDisruptionBudget gives zero protection on the Operator's current delete path.** The docs state plainly: "deleting deployments or pods bypasses Pod Disruption Budgets" — PDB is enforced only through the Eviction API subresource, which `client.Delete()` never calls. This confirms the document's existing "Out of Scope" call on PDB-based sequencing (Section 5) is correct for the current code, but also means adding a PDB later would do nothing unless the Operator's delete path is changed to use Eviction — which it currently doesn't and Section 5 correctly doesn't propose.
- **`RollingUpdate` is effectively one-pod-at-a-time by default.** `maxUnavailable > 1` exists but is gated behind `MaxUnavailableStatefulSet`, which remains **disabled by default** across the entire 1.27+ supported range. G6's `partition`-gated design should assume strictly serial rollout unless that gate is explicitly enabled — do not design around parallel-unavailable members.
- **`PodManagementPolicy: Parallel` removes ordering for termination too, not just startup.** The docs name both "launch or terminate all Pods in parallel." This sharpens G11: `Parallel` isn't just a bootstrap-ordering risk (no guaranteed pod-0-first startup) — it also means a scale-down or coordinated multi-pod deletion has no ordinal ordering guarantee at all today. Note `RollingUpdate`'s own reverse-ordinal update ordering is independent of this setting and would still apply once G6 lands.
- **`preStop` shares the same grace-period budget as the main container's shutdown, with only ~2s of slack.** Per the docs, the kubelet grants a one-off 2-second extension if `preStop` is still running when the grace period expires, then sends SIGKILL. This means G5's coordination primitive and G4's grace-period sizing are not independent — a slow `preStop` eats directly into the time `docker-splunk`'s own TERM handler has left to finish `splunk stop`. G4's 1200s default must be sized with this shared budget in mind, not just against splunkd's own shutdown-duration telemetry.
- **`preStop` does not run on every termination path.** It's skipped on already-crashed/terminated containers, forced deletion (`--grace-period=0`), node loss, and OOM/SIGKILL paths. G5's design must not assume `preStop` is the only place captain-transfer-before-stop logic can live — a purely `preStop`-based fix has no coverage for forced deletion or node loss, which the Confluence "SHC Captain Lifecycle Problem" page's failure/recovery scenario (node hosting the captain fails or is drained) explicitly includes.
- **There is a real Service/EndpointSlice traffic race during termination**, independent of Splunk-side detention: endpoint removal from EndpointSlices is triggered concurrently with `preStop`, not gated on readiness flipping first, and propagation to kube-proxy/ingress/LB is eventually consistent. This means a member can still receive new connections briefly after termination begins, regardless of how correct the SHC-specific readiness/detention logic (G1-G3) is. Worth a `sleep`-based `preStop` delay stage ahead of any Splunk-specific stop logic, to hold the container up long enough for endpoint de-registration to propagate.
- **Finalizers protect the Pod API object, not the running container.** Adding a finalizer keeps the Pod object present (Terminating) until removed, but the kubelet still begins the SIGTERM/grace-period shutdown of the container immediately regardless of the finalizer, and the StatefulSet controller doesn't defer its own state machine for a foreign finalizer. This confirms the existing "Out of Scope" call against a finalizer-to-keep-splunkd-alive design (Section 5) — it would not actually delay the container shutdown clock, only the object's removal from the API.

## 4. Gap List — What Needs to Be Added

Ranked by what blocks safe `RollingUpdate` adoption. Each item states the repo it belongs in.

### G1. Operator: captain-aware quarantine before recycle/scale-down (`splunk-operator`)
`PrepareRecycle`/`PrepareScaleDown` must check `mgr.cr.Status.Captain` against the target member **before** detaining it. If the target is captain, issue `transfer_captaincy` first and wait for `CaptainReady` to flip to a *different* member before proceeding with the existing detain/drain/delete sequence. This is the single highest-value fix — it directly addresses the gap the Confluence "SHC Captain Lifecycle Problem" page and CSPL-4966 both describe, using a native Splunk capability (`transfer_captaincy`) the Operator already has REST access to but never calls.
- Needs a bounded-wait/abort path: if the transfer doesn't produce a new confirmed captain within N seconds, do not proceed with detain/delete (a failed handoff can leave the cluster captain-less for an indeterminate window since transfer is stepdown-then-force-election, not atomic).

### G2. Operator: bounded timeout on the search-drain poll (`splunk-operator`)
`PrepareRecycle`'s `ManualDetention` branch (`searchheadclusterpodmanager.go:183-200`) polls forever with no timeout. This must gain an explicit, Operator-owned timeout (configurable, not reused from any native Splunk value — native Splunk's only comparable timeout is internal to the rolling-restart flow and isn't reachable via the Operator's detention path). On timeout, the member should proceed to termination with an emitted Event/status reason, rather than blocking scale/update forever.

### G3. Operator: correct the readiness probe for SHC (`splunk-operator` + `docker-splunk` probe script)
Add a SHC-aware branch to the readiness probe (`tools/k8_probes/readinessProbe.sh` + `getReadinessProbe`) that calls `/services/shcluster/member/ready` for search-head Pods instead of the bare management root. This directly matches a live customer ask (external corroboration, not just internal analysis) and is prerequisite to any partition-gated rollout, since `RollingUpdate` partition advancement depends on the StatefulSet controller trusting Pod readiness.
- Must account for management-port (8089) contention causing probe timeouts unrelated to actual detention state — tune probe timeout/failure-threshold independently, since this is a known current limitation, not something fixed by switching endpoints alone.

### G4. Operator: default and configurable `terminationGracePeriodSeconds` (`splunk-operator`)
`sok/develop` sets **no** grace period today — every SHC Pod runs under the Kubernetes 30s default. Per the Confluence requirement (PROD-1080330322108, "SOK Migration Requirement: Reliable Pod Termination and Configurable Grace Period"), fleet telemetry shows 67.8% of search-head shutdowns exceed 30s. This needs:
- `spec.terminationGracePeriodSeconds` on `CommonSplunkSpec`, default 1200s when omitted, validated (positive, in-range) before being applied.
- Applied consistently across create/replace/scale-up/scale-down/restart, per TGP-001 through TGP-004.
- A safe migration path so existing 30s-default pods aren't force-killed on the *first* replacement after upgrade (TGP-005).
- **Note:** a local, unpushed branch (`codex/gitlab-termination-grace-period`, 1 commit ahead of `sok/develop`, not on the `sok` remote) already implements the field, default, and a `getTerminationGracePeriodSeconds()` helper. This is draft/local work, not merged or submitted — treat as a starting point to review, not as already-delivered.

### G5. Operator + docker-splunk: `preStop` hook coordinated with the existing TERM trap (`splunk-operator` + `docker-splunk`)
No `preStop` hook exists today, and `docker-splunk`'s TERM handling (`entrypoint.sh:33-36`) is a standalone `splunk stop` (swallowed via `|| true`) with no external state marker, lock file, or PID guard of any kind (confirmed: zero hits for `flock|lockfile` in the shell scripts). If a `preStop` hook independently runs `splunk stop`/`transfer_captaincy` logic and the container then also receives SIGTERM, `teardown()` fires a **second, concurrent `splunk stop`** with nothing at the container layer serializing the two — the only thing preventing an actual failure today is `splunk stop`'s own internal locking inside splunkd, not a container-level guard. This needs a shared coordination primitive (e.g. a state marker file) so `preStop` and the TERM trap agree on "who owns the stop." Two additional constraints on this design, confirmed against Kubernetes docs (Section 3b):
- `preStop` shares the same termination-grace budget as the container's own shutdown (only ~2s of automatic slack past the configured grace period) — so this can't be designed independently of G4's grace-period sizing.
- `preStop` doesn't run on every termination path (skipped on forced deletion, node loss, already-crashed containers) — so captain-transfer-before-stop logic (G1) cannot rely on `preStop` as its only trigger; G1 must remain reachable via the Operator's own reconcile-driven quarantine sequence, independent of whether a `preStop` hook exists.
- Separately, the container's own state file (`splunk-container.state`) never records a `stopping`/`terminating` value — it's stuck reporting whatever it last wrote (`started`) for the entire shutdown, including during any liveness-probe evaluation. A future coordination marker (this same gap) should also close this: write an explicit shutdown-state value before invoking `splunk stop`, so any prober or downstream automation reading this file can distinguish "running normally" from "shutting down."

### G6. Operator: `RollingUpdate` with `partition` gate (`splunk-operator`)
Once G1-G3 exist, move the StatefulSet `UpdateStrategy` from `OnDelete` to `RollingUpdate` with an Operator-managed `partition`, so the Operator — not the StatefulSet controller — still decides pacing, one ordinal at a time, gated on the corrected readiness signal (G3) and captain-aware quarantine (G1). This is what actually gets the Operator out of the business of calling `client.Delete()` directly and into a model Kubernetes primitives can enforce.

### G7. docker-splunk/splunk-ansible: guard SHC cluster-forming tasks on persisted/runtime state (`splunk-ansible`)
`search_head_clustering.yml` runs `init shcluster-config`, `bootstrap shcluster-captain`, preferred-captaincy writes, and `add shcluster-member` unconditionally on every container start, relying only on splunkd's own idempotency error strings (`"node seems to have already joined another cluster"`, `"is already part of cluster"`, `"this instance is the captain"`). Notably, `splunk-ansible` already has a `first_run` fact (`roles/splunk_common/tasks/get_facts.yml:49-51`, true when `/opt/splunk/etc/auth/splunk.secret` is absent) and uses it to gate other first-boot-only tasks — but the SHC cluster-forming tasks in `search_head_clustering.yml` do not use it at all. Per Confluence 1080087511222 (R1-R4), add an explicit guard before these tasks:
1. Check persisted local SHC config in `/opt/splunk/etc` (the existing `first_run` fact is a reasonable first-pass signal here, though not sufficient alone — see below).
2. If splunkd is reachable, query `/services/shcluster/member/info` + `/services/shcluster/captain/info`.
3. If already configured/registered, skip cluster-forming tasks entirely and let splunkd rejoin via its own RAFT/persisted-state logic.
4. Only run bootstrap when the cluster genuinely doesn't exist yet.
A marker file (or `first_run` alone) is explicitly called out as insufficient in the Confluence page — the decision needs persisted config *and* a runtime API check, to correctly handle interrupted first-time bootstrap.
The persistent-state ambiguity path must also account for the Docker-Splunk
entrypoint's fail-fast execution. During a simultaneous persistent cold
restart, runtime member/captain APIs can be temporarily inconclusive on every
Pod. The safe result is to run no cluster-forming command while leaving
splunkd alive for election and Raft recovery; failing the Ansible play can exit
every container and create a restart loop.

### G8. splunk-ansible: stop treating `SPLUNK_SEARCH_HEAD_CAPTAIN_URL` as a durable captain signal (`splunk-ansible`)
`splunk_search_head_captain` is recomputed from the static ordinal-0 env var on every restart (`main.yml:1-12`), meaning a rescheduled pod-0 re-evaluates itself as "the captain" regardless of actual RAFT state. Per R9, this variable should be treated only as a one-time bootstrap seed. Combined with G7's guard, this stops pod-0 from repeatedly attempting preferred-captaincy writes or bootstrap after the cluster already exists.

### G9. Operator: default `SPLUNK_PREFERRED_CAPTAINCY=false` for Kubernetes SHCs (`splunk-operator`)
Today preferred captaincy defaults to `true` (`splunk-ansible/inventory/environ.py:139-141`), and the Operator doesn't override it. Per R5, Kubernetes SHCs should not force captaincy back toward pod-0 by default, since pod-0 has no operational significance in this environment beyond being the bootstrap seed. This is a one-line env-default change on the Operator side.

### G10. Operator: App Framework bundle-push target selection (`splunk-operator`)
`getSearchHeadExtraEnv` hardcodes ordinal 0 as the bundle-push target. Per R4, `apply shcluster-bundle -target` requires *a* live member, not the captain and not specifically ordinal 0. Change target selection to pick any currently-healthy member (the Operator already has per-member status from `updateStatus()` to make this decision) instead of hardcoding ordinal 0.

### G11. Operator: address the `PodManagementPolicy: Parallel` ordering risk on both startup and termination (`splunk-operator`, verification task)
With `Parallel` pod management (`configuration.go:746`), Kubernetes gives no ordering guarantee for **either** launch or termination — the official docs describe `Parallel` as launching or terminating "all Pods in parallel," not just startup. This means: (a) there's no guarantee ordinal 0 starts before ordinals 1..N-1 on first-time cluster creation (the original bootstrap-ordering concern), and (b) a scale-down or coordinated multi-pod deletion today has no ordinal-ordering guarantee at all, independent of anything the Operator's own `PrepareScaleDown`/`PrepareRecycle` sequencing does one-at-a-time via reconcile loops. Combined with G7/G8, first-boot bootstrap races need to be explicitly tested — this is a verification task against `splunk-ansible`'s bootstrap sequencing, not yet a confirmed defect, but currently untested. Note `RollingUpdate`'s reverse-ordinal update ordering (relevant once G6 lands) is independent of this setting and unaffected by it.

G11 qualification must separately cover every first-formation scheduling
permutation and a simultaneous retained-storage restart. The former requires
one stable bootstrap action plus join actions; the latter requires only rejoin
or await-rejoin behavior and no fatal provisioning result.

### G12. Operator + splunkd interplay: no fallback for rejoin when a member's Raft log has fallen too far behind (`splunk-operator`, tracked against `splunkd`)
Since `InstallSnapshot` is stubbed out in current `splunkd`, a member that returns after falling far enough behind (e.g. PVC lost/recreated) cannot be walked back to caught-up automatically. The Operator has no reliable detection or supported automatic repair for this case today. This is bounded by splunkd's own limitation, so the near-term Operator-side requirement is to detect and classify a stuck rejoin, preserve evidence, and block further rollout. Consensus removal and re-addition require a separately authorized, supported recovery workflow; elapsed time alone must never trigger that destructive change.

## 5. Gaps Explicitly Out of Scope for This Document

- Multi-container Pod design, distroless Splunk image, Ansible removal, splunkd-native readiness/liveness listener (MRs !98343/!99190, both unmerged/disabled-by-default) — tracked as future-direction work, not near-term.
- PodDisruptionBudget-based sequencing — confirmed as an intentional non-goal; partition-gated `RollingUpdate` (G6) is the correct sequencing mechanism, not PDBs, and a PDB would have no effect on the Operator's current `client.Delete()` path regardless (Section 3b). An SHC-scoped PDB remains in scope for voluntary disruption through the Eviction API, such as policy-compliant node drain.
- A Pod finalizer to keep splunkd alive past the intended termination point — confirmed as an intentional non-goal; a finalizer delays the API object's removal, not the kubelet's SIGTERM/grace-period shutdown clock, so it would not achieve the intended effect (Section 3b).
- Any change to splunkd's Raft/consensus internals (e.g. implementing `InstallSnapshot`) — G12 above is scoped to Operator-side detection/fallback only, not a splunkd fix.

## 6. Suggested Near-Term Ordering

1. G3 (readiness probe fix) — low risk, immediately correct, unblocks everything else that depends on trustworthy readiness.
2. G1 + G2 (captain-aware quarantine + drain timeout) — the core reliability fix; directly resolves the CSPL-4966-class problem.
3. G4 (grace period default/config) — needed regardless of RollingUpdate; current 30s default is already a live risk on `OnDelete` deletes, not just future RollingUpdate.
4. G7 + G8 + G9 + G10 (splunk-ansible/Operator captaincy-lifecycle guards) — independent of G1-G4, can proceed in parallel; removes the restart-noise/bundle-push-fragility risk.
5. G5 (preStop/TERM coordination, including the confirmed double-stop race and missing shutdown state value) + G6 (RollingUpdate + partition, assume strictly serial rollout since `maxUnavailable > 1` is disabled by default across the supported range) — last, since these depend on G1-G4 being solid first.
6. G11, G12 — verification/detection tasks, not blocking, but should be resolved before declaring the above complete.
7. Independent of the above ranking: a `preStop` sleep/delay stage to cover the confirmed Service/EndpointSlice de-registration race (Section 3b) should be added alongside G5/G6 — it addresses a plain Kubernetes traffic-routing race, not an SHC-specific correctness issue, so it doesn't block G1-G4 but must land before G6 is considered complete.

## 7. Sources

- `~/Projects/splunk-operator`, branch `vivek/operator-multicontainer-20260301`; facts verified against `sok/develop` @ `39316c19f` directly via `git show sok/develop:<path>`.
- `~/Projects/splunk-ansible`, `develop` @ `ea97abb`.
- `~/Projects/docker-splunk`, `master` @ `2b08932` (mirrors `develop`).
- `~/Projects/splunkd`, `develop` (captain-transfer, election-timeout, and InstallSnapshot facts from prior-session source review).
- Confluence PROD-1080087511222, "SHC Captain Lifecycle Problem: Analysis and Requirements."
- Confluence PROD-1080330322108, "SOK Migration Requirement: Reliable Pod Termination and Configurable Grace Period."
- Local branch `codex/gitlab-termination-grace-period` (1 commit ahead of `sok/develop`, unpushed) — reviewed for G4 only, not treated as delivered.
- Official Kubernetes documentation (kubernetes.io / kubernetes/website source, 1.27+ floor): `concepts/workloads/controllers/statefulset` (update strategies, partitions, maxUnavailable, pod management policies), `concepts/containers/container-lifecycle-hooks` and `concepts/workloads/pods/pod-lifecycle` (preStop guarantees, grace-budget sharing, Service/EndpointSlice termination race), `concepts/workloads/pods/disruptions` and `tasks/run-application/configure-pdb` (PDB vs. direct delete), `concepts/overview/working-with-objects/finalizers` (finalizer scope) — verified directly against raw markdown source for pages that truncate in rendered form.
