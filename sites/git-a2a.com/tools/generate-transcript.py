#!/usr/bin/env python3
from __future__ import annotations

import html
import difflib
import json
import os
import pathlib
import re
import shutil
import subprocess
import sys
import tempfile
import time
import urllib.request

ROOT = pathlib.Path(__file__).resolve().parents[3]
SITE = ROOT / "sites/git-a2a.com"
FIXTURE = SITE / "tools/transcript-fixture"
PUBLIC_URL = "https://github.com/neprel/git-a2a-demo-acme-lib"
NPM_URL = "ssh://git@github.com/neprel/git-a2a-demo-acme-lib.git"
CARD_PORT = 18765


def capture(arguments: list[str], cwd: pathlib.Path, environment: dict[str, str]) -> dict[str, str]:
    result = subprocess.run(arguments, cwd=cwd, env=environment, capture_output=True)
    if result.returncode != 0:
        raise RuntimeError(
            f"{arguments!r} exited {result.returncode}\n"
            f"stdout:\n{result.stdout.decode('utf-8', errors='replace')}\n"
            f"stderr:\n{result.stderr.decode('utf-8', errors='replace')}"
        )
    return {"stdout": result.stdout.decode("utf-8"), "stderr": result.stderr.decode("utf-8")}


def run(arguments: list[str], cwd: pathlib.Path, environment: dict[str, str]) -> str:
    result = capture(arguments, cwd, environment)
    return result["stdout"] + result["stderr"]


def exact_lines(raw: str) -> list[str]:
    if raw == "":
        return []
    return raw.splitlines()


def rendered_lines(group: dict[str, object]) -> list[tuple[str, str]]:
    lines: list[tuple[str, str]] = []
    rendered = group.get("render", [])
    assert isinstance(rendered, list)
    for raw_segment in rendered:
        assert isinstance(raw_segment, dict)
        stream = str(raw_segment["stream"])
        classes = raw_segment["classes"]
        assert isinstance(classes, list)
        raw = group[stream]
        assert isinstance(raw, str)
        values = exact_lines(raw)
        if len(values) != len(classes):
            raise RuntimeError(f"{group['command']}: {stream} has {len(values)} lines for {len(classes)} classes")
        lines.extend(zip(values, (str(value) for value in classes)))
    return lines


def render_plan(result: dict[str, str], classes: list[str], streams: tuple[str, ...] = ("stdout", "stderr")) -> list[dict[str, object]]:
    plan: list[dict[str, object]] = []
    offset = 0
    for stream in streams:
        count = len(exact_lines(result[stream]))
        if count:
            plan.append({"stream": stream, "classes": classes[offset:offset + count]})
        offset += count
    if offset != len(classes):
        raise RuntimeError(f"captured {offset} displayed lines for {len(classes)} classes")
    return plan


def normalized(transcript: dict[str, object]) -> str:
    value = json.dumps(transcript, indent=2, ensure_ascii=False, sort_keys=True)
    return re.sub(r"(added acme-lib-utils at )[0-9a-f]{40}", r"\1<COMMIT>", value)


def fallback(transcript: dict[str, object]) -> str:
    lines: list[str] = []
    groups = transcript["groups"]
    assert isinstance(groups, list)
    for index, raw_group in enumerate(groups):
        assert isinstance(raw_group, dict)
        command = html.escape(str(raw_group["command"]))
        comment = raw_group.get("comment")
        suffix = ""
        if comment:
            suffix = f'<span class="term-comment">  {html.escape(str(comment))}</span>'
        lines.append(f'<div class="term-line command"><span class="prompt">$ </span>{command}{suffix}</div>')
        for text, css_class in rendered_lines(raw_group):
            lines.append(f'<div class="term-line {css_class}">{html.escape(text)}</div>')
        if index < len(groups) - 1:
            lines.append('<div class="term-line blank"></div>')
    lines.append('<div class="term-line command"><span class="prompt">$ </span><span class="caret"></span></div>')
    return "".join(lines)


