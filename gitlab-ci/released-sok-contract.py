#!/usr/bin/env python3
from __future__ import annotations

import json
import os
import urllib.parse
import urllib.request
from datetime import datetime, timezone
from pathlib import Path


GITHUB_RELEASE_URL = "https://api.github.com/repos/splunk/splunk-operator/releases/latest"
DEFAULT_HELM_REPO_URL = "https://splunk.github.io/splunk-operator"
DOCKER_AUTH_URL = "https://auth.docker.io/token"
DOCKER_REGISTRY_URL = "https://registry-1.docker.io"
DEFAULT_OPERATOR_REPOSITORY = "docker.io/splunk/splunk-operator"


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


def fetch_json_with_headers(url: str, headers: dict[str, str]) -> dict:
    request = urllib.request.Request(url, headers=headers)
    with urllib.request.urlopen(request, timeout=30) as response:
        return json.loads(response.read().decode("utf-8"))


def env_first(*names: str, default: str = "") -> str:
    for name in names:
        value = os.getenv(name, "").strip()
        if value:
            return value
    return default


def normalize_version(raw_version: str) -> str:
    return raw_version.strip().removeprefix("v")


def split_image_repository(repository: str) -> tuple[str, str]:
    normalized = repository.strip()
    if not normalized:
        raise RuntimeError("Released operator repository is not configured")

    first_component, separator, remainder = normalized.partition("/")
    if separator and ("." in first_component or ":" in first_component or first_component == "localhost"):
        if not remainder:
            raise RuntimeError(f"Released operator repository is missing a path: {repository}")
        return first_component, remainder

    return "docker.io", normalized


def build_image_ref(registry: str, repository_path: str, version: str) -> str:
    return f"{registry}/{repository_path}:{version}"


def released_operator_repository() -> tuple[str, str]:
    repository = env_first(
        "PIPELINE_RELEASED_OPERATOR_IMAGE_REPOSITORY",
        "PIPELINE_RELEASE_IMAGE_REPOSITORY",
        default=DEFAULT_OPERATOR_REPOSITORY,
    )
    return split_image_repository(repository)


def released_helm_repo_url() -> str:
    return env_first(
        "PIPELINE_RELEASED_HELM_REPO_URL",
        "PIPELINE_CHART_RELEASE_REPOSITORY",
        default=DEFAULT_HELM_REPO_URL,
    )


def released_helm_index_url(repo_url: str) -> str:
    if repo_url.startswith("oci://"):
        return ""
    return f"{repo_url.rstrip('/')}/index.yaml"


def chart_download_url(repo_url: str, chart_name: str, version: str) -> str:
    if repo_url.startswith("oci://"):
        return f"{repo_url}/{chart_name}"
    return f"{repo_url.rstrip('/')}/{chart_name}-{version}.tgz"


def require_chart_release(repo_url: str, helm_index: str, chart_name: str, version: str) -> str:
    chart_ref = chart_download_url(repo_url, chart_name, version)
    if repo_url.startswith("oci://"):
        return chart_ref
    if chart_ref not in helm_index:
        raise RuntimeError(
            f"Released Helm repo is missing {chart_name} chart version {version}: {chart_ref}"
        )
    return chart_ref


def fetch_docker_registry_token(repository: str) -> str:
    query = urllib.parse.urlencode(
        {
            "service": "registry.docker.io",
            "scope": f"repository:{repository}:pull",
        }
    )
    payload = fetch_json_with_headers(f"{DOCKER_AUTH_URL}?{query}", {"User-Agent": "sok-gitlab-qualification"})
    token = payload.get("token", "")
    if not token:
        raise RuntimeError(f"Unable to get Docker registry token for {repository}")
    return token


