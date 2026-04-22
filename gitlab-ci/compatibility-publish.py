#!/usr/bin/env python3
from __future__ import annotations

import json
import os
from pathlib import Path


def main() -> int:
    output_dir = Path.cwd() / "ci-output" / "release-controller"
    output_dir.mkdir(parents=True, exist_ok=True)

    compatibility = json.loads((output_dir / "compatibility-record.json").read_text(encoding="utf-8"))
    confluence_target = os.environ.get("SOK_COMPATIBILITY_CONFLUENCE_TARGET", "decision-records/migration")
    slack_target = os.environ.get("SOK_COMPATIBILITY_SLACK_TARGET", "sok-release-qualification")
    status_record_target = os.environ.get("SOK_COMPATIBILITY_STATUS_RECORD", "compatibility-matrix")

    publish_plan = {
        "schema_version": "v1alpha1",
        "disposition": compatibility["disposition"],
        "disposition_reason": compatibility["disposition_reason"],
        "targets": {
            "confluence": confluence_target,
            "slack": slack_target,
            "status_record": status_record_target,
        },
        "next_action": "publish-qualification-compatibility-update",
    }

    (output_dir / "compatibility-publish-plan.json").write_text(
        json.dumps(publish_plan, indent=2, sort_keys=True) + "\n",
        encoding="utf-8",
    )
    (output_dir / "compatibility-publish-plan.md").write_text(
        "\n".join(
            [
                "# Compatibility Publish Plan",
                "",
                f"- disposition: {publish_plan['disposition']}",
                f"- disposition_reason: {publish_plan['disposition_reason']}",
                f"- confluence_target: {confluence_target}",
                f"- slack_target: {slack_target}",
                f"- status_record_target: {status_record_target}",
                f"- next_action: {publish_plan['next_action']}",
            ]
        )
        + "\n",
        encoding="utf-8",
    )
    print(output_dir / "compatibility-publish-plan.md")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
