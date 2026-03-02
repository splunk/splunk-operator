---
title: CONTRIBUTING
nav_order: 7
---

# Contributing to the Project

This document is the single source of truth on contributing towards this codebase. Feel free to browse the [open issues](https://github.com/splunk/splunk-operator/issues) and file new ones - all feedback is welcome!

## Navigation

- [Contributing to the Project](#contributing-to-the-project)
  - [Navigation](#navigation)
  - [Prerequisites](#prerequisites)
      - [Contributor License Agreement](#contributor-license-agreement)
      - [Code of Conduct](#code-of-conduct)
  - [Contribution Workflow](#contribution-workflow)
      - [Bug reports and feature requests](#bug-reports-and-feature-requests)
      - [Fixing issues](#fixing-issues)
      - [Spec-First Workflow](#spec-first-workflow)
      - [Pull requests](#pull-requests)
      - [Maintainer Workflow for External Contributions](#maintainer-workflow-for-external-contributions)
      - [Code Review](#code-review)
      - [Testing](#testing)
      - [Documentation](#documentation)
  - [Maintainers](#maintainers)

## Prerequisites
When contributing to this repository, first discuss the issue with a [repository maintainer](#maintainers) via GitHub issue, Slack message, or email.

#### Contributor License Agreement
We only accept pull requests submitted from:
* Splunk employees
* Individuals who have signed the [Splunk Contributor License Agreement](https://www.splunk.com/en_us/form/contributions.html)

#### Code of Conduct
All contributors are expected to read our [Code of Conduct](../CODE_OF_CONDUCT.md) and observe it in all interactions involving this project.

## Contribution Workflow
Help is always welcome! For example, documentation can always use improvement. There's always code that can be clarified, functionality that can be extended, and tests to be added to guarantee behavior. If you see something you think should be fixed, don't be afraid to own it.

#### Bug reports and feature requests
Have ideas on improvements? See something that needs work? While the community encourages everyone to contribute code, it is also appreciated when someone reports an issue. Please report any issues or bugs you find through our [issue tracker](https://github.com/splunk/splunk-operator/issues).

If you are reporting a bug, please include:
* Your operating system name and version
* Details about your local setup that might be helpful in troubleshooting (e.g. Kubernetes Version, Container Platform, Ansible version, etc.)
* Detailed steps to reproduce the bug

We'd also like to hear your feature suggestions. Feel free to submit them as issues by:
* Explaining in detail how they should work
* Keeping the scope as narrow as possible. This will make it easier to implement

#### Fixing issues
Look through our [issue tracker](https://github.com/splunk/splunk-operator/issues) to find problems to fix! Feel free to comment and tag corresponding stakeholders or full-time maintainers of this project with any questions or concerns.

#### Spec-First Workflow
For non-trivial changes, start with Spec Kit + KEP-lite before implementation:
1. Bootstrap planning, KEP, and manifest:
   ```bash
   $ scripts/dev/speckit_bridge.sh bootstrap --change-id CSPL-XXXX --title "your title"
   ```
2. Refine `speckit/specs/<id>-<slug>/spec.md`, `plan.md`, and `tasks.md`.
3. Fill KEP sections and request review.
4. Move KEP status to `Approved` before implementation.
5. Use `risk_tier` and scope in `harness/manifests/*.yaml` as execution policy.
6. Keep KEP in sync and move status to `Implemented` when merged.
7. Add one PR label that matches manifest risk tier (`risk:low|risk:medium|risk:high`).

The PR harness enforces this with:
```bash
$ scripts/dev/spec_check.sh
$ scripts/dev/harness_manifest_check.sh
$ scripts/dev/risk_policy_check.sh
$ scripts/dev/risk_label_check.sh --labels risk:<tier>
```

#### Pull requests
A pull request informs the project's core developers about the changes you want to review and merge. Once you submit a pull request, it enters a stage of code review where you and others can discuss its potential modifications and add more commits later on.

To learn more, see [Proposing changes to your work with pull requests
](https://help.github.com/en/github/collaborating-with-issues-and-pull-requests/proposing-changes-to-your-work-with-pull-requests) in the [GitHub Help Center](https://help.github.com/).

To make a pull request against this project:
1. Fork the [splunk-operator GitHub repository](https://github.com/splunk/splunk-operator/).
1. Clone your fork and create a branch off of `develop`.
    ```
    # Create a local copy (or clone) of the repository
    $ git clone git@github.com:YOUR_GITHUB_USERNAME/splunk-operator.git
    $ cd splunk-operator

    # Create your feature/bugfix branch
    $ git checkout -b your-branch-name develop
    ```
1. Create or update the governing spec for non-trivial changes.
    ```bash
    $ scripts/dev/speckit_bridge.sh bootstrap --change-id CSPL-XXXX --title "your title"
    ```
1. Run harness and tests to verify your changes.
    ```
    $ cd splunk-operator
    $ scripts/dev/spec_check.sh
    $ scripts/dev/harness_manifest_check.sh
    $ scripts/dev/risk_policy_check.sh
    $ scripts/dev/risk_label_check.sh --labels risk:<tier>
    $ scripts/dev/harness_eval.sh --suite docs/agent/evals/policy-regression.yaml
    $ scripts/dev/harness_run.sh --fast
    $ scripts/dev/pr_check.sh
    $ scripts/dev/autonomy_scorecard.sh --base-ref develop
    $ make test
    ```
1. Push your changes once your tests have passed.
    ```
    # Add the files to the queue of changes
    $ git add <modified file(s)>

    # Commit the change to your repo with a log message
    $ git commit -m "<helpful commit message>"

    # Push the change to the remote repository
    $ git push
    ```
1. Submit a pull request through the GitHub website using the changes from your forked codebase

#### Maintainer Workflow for External Contributions

When a pull request is submitted from a forked repository (external contributor), the CI/CD pipeline running on the fork will not have access to our internal test infrastructure. To properly test these contributions, maintainers should follow this workflow:

1. **Initial Review**: Before running any code on internal infrastructure, thoroughly review the pull request to verify it is safe to execute. Check for:
   - **Security concerns**: No malicious code, credential harvesting, or unauthorized access attempts
   - **Resource safety**: No code that could damage or overload internal infrastructure
   - **Code quality**: Adherence to project standards and coding conventions
   - **Overall approach**: Changes align with project goals and architecture
   
   **Only proceed to the next step if you are confident the changes are safe to run on internal infrastructure.**

2. **Pull and Push to Internal Branch**: Once the initial review is satisfactory and you want to run the full test suite:
   ```bash
   # Add the contributor's fork as a remote (one-time setup per contributor)
   $ git remote add contributor-name https://github.com/CONTRIBUTOR_USERNAME/splunk-operator.git

   # Fetch the contributor's branch
   $ git fetch contributor-name their-branch-name

   # Create a new branch in the main repository based on their work
   $ git checkout -b external/contributor-name/their-branch-name contributor-name/their-branch-name

   # Push to the main repository
   $ git push origin external/contributor-name/their-branch-name
   ```

3. **Run CI/CD Pipeline**: The pipeline will now run with full access to internal test infrastructure, including:
   - Integration tests requiring cloud provider credentials
   - Access to private test clusters
   - Internal performance testing environments

4. **Review Test Results**: Monitor the pipeline execution and review all test results.

5. **Communicate Findings**: If tests fail or changes are needed:
   - Comment on the original pull request with detailed feedback
   - Request changes from the contributor
   - Once the contributor updates their PR, repeat this process

6. **Merge**: Once all tests pass and the code is approved:
   - Merge the original pull request from the fork (not the internal branch)
   - Delete the internal test branch
   ```bash
   $ git push origin --delete external/contributor-name/their-branch-name
   ```

**Important Notes:**
- Always maintain clear communication with external contributors about the testing process
- The internal test branch is temporary and should be deleted after the PR is merged or closed
- This workflow ensures external contributions receive the same level of testing as internal changes
- Never merge the internal test branch directly; always merge the original PR from the fork

#### Code Review
There are two aspects of code review: giving and receiving.

A PR is easy to review if you:
* Follow the project coding conventions.
* Write good commit messages, concise and descriptive.
* Break large changes into a logical series of smaller patches. Patches individually make easily understandable changes, and in aggregate, solve a broader issue.

Reviewers are highly encouraged to revisit the [Code of Conduct](contributing/code-of-conduct.md) and must go above and beyond to promote a collaborative, respectful community.

When reviewing PRs from others, [The Gentle Art of Patch Review](http://sage.thesharps.us/2014/09/01/the-gentle-art-of-patch-review/) suggests an iterative series of focuses, designed to lead new contributors to positive collaboration without inundating them initially with nuances:
* Is the idea behind the contribution sound?
* Is the contribution architected correctly?
* Is the contribution polished?

Merge requirements for this project:
* at least 2 approvals
* a passing build from our continuous integration system.

Any new commits to an open pull request will automatically dismiss old reviews and trigger another build.

#### Testing
Testing is the responsibility of all contributors. To run Unit Tests in Splunk Operator code, you can run below command -
```
$ make test
```

For agent-assisted or standardized local workflows, prefer the scripts under `scripts/dev/`:
- `scripts/dev/speckit_bridge.sh`
- `scripts/dev/spec_check.sh`
- `scripts/dev/harness_manifest_check.sh`
- `scripts/dev/risk_policy_check.sh`
- `scripts/dev/risk_label_check.sh`
- `scripts/dev/harness_eval.sh`
- `scripts/dev/harness_run.sh`
- `scripts/dev/autonomy_scorecard.sh`
- `scripts/dev/unit.sh`
- `scripts/dev/lint.sh`
- `scripts/dev/pr_check.sh`

#### Agentic Development Workflow
This repo includes skills and harness scripts to make common workflows repeatable.
Start with:
- `AGENTS.md` (root) for repo map and conventions
- `.agents/skills/*/SKILL.md` for skill workflows
- `docs/agent/README.md` for agent-focused guidance

#### Documentation
We can always use improvements to our documentation! Anyone can contribute to these docs, whether you identify as a developer, an end user, or someone who just can’t stand seeing typos. What exactly is needed?

1. More complementary documentation. Have you found something unclear?
1. More examples or generic templates that others can use.
1. Blog posts, articles and such – they’re all very appreciated.

You can also edit documentation files directly in the GitHub web interface, without creating a local copy. This can be convenient for small typos or grammar fixes.

## Maintainers

If you need help, tag one of the active maintainers of this project in a post or comment. We'll do our best to reach out to you as quickly as we can.

```
# Active maintainers marked with (*)

(*) Vivek Reddy
(*) Raizel Lieberman
(*) Patryk Wasielewski
(*) Igor Grzankowski
(*) Kasia Kozioł
(*) Jakub Buczak
(*) Qing Wang
(*) Gabriel Mendoza
(*) Minjie Qiu
(*) Yuhan Yang
() Sirish Mohan
() Gaurav Gupta
() Subba Gontla
() Arjun Kondur



```
