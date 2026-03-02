# Spec Kit to KEP Bridge

This bridge keeps planning (`speckit/`), design governance (`docs/specs/`),
and execution policy (`harness/manifests/`) synchronized.

## Bootstrap
Run:

```bash
scripts/dev/speckit_bridge.sh bootstrap \
  --change-id CSPL-5000 \
  --title "Indexer rollout validation improvements" \
  --risk-tier medium \
  --delivery-mode agent
```

Generated artifacts:
- `speckit/specs/CSPL-5000-.../spec.md`
- `speckit/specs/CSPL-5000-.../plan.md`
- `speckit/specs/CSPL-5000-.../tasks.md`
- `docs/specs/CSPL-5000-....md`
- `harness/manifests/CSPL-5000-....yaml`

## Required Progression
1. Refine Spec Kit docs and KEP draft.
2. Review and move KEP to `Status: Approved`.
3. Execute implementation scoped by manifest `allowed_paths`.
4. Run harness checks:
   - `scripts/dev/spec_check.sh`
   - `scripts/dev/harness_manifest_check.sh`
   - `scripts/dev/risk_policy_check.sh`
   - `scripts/dev/harness_eval.sh --suite docs/agent/evals/policy-regression.yaml`
   - `scripts/dev/harness_run.sh --fast`
   - `scripts/dev/pr_check.sh --fast`

## Output Contract
Each implementation PR should include:
- Spec Kit path
- KEP path and status
- Manifest path and risk tier
- Harness command results
