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
issue = event["issue"]
payload = {
    "number": issue["number"],
    "title": issue["title"],
    "body": issue.get("body") or "",
    "html_url": issue["html_url"],
    "author": issue["user"]["login"],
    "state": issue["state"],
}
print(json.dumps(payload))
PY
)"

ISSUE_NUMBER="$(python3 -c 'import json,sys; print(json.loads(sys.argv[1])["number"])' "${EVENT_JSON}")"
ISSUE_TITLE="$(python3 -c 'import json,sys; print(json.loads(sys.argv[1])["title"])' "${EVENT_JSON}")"
ISSUE_BODY="$(python3 -c 'import json,sys; print(json.loads(sys.argv[1])["body"])' "${EVENT_JSON}")"
ISSUE_URL="$(python3 -c 'import json,sys; print(json.loads(sys.argv[1])["html_url"])' "${EVENT_JSON}")"
ISSUE_AUTHOR="$(python3 -c 'import json,sys; print(json.loads(sys.argv[1])["author"])' "${EVENT_JSON}")"
ISSUE_STATE="$(python3 -c 'import json,sys; print(json.loads(sys.argv[1])["state"])' "${EVENT_JSON}")"

MARKER="github-intake:issue:${GITHUB_REPOSITORY}#${ISSUE_NUMBER}"
LABELS="${GITLAB_INTAKE_LABEL:-github-intake,github-intake::issue}"
EXISTING="$(gitlab_find_issue_by_marker "${MARKER}")"

if [[ -n "${EXISTING}" ]]; then
  GITLAB_ISSUE_URL="$(python3 -c 'import json,sys; print(json.loads(sys.argv[1])["web_url"])' "${EXISTING}")"
  echo "GitLab issue already exists: ${GITLAB_ISSUE_URL}"
else
  DESCRIPTION="$(cat <<EOF
<!-- ${MARKER} -->
# GitHub Issue Intake

- Source issue: ${ISSUE_URL}
- Source repository: ${GITHUB_REPOSITORY}
- Source author: ${ISSUE_AUTHOR}
- Source state: ${ISSUE_STATE}
- Intake marker: \`${MARKER}\`

## GitHub Body

\`\`\`
${ISSUE_BODY}
\`\`\`
EOF
)"

  CREATED="$(gitlab_create_issue "[GitHub Issue #${ISSUE_NUMBER}] ${ISSUE_TITLE}" "${DESCRIPTION}" "${LABELS}")"
  GITLAB_ISSUE_URL="$(python3 -c 'import json,sys; print(json.loads(sys.argv[1])["web_url"])' "${CREATED}")"
  echo "Created GitLab issue: ${GITLAB_ISSUE_URL}"
fi

maybe_post_backlink_comment \
  "${ISSUE_NUMBER}" \
  "${MARKER}" \
  "Tracked in GitLab: ${GITLAB_ISSUE_URL}\n\nMarker: \`${MARKER}\`"

cat <<EOF
issue_number=${ISSUE_NUMBER}
marker=${MARKER}
gitlab_issue_url=${GITLAB_ISSUE_URL}
backlink_mode=${GITHUB_BACKLINK_MODE:-none}
EOF
