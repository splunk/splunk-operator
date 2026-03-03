# Agent Harness Docs

These documents are the system of record for agent-assisted development in this repo.
They are short, concrete, and intended to be read by Codex skills and humans.

KEP-first governance lives in `docs/specs/`. For non-trivial implementation
changes, agents must link an approved KEP via `harness/manifests/*.yaml` and
pass harness checks.

## Index
- `../specs/` contains KEP-lite design docs and lifecycle state
- `../changes/` contains doc-first change-intent records for implementation work
- `CRD_MAP.md` maps kinds to API versions, types, controllers, and enterprise logic files
- `RECONCILE_FLOW.md` outlines the reconciliation flow, gates, and status phases
- `TEST_MATRIX.md` lists unit and integration test paths and environment variables
- `TESTCASE_SPEC.yaml` is a template for generating new integration/KUTTL tests
- `TESTCASE_PATTERNS.md` maps SVA patterns and features to test helpers
- `OPERATIONS.md` provides debug commands, log access, and pprof access notes
- `../../skaffold.yaml` defines shared local/CI deployment workflows
- `../../.devcontainer/` defines the reproducible local/agent development environment
- `RELEASE_FLOW.md` provides a concise release checklist and artifact map
- `HARNESS_CONTRACT.md` defines required harness gates and output contract
- `HARNESS_MANIFEST.md` defines the machine-readable harness manifest format
- `DOC_FIRST_WORKFLOW.md` defines required change-intent sections and flow
- `COMMIT_DISCIPLINE.md` defines commit slicing rules for implementation changes
- `APPFRAMEWORK_PARITY.md` defines appframework parity evidence rules
- `SKILL_ALIGNMENT.md` defines the shared skill contract and splcore-to-SOK mapping
- `SPECKIT_KEP_BRIDGE.md` explains Spec Kit to KEP to harness-manifest mapping
- `BRANCH_POLICY.md` defines branch protection and merge-queue requirements
- `evals/policy-regression.yaml` is the replayable governance regression corpus
- `.github/workflows/autonomy-scorecard.yml` publishes autonomy score reports
- `scripts/dev/autonomy_scorecard.sh` generates score metrics for current diff
- `scripts/dev/risk_label_check.sh` validates PR risk label versus manifest tier
