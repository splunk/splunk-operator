#!/bin/sh
set -eu

# Runtime contract
# - Purpose: generate release artifacts from the staged candidate image during release validation.
# - Inputs: build-stage-image artifact, repo VERSION, and release Enterprise image pin.
# - Outputs: rendered release manifests and a short artifact inventory under ci-output/.
# - Guardrails: no public mutation and no tag promotion; this is validation evidence only.

. "${CI_PROJECT_DIR}/gitlab-ci/lib/pipeline-common.sh"

context_file="ci-output/${WORKFLOW_SLUG}-runtime-context.txt"
output_dir="ci-output/${WORKFLOW_SLUG}-output"
manifest_file="${output_dir}/artifact-manifest.txt"
image_ref_file="${BUILD_IMAGE_REF_FILE:-ci-output/build-test-push-workflow-image-ref.txt}"

mkdir -p "ci-output" "${output_dir}"
: > "${context_file}"

require_file "${image_ref_file}" "release candidate image reference"
load_repo_dotenv "${CI_PROJECT_DIR}/.env"
resolve_release_version "${CI_PROJECT_DIR}/Makefile"
resolve_enterprise_release_image

candidate_image_ref="$(cat "${image_ref_file}")"
release_version="${RESOLVED_RELEASE_VERSION}"
enterprise_image="${RESOLVED_ENTERPRISE_IMAGE}"
release_dir="${CI_PROJECT_DIR}/release-${release_version}"

append_context "${context_file}" "candidate_image_ref" "${candidate_image_ref}"
append_context "${context_file}" "release_version" "${release_version}"
append_context "${context_file}" "enterprise_image" "${enterprise_image}"

make generate-artifacts \
  IMG="${candidate_image_ref}" \
  VERSION="${release_version}" \
  SPLUNK_ENTERPRISE_IMAGE="${enterprise_image}" \
  SPLUNK_GENERAL_TERMS="--accept-sgt-current-at-splunk-com" \
  WATCH_NAMESPACE=""

find "${release_dir}" -maxdepth 1 -type f | sort > "${manifest_file}"
cp -R "${release_dir}" "${output_dir}/"
