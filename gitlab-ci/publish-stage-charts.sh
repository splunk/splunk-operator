#!/bin/sh
set -eu

# Runtime contract
# - Purpose: package branch snapshot Helm charts and publish them to the
#   approved internal Artifactory Helm repository for develop-lane consumers.
# - Inputs: chart publish base URL, Artifactory publish auth, repo Helm sources, and CI
#   metadata used to derive a collision-safe prerelease chart version.
# - Outputs: packaged charts, published chart URLs, and a summary under
#   ci-output/.
# - Guardrails: repo-style Artifactory publication, prerelease chart versions
#   for non-release pipelines, stage-image alignment for develop testing, and no mutation of
#   the checked-in chart sources.

. "${CI_PROJECT_DIR}/gitlab-ci/lib/pipeline-common.sh"

context_file="ci-output/${WORKFLOW_SLUG}-runtime-context.txt"
output_dir="ci-output/${WORKFLOW_SLUG}-output"
workspace_dir="${output_dir}/workspace"
package_dir="${output_dir}/packages"
summary_file="${output_dir}/summary.txt"
inventory_file="${output_dir}/published-charts.txt"
operator_chart_ref_file="ci-output/${WORKFLOW_SLUG}-operator-chart-ref.txt"
uf_chart_ref_file="ci-output/${WORKFLOW_SLUG}-uf-chart-ref.txt"
enterprise_chart_ref_file="ci-output/${WORKFLOW_SLUG}-enterprise-chart-ref.txt"
build_image_ref_file="${BUILD_IMAGE_REF_FILE:-ci-output/build-test-push-workflow-artifactory-image-ref.txt}"

mkdir -p "ci-output" "${output_dir}" "${package_dir}"
: > "${context_file}"

load_repo_dotenv "${CI_PROJECT_DIR}/.env"
resolve_release_version "${CI_PROJECT_DIR}/Makefile"
require_file "${build_image_ref_file}" "validated staged operator image reference"
stage_operator_image="$(cat "${build_image_ref_file}")"
require_nonempty "${stage_operator_image}" "staged operator image reference"

case "${CI_COMMIT_REF_NAME:-}" in
  main|develop)
    chart_repo="https://repo.splunkdev.net/artifactory/helm/sok/splunk-operator"
    ;;
  *)
    chart_repo="https://repo.splunkdev.net/artifactory/helm-test/sok/splunk-operator"
    ;;
esac

sanitize_chart_prerelease_token() {
  token="$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]')"
  token="$(printf '%s' "${token}" | tr -cs 'a-z0-9-' '-')"
  token="$(printf '%s' "${token}" | sed -E 's/^-+//; s/-+$//')"
  if [ -z "${token}" ]; then
    token="dev"
  fi
  printf '%s' "${token}"
}

base_version="${RESOLVED_RELEASE_VERSION}"
snapshot_channel="$(sanitize_chart_prerelease_token "$(first_nonempty "${PIPELINE_HELM_CHART_CHANNEL:-}" "${CI_COMMIT_REF_NAME:-develop}")")"
snapshot_build_id="$(first_nonempty "${PIPELINE_HELM_CHART_BUILD_ID:-}" "${CI_PIPELINE_IID:-}" "${CI_PIPELINE_ID:-}" "${CI_JOB_ID:-}" "0")"
snapshot_sha="$(first_nonempty "${CI_COMMIT_SHORT_SHA:-}" "${CI_COMMIT_SHA:-}" "")"
snapshot_sha="$(printf '%.12s' "${snapshot_sha}")"

chart_version="$(first_nonempty \
  "${PIPELINE_HELM_CHART_VERSION:-}" \
  "${JOB_HELM_CHART_VERSION:-}" \
  "${base_version}-${snapshot_channel}.${snapshot_build_id}.${snapshot_sha}")"
chart_app_version="$(first_nonempty \
  "${PIPELINE_HELM_CHART_APP_VERSION:-}" \
  "${JOB_HELM_CHART_APP_VERSION:-}" \
  "${CI_COMMIT_SHA:-${base_version}}")"

ci_bin_dir="${CI_PROJECT_DIR}/bin"
ensure_ci_bin_path "${ci_bin_dir}"
helm_version="$(first_nonempty "${PIPELINE_HELM_VERSION:-}" "${JOB_HELM_VERSION:-}" "v3.8.2")"
append_context "${context_file}" "helm_version" "${helm_version}"
make setup/helm \
  HELM_VERSION="${helm_version}" \
  CI_BIN_DIR="${ci_bin_dir}"

rm -rf "${workspace_dir}"
mkdir -p "${workspace_dir}"
cp -R "${CI_PROJECT_DIR}/helm-chart" "${workspace_dir}/"

