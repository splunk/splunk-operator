#!/bin/sh
set -eu

# Runtime contract
# - Purpose: publish the validated Helm chart archives to the approved Artifactory
#   Helm repository.
# - Inputs: release-candidate contract, chart publish base URL, and Artifactory
#   publish auth.
# - Outputs: packaged chart archives and publish evidence under ci-output/.
# - Guardrails: repo-style Artifactory publication, no chart repackaging during
#   publication, and no historical chart backfill in this job.

. "${CI_PROJECT_DIR}/gitlab-ci/lib/pipeline-common.sh"

context_file="ci-output/${WORKFLOW_SLUG}-runtime-context.txt"
output_dir="ci-output/${WORKFLOW_SLUG}-output"
summary_file="${output_dir}/summary.txt"
inventory_file="${output_dir}/published-charts.txt"
release_candidate_contract_file="${RELEASE_CANDIDATE_CONTRACT_FILE:-ci-output/fetch-release-candidate-output/release-candidate/release-candidate-contract.env}"

mkdir -p "ci-output" "${output_dir}"
: > "${context_file}"

load_repo_dotenv "${CI_PROJECT_DIR}/.env"
require_file "${release_candidate_contract_file}" "release candidate contract"
load_optional_env_file "${release_candidate_contract_file}"
resolve_release_version "${CI_PROJECT_DIR}/Makefile"

chart_repo="$(first_nonempty \
  "${PIPELINE_CHART_RELEASE_REPOSITORY:-}" \
  "${JOB_CHART_RELEASE_REPOSITORY:-}" \
  "${DEFAULT_CHART_RELEASE_REPOSITORY:-}" \
  "")"
require_nonempty "${chart_repo}" "PIPELINE_CHART_RELEASE_REPOSITORY"
case "${chart_repo}" in
  https://*/artifactory/*) ;;
  *)
    echo "PIPELINE_CHART_RELEASE_REPOSITORY must be an Artifactory publish URL (https://.../artifactory/...)" >&2
    exit 1
    ;;
esac

chart_repo_url="$(first_nonempty \
  "${PIPELINE_CHART_RELEASE_REPO_URL:-}" \
  "${JOB_CHART_RELEASE_REPO_URL:-}" \
  "${DEFAULT_CHART_RELEASE_REPO_URL:-}" \
  "${PIPELINE_RELEASED_HELM_REPO_URL:-}" \
  "$(artifactory_helm_repo_url_from_publish_base "${chart_repo}")" \
  "")"
require_nonempty "${chart_repo_url}" "PIPELINE_CHART_RELEASE_REPO_URL"

chart_username="$(first_nonempty "${PIPELINE_CHART_RELEASE_USERNAME:-}" "")"
chart_password="$(first_nonempty "${PIPELINE_CHART_RELEASE_PASSWORD:-}" "")"
chart_artifactory_role="$(first_nonempty \
  "${PIPELINE_CHART_RELEASE_ARTIFACTORY_ROLE:-}" \
  "${JOB_CHART_RELEASE_ARTIFACTORY_ROLE:-}" \
  "")"
candidate_root="$(first_nonempty "${RELEASE_CANDIDATE_ARTIFACT_ROOT:-}" "ci-output/fetch-release-candidate-output/release-candidate")"
operator_chart_archive="${candidate_root}/$(first_nonempty "${RELEASE_OPERATOR_CHART_ARCHIVE:-}" "")"
enterprise_chart_archive="${candidate_root}/$(first_nonempty "${RELEASE_ENTERPRISE_CHART_ARCHIVE:-}" "")"
require_file "${operator_chart_archive}" "validated splunk-operator chart archive"
require_file "${enterprise_chart_archive}" "validated splunk-enterprise chart archive"

: > "${inventory_file}"
resolve_artifactory_publish_auth "${chart_username}" "${chart_password}" "${chart_artifactory_role}"
for chart_archive in "${operator_chart_archive}" "${enterprise_chart_archive}"; do
  published_chart_url="${chart_repo%/}/$(basename "${chart_archive}")"
  artifactory_upload_file "${published_chart_url}" "${chart_archive}"
  printf '%s\n' "${published_chart_url}" >> "${inventory_file}"
done

append_context "${context_file}" "release_version" "${RESOLVED_RELEASE_VERSION}"
append_context "${context_file}" "chart_repo" "${chart_repo}"
append_context "${context_file}" "chart_repo_url" "${chart_repo_url}"
append_context "${context_file}" "candidate_root" "${candidate_root}"

cat > "${summary_file}" <<EOF
Published release charts.

- release_version: ${RESOLVED_RELEASE_VERSION}
- chart_repo: ${chart_repo}
- chart_repo_url: ${chart_repo_url}
- operator_chart_archive: ${operator_chart_archive}
- enterprise_chart_archive: ${enterprise_chart_archive}
- inventory_file: ${inventory_file}
EOF
