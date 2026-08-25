#!/usr/bin/env python3
from __future__ import annotations

import argparse
import html
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1]
SITE = ROOT / "sites" / "git-a2a.com"
OUTPUT = SITE / "spec" / "index.html"


def marked(body: str, name: str) -> str:
    match = re.search(rf"<!-- {name}:start -->\n.*?\n<!-- {name}:end -->", body, re.S)
    if not match:
        raise SystemExit(f"gen-spec-page: {name} markers missing")
    return match.group(0)


def render() -> str:
    shell = (SITE / "schema" / "index.html").read_text()
    header = marked(shell, "site-header")
    footer = marked(shell, "site-footer")
    overview = html.escape((ROOT / "spec" / "README.md").read_text())
    normative = html.escape((ROOT / "spec" / "_.hint").read_text())
    return f'''<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
  <title>a2amodule specification — git-a2a</title><meta name="description" content="Normative a2amodule schema 1 specification for agent-owned Git dependencies.">
  <meta name="robots" content="index,follow,max-image-preview:large,max-snippet:-1"><link rel="canonical" href="https://git-a2a.com/spec/"><link rel="alternate" type="text/plain" href="https://git-a2a.com/llms.txt" title="LLM project guide"><meta name="theme-color" content="#ffffff">
  <meta property="og:type" content="article"><meta property="og:title" content="a2amodule specification — git-a2a"><meta property="og:description" content="Normative a2amodule schema 1 specification for agent-owned Git dependencies."><meta property="og:url" content="https://git-a2a.com/spec/"><meta property="og:site_name" content="git-a2a"><meta property="og:image" content="https://git-a2a.com/assets/og.png"><meta property="og:image:alt" content="a2amodule specification — git-a2a"><meta name="twitter:card" content="summary_large_image"><meta name="twitter:title" content="a2amodule specification — git-a2a"><meta name="twitter:description" content="Normative a2amodule schema 1 specification for agent-owned Git dependencies."><meta name="twitter:image" content="https://git-a2a.com/assets/og-twitter.png"><meta name="twitter:image:alt" content="a2amodule specification — git-a2a">
  <link rel="icon" href="../assets/favicon.svg" type="image/svg+xml"><link rel="icon" href="../assets/favicon.ico" sizes="any"><link rel="apple-touch-icon" href="../assets/apple-touch-icon.png"><link rel="stylesheet" href="../assets/site.css">
  <script type="application/ld+json">{{"@context":"https://schema.org","@type":"TechArticle","@id":"https://git-a2a.com/spec/#article","url":"https://git-a2a.com/spec/","headline":"a2amodule specification, schema 1","description":"Normative a2amodule schema 1 specification for agent-owned Git dependencies.","isPartOf":{{"@id":"https://git-a2a.com/#website"}},"about":{{"@type":"SoftwareApplication","@id":"https://git-a2a.com/#software","name":"git-a2a"}}}}</script>
</head>
<body>
{header}
<main class="utility spec-utility"><a class="back" href="../index.html">← git-a2a</a><h1>a2amodule specification</h1><p class="utility-lead">Schema 1. The GitHub source remains canonical for review; this page is the deterministic reader copy generated from <code>spec/README.md</code> and <code>spec/_.hint</code>.</p><div class="actions"><a class="button secondary" href="../schema/">JSON Schema</a><a class="button secondary" href="https://github.com/neprel/git-a2a/blob/main/docs/manifest-reference.md">Field reference</a></div><section class="spec-source-section"><h2>Specification overview</h2><pre class="spec-source">{overview}</pre></section><section class="spec-source-section"><h2>Normative HINT source</h2><pre class="spec-source">{normative}</pre></section></main>
{footer}
</body>
</html>
'''


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    expected = render()
    if args.check:
        if not OUTPUT.exists() or OUTPUT.read_text() != expected:
            print("gen-spec-page: sites/git-a2a.com/spec/index.html is stale", file=sys.stderr)
            raise SystemExit(1)
        print("gen-spec-page: public specification page is current")
        return
    OUTPUT.parent.mkdir(parents=True, exist_ok=True)
    OUTPUT.write_text(expected)
    print("gen-spec-page: wrote sites/git-a2a.com/spec/index.html")


if __name__ == "__main__":
    main()
