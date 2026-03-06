#!/usr/bin/env bash
#
# Download JUnit test report artifacts from GitHub Actions and run flaky test detection.
#
# Usage:
#   ./tools/flaky-test-analysis.sh [start-date] [end-date] [--branch <name>] [--dry-run] [--skip-analysis]
#
# Dates default to the last 7 full days (excluding today) if not provided.
#
# Examples:
#   ./tools/flaky-test-analysis.sh                          # last 7 full days
#   ./tools/flaky-test-analysis.sh 2026-02-01 2026-02-26
#   ./tools/flaky-test-analysis.sh --branch develop         # only runs on develop
#   ./tools/flaky-test-analysis.sh --dry-run                # preview only
#   ./tools/flaky-test-analysis.sh --skip-analysis          # download only, no flaky detection
#
# Requires: gh (GitHub CLI), authenticated via 'gh auth login' or GH_TOKEN.
# Optional: flaky-tests-detection (pip install flaky-tests-detection)
#
set -euo pipefail

REPO="${REPO:-splunk/splunk-operator}"
OUTPUT_DIR="${OUTPUT_DIR:-./junit-reports}"
ARTIFACT_PATTERN="${ARTIFACT_PATTERN:-^test-report-.*}"
TOP_N="${TOP_N:-20}"
WINDOW_SIZE="${WINDOW_SIZE:-1}"

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  sed -n '2,17p' "$0" | sed 's/^# \?//'
  exit 0
fi

DRY_RUN="${DRY_RUN:-false}"
SKIP_ANALYSIS="${SKIP_ANALYSIS:-false}"
BRANCH="${BRANCH:-}"
START_DATE="${START_DATE:-}"
END_DATE="${END_DATE:-}"

_next_is_branch=false
for arg in "$@"; do
  if [[ "$_next_is_branch" == "true" ]]; then
    BRANCH="$arg"
    _next_is_branch=false
  elif [[ "$arg" == "--branch" ]]; then
    _next_is_branch=true
  elif [[ "$arg" == --branch=* ]]; then
    BRANCH="${arg#--branch=}"
  elif [[ "$arg" == "--dry-run" ]]; then
    DRY_RUN=true
  elif [[ "$arg" == "--skip-analysis" ]]; then
    SKIP_ANALYSIS=true
  elif [[ -z "$START_DATE" ]]; then
    START_DATE="$arg"
  elif [[ -z "$END_DATE" ]]; then
    END_DATE="$arg"
  fi
done

END_DATE="${END_DATE:-$(date -u -v-1d +%Y-%m-%d 2>/dev/null || date -u -d 'yesterday' +%Y-%m-%d)}"
START_DATE="${START_DATE:-$(date -u -v-7d +%Y-%m-%d 2>/dev/null || date -u -d '7 days ago' +%Y-%m-%d)}"

# Derive WINDOW_COUNT from the date range (one window per WINDOW_SIZE days)
if date -v+0d +%s &>/dev/null; then
  _start_epoch=$(date -jf "%Y-%m-%d" "$START_DATE" +%s)
  _end_epoch=$(date -jf "%Y-%m-%d" "$END_DATE" +%s)
else
  _start_epoch=$(date -d "$START_DATE" +%s)
  _end_epoch=$(date -d "$END_DATE" +%s)
fi
WINDOW_COUNT=$(( ((_end_epoch - _start_epoch) / 86400 + WINDOW_SIZE) / WINDOW_SIZE ))

if ! command -v gh &>/dev/null; then
  echo "ERROR: 'gh' (GitHub CLI) is required. Install from https://cli.github.com/" >&2
  exit 1
fi

echo "Repository:  $REPO"
echo "Date range:  $START_DATE .. $END_DATE"
if [[ -n "$BRANCH" ]]; then
  echo "Branch:      $BRANCH"
fi
echo "Output:      $OUTPUT_DIR"
echo ""

BRANCH_FILTER=""
if [[ -n "$BRANCH" ]]; then
  BRANCH_FILTER="| select(.workflow_run.head_branch == \"${BRANCH}\")"
fi
ART_FILTER=".artifacts[] | select(.name | test(\"${ARTIFACT_PATTERN}\")) | select(.expired == false) ${BRANCH_FILTER}"
DATE_FI LTER="select(.created_at >= \"${START_DATE}T00:00:00Z\" and .created_at <= \"${END_DATE}T23:59:59Z\")"

echo "Fetching artifact list..."

