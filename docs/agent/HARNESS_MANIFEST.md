# Harness Manifest Format

`harness/manifests/*.yaml` defines machine-readable governance for one
implementation change.

## Required Fields

```yaml
version: v1
change_id: CSPL-0000
title: Short title
spec_file: docs/specs/CSPL-0000-example.md
owner: @splunk/splunk-operator-for-kubernetes
delivery_mode: agent
risk_tier: medium
human_approvals_required: 1
auto_merge_allowed: false
merge_queue_required: true
evaluation_suite: docs/agent/evals/policy-regression.yaml
allowed_paths:
  - api/**
  - internal/**
forbidden_paths:
  - vendor/**
required_commands:
  - scripts/dev/spec_check.sh
  - scripts/dev/risk_policy_check.sh
  - scripts/dev/pr_check.sh --fast
```

## Field Notes
- `version`: currently `v1`.
- `change_id`: Jira issue or GitHub issue identifier.
- `spec_file`: path to governing KEP in `docs/specs/`.
- `owner`: owning team or maintainer group.
- `delivery_mode`: one of `agent`, `hybrid`, `human`.
- `risk_tier`: one of `low`, `medium`, `high`.
- `human_approvals_required`: minimum human approvals expected by policy.
- `auto_merge_allowed`: indicates whether branch policy may auto-merge.
- `merge_queue_required`: indicates whether merge queue is required.
- `evaluation_suite`: replayable eval corpus path.
- `allowed_paths`: glob patterns that changed files are allowed to touch.
- `forbidden_paths`: glob patterns that changed files must not touch.
- `required_commands`: minimum commands expected for this change.

## Validation Rules
`scripts/dev/harness_manifest_check.sh` enforces:
- non-trivial code changes require a changed manifest
- referenced `spec_file` exists and has status `Approved` or `Implemented`
- risk and delivery fields exist
- `allowed_paths`, `forbidden_paths`, and `required_commands` exist
- `required_commands` include `scripts/dev/spec_check.sh`, `scripts/dev/risk_policy_check.sh`, and `scripts/dev/pr_check.sh`
- changed files satisfy manifest scope policy

`scripts/dev/risk_policy_check.sh` enforces:
- risk-tier value constraints
- minimum approval and merge-queue policy by tier
- deeper required command expectations for medium/high risk changes
