#!/usr/bin/env python3
"""Build platform wheels containing the exact GoReleaser binaries."""
import argparse
import base64
import csv
import hashlib
import io
import tarfile
import zipfile
from pathlib import Path

TARGETS = {
    ("darwin", "amd64"): "macosx_10_15_x86_64",
    ("darwin", "arm64"): "macosx_11_0_arm64",
    ("linux", "amd64"): "manylinux_2_17_x86_64",
    ("linux", "arm64"): "manylinux_2_17_aarch64",
    ("windows", "amd64"): "win_amd64",
    ("windows", "arm64"): "win_arm64",
}

LAUNCHER = '''from importlib.resources import files\nimport os, subprocess, sys\ndef main():\n    binary = files("git_a2a_bin").joinpath("git-a2a.exe" if os.name == "nt" else "git-a2a")\n    raise SystemExit(subprocess.call([str(binary), *sys.argv[1:]]))\n'''

def binary_from(archive: Path, windows: bool) -> bytes:
    suffix = "git-a2a.exe" if windows else "git-a2a"
    if archive.suffix == ".zip":
        with zipfile.ZipFile(archive) as source:
            return source.read(next(name for name in source.namelist() if name.endswith(suffix)))
    with tarfile.open(archive, "r:gz") as source:
        item = next(item for item in source.getmembers() if item.name.endswith(suffix))
        return source.extractfile(item).read()

def digest(data: bytes) -> str:
    return "sha256=" + base64.urlsafe_b64encode(hashlib.sha256(data).digest()).rstrip(b"=").decode()

def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--artifacts", type=Path, required=True)
    parser.add_argument("--version", required=True)
    parser.add_argument("--out", type=Path, required=True)
    args = parser.parse_args(); args.out.mkdir(parents=True, exist_ok=True)
    for (goos, goarch), platform in TARGETS.items():
        matches = list(args.artifacts.glob(f"git-a2a_pypi_*_{goos}_{goarch}.tar.gz")) + list(args.artifacts.glob(f"git-a2a_pypi_*_{goos}_{goarch}.zip"))
        if len(matches) != 1: raise SystemExit(f"expected one archive for {goos}/{goarch}, found {matches}")
        tag = f"py3-none-{platform}"; dist_info = f"git_a2a-{args.version}.dist-info"
        files = {
            "git_a2a.py": LAUNCHER.encode(), "git_a2a_bin/__init__.py": b"",
            "git_a2a_bin/" + ("git-a2a.exe" if goos == "windows" else "git-a2a"): binary_from(matches[0], goos == "windows"),
            f"{dist_info}/METADATA": f"Metadata-Version: 2.4\nName: git-a2a\nVersion: {args.version}\nSummary: Import git modules together with their owning agents\nLicense-Expression: MIT\nRequires-Python: >=3.9\n\n".encode(),
            f"{dist_info}/WHEEL": f"Wheel-Version: 1.0\nGenerator: git-a2a\nRoot-Is-Purelib: false\nTag: {tag}\n".encode(),
            f"{dist_info}/entry_points.txt": b"[console_scripts]\ngit-a2a = git_a2a:main\n",
        }
        record = io.StringIO(); writer = csv.writer(record, lineterminator="\n")
        for name, data in files.items(): writer.writerow([name, digest(data), len(data)])
        record_name = f"{dist_info}/RECORD"; writer.writerow([record_name, "", ""]); files[record_name] = record.getvalue().encode()
        wheel = args.out / f"git_a2a-{args.version}-{tag}.whl"
        with zipfile.ZipFile(wheel, "w", zipfile.ZIP_DEFLATED) as target:
            for name, data in files.items():
                info = zipfile.ZipInfo(name, (1980, 1, 1, 0, 0, 0)); info.compress_type = zipfile.ZIP_DEFLATED
                info.external_attr = (0o755 if name.endswith(("git-a2a", ".exe")) else 0o644) << 16
                target.writestr(info, data)

if __name__ == "__main__":
    main()
