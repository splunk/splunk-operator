#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: scripts/dev/appframework_parity_check.sh [--base-ref <branch>] [--help]

Enforces AppFramework parity governance when AppFramework code/API paths change.
- Validates required governance artifacts and review-template references.
- If appframework-gated files changed, requires parity doc updates and test updates.

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
    echo "Unable to resolve origin/${ref} for appframework parity check." >&2
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

need_file() {
  local file="$1"
  if [[ ! -f "${file}" ]]; then
    echo "appframework_parity_check: FAIL: missing required artifact ${file}" >&2
    exit 1
  fi
}

need_pattern() {
  local file="$1"
  local pattern="$2"
  local label="$3"
  if command -v rg >/dev/null 2>&1; then
    if ! rg -q "${pattern}" "${file}"; then
      echo "appframework_parity_check: FAIL: ${label} missing in ${file}" >&2
      exit 1
    fi
  else
    if ! grep -Eq "${pattern}" "${file}"; then
      echo "appframework_parity_check: FAIL: ${label} missing in ${file}" >&2
      exit 1
    fi
  fi
}

need_file "docs/AppFramework.md"
need_file "docs/agent/APPFRAMEWORK_PARITY.md"
need_file ".github/pull_request_template.md"
need_file "templates/pull_request.md"

need_pattern "docs/agent/APPFRAMEWORK_PARITY.md" '^# App Framework Parity Governance' "title"
need_pattern "docs/agent/APPFRAMEWORK_PARITY.md" '^## Trigger Paths' "trigger paths section"
need_pattern "docs/agent/APPFRAMEWORK_PARITY.md" '^## Required Evidence' "required evidence section"
need_pattern "docs/agent/APPFRAMEWORK_PARITY.md" 'scripts/dev/appframework_parity_check.sh' "gate command reference"
need_pattern ".github/pull_request_template.md" 'scripts/dev/appframework_parity_check.sh' "PR template gate reference"
need_pattern "templates/pull_request.md" 'scripts/dev/appframework_parity_check.sh' "local PR template gate reference"

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
  echo "appframework_parity_check: no changed files detected."
  exit 0
fi

needs_parity=0
for file in "${changed_files[@]}"; do
  case "${file}" in
    docs/AppFramework.md|pkg/splunk/enterprise/afwscheduler.go|pkg/splunk/enterprise/afwscheduler_test.go|test/testenv/appframework_utils.go|test/appframework_*|config/samples/*appframework*)
      needs_parity=1
      ;;
  esac
done

if [[ "${needs_parity}" -eq 0 ]]; then
  echo "appframework_parity_check: passed (no appframework-gated paths changed)."
  exit 0
fi

if ! printf '%s\n' "${changed_files[@]}" | grep -Eq '^(docs/AppFramework\.md|docs/agent/APPFRAMEWORK_PARITY\.md)$'; then
  cat >&2 <<'ERROR'
appframework_parity_check: FAIL: appframework-gated changes detected without parity-doc update.

Required update in this diff:
- docs/AppFramework.md or
- docs/agent/APPFRAMEWORK_PARITY.md
ERROR
  exit 1
fi

if ! printf '%s\n' "${changed_files[@]}" | grep -Eq '^(test/appframework_.*|test/testenv/appframework_utils\.go|pkg/splunk/enterprise/afwscheduler_test\.go)$'; then
  cat >&2 <<'ERROR'
appframework_parity_check: FAIL: appframework-gated changes detected without appframework test updates.

Add at least one relevant test update:
- test/appframework_*/...
- test/testenv/appframework_utils.go
- pkg/splunk/enterprise/afwscheduler_test.go
ERROR
  exit 1
fi

echo "appframework_parity_check: passed."
