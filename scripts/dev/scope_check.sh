#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(git -C "${script_dir}" rev-parse --show-toplevel 2>/dev/null || true)"
if [[ -z "${repo_root}" ]]; then
  echo "Unable to locate repo root. Run from inside the git repo." >&2
  exit 1
fi

cd "${repo_root}"

allowed_raw="${ALLOWED_PATHS:-}"
forbidden_raw="${FORBIDDEN_PATHS:-}"

split_patterns() {
  local raw="$1"
  raw="${raw//,/ }"
  echo "${raw}"
}

allowed_patterns=()
forbidden_patterns=()

if [[ -n "${allowed_raw}" ]]; then
  # shellcheck disable=SC2206
  allowed_patterns=($(split_patterns "${allowed_raw}"))
fi
if [[ -n "${forbidden_raw}" ]]; then
  # shellcheck disable=SC2206
  forbidden_patterns=($(split_patterns "${forbidden_raw}"))
fi

files=()
while IFS= read -r line; do
  [[ -n "${line}" ]] && files+=("${line}")
done < <(git diff --name-only --diff-filter=ACMR)

while IFS= read -r line; do
  [[ -n "${line}" ]] && files+=("${line}")
done < <(git diff --name-only --cached --diff-filter=ACMR)

while IFS= read -r line; do
  [[ -n "${line}" ]] && files+=("${line}")
done < <(git ls-files --others --exclude-standard)

if [[ ${#files[@]} -eq 0 ]]; then
  echo "No changed files detected."
  exit 0
fi

errors=()

if [[ ${#allowed_patterns[@]} -gt 0 ]]; then
  for file in "${files[@]}"; do
    matched=false
    for pat in "${allowed_patterns[@]}"; do
      if [[ "${file}" == ${pat} ]]; then
        matched=true
        break
      fi
    done
    if [[ "${matched}" == "false" ]]; then
      errors+=("File not in allowed scope: ${file}")
    fi
  done
fi

if [[ ${#forbidden_patterns[@]} -gt 0 ]]; then
  for file in "${files[@]}"; do
    for pat in "${forbidden_patterns[@]}"; do
      if [[ "${file}" == ${pat} ]]; then
        errors+=("File in forbidden scope: ${file} (matches ${pat})")
      fi
    done
  done
fi

if [[ ${#errors[@]} -gt 0 ]]; then
  printf "Scope check failed:\n"
  printf " - %s\n" "${errors[@]}"
  exit 1
fi

echo "Scope check passed."