def main() -> None:
    check = len(sys.argv) == 2 and sys.argv[1] == "--check"
    if len(sys.argv) > 2 or (len(sys.argv) == 2 and not check):
        raise SystemExit("usage: generate-transcript.py [--check]")
    network = os.environ.get("SITE_NET") == "1"
    port = CARD_PORT
    card_url = f"http://127.0.0.1:{port}/acme-lib-utils/.well-known/agent-card.json"
    server = subprocess.Popen(
        [sys.executable, "-m", "http.server", str(port), "--bind", "127.0.0.1", "--directory", str(FIXTURE / "cards")],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )
    try:
        for _ in range(50):
            try:
                with urllib.request.urlopen(card_url, timeout=0.2) as response:
                    if response.status == 200:
                        break
            except OSError:
                time.sleep(0.05)
        else:
            raise RuntimeError("local card server did not start")

        with tempfile.TemporaryDirectory(prefix="git-a2a-transcript-") as temporary:
            work = pathlib.Path(temporary)
            source = work / "source"
            bare = work / "library.git"
            consumer = work / "consumer-app"
            shutil.copytree(FIXTURE / "library", source)
            shutil.copytree(FIXTURE / "consumer", consumer)
            manifest_path = source / "a2amodule.yml"
            manifest_path.write_text(manifest_path.read_text().replace("__CARD_PORT__", str(port)))

            environment = os.environ.copy()
            environment.update({
                "GIT_AUTHOR_DATE": "2026-08-22T12:00:00Z",
                "GIT_COMMITTER_DATE": "2026-08-22T12:00:00Z",
                "GIT_CONFIG_GLOBAL": os.devnull,
                "GONOSUMDB": "github.com/neprel/*",
                "GOPRIVATE": "github.com/neprel/*",
                "GOPROXY": "direct",
                "GOSUMDB": "off",
            })
            run(["git", "init", "-b", "main"], consumer, environment)
            if not network:
                run(["git", "init", "-b", "main"], source, environment)
                run(["git", "config", "user.name", "Transcript Fixture"], source, environment)
                run(["git", "config", "user.email", "fixture@example.invalid"], source, environment)
                run(["git", "add", "."], source, environment)
                run(["git", "commit", "-m", "fixture"], source, environment)
                run(["git", "clone", "--bare", str(source), str(bare)], work, environment)
                local_url = "file://" + str(bare)
                run(["git", "config", f"url.{local_url}.insteadOf", PUBLIC_URL], consumer, environment)
                run(["git", "config", "--add", f"url.{local_url}.insteadOf", NPM_URL], consumer, environment)
                # Go invokes Git from its module cache, outside the consumer repository.
                # Repeat the rewrite through process-scoped config for those child processes.
                environment.update({
                    "GIT_CONFIG_COUNT": "2",
                    "GIT_CONFIG_KEY_0": f"url.{local_url}.insteadOf",
                    "GIT_CONFIG_VALUE_0": PUBLIC_URL,
                    "GIT_CONFIG_KEY_1": f"url.{local_url}.insteadOf",
                    "GIT_CONFIG_VALUE_1": NPM_URL,
                })

            binary = work / "git-a2a"
            commit = run(["git", "rev-parse", "HEAD"], ROOT, environment).strip()
            ldflags = (
                f"-s -w -X github.com/neprel/git-a2a/internal/cli.Commit={commit} "
                "-X github.com/neprel/git-a2a/internal/cli.Channel=binary"
            )
            run(["go", "build", "-trimpath", "-ldflags", ldflags, "-o", str(binary), "./cmd/git-a2a"], ROOT, environment)

            commands = [
                ("git-a2a init", [str(binary), "init"]),
                (f"git-a2a add {PUBLIC_URL}", [str(binary), "add", PUBLIC_URL]),
                ("git-a2a who acme-lib-utils --intent change", [str(binary), "who", "acme-lib-utils", "--intent", "change"]),
                ("git-a2a status", [str(binary), "status"]),
            ]
            results = [capture(arguments, consumer, environment) for _, arguments in commands[:2]]
            run(["go", "mod", "tidy"], consumer, environment)
            results.extend(capture(arguments, consumer, environment) for _, arguments in commands[2:])
            lines = [exact_lines(result["stdout"] + result["stderr"]) for result in results]
            complete = "\n".join(line for group in lines for line in group).lower()
            if "warning:" in complete or "unhealthy" in complete:
                raise RuntimeError("transcript contains a warning or unhealthy verdict")
            expected_status = [
                "MODULE", "acme-lib-utils", "consumer-app: manifest valid · agents none · roster none", "1 dependency: clean",
            ]
            if len(lines[3]) != 4 or any(not line.startswith(prefix) for line, prefix in zip(lines[3], expected_status)):
                raise RuntimeError("unexpected status transcript:\n" + "\n".join(lines[3]))
            for required in ("npm clean", "pypi clean", "golang clean", "up", "none"):
                if required not in lines[3][1]:
                    raise RuntimeError(f"status dependency row lacks {required!r}: {lines[3][1]}")
            lock_expectations = {
                "package-lock.json": "@acme/lib-utils",
                "uv.lock": "acme-lib-utils",
                "go.sum": "github.com/neprel/git-a2a-demo-acme-lib",
            }
            for name, marker in lock_expectations.items():
                lock_path = consumer / name
                if not lock_path.exists() or marker not in lock_path.read_text():
                    raise RuntimeError(f"offline Refresh did not materialize {name} with {marker}")

            transcript = {
                "timing": {"initial": 300, "character": 19, "afterCommand": 260, "betweenOutput": 170, "betweenGroups": 420},
                "groups": [
                    {"command": commands[0][0], "comment": "# describe this repository", **results[0], "render": render_plan(results[0], ["plain"] * len(lines[0]))},
                    {"command": commands[1][0], **results[1], "render": render_plan(results[1], ["accent", "dim"])},
                    {"command": commands[2][0], **results[2], "render": render_plan(results[2], ["plain", "dim"], ("stdout",))},
                    {"command": commands[3][0], **results[3], "render": render_plan(results[3], ["dim", "plain", "dim", "dim"])},
                ],
            }
            if check:
                current = json.loads((SITE / "assets/transcript.json").read_text())
                expected, actual = normalized(transcript), normalized(current)
                if expected != actual:
                    difference = "\n".join(difflib.unified_diff(
                        actual.splitlines(), expected.splitlines(), fromfile="assets/transcript.json", tofile="fresh fixture", lineterm=""
                    ))
                    raise RuntimeError("published transcript differs from fresh fixture output:\n" + difference)
                print("transcript-check: every captured line matches a fresh fixture run (commit normalized)")
                return
            (SITE / "assets/transcript.json").write_text(json.dumps(transcript, indent=2, ensure_ascii=False) + "\n")

            page_path = SITE / "index.html"
            page = page_path.read_text()
            body_start = page.index('<div class="terminal-body" id="terminal-body">')
            content_start = body_start + len('<div class="terminal-body" id="terminal-body">')
            next_section = page.index('\n  <section class="section alt" id="idea">', content_start)
            container_close = page.rfind("\n  </div></section>", content_start, next_section)
            page = page[:content_start] + fallback(transcript) + "</div></div>" + page[container_close:]
            compact = json.dumps(transcript, separators=(",", ":"), ensure_ascii=False)
            page = re.sub(
                r'<script id="transcript-data" type="application/json">.*?</script>',
                lambda _: f'<script id="transcript-data" type="application/json">{compact}</script>',
                page,
                flags=re.S,
            )
            page_path.write_text(page)
            if network:
                print("transcript: generated from the public demo repository and public A2A cards")
            else:
                print("transcript: generated from the byte-equivalent local bare fixture and two live local A2A cards")
            print("\n".join(lines[3]))
    finally:
        server.terminate()
        server.wait(timeout=5)


if __name__ == "__main__":
    main()
