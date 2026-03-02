#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(git -C "${script_dir}" rev-parse --show-toplevel 2>/dev/null || true)"
if [[ -z "${repo_root}" ]]; then
  echo "Unable to locate repo root. Run from inside the git repo." >&2
  exit 1
fi

cd "${repo_root}"

spec_args=()
base_ref="${SPEC_CHECK_BASE_REF:-${HARNESS_BASE_REF:-}}"
if [[ -n "${base_ref}" ]]; then
  spec_args+=(--base-ref "${base_ref}")
fi

if [[ "${SKIP_SPEC_CHECK:-0}" != "1" ]]; then
  echo "Running spec check: ./scripts/dev/spec_check.sh ${spec_args[*]}"
  ./scripts/dev/spec_check.sh "${spec_args[@]}"
fi

if [[ "${SKIP_MANIFEST_CHECK:-0}" != "1" ]]; then
  echo "Running harness manifest check: ./scripts/dev/harness_manifest_check.sh ${spec_args[*]}"
  ./scripts/dev/harness_manifest_check.sh "${spec_args[@]}"
fi

if [[ "${SKIP_RISK_POLICY_CHECK:-0}" != "1" ]]; then
  echo "Running risk policy check: ./scripts/dev/risk_policy_check.sh ${spec_args[*]}"
  ./scripts/dev/risk_policy_check.sh "${spec_args[@]}"
fi

if [[ "${SKIP_RISK_LABEL_CHECK:-0}" != "1" ]]; then
  label_args=()
  if [[ -n "${RISK_LABELS:-}" ]]; then
    label_args+=(--labels "${RISK_LABELS}")
  fi
  echo "Running risk label check: ./scripts/dev/risk_label_check.sh ${spec_args[*]} ${label_args[*]}"
  ./scripts/dev/risk_label_check.sh "${spec_args[@]}" "${label_args[@]}"
fi

if [[ "${SKIP_HARNESS_EVAL:-0}" != "1" ]]; then
  suite="${HARNESS_EVAL_SUITE:-docs/agent/evals/policy-regression.yaml}"
  echo "Running harness eval: ./scripts/dev/harness_eval.sh --suite ${suite}"
  ./scripts/dev/harness_eval.sh --suite "${suite}"
fi

if [[ "${SKIP_SCRIPT_SANITY:-0}" != "1" ]]; then
  echo "Running script sanity checks: ./scripts/dev/script_sanity_check.sh"
  ./scripts/dev/script_sanity_check.sh
fi

args=()
if [[ "${RUN_ALL:-}" == "1" ]]; then
  args+=(--all)
else
  if [[ "${RUN_BUNDLE:-}" == "1" ]]; then
    args+=(--bundle)
  fi
  if [[ "${RUN_TESTS:-}" == "1" ]]; then
    args+=(--tests)
  fi
fi

if [[ -n "${PR_CHECK_FLAGS:-}" ]]; then
  # shellcheck disable=SC2206
  extra_flags=(${PR_CHECK_FLAGS})
  args+=("${extra_flags[@]}")
fi

echo "Running repo PR checks: ./scripts/verify_repo.sh ${args[*]}"
./scripts/verify_repo.sh "${args[@]}"
