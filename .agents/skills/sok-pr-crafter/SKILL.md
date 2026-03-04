---
name: sok-pr-crafter
description: Generate clean PR descriptions, checklists, and risk notes. Use after code changes are complete and tests are run (or explicitly skipped).
---

# SOK PR Crafter

## Overview
Create a PR-ready summary and checklist from the current diff and test results.

## Preconditions
- Code changes are complete for this pass.
- Test commands were run (or explicitly skipped with reason).

## Scope
Allowed paths:
- `templates/**`
- `docs/**`
- `.agents/**`

Forbidden paths:
- `api/**`
- `internal/**`
- `pkg/**`
- `test/**`
- `kuttl/**`
- `config/**`
- `bundle/**`
- `helm-chart/**`
- `vendor/**`

This skill should not change product code. If changes are required, stop and hand off.

## Workflow
1. Summarize the change set and key behavior changes.
2. List tests run (or explicitly not run).
3. Call out risks, rollbacks, and compatibility notes.
4. Format output using `templates/pull_request.md`.

## Pass / Fail Criteria
- Pass: PR content accurately reflects diff/tests/risks and matches template expectations.
- Fail: summary is inconsistent with code or validation evidence is missing.

## Output Contract
- Changed files
- Commands run
- Results
- PR-ready summary
