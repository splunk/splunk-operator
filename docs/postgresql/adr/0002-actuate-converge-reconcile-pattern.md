# ADR-0002: Actuate/Converge reconcile pattern (component pipeline)

- **Status:** Accepted
- **Date:** 2026-07-20
- **Deciders:** Postgres operator team (CPI)
- **Related:** CPI-1961 (initial pattern write-up — this ADR extends it), [ADR-0003](0003-cnpg-integration-and-drift-reconciliation.md), [architecture overview](../architecture-overview.md)

> This ADR **extends** the reconcile-pattern description started in CPI-1961.
> It does not restate the tutorial-level material there; it records the *decision*
> and its current, as-built shape so the code and the doc don't drift.

## Context

Both Postgres controllers manage several downstream Kubernetes objects that have
ordering dependencies (a Secret must exist before the CNPG Cluster can reference
it; the CNPG Cluster must exist before roles or a pooler can attach). A naive
"do everything top to bottom, return on first error" reconcile makes three
things hard:

- **Ordering and prerequisites** become implicit and easy to break when a step
  is inserted in the wrong place.
- **Status** becomes ad-hoc: every step invents its own way to set phase and
  conditions, and it's unclear who is allowed to declare the cluster `Ready`.
- **Testing** a single concern (e.g. pooler logic) drags in the whole reconcile.

The `PostgresCluster` reconciler in particular fans out to a CNPG Cluster, a
superuser Secret, an optional barman ObjectStore, managed roles, an optional
PgBouncer pooler, a scheduled backup, and a connection-info ConfigMap — each of
which can independently be missing, provisioning, applying a change, ready, or
failed.

## Decision

The `PostgresCluster` service (`pkg/postgresql/cluster/core/cluster.go`,
`PostgresClusterService`) reconciles through an **ordered list of components**,
each implementing a small `component` interface with a two-phase
**Reconcile (actuate) → Observe (converge)** contract:

```go
type component interface {
    Reconcile(ctx context.Context) error                       // actuate: make desired state true
    Observe(ctx context.Context, reconcileErr error) (componentHealth, error) // converge: report where we are
    CheckContracts() error                                     // are my upstream inputs present?
    Name() string
    Requires() []contractKey                                   // inputs I consume
    Provides() []contractKey                                   // outputs I publish
}
```

The runner (`runComponents`) walks the list in order. For each component it:

1. calls `CheckContracts()` — if an upstream contract (e.g. the CNPG Cluster or
   Secret) is not yet published this cycle, the component reports `Pending`;
2. calls `Reconcile(ctx)` to actuate desired state;
3. calls `Observe(ctx, reconcileErr)` to classify the result into a
   `componentHealth` — one of `Ready`, `Pending`, `Provisioning`,
   `Configuring`, or `Failed` — carrying the condition, reason, message, phase,
   and a `ctrl.Result` (requeue hint).

Each component persists its own status inside `Observe` (via
`writeComponentStatus`) before returning its `componentHealth`. If a component
observes an **intermediate** state (`Pending`, `Provisioning`, `Configuring`),
the runner **returns early with that component's requeue hint** — later
components don't run until the blocker clears. Only when *every* component
observes `Ready` does the top-level service set the cluster phase to `Ready`.

**Contracts** make the ordering explicit and checkable. Components publish live
objects into a shared `reconcileContracts` struct (`CNPGCluster`, `Secret`) and
declare `Requires()`/`Provides()`. `validateComponentOrder` runs each reconcile
(right after the component slice is rebuilt) and fails loudly — returning a
reconcile error, not silently requeuing — if any component requires a contract
no earlier component provides. A wiring bug is thus a programming error surfaced
immediately, not a silent requeue loop.

**Status ownership rule:** individual components must *not* set the phase to
`Ready`. `newReadyHealth` leaves `Phase` empty on purpose; only the top-level
reconciler declares `Ready`, once all components have converged. Components may
set `Failed`/`Pending`/`Provisioning`/`Configuring` phases because those reflect
a specific component's own blocking state.

The cluster's `Ready` phase is otherwise **projected from CNPG**: the cluster
model maps CNPG's own cluster phase (`Healthy`, `Switchover`, `Upgrade`,
`Unrecoverable`, …) onto our phase/condition vocabulary rather than
self-determining health (see [ADR-0003](0003-cnpg-integration-and-drift-reconciliation.md)
and the state-machine section of the [architecture overview](../architecture-overview.md)).

The `PostgresDatabase` service uses a **different** shape deliberately (see
Consequences): a single linear pipeline that accumulates conditions
(`ClusterReady → SecretsReady → ConfigMapsReady → RolesReady → DatabasesReady →
PrivilegesReady`) with each step persisted before the next.

## Alternatives considered

- **Flat imperative reconcile** (one long function, return on first error).
  Rejected: implicit ordering, ad-hoc status, and untestable in isolation — the
  exact problems above.
- **A single generic "component manager" abstraction shared by both
  controllers.** Explored in the "Component Manager (Actuate/Converge)
  Simplified" design note. The two controllers have genuinely different status
  strategies — the cluster *mirrors* CNPG's phase and can regress a condition as
  CNPG changes; the database *accumulates* conditions monotonically along a
  pipeline. Forcing one abstraction over both hid that difference. Chosen: a
  component pipeline for the cluster, a linear condition pipeline for the
  database, sharing only small helpers (`shared/reconcile`, contract checking).
- **Terminology "Evaluate → Actuate → Converge" (EAC)** from the original
  design note. The as-built code uses **Reconcile/Observe** with contract
  checking and no separate `Evaluate` step; the pre-reconcile validation
  (`GetMergedConfig` + `Validate*`) runs once before the component loop rather
  than per component. This ADR records the current names to end the EAC vs
  Reconcile/Observe drift between the two prior design notes.

## Consequences

- **Positive:** ordering is explicit and validated on every reconcile
  (`validateComponentOrder`); each component is
  unit-testable in isolation against a fake client; status is uniform
  (`componentHealth`) and has one owner for `Ready`; adding a concern means
  adding a component to the slice with its `Requires`/`Provides`.
- **Negative / costs:**
  - Two different reconcile shapes to learn (cluster component pipeline vs
    database linear pipeline). This is intentional but is a documentation and
    onboarding cost.
  - Early-return-on-intermediate means a single slow component (e.g. a pooler
    still provisioning) defers later components until the next requeue; this is
    correct but can make a full cluster take several reconcile cycles to reach
    `Ready`.
  - The contract mechanism only guards *object presence*, not content
    correctness; a published-but-wrong object is caught by the consuming
    component's own logic, not by `CheckContracts`.

## References

- Code: `pkg/postgresql/cluster/core/cluster.go` (`PostgresClusterService`,
  `runComponents`, `componentHealth`, health constructors),
  `pkg/postgresql/cluster/core/contracts.go` (`reconcileContracts`,
  `validateComponentOrder`), `pkg/postgresql/database/core/database.go`
  (`PostgresDatabaseService` linear pipeline)
- Design docs: "Postgres Cluster: Actuate-Converge pattern design",
  "Component Manager (Actuate/Converge Pattern) Simplified",
  "Postgres Controller Architecture: Goals, Patterns, and Responsibilities"
- Jira: CPI-1961, CPI-2066
