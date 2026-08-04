# Reliable Splunk Search Head Clusters on Kubernetes -- Product and Architecture Requirements

| **Authors:** Vivek Reddy | **Status:** DRAFT <br>**Last updated:** July 24, 2026 |
|---|---|
| **Contributors:** Splunk Enterprise, Splunk Operator, container platform, Support, and Kubernetes engineering teams | **Reviewers:** TBD |

| **Target release** | TBD -- phased delivery |
|---|---|
| **Epic** | TBD |
| **Document status** | DRAFT |
| **Document owner** | Vivek Reddy |
| **UX Designer** | N/A -- no new graphical interface is required |
| **Engineering owner** | TBD |
| **Technical lead / PM** | TBD |
| **Cross-functional PM** | TBD |
| **Engineering review date** | TBD |
| **Technical writers** | TBD |
| **QA** | TBD |

# Section 1 -- Purpose and Current Product Behavior

## Executive Summary

This document defines how to run a Splunk Enterprise Search Head Cluster (SHC) reliably on Kubernetes while preserving Splunk's existing clustering, search scheduling, captain election, and search-drain behavior.

It is written for readers who understand Splunk Enterprise but might not work with Kubernetes every day. The essential Kubernetes concepts are:

- A **Pod** is the running Search Head instance.
- A **StatefulSet** maintains a fixed set of named Search Head Pods and their persistent storage.
- A **readiness probe** decides whether a Pod should receive normal network traffic. It does not restart the process.
- A **liveness probe** decides whether Kubernetes should restart a container because it is no longer functioning.
- A **RollingUpdate** replaces StatefulSet Pods in a controlled order when their image or Pod configuration changes.
- A **preStop hook** runs inside a container after termination has started but before Kubernetes sends the normal process termination signal.
- A **termination grace period** is the total time available for the preStop hook and normal process shutdown before Kubernetes forcibly kills remaining processes.

Today, lifecycle ownership is split across four layers:

1. Splunk Enterprise owns SHC membership, captain election, scheduled-search coordination, configuration and artifact replication, detention, and runtime status.
2. Splunk Operator for Kubernetes owns the desired topology and currently performs Splunk-aware Pod recycling.
3. Docker-Splunk and Splunk Ansible configure Splunk when a container starts and stop Splunk when the container receives a termination signal.
4. Kubernetes owns Pod scheduling, storage attachment, network endpoints, container termination, and StatefulSet replacement.

Each layer is individually reasonable, but there is no single end-to-end lifecycle contract. Kubernetes can see a running process while Splunk considers the member detained, unregistered, or still rejoining. Startup automation can repeat cluster-forming operations during an ordinary restart. A Pod can be deleted before the Search Head has stopped receiving new work or completed existing searches. A captain can be restarted through an unplanned election path even when Splunk provides a supported captain-transfer operation.

The current Splunk Operator design does **not** define a `preStop` hook on Splunk workload Pods. It generates StatefulSets with the `OnDelete` update strategy, and the Operator prepares and explicitly deletes Pods. Docker-Splunk separately handles the container termination signal and invokes `splunk stop`.

The immediate requirement is a compatibility architecture for the current implementation of Splunk Enterprise:

- Kubernetes performs Pod replacement through a gated StatefulSet `RollingUpdate`.
- The Operator decides when one replacement is safe and never authorizes more than one planned SHC disruption at a time.
- A bounded, idempotent `preStop` hook is introduced as part of the same lifecycle change; it is not part of the current Operator design.
- Every Search Head Pod, including the current captain, uses the existing container-state and local-management readiness check together with an Operator-owned SHC serving gate to decide whether it should receive new traffic; the design does not invent an unsupported member-readiness endpoint.
- Manual detention, active-search draining, captain transfer, graceful Splunk shutdown, persistent identity, and verified SHC rejoin remain part of the lifecycle.
- The lifecycle is observable and recoverable after an Operator restart.

This architecture should not be treated as the final shape of Splunk on Kubernetes. The final section identifies changes in Splunk Enterprise and its container image contract that could make the product natively lifecycle-aware, reduce the need for Operator-side orchestration, and support a future distroless image without Docker-Splunk and general-purpose Ansible provisioning.

## Scope of the Conclusions

The requirements in this document are based on the current behavior observed in the reviewed Splunk Enterprise, Splunk Operator, Docker-Splunk, and Splunk Ansible sources and the current official Splunk documentation.

They are not claims that Splunk must retain these behaviors permanently. In particular:

- The current local container readiness check has intentionally narrow behavior.
- Ordinary Splunk shutdown does not perform the complete searchable drain workflow.
- Cluster formation and rejoin still depend on imperative commands and container startup automation.
- Current SOK uses `OnDelete` and has no Splunk workload `preStop` hook.
- The Operator must compensate for those current product boundaries.

If Splunk Enterprise later provides richer readiness, lifecycle, shutdown, membership, or configuration interfaces, the Operator design should simplify and delegate those responsibilities back to Splunk.

## Relationship to the Per-Pod Rolling Restart Design

The Confluence page *Per-Pod Rolling Restart: Kubernetes-Native Splunk Management* describes a broader, earlier lifecycle direction across Splunk roles. This document shares its central goals:

- replace `OnDelete` and direct Pod deletion with StatefulSet `RollingUpdate`;
- keep long waits out of a controller worker;
- distinguish temporary restart from permanent scale-down;
- give the container a bounded local shutdown path;
- expose a configurable termination budget; and
- use Kubernetes disruption and lifecycle primitives where their guarantees apply.

For Search Head Clusters, this document is the authoritative specialization of that direction. Generic per-Pod pseudocode must not be applied to an SHC where it conflicts with the current Splunk product behavior verified here. In particular:

| Generic design statement | Authoritative SHC requirement |
|---|---|
| Move detention, decommission, and lifecycle safety entirely into `preStop` | The Operator performs recoverable cluster preflight, detention, search drain, and planned captain transfer before authorizing deletion. `preStop` is a bounded last-mile fallback after termination begins. |
| Set `RollingUpdate.partition: 0` and allow the StatefulSet to advance on Pod readiness | The Operator manages the partition one ordinal at a time. Pod readiness controls local traffic; separate SHC rejoin and cluster-safety gates control partition advancement. |
| Use PDB to control StatefulSet rollout concurrency | PDB protects voluntary disruption through the Eviction API. StatefulSet-controlled deletion does not consult PDB. SHC rollout concurrency is controlled by the partition gate and durable Operator state. |
| Use a Pod finalizer as the termination checkpoint or to ensure cleanup before the container exits | A finalizer retains the Pod API object but does not delay kubelet `preStop`, TERM, or forced process termination. It must not be used to claim that splunkd remains available or that unfinished search drain can complete after the grace period. |
| Treat Pod logs and `Terminating` plus finalizer as sufficient rollout state | SHC progression requires durable operation stage, authorization, target, revision, and blocker state that the Operator can reconstruct after restart. |
| On a cleanup failure or timeout, always stop and let the cluster recover | Planned SHC continuation follows an explicit customer policy. The conservative default blocks rather than silently interrupting searches or forcing an unsafe captain transition. |
| Remove a Search Head from SHC membership in `preStop` for a restart | Ordinary restart preserves persistent membership. Consensus removal is reserved for confirmed permanent scale-down or a separately authorized supported recovery. |
| Let captain re-election occur as part of planned Search Head termination | After detention and drain, a planned captain target uses supported captain transfer and verifies the new captain before termination. Unplanned failure continues to rely on Splunk election. |
| Use a role-specific fixed Search Head grace period | The SearchHeadCluster interface has a documented 1200-second compatibility default, customer override, and separate drain, transfer, stop, and rejoin budgets. |

The broader page can continue to describe shared mechanics and non-SHC roles, but its diagrams, examples, and summaries must label these SHC differences. Where the two documents differ for Search Heads, the requirements and acceptance criteria in this document take precedence.

## The Availability Model

“Search Head availability” is not one state. The current product exposes at least three distinct capabilities.

| Capability | Current Splunk meaning | Customer-visible effect |
|---|---|---|
| Local search acceptance | This member can accept a new ad-hoc or delegated search | A load balancer may send users to this Search Head |
| Captain-led cluster service | One captain is available to coordinate scheduled searches and cluster work | Scheduled reports and alerts can be dispatched |
| Safe lifecycle progression | The cluster can tolerate one planned disruption and recover before the next | A restart or upgrade can advance safely |

