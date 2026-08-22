#!/usr/bin/env python3
from __future__ import annotations

import html.parser
import json
import os
import pathlib
import re
import subprocess
import sys
import tempfile
from urllib.parse import urlsplit

ROOT = pathlib.Path(__file__).resolve().parents[3]
SITE = ROOT / "sites" / "git-a2a.com"
PAGES = [SITE / "index.html", SITE / "ext/module/v1/index.html", SITE / "schema/index.html"]


def fail(message: str) -> None:
    print(f"site-check: {message}", file=sys.stderr)
    raise SystemExit(1)


class Document(html.parser.HTMLParser):
    def __init__(self, path: pathlib.Path) -> None:
        super().__init__(convert_charrefs=True)
        self.path = path
        self.links: list[str] = []
        self.text: list[str] = []
        self.code_text: list[str] = []
        self.stack: list[str] = []
        self.ids: set[str] = set()
        self.errors: list[str] = []

    def handle_starttag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        values = dict(attrs)
        if values.get("id"):
            if values["id"] in self.ids:
                self.errors.append(f"duplicate id {values['id']}")
            self.ids.add(values["id"] or "")
        for name in ("href", "src"):
            if values.get(name):
                self.links.append(values[name] or "")
        if tag not in {"area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta", "source", "track", "wbr", "path", "circle"}:
            self.stack.append(tag)

    def handle_startendtag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        depth = len(self.stack)
        self.handle_starttag(tag, attrs)
        if len(self.stack) > depth and self.stack[-1] == tag:
            self.stack.pop()

    def handle_endtag(self, tag: str) -> None:
        if not self.stack or self.stack[-1] != tag:
            self.errors.append(f"unexpected closing tag {tag}")
            return
        self.stack.pop()

    def handle_data(self, data: str) -> None:
        if not any(tag in self.stack for tag in ("script", "style")):
            self.text.append(data)
        if any(tag in self.stack for tag in ("code", "pre")):
            self.code_text.append(data)


def block(body: str, name: str) -> str:
    match = re.search(rf"<!-- {name}:start -->\n(.*?)\n<!-- {name}:end -->", body, re.S)
    if not match:
        fail(f"{name} markers missing")
    return match.group(1)


def resolve(page: pathlib.Path, link: str) -> pathlib.Path | None:
    parsed = urlsplit(link)
    if parsed.scheme or parsed.netloc or link.startswith("#"):
        return None
    target = parsed.path
    if target.startswith("/"):
        candidate = SITE / target.lstrip("/")
    else:
        candidate = page.parent / target
    if target.endswith("/") or candidate.is_dir():
        candidate /= "index.html"
    return candidate.resolve()


