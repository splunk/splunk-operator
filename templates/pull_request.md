## Summary
- 

## Governing Spec
- Spec file: 
- Spec status (`Draft`/`In Review`/`Approved`/`Implemented`/`Superseded`):
- Spec Kit path (`speckit/specs/<id>-<slug>/`):
- Harness manifest (`harness/manifests/*.yaml`):
- Risk tier (`low`/`medium`/`high`):
- Delivery mode (`agent`/`hybrid`/`human`):
- PR risk label (`risk:low`/`risk:medium`/`risk:high`):

## Changes
- 

## Tests
- [ ] `scripts/dev/spec_check.sh`
- [ ] `scripts/dev/harness_manifest_check.sh`
- [ ] `scripts/dev/risk_policy_check.sh`
- [ ] `scripts/dev/risk_label_check.sh --labels risk:<tier>`
- [ ] `scripts/dev/harness_eval.sh --suite docs/agent/evals/policy-regression.yaml`
- [ ] `scripts/dev/harness_run.sh --fast`
- [ ] `scripts/dev/pr_check.sh`
- [ ] `scripts/dev/autonomy_scorecard.sh --base-ref <target-branch>`
- [ ] `scripts/dev/unit.sh`
- [ ] Other:

## Risks / Rollback
- 

## Checklist
- [ ] CRD/RBAC artifacts updated (if applicable)
- [ ] Docs/examples updated (if user-facing change)
- [ ] Backward compatibility considered