These capabilities must not be collapsed into one Kubernetes Pod readiness result.

If a cluster temporarily lacks a captain or loses its majority, Splunk documents that reachable members can continue serving ad-hoc searches as independent Search Heads. Scheduled reports and alerts do not run because the scheduler belongs to the captain. Marking every Pod unready merely because the captain is unavailable would remove an ad-hoc capability that Splunk intentionally preserves.

Conversely, a locally reachable management port does not prove that a member should receive searches. A member in detention still answers on its management port while Splunk rejects new searches.

## Verified Current Readiness Behavior

The current supported Splunk runtime does not expose
`/services/shcluster/member/ready`. The image-provided readiness check reads
the container state marker and makes a bounded request to the local splunkd
management root using the configured HTTP or HTTPS scheme. It succeeds only
after image-owned setup reports `running` or `started` and the management
service responds.

That local check does not verify:

- whether the member is in manual or automatic detention;
- whether the member is registered with the captain;
- whether the member status is `Up`;
- whether a captain is present;
- `service_ready_flag`;
- stable captain state;
- configuration synchronization;
- KV Store readiness;
- active search counts;
- restart state; or
- the complete Splunk shutdown state.

Current Splunk code rejects new user-created searches and new scheduled-search
dispatch on a member in detention while allowing existing searches to
continue. Because there is no current local traffic-readiness endpoint that
publishes that decision, the Operator must withdraw its Pod readiness gate
during a planned detention and must confirm current member state separately.

### Required Kubernetes readiness behavior

Every SHC Pod, including the Pod that is currently captain, must use the same local readiness rule:

```text
the container has completed image-owned initialization
AND
the local splunkd management service responds
AND
the Operator readiness gate confirms that the current member is eligible
for client traffic
```

The local management root alone is insufficient. During current-runtime
compatibility, the Operator gate must withhold initial formation, require the
local member to be registered and `Up`, and withdraw a planned target when
the operation selects it, before detention begins. It must not invent or call
an unsupported Splunk endpoint.

The captain uses the same local probe and member gate because:

- the captain is also a Search Head member;
- it can accept ad-hoc traffic;
- detention has the same local search-acceptance meaning on the captain;
- captain health is a cluster property and must not be mixed into local traffic readiness.

### Signals that must remain separate from Pod readiness

The following signals must be Operator conditions, metrics, and lifecycle gates rather than inputs that remove all Pods from normal traffic:

- actual captain identity;
- `initialized_flag`;
- `min_peers_joined_flag`;
- `service_ready_flag`;
- `stable_captain`;
- member registration;
- member status;
- configuration synchronization;
- KV Store status;
- active historical and real-time search counts; and
- rolling restart or rolling upgrade state.

This separation preserves the maximum search capability that Splunk supports while still preventing unsafe lifecycle progression.

## Verified Current Restart and Shutdown Behavior

Splunk's searchable rolling restart establishes the product precedent for safe maintenance:

- It performs health checks before starting.
- It processes members one at a time.
- It places a member in manual detention.
- The detained member stops accepting new searches while existing searches continue.
- It waits for search completion or a bounded timeout.
- It restarts the captain last and uses captain transfer.
- It waits for members to return before advancing.

The current implementation also exposes:

- `/services/shcluster/member/info`, including local status, registration, restart state, and active historical and real-time search counts;
- `/services/shcluster/status`, including captain and member health;
- captain information including `service_ready_flag`;
- stable-captain and configuration synchronization information in advanced status;
- manual detention controls; and
- a supported captain-transfer operation.

Ordinary Splunk process shutdown is not equivalent to searchable rolling restart. The ordinary SHC shutdown path changes the member to `Stopping` or `Restarting`, informs the captain, and stops SHC heartbeat processing. It does not first place the member in detention and wait for all active searches. Search draining must therefore happen before ordinary shutdown if planned maintenance is expected to preserve searches.

Manual detention persists through a restart. This is useful because a replacement member cannot accidentally receive new work before lifecycle verification, but it also means the workflow must explicitly remove detention after the member has returned.

### Historical and real-time search handling

Official documentation instructs administrators to inspect both:

```text
active_historical_search_count
active_realtime_search_count
```

The current native searchable rolling-restart source calculates both counts, but the observed restart decision is gated by the historical count or timeout. The real-time count is not used by that decision.

The Operator-managed lifecycle must therefore:

- report both counters;
- define whether both counters block shutdown;
- use a bounded customer-visible timeout because real-time searches may be long-lived; and
- record whether termination continued with searches still active.

The default policy should protect both historical and real-time searches during a planned Operator-controlled restart. A customer-approved timeout policy may allow continuation, but the resulting interruption must be explicit and observable.

## Problem Statement

A planned change to an SHC can cause avoidable search interruption or prolonged unavailability when the four lifecycle layers act without a shared state model. Current implementation gaps and risks in the proposed lifecycle must be described separately.

### Verified gaps in the current Operator design

- The Search Head container readiness probe checks the container state and
  splunkd management root. Those local signals do not expose detention. The
  Operator must therefore own the planned readiness-gate withdrawal before it
  authorizes Pod deletion.
- Splunk StatefulSets use `OnDelete`. The Operator detects a revision mismatch, prepares one member, and explicitly deletes that Pod. Kubernetes records the desired revision but does not own progression of the complete rollout.
- The current recycle path detains the selected member and waits for both historical and real-time active-search counts to reach zero before deletion. This Operator-owned polling path has no bounded timeout, so a long-running or real-time search can block the operation indefinitely.
- The current recycle path requires a service-ready captain before it starts or continues preparation. However, it does not detect that the selected target is the current captain and does not request the supported captain-transfer operation before deleting it. It waits for Splunk election and captain readiness after the replacement begins.
- Permanent scale-down is already distinct from ordinary recycle: both use detention and drain, while scale-down additionally removes the member from the SHC. The new architecture must preserve this existing distinction.
- SHC StatefulSets currently use `PodManagementPolicy: Parallel`. Initial creation can therefore start all member Pods without ordinal ordering. The Operator currently serializes its own scale-down by preparing and reducing one highest ordinal per reconciliation, but Kubernetes would not provide ordered scaling if the replica count were reduced by multiple members outside that sequencing.
- The current Splunk workload Pod template has no `preStop` hook.
- Docker-Splunk handles the container termination signal and invokes `splunk stop`, but the generated Splunk Pod does not set an explicit workload termination grace period. Kubernetes therefore applies its default deadline to process shutdown.
- Docker-Splunk's container state file records startup and running states but does not record an explicit stopping or terminating state before `splunk stop`. The image also has no container-level coordination marker around its TERM shutdown path.
- Docker-Splunk runs its provisioning flow when the container starts. Current Splunk Ansible SHC tasks can repeat `init shcluster-config`, captain bootstrap, preferred-captain configuration, and member-add commands, relying on Splunk command results for practical idempotency rather than an explicit bootstrap-versus-rejoin decision.
- Splunk Ansible has a `first_run` fact, but that fact does not gate the SHC cluster-forming task sequence and is not sufficient by itself to distinguish a partially completed bootstrap from a persistent member rejoin.
- Current preferred-captain automation derives a static bootstrap captain from the configured captain URL and can configure that member as preferred while configuring other members as non-preferred. This turns ordinal-zero bootstrap convention into an ongoing captain-election preference even though Splunk's product default makes all members eligible preferred captains.
- Docker-Splunk's entrypoint uses fail-fast shell execution around the Ansible provisioning play. During a simultaneous persistent cold restart, member and captain APIs can be temporarily inconclusive on every Pod. Treating that ambiguity as a fatal Ansible result can exit all containers before splunkd has time to recover its persisted election and consensus state.
- Some workflows retain ordinal-zero coupling. For example, current App Framework SHC bundle targeting constructs the ordinal-zero Search Head address even though runtime captaincy is dynamic and Splunk can proxy supported requests through another reachable member.
- The current Operator does not create a PodDisruptionBudget for the SHC. Its direct Pod deletion path would bypass PDB enforcement in any case, although a future PDB could still protect voluntary disruptions that use the Kubernetes Eviction API, such as a policy-compliant node drain.
- Current Splunk Enterprise source contains an unfinished Raft snapshot-install path. This is a product recovery limitation worth detecting and diagnosing, but it does not establish that the Operator can safely repair every stalled rejoin by automatically removing and re-adding the member.
- Current status exposes useful member and captain fields, but it does not provide the durable per-stage lifecycle state and timings needed to distinguish drain, captain transition, deletion, scheduling, storage, startup, and SHC rejoin delays.

