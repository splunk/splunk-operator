---
name: sok-pr-crafter
description: Generate clean PR descriptions, checklists, and risk notes. Use after code changes are complete and tests are run (or explicitly skipped).
---

# SOK PR Crafter

## Overview
Create a PR-ready summary and checklist from the current diff and test results.

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

## Definition of Done
- PR summary matches the actual diff and behavior.
- Tests section is accurate and explicit.
- Risks/rollback are captured if relevant.

## Output Contract
- Changed files
- Commands run
- Results
- PR-ready summary
