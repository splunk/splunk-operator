---
title: "ADR-0006: PostgresDatabase execution pipeline"
parent: Architecture Decision Records
grand_parent: PostgreSQL
nav_order: 6
---

# ADR-0006: PostgresDatabase execution pipeline

- **Status:** Proposed
- **Date:** 2026-08-31
- **Deciders:** Postgres operator team (CPI), proposed for CPI-2150 review
- **Related:** CPI-2150, CPI-1962, CPI-1961,
  [ADR-0002](0002-actuate-converge-reconcile-pattern.md)

## Context

`PostgresDatabase` reconciliation is currently implemented as one procedural
service function, `PostgresDatabaseService`, in
`pkg/postgresql/database/core/database.go`. It performs a fixed sequence:

1. finalizer and sticky failure checks
2. `PostgresCluster` existence and readiness checks
3. role credential secret reconciliation
4. connection metadata `ConfigMap` reconciliation
5. managed-role acknowledgement checks
6. CNPG `Database` reconciliation
7. PostgreSQL RW privilege reconciliation
8. custom metrics acknowledgement
9. final Ready status

The sibling `PostgresCluster` controller already has an ordered component
runner and a finite use-case runner. That code is useful precedent for
dependency validation and ordered execution, but the database controller has
different lifecycle semantics:

- the database has no `Configuring` phase equivalent;
- terminal user-actionable failures use `reconcile.TerminalError` and sticky
  reconcile failure status;
- earlier phases can persist status and continue;
- the tail region, from RW privileges through custom metrics and final Ready,
  accumulates in memory and flushes a single status write.

CPI-2150 asks for the minimum package, contract, outcome, and execution model
baseline needed before concrete database phases are extracted. This ADR
therefore avoids production facade cutover and avoids unreachable parallel
implementations of existing phases.

## Decision

### 1. Keep the database runner local

`PostgresDatabase` gets its own pipeline package under
`pkg/postgresql/database/core/pipeline`.

Do not move a shared generic runner into `pkg/postgresql/shared` now. The
cluster and database controllers have enough different lifecycle semantics that
sharing would hide important behavior behind a generic abstraction before a
second identical consumer exists.

The database runner may reuse cluster mechanics as precedent:

- ordered registration;
- `Requires` / `Provides` dependency contracts;
- fail-fast order validation;
- `Reconcile` before `Observe` for mutating steps;
- stopping on non-converged outcomes.

It must not copy cluster-specific lifecycle semantics such as CNPG phase
mirroring or bitmask component health.

### 2. Use one ordered pipeline

Use one ordered list of steps. Finite workflows are adapted into that same
order when they need a precise position between steady-state checks.

This model fits the database tail, where RW privilege bootstrap must occur
after CNPG database convergence but before custom metrics and the final Ready
flush.

The prototype proves that shape with a database-ordered test:

```text
CNPG databases
RW privilege bootstrap
custom metrics
Ready flush
```

A later workflow cannot retroactively block a step that already ran. Any
workflow that can block a component must be ordered before that component.
`ErrPrerequisiteNotReady` is a non-blocking use-case deferral: the pass may
continue through unrelated later steps, but it remains incomplete. If no later
waiting, retryable, terminal, or valid flush outcome stops the pass, the runner
returns `Deferred` with the earliest explicit deferred requeue delay, falling
back to the database retry delay only when an incomplete runtime dependency did
not come from a recorded deferred outcome. A true blocking wait must be
represented by a `Waiting` outcome.

### 3. Mutation is opt-in

A pure observation gate implements `Step` only. A mutating step also implements
`MutatingStep`.

```go
type Step interface {
    Name() string
    Requires() []ContractKey
    Provides() []ContractKey
    Observe(ctx context.Context, contracts *Contracts, mutationErr error) (reconciliationTypes.Outcome, error)
}

type MutatingStep interface {
    Step
    Reconcile(ctx context.Context, contracts *Contracts) error
}
```

The runner calls `Reconcile` only for `MutatingStep`. Pure gates do not expose
a fake mutation method.

### 4. Use explicit dependency contracts

Pipeline contracts are execution facts, not domain repositories. A step
declares what it requires and what it provides:

```go
type ContractKey string

type Contracts struct {
    CNPGDatabasesReady *CNPGDatabasesReadyContract
    RWPrivilegesReady  *RWPrivilegesReadyContract
}
```

`ValidateStepOrder` verifies that every required contract is provided by an
earlier step. A missing provider is a programming error, not a reconcile-time
condition. The runner returns the validation error immediately.

