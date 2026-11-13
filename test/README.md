# Integration testing for Splunk Operator

The full integration test guide lives in [docs/develop/IntegrationTesting.md](../docs/develop/IntegrationTesting.md), including local k3s workstation setup.

When modifying the test framework (`test/testenv/`, `test/run-tests.sh`, `test/env.sh`), please update that document to keep it accurate.

## SCS sanity gate (`test/scs_sanity/`)

The SCS deployment lane runs operator health checks, a scoped Standalone canary,
and a post-canary health recheck. See the [pipeline operating guide](../gitlab-ci/README.md#scs-sanity-gate-testscs_sanity)
for phase selection, configuration, cleanup, and failure behavior. Run
`make test-scs-sanity` for its cluster-free unit tests.
