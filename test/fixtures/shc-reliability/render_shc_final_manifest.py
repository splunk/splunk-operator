#!/usr/bin/env python3
"""Render the immutable final SHC qualification manifest."""

from __future__ import annotations

import argparse
import hashlib
import ipaddress
import os
from pathlib import Path
import re
import tempfile


NAMESPACE_PATTERN = re.compile(r"^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$")
S3_BUCKET_PATTERN = re.compile(r"^[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?$")
S3_PREFIX_PATTERN = re.compile(r"^[A-Za-z0-9](?:[A-Za-z0-9._/-]*[A-Za-z0-9])?$")
IMAGE_PATTERN = re.compile(r"^[^\s@]+@sha256:[0-9a-f]{64}$")

TOKENS = {
    "namespace": "__NAMESPACE__",
    "runtime_image": "__RUNTIME_IMAGE__",
    "s3_bucket": "__S3_BUCKET__",
    "s3_prefix": "__S3_PREFIX__",
}


def _validate_namespace(value: str) -> None:
    if len(value) > 63 or not NAMESPACE_PATTERN.fullmatch(value):
        raise ValueError("namespace must be a valid Kubernetes DNS label")


def _validate_runtime_image(value: str) -> None:
    if not IMAGE_PATTERN.fullmatch(value):
        raise ValueError("runtime image must include an immutable @sha256 digest")


def _validate_s3_bucket(value: str) -> None:
    if not 3 <= len(value) <= 63 or not S3_BUCKET_PATTERN.fullmatch(value):
        raise ValueError("S3 bucket name is invalid")
    if ".." in value or ".-" in value or "-." in value:
        raise ValueError("S3 bucket name is invalid")
    try:
        ipaddress.ip_address(value)
    except ValueError:
        return
    raise ValueError("S3 bucket name must not be an IP address")


def _validate_s3_prefix(value: str) -> None:
    normalized = value.strip("/")
    if not normalized or normalized != value or not S3_PREFIX_PATTERN.fullmatch(value):
        raise ValueError("S3 prefix must be non-empty and must not start or end with a slash")
    if any(segment in {"", ".", ".."} for segment in value.split("/")):
        raise ValueError("S3 prefix contains an invalid path segment")


def render_manifest(
    template: Path,
    output: Path,
    *,
    namespace: str,
    runtime_image: str,
    s3_bucket: str,
    s3_prefix: str,
) -> str:
    _validate_namespace(namespace)
    _validate_runtime_image(runtime_image)
    _validate_s3_bucket(s3_bucket)
    _validate_s3_prefix(s3_prefix)

    if not template.is_file():
        raise ValueError(f"manifest template not found: {template}")
    rendered = template.read_text(encoding="utf-8")
    values = {
        "namespace": namespace,
        "runtime_image": runtime_image,
        "s3_bucket": s3_bucket,
        "s3_prefix": s3_prefix,
    }
    for name, token in TOKENS.items():
        if token not in rendered:
            raise ValueError(f"manifest template is missing {token}")
        rendered = rendered.replace(token, values[name])
    leftovers = sorted(set(re.findall(r"__[A-Z0-9_]+__", rendered)))
    if leftovers:
        raise ValueError(f"unresolved manifest tokens: {', '.join(leftovers)}")

    output = output.resolve()
    output.parent.mkdir(parents=True, exist_ok=True)
    temporary_path: Path | None = None
    try:
        with tempfile.NamedTemporaryFile(
            mode="w", encoding="utf-8", dir=output.parent, delete=False
        ) as temporary:
            temporary.write(rendered)
            temporary_path = Path(temporary.name)
        os.replace(temporary_path, output)
        temporary_path = None
    finally:
        if temporary_path is not None:
            temporary_path.unlink(missing_ok=True)

    return hashlib.sha256(output.read_bytes()).hexdigest()


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--template", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--namespace", required=True)
    parser.add_argument("--runtime-image", required=True)
    parser.add_argument("--s3-bucket", required=True)
    parser.add_argument("--s3-prefix", required=True)
    arguments = parser.parse_args()

    try:
        digest = render_manifest(
            arguments.template,
            arguments.output,
            namespace=arguments.namespace,
            runtime_image=arguments.runtime_image,
            s3_bucket=arguments.s3_bucket,
            s3_prefix=arguments.s3_prefix,
        )
    except ValueError as error:
        parser.error(str(error))
    print(f"{digest}  {arguments.output}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
