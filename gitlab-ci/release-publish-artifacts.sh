#!/bin/sh
set -eu

# Runtime contract
# - Purpose: generate the shipped deployment artifacts against the published release image.
# - Inputs: published release image contract, repo VERSION, and release Enterprise image pin.
# - Outputs: rendered GA release manifests under ci-output/.
# - Guardrails: render only; no GitHub release mutation or public note publication here.

. "${CI_PROJECT_DIR}/gitlab-ci/lib/pipeline-common.sh"

context_file="ci-output/${WORKFLOW_SLUG}-runtime-context.txt"
output_dir="ci-output/${WORKFLOW_SLUG}-output"
summary_file="${output_dir}/summary.txt"
contract_file="${RELEASE_IMAGE_CONTRACT_FILE:-ci-output/publish-release-images-output/release-image-contract.env}"

mkdir -p "ci-output" "${output_dir}"
: > "${context_file}"

load_repo_dotenv "${CI_PROJECT_DIR}/.env"
load_optional_env_file "${contract_file}"
resolve_release_version "${CI_PROJECT_DIR}/Makefile"
resolve_release_image_repository
resolve_enterprise_release_image

release_version="${RELEASE_VERSION:-${RESOLVED_RELEASE_VERSION}}"
release_image="${RELEASE_IMAGE:-${RESOLVED_RELEASE_IMAGE_REPOSITORY}:${RESOLVED_RELEASE_OPERATOR_TAG}}"
enterprise_image="${RESOLVED_ENTERPRISE_IMAGE}"
release_dir="${CI_PROJECT_DIR}/release-${release_version}"

append_context "${context_file}" "release_version" "${release_version}"
append_context "${context_file}" "release_image" "${release_image}"
append_context "${context_file}" "enterprise_image" "${enterprise_image}"

make generate-artifacts \
  IMG="${release_image}" \
  VERSION="${release_version}" \
  SPLUNK_ENTERPRISE_IMAGE="${enterprise_image}" \
  SPLUNK_GENERAL_TERMS="--accept-sgt-current-at-splunk-com" \
  WATCH_NAMESPACE=""

cp -R "${release_dir}" "${output_dir}/"

cat > "${summary_file}" <<EOF
Prepared release deployment artifacts.

- release_version: ${release_version}
- release_image: ${release_image}
- enterprise_image: ${enterprise_image}
EOF