### Risks that must be prevented when introducing `RollingUpdate` and `preStop`

- An unrestricted StatefulSet `RollingUpdate` would advance based on Kubernetes Pod readiness, which does not by itself prove that the member is registered, synchronized, and safe before another member is disrupted.
- A generic readiness probe that includes captain health could remove every otherwise usable Search Head from traffic during captain election. Local traffic readiness and cluster/captain readiness must remain separate.
- Moving all detention, drain, or captain-transfer work into preStop would start that work only after termination has begun and would place it inside the termination grace-period deadline.
- A long or non-idempotent preStop hook could consume the grace period before `splunk stop` completes.
- Kubernetes normally completes `preStop` before sending TERM, so the two shutdown paths are not inherently concurrent. They can still invoke duplicate sequential shutdown work, and lifecycle hooks can be delivered more than once, so one idempotent shutdown coordinator remains necessary.
- Kubernetes endpoint removal and downstream routing propagation are not instantaneous. The Operator should withdraw the target's serving gate, observe Pod and EndpointSlice withdrawal for a configurable propagation interval, and only then request detention. This preparation completes before replacement authorization and is qualified against the supported Service, ingress, service-mesh, and load-balancer paths.
- If RollingUpdate is enabled before the Operator partition gate is active, Kubernetes could replace members without the existing Splunk-aware preparation.
- If restart and scale-down intent are not kept separate, a temporary replacement could incorrectly remove a persistent member from SHC consensus.
- If bootstrap and rejoin remain implicit, a replacement Pod could repeat cluster-forming actions instead of recovering the existing member identity.
- If a stalled rejoin is treated automatically as a missing member, the Operator could perform a destructive consensus change for what is actually a delayed storage attachment, lost identity, network partition, configuration mismatch, or recoverable Raft catch-up problem.
- If lifecycle state is retained only in controller memory or hook logs, an Operator or Pod restart could lose the evidence and authorization state needed to resume safely.

The objective is not to claim zero impact during node loss, force deletion, network partition, or loss of quorum. The objective is to make planned lifecycle operations safe and deterministic, preserve available search capability during failures, and make every blocked or failed stage diagnosable.

## Personas

> **Splunk Enterprise developer**
>
> - Understands SHC membership, captaincy, scheduler behavior, detention, and replication.
> - Needs to understand which product signals Kubernetes uses and why one readiness bit cannot represent the entire cluster.
> - Needs a clear boundary between current compatibility behavior and future Splunk-native lifecycle capabilities.

> **Splunk administrator**
>
> - Manages scheduled searches, apps, SHC configuration, and capacity.
> - Expects planned updates not to stop avoidable searches.
> - Needs explicit policies for drain time and forced continuation.

> **Kubernetes platform administrator**
>
> - Applies image and configuration changes and performs node maintenance.
> - Needs standard StatefulSet behavior, status, events, and safe automation.
> - Might not understand captain election or scheduled-search ownership.

> **Site reliability engineer**
>
> - Monitors many deployments.
> - Needs stable metrics and alerts for captain availability, rollout progress, and member recovery.
> - Needs reconciliation to resume safely after controller or node failure.

> **Support engineer**
>
> - Often investigates after the original Pod has disappeared.
> - Needs retained stage, duration, reason, and sanitized runtime evidence.

## Assumptions and Boundaries

- Production high-availability SHCs have enough healthy members to retain a majority during one planned disruption.
- StatefulSet storage preserves the member's Splunk identity across ordinary Pod replacement.
- Splunk runtime APIs are the authority for captain, member, search, and cluster state.
- A planned rollout can be delayed when safety conditions are not met.
- Kubernetes cannot guarantee preStop execution for all failures.
- A termination grace period is a time budget, not a guarantee that shutdown completes.
- Kubernetes starts the termination grace-period clock before invoking preStop. The hook and the subsequent normal process shutdown consume the same total budget.
- A PodDisruptionBudget constrains voluntary disruption through the Eviction API. It does not authorize SHC rollout progression and does not prevent direct Pod deletion or StatefulSet-controlled replacement.
- `PodManagementPolicy: Parallel` affects StatefulSet scaling behavior. It does not negate explicit one-at-a-time authorization performed by the Operator, but that serialization must be preserved and tested.
- Operator restarts and reconciliation retries are normal.
- Current SHC configuration rules remain applicable, including cases where a one-member-at-a-time restart is not supported for a particular `[shclustering]` change.
- This document does not direct or constrain the Search Head team's ongoing internal service decomposition. It defines the lifecycle contracts that any future architecture should expose to Kubernetes.

# Section 2 -- Target Architecture for the Current Splunk Implementation

## Architectural Principle

Kubernetes should own replacement; Splunk should own Splunk state; the Operator should coordinate the boundary.

This means:

- The StatefulSet controller creates and replaces Pods.
- Splunk Enterprise remains authoritative for detention, searches, captaincy, membership, and synchronization.
- The Operator observes both systems and authorizes one replacement only when Splunk reports that it is safe.
- Container lifecycle logic performs bounded local shutdown work after termination begins.
- Progress is persisted in Kubernetes state rather than held only in controller memory.

## Why Move from `OnDelete` to `RollingUpdate`

The current Operator design uses `OnDelete` and does not add `preStop` to Splunk Pods. With `OnDelete`, changing a StatefulSet template does not replace existing Pods. The Operator must identify and delete every Pod itself. Kubernetes stores the desired template revision but does not own the complete progression to that revision.

With `RollingUpdate`, Kubernetes understands that existing Pods must be replaced to match the new revision. However, unrestricted `RollingUpdate` is not safe for an SHC because a generic StatefulSet waits only for Kubernetes readiness and does not understand:

- captain transfer;
- search detention and drain;
- SHC quorum;
- configuration synchronization;
- KV Store status;
- Splunk upgrade mode; or
- whether the returned member is safe before the next member leaves.

The required architecture therefore uses a partitioned, one-member-at-a-time `RollingUpdate`:

1. The partition initially prevents Kubernetes from replacing any Pod.
2. The Operator chooses the next ordinal and completes Splunk safety preparation.
3. The Operator lowers the partition to authorize exactly that replacement.
4. Kubernetes terminates and recreates the Pod.
5. The Operator verifies Pod startup and Splunk rejoin.
6. Only then does the Operator authorize the next ordinal.

This preserves standard Kubernetes replacement behavior without surrendering Splunk-specific safety.

The new `preStop` hook and the `RollingUpdate` strategy are one coordinated feature. `RollingUpdate` must not be enabled for SHC Pods until the matching readiness, termination grace, shutdown-hook, and Operator gating contracts are present and qualified. The hook does not replace controller preparation; it provides bounded local protection once Kubernetes has begun terminating the authorized Pod.

## Lifecycle Responsibilities

| Layer | Required responsibility for the current architecture |
|---|---|
| Splunk Enterprise | Report local search acceptance, captain and member state, active searches, cluster health, synchronization, and KV status; perform detention and captain transfer; shut down gracefully |
| Splunk Operator | Classify lifecycle intent, validate safety, select one target, initiate detention and transfer, gate the StatefulSet partition, verify rejoin, publish durable status |
| Container lifecycle -- proposed | Start Splunk using persisted identity; execute the new bounded and idempotent preStop fallback; translate TERM into one graceful Splunk shutdown |
| Kubernetes | Schedule Pods, attach storage, maintain Services, execute probes and hooks, apply the grace period, and replace StatefulSet Pods |

## Planned Member Replacement Sequence

### 1. Classify the change

The Operator must first distinguish:

- ordinary restart;
- image upgrade;
- Splunk rolling upgrade;
- deployer or bundle-driven restart;
- shared-secret rotation;
- SHC configuration change;
- scale-up;
- permanent scale-down;
- complete deletion; and
- recovery from an unplanned failure.

