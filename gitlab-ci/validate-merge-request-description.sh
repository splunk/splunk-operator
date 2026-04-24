#!/bin/sh
set -eu

pipeline_source="${CI_PIPELINE_SOURCE:-}"
if [ "${pipeline_source}" != "merge_request_event" ]; then
  echo "merge-request-description-check only applies to merge request pipelines"
  exit 0
fi

description="${CI_MERGE_REQUEST_DESCRIPTION:-}"
source_branch="${CI_MERGE_REQUEST_SOURCE_BRANCH_NAME:-}"
target_branch="${CI_MERGE_REQUEST_TARGET_BRANCH_NAME:-}"

if [ -z "${description}" ]; then
  echo "Merge request description is empty." >&2
  echo "Use .gitlab/merge_request_templates/Default.md or .gitlab/merge_request_templates/Release.md." >&2
  exit 1
fi

template_path=".gitlab/merge_request_templates/Default.md"
set -- \
  "## Summary" \
  "## Pipeline Impact" \
  "## Testing" \
  "## Jira" \
  "## Checklist"

case "${target_branch}:${source_branch}" in
  main:release/*|main:release-*)
    template_path=".gitlab/merge_request_templates/Release.md"
    set -- \
      "## Summary" \
      "## Release Context" \
      "## Validation Evidence" \
      "## Publish Plan" \
      "## Jira" \
      "## Checklist"
    ;;
esac

missing_headings=""
for heading in "$@"; do
  if ! printf '%s\n' "${description}" | grep -Fq "${heading}"; then
    missing_headings="${missing_headings}\n- ${heading}"
  fi
done

if [ -n "${missing_headings}" ]; then
  echo "Merge request description is missing required headings for ${template_path}:" >&2
  printf '%b\n' "${missing_headings}" >&2
  echo "Use the matching GitLab merge request template and update the description." >&2
  exit 1
fi

echo "Merge request description contains the required headings for ${template_path}."

