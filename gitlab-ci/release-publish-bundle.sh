#!/bin/sh
set -eu

# Runtime contract
# - Purpose: promote the validated candidate bundle and catalog images for the release.
# - Inputs: release-candidate contract, published release image contract, bundle registry target, and OCI auth.
# - Outputs: pushed bundle/catalog refs and a bundle contract for certification/submission jobs.
# - Guardrails: explicit manual release-publish path only; no bundle/catalog
#   rebuild during publication.

. "${CI_PROJECT_DIR}/gitlab-ci/lib/pipeline-common.sh"

context_file="ci-output/${WORKFLOW_SLUG}-runtime-context.txt"
output_dir="ci-output/${WORKFLOW_SLUG}-output"
contract_file="${output_dir}/bundle-contract.env"
summary_file="${output_dir}/summary.txt"
release_contract_file="${RELEASE_IMAGE_CONTRACT_FILE:-ci-output/publish-release-images-output/release-image-contract.env}"
release_candidate_contract_file="${RELEASE_CANDIDATE_CONTRACT_FILE:-ci-output/fetch-release-candidate-output/release-candidate/release-candidate-contract.env}"

mkdir -p "ci-output" "${output_dir}"
: > "${context_file}"

load_repo_dotenv "${CI_PROJECT_DIR}/.env"
require_file "${release_candidate_contract_file}" "release candidate contract"
require_file "${release_contract_file}" "release image contract"
load_optional_env_file "${release_contract_file}"
load_optional_env_file "${release_candidate_contract_file}"
resolve_release_version "${CI_PROJECT_DIR}/Makefile"

release_version="${RELEASE_VERSION:-${RESOLVED_RELEASE_VERSION}}"
bundle_image="${RELEASE_BUNDLE_IMAGE:-}"
catalog_image="${RELEASE_CATALOG_IMAGE:-}"
candidate_bundle_image="$(first_nonempty "${RELEASE_BUNDLE_CANDIDATE_IMAGE:-}" "")"
candidate_catalog_image="$(first_nonempty "${RELEASE_CATALOG_CANDIDATE_IMAGE:-}" "")"
release_image="${RELEASE_IMAGE:-}"
require_nonempty "${bundle_image}" "published release bundle image"
require_nonempty "${catalog_image}" "published release catalog image"
require_nonempty "${candidate_bundle_image}" "release candidate bundle image"
require_nonempty "${candidate_catalog_image}" "release candidate catalog image"
require_nonempty "${release_image}" "published release operator image"

bundle_registry="$(registry_host_from_image_ref "${bundle_image}")"
candidate_bundle_registry="$(registry_host_from_image_ref "${candidate_bundle_image}")"
candidate_catalog_registry="$(registry_host_from_image_ref "${candidate_catalog_image}")"

registry_username="$(first_nonempty "${PIPELINE_BUNDLE_REGISTRY_USERNAME:-}" "${PIPELINE_DOCKER_USERNAME:-}" "")"
registry_password="$(first_nonempty "${PIPELINE_BUNDLE_REGISTRY_PASSWORD:-}" "${PIPELINE_DOCKER_PASSWORD:-}" "")"

append_context "${context_file}" "release_version" "${release_version}"
append_context "${context_file}" "release_image" "${release_image}"
append_context "${context_file}" "bundle_image" "${bundle_image}"
append_context "${context_file}" "catalog_image" "${catalog_image}"
append_context "${context_file}" "candidate_bundle_image" "${candidate_bundle_image}"
append_context "${context_file}" "candidate_catalog_image" "${candidate_catalog_image}"

docker_login_registry "${candidate_bundle_registry}" "${registry_username}" "${registry_password}"
if [ "${candidate_catalog_registry}" != "${candidate_bundle_registry}" ]; then
  docker_login_registry "${candidate_catalog_registry}" "${registry_username}" "${registry_password}"
fi
docker_login_registry "${bundle_registry}" "${registry_username}" "${registry_password}"

docker buildx imagetools create -t "${bundle_image}" "${candidate_bundle_image}"
docker buildx imagetools create -t "${catalog_image}" "${candidate_catalog_image}"

cat > "${contract_file}" <<EOF
BUNDLE_IMG=${bundle_image}
CATALOG_IMG=${catalog_image}
VERSION=${release_version}
EOF

cat > "${summary_file}" <<EOF
Promoted release bundle and catalog images.

- release_version: ${release_version}
- release_image: ${release_image}
- candidate_bundle_image: ${candidate_bundle_image}
- candidate_catalog_image: ${candidate_catalog_image}
- bundle_image: ${bundle_image}
- catalog_image: ${catalog_image}
- contract_file: ${contract_file}
EOF