These intents do not share one safe procedure. In particular, consensus removal is for permanent scale-down, not ordinary replacement.

### 2. Validate cluster safety

Before authorizing a planned replacement, the Operator must verify:

- no other SHC member is already terminating or authorized for replacement;
- the cluster retains a majority after one member leaves;
- one runtime captain is identified;
- the captain reports `service_ready_flag=true`;
- the captain is stable for workflows that require stable captaincy;
- the target is a known SHC member;
- no incompatible rolling restart, rolling upgrade, bundle push, membership change, or captain transition is active;
- configuration replication and KV Store satisfy the qualified policy; and
- the change type supports one-at-a-time replacement.

The native searchable rolling-restart preflight provides the baseline: dynamic and stable captain, service readiness, acceptable KV state, no conflicting rolling upgrade, and no out-of-sync member.

### 3. Withdraw Kubernetes traffic and stop new work on the target

The Operator first sets the selected target's serving gate to false. It waits
until the Pod is not Ready, no client-Service EndpointSlice lists the Pod as a
ready or unknown endpoint, and that withdrawal remains true for the configured
propagation interval. Observation failure blocks the operation.

Only after that barrier completes does the target enter manual detention. As
soon as detention is active:

- Splunk rejects new ad-hoc searches on that member;
- the captain does not assign new scheduled searches to that member;
- existing searches continue;
- the member continues participating in most SHC operations; and
- the Operator keeps the target's serving gate false throughout detention,
  drain, shutdown, and rejoin.

### 4. Drain existing work

The Operator observes historical and real-time active-search counts until:

- both reach zero; or
- the configured timeout policy is reached.

The default planned-restart policy must stop and report a blocked operation rather than silently interrupt searches. A customer can configure a bounded continuation policy where required.

### 5. Handle the captain

If the target is not the captain, no captain transfer is necessary.

If the target is the current captain:

1. Select a healthy, reachable, registered, `Up`, synchronized, eligible member.
2. Request captain transfer using the supported Splunk operation.
3. Verify that one new captain is authoritative.
4. Verify `service_ready_flag=true`.
5. Wait for stable captaincy when required by the rollout policy.
6. Only then authorize termination of the former captain.

The Operator must discover actual captaincy from Splunk. It must not infer captaincy from ordinal zero, an environment variable, or a previous observation.

For an unplanned captain loss, the Operator must observe Splunk's election rather than trying to replace RAFT behavior. Planned rollout progression remains paused until the resulting captain and majority are healthy.

### 6. Authorize Kubernetes replacement

After preparation completes, the Operator advances the StatefulSet partition for one ordinal. Kubernetes then owns:

- marking the Pod terminating;
- executing preStop;
- sending the termination signal;
- enforcing the grace-period deadline;
- deleting the old Pod;
- creating the replacement;
- attaching storage; and
- running startup and readiness probes.

### 7. Perform bounded local shutdown

There is no `preStop` hook in the current Operator-generated Splunk Pod template. The following behavior is proposed together with gated `RollingUpdate`.

The new preStop hook is a last-mile safeguard, not the primary rollout coordinator. Termination has already begun when the hook runs, so the hook cannot safely perform an unbounded cluster operation.

The hook must:

- be idempotent;
- delegate to one shared runtime shutdown helper;
- write a machine-readable stopping state before local shutdown work;
- invoke exactly one bounded graceful Splunk shutdown across preStop and TERM;
- emit local owner, result, and elapsed-time information; and
- perform no detention, search drain, captain transfer, membership, or other
  distributed-cluster orchestration.

Kubernetes normally waits for preStop to finish before it sends TERM, so preStop and the existing Docker-Splunk TERM trap do not inherently call `splunk stop` concurrently. They can nevertheless cause duplicate sequential stop attempts, preStop can be delivered more than once, and a locally launched background operation could overlap TERM. Both paths must therefore share one idempotent state marker, lock, or shutdown coordinator and converge on one graceful Splunk stop operation.

The normal planned sequence makes the Operator-owned serving condition false
and proves EndpointSlice withdrawal before detention, drain, and Kubernetes
replacement authorization. Propagation waiting belongs to the durable
controller lifecycle, not preStop, and therefore does not consume the local
shutdown portion of the Pod termination budget.

### 8. Rejoin using persistent identity

An existing member restart must not repeat initial SHC formation.

Startup must distinguish:

- a new cluster;
- a new member joining an existing cluster;
- an existing member restarting with its persistent identity;
- an upgrade rejoin;
- a permanent member replacement; and
- disaster recovery.

The existence of `splunk.secret` alone is not a sufficient lifecycle discriminator.

The startup decision must combine:

- persisted local SHC identity and configuration;
- an explicit lifecycle intent supplied by the Operator or image contract;
- the desired cluster identity and expected member identity; and
- supported Splunk runtime observations when splunkd has reached the point where those APIs are available.

Runtime API checks cannot be assumed to be available before splunkd starts. The container startup design must therefore define the ordering explicitly: inspect persisted state first, start the minimum runtime needed for authoritative observation where required, and only then execute any cluster-forming action that remains necessary. Repeated startup must safely converge after an interrupted first bootstrap.

### 9. Verify recovery

Kubernetes Pod readiness answers whether the returned member accepts local search traffic. It is not sufficient to advance the rollout.

Before the next member is authorized, the Operator must verify:

- the Pod is running on the desired StatefulSet revision;
- persistent storage and member identity are correct;
- `is_registered=true`;
- member status is `Up`;
- manual and automatic detention are off;
- the captain's member view contains the expected member;
- configuration synchronization is acceptable;
- KV Store is ready where required;
- the captain is service-ready;
- the cluster still has one authoritative captain; and
- the required stabilization interval has passed.

Manual detention must be removed explicitly after the member has safely rejoined. Because detention persists through restart, forgetting this step leaves the member unable to receive searches.

A member that does not rejoin within its configured recovery window must enter a durable blocked or terminal diagnostic state. The Operator must distinguish at least:

- Pod scheduling or volume attachment delay;
- missing or changed persistent identity;
- splunkd startup failure;
- inability to reach the captain;
- rejected or missing membership;
- configuration or version incompatibility;
- member present but not `Up`;
- configuration or KV synchronization delay; and
- suspected consensus catch-up limitation.

Detection must not automatically remove and re-add the member. Consensus removal is a materially different and potentially destructive operation that requires a confirmed permanent-replacement or recovery intent and a Splunk-supported recovery procedure.

## Readiness, Liveness, and Startup Contract

### Readiness

Readiness controls traffic only:

```text
image initialization complete
AND local splunkd reachable
AND the Operator confirms this member is registered, Up, and eligible
AND any planned detention has withdrawn the member
```

This applies identically to captains and non-captains.

For the current runtime, the Operator explicitly withdraws readiness before
and throughout its own planned detention. A future Splunk local traffic-readiness contract
should also cover automatic detention. Captain instability must not
automatically make every member unready.

### Liveness

Liveness must answer whether the local Splunk process is irrecoverably nonfunctional and should be restarted.

Liveness must not fail only because:

- the member is in detention;
- the member cannot currently reach the captain;
- a captain election is in progress;
- the cluster is out of sync;
- KV Store is degraded at the cluster level; or
- a planned drain is taking longer than expected.

Restarting a live member because of a cluster-level condition can reduce the majority and worsen the incident. Cluster health belongs in conditions and alerts, not in the process-restart decision.

### Startup

Startup must allow Splunk enough time to:

- load persistent configuration;
- start splunkd;
- recover local services; and
- expose the local management service used by the container readiness check.

Startup completion does not prove full SHC recovery. The Operator's rejoin gate provides that stronger guarantee.

## Configuration and Membership Requirements

- Ordinal zero is a bootstrap seed, not a permanent captain identity.
- App and bundle operations must select a reachable healthy member dynamically rather than always targeting ordinal zero.
- A request that Splunk can proxy to the captain should use that supported behavior rather than storing a stale captain address.
- Ordinary restart must preserve consensus membership.
- Permanent scale-down must use an explicit membership-removal workflow after search drain and captain transfer where required.
- Preferred-captain policy must not be implicitly pinned to ordinal zero. Splunk's documented default treats every member as preferred. Kubernetes deployments should preserve an equivalent all-eligible policy unless the customer explicitly supplies a supported alternative.
- When persistent SHC configuration exists but runtime member/captain APIs are temporarily inconclusive, startup automation must execute no cluster-forming command and must leave splunkd alive for election and consensus recovery. Kubernetes readiness and the Operator rejoin timeout must expose a member that does not recover.
- Under `PodManagementPolicy: Parallel`, fresh formation must deterministically assign exactly one stable bootstrap action and join actions to all other members without depending on Pod scheduling order. A simultaneous persistent restart must select only rejoin or await-rejoin behavior.

