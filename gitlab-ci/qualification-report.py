#!/usr/bin/env python3
from __future__ import annotations

import json
from pathlib import Path
from xml.etree import ElementTree


JOB_EVIDENCE = {
    "build-stage-image": ["ci-output/build-test-push-workflow-image-ref.txt"],
    "scan-stage-image-trivy": ["ci-output/scan-stage-image-trivy-trivy-results.txt"],
    "gosec-scan": ["gosec-results.txt"],
    "govulncheck-scan": ["govulncheck-results.txt"],
    "eks-qualification-integration-validation": ["ci-output/qualification-int-test-workflow-inttest-junit.xml"],
    "helm-eks-validation": ["ci-output/helm-test-workflow-kuttl-junit.xml"],
}

JOB_JUNIT_EVIDENCE = {
    "eks-qualification-integration-validation": "ci-output/qualification-int-test-workflow-inttest-junit.xml",
    "helm-eks-validation": "ci-output/helm-test-workflow-kuttl-junit.xml",
}


def job_executed(project_dir: Path, job_name: str) -> bool:
    for relative_path in JOB_EVIDENCE.get(job_name, []):
        if (project_dir / relative_path).exists():
            return True
    return False


def load_optional_json(path: Path) -> dict | list | None:
    if not path.exists():
        return None
    return json.loads(path.read_text(encoding="utf-8"))


def read_optional_text(path: Path) -> str:
    if not path.exists():
        return ""
    return path.read_text(encoding="utf-8").strip()


def read_int(value: str | None) -> int:
    try:
        return int(value or "0")
    except ValueError:
        return 0


def count_trivy_findings(project_dir: Path) -> int:
    sarif_path = project_dir / "ci-output" / "scan-stage-image-trivy-trivy-results.sarif"
    sarif = load_optional_json(sarif_path)
    if not isinstance(sarif, dict):
        return 0

    findings = 0
    for run in sarif.get("runs", []):
        findings += len(run.get("results", []))
    return findings


def junit_failed(project_dir: Path, job_name: str) -> bool:
    relative_path = JOB_JUNIT_EVIDENCE.get(job_name)
    if not relative_path:
        return False

    junit_path = project_dir / relative_path
    if not junit_path.exists():
        return False

    root = ElementTree.fromstring(junit_path.read_text(encoding="utf-8"))

    if root.tag in {"testsuite", "testsuites"} and (
        "failures" in root.attrib or "errors" in root.attrib
    ):
        return read_int(root.attrib.get("failures")) > 0 or read_int(root.attrib.get("errors")) > 0

    failures = 0
    errors = 0
    for suite in root.findall(".//testsuite"):
        failures += read_int(suite.attrib.get("failures"))
        errors += read_int(suite.attrib.get("errors"))
    return failures > 0 or errors > 0


def main() -> int:
    project_dir = Path.cwd()
    output_dir = project_dir / "ci-output" / "release-controller"
    output_dir.mkdir(parents=True, exist_ok=True)

    manifest = json.loads((output_dir / "qualification-manifest.json").read_text(encoding="utf-8"))
    required_jobs = manifest["qualification"]["required_jobs"]

    executed = [job for job in required_jobs if job_executed(project_dir, job)]
    missing = [job for job in required_jobs if job not in executed]
    trivy_findings = count_trivy_findings(project_dir)
    gosec_status = read_optional_text(project_dir / "gosec-status.txt")
    govulncheck_status = read_optional_text(project_dir / "govulncheck-status.txt")
    failing_test_jobs = [job for job in JOB_JUNIT_EVIDENCE if junit_failed(project_dir, job)]

    security_blocked: list[str] = []
    if trivy_findings > 0:
        security_blocked.append("scan-stage-image-trivy")
    if gosec_status == "failed":
        security_blocked.append("gosec-scan")
    if govulncheck_status == "failed":
        security_blocked.append("govulncheck-scan")

    if security_blocked:
        disposition = "not qualified"
        disposition_reason = "security evidence failed"
    elif failing_test_jobs:
        disposition = "not qualified"
        disposition_reason = "qualification test evidence failed"
    elif missing:
        disposition = "not qualified"
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
        "failing_test_jobs": failing_test_jobs,
        "security_blocked_jobs": security_blocked,
        "security_findings": {
            "trivy_result_count": trivy_findings,
            "gosec_status": gosec_status or "unknown",
            "govulncheck_status": govulncheck_status or "unknown",
        },
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
                f"- trivy_result_count: {trivy_findings}",
                f"- gosec_status: {gosec_status or 'unknown'}",
                f"- govulncheck_status: {govulncheck_status or 'unknown'}",
                "",
                "## Executed Jobs",
                *([f"- {job}" for job in executed] or ["- none"]),
                "",
                "## Security Blocked Jobs",
                *([f"- {job}" for job in security_blocked] or ["- none"]),
                "",
                "## Failing Test Jobs",
                *([f"- {job}" for job in failing_test_jobs] or ["- none"]),
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
