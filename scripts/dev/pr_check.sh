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
if [[ -n "${SPEC_CHECK_BASE_REF:-}" ]]; then
  spec_args+=(--base-ref "${SPEC_CHECK_BASE_REF}")
fi

if [[ "${SKIP_SPEC_CHECK:-0}" != "1" ]]; then
  echo "Running spec check: ./scripts/dev/spec_check.sh ${spec_args[*]}"
  ./scripts/dev/spec_check.sh "${spec_args[@]}"
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
