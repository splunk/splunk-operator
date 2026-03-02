#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: scripts/dev/risk_label_check.sh [--base-ref <branch>] [--labels <csv>] [--help]

Checks that PR risk label matches manifest risk_tier for changed manifests.

Rules:
- Reads changed manifests under harness/manifests/ (excluding EXAMPLE.yaml).
- Requires exactly one risk label of form risk:low|risk:medium|risk:high.
- Label tier must equal manifest risk_tier.

Options:
  --base-ref <branch>  Compare HEAD against origin/<branch> (CI mode).
  --labels <csv>       Comma-separated labels. Fallback: RISK_LABELS env var.
  -h, --help           Show this help.

Env:
  RISK_LABELS                  Comma-separated labels.
  ENFORCE_RISK_LABEL_CHECK     Set to 1 to fail when labels are unavailable.
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
labels_raw="${RISK_LABELS:-}"
enforce="${ENFORCE_RISK_LABEL_CHECK:-0}"

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
    --labels)
      if [[ $# -lt 2 ]]; then
        echo "--labels requires a value." >&2
        exit 1
      fi
      labels_raw="$2"
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
    echo "Unable to resolve origin/${ref} for risk label check." >&2
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
  echo "risk_label_check: no changed files detected."
  exit 0
fi

manifest_files=()
for file in "${changed_files[@]}"; do
  case "${file}" in
    harness/manifests/*.yaml|harness/manifests/*.yml)
      manifest_name="$(basename "${file}")"
      if [[ "${manifest_name}" != "EXAMPLE.yaml" && "${manifest_name}" != "EXAMPLE.yml" ]]; then
        manifest_files+=("${file}")
      fi
      ;;
  esac
done

if [[ ${#manifest_files[@]} -eq 0 ]]; then
  echo "risk_label_check: no changed non-example manifests detected."
  exit 0
fi

manifest_tier=""
for manifest in "${manifest_files[@]}"; do
  if [[ ! -f "${manifest}" ]]; then
    echo "risk_label_check: missing manifest file: ${manifest}" >&2
    exit 1
  fi

  tier="$(extract_scalar "risk_tier" "${manifest}")"
  case "${tier}" in
    low|medium|high)
      ;;
    *)
      echo "risk_label_check: invalid or missing risk_tier in ${manifest}" >&2
      exit 1
      ;;
  esac

  if [[ -z "${manifest_tier}" ]]; then
    manifest_tier="${tier}"
  elif [[ "${manifest_tier}" != "${tier}" ]]; then
    echo "risk_label_check: manifests in this change have mixed risk tiers (${manifest_tier} vs ${tier})." >&2
    exit 1
  fi
done

if [[ -z "${labels_raw}" ]]; then
  if [[ "${enforce}" == "1" ]]; then
    echo "risk_label_check: labels unavailable; set RISK_LABELS or provide --labels." >&2
    exit 1
  fi
  echo "risk_label_check: labels unavailable in local mode; skipping strict check."
  exit 0
fi

labels_normalized="${labels_raw//,/ }"
risk_labels=()
for label in ${labels_normalized}; do
  label="${label# }"
  label="${label% }"
  case "${label}" in
    risk:low|risk:medium|risk:high)
      risk_labels+=("${label}")
      ;;
  esac
done

if [[ ${#risk_labels[@]} -eq 0 ]]; then
  echo "risk_label_check: missing required risk label. Expected one of risk:low|risk:medium|risk:high" >&2
  exit 1
fi

if [[ ${#risk_labels[@]} -gt 1 ]]; then
  echo "risk_label_check: multiple risk labels found (${risk_labels[*]}). Keep exactly one." >&2
  exit 1
fi

label_tier="${risk_labels[0]#risk:}"
if [[ "${label_tier}" != "${manifest_tier}" ]]; then
  echo "risk_label_check: label risk:${label_tier} does not match manifest risk_tier ${manifest_tier}." >&2
  exit 1
fi

echo "risk_label_check: passed (risk:${label_tier})."
