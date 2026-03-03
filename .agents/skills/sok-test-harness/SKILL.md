---
name: sok-test-harness
description: Run or troubleshoot Splunk Operator tests, including unit tests, integration tests, and local kind-based workflows. Use when asked to run tests, set up a kind cluster, or produce a test failure triage summary.
---

# SOK Test Harness

## Overview
Run the repo's standard unit and integration tests, and summarize failures consistently.

## Quick Start
- Unit tests: `scripts/dev/unit.sh`
- Lint/format checks: `scripts/dev/lint.sh`
- Envtest assets: `scripts/dev/envtest.sh`
- Kind smoke: `scripts/dev/kind_smoke.sh`
- Repo verification: `scripts/dev/pr_check.sh` (or `scripts/verify_repo.sh --all`)
- Generate new tests: `python3 scripts/generate_testcase.py --spec docs/agent/TESTCASE_SPEC.yaml`

## Workflow
1. Confirm prerequisites.
2. Run unit tests or kind integration tests.
3. If tests fail, summarize the failure and propose next steps.
4. When CRDs or bundles changed, run `make verify` to confirm generated outputs.

## Preconditions
- Go toolchain and `ginkgo`
- Docker and `kubectl`
- `kind` installed for local integration tests

## Unit Tests
- Default command: `make test`
- Prefer `scripts/dev/unit.sh` to run with the repo defaults.

## Integration Tests (Kind)
- Default commands: `make cluster-up`, `make int-test`, `make cluster-down`
- Prefer `scripts/dev/kind_smoke.sh` for a quick local sanity run.
- Skill-local scripts (`scripts/run_kind_e2e.sh`, `scripts/push_kind_operator_image.sh`) remain available for deeper e2e flows.

## Common Environment Variables
These are defined in `test/env.sh` and can be overridden in your shell.
- `SPLUNK_OPERATOR_IMAGE` default `splunk/splunk-operator:latest`
- `SPLUNK_ENTERPRISE_IMAGE` default `splunk/splunk:latest`
- `CLUSTER_PROVIDER` default `kind` for local runs
- `PRIVATE_REGISTRY` default `localhost:5000` when using kind
- `TEST_REGEX` or `TEST_FOCUS` to filter tests
- `SKIP_REGEX` to skip tests
- `CLUSTER_WIDE` to run cluster-wide operator install

## Failure Triage Output
- Provide the failing test names or package paths.
- Include the first error and any repeated error pattern.
- Suggest the most likely code area to inspect.

## Pass / Fail Criteria
- Pass: requested test commands complete with actionable output and no unresolved errors.
- Fail: commands fail without a reproducible triage summary or required follow-up.

## Output Contract
- Changed files
- Commands run
- Results
- PR-ready summary

## Key Paths
- Test harness: `test/README.md`
- Integration scripts: `test/run-tests.sh`, `test/deploy-cluster.sh`, `test/deploy-kind-cluster.sh`
- Unit test target: `Makefile` (`make test`)
- Environment defaults: `test/env.sh`
- Harness docs: `docs/agent/TEST_MATRIX.md`
