# Search Head Runtime Lifecycle Contract

Status: outline for cross-repository design review.

This document will define the versioned boundary among Splunk Operator,
Kubernetes Pod lifecycle, Docker-Splunk/Splunk Ansible, and splunkd. It must
avoid placing cluster-wide orchestration exclusively in `preStop`, because the
hook is not guaranteed on force deletion, crash, OOM, or node loss.

## Required contracts

### Readiness

The current supported Splunk runtime does not expose
`/services/shcluster/member/ready`. The container readiness probe therefore
uses the image state marker and a bounded request to the local splunkd
management root. This proves only that image-owned initialization is complete
and the local management service responds. Captain and non-captain members use
the same local probe.

SHC membership, registration, `Up` status, synchronization, initial-formation
completion, detention, and rollout safety are separate Operator observations.
The Operator-owned Pod readiness gate combines those confirmed SHC facts with
the container result before the Pod enters client Service endpoints. Planned
lifecycle work withdraws that gate before deletion authorization. Captain
availability remains a cluster condition and does not automatically make every
otherwise usable Search Head unready.

A future Splunk-owned local traffic-readiness API could replace part of this
compatibility adapter, but the Operator must not call or document an endpoint
that the current product does not provide.

Qualification of the supported 10.4-to-10.5 upgrade confirmed the current REST
boundary on every post-upgrade member:

- `/services/shcluster/member/info` returned HTTP 200 and the local registered,
  `Up`, and restart-state facts;
- `/services/shcluster/captain/info` returned HTTP 200 and identified the active
  captain from both captain and non-captain members; and
- `/services/shcluster/captain/members` returned HTTP 503 on non-captains and
  HTTP 200 only on the active captain.

Consequently, captain-members is not a universal readiness API and must not be
called from the per-Pod kubelet probe. The supported
`splunk show shcluster-status` command succeeded on all three members in this
campaign, but a CLI command used for qualification is not a low-cost,
per-probe readiness contract.

### Liveness

Define the smallest local process-health signal that justifies kubelet restart.
Cluster membership, captain reachability, election, detention, synchronization,
and active search drain are not by themselves liveness failures.

### Startup

Startup protects image-owned first-start and upgrade work. The default budget
is about 30 minutes. Startup success means the image state marker is running
and the local splunkd management root responds; full SHC rejoin remains a
separate Operator gate.

Startup- and liveness-triggered restarts use a 660-second container termination
grace when the runtime lifecycle contract is enabled. Planned Pod deletion
retains its separate 1200-second grace. The shorter probe-failure budget is
sized for the image's 600-second stop deadline, its default 10-second
TERM-to-KILL interval, and 50 seconds of kubelet margin. It does not change
drain, captain-transfer, Pod-startup, or member-rejoin deadlines.

### Shutdown

The Operator controller does not invoke `splunk stop`; it prepares the member
and eventually causes Pod replacement. Search Head `preStop` checks for the
stable runtime executable `/sbin/splunk-shutdown`. When present, the hook
invokes it with `--source=prestop`. The image's TERM trap invokes the same
operation with `--source=term`; its retained lock and result ensure the local
stop runs once.

If TERM overlaps a still-running preStop, TERM is a follower rather than a
second stop owner. The follower waits through the configured stop deadline
plus the configured TERM-to-KILL interval, returns the owner's exact result,
and keeps PID 1 alive while that result is pending. If the owner disappears
without writing a result, the follower returns `124` at the same bound. This
prevents an apparently successful TERM handler from exiting the container
while the preStop-owned `splunk stop` is still running.

For an older image without that executable, `preStop` atomically writes
`stopping` to `splunk-container.state` and returns. The readiness probe rejects
that state, and Kubernetes then sends TERM through the older image's existing
stop path. This capability check supports either Operator-first or image-first
upgrade order without assuming synchronized rollout.

Neither hook path performs detention, search drain, captain transfer, or
membership removal. Cluster-wide preparation remains durable controller work
performed before replacement authorization. Forced deletion, crash, OOM, and
node loss remain recovery paths because they may skip the hook.

The shared runtime shutdown contract provides:

- explicit `stopping` state written before work begins;
- lock/ownership behavior for concurrent triggers, including a bounded
  follower wait and exact owner-result propagation;
