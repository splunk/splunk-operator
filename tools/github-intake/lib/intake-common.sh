#!/usr/bin/env bash
set -euo pipefail

require_env() {
  local name="$1"
  if [[ -z "${!name:-}" ]]; then
    echo "Missing required environment variable: ${name}" >&2
    exit 1
  fi
}

urlencode() {
  python3 - "$1" <<'PY'
import sys
import urllib.parse

print(urllib.parse.quote(sys.argv[1], safe=""))
PY
}

gitlab_project_api() {
  local encoded
  encoded="$(urlencode "${GITLAB_PROJECT_PATH}")"
  printf '%s/api/v4/projects/%s' "${GITLAB_BASE_URL%/}" "${encoded}"
}

gitlab_auth_header() {
  printf 'PRIVATE-TOKEN: %s' "${GITLAB_API_TOKEN}"
}

gitlab_find_issue_by_marker() {
  local marker="$1"
  local search_url

  search_url="$(gitlab_project_api)/issues?search=$(urlencode "${marker}")&per_page=100"
  curl -fsSL \
    --header "$(gitlab_auth_header)" \
    "${search_url}" \
    | python3 - "${marker}" <<'PY'
import json
import sys

marker = sys.argv[1]
issues = json.load(sys.stdin)
for issue in issues:
    description = issue.get("description") or ""
    if marker in description:
        print(json.dumps({
            "iid": issue["iid"],
            "web_url": issue["web_url"],
            "title": issue["title"],
        }))
        raise SystemExit(0)
print("")
PY
}

gitlab_create_issue() {
  local title="$1"
  local description="$2"
  local labels="$3"

  curl -fsSL \
    --request POST \
    --header "$(gitlab_auth_header)" \
    --data-urlencode "title=${title}" \
    --data-urlencode "description=${description}" \
    --data-urlencode "labels=${labels}" \
    "$(gitlab_project_api)/issues"
}

github_comments_api() {
  local issue_number="$1"
  printf '%s/repos/%s/issues/%s/comments' "${GITHUB_API_URL%/}" "${GITHUB_REPOSITORY}" "${issue_number}"
}

github_comment_exists() {
  local issue_number="$1"
  local marker="$2"

  curl -fsSL \
    --header "Authorization: Bearer ${GITHUB_TOKEN}" \
    --header "Accept: application/vnd.github+json" \
    "$(github_comments_api "${issue_number}")" \
    | python3 - "${marker}" <<'PY'
import json
import sys

marker = sys.argv[1]
comments = json.load(sys.stdin)
for comment in comments:
    if marker in (comment.get("body") or ""):
        raise SystemExit(0)
raise SystemExit(1)
PY
}

github_post_comment() {
  local issue_number="$1"
  local body="$2"

  python3 - "${body}" <<'PY' >/tmp/github-intake-comment.json
import json
import sys

print(json.dumps({"body": sys.argv[1]}))
PY

  curl -fsSL \
    --request POST \
    --header "Authorization: Bearer ${GITHUB_TOKEN}" \
    --header "Accept: application/vnd.github+json" \
    --header "Content-Type: application/json" \
    --data @/tmp/github-intake-comment.json \
    "$(github_comments_api "${issue_number}")" >/dev/null
}

maybe_post_backlink_comment() {
  local issue_number="$1"
  local marker="$2"
  local body="$3"
  local mode="${GITHUB_BACKLINK_MODE:-none}"

  if [[ "${mode}" != "comment" ]]; then
    echo "GitHub backlink comment skipped: mode=${mode}"
    return 0
  fi

  if github_comment_exists "${issue_number}" "${marker}"; then
    echo "GitHub backlink comment already present for ${marker}"
    return 0
  fi

  github_post_comment "${issue_number}" "${body}"
  echo "GitHub backlink comment posted for ${marker}"
}
