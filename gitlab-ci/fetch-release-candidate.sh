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
current_sha="${CI_COMMIT_SHA:-}"

mkdir -p "ci-output" "${output_dir}"
: > "${context_file}"

install_os_packages curl jq python3 unzip
require_commands curl jq python3 unzip

load_repo_dotenv "${CI_PROJECT_DIR}/.env"
resolve_release_version "${CI_PROJECT_DIR}/Makefile"

release_version="${RESOLVED_RELEASE_VERSION}"
resolved_source_ref=""
resolved_source_pipeline_id=""

download_release_candidate_for_exact_ref_sha() {
  candidate_ref="$1"
  expected_sha="$2"

  require_gitlab_job_token
  ensure_jq

  encoded_ref="$(urlencode "${candidate_ref}")"
  pipelines_json="$(curl --fail --location --silent --show-error \
    --header "JOB-TOKEN: ${CI_JOB_TOKEN}" \
    "${CI_API_V4_URL}/projects/${CI_PROJECT_ID}/pipelines?ref=${encoded_ref}&sha=${expected_sha}&status=success&per_page=20")"
  pipeline_ids="$(printf '%s' "${pipelines_json}" | jq -r '.[].id // empty')"
  require_nonempty "${pipeline_ids}" "successful release validation pipeline for ${candidate_ref} at ${expected_sha}"

  for pipeline_id in ${pipeline_ids}; do
    jobs_json="$(curl --fail --location --silent --show-error \
      --header "JOB-TOKEN: ${CI_JOB_TOKEN}" \
      "${CI_API_V4_URL}/projects/${CI_PROJECT_ID}/pipelines/${pipeline_id}/jobs?scope[]=success&per_page=100")"
    candidate_job_id="$(printf '%s' "${jobs_json}" | jq -r --arg job_name "${job_name}" '.[] | select(.name == $job_name) | .id' | head -n 1)"
    if [ -z "${candidate_job_id}" ]; then
      continue
    fi

    curl --fail --location --silent --show-error \
      --header "JOB-TOKEN: ${CI_JOB_TOKEN}" \
      "${CI_API_V4_URL}/projects/${CI_PROJECT_ID}/jobs/${candidate_job_id}/artifacts" \
      -o "${archive_file}"
    resolved_source_pipeline_id="${pipeline_id}"
    return 0
  done

  echo "Unable to find a successful ${job_name} job for ${candidate_ref} at ${expected_sha}" >&2
  return 1
}

if [ -n "${source_pipeline_id}" ]; then
  download_gitlab_job_artifacts_archive_by_pipeline "${source_pipeline_id}" "${job_name}" "${archive_file}"
else
  exact_current_ref_required="false"
  case "${current_ref}" in
    release/*|release-*)
      exact_current_ref_required="true"
      ;;
  esac

  if [ "${exact_current_ref_required}" = "true" ] && [ -z "${source_ref_override}" ]; then
    require_nonempty "${current_sha}" "current commit SHA for maintenance release publish"
    download_release_candidate_for_exact_ref_sha "${current_ref}" "${current_sha}"
    resolved_source_ref="${current_ref}"
  else
    for candidate_ref in \
      "${source_ref_override}" \
      "release-${release_version}" \
      "release/${release_version}"
    do
      if [ -z "${candidate_ref}" ]; then
        continue
      fi

      if [ "${exact_current_ref_required}" = "true" ] && [ "${candidate_ref}" = "${current_ref}" ]; then
        require_nonempty "${current_sha}" "current commit SHA for maintenance release publish"
        if download_release_candidate_for_exact_ref_sha "${candidate_ref}" "${current_sha}"; then
          resolved_source_ref="${candidate_ref}"
          break
        fi
      elif download_gitlab_job_artifacts_archive_by_ref "${candidate_ref}" "${job_name}" "${archive_file}"; then
        resolved_source_ref="${candidate_ref}"
        break
      fi
    done
  fi
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
elif [ -n "${resolved_source_pipeline_id}" ]; then
  printf 'RELEASE_SOURCE_FETCH_PIPELINE_ID=%s\n' "${resolved_source_pipeline_id}" >> "${candidate_contract_file}"
fi

append_context "${context_file}" "release_version" "${release_version}"
append_context "${context_file}" "release_candidate_job" "${job_name}"
append_context "${context_file}" "release_candidate_contract" "${candidate_contract_file}"
if [ -n "${resolved_source_ref}" ]; then
  append_context "${context_file}" "release_source_ref" "${resolved_source_ref}"
fi
if [ -n "${source_pipeline_id}" ]; then
  append_context "${context_file}" "release_source_pipeline_id" "${source_pipeline_id}"
elif [ -n "${resolved_source_pipeline_id}" ]; then
  append_context "${context_file}" "release_source_pipeline_id" "${resolved_source_pipeline_id}"
fi

cat > "${summary_file}" <<EOF
Fetched the validated release-candidate artifact set.

- release_version: ${release_version}
- release_candidate_job: ${job_name}
- release_source_ref: ${resolved_source_ref:-auto-not-used}
- release_source_pipeline_id: ${source_pipeline_id:-${resolved_source_pipeline_id:-not-set}}
- release_candidate_contract: ${candidate_contract_file}
EOF
