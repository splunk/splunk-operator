#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage:
  scripts/dev/speckit_bridge.sh bootstrap --change-id <ID> --title <title> [options]

Bootstraps a Spec Kit workspace, KEP-lite spec, and harness manifest.

Options:
  --change-id <ID>          Required. Jira/GitHub ID (for example CSPL-5000).
  --title <title>           Required. Human-readable change title.
  --slug <slug>             Optional. Kebab-case slug override.
  --owner <owner>           Optional. Default: @splunk/splunk-operator-for-kubernetes
  --risk-tier <tier>        Optional. low|medium|high (default: medium)
  --delivery-mode <mode>    Optional. agent|hybrid|human (default: agent)
  -h, --help                Show this help.
USAGE
}

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(git -C "${script_dir}" rev-parse --show-toplevel 2>/dev/null || true)"
if [[ -z "${repo_root}" ]]; then
  echo "Unable to locate repo root. Run from inside the git repo." >&2
  exit 1
fi
cd "${repo_root}"

slugify() {
  local value="$1"
  value="$(printf '%s' "${value}" | tr '[:upper:]' '[:lower:]')"
  value="$(printf '%s' "${value}" | sed -E 's/[^a-z0-9]+/-/g; s/^-+//; s/-+$//; s/-+/-/g')"
  printf '%s\n' "${value}"
}

set_risk_defaults() {
  local tier="$1"
  case "${tier}" in
    low)
      risk_approvals=0
      risk_auto_merge=true
      risk_merge_queue=false
      ;;
    medium)
      risk_approvals=1
      risk_auto_merge=false
      risk_merge_queue=true
      ;;
    high)
      risk_approvals=2
      risk_auto_merge=false
      risk_merge_queue=true
      ;;
    *)
      echo "Invalid --risk-tier: ${tier}. Use low|medium|high." >&2
      exit 1
      ;;
  esac
}

write_file_if_absent() {
  local file="$1"
  shift
  if [[ -f "${file}" ]]; then
    echo "exists: ${file}"
    return
  fi
  mkdir -p "$(dirname "${file}")"
  cat > "${file}"
  echo "created: ${file}"
}

cmd="${1:-}"
if [[ -z "${cmd}" ]]; then
  usage
  exit 1
fi
shift || true

case "${cmd}" in
  bootstrap)
    ;;
  -h|--help)
    usage
    exit 0
    ;;
  *)
    echo "Unknown command: ${cmd}" >&2
    usage
    exit 1
    ;;
esac

change_id=""
title=""
slug=""
owner="@splunk/splunk-operator-for-kubernetes"
risk_tier="medium"
delivery_mode="agent"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --change-id)
      change_id="$2"
      shift 2
      ;;
    --title)
      title="$2"
      shift 2
      ;;
    --slug)
      slug="$2"
      shift 2
      ;;
    --owner)
      owner="$2"
      shift 2
      ;;
    --risk-tier)
      risk_tier="$2"
      shift 2
      ;;
    --delivery-mode)
      delivery_mode="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      usage
      exit 1
      ;;
  esac
done

if [[ -z "${change_id}" || -z "${title}" ]]; then
  echo "--change-id and --title are required." >&2
  usage
  exit 1
fi

case "${delivery_mode}" in
  agent|hybrid|human)
    ;;
  *)
    echo "Invalid --delivery-mode: ${delivery_mode}. Use agent|hybrid|human." >&2
    exit 1
    ;;
esac

if [[ -z "${slug}" ]]; then
  slug="$(slugify "${title}")"
fi
if [[ -z "${slug}" ]]; then
  echo "Unable to derive slug from title. Pass --slug explicitly." >&2
  exit 1
fi

set_risk_defaults "${risk_tier}"

today="$(date -u +"%Y-%m-%d")"
base_name="${change_id}-${slug}"
speckit_dir="speckit/specs/${base_name}"
spec_file="docs/specs/${base_name}.md"
manifest_file="harness/manifests/${base_name}.yaml"

write_file_if_absent "${speckit_dir}/spec.md" <<EOF_SPEC
# ${title}

- Change ID: ${change_id}
- Status: Draft
- Owner: ${owner}
- Created: ${today}

## Problem
Describe the concrete user/operator problem.

## Success Criteria
- Criterion 1
- Criterion 2

## Constraints
- Constraint 1
- Constraint 2

## Links
- KEP: ${spec_file}
- Harness manifest: ${manifest_file}
EOF_SPEC

