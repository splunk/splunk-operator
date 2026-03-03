# Change Intent: Doc-First, Commit Discipline, and AppFramework Parity Gates

## Intent
- Add three governance gates to make agent-driven development more reviewable and deterministic:
  - doc-first change intent enforcement
  - incremental commit discipline enforcement
  - appframework parity evidence enforcement

## Scope
- In scope:
  - new scripts under `scripts/dev/` for the three governance gates
  - `pr_check.sh` integration
  - harness governance docs/evals/manifests updates
  - PR templates and make targets updates
- Out of scope:
  - changing operator runtime behavior
  - changing existing feature logic or CRD semantics

## Constitution Impact
- Risk tier expectation: `medium` (governance/pipeline behavior update).
- This strengthens governance and review evidence requirements for non-trivial changes.

## Harness Coverage Plan
- `scripts/dev/spec_check.sh --base-ref develop`
- `scripts/dev/harness_manifest_check.sh --base-ref develop`
- `scripts/dev/doc_first_check.sh --base-ref develop`
- `scripts/dev/commit_discipline_check.sh --base-ref develop`
- `scripts/dev/appframework_parity_check.sh --base-ref develop`
- `scripts/dev/harness_eval.sh --suite docs/agent/evals/policy-regression.yaml`
- `scripts/dev/pr_check.sh --fast`

## Test Plan
- Shell syntax checks for new scripts via `scripts/dev/script_sanity_check.sh`.
- Full governance fast-pass via `PR_CHECK_FLAGS=--fast scripts/dev/pr_check.sh`.
- Validate policy regression suite includes new gate references.

## Implementation Log
- 2026-03-03: Added new governance check scripts and integrated them into `pr_check.sh`.
- 2026-03-03: Added `docs/changes/` doc-first workflow artifacts and a helper creator script.
- 2026-03-03: Updated harness docs, eval suite, and manifest required commands.
