#!/bin/sh
set -eu

# Runtime contract
# - Purpose: publish the validated deployment artifacts for the release.
# - Inputs: release-candidate contract plus the published release image contract.
# - Outputs: rendered GA release manifests under ci-output/.
# - Guardrails: artifact publication only; no manifest regeneration on main.

. "${CI_PROJECT_DIR}/gitlab-ci/lib/pipeline-common.sh"

context_file="ci-output/${WORKFLOW_SLUG}-runtime-context.txt"
output_dir="ci-output/${WORKFLOW_SLUG}-output"
summary_file="${output_dir}/summary.txt"
contract_file="${RELEASE_IMAGE_CONTRACT_FILE:-ci-output/publish-release-images-output/release-image-contract.env}"
release_candidate_contract_file="${RELEASE_CANDIDATE_CONTRACT_FILE:-ci-output/fetch-release-candidate-output/release-candidate/release-candidate-contract.env}"

mkdir -p "ci-output" "${output_dir}"
: > "${context_file}"

load_repo_dotenv "${CI_PROJECT_DIR}/.env"
require_file "${release_candidate_contract_file}" "release candidate contract"
require_file "${contract_file}" "release image contract"
load_optional_env_file "${contract_file}"
load_optional_env_file "${release_candidate_contract_file}"
resolve_release_version "${CI_PROJECT_DIR}/Makefile"

release_version="${RELEASE_VERSION:-${RESOLVED_RELEASE_VERSION}}"
release_image="${RELEASE_IMAGE:-}"
artifact_root="$(first_nonempty "${RELEASE_CANDIDATE_ARTIFACT_ROOT:-}" "ci-output/fetch-release-candidate-output/release-candidate")"
release_dir_name="$(first_nonempty "${RELEASE_ARTIFACT_DIRECTORY:-}" "release-${release_version}")"
release_archive_name="$(first_nonempty "${RELEASE_ARTIFACT_ARCHIVE:-}" "${release_dir_name}.tgz")"
release_archive_path="${artifact_root}/${release_archive_name}"

require_file "${release_archive_path}" "validated release artifact archive"

append_context "${context_file}" "release_version" "${release_version}"
append_context "${context_file}" "release_image" "${release_image}"
append_context "${context_file}" "release_artifact_archive" "${release_archive_path}"

tar -C "${output_dir}" -xzf "${release_archive_path}"

cat > "${summary_file}" <<EOF
Prepared release deployment artifacts.

- release_version: ${release_version}
- release_image: ${release_image:-recorded-in-release-image-contract}
- release_artifact_directory: ${release_dir_name}
- release_artifact_archive: ${release_archive_path}
EOF
