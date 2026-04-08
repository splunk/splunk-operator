#!/usr/bin/env bash
#
# Analyze test case durations from JUnit XML reports.
#
# This script can either:
#   1. Use already-downloaded JUnit XMLs (from flaky-test-analysis.sh --skip-analysis)
#   2. Download fresh XMLs from GitHub Actions and then analyze them
#
# Usage:
#   ./tools/test-duration-analysis.sh [OPTIONS]
#
# Options:
#   --junit-dir DIR        Directory with JUnit XMLs (default: ./junit-reports)
#   --download             Download XMLs first using flaky-test-analysis.sh
#   --start-date DATE      Start date for download (default: 7 days ago)
#   --end-date DATE        End date for download (default: yesterday)
#   --branch NAME          Filter by branch when downloading
#   --csv FILE             Write per-test-case CSV
#   --suite-csv FILE       Write per-suite CSV
#   --run-csv FILE         Write per-run CSV
#   --multiplier N         Timeout multiplier on p95 (default: 2.0)
#   --top N                Show top N slowest tests (default: all)
#   --sort-by FIELD        Sort by: max, p95, mean, median, count (default: p95)
#   --format FMT           Output: table, csv, json (default: table)
#   --min-samples N        Minimum observations to include (default: 1)
#   --exclude-timeout      Auto-detect and exclude CI timeout-ceiling observations
#   --max-duration SECS    Exclude observations longer than SECS seconds
#
# Examples:
#   # Analyze existing XMLs
#   ./tools/test-duration-analysis.sh
#
#   # Download fresh data and analyze
#   ./tools/test-duration-analysis.sh --download --branch develop
#
#   # Top 20 slowest, output as CSV
#   ./tools/test-duration-analysis.sh --top 20 --format csv
#
#   # Export all three levels to CSV files
#   ./tools/test-duration-analysis.sh --csv test-durations.csv --suite-csv suite-durations.csv --run-csv run-durations.csv
#
# Requires: python3 (standard library only)
# Optional: gh (GitHub CLI) if using --download
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

JUNIT_DIR="${JUNIT_DIR:-./junit-reports}"
DOWNLOAD=false
START_DATE=""
END_DATE=""
BRANCH=""

# Passthrough args for the Python script
PYTHON_ARGS=()

# Parse arguments
while [[ $# -gt 0 ]]; do
  case "$1" in
    --junit-dir)
      JUNIT_DIR="$2"; shift 2 ;;
    --junit-dir=*)
      JUNIT_DIR="${1#--junit-dir=}"; shift ;;
    --download)
      DOWNLOAD=true; shift ;;
    --start-date)
      START_DATE="$2"; shift 2 ;;
    --start-date=*)
      START_DATE="${1#--start-date=}"; shift ;;
    --end-date)
      END_DATE="$2"; shift 2 ;;
    --end-date=*)
      END_DATE="${1#--end-date=}"; shift ;;
    --branch)
      BRANCH="$2"; shift 2 ;;
    --branch=*)
      BRANCH="${1#--branch=}"; shift ;;
    -h|--help)
      sed -n '2,46p' "$0" | sed 's/^# \?//'
      exit 0 ;;
    *)
      PYTHON_ARGS+=("$1"); shift ;;
  esac
done

# ── Download phase (optional) ─────────────────────────────────────
if [[ "$DOWNLOAD" == "true" ]]; then
  echo "Downloading JUnit XMLs via flaky-test-analysis.sh..."
  dl_args=(--skip-analysis)
  [[ -n "$START_DATE" ]] && dl_args+=("$START_DATE")
  [[ -n "$END_DATE" ]] && dl_args+=("$END_DATE")
  [[ -n "$BRANCH" ]] && dl_args+=(--branch "$BRANCH")

  OUTPUT_DIR="$JUNIT_DIR" bash "$SCRIPT_DIR/flaky-test-analysis.sh" "${dl_args[@]}"
  echo ""
fi

# ── Verify XMLs exist ─────────────────────────────────────────────
xml_count=$(find "$JUNIT_DIR" -name '*.xml' 2>/dev/null | wc -l | tr -d ' ')
if [[ "$xml_count" -eq 0 ]]; then
  echo "ERROR: No XML files found in $JUNIT_DIR" >&2
  echo "  Run with --download to fetch from GitHub Actions, or" >&2
  echo "  run flaky-test-analysis.sh --skip-analysis first." >&2
  exit 1
fi
echo "Found $xml_count JUnit XML files in $JUNIT_DIR"
echo ""

# ── Find Python ───────────────────────────────────────────────────
PYTHON=""
if [[ -x "${SCRIPT_DIR}/.venv/bin/python" ]]; then
  PYTHON="${SCRIPT_DIR}/.venv/bin/python"
elif command -v python3 &>/dev/null; then
  PYTHON="python3"
else
  echo "ERROR: python3 not found." >&2
  exit 1
fi

# ── Run analysis ──────────────────────────────────────────────────
exec "$PYTHON" "$SCRIPT_DIR/test-duration-analysis.py" \
  --junit-dir "$JUNIT_DIR" \
  "${PYTHON_ARGS[@]}"
