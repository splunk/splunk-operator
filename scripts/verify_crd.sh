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

expected_controller_gen_version="$(
  awk -F'?=' '/^CONTROLLER_TOOLS_VERSION[[:space:]]*\\?=/{gsub(/^[[:space:]]+|[[:space:]]+$/, "", $2); print $2; exit}' Makefile
)"
if [[ -n "${expected_controller_gen_version}" ]]; then
  current_controller_gen_version=""
  if [[ -x "${repo_root}/bin/controller-gen" ]]; then
    current_controller_gen_version="$("${repo_root}/bin/controller-gen" --version 2>/dev/null || true)"
  fi
  if [[ "${current_controller_gen_version}" != *"${expected_controller_gen_version}"* ]]; then
    echo "Installing controller-gen ${expected_controller_gen_version} for deterministic CRD generation..."
    GOBIN="${repo_root}/bin" go install "sigs.k8s.io/controller-tools/cmd/controller-gen@${expected_controller_gen_version}"
  fi
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
