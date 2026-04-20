#!/bin/sh
set -eu

# Runtime contract
# - Purpose: build the canonical staging operator image that downstream scan and runtime jobs consume.
# - Inputs: STAGING_ECR_REPOSITORY, AWS_REGION (or STAGING_AWS_DEFAULT_REGION), and AWS credentials.
# - Outputs: image reference and digest artifacts under ci-output/.
# - Guardrails: staging-only ECR publication, commit-scoped tag only, no latest/public registry mutation.

. "${CI_PROJECT_DIR}/gitlab-ci/lib/pipeline-common.sh"

context_file="ci-output/${WORKFLOW_SLUG}-runtime-context.txt"
mkdir -p "ci-output"
: > "${context_file}"

# Registry and region — prefer explicit variables, fall back to hostname parsing.
ECR_REGISTRY="${STAGING_ECR_REGISTRY:-${STAGING_ECR_REPOSITORY%%/*}}"
IMAGE_REPOSITORY="${STAGING_ECR_REPOSITORY:-${ECR_REGISTRY}/splunk/splunk-operator}"
ECR_REGION="${AWS_REGION:-${STAGING_AWS_DEFAULT_REGION:-$(printf '%s' "${ECR_REGISTRY}" | cut -d. -f4)}}"

if [ -z "${ECR_REGION}" ]; then
  echo "Unable to determine ECR region — set AWS_REGION or STAGING_AWS_DEFAULT_REGION" >&2
  exit 1
fi

export AWS_DEFAULT_REGION="${ECR_REGION}"
export AWS_REGION="${ECR_REGION}"

IMAGE_TAG="${CI_COMMIT_SHA}"
IMAGE_REF="${IMAGE_REPOSITORY}:${IMAGE_TAG}"
REPOSITORY_NAME="${IMAGE_REPOSITORY#${ECR_REGISTRY}/}"
BUILD_PLATFORMS="${STAGING_BUILD_PLATFORMS:-${JOB_BUILD_PLATFORMS:-linux/amd64}}"

append_context "${context_file}" "ecr_registry" "${ECR_REGISTRY}"
append_context "${context_file}" "ecr_region" "${ECR_REGION}"
append_context "${context_file}" "image_tag" "${IMAGE_TAG}"
append_context "${context_file}" "build_platforms" "${BUILD_PLATFORMS}"

printf '%s\n' "${IMAGE_REF}" > "ci-output/${WORKFLOW_SLUG}-image-ref.txt"

docker version
aws ecr get-login-password --region "${ECR_REGION}" | docker login --username AWS --password-stdin "${ECR_REGISTRY}"

# Let the Makefile and Dockerfile own their own defaults for BASE_IMAGE,
# BASE_IMAGE_VERSION, and BUILDER_IMAGE. Only pass what we must override.
make docker-buildx \
  IMG="${IMAGE_REF}" \
  PLATFORMS="${BUILD_PLATFORMS}"

aws ecr describe-images \
  --region "${ECR_REGION}" \
  --repository-name "${REPOSITORY_NAME}" \
  --image-ids imageTag="${IMAGE_TAG}" \
  --query 'imageDetails[0].imageDigest' \
  --output text > "ci-output/${WORKFLOW_SLUG}-digest.txt"
