# GitLab CI Files

[`gitlab-ci/includes/base.yml`](includes/base.yml) defines shared job defaults and hidden reusable templates.

[`gitlab-ci/includes/baseline.yml`](includes/baseline.yml) defines the repository verification and unit-test jobs.

[`gitlab-ci/includes/runtime.yml`](includes/runtime.yml) defines the staged image build, image scan, and EKS smoke jobs.

[`gitlab-ci/build-test-push.sh`](build-test-push.sh) builds the operator image for the current commit and pushes it to the staging ECR target.

[`gitlab-ci/build-test-push-trivy-scan.sh`](build-test-push-trivy-scan.sh) scans the staged image artifact with Trivy.

[`gitlab-ci/int-test-workflow.sh`](int-test-workflow.sh) reuses the staged operator image, provisions an ephemeral EKS cluster, runs the bounded smoke profile, and writes runtime artifacts under `ci-output/`.

[`gitlab-ci/lib/pipeline-common.sh`](lib/pipeline-common.sh) contains shared runtime helpers for registry resolution, environment loading, tool bootstrap, and artifact checks.

[`gitlab-ci/diagrams/`](diagrams) contains the PlantUML source and rendered PNGs for the current develop lane and the planned nightly, qualification, and release flows.