write_file_if_absent "${speckit_dir}/plan.md" <<EOF_PLAN
# Implementation Plan: ${title}

## Milestones
1. KEP review and approval
2. Implementation with harness checks
3. Validation and PR merge

## Scope
- In scope:
- Out of scope:

## Risks
- Risk:
- Mitigation:
EOF_PLAN

write_file_if_absent "${speckit_dir}/tasks.md" <<EOF_TASKS
# Task List: ${title}

- [ ] Draft and review KEP
- [ ] Set risk tier and scope in manifest
- [ ] Implement code changes
- [ ] Add/update tests
- [ ] Run harness gates
- [ ] Prepare PR summary and rollout notes
EOF_TASKS

write_file_if_absent "${speckit_dir}/bridge.yaml" <<EOF_BRIDGE
version: v1
change_id: ${change_id}
title: ${title}
speckit_dir: ${speckit_dir}
kepspec: ${spec_file}
manifest: ${manifest_file}
risk_tier: ${risk_tier}
delivery_mode: ${delivery_mode}
owner: ${owner}
EOF_BRIDGE

write_file_if_absent "${spec_file}" <<EOF_KEP
# ${title}

- ID: ${change_id}
- Status: Draft
- Owners: ${owner}
- Reviewers: ${owner}
- Created: ${today}
- Last Updated: ${today}
- Related Links: ${speckit_dir}/spec.md, ${speckit_dir}/plan.md, ${speckit_dir}/tasks.md

## Summary
One paragraph describing what is changing and why.

## Motivation
Describe user/operator pain and current limitations.

## Goals
- Goal 1
- Goal 2

## Non-Goals
- Out of scope 1
- Out of scope 2

## Proposal
Describe the technical design and decision rationale.

## API/CRD Impact
- Affected CRDs/kinds:
- Schema/marker/defaulting changes:
- Compatibility notes:

## Reconcile/State Impact
- Reconciler flows touched:
- Status phase/conditions behavior:
- Idempotency and requeue behavior:

## Test Plan
- Unit:
- Integration (Ginkgo):
- KUTTL:
- Upgrade/regression:

## Harness Validation
- scripts/dev/spec_check.sh
- scripts/dev/harness_manifest_check.sh
- scripts/dev/risk_policy_check.sh
- scripts/dev/harness_eval.sh --suite docs/agent/evals/policy-regression.yaml
- scripts/dev/harness_run.sh --fast
- scripts/dev/pr_check.sh --fast

## Risks
List behavioral, operational, and compatibility risks plus mitigations.

## Rollout and Rollback
Define rollout order, observability, and rollback steps.

## Graduation Criteria
- [ ] Design reviewed and status moved to Approved
- [ ] Implementation merged and status moved to Implemented
- [ ] Required harness checks pass in CI
- [ ] Docs and examples updated (if applicable)
EOF_KEP

write_file_if_absent "${manifest_file}" <<EOF_MANIFEST
version: v1
change_id: ${change_id}
title: ${title}
spec_file: ${spec_file}
owner: ${owner}
delivery_mode: ${delivery_mode}
risk_tier: ${risk_tier}
human_approvals_required: ${risk_approvals}
auto_merge_allowed: ${risk_auto_merge}
merge_queue_required: ${risk_merge_queue}
evaluation_suite: docs/agent/evals/policy-regression.yaml
allowed_paths:
  - api/**
  - cmd/**
  - config/**
  - docs/**
  - harness/**
  - internal/**
  - kuttl/**
  - pkg/**
  - scripts/**
  - test/**
  - templates/**
forbidden_paths:
  - vendor/**
  - bin/**
  - .git/**
required_commands:
  - scripts/dev/spec_check.sh
  - scripts/dev/harness_manifest_check.sh
  - scripts/dev/risk_policy_check.sh
  - scripts/dev/harness_eval.sh --suite docs/agent/evals/policy-regression.yaml
  - scripts/dev/harness_run.sh --fast
  - scripts/dev/pr_check.sh --fast
EOF_MANIFEST

cat <<EOF_OUT

Bridge bootstrap complete.
- Spec Kit: ${speckit_dir}/
- KEP: ${spec_file}
- Manifest: ${manifest_file}

Next:
1. Fill ${speckit_dir}/spec.md and ${speckit_dir}/plan.md.
2. Drive ${spec_file} to Status: Approved.
3. Implement code and run scripts/dev/harness_run.sh --fast.
EOF_OUT
