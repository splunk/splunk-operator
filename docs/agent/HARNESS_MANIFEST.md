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
evaluation_suite: docs/agent/evals/policy-regression.yaml
allowed_paths:
  - api/**
  - internal/**
forbidden_paths:
  - vendor/**
required_commands:
  - scripts/dev/spec_check.sh
  - scripts/dev/pr_check.sh --fast
```

## Field Notes
- `version`: currently `v1`.
- `change_id`: Jira issue or GitHub issue identifier.
- `spec_file`: path to governing KEP in `docs/specs/`.
- `owner`: owning team or maintainer group.
- `evaluation_suite`: replayable eval corpus path.
- `allowed_paths`: glob patterns that changed files are allowed to touch.
- `forbidden_paths`: glob patterns that changed files must not touch.
- `required_commands`: minimum commands expected for this change.

## Validation Rules
`scripts/dev/harness_manifest_check.sh` enforces:
- non-trivial code changes require a changed manifest
- referenced `spec_file` exists and has status `Approved` or `Implemented`
- `allowed_paths`, `forbidden_paths`, and `required_commands` exist
- `required_commands` include `scripts/dev/spec_check.sh` and `scripts/dev/pr_check.sh`
- changed files satisfy manifest scope policy
