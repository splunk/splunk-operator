# Harness Engineering Parity

Source reference: https://openai.com/index/harness-engineering/

## Product Profile
- Product: Splunk Operator for Kubernetes (SOK)
- Development mode: KEP-first + harness-governed execution
- Primary objective: keep agent-generated changes auditable, scoped, and test-backed

## Backend Parity Matrix
| ID | Capability | SOK Implementation | Status | Evidence |
|---|---|---|---|---|
| H01 | Spec-first workflow | Non-trivial changes require KEP in `docs/specs/` | Implemented | `docs/specs/README.md`, `scripts/dev/spec_check.sh` |
| H02 | Machine-readable execution policy | Harness manifests define scope/risk/commands | Implemented | `harness/manifests/*.yaml`, `scripts/dev/harness_manifest_check.sh` |
| H03 | Risk-tier governance | Risk tier enforces approvals/merge queue/command depth | Implemented | `scripts/dev/risk_policy_check.sh`, `docs/agent/BRANCH_POLICY.md` |
| H04 | PR label and policy consistency | PR labels must match manifest risk tier | Implemented | `scripts/dev/risk_label_check.sh`, `.github/workflows/pr-check.yml` |
| H05 | Doc-first implementation intent | Code-heavy changes require `docs/changes/*.md` with mandatory sections | Implemented | `scripts/dev/doc_first_check.sh`, `docs/changes/TEMPLATE.md` |
| H06 | Reviewable commit slicing | Implementation-heavy diffs require incremental commits or explicit override | Implemented | `scripts/dev/commit_discipline_check.sh`, `docs/agent/COMMIT_DISCIPLINE.md` |
| H07 | App framework parity guard | App framework code changes require parity docs + tests | Implemented | `scripts/dev/appframework_parity_check.sh`, `docs/agent/APPFRAMEWORK_PARITY.md` |
| H08 | KEP/component coverage validation | Impacted components must map to referenced approved IDs | Implemented | `scripts/dev/keps_check.sh`, `docs/specs/COMPONENT_KEP_INDEX.md` |
| H09 | Constitution and runtime issue governance | Always-on engineering rules + runtime issue lifecycle tracking | Implemented | `docs/engineering/CONSTITUTION.md`, `docs/testing/RUNTIME_ISSUE_TRACKER.md`, `scripts/dev/constitution_runtime_policy_check.sh` |
| H10 | Replayable policy regression checks | Governance contract validated via file-pattern eval suite | Implemented | `docs/agent/evals/policy-regression.yaml`, `scripts/dev/harness_eval.sh` |
| H11 | Harness run audit artifacts | Harness run emits trace/log/summary artifacts under `.harness/runs/` | Implemented | `scripts/dev/harness_run.sh`, `.harness/runs/` |
| H12 | Human-in-the-loop UI workflows | GitHub review UI for approvals/labels/merge queue remains required | N/A (No UI) | `docs/agent/BRANCH_POLICY.md`, `.github/workflows/merge-queue-check.yml` |

## No-UI Equivalents (Required)
SOK is backend-focused. UI-only controls from general harness guidance are represented as
policy + CI checks. Rows marked `N/A (No UI)` must still point to auditable evidence.

## Maintenance Rules
- Update this matrix whenever governance capability status changes.
- Keep every row status in: `Implemented`, `In Progress`, `Planned`, `N/A (No UI)`.
- Keep evidence column non-empty and repository-local where possible.
