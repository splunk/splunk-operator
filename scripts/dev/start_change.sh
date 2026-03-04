#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: scripts/dev/start_change.sh <topic>

Creates docs/changes/<YYYY-MM-DD>-<topic>.md from docs/changes/TEMPLATE.md.
USAGE
}

if [[ $# -ne 1 ]]; then
  usage >&2
  exit 1
fi

topic="$1"
slug="$(printf '%s' "${topic}" | tr '[:upper:]' '[:lower:]' | sed -E 's/[^a-z0-9]+/-/g; s/^-+|-+$//g')"
if [[ -z "${slug}" ]]; then
  echo "Unable to derive topic slug from input: ${topic}" >&2
  exit 1
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(git -C "${script_dir}" rev-parse --show-toplevel 2>/dev/null || true)"
if [[ -z "${repo_root}" ]]; then
  echo "Unable to locate repo root. Run from inside the git repo." >&2
  exit 1
fi
cd "${repo_root}"

mkdir -p docs/changes

date_prefix="$(date +%F)"
out_file="docs/changes/${date_prefix}-${slug}.md"
template="docs/changes/TEMPLATE.md"

if [[ ! -f "${template}" ]]; then
  echo "Missing template: ${template}" >&2
  exit 1
fi

if [[ -e "${out_file}" ]]; then
  echo "File already exists: ${out_file}" >&2
  exit 1
fi

cp "${template}" "${out_file}"
{
  echo
  echo "<!-- topic: ${topic} -->"
} >> "${out_file}"

echo "Created ${out_file}"
