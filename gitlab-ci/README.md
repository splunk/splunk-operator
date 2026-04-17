# GitLab CI Scope

This directory contains the checked-in runtime helpers for the first GitLab CI slice on `develop`.

[`gitlab-ci/build-test-push.sh`](build-test-push.sh) builds the operator image for the current commit and pushes
it to the staging ECR target.

[`gitlab-ci/build-test-push-trivy-scan.sh`](build-test-push-trivy-scan.sh) scans the staged image artifact with
Trivy.

[`gitlab-ci/int-test-workflow.sh`](int-test-workflow.sh) reuses the staged operator image, provisions an
ephemeral EKS cluster, runs the bounded smoke profile, and writes runtime artifacts under `ci-output/`.

[`gitlab-ci/lib/pipeline-common.sh`](lib/pipeline-common.sh) contains shared runtime helpers for registry
resolution, environment loading, tool bootstrap, and artifact checks.
