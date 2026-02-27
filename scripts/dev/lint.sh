#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(git -C "${script_dir}" rev-parse --show-toplevel 2>/dev/null || true)"
if [[ -z "${repo_root}" ]]; then
  echo "Unable to locate repo root. Run from inside the git repo." >&2
  exit 1
fi

cd "${repo_root}"
echo "Running format + vet"
make fmt vet

if [[ "${RUN_STATICCHECK:-}" == "1" ]]; then
  echo "Running staticcheck"
  make scheck
fi

if [[ "${RUN_BIAS_LINT:-}" == "1" ]]; then
  echo "Running bias language linter"
  make lang
fi
