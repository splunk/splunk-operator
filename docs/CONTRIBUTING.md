# Contributing to the Project

This document is the single source of truth on contributing towards this codebase. Feel free to browse the [open issues](https://github.com/splunk/splunk-operator/issues) and file new ones - all feedback is welcome!

## Navigation

* [Prerequisites](#prerequisites)
    * [Contributor License Agreement](#contributor-license-agreement)
    * [Code of Conduct](#code-of-conduct)
* [Contribution Workflow](#contribution-workflow)
    * [Bug reports and feature requests](#bug-reports-and-feature-requests)
    * [Fixing issues](#fixing-issues)
    * [Pull requests](#pull-requests)
    * [Code Review](#code-review)
    * [Testing](#testing)
    * [Documentation](#documentation)
* [Maintainers](#maintainers)

## Prerequisites
When contributing to this repository, first discuss the issue with a [repository maintainer](#maintainers) via GitHub issue, Slack message, or email.

#### Contributor License Agreement
We only accept pull requests submitted from:
* Splunk employees
* Individuals who have signed the [Splunk Contributor License Agreement](https://www.splunk.com/en_us/form/contributions.html)

#### Code of Conduct
All contributors are expected to read our [Code of Conduct](contributing/code-of-conduct.md) and observe it in all interactions involving this project.

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
1. Fork the [splunk-operator GitHub repository](https://github.com/splunk/splunk-operator/).
1. Clone your fork and create a branch off of `develop`.
    ```
    # Create a local copy (or clone) of the repository
    $ git clone git@github.com:YOUR_GITHUB_USERNAME/splunk-operator.git
    $ cd splunk-operator

    # Create your feature/bugfix branch
    $ git checkout -b your-branch-name develop
    ```
1. Run tests to verify your environment.
    ```
    $ cd splunk-operator
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

(*) Mike Dickey - admin
(*) Sirish Mohan - admin
(*) Gaurav Gupta
(*) Subba Gontla
(*) Arjun Kondur
(*) Kriti Ashok
(*) Param Dhanoya
() Victor Ebken
() Ajeet Kumar
() Marcus Galdino
() Jeff Rybczynski
() Patrick Ogdin

```
