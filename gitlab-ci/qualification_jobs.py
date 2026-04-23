from __future__ import annotations

BASE_QUALIFICATION_REQUIRED_JOBS = [
    "released-sok-contract",
    "scan-released-operator-image-trivy",
    "gosec-scan",
    "govulncheck-scan",
]

QUALIFICATION_HELM_JOB = "helm-eks-validation"

QUALIFICATION_INTEGRATION_JOBS = [
    (
        "qualification-eks-integration-appframeworks-s1-validation",
        "qualification-eks-integration-appframeworks-s1-validation",
    ),
    (
        "qualification-eks-integration-managerappframework-c3-validation",
        "qualification-eks-integration-managerappframework-c3-validation",
    ),
    (
        "qualification-eks-integration-managerappframework-m4-validation",
        "qualification-eks-integration-managerappframework-m4-validation",
    ),
    (
        "qualification-eks-integration-managersecret-validation",
        "qualification-eks-integration-managersecret-validation",
    ),
    (
        "qualification-eks-integration-managersmartstore-validation",
        "qualification-eks-integration-managersmartstore-validation",
    ),
    (
        "qualification-eks-integration-managermc1-validation",
        "qualification-eks-integration-managermc1-validation",
    ),
    (
        "qualification-eks-integration-managermc2-validation",
        "qualification-eks-integration-managermc2-validation",
    ),
    (
        "qualification-eks-integration-managerscaling-validation",
        "qualification-eks-integration-managerscaling-validation",
    ),
    (
        "qualification-eks-integration-managercrcrud-validation",
        "qualification-eks-integration-managercrcrud-validation",
    ),
    (
        "qualification-eks-integration-licensemanager-validation",
        "qualification-eks-integration-licensemanager-validation",
    ),
    (
        "qualification-eks-integration-indingsep-validation",
        "qualification-eks-integration-indingsep-validation",
    ),
]

QUALIFICATION_REQUIRED_JOBS = [
    *BASE_QUALIFICATION_REQUIRED_JOBS,
    *[job_name for job_name, _ in QUALIFICATION_INTEGRATION_JOBS],
    QUALIFICATION_HELM_JOB,
]


def qualification_job_evidence() -> dict[str, list[str]]:
    evidence = {
        "released-sok-contract": ["ci-output/release-controller/released-sok-contract.json"],
        "scan-released-operator-image-trivy": ["ci-output/scan-released-operator-image-trivy-trivy-results.txt"],
        "gosec-scan": ["gosec-results.txt"],
        "govulncheck-scan": ["govulncheck-results.txt"],
        QUALIFICATION_HELM_JOB: ["ci-output/helm-test-workflow-kuttl-junit.xml"],
    }
    for job_name, workflow_slug in QUALIFICATION_INTEGRATION_JOBS:
        evidence[job_name] = [f"ci-output/{workflow_slug}-inttest-junit.xml"]
    return evidence


def qualification_job_junit_evidence() -> dict[str, str]:
    junit_evidence = {
        QUALIFICATION_HELM_JOB: "ci-output/helm-test-workflow-kuttl-junit.xml",
    }
    for job_name, workflow_slug in QUALIFICATION_INTEGRATION_JOBS:
        junit_evidence[job_name] = f"ci-output/{workflow_slug}-inttest-junit.xml"
    return junit_evidence
