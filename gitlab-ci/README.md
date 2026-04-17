# GitLab CI Operating Notes

This directory is the checked-in runtime surface for the canonical GitLab migration of
`sok/splunk-operator`. The review stack is intentionally staged. The first MR proves only the minimum
`develop` lane that makes the canonical repo operational. This follow-up keeps that same lane behavior and
recasts the CI into a production-facing module layout so later MRs can add lanes without turning the root
pipeline file back into a single monolith.

The design rule is simple. The top-level [`.gitlab-ci.yml`](../.gitlab-ci.yml) stays thin and owns only entry
rules plus local includes. Shared defaults and reusable hidden templates live in
[`gitlab-ci/includes/base.yml`](includes/base.yml). Repository verification and test jobs live in
[`gitlab-ci/includes/baseline.yml`](includes/baseline.yml). Runtime jobs that stage an image, scan that exact
artifact, and validate it in EKS live in [`gitlab-ci/includes/runtime.yml`](includes/runtime.yml). The visible
jobs remain explicit so a reviewer can still read the active lane from top to bottom, while the hidden
templates provide the pluggable CI pattern boundary for future lanes.

This split is deliberately responsibility-based rather than tool-based. A future nightly, qualification, or
release lane should plug into the same model by adding another include file and extending the hidden template
family it needs. The repo should not go back to large inline shell blocks, dynamic job assembly, or opaque
script indirection. Job behavior belongs in checked-in scripts under [`gitlab-ci/`](.) and
[`gitlab-ci/lib/`](lib), versioned beside the code they validate.

The current active canonical lane is still the narrow `develop` path only. It performs repository formatting
and vetting, Go unit coverage, kubectl-splunk test coverage, staged operator image build, staged image Trivy
scan, and a bounded EKS smoke run. Nightly, qualification, release, variant-runtime, mirror, intake, and
rollback remain separate follow-up MRs. That boundary is deliberate. The goal of the current stack is to prove
the smallest production-relevant GitLab path before enabling broader operating modes.

The checked-in runtime scripts define the data contract between jobs. [`gitlab-ci/build-test-push.sh`](build-test-push.sh)
builds the operator image for the current commit, pushes it only to the staging ECR path, and writes the image
reference, digest, and runtime context into `ci-output/`. [`gitlab-ci/build-test-push-trivy-scan.sh`](build-test-push-trivy-scan.sh)
consumes that exact staged image artifact so the scan validates the same image that was built. [`gitlab-ci/int-test-workflow.sh`](int-test-workflow.sh)
consumes the same image reference, resolves the Splunk Enterprise image into staging ECR, provisions the
ephemeral EKS environment, runs the bounded `managersecret-smoke-s1` focus, and emits runtime evidence under
`ci-output/`, including runtime context, cluster logs, cleanup logs, copied pod logs, and JUnit-style output.

The production-readiness rule for follow-up work is that a structural MR should only improve composition and
maintainability. It should not silently widen the runtime matrix, add credentials, or alter which environments
are provisioned. New behavior belongs in a new lane MR with its own review boundary. That is how we keep the
port reviewable and avoid mixing parity work with new capability.

The diagrams in [`gitlab-ci/diagrams/`](diagrams) show the current `develop` lane and the intended later
qualification and release flows. The develop diagram reflects what is active today. The qualification and
release diagrams describe the target operating model for later MRs and should not be read as already-enabled
behavior in the canonical repo.

![Develop Lane](diagrams/develop-lane.png)

![Qualification Lane Target](diagrams/qualification-lane-target.png)

![Release Lane Target](diagrams/release-lane-target.png)

The planned migration-owned sequence after this MR remains straightforward. Security template adoption should
land without unexpectedly widening the proven `develop` lane. After that, nightly, qualification, release, and
then mirror, intake, and rollback can each land in their own review slices. Migration-owned branches should
use the owning `CSPL-xxxx` key in the branch name so Jira, Git history, and MR review can be correlated
directly. Pipeline-driving pushes and automation changes should continue to use the shared non-personal bot
identity rather than a developer account wherever practical.
