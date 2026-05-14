#!/usr/bin/env bash
#
# Fail if any file that would be mirrored to GitHub references
# docs/splunk_private/.  Directories listed in PRIVATE_DIRS are
# GitLab-only and are allowed to reference private docs.
set -euo pipefail

PRIVATE_DIRS=(
  "gitlab-ci/"
  "docs/splunk_private/"
)

exclude_args=()
for dir in "${PRIVATE_DIRS[@]}"; do
  exclude_args+=(":!:${dir}*")
done

hits=$(git --no-pager grep -l 'splunk_private/' -- '.' "${exclude_args[@]}" 2>&1) || {
  rc=$?
  if [ "$rc" -ne 1 ]; then
    printf 'ERROR: git grep failed (exit %d):\n%s\n' "$rc" "$hits" >&2
    exit 1
  fi
  hits=""
}

if [ -n "$hits" ]; then
  printf 'ERROR: The following public files reference docs/splunk_private/.\n'
  printf 'These references would be dangling after mirroring to GitHub.\n\n'
  printf '%s\n' "$hits"
  printf '\nMove the reference into a private directory or remove it.\n'
  exit 1
fi

printf 'OK: no dangling splunk_private references in public files.\n'
