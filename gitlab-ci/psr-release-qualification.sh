#!/bin/sh
set -eu

# Runtime contract
# - Purpose: capture the PSR release-qualification plan for the release branch/main candidate.
# - Inputs: release version, optional base version override, and enterprise image.
# - Outputs: PSR trigger matrix and operator-facing summary under ci-output/.
# - Guardrails: plan only; no downstream PSR dispatch in this lane yet because
#   bundle handoff into the PSR repo still stays manual outside this job.

. "${CI_PROJECT_DIR}/gitlab-ci/lib/pipeline-common.sh"

context_file="ci-output/${WORKFLOW_SLUG}-runtime-context.txt"
output_dir="ci-output/${WORKFLOW_SLUG}-output"
matrix_file="${output_dir}/psr-trigger-matrix.md"
summary_file="${output_dir}/summary.txt"

mkdir -p "ci-output" "${output_dir}"
: > "${context_file}"

load_repo_dotenv "${CI_PROJECT_DIR}/.env"
resolve_release_version "${CI_PROJECT_DIR}/Makefile"
resolve_enterprise_release_image

target_version="${RESOLVED_RELEASE_VERSION}"
base_version="$(first_nonempty "${PIPELINE_PSR_BASE_VERSION:-}" "")"
test_types="$(first_nonempty "${PIPELINE_PSR_TEST_TYPES:-}" "upgrade,app_framework,perf")"
clouds="$(first_nonempty "${PIPELINE_PSR_CLOUDS:-}" "aws,azure")"
psr_project="$(first_nonempty "${PIPELINE_PSR_PROJECT_PATH:-}" "psr/k8s-operator")"

append_context "${context_file}" "target_version" "${target_version}"
append_context "${context_file}" "base_version" "${base_version}"
append_context "${context_file}" "test_types" "${test_types}"
append_context "${context_file}" "clouds" "${clouds}"
append_context "${context_file}" "psr_project" "${psr_project}"
append_context "${context_file}" "enterprise_image" "${RESOLVED_ENTERPRISE_IMAGE}"

cat > "${matrix_file}" <<EOF
# PSR Release Qualification Plan

- project: ${psr_project}
- target_version: ${target_version}
- base_version: ${base_version:-unset}
- enterprise_image: ${RESOLVED_ENTERPRISE_IMAGE}
- test_types: ${test_types}
- clouds: ${clouds}

## Release policy

- PSR remains a release gate before GA publication.
- This lane records the exact trigger matrix only.
- \`PIPELINE_PSR_BASE_VERSION\` should be set before downstream upgrade PSR execution is enabled.
- Do not trigger \`TEST_TYPE=all\` from the SOK release lane.
EOF

cat > "${summary_file}" <<EOF
Prepared the PSR release-qualification plan.

- project: ${psr_project}
- target_version: ${target_version}
- base_version: ${base_version:-unset}
- matrix_file: ${matrix_file}
- downstream_dispatch: manual-only
EOF
