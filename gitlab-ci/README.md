# GitLab CI Operating Notes

This directory contains the checked-in runtime pieces for the canonical GitLab migration of
`sok/splunk-operator`. The current review stack is intentionally staged. The first MR proves only the
minimum `develop` lane needed to make the canonical repo operational. This follow-up keeps that same lane
behavior but reshapes the CI into a more maintainable, production-facing layout so future MRs can extend it
without collapsing back into one large YAML file.

The top-level [`.gitlab-ci.yml`](../.gitlab-ci.yml) should stay small. It owns only workflow entry rules and
the list of local include files. That keeps the review surface obvious: pipeline entry logic at the root,
shared defaults in one place, baseline verification in one place, and runtime jobs in one place. The actual
job behavior still lives in the checked-in scripts under [`gitlab-ci/`](.) and
[`gitlab-ci/lib/`](lib), not in large inline shell blocks.

## Current lane boundary

The active canonical migration lane is still the narrow `develop` path only. It is limited to:

- repository verification
- unit tests
- kubectl-splunk tests
- staged operator image build
- staged image Trivy scan
- bounded EKS smoke validation

Nightly, qualification, release, variant-runtime, mirror, intake, and rollback remain separate follow-up MRs.
That boundary is deliberate. The point of the current stack is to let the team verify the smallest production-
relevant GitLab path before adding broader operating modes.

## File layout

The local include split is by responsibility rather than by tool:

- [`gitlab-ci/includes/base.yml`](includes/base.yml)
  keeps global stages, default job image, and common top-level variables.
- [`gitlab-ci/includes/baseline.yml`](includes/baseline.yml)
  keeps the repository verification and test jobs.
- [`gitlab-ci/includes/runtime.yml`](includes/runtime.yml)
  keeps the staging build, image scan, and EKS smoke jobs.

That split is intentionally modest. It makes the current lane easier to read without inventing a large
abstraction layer too early.

The includes also use hidden template jobs as the reusable CI pattern boundary. Shared defaults such as Go
verification, Go test execution, Python venv setup, runtime artifact retention, and runtime-stage job families
live in [`gitlab-ci/includes/base.yml`](includes/base.yml). The visible jobs in the other include files extend
those templates instead of duplicating stage and artifact policy by hand. That keeps the current lane explicit
while still making later lanes pluggable.

## Runtime contract

[`gitlab-ci/build-test-push.sh`](build-test-push.sh) builds the operator image for the current commit and pushes
it only to the staging ECR path. It writes the image reference, digest, and runtime context into `ci-output/`
so downstream jobs validate the same artifact instead of rebuilding their own copy.

[`gitlab-ci/build-test-push-trivy-scan.sh`](build-test-push-trivy-scan.sh) consumes that staged image artifact
and runs the bounded image scan used in this first migration slice.

[`gitlab-ci/int-test-workflow.sh`](int-test-workflow.sh) consumes the staged operator image, resolves the
Splunk Enterprise image into staging ECR, provisions the ephemeral EKS environment, runs the bounded
`managersecret-smoke-s1` focus, and emits runtime evidence under `ci-output/`. The outputs include runtime
context, cluster logs, cleanup logs, copied pod logs, and JUnit-style test output.

## Why this MR exists

This restructure MR is meant to be the first maintainability follow-up after the minimal lane proves out. It
should not introduce new pipeline capabilities. If a change would alter which jobs run, which environments are
provisioned, or which credentials are required, that belongs in a later MR with its own review boundary.

The next planned migration-owned MRs after this one are:

1. security template and scanner adoption where it does not change the proven `develop` lane unexpectedly
2. nightly lane
3. qualification lane
4. release lane
5. mirror, intake, and rollback reapplication

## Branch and ownership rule

Migration-owned branches should use the owning `CSPL-xxxx` key in the branch name so Jira, Git history, and MR
review can be correlated directly. Pipeline-driving pushes and automation changes should continue to use the
shared non-personal bot identity rather than a developer account wherever practical.
