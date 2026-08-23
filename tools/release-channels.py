#!/usr/bin/env python3
"""Render Homebrew and Scoop manifests from immutable GitHub release checksums."""

from __future__ import annotations

import argparse
import json
import re
from pathlib import Path


SHA256 = re.compile(r"[0-9a-f]{64}")
VERSION = re.compile(r"[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?")


def checksum_table(path: Path) -> dict[str, str]:
    result: dict[str, str] = {}
    for line in path.read_text(encoding="utf-8").splitlines():
        fields = line.split(None, 1)
        if len(fields) == 2 and SHA256.fullmatch(fields[0]):
            result[fields[1].lstrip("*")] = fields[0]
    return result


def required_checksum(checksums: dict[str, str], filename: str) -> str:
    try:
        return checksums[filename]
    except KeyError as error:
        raise SystemExit(f"release checksum missing for {filename}") from error


def render_formula(version: str, checksums: dict[str, str]) -> str:
    amd64_name = f"git-a2a_brew_{version}_darwin_amd64.tar.gz"
    arm64_name = f"git-a2a_brew_{version}_darwin_arm64.tar.gz"
    return f'''class GitA2a < Formula
  desc "Import git modules together with their owning agents"
  homepage "https://github.com/neprel/git-a2a"
  version "{version}"
  license "Apache-2.0"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/neprel/git-a2a/releases/download/v{version}/{arm64_name}"
      sha256 "{required_checksum(checksums, arm64_name)}"
    else
      url "https://github.com/neprel/git-a2a/releases/download/v{version}/{amd64_name}"
      sha256 "{required_checksum(checksums, amd64_name)}"
    end
  end

  def install
    bin.install "git-a2a"
    system "/bin/sh", "-c", 'if /usr/bin/xattr -p com.apple.quarantine "$1" >/dev/null 2>&1; then exec /usr/bin/xattr -d com.apple.quarantine "$1"; fi', "git-a2a", bin/"git-a2a"
  end

  test do
    assert_match "git-a2a {version}", shell_output("#{{bin}}/git-a2a --version")
  end
end
'''


def render_scoop(version: str, checksums: dict[str, str]) -> str:
    amd64_name = f"git-a2a_scoop_{version}_windows_amd64.zip"
    arm64_name = f"git-a2a_scoop_{version}_windows_arm64.zip"
    manifest = {
        "version": version,
        "architecture": {
            "64bit": {
                "url": f"https://github.com/neprel/git-a2a/releases/download/v{version}/{amd64_name}",
                "bin": ["git-a2a.exe"],
                "hash": required_checksum(checksums, amd64_name),
            },
            "arm64": {
                "url": f"https://github.com/neprel/git-a2a/releases/download/v{version}/{arm64_name}",
                "bin": ["git-a2a.exe"],
                "hash": required_checksum(checksums, arm64_name),
            },
        },
        "homepage": "https://github.com/neprel/git-a2a",
        "description": "Import git modules together with their owning agents",
    }
    return json.dumps(manifest, indent=4) + "\n"


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--tag", required=True)
    parser.add_argument("--checksums", required=True, type=Path)
    parser.add_argument("--homebrew", required=True, type=Path)
    parser.add_argument("--scoop", required=True, type=Path)
    args = parser.parse_args()

    version = args.tag.removeprefix("v")
    if not VERSION.fullmatch(version):
        raise SystemExit(f"invalid release tag: {args.tag}")
    checksums = checksum_table(args.checksums)
    args.homebrew.parent.mkdir(parents=True, exist_ok=True)
    args.scoop.parent.mkdir(parents=True, exist_ok=True)
    args.homebrew.write_text(render_formula(version, checksums), encoding="utf-8")
    args.scoop.write_text(render_scoop(version, checksums), encoding="utf-8")


if __name__ == "__main__":
    main()
