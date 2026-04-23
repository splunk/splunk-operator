#!/bin/sh
set -eu

# Runtime contract
# - Purpose: build the canonical pipeline operator image that downstream scan and runtime jobs consume.
# - Inputs: PIPELINE_ECR_REPOSITORY, AWS region configuration, and AWS credentials.
# - Outputs: image reference and digest artifacts under ci-output/.
# - Guardrails: nonprod ECR publication, commit-scoped tag only, no latest/public registry mutation.

. "${CI_PROJECT_DIR}/gitlab-ci/lib/pipeline-common.sh"

context_file="ci-output/${WORKFLOW_SLUG}-runtime-context.txt"
mkdir -p "ci-output"
: > "${context_file}"

ensure_pipeline_aws_env

resolve_pipeline_image_repository "$(first_nonempty "${PIPELINE_ECR_REPOSITORY:-}" "")" "splunk/splunk-operator"

ECR_REGISTRY="$(first_nonempty "${PIPELINE_ECR_REGISTRY:-}" "${RESOLVED_ECR_REGISTRY}")"
IMAGE_REPOSITORY="${RESOLVED_IMAGE_REPOSITORY}"
resolve_ecr_region "${ECR_REGISTRY}"
ECR_REGION="${RESOLVED_ECR_REGION}"

if [ -z "${ECR_REGION}" ]; then
  echo "Unable to determine ECR region — set AWS_REGION, AWS_DEFAULT_REGION, or PIPELINE_AWS_DEFAULT_REGION" >&2
  exit 1
fi

export AWS_DEFAULT_REGION="${ECR_REGION}"

IMAGE_TAG="${CI_COMMIT_SHA}"
IMAGE_REF="${IMAGE_REPOSITORY}:${IMAGE_TAG}"
REPOSITORY_NAME="${RESOLVED_REPOSITORY_NAME}"
BUILD_PLATFORMS="$(first_nonempty "${PIPELINE_BUILD_PLATFORMS:-}" "${JOB_BUILD_PLATFORMS:-}" "linux/amd64")"
BUILD_DISTROLESS="false"
if bool_is_true "$(first_nonempty "${PIPELINE_BUILD_DISTROLESS:-}" "${JOB_BUILD_DISTROLESS:-}" "false")"; then
  BUILD_DISTROLESS="true"
fi
DISTROLESS_IMAGE_REF="${IMAGE_REPOSITORY}:${IMAGE_TAG}-distroless"

append_context "${context_file}" "ecr_registry" "${ECR_REGISTRY}"
append_context "${context_file}" "ecr_region" "${ECR_REGION}"
append_context "${context_file}" "image_repository_mode" "${RESOLVED_IMAGE_REPOSITORY_MODE}"
append_context "${context_file}" "image_tag" "${IMAGE_TAG}"
append_context "${context_file}" "build_platforms" "${BUILD_PLATFORMS}"
append_context "${context_file}" "build_distroless" "${BUILD_DISTROLESS}"

printf '%s\n' "${IMAGE_REF}" > "ci-output/${WORKFLOW_SLUG}-image-ref.txt"
if [ "${BUILD_DISTROLESS}" = "true" ]; then
  printf '%s\n' "${DISTROLESS_IMAGE_REF}" > "ci-output/${WORKFLOW_SLUG}-distroless-image-ref.txt"
fi

docker version
aws ecr get-login-password --region "${ECR_REGION}" | docker login --username AWS --password-stdin "${ECR_REGISTRY}"

# Let the Makefile and Dockerfile own their own defaults for BASE_IMAGE,
# BASE_IMAGE_VERSION, and BUILDER_IMAGE. Only pass what we must override.
make docker-buildx \
  IMG="${IMAGE_REF}" \
  PLATFORMS="${BUILD_PLATFORMS}"

if [ "${BUILD_DISTROLESS}" = "true" ]; then
  make docker-buildx \
    IMG="${DISTROLESS_IMAGE_REF}" \
    PLATFORMS="${BUILD_PLATFORMS}" \
    BASE_IMAGE="gcr.io/distroless/static" \
    BASE_IMAGE_VERSION="latest"
fi

aws ecr describe-images \
  --region "${ECR_REGION}" \
  --repository-name "${REPOSITORY_NAME}" \
  --image-ids imageTag="${IMAGE_TAG}" \
  --query 'imageDetails[0].imageDigest' \
  --output text > "ci-output/${WORKFLOW_SLUG}-digest.txt"

if [ "${BUILD_DISTROLESS}" = "true" ]; then
  aws ecr describe-images \
    --region "${ECR_REGION}" \
    --repository-name "${REPOSITORY_NAME}" \
    --image-ids imageTag="${IMAGE_TAG}-distroless" \
    --query 'imageDetails[0].imageDigest' \
    --output text > "ci-output/${WORKFLOW_SLUG}-distroless-digest.txt"
fi
