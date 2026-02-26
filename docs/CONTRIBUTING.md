---
title: CONTRIBUTING
nav_order: 7
---

# Contributing to Splunk Operator for Kubernetes

Thanks for contributing to Splunk Operator for Kubernetes (SOK).
This guide describes how to contribute changes that are easy to review, safe to run, and aligned with this repository's workflow.

## Table of Contents

- [Quick Start](#quick-start)
- [Before You Start](#before-you-start)
- [Local Development Setup](#local-development-setup)
- [Contribution Workflow](#contribution-workflow)
- [Testing Expectations](#testing-expectations)
- [Pull Request Expectations](#pull-request-expectations)
- [Maintainer Workflow for External Contributions](#maintainer-workflow-for-external-contributions)
- [Code Review and Merge Criteria](#code-review-and-merge-criteria)
- [Documentation Contributions](#documentation-contributions)
- [Security Reporting](#security-reporting)
- [Getting Help](#getting-help)

## Quick Start

1. Fork the repository and branch from `develop`.
1. Implement your change.
1. Run local validation:
   ```bash
   make fmt vet
   make test
   make build
   ```
1. If you changed API types (`api/**`), regenerate generated artifacts:
   ```bash
   make manifests generate
   ```
1. Open a PR against `develop` using the repository PR template.

## Before You Start

### Align on non-trivial changes

For medium/large features, behavior changes, or refactors, open or discuss an issue first.
This reduces rework and helps align scope, implementation, and testing strategy.

### Contributor License Agreement (CLA)

Contributions are accepted from:
- Splunk employees
- External contributors who have signed the Splunk CLA

CLA workflow is enforced by GitHub checks.

### Code of Conduct (COC)

All contributors must follow the Code of Conduct:
https://github.com/splunk/cla-agreement/blob/main/CODE_OF_CONDUCT.md

COC acceptance is also enforced by GitHub checks.

### How to satisfy CLA/COC checks

If checks fail, add the exact PR comments below:

- `I have read the CLA Document and I hereby sign the CLA`
- `I have read the Code of Conduct and I hereby sign the COC`

You can retrigger the agreements workflow with:

- `recheck`

## Local Development Setup

Key tool versions are maintained in `.env` (for example `GO_VERSION`, `OPERATOR_SDK_VERSION`).

Useful commands:

```bash
make help
make fmt
make vet
make test
make build
```

If you modify user-facing behavior, update documentation in `docs/`.

## Contribution Workflow

### Branching

- Base branch: `develop`
- Use one short-lived branch per change
- Keep each PR focused on one logical objective

Example:

```bash
git checkout develop
git pull
git checkout -b your-branch-name
```

### Commits

- Keep commits small and coherent
- Use clear commit messages
- Avoid mixing unrelated cleanup with functional changes

### Push and open PR

```bash
git add <files>
git commit -m "<concise and descriptive message>"
git push -u origin your-branch-name
```

Then open a PR against `develop`.

## Testing Expectations

At minimum, run for code changes:

```bash
make fmt vet
make test
make build
```

For API/CRD changes:

```bash
make manifests generate
```

Integration test suites are located under `test/` and workflow-based integration jobs run in GitHub Actions.
Use targeted test runs when possible and include evidence in your PR description.

## Pull Request Expectations

A strong PR includes:

- Clear problem statement and scope
- Summary of key changes
- Test evidence (commands + outcomes)
- Related issue/Jira/support references
- Documentation updates when behavior changes

Use `.github/pull_request_template.md` as the baseline checklist.

## Maintainer Workflow for External Contributions

PRs from forks do not have access to all internal credentials and environments.
When full internal validation is needed, maintainers should:

1. Perform an initial safety review (security, resource usage, overall approach).
1. Fetch contributor branch and push it to a temporary internal branch.
1. Run full CI/internal test workflows from that temporary branch.
1. Report results on the original PR and request updates if needed.
1. Merge the original PR when approved.
1. Delete the temporary internal branch.

Example:

```bash
git remote add contributor-name https://github.com/CONTRIBUTOR_USERNAME/splunk-operator.git
git fetch contributor-name their-branch-name
git checkout -b external/contributor-name/their-branch-name contributor-name/their-branch-name
git push origin external/contributor-name/their-branch-name
```

Cleanup:

```bash
git push origin --delete external/contributor-name/their-branch-name
```

Never merge the temporary internal branch directly.

## Code Review and Merge Criteria

Review principles:

- Validate design correctness first
- Validate behavior and test coverage second
- Validate polish/maintainability third

Current merge requirements:

- At least 2 approvals
- Passing required CI checks

New commits can dismiss previous approvals and retrigger checks.

## Documentation Contributions

Documentation updates are welcome and encouraged:

- Clarify existing behavior
- Add examples and troubleshooting
- Fix typos, stale instructions, or broken links

For docs-only changes, editing directly in GitHub is acceptable for small fixes.

## Security Reporting

Do not post sensitive vulnerability details publicly.
If you are a Splunk customer, report via Splunk Support:
https://www.splunk.com/support

For regular bug reports and non-sensitive issues, use GitHub Issues.

## Getting Help

Use GitHub Issues for questions, bugs, and enhancement discussions.
Please use the appropriate issue template in `.github/ISSUE_TEMPLATE/` whenever possible.
