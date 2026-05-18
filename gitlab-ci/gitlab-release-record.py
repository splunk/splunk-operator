#!/usr/bin/env python3
from __future__ import annotations

import json
import os
import sys
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path


def first_nonempty(*values: str) -> str:
    for value in values:
        if value and value.strip():
            return value.strip()
    return ""


def api_headers() -> dict[str, str]:
    private_token = first_nonempty(
        os.getenv("PIPELINE_GITLAB_RELEASE_API_TOKEN", ""),
        os.getenv("PIPELINE_GITLAB_API_TOKEN", ""),
    )
    if private_token:
        return {"PRIVATE-TOKEN": private_token}
    job_token = os.getenv("CI_JOB_TOKEN", "").strip()
    if job_token:
        return {"JOB-TOKEN": job_token}
    raise RuntimeError("Missing PIPELINE_GITLAB_RELEASE_API_TOKEN, PIPELINE_GITLAB_API_TOKEN, or CI_JOB_TOKEN")


def api_request_json(method: str, url: str, payload: dict | None = None) -> dict | list:
    headers = api_headers()
    data = None
    if payload is not None:
        headers = {**headers, "Content-Type": "application/json"}
        data = json.dumps(payload).encode("utf-8")
    request = urllib.request.Request(url, method=method, headers=headers, data=data)
    with urllib.request.urlopen(request) as response:
        body = response.read()
        if not body:
            return {}
        return json.loads(body)


def api_upload_file(url: str, path: Path) -> int:
    request = urllib.request.Request(url, method="PUT", headers=api_headers(), data=path.read_bytes())
    with urllib.request.urlopen(request) as response:
        response.read()
        return response.status


def load_optional_env_file(path: Path) -> dict[str, str]:
    values: dict[str, str] = {}
    if not path.exists():
        return values
    for raw_line in path.read_text(encoding="utf-8").splitlines():
        line = raw_line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, value = line.split("=", 1)
        values[key.strip()] = value.strip()
    return values


def load_optional_text(path: Path) -> str:
    if not path.exists():
        return ""
    return path.read_text(encoding="utf-8").strip()


def release_download_url(project_url: str, tag_name: str, direct_asset_path: str) -> str:
    encoded_tag = urllib.parse.quote(tag_name, safe="")
    normalized_path = direct_asset_path if direct_asset_path.startswith("/") else f"/{direct_asset_path}"
    return f"{project_url}/-/releases/{encoded_tag}/downloads{normalized_path}"