def main() -> None:
    if (ROOT / "install.sh").read_bytes() != (SITE / "install.sh").read_bytes():
        fail("install.sh copies differ")
    pairs = [
        (ROOT / "spec/schema/a2amodule.schema.json", SITE / "schema/a2amodule.v1.json"),
        (ROOT / "spec/schema/a2amodule.lock.schema.json", SITE / "schema/a2amodule-lock.v1.json"),
    ]
    for source, public in pairs:
        if source.read_bytes() != public.read_bytes():
            fail(f"schema copy differs: {public.name}")

    with tempfile.TemporaryDirectory() as temporary:
        package = pathlib.Path(temporary) / "public"
        subprocess.run(
            [str(ROOT / "scripts/site-package.sh"), str(package)],
            cwd=ROOT,
            check=True,
            stdout=subprocess.DEVNULL,
        )
        expected_top_level = {
            "index.html", "404.html", "robots.txt", "install.sh", "install.ps1",
            "assets", "fonts", "ext", "schema",
        }
        packaged_top_level = {path.name for path in package.iterdir()}
        if packaged_top_level != expected_top_level:
            fail(f"publication allowlist differs: {sorted(packaged_top_level)}")
        for excluded in ("README.md", "tools", "CNAME", ".nojekyll"):
            if (package / excluded).exists():
                fail(f"operator-only path was packaged: {excluded}")

    with tempfile.TemporaryDirectory() as temporary:
        test_root = pathlib.Path(temporary)
        fake_bin = test_root / "bin"
        fake_bin.mkdir()
        arguments = test_root / "scp-arguments"
        fake_scp = fake_bin / "scp"
        fake_scp.write_text('#!/bin/sh\nprintf \'%s\\n\' "$@" > "$SITE_PUBLISH_TEST_ARGS"\n')
        fake_scp.chmod(0o755)
        environment_file = test_root / ".env"
        environment_file.write_text(
            "SITE_DEPLOY_HOST=example.invalid\n"
            "SITE_DEPLOY_USER=deploy\n"
            "SITE_DEPLOY_PORT=22\n"
            "SITE_DEPLOY_PATH=/public\n"
        )
        environment = os.environ.copy()
        environment.update({
            "PATH": f"{fake_bin}{os.pathsep}{environment['PATH']}",
            "SITE_DEPLOY_ENV_FILE": str(environment_file),
            "SITE_PUBLISH_TEST_ARGS": str(arguments),
        })
        subprocess.run(
            [str(ROOT / "scripts/site-publish.sh")],
            cwd=ROOT,
            check=True,
            env=environment,
            stdout=subprocess.DEVNULL,
        )
        scp_arguments = arguments.read_text().splitlines()
        if scp_arguments[-1] != "site-production:/public":
            fail("manual publication does not use the private SSH alias and configured path")
        if not any(argument.endswith("/public/.") for argument in scp_arguments):
            fail("manual publication does not upload the staged allowlist")

    bodies = [page.read_text() for page in PAGES]
    for name in ("site-header", "site-footer"):
        values = [block(body, name) for body in bodies]
        if len(set(values)) != 1:
            fail(f"{name} differs across the three pages")

    documents: list[Document] = []
    for page, body in zip(PAGES + [SITE / "404.html"], bodies + [(SITE / "404.html").read_text()]):
        parser = Document(page)
        documents.append(parser)
        try:
            parser.feed(body)
            parser.close()
        except Exception as error:
            fail(f"invalid HTML in {page.relative_to(ROOT)}: {error}")
        if parser.stack or parser.errors:
            fail(f"invalid HTML structure in {page.relative_to(ROOT)}: {parser.errors or parser.stack}")
        prose = " ".join(parser.text)
        if "!" in prose:
            fail(f"exclamation mark in prose: {page.relative_to(ROOT)}")
        for link in parser.links:
            target = resolve(page, link)
            if target is not None and not target.exists():
                fail(f"broken link in {page.relative_to(ROOT)}: {link}")

    index = bodies[0]
    if re.search(r"(?:googleapis|gstatic|fonts\.google)", index + (SITE / "assets/site.css").read_text(), re.I):
        fail("remote Google font request found")
    if "<text" in (SITE / "assets/wordmark.svg").read_text():
        fail("wordmark must contain outlined glyph paths")
    if "plans/PILOT-PRIVATE" in "\n".join(bodies):
        fail("private pilot material found")

    embedded_match = re.search(r'<script id="transcript-data" type="application/json">(.*?)</script>', index, re.S)
    if not embedded_match:
        fail("embedded file-mode transcript is missing")
    embedded = json.loads(embedded_match.group(1))
    transcript = json.loads((SITE / "assets/transcript.json").read_text())
    if embedded != transcript:
        fail("embedded transcript differs from assets/transcript.json")

    references = (ROOT / "docs/cli.md").read_text() + "\n" + (ROOT / "README.md").read_text()
    commands = {text.strip() for text in documents[0].code_text if re.match(r"^(?:git-a2a|go install|go run|curl -fsSL|irm |brew install|scoop install|npx |uvx |docker run)", text.strip())}
    documented_roots = {" ".join(command.split()[:2]) for command in commands}
    for command_root in sorted(documented_roots):
        if command_root not in references:
            fail(f"page command is absent from CLI or install documentation: {command_root}")

    above_fold = [SITE / "index.html", SITE / "assets/site.css", SITE / "assets/site.js",
                  SITE / "fonts/ibm-plex-sans-400-latin.woff2", SITE / "fonts/jetbrains-mono-400-latin.woff2"]
    size = sum(path.stat().st_size for path in above_fold)
    if size >= 250 * 1024:
        fail(f"above-the-fold transfer budget exceeded: {size} bytes")
    print(f"site-check: 3 pages valid, links resolved, canonical files synchronized")
    print("site-check: publication package contains only allowlisted files")
    print("site-check: manual publication uses ignored environment settings and staged files")
    print(f"site-check: header/footer identical; above-fold budget {size} bytes (< 256000)")


if __name__ == "__main__":
    main()
