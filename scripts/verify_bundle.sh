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
  echo "git is required for verify_bundle.sh" >&2
  exit 1
fi

if ! command -v operator-sdk >/dev/null 2>&1; then
  echo "operator-sdk is required for bundle generation." >&2
  echo "Install it via: make setup/devsetup" >&2
  exit 1
fi

printf "Running bundle generation...\n"
make bundle

changed=$(git diff --name-only -- bundle/manifests helm-chart/splunk-operator/crds)
if [[ -n "${changed}" ]]; then
  echo "Bundle or Helm CRD outputs changed after regeneration:" >&2
  echo "${changed}" >&2
  echo "Please commit regenerated files." >&2
  exit 1
fi

echo "Bundle outputs are up to date."
