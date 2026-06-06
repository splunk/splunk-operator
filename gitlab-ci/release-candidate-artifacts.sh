#!/bin/sh
set -eu

# Runtime contract
# - Purpose: prepare the promotable release-candidate set on the release branch.
# - Inputs: validated staged image artifacts, release version, internal
#   candidate registries, and OCI auth.
# - Outputs: candidate image refs, release manifests, packaged charts, and a
#   single release-candidate contract under ci-output/.
# - Guardrails: publish only release-candidate tags on internal registries and
#   promote the exact staged images already consumed by validation.

. "${CI_PROJECT_DIR}/gitlab-ci/lib/pipeline-common.sh"

context_file="ci-output/${WORKFLOW_SLUG}-runtime-context.txt"
output_dir="ci-output/${WORKFLOW_SLUG}-output"
candidate_dir="${output_dir}/release-candidate"
contract_file="${candidate_dir}/release-candidate-contract.env"
manifest_file="${candidate_dir}/artifact-manifest.txt"
summary_file="${candidate_dir}/summary.txt"
chart_inventory_file="${candidate_dir}/chart-inventory.txt"
build_image_ref_file="${BUILD_IMAGE_REF_FILE:-ci-output/build-test-push-workflow-ecr-image-ref.txt}"
build_image_digest_file="${BUILD_IMAGE_DIGEST_FILE:-ci-output/build-test-push-workflow-digest.txt}"
build_distroless_image_ref_file="${BUILD_DISTROLESS_IMAGE_REF_FILE:-ci-output/build-test-push-workflow-ecr-distroless-image-ref.txt}"
build_distroless_image_digest_file="${BUILD_DISTROLESS_IMAGE_DIGEST_FILE:-ci-output/build-test-push-workflow-distroless-digest.txt}"

mkdir -p "ci-output" "${output_dir}" "${candidate_dir}"
: > "${context_file}"

load_repo_dotenv "${CI_PROJECT_DIR}/.env"
require_file "${build_image_ref_file}" "validated staged operator image reference"
require_file "${build_image_digest_file}" "validated staged operator image digest"
require_file "${build_distroless_image_ref_file}" "validated staged distroless image reference"
require_file "${build_distroless_image_digest_file}" "validated staged distroless image digest"
resolve_release_version "${CI_PROJECT_DIR}/Makefile"
resolve_release_image_repository
resolve_enterprise_release_image
ensure_pipeline_aws_env
resolve_ecr_pipeline_image_repository "$(first_nonempty "${PIPELINE_ECR_REPOSITORY:-}" "")" "splunk/splunk-operator"

candidate_registry="${RESOLVED_ECR_REGISTRY}"
candidate_repository="${RESOLVED_IMAGE_REPOSITORY}"
candidate_repository_name="${RESOLVED_REPOSITORY_NAME}"
resolve_ecr_region "${candidate_registry}"
candidate_region="${RESOLVED_ECR_REGION}"
require_nonempty "${candidate_region}" "candidate ECR region"
export AWS_DEFAULT_REGION="${candidate_region}"

release_version="${RESOLVED_RELEASE_VERSION}"
release_candidate_number="${RESOLVED_RELEASE_CANDIDATE_NUMBER}"
candidate_tag="v${release_version}-rc.${release_candidate_number}"
candidate_image="${candidate_repository}:${candidate_tag}"
candidate_distroless_image="${candidate_repository}:${candidate_tag}-distroless"
release_image="${RESOLVED_RELEASE_IMAGE_REPOSITORY}:${RESOLVED_RELEASE_OPERATOR_TAG}"
release_distroless_image="${RESOLVED_RELEASE_IMAGE_REPOSITORY}:${RESOLVED_RELEASE_OPERATOR_TAG}-distroless"
enterprise_image="${RESOLVED_ENTERPRISE_IMAGE}"
release_dir_name="release-${release_version}"
release_dir="${CI_PROJECT_DIR}/${release_dir_name}"
release_archive_name="${release_dir_name}.tgz"
release_archive_path="${candidate_dir}/${release_archive_name}"
build_image_ref="$(cat "${build_image_ref_file}")"
build_image_digest="$(cat "${build_image_digest_file}")"
build_image_repository="${build_image_ref%:*}"
build_distroless_image_ref="$(cat "${build_distroless_image_ref_file}")"
build_distroless_image_digest="$(cat "${build_distroless_image_digest_file}")"
build_distroless_image_repository="${build_distroless_image_ref%:*}"
build_image_source="${build_image_ref}"
build_distroless_image_source="${build_distroless_image_ref}"
if [ -n "${build_image_digest}" ] && [ "${build_image_digest}" != "None" ] && [ "${build_image_digest}" != "null" ]; then
  build_image_source="${build_image_repository}@${build_image_digest}"
fi
if [ -n "${build_distroless_image_digest}" ] && [ "${build_distroless_image_digest}" != "None" ] && [ "${build_distroless_image_digest}" != "null" ]; then
  build_distroless_image_source="${build_distroless_image_repository}@${build_distroless_image_digest}"
