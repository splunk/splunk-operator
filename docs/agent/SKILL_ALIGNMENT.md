# Skill Alignment (splcore <-> SOK)

This document standardizes how Codex skills are written in this repository so
contributors can move between `~/main` (splcore) and `splunk-operator` with the
same execution model.

## Shared Skill Contract

Every `SKILL.md` under `.agents/skills/` must include:
- YAML frontmatter with `name` and `description`
- `## Overview`
- `## Preconditions`
- `## Workflow`
- `## Pass / Fail Criteria`
- `## Output Contract`

This mirrors the practical structure used in splcore skills:
- predictable start (`Preconditions`)
- deterministic execution (`Workflow`)
- explicit completion signal (`Pass / Fail Criteria`)
- consistent reporting (`Output Contract`)

The contract is enforced by `scripts/dev/skill_lint.sh` and included in
`scripts/dev/pr_check.sh`.

## Naming Alignment

SOK keeps `sok-*` prefixes to avoid collisions with globally installed skills,
but follows splcore conventions for base developer workflows.

| splcore pattern | SOK equivalent | Purpose |
| --- | --- | --- |
| `splcore-prerequisites` | `sok-prerequisites` | Validate local tooling and repo prerequisites |
| `splcore-build` | `sok-build` | Build operator artifacts/images |
| `splcore-test` | `sok-test` | Run standard local test flows |
| `splcore-commit-mr` | `sok-commit-pr` | Commit/push and create draft PR workflow |

Domain-specific SOK skills remain separate (for example:
`sok-feature-scaffold`, `sok-reconcile-debugger`, `sok-testcase-builder`).

## Output Contract (Standard)

All skills should end with the same reporting block:
- Changed files
- Commands run
- Results
- PR-ready summary

## Review Policy

When adding or updating skills:
- keep the skill body concise and task-focused
- move deep reference material to `references/` or docs under `docs/agent/`
- prefer deterministic scripts under `scripts/dev/` for repeated commands