The runner owns a fresh `Contracts` bag for each pass and passes it to mutation
and observation methods. A nil contract field means the producing step has not
successfully published that fact this pass. Before running a step, the runner
checks its runtime `Requires()` against those fields; missing runtime contracts
skip only that dependent step so unrelated later steps can still run. Runtime
skips mark the pass incomplete; the runner must not report clean convergence
while required runtime facts are still absent.

The prototype defines only the first database contract keys needed to prove
the positional workflow rule: CNPG database convergence and RW privilege
bootstrap readiness. A converged non-deferred step that declares `Provides()`
must publish those typed contract fields before downstream steps can consume
them. A use case deferred by `ErrPrerequisiteNotReady` may omit its declared
provided contracts for that pass; dependent steps are skipped by the runtime
requirement check.

No-work provider semantics are explicit. If `Schedule` returns false for a
use case that declares `Provides()`, that means no mutation is needed because
the provided fact is already satisfied. The use case must publish that
satisfied contract before returning false. If the fact is not true yet, the use
case must defer or report a non-converged outcome instead of pretending to
converge.

### 5. Use one database outcome vocabulary

Every step reports a `reconciliationTypes.Outcome` from
`pkg/postgresql/database/core/types/reconciliation`. The outcome vocabulary
must distinguish the current database stop shapes:

- `Converged`: desired state matches observed state;
- `Waiting`: expected wait with fixed `RequeueAfter`;
- `RetryableRequeue`: transient error returned to controller-runtime for
  normal backoff;
- `TerminalError`: user-actionable failure wrapped with
  `reconcile.TerminalError`;
- `SilentStop`: stop without status write, requeue, or error;
- `Deferred`: prerequisite or runtime dependency missing, status-free requeue
  after unrelated later steps have had a chance to run;
- `ConvergedApply`: apply status in memory and continue;
- `ConvergedStatus`: persist status and continue;
- `ConvergedFlush`: persist the authoritative final status and stop.

Status action is explicit on the outcome:

- no status action;
- apply in memory and continue;
- persist and continue;
- persist and stop.

`ConvergedApply` marks status dirty in memory. A later persist action must
flush that dirty state before the pass can stop or complete successfully.
`ConvergedFlush` is the final authoritative status write and is invalid after
runtime dependency skips, because skipped required facts mean the pipeline did
not fully converge.

The status-bearing outcome carries the phase, condition, reason, and message.
The runner passes that outcome to the status handler. This lets database steps
represent current behavior such as `Pending` waits, `Provisioning` progress
writes, `Failed` terminal writes, `Deleting` writes, the in-memory tail status
updates, and the final `Ready` flush without hard-coding phases in the runner.

Events stay outside `Outcome`. Event emission depends on pre-write transition
state, so it remains the caller's responsibility rather than becoming a hidden
side effect of `Observe`.

### 6. Keep ports consumer-owned

Ports should be introduced by the component or workflow that consumes them, not
as broad interfaces mirroring Kubernetes clients, CNPG clients, or PostgreSQL
connections.

Good port shape:

- names the business fact or intent the core needs;
- returns provider-neutral facts where practical;
- maps provider errors into domain outcomes at the adapter boundary;
- exposes narrow operations, not generic CRUD.

Do not create empty port packages in this prototype. The pipeline package proves
the runner and contract mechanics without inventing consumers.

### 7. Keep infrastructure narrow

Infrastructure owns external operations only:

- Kubernetes and CNPG reads/applies;
- PostgreSQL connection setup, quoting, SQL execution, and driver error
  classification;
- provider-specific object translation needed by adapters.

Business decisions remain in core. For example, core should decide the desired
privilege intent; infrastructure should perform the PostgreSQL mechanics needed
to apply it.

### 8. Package boundaries

The intended database package direction is:

```text
database/core
database/core/pipeline
database/core/components/<real-component>
database/core/use_cases/<real-workflow>
        ^
        |
database/adapter
        |
database/infrastructure
```

Rules:

- `core` packages define behavior, policies, contracts, and consumer-owned
  ports;
- `adapter` implements core-owned ports and translates provider shapes;
- `infrastructure` performs external Kubernetes, CNPG, and PostgreSQL
  operations;
- `shared` is used only for contracts or infrastructure whose meaning,
  ownership, and lifecycle are identical across consumers;
- new packages must contain real, compiled code and tests, not placeholders.

### 9. Extract the database cluster-readiness observation gate

The first concrete database observation component is
`database/core/components/clusterreadiness`. It owns a narrow reader port,
database-resolved cluster facts, and the policy that classifies the
`PostgresDatabase` prerequisite as available, provisioning, recovering,
missing, or temporarily unreadable.

