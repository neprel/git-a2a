#!/usr/bin/env python3
"""Build the npm launcher and per-platform packages from GoReleaser archives."""
import argparse
import json
import shutil
import tarfile
import zipfile
from pathlib import Path

TARGETS = {
    ("darwin", "amd64"): ("darwin", "x64"),
    ("darwin", "arm64"): ("darwin", "arm64"),
    ("linux", "amd64"): ("linux", "x64"),
    ("linux", "arm64"): ("linux", "arm64"),
    ("windows", "amd64"): ("win32", "x64"),
    ("windows", "arm64"): ("win32", "arm64"),
}

def extract_binary(archive: Path, destination: Path) -> None:
    destination.parent.mkdir(parents=True, exist_ok=True)
    if archive.suffix == ".zip":
        with zipfile.ZipFile(archive) as source:
            member = next(name for name in source.namelist() if name.endswith("git-a2a.exe"))
            destination.write_bytes(source.read(member))
    else:
        with tarfile.open(archive, "r:gz") as source:
            member = next(item for item in source.getmembers() if item.name.endswith("git-a2a"))
            destination.write_bytes(source.extractfile(member).read())
    destination.chmod(0o755)

def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--artifacts", type=Path, required=True)
    parser.add_argument("--version", required=True)
    parser.add_argument("--out", type=Path, required=True)
    args = parser.parse_args()
    args.out.mkdir(parents=True, exist_ok=True)
    optional = {}
    for (goos, goarch), (node_os, node_cpu) in TARGETS.items():
        matches = list(args.artifacts.glob(f"git-a2a_*_{goos}_{goarch}.tar.gz")) + list(args.artifacts.glob(f"git-a2a_*_{goos}_{goarch}.zip"))
        if len(matches) != 1:
            raise SystemExit(f"expected one archive for {goos}/{goarch}, found {matches}")
        name = f"@git-a2a/{goos}-{goarch}"
        package = args.out / f"{goos}-{goarch}"
        binary = package / "bin" / ("git-a2a.exe" if goos == "windows" else "git-a2a")
        extract_binary(matches[0], binary)
        (package / "package.json").write_text(json.dumps({
            "name": name, "version": args.version, "license": "MIT",
            "os": [node_os], "cpu": [node_cpu], "files": ["bin/"]
        }, indent=2) + "\n")
        optional[name] = args.version
    root = args.out / "git-a2a"
    (root / "bin").mkdir(parents=True)
    shutil.copy2(Path(__file__).parent / "bin" / "git-a2a.js", root / "bin" / "git-a2a.js")
    (root / "package.json").write_text(json.dumps({
        "name": "git-a2a", "version": args.version,
        "description": "Import git modules together with their owning agents",
        "license": "MIT", "bin": {"git-a2a": "bin/git-a2a.js"},
        "engines": {"node": ">=18"}, "optionalDependencies": optional
    }, indent=2) + "\n")

if __name__ == "__main__":
    main()
