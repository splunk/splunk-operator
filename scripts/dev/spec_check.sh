#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: scripts/dev/spec_check.sh [--base-ref <branch>] [--help]

Checks KEP-lite quality for changed files.
- Validates changed docs/specs/*.md files for required status and sections.
- Non-trivial code changes are linked to KEPs via harness manifests (checked by
  scripts/dev/harness_manifest_check.sh).

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
    echo "Unable to resolve origin/${ref} for spec check." >&2
    exit 1
  fi

  local merge_base
  merge_base="$(git merge-base HEAD "${remote_ref}" || true)"
  if [[ -z "${merge_base}" ]]; then
    echo "Unable to compute merge-base against origin/${ref}." >&2
    exit 1
  fi

  git diff --name-only --diff-filter=ACMRT "${merge_base}...HEAD"
}

has_rg=false
if command -v rg >/dev/null 2>&1; then
  has_rg=true
fi

has_section() {
  local section="$1"
  local file="$2"
  if [[ "${has_rg}" == "true" ]]; then
    rg -Fq "${section}" "${file}"
  else
    grep -Fq "${section}" "${file}"
  fi
}

match_status() {
  local file="$1"
  local pattern='^(- )?Status:[[:space:]]*(Draft|In Review|Approved|Implemented|Superseded)[[:space:]]*$'
  if [[ "${has_rg}" == "true" ]]; then
    rg -q "${pattern}" "${file}"
  else
    grep -Eq "${pattern}" "${file}"
  fi
}

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
  echo "spec_check: no changed files detected."
  exit 0
fi

spec_files=()
for file in "${changed_files[@]}"; do
  case "${file}" in
    docs/specs/*.md)
      base_name="$(basename "${file}")"
      if [[ "${base_name}" != "README.md" && "${base_name}" != "SPEC_TEMPLATE.md" ]]; then
        spec_files+=("${file}")
      fi
      ;;
  esac
done

if [[ ${#spec_files[@]} -eq 0 ]]; then
  echo "spec_check: no changed KEP files detected."
  exit 0
fi

required_sections=(
  "## Summary"
  "## Motivation"
  "## Goals"
  "## Non-Goals"
  "## Proposal"
  "## API/CRD Impact"
  "## Reconcile/State Impact"
  "## Test Plan"
  "## Harness Validation"
  "## Risks"
  "## Rollout and Rollback"
  "## Graduation Criteria"
)

errors=()
for spec in "${spec_files[@]}"; do
  if [[ ! -f "${spec}" ]]; then
    errors+=("${spec}: file missing")
    continue
  fi

  if ! match_status "${spec}"; then
    errors+=("${spec}: missing or invalid Status field")
  fi

  for section in "${required_sections[@]}"; do
    if ! has_section "${section}" "${spec}"; then
      errors+=("${spec}: missing section '${section}'")
    fi
  done
done

if [[ ${#errors[@]} -gt 0 ]]; then
  printf 'spec_check failed:\n' >&2
  printf ' - %s\n' "${errors[@]}" >&2
  exit 1
fi

echo "spec_check: passed."
