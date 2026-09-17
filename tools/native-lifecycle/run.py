#!/usr/bin/env python3
"""Run pinned native adapter lifecycle cases against a dirty-tree snapshot."""

from __future__ import annotations

import argparse
import datetime as dt
import json
import os
from pathlib import Path
import platform
import shutil
import subprocess
import sys
import tempfile


HERE = Path(__file__).resolve().parent
ROOT = HERE.parent.parent
MATRIX_PATH = HERE / "matrix.json"


def command(argv: list[str], **kwargs: object) -> subprocess.CompletedProcess[bytes]:
    return subprocess.run(argv, check=False, **kwargs)  # type: ignore[arg-type]


def snapshot(destination: Path) -> int:
    listed = command(
        ["git", "ls-files", "--cached", "--others", "--exclude-standard", "-z"],
        cwd=ROOT,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    if listed.returncode:
        raise RuntimeError(listed.stderr.decode("utf-8", "replace"))
    count = 0
    for raw in listed.stdout.split(b"\0"):
        if not raw:
            continue
        relative = os.fsdecode(raw)
        source = ROOT / relative
        if not source.exists() and not source.is_symlink():
            continue
        target = destination / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        if source.is_symlink():
            target.symlink_to(os.readlink(source))
        elif source.is_dir():
            target.mkdir(parents=True, exist_ok=True)
        else:
            shutil.copy2(source, target)
        count += 1
    return count


def docker_base(matrix: dict[str, object]) -> list[str]:
    return ["docker", "run", "--rm", "--platform", str(matrix["platform"]), "--entrypoint", "/bin/sh"]


TEST_PACKAGES = {
    "cli-it": "./internal/cli",
    "submodule-it": "./adapters/submodule",
    "npm-it": "./adapters/npm",
    "golang-it": "./adapters/golang",
    "cargo-it": "./adapters/cargo",
    "swift-it": "./adapters/swift",
    "gem-it": "./adapters/gem",
    "composer-it": "./adapters/composer",
    "hex-it": "./adapters/hex",
    "hackage-it": "./adapters/hackage",
    "zig-it": "./adapters/zig",
    "clojure-it": "./adapters/clojure",
    "nix-it": "./adapters/nix",
    "cmake-it": "./adapters/cmake",
    "gradle-it": "./adapters/gradle",
    "msbuild-it": "./adapters/msbuild",
    "maven-it": "./adapters/maven",
    "meson-it": "./adapters/meson",
}


def build_binaries(matrix: dict[str, object], selected: list[dict[str, str]], source: Path, binaries: Path, log: Path) -> bool:
    binaries.mkdir(parents=True)
    selected_targets = [
        (name, package)
        for name, package in TEST_PACKAGES.items()
        if any(f"/native/bin/{name}" in case["test"] for case in selected)
    ]
    build_tests = "\n".join(
        f"CGO_ENABLED=0 go test -c -o /out/{name} {package}"
        for name, package in selected_targets
    )
    script = f"""set -eu
export GOCACHE=/tmp/go-cache GOPATH=/tmp/go HOME=/tmp/home
mkdir -p "$GOCACHE" "$GOPATH" "$HOME"
cd /src
go version
CGO_ENABLED=0 go build -trimpath -o /out/git-a2a ./cmd/git-a2a
{build_tests}
"""
    argv = docker_base(matrix) + [
        "--user", f"{os.getuid()}:{os.getgid()}",
        "-v", f"{source}:/src:ro",
        "-v", f"{binaries}:/out",
        str(matrix["builder"]), "-ceu", script,
    ]
    with log.open("wb") as stream:
        result = command(argv, stdout=stream, stderr=subprocess.STDOUT)
    return result.returncode == 0


def shell_script(case: dict[str, str]) -> str:
    prepare = case["prepare"].replace(
        "require_git",
        "command -v git >/dev/null || { apt-get update && apt-get install -y --no-install-recommends git ca-certificates; }",
    )
    return f"""set -eu
export HOME=/tmp/native-home XDG_CACHE_HOME=/tmp/native-cache
mkdir -p "$HOME" "$XDG_CACHE_HOME"
cd /workspace/adapters
{prepare}
command -v {case['tool']}
echo NATIVE_VERSION_BEGIN
{case['version']}
echo NATIVE_VERSION_END
echo 'NATIVE_TEST_COMMAND={case['test']}'
{case['test']}
"""


def observed_version(text: str) -> str:
    # The diagnostic command line contains the shell script itself, including
    # marker literals. The last begin marker is the one emitted by the running
    # container, not the earlier argv rendering.
    start = text.rfind("NATIVE_VERSION_BEGIN\n")
    end = text.find("\nNATIVE_VERSION_END", start + 1)
    if start < 0 or end < 0:
        return "unknown"
    value = text[start + len("NATIVE_VERSION_BEGIN\n"):end]
    return " / ".join(line.strip() for line in value.splitlines() if line.strip())


def run_case(matrix: dict[str, object], case: dict[str, str], source: Path, binaries: Path, log: Path) -> tuple[str, str, str]:
    dockerfile = case.get("dockerfile")
    if dockerfile:
        build_context = source / case.get("build_context", ".")
        argv = [
            "docker", "build", "--platform", str(matrix["platform"]),
            "--file", str(source / dockerfile), "--tag", case["image"],
            str(build_context),
        ]
        with log.open("wb") as stream:
            stream.write(("$ " + " ".join(argv) + "\n").encode())
            stream.flush()
            built = command(argv, stdout=stream, stderr=subprocess.STDOUT)
        if built.returncode:
            return "BLOCKED", "pinned toolchain image build unavailable", "unknown"
    inspect = command(
        ["docker", "image", "inspect", "--format", "{{json .RepoDigests}}", case["image"]],
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
    )
    with log.open("ab" if dockerfile else "wb") as stream:
        if inspect.returncode == 0:
            stream.write(b"$ docker image inspect " + case["image"].encode() + b"\n")
            stream.write(inspect.stdout)
        else:
            pull = command(["docker", "pull", "--platform", str(matrix["platform"]), case["image"]], stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
            stream.write(b"$ docker pull " + case["image"].encode() + b"\n")
            stream.write(pull.stdout)
            if pull.returncode:
                return "BLOCKED", "container image unavailable", "unknown"
        argv = docker_base(matrix) + [
            "--network", case.get("network", "bridge"),
            "-e", "GIT_CONFIG_NOSYSTEM=1",
            "-e", f"GIT_ALLOW_PROTOCOL={case.get('git_allow_protocol', 'file:git')}",
            "-e", "PUB_CACHE=/tmp/pub-cache",
            "-e", "GITA2A_CLI_BIN=/native/bin/git-a2a",
            "-e", "GITA2A_IT=1",
            "-e", "GITA2A_NATIVE_REQUIRED=1",
            "-e", "GITA2A_IT_SUBMODULE_CLI=1",
            "-e", "GITA2A_IT_NPM_CLI=1",
            "-e", "GITA2A_IT_GO_CLI=1",
            "-e", "GITA2A_IT_CARGO_CLI=1",
            "-e", "GITA2A_IT_CARGO_VERSION=cargo 1.89.0 (c24e10642 2025-06-23)",
            "-e", "GITA2A_IT_SWIFT=1",
            "-e", "GITA2A_IT_GEM_CLI=1",
            "-e", "GITA2A_NATIVE_COMPOSER=1",
            "-e", "GITA2A_COMPOSER_LIFECYCLE=/workspace/adapters/composer/native/lifecycle.sh",
            "-e", "GITA2A_NATIVE_HEX=1",
            "-e", f"GITA2A_NATIVE_HACKAGE_IT={case['filter']}",
            "-e", "GITA2A_IT_ZIG_CLI=1",
            "-e", "GITA2A_IT_CLOJURE_CLI=1",
            "-e", "GITA2A_IT_NIX=1",
            "-e", f"GITA2A_IT_ECOSYSTEM={case['filter']}",
            "-e", f"GITA2A_NATIVE_CASE={case['filter']}",
            "-e", f"GITA2A_NATIVE_BUILD_IT={case['filter']}",
            "-v", f"{source}:/workspace:ro",
            "-v", f"{binaries}:/native/bin:ro",
            case["image"], "-ceu", shell_script(case),
        ]
        stream.write(("\n$ " + " ".join(argv) + "\n").encode())
        stream.flush()
        run = command(argv, stdout=stream, stderr=subprocess.STDOUT)
    text = log.read_text("utf-8", "replace")
    version = observed_version(text)
    if run.returncode:
        return "FAIL", f"setup or test exited {run.returncode}", version
    if "--- SKIP:" in text or case["expect"] not in text:
        return "FAIL", "required test skipped or expected case did not execute", version
    return "PASS", "native command and expected lifecycle test passed", version


def write_summaries(output: Path, matrix: dict[str, object], results: list[dict[str, str]]) -> None:
    payload = {
        "schema": 1,
        "generated_at": dt.datetime.now(dt.timezone.utc).isoformat(),
        "host": f"{platform.system()}/{platform.machine()}",
        "target": matrix["platform"],
        "builder": matrix["builder"],
        "results": results,
    }
    (output / "summary.json").write_text(json.dumps(payload, indent=2) + "\n", encoding="utf-8")
    lines = [
        "# Native lifecycle result",
        "",
        f"Target: `{matrix['platform']}`. Builder: `{matrix['builder']}`.",
        "",
        "| Family | Variant | Image | Observed version | Kind | Command | Status | Detail | Log |",
        "| --- | --- | --- | --- | --- | --- | --- | --- | --- |",
    ]
    for item in results:
        lines.append(
            f"| {item['family']} | {item['variant']} | `{item['image']}` | {item['observed_version']} | "
            f"{item['kind']} | `{item['command']}` | {item['status']} | {item['detail']} | `{item['log']}` |"
        )
    (output / "summary.md").write_text("\n".join(lines) + "\n", encoding="utf-8")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--case", action="append", default=[], help="matrix case id; repeatable")
    parser.add_argument("--group", choices=("all", "core", "python"), default="all")
    parser.add_argument("--list", action="store_true", help="print the matrix without Docker")
    parser.add_argument("--keep-going", action="store_true", help="continue after FAIL/BLOCKED")
    parser.add_argument("--output", type=Path, help="result directory (default: build/native-lifecycle/<UTC>)")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    matrix = json.loads(MATRIX_PATH.read_text(encoding="utf-8"))
    cases: list[dict[str, str]] = matrix["cases"]
    if args.list:
        for case in cases:
            print(f"{case['id']}\t{case['family']}\t{case['variant']}\t{case['image']}\t{case['kind']}")
        return 0
    requested = set(args.case)
    known = {case["id"] for case in cases}
    unknown = sorted(requested - known)
    if unknown:
        print("unknown native case(s): " + ", ".join(unknown), file=sys.stderr)
        return 2
    selected = [
        case for case in cases
        if case["id"] in requested
        or (not requested and (args.group == "all" or case["group"] == args.group or (args.group == "core" and case["group"] == "core")))
    ]
    timestamp = dt.datetime.now(dt.timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    output = (args.output or ROOT / "build" / "native-lifecycle" / timestamp).resolve()
    output.mkdir(parents=True, exist_ok=True)
    results_by_id: dict[str, dict[str, str]] = {
        case["id"]: {
            **{key: case[key] for key in ("family", "variant", "image", "kind")},
            "command": case["test"],
            "version_command": case["version"],
            "observed_version": "not run",
            "status": "NOT RUN",
            "detail": "not selected",
            "log": "",
        }
        for case in cases
    }
    ordered_results = lambda: [results_by_id[case["id"]] for case in cases]
    if shutil.which("docker") is None or command(["docker", "info"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode:
        for case in selected:
            results_by_id[case["id"]].update(status="BLOCKED", detail="Docker daemon unavailable")
        write_summaries(output, matrix, ordered_results())
        print(f"Docker daemon unavailable; summary: {output / 'summary.md'}", file=sys.stderr)
        return 1
    with tempfile.TemporaryDirectory(prefix="git-a2a-native-") as temporary:
        temporary_path = Path(temporary)
        source = temporary_path / "source"
        source.mkdir()
        copied = snapshot(source)
        binaries = temporary_path / "bin"
        build_log = output / "build.log"
        print(f"snapshot: {copied} files; build log: {build_log}")
        if not build_binaries(matrix, selected, source, binaries, build_log):
            for case in selected:
                results_by_id[case["id"]].update(status="FAIL", detail="Linux test-binary build failed", log=str(build_log))
            write_summaries(output, matrix, ordered_results())
            print(f"Linux binary build failed: {build_log}", file=sys.stderr)
            return 1
        for case in selected:
            log = output / f"{case['id']}.log"
            status, detail, version = run_case(matrix, case, source, binaries, log)
            results_by_id[case["id"]].update(status=status, detail=detail, observed_version=version, log=str(log))
            print(f"{status:7} {case['id']}: {detail} ({log})")
            write_summaries(output, matrix, ordered_results())
            if status != "PASS" and not args.keep_going:
                reached = False
                for remaining in selected:
                    if reached:
                        results_by_id[remaining["id"]].update(detail=f"stopped after {case['id']}")
                    elif remaining["id"] == case["id"]:
                        reached = True
                break
    results = ordered_results()
    write_summaries(output, matrix, results)
    print(f"summary: {output / 'summary.md'}")
    selected_statuses = [results_by_id[case["id"]]["status"] for case in selected]
    return 0 if selected_statuses and all(status == "PASS" for status in selected_statuses) else 1


if __name__ == "__main__":
    raise SystemExit(main())