- bounded commands and exit reporting;
- a sanitized owner and result retained for the remaining container lifetime
  and repeated by the TERM path where the container log can capture it;
- relationship to readiness withdrawal and endpoint propagation;
- how remaining grace is preserved for splunkd shutdown;
- behavior if the caller disappears or retries; and
- sanitized stage logging.

The files themselves are container-ephemeral. Product qualification and
supportability still require the timestamped container log to be retained by
the deployment's logging pipeline, or a future durable status/event export,
before the old Pod disappears.

### Bootstrap and rejoin

Startup automation must classify initial cluster formation, joining a new
member, persistent-member rejoin, interrupted initial formation, upgrade, and
supported recovery before it invokes a cluster-forming command. The decision
uses both persisted local SHC configuration and the local
`/services/shcluster/member/info` and `/services/shcluster/captain/info`
responses when those APIs are available. The existing `first_run` fact or a
marker file is not sufficient by itself.

Ordinal zero is a stable bootstrap seed only. It is allowed to run the
one-time bootstrap action for a new cluster, but its hostname is never runtime
proof that it is captain. Actual captaincy is always discovered from Splunk.
Kubernetes Search Heads default the image's preferred-captain automation off,
unless the customer explicitly supplies a supported alternative, so bootstrap
identity does not become a permanent election preference.

The required startup actions are:

- a fresh bootstrap seed initializes its member configuration and bootstraps
  the cluster;
- every other fresh member initializes and joins through the seed;
- an initialized or registered persistent member runs no `init`, `bootstrap`,
  `add`, preferred-captain, or destructive-resync action and lets splunkd
  rejoin from persisted state;
- verifiably interrupted first-time formation resumes bootstrap or join
  without repeating member initialization; and
- persistent configuration with temporarily unavailable or inconclusive
  runtime APIs runs no cluster-forming command and leaves splunkd alive for
  election and Raft recovery.

The last case is essential during a simultaneous cold restart. Docker-Splunk's
entrypoint uses shell fail-fast behavior, so treating temporary API ambiguity
as a fatal Ansible task can exit every container and create a restart loop.
Fail-closed therefore means refusing cluster formation, not killing a
persisted member. Kubernetes readiness remains false until local readiness
recovers, while the Operator's durable rejoin timeout and diagnostics expose a
member that does not recover.

`PodManagementPolicy: Parallel` gives no Pod startup ordering. Qualification
must prove every scheduling permutation produces exactly one stable bootstrap
action and join actions for the remaining members. It must separately prove
that a simultaneous persistent restart produces only rejoin or await-rejoin
actions and leaves splunkd running.

### Captain identity and management targets

The compatibility environment variable currently named
`SPLUNK_SEARCH_HEAD_CAPTAIN_URL` is treated as bootstrap discovery input, not a
durable captain fact. Search Head lifecycle decisions, captain transfer, and
rollout gates use the captain observed from Splunk APIs.

The active captain is discovered before a captain-only cluster-members request
is made. A non-captain HTTP 503 from
`/services/shcluster/captain/members` is a role response, not by itself a local
liveness or readiness failure.

App installation and bundle operations do not require ordinal zero. Both
Operator-owned and image-owned bundle paths select a reachable, bundle-ready
member dynamically and fail without performing the operation when no qualified
member exists. Local management requests use the configured splunkd scheme and
port, bypass ambient HTTP proxy variables, and do not derive their transport
from ingress TLS termination or assume a service mesh exists.

### Version compatibility

Define feature discovery or a compatibility matrix so an Operator never assumes
an image provides a new probe, hook, state marker, or bootstrap contract. State
the safe fallback and upgrade order.

## Approval gate

The Search Head/splunkd and container owners must confirm endpoint and command
semantics from product code and supported documentation. The Operator design
must consume only those confirmed contracts or clearly label an interim
compatibility adapter.

## Revision Note

2026-07-25: Added the verified ordinal-zero coupling, explicit startup-action
classification, simultaneous persistent cold-restart behavior, fail-closed
leave-running rule, deterministic `Parallel` formation proof, preferred
captain policy, and dynamic management-target contract. These requirements
were discovered while tracing Operator environment rendering, Splunk Ansible
startup tasks, the Docker-Splunk fail-fast entrypoint, and deployer bundle
targeting together.