def collect_assets(project_dir: Path, package_base_url: str, project_url: str, tag_name: str) -> list[dict[str, str]]:
    candidate_contract = load_optional_env_file(
        project_dir / "ci-output" / "fetch-release-candidate-output" / "release-candidate" / "release-candidate-contract.env"
    )
    artifact_root = Path(
        first_nonempty(
            candidate_contract.get("RELEASE_CANDIDATE_ARTIFACT_ROOT", ""),
            str(project_dir / "ci-output" / "fetch-release-candidate-output" / "release-candidate"),
        )
    )
    if not artifact_root.is_absolute():
        artifact_root = project_dir / artifact_root
    release_archive_name = candidate_contract.get("RELEASE_ARTIFACT_ARCHIVE", "")
    operator_chart_name = candidate_contract.get("RELEASE_OPERATOR_CHART_ARCHIVE", "")
    enterprise_chart_name = candidate_contract.get("RELEASE_ENTERPRISE_CHART_ARCHIVE", "")

    candidates = [
        (
            "release-artifacts.tgz",
            artifact_root / release_archive_name,
            "Release artifacts archive",
            "package",
            "/artifacts/release-artifacts.tgz",
        ),
        (
            "splunk-operator-chart.tgz",
            artifact_root / operator_chart_name,
            "splunk-operator chart",
            "package",
            "/charts/splunk-operator-chart.tgz",
        ),
        (
            "splunk-enterprise-chart.tgz",
            artifact_root / enterprise_chart_name,
            "splunk-enterprise chart",
            "package",
            "/charts/splunk-enterprise-chart.tgz",
        ),
        (
            "preflight-commands.md",
            project_dir / "ci-output" / "preflight-certification-output" / "preflight-commands.md",
            "Red Hat preflight plan",
            "runbook",
            "/reports/preflight-commands.md",
        ),
        (
            "publish-release-images-summary.txt",
            project_dir / "ci-output" / "publish-release-images-output" / "summary.txt",
            "Published image summary",
            "other",
            "/reports/publish-release-images-summary.txt",
        ),
        (
            "publish-release-bundle-summary.txt",
            project_dir / "ci-output" / "publish-release-bundle-output" / "summary.txt",
            "Published bundle summary",
            "other",
            "/reports/publish-release-bundle-summary.txt",
        ),
        (
            "publish-release-charts-summary.txt",
            project_dir / "ci-output" / "publish-release-charts-output" / "summary.txt",
            "Published chart summary",
            "other",
            "/reports/publish-release-charts-summary.txt",
        ),
        (
            "release-psr-qualification-summary.txt",
            project_dir / "ci-output" / "release-psr-qualification-plan-output" / "summary.txt",
            "PSR qualification plan",
            "other",
            "/reports/release-psr-qualification-summary.txt",
        ),
        (
            "psr-qualification-verdict.md",
            project_dir / "ci-output" / "release-controller" / "psr-qualification-verdict.md",
            "PSR qualification verdict",
            "other",
            "/reports/psr-qualification-verdict.md",
        ),
        (
            "prepare-certified-operators-submission-summary.txt",
            project_dir / "ci-output" / "prepare-certified-operators-submission-output" / "summary.txt",
            "Certified Operators submission summary",
            "other",
            "/reports/prepare-certified-operators-submission-summary.txt",
        ),
        (
            "prepare-community-operators-submission-summary.txt",
            project_dir / "ci-output" / "prepare-community-operators-submission-output" / "summary.txt",
            "Community Operators submission summary",
            "other",
            "/reports/prepare-community-operators-submission-summary.txt",
        ),
    ]

    assets: list[dict[str, str]] = []
    for package_name, path, label, link_type, direct_asset_path in candidates:
        if not package_name or not path.exists() or not path.is_file():
            continue
        assets.append(
            {
                "label": label,
                "package_name": package_name,
                "path": str(path.relative_to(project_dir)),
                "upload_url": f"{package_base_url}/{urllib.parse.quote(package_name)}",
                "package_url": f"{package_base_url}/{urllib.parse.quote(package_name)}",
                "public_url": release_download_url(project_url, tag_name, direct_asset_path),
                "link_type": link_type,
                "direct_asset_path": direct_asset_path,
            }
        )
    return assets


def upsert_release(project_id: str, api_url: str, tag_name: str, ref: str, name: str, description: str) -> dict:
    encoded_tag = urllib.parse.quote(tag_name, safe="")
    release_url = f"{api_url}/projects/{project_id}/releases/{encoded_tag}"
    try:
        api_request_json("GET", release_url)
        return api_request_json("PUT", release_url, {"name": name, "description": description})
    except urllib.error.HTTPError as exc:
        if exc.code != 404:
            raise
    return api_request_json(
        "POST",
        f"{api_url}/projects/{project_id}/releases",
        {
            "name": name,
            "tag_name": tag_name,
            "ref": ref,
            "description": description,
        },
    )


def upsert_release_links(project_id: str, api_url: str, tag_name: str, assets: list[dict[str, str]]) -> list[dict]:
    encoded_tag = urllib.parse.quote(tag_name, safe="")
    links_url = f"{api_url}/projects/{project_id}/releases/{encoded_tag}/assets/links"
    existing = api_request_json("GET", links_url)
    by_name = {item["name"]: item for item in existing} if isinstance(existing, list) else {}
    results: list[dict] = []
    for asset in assets:
        payload = {
            "name": asset["label"],
            "url": asset["package_url"],
            "link_type": asset["link_type"],
            "direct_asset_path": asset["direct_asset_path"],
        }
        current = by_name.get(asset["label"])
        if current:
            result = api_request_json("PUT", f"{links_url}/{current['id']}", payload)
        else:
            result = api_request_json("POST", links_url, payload)
        results.append(result)
    return results


