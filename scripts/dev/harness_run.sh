#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: scripts/dev/harness_run.sh [--base-ref <branch>] [--suite <path>] [--fast] [--skip-pr-check] [--help]

Runs harness gates and stores auditable artifacts in .harness/runs/<timestamp>-<sha>/.
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
run_fast=false
skip_pr_check=false

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
    --suite)
      if [[ $# -lt 2 ]]; then
        echo "--suite requires a value." >&2
        exit 1
      fi
      suite="$2"
      shift 2
      ;;
    --fast)
      run_fast=true
      shift
      ;;
    --skip-pr-check)
      skip_pr_check=true
      shift
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

short_sha="$(git rev-parse --short HEAD)"
timestamp="$(date -u +"%Y%m%dT%H%M%SZ")"
run_dir=".harness/runs/${timestamp}-${short_sha}"
mkdir -p "${run_dir}"

trace_file="${run_dir}/trace.tsv"
summary_file="${run_dir}/summary.txt"

echo -e "step\tstatus\texit_code\tstarted_at\tended_at\tcommand\tlog_file" > "${trace_file}"

failures=0
run_step() {
  local step="$1"
  shift
  local log_file="${run_dir}/${step}.log"
  local started_at ended_at rc status

  started_at="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
  set +e
  "$@" >"${log_file}" 2>&1
  rc=$?
  set -e
  ended_at="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"

  if [[ ${rc} -eq 0 ]]; then
    status="pass"
  else
    status="fail"
    failures=$((failures + 1))
  fi

  echo -e "${step}\t${status}\t${rc}\t${started_at}\t${ended_at}\t$*\t${log_file}" >> "${trace_file}"
  echo "[${status^^}] ${step} (log: ${log_file})"
}

base_args=()
if [[ -n "${base_ref}" ]]; then
  base_args+=(--base-ref "${base_ref}")
fi

run_step spec_check scripts/dev/spec_check.sh "${base_args[@]}"
run_step manifest_check scripts/dev/harness_manifest_check.sh "${base_args[@]}"
run_step risk_policy_check scripts/dev/risk_policy_check.sh "${base_args[@]}"
run_step harness_eval scripts/dev/harness_eval.sh --suite "${suite}"

if [[ "${skip_pr_check}" != "true" ]]; then
  if [[ "${run_fast}" == "true" ]]; then
    run_step pr_check bash -lc "SKIP_SPEC_CHECK=1 SKIP_MANIFEST_CHECK=1 SKIP_RISK_POLICY_CHECK=1 SKIP_RISK_LABEL_CHECK=1 SKIP_HARNESS_EVAL=1 PR_CHECK_FLAGS=--fast scripts/dev/pr_check.sh"
  else
    run_step pr_check bash -lc "SKIP_SPEC_CHECK=1 SKIP_MANIFEST_CHECK=1 SKIP_RISK_POLICY_CHECK=1 SKIP_RISK_LABEL_CHECK=1 SKIP_HARNESS_EVAL=1 scripts/dev/pr_check.sh"
  fi
fi

{
  echo "Harness Run Summary"
  echo "run_dir: ${run_dir}"
  echo "git_sha: $(git rev-parse HEAD)"
  echo "branch: $(git branch --show-current)"
  echo "base_ref: ${base_ref:-<none>}"
  echo "suite: ${suite}"
  echo "failed_steps: ${failures}"
  echo ""
  echo "Changed files:"
  if [[ -n "${base_ref}" ]]; then
    remote_ref="refs/remotes/origin/${base_ref}"
    merge_base="$(git merge-base HEAD "${remote_ref}" || true)"
    if [[ -n "${merge_base}" ]]; then
      git diff --name-only --diff-filter=ACMRT "${merge_base}...HEAD"
    else
      echo "<unable to compute merge-base against origin/${base_ref}>"
    fi
  else
    git diff --name-only --diff-filter=ACMRT
    git diff --name-only --cached --diff-filter=ACMRT
    git ls-files --others --exclude-standard
  fi
} > "${summary_file}"

if [[ ${failures} -gt 0 ]]; then
  echo "harness_run: completed with failures (see ${summary_file})." >&2
  exit 1
fi

echo "harness_run: passed (artifacts: ${run_dir})."