fi

append_context "${context_file}" "release_source_ref" "${CI_COMMIT_REF_NAME}"
append_context "${context_file}" "release_version" "${release_version}"
append_context "${context_file}" "release_candidate_number" "${release_candidate_number}"
append_context "${context_file}" "candidate_registry" "${candidate_registry}"
append_context "${context_file}" "candidate_image" "${candidate_image}"
append_context "${context_file}" "candidate_distroless_image" "${candidate_distroless_image}"
append_context "${context_file}" "validated_stage_image" "${build_image_source}"
append_context "${context_file}" "validated_stage_distroless_image" "${build_distroless_image_source}"
append_context "${context_file}" "release_image" "${release_image}"
append_context "${context_file}" "release_distroless_image" "${release_distroless_image}"
append_context "${context_file}" "enterprise_image" "${enterprise_image}"

docker_login_registry "${candidate_registry}" "" ""
docker buildx imagetools create -t "${candidate_image}" "${build_image_source}"
docker buildx imagetools create -t "${candidate_distroless_image}" "${build_distroless_image_source}"

candidate_image_digest="$(aws ecr describe-images \
  --region "${candidate_region}" \
  --repository-name "${candidate_repository_name}" \
  --image-ids imageTag="${candidate_tag}" \
  --query 'imageDetails[0].imageDigest' \
  --output text)"
candidate_distroless_image_digest="$(aws ecr describe-images \
  --region "${candidate_region}" \
  --repository-name "${candidate_repository_name}" \
  --image-ids imageTag="${candidate_tag}-distroless" \
  --query 'imageDetails[0].imageDigest' \
  --output text)"

candidate_image_source="${candidate_image}"
candidate_distroless_image_source="${candidate_distroless_image}"
if [ -n "${candidate_image_digest}" ] && [ "${candidate_image_digest}" != "None" ] && [ "${candidate_image_digest}" != "null" ]; then
  candidate_image_source="${candidate_repository}@${candidate_image_digest}"
fi
if [ -n "${candidate_distroless_image_digest}" ] && [ "${candidate_distroless_image_digest}" != "None" ] && [ "${candidate_distroless_image_digest}" != "null" ]; then
  candidate_distroless_image_source="${candidate_repository}@${candidate_distroless_image_digest}"
fi

rm -rf "${release_dir}"

make generate-artifacts \
  IMG="${release_image}" \
  VERSION="${release_version}" \
  SPLUNK_ENTERPRISE_IMAGE="${enterprise_image}" \
  SPLUNK_GENERAL_TERMS="--accept-sgt-current-at-splunk-com" \
  WATCH_NAMESPACE=""

find "${release_dir}" -maxdepth 1 -type f | sort > "${manifest_file}"
cp -R "${release_dir}" "${candidate_dir}/"
tar -C "${CI_PROJECT_DIR}" -czf "${release_archive_path}" "${release_dir_name}"

ci_bin_dir="${CI_PROJECT_DIR}/bin"
ensure_ci_bin_path "${ci_bin_dir}"
make setup/helm \
  HELM_VERSION="$(first_nonempty "${PIPELINE_HELM_VERSION:-}" "${HELM_VERSION:-}" "v3.8.2")" \
  CI_BIN_DIR="${ci_bin_dir}"
make helm-package
operator_chart_source="$(find "${CI_PROJECT_DIR}/helm-chart/splunk-enterprise/charts" -maxdepth 1 -type f -name 'splunk-operator-*.tgz' | head -n 1)"
require_file "${operator_chart_source}" "packaged splunk-operator chart"
cp "${operator_chart_source}" "${candidate_dir}/"
uf_chart_source="$(find "${CI_PROJECT_DIR}/helm-chart/splunk-enterprise/charts" -maxdepth 1 -type f -name 'splunk-universalforwarder-*.tgz' | head -n 1)"
require_file "${uf_chart_source}" "packaged splunk-universalforwarder chart"
cp "${uf_chart_source}" "${candidate_dir}/"
helm package "${CI_PROJECT_DIR}/helm-chart/splunk-enterprise" --destination "${candidate_dir}"
operator_chart_archive="$(basename "${operator_chart_source}")"
uf_chart_archive="$(basename "${uf_chart_source}")"
enterprise_chart_path="$(find "${candidate_dir}" -maxdepth 1 -type f -name 'splunk-enterprise-*.tgz' | head -n 1)"
enterprise_chart_archive="$(basename "${enterprise_chart_path}")"
require_nonempty "${enterprise_chart_archive}" "packaged splunk-enterprise chart"
printf '%s\n%s\n%s\n' "${operator_chart_archive}" "${enterprise_chart_archive}" "${uf_chart_archive}" > "${chart_inventory_file}"

