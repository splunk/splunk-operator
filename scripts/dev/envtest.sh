#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(git -C "${script_dir}" rev-parse --show-toplevel 2>/dev/null || true)"
if [[ -z "${repo_root}" ]]; then
  echo "Unable to locate repo root. Run from inside the git repo." >&2
  exit 1
fi

cd "${repo_root}"
echo "Ensuring envtest assets are available"
make envtest

if [[ "${RUN_TESTS:-}" == "1" ]]; then
  echo "Running unit/envtest suite (make test)"
  make test
fi
