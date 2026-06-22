---
title: Build and Test
parent: Internal Onboarding
nav_order: 3
---

# Build and Test

## Table of Contents

- [Internal Repository](#internal-repository)
- [Internal Setup Additions](#internal-setup-additions)
  - [Artifactory Authentication](#artifactory-authentication)
- [Internal Integration Testing Setup](#internal-integration-testing-setup)
  - [Obtaining a Splunk Enterprise License](#obtaining-a-splunk-enterprise-license)
  - [Test Cluster Provisioning](#test-cluster-provisioning)
  - [Debugging Test Cases](#debugging-test-cases)

---

Start with the public [Development Setup](../develop/DevelopmentSetup.md) guide. It is the source of truth for prerequisites, common Make targets, local build/deploy commands, unit tests, and debugging.

This page only covers Splunk-internal additions.

## Internal Repository

Use the internal [sok/splunk-operator](https://cd.splunkdev.com/sok/splunk-operator) GitLab repository as the development repository for code changes.

```bash
git clone git@cd.splunkdev.com:sok/splunk-operator.git
cd splunk-operator
```

## Internal Setup Additions

### Artifactory Authentication

Each push to a branch with an open MR pushes a multi-platform image to `docker-test.repo.splunkdev.net`. Images are named `docker-test.repo.splunkdev.net/sok/splunk-operator:<CI_COMMIT_SHA>`.

To pull images to use locally, run `okta-artifactory-login` to authenticate.

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

### Test Cluster Provisioning

There are two supported internal paths for integration test clusters:

- **Kraken shared infrastructure:** See [Set Up SOK with Kraken](./SetupWithKraken.md) to request a Kubernetes cluster and deploy the Splunk Operator. Use the [#kraken](https://splunk.enterprise.slack.com/archives/C0AE70QE17U) Slack channel for questions. This is currently in a closed beta phase and is under active development.
- **Team-owned AWS account:** Provision an EKS cluster with Terraform from the [infra-resources](https://cd.splunkdev.com/splunk-operator/infra-resources) repository. This is the recommended path for team-owned infrastructure because it gives better reproducibility and state management.
  - For SOK team members, new hires on the SOK team can [request access through the Okta group](https://splunk.atlassian.net/wiki/spaces/PROD/pages/1078700413461/AWS+Account+for+New+Hires+Partner+Teams).
  - For other teams, reach out to team leaders to see if your team has an AWS account to use.

The Makefile still has `make cluster-up` and `make cluster-down`, but prefer Terraform for long-lived or team-owned test infrastructure.

### Debugging Test Cases

Follow the [Troubleshooting](./Troubleshooting.md) guide for step-by-step instructions for troubleshooting integration test cases locally.
