#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
. "${ROOT_DIR}/github-intake/lib/intake-common.sh"

require_env GITHUB_EVENT_PATH
require_env GITHUB_REPOSITORY
require_env GITHUB_SERVER_URL
require_env GITHUB_API_URL
require_env GITLAB_BASE_URL
require_env GITLAB_PROJECT_PATH
require_env GITLAB_API_TOKEN

EVENT_JSON="$(python3 - "${GITHUB_EVENT_PATH}" <<'PY'
import json
import sys

event = json.load(open(sys.argv[1]))
pr = event["pull_request"]
payload = {
    "number": pr["number"],
    "title": pr["title"],
    "body": pr.get("body") or "",
    "html_url": pr["html_url"],
    "author": pr["user"]["login"],
    "state": pr["state"],
    "draft": pr["draft"],
    "head_ref": pr["head"]["ref"],
    "head_sha": pr["head"]["sha"],
    "head_repo": pr["head"]["repo"]["full_name"],
    "base_ref": pr["base"]["ref"],
    "base_sha": pr["base"]["sha"],
}
print(json.dumps(payload))
PY
)"

PR_NUMBER="$(python3 -c 'import json,sys; print(json.loads(sys.argv[1])["number"])' "${EVENT_JSON}")"
PR_TITLE="$(python3 -c 'import json,sys; print(json.loads(sys.argv[1])["title"])' "${EVENT_JSON}")"
PR_BODY="$(python3 -c 'import json,sys; print(json.loads(sys.argv[1])["body"])' "${EVENT_JSON}")"
PR_URL="$(python3 -c 'import json,sys; print(json.loads(sys.argv[1])["html_url"])' "${EVENT_JSON}")"
PR_AUTHOR="$(python3 -c 'import json,sys; print(json.loads(sys.argv[1])["author"])' "${EVENT_JSON}")"
PR_STATE="$(python3 -c 'import json,sys; print(json.loads(sys.argv[1])["state"])' "${EVENT_JSON}")"
PR_DRAFT="$(python3 -c 'import json,sys; print(json.loads(sys.argv[1])["draft"])' "${EVENT_JSON}")"
PR_HEAD_REF="$(python3 -c 'import json,sys; print(json.loads(sys.argv[1])["head_ref"])' "${EVENT_JSON}")"
PR_HEAD_SHA="$(python3 -c 'import json,sys; print(json.loads(sys.argv[1])["head_sha"])' "${EVENT_JSON}")"
PR_HEAD_REPO="$(python3 -c 'import json,sys; print(json.loads(sys.argv[1])["head_repo"])' "${EVENT_JSON}")"
PR_BASE_REF="$(python3 -c 'import json,sys; print(json.loads(sys.argv[1])["base_ref"])' "${EVENT_JSON}")"
PR_BASE_SHA="$(python3 -c 'import json,sys; print(json.loads(sys.argv[1])["base_sha"])' "${EVENT_JSON}")"

MARKER="github-intake:pr:${GITHUB_REPOSITORY}#${PR_NUMBER}"
LABELS="${GITLAB_INTAKE_LABEL:-github-intake,github-intake::pr}"
EXISTING="$(gitlab_find_issue_by_marker "${MARKER}")"

if [[ -n "${EXISTING}" ]]; then
  GITLAB_ISSUE_URL="$(python3 -c 'import json,sys; print(json.loads(sys.argv[1])["web_url"])' "${EXISTING}")"
  echo "GitLab intake record already exists: ${GITLAB_ISSUE_URL}"
else
  DESCRIPTION="$(cat <<EOF
<!-- ${MARKER} -->
# GitHub PR Intake

- Source PR: ${PR_URL}
- Source repository: ${GITHUB_REPOSITORY}
- Source author: ${PR_AUTHOR}
- Source state: ${PR_STATE}
- Draft: ${PR_DRAFT}
- Intake marker: \`${MARKER}\`
- Base branch: \`${PR_BASE_REF}\`
- Base SHA: \`${PR_BASE_SHA}\`
- Head branch: \`${PR_HEAD_REF}\`
- Head SHA: \`${PR_HEAD_SHA}\`
- Head repository: \`${PR_HEAD_REPO}\`

This intake record is metadata-only. It is not an authoritative GitLab merge request.

## GitHub Body

\`\`\`
${PR_BODY}
\`\`\`
EOF
)"

  CREATED="$(gitlab_create_issue "[GitHub PR #${PR_NUMBER}] ${PR_TITLE}" "${DESCRIPTION}" "${LABELS}")"
  GITLAB_ISSUE_URL="$(python3 -c 'import json,sys; print(json.loads(sys.argv[1])["web_url"])' "${CREATED}")"
  echo "Created GitLab intake record: ${GITLAB_ISSUE_URL}"
fi

maybe_post_backlink_comment \
  "${PR_NUMBER}" \
  "${MARKER}" \
  "Tracked in GitLab intake: ${GITLAB_ISSUE_URL}\n\nMarker: \`${MARKER}\`"

cat <<EOF
pr_number=${PR_NUMBER}
marker=${MARKER}
gitlab_issue_url=${GITLAB_ISSUE_URL}
backlink_mode=${GITHUB_BACKLINK_MODE:-none}
metadata_only=true
EOF
