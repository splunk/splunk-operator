#!/bin/sh
set -eu

# Runtime contract
# - Purpose: prepare the community-operators / OperatorHub submission payload for the release.
# - Inputs: release version, community-operators metadata, and preflight evidence.
# - Outputs: PR-ready plan under ci-output/.
# - Guardrails: payload preparation only; no external PR creation.

. "${CI_PROJECT_DIR}/gitlab-ci/lib/operator-catalog-submission-common.sh"

catalog_submission_init

package_name="${PIPELINE_COMMUNITY_OPERATOR_PACKAGE_NAME:-splunk}"
target_repo="${PIPELINE_COMMUNITY_OPERATOR_REPO:-k8s-operatorhub/community-operators}"
bundle_directory="${PIPELINE_COMMUNITY_OPERATOR_BUNDLE_DIR:-operators/${package_name}/${release_version}}"
ci_yaml_path="${PIPELINE_COMMUNITY_OPERATOR_CI_YAML_PATH:-operators/${package_name}/ci.yaml}"
reviewers="${PIPELINE_COMMUNITY_OPERATOR_REVIEWERS:-vivekr-splunk,rlieberman-splunk,kasiakoziol,patrykw-splunk,Igor-splunk}"
update_graph="${PIPELINE_COMMUNITY_OPERATOR_UPDATE_GRAPH:-replaces-mode}"

append_context "${context_file}" "package_name" "${package_name}"
append_context "${context_file}" "target_repo" "${target_repo}"
append_context "${context_file}" "bundle_directory" "${bundle_directory}"

cat > "${submission_file}" <<EOF
# Community Operators Submission Plan

- target_repo: ${target_repo}
- package_name: ${package_name}
- release_version: ${release_version}
- release_candidate_number: ${release_candidate_number}
- bundle_directory: ${bundle_directory}
- ci_yaml_path: ${ci_yaml_path}
- reviewers: ${reviewers}
- update_graph: ${update_graph}

## Required evidence

- Red Hat preflight certification from \`preflight-certification\`
- published bundle/catalog refs from \`publish-release-bundle\`

## Execution guardrails

- Keep reviewer and update-graph metadata aligned with the upstream contract unless maintainers approve a change.
- This job prepares the PR payload only; it does not open or update the external PR.
EOF

write_catalog_common_summary "community-operators" "${target_repo}" "${package_name}" "${bundle_directory}"