Here, **await-rejoin** means startup automation has found persistent SHC
configuration but cannot yet prove runtime member or captain state. It performs
no cluster-forming action, leaves splunkd alive, and lets Kubernetes readiness
plus the Operator's bounded rejoin workflow report recovery or timeout.
- Changes to `[shclustering]` settings must be classified. Current Splunk documentation requires approximately simultaneous restart for most such changes and does not permit them to be treated blindly as an ordinary rolling update.
- A deployer-triggered Splunk rolling restart and a Kubernetes StatefulSet rollout must not execute concurrently.
- Upgrade initialization and finalization must occur once per compatible Splunk rolling-upgrade workflow, not once per Pod.

## Termination Grace Requirements

The SearchHeadCluster customer interface must expose a termination grace-period setting for Splunk workload Pods and provide an explicit documented default.

For the current compatibility architecture, the requirement baseline is a 1200-second default when the customer omits the field. The default and allowed range must be qualified against measured fleet shutdown durations, including the captured fleet evidence that 67.8% of observed Search Head shutdowns exceeded Kubernetes' 30-second default. A customer-specified value remains authoritative after validation.

The total grace period must cover:

```text
preStop execution
+ any permitted final drain
+ captain handoff fallback when applicable
+ splunk stop
+ container exit
+ safety margin
```

The grace period must not be confused with:

- search-drain timeout;
- captain-transfer timeout;
- member-rejoin timeout; or
- complete rollout timeout.

Each duration needs a separate condition and metric. A single large grace period without stage visibility only makes failures take longer to diagnose.

## Functional Requirements

| **ID** | **Requirement** | **Priority** | **Reason** |
|---|---|---:|---|
| SHC-R1 | Use the current container-state and local-management check plus an Operator-owned member readiness gate for every SHC Pod, including the captain; never call an unsupported endpoint | Must | Aligns Kubernetes traffic with confirmed current-runtime capabilities while withholding formation, rejoin, and planned-detention targets |
| SHC-R2 | Keep captain and cluster health separate from Pod traffic readiness | Must | Preserves ad-hoc search availability during captain disruption |
| SHC-R3 | Use local process responsiveness only for liveness; do not restart a Pod for detention or captain instability | Must | Prevents a cluster problem from causing additional member loss |
| SHC-R4 | Discover the runtime captain through Splunk APIs | Must | Captaincy is dynamic |
| SHC-R5 | After detention and drain, use supported captain transfer before planned captain termination | Must | Matches Splunk's searchable-restart precedent and avoids an unnecessary election and scheduled-search gap |
| SHC-R6 | Put a planned target in manual detention and observe both active-search counters | Must | Stops new work and protects existing searches |
| SHC-R7 | Keep restart and permanent scale-down workflows separate | Must | Temporary replacement must preserve membership |
| SHC-R8 | Introduce the bounded preStop lifecycle and partition-gated StatefulSet `RollingUpdate` as one qualified change, with one planned disruption at a time | Must | Gives Kubernetes ownership of replacement without losing Splunk safety |
| SHC-R9 | Gate rollout advancement on verified SHC rejoin, not Pod readiness alone | Must | A locally ready member may not yet be safe for another disruption |
| SHC-R10 | Persist lifecycle state in CR status and Kubernetes resources | Must | Allows safe recovery after Operator restart |
| SHC-R11 | Expose customer-configurable termination grace and drain policies | Must | Workload duration varies and Kubernetes otherwise enforces a generic deadline |
| SHC-R12 | Make preStop and the container TERM path idempotent and bounded | Must | Kubernetes can retry hooks and the current container already handles TERM |
| SHC-R13 | Distinguish bootstrap, rejoin, upgrade, scale-down, and recovery at startup | Must | Prevents repeated cluster-forming operations |
| SHC-R14 | Select healthy runtime members dynamically for App Framework and bundle operations | Must | Removes dependency on ordinal-zero availability |
| SHC-R15 | Classify changes that are eligible for one-at-a-time rollout | Must | Not every SHC configuration change supports rolling replacement |
| SHC-R16 | Publish stage-specific conditions, events, logs, metrics, and sanitized diagnostics | Must | Makes blocked and failed operations supportable |
| SHC-R17 | Provide deterministic abort, retry, resume, and forced-continuation behavior | Must | Prevents unsafe manual recovery |
| SHC-R18 | Preserve current supported Splunk version, TLS, service mesh, and Kubernetes platform coverage | Must | Reliability must apply across the supported deployment matrix |
| SHC-R19 | Remove static ordinal-zero preferred-captain pinning and apply one documented policy to all eligible members | Must | Bootstrap identity must not control later elections |
| SHC-R20 | Detect and classify stalled member rejoin without automatically changing consensus membership | Must | Avoids destructive recovery based on an ambiguous symptom |
| SHC-R21 | Qualify `Parallel` initial bootstrap, one-at-a-time scaling, and partition-gated update behavior separately | Must | These Kubernetes paths have different ordering guarantees |
| SHC-R22 | Provide a PodDisruptionBudget for supported voluntary-disruption protection while keeping rollout sequencing in the Operator partition gate | Should | PDB protects Eviction API operations but not StatefulSet replacement or direct deletion |
| SHC-R23 | Qualify endpoint-removal propagation and support a configurable bounded delay only where the traffic path requires it | Should | Prevents stale routing without imposing an arbitrary universal sleep |
| SHC-R24 | On simultaneous persistent restart, suppress cluster-forming commands without terminating splunkd when runtime SHC APIs are temporarily inconclusive | Must | Prevents fail-fast startup automation from creating a cluster-wide container restart loop |

SHC-R24 is qualified by STS-012 in the scenario matrix and by the runtime-image
gate. The evidence must show each member's startup classification, selected
action, absence of cluster-forming commands, and continued splunkd process
availability while the runtime APIs are inconclusive.

## Use Scenarios and Acceptance Criteria

| **Goal** | **Scenario** | **Acceptance criteria** |
|---|---|---|
| Route traffic correctly | Any member, including the captain, enters detention | The Operator-owned SHC serving condition becomes false and Kubernetes removes the Pod from normal Service traffic; other ready members remain available |
| Restart a non-captain | Image or Pod configuration changes | Target drains according to policy, Kubernetes replaces one Pod, persistent identity is retained, member rejoins `Up`, and no second member starts replacement early |
| Restart the captain | The next rollout target is the actual captain | Supported transfer completes, the new captain becomes service-ready, and only then is the old captain replaced |
| Preserve ad-hoc availability | Captain election is in progress | Healthy non-detained members remain traffic-ready; cluster condition reports scheduled-search risk separately |
| Recover from unplanned captain loss | Node or process disappears without preStop | Splunk election is observed when a majority remains; no additional planned Pod is disrupted until the new captain and cluster stabilize |
| Restart ordinal zero | Another member is captain | Ordinal zero rejoins with its prior identity and does not bootstrap the cluster or force captaincy |
| Recover all persistent members | All Search Head Pods cold-start together and captain/member APIs are initially inconclusive | Every member runs only rejoin or await-rejoin behavior, no member runs cluster formation, splunkd remains alive, and the cluster either elects one service-ready captain or reaches a classified bounded rejoin failure |
| Drain long searches | Active searches exceed the normal drain window | Operation follows the configured abort or continuation policy and reports both historical and real-time counts |
| Resume reconciliation | Operator restarts during any rollout stage | State is reconstructed without authorizing a second Pod |
| Handle storage delay | Replacement waits for its persistent volume | Rollout remains blocked at the storage or scheduling stage without affecting another member |
| Handle stalled rejoin | A replacement does not return to registered and `Up` state | The cause is classified and surfaced; rollout remains blocked; consensus membership is not changed without a separately authorized supported recovery |
| Permanently scale down | Replica count decreases | Target drains, transfers captaincy if needed, leaves consensus only for confirmed scale-down, and storage follows explicit retention policy |
| Run a bundle operation | Ordinal zero is unavailable | A healthy reachable member is selected and supported proxying reaches the captain |
| Protect node maintenance | A policy-compliant node drain uses the Eviction API | PDB prevents voluntary disruption beyond the qualified healthy-member budget; it does not replace the Operator rollout gate |
| Diagnose a failure | The rollout stalls or terminates forcibly | Standard diagnostics identify the stage, reason, elapsed time, target, captain, and relevant Kubernetes and Splunk evidence |

