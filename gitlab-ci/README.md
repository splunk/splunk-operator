# GitLab CI Files

[`gitlab-ci/includes/base.yml`](includes/base.yml) defines shared job defaults and hidden reusable templates.

[`gitlab-ci/includes/baseline.yml`](includes/baseline.yml) defines the repository verification, unit-test, and qualification security-evidence jobs.

[`gitlab-ci/includes/runtime.yml`](includes/runtime.yml) defines the staged image build, image scan, EKS smoke, nightly integration, and Helm validation jobs.

[`gitlab-ci/includes/qualification.yml`](includes/qualification.yml) defines the qualification manifest, report, and compatibility publication jobs.

[`gitlab-ci/includes/admin.yml`](includes/admin.yml) defines the one-off admin jobs and the daily scheduled GitHub intake backfill and GitHub mirror health-check jobs. The daily lane defaults to the public `splunk/splunk-operator` GitHub repository, so the schedule only needs `SOK_PIPELINE_MODE=github_admin_daily` unless a different mirror target is required. The intake writer still requires `PIPELINE_GITLAB_API_TOKEN` because GitLab issue and merge-request creation is not authorized with `CI_JOB_TOKEN`.

[`gitlab-ci/build-test-push.sh`](build-test-push.sh) builds the operator image for the current commit and pushes it to the staging ECR target.

[`gitlab-ci/build-test-push-trivy-scan.sh`](build-test-push-trivy-scan.sh) scans the staged image artifact with Trivy.

[`gitlab-ci/int-test-workflow.sh`](int-test-workflow.sh) reuses the staged operator image, provisions an ephemeral EKS cluster, runs the bounded smoke profile, and writes runtime artifacts under `ci-output/`.

[`gitlab-ci/helm-test-workflow.sh`](helm-test-workflow.sh) reuses the staged operator image, provisions an ephemeral EKS cluster, packages the Helm chart, and runs KUTTL-based Helm validation.

[`gitlab-ci/qualification-manifest.py`](qualification-manifest.py) writes the qualification manifest and the required-evidence contract for a qualification run.

[`gitlab-ci/qualification-report.py`](qualification-report.py) assembles the observed qualification evidence into the compatibility report.

[`gitlab-ci/compatibility-publish.py`](compatibility-publish.py) writes the publish plan for the qualification compatibility result.

[`gitlab-ci/github-intake-backfill.py`](github-intake-backfill.py) backfills selected GitHub issues and PRs into GitLab issue and MR records, and the daily admin lane can auto-discover recently updated GitHub items without manual number input.

[`gitlab-ci/mirror-health-check.sh`](mirror-health-check.sh) performs a read-only branch parity check against the configured GitHub mirror repository.

[`gitlab-ci/lib/pipeline-common.sh`](lib/pipeline-common.sh) contains shared runtime helpers for registry resolution, environment loading, tool bootstrap, and artifact checks.

[`gitlab-ci/diagrams/`](diagrams) contains the PlantUML source and rendered PNGs for the current develop lane and the planned nightly, qualification, and release flows.
