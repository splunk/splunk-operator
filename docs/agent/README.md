# Agent Harness Docs

These documents are the system of record for agent-assisted development in this repo.
They are short, concrete, and intended to be read by Codex skills and humans.

Spec-first governance lives in `docs/specs/`. For non-trivial code changes, agents
must update a governing spec and pass `scripts/dev/spec_check.sh`.

## Index
- `../specs/` contains governing design specs and lifecycle state
- `CRD_MAP.md` maps kinds to API versions, types, controllers, and enterprise logic files
- `RECONCILE_FLOW.md` outlines the reconciliation flow, gates, and status phases
- `TEST_MATRIX.md` lists unit and integration test paths and environment variables
- `TESTCASE_SPEC.yaml` is a template for generating new integration/KUTTL tests
- `TESTCASE_PATTERNS.md` maps SVA patterns and features to test helpers
- `OPERATIONS.md` provides debug commands, log access, and pprof access notes
- `RELEASE_FLOW.md` provides a concise release checklist and artifact map
- `HARNESS_CONTRACT.md` defines required harness gates and output contract