bundle_target="$(first_nonempty "${PIPELINE_BUNDLE_REGISTRY:-}" "")"
require_nonempty "${bundle_target}" "PIPELINE_BUNDLE_REGISTRY"
resolve_ecr_pipeline_image_repository "${bundle_target}" "splunk/splunk-operator"
bundle_registry="${RESOLVED_ECR_REGISTRY}"
image_tag_base="${RESOLVED_IMAGE_REPOSITORY}"
candidate_bundle_image="${image_tag_base}-bundle:v${release_version}-rc.${release_candidate_number}"
candidate_catalog_image="${image_tag_base}-catalog:v${release_version}-rc.${release_candidate_number}"
release_bundle_image="${image_tag_base}-bundle:v${release_version}"
release_catalog_image="${image_tag_base}-catalog:v${release_version}"
bundle_registry_username="$(first_nonempty "${PIPELINE_BUNDLE_REGISTRY_USERNAME:-}" "${PIPELINE_DOCKER_USERNAME:-}" "")"
bundle_registry_password="$(first_nonempty "${PIPELINE_BUNDLE_REGISTRY_PASSWORD:-}" "${PIPELINE_DOCKER_PASSWORD:-}" "")"

docker_login_registry "${bundle_registry}" "${bundle_registry_username}" "${bundle_registry_password}"
ensure_operator_sdk "${ci_bin_dir}" "$(first_nonempty "${OPERATOR_SDK_VERSION:-}" "")"

make bundle \
  IMAGE_TAG_BASE="${image_tag_base}" \
  VERSION="${release_version}" \
  IMG="${release_image}" \
  SPLUNK_ENTERPRISE_IMAGE="${enterprise_image}"

make bundle-build bundle-push catalog-build catalog-push \
  IMAGE_TAG_BASE="${image_tag_base}" \
  VERSION="${release_version}" \
  IMG="${release_image}" \
  BUNDLE_IMG="${candidate_bundle_image}" \
  BUNDLE_IMGS="${candidate_bundle_image}" \
  CATALOG_IMG="${candidate_catalog_image}"

cat > "${contract_file}" <<EOF
RELEASE_SOURCE_REF=${CI_COMMIT_REF_NAME}
RELEASE_VERSION=${release_version}
RELEASE_CANDIDATE_NUMBER=${release_candidate_number}
RELEASE_IMAGE=${release_image}
RELEASE_DISTROLESS_IMAGE=${release_distroless_image}
RELEASE_IMAGE_REGISTRY=${RESOLVED_RELEASE_IMAGE_REGISTRY}
RELEASE_IMAGE_REPOSITORY=${RESOLVED_RELEASE_IMAGE_REPOSITORY}
RELEASE_OPERATOR_TAG=${RESOLVED_RELEASE_OPERATOR_TAG}
RELEASE_ENTERPRISE_IMAGE=${enterprise_image}
RELEASE_CANDIDATE_IMAGE=${candidate_image}
RELEASE_CANDIDATE_IMAGE_DIGEST=${candidate_image_digest}
RELEASE_CANDIDATE_IMAGE_SOURCE=${candidate_image_source}
RELEASE_CANDIDATE_DISTROLESS_IMAGE=${candidate_distroless_image}
RELEASE_CANDIDATE_DISTROLESS_IMAGE_DIGEST=${candidate_distroless_image_digest}
RELEASE_CANDIDATE_DISTROLESS_IMAGE_SOURCE=${candidate_distroless_image_source}
RELEASE_BUNDLE_IMAGE=${release_bundle_image}
RELEASE_CATALOG_IMAGE=${release_catalog_image}
RELEASE_BUNDLE_CANDIDATE_IMAGE=${candidate_bundle_image}
RELEASE_CATALOG_CANDIDATE_IMAGE=${candidate_catalog_image}
RELEASE_ARTIFACT_DIRECTORY=${release_dir_name}
RELEASE_ARTIFACT_ARCHIVE=${release_archive_name}
RELEASE_OPERATOR_CHART_ARCHIVE=${operator_chart_archive}
RELEASE_ENTERPRISE_CHART_ARCHIVE=${enterprise_chart_archive}
RELEASE_UF_CHART_ARCHIVE=${uf_chart_archive}
EOF

cat > "${summary_file}" <<EOF
Prepared the release-candidate artifact set.

- release_source_ref: ${CI_COMMIT_REF_NAME}
- release_version: ${release_version}
- release_candidate_number: ${release_candidate_number}
- candidate_image: ${candidate_image_source}
- candidate_distroless_image: ${candidate_distroless_image_source}
- release_image: ${release_image}
- release_bundle_candidate_image: ${candidate_bundle_image}
- release_catalog_candidate_image: ${candidate_catalog_image}
- release_artifact_archive: ${release_archive_name}
- operator_chart_archive: ${operator_chart_archive}
- enterprise_chart_archive: ${enterprise_chart_archive}
- uf_chart_archive: ${uf_chart_archive}
- contract_file: ${contract_file}
EOF
