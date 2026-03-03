---
name: sok-test
description: Run Splunk Operator tests using standard local harness commands. Use for unit, fast PR gates, and smoke/integration test triage.
---

# SOK Test

## Overview
Execute consistent local test workflows so behavior matches CI expectations.

## Preconditions
- Run `sok-prerequisites` first.
- For code changes, run `sok-build` before broader test runs.

## Workflow
1. Run unit tests: `scripts/dev/unit.sh`.
2. Run fast policy + repo gate: `PR_CHECK_FLAGS=--fast scripts/dev/pr_check.sh`.
3. For kind smoke validation, run: `scripts/dev/kind_smoke.sh`.
4. For failing cases, capture first failure and likely owner path.

## Pass / Fail Criteria
- Pass: requested test commands complete successfully.
- Fail: one or more commands fail, with the first actionable failure identified.

## Output Contract
- Changed files
- Commands run
- Results
- PR-ready summary
