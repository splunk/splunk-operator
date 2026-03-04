#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: scripts/dev/harness_manifest_check.sh [--base-ref <branch>] [--help]

Checks harness manifest policy for changed files.
- Non-trivial changes require a changed manifest under harness/manifests/.
- Each changed manifest must reference an Approved/Implemented KEP.
- Scope policy from manifest must match changed files.

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
    echo "Unable to resolve origin/${ref} for harness manifest check." >&2
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

extract_scalar() {
  local key="$1"
  local file="$2"
  local line
  line="$(grep -E "^${key}:[[:space:]]*" "${file}" | head -n1 || true)"
  line="${line#*:}"
  line="${line# }"
  line="${line% }"
  line="${line#\"}"
  line="${line%\"}"
  line="${line#\'}"
  line="${line%\'}"
  printf '%s\n' "${line}"
}

extract_list() {
  local key="$1"
  local file="$2"
  awk -v key="${key}" '
    BEGIN {in_section=0}
    $0 ~ "^" key ":[[:space:]]*$" {in_section=1; next}
    in_section == 1 && $0 ~ /^[^[:space:]]/ {in_section=0}
    in_section == 1 && $0 ~ /^[[:space:]]*-[[:space:]]*/ {
      line=$0
      sub(/^[[:space:]]*-[[:space:]]*/, "", line)
      gsub(/^["\047]|["\047]$/, "", line)
      print line
    }
  ' "${file}"
}

has_value() {
  [[ -n "$1" ]]
}

