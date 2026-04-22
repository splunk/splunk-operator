#!/usr/bin/env python3
from __future__ import annotations

import json
from pathlib import Path


JOB_EVIDENCE = {
    "build-stage-image": ["ci-output/build-test-push-workflow-image-ref.txt"],
    "scan-stage-image-trivy": ["ci-output/scan-stage-image-trivy-trivy-results.txt"],
    "gosec-scan": ["gosec-results.txt"],
    "govulncheck-scan": ["govulncheck-results.txt"],
    "eks-smoke-validation": ["ci-output/int-test-workflow-inttest-junit.xml"],
    "helm-eks-validation": ["ci-output/helm-test-workflow-kuttl-junit.xml"],
}


def job_executed(project_dir: Path, job_name: str) -> bool:
    for relative_path in JOB_EVIDENCE.get(job_name, []):
        if (project_dir / relative_path).exists():
            return True
    return False


def main() -> int:
    project_dir = Path.cwd()
    output_dir = project_dir / "ci-output" / "release-controller"
    output_dir.mkdir(parents=True, exist_ok=True)

    manifest = json.loads((output_dir / "qualification-manifest.json").read_text(encoding="utf-8"))
    required_jobs = manifest["qualification"]["required_jobs"]

    executed = [job for job in required_jobs if job_executed(project_dir, job)]
    missing = [job for job in required_jobs if job not in executed]

    if missing:
        disposition = "qualified with caveats"
        disposition_reason = "missing evidence jobs"
    else:
        disposition = "qualified with current SOK"
        disposition_reason = "all required evidence jobs executed"

    compatibility = {
        "schema_version": "v1alpha1",
        "generated_at_utc": manifest["generated_at_utc"],
        "baseline_version": manifest["sok"]["baseline_version"],
        "enterprise_image": manifest["splunk"]["enterprise_image"],
        "qualification_profile": manifest["qualification"]["profile"],
        "helm_profile": manifest["qualification"]["helm_profile"],
        "disposition": disposition,
        "disposition_reason": disposition_reason,
        "executed_jobs": executed,
        "missing_jobs": missing,
    }

    (output_dir / "compatibility-record.json").write_text(
        json.dumps(compatibility, indent=2, sort_keys=True) + "\n",
        encoding="utf-8",
    )
    (output_dir / "qualification-report.md").write_text(
        "\n".join(
            [
                "# Qualification Report",
                "",
                f"- disposition: {disposition}",
                f"- disposition_reason: {disposition_reason}",
                f"- baseline_version: {manifest['sok']['baseline_version']}",
                f"- qualification_profile: {manifest['qualification']['profile']}",
                f"- helm_profile: {manifest['qualification']['helm_profile']}",
                f"- enterprise_image: {manifest['splunk']['enterprise_image']}",
                "",
                "## Executed Jobs",
                *([f"- {job}" for job in executed] or ["- none"]),
                "",
                "## Missing Jobs",
                *([f"- {job}" for job in missing] or ["- none"]),
            ]
        )
        + "\n",
        encoding="utf-8",
    )
    print(output_dir / "qualification-report.md")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
