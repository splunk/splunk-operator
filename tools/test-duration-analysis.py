#!/usr/bin/env python3
"""
Parse JUnit XML test reports and summarize test case durations.

Produces per-test-case and per-suite statistics (count, min, max, mean,
median, p95) so that realistic timeouts can be set at each level:
  - Test Case
  - Test Suite
  - Test Run

Usage:
    python3 tools/test-duration-analysis.py [OPTIONS]

Options:
    --junit-dir DIR      Directory containing JUnit XML files (default: ./junit-reports)
    --csv FILE           Write per-test-case CSV to FILE
    --suite-csv FILE     Write per-suite CSV to FILE
    --run-csv FILE       Write per-run CSV to FILE
    --multiplier N       Timeout multiplier applied to p95 (default: 2.0)
    --min-samples N      Minimum observations to include a test (default: 1)
    --top N              Show only top N slowest tests (default: all)
    --format FMT         Output format: table (default), csv, json
    --sort-by FIELD      Sort by: max, p95, mean, median, count (default: p95)
    --max-duration SECS  Exclude observations longer than SECS (filters CI timeout hits)
    --exclude-timeout    Auto-detect and exclude CI timeout-ceiling observations
    --only-passed        Only include test cases with status="passed" (exclude failed/error)

Requires only the Python standard library (xml.etree, statistics, csv, json).
"""

import argparse
import csv
import json
import statistics
import sys
import xml.etree.ElementTree as ET
from collections import defaultdict
from pathlib import Path


def parse_junit_xmls(junit_dir):
    """Yield (run_id, suite_name, test_name, classname, status, duration_s) from all XMLs."""
    xml_dir = Path(junit_dir)
    if not xml_dir.is_dir():
        print(f"ERROR: {junit_dir} is not a directory", file=sys.stderr)
        sys.exit(1)

    for xml_file in sorted(xml_dir.glob("*.xml")):
        # Extract run_id from filename pattern: <run_id>-report-junit-...xml
        run_id = xml_file.name.split("-")[0]

        try:
            tree = ET.parse(xml_file)
        except ET.ParseError as e:
            print(f"WARNING: skipping {xml_file.name}: {e}", file=sys.stderr)
            continue

        root = tree.getroot()

        # Handle both <testsuites><testsuite>... and bare <testsuite>...
        if root.tag == "testsuites":
            suites = root.findall("testsuite")
        elif root.tag == "testsuite":
            suites = [root]
        else:
            continue

        for suite in suites:
            suite_name = suite.get("name", "unknown")
            for tc in suite.findall("testcase"):
                name = tc.get("name", "")
                classname = tc.get("classname", "")
                status = tc.get("status", "")
                time_str = tc.get("time", "0")
                try:
                    duration = float(time_str)
                except ValueError:
                    duration = 0.0
                yield run_id, suite_name, name, classname, status, duration


def compute_stats(durations):
    """Return a dict of statistics for a list of durations."""
    n = len(durations)
    if n == 0:
        return None
    s = sorted(durations)
    result = {
        "count": n,
        "min_s": s[0],
        "max_s": s[-1],
        "mean_s": statistics.mean(s),
        "median_s": statistics.median(s),
        "stdev_s": statistics.stdev(s) if n > 1 else 0.0,
    }
    # p95
    idx = int(0.95 * (n - 1))
    result["p95_s"] = s[idx]
    return result


def fmt_duration(seconds):
    """Format seconds as HH:MM:SS."""
    h = int(seconds) // 3600
    m = (int(seconds) % 3600) // 60
    s = int(seconds) % 60
    if h > 0:
        return f"{h}h{m:02d}m{s:02d}s"
    if m > 0:
        return f"{m}m{s:02d}s"
    return f"{s}s"


