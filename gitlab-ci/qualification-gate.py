#!/usr/bin/env python3
from __future__ import annotations

import json
from pathlib import Path


EXPECTED_DISPOSITION = "qualified with current SOK"


def main() -> int:
    project_dir = Path.cwd()
    record_path = project_dir / "ci-output" / "release-controller" / "compatibility-record.json"
    if not record_path.exists():
        raise RuntimeError(f"Missing qualification record: {record_path}")

    record = json.loads(record_path.read_text(encoding="utf-8"))
    disposition = record.get("disposition", "")
    reason = record.get("disposition_reason", "")

    print(f"qualification disposition: {disposition}")
    print(f"qualification reason: {reason}")

    if disposition != EXPECTED_DISPOSITION:
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
