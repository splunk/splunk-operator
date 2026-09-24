---
name: splunk-operator-integration-tests
description: Design, change, review, or safely validate Splunk Operator cluster-backed Ginkgo tests and their shared execution contracts. Use for suites under test/, framework code under test/testenv/, integration-test runners or configuration, and docs/develop/IntegrationTesting.md; use the repository guide for unit, envtest, Helm, or KUTTL work.
---

# Splunk Operator Integration Tests

Prove behavior owned by the operator while keeping cluster-heavy validation
scoped. Treat the current test code and shared framework as executable truth;
use `docs/develop/IntegrationTesting.md` for the wider architecture, setup, and
execution guide.

## Choose the task path

| Task | Start with | Continue with |
|---|---|---|
| Design, add, or review a suite or spec | The nearest `test/<suite>/` and **Design suite or spec work** | **Use shared lifecycle contracts**, then **Validate or execute** |
| Change lifecycle helpers, framework code, runners, configuration, or the guide | Call sites under `test/`, the owning file under `test/testenv/` or `test/*.sh`, and **Change a shared contract** | **Validate or execute** with representative suites |
| Validate or run an existing suite without editing it | The exact suite path and its prerequisites in `docs/develop/IntegrationTesting.md` | **Validate or execute**; stop before live work unless its scope is authorized |

## Design suite or spec work

1. Identify the operator behavior that could regress.
2. Use a unit test for isolated package behavior that needs neither a Kubernetes
   API server nor a deployed operator.
3. Use controller envtest under `internal/controller/` for reconciliation
   behavior that needs a Kubernetes API server but not a live cluster.
   `make test` is the combined package and controller set. For a controller-only
   target, inspect its package path and label filter in the current `Makefile`;
   complete coverage includes both classic and Postgres specs.
4. Use an in-cluster E2E suite under `test/` only for behavior that requires a
   deployed operator, real workloads, storage, or cloud integration.
5. Do not make Splunk-internal behavior the sole assertion. Pair any Splunk REST
   or health check with an operator-owned signal such as CR status, pod count,
   StatefulSet readiness, or Kubernetes events.

- Discover current suites from `*_suite_test.go` files instead of relying on a
  copied suite list.
- Add a spec to the nearest existing suite unless isolation, setup, credentials,
  or CI selection requires a separate suite.
- For a new suite, start from `test/example/`, compile the copied suite, and
  compare it with a recently maintained suite with similar topology and
  prerequisites.
- Read the current helper signatures in `test/testenv/` and their call sites.
  Verify examples from documentation against compiling code and correct the
  owning source when they diverge.

## Use shared lifecycle contracts

- Create suite-scoped state with the current `TestEnv` pattern and spec-scoped
  state with `SetupTestCaseEnv`.
- Pair every setup with the current `TeardownTestCaseEnv` pattern, including its
  context and failure-preservation behavior.
- Prefer `TestCaseEnv` workflow, deployment, watch, and verification helpers to
  open-coded resource orchestration.
- Use timeout constants from `test/testenv/timeouts.go` for suite, setup,
  teardown, and spec deadlines. Do not add arbitrary sleeps or duplicate
  polling loops.
- Accept `SpecContext` in nodes that perform cluster work and propagate it to
  context-aware helpers.
- Add orthogonal Ginkgo labels in the canonical order documented in
  `docs/develop/IntegrationTesting.md`. Confirm the current neighboring tests
  and CI label filters before introducing a new label.
- Keep credentials in environment variables or existing test configuration;
  never place secrets in specs, fixtures, logs, or tracked environment files.

## Change a shared contract

When a change to `test/testenv/`, `test/run-tests.sh`, `test/env.sh`,
`test/trigger-tests.sh`, or `test/deploy-operator.sh` alters a shared lifecycle
or execution contract:

1. Search all call sites before changing a shared contract.
2. Update `docs/develop/IntegrationTesting.md` to match the changed contract.
3. Compile representative suites that exercise the changed contract.
4. Prefer one coherent framework change over compatibility branches in every
   suite.

If the guide conflicts with compiling code, follow the current implementation
and correct the guide rather than encoding the discrepancy in this skill.

## Validate or execute

1. Run formatting and static checks for changed Go files:

   ```bash
   make fmt vet
   ```

2. Compile the affected suite without starting its Ginkgo entry point:

   ```bash
   go test -run '^$' ./test/<suite>
   ```

3. Inspect test registration and labels with Ginkgo's dry-run mode when the
   local Ginkgo binary is available.
4. Before using an E2E wrapper, inspect its complete call chain and confirm:
   - test execution does not install or uninstall cluster-scoped resources;
   - any separate deployment step requires an explicitly authorized disposable
     context and passes through the user's terms value;
   - the selected command discovers only the intended suite, or the environment
     is sized and configured for every suite's `BeforeSuite`;
   - cluster-wide execution verifies the deployed operator image.
5. If any condition is not met, stop at compilation and dry-run. When live
   execution is explicitly in scope, verify the context, namespace, images,
   storage, and credentials, then run only the narrowest authorized suite.

Report compilation-only coverage and missing prerequisites explicitly; do not
describe a test as passing when it was only discovered or compiled.
