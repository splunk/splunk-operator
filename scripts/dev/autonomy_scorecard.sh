#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: scripts/dev/autonomy_scorecard.sh [--base-ref <branch>] [--suite <path>] [--output <json>] [--markdown <md>] [--help]

Generates an autonomy scorecard for the current diff.
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
suite="docs/agent/evals/policy-regression.yaml"
json_out=""
md_out=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --base-ref)
      base_ref="$2"
      shift 2
      ;;
    --suite)
      suite="$2"
      shift 2
      ;;
    --output)
      json_out="$2"
      shift 2
      ;;
    --markdown)
      md_out="$2"
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
    echo "Unable to resolve origin/${ref} for autonomy scorecard." >&2
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

total_changed=${#changed_files[@]}
non_trivial_count=0
manifest_files=()
for file in "${changed_files[@]}"; do
  if is_non_trivial_path "${file}"; then
    non_trivial_count=$((non_trivial_count + 1))
  fi
  case "${file}" in
    harness/manifests/*.yaml|harness/manifests/*.yml)
      manifest_files+=("${file}")
      ;;
  esac
done

manifest_count=${#manifest_files[@]}
agent_mode_count=0
hybrid_mode_count=0
human_mode_count=0
risk_low_count=0
risk_medium_count=0
risk_high_count=0

for manifest in "${manifest_files[@]}"; do
  [[ -f "${manifest}" ]] || continue
  mode="$(extract_scalar "delivery_mode" "${manifest}")"
  risk="$(extract_scalar "risk_tier" "${manifest}")"

  case "${mode}" in
    agent) agent_mode_count=$((agent_mode_count + 1)) ;;
    hybrid) hybrid_mode_count=$((hybrid_mode_count + 1)) ;;
    human) human_mode_count=$((human_mode_count + 1)) ;;
  esac

  case "${risk}" in
    low) risk_low_count=$((risk_low_count + 1)) ;;
    medium) risk_medium_count=$((risk_medium_count + 1)) ;;
    high) risk_high_count=$((risk_high_count + 1)) ;;
  esac
done

range=""
if [[ -n "${base_ref}" ]]; then
  remote_ref="refs/remotes/origin/${base_ref}"
  if git show-ref --verify --quiet "${remote_ref}"; then
    merge_base="$(git merge-base HEAD "${remote_ref}" || true)"
    if [[ -n "${merge_base}" ]]; then
      range="${merge_base}...HEAD"
    fi
  fi
fi

if [[ -z "${range}" ]]; then
  if git rev-parse --verify HEAD~1 >/dev/null 2>&1; then
    range="HEAD~1...HEAD"
  else
    range="HEAD"
  fi
fi

total_commits="$(git rev-list --count "${range}" 2>/dev/null || echo 0)"
agent_commits="$(git log --format='%an <%ae>' "${range}" 2>/dev/null | grep -Eic '(codex|openai|bot|agent)' || true)"
if [[ -z "${agent_commits}" ]]; then
  agent_commits=0
fi

agent_commit_ratio=0
if [[ "${total_commits}" -gt 0 ]]; then
  agent_commit_ratio=$((agent_commits * 100 / total_commits))
fi

spec_rc=0
manifest_rc=0
risk_rc=0
eval_rc=0

spec_args=()
if [[ -n "${base_ref}" ]]; then
  spec_args+=(--base-ref "${base_ref}")
fi

set +e
scripts/dev/spec_check.sh "${spec_args[@]}" >/tmp/scorecard-spec.log 2>&1
spec_rc=$?
scripts/dev/harness_manifest_check.sh "${spec_args[@]}" >/tmp/scorecard-manifest.log 2>&1
manifest_rc=$?
scripts/dev/risk_policy_check.sh "${spec_args[@]}" >/tmp/scorecard-risk.log 2>&1
risk_rc=$?
scripts/dev/harness_eval.sh --suite "${suite}" >/tmp/scorecard-eval.log 2>&1
eval_rc=$?
set -e

score=0
[[ ${spec_rc} -eq 0 ]] && score=$((score + 25))
[[ ${manifest_rc} -eq 0 ]] && score=$((score + 25))
[[ ${risk_rc} -eq 0 ]] && score=$((score + 20))
[[ ${eval_rc} -eq 0 ]] && score=$((score + 20))
if [[ ${agent_commit_ratio} -ge 50 || ${agent_mode_count} -gt 0 ]]; then
  score=$((score + 10))
fi

status="needs-work"
if [[ ${score} -ge 90 ]]; then
  status="excellent"
elif [[ ${score} -ge 75 ]]; then
  status="good"
elif [[ ${score} -ge 60 ]]; then
  status="fair"
fi

json_payload="{\n"
json_payload+="  \"generated_at\": \"$(date -u +"%Y-%m-%dT%H:%M:%SZ")\",\n"
json_payload+="  \"branch\": \"$(git branch --show-current)\",\n"
json_payload+="  \"head_sha\": \"$(git rev-parse HEAD)\",\n"
json_payload+="  \"base_ref\": \"${base_ref}\",\n"
json_payload+="  \"changed_files\": ${total_changed},\n"
json_payload+="  \"non_trivial_files\": ${non_trivial_count},\n"
json_payload+="  \"manifests_changed\": ${manifest_count},\n"
json_payload+="  \"risk_tiers\": {\"low\": ${risk_low_count}, \"medium\": ${risk_medium_count}, \"high\": ${risk_high_count}},\n"
json_payload+="  \"delivery_modes\": {\"agent\": ${agent_mode_count}, \"hybrid\": ${hybrid_mode_count}, \"human\": ${human_mode_count}},\n"
json_payload+="  \"commit_metrics\": {\"total\": ${total_commits}, \"agent_like\": ${agent_commits}, \"agent_like_ratio\": ${agent_commit_ratio}},\n"
json_payload+="  \"gate_results\": {\"spec_check\": ${spec_rc}, \"manifest_check\": ${manifest_rc}, \"risk_policy_check\": ${risk_rc}, \"harness_eval\": ${eval_rc}},\n"
json_payload+="  \"autonomy_score\": ${score},\n"
json_payload+="  \"status\": \"${status}\"\n"
json_payload+="}"

markdown_payload="## Autonomy Scorecard\n"
markdown_payload+="- Status: **${status}**\n"
markdown_payload+="- Score: **${score}/100**\n"
markdown_payload+="- Branch: $(git branch --show-current)\n"
markdown_payload+="- Head SHA: $(git rev-parse --short HEAD)\n"
if [[ -n "${base_ref}" ]]; then
  markdown_payload+="- Base ref: ${base_ref}\n"
fi
markdown_payload+="\n### Diff Metrics\n"
markdown_payload+="- Changed files: ${total_changed}\n"
markdown_payload+="- Non-trivial changed files: ${non_trivial_count}\n"
markdown_payload+="- Manifests changed: ${manifest_count}\n"
markdown_payload+="\n### Delivery Modes\n"
markdown_payload+="- Agent: ${agent_mode_count}\n"
markdown_payload+="- Hybrid: ${hybrid_mode_count}\n"
markdown_payload+="- Human: ${human_mode_count}\n"
markdown_payload+="\n### Risk Tiers\n"
markdown_payload+="- Low: ${risk_low_count}\n"
markdown_payload+="- Medium: ${risk_medium_count}\n"
markdown_payload+="- High: ${risk_high_count}\n"
markdown_payload+="\n### Commit Signals\n"
markdown_payload+="- Total commits in range: ${total_commits}\n"
markdown_payload+="- Agent-like commit authors: ${agent_commits}\n"
markdown_payload+="- Agent-like commit ratio: ${agent_commit_ratio}%\n"
markdown_payload+="\n### Gate Results\n"
markdown_payload+="- spec_check: ${spec_rc}\n"
markdown_payload+="- harness_manifest_check: ${manifest_rc}\n"
markdown_payload+="- risk_policy_check: ${risk_rc}\n"
markdown_payload+="- harness_eval: ${eval_rc}\n"

if [[ -n "${json_out}" ]]; then
  mkdir -p "$(dirname "${json_out}")"
  printf '%b\n' "${json_payload}" > "${json_out}"
else
  printf '%b\n' "${json_payload}"
fi

if [[ -n "${md_out}" ]]; then
  mkdir -p "$(dirname "${md_out}")"
  printf '%b\n' "${markdown_payload}" > "${md_out}"
else
  printf '%b\n' "${markdown_payload}"
fi