def build_description(release_version: str, project_dir: Path, assets: list[dict[str, str]]) -> str:
    release_contract = load_optional_env_file(project_dir / "ci-output" / "publish-release-images-output" / "release-image-contract.env")
    bundle_contract = load_optional_env_file(project_dir / "ci-output" / "publish-release-bundle-output" / "bundle-contract.env")
    candidate_contract = load_optional_env_file(
        project_dir / "ci-output" / "fetch-release-candidate-output" / "release-candidate" / "release-candidate-contract.env"
    )
    publish_charts_context = load_optional_env_file(
        project_dir / "ci-output" / "publish-release-charts-runtime-context.txt"
    )
    chart_repo_url = first_nonempty(
        os.getenv("PIPELINE_CHART_RELEASE_REPO_URL", ""),
        os.getenv("JOB_CHART_RELEASE_REPO_URL", ""),
        os.getenv("DEFAULT_CHART_RELEASE_REPO_URL", ""),
        publish_charts_context.get("chart_repo_url", ""),
        os.getenv("PIPELINE_RELEASED_HELM_REPO_URL", ""),
        "unset",
    )
    chart_publish_base = first_nonempty(
        os.getenv("PIPELINE_CHART_RELEASE_REPOSITORY", ""),
        os.getenv("JOB_CHART_RELEASE_REPOSITORY", ""),
        os.getenv("DEFAULT_CHART_RELEASE_REPOSITORY", ""),
        publish_charts_context.get("chart_repo", ""),
        "unset",
    )
    psr_summary = load_optional_text(project_dir / "ci-output" / "release-psr-qualification-plan-output" / "summary.txt")
    preflight_summary = load_optional_text(project_dir / "ci-output" / "preflight-certification-output" / "summary.txt")

    lines = [
        f"# Splunk Operator {release_version}",
        "",
        "## Published Images",
        "",
        f"- operator: `{release_contract.get('RELEASE_IMAGE', 'unset')}`",
        f"- distroless: `{release_contract.get('RELEASE_DISTROLESS_IMAGE', 'unset')}`",
        f"- bundle: `{bundle_contract.get('BUNDLE_IMG', 'unset')}`",
        f"- catalog: `{bundle_contract.get('CATALOG_IMG', 'unset')}`",
        "",
        "## Published Charts",
        "",
        f"- chart_repo_url: `{chart_repo_url}`",
        f"- chart_publish_base: `{chart_publish_base}`",
        f"- operator_chart_archive: `{candidate_contract.get('RELEASE_OPERATOR_CHART_ARCHIVE', 'unset')}`",
        f"- enterprise_chart_archive: `{candidate_contract.get('RELEASE_ENTERPRISE_CHART_ARCHIVE', 'unset')}`",
        "",
        "## Release Automation Notes",
        "",
        "- release artifacts were promoted from the validated release-candidate set",
        "- PSR remains plan-only in this lane until downstream dispatch is enabled",
    ]
    if preflight_summary:
        lines.extend(["", "## Preflight", "", preflight_summary])
    if psr_summary:
        lines.extend(["", "## PSR", "", psr_summary])
    if assets:
        lines.extend(["", "## Stable Assets", ""])
        lines.extend(f"- [{asset['label']}]({asset['public_url']})" for asset in assets)
    return "\n".join(lines).strip() + "\n"


