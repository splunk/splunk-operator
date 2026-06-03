---
title: Build and Test
parent: Internal Onboarding
nav_order: 3
---

# Build and Test

Start with the public [Development Setup](../develop/DevelopmentSetup.html) guide. It is the source of truth for prerequisites, common Make targets, local build/deploy commands, unit tests, and debugging.

This page only covers Splunk-internal additions.

## Internal Repository

Use this internal https://cd.splunkdev.com/sok/splunk-operator repo as the development repo for code changes.

```
git clone git@cd.splunkdev.com:sok/splunk-operator.git
cd splunk-operator
```

## Internal Setup Additions

### Artifactory Authentication

Each push to a branch with an open MR pushes a multi-platform built image to docker-test.repo.splunkdev.net. Images are named `docker-test.repo.splunkdev.net/sok/splunk-operator:<CI_COMMIT_SHA>`.

To pull images to use locally, run `okta-artifactory-login` to authenticate.

### SPLUNK_GENERAL_TERMS value

**Note:** Customers are redirected to the README to find the value for the `SPLUNK_GENERAL_TERMS` environment variable. This is so they see the link to the terms they are accepting. For developer use, the value is included here.

Use `make deploy IMG=<image> SPLUNK_ENTERPRISE_IMAGE=<splunk enterprise image> SPLUNK_GENERAL_TERMS=--accept-sgt-current-at-splunk-com ENVIRONMENT=debug` to deploy the Splunk Operator on a Kubernetes cluster.

## Internal Integration Testing Setup

### Obtaining a Splunk Enterprise License

Integration tests that exercise licensed features (License Manager, App Framework, SmartStore, etc.) require an Enterprise license file. Without one, Splunk instances start with a trial license, which is sufficient for basic smoke tests but not for full integration coverage.

Download the current NFR license from the [Internal NFR License Keys](https://splunk.atlassian.net/wiki/spaces/PROD/pages/313538952312) Confluence page.

Save the `.lic` file to a secure location on your machine (e.g., `~/.splunk/enterprise.lic`). Do not commit it to any repository.

If you need a license with different parameters (higher volume, specific features), the NFR page has details on the SNOW request process.

#### Using the License with Integration Tests

Pass the license file to the test framework using either approach:

```bash
# CLI flag
ginkgo -v --license-file=$HOME/.splunk/enterprise.lic ...

# Environment variable (used by CI and test/env.sh)
export ENTERPRISE_LICENSE_LOCATION=$HOME/.splunk/enterprise.lic
make int-test
```

CI-style runs use cloud-hosted license material. For EKS, `test/trigger-tests.sh` defaults `ENTERPRISE_LICENSE_LOCATION` to `ENTERPRISE_LICENSE_S3_PATH`, which defaults to `test_licenses/`, and downloads `enterprise.lic` from the configured test bucket.

### EKS cluster provisioning

There are two supported internal paths for EKS test clusters:

- **Kraken shared infrastructure:** Request a cluster through the Kraken team. Use the [#kraken](https://splunk.enterprise.slack.com/archives/C0AE70QE17U) Slack channel for onboarding and provisioning. This is currently in a Closed Beta phase, but is in active development. <!-- TODO: update this when a new process is available. -->
- **Team-owned AWS account:** provision an EKS with Terraform from the [infra-resources](https://cd.splunkdev.com/splunk-operator/infra-resources) repository. This is the recommended path for team-owned infrastructure because it gives better reproducibility and state management.
  - For SOK team members, new hires on the SOK team can [request access through the okta group](https://splunk.atlassian.net/wiki/spaces/PROD/pages/1078700413461/AWS+Account+for+New+Hires+Partner+Teams).
  - For other teams, reach out to team leaders to see if your team has an AWS account to use.

The Makefile still has `make cluster-up` and `make cluster-down`, but prefer Terraform for long-lived or team-owned test infrastructure.

## Internal CI expectations

Current GitLab pipeline expectations:

- Pipelines are defined in `.gitlab-ci.yml` with shared includes under `gitlab-ci/includes/`. The stage order is `verify`, `test`, `build`, `image_scan`, `security`, `integration`, `qualification`, `release`, and `publish`.
- Merge request pipelines run the default validation lane: MR description check, `format-and-vet` (`make fmt`, `make vet`, `git diff --exit-code`), unit tests (`make setup/ginkgo`, `make test`, coverage/JUnit artifacts), `kubectl-splunk` Python tests, Helm lint/unit tests, OSS scan, stage-image build, and container scan.
- Runtime smoke validation fans out by suite on EKS. Each smoke job gets its own cluster and runs through `gitlab-ci/int-test-workflow.sh`.
- Scheduled `develop` pipelines run the nightly integration lane, also fanned out one suite/job and one EKS cluster/job.
- Release and qualification lanes are explicit `SOK_PIPELINE_MODE` workflows. They validate released/staged SOK inputs across EKS, Helm/KUTTL, Azure, GCP, optional FIPS, distroless, and Graviton/arm64 paths, then publish qualification evidence and use `qualification-gate` as the authoritative go/no-go signal.
- Publish jobs are intentionally gated: normal release publishing runs from merged `main`; maintenance publishing runs from a protected `release/*`/`release-*` branch with `SOK_PIPELINE_MODE=release_publish`.
- CI artifacts include coverage summaries, JUnit reports, container/security scan evidence, `ci-output/` runtime logs, release/qualification evidence, and compatibility publication artifacts.
- Branch push pipelines with an open MR are suppressed to avoid duplicate validation; merge commits, `develop`, `main`, and release branches keep their own lanes.