def require_image_tag_release(repository: str, tag: str) -> str:
    token = fetch_docker_registry_token(repository)
    manifest_url = f"{DOCKER_REGISTRY_URL}/v2/{repository}/manifests/{tag}"
    request = urllib.request.Request(
        manifest_url,
        headers={
            "Accept": ",".join(
                [
                    "application/vnd.oci.image.index.v1+json",
                    "application/vnd.docker.distribution.manifest.list.v2+json",
                    "application/vnd.oci.image.manifest.v1+json",
                    "application/vnd.docker.distribution.manifest.v2+json",
                ]
            ),
            "Authorization": f"Bearer {token}",
            "User-Agent": "sok-gitlab-qualification",
        },
        method="HEAD",
    )
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            if response.status != 200:
                raise RuntimeError(
                    f"Released operator image is not available for {repository}:{tag}: HTTP {response.status}"
                )
    except urllib.error.HTTPError as exc:
        raise RuntimeError(
            f"Released operator image is not available for {repository}:{tag}: HTTP {exc.code}"
        ) from exc
    return f"docker.io/{repository}:{tag}"


def require_released_operator_image(registry: str, repository_path: str, tag: str) -> str:
    if registry in {"docker.io", "registry-1.docker.io", "index.docker.io"}:
        return require_image_tag_release(repository_path, tag)

    # Non-Docker Hub repositories are configuration-driven. The release lane
    # and the released-SOK contract now share the same repository variable
    # contract, so downstream official-release consumers can follow the same
    # published location even when the official registry path changes.
    return build_image_ref(registry, repository_path, tag)


def build_contract() -> dict:
    release = fetch_json(GITHUB_RELEASE_URL)
    released_version = normalize_version(release["tag_name"])
    helm_repo_url = released_helm_repo_url()
    helm_index_url = released_helm_index_url(helm_repo_url)
    helm_index = fetch_text(helm_index_url) if helm_index_url else ""
    operator_registry, operator_repository_path = released_operator_repository()
    enterprise_chart_url = require_chart_release(helm_repo_url, helm_index, "splunk-enterprise", released_version)
    operator_chart_url = require_chart_release(helm_repo_url, helm_index, "splunk-operator", released_version)
    operator_image_source = require_released_operator_image(operator_registry, operator_repository_path, released_version)
    distroless_image_source = require_released_operator_image(
        operator_registry,
        operator_repository_path,
        f"{released_version}-distroless",
    )

    return {
        "schema_version": "v1alpha1",
        "generated_at_utc": utc_now(),
        "release_source": {
            "github_release_api": GITHUB_RELEASE_URL,
            "github_release_html": release.get("html_url", ""),
            "helm_repo_index": helm_index_url,
            "helm_repo_url": helm_repo_url,
            "docker_registry": operator_registry if operator_registry != "docker.io" else DOCKER_REGISTRY_URL,
        },
        "released_sok": {
            "version": released_version,
            "operator_image_source": operator_image_source,
            "operator_image_mirror_path": f"{operator_repository_path}:{released_version}",
            "distroless_image_source": distroless_image_source,
            "distroless_image_mirror_path": f"{operator_repository_path}:{released_version}-distroless",
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
                f"SOK_RELEASED_DISTROLESS_IMAGE_SOURCE={contract['released_sok']['distroless_image_source']}",
                f"SOK_RELEASED_DISTROLESS_IMAGE_MIRROR_PATH={contract['released_sok']['distroless_image_mirror_path']}",
                f"SOK_RELEASED_ENTERPRISE_CHART_VERSION={released_version}",
                f"SOK_RELEASED_OPERATOR_CHART_VERSION={released_version}",
                f"SOK_RELEASED_HELM_REPO_URL={contract['release_source']['helm_repo_url']}",
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
    (output_dir / "released-distroless-image-source.txt").write_text(
        contract["released_sok"]["distroless_image_source"] + "\n",
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
                f"- distroless_image_source: {contract['released_sok']['distroless_image_source']}",
                f"- distroless_image_mirror_path: {contract['released_sok']['distroless_image_mirror_path']}",
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