artifacts_json="[]"
page=1
while true; do
  response=$(gh api "repos/${REPO}/actions/artifacts?per_page=100&page=${page}" 2>/dev/null)

  page_artifacts=$(echo "$response" | \
    jq "[${ART_FILTER} | ${DATE_FILTER} | {id, name, created_at, workflow_run_id: .workflow_run.id}]")
  artifacts_json=$(echo "$artifacts_json" "$page_artifacts" | jq -s 'add')

  oldest=$(echo "$response" | jq -r '.artifacts[-1].created_at // empty')
  if [[ -z "$oldest" || "$oldest" < "${START_DATE}T00:00:00Z" ]]; then
    break
  fi

  count=$(echo "$response" | jq '.artifacts | length')
  if [[ "$count" -lt 100 ]]; then
    break
  fi

  page=$((page + 1))
done

artifact_count=$(echo "$artifacts_json" | jq 'length')
echo "Found $artifact_count matching artifacts."
echo ""

if [[ "$artifact_count" -eq 0 ]]; then
  echo "No artifacts matched. Check your date range and artifact retention settings."
  exit 0
fi

echo "--------------------------------------------------------------"
printf "%-12s %-50s %s\n" "RUN ID" "ARTIFACT NAME" "CREATED"
echo "--------------------------------------------------------------"
echo "$artifacts_json" | jq -r '.[] | "\(.workflow_run_id)\t\(.name)\t\(.created_at)"' | \
  while IFS=$'\t' read -r run_id name created; do
    printf "%-12s %-50s %s\n" "$run_id" "$name" "${created%%T*}"
  done
echo "--------------------------------------------------------------"
echo ""

if [[ "$DRY_RUN" == "true" ]]; then
  echo "(dry run - skipping downloads)"
  exit 0
fi

mkdir -p "$OUTPUT_DIR"

tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT

dl_current=0
echo "$artifacts_json" | jq -r '.[] | "\(.id)\t\(.name)\t\(.workflow_run_id)"' | \
  while IFS=$'\t' read -r art_id art_name run_id; do
    dl_current=$((dl_current + 1))
    echo "  [${dl_current}/${artifact_count}] ${art_name} (run ${run_id})..."
    zipfile="${tmpdir}/${art_id}.zip"
    if gh api "repos/${REPO}/actions/artifacts/${art_id}/zip" > "$zipfile" 2>/dev/null; then
      unzip -qo "$zipfile" -d "$tmpdir/extract" 2>/dev/null
      for f in "$tmpdir/extract"/*.xml; do
        [[ -f "$f" ]] || continue
        base=$(basename "$f" .xml)
        mv "$f" "${OUTPUT_DIR}/${run_id}-${base}.xml"
      done
      rm -rf "$tmpdir/extract" "$zipfile"
    else
      echo "    FAILED to download artifact ${art_id}"
      rm -f "$zipfile"
    fi
  done

total_files=$(find "$OUTPUT_DIR" -name '*.xml' 2>/dev/null | wc -l | tr -d ' ')
echo ""
echo "Done. ${total_files} XML files saved to ${OUTPUT_DIR}/"

echo ""
echo "Normalizing classnames (stripping Ginkgo random suffixes)..."
for f in "${OUTPUT_DIR}"/*.xml; do
  [[ -f "$f" ]] || continue
  sed -E 's/classname="Running (.+)-[a-z0-9]{3}"/classname="\1"/g' "$f" > "${f}.tmp" && mv "${f}.tmp" "$f"
done
echo "Done."

if [[ "$SKIP_ANALYSIS" == "true" ]]; then
  exit 0
fi

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PYTHON=""
if [[ -x "${SCRIPT_DIR}/.venv/bin/python" ]]; then
  PYTHON="${SCRIPT_DIR}/.venv/bin/python"
elif command -v python3 &>/dev/null; then
  PYTHON="python3"
else
  echo ""
  echo "Python not found. Install with: cd tools && poetry install"
  exit 0
fi

if ! "$PYTHON" -c "from flaky_tests_detection.check_flakes import main" &>/dev/null; then
  echo ""
  echo "flaky-tests-detection not installed. Install with: cd tools && poetry install"
  exit 0
fi

echo ""
echo "================================================================"
echo "Running flaky test detection..."
echo "  Window size:  ${WINDOW_SIZE} days"
echo "  Window count: ${WINDOW_COUNT}"
echo "  Top N:        ${TOP_N}"
echo "================================================================"
echo ""

RESULTS_FILE="${RESULTS_FILE:-flaky-results.txt}"

PYTHONPATH="${SCRIPT_DIR}:${PYTHONPATH:-}" "$PYTHON" -c \
  "import importlib; importlib.import_module('flaky-test-analysis-mpl-config'); from flaky_tests_detection.check_flakes import main; main()" \
  --junit-files="${OUTPUT_DIR}" \
  --grouping-option=days \
  --window-size="${WINDOW_SIZE}" \
  --window-count="${WINDOW_COUNT}" \
  --top-n="${TOP_N}" \
  --heatmap \
  2>&1 | tee "$RESULTS_FILE"
