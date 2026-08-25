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
- **Agent implementation & validation workflow**: [`DEVELOPMENT.md`](DEVELOPMENT.md)
- **Testing strategy**: [`docs/develop/IntegrationTesting.md`](docs/develop/IntegrationTesting.md)
- **Webhooks / feature gates**: [`docs/develop/WebhookDevelopment.md`](docs/develop/WebhookDevelopment.md), [`docs/develop/FeatureGates.md`](docs/develop/FeatureGates.md)
- **Logging conventions**: [`docs/develop/LoggingAndEvents.md`](docs/develop/LoggingAndEvents.md)
- **User-facing operation docs**: [`docs/operate/`](docs/operate/)
- **CI & release automation**: [`.gitlab-ci.yml`](.gitlab-ci.yml), [`gitlab-ci/`](gitlab-ci/), and [`.github/workflows/`](.github/workflows/)
- **Production ownership & operational links**: [`.service-manifests/splunk-operator-component.yaml`](.service-manifests/splunk-operator-component.yaml)
- **Metrics, telemetry & monitoring hardening**: [`config/prometheus/`](config/prometheus/), [`config/network-policy/`](config/network-policy/), [`config/manager/controller_manager_telemetry.yaml`](config/manager/controller_manager_telemetry.yaml), [`pkg/splunk/workflow/telapp/`](pkg/splunk/workflow/telapp/), and [`docs/postgresql/PostgreSQLObservabilityDashboard.md`](docs/postgresql/PostgreSQLObservabilityDashboard.md)
- **Full command list**: `make help`

### Where code lives

- **CRD API types**: `api/<group>/<version>/` (stable Enterprise: `enterprise/v4`; PostgreSQL: `platform/v1alpha1`)
- **Controller / reconcile logic**: `internal/controller/<api-group>/` (Enterprise: `enterprise/`; PostgreSQL: `platform/`)
- **Business logic**: `pkg/splunk/` (see `pkg/splunk/README.md`)
- **Entry point**: `cmd/main.go`
- **Manifests / RBAC / samples**: `config/`
- **Helm charts**: `helm-chart/`
- **Integration tests**: `test/`; **KUTTL scenarios**: `kuttl/`
- **Shared integration-test framework and template**: `test/testenv/` and `test/example/`

Use these routes without assuming every task requires an edit. Research,
review, design, and triage start from the owning source; implementation,
generation, build, and test work continues in `DEVELOPMENT.md`.

## Ownership & Escalation

- **Code ownership** is defined in [`.gitlab/CODEOWNERS`](.gitlab/CODEOWNERS):
  - Default owner: `@okta-groups/sg-cloud-sok-developer-platform`
  - PostgreSQL surfaces (`pkg/postgresql/`, `*postgres*`): `@okta-groups/sg-cloud-pfm-cse`
- **Source of truth**: GitLab (`cd.splunkdev.com/sok/splunk-operator`); GitHub is a read-only mirror.
- **Work intake / review**: open an MR using [`.gitlab/merge_request_templates/Default.md`](.gitlab/merge_request_templates/Default.md); Jira epic/ticket fields are required.

## What To Do When Stuck Or Blocked

- **Ambiguous requirements**: check `docs/develop/` and `docs/operate/` first; if still unclear, ask the owning team (see CODEOWNERS) in the MR rather than guessing.
- **Failing / flaky tests**: see [`docs/develop/IntegrationTesting.md`](docs/develop/IntegrationTesting.md).
- **Generated-code drift** (CRDs, RBAC, DeepCopy): re-run `make manifests generate`; never hand-edit generated files.
- **High-risk / human sign-off required** before proceeding: CRD schema or API-version changes, RBAC changes, cluster deploy/undeploy, and anything under `pkg/postgresql/`.
- **Git operations**: Do not commit or push unless the user explicitly requests it. When requested, commit only task-related changes and push only the current feature branch. Never force-push, rewrite history, or include unrelated changes without explicit approval.
- **Never**: introduce breaking changes to public CRD APIs without owner approval.

### Common Issues

- **CRD not found**: run `make install`
- **Permission errors**: check `kubectl auth can-i --list`
- **Image pull errors**: verify `IMG` and registry access

### Cluster and legal safety

Before live Kubernetes work, inspect the full command call chain and verify the
exact authorized context, namespace, and scope. Never infer acceptance of the
Splunk General Terms or populate `SPLUNK_GENERAL_TERMS`; use only a value
supplied by the user after they follow `docs/README.md`.

## Build, Test & Validate

Primary loop, run before every MR:

```bash
make fmt vet   # format + static checks
make test      # unit tests (Ginkgo/Gomega, envtest) with coverage
make build     # compile the operator binary
```

- **After changing API types** (`api/enterprise/**`): also run `make manifests generate`.
- **Docs-only changes**: `make docs-preview` (no code build required).
- Full target list: `make help`.
- **Completion evidence**: state what changed, checks that passed, and checks
  not run with their concrete missing prerequisites.

## Environment

- Go version is sourced from `.env` (`GO_VERSION`); the toolchain is pinned via `go.mod`.
- Key variables are documented in [`docs/develop/DevelopmentSetup.md`](docs/develop/DevelopmentSetup.md): `NAMESPACE`, `WATCH_NAMESPACE`, `SPLUNK_GENERAL_TERMS` (required), and cloud test credentials.
- Put secrets in `.env.local` (gitignored) - never commit credentials.

## Reference

- [Operator SDK](https://sdk.operatorframework.io/) · [Kubernetes API](https://kubernetes.io/docs/reference/) · [Splunk Enterprise](https://help.splunk.com/en)
