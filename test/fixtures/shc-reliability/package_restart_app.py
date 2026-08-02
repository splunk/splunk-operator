#!/usr/bin/env python3
"""Build a deterministic restart-required Splunk app archive."""

from __future__ import annotations

import argparse
import gzip
import hashlib
import io
import os
from pathlib import Path
import re
import tarfile
import tempfile


VERSION_PATTERN = re.compile(r"^[0-9]+\.[0-9]+\.[0-9]+$")
APP_CONF_VERSION_PATTERN = re.compile(r"^version\s*=.*$", re.MULTILINE)


def _archive_info(name: str, *, directory: bool, executable: bool = False) -> tarfile.TarInfo:
    info = tarfile.TarInfo(name=name)
    info.mtime = 0
    info.uid = 0
    info.gid = 0
    info.uname = ""
    info.gname = ""
    if directory:
        info.type = tarfile.DIRTYPE
        info.mode = 0o755
        info.size = 0
    else:
        info.type = tarfile.REGTYPE
        info.mode = 0o755 if executable else 0o644
    return info


def _file_data(path: Path, relative_path: Path, version: str) -> bytes:
    data = path.read_bytes()
    if relative_path.as_posix() != "default/app.conf":
        return data

    try:
        text = data.decode("utf-8")
    except UnicodeDecodeError as error:
        raise ValueError(f"{path} must be UTF-8 text") from error
    text, replacements = APP_CONF_VERSION_PATTERN.subn(f"version = {version}", text)
    if replacements != 1:
        raise ValueError(f"{path} must contain exactly one version setting")
    return text.encode("utf-8")


def build_archive(source_dir: Path, version: str, output: Path) -> str:
    """Create the archive and return its SHA-256 digest."""
    source_dir = source_dir.resolve()
    if not source_dir.is_dir():
        raise ValueError(f"source directory not found: {source_dir}")
    if not VERSION_PATTERN.fullmatch(version):
        raise ValueError("version must use numeric major.minor.patch form")
    if not (source_dir / "default" / "app.conf").is_file():
        raise ValueError(f"missing required file: {source_dir / 'default' / 'app.conf'}")

    paths = sorted(source_dir.rglob("*"), key=lambda path: path.relative_to(source_dir).as_posix())
    if any(path.is_symlink() for path in paths):
        raise ValueError("symbolic links are not supported in qualification fixtures")

    output = output.resolve()
    output.parent.mkdir(parents=True, exist_ok=True)
    temporary_path: Path | None = None
    try:
        with tempfile.NamedTemporaryFile(dir=output.parent, delete=False) as temporary:
            temporary_path = Path(temporary.name)
            with gzip.GzipFile(filename="", mode="wb", fileobj=temporary, mtime=0, compresslevel=9) as compressed:
                with tarfile.open(fileobj=compressed, mode="w", format=tarfile.GNU_FORMAT) as archive:
                    root = _archive_info(f"{source_dir.name}/", directory=True)
                    archive.addfile(root)
                    for path in paths:
                        relative_path = path.relative_to(source_dir)
                        archive_name = f"{source_dir.name}/{relative_path.as_posix()}"
                        if path.is_dir():
                            archive.addfile(_archive_info(f"{archive_name}/", directory=True))
                            continue
                        if not path.is_file():
                            raise ValueError(f"unsupported fixture entry: {path}")
                        data = _file_data(path, relative_path, version)
                        executable = bool(path.stat().st_mode & 0o111)
                        info = _archive_info(archive_name, directory=False, executable=executable)
                        info.size = len(data)
                        archive.addfile(info, io.BytesIO(data))
        os.replace(temporary_path, output)
        temporary_path = None
    finally:
        if temporary_path is not None:
            temporary_path.unlink(missing_ok=True)

    digest = hashlib.sha256(output.read_bytes()).hexdigest()
    return digest


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source-dir", required=True, type=Path)
    parser.add_argument("--version", required=True)
    parser.add_argument("--output", required=True, type=Path)
    arguments = parser.parse_args()

    try:
        digest = build_archive(arguments.source_dir, arguments.version, arguments.output)
    except ValueError as error:
        parser.error(str(error))
    print(f"{digest}  {arguments.output}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
