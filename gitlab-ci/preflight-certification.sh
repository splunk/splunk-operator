#!/bin/sh
set -eu

# Runtime contract
# - Purpose: execute Red Hat preflight checks for the published operator images and bundle.
# - Inputs: release-image contract, bundle contract, Pyxis auth, and optional registry auth.
# - Outputs: preflight logs plus an operator-facing certification plan under ci-output/.
# - Guardrails: preflight only; partner-portal and external catalog mutation stay out of this job.

. "${CI_PROJECT_DIR}/gitlab-ci/lib/redhat-preflight-common.sh"

context_file="ci-output/${WORKFLOW_SLUG}-runtime-context.txt"
output_dir="ci-output/${WORKFLOW_SLUG}-output"
summary_file="${output_dir}/summary.txt"
commands_file="${output_dir}/preflight-commands.md"
results_dir="${output_dir}/results"
dockerconfig_file="${output_dir}/docker-config.json"
release_contract_file="${RELEASE_IMAGE_CONTRACT_FILE:-ci-output/publish-release-images-output/release-image-contract.env}"
bundle_contract_file="${BUNDLE_CONTRACT_FILE:-ci-output/publish-release-bundle-output/bundle-contract.env}"

mkdir -p "ci-output" "${output_dir}" "${results_dir}" "${CI_PROJECT_DIR}/bin"
: > "${context_file}"

ensure_jq
require_commands awk base64 curl docker jq
require_envs PIPELINE_PYXIS_API_TOKEN PIPELINE_PYXIS_CERTIFICATION_COMPONENT_ID
require_file "${release_contract_file}" "release image contract"
require_file "${bundle_contract_file}" "bundle contract"

load_optional_env_file "${release_contract_file}"
load_optional_env_file "${bundle_contract_file}"

container_image="${RELEASE_IMAGE:-}"
distroless_image="${RELEASE_DISTROLESS_IMAGE:-}"
bundle_image="${BUNDLE_IMG:-}"
release_version="${RELEASE_VERSION:-}"
component_id="${PIPELINE_PYXIS_CERTIFICATION_COMPONENT_ID}"
preflight_version="$(first_nonempty "${PIPELINE_PREFLIGHT_VERSION:-}" "1.16.0")"
container_log="${results_dir}/container-preflight.log"
distroless_log="${results_dir}/distroless-preflight.log"
bundle_log="${results_dir}/bundle-preflight.log"

require_nonempty "${container_image}" "published release container image"
require_nonempty "${distroless_image}" "published distroless release image"
require_nonempty "${bundle_image}" "published release bundle image"
require_nonempty "${release_version}" "release version"

install_preflight_release_binary "${preflight_version}" "${CI_PROJECT_DIR}/bin"
prepare_preflight_dockerconfig "${dockerconfig_file}" "${container_image}" "${distroless_image}" "${bundle_image}" || true

run_container_preflight() {
  image_ref="$1"
  log_file="$2"

  set -- preflight check container "${image_ref}" \
    --submit \
    --pyxis-api-token "${PIPELINE_PYXIS_API_TOKEN}" \
    --certification-component-id "${component_id}"
  if [ -f "${dockerconfig_file}" ]; then
    set -- "$@" --docker-config "${dockerconfig_file}"
  fi
  "$@" > "${log_file}" 2>&1
}

run_bundle_preflight() {
  set -- preflight check operator "${bundle_image}"
  if [ -f "${dockerconfig_file}" ]; then
    set -- "$@" --docker-config "${dockerconfig_file}"
  fi
  "$@" > "${bundle_log}" 2>&1
}

run_container_preflight "${container_image}" "${container_log}"
run_container_preflight "${distroless_image}" "${distroless_log}"
run_bundle_preflight

append_context "${context_file}" "release_version" "${release_version}"
append_context "${context_file}" "container_image" "${container_image}"
append_context "${context_file}" "distroless_image" "${distroless_image}"
append_context "${context_file}" "bundle_image" "${bundle_image}"
append_context "${context_file}" "preflight_version" "${preflight_version}"
append_context "${context_file}" "pyxis_component_id" "${component_id}"

cat > "${commands_file}" <<EOF
# Red Hat Preflight Certification Plan

- release_version: ${release_version}
- bundle_image: ${bundle_image}
- container_image: ${container_image}
- distroless_image: ${distroless_image}
- pyxis_component_id: ${component_id}

## Executed checks

\`\`\`bash
preflight check container ${container_image} --certification-component-id ${component_id} --submit
preflight check container ${distroless_image} --certification-component-id ${component_id} --submit
preflight check operator ${bundle_image}
\`\`\`

## Release policy

- Container and bundle preflight are required before Red Hat marketplace/certified-operator submission.
- External partner-portal approval remains outside GitLab, but the evidence bundle is produced here.
EOF

cat > "${summary_file}" <<EOF
Executed Red Hat preflight certification.

- release_version: ${release_version}
- container_image: ${container_image}
- distroless_image: ${distroless_image}
- bundle_image: ${bundle_image}
- commands_file: ${commands_file}
EOF
