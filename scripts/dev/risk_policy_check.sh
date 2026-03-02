#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: scripts/dev/risk_policy_check.sh [--base-ref <branch>] [--help]

Checks risk-tier governance on changed harness manifests.
- Validates required risk keys and allowed values.
- Enforces minimum approvals/merge queue policy by risk tier.
- Enforces command-depth requirements for medium/high risk changes.

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
    echo "Unable to resolve origin/${ref} for risk policy check." >&2
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
  echo "risk_policy_check: no changed files detected."
  exit 0
fi

manifest_files=()
for file in "${changed_files[@]}"; do
  case "${file}" in
    harness/manifests/*.yaml|harness/manifests/*.yml)
      manifest_files+=("${file}")
      ;;
  esac
done

if [[ ${#manifest_files[@]} -eq 0 ]]; then
  echo "risk_policy_check: no changed manifest files detected."
  exit 0
fi

errors=()
for manifest in "${manifest_files[@]}"; do
  if [[ ! -f "${manifest}" ]]; then
    errors+=("${manifest}: file missing")
    continue
  fi

  manifest_name="$(basename "${manifest}")"
  if [[ "${manifest_name}" == "EXAMPLE.yaml" || "${manifest_name}" == "EXAMPLE.yml" ]]; then
    continue
  fi

  risk_tier="$(extract_scalar "risk_tier" "${manifest}")"
  delivery_mode="$(extract_scalar "delivery_mode" "${manifest}")"
  approvals="$(extract_scalar "human_approvals_required" "${manifest}")"
  auto_merge="$(extract_scalar "auto_merge_allowed" "${manifest}")"
  merge_queue="$(extract_scalar "merge_queue_required" "${manifest}")"

  if [[ -z "${risk_tier}" ]]; then
    errors+=("${manifest}: missing required key 'risk_tier'")
    continue
  fi
  if [[ -z "${delivery_mode}" ]]; then
    errors+=("${manifest}: missing required key 'delivery_mode'")
  fi
  if [[ -z "${approvals}" ]]; then
    errors+=("${manifest}: missing required key 'human_approvals_required'")
  fi
  if [[ -z "${auto_merge}" ]]; then
    errors+=("${manifest}: missing required key 'auto_merge_allowed'")
  fi
  if [[ -z "${merge_queue}" ]]; then
    errors+=("${manifest}: missing required key 'merge_queue_required'")
  fi

  case "${risk_tier}" in
    low|medium|high)
      ;;
    *)
      errors+=("${manifest}: invalid risk_tier '${risk_tier}' (use low|medium|high)")
      ;;
  esac

  case "${delivery_mode}" in
    agent|hybrid|human)
      ;;
    *)
      errors+=("${manifest}: invalid delivery_mode '${delivery_mode}' (use agent|hybrid|human)")
      ;;
  esac

  if [[ ! "${approvals}" =~ ^[0-9]+$ ]]; then
    errors+=("${manifest}: human_approvals_required must be an integer")
    continue
  fi

  if [[ "${auto_merge}" != "true" && "${auto_merge}" != "false" ]]; then
    errors+=("${manifest}: auto_merge_allowed must be true|false")
  fi

  if [[ "${merge_queue}" != "true" && "${merge_queue}" != "false" ]]; then
    errors+=("${manifest}: merge_queue_required must be true|false")
  fi

  min_approvals=0
  case "${risk_tier}" in
    low)
      min_approvals=0
      ;;
    medium)
      min_approvals=1
      ;;
    high)
      min_approvals=2
      ;;
  esac

  if (( approvals < min_approvals )); then
    errors+=("${manifest}: approvals ${approvals} below minimum ${min_approvals} for ${risk_tier} risk")
  fi

  if [[ "${risk_tier}" == "medium" || "${risk_tier}" == "high" ]]; then
    if [[ "${merge_queue}" != "true" ]]; then
      errors+=("${manifest}: merge_queue_required must be true for ${risk_tier} risk")
    fi
    if [[ "${auto_merge}" != "false" ]]; then
      errors+=("${manifest}: auto_merge_allowed must be false for ${risk_tier} risk")
    fi
  fi

  mapfile -t required_commands < <(extract_list "required_commands" "${manifest}")
  has_harness_run=false
  has_unit=false
  for cmd in "${required_commands[@]}"; do
    [[ "${cmd}" == scripts/dev/harness_run.sh* ]] && has_harness_run=true
    [[ "${cmd}" == scripts/dev/unit.sh* || "${cmd}" == make\ test* ]] && has_unit=true
  done

  if [[ "${risk_tier}" == "medium" || "${risk_tier}" == "high" ]]; then
    if [[ "${has_harness_run}" != "true" ]]; then
      errors+=("${manifest}: required_commands must include scripts/dev/harness_run.sh for ${risk_tier} risk")
    fi
  fi

  if [[ "${risk_tier}" == "high" ]]; then
    if [[ "${has_unit}" != "true" ]]; then
      errors+=("${manifest}: required_commands must include scripts/dev/unit.sh or make test for high risk")
    fi
  fi
done

if [[ ${#errors[@]} -gt 0 ]]; then
  printf 'risk_policy_check failed:\n' >&2
  printf ' - %s\n' "${errors[@]}" >&2
  exit 1
fi

echo "risk_policy_check: passed."
