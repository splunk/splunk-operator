# Commit Discipline

For implementation-heavy changes, prefer multiple reviewable commits instead of one monolithic commit.

## Gate
- `scripts/dev/commit_discipline_check.sh`

## Rules
- Minimum `2` non-merge commits for implementation-gated paths.
- Single-commit exceptions require `[single-commit-ok]` in commit message with justification.
- Oversized commits fail when changed file count exceeds configured threshold.

## Why
- Improves code review quality.
- Helps isolate regressions and revert safely.
