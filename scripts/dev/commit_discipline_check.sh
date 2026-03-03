#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: scripts/dev/commit_discipline_check.sh [--base-ref <branch>] [--help]

Enforces incremental commit discipline for implementation-gated paths.
- Requires at least 2 non-merge commits for implementation-heavy branches.
- Allows a single commit only with explicit override token: [single-commit-ok].
- Fails oversized commits by changed file count threshold.

Options:
  --base-ref <branch>  Compare commit history against origin/<branch> (CI mode).
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

base_input="${COMMIT_BASE_REF:-${KEP_BASE_REF:-origin/develop}}"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --base-ref)
      if [[ $# -lt 2 ]]; then
        echo "--base-ref requires a value." >&2
        exit 1
      fi
      base_input="$2"
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

resolve_base_ref() {
  local candidate="$1"

  if git rev-parse --verify "${candidate}" >/dev/null 2>&1; then
    printf '%s\n' "${candidate}"
    return 0
  fi

  local normalized="${candidate#origin/}"
  local remote_ref="refs/remotes/origin/${normalized}"
  if ! git show-ref --verify --quiet "${remote_ref}"; then
    git fetch --no-tags --depth=200 origin "${normalized}:${remote_ref}" >/dev/null 2>&1 || true
  fi
  if git show-ref --verify --quiet "${remote_ref}"; then
    printf '%s\n' "${remote_ref}"
    return 0
  fi

  local fallback
  for fallback in refs/remotes/origin/develop refs/remotes/origin/main refs/remotes/origin/master; do
    if git show-ref --verify --quiet "${fallback}"; then
      printf '%s\n' "${fallback}"
      return 0
    fi
  done

  return 1
}

if ! base_ref="$(resolve_base_ref "${base_input}")"; then
  echo "commit_discipline_check: unable to resolve base ref (set --base-ref or COMMIT_BASE_REF)."
  exit 0
fi

if ! merge_base="$(git merge-base HEAD "${base_ref}" || true)"; then
  merge_base=""
fi
if [[ -z "${merge_base}" ]]; then
  echo "commit_discipline_check: unable to compute merge-base against ${base_ref}."
  exit 1
fi

range_files="$(git diff --name-only "${merge_base}...HEAD" --)"
if [[ -z "${range_files}" ]]; then
  echo "commit_discipline_check: no changes detected in ${merge_base}...HEAD"
  exit 0
fi

requires_incremental=0
while IFS= read -r file; do
  [[ -n "${file}" ]] || continue
  case "${file}" in
    api/*|cmd/*|config/*|internal/*|kuttl/*|pkg/*|scripts/*|test/*|Makefile|go.mod|go.sum|PROJECT|skaffold.yaml|.github/workflows/*)
      requires_incremental=1
      ;;
  esac
done < <(printf '%s\n' "${range_files}")

if [[ "${requires_incremental}" -eq 0 ]]; then
  echo "commit_discipline_check: no commit-discipline-gated paths changed."
  exit 0
fi

commits="$(git rev-list --reverse "${base_ref}"..HEAD)"
if [[ -z "${commits}" ]]; then
  echo "commit_discipline_check: no commits in range."
  exit 0
fi

non_merge_count=0
max_files_per_commit="${COMMIT_MAX_FILES_PER_COMMIT:-200}"
for sha in ${commits}; do
  subject="$(git log -1 --pretty=%s "${sha}")"
  if [[ "${subject}" =~ ^Merge[[:space:]] ]]; then
    continue
  fi

  non_merge_count=$((non_merge_count + 1))
  files_changed="$(git show --name-only --pretty=format: "${sha}" | awk 'NF' | wc -l | tr -d '[:space:]')"
  if [[ "${files_changed}" -gt "${max_files_per_commit}" ]]; then
    echo "commit_discipline_check: FAIL: ${sha} touches ${files_changed} files (limit ${max_files_per_commit})." >&2
    exit 1
  fi
done

if [[ "${non_merge_count}" -lt 2 ]]; then
  latest_msg="$(git log -1 --pretty=%B HEAD)"
  if ! printf '%s\n' "${latest_msg}" | grep -q '\[single-commit-ok\]'; then
    cat >&2 <<'ERROR'
commit_discipline_check: FAIL: implementation-gated changes must be split into at least 2 non-merge commits.

If a single commit is required, add [single-commit-ok] in the latest commit message with justification.
ERROR
    exit 1
  fi
fi

echo "commit_discipline_check: passed."