def _detect_timeout_ceiling(all_durations, test_name_durations=None):
    """Auto-detect a CI timeout ceiling from duration data.

    When a CI job timeout kills a run, every still-running test gets the same
    wall-clock duration.  This means many *different* test cases will share
    a nearly identical max duration — a pattern that never occurs naturally.

    Strategy: bucket the per-test-case max durations into 5-minute bins and
    look for a bin in the top 25% of the range that contains >= 3 different
    test cases.  Falls back to raw-observation histogram if per-test data
    is unavailable.

    Returns the lower bound of the detected ceiling (seconds), or 0 if none found.
    """
    # ── Per-test-case max approach (preferred) ────────────────────
    if test_name_durations and len(test_name_durations) >= 5:
        max_per_test = [max(durs) for durs in test_name_durations.values() if durs]
        if len(max_per_test) >= 5:
            bucket_width = 300  # 5-minute bins
            buckets = {}
            for d in max_per_test:
                key = int(d / bucket_width) * bucket_width
                buckets[key] = buckets.get(key, 0) + 1

            sorted_keys = sorted(buckets.keys())
            if sorted_keys:
                dur_range = sorted_keys[-1] - sorted_keys[0]
                threshold = sorted_keys[0] + dur_range * 0.75 if dur_range > 0 else sorted_keys[-1]

                for key in reversed(sorted_keys):
                    if key < threshold:
                        break
                    # 3+ different tests sharing the same 5-min max-duration bucket = timeout
                    if buckets[key] >= 3:
                        return key * 0.99

    # ── Fallback: raw observation histogram ───────────────────────
    if len(all_durations) < 20:
        return 0

    bucket_width = 300
    buckets = {}
    for d in all_durations:
        key = int(d / bucket_width) * bucket_width
        buckets[key] = buckets.get(key, 0) + 1

    sorted_keys = sorted(buckets.keys())
    if not sorted_keys:
        return 0

    counts = [buckets[k] for k in sorted_keys]
    median_count = sorted(counts)[len(counts) // 2]
    dur_range = sorted_keys[-1] - sorted_keys[0]
    threshold = sorted_keys[0] + dur_range * 0.75 if dur_range > 0 else sorted_keys[-1]
    spike_threshold = max(median_count * 3, 5)

    for key in reversed(sorted_keys):
        if key < threshold:
            break
        if buckets[key] >= spike_threshold:
            return key * 0.99

    return 0


def normalize_test_name(name):
    """Strip Ginkgo [It] prefix and clean up whitespace for grouping."""
    n = name.strip()
    if n.startswith("[It] "):
        n = n[5:]
    # Collapse whitespace (XML sometimes has newlines in attributes)
    return " ".join(n.split())


def main():
    parser = argparse.ArgumentParser(description="Summarize test case durations from JUnit XMLs")
    parser.add_argument("--junit-dir", default="./junit-reports", help="JUnit XML directory")
    parser.add_argument("--csv", dest="csv_file", default=None, help="Write per-test CSV")
    parser.add_argument("--suite-csv", default=None, help="Write per-suite CSV")
    parser.add_argument("--run-csv", default=None, help="Write per-run CSV")
    parser.add_argument("--multiplier", type=float, default=2.0, help="Timeout multiplier on p95")
    parser.add_argument("--min-samples", type=int, default=1, help="Minimum observations")
    parser.add_argument("--top", type=int, default=0, help="Show top N slowest (0=all)")
    parser.add_argument("--format", choices=["table", "csv", "json"], default="table")
    parser.add_argument("--sort-by", choices=["max", "p95", "mean", "median", "count"], default="p95")
    parser.add_argument("--max-duration", type=float, default=0,
                        help="Exclude observations longer than this many seconds")
    parser.add_argument("--exclude-timeout", action="store_true",
                        help="Auto-detect and exclude CI timeout-ceiling observations")
    parser.add_argument("--only-passed", action="store_true",
                        help="Only include test cases with status='passed'")
    args = parser.parse_args()

    # ── Collect data ──────────────────────────────────────────────
    test_durations = defaultdict(list)    # test_name -> [durations]
    suite_durations = defaultdict(list)   # suite_name -> [total suite durations]
    run_durations = defaultdict(float)    # run_id -> total seconds
    test_classnames = {}                  # test_name -> classname

    # Track per-suite totals grouped by (run_id, suite_name)
    suite_run_totals = defaultdict(float)

    skipped = 0
    filtered = 0
    total = 0
    all_durations = []  # for auto-detecting timeout ceiling

    # First pass: collect all durations (needed for --exclude-timeout auto-detection)
    raw_records = []
    pre_filter_test_durs = defaultdict(list)  # for auto-detection
    status_filtered = 0
    for run_id, suite_name, name, classname, status, duration in parse_junit_xmls(args.junit_dir):
        total += 1
        if status == "skipped" or duration == 0.0:
            skipped += 1
            continue
        if args.only_passed and status != "passed":
            status_filtered += 1
            continue
        raw_records.append((run_id, suite_name, name, classname, duration))
        all_durations.append(duration)
        pre_filter_test_durs[normalize_test_name(name)].append(duration)

    # Determine max-duration cutoff
    max_dur = args.max_duration
    if args.exclude_timeout and not max_dur and all_durations:
        max_dur = _detect_timeout_ceiling(all_durations, pre_filter_test_durs)
        if max_dur:
            print(f"Auto-detected CI timeout ceiling: {fmt_duration(max_dur)}")
            print(f"Excluding observations >= {fmt_duration(max_dur)}")
        else:
            print("No timeout ceiling detected; including all observations.")

    # Second pass: apply filter and build aggregates
    for run_id, suite_name, name, classname, duration in raw_records:
        if max_dur and duration >= max_dur:
            filtered += 1
            continue
        norm_name = normalize_test_name(name)
        test_durations[norm_name].append(duration)
        test_classnames[norm_name] = classname
        run_durations[run_id] += duration
        suite_run_totals[(run_id, classname)] += duration

    # Aggregate suite durations per run
    for (run_id, suite_name), total_dur in suite_run_totals.items():
        suite_durations[suite_name].append(total_dur)

    status_msg = f", {status_filtered} non-passed" if status_filtered else ""
    print(f"Parsed {total} test cases ({skipped} skipped{status_msg}, {filtered} filtered as timeout-hits) from {args.junit_dir}")
    print(f"Unique test cases with results: {len(test_durations)}")
    print(f"Unique suites: {len(suite_durations)}")
    print(f"Unique runs: {len(run_durations)}")
    print()

    # ── Per-test-case stats ───────────────────────────────────────
    test_stats = []
    for name, durs in test_durations.items():
        if len(durs) < args.min_samples:
            continue
        s = compute_stats(durs)
        s["name"] = name
        s["classname"] = test_classnames.get(name, "")
        s["suggested_timeout_s"] = round(s["p95_s"] * args.multiplier, 1)
        test_stats.append(s)

    sort_key = {"max": "max_s", "p95": "p95_s", "mean": "mean_s", "median": "median_s", "count": "count"}
    test_stats.sort(key=lambda x: x[sort_key[args.sort_by]], reverse=True)

    if args.top > 0:
        test_stats = test_stats[: args.top]

    # ── Per-suite stats ───────────────────────────────────────────
    suite_stats = []
    for sname, durs in suite_durations.items():
        s = compute_stats(durs)
        if s:
            s["suite"] = sname
            s["suggested_timeout_s"] = round(s["p95_s"] * args.multiplier, 1)
            suite_stats.append(s)
    suite_stats.sort(key=lambda x: x["p95_s"], reverse=True)

    # ── Per-run stats ─────────────────────────────────────────────
    run_durs = list(run_durations.values())
    run_stats_summary = compute_stats(run_durs) if run_durs else None

    # ── Output ────────────────────────────────────────────────────
    if args.format == "json":
        output = {
            "test_cases": test_stats,
            "suites": suite_stats,
            "run_summary": run_stats_summary,
        }
        print(json.dumps(output, indent=2))
    elif args.format == "csv":
        _write_test_csv(sys.stdout, test_stats)
    else:
        _print_test_table(test_stats, args.multiplier)
        print()
        _print_suite_table(suite_stats, args.multiplier)
        print()
        _print_run_summary(run_stats_summary, args.multiplier)

    # ── Optional CSV files ────────────────────────────────────────
    if args.csv_file:
        with open(args.csv_file, "w", newline="", encoding="utf-8") as f:
            _write_test_csv(f, test_stats)
        print(f"\nPer-test CSV written to {args.csv_file}")

    if args.suite_csv:
        with open(args.suite_csv, "w", newline="", encoding="utf-8") as f:
            _write_suite_csv(f, suite_stats)
        print(f"Per-suite CSV written to {args.suite_csv}")

    if args.run_csv:
        with open(args.run_csv, "w", newline="", encoding="utf-8") as f:
            w = csv.writer(f)
            w.writerow(["run_id", "total_duration_s", "total_duration_human"])
            for rid in sorted(run_durations, key=run_durations.get, reverse=True):
                d = run_durations[rid]
                w.writerow([rid, round(d, 1), fmt_duration(d)])
        print(f"Per-run CSV written to {args.run_csv}")


def _print_test_table(test_stats, multiplier):
    if not test_stats:
        print("No test case data to display.")
        return

    print("=" * 120)
    print("TEST CASE DURATION SUMMARY")
    print(f"(suggested timeout = p95 x {multiplier})")
    print("=" * 120)
    hdr = f"{'#':>3}  {'Runs':>4}  {'Min':>10}  {'Median':>10}  {'Mean':>10}  {'P95':>10}  {'Max':>10}  {'Timeout':>10}  Test Name"
    print(hdr)
    print("-" * 120)
    for i, s in enumerate(test_stats, 1):
        short_name = s["name"][:80] + ("..." if len(s["name"]) > 80 else "")
        print(
            f"{i:>3}  {s['count']:>4}  {fmt_duration(s['min_s']):>10}  "
            f"{fmt_duration(s['median_s']):>10}  {fmt_duration(s['mean_s']):>10}  "
            f"{fmt_duration(s['p95_s']):>10}  {fmt_duration(s['max_s']):>10}  "
            f"{fmt_duration(s['suggested_timeout_s']):>10}  {short_name}"
        )
    print("-" * 120)


def _print_suite_table(suite_stats, multiplier):
    if not suite_stats:
        print("No suite data to display.")
        return

    print("=" * 110)
    print("SUITE DURATION SUMMARY (total wall-clock per suite per run)")
    print(f"(suggested timeout = p95 x {multiplier})")
    print("=" * 110)
    hdr = f"{'#':>3}  {'Runs':>4}  {'Min':>10}  {'Median':>10}  {'Mean':>10}  {'P95':>10}  {'Max':>10}  {'Timeout':>10}  Suite"
    print(hdr)
    print("-" * 110)
    for i, s in enumerate(suite_stats, 1):
        print(
            f"{i:>3}  {s['count']:>4}  {fmt_duration(s['min_s']):>10}  "
            f"{fmt_duration(s['median_s']):>10}  {fmt_duration(s['mean_s']):>10}  "
            f"{fmt_duration(s['p95_s']):>10}  {fmt_duration(s['max_s']):>10}  "
            f"{fmt_duration(s['suggested_timeout_s']):>10}  {s['suite']}"
        )
    print("-" * 110)


def _print_run_summary(run_stats, multiplier):
    if not run_stats:
        print("No run data to display.")
        return

    print("=" * 60)
    print("TEST RUN DURATION SUMMARY (aggregate per workflow run)")
    print(f"(suggested timeout = p95 x {multiplier})")
    print("=" * 60)
    print(f"  Total runs:          {run_stats['count']}")
    print(f"  Min:                 {fmt_duration(run_stats['min_s'])}")
    print(f"  Median:              {fmt_duration(run_stats['median_s'])}")
    print(f"  Mean:                {fmt_duration(run_stats['mean_s'])}")
    print(f"  P95:                 {fmt_duration(run_stats['p95_s'])}")
    print(f"  Max:                 {fmt_duration(run_stats['max_s'])}")
    timeout = run_stats["p95_s"] * multiplier
    print(f"  Suggested timeout:   {fmt_duration(timeout)}")
    print("=" * 60)


def _write_test_csv(f, test_stats):
    w = csv.writer(f)
    w.writerow([
        "classname", "test_name", "count", "min_s", "median_s", "mean_s",
        "p95_s", "max_s", "stdev_s", "suggested_timeout_s",
    ])
    for s in test_stats:
        w.writerow([
            s["classname"], s["name"], s["count"],
            round(s["min_s"], 1), round(s["median_s"], 1), round(s["mean_s"], 1),
            round(s["p95_s"], 1), round(s["max_s"], 1), round(s["stdev_s"], 1),
            s["suggested_timeout_s"],
        ])


def _write_suite_csv(f, suite_stats):
    w = csv.writer(f)
    w.writerow([
        "suite", "count", "min_s", "median_s", "mean_s",
        "p95_s", "max_s", "stdev_s", "suggested_timeout_s",
    ])
    for s in suite_stats:
        w.writerow([
            s["suite"], s["count"],
            round(s["min_s"], 1), round(s["median_s"], 1), round(s["mean_s"], 1),
            round(s["p95_s"], 1), round(s["max_s"], 1), round(s["stdev_s"], 1),
            s["suggested_timeout_s"],
        ])


if __name__ == "__main__":
    main()
