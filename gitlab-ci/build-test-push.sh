#!/bin/sh
set -eu

# Runtime contract
# - Purpose: build the canonical pipeline operator image that downstream scan and runtime jobs consume.
# - Inputs: registry target variables and registry credentials prepared by the job.
# - Outputs: ECR and Artifactory image references plus registry-specific digest artifacts under ci-output/.
# - Guardrails: commit- or pipeline-scoped tag only, no latest/public registry mutation.

. "${CI_PROJECT_DIR}/gitlab-ci/lib/pipeline-common.sh"

context_file="ci-output/${WORKFLOW_SLUG}-runtime-context.txt"
mkdir -p "ci-output"
: > "${context_file}"

# ECR Setup
ensure_pipeline_aws_env

resolve_ecr_pipeline_image_repository "$(first_nonempty "${PIPELINE_ECR_REPOSITORY:-}" "")" "splunk/splunk-operator"

ECR_REGISTRY="$(first_nonempty "${PIPELINE_ECR_REGISTRY:-}" "${RESOLVED_ECR_REGISTRY}")"
ECR_IMAGE_REPOSITORY="${RESOLVED_IMAGE_REPOSITORY}"
resolve_ecr_region "${ECR_REGISTRY}"
ECR_REGION="${RESOLVED_ECR_REGION}"
REPOSITORY_NAME="${RESOLVED_REPOSITORY_NAME}"

if [ -z "${ECR_REGION}" ]; then
  echo "Unable to determine ECR region — set AWS_REGION, AWS_DEFAULT_REGION, or PIPELINE_AWS_DEFAULT_REGION" >&2
  exit 1
fi

export AWS_DEFAULT_REGION="${ECR_REGION}"

# Artifactory Setup
ARTIFACTORY_TARGET="$(first_nonempty "${PIPELINE_ARTIFACTORY_REPOSITORY:-}" "${JOB_ARTIFACTORY_REPOSITORY:-}" "")"
if [ -z "${ARTIFACTORY_TARGET}" ]; then
  case "${CI_COMMIT_REF_NAME:-}" in
    main|develop)
      ARTIFACTORY_TARGET="docker.repo.splunkdev.net"
      ;;
    *)
      ARTIFACTORY_TARGET="docker-test.repo.splunkdev.net"
      ;;
  esac
fi

ARTIFACTORY_IMAGE_PATH="$(first_nonempty "${PIPELINE_ARTIFACTORY_IMAGE_PATH:-}" "${JOB_ARTIFACTORY_IMAGE_PATH:-}" "sok/splunk-operator")"
case "${ARTIFACTORY_TARGET}" in
  */*)
    ARTIFACTORY_IMAGE_REPOSITORY="${ARTIFACTORY_TARGET}"
    ;;
  *)
    ARTIFACTORY_IMAGE_REPOSITORY="${ARTIFACTORY_TARGET}/${ARTIFACTORY_IMAGE_PATH}"
    append_context "${context_file}" "artifactory_image_path" "${ARTIFACTORY_IMAGE_PATH}"
    ;;
esac

IMAGE_TAG="$(first_nonempty "${PIPELINE_IMAGE_TAG:-}" "${JOB_IMAGE_TAG:-}" "${CI_COMMIT_SHA:-}")"
require_nonempty "${IMAGE_TAG}" "image tag"

ECR_IMAGE_REF="${ECR_IMAGE_REPOSITORY}:${IMAGE_TAG}"
ARTIFACTORY_IMAGE_REF="${ARTIFACTORY_IMAGE_REPOSITORY}:${IMAGE_TAG}"
BUILD_PLATFORMS="$(first_nonempty "${PIPELINE_BUILD_PLATFORMS:-}" "${JOB_BUILD_PLATFORMS:-}" "linux/amd64,linux/arm64")"
BUILD_DISTROLESS="false"
if bool_is_true "$(first_nonempty "${PIPELINE_BUILD_DISTROLESS:-}" "${JOB_BUILD_DISTROLESS:-}" "false")"; then
  BUILD_DISTROLESS="true"
fi
ECR_DISTROLESS_IMAGE_REF="${ECR_IMAGE_REPOSITORY}:${IMAGE_TAG}-distroless"
ARTIFACTORY_DISTROLESS_IMAGE_REF="${ARTIFACTORY_IMAGE_REPOSITORY}:${IMAGE_TAG}-distroless"

append_context "${context_file}" "ecr_registry" "${ECR_REGISTRY}"
append_context "${context_file}" "ecr_region" "${ECR_REGION}"
append_context "${context_file}" "image_repository_mode" "${RESOLVED_IMAGE_REPOSITORY_MODE}"
append_context "${context_file}" "image_repository" "${ECR_IMAGE_REPOSITORY}"
append_context "${context_file}" "artifactory_target" "${ARTIFACTORY_TARGET}"
append_context "${context_file}" "image_tag" "${IMAGE_TAG}"
append_context "${context_file}" "build_platforms" "${BUILD_PLATFORMS}"
append_context "${context_file}" "build_distroless" "${BUILD_DISTROLESS}"

printf '%s\n' "${ECR_IMAGE_REF}" > "ci-output/${WORKFLOW_SLUG}-ecr-image-ref.txt"
printf '%s\n' "${ARTIFACTORY_IMAGE_REF}" > "ci-output/${WORKFLOW_SLUG}-artifactory-image-ref.txt"
if [ "${BUILD_DISTROLESS}" = "true" ]; then
  printf '%s\n' "${ECR_DISTROLESS_IMAGE_REF}" > "ci-output/${WORKFLOW_SLUG}-ecr-distroless-image-ref.txt"
  printf '%s\n' "${ARTIFACTORY_DISTROLESS_IMAGE_REF}" > "ci-output/${WORKFLOW_SLUG}-artifactory-distroless-image-ref.txt"
fi

docker version
aws ecr get-login-password --region "${ECR_REGION}" | docker login --username AWS --password-stdin "${ECR_REGISTRY}"

# Let the Makefile and Dockerfile own their own defaults for BASE_IMAGE,
# BASE_IMAGE_VERSION, and BUILDER_IMAGE. Only pass what we must override.
make docker-buildx \
  IMG="${ECR_IMAGE_REF}" \
  PLATFORMS="${BUILD_PLATFORMS}"

make docker-buildx \
  IMG="${ARTIFACTORY_IMAGE_REF}" \
  PLATFORMS="${BUILD_PLATFORMS}"

if [ "${BUILD_DISTROLESS}" = "true" ]; then
  make docker-buildx \
    IMG="${ECR_DISTROLESS_IMAGE_REF}" \
    PLATFORMS="${BUILD_PLATFORMS}" \
    BASE_IMAGE="gcr.io/distroless/static" \
    BASE_IMAGE_VERSION="latest"

  make docker-buildx \
    IMG="${ARTIFACTORY_DISTROLESS_IMAGE_REF}" \
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
