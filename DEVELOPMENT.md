# Development Workflow

Use this guide when a task includes implementation, generation, build, or test
work. For environment setup and tool installation, use
`docs/develop/DevelopmentSetup.md`. Treat `Makefile` and `make help` as command
truth instead of extending this file with a command catalog.

## Start from executable evidence

- Inspect the working tree and preserve unrelated changes.
- Read the nearest implementation and tests before editing.
- For code under `pkg/splunk/`, read `pkg/splunk/README.md` and the selected
  package's `doc.go` before placing logic.
- Use `.env` for versions used by CI and the `Makefile` for current build,
  generation, test, image, and deployment entry points.

## Keep the change at its owner

- Keep controllers under `internal/controller/` focused on Kubernetes
  reconciliation and delegation. Put reusable state transitions, resource
  construction, and external-system behavior in the owning `pkg/splunk`
  package.
- Treat controller-gen outputs as generated files. Change API types, markers,
  or generator inputs, then regenerate; do not hand-edit generated DeepCopy
  methods, CRDs, RBAC roles, or webhook manifests.
- Update the owning user or developer document when behavior or configuration
  changes.
- When a change to `test/testenv/`, an integration runner, or integration
  configuration alters a shared lifecycle or execution contract, follow the
  `splunk-operator-integration-tests` skill and update
  `docs/develop/IntegrationTesting.md` to match that contract.

## Go naming conventions

- Use short, lowercase package names without underscores or mixed capitals;
  name packages for their primary responsibility (for example, `resources` or
  `reconcile`).
- Use `MixedCaps` for exported Go names and `mixedCaps` for unexported names.
  Preserve Go initialisms such as `ID`, `URL`, and `API` (`PodURL`,
  `clusterID`). Prefer descriptive names for exported symbols; avoid redundant
  package prefixes (`resources.NewConfigMap`, not
  `resources.NewResourcesConfigMap`).
- Name interfaces for the capability they represent. Use a `-er` name when it
  reads naturally (`Reader`); otherwise use a domain name that describes the
  contract (`DBRepo`). Keep interfaces small and close to their consumers.
- Put tests in `*_test.go` files. Name standard-library tests `Test<Behavior>`
  and benchmarks `Benchmark<Behavior>`; in Ginkgo specs, describe the behavior
  with `Describe`, `Context`, and `It`.
- In Go identifiers and prose, use the operator's current API terms such as
  `ClusterManager`, `LicenseManager`, `IndexerCluster`, and `SearchHeadCluster`.
  Keep legacy Splunk or Kubernetes wire names only where compatibility requires
  them, and use the exact external spelling at those boundaries.

The `make lint` path runs `staticcheck`, including its ST1003 naming
check for non-idiomatic Go identifiers and initialisms.

## Validate at the owning boundary

Choose the narrowest meaningful check first and broaden when the change crosses
package, controller, generated, or user-visible boundaries.

| Changed path | Validation |
|---|---|
| `AGENTS.md`, `DEVELOPMENT.md`, `.agents/skills/`, or documentation | Verify paths, links, and skill metadata; run `git diff --check`; use `make docs-preview` when practical |
| `pkg/splunk/` Go code | Run `make fmt vet`, then a scoped `go test` or `make test-unit` |
| `internal/controller/` behavior | Run relevant scoped envtest; inspect current `Makefile` labels and include classic and Postgres specs for complete coverage; then run `make build` |
| Cross-package Go behavior | Run relevant scoped tests, then `make test` and `make build` |
| `api/` or generated `config/` assets | Run `make manifests generate`, relevant tests, and `make build`; inspect generated changes |
| `helm-chart/` | Run `make helm-check` or the narrower chart target shown by `make help` |
| `test/`, `test/testenv/`, or integration runners | Follow the `splunk-operator-integration-tests` skill and `docs/develop/IntegrationTesting.md`; compile and dry-run the scoped suite before authorized live execution |
| `kuttl/tests/` | Run `kubectl kuttl test --config kuttl/kuttl-test-kind.yaml` only when its cluster context is explicitly in scope |

`make test-unit` needs no Kubernetes API server. Controller tests use envtest.
In-cluster Ginkgo E2E requires a deployed operator, CRDs, workload pods, and
often storage or cloud services; KUTTL owns declarative Helm and Splunk
Validated Architectures scenarios.

Before using a narrower controller target, inspect its package path and label
filter in `Makefile`; complete coverage includes classic and Postgres specs.

Several Make targets generate or format files. Inspect the full resulting diff
so unrelated or accidental generated churn does not enter the change. If a
required check cannot run because a cluster, image, credential, or tool is
unavailable, report that limitation instead of implying coverage.

## Treat cluster-backed work as a separate boundary

Before using an in-cluster test target, follow the
`splunk-operator-integration-tests` skill and inspect the wrapper's full call
chain. Keep deployment separate from test execution, verify the exact
authorized context and suite scope, and use only a terms value supplied by the
user after they follow `docs/README.md`. Verify the operator and Splunk images,
storage, and credentials before live execution. Compilation or dry-run
evidence is not a passed live test.

## Finish with evidence

Summarize the behavior changed, identify generated or documentation updates,
list the checks that passed, and name each check not run with its concrete
missing prerequisite.
