---
title: Contributing
parent: Develop & Contribute
nav_order: 1
---

# Contributing to the Project

This document is the single source of truth on contributing towards this codebase. Feel free to browse the [open issues](https://github.com/splunk/splunk-operator/issues) and file new ones - all feedback is welcome!

## Prerequisites

When contributing to this repository, first discuss the issue with a [repository maintainer](#maintainers) via GitHub issue, Slack message, or email.

#### Contributor License Agreement

We only accept pull requests submitted from:
* Splunk employees
* Individuals who have signed the [Splunk Contributor License Agreement](https://www.splunk.com/en_us/form/contributions.html)

#### Code of Conduct

All contributors are expected to read the [Splunk Community Code of Conduct](https://www.splunk.com/en_us/community/code-of-conduct.html) and observe it in all interactions involving this project.

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

#### Pull requests

A pull request informs the project's core developers about the changes you want to review and merge. Once you submit a pull request, it enters a stage of code review where you and others can discuss its potential modifications and add more commits later on.

To learn more, see [Proposing changes to your work with pull requests
](https://help.github.com/en/github/collaborating-with-issues-and-pull-requests/proposing-changes-to-your-work-with-pull-requests) in the [GitHub Help Center](https://help.github.com/).

To make a pull request against this project:
1. Fork the [splunk-operator GitHub repository](https://github.com/splunk/splunk-operator/)
1. Clone your fork and create a branch off of `develop`
    ```
    # Create a local copy (or clone) of the repository
    $ git clone git@github.com:YOUR_GITHUB_USERNAME/splunk-operator.git
    $ cd splunk-operator

    # Create your feature/bugfix branch
    $ git checkout -b your-branch-name develop
    ```
1. Run tests to verify your environment
    ```
    $ cd splunk-operator
    $ make test
    ```
1. Push your changes once your tests have passed
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

> This section is for project **maintainers**. External contributors only need the fork-and-PR steps above; a maintainer takes it from there.

External contributions arrive as pull requests on GitHub. The GitHub repository is a **read-only, sanitized mirror** of the authoritative internal repository: every mirror run strips internal-only paths and **rewrites commit SHAs**, so the GitHub and internal `develop` branches never share commit SHAs. Two consequences shape this workflow:

- The full test suite (integration tests with cloud credentials, private clusters, performance environments) runs **only on the internal pipeline**, never on a fork.
- A contribution must be **cherry-picked** onto the internal `develop` — never pushed as-is. The PR branch is built on GitHub's sanitized history, so pushing it to the internal repo would delete the internal-only files and roll back unrelated files.

The commands below assume a clone of the **internal repository** (so `origin` is the authoritative remote) with two extra remotes added:

```bash
# origin       -> internal repo (authoritative; full CI + internal-only paths)
# github       -> read-only GitHub mirror
$ git remote add github https://github.com/splunk/splunk-operator.git
# <fork>       -> the external contributor's GitHub fork
$ git remote add <fork> https://github.com/CONTRIBUTOR_USERNAME/splunk-operator.git
```

1. **Initial review (safety)**: Before running any contributor code on internal infrastructure, thoroughly review the PR. Check for:
   - **Security concerns**: No malicious code, credential harvesting, or unauthorized access attempts
   - **Resource safety**: No code that could damage or overload internal infrastructure
   - **Code quality**: Adherence to project standards and coding conventions
   - **Overall approach**: Changes align with project goals and architecture

   **Only proceed if you are confident the changes are safe to run on internal infrastructure.**

2. **Bridge the PR onto the internal repo (cherry-pick, never raw-push)**: Replay only the contributor's commits onto a fresh branch off the internal `develop`:
   ```bash
   $ git fetch origin develop
   $ git fetch github develop
   $ git fetch <fork> THEIR_BRANCH

   # Branch off the authoritative internal develop
   $ git checkout -b external/CONTRIBUTOR/THEIR_BRANCH origin/develop

   # Replay ONLY the contributor's commits (everything they added on top of GitHub develop)
   $ git cherry-pick github/develop..<fork>/THEIR_BRANCH

   # Push the bridge branch to the internal repo and open an MR targeting develop
   $ git push origin external/CONTRIBUTOR/THEIR_BRANCH
   ```
   Cherry-pick preserves the original author (you become the committer) and leaves the internal-only paths untouched. If the contributor's changes conflict with public-file drift since they forked, resolve the conflicts, `git add`, and `git cherry-pick --continue`.

   > **Do not** `git push` the contributor's branch directly to the internal repo. It is based on GitHub's sanitized history, so it would delete the internal-only directories and revert unrelated files.

3. **Run CI/CD on the internal MR**: The MR pipeline runs with full access to internal test infrastructure (integration tests requiring cloud provider credentials, private test clusters, internal performance testing environments).

4. **Review test results**: Monitor the pipeline execution and review all test results.

5. **Communicate findings**: If tests fail or changes are needed, comment on the **original GitHub PR** (where the contributor works) and request changes. When the contributor pushes updates to their fork, rebuild the bridge branch from scratch so the replay stays clean:
   ```bash
   $ git fetch <fork> THEIR_BRANCH
   $ git fetch origin develop
   $ git checkout external/CONTRIBUTOR/THEIR_BRANCH
   $ git reset --hard origin/develop
   $ git cherry-pick github/develop..<fork>/THEIR_BRANCH
   $ git push --force-with-lease origin external/CONTRIBUTOR/THEIR_BRANCH
   ```

6. **Merge on the internal repo, then close the GitHub PR manually**: Once the MR has the required approvals and a green build (see [Code Review](#code-review)):
   - Merge the **internal MR** into `develop`. This is the real merge — never merge the GitHub PR, which is downstream and would be overwritten by the next mirror run.
   - The next mirror run publishes the merged change to GitHub `develop`, preserving the contributor's authorship.
   - The GitHub PR will **not** auto-close (its commit SHA was rewritten by the mirror), so close it manually with a comment noting it was merged via the internal pipeline. Referencing the PR number in the merge commit leaves a back-link for traceability.
   - Delete the bridge branch:
   ```bash
   $ git push origin --delete external/CONTRIBUTOR/THEIR_BRANCH
   ```

**Important Notes:**
- Always keep external contributors informed throughout the process.
- The `external/...` bridge branch is temporary; delete it after the MR is merged or closed.
- This workflow ensures external contributions receive the same level of testing as internal changes.
- Merge on the internal repo and close the GitHub PR manually — never merge the GitHub PR directly.

#### Rebasing your branch

Keep your branch up to date with `develop` using rebase (not merge) to maintain a linear history:
```
$ git fetch origin
$ git rebase origin/develop
$ git push --force-with-lease
```
If you hit conflicts, resolve them, `git add` the files, and `git rebase --continue` (or `--abort` to start over). Always use `--force-with-lease` instead of `--force` to avoid overwriting others' work. Squashing is not required, but before merging ensure the branch contains only meaningful commits — either use GitHub's "Squash and merge" option or clean up locally with `git rebase -i HEAD~N`.

#### Code Review

There are two aspects of code review: giving and receiving.

A PR is easy to review if you:
* Follow the project coding conventions
* Write good commit messages, concise and descriptive
* Break large changes into a logical series of smaller patches. Patches individually make easily understandable changes, and in aggregate, solve a broader issue

Reviewers are highly encouraged to revisit the [Code of Conduct](https://www.splunk.com/en_us/community/code-of-conduct.html) and must go above and beyond to promote a collaborative, respectful community.

When reviewing PRs from others, [The Gentle Art of Patch Review](http://sage.thesharps.us/2014/09/01/the-gentle-art-of-patch-review/) suggests an iterative series of focuses, designed to lead new contributors to positive collaboration without inundating them initially with nuances:
* Is the idea behind the contribution sound?
* Is the contribution architected correctly?
* Is the contribution polished?

Merge requirements for this project:
* at least 2 approvals
* a passing build from our continuous integration system

Any new commits to an open pull request will automatically dismiss old reviews and trigger another build.

#### Testing

Testing is the responsibility of all contributors. To run unit tests:
```
$ make test
```

For integration and end-to-end testing, see the [Integration Testing](IntegrationTesting) guide.

#### Documentation

We can always use improvements to our documentation! Anyone can contribute to these docs, whether you identify as a developer, an end user, or someone who just can't stand seeing typos. What exactly is needed?

1. More complementary documentation. Have you found something unclear?
1. More examples or generic templates that others can use
1. Blog posts, articles and such – they're all very appreciated

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
() Kriti Ashok
() Param Dhanoya
() Victor Ebken
() Ajeet Kumar
() Jeff Rybczynski
() Patrick Ogdin

```
