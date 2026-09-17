#!/usr/bin/env python3
from __future__ import annotations

import html.parser
import json
import pathlib
import subprocess
import tempfile
import xml.etree.ElementTree as ET
from urllib.parse import urlsplit

ROOT = pathlib.Path(__file__).resolve().parents[3]
SITE = ROOT / "sites" / "git-a2a.com"
PAGES = [SITE / "index.html", SITE / "schema/index.html", SITE / "spec/index.html"]

def fail(message: str) -> None:
    raise SystemExit(f"site-check: {message}")

class Document(html.parser.HTMLParser):
    def __init__(self) -> None:
        super().__init__(convert_charrefs=True)
        self.links: list[str] = []
        self.ids: set[str] = set()
        self.errors: list[str] = []
        self.stack: list[str] = []
    def handle_starttag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        values = dict(attrs)
        if values.get("id"):
            if values["id"] in self.ids: self.errors.append(f"duplicate id {values['id']}")
            self.ids.add(values["id"] or "")
        for name in ("href", "src"):
            if values.get(name): self.links.append(values[name] or "")
        if tag not in {"area","base","br","col","embed","hr","img","input","link","meta","source","track","wbr"}: self.stack.append(tag)
    def handle_endtag(self, tag: str) -> None:
        if not self.stack or self.stack[-1] != tag: self.errors.append(f"unexpected closing tag {tag}")
        else: self.stack.pop()

def local_target(page: pathlib.Path, link: str) -> pathlib.Path | None:
    parsed = urlsplit(link)
    if parsed.scheme or parsed.netloc or link.startswith("#"): return None
    candidate = SITE / parsed.path.lstrip("/") if parsed.path.startswith("/") else page.parent / parsed.path
    if parsed.path.endswith("/") or candidate.is_dir(): candidate /= "index.html"
    return candidate.resolve()

def main() -> None:
    if (ROOT / "install.sh").read_bytes() != (SITE / "install.sh").read_bytes(): fail("install.sh copy differs")
    pairs = [(ROOT/"spec/schema/a2amodule.schema.json", SITE/"schema/a2amodule.v2.json"),(ROOT/"spec/schema/a2amodule.lock.schema.json", SITE/"schema/a2amodule-lock.v2.json")]
    for source, public in pairs:
        if source.read_bytes() != public.read_bytes(): fail(f"schema copy differs: {public.name}")
        json.loads(public.read_text())
    subprocess.run(["python3", str(ROOT/"tools/sync-skill.py"), "--check"], cwd=ROOT, check=True)
    canonicals = ["https://git-a2a.com/","https://git-a2a.com/schema/","https://git-a2a.com/spec/"]
    for page, canonical in zip(PAGES, canonicals):
        body = page.read_text()
        if body.count(f'<link rel="canonical" href="{canonical}">') != 1: fail(f"canonical missing in {page}")
        if f'<meta property="og:url" content="{canonical}">' not in body: fail(f"og:url missing in {page}")
        doc = Document(); doc.feed(body); doc.close()
        if doc.errors or doc.stack: fail(f"invalid HTML in {page}: {doc.errors or doc.stack}")
        for link in doc.links:
            target = local_target(page, link)
            if target is not None and not target.exists(): fail(f"broken local link {link} in {page}")
    urls = {node.text for node in ET.parse(SITE/"sitemap.xml").findall("{http://www.sitemaps.org/schemas/sitemap/0.9}url/{http://www.sitemaps.org/schemas/sitemap/0.9}loc")}
    if urls != set(canonicals): fail(f"sitemap differs: {sorted(urls)}")
    tracked = "\n".join(path for path in subprocess.check_output(["git","ls-files","sites/git-a2a.com"],cwd=ROOT,text=True).splitlines() if (ROOT/path).exists())
    forbidden = ("ai-catalog","ext/module","demo/agents","transcript","a2amodule.v1","mcp")
    if any(term in tracked.lower() for term in forbidden): fail("legacy site artifact remains")
    visible = "\n".join(page.read_text().lower() for page in PAGES)
    for required in ("init","add source","pull [name]","remove name","list [name]","agent card","surface"):
        if required not in visible: fail(f"required copy missing: {required}")
    with tempfile.TemporaryDirectory() as temporary:
        package = pathlib.Path(temporary)/"public"
        subprocess.run([str(ROOT/"scripts/site-package.sh"),str(package)],cwd=ROOT,check=True,stdout=subprocess.DEVNULL)
        expected={".htaccess","index.html","404.html","robots.txt","sitemap.xml","llms.txt","llms-full.txt","install.sh","install.ps1",".well-known","assets","fonts","schema","spec"}
        if {p.name for p in package.iterdir()} != expected: fail("publication allowlist differs")
        if sum(p.stat().st_size for p in package.rglob("*") if p.is_file()) > 3_000_000: fail("site transfer budget exceeded")
    print("site-check: schema 2 static package is valid")

if __name__ == "__main__": main()