# Section 3 -- Operability, Supportability, and Qualification

## Durable Lifecycle State

The Operator must expose a stable current-operation summary:

- operation identifier;
- lifecycle intent;
- desired StatefulSet revision;
- target Pod and ordinal;
- current stage;
- stage and operation start times;
- actual captain;
- active search counts;
- completed ordinals;
- blocked reason;
- retry count;
- last successful SHC observation; and
- whether continuation after a timeout was requested.

Recommended stages are:

```text
ValidatingCluster
DetainingTarget
DrainingSearches
TransferringCaptain
AuthorizingReplacement
WaitingForTermination
WaitingForScheduling
WaitingForStorage
WaitingForContainer
WaitingForMemberRejoin
ValidatingRecovery
Completed
Blocked
Failed
```

The controller must not block a worker for the duration of these stages. Each reconciliation observes state, performs one bounded idempotent action, records the result, and requeues.

## Conditions

| Condition | Meaning |
|---|---|
| `SearchHeadClusterReady` | The cluster satisfies the documented overall service policy |
| `TrafficReadyMembers` | Number of members currently accepting local search traffic |
| `CaptainReady` | Exactly one authoritative service-ready captain is observed |
| `MembersReady` | Expected members are registered and healthy |
| `RolloutInProgress` | A planned rollout is active |
| `RolloutBlocked` | Progress is intentionally paused on a safety prerequisite |
| `MemberDraining` | A target is detained and existing searches are being observed |
| `CaptainTransferInProgress` | Planned captain handoff is active |
| `MemberRejoining` | The replacement is recovering SHC membership |
| `Degraded` | Some service remains available but one or more cluster requirements are not met |
| `TerminalFailure` | Automatic reconciliation cannot safely continue |

`SearchHeadClusterReady` must not be copied directly to every Pod's readiness. The document must define which customer capabilities remain available in each degraded state.

## Kubernetes Events

Events should report transitions such as:

- rollout detected, validated, blocked, resumed, aborted, and completed;
- member detention started;
- search drain started, completed, or timed out;
- captain transfer started, completed, or failed;
- Pod replacement authorized;
- termination started or grace expired;
- replacement created, scheduled, or blocked on storage;
- member rejoin started and completed;
- bootstrap or rejoin path selected; and
- permanent membership removal started or failed.

Events are short-lived, best-effort evidence. Durable state must remain in conditions, logs, metrics, and bounded diagnostic snapshots.

## Structured Logging

Lifecycle logs must include bounded searchable fields:

```text
namespace
searchHeadCluster
statefulSet
pod
ordinal
operationId
intent
desiredRevision
stage
reason
attempt
captain
captainReady
memberStatus
containerLifecycleState
activeHistoricalSearches
activeRealtimeSearches
elapsedSeconds
timeoutSeconds
```

Stage transitions are logged at Info. Repeated polling belongs at Debug or must be sampled. Passwords, tokens, authorization headers, complete Secret objects, and customer search text must not be logged.

## Metrics

Required metric families include:

| Metric purpose | Required measurement |
|---|---|
| Traffic availability | Number of locally traffic-ready members |
| Captain availability | Whether one service-ready captain is observed and duration without one |
| Membership | Expected, registered, and `Up` members |
| Rollout | Current stage, total duration, intent, result, and blocked duration |
| Per-member lifecycle | Detention, drain, captain transfer, termination, scheduling, startup, and rejoin duration |
| Search drain | Historical and real-time active counts and timeout outcomes |
| Elections | Planned transfers and unplanned captain changes |
| Forced termination | Grace-period expirations and continuation with active searches |
| Rejoin classification | Rejoin timeout count and bounded cause category |
| Kubernetes disruption | PDB-allowed and denied evictions, forced deletion observations, and endpoint propagation measurements from qualification |
| Runtime API health | Errors by bounded operation and category |

Operation IDs, arbitrary error text, search IDs, credentials, and customer-provided values must not become metric labels.

## Alerts

At minimum, alert on:

- no service-ready captain beyond a qualified tolerance;
- conflicting captain observations;
- insufficient healthy members for another disruption;
- rollout stage exceeding its expected duration;
- member rejoin failure;
- persistent identity mismatch or suspected consensus catch-up failure;
- search-drain timeout;
- captain-transfer failure;
- repeated unplanned captain elections;
- forced termination; and
- sustained Splunk runtime API failures.

Alerts must avoid firing as critical during an expected short transition. Each alert must identify the current stage and link to a stage-specific runbook.

## Diagnostic Bundle

The support bundle must retain:

- SearchHeadCluster spec, status, and conditions;
- StatefulSet strategy, partition, revisions, and status;
- current and recently terminated Pod descriptions;
- relevant Kubernetes events;
- Operator logs correlated by operation;
- sanitized readiness, preStop, shutdown, and startup logs;
- durable shutdown owner, result, and timestamps that survive deletion of the
  old Pod; a live `kubectl logs -f` stream is not sufficient evidence;
- actual captain and member summaries;
- active historical and real-time search counts;
- configuration synchronization and KV status;
- recent captain transitions;
- lifecycle stage timestamps;
- Pod disruption budget status;
- scheduling, eviction, volume attachment, and mount evidence;
- persistent-volume identity without credentials;
- selected bootstrap or rejoin path;
- selected App Framework target where relevant;
- image and product versions; and
- configured grace, drain, transfer, and rejoin timeouts.

The bundle must redact credentials, tokens, private keys, authorization headers, Secret data, and customer search text unless explicitly approved for a support investigation.

## Qualification Matrix

Release qualification must include:

- readiness behavior for a normal member and the captain;
- manual and automatic detention;
- initial SHC bootstrap with `PodManagementPolicy: Parallel`;
- non-captain and captain restart;
- ordinal-zero restart while another member is captain;
- complete image rollout;
- supported and unsupported SHC configuration changes;
- Splunk rolling upgrade;
- deployer-triggered restart coordination;
- App Framework operation while ordinal zero is unavailable;
- Operator restart at every lifecycle stage;
- node drain and Eviction API behavior;
- PDB protection during voluntary disruption and its non-interference with the partition-gated rollout;
- EndpointSlice, ingress, service-mesh, and external load-balancer propagation during planned detention and termination;
- direct deletion, force deletion, and node loss;
- storage attachment delay and failure;
- unschedulable replacement;
- splunkd startup failure;
- member rejoin failure with retained identity, missing identity, delayed storage, rejected membership, and suspected consensus catch-up limitation;
- captain API failure and network partition;
- historical and real-time drain timeout;
- termination-grace expiration;
- scale-up, scale-down, and complete deletion;
- TLS, private registry, service mesh, and air-gapped deployments; and
- supported single-container and multi-container Pod shutdown.

Every test must verify separately:

- local ad-hoc traffic availability;
- scheduled-search coordination;
- existing-search handling;
- cluster majority;
- authoritative captain recovery;
- persistent member identity;
- absence of concurrent planned disruption;
- StatefulSet partition and revision;
- status, events, logs, metrics, and alerts;
- Operator restart recovery; and
- diagnostic redaction.

## Success Metrics

| **Success metric** | **Target** |
|---|---|
| Planned captain replacements using verified captain transfer before shutdown | 100% |
| Planned members simultaneously unavailable because of Operator rollout | No more than one |
| Detained members removed from normal Service traffic | 100% |
| Persistent member restarts that skip cluster bootstrap and duplicate membership operations | 100% |
| Interrupted Operator rollouts that resume without a second concurrent disruption | 100% |
| Bundle operations that succeed while ordinal zero is unavailable and another healthy member exists | 100% |
| Failed operations with stage, reason, elapsed time, and correlated evidence | 100% |
| Planned rollout tests preserving ad-hoc availability on healthy members | 100% |
| Forced termination rate | Establish baseline, then define release target |
| Support time to identify the failing lifecycle stage | Establish baseline, then define release target |

