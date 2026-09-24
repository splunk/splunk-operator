# Use cases

Cross-cutting, episodic workflows that orchestrate the cluster's foundation
components — not the steady-state reconciliation itself.

## Component vs. use case

- A **foundation component** (provisioner, backup, pooler, …) owns one piece of
  the desired steady state and reconciles it every loop, idempotently.
- A **use case** is a finite, stateful procedure with a beginning and an end
  (e.g. a major-version upgrade) that temporarily takes over: it may pause
  components while it runs and persists its own progress across reconciles.

## Why not just a component?

A component reconciles one piece of desired world state, every loop,
idempotently — no concept of phases, policies, or completion. A use case is a
finite business workflow with a beginning, middle, and end. Collapsing them
would lose the two-phase ordering guarantee (components first, use cases only
after all required status is stable), make `BlocksComponents` self-referential,
and leak domain policy — retry gates, rollback decisions, validation — into the
infrastructure loop where Kubernetes reconciliation logic lives.

Keep them separate. The boundary is the point.

## Contract

Every use case implements five methods:

- `Prerequisites` — verify that the world state needed to execute safely is
  ready (e.g. source version written to status); called after components
  stabilise, before `Schedule`; returning a non-nil error requeues without
  reaching `Act`.
- `Schedule` — decide if there is work this loop (reads persisted state); cheap,
  side-effect free, idempotent.
- `BlocksComponents` / `BlocksUseCases` — while scheduled, which components and
  peer use cases must stand down so they don't fight the workflow.
- `Act` — advance the state machine by exactly one step, persist progress, and
  return a `Report` (`Retry=true` to be called again next loop).

## Predicates and lazy construction

Every use case has a companion predicate — a pure, I/O-free function that checks
the live `PostgresClusterSpec` for an explicit user signal before anything is
built. **Only `Spec` fields may act as a trigger.** Status fields and annotations
reflect the system's own output and are excluded; a use case triggered by its
own status would be non-deterministic.

The predicate is a **necessary, not sufficient** pre-condition. Returning `false`
means "the user has not asked for this — skip construction entirely." Returning
`true` means "something in Spec suggests this workflow is requested — build it
and let `Schedule` decide with the full live state in hand."

This enables **lazy construction**: a use case and its adapters are only
instantiated when the spec contains a clear sign of user intent. In the common
steady state — feature switched off — the reconciler never touches the factory,
never reads upgrade status, and pays nothing beyond the predicate check itself.
The cost of an inactive use case is one cheap local function call per pass.

`Schedule` then makes the precise determination: it reads persisted upgrade
state, CNPG status, and other live data to decide whether there is actually work
to do. The predicate only ensures that construction is not wasted on clusters
where the user has not opted in at all.

## Execution model

Each reconcile pass runs in two phases:

1. **Schedule** — prerequisites are checked inside `Schedule`; a use case whose
   prereqs are unmet is silently deferred (not scheduled, not blocking). This
   runs *before* components so `BlocksComponents` is populated before any
   component reconciles — preventing drift on blocked resources.
2. **Components + Act** — non-blocked components reconcile; if any is
   intermediate the pass returns and requeues. Then exactly **one** use case
   executes per pass. The first scheduled use case in order runs exclusively;
   others wait until it completes (non-retry).

Only one use case runs at a time. This prevents concurrent workflows from
fighting over the same components or producing conflicting status writes.

Keep the generic surface narrow and put workflow specifics behind ports, so the
next workflow copies this shape instead of leaking into the foundation.
