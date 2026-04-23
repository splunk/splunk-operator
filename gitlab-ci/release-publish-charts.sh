#!/bin/sh
set -eu

# Runtime contract
# - Purpose: package and publish the release Helm charts to the approved OCI repo.
# - Inputs: chart OCI target and Helm registry auth.
# - Outputs: packaged chart archives and push evidence under ci-output/.
# - Guardrails: OCI-only publication; no GitHub Pages mutation from this lane.

. "${CI_PROJECT_DIR}/gitlab-ci/lib/pipeline-common.sh"

context_file="ci-output/${WORKFLOW_SLUG}-runtime-context.txt"
output_dir="ci-output/${WORKFLOW_SLUG}-output"
summary_file="${output_dir}/summary.txt"
inventory_file="${output_dir}/published-charts.txt"

mkdir -p "ci-output" "${output_dir}"
: > "${context_file}"

load_repo_dotenv "${CI_PROJECT_DIR}/.env"
resolve_release_version "${CI_PROJECT_DIR}/Makefile"

chart_repo="$(first_nonempty "${PIPELINE_CHART_RELEASE_REPOSITORY:-}" "")"
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
require_nonempty "${chart_username}" "chart registry username"
require_nonempty "${chart_password}" "chart registry password"

ci_bin_dir="${CI_PROJECT_DIR}/bin"
ensure_ci_bin_path "${ci_bin_dir}"
make setup/helm \
  HELM_VERSION="$(first_nonempty "${PIPELINE_HELM_VERSION:-}" "${HELM_VERSION:-}" "v3.8.2")" \
  CI_BIN_DIR="${ci_bin_dir}"

printf '%s' "${chart_password}" | helm registry login "${chart_registry}" --username "${chart_username}" --password-stdin

make helm-package
helm package "${CI_PROJECT_DIR}/helm-chart/splunk-operator" --destination "${output_dir}"
helm package "${CI_PROJECT_DIR}/helm-chart/splunk-enterprise" --destination "${output_dir}"

: > "${inventory_file}"
for chart_archive in "${output_dir}"/*.tgz; do
  helm push "${chart_archive}" "${chart_repo}"
  printf '%s\n' "${chart_archive}" >> "${inventory_file}"
done

append_context "${context_file}" "release_version" "${RESOLVED_RELEASE_VERSION}"
append_context "${context_file}" "chart_repo" "${chart_repo}"

cat > "${summary_file}" <<EOF
Published release charts.

- release_version: ${RESOLVED_RELEASE_VERSION}
- chart_repo: ${chart_repo}
- inventory_file: ${inventory_file}
EOF
