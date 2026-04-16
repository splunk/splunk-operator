# GitLab CI Scope

This directory contains only the runtime pieces needed for the first GitLab CI migration slice on `develop`.
The corresponding pipeline intentionally covers the smallest production-relevant path: repository verification,
unit and kubectl plugin tests, staged operator image build, staged image Trivy scan, and bounded EKS smoke
validation. Nightly, qualification, release, variant-runtime, and intake flows are intentionally excluded from
this first MR so the review surface stays narrow and behavior remains easy to verify.

The top-level [`.gitlab-ci.yml`](../.gitlab-ci.yml) keeps
job orchestration explicit. Runtime behavior lives in checked-in shell scripts instead of large inline YAML blocks.
Shared runtime parsing, registry resolution, environment loading, and tool bootstrap logic is centralized in
[`gitlab-ci/lib/pipeline-common.sh`](lib/pipeline-common.sh).

[`gitlab-ci/build-test-push.sh`](build-test-push.sh)
builds the operator image for the current commit and pushes it only to the internal staging ECR target. The
Dockerfile builder stage is parameterized so GitLab can use the internal Go builder image and avoid Docker Hub
rate limiting.

[`gitlab-ci/build-test-push-trivy-scan.sh`](build-test-push-trivy-scan.sh)
consumes the staged image artifact and performs the bounded Trivy scan used in this first cut. It does not pull
from public registries for the operator image and it does not introduce shared-prodsec template coupling yet.

[`gitlab-ci/int-test-workflow.sh`](int-test-workflow.sh)
reuses the staged operator image, mirrors the Splunk Enterprise image into staging ECR, provisions an ephemeral
EKS cluster, runs the `managersecret-smoke-s1` profile, and always emits cleanup and runtime artifacts under
`ci-output/`.

The required project variables for this first slice are limited to the staging AWS and EKS values needed by the
build and smoke path. Security-scanner onboarding tokens, nightly controls, qualification controls, and release
controls belong in later MRs once the `develop` lane is proven.
