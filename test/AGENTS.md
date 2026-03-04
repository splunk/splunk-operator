# test/ — Integration Tests (Ginkgo)

## What Lives Here
- Ginkgo integration suites (`test/<suite>/...`)
- `test/testenv` helpers for deployment and verification

## Invariants
- Tests should clean up resources (unless DEBUG_RUN / failure).
- Use `testenv` helpers for common readiness checks.
- Keep suites scoped and name tests with searchable tags.

## Common Pitfalls
- Skipping teardown without `DEBUG_RUN`.
- Mixing cluster-wide and namespace-scoped assumptions.
- Assuming license manager exists without a license file/configmap.

## Commands
- Run integration tests: `make int-test`
- Run unit/envtest suite: `make test`
- Generate scaffolds: `python3 scripts/generate_testcase.py --spec docs/agent/TESTCASE_SPEC.yaml`

## Notes
KUTTL tests live under `kuttl/` and are not executed by `make test`.
