#!/bin/sh
set -eu

# Runtime contract
# - Purpose: prepare the Red Hat certified-operators submission payload for the release.
# - Inputs: release version, certified-operators metadata, and preflight evidence.
# - Outputs: PR-ready plan under ci-output/.
# - Guardrails: payload preparation only; no external PR creation.

. "${CI_PROJECT_DIR}/gitlab-ci/lib/operator-catalog-submission-common.sh"

catalog_submission_init

package_name="${PIPELINE_CERTIFIED_OPERATOR_PACKAGE_NAME:-splunk-operator}"
target_repo="${PIPELINE_CERTIFIED_OPERATOR_REPO:-redhat-openshift-ecosystem/certified-operators}"
bundle_directory="${PIPELINE_CERTIFIED_OPERATOR_BUNDLE_DIR:-operators/${package_name}/${release_version}}"
ci_yaml_path="${PIPELINE_CERTIFIED_OPERATOR_CI_YAML_PATH:-operators/${package_name}/ci.yaml}"
openshift_versions="${PIPELINE_CERTIFIED_OPERATOR_OPENSHIFT_VERSIONS:-v4.11-v4.20}"
default_channel="${PIPELINE_CERTIFIED_OPERATOR_DEFAULT_CHANNEL:-stable}"

append_context "${context_file}" "package_name" "${package_name}"
append_context "${context_file}" "target_repo" "${target_repo}"
append_context "${context_file}" "bundle_directory" "${bundle_directory}"

cat > "${submission_file}" <<EOF
# Certified Operators Submission Plan

- target_repo: ${target_repo}
- package_name: ${package_name}
- release_version: ${release_version}
- release_candidate_number: ${release_candidate_number}
- bundle_directory: ${bundle_directory}
- ci_yaml_path: ${ci_yaml_path}
- default_channel: ${default_channel}
- openshift_versions: ${openshift_versions}

## Required evidence

- Red Hat preflight certification from \`preflight-certification\`
- published bundle/catalog refs from \`publish-release-bundle\`

## Execution guardrails

- Keep the upstream \`${ci_yaml_path}\` contract aligned unless a reviewed change is approved.
- This job prepares the PR payload only; it does not open or update the external PR.
EOF

write_catalog_common_summary "certified-operators" "${target_repo}" "${package_name}" "${bundle_directory}"