is_non_trivial_path() {
  case "$1" in
    api/*|cmd/*|config/*|internal/*|kuttl/*|pkg/*|scripts/*|test/*|Makefile|go.mod|go.sum|PROJECT)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

has_rg=false
if command -v rg >/dev/null 2>&1; then
  has_rg=true
fi

match_status_approved() {
  local file="$1"
  local pattern='^(- )?Status:[[:space:]]*(Approved|Implemented)[[:space:]]*$'
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
  echo "harness_manifest_check: no changed files detected."
  exit 0
fi

manifest_files=()
non_trivial=false
for file in "${changed_files[@]}"; do
  if is_non_trivial_path "${file}"; then
    non_trivial=true
  fi

  case "${file}" in
    harness/manifests/*.yaml|harness/manifests/*.yml)
      manifest_files+=("${file}")
      ;;
  esac
done

if [[ "${non_trivial}" != "true" ]]; then
  echo "harness_manifest_check: no non-trivial code paths changed."
  exit 0
fi

if [[ ${#manifest_files[@]} -eq 0 ]]; then
  echo "harness_manifest_check: non-trivial changes detected but no manifest changed." >&2
  echo "Add harness/manifests/<ticket>-<topic>.yaml and link an approved KEP." >&2
  exit 1
fi

errors=()
allowed_union=()
forbidden_union=()

for manifest in "${manifest_files[@]}"; do
  if [[ ! -f "${manifest}" ]]; then
    errors+=("${manifest}: file missing")
    continue
  fi

  is_example_manifest=false
  manifest_name="$(basename "${manifest}")"
  if [[ "${manifest_name}" == "EXAMPLE.yaml" || "${manifest_name}" == "EXAMPLE.yml" ]]; then
    is_example_manifest=true
  fi

  version="$(extract_scalar "version" "${manifest}")"
  change_id="$(extract_scalar "change_id" "${manifest}")"
  title="$(extract_scalar "title" "${manifest}")"
  spec_file="$(extract_scalar "spec_file" "${manifest}")"
  owner="$(extract_scalar "owner" "${manifest}")"
  delivery_mode="$(extract_scalar "delivery_mode" "${manifest}")"
  risk_tier="$(extract_scalar "risk_tier" "${manifest}")"
  human_approvals_required="$(extract_scalar "human_approvals_required" "${manifest}")"
  auto_merge_allowed="$(extract_scalar "auto_merge_allowed" "${manifest}")"
  merge_queue_required="$(extract_scalar "merge_queue_required" "${manifest}")"
  evaluation_suite="$(extract_scalar "evaluation_suite" "${manifest}")"

  if ! has_value "${version}"; then
    errors+=("${manifest}: missing required key 'version'")
  fi
  if ! has_value "${change_id}"; then
    errors+=("${manifest}: missing required key 'change_id'")
  fi
  if ! has_value "${title}"; then
    errors+=("${manifest}: missing required key 'title'")
  fi
  if ! has_value "${spec_file}"; then
    errors+=("${manifest}: missing required key 'spec_file'")
  fi
  if ! has_value "${owner}"; then
    errors+=("${manifest}: missing required key 'owner'")
  fi
  if ! has_value "${delivery_mode}"; then
    errors+=("${manifest}: missing required key 'delivery_mode'")
  fi
  if ! has_value "${risk_tier}"; then
    errors+=("${manifest}: missing required key 'risk_tier'")
  fi
  if ! has_value "${human_approvals_required}"; then
    errors+=("${manifest}: missing required key 'human_approvals_required'")
  fi
  if ! has_value "${auto_merge_allowed}"; then
    errors+=("${manifest}: missing required key 'auto_merge_allowed'")
  fi
  if ! has_value "${merge_queue_required}"; then
    errors+=("${manifest}: missing required key 'merge_queue_required'")
  fi
  if ! has_value "${evaluation_suite}"; then
    errors+=("${manifest}: missing required key 'evaluation_suite'")
  fi

  if [[ -n "${spec_file}" && "${is_example_manifest}" != "true" ]]; then
    if [[ ! -f "${spec_file}" ]]; then
      errors+=("${manifest}: referenced spec file not found: ${spec_file}")
    elif ! match_status_approved "${spec_file}"; then
      errors+=("${manifest}: referenced spec must be Status Approved or Implemented: ${spec_file}")
    fi
  fi

  if [[ -n "${evaluation_suite}" && ! -f "${evaluation_suite}" ]]; then
    errors+=("${manifest}: evaluation_suite file not found: ${evaluation_suite}")
  fi

  mapfile -t allowed_paths < <(extract_list "allowed_paths" "${manifest}")
  mapfile -t forbidden_paths < <(extract_list "forbidden_paths" "${manifest}")
  mapfile -t required_commands < <(extract_list "required_commands" "${manifest}")

  if ! grep -Eq '^required_commands:[[:space:]]*$' "${manifest}"; then
    errors+=("${manifest}: missing required key 'required_commands'")
  fi
  if [[ ${#required_commands[@]} -eq 0 ]]; then
    errors+=("${manifest}: required_commands must include at least spec_check and pr_check")
  fi

  has_spec_cmd=false
  has_pr_cmd=false
  has_risk_cmd=false
  for cmd in "${required_commands[@]}"; do
    [[ "${cmd}" == scripts/dev/spec_check.sh* ]] && has_spec_cmd=true
    [[ "${cmd}" == scripts/dev/pr_check.sh* ]] && has_pr_cmd=true
    [[ "${cmd}" == scripts/dev/risk_policy_check.sh* ]] && has_risk_cmd=true
  done
  if [[ "${has_spec_cmd}" != "true" ]]; then
    errors+=("${manifest}: required_commands missing scripts/dev/spec_check.sh")
  fi
  if [[ "${has_pr_cmd}" != "true" ]]; then
    errors+=("${manifest}: required_commands missing scripts/dev/pr_check.sh")
  fi
  if [[ "${has_risk_cmd}" != "true" ]]; then
    errors+=("${manifest}: required_commands missing scripts/dev/risk_policy_check.sh")
  fi

  if grep -Eq '^allowed_paths:[[:space:]]*$' "${manifest}"; then
    for item in "${allowed_paths[@]}"; do
      allowed_union+=("${item}")
    done
  else
    errors+=("${manifest}: missing required key 'allowed_paths'")
  fi

  if grep -Eq '^forbidden_paths:[[:space:]]*$' "${manifest}"; then
    for item in "${forbidden_paths[@]}"; do
      forbidden_union+=("${item}")
    done
  else
    errors+=("${manifest}: missing required key 'forbidden_paths'")
  fi

done

if [[ ${#errors[@]} -gt 0 ]]; then
  printf 'harness_manifest_check failed:\n' >&2
  printf ' - %s\n' "${errors[@]}" >&2
  exit 1
fi

if [[ ${#allowed_union[@]} -eq 0 ]]; then
  echo "harness_manifest_check failed: manifest allowed_paths resolved to empty set." >&2
  exit 1
fi

allowed_csv="$(IFS=,; echo "${allowed_union[*]}")"
forbidden_csv="$(IFS=,; echo "${forbidden_union[*]}")"

scope_args=()
if [[ -n "${base_ref}" ]]; then
  scope_args+=(--base-ref "${base_ref}")
fi

ALLOWED_PATHS="${allowed_csv}" FORBIDDEN_PATHS="${forbidden_csv}" \
  scripts/dev/scope_check.sh "${scope_args[@]}"

echo "harness_manifest_check: passed."
