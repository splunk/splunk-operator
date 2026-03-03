---
name: sok-doc-updater
description: Update Splunk Operator docs and examples for a change. Use when a change requires docs, CR examples, or user-facing guidance updates.
---

# SOK Doc Updater

## Overview
Keep docs, examples, and user-facing guidance in sync with code changes.

## Preconditions
- Product behavior change and impacted docs are known.
- If code changes are still pending, defer this skill until code scope is stable.

## Scope
Allowed paths:
- `docs/**`
- `README.md`
- `config/samples/**`
- `helm-chart/**`

Forbidden paths:
- `api/**`
- `internal/**`
- `pkg/**`
- `test/**`
- `kuttl/**`
- `bundle/**`
- `vendor/**`

If product code changes are required, stop and hand off to the appropriate skill.

## Workflow
1. Identify the user-facing change and affected docs.
2. Update spec fields, examples, and any compatibility notes.
3. Verify examples are consistent with current CRD schema.
4. Provide a short summary and a test/validation note if applicable.

## Pass / Fail Criteria
- Pass: docs/examples match current behavior and schema, with follow-ups called out.
- Fail: docs are incomplete, inconsistent, or missing validation notes.

## Output Contract
- Changed files
- Commands run
- Results
- PR-ready summary
