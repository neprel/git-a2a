#!/usr/bin/env python3
"""Select missing immutable PyPI wheels without overwriting published files."""

from __future__ import annotations

import argparse
import hashlib
import json
import re
import shutil
import sys
import urllib.error
import urllib.request
from pathlib import Path


PLATFORMS = (
    "macosx_10_15_x86_64",
    "macosx_11_0_arm64",
    "manylinux_2_17_x86_64",
    "manylinux_2_17_aarch64",
    "win_amd64",
    "win_arm64",
)
SHA256 = re.compile(r"^[0-9a-f]{64}$")


def expected_names(version: str) -> set[str]:
    return {f"git_a2a-{version}-py3-none-{platform}.whl" for platform in PLATFORMS}


def file_digest(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def registry_files(url: str) -> dict[str, str]:
    request = urllib.request.Request(
        url,
        headers={"Accept": "application/json", "User-Agent": "git-a2a-release-recovery"},
    )
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            payload = json.load(response)
    except urllib.error.HTTPError as error:
        if error.code == 404:
            return {}
        raise RuntimeError(f"PyPI registry request failed: HTTP {error.code}") from error
    except (urllib.error.URLError, TimeoutError, json.JSONDecodeError) as error:
        raise RuntimeError(f"PyPI registry request failed: {error}") from error

    if not isinstance(payload, dict) or not isinstance(payload.get("urls"), list):
        raise RuntimeError("PyPI registry returned malformed version metadata")
    published: dict[str, str] = {}
    for item in payload["urls"]:
        if not isinstance(item, dict):
            raise RuntimeError("PyPI registry returned malformed file metadata")
        filename = item.get("filename")
        digests = item.get("digests")
        if not isinstance(filename, str) or not isinstance(digests, dict):
            raise RuntimeError("PyPI registry returned malformed file metadata")
        sha256 = digests.get("sha256")
        if not isinstance(sha256, str) or not SHA256.fullmatch(sha256.lower()):
            raise RuntimeError(f"PyPI registry returned an invalid SHA-256 for {filename}")
        sha256 = sha256.lower()
        if filename in published and published[filename] != sha256:
            raise RuntimeError(f"PyPI registry returned conflicting SHA-256 values for {filename}")
        published[filename] = sha256
    return published


def select_missing(wheelhouse: Path, output: Path, version: str, metadata_url: str) -> list[str]:
    expected = expected_names(version)
    actual = {path.name for path in wheelhouse.glob("*.whl") if path.is_file()}
    if actual != expected:
        missing = sorted(expected - actual)
        extra = sorted(actual - expected)
        raise RuntimeError(f"wheelhouse does not contain the expected six wheels; missing={missing}, extra={extra}")

    published = registry_files(metadata_url)
    absent: list[str] = []
    for filename in sorted(expected):
        local_sha256 = file_digest(wheelhouse / filename)
        remote_sha256 = published.get(filename)
        if remote_sha256 is None:
            absent.append(filename)
        elif remote_sha256 != local_sha256:
            raise RuntimeError(
                f"checksum conflict for existing PyPI file {filename}: "
                f"published={remote_sha256}, expected={local_sha256}"
            )

    output.mkdir(parents=True, exist_ok=True)
    if any(output.iterdir()):
        raise RuntimeError(f"output directory is not empty: {output}")
    for filename in absent:
        shutil.copy2(wheelhouse / filename, output / filename)
    return absent


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--wheelhouse", type=Path, required=True)
    parser.add_argument("--out", type=Path, required=True)
    parser.add_argument("--version", required=True)
    parser.add_argument("--metadata-url")
    parser.add_argument("--github-output")
    args = parser.parse_args()
    metadata_url = args.metadata_url or f"https://pypi.org/pypi/git-a2a/{args.version}/json"
    try:
        absent = select_missing(args.wheelhouse, args.out, args.version, metadata_url)
    except RuntimeError as error:
        raise SystemExit(str(error)) from error

    result = {"missing": absent, "missing_count": len(absent)}
    if args.github_output:
        with open(args.github_output, "a", encoding="utf-8") as output:
            output.write(f"missing_count={len(absent)}\n")
    json.dump(result, sys.stdout, sort_keys=True)
    sys.stdout.write("\n")


if __name__ == "__main__":
    main()
