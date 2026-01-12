#!/usr/bin/env python3
"""
Check that all actions/checkout usages have 'with.ref' specified.

This ensures consistent and explicit checkout behavior across all workflows.
"""

import sys
from pathlib import Path

import yaml


def check_workflow_file(filepath: Path) -> list[dict]:
    """
    Check a workflow file for actions/checkout usages without 'with.ref'.

    Returns a list of violations.
    """
    violations = []

    with open(filepath, "r") as f:
        try:
            data = yaml.safe_load(f)
        except yaml.YAMLError as e:
            print(f"Warning: Failed to parse {filepath}: {e}")
            return []

    if not data or "jobs" not in data:
        return []

    for job_name, job in data["jobs"].items():
        steps = job.get("steps", [])
        for i, step in enumerate(steps):
            uses = step.get("uses", "")
            if "actions/checkout" in uses:
                with_block = step.get("with", {})
                has_ref = isinstance(with_block, dict) and "ref" in with_block

                if not has_ref:
                    violations.append({
                        "file": str(filepath),
                        "job": job_name,
                        "step": i,
                        "uses": uses,
                    })

    return violations


def main():
    workflows_dir = Path(".github/workflows")

    if not workflows_dir.exists():
        print("Error: .github/workflows directory not found")
        sys.exit(1)

    all_violations = []

    for pattern in ("*.yml", "*.yaml"):
        for workflow_file in sorted(workflows_dir.glob(pattern)):
            all_violations.extend(check_workflow_file(workflow_file))

    if all_violations:
        print("❌ Found actions/checkout usages without 'with.ref' specified:\n")
        for v in all_violations:
            print(f"  {v['file']}")
            print(f"    Job: {v['job']}, Step: {v['step']}")
            print(f"    Uses: {v['uses']}\n")
        print(f"Total violations: {len(all_violations)}")
        print("\nAll actions/checkout steps should specify 'with.ref' to ensure")
        print("consistent and explicit checkout behavior.")
        print("\nSee .github/README.md for security requirements and examples.")
        sys.exit(1)
    else:
        print("✅ All actions/checkout usages have 'with.ref' specified")
        sys.exit(0)


if __name__ == "__main__":
    main()
