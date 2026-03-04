#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: scripts/dev/constitution_runtime_policy_check.sh [--base-ref <branch>] [--help]

Validates always-on engineering constitution and runtime issue tracker policy.
- Checks required constitution/runtime policy docs and mandatory sections.
- Validates runtime issue tracker status values.
- For implementation-gated changes, requires either runtime tracker update or an
  explicit review note in docs/changes/*.md.

Options:
  --base-ref <branch>  Compare HEAD against origin/<branch> (CI mode).
  -h, --help           Show this help.
USAGE
}

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(git -C "${script_dir}" rev-parse --show-toplevel 2>/dev/null || true)"
if [[ -z "${repo_root}" ]]; then
  echo "Unable to locate repo root. Run from inside the git repo." >&2
  exit 1
fi
cd "${repo_root}"

base_ref=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --base-ref)
      if [[ $# -lt 2 ]]; then
        echo "--base-ref requires a value." >&2
        exit 1
      fi
      base_ref="$2"
      shift 2
      ;;
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

collect_changed_files_local() {
  local files=()
  while IFS= read -r line; do
    [[ -n "${line}" ]] && files+=("${line}")
  done < <(git diff --name-only --diff-filter=ACMRT)

  while IFS= read -r line; do
    [[ -n "${line}" ]] && files+=("${line}")
  done < <(git diff --name-only --cached --diff-filter=ACMRT)

  while IFS= read -r line; do
    [[ -n "${line}" ]] && files+=("${line}")
  done < <(git ls-files --others --exclude-standard)

  printf '%s\n' "${files[@]}"
}

collect_changed_files_ci() {
  local ref="$1"
  local remote_ref="refs/remotes/origin/${ref}"

  if ! git show-ref --verify --quiet "${remote_ref}"; then
    git fetch --no-tags --depth=200 origin "${ref}:${remote_ref}" >/dev/null 2>&1 || true
  fi

  if ! git show-ref --verify --quiet "${remote_ref}"; then
    echo "Unable to resolve origin/${ref} for constitution/runtime policy check." >&2
    exit 1
  fi

  local merge_base
  merge_base="$(git merge-base HEAD "${remote_ref}" || true)"
  if [[ -z "${merge_base}" ]]; then
    echo "Unable to compute merge-base against origin/${ref}." >&2
    exit 1
  fi

  {
    git diff --name-only --diff-filter=ACMRT "${merge_base}...HEAD"
    git diff --name-only --diff-filter=ACMRT
    git diff --name-only --cached --diff-filter=ACMRT
    git ls-files --others --exclude-standard
  } | awk 'NF' | sort -u
}

has_pattern() {
  local pattern="$1"
  local file="$2"
  if command -v rg >/dev/null 2>&1; then
    rg -q "${pattern}" "${file}"
  else
    grep -Eq "${pattern}" "${file}"
  fi
}

constitution_doc="docs/engineering/CONSTITUTION.md"
runtime_doc="docs/testing/RUNTIME_ISSUE_TRACKER.md"

for f in "${constitution_doc}" "${runtime_doc}"; do
  if [[ ! -f "${f}" ]]; then
    echo "constitution_runtime_policy_check: FAIL: missing required file ${f}" >&2
    exit 1
  fi
done

required_constitution_sections=(
  '^# Engineering Constitution'
  '^## Product Goal'
  '^## Security Baselines'
  '^## Reconcile and State Management'
  '^## CRD and Compatibility Rules'
  '^## Test and Harness Requirements'
  '^## Runtime Issue Governance'
)
for section in "${required_constitution_sections[@]}"; do
  if ! has_pattern "${section}" "${constitution_doc}"; then
    echo "constitution_runtime_policy_check: FAIL: missing section ${section} in ${constitution_doc}" >&2
    exit 1
  fi
done

required_runtime_sections=(
  '^# Runtime Issue Tracker'
  '^## How To Use'
  'Open` -> `In Progress` -> `Mitigated` -> `Closed'
)
for section in "${required_runtime_sections[@]}"; do
  if ! has_pattern "${section}" "${runtime_doc}"; then
    echo "constitution_runtime_policy_check: FAIL: missing section/policy '${section}' in ${runtime_doc}" >&2
    exit 1
  fi
done

# Validate issue status values in markdown tables.
status_errors=0
while IFS= read -r line; do
  [[ -n "${line}" ]] || continue
  [[ "${line}" =~ ^\| ]] || continue
  [[ "${line}" =~ ^\|[[:space:]]*ID[[:space:]]*\| ]] && continue
  [[ "${line}" =~ ^\|[-[:space:]]+\| ]] && continue

  status="$(printf '%s\n' "${line}" | awk -F'|' '{s=$3; gsub(/^[ \t`]+|[ \t`]+$/, "", s); print s}')"
  if [[ -z "${status}" ]]; then
    continue
  fi

  case "${status}" in
    Open|In\ Progress|Mitigated|Closed)
      ;;
    *)
      echo "constitution_runtime_policy_check: FAIL: invalid runtime issue status '${status}' in line:" >&2
      echo "${line}" >&2
      status_errors=1
      ;;
  esac
done < "${runtime_doc}"

if [[ "${status_errors}" -ne 0 ]]; then
  exit 1
fi

declare -A seen=()
changed_files=()
if [[ -n "${base_ref}" ]]; then
  while IFS= read -r f; do
    [[ -n "${f}" ]] || continue
    if [[ -z "${seen[$f]+x}" ]]; then
      seen["$f"]=1
      changed_files+=("$f")
    fi
  done < <(collect_changed_files_ci "${base_ref}")
else
  while IFS= read -r f; do
    [[ -n "${f}" ]] || continue
    if [[ -z "${seen[$f]+x}" ]]; then
      seen["$f"]=1
      changed_files+=("$f")
    fi
  done < <(collect_changed_files_local)
fi

if [[ ${#changed_files[@]} -eq 0 ]]; then
  echo "constitution_runtime_policy_check: passed."
  exit 0
fi

requires_runtime_review=false
runtime_tracker_touched=false
review_noted=false

for file in "${changed_files[@]}"; do
  case "${file}" in
    api/*|cmd/*|config/*|internal/*|kuttl/*|pkg/*|scripts/*|test/*|Makefile|go.mod|go.sum|PROJECT|skaffold.yaml|.github/workflows/*)
      requires_runtime_review=true
      ;;
  esac

  if [[ "${file}" == "docs/testing/RUNTIME_ISSUE_TRACKER.md" ]]; then
    runtime_tracker_touched=true
  fi

  case "${file}" in
    docs/changes/*.md)
      base_name="$(basename "${file}")"
      if [[ "${base_name}" != "README.md" && "${base_name}" != "TEMPLATE.md" && -f "${file}" ]]; then
        if has_pattern '^## Runtime Issue Tracker Review' "${file}"; then
          review_noted=true
        fi
      fi
      ;;
  esac
done

if [[ "${requires_runtime_review}" == "true" && "${runtime_tracker_touched}" != "true" && "${review_noted}" != "true" ]]; then
  cat >&2 <<'ERROR'
constitution_runtime_policy_check: FAIL: implementation-gated changes require runtime issue governance evidence.

Provide one of:
- update docs/testing/RUNTIME_ISSUE_TRACKER.md, or
- add `## Runtime Issue Tracker Review` section in docs/changes/<date>-<topic>.md.
ERROR
  exit 1
fi

echo "constitution_runtime_policy_check: passed."
