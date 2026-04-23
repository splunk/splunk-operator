#!/bin/sh
set -eu

# Runtime contract
# - Purpose: build and publish the operator bundle and catalog images for the release.
# - Inputs: published release image contract, bundle registry target, and OCI auth.
# - Outputs: pushed bundle/catalog refs and a bundle contract for certification/submission jobs.
# - Guardrails: explicit manual/main path only; no hidden registry mutation.

. "${CI_PROJECT_DIR}/gitlab-ci/lib/pipeline-common.sh"

context_file="ci-output/${WORKFLOW_SLUG}-runtime-context.txt"
output_dir="ci-output/${WORKFLOW_SLUG}-output"
contract_file="${output_dir}/bundle-contract.env"
summary_file="${output_dir}/summary.txt"
release_contract_file="${RELEASE_IMAGE_CONTRACT_FILE:-ci-output/publish-release-images-output/release-image-contract.env}"

mkdir -p "ci-output" "${output_dir}"
: > "${context_file}"

load_repo_dotenv "${CI_PROJECT_DIR}/.env"
load_optional_env_file "${release_contract_file}"
resolve_release_version "${CI_PROJECT_DIR}/Makefile"
resolve_enterprise_release_image

bundle_target="$(first_nonempty "${PIPELINE_BUNDLE_REGISTRY:-}" "")"
require_nonempty "${bundle_target}" "PIPELINE_BUNDLE_REGISTRY"
resolve_pipeline_image_repository "${bundle_target}" "splunk/splunk-operator"

ci_bin_dir="${CI_PROJECT_DIR}/bin"
ensure_ci_bin_path "${ci_bin_dir}"
ensure_operator_sdk "${ci_bin_dir}" "$(first_nonempty "${OPERATOR_SDK_VERSION:-}" "")"

release_version="${RELEASE_VERSION:-${RESOLVED_RELEASE_VERSION}}"
release_image="${RELEASE_IMAGE:-docker.io/splunk/splunk-operator:${RESOLVED_RELEASE_OPERATOR_TAG}}"
enterprise_image="${RESOLVED_ENTERPRISE_IMAGE}"
bundle_registry="${RESOLVED_ECR_REGISTRY}"
image_tag_base="${RESOLVED_IMAGE_REPOSITORY}"
bundle_image="${image_tag_base}-bundle:v${release_version}"
catalog_image="${image_tag_base}-catalog:v${release_version}"

registry_username="$(first_nonempty "${PIPELINE_BUNDLE_REGISTRY_USERNAME:-}" "${PIPELINE_DOCKER_USERNAME:-}" "")"
registry_password="$(first_nonempty "${PIPELINE_BUNDLE_REGISTRY_PASSWORD:-}" "${PIPELINE_DOCKER_PASSWORD:-}" "")"

append_context "${context_file}" "release_version" "${release_version}"
append_context "${context_file}" "release_image" "${release_image}"
append_context "${context_file}" "bundle_image" "${bundle_image}"
append_context "${context_file}" "catalog_image" "${catalog_image}"

docker_login_registry "${bundle_registry}" "${registry_username}" "${registry_password}"

make bundle \
  IMAGE_TAG_BASE="${image_tag_base}" \
  VERSION="${release_version}" \
  IMG="${release_image}" \
  SPLUNK_ENTERPRISE_IMAGE="${enterprise_image}"

make bundle-build bundle-push catalog-build catalog-push \
  IMAGE_TAG_BASE="${image_tag_base}" \
  VERSION="${release_version}" \
  IMG="${release_image}"

cat > "${contract_file}" <<EOF
BUNDLE_IMG=${bundle_image}
CATALOG_IMG=${catalog_image}
IMAGE_TAG_BASE=${image_tag_base}
VERSION=${release_version}
EOF

cat > "${summary_file}" <<EOF
Published release bundle and catalog images.

- release_version: ${release_version}
- release_image: ${release_image}
- bundle_image: ${bundle_image}
- catalog_image: ${catalog_image}
- contract_file: ${contract_file}
EOF
