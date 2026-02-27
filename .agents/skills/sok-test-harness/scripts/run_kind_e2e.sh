#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "Usage: $(basename "$0") [--keep]"
  echo "  --keep   Keep the kind cluster running after tests"
  echo "  --push-operator-image   Push operator image to local kind registry before tests"
}

keep_cluster=false
push_operator_image=false
while [[ $# -gt 0 ]]; do
  case "$1" in
    --keep)
      keep_cluster=true
      shift
      ;;
    --push-operator-image)
      push_operator_image=true
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $1"
      usage
      exit 1
      ;;
  esac
done

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(git -C "${script_dir}" rev-parse --show-toplevel 2>/dev/null || true)"
if [[ -z "${repo_root}" ]]; then
  repo_root="$(cd "${script_dir}/../../../.." && pwd)"
fi

cd "${repo_root}"

export CLUSTER_PROVIDER="${CLUSTER_PROVIDER:-kind}"
export TEST_CLUSTER_PLATFORM="${TEST_CLUSTER_PLATFORM:-kind}"

cleanup() {
  if [[ "${keep_cluster}" == "true" ]]; then
    echo "Keeping kind cluster running (requested)."
    return 0
  fi
  echo "Tearing down kind cluster: make cluster-down"
  make cluster-down
}

trap cleanup EXIT

echo "Bringing up kind cluster: make cluster-up"
make cluster-up

if [[ "${push_operator_image}" == "true" ]]; then
  echo "Pushing operator image to local kind registry"
  "${script_dir}/push_kind_operator_image.sh"
fi

echo "Running integration tests: make int-test"
make int-test
