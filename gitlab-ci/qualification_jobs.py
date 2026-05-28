#!/usr/bin/env python3
from __future__ import annotations


BASE_REQUIRED_QUALIFICATION_JOBS = [
    "released-sok-contract",
    "gosec-scan",
    "govulncheck-scan",
    "eks-qualification-integration-validation",
    "helm-eks-validation",
    "qualification-azure-validation",
    "qualification-gcp-c3-validation",
    "qualification-gcp-c3-manager-validation",
    "qualification-gcp-m4-validation",
    "qualification-gcp-m4-manager-validation",
    "qualification-gcp-s1-validation",
]

FIPS_QUALIFICATION_JOBS = [
    "qualification-fips-smoke-validation",
    "qualification-fips-integration-validation",
]

DISTROLESS_QUALIFICATION_SUITE_JOBS = [
    "qualification-distroless-appframeworks-s1-validation",
    "qualification-distroless-managerappframework-c3-validation",
    "qualification-distroless-managerappframework-m4-validation",
    "qualification-distroless-managersecret-validation",
    "qualification-distroless-managersmartstore-validation",
    "qualification-distroless-managermc1-validation",
    "qualification-distroless-managermc2-validation",
    "qualification-distroless-managercrcrud-validation",
    "qualification-distroless-licensemanager-validation",
    "qualification-distroless-managerdeletecr-validation",
    "qualification-distroless-indingsep-validation",
]

GRAVITON_QUALIFICATION_SUITE_JOBS = [
    "qualification-graviton-appframeworks-s1-validation",
    "qualification-graviton-managersecret-validation",
    "qualification-graviton-managersmartstore-validation",
    "qualification-graviton-managermc1-validation",
    "qualification-graviton-managermc2-validation",
    "qualification-graviton-managercrcrud-validation",
    "qualification-graviton-licensemanager-validation",
    "qualification-graviton-managerdeletecr-validation",
    "qualification-graviton-indingsep-validation",
]

REQUIRED_QUALIFICATION_JOBS = (
    BASE_REQUIRED_QUALIFICATION_JOBS
    + FIPS_QUALIFICATION_JOBS
    + DISTROLESS_QUALIFICATION_SUITE_JOBS
    + GRAVITON_QUALIFICATION_SUITE_JOBS
)


def qualification_jobs_for_environment(*, include_fips: bool, include_graviton: bool) -> list[str]:
    jobs = list(BASE_REQUIRED_QUALIFICATION_JOBS)
    if include_fips:
        jobs.extend(FIPS_QUALIFICATION_JOBS)
    jobs.extend(DISTROLESS_QUALIFICATION_SUITE_JOBS)
    if include_graviton:
        jobs.extend(GRAVITON_QUALIFICATION_SUITE_JOBS)
    return jobs


JOB_EVIDENCE = {
    "released-sok-contract": ["ci-output/release-controller/released-sok-contract.json"],
    "gosec-scan": ["gosec-results.txt"],
    "govulncheck-scan": ["govulncheck-results.txt"],
    "eks-qualification-integration-validation": ["ci-output/qualification-int-test-workflow-inttest-junit.xml"],
    "helm-eks-validation": ["ci-output/helm-test-workflow-kuttl-junit.xml"],
    "qualification-fips-smoke-validation": ["ci-output/qualification-fips-smoke-validation-inttest-junit.xml"],
    "qualification-fips-integration-validation": ["ci-output/qualification-fips-integration-validation-inttest-junit.xml"],
    "qualification-azure-validation": ["ci-output/qualification-azure-validation-inttest-junit.xml"],
    "qualification-gcp-c3-validation": ["ci-output/qualification-gcp-c3-validation-inttest-junit.xml"],
    "qualification-gcp-c3-manager-validation": ["ci-output/qualification-gcp-c3-manager-validation-inttest-junit.xml"],
    "qualification-gcp-m4-validation": ["ci-output/qualification-gcp-m4-validation-inttest-junit.xml"],
    "qualification-gcp-m4-manager-validation": ["ci-output/qualification-gcp-m4-manager-validation-inttest-junit.xml"],
    "qualification-gcp-s1-validation": ["ci-output/qualification-gcp-s1-validation-inttest-junit.xml"],
    "qualification-distroless-appframeworks-s1-validation": ["ci-output/qualification-distroless-appframeworks-s1-validation-inttest-junit.xml"],
    "qualification-distroless-managerappframework-c3-validation": ["ci-output/qualification-distroless-managerappframework-c3-validation-inttest-junit.xml"],
    "qualification-distroless-managerappframework-m4-validation": ["ci-output/qualification-distroless-managerappframework-m4-validation-inttest-junit.xml"],
    "qualification-distroless-managersecret-validation": ["ci-output/qualification-distroless-managersecret-validation-inttest-junit.xml"],
    "qualification-distroless-managersmartstore-validation": ["ci-output/qualification-distroless-managersmartstore-validation-inttest-junit.xml"],
    "qualification-distroless-managermc1-validation": ["ci-output/qualification-distroless-managermc1-validation-inttest-junit.xml"],
    "qualification-distroless-managermc2-validation": ["ci-output/qualification-distroless-managermc2-validation-inttest-junit.xml"],
    "qualification-distroless-managercrcrud-validation": ["ci-output/qualification-distroless-managercrcrud-validation-inttest-junit.xml"],
    "qualification-distroless-licensemanager-validation": ["ci-output/qualification-distroless-licensemanager-validation-inttest-junit.xml"],
    "qualification-distroless-managerdeletecr-validation": ["ci-output/qualification-distroless-managerdeletecr-validation-inttest-junit.xml"],
    "qualification-distroless-indingsep-validation": ["ci-output/qualification-distroless-indingsep-validation-inttest-junit.xml"],
    "qualification-graviton-appframeworks-s1-validation": ["ci-output/qualification-graviton-appframeworks-s1-validation-inttest-junit.xml"],
    "qualification-graviton-managersecret-validation": ["ci-output/qualification-graviton-managersecret-validation-inttest-junit.xml"],
    "qualification-graviton-managersmartstore-validation": ["ci-output/qualification-graviton-managersmartstore-validation-inttest-junit.xml"],
    "qualification-graviton-managermc1-validation": ["ci-output/qualification-graviton-managermc1-validation-inttest-junit.xml"],
    "qualification-graviton-managermc2-validation": ["ci-output/qualification-graviton-managermc2-validation-inttest-junit.xml"],
    "qualification-graviton-managercrcrud-validation": ["ci-output/qualification-graviton-managercrcrud-validation-inttest-junit.xml"],
    "qualification-graviton-licensemanager-validation": ["ci-output/qualification-graviton-licensemanager-validation-inttest-junit.xml"],
    "qualification-graviton-managerdeletecr-validation": ["ci-output/qualification-graviton-managerdeletecr-validation-inttest-junit.xml"],
    "qualification-graviton-indingsep-validation": ["ci-output/qualification-graviton-indingsep-validation-inttest-junit.xml"],
}

JOB_JUNIT_EVIDENCE = {
    job_name: evidence_paths[0]
    for job_name, evidence_paths in JOB_EVIDENCE.items()
    if evidence_paths and evidence_paths[0].endswith(".xml")
}
