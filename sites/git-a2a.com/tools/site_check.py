#!/usr/bin/env python3
from __future__ import annotations

import html.parser
import difflib
import json
import os
import pathlib
import re
import subprocess
import sys
import tempfile
import xml.etree.ElementTree as ET
from urllib.parse import urlsplit

ROOT = pathlib.Path(__file__).resolve().parents[3]
SITE = ROOT / "sites" / "git-a2a.com"
PAGES = [SITE / "index.html", SITE / "ext/module/v1/index.html", SITE / "schema/index.html", SITE / "spec/index.html"]


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


class VisibleText(html.parser.HTMLParser):
    def __init__(self, route: str, prototype: bool) -> None:
        super().__init__(convert_charrefs=True)
        self.route = route
        self.prototype = prototype
        self.nodes: list[str] = []
        self.stack: list[tuple[bool, bool, bool]] = []

    def handle_starttag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        values = dict(attrs)
        ignored = tag in {"head", "script", "style", "helmet"}
        allowed = True
        if self.prototype and tag == "sc-if":
            condition = values.get("value", "")
            routes = {"{{ isLanding }}": "landing", "{{ isExt }}": "extension", "{{ isSchema }}": "schema"}
            if condition in routes:
                allowed = routes[condition] == self.route
        dynamic = tag == "sc-for"
        if not self.prototype:
            dynamic = (
                "data-copy" in values
                or "data-plan17-demo" in values
                or values.get("id") in {"terminal-body", "transcript-data"}
            )
        self.stack.append((ignored, allowed, dynamic))

    def handle_startendtag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        self.handle_starttag(tag, attrs)
        self.stack.pop()

    def handle_endtag(self, tag: str) -> None:
        if self.stack:
            self.stack.pop()

    def handle_data(self, data: str) -> None:
        if any(ignored or not allowed or dynamic for ignored, allowed, dynamic in self.stack):
            return
        if "{{" in data or "}}" in data:
            return
        text = " ".join(data.split())
        if text:
            self.nodes.append(text)


