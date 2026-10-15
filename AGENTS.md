# Splunk Operator - AI Agent Guide

Kubernetes operator (Go, Operator SDK + controller-runtime) that manages Splunk
Enterprise deployments. This file is routing-first: it tells you where to look
and who to ask. Detailed docs live under `docs/` and `pkg/splunk/README.md`.

When changing this guide, read [`AGENTS.maintenance.md`](AGENTS.maintenance.md).

## Start Here - Where To Find Things

- **Architecture & reconcile flow**: [`docs/develop/Architecture.md`](docs/develop/Architecture.md)
- **Package layout & import-layer rules**: [`pkg/splunk/README.md`](pkg/splunk/README.md) (each package has a `doc.go`)
- **Local setup & prerequisites**: [`docs/develop/DevelopmentSetup.md`](docs/develop/DevelopmentSetup.md)
- **Contributing workflow**: [`docs/develop/Contributing.md`](docs/develop/Contributing.md)
- **Implementation, build & validation workflow**: [`DEVELOPMENT.md`](DEVELOPMENT.md)
- **Testing strategy**: [`docs/develop/IntegrationTesting.md`](docs/develop/IntegrationTesting.md)
- **Webhooks / feature gates**: [`docs/develop/WebhookDevelopment.md`](docs/develop/WebhookDevelopment.md), [`docs/develop/FeatureGates.md`](docs/develop/FeatureGates.md)
- **Logging conventions**: [`docs/develop/LoggingAndEvents.md`](docs/develop/LoggingAndEvents.md)
- **User-facing operation docs**: [`docs/operate/`](docs/operate/)
- **CI & release automation**: [`.gitlab-ci.yml`](.gitlab-ci.yml), [`gitlab-ci/`](gitlab-ci/), and [`.github/workflows/`](.github/workflows/)
- **Production ownership & operational links**: [`.service-manifests/splunk-operator-component.yaml`](.service-manifests/splunk-operator-component.yaml)
- **Metrics, telemetry & monitoring hardening**: [`config/prometheus/`](config/prometheus/), [`config/network-policy/`](config/network-policy/), [`config/manager/controller_manager_telemetry.yaml`](config/manager/controller_manager_telemetry.yaml), [`pkg/splunk/workflow/telapp/`](pkg/splunk/workflow/telapp/), and [`docs/postgresql/PostgreSQLObservabilityDashboard.md`](docs/postgresql/PostgreSQLObservabilityDashboard.md)
- **Code ownership & escalation**: [`.gitlab/CODEOWNERS`](.gitlab/CODEOWNERS) routes reviewers — default `@okta-groups/sg-cloud-sok-developer-platform`, PostgreSQL surfaces (`pkg/postgresql/`, `*postgres*`) `@okta-groups/sg-cloud-pfm-cse`
- **Work intake / review**: open an MR using [`.gitlab/merge_request_templates/Default.md`](.gitlab/merge_request_templates/Default.md) (Jira epic/ticket required); GitLab (`cd.splunkdev.com/sok/splunk-operator`) is the source of truth and GitHub is a read-only mirror
- **CRD API types**: `api/<group>/<version>/` (stable Enterprise `enterprise/v4`; PostgreSQL `platform/v1alpha1`)
- **Controller / reconcile logic**: `internal/controller/<api-group>/` (Enterprise `enterprise/`; PostgreSQL `platform/`)
- **Business logic**: `pkg/splunk/`; **entry point**: `cmd/main.go`; **manifests / RBAC / samples**: `config/`; **Helm charts**: `helm-chart/`
- **Integration tests**: `test/`; **KUTTL scenarios**: `kuttl/`; **shared framework and template**: `test/testenv/` and `test/example/`
- **Full command list**: `make help`

Use these routes without assuming every task requires an edit. Research, review,
design, and triage start from the owning source above; implementation,
generation, build, and test work continues in [`DEVELOPMENT.md`](DEVELOPMENT.md).

## Always-Read Guidance

Short, task-neutral boundaries; detailed procedures live in the docs routed above.

- **Primary loop before every MR**: run `make fmt vet`, then `make test`, then `make build`; after changing API types (`api/enterprise/**`) also run `make manifests generate`. Full workflow and validation depth live in [`DEVELOPMENT.md`](DEVELOPMENT.md).
- **Generated-code drift** (CRDs, RBAC, DeepCopy): re-run `make manifests generate`; never hand-edit generated files.
- **Human sign-off required** before CRD schema or API-version changes, RBAC changes, cluster deploy/undeploy, and anything under `pkg/postgresql/`; never introduce breaking changes to public CRD APIs without owner approval.
- **Git**: do not commit or push unless the user asks; never force-push, rewrite history, or bundle unrelated changes.
- **Cluster & legal safety**: before live Kubernetes work, verify the authorized context, namespace, and scope; never infer acceptance of the Splunk General Terms or populate `SPLUNK_GENERAL_TERMS`, and use only a value the user supplies after they follow [`docs/README.md`](docs/README.md). The sole documented exception is the checked-in `SOK_PIPELINE_MODE=scs_deploy` CI lane (in-house SCS Helm deploy by Splunk employees); it never extends to local commands or customer deployments.
- **Environment & secrets**: Go version comes from `.env` (`GO_VERSION`) with the toolchain pinned via `go.mod`; key variables (`NAMESPACE`, `WATCH_NAMESPACE`, `SPLUNK_GENERAL_TERMS`, cloud test credentials) are documented in [`docs/develop/DevelopmentSetup.md`](docs/develop/DevelopmentSetup.md); put secrets in `.env.local` (gitignored) and never commit credentials.
- **When stuck**: check [`docs/develop/`](docs/develop/) and [`docs/operate/`](docs/operate/) first; for failing or flaky tests see [`docs/develop/IntegrationTesting.md`](docs/develop/IntegrationTesting.md); then ask the owning team (see CODEOWNERS) in the MR rather than guessing.
- **Common issues**: CRD not found → run `make install`; permission errors → check `kubectl auth can-i --list`; image pull errors → verify `IMG` and registry access.
- **Completion evidence**: state what changed, which checks passed, and which checks were not run with their concrete missing prerequisites.

## Reference

- [Operator SDK](https://sdk.operatorframework.io/) · [Kubernetes API](https://kubernetes.io/docs/reference/) · [Splunk Enterprise](https://help.splunk.com/en)
