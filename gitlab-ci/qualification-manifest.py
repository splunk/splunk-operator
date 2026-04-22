#!/usr/bin/env python3
from __future__ import annotations

import json
import os
from datetime import datetime, timezone
from pathlib import Path


def utc_now() -> str:
    return datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def read_makefile_version(project_dir: Path) -> str:
    makefile = project_dir / "Makefile"
    for raw_line in makefile.read_text(encoding="utf-8").splitlines():
        line = raw_line.strip()
        if line.startswith("VERSION") and "?=" in line:
            return line.split("?=", 1)[1].strip()
    raise RuntimeError("Unable to resolve VERSION from Makefile")


def main() -> int:
    project_dir = Path.cwd()
    output_dir = project_dir / "ci-output" / "release-controller"
    output_dir.mkdir(parents=True, exist_ok=True)

    qualification_profile = os.environ.get("PIPELINE_QUALIFICATION_PROFILE", "monthly")
    helm_profile = os.environ.get("PIPELINE_HELM_TEST_PROFILE") or os.environ.get("JOB_HELM_TEST_PROFILE") or "full"
    enterprise_image = (
        os.environ.get("PIPELINE_SPLUNK_ENTERPRISE_IMAGE")
        or os.environ.get("SPLUNK_ENTERPRISE_RELEASE_IMAGE")
        or "splunk/splunk:latest"
    )

    manifest = {
        "schema_version": "v1alpha1",
        "generated_at_utc": utc_now(),
        "pipeline": {
            "id": os.environ.get("CI_PIPELINE_ID", ""),
            "source": os.environ.get("CI_PIPELINE_SOURCE", ""),
            "ref": os.environ.get("CI_COMMIT_REF_NAME", ""),
            "commit_sha": os.environ.get("CI_COMMIT_SHA", ""),
        },
        "sok": {
            "baseline_version": read_makefile_version(project_dir),
        },
        "splunk": {
            "enterprise_image": enterprise_image,
        },
        "qualification": {
            "profile": qualification_profile,
            "helm_profile": helm_profile,
            "required_jobs": [
                "build-stage-image",
                "scan-stage-image-trivy",
                "gosec-scan",
                "govulncheck-scan",
                "eks-qualification-integration-validation",
                "helm-eks-validation",
            ],
        },
    }

    (output_dir / "qualification-manifest.json").write_text(
        json.dumps(manifest, indent=2, sort_keys=True) + "\n",
        encoding="utf-8",
    )
    (output_dir / "qualification-manifest.env").write_text(
        "\n".join(
            [
                f"SOK_QUALIFICATION_PROFILE={qualification_profile}",
                f"SOK_HELM_PROFILE={helm_profile}",
                f"SOK_BASELINE_VERSION={manifest['sok']['baseline_version']}",
                f"SOK_ENTERPRISE_IMAGE={enterprise_image}",
            ]
        )
        + "\n",
        encoding="utf-8",
    )
    (output_dir / "qualification-manifest.md").write_text(
        "\n".join(
            [
                "# Qualification Manifest",
                "",
                f"- profile: {qualification_profile}",
                f"- helm_profile: {helm_profile}",
                f"- baseline_version: {manifest['sok']['baseline_version']}",
                f"- enterprise_image: {enterprise_image}",
                f"- pipeline_id: {manifest['pipeline']['id']}",
                f"- pipeline_source: {manifest['pipeline']['source']}",
                f"- ref: {manifest['pipeline']['ref']}",
                "",
                "## Required Jobs",
                *[f"- {job}" for job in manifest["qualification"]["required_jobs"]],
            ]
        )
        + "\n",
        encoding="utf-8",
    )
    print(output_dir / "qualification-manifest.json")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