## Delivery Phases

### Phase 1 -- Correct health contracts

- Keep the supported local container-state and splunkd-management probe, and
  add the Operator-owned SHC member readiness gate required by the current
  runtime.
- Keep captain and cluster health as separate conditions.
- Define a conservative local liveness contract.
- Add customer-configurable termination grace with the 1200-second compatibility default and qualification evidence.
- Add stage and reason-code contracts.

### Phase 2 -- Startup and identity correctness

- Distinguish bootstrap, member join, persistent rejoin, upgrade, and recovery.
- Treat ordinal zero as a seed rather than runtime captain.
- Dynamically select healthy members for App Framework and bundle operations.
- Remove implicit ordinal-zero preferred-captain coupling.
- Gate cluster-forming commands on explicit intent and persisted/runtime state using a startup order that does not assume splunkd APIs are already available.
- Keep splunkd alive and run no cluster-forming command when a persistent cold restart has not yet produced conclusive runtime SHC APIs.
- Qualify every three-member `Parallel` first-start ordering and a simultaneous persistent cold restart separately.

### Phase 3 -- Planned lifecycle correctness

- Make detention and active-search draining recoverable.
- Add planned captain transfer and verification.
- Implement bounded idempotent preStop behavior as a prerequisite delivered with the RollingUpdate capability.
- Unify preStop and TERM shutdown.
- Record an explicit container stopping state and converge all stop triggers on one shutdown operation.
- Separate ordinary restart from permanent scale-down.

### Phase 4 -- Gated StatefulSet `RollingUpdate`

- Enable the migration-safe RollingUpdate strategy only with the qualified readiness, preStop, termination-grace, and controller-gating behavior.
- Use partition control to authorize one Pod at a time.
- Gate advancement on Splunk rejoin and cluster recovery.
- Add deterministic abort, continuation, and resume behavior.
- Add stalled-rejoin classification without automatic consensus removal.

### Phase 5 -- Qualification and support readiness

- Complete failure-injection and upgrade testing.
- Qualify `Parallel` bootstrap, PDB-protected eviction, and endpoint propagation behavior.
- Publish dashboards, alerts, runbooks, and diagnostic collection.
- Establish measured duration and alert thresholds.
- Train Support, field teams, and Splunk developers on the separated readiness model.

## Customer Impact

Customers should see safer automated updates and clearer lifecycle status. A rollout can take longer because it waits for actual Splunk safety rather than only for a running container. A blocked rollout is an intentional protection when another disruption would risk search service or cluster majority.

The expected customer-facing changes are:

- correct Service traffic removal during detention;
- customer-configurable termination budget and drain policy;
- migration from `OnDelete` to gated `RollingUpdate`;
- explicit rollout stages and blockers;
- improved metrics, alerts, and diagnostics;
- revised startup behavior for persistent members; and
- validation that blocks unsupported or unsafe rollout combinations.

No new license or SKU is expected.

# Section 4 -- What Kubernetes-Native Splunk Should Mean

## Current Architecture Is a Compatibility Layer

The preceding architecture is the correct way to run the current Splunk implementation safely. It necessarily requires the Operator to understand several Splunk internals and combine multiple runtime APIs into a lifecycle decision.

A more Kubernetes-native Splunk would reduce that coupling. Kubernetes-native does not mean replacing SHC consensus with Kubernetes leader election or asking Kubernetes to understand search scheduling. It means that Splunk exposes declarative, idempotent, local and cluster lifecycle contracts that standard orchestrators can call without replaying CLI workflows or reverse-engineering internal state.

The desired long-term boundary is:

- Splunk decides Splunk readiness and shutdown safety.
- Kubernetes decides placement, replacement, and process deadlines.
- The Operator declares intent and observes progress instead of reconstructing Splunk's lifecycle algorithm.

## Product-Level Readiness Contract

Splunk should eventually expose separate, stable health contracts rather than requiring an orchestrator to combine unrelated fields.

Recommended semantic contracts are:

1. **Local traffic readiness**

   Whether this member can accept a new user or delegated search. The current
   product does not expose a dedicated endpoint for this decision. A future
   versioned API should publish a stable reason and machine-readable state,
   including detention.

2. **Local process liveness**

   Whether the local Splunk service is functioning or irrecoverably hung. This must exclude normal maintenance states such as detention, rejoin, captain election, and temporary loss of cluster service.

3. **Member operational readiness**

   Whether this persistent member has completed local startup, is registered, has an acceptable configuration baseline, and is ready to count as recovered for a rolling operation.

4. **Cluster service readiness**

   Whether the SHC has one authoritative service-ready captain, sufficient members, acceptable synchronization, and required KV capability.

5. **Maintenance readiness**

   Whether the cluster can safely remove one specified member for a declared restart, upgrade, or scale-down intent.

Each contract should return a versioned state, stable reason code, observed generation or epoch where relevant, and only the data needed for that decision.

## Product-Level Shutdown Contract

Splunk should provide one idempotent graceful-shutdown operation that understands lifecycle intent.

For a planned restart, the operation should be able to:

- reject new work locally;
- make local traffic readiness false;
- report active historical and real-time searches;
- wait according to a supplied deadline or return a drain token;
- coordinate captain transfer when this member is captain;
- distinguish restart from permanent membership removal;
- report progress and the remaining blocking reason;
- checkpoint the necessary local state;
- stop accepting new control work; and
- complete process shutdown within the caller's deadline.

The API must not require a long synchronous HTTP connection. A request should create or reconcile a lifecycle operation that can be polled and safely retried.

If Splunk owns this state machine, preStop can request shutdown and wait for completion instead of reimplementing detention, search polling, captain transfer, and stop sequencing in shell or Operator code.

## Declarative Bootstrap and Membership

The current container flow relies on imperative Splunk commands to initialize members, bootstrap a captain, add peers, resynchronize configuration, and change runtime settings.

A Kubernetes-native product contract should make these operations declarative and idempotent:

- A member starts with a persistent identity and a desired cluster identity.
- A new cluster is distinguished explicitly from rejoining an existing cluster.
- A seed address is discovery input, not a permanent captain declaration.
- Repeating the same desired membership request has no harmful side effect.
- Temporary restart never requires consensus removal and re-addition.
- Permanent scale-down is an explicit, separately authorized operation.
- Membership and rejoin progress are available through a stable API.
- A member that cannot catch up reports a stable recovery classification and a supported remediation contract instead of requiring the orchestrator to infer a Raft failure from elapsed time.
- Bootstrap credentials can be delivered through files or short-lived identity mechanisms without appearing in command arguments.

This removes the need for the Operator or startup automation to decide whether to call `init`, `bootstrap`, `add`, or `resync` based on incidental files.

## Declarative Configuration Reconciliation

The future runtime should accept a desired configuration revision and report:

- whether the revision is valid;
- whether it can be applied online;
- whether it requires a member restart, rolling cluster restart, or approximately simultaneous cluster restart;
- whether it conflicts with another maintenance operation;
- which members have applied it; and
- whether rollback is supported.

This would replace imperative per-start configuration commands with a versioned reconciliation contract.

The Operator should not need to run general Splunk CLI commands to add peers or repeatedly configure a running cluster. It should submit desired state and wait for Splunk to converge or return a stable reason that it cannot.

## Distroless Splunk Image Direction

Moving away from Docker-Splunk and general-purpose Ansible provisioning toward a distroless Splunk image requires a new image contract, not merely removing packages.

The future image should:

- contain the Splunk runtime and a minimal supported lifecycle entrypoint;
- start without requiring a full configuration-management run;
- read immutable desired configuration and mounted secrets;
- preserve mutable member identity and runtime data only in documented persistent locations;
- expose native startup, readiness, liveness, maintenance, and shutdown endpoints;
- avoid depending on an interactive shell, package manager, Python environment, or broad init system;
- handle signals directly and exit with meaningful status;
- provide structured startup and shutdown logs;
- support debug collection through an ephemeral debug container or a separately supplied diagnostic image; and
- publish a supported compatibility contract between the image, Splunk version, and Operator.

