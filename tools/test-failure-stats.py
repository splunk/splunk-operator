#!/usr/bin/env python3
"""
Analyze JUnit XML reports and produce a Markdown report of test failure statistics.

Parses all JUnit XML files (downloaded by flaky-test-analysis.sh) and writes
a Markdown file with per-test-case failure counts and rates.

Usage:
    ./tools/test-failure-stats.py [junit-dir]
    ./tools/test-failure-stats.py > report.md

    junit-dir   Directory with JUnit XML files (default: ./junit-reports)

Requires: Python 3.8+ (stdlib only).
"""

import re
import sys
import xml.etree.ElementTree as ET
from collections import defaultdict
from dataclasses import dataclass, field
from pathlib import Path


FILENAME_RE = re.compile(
    r"^(\d+)-(?:report-junit|unit_test)-(\d{8})-(\d{6})-\d+(?:-(.+))?\.xml$"
)

INFRA_NAMES = {
    "[BeforeSuite]", "[AfterSuite]", "[ReportAfterSuite]",
    "[SynchronizedBeforeSuite]", "[SynchronizedAfterSuite]",
}


@dataclass
class TestRecord:
    runs: int = 0
    failures: int = 0
    timeouts: int = 0
    passes: int = 0
    failure_dates: list = field(default_factory=list)


def parse_filename(fname: str):
    m = FILENAME_RE.match(fname)
    if not m:
        return None, None
    date_str = m.group(2)
    return f"{date_str[:4]}-{date_str[4:6]}-{date_str[6:]}", m.group(4) or "unit_test"


def parse_junit_file(filepath: Path):
    try:
        tree = ET.parse(str(filepath))
    except ET.ParseError:
        print(f"  WARNING: Could not parse {filepath.name}, skipping", file=sys.stderr)
        return

    for tc in tree.iter("testcase"):
        name = tc.get("name", "")
        if name in INFRA_NAMES:
            continue
        if tc.get("status") == "skipped" or tc.find("skipped") is not None:
            continue

        classname = tc.get("classname", "")
        status = tc.get("status", "")
        has_failure = tc.find("failure") is not None
        yield classname, name, status, has_failure


def build_stats(junit_dir: Path):
    stats: dict[str, TestRecord] = defaultdict(TestRecord)
    files_parsed = 0

    for fpath in sorted(junit_dir.glob("*.xml")):
        date_str, _ = parse_filename(fpath.name)
        if date_str is None:
            continue

        files_parsed += 1
        for classname, name, status, has_failure in parse_junit_file(fpath):
            rec = stats[f"{classname}::{name}"]
            rec.runs += 1
            if has_failure:
                rec.failures += 1
                if status == "timedout":
                    rec.timeouts += 1
                rec.failure_dates.append(date_str)
            else:
                rec.passes += 1

    return stats, files_parsed


def short_name(full_name: str, max_len: int = 120) -> str:
    name = re.sub(r"^\[It\]\s*", "", full_name)
    if len(name) > max_len:
        return name[: max_len - 3] + "..."
    return name


def write_markdown(stats: dict[str, TestRecord], files_parsed: int):
    failing = {k: v for k, v in stats.items() if v.failures > 0}
    total_runs = sum(r.runs for r in stats.values())

    print("# Test Failure Statistics")
    print()
    print("| Metric | Value |")
    print("|--------|-------|")
    print(f"| Files parsed | {files_parsed} |")
    print(f"| Unique tests | {len(stats)} |")
    print(f"| Tests with failures | {len(failing)} |")
    print(f"| Total test runs (non-skipped) | {total_runs} |")
    print(f"| Total failure occurrences | {sum(r.failures for r in failing.values())} |")
    print()

    if not failing:
        print("**No test failures found.**")
        return

    ranked = sorted(failing.items(),
                    key=lambda x: (-x[1].failures, -x[1].failures / max(x[1].runs, 1)))

    print("## Failing Tests")
    print()
    print("| # | Fail | Runs | Rate | Timeouts | Last Failure | Suite | Test |")
    print("|--:|-----:|-----:|-----:|---------:|:------------:|:------|:-----|")

    for i, (key, rec) in enumerate(ranked, 1):
        classname, name = key.split("::", 1)
        rate = rec.failures / rec.runs * 100 if rec.runs > 0 else 0
        last_fail = max(rec.failure_dates) if rec.failure_dates else "n/a"
        display = short_name(name).replace("|", "\\|")
        print(
            f"| {i} | {rec.failures} | {rec.runs} | {rate:.1f}% "
            f"| {rec.timeouts} | {last_fail} | `{classname}` | {display} |"
        )


def main():
    junit_dir = Path(sys.argv[1]) if len(sys.argv) > 1 else Path("./junit-reports")

    if not junit_dir.is_dir():
        print(f"ERROR: {junit_dir} is not a directory", file=sys.stderr)
        sys.exit(1)

    stats, files_parsed = build_stats(junit_dir)
    write_markdown(stats, files_parsed)


if __name__ == "__main__":
    main()
