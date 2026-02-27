#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(git -C "${script_dir}" rev-parse --show-toplevel 2>/dev/null || true)"
if [[ -z "${repo_root}" ]]; then
  echo "Unable to locate repo root. Run from inside the git repo." >&2
  exit 1
fi

cd "${repo_root}"

if ! command -v git >/dev/null 2>&1; then
  echo "git is required for verify_crd.sh" >&2
  exit 1
fi

printf "Running CRD/RBAC generation...\n"
make generate
make manifests

changed=$(git diff --name-only -- config/crd/bases config/rbac/role.yaml)
if [[ -n "${changed}" ]]; then
  echo "CRD/RBAC outputs changed after regeneration:" >&2
  echo "${changed}" >&2
  echo "Please commit regenerated files." >&2
  exit 1
fi

echo "CRD/RBAC outputs are up to date."
