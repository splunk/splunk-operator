#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(git -C "${script_dir}" rev-parse --show-toplevel 2>/dev/null || true)"
if [[ -z "${repo_root}" ]]; then
  repo_root="$(cd "${script_dir}/../../../.." && pwd)"
fi

cd "${repo_root}"

SPLUNK_OPERATOR_IMAGE="${SPLUNK_OPERATOR_IMAGE:-splunk/splunk-operator:latest}"
PRIVATE_REGISTRY="${PRIVATE_REGISTRY:-localhost:5000}"

first_segment="${SPLUNK_OPERATOR_IMAGE%%/*}"
if [[ "${first_segment}" == *.* || "${first_segment}" == *:* ]]; then
  echo "SPLUNK_OPERATOR_IMAGE looks like it already includes a registry: ${first_segment}"
  echo "Set SPLUNK_OPERATOR_IMAGE to a repo/name:tag without a registry prefix."
  echo "Example: SPLUNK_OPERATOR_IMAGE=splunk/splunk-operator:latest"
  exit 1
fi

if [[ "${PRIVATE_REGISTRY}" == localhost:* || "${PRIVATE_REGISTRY}" == 127.0.0.1:* ]]; then
  if ! docker ps --format '{{.Names}}' | grep -q '^kind-registry$'; then
    echo "Local kind registry container is not running. Run: make cluster-up"
    exit 1
  fi
else
  if [[ "${FORCE_PUSH:-}" != "1" ]]; then
    echo "Refusing to push to non-local registry '${PRIVATE_REGISTRY}'."
    echo "Set FORCE_PUSH=1 to override."
    exit 1
  fi
fi

target_image="${PRIVATE_REGISTRY}/${SPLUNK_OPERATOR_IMAGE}"

if ! docker image inspect "${SPLUNK_OPERATOR_IMAGE}" >/dev/null 2>&1; then
  echo "Local image ${SPLUNK_OPERATOR_IMAGE} not found, pulling..."
  docker pull "${SPLUNK_OPERATOR_IMAGE}"
fi

docker tag "${SPLUNK_OPERATOR_IMAGE}" "${target_image}"

echo "Pushing ${target_image}"
docker push "${target_image}"

echo "Pushed operator image: ${target_image}"
