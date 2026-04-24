#!/bin/sh
set -eu

# Runtime contract
# - Purpose: publish the validated Helm chart archives to the approved OCI repo.
# - Inputs: release-candidate contract, chart OCI target, and Helm registry auth.
# - Outputs: packaged chart archives and push evidence under ci-output/.
# - Guardrails: OCI-only publication, no chart repackaging on main, and no
#   historical chart backfill in this job.

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

chart_repo="$(first_nonempty "${PIPELINE_CHART_RELEASE_REPOSITORY:-}" "${PIPELINE_RELEASED_HELM_REPO_URL:-}" "")"
require_nonempty "${chart_repo}" "PIPELINE_CHART_RELEASE_REPOSITORY"
case "${chart_repo}" in
  oci://*)
    chart_repo_path="${chart_repo#oci://}"
    chart_registry="${chart_repo_path%%/*}"
    ;;
  *)
    echo "PIPELINE_CHART_RELEASE_REPOSITORY must be an OCI target (oci://...)" >&2
    exit 1
    ;;
esac

chart_username="$(first_nonempty "${PIPELINE_CHART_RELEASE_USERNAME:-}" "${PIPELINE_DOCKER_USERNAME:-}" "")"
chart_password="$(first_nonempty "${PIPELINE_CHART_RELEASE_PASSWORD:-}" "${PIPELINE_DOCKER_PASSWORD:-}" "")"
candidate_root="$(first_nonempty "${RELEASE_CANDIDATE_ARTIFACT_ROOT:-}" "ci-output/fetch-release-candidate-output/release-candidate")"
operator_chart_archive="${candidate_root}/$(first_nonempty "${RELEASE_OPERATOR_CHART_ARCHIVE:-}" "")"
enterprise_chart_archive="${candidate_root}/$(first_nonempty "${RELEASE_ENTERPRISE_CHART_ARCHIVE:-}" "")"
require_file "${operator_chart_archive}" "validated splunk-operator chart archive"
require_file "${enterprise_chart_archive}" "validated splunk-enterprise chart archive"

ci_bin_dir="${CI_PROJECT_DIR}/bin"
ensure_ci_bin_path "${ci_bin_dir}"
make setup/helm \
  HELM_VERSION="$(first_nonempty "${PIPELINE_HELM_VERSION:-}" "${HELM_VERSION:-}" "v3.8.2")" \
  CI_BIN_DIR="${ci_bin_dir}"

helm_login_registry "${chart_registry}" "${chart_username}" "${chart_password}"

: > "${inventory_file}"
for chart_archive in "${operator_chart_archive}" "${enterprise_chart_archive}"; do
  helm push "${chart_archive}" "${chart_repo}"
  printf '%s\n' "${chart_archive}" >> "${inventory_file}"
done

append_context "${context_file}" "release_version" "${RESOLVED_RELEASE_VERSION}"
append_context "${context_file}" "chart_repo" "${chart_repo}"
append_context "${context_file}" "candidate_root" "${candidate_root}"

cat > "${summary_file}" <<EOF
Published release charts.

- release_version: ${RESOLVED_RELEASE_VERSION}
- chart_repo: ${chart_repo}
- operator_chart_archive: ${operator_chart_archive}
- enterprise_chart_archive: ${enterprise_chart_archive}
- inventory_file: ${inventory_file}
EOF
