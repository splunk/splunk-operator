#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: scripts/dev/skaffold_dev.sh [--profile <name>] [--help] [-- <extra skaffold args>]

Runs skaffold dev for Splunk Operator.
Default profile: dev-kind
USAGE
}

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(git -C "${script_dir}" rev-parse --show-toplevel 2>/dev/null || true)"
if [[ -z "${repo_root}" ]]; then
  echo "Unable to locate repo root. Run from inside the git repo." >&2
  exit 1
fi

cd "${repo_root}"

if ! command -v skaffold >/dev/null 2>&1; then
  echo "skaffold is required. Install it from https://skaffold.dev/docs/install/" >&2
  exit 1
fi

profile="${SKAFFOLD_PROFILE:-dev-kind}"
extra_args=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --profile)
      if [[ $# -lt 2 ]]; then
        echo "--profile requires a value." >&2
        exit 1
      fi
      profile="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    --)
      shift
      extra_args+=("$@")
      break
      ;;
    *)
      extra_args+=("$1")
      shift
      ;;
  esac
done

echo "Running skaffold dev (profile=${profile})"
skaffold dev -p "${profile}" "${extra_args[@]}"