The removal of shell and Ansible increases the importance of first-class Splunk APIs. A distroless image cannot depend on complex shell hooks as the primary lifecycle mechanism.

## Operator Role in the Future Architecture

As Splunk gains native lifecycle contracts, the Operator should become simpler:

- declare the desired SHC size, image, configuration revision, and lifecycle intent;
- create Kubernetes resources;
- request a Splunk maintenance operation;
- expose Splunk's operation status as Kubernetes conditions;
- advance StatefulSet RollingUpdate only when Splunk reports maintenance readiness;
- enforce Kubernetes policy such as one planned disruption at a time; and
- surface Kubernetes scheduling, storage, and termination failures.

The Operator should no longer need to:

- infer first run from files;
- call a sequence of imperative cluster-forming commands;
- reconstruct captain-transfer eligibility from many endpoints;
- implement search-drain loops independently;
- duplicate Splunk shutdown behavior in shell;
- treat ordinal zero as a special runtime authority; or
- encode undocumented SHC state transitions.

## Relationship to Search Head Service Decomposition

The Search Head team is already considering internal service separation. This document does not prescribe how those services should be divided, deployed, or owned.

The lifecycle requirement is independent of that internal design:

- each externally schedulable unit needs a clear local readiness and liveness contract;
- the cluster needs one authoritative service-readiness and maintenance-readiness contract;
- shutdown must identify which work is local, which work is cluster-coordinated, and which state must persist;
- dependencies between services must be visible through machine-readable status; and
- the orchestrator must not need to know private service implementation details.

Whether the future Search Head remains one process, becomes several processes in one Pod, or becomes multiple independently scheduled services, these contracts allow Kubernetes and the Operator to manage it safely without designing the Search Head architecture on the team's behalf.

## Long-Term Exit Criteria

The compatibility orchestration described in Sections 2 and 3 can be retired when Splunk provides and qualifies:

- native local traffic-readiness and process-liveness endpoints;
- member, cluster, and maintenance-readiness contracts;
- an idempotent deadline-aware graceful-shutdown API;
- declarative bootstrap, membership, rejoin, and permanent removal;
- declarative versioned configuration with restart-impact classification;
- persistent operation state that survives process restart;
- stable metrics and reason codes for lifecycle operations; and
- a minimal container image contract that does not require Ansible or imperative startup configuration.

Until those capabilities exist across the supported Splunk versions, the Operator must continue to implement the current compatibility architecture rather than assuming that generic Kubernetes readiness and RollingUpdate alone are sufficient.

# Section 5 -- Planning Details

## Deployment Matrix

The requirements apply to:

- upstream Kubernetes versions supported by SOK;
- Amazon EKS;
- Azure Kubernetes Service;
- Google Kubernetes Engine;
- supported OpenShift versions;
- private-registry and air-gapped environments;
- supported service-mesh and TLS configurations;
- single-member Search Heads, without an HA guarantee; and
- three-member and larger SHCs with the full lifecycle behavior.

## Security Considerations

- Runtime control operations require appropriately scoped Splunk authorization.
- Kubernetes service accounts receive only permissions required for lifecycle reconciliation.
- Credentials must not appear in process arguments, logs, events, metrics, or support bundles.
- TLS verification must follow supported certificate configuration.
- New lifecycle endpoints and mounted-secret mechanisms require Product Security review.
- Diagnostic data must have bounded retention and explicit redaction.

## Enablement

Required material includes:

- customer migration guidance from `OnDelete`;
- readiness, liveness, and cluster-condition explanations;
- planned versus unplanned disruption behavior;
- captain transfer and search-drain runbooks;
- termination-grace and timeout guidance;
- per-alert support runbooks;
- diagnostic collection and redaction guidance;
- field guidance explaining safe rollout blocking; and
- developer documentation for the current compatibility boundary and future native contracts.

## Open Questions

| **Question** | **Current position** |
|---|---|
| Is the 1200-second compatibility default sufficient across the supported deployment matrix? | Treat 1200 seconds as the requirement baseline; validate the allowed range and safety margin against stage-level fleet and qualification measurements |
| What is the default active-search timeout policy? | Begin with the documented searchable-restart time as a reference, but qualify historical and real-time behavior separately |
| Which configuration changes are eligible for one-at-a-time RollingUpdate? | Requires a versioned allowlist based on Splunk configuration semantics |
| How should one-time forced continuation be approved? | Use an audited operation-scoped override rather than an unrecorded manual Pod deletion |
| Where should bounded lifecycle snapshots be stored? | Current and most recent summaries can remain in CR status; larger evidence requires a bounded diagnostic object or logging platform |
| What storage policy applies after permanent scale-down? | Preserve current compatibility until an explicit retention policy is approved |
| Which stalled-rejoin conditions have a Splunk-supported automatic recovery? | Detection and blocking are required now; do not remove and re-add membership automatically until each recovery class has an approved product procedure |
| What endpoint-withdrawal propagation interval applies to each supported network path? | Determine through qualification; the durable Operator barrier observes Pod and EndpointSlice withdrawal before detention, while persistent connections remain a separate Splunk/client contract |
| What PDB budget applies to each supported SHC size? | Protect Eviction API disruptions without presenting PDB as the StatefulSet rollout sequencer |
| Which future Splunk release introduces native lifecycle contracts? | TBD with Splunk Enterprise roadmap |
| What is the migration boundary from Docker-Splunk/Ansible to a distroless image? | Requires a supported image, configuration, identity, lifecycle, and diagnostics contract before removal |

## Out of Scope

- Replacing Splunk's RAFT election with Kubernetes leader election.
- Guaranteeing zero disruption during loss of majority, force deletion, or simultaneous infrastructure failure.
- Using Pod readiness as the complete SHC health definition.
- Using a Pod finalizer to keep splunkd alive during termination.
- Using a Pod disruption budget as the StatefulSet rollout sequencer. This does not exclude using a PDB for voluntary-disruption protection through the Eviction API.
- Removing a member from consensus during ordinary restart.
- Automatically removing and re-adding a stalled member without a separately authorized, Splunk-supported recovery procedure.
- Allowing concurrent planned SHC member replacement by default.
- Prescribing the Search Head team's internal service decomposition.
- Redesigning the search scheduler in this requirement.

## References

- [Restart the search head cluster](https://help.splunk.com/en/splunk-enterprise/administer/distributed-search/10.4/manage-search-head-clustering/restart-the-search-head-cluster)
- [Perform a rolling upgrade of a search head cluster](https://help.splunk.com/en/splunk-enterprise/administer/distributed-search/10.4/deploy-search-head-clustering/perform-a-rolling-upgrade-of-a-search-head-cluster)
- [Control captaincy](https://help.splunk.com/en/splunk-enterprise/administer/distributed-search/10.4/manage-search-head-clustering/control-captaincy)
- [Search head cluster endpoint descriptions](https://help.splunk.com/en/splunk-enterprise/rest-api-reference/10.4/cluster-endpoints/cluster-endpoint-descriptions)
- [Kubernetes container lifecycle hooks](https://kubernetes.io/docs/concepts/containers/container-lifecycle-hooks/)
- [Kubernetes StatefulSets](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/)
- [Kubernetes disruptions and PodDisruptionBudgets](https://kubernetes.io/docs/concepts/workloads/pods/disruptions/)
- [Per-Pod Rolling Restart: Kubernetes-Native Splunk Management](https://splunk.atlassian.net/wiki/spaces/~617fa0955800630069d1c00f/pages/1079605003054/Per-Pod+Rolling+Restart+Kubernetes-Native+Splunk+Management)
- [SHC Captain Lifecycle Problem: Analysis and Requirements](https://splunk.atlassian.net/wiki/spaces/PROD/pages/1080087511222/SHC+Captain+Lifecycle+Problem+Analysis+and+Requirements)
- [SOK Migration Requirement: Reliable Pod Termination and Configurable Grace Period](https://splunk.atlassian.net/wiki/spaces/PROD/pages/1080330322108/)
- [SOK Migration Requirements: VM/Bare-Metal to Kubernetes](https://splunk.atlassian.net/wiki/spaces/PROD/pages/1080312214728/SOK+Migration+Requirements+VM+Bare-Metal+to+Kubernetes)
