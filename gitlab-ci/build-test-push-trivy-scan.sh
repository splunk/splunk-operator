#!/bin/sh
set -eu

# Runtime contract
# - Purpose: scan the staged operator image emitted by build-stage-image.
# - Inputs: build artifact containing the image ref plus pipeline AWS credentials.
# - Outputs: SARIF and human-readable Trivy reports under ci-output/.
# - Guardrails: read-only access to nonprod ECR, severity limited to CRITICAL for the current migration slice.

. "${CI_PROJECT_DIR}/gitlab-ci/lib/pipeline-common.sh"

context_file="ci-output/${WORKFLOW_SLUG}-runtime-context.txt"
mkdir -p "ci-output"
: > "${context_file}"

TRIVY_RELEASE="$(first_nonempty "${PIPELINE_TRIVY_RELEASE:-}" "v0.69.3")"
TRIVY_ASSET_URL="$(first_nonempty "${PIPELINE_TRIVY_ASSET_URL:-}" "")"
trivy_resolution_mode="direct-url"

install_os_packages bash curl jq tar
ensure_pipeline_aws_env

if [ -z "${TRIVY_ASSET_URL}" ]; then
  if [ "${TRIVY_RELEASE}" = "latest" ]; then
    trivy_resolution_mode="github-api"
    trivy_release_api="https://api.github.com/repos/aquasecurity/trivy/releases/latest"
    trivy_release_json="$(curl -fsSL \
      -H 'Accept: application/vnd.github+json' \
      -H 'X-GitHub-Api-Version: 2022-11-28' \
      "${trivy_release_api}")"
    TRIVY_TAG="$(printf '%s' "${trivy_release_json}" | jq -r '.tag_name')"
    TRIVY_ASSET_URL="$(printf '%s' "${trivy_release_json}" | jq -r '.assets[] | select(.name | endswith("_Linux-64bit.tar.gz")) | .browser_download_url' | head -n 1)"
  else
    trivy_release_tag="${TRIVY_RELEASE#v}"
    TRIVY_TAG="v${trivy_release_tag}"
    TRIVY_ASSET_URL="https://github.com/aquasecurity/trivy/releases/download/${TRIVY_TAG}/trivy_${trivy_release_tag}_Linux-64bit.tar.gz"
  fi
else
  TRIVY_TAG="${TRIVY_RELEASE}"
  trivy_resolution_mode="explicit-url"
fi

if [ -z "${TRIVY_TAG:-}" ] || [ "${TRIVY_TAG}" = "null" ] || [ -z "${TRIVY_ASSET_URL}" ]; then
  echo "Unable to resolve Trivy release asset for selector ${TRIVY_RELEASE}" >&2
  exit 1
fi

curl -fsSL -o /tmp/trivy.tgz "${TRIVY_ASSET_URL}"
tar -xzf /tmp/trivy.tgz -C /tmp trivy
install /tmp/trivy /usr/local/bin/trivy
trivy --version
aws --version

require_file "ci-output/build-test-push-workflow-image-ref.txt" "build image reference artifact"
IMAGE_REF="$(cat ci-output/build-test-push-workflow-image-ref.txt)"
ECR_REGISTRY="${IMAGE_REF%%/*}"
ECR_REGION="$(first_nonempty "${AWS_REGION:-}" "${AWS_DEFAULT_REGION:-}" "${PIPELINE_AWS_DEFAULT_REGION:-}" "$(printf '%s' "${ECR_REGISTRY}" | cut -d. -f4)")"

if [ -z "${ECR_REGION}" ]; then
  echo "Unable to determine ECR region — set AWS_REGION, AWS_DEFAULT_REGION, or PIPELINE_AWS_DEFAULT_REGION" >&2
  exit 1
fi

export AWS_DEFAULT_REGION="${ECR_REGION}"

# ECR_PASSWORD is consumed only by the trivy --password flag below.
# It is not echoed or written to disk; GitLab masked-variable protection
# covers the aws ecr get-login-password output in job traces.
ECR_PASSWORD="$(aws ecr get-login-password --region "${ECR_REGION}")"

append_context "${context_file}" "input_artifact" "ci-output/build-test-push-workflow-image-ref.txt"
append_context "${context_file}" "ecr_registry" "${ECR_REGISTRY}"
append_context "${context_file}" "ecr_region" "${ECR_REGION}"
append_context "${context_file}" "trivy_release_selector" "${TRIVY_RELEASE}"
append_context "${context_file}" "trivy_resolution_mode" "${trivy_resolution_mode}"
append_context "${context_file}" "trivy_tag" "${TRIVY_TAG}"
append_context "${context_file}" "trivy_asset_url" "${TRIVY_ASSET_URL}"

printf '%s\n' "${IMAGE_REF}" > "ci-output/${WORKFLOW_SLUG}-image-ref.txt"

trivy image \
  --username AWS \
  --password "${ECR_PASSWORD}" \
  --severity CRITICAL \
  --ignore-unfixed \
  --format sarif \
  --output "ci-output/${WORKFLOW_SLUG}-trivy-results.sarif" \
  "${IMAGE_REF}"

# --skip-db-update reuses the DB already downloaded by the SARIF pass above.
trivy image \
  --username AWS \
  --password "${ECR_PASSWORD}" \
  --severity CRITICAL \
  --ignore-unfixed \
  --skip-db-update \
  "${IMAGE_REF}" | tee "ci-output/${WORKFLOW_SLUG}-trivy-results.txt"
