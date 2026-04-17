#!/bin/sh
set -eu

# Runtime contract
# - Purpose: build the canonical staging operator image that downstream scan and runtime jobs consume.
# - Inputs: STAGING_ECR_REPOSITORY plus staging AWS credentials and region resolution.
# - Outputs: image reference and digest artifacts under ci-output/.
# - Guardrails: staging-only ECR publication, commit-scoped tag only, no latest/public registry mutation.

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
export BASE_IMAGE="registry.access.redhat.com/ubi8/ubi-minimal"
repo_default_base_image_version="$(awk '/^BASE_IMAGE[[:space:]]*\\?=/ { next } /^BASE_IMAGE_VERSION[[:space:]]*\\?=/ { print $3; exit }' "${CI_PROJECT_DIR}/Makefile")"
export BASE_IMAGE_VERSION="${BASE_IMAGE_VERSION:-${repo_default_base_image_version:-8.10-1775152441}}"
context_file="ci-output/${WORKFLOW_SLUG}-runtime-context.txt"
mkdir -p "ci-output"
: > "${context_file}"

resolve_staging_image_repository "${STAGING_ECR_REPOSITORY}" "splunk/splunk-operator"
ECR_REGISTRY="${RESOLVED_ECR_REGISTRY}"
IMAGE_REPOSITORY="${RESOLVED_IMAGE_REPOSITORY}"

resolve_ecr_region "${STAGING_AWS_DEFAULT_REGION:-}" "${ECR_REGISTRY}"
ECR_REGION="${RESOLVED_ECR_REGION}"

if [ -z "${ECR_REGION}" ]; then
  echo "Unable to determine ECR region from STAGING_AWS_DEFAULT_REGION or the staging registry host" >&2
  exit 1
fi

export AWS_DEFAULT_REGION="${ECR_REGION}"
export AWS_REGION="${ECR_REGION}"
export IMAGE_TAG="${CI_COMMIT_SHA}"
export IMAGE_REF="${IMAGE_REPOSITORY}:${IMAGE_TAG}"
export REPOSITORY_NAME="${IMAGE_REPOSITORY#${ECR_REGISTRY}/}"
dockerfile_default_builder_image="$(awk -F= '/^ARG BUILDER_IMAGE=/{print $2; exit}' "${CI_PROJECT_DIR}/Dockerfile")"
export BUILDER_IMAGE="${BUILDER_IMAGE:-${dockerfile_default_builder_image:-golang:1.25.8}}"
export BUILD_PLATFORMS="${STAGING_BUILD_PLATFORMS:-${JOB_BUILD_PLATFORMS:-linux/amd64}}"

append_context "${context_file}" "ecr_registry_present" "true"
append_context "${context_file}" "image_repository_mode" "${RESOLVED_IMAGE_REPOSITORY_MODE}"
append_context "${context_file}" "ecr_region_source" "${RESOLVED_ECR_REGION_SOURCE}"
append_context "${context_file}" "aws_auth_mode" "${aws_auth_mode}"
append_context "${context_file}" "image_tag" "${IMAGE_TAG}"
append_context "${context_file}" "builder_image" "${BUILDER_IMAGE}"
append_context "${context_file}" "build_platforms" "${BUILD_PLATFORMS}"

printf '%s\n' "${IMAGE_REF}" > "ci-output/${WORKFLOW_SLUG}-image-ref.txt"

echo "Using staging ECR host derived from STAGING_ECR_REPOSITORY"
docker version
if [ "${aws_auth_mode}" = "oidc" ]; then
  aws_prepare_oidc_env "${aws_oidc_token_file}"
fi
aws ecr get-login-password --region "${ECR_REGION}" | docker login --username AWS --password-stdin "${ECR_REGISTRY}"
make docker-buildx \
  IMG="${IMAGE_REF}" \
  PLATFORMS="${BUILD_PLATFORMS}" \
  BASE_IMAGE="${BASE_IMAGE}" \
  BASE_IMAGE_VERSION="${BASE_IMAGE_VERSION}" \
  BUILDER_IMAGE="${BUILDER_IMAGE}"
aws ecr describe-images \
  --region "${ECR_REGION}" \
  --repository-name "${REPOSITORY_NAME}" \
  --image-ids imageTag="${IMAGE_TAG}" \
  --query 'imageDetails[0].imageDigest' \
  --output text > "ci-output/${WORKFLOW_SLUG}-digest.txt"
