#!/bin/sh
set -eu

# Runtime contract
# - Purpose: promote the validated release-candidate images from main.
# - Inputs: release-candidate contract, target repository, and OCI registry auth.
# - Outputs: published image refs and a contract file for downstream release jobs.
# - Guardrails: explicit main-only/manual trigger path; no rebuilds after
#   release-branch validation completes.

. "${CI_PROJECT_DIR}/gitlab-ci/lib/pipeline-common.sh"

context_file="ci-output/${WORKFLOW_SLUG}-runtime-context.txt"
output_dir="ci-output/${WORKFLOW_SLUG}-output"
contract_file="${output_dir}/release-image-contract.env"
summary_file="${output_dir}/summary.txt"
release_candidate_contract_file="${RELEASE_CANDIDATE_CONTRACT_FILE:-ci-output/fetch-release-candidate-output/release-candidate/release-candidate-contract.env}"

mkdir -p "ci-output" "${output_dir}"
: > "${context_file}"

load_repo_dotenv "${CI_PROJECT_DIR}/.env"
require_file "${release_candidate_contract_file}" "release candidate contract"
load_optional_env_file "${release_candidate_contract_file}"
resolve_release_version "${CI_PROJECT_DIR}/Makefile"
resolve_release_image_repository

release_version="${RELEASE_VERSION:-${RESOLVED_RELEASE_VERSION}}"
operator_tag="${RELEASE_OPERATOR_TAG:-${RESOLVED_RELEASE_OPERATOR_TAG}}"
release_registry="${RELEASE_IMAGE_REGISTRY:-${RESOLVED_RELEASE_IMAGE_REGISTRY}}"
release_repository="${RELEASE_IMAGE_REPOSITORY:-${RESOLVED_RELEASE_IMAGE_REPOSITORY}}"
release_image="${RELEASE_IMAGE:-${release_repository}:${operator_tag}}"
distroless_image="${RELEASE_DISTROLESS_IMAGE:-${release_repository}:${operator_tag}-distroless}"
candidate_image_source="$(first_nonempty "${RELEASE_CANDIDATE_IMAGE_SOURCE:-}" "${RELEASE_CANDIDATE_IMAGE:-}" "")"
candidate_distroless_image_source="$(first_nonempty "${RELEASE_CANDIDATE_DISTROLESS_IMAGE_SOURCE:-}" "${RELEASE_CANDIDATE_DISTROLESS_IMAGE:-}" "")"
require_nonempty "${candidate_image_source}" "release candidate operator image source"
require_nonempty "${candidate_distroless_image_source}" "release candidate distroless image source"

update_latest="true"
if ! bool_is_true "$(first_nonempty "${PIPELINE_RELEASE_UPDATE_LATEST:-}" "true")"; then
  update_latest="false"
fi

registry_username="$(first_nonempty "${PIPELINE_RELEASE_REGISTRY_USERNAME:-}" "${PIPELINE_DOCKER_USERNAME:-}" "")"
registry_password="$(first_nonempty "${PIPELINE_RELEASE_REGISTRY_PASSWORD:-}" "${PIPELINE_DOCKER_PASSWORD:-}" "")"
candidate_registry="$(registry_host_from_image_ref "${candidate_image_source}")"
candidate_distroless_registry="$(registry_host_from_image_ref "${candidate_distroless_image_source}")"

append_context "${context_file}" "release_version" "${release_version}"
append_context "${context_file}" "release_repository" "${release_repository}"
append_context "${context_file}" "release_registry" "${release_registry}"
append_context "${context_file}" "operator_tag" "${operator_tag}"
append_context "${context_file}" "update_latest" "${update_latest}"
append_context "${context_file}" "candidate_image_source" "${candidate_image_source}"
append_context "${context_file}" "candidate_distroless_image_source" "${candidate_distroless_image_source}"

docker_login_registry "${candidate_registry}" "" ""
if [ "${candidate_distroless_registry}" != "${candidate_registry}" ]; then
  docker_login_registry "${candidate_distroless_registry}" "" ""
fi
docker_login_registry "${release_registry}" "${registry_username}" "${registry_password}"

docker buildx imagetools create -t "${release_image}" "${candidate_image_source}"
docker buildx imagetools create -t "${distroless_image}" "${candidate_distroless_image_source}"

latest_image=""
if [ "${update_latest}" = "true" ]; then
  latest_image="${release_repository}:latest"
  docker buildx imagetools create -t "${latest_image}" "${candidate_image_source}"
fi

cat > "${contract_file}" <<EOF
RELEASE_VERSION=${release_version}
RELEASE_OPERATOR_TAG=${operator_tag}
RELEASE_IMAGE=${release_image}
RELEASE_DISTROLESS_IMAGE=${distroless_image}
RELEASE_LATEST_IMAGE=${latest_image}
RELEASE_IMAGE_REGISTRY=${release_registry}
RELEASE_IMAGE_REPOSITORY=${release_repository}
EOF

cat > "${summary_file}" <<EOF
Published release images.

- release_version: ${release_version}
- operator_tag: ${operator_tag}
- candidate_image_source: ${candidate_image_source}
- candidate_distroless_image_source: ${candidate_distroless_image_source}
- release_image: ${release_image}
- distroless_image: ${distroless_image}
- latest_image: ${latest_image:-disabled}
- contract_file: ${contract_file}
EOF