def visible_text(body: str, route: str, prototype: bool = False) -> list[str]:
    parser = VisibleText(route, prototype)
    parser.feed(body)
    parser.close()
    return parser.nodes


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
    subprocess.run(["python3", str(ROOT / "tools/sync-skill.py"), "--check"], cwd=ROOT, check=True)
    subprocess.run(["python3", str(ROOT / "tools/gen-spec-page.py"), "--check"], cwd=ROOT, check=True)

    with tempfile.TemporaryDirectory() as temporary:
        package = pathlib.Path(temporary) / "public"
        subprocess.run(
            [str(ROOT / "scripts/site-package.sh"), str(package)],
            cwd=ROOT,
            check=True,
            stdout=subprocess.DEVNULL,
        )
        expected_top_level = {
            ".htaccess",
            "index.html", "404.html", "robots.txt", "sitemap.xml", "llms.txt", "llms-full.txt",
            "install.sh", "install.ps1",
            ".well-known", "assets", "demo", "fonts", "ext", "schema", "spec",
        }
        packaged_top_level = {path.name for path in package.iterdir()}
        if packaged_top_level != expected_top_level:
            fail(f"publication allowlist differs: {sorted(packaged_top_level)}")
        htaccess = (package / ".htaccess").read_text()
        if htaccess != "AddType text/plain .ps1 .sh\nAddType text/markdown .md\nAddType application/json .json\nAddDefaultCharset utf-8\n":
            fail("publication .htaccess does not set installer/skill MIME types and UTF-8")
        for excluded in ("README.md", "tools", "CNAME", ".nojekyll"):
            if (package / excluded).exists():
                fail(f"operator-only path was packaged: {excluded}")
        card_paths = [
            package / "demo/agents/acme-lib-utils/.well-known/agent-card.json",
            package / "demo/agents/acme-pm/.well-known/agent-card.json",
        ]
        cards = []
        for card_path in card_paths:
            try:
                card = json.loads(card_path.read_text())
            except (OSError, json.JSONDecodeError) as error:
                fail(f"invalid packaged demo card {card_path}: {error}")
            if card.get("version") != "1.0.0" or not card.get("supportedInterfaces"):
                fail(f"demo card lacks A2A v1.0 interface: {card_path}")
            signatures = card.get("signatures", [])
            if len(signatures) != 1 or not signatures[0].get("protected") or not signatures[0].get("signature"):
                fail(f"demo card lacks its detached JWS: {card_path}")
            cards.append(card)
        jwks = json.loads((package / "demo/agents/.well-known/jwks.json").read_text())
        keys = jwks.get("keys", [])
        if len(keys) != 1 or keys[0].get("kid") != "acme-demo-2026" or keys[0].get("alg") != "EdDSA":
            fail("packaged demo JWKS does not contain exactly the demo signing key")
        allowed_signers = (package / "demo/agents/allowed_signers").read_text()
        if "demo-only git-a2a public demo key" not in allowed_signers or "PRIVATE" in allowed_signers:
            fail("packaged demo allowed_signers is missing its demo-only public-key marker")
        catalog = json.loads((package / ".well-known/ai-catalog.json").read_text())
        catalog_urls = {entry.get("url") for entry in catalog.get("entries", [])}
        expected_card_urls = {
            "https://git-a2a.com/demo/agents/acme-lib-utils/.well-known/agent-card.json",
            "https://git-a2a.com/demo/agents/acme-pm/.well-known/agent-card.json",
        }
        if catalog.get("specVersion") != "1.0" or catalog_urls != expected_card_urls:
            fail("packaged demo catalog does not list exactly the two public cards")
        skill_index = json.loads((package / ".well-known/skills/index.json").read_text())
        if [entry.get("name") for entry in skill_index.get("skills", [])] != ["git-a2a"]:
            fail("packaged skill index does not list exactly git-a2a")
        public_skill = package / ".well-known/skills/git-a2a"
        source_skill = ROOT / "skills/git-a2a"
        source_files = {path.relative_to(source_skill) for path in source_skill.rglob("*") if path.is_file()}
        public_files = {path.relative_to(public_skill) for path in public_skill.rglob("*") if path.is_file()}
        if source_files != public_files or any((source_skill / path).read_bytes() != (public_skill / path).read_bytes() for path in source_files):
            fail("packaged skill differs from skills/git-a2a")

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

    expected_canonicals = [
        "https://git-a2a.com/",
        "https://git-a2a.com/ext/module/v1",
        "https://git-a2a.com/schema/",
        "https://git-a2a.com/spec/",
    ]
    for page, body, canonical in zip(PAGES, bodies, expected_canonicals):
        if body.count(f'<link rel="canonical" href="{canonical}">') != 1:
            fail(f"missing or duplicate canonical URL in {page.relative_to(ROOT)}")
        if f'<meta property="og:url" content="{canonical}">' not in body:
            fail(f"Open Graph URL differs from canonical in {page.relative_to(ROOT)}")
        if '<link rel="alternate" type="text/plain" href="https://git-a2a.com/llms.txt" title="LLM project guide">' not in body:
            fail(f"LLM discovery link missing in {page.relative_to(ROOT)}")
        if 'name="robots" content="index,follow,max-image-preview:large,max-snippet:-1"' not in body:
            fail(f"indexing directives missing in {page.relative_to(ROOT)}")
        structured = re.findall(r'<script type="application/ld\+json">(.*?)</script>', body, re.S)
        if len(structured) != 1:
            fail(f"expected one structured-data graph in {page.relative_to(ROOT)}")
        try:
            graph = json.loads(structured[0])
        except json.JSONDecodeError as error:
            fail(f"invalid structured data in {page.relative_to(ROOT)}: {error}")
        if graph.get("@context") != "https://schema.org":
            fail(f"structured-data context missing in {page.relative_to(ROOT)}")

    try:
        sitemap = ET.parse(SITE / "sitemap.xml")
    except ET.ParseError as error:
        fail(f"invalid sitemap.xml: {error}")
    sitemap_urls = {
        element.text for element in sitemap.findall("{http://www.sitemaps.org/schemas/sitemap/0.9}url/{http://www.sitemaps.org/schemas/sitemap/0.9}loc")
    }
    if sitemap_urls != set(expected_canonicals):
        fail(f"sitemap URLs differ from page canonicals: {sorted(sitemap_urls)}")
    if "Sitemap: https://git-a2a.com/sitemap.xml" not in (SITE / "robots.txt").read_text():
        fail("robots.txt does not advertise sitemap.xml")
    for llms_name in ("llms.txt", "llms-full.txt"):
        llms = (SITE / llms_name).read_text()
        for required in (
            "https://git-a2a.com/",
            "https://github.com/neprel/git-a2a/blob/main/spec/README.md",
            "https://github.com/neprel/git-a2a/blob/main/docs/cli.md",
        ):
            if required not in llms:
                fail(f"{llms_name} lacks canonical reference {required}")

    prototype = (ROOT / "sites/design/git-a2a Landing.dc.html").read_text()
    for route, page, body in zip(("landing", "extension", "schema"), PAGES[:3], bodies[:3]):
        expected = visible_text(prototype, route, prototype=True)
        actual = visible_text(body, route)
        if actual != expected:
            difference = "\n".join(difflib.unified_diff(
                expected, actual, fromfile="design/" + route, tofile=str(page.relative_to(ROOT)), lineterm=""
            ))
            fail(f"visible copy differs from the design prototype:\n{difference}")
    for name in ("site-header", "site-footer"):
        values = [block(body, name) for body in bodies]
        if len(set(values)) != 1:
            fail(f"{name} differs across the three pages")
    demo_lib = "https://github.com/neprel/git-a2a-demo-acme-lib"
    demo_app = "https://github.com/neprel/git-a2a-demo-acme-app"
    if bodies[0].count(demo_app) < 1:
        fail("landing page lacks the consumer demo link")
    for body in bodies:
        if body.count(demo_lib) < 1:
            fail("site footer lacks the library demo link")

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
    try:
        subprocess.run(
            [str(SITE / "tools/transcript-generate.sh"), "--check"],
            cwd=ROOT,
            check=True,
        )
    except subprocess.CalledProcessError:
        fail("published transcript does not match a fresh fixture run")
    transcript_text = "\n".join(
        str(group.get(stream, ""))
        for group in transcript.get("groups", [])
        for stream in ("stdout", "stderr")
    )
    for forbidden in ("warning:", "unhealthy"):
        if forbidden in transcript_text.lower():
            fail(f"transcript contains forbidden verdict {forbidden}")
    for required in (
        "npm clean, pypi clean, golang clean",
        "consumer-app: manifest valid · agents none · roster none",
        "1 dependency: clean",
    ):
        if required not in transcript_text:
            fail(f"transcript lacks required result: {required}")

    cli_reference = (ROOT / "docs/cli.md").read_text()
    install_reference = (ROOT / "README.md").read_text()
    references = cli_reference + "\n" + install_reference
    commands = {text.strip() for text in documents[0].code_text if re.match(r"^(?:git-a2a|go install|go run|curl -fsSL|irm |brew install|scoop install|npx |uvx |docker run)", text.strip())}
    grouped = {"agent", "card", "catalog", "export", "policy", "trust"}
    for command in sorted(commands):
        words = command.split()
        if words[0] == "git-a2a":
            if len(words) < 2 or not re.search(rf"^## {re.escape(words[1])}$", cli_reference, re.M):
                fail(f"page command is absent from CLI documentation: {command}")
            if words[1] in grouped:
                if len(words) < 3 or not re.search(rf"\b{re.escape(words[1])}\s+{re.escape(words[2])}\b", cli_reference):
                    fail(f"page subcommand is absent from CLI documentation: {command}")
            continue
        signature = " ".join(words[:2])
        if signature not in references:
            fail(f"page install command is absent from installation documentation: {command}")

    above_fold = [SITE / "index.html", SITE / "assets/site.css", SITE / "assets/site.js",
                  SITE / "fonts/ibm-plex-sans-400-latin.woff2", SITE / "fonts/jetbrains-mono-400-latin.woff2"]
    size = sum(path.stat().st_size for path in above_fold)
    if size >= 250 * 1024:
        fail(f"above-the-fold transfer budget exceeded: {size} bytes")
    print("site-check: visible text on 3 designed pages matches the design prototype")
    print(f"site-check: 4 pages valid, links resolved, canonical files synchronized")
    print("site-check: canonical metadata, structured data, sitemap, and LLM guides valid")
    print("site-check: publication package contains only allowlisted files, two valid demo cards, and their catalog")
    print("site-check: manual publication uses ignored environment settings and staged files")
    print(f"site-check: header/footer identical; above-fold budget {size} bytes (< 256000)")


if __name__ == "__main__":
    main()
