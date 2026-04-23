#!/bin/sh

. "${CI_PROJECT_DIR}/gitlab-ci/lib/pipeline-common.sh"

catalog_submission_init() {
  context_file="ci-output/${WORKFLOW_SLUG}-runtime-context.txt"
  output_dir="ci-output/${WORKFLOW_SLUG}-output"
  submission_file="${output_dir}/submission-plan.md"
  summary_file="${output_dir}/summary.txt"

  mkdir -p "ci-output" "${output_dir}"
  : > "${context_file}"

  load_repo_dotenv "${CI_PROJECT_DIR}/.env"
  resolve_release_version "${CI_PROJECT_DIR}/Makefile"

  release_version="${RESOLVED_RELEASE_VERSION}"
  release_candidate_number="${RESOLVED_RELEASE_CANDIDATE_NUMBER}"

  export context_file output_dir submission_file summary_file
  export release_version release_candidate_number
}

write_catalog_common_summary() {
  target_kind="$1"
  target_repo="$2"
  package_name="$3"
  bundle_directory="$4"

  cat > "${summary_file}" <<EOF
Prepared the ${target_kind} submission plan.

- package_name: ${package_name}
- version: ${release_version}
- target_repo: ${target_repo}
- bundle_directory: ${bundle_directory}
- submission_file: ${submission_file}
EOF
}
