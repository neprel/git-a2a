#!/usr/bin/env python3
"""Build a deterministic cross-platform MCPB and its MCP Registry metadata."""

from __future__ import annotations

import argparse
import hashlib
import json
import pathlib
import zipfile

ROOT = pathlib.Path(__file__).resolve().parents[2]
TARGETS = (
    ("darwin", "amd64"),
    ("darwin", "arm64"),
    ("linux", "amd64"),
    ("linux", "arm64"),
    ("windows", "amd64"),
    ("windows", "arm64"),
)
MCP_NAME = "io.github.neprel/git-a2a"
ZIP_TIME = (1980, 1, 1, 0, 0, 0)


def manifest(version: str) -> dict:
    return {
        "manifest_version": "0.3",
        "name": "git-a2a",
        "display_name": "git-a2a",
        "version": version,
        "description": "Git module dependencies and owning-agent routing for coding agents",
        "author": {"name": "Andrey Neprel", "url": "https://github.com/neprel"},
        "repository": {"type": "git", "url": "https://github.com/neprel/git-a2a"},
        "homepage": "https://git-a2a.com",
        "documentation": "https://github.com/neprel/git-a2a/blob/main/docs/mcp.md",
        "server": {
            "type": "node",
            "entry_point": "server/launcher.js",
            "mcp_config": {
                "command": "node",
                "args": ["${__dirname}/server/launcher.js", "mcp"],
            },
        },
        "compatibility": {
            "platforms": ["darwin", "linux", "win32"],
            "runtimes": {"node": ">=18"},
        },
        "tools": [
            {"name": "who", "description": "Resolve owners and contacts."},
            {"name": "show", "description": "Read a module and its published surface."},
            {"name": "status", "description": "Check dependency and roster health."},
            {"name": "validate", "description": "Validate manifest and lock files."},
            {"name": "doctor", "description": "Report required local tools."},
            {"name": "explain", "description": "Read one normative field entry."},
            {"name": "usage", "description": "Read the coding-agent briefing."},
        ],
    }


def registry(version: str, bundle_name: str, digest: str) -> dict:
    return {
        "$schema": "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json",
        "name": MCP_NAME,
        "title": "git-a2a",
        "description": "Git module dependencies and owning-agent routing for coding agents",
        "repository": {"url": "https://github.com/neprel/git-a2a", "source": "github"},
        "version": version,
        "packages": [
            {
                "registryType": "npm",
                "identifier": "git-a2a",
                "version": version,
                "transport": {"type": "stdio"},
                "packageArguments": [{"type": "positional", "value": "mcp"}],
            },
            {
                "registryType": "oci",
                "identifier": f"ghcr.io/neprel/git-a2a:{version}",
                "transport": {"type": "stdio"},
                "packageArguments": [{"type": "positional", "value": "mcp"}],
            },
            {
                "registryType": "mcpb",
                "identifier": f"https://github.com/neprel/git-a2a/releases/download/v{version}/{bundle_name}",
                "fileSha256": digest,
                "transport": {"type": "stdio"},
            },
        ],
    }


def add_bytes(archive: zipfile.ZipFile, name: str, body: bytes, mode: int) -> None:
    info = zipfile.ZipInfo(name, ZIP_TIME)
    info.create_system = 3
    info.external_attr = (0o100000 | mode) << 16
    info.compress_type = zipfile.ZIP_DEFLATED
    archive.writestr(info, body)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--packages", required=True, type=pathlib.Path)
    parser.add_argument("--version", required=True)
    parser.add_argument("--out", required=True, type=pathlib.Path)
    args = parser.parse_args()
    args.out.mkdir(parents=True, exist_ok=True)
    bundle_name = f"git-a2a_{args.version}.mcpb"
    bundle = args.out / bundle_name
    with zipfile.ZipFile(bundle, "w") as archive:
        add_bytes(archive, "manifest.json", (json.dumps(manifest(args.version), indent=2) + "\n").encode(), 0o644)
        add_bytes(archive, "server/launcher.js", (ROOT / "dist/mcpb/launcher.js").read_bytes(), 0o755)
        for goos, goarch in TARGETS:
            executable = "git-a2a.exe" if goos == "windows" else "git-a2a"
            source = args.packages / f"{goos}-{goarch}" / "bin" / executable
            if not source.is_file():
                raise SystemExit(f"missing MCPB binary: {source}")
            add_bytes(archive, f"server/bin/{goos}-{goarch}/{executable}", source.read_bytes(), 0o755)
    digest = hashlib.sha256(bundle.read_bytes()).hexdigest()
    server = args.out / "server.json"
    server.write_bytes((json.dumps(registry(args.version, bundle_name, digest), indent=2) + "\n").encode())
    print(f"mcpb: wrote {bundle} sha256={digest}")
    print(f"mcpb: wrote {server}")


if __name__ == "__main__":
    main()
