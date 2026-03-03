#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: scripts/dev/doc_first_check.sh [--base-ref <branch>] [--help]

Enforces doc-first governance for implementation-gated changes.
- If code/manifests/tests/scripts changed, require a change-intent doc under docs/changes/.
- Validate required sections in changed docs/changes/*.md files.
- Require at least one harness/test artifact in the overall change set.

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
    echo "Unable to resolve origin/${ref} for doc-first check." >&2
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

is_doc_first_gated_path() {
  case "$1" in
    api/*|cmd/*|config/*|internal/*|kuttl/*|pkg/*|scripts/*|test/*|Makefile|go.mod|go.sum|PROJECT|skaffold.yaml|.github/workflows/*)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
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
  echo "doc_first_check: no changed files detected."
  exit 0
fi

requires_doc_first=false
for file in "${changed_files[@]}"; do
  if is_doc_first_gated_path "${file}"; then
    requires_doc_first=true
    break
  fi
done

if [[ "${requires_doc_first}" != "true" ]]; then
  echo "doc_first_check: no doc-first-gated paths changed."
  exit 0
fi

change_docs=()
for file in "${changed_files[@]}"; do
  case "${file}" in
    docs/changes/*.md)
      base_name="$(basename "${file}")"
      if [[ "${base_name}" != "README.md" && "${base_name}" != "TEMPLATE.md" ]]; then
        change_docs+=("${file}")
      fi
      ;;
  esac
done

if [[ ${#change_docs[@]} -eq 0 ]]; then
  cat >&2 <<'ERROR'
doc_first_check: FAIL: doc-first-gated paths changed without a change-intent document.

Add: docs/changes/<YYYY-MM-DD>-<topic>.md
Helper: scripts/dev/start_change.sh "<topic>"
ERROR
  exit 1
fi

required_sections=(
  "## Intent"
  "## Scope"
  "## Constitution Impact"
  "## Harness Coverage Plan"
  "## Test Plan"
  "## Implementation Log"
)

errors=()
for doc in "${change_docs[@]}"; do
  if [[ ! -f "${doc}" ]]; then
    errors+=("${doc}: file missing")
    continue
  fi

  for section in "${required_sections[@]}"; do
    if ! has_section "${section}" "${doc}"; then
      errors+=("${doc}: missing section '${section}'")
    fi
  done

  if [[ "${has_rg}" == "true" ]]; then
    if ! rg -q 'scripts/dev/|make |go test|ginkgo|kubectl kuttl|test/' "${doc}"; then
      errors+=("${doc}: Harness Coverage Plan/Test Plan appears empty (add command evidence)")
    fi
  else
    if ! grep -Eq 'scripts/dev/|make |go test|ginkgo|kubectl kuttl|test/' "${doc}"; then
      errors+=("${doc}: Harness Coverage Plan/Test Plan appears empty (add command evidence)")
    fi
  fi
done

if [[ "${has_rg}" == "true" ]]; then
  if ! printf '%s\n' "${changed_files[@]}" | rg -q '(^scripts/dev/.*(check|eval|run).*\.sh$|_test\.go$|^test/|^kuttl/|^harness/)'; then
    errors+=("overall diff: missing harness/test evidence file (scripts/dev/*check*.sh, *_test.go, test/, kuttl/, or harness/)")
  fi
else
  if ! printf '%s\n' "${changed_files[@]}" | grep -Eq '(^scripts/dev/.*(check|eval|run).*\.sh$|_test\.go$|^test/|^kuttl/|^harness/)'; then
    errors+=("overall diff: missing harness/test evidence file (scripts/dev/*check*.sh, *_test.go, test/, kuttl/, or harness/)")
  fi
fi

if [[ ${#errors[@]} -gt 0 ]]; then
  printf 'doc_first_check failed:\n' >&2
  printf ' - %s\n' "${errors[@]}" >&2
  exit 1
fi

echo "doc_first_check: passed."
