#!/usr/bin/env python3
from __future__ import annotations

import json
import os
import sys
import urllib.error
import urllib.request
from pathlib import Path


def api_headers() -> dict[str, str]:
    private_token = os.getenv("PIPELINE_GITLAB_RELEASE_API_TOKEN", "").strip()
    if private_token:
        return {"PRIVATE-TOKEN": private_token}
    job_token = os.getenv("CI_JOB_TOKEN", "").strip()
    if job_token:
        return {"JOB-TOKEN": job_token}
    raise RuntimeError("Missing PIPELINE_GITLAB_RELEASE_API_TOKEN or CI_JOB_TOKEN for GitLab API access")


def api_get_json(url: str) -> list[dict]:
    request = urllib.request.Request(url, headers=api_headers())
    with urllib.request.urlopen(request) as response:
        return json.load(response)


def main() -> int:
    project_dir = Path.cwd()
    output_dir = project_dir / "ci-output" / "release-controller"
    output_dir.mkdir(parents=True, exist_ok=True)

    api_url = os.getenv("CI_API_V4_URL", "https://cd.splunkdev.com/api/v4")
    project_id = os.environ["CI_PROJECT_ID"]
    pipeline_id = os.environ["CI_PIPELINE_ID"]
    dispatch_job_name = os.getenv("PSR_DISPATCH_JOB_NAME", "psr-release-qualification-dispatch")

    bridges = api_get_json(f"{api_url}/projects/{project_id}/pipelines/{pipeline_id}/bridges?per_page=100")
    bridge = next((item for item in bridges if item.get("name") == dispatch_job_name), None)
    verdict = {
        "schema_version": "v1alpha1",
        "bridge_job_name": dispatch_job_name,
        "bridge_job_id": "",
        "bridge_status": "not-requested",
        "bridge_job_url": "",
        "downstream_project_id": "",
        "downstream_pipeline_id": "",
        "downstream_pipeline_status": "not-created",
        "downstream_pipeline_url": "",
        "target_version": os.getenv("SOK_PSR_TARGET_VERSION", ""),
        "base_version": os.getenv("SOK_PSR_BASE_VERSION", ""),
        "test_type": os.getenv("SOK_PSR_TRIGGER_TEST_TYPE", ""),
        "enterprise_image": os.getenv("SOK_ENTERPRISE_IMAGE", ""),
        "project_path": os.getenv("SOK_PSR_PROJECT_PATH", ""),
    }
    if bridge is None:
        verdict["verdict"] = "skipped"
    else:
        downstream = bridge.get("downstream_pipeline") or {}
        verdict.update(
            {
                "bridge_job_id": bridge.get("id"),
                "bridge_status": bridge.get("status", "unknown"),
                "bridge_job_url": bridge.get("web_url", ""),
                "downstream_project_id": downstream.get("project_id"),
                "downstream_pipeline_id": downstream.get("id"),
                "downstream_pipeline_status": downstream.get("status", "not-created"),
                "downstream_pipeline_url": downstream.get("web_url", ""),
            }
        )
        verdict["verdict"] = (
            "passed"
            if verdict["bridge_status"] == "success" and verdict["downstream_pipeline_status"] == "success"
            else "pending"
            if verdict["bridge_status"] in {"running", "pending"}
            or verdict["downstream_pipeline_status"] in {"running", "pending"}
            else "failed"
        )

    json_path = output_dir / "psr-qualification-verdict.json"
    md_path = output_dir / "psr-qualification-verdict.md"
    json_path.write_text(json.dumps(verdict, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    md_path.write_text(
        "\n".join(
            [
                "# PSR Qualification Verdict",
                "",
                f"- verdict: {verdict['verdict']}",
                f"- project_path: {verdict['project_path'] or 'unset'}",
                f"- bridge_status: {verdict['bridge_status']}",
                f"- bridge_job_url: {verdict['bridge_job_url'] or 'unavailable'}",
                f"- downstream_pipeline_status: {verdict['downstream_pipeline_status']}",
                f"- downstream_pipeline_url: {verdict['downstream_pipeline_url'] or 'unavailable'}",
                f"- target_version: {verdict['target_version'] or 'unset'}",
                f"- base_version: {verdict['base_version'] or 'unset'}",
                f"- test_type: {verdict['test_type'] or 'unset'}",
                f"- enterprise_image: {verdict['enterprise_image'] or 'unset'}",
            ]
        )
        + "\n",
        encoding="utf-8",
    )
    print(md_path)
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (RuntimeError, urllib.error.URLError) as exc:
        print(str(exc), file=sys.stderr)
        raise SystemExit(1)
