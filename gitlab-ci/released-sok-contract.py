#!/usr/bin/env python3
from __future__ import annotations

import json
import urllib.request
from datetime import datetime, timezone
from pathlib import Path


GITHUB_RELEASE_URL = "https://api.github.com/repos/splunk/splunk-operator/releases/latest"
HELM_INDEX_URL = "https://splunk.github.io/splunk-operator/index.yaml"
HELM_REPO_URL = "https://splunk.github.io/splunk-operator"


def utc_now() -> str:
    return datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def fetch_text(url: str) -> str:
    request = urllib.request.Request(
        url,
        headers={
            "Accept": "application/vnd.github+json, text/plain, */*",
            "User-Agent": "sok-gitlab-qualification",
        },
    )
    with urllib.request.urlopen(request, timeout=30) as response:
        return response.read().decode("utf-8")


def fetch_json(url: str) -> dict:
    return json.loads(fetch_text(url))


def normalize_version(raw_version: str) -> str:
    return raw_version.strip().removeprefix("v")


def chart_download_url(chart_name: str, version: str) -> str:
    return f"{HELM_REPO_URL}/{chart_name}-{version}.tgz"


def require_chart_release(helm_index: str, chart_name: str, version: str) -> str:
    chart_url = chart_download_url(chart_name, version)
    if chart_url not in helm_index:
        raise RuntimeError(
            f"Released Helm repo is missing {chart_name} chart version {version}: {chart_url}"
        )
    return chart_url


def build_contract() -> dict:
    release = fetch_json(GITHUB_RELEASE_URL)
    released_version = normalize_version(release["tag_name"])
    helm_index = fetch_text(HELM_INDEX_URL)
    enterprise_chart_url = require_chart_release(helm_index, "splunk-enterprise", released_version)
    operator_chart_url = require_chart_release(helm_index, "splunk-operator", released_version)

    return {
        "schema_version": "v1alpha1",
        "generated_at_utc": utc_now(),
        "release_source": {
            "github_release_api": GITHUB_RELEASE_URL,
            "github_release_html": release.get("html_url", ""),
            "helm_repo_index": HELM_INDEX_URL,
            "helm_repo_url": HELM_REPO_URL,
        },
        "released_sok": {
            "version": released_version,
            "operator_image_source": f"docker.io/splunk/splunk-operator:{released_version}",
            "operator_image_mirror_path": f"splunk/splunk-operator:{released_version}",
            "enterprise_chart_version": released_version,
            "operator_chart_version": released_version,
            "enterprise_chart_url": enterprise_chart_url,
            "operator_chart_url": operator_chart_url,
        },
    }


def write_contract_artifacts(output_dir: Path, contract: dict) -> None:
    released_version = contract["released_sok"]["version"]

    (output_dir / "released-sok-contract.json").write_text(
        json.dumps(contract, indent=2, sort_keys=True) + "\n",
        encoding="utf-8",
    )
    (output_dir / "released-sok-contract.env").write_text(
        "\n".join(
            [
                f"SOK_RELEASED_VERSION={released_version}",
                f"SOK_RELEASED_OPERATOR_IMAGE_SOURCE={contract['released_sok']['operator_image_source']}",
                f"SOK_RELEASED_OPERATOR_IMAGE_MIRROR_PATH={contract['released_sok']['operator_image_mirror_path']}",
                f"SOK_RELEASED_ENTERPRISE_CHART_VERSION={released_version}",
                f"SOK_RELEASED_OPERATOR_CHART_VERSION={released_version}",
                f"SOK_RELEASED_HELM_REPO_URL={HELM_REPO_URL}",
                f"SOK_RELEASED_ENTERPRISE_CHART_URL={contract['released_sok']['enterprise_chart_url']}",
                f"SOK_RELEASED_OPERATOR_CHART_URL={contract['released_sok']['operator_chart_url']}",
            ]
        )
        + "\n",
        encoding="utf-8",
    )
    (output_dir / "released-operator-image-source.txt").write_text(
        contract["released_sok"]["operator_image_source"] + "\n",
        encoding="utf-8",
    )
    (output_dir / "released-sok-contract.md").write_text(
        "\n".join(
            [
                "# Released SOK Contract",
                "",
                f"- released_version: {released_version}",
                f"- operator_image_source: {contract['released_sok']['operator_image_source']}",
                f"- operator_image_mirror_path: {contract['released_sok']['operator_image_mirror_path']}",
                f"- enterprise_chart_version: {released_version}",
                f"- operator_chart_version: {released_version}",
                f"- github_release_html: {contract['release_source']['github_release_html']}",
                f"- enterprise_chart_url: {contract['released_sok']['enterprise_chart_url']}",
                f"- operator_chart_url: {contract['released_sok']['operator_chart_url']}",
            ]
        )
        + "\n",
        encoding="utf-8",
    )


def main() -> int:
    output_dir = Path.cwd() / "ci-output" / "release-controller"
    output_dir.mkdir(parents=True, exist_ok=True)

    contract = build_contract()
    write_contract_artifacts(output_dir, contract)
    print(output_dir / "released-sok-contract.json")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