def main() -> int:
    project_dir = Path.cwd()
    output_dir = project_dir / "ci-output" / "gitlab-release-record-output"
    output_dir.mkdir(parents=True, exist_ok=True)

    release_contract = load_optional_env_file(project_dir / "ci-output" / "publish-release-images-output" / "release-image-contract.env")
    candidate_contract = load_optional_env_file(
        project_dir / "ci-output" / "fetch-release-candidate-output" / "release-candidate" / "release-candidate-contract.env"
    )

    release_version = first_nonempty(
        release_contract.get("RELEASE_VERSION", ""),
        candidate_contract.get("RELEASE_VERSION", ""),
        os.getenv("PIPELINE_RELEASE_VERSION", ""),
    )
    if not release_version:
        raise RuntimeError("Unable to resolve release version for GitLab release record")

    api_url = os.getenv("CI_API_V4_URL", "https://cd.splunkdev.com/api/v4")
    project_id = os.environ["CI_PROJECT_ID"]
    project_url = os.getenv("CI_PROJECT_URL", "")
    tag_name = first_nonempty(os.getenv("PIPELINE_GITLAB_RELEASE_TAG", ""), f"v{release_version}")
    release_name = first_nonempty(os.getenv("PIPELINE_GITLAB_RELEASE_NAME", ""), f"Splunk Operator {release_version}")
    package_name = first_nonempty(
        os.getenv("PIPELINE_GITLAB_RELEASE_PACKAGE_NAME", ""),
        "splunk-operator-release-assets",
    )
    package_version = tag_name.removeprefix("v")
    package_base_url = (
        f"{api_url}/projects/{project_id}/packages/generic/"
        f"{urllib.parse.quote(package_name, safe='')}/"
        f"{urllib.parse.quote(package_version, safe='')}"
    )

    assets = collect_assets(project_dir, package_base_url, project_url, tag_name)
    description = build_description(release_version, project_dir, assets)

    plan = {
        "schema_version": "v1alpha1",
        "tag_name": tag_name,
        "release_name": release_name,
        "package_name": package_name,
        "package_version": package_version,
        "package_base_url": package_base_url,
        "assets": assets,
    }
    (output_dir / "gitlab-release-record-plan.json").write_text(
        json.dumps(plan, indent=2, sort_keys=True) + "\n",
        encoding="utf-8",
    )
    (output_dir / "gitlab-release-record-plan.md").write_text(
        "\n".join(
            [
                "# GitLab Release Record Plan",
                "",
                f"- tag_name: {tag_name}",
                f"- release_name: {release_name}",
                f"- package_name: {package_name}",
                f"- package_version: {package_version}",
                "",
                "## Assets",
                *([f"- {asset['label']}: {asset['public_url']}" for asset in assets] or ["- none"]),
            ]
        )
        + "\n",
        encoding="utf-8",
    )

    uploaded_assets: list[dict[str, str | int]] = []
    for asset in assets:
        status = api_upload_file(asset["upload_url"], project_dir / asset["path"])
        uploaded_assets.append(
            {
                "name": asset["label"],
                "status": status,
                "package_url": asset["package_url"],
                "release_asset_url": asset["public_url"],
            }
        )

    release = upsert_release(
        project_id=project_id,
        api_url=api_url,
        tag_name=tag_name,
        ref=os.getenv("CI_COMMIT_SHA", ""),
        name=release_name,
        description=description,
    )
    release_links = upsert_release_links(project_id, api_url, tag_name, assets)
    result = {
        "schema_version": "v1alpha1",
        "execution_status": "executed",
        "tag_name": tag_name,
        "release_url": release.get("url") or f"{project_url}/-/releases/{urllib.parse.quote(tag_name, safe='')}",
        "uploaded_assets": uploaded_assets,
        "release_links": release_links,
    }

    (output_dir / "gitlab-release-record-result.json").write_text(
        json.dumps(result, indent=2, sort_keys=True) + "\n",
        encoding="utf-8",
    )
    (output_dir / "gitlab-release-record-result.md").write_text(
        "\n".join(
            [
                "# GitLab Release Record Result",
                "",
                f"- execution_status: {result['execution_status']}",
                f"- tag_name: {tag_name}",
                f"- release_url: {result['release_url']}",
                "",
                "## Uploaded Assets",
                *(
                    f"- {item['name']}: {item['release_asset_url']} ({item['status']})"
                    for item in uploaded_assets
                ),
            ]
        )
        + "\n",
        encoding="utf-8",
    )
    print(output_dir / "gitlab-release-record-result.md")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (RuntimeError, urllib.error.URLError, urllib.error.HTTPError) as exc:
        print(str(exc), file=sys.stderr)
        raise SystemExit(1)
