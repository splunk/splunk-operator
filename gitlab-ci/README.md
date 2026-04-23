# GitLab CI Files

[`gitlab-ci/includes/base.yml`](includes/base.yml) defines shared job defaults and hidden reusable templates.

[`gitlab-ci/includes/baseline.yml`](includes/baseline.yml) defines the repository verification, unit-test, and qualification security-evidence jobs.

[`gitlab-ci/includes/runtime.yml`](includes/runtime.yml) defines the staged image build, image scan, EKS smoke, nightly integration, and Helm validation jobs.

[`gitlab-ci/includes/qualification.yml`](includes/qualification.yml) defines the qualification manifest, report, and compatibility publication jobs.

[`gitlab-ci/includes/release.yml`](includes/release.yml) defines the release-branch validation lane, the main-branch publish jobs, Red Hat preflight certification, and the operator-catalog submission-prep jobs.

[`gitlab-ci/build-test-push.sh`](build-test-push.sh) builds the operator image for the current commit and pushes it to the staging ECR target.

[`gitlab-ci/build-test-push-trivy-scan.sh`](build-test-push-trivy-scan.sh) scans the staged image artifact with Trivy.

[`gitlab-ci/int-test-workflow.sh`](int-test-workflow.sh) reuses the staged operator image, provisions an ephemeral EKS cluster, runs the bounded smoke profile, and writes runtime artifacts under `ci-output/`.

[`gitlab-ci/helm-test-workflow.sh`](helm-test-workflow.sh) reuses the staged operator image, provisions an ephemeral EKS cluster, packages the Helm chart, and runs KUTTL-based Helm validation.

[`gitlab-ci/qualification-manifest.py`](qualification-manifest.py) writes the qualification manifest and the required-evidence contract for a qualification run.

[`gitlab-ci/qualification-report.py`](qualification-report.py) assembles the observed qualification evidence into the compatibility report.

[`gitlab-ci/compatibility-publish.py`](compatibility-publish.py) writes the publish plan for the qualification compatibility result.

[`gitlab-ci/release-candidate-artifacts.sh`](release-candidate-artifacts.sh), [`gitlab-ci/fetch-release-candidate.sh`](fetch-release-candidate.sh), [`gitlab-ci/release-publish-images.sh`](release-publish-images.sh), [`gitlab-ci/release-publish-artifacts.sh`](release-publish-artifacts.sh), [`gitlab-ci/release-publish-bundle.sh`](release-publish-bundle.sh), and [`gitlab-ci/release-publish-charts.sh`](release-publish-charts.sh) implement the checked-in release path: package once on the release branch, then promote/publish those validated outputs on `main`.

[`gitlab-ci/preflight-certification.sh`](preflight-certification.sh), [`gitlab-ci/certified-operators-submission.sh`](certified-operators-submission.sh), and [`gitlab-ci/community-operators-submission.sh`](community-operators-submission.sh) capture the Red Hat certification and operator-catalog submission-prep path.

[`gitlab-ci/lib/pipeline-common.sh`](lib/pipeline-common.sh) contains shared runtime helpers for registry resolution, environment loading, tool bootstrap, and artifact checks.

[`gitlab-ci/diagrams/`](diagrams) contains the PlantUML source and rendered PNGs for the current develop lane and the planned nightly, qualification, and release flows.
