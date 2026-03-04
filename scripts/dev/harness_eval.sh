#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: scripts/dev/harness_eval.sh [--suite <path>] [--help]

Runs replayable governance regression checks from a suite YAML.
Default suite: docs/agent/evals/policy-regression.yaml
USAGE
}

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(git -C "${script_dir}" rev-parse --show-toplevel 2>/dev/null || true)"
if [[ -z "${repo_root}" ]]; then
  echo "Unable to locate repo root. Run from inside the git repo." >&2
  exit 1
fi
cd "${repo_root}"

suite="docs/agent/evals/policy-regression.yaml"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --suite)
      if [[ $# -lt 2 ]]; then
        echo "--suite requires a value." >&2
        exit 1
      fi
      suite="$2"
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

if [[ ! -f "${suite}" ]]; then
  echo "harness_eval: suite file not found: ${suite}" >&2
  exit 1
fi

has_rg=false
if command -v rg >/dev/null 2>&1; then
  has_rg=true
fi

contains_pattern() {
  local file="$1"
  local pattern="$2"
  if [[ "${has_rg}" == "true" ]]; then
    rg -Fq -- "${pattern}" "${file}"
  else
    grep -Fq -- "${pattern}" "${file}"
  fi
}

parse_cases() {
  local suite_file="$1"
  awk '
    function trim(s) {
      gsub(/^[[:space:]]+|[[:space:]]+$/, "", s)
      gsub(/^["\047]|["\047]$/, "", s)
      return s
    }
    function flush_case() {
      if (id != "" && file != "" && pattern != "") {
        print id "|" file "|" pattern
      }
      id = ""
      file = ""
      pattern = ""
    }
    /^[[:space:]]*-[[:space:]]id:[[:space:]]*/ {
      flush_case()
      line = $0
      sub(/^[[:space:]]*-[[:space:]]id:[[:space:]]*/, "", line)
      id = trim(line)
      next
    }
    /^[[:space:]]*file:[[:space:]]*/ {
      line = $0
      sub(/^[[:space:]]*file:[[:space:]]*/, "", line)
      file = trim(line)
      next
    }
    /^[[:space:]]*pattern:[[:space:]]*/ {
      line = $0
      sub(/^[[:space:]]*pattern:[[:space:]]*/, "", line)
      pattern = trim(line)
      next
    }
    END {
      flush_case()
    }
  ' "${suite_file}"
}

mapfile -t cases < <(parse_cases "${suite}")
if [[ ${#cases[@]} -eq 0 ]]; then
  echo "harness_eval: no cases found in ${suite}" >&2
  exit 1
fi

total=0
passed=0
failed=0

for entry in "${cases[@]}"; do
  total=$((total + 1))
  IFS='|' read -r case_id case_file case_pattern <<<"${entry}"

  if [[ ! -f "${case_file}" ]]; then
    echo "[FAIL] ${case_id}: missing file ${case_file}"
    failed=$((failed + 1))
    continue
  fi

  if contains_pattern "${case_file}" "${case_pattern}"; then
    echo "[PASS] ${case_id}"
    passed=$((passed + 1))
  else
    echo "[FAIL] ${case_id}: pattern not found in ${case_file}: ${case_pattern}"
    failed=$((failed + 1))
  fi
done

score=0
if [[ ${total} -gt 0 ]]; then
  score=$((passed * 100 / total))
fi

echo "harness_eval: total=${total} passed=${passed} failed=${failed} score=${score}%"

if [[ ${failed} -gt 0 ]]; then
  exit 1
fi
