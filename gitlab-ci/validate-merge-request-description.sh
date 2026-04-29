#!/bin/sh
set -eu

fetch_merge_request_description() {
  python3 - <<'PY'
import json
import os
import sys
from urllib import request

api_root = os.environ.get("CI_API_V4_URL", "")
project_id = os.environ.get("CI_PROJECT_ID", "")
mr_iid = os.environ.get("CI_MERGE_REQUEST_IID", "")
job_token = os.environ.get("CI_JOB_TOKEN", "")

if not all((api_root, project_id, mr_iid, job_token)):
    missing = [
        name
        for name, value in (
            ("CI_API_V4_URL", api_root),
            ("CI_PROJECT_ID", project_id),
            ("CI_MERGE_REQUEST_IID", mr_iid),
            ("CI_JOB_TOKEN", job_token),
        )
        if not value
    ]
    print(
        "Unable to fetch full merge request description; missing: "
        + ", ".join(missing),
        file=sys.stderr,
    )
    sys.exit(1)

url = f"{api_root}/projects/{project_id}/merge_requests/{mr_iid}"
req = request.Request(url, headers={"JOB-TOKEN": job_token})

try:
    with request.urlopen(req) as response:
        payload = json.load(response)
except Exception as exc:
    print(f"Unable to fetch full merge request description: {exc}", file=sys.stderr)
    sys.exit(1)

description = payload.get("description", "")
if not isinstance(description, str):
    print("Merge request description payload is not a string", file=sys.stderr)
    sys.exit(1)

sys.stdout.write(description)
PY
}

pipeline_source="${CI_PIPELINE_SOURCE:-}"
if [ "${pipeline_source}" != "merge_request_event" ]; then
  echo "merge-request-description-check only applies to merge request pipelines"
  exit 0
fi

description="${CI_MERGE_REQUEST_DESCRIPTION:-}"
if [ "${CI_MERGE_REQUEST_DESCRIPTION_IS_TRUNCATED:-false}" = "true" ]; then
  description="$(fetch_merge_request_description)"
fi
source_branch="${CI_MERGE_REQUEST_SOURCE_BRANCH_NAME:-}"
target_branch="${CI_MERGE_REQUEST_TARGET_BRANCH_NAME:-}"

case "${source_branch}" in
  renovate/*)
    echo "Skipping merge request description check for Renovate bot MR."
    exit 0
    ;;
esac

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
