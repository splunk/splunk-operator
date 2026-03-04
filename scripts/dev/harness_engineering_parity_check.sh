#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: scripts/dev/harness_engineering_parity_check.sh [--help]

Validates docs/engineering/HARNESS_ENGINEERING_PARITY.md structure and matrix quality.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      usage
      exit 1
      ;;
  esac
done

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(git -C "${script_dir}" rev-parse --show-toplevel 2>/dev/null || true)"
if [[ -z "${repo_root}" ]]; then
  echo "Unable to locate repo root. Run from inside the git repo." >&2
  exit 1
fi
cd "${repo_root}"

doc="docs/engineering/HARNESS_ENGINEERING_PARITY.md"
if [[ ! -f "${doc}" ]]; then
  echo "harness_engineering_parity_check: FAIL: missing ${doc}" >&2
  exit 1
fi

must_contain() {
  local pattern="$1"
  local label="$2"
  if command -v rg >/dev/null 2>&1; then
    if ! rg -q "${pattern}" "${doc}"; then
      echo "harness_engineering_parity_check: FAIL: missing ${label} in ${doc}" >&2
      exit 1
    fi
  else
    if ! grep -Eq "${pattern}" "${doc}"; then
      echo "harness_engineering_parity_check: FAIL: missing ${label} in ${doc}" >&2
      exit 1
    fi
  fi
}

must_contain '^# Harness Engineering Parity' "document title"
must_contain '^## Product Profile' "product profile section"
must_contain '^## Backend Parity Matrix' "backend parity matrix section"
must_contain '^## No-UI Equivalents \(Required\)' "no-UI equivalents section"
must_contain 'N/A \(No UI\)' "no-UI status entry"
must_contain 'https://openai.com/index/harness-engineering/' "source link"

rows="$(awk '/^\| H[0-9]+ /{print}' "${doc}")"
row_count="$(printf '%s\n' "${rows}" | awk 'NF{c++} END{print c+0}')"
if [[ "${row_count}" -lt 10 ]]; then
  echo "harness_engineering_parity_check: FAIL: expected at least 10 parity rows, found ${row_count}" >&2
  exit 1
fi

bad=0
while IFS= read -r row; do
  [[ -n "${row}" ]] || continue
  status="$(printf '%s\n' "${row}" | awk -F'|' '{s=$5; gsub(/^[ \t]+|[ \t]+$/, "", s); print s}')"
  evidence="$(printf '%s\n' "${row}" | awk -F'|' '{e=$6; gsub(/^[ \t]+|[ \t]+$/, "", e); print e}')"

  case "${status}" in
    "Implemented"|"In Progress"|"Planned"|"N/A (No UI)")
      ;;
    *)
      echo "harness_engineering_parity_check: FAIL: invalid status '${status}' in row: ${row}" >&2
      bad=1
      ;;
  esac

  if [[ -z "${evidence}" ]]; then
    echo "harness_engineering_parity_check: FAIL: missing evidence column in row: ${row}" >&2
    bad=1
  fi
done < <(printf '%s\n' "${rows}")

if [[ "${bad}" -ne 0 ]]; then
  exit 1
fi

echo "harness_engineering_parity_check: passed."
