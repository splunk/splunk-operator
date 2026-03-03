---
name: sok-commit-pr
description: Commit changes and create/update GitHub draft PRs for Splunk Operator. Use after tests pass and the change summary is ready.
---

# SOK Commit PR

## Overview
Apply a consistent commit and draft PR workflow aligned with splcore commit/MR discipline, adapted for GitHub.

## Preconditions
- Local branch is based on the intended target branch.
- Required local checks are complete (at minimum `scripts/dev/pr_check.sh`).
- Git identity is configured correctly for the branch owner.

## Workflow
1. Verify branch and working tree: `git status`, `git branch --show-current`.
2. Stage only in-scope files (`git add -p` preferred).
3. Commit with ticket-first subject and concise body.
4. Push branch: `git push -u origin <branch>`.
5. Create or update draft PR with `gh pr create --draft` (or `gh pr edit` if PR exists).
6. Ensure PR description includes: summary, tests, risks, rollback notes.

## Pass / Fail Criteria
- Pass: commit exists on remote branch and draft PR is created/updated with required sections.
- Fail: push or PR creation/edit fails, or required PR metadata is missing.

## Output Contract
- Changed files
- Commands run
- Results
- PR-ready summary
