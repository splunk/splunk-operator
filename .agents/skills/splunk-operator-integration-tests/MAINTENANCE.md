# Maintaining the Integration-Test Skill

## Objective

This skill helps an agent make sound, safe decisions across one cluster-backed
Ginkgo test surface. It supports three entry paths: suite/spec work, shared
framework or runner work, and validation or execution. Each path must have a
direct starting point without copying the integration-testing guide.

## File responsibilities

| Path | Purpose |
|---|---|
| `SKILL.md` | Runtime task router, decision criteria, shared contracts, safety, and success boundary |
| `agents/openai.yaml` | User-facing discovery metadata and a prompt consistent with all supported task paths |
| `MAINTENANCE.md` | Design rationale and modification checks |
| `docs/develop/IntegrationTesting.md` | Detailed architecture, setup, examples, execution, and debugging |
| `test/` and `test/testenv/` | Executable truth for suite and framework behavior |

## Modification checks

- Keep the description bounded to cluster-backed Ginkgo tests. Unit, envtest,
  Helm, and KUTTL work routes through `AGENTS.md`.
- Keep a direct entry in the task table for every task named by the description.
  Do not add a heading for every helper noun; route related work to one owning
  workflow.
- Prefer current code and call sites over copied signatures, suite catalogs, or
  timeout values. Put stable, reusable examples in
  `docs/develop/IntegrationTesting.md`; ordinary suite or spec changes do not
  require a documentation update.
- Preserve the distinction between compile/dry-run evidence and a live passed
  test. Preserve the explicit authorization, context, legal-input, image,
  credential, and suite-scope boundaries for cluster-backed execution.
- When runtime scope changes, update `agents/openai.yaml` in the same change.
- Verify referenced paths and Markdown structure, parse both YAML files, run
  `git diff --check`, and compile an affected suite when the change touches
  executable test code.

## Decision log

| Date | Decision | Reason |
|---|---|---|
| 2026-07-30 | Organize runtime guidance around three task paths | The original broad trigger was accurate, but agents entering for framework or validation work lacked a direct starting point and could mistake new-test guidance for their workflow. |
