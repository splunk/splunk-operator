#!/bin/sh
set -eu

# Runtime contract
# - Purpose: scan the staged operator image emitted by build-stage-image.
# - Inputs: build artifact containing the image ref plus staging AWS credentials.
# - Outputs: SARIF and human-readable Trivy reports under ci-output/.
# - Guardrails: read-only access to staging ECR, severity limited to CRITICAL for the current migration slice.

. "${CI_PROJECT_DIR}/gitlab-ci/lib/pipeline-common.sh"

aws_oidc_token_file="$(mktemp /tmp/${WORKFLOW_SLUG}-aws-oidc.XXXXXX.jwt)"
trap 'rm -f "${aws_oidc_token_file}"' EXIT INT TERM

aws_auth_mode="static-key"
if aws_oidc_ready; then
  aws_auth_mode="oidc"
else
  export AWS_ACCESS_KEY_ID="${STAGING_AWS_ACCESS_KEY_ID}"
  export AWS_SECRET_ACCESS_KEY="${STAGING_AWS_SECRET_ACCESS_KEY}"
fi
context_file="ci-output/${WORKFLOW_SLUG}-runtime-context.txt"
mkdir -p "ci-output"
: > "${context_file}"
TRIVY_RELEASE="${STAGING_TRIVY_RELEASE:-v0.69.3}"
TRIVY_ASSET_URL="${STAGING_TRIVY_ASSET_URL:-}"
trivy_resolution_mode="direct-url"

ensure_trivy_scan_tooling
python3 -m venv /tmp/trivy-tools-venv
. /tmp/trivy-tools-venv/bin/activate
pip install --no-cache-dir awscli
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
export IMAGE_REF="$(cat ci-output/build-test-push-workflow-image-ref.txt)"
export ECR_REGISTRY="${IMAGE_REF%%/*}"

resolve_ecr_region "${STAGING_AWS_DEFAULT_REGION:-}" "${ECR_REGISTRY}"
AWS_DEFAULT_REGION="${RESOLVED_ECR_REGION}"

if [ -z "${AWS_DEFAULT_REGION}" ]; then
  echo "Unable to determine ECR region for Trivy scan" >&2
  exit 1
fi

export AWS_REGION="${AWS_DEFAULT_REGION}"
if [ "${aws_auth_mode}" = "oidc" ]; then
  aws_prepare_oidc_env "${aws_oidc_token_file}"
fi
export ECR_PASSWORD="$(aws ecr get-login-password --region "${AWS_DEFAULT_REGION}")"

append_context "${context_file}" "input_artifact" "ci-output/build-test-push-workflow-image-ref.txt"
append_context "${context_file}" "ecr_registry_present" "true"
append_context "${context_file}" "ecr_region_source" "${RESOLVED_ECR_REGION_SOURCE}"
append_context "${context_file}" "aws_auth_mode" "${aws_auth_mode}"
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

trivy image \
  --username AWS \
  --password "${ECR_PASSWORD}" \
  --severity CRITICAL \
  --ignore-unfixed \
  "${IMAGE_REF}" | tee "ci-output/${WORKFLOW_SLUG}-trivy-results.txt"
