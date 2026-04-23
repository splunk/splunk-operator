#!/bin/sh
set -eu

# Runtime contract
# - Purpose: publish the supported release and distroless images from main.
# - Inputs: release version/tag, target repository, and OCI registry auth.
# - Outputs: published image refs and a contract file for downstream release jobs.
# - Guardrails: explicit main-only/manual trigger path; no implicit publish on release branches.

. "${CI_PROJECT_DIR}/gitlab-ci/lib/pipeline-common.sh"

context_file="ci-output/${WORKFLOW_SLUG}-runtime-context.txt"
output_dir="ci-output/${WORKFLOW_SLUG}-output"
contract_file="${output_dir}/release-image-contract.env"
summary_file="${output_dir}/summary.txt"

mkdir -p "ci-output" "${output_dir}"
: > "${context_file}"

load_repo_dotenv "${CI_PROJECT_DIR}/.env"
resolve_release_version "${CI_PROJECT_DIR}/Makefile"
resolve_release_image_repository

release_version="${RESOLVED_RELEASE_VERSION}"
operator_tag="${RESOLVED_RELEASE_OPERATOR_TAG}"
release_registry="${RESOLVED_RELEASE_IMAGE_REGISTRY}"
release_repository="${RESOLVED_RELEASE_IMAGE_REPOSITORY}"
release_image="${release_repository}:${operator_tag}"
distroless_image="${release_repository}:${operator_tag}-distroless"
update_latest="true"
if ! bool_is_true "$(first_nonempty "${PIPELINE_RELEASE_UPDATE_LATEST:-}" "true")"; then
  update_latest="false"
fi

registry_username="$(first_nonempty "${PIPELINE_RELEASE_REGISTRY_USERNAME:-}" "${PIPELINE_DOCKER_USERNAME:-}" "")"
registry_password="$(first_nonempty "${PIPELINE_RELEASE_REGISTRY_PASSWORD:-}" "${PIPELINE_DOCKER_PASSWORD:-}" "")"

append_context "${context_file}" "release_version" "${release_version}"
append_context "${context_file}" "release_repository" "${release_repository}"
append_context "${context_file}" "release_registry" "${release_registry}"
append_context "${context_file}" "operator_tag" "${operator_tag}"
append_context "${context_file}" "update_latest" "${update_latest}"

docker_login_registry "${release_registry}" "${registry_username}" "${registry_password}"

make docker-buildx IMG="${release_image}"
make docker-buildx \
  IMG="${distroless_image}" \
  BASE_IMAGE="gcr.io/distroless/static" \
  BASE_IMAGE_VERSION="latest"

latest_image=""
if [ "${update_latest}" = "true" ]; then
  latest_image="${release_repository}:latest"
  docker buildx imagetools create -t "${latest_image}" "${release_image}"
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
- release_image: ${release_image}
- distroless_image: ${distroless_image}
- latest_image: ${latest_image:-disabled}
- contract_file: ${contract_file}
EOF
