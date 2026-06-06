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
publish_dir="${output_dir}/publish-charts"
release_candidate_contract_file="${RELEASE_CANDIDATE_CONTRACT_FILE:-ci-output/fetch-release-candidate-output/release-candidate/release-candidate-contract.env}"

mkdir -p "ci-output" "${output_dir}" "${publish_dir}"
: > "${context_file}"

load_repo_dotenv "${CI_PROJECT_DIR}/.env"
require_file "${release_candidate_contract_file}" "release candidate contract"
load_optional_env_file "${release_candidate_contract_file}"
resolve_release_version "${CI_PROJECT_DIR}/Makefile"

chart_repo="https://repo.splunkdev.net/artifactory/helm/sok/splunk-operator"
candidate_root="$(first_nonempty "${RELEASE_CANDIDATE_ARTIFACT_ROOT:-}" "ci-output/fetch-release-candidate-output/release-candidate")"
operator_chart_archive="${candidate_root}/$(first_nonempty "${RELEASE_OPERATOR_CHART_ARCHIVE:-}" "")"
enterprise_chart_archive="${candidate_root}/$(first_nonempty "${RELEASE_ENTERPRISE_CHART_ARCHIVE:-}" "")"
uf_chart_archive="${candidate_root}/$(first_nonempty "${RELEASE_UF_CHART_ARCHIVE:-}" "")"
require_file "${operator_chart_archive}" "validated splunk-operator chart archive"
require_file "${enterprise_chart_archive}" "validated splunk-enterprise chart archive"
require_file "${uf_chart_archive}" "validated splunk-universalforwarder chart archive"

: > "${inventory_file}"
rm -f "${publish_dir}/"*.tgz
cp "${operator_chart_archive}" "${publish_dir}/"
cp "${enterprise_chart_archive}" "${publish_dir}/"
cp "${uf_chart_archive}" "${publish_dir}/"
require_commands artifact-ci
artifact-ci publish helm -d "${publish_dir}" sok/splunk-operator
for chart_archive in "${operator_chart_archive}" "${enterprise_chart_archive}" "${uf_chart_archive}"; do
  published_chart_url="${chart_repo%/}/$(basename "${chart_archive}")"
  printf '%s\n' "${published_chart_url}" >> "${inventory_file}"
done

append_context "${context_file}" "release_version" "${RESOLVED_RELEASE_VERSION}"
append_context "${context_file}" "chart_repo" "${chart_repo}"
append_context "${context_file}" "candidate_root" "${candidate_root}"
append_context "${context_file}" "publish_dir" "${publish_dir}"

cat > "${summary_file}" <<EOF
Published release charts.

- release_version: ${RESOLVED_RELEASE_VERSION}
- chart_repo: ${chart_repo}
- operator_chart_archive: ${operator_chart_archive}
- enterprise_chart_archive: ${enterprise_chart_archive}
- publish_dir: ${publish_dir}
- inventory_file: ${inventory_file}
EOF
