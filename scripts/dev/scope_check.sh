#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: scripts/dev/scope_check.sh [--base-ref <branch>] [--allowed <patterns>] [--forbidden <patterns>] [--exclude <patterns>] [--help]

Checks changed files against allowed/forbidden glob patterns.
Patterns accept comma or whitespace separators (for example: "api/**,internal/**").

Options:
  --base-ref <branch>  Compare HEAD against origin/<branch> (CI mode).
  --allowed <patterns> Allowed path globs. Falls back to ALLOWED_PATHS env var.
  --forbidden <patterns> Forbidden path globs. Falls back to FORBIDDEN_PATHS env var.
  --exclude <patterns> Excluded path globs. Falls back to EXCLUDE_PATHS env var.
  -h, --help          Show this help.
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
allowed_raw="${ALLOWED_PATHS:-}"
forbidden_raw="${FORBIDDEN_PATHS:-}"
exclude_raw="${EXCLUDE_PATHS:-}"

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
    --allowed)
      if [[ $# -lt 2 ]]; then
        echo "--allowed requires a value." >&2
        exit 1
      fi
      allowed_raw="$2"
      shift 2
      ;;
    --forbidden)
      if [[ $# -lt 2 ]]; then
        echo "--forbidden requires a value." >&2
        exit 1
      fi
      forbidden_raw="$2"
      shift 2
      ;;
    --exclude)
      if [[ $# -lt 2 ]]; then
        echo "--exclude requires a value." >&2
        exit 1
      fi
      exclude_raw="$2"
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
    echo "Unable to resolve origin/${ref} for scope check." >&2
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

split_patterns() {
  local raw="$1"
  raw="${raw//,/ }"
  local pat
  set -f
  for pat in ${raw}; do
    [[ -n "${pat}" ]] && printf '%s\n' "${pat}"
  done
  set +f
}

matches_any_pattern() {
  local file="$1"
  shift
  local pat
  for pat in "$@"; do
    [[ -z "${pat}" ]] && continue
    if [[ "${file}" == ${pat} ]]; then
      return 0
    fi
  done
  return 1
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
  echo "scope_check: no changed files detected."
  exit 0
fi

allowed_patterns=()
forbidden_patterns=()
exclude_patterns=()

if [[ -n "${allowed_raw}" ]]; then
  while IFS= read -r pat; do
    [[ -n "${pat}" ]] && allowed_patterns+=("${pat}")
  done < <(split_patterns "${allowed_raw}")
fi
if [[ -n "${forbidden_raw}" ]]; then
  while IFS= read -r pat; do
    [[ -n "${pat}" ]] && forbidden_patterns+=("${pat}")
  done < <(split_patterns "${forbidden_raw}")
fi
if [[ -n "${exclude_raw}" ]]; then
  while IFS= read -r pat; do
    [[ -n "${pat}" ]] && exclude_patterns+=("${pat}")
  done < <(split_patterns "${exclude_raw}")
fi

errors=()
for file in "${changed_files[@]}"; do
  if [[ ${#exclude_patterns[@]} -gt 0 ]] && matches_any_pattern "${file}" "${exclude_patterns[@]}"; then
    continue
  fi

  if [[ ${#allowed_patterns[@]} -gt 0 ]] && ! matches_any_pattern "${file}" "${allowed_patterns[@]}"; then
    errors+=("File not in allowed scope: ${file}")
    continue
  fi

  if [[ ${#forbidden_patterns[@]} -gt 0 ]] && matches_any_pattern "${file}" "${forbidden_patterns[@]}"; then
    errors+=("File in forbidden scope: ${file}")
  fi
done

if [[ ${#errors[@]} -gt 0 ]]; then
  printf 'scope_check failed:\n' >&2
  printf ' - %s\n' "${errors[@]}" >&2
  exit 1
fi

echo "scope_check: passed."