The gate is read-only. It returns the existing database
`reconciliationTypes.Outcome`, including the authoritative `ClusterReady`
condition data and either a fixed wait or the original transient error. The
procedural database facade remains responsible for status persistence,
transition-aware event emission, and conflict handling, then uses the resolved
facts for the remaining phases of that pass.

The facts remain database-owned rather than shared with `PostgresCluster`.
Cluster core interprets provider health, component convergence, scale/resize,
and lifecycle progression differently; database core treats the cluster as an
upstream prerequisite and needs recovery reporting plus a provider reference.
Only the generic database outcome mechanics are shared. The gate therefore
does not expose a `PostgresCluster` or CNPG object, and provider/Kubernetes
translation stays in `database/adapter` and `database/infrastructure/k8s`.

This is a facade-boundary extraction, not a migration of the full production
facade to the pipeline runner. That migration remains future work.

## CPI-2150 requirement coverage

| Requirement | How this MR covers it |
| --- | --- |
| Minimum package baseline | Adds `database/core/pipeline` and `database/core/types/reconciliation` only, with compiled code and tests. |
| Contract baseline | Defines public contract keys, typed per-pass contracts, two concrete database contract constants, `Requires`, `Provides`, order validation, runtime requirement checks, and incomplete runtime dependency handling. |
| Outcome baseline | Defines converged, waiting, deferred, waiting with explicit condition status, retryable, terminal, silent stop, apply-and-continue, persist-and-continue, and flush outcomes in a lower-level types package importable by future components and use cases. |
| Sequential execution | Adds a runner that executes ordered steps and stops on the first non-converged outcome. |
| Observe-only gates | Keeps mutation opt-in through `MutatingStep`; observe-only steps do not implement fake mutation. |
| Authoritative status | Routes status-bearing outcomes to a status handler, including in-memory tail updates and persisted `Pending`, `Provisioning`, `Failed`, and final `Ready` phases; rejects unflushed in-memory status. |
| Scheduling and precedence | Tests the database order with RW privilege bootstrap between CNPG database convergence and custom metrics, including deferred and unscheduled-provider contract behavior. |
| Pipeline sharing decision | Keeps database mechanics local and records why cluster lifecycle semantics are not reused. |
| Ports and infrastructure placement | Documents consumer-owned ports, adapter translation, infrastructure boundaries, and shared-code criteria. |
| No production facade cutover | Leaves `PostgresDatabaseService` behavior intact. |
| No empty packages | Does not create `components`, `use_cases`, or infrastructure packages without real code. |
| Characterization coverage | Adds tests for current status-write behavior and sticky terminal stop behavior. |
| Concrete observation gate | Adds a database-owned cluster reader, resolved facts, read-only readiness policy, Kubernetes/provider translation, and facade integration without sharing cluster health semantics. |

## Alternatives considered

- **Reuse the cluster runner and health model directly.** Rejected because the
  database lifecycle has different terminal, retry, and status-flush behavior.
- **Introduce concrete database components now.** Rejected because this ADR does
  not cut production over to a component runner; unreachable component copies
  would make the MR larger without proving production behavior.
- **Use two database runners, one for components and one for use cases.**
  Rejected for the prototype because the first required database workflow shape
  is positional. A single ordered pipeline proves that decision with less
  machinery.
- **Collapse all stops into a generic retryable outcome.** Rejected because
  fixed waits, transient errors, terminal errors, and silent sticky stops have
  different reconcile and operator-visible behavior.

## Consequences

- The MR establishes a concrete, tested runner and outcome vocabulary without
  changing production reconciliation.
- Dependency ordering failures become explicit programming errors.
- Runtime dependency misses remain non-blocking for independent later steps, but
  they cannot end as clean convergence.
- In-memory status accumulation must be flushed by a later persistence action.
- Pure observation gates stay honest.
- The design keeps business policy in core and external operations in adapters
  and infrastructure.
- Concrete phase extraction will still require careful tests around idempotency,
  status writes, events, conflicts, and requeue behavior when production code is
  moved.

## References

- `pkg/postgresql/database/core/database.go`
- `pkg/postgresql/database/core/types.go`
- `pkg/postgresql/cluster/core/cluster.go`
- `pkg/postgresql/cluster/core/contracts.go`
- `pkg/postgresql/cluster/core/use_cases/use_cases.go`
- [ADR-0002](0002-actuate-converge-reconcile-pattern.md)
- CPI-2150, CPI-1962, CPI-1961
