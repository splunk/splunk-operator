# GitLab CI Files

This directory holds the checked-in scripts, shared helpers, and local include files for the
GitLab CI pipeline.

[`gitlab-ci/includes/base.yml`](includes/base.yml) defines the common pipeline stages, global
variables, default runner image, and the shared AWS staging-variable mapping used by the build,
scan, and smoke jobs.

[`gitlab-ci/includes/baseline.yml`](includes/baseline.yml) defines the repository verification,
unit-test, and `kubectl-splunk` test jobs.

[`gitlab-ci/includes/runtime.yml`](includes/runtime.yml) defines the staged image build, Trivy
scan, and bounded EKS smoke jobs.

[`gitlab-ci/build-test-push.sh`](build-test-push.sh) builds the operator image and pushes it to
the staging registry.

[`gitlab-ci/build-test-push-trivy-scan.sh`](build-test-push-trivy-scan.sh) scans the staged image
with Trivy.

[`gitlab-ci/int-test-workflow.sh`](int-test-workflow.sh) provisions the ephemeral EKS environment,
runs the bounded smoke slice, and writes runtime artifacts under `ci-output/`.

[`gitlab-ci/lib/pipeline-common.sh`](lib/pipeline-common.sh) contains shared shell helpers for
registry resolution, environment loading, tool bootstrap, and artifact handling.

[`gitlab-ci/diagrams/`](diagrams) contains the PlantUML sources and rendered PNG files for the CI
flow diagrams used in review.
