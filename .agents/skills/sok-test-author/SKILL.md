---
name: sok-test-author
description: Write or update Splunk Operator tests (unit/envtest/integration/KUTTL). Use when a change needs new tests or test adjustments.
---

# SOK Test Author

## Overview
Create or update tests for operator behavior, using existing testenv helpers and patterns.

## Scope
Allowed paths:
- `test/**`
- `kuttl/**`
- `scripts/**`
- `docs/**`
- `config/samples/**`

Forbidden paths:
- `api/**`
- `internal/**`
- `pkg/**`
- `bundle/**`
- `helm-chart/**`
- `vendor/**`

If product code changes are required, stop and hand off to the appropriate skill.

## Workflow
1. Determine test type: unit/envtest, integration (Ginkgo), or KUTTL.
2. Locate existing patterns in `docs/agent/TESTCASE_PATTERNS.md` and `test/`.
3. Scaffold tests using `scripts/generate_testcase.py` if helpful.
4. Implement assertions using `test/testenv` helpers.
5. Run tests (or specify exact commands).

## Commands
- Unit/envtest: `scripts/dev/unit.sh`
- Lint/format: `scripts/dev/lint.sh`
- KUTTL scaffolds: `python3 scripts/generate_testcase.py --spec docs/agent/TESTCASE_SPEC.yaml`

## Definition of Done
- Tests compile and run (or a clear reason is recorded).
- Assertions cover the intended behavior and failure modes.
- Test names and suite structure match repo conventions.

## Output Contract
- Changed files
- Commands run
- Results
- PR-ready summary
