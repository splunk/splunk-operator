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
enterprise_chart_ref_file="ci-output/${WORKFLOW_SLUG}-enterprise-chart-ref.txt"
build_image_ref_file="${BUILD_IMAGE_REF_FILE:-ci-output/build-test-push-workflow-artifactory-image-ref.txt}"

mkdir -p "ci-output" "${output_dir}" "${package_dir}"
: > "${context_file}"

load_repo_dotenv "${CI_PROJECT_DIR}/.env"
resolve_release_version "${CI_PROJECT_DIR}/Makefile"
require_file "${build_image_ref_file}" "validated staged operator image reference"
stage_operator_image="$(cat "${build_image_ref_file}")"
require_nonempty "${stage_operator_image}" "staged operator image reference"

chart_repo="$(first_nonempty \
  "${PIPELINE_CHART_STAGE_REPOSITORY:-}" \
  "${JOB_CHART_STAGE_REPOSITORY:-}" \
  "${DEFAULT_CHART_STAGE_REPOSITORY:-}" \
  "")"
require_nonempty "${chart_repo}" "PIPELINE_CHART_STAGE_REPOSITORY"
case "${chart_repo}" in
  https://*/artifactory/*) ;;
  *)
    echo "PIPELINE_CHART_STAGE_REPOSITORY must be an Artifactory publish URL (https://.../artifactory/...)" >&2
    exit 1
    ;;
esac

chart_repo_url="$(first_nonempty \
  "${PIPELINE_CHART_STAGE_REPO_URL:-}" \
  "${JOB_CHART_STAGE_REPO_URL:-}" \
  "${DEFAULT_CHART_STAGE_REPO_URL:-}" \
  "$(artifactory_helm_repo_url_from_publish_base "${chart_repo}")" \
  "")"
require_nonempty "${chart_repo_url}" "PIPELINE_CHART_STAGE_REPO_URL"

chart_username="$(first_nonempty \
  "${PIPELINE_CHART_STAGE_USERNAME:-}" \
  "")"
chart_password="$(first_nonempty \
  "${PIPELINE_CHART_STAGE_PASSWORD:-}" \
  "")"
chart_artifactory_role="$(first_nonempty \
  "${PIPELINE_CHART_STAGE_ARTIFACTORY_ROLE:-}" \
  "${JOB_CHART_STAGE_ARTIFACTORY_ROLE:-}" \
  "")"

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
make setup/helm \
  HELM_VERSION="$(first_nonempty "${PIPELINE_HELM_VERSION:-}" "${HELM_VERSION:-}" "v3.8.2")" \
  CI_BIN_DIR="${ci_bin_dir}"

rm -rf "${workspace_dir}"
mkdir -p "${workspace_dir}"
cp -R "${CI_PROJECT_DIR}/helm-chart" "${workspace_dir}/"

operator_chart_dir="${workspace_dir}/helm-chart/splunk-operator"
enterprise_chart_dir="${workspace_dir}/helm-chart/splunk-enterprise"
enterprise_dependency_dir="${enterprise_chart_dir}/charts"
enterprise_chart_yaml="${enterprise_chart_dir}/Chart.yaml"
operator_values_yaml="${operator_chart_dir}/values.yaml"

mkdir -p "${enterprise_dependency_dir}"
rm -f "${enterprise_dependency_dir}/splunk-operator-"*.tgz

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

# Keep the parent chart dependency metadata aligned with the packaged operator
# snapshot so Helm does not reject the staged dependency set.
sed -i -E '/^- name: splunk-operator$/,/^(  repository:|  condition:)/ s/^  version: .*/  version: "'"${chart_version}"'"/' "${enterprise_chart_yaml}"

helm package "${enterprise_chart_dir}" \
  --version "${chart_version}" \
  --app-version "${chart_app_version}" \
  --destination "${package_dir}"
enterprise_chart_archive="${package_dir}/splunk-enterprise-${chart_version}.tgz"
require_file "${enterprise_chart_archive}" "packaged splunk-enterprise chart"

resolve_artifactory_publish_auth "${chart_username}" "${chart_password}" "${chart_artifactory_role}"
operator_chart_ref="${chart_repo%/}/$(basename "${operator_chart_archive}")"
enterprise_chart_ref="${chart_repo%/}/$(basename "${enterprise_chart_archive}")"
artifactory_upload_file "${operator_chart_ref}" "${operator_chart_archive}"
artifactory_upload_file "${enterprise_chart_ref}" "${enterprise_chart_archive}"

printf '%s\n' "${operator_chart_ref}" > "${operator_chart_ref_file}"
printf '%s\n' "${enterprise_chart_ref}" > "${enterprise_chart_ref_file}"
printf '%s\n%s\n' "${operator_chart_ref}" "${enterprise_chart_ref}" > "${inventory_file}"

append_context "${context_file}" "chart_repo" "${chart_repo}"
append_context "${context_file}" "chart_repo_url" "${chart_repo_url}"
append_context "${context_file}" "chart_version" "${chart_version}"
append_context "${context_file}" "chart_app_version" "${chart_app_version}"
append_context "${context_file}" "chart_channel" "${snapshot_channel}"
append_context "${context_file}" "stage_operator_image" "${stage_operator_image}"
append_context "${context_file}" "operator_chart_ref" "${operator_chart_ref}"
append_context "${context_file}" "enterprise_chart_ref" "${enterprise_chart_ref}"

cat > "${summary_file}" <<EOF
Published develop-lane snapshot Helm charts.

- chart_repo: ${chart_repo}
- chart_repo_url: ${chart_repo_url}
- chart_version: ${chart_version}
- chart_app_version: ${chart_app_version}
- stage_operator_image: ${stage_operator_image}
- operator_chart_ref: ${operator_chart_ref}
- enterprise_chart_ref: ${enterprise_chart_ref}
- inventory_file: ${inventory_file}
EOF
