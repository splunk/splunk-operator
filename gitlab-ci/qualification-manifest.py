#!/usr/bin/env python3
from __future__ import annotations

import json
import os
from datetime import datetime, timezone
from pathlib import Path

from qualification_jobs import qualification_jobs_for_environment


def utc_now() -> str:
    return datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def read_makefile_version(project_dir: Path) -> str:
    makefile = project_dir / "Makefile"
    for raw_line in makefile.read_text(encoding="utf-8").splitlines():
        line = raw_line.strip()
        if line.startswith("VERSION") and "?=" in line:
            return line.split("?=", 1)[1].strip()
    raise RuntimeError("Unable to resolve VERSION from Makefile")


def normalize_helm_profile(raw_profile: str) -> str:
    if raw_profile in {"", "qualification", "full"}:
        return "full"
    return raw_profile


def main() -> int:
    project_dir = Path.cwd()
    output_dir = project_dir / "ci-output" / "release-controller"
    output_dir.mkdir(parents=True, exist_ok=True)
    released_contract = json.loads((output_dir / "released-sok-contract.json").read_text(encoding="utf-8"))

    qualification_profile = os.environ.get("PIPELINE_QUALIFICATION_PROFILE", "monthly")
    helm_profile = normalize_helm_profile(
        os.environ.get("PIPELINE_HELM_TEST_PROFILE") or os.environ.get("JOB_HELM_TEST_PROFILE") or "full"
    )
    enterprise_image = (
        os.environ.get("SPLUNK_ENTERPRISE_RELEASE_IMAGE") or "splunk/splunk:latest"
    )
    fips_cluster_name = os.environ.get("PIPELINE_FIPS_EKS_CLUSTER_NAME", "").strip()
    qualification_jobs = qualification_jobs_for_environment(include_fips=bool(fips_cluster_name))

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
            "candidate_version": read_makefile_version(project_dir),
            "latest_released_version": released_contract["released_sok"]["version"],
            "released_operator_image_source": released_contract["released_sok"]["operator_image_source"],
        },
        "splunk": {
            "enterprise_image": enterprise_image,
        },
        "qualification": {
            "profile": qualification_profile,
            "helm_profile": helm_profile,
            "fips_cluster_name": fips_cluster_name,
            "fips_enabled": bool(fips_cluster_name),
            "required_jobs": qualification_jobs,
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
                f"SOK_CANDIDATE_VERSION={manifest['sok']['candidate_version']}",
                f"SOK_RELEASED_VERSION={manifest['sok']['latest_released_version']}",
                f"SOK_ENTERPRISE_IMAGE={enterprise_image}",
                f"SOK_FIPS_ENABLED={'true' if fips_cluster_name else 'false'}",
                f"SOK_FIPS_CLUSTER_NAME={fips_cluster_name}",
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
                f"- candidate_version: {manifest['sok']['candidate_version']}",
                f"- latest_released_version: {manifest['sok']['latest_released_version']}",
                f"- released_operator_image_source: {manifest['sok']['released_operator_image_source']}",
                f"- enterprise_image: {enterprise_image}",
                f"- fips_enabled: {'true' if fips_cluster_name else 'false'}",
                f"- fips_cluster_name: {fips_cluster_name or 'not-configured'}",
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
