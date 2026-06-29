#!/usr/bin/env bash
#
# Fail if any file that would be mirrored to GitHub references
# docs/splunk_private/.  Directories listed in gitlab-only-paths.conf
# are GitLab-only (stripped before the mirror push) and are therefore
# allowed to reference private docs.
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
config_file="${script_dir}/gitlab-only-paths.conf"

if [ ! -f "$config_file" ]; then
  printf 'ERROR: missing config file: %s\n' "$config_file" >&2
  exit 1
fi

PRIVATE_DIRS=()
while IFS= read -r line || [ -n "$line" ]; do
  line="${line%%#*}"                 # strip inline comments
  line="${line#"${line%%[![:space:]]*}"}"  # strip leading whitespace
  line="${line%"${line##*[![:space:]]}"}"  # strip trailing whitespace
  [ -z "$line" ] && continue
  PRIVATE_DIRS+=("$line")
done < "$config_file"

if [ "${#PRIVATE_DIRS[@]}" -eq 0 ]; then
  printf 'ERROR: no paths parsed from %s\n' "$config_file" >&2
  exit 1
fi

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
