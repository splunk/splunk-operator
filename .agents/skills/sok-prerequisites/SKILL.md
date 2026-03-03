---
name: sok-prerequisites
description: Prepare local prerequisites for Splunk Operator development. Use before build/test/CI-fix tasks that require toolchain, cluster tooling, and repo scripts.
---

# SOK Prerequisites

## Overview
Validate the local environment and repo prerequisites before running build or test workflows.

## Preconditions
- Run from the repository root or any subdirectory inside the repository.

## Workflow
1. Confirm repository root and current branch: `git rev-parse --show-toplevel`, `git branch --show-current`.
2. Check required tools: `go`, `make`, `docker`, `kubectl`, `python3`.
3. Check optional-but-common tools: `kind`, `skaffold`, `operator-sdk`, `ginkgo`, `gh`.
4. Print versions for tools that are present.
5. Verify baseline scripts are executable: `scripts/dev/pr_check.sh`, `scripts/dev/unit.sh`, `scripts/verify_repo.sh`.
6. If prerequisites are missing, stop and provide exact install/fix actions.

## Pass / Fail Criteria
- Pass: required tools are available and baseline scripts are executable.
- Fail: one or more required tools are missing or scripts are not runnable.

## Output Contract
- Changed files
- Commands run
- Results
- PR-ready summary