operator_chart_dir="${workspace_dir}/helm-chart/splunk-operator"
enterprise_chart_dir="${workspace_dir}/helm-chart/splunk-enterprise"
enterprise_dependency_dir="${enterprise_chart_dir}/charts"
enterprise_chart_yaml="${enterprise_chart_dir}/Chart.yaml"
operator_values_yaml="${operator_chart_dir}/values.yaml"

uf_chart_dir="${workspace_dir}/helm-chart/splunk-universalforwarder"

mkdir -p "${enterprise_dependency_dir}"
rm -f "${enterprise_dependency_dir}/splunk-operator-"*.tgz
rm -f "${enterprise_dependency_dir}/splunk-universalforwarder-"*.tgz

# Keep the develop snapshot chart aligned with the exact staged operator image
# that passed the same pipeline's build path instead of the checked-in GA
# docker.io default.
sed -i -E '/^[[:space:]]+repository: docker\.io\/splunk\/splunk-operator:/ s#docker\.io/splunk/splunk-operator:[^"]*#'"${stage_operator_image}"'#' "${operator_values_yaml}"

helm package "${operator_chart_dir}" \
  --version "${chart_version}" \
  --app-version "${chart_app_version}" \
  --destination "${package_dir}"
operator_chart_archive="${package_dir}/splunk-operator-${chart_version}.tgz"
require_file "${operator_chart_archive}" "packaged splunk-operator chart"
cp "${operator_chart_archive}" "${enterprise_dependency_dir}/"

helm package "${uf_chart_dir}" \
  --version "${chart_version}" \
  --app-version "${chart_app_version}" \
  --destination "${package_dir}"
uf_chart_archive="${package_dir}/splunk-universalforwarder-${chart_version}.tgz"
require_file "${uf_chart_archive}" "packaged splunk-universalforwarder chart"
cp "${uf_chart_archive}" "${enterprise_dependency_dir}/"

# Keep the parent chart dependency metadata aligned with the packaged snapshot
# versions so Helm does not reject the staged dependency set.
sed -i -E '/^- name: splunk-operator$/,/^(  repository:|  condition:)/ s/^  version: .*/  version: "'"${chart_version}"'"/' "${enterprise_chart_yaml}"
sed -i -E '/^- name: splunk-universalforwarder$/,/^(  repository:|  condition:)/ s/^  version: .*/  version: "'"${chart_version}"'"/' "${enterprise_chart_yaml}"

helm package "${enterprise_chart_dir}" \
  --version "${chart_version}" \
  --app-version "${chart_app_version}" \
  --destination "${package_dir}"
enterprise_chart_archive="${package_dir}/splunk-enterprise-${chart_version}.tgz"
require_file "${enterprise_chart_archive}" "packaged splunk-enterprise chart"

require_commands artifact-ci
operator_chart_ref="${chart_repo%/}/$(basename "${operator_chart_archive}")"
uf_chart_ref="${chart_repo%/}/$(basename "${uf_chart_archive}")"
enterprise_chart_ref="${chart_repo%/}/$(basename "${enterprise_chart_archive}")"
artifact-ci publish helm -d "${package_dir}" sok/splunk-operator

printf '%s\n' "${operator_chart_ref}" > "${operator_chart_ref_file}"
printf '%s\n' "${uf_chart_ref}" > "${uf_chart_ref_file}"
printf '%s\n' "${enterprise_chart_ref}" > "${enterprise_chart_ref_file}"
printf '%s\n%s\n%s\n' "${operator_chart_ref}" "${uf_chart_ref}" "${enterprise_chart_ref}" > "${inventory_file}"

append_context "${context_file}" "chart_repo" "${chart_repo}"
append_context "${context_file}" "chart_version" "${chart_version}"
append_context "${context_file}" "chart_app_version" "${chart_app_version}"
append_context "${context_file}" "chart_channel" "${snapshot_channel}"
append_context "${context_file}" "stage_operator_image" "${stage_operator_image}"
append_context "${context_file}" "operator_chart_ref" "${operator_chart_ref}"
append_context "${context_file}" "uf_chart_ref" "${uf_chart_ref}"
append_context "${context_file}" "enterprise_chart_ref" "${enterprise_chart_ref}"

cat > "${summary_file}" <<EOF
Published stage snapshot Helm charts.

- chart_repo: ${chart_repo}
- helm_version: ${helm_version}
- chart_version: ${chart_version}
- chart_app_version: ${chart_app_version}
- stage_operator_image: ${stage_operator_image}
- operator_chart_ref: ${operator_chart_ref}
- uf_chart_ref: ${uf_chart_ref}
- enterprise_chart_ref: ${enterprise_chart_ref}
- inventory_file: ${inventory_file}
EOF
