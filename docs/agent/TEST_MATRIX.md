# Test Matrix

## Unit Tests
- Command: `make test`
- Scope: `./pkg/splunk/common`, `./pkg/splunk/enterprise`, `./internal/controller`, and related packages.

## Integration Tests (Kind)
- Create cluster: `make cluster-up`
- Run tests: `make int-test`
- Teardown: `make cluster-down`

## KUTTL Tests
- Suite config: `kuttl/kuttl-test.yaml`
- Tests live under: `kuttl/tests/`
- Example run (if kuttl is installed): `kubectl kuttl test --config kuttl/kuttl-test.yaml`
- Upgrade suite: `kubectl kuttl test --config kuttl/kuttl-test-helm-upgrade.yaml`

## Patterns
- Use `docs/agent/TESTCASE_PATTERNS.md` for SVA helper mapping.

## Environment Variables (from `test/env.sh`)
- `SPLUNK_OPERATOR_IMAGE` default `splunk/splunk-operator:latest`
- `SPLUNK_ENTERPRISE_IMAGE` default `splunk/splunk:latest`
- `CLUSTER_PROVIDER` default `kind`
- `PRIVATE_REGISTRY` default `localhost:5000` for kind
- `TEST_REGEX` or `TEST_FOCUS` to filter tests
- `SKIP_REGEX` to skip tests
- `CLUSTER_WIDE` to install operator cluster-wide
- `DEPLOYMENT_TYPE` set to `manifest` or `helm`

## Targeted Test Runs
- Run a single suite: `cd test/<suite> && ginkgo -v -progress ...`
- Default focus is `smoke` when `TEST_REGEX` is not set.
