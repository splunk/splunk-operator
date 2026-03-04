#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "Usage: $(basename "$0") <kind> <name> <namespace>" >&2
  echo "Environment overrides:" >&2
  echo "  OPERATOR_NAMESPACE (default: splunk-operator)" >&2
  echo "  LOG_SINCE (default: 30m)" >&2
  echo "  OUTPUT_DIR (default: ./.agent-output/reconcile-<kind>-<name>-<timestamp>)" >&2
}

if [[ $# -ne 3 ]]; then
  usage
  exit 1
fi

kind="$1"
name="$2"
namespace="$3"

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(git -C "${script_dir}" rev-parse --show-toplevel 2>/dev/null || true)"
if [[ -z "${repo_root}" ]]; then
  echo "Unable to locate repo root. Run from inside the git repo." >&2
  exit 1
fi

cd "${repo_root}"

if ! command -v kubectl >/dev/null 2>&1; then
  echo "kubectl is required for debug_reconcile.sh" >&2
  exit 1
fi

operator_namespace="${OPERATOR_NAMESPACE:-splunk-operator}"
log_since="${LOG_SINCE:-30m}"

if [[ -n "${OUTPUT_DIR:-}" ]]; then
  out_dir="${OUTPUT_DIR}"
else
  ts="$(date +%Y%m%d-%H%M%S)"
  out_dir="${repo_root}/.agent-output/reconcile-${kind}-${name}-${ts}"
fi

mkdir -p "${out_dir}"

printf "Collecting reconcile debug data into %s\n" "${out_dir}"

kubectl get "${kind}" "${name}" -n "${namespace}" -o yaml > "${out_dir}/cr.yaml"
kubectl describe "${kind}" "${name}" -n "${namespace}" > "${out_dir}/cr.describe.txt"

kubectl get events -n "${namespace}" --sort-by=.lastTimestamp > "${out_dir}/events.txt"

kubectl get pods -n "${namespace}" -o wide > "${out_dir}/pods.txt"
kubectl get svc -n "${namespace}" -o wide > "${out_dir}/services.txt"

kubectl logs -n "${operator_namespace}" deploy/splunk-operator-controller-manager -c manager --since="${log_since}" > "${out_dir}/operator.logs.txt"

printf "Done.\n"
