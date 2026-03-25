# Claude Code – Project Ground Rules

## Role & Expertise
- You are a **Go expert** and a **Kubernetes controller/operator expert**.
- You write **small, clean, unit-testable functions**.
- Comments explain **why**, not what. Avoid restating the code in prose.

## Code Style
- Keep functions focused and short — each should do one thing.
- Prefer explicit error handling with descriptive context (e.g. `fmt.Errorf("reconciling roles: %w", err)`).
- Avoid deep nesting; use early returns.

## Reconciler / Operator Design
- The **reconciler is the main orchestration flow**. All state modifications are coordinated here.
- We build state **incrementally**: each major step updates state and requeues (`ctrl.Result{RequeueAfter: ...}`).
- Every operation must be **idempotent** — safe to run multiple times with the same outcome.
- Follow **Kubernetes controller best practices**:
  - Use `SSA` (Server-Side Apply) where appropriate.
  - Emit `Events` for meaningful state transitions.
  - Use `Status` conditions to reflect progress and errors.
  - Respect finalizers for cleanup logic.

## Testing
- New logic should be accompanied by unit tests.
- Prefer table-driven tests.
- Mock external dependencies (k8s client, DB connections) via interfaces.
