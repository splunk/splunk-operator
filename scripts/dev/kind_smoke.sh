#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(git -C "${script_dir}" rev-parse --show-toplevel 2>/dev/null || true)"
if [[ -z "${repo_root}" ]]; then
  echo "Unable to locate repo root. Run from inside the git repo." >&2
  exit 1
fi

cd "${repo_root}"

if ! command -v kubectl >/dev/null 2>&1; then
  echo "kubectl not found in PATH." >&2
  exit 1
fi
if ! command -v kind >/dev/null 2>&1; then
  echo "kind not found in PATH." >&2
  exit 1
fi

export CLUSTER_PROVIDER=kind
export TEST_CLUSTER_PLATFORM=kind
: "${TEST_CLUSTER_NAME:=sok-kind}"
: "${CLUSTER_WORKERS:=3}"

echo "Bringing up kind cluster"
make cluster-up

: "${NAMESPACE:=splunk-operator}"
: "${WATCH_NAMESPACE:=${NAMESPACE}}"
: "${ENVIRONMENT:=default}"
: "${SPLUNK_GENERAL_TERMS:=--accept-sgt-current-at-splunk-com}"
: "${SPLUNK_ENTERPRISE_IMAGE:=splunk/splunk:latest}"
: "${IMG:=splunk/splunk-operator:latest}"

export NAMESPACE WATCH_NAMESPACE ENVIRONMENT SPLUNK_GENERAL_TERMS SPLUNK_ENTERPRISE_IMAGE IMG

kubectl create namespace "${NAMESPACE}" >/dev/null 2>&1 || true

echo "Deploying operator"
make deploy

echo "Waiting for operator deployment to be ready"
kubectl -n "${NAMESPACE}" rollout status deploy/splunk-operator-controller-manager --timeout="${OPERATOR_READY_TIMEOUT:-5m}"

if [[ "${APPLY_SAMPLE:-}" == "1" ]]; then
  echo "Applying sample CR (best effort)"
  kubectl -n "${NAMESPACE}" apply -f config/samples/enterprise_v4_standalone.yaml
fi

echo "Kind smoke complete"
