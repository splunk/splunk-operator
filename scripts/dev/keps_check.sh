#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: scripts/dev/keps_check.sh [--base-ref <branch>] [--help]

Validates component-to-KEP mapping for implementation-gated changes.
- Detects impacted components from changed paths.
- Collects referenced change IDs (CSPL-#### / GH-####) from specs, manifests,
  change docs, and commit messages.
- Ensures every impacted component maps to at least one referenced ID in
  docs/specs/COMPONENT_KEP_INDEX.md.

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
    echo "Unable to resolve origin/${ref} for keps check." >&2
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

extract_ids_from_text() {
  awk '
    {
      while (match($0, /(CSPL-[0-9]+|GH-[0-9]+)/)) {
        print substr($0, RSTART, RLENGTH)
        $0 = substr($0, RSTART + RLENGTH)
      }
    }
  '
}

component_has_section() {
  local component="$1"
  if command -v rg >/dev/null 2>&1; then
    rg -q "^## ${component}$" "${index_file}"
  else
    grep -Eq "^## ${component}$" "${index_file}"
  fi
}

component_has_id() {
  local component="$1"
  local id="$2"
  awk -v section="## ${component}" '
    $0 == section {in_section=1; next}
    in_section && /^## / {exit}
    in_section {print}
  ' "${index_file}" | grep -Fq "${id}"
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
  echo "keps_check: no changed files detected."
  exit 0
fi

declare -A impacted_components=()
mark_component() {
  impacted_components["$1"]=1
}

for file in "${changed_files[@]}"; do
  case "${file}" in
    api/v*/*)
      mark_component "api-crd"
      ;;
    internal/controller/*.go|internal/controller/*/*.go)
      mark_component "controller-reconcile"
      ;;
    pkg/splunk/*.go|pkg/splunk/*/*.go|pkg/splunk/*/*/*.go)
      mark_component "enterprise-runtime"
      ;;
    config/crd/*|config/manager/*|config/default/*|config/samples/*|bundle/*|helm-chart/*)
      mark_component "manifests-release"
      ;;
    test/*|kuttl/*)
      mark_component "test-harness"
      ;;
    scripts/*|harness/*|docs/agent/*|docs/specs/*|docs/changes/*|.github/workflows/*|.agents/skills/*|Makefile|skaffold.yaml|AGENTS.md)
      mark_component "governance-harness"
      ;;
    docs/AppFramework.md|pkg/splunk/enterprise/afwscheduler.go|pkg/splunk/enterprise/afwscheduler_test.go|test/testenv/appframework_utils.go|test/appframework_*/*)
      mark_component "app-framework"
      ;;
  esac
done

if [[ ${#impacted_components[@]} -eq 0 ]]; then
  echo "keps_check: passed (no KEP-mapped components impacted)."
  exit 0
fi

index_file="docs/specs/COMPONENT_KEP_INDEX.md"
if [[ ! -f "${index_file}" ]]; then
  echo "keps_check: FAIL: missing ${index_file}" >&2
  exit 1
fi

declare -A referenced_ids=()
add_id() {
  local id="${1:-}"
  [[ -n "${id}" ]] || return 0
  referenced_ids["${id}"]=1
}

for file in "${changed_files[@]}"; do
  case "${file}" in
    docs/specs/*.md)
      base_name="$(basename "${file}")"
      if [[ "${base_name}" != "README.md" && "${base_name}" != "SPEC_TEMPLATE.md" && "${base_name}" != "COMPONENT_KEP_INDEX.md" ]]; then
        while IFS= read -r id; do
          add_id "${id}"
        done < <(printf '%s\n' "${file}" | extract_ids_from_text | sort -u)

        if [[ -f "${file}" ]]; then
          while IFS= read -r id; do
            add_id "${id}"
          done < <(extract_ids_from_text < "${file}" | sort -u)
        fi
      fi
      ;;
    docs/changes/*.md|harness/manifests/*.yaml|harness/manifests/*.yml)
      while IFS= read -r id; do
        add_id "${id}"
      done < <(printf '%s\n' "${file}" | extract_ids_from_text | sort -u)

      if [[ -f "${file}" ]]; then
        while IFS= read -r id; do
          add_id "${id}"
        done < <(extract_ids_from_text < "${file}" | sort -u)

        if [[ "${file}" == harness/manifests/*.y*ml ]]; then
          manifest_change_id="$(grep -E '^change_id:' "${file}" | head -n1 | cut -d':' -f2- | xargs || true)"
          add_id "${manifest_change_id}"
        fi
      fi
      ;;
  esac
done

if [[ -n "${base_ref}" ]]; then
  remote_ref="refs/remotes/origin/${base_ref}"
  if ! git show-ref --verify --quiet "${remote_ref}"; then
    git fetch --no-tags --depth=200 origin "${base_ref}:${remote_ref}" >/dev/null 2>&1 || true
  fi
  if git show-ref --verify --quiet "${remote_ref}"; then
    while IFS= read -r id; do
      add_id "${id}"
    done < <(git log --pretty=%B "${remote_ref}"..HEAD 2>/dev/null | extract_ids_from_text | sort -u || true)
  fi
else
  while IFS= read -r id; do
    add_id "${id}"
  done < <(git log -n 50 --pretty=%B 2>/dev/null | extract_ids_from_text | sort -u || true)
fi

if [[ ${#referenced_ids[@]} -eq 0 ]]; then
  cat >&2 <<'ERROR'
keps_check: FAIL: impacted components detected without referenced change IDs.

Include at least one ID (CSPL-#### or GH-####) in:
- docs/specs/<id>-*.md, or
- docs/changes/*.md, or
- harness/manifests/*.yaml (change_id), or
- commit message.
ERROR
  exit 1
fi

missing=0
for component in "${!impacted_components[@]}"; do
  if ! component_has_section "${component}"; then
    echo "keps_check: FAIL: missing component section in ${index_file}: ${component}" >&2
    missing=1
    continue
  fi

  matched=0
  for id in "${!referenced_ids[@]}"; do
    if component_has_id "${component}" "${id}"; then
      matched=1
      break
    fi
  done

  if [[ "${matched}" -eq 0 ]]; then
    echo "keps_check: FAIL: component '${component}' has no referenced ID mapped in ${index_file}" >&2
    echo "keps_check: referenced IDs: $(printf '%s ' "${!referenced_ids[@]}")" >&2
    missing=1
  fi
done

if [[ "${missing}" -ne 0 ]]; then
  exit 1
fi

echo "keps_check: passed."
