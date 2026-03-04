#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: scripts/dev/skaffold_ci_smoke.sh [--profile <name>] [--namespace <ns>] [--timeout <duration>] [--no-cleanup] [--help]

Builds and deploys the operator using skaffold, then validates controller rollout.
Defaults:
  profile=ci-smoke
  namespace=splunk-operator
  timeout=300s
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

if ! command -v kubectl >/dev/null 2>&1; then
  echo "kubectl is required to validate smoke deployment." >&2
  exit 1
fi

profile="${SKAFFOLD_PROFILE:-ci-smoke}"
namespace="${OPERATOR_NAMESPACE:-splunk-operator}"
timeout="${SKAFFOLD_ROLLOUT_TIMEOUT:-300s}"
cleanup="${SKAFFOLD_CLEANUP:-1}"

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
    --namespace)
      if [[ $# -lt 2 ]]; then
        echo "--namespace requires a value." >&2
        exit 1
      fi
      namespace="$2"
      shift 2
      ;;
    --timeout)
      if [[ $# -lt 2 ]]; then
        echo "--timeout requires a value." >&2
        exit 1
      fi
      timeout="$2"
      shift 2
      ;;
    --no-cleanup)
      cleanup="0"
      shift
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

cleanup_fn() {
  if [[ "${cleanup}" == "1" ]]; then
    skaffold delete -p "${profile}" || true
  fi
}
trap cleanup_fn EXIT

echo "Running skaffold smoke deploy (profile=${profile})"
skaffold run -p "${profile}" --status-check=true

echo "Validating operator rollout in namespace ${namespace}"
kubectl -n "${namespace}" rollout status deployment/splunk-operator-controller-manager --timeout="${timeout}"
kubectl -n "${namespace}" get pods -l control-plane=controller-manager

echo "Skaffold smoke deployment succeeded."
