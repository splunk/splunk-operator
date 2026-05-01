#!/bin/sh
set -eu

# Runtime contract
# - Purpose: fetch the validated release-candidate artifact set into the active
#   publish pipeline.
# - Inputs: CI_JOB_TOKEN plus either a release source ref or a pinned source
#   pipeline ID.
# - Outputs: a local release-candidate directory and an augmented contract file
#   under ci-output/.
# - Guardrails: read-only GitLab artifact download; no registry mutation here.

. "${CI_PROJECT_DIR}/gitlab-ci/lib/pipeline-common.sh"

context_file="ci-output/${WORKFLOW_SLUG}-runtime-context.txt"
output_dir="ci-output/${WORKFLOW_SLUG}-output"
archive_file="${output_dir}/release-candidate-artifacts.zip"
unpack_dir="${output_dir}/artifact-unpack"
candidate_dir="${output_dir}/release-candidate"
candidate_contract_file="${candidate_dir}/release-candidate-contract.env"
summary_file="${output_dir}/summary.txt"
job_name="$(first_nonempty "${PIPELINE_RELEASE_CANDIDATE_JOB_NAME:-}" "release-candidate-packaging")"
source_pipeline_id="$(first_nonempty "${PIPELINE_RELEASE_SOURCE_PIPELINE_ID:-}" "")"
source_ref_override="$(first_nonempty "${PIPELINE_RELEASE_SOURCE_REF:-}" "")"
current_ref="${CI_COMMIT_BRANCH:-}"

mkdir -p "ci-output" "${output_dir}"
: > "${context_file}"

install_os_packages curl jq python3 unzip
require_commands curl jq python3 unzip

load_repo_dotenv "${CI_PROJECT_DIR}/.env"
resolve_release_version "${CI_PROJECT_DIR}/Makefile"

release_version="${RESOLVED_RELEASE_VERSION}"
resolved_source_ref=""

if [ -n "${source_pipeline_id}" ]; then
  download_gitlab_job_artifacts_archive_by_pipeline "${source_pipeline_id}" "${job_name}" "${archive_file}"
else
  for candidate_ref in \
    "${source_ref_override}" \
    "${current_ref}" \
    "release-${release_version}" \
    "release/${release_version}"
  do
    if [ -z "${candidate_ref}" ]; then
      continue
    fi

    if download_gitlab_job_artifacts_archive_by_ref "${candidate_ref}" "${job_name}" "${archive_file}"; then
      resolved_source_ref="${candidate_ref}"
      break
    fi
  done
fi

if [ ! -f "${archive_file}" ]; then
  echo "Unable to fetch ${job_name} artifacts for release ${release_version}. Set PIPELINE_RELEASE_SOURCE_REF or PIPELINE_RELEASE_SOURCE_PIPELINE_ID." >&2
  exit 1
fi

require_file "${archive_file}" "release candidate artifact archive"
rm -rf "${unpack_dir}" "${candidate_dir}"
mkdir -p "${unpack_dir}" "${candidate_dir}"
unzip -q "${archive_file}" -d "${unpack_dir}"

downloaded_contract_file="$(find "${unpack_dir}" -type f -name 'release-candidate-contract.env' | head -n 1)"
require_file "${downloaded_contract_file}" "downloaded release candidate contract"
downloaded_candidate_root="$(dirname "${downloaded_contract_file}")"
cp -R "${downloaded_candidate_root}/." "${candidate_dir}/"

printf '\nRELEASE_CANDIDATE_ARTIFACT_ROOT=%s\n' "${candidate_dir}" >> "${candidate_contract_file}"
if [ -n "${resolved_source_ref}" ]; then
  printf 'RELEASE_SOURCE_FETCH_REF=%s\n' "${resolved_source_ref}" >> "${candidate_contract_file}"
fi
if [ -n "${source_pipeline_id}" ]; then
  printf 'RELEASE_SOURCE_FETCH_PIPELINE_ID=%s\n' "${source_pipeline_id}" >> "${candidate_contract_file}"
fi

append_context "${context_file}" "release_version" "${release_version}"
append_context "${context_file}" "release_candidate_job" "${job_name}"
append_context "${context_file}" "release_candidate_contract" "${candidate_contract_file}"
if [ -n "${resolved_source_ref}" ]; then
  append_context "${context_file}" "release_source_ref" "${resolved_source_ref}"
fi
if [ -n "${source_pipeline_id}" ]; then
  append_context "${context_file}" "release_source_pipeline_id" "${source_pipeline_id}"
fi

cat > "${summary_file}" <<EOF
Fetched the validated release-candidate artifact set.

- release_version: ${release_version}
- release_candidate_job: ${job_name}
- release_source_ref: ${resolved_source_ref:-auto-not-used}
- release_source_pipeline_id: ${source_pipeline_id:-not-set}
- release_candidate_contract: ${candidate_contract_file}
EOF
