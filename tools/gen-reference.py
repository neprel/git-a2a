#!/usr/bin/env python3
"""Generate docs/manifest-reference.md from compiled HINT and the public schema."""

from __future__ import annotations

import argparse
import difflib
import html
import json
import os
import pathlib
import re
import subprocess
import textwrap

ROOT = pathlib.Path(__file__).resolve().parents[1]
SCHEMA_PATH = ROOT / "spec/schema/a2amodule.schema.json"
OUTPUT_PATH = ROOT / "docs/manifest-reference.md"
EMBED_PATH = ROOT / "internal/reference/manifest-reference.md"
CONTACT_PATH = ROOT / "docs/contact-kinds.md"
LLMS_PATH = ROOT / "sites/git-a2a.com/llms.txt"
LLMS_FULL_PATH = ROOT / "sites/git-a2a.com/llms-full.txt"
WORKS_WITH_PATH = ROOT / "docs/works-with.md"

WORKS_WITH = [
    ("Native ecosystems", "npm, uv/PyPI, Go, Cargo, SwiftPM, Pub, Bundler, Composer, Mix, Cabal/Stack, Zig, Clojure, Nix", "native Git forms, or local path forms when vendored"),
    ("Build systems", "CMake, Gradle, Maven, MSBuild, Meson", "one generated include/import for vendored source"),
    ("Agent harnesses", "Claude Code, Codex, Cursor, GitHub Copilot, Gemini CLI, OpenCode, Hermes Agent, OpenClaw", "skill and repository-scoped MCP setup"),
    ("Standards", "A2A, AGENTS.md, Agent Skills, ARD catalogs, MCP", "native projections; MCP Registry listing"),
]

# entity id, rendered path prefix, schema location
ROUTES = [
    ("manifest", "", ()),
    ("module", "module.", ("$defs", "module")),
    ("moved_to", "module.moved-to.", ("$defs", "movedTo")),
    ("release", "module.release.", ("$defs", "release")),
    ("export", "module.exports[].", ("$defs", "export")),
    ("agent", "agents[].", ("$defs", "agent")),
    ("contact", "agents[].contacts[].", ("$defs", "contact")),
    ("trust", "agents[].trust.", ("$defs", "trust")),
    ("policy", "policy.", ("$defs", "policy")),
    ("consumers", "policy.consumers.", ("$defs", "consumers")),
    ("settings", "settings.", ("$defs", "settings")),
    ("dependency", "dependencies[].", ("$defs", "dependency")),
    ("require", "dependencies[].require.", ("$defs", "require")),
    ("vendor", "dependencies[].vendor.", ("$defs", "vendor")),
]

VOCABULARIES = {
    "module.languages": ("vocabulary_languages",),
    "module.exports[].ecosystem": ("vocabulary_ecosystems",),
    "agents[].role": ("vocabulary_roles",),
    "agents[].contacts[].intents": ("vocabulary_intents",),
    "agents[].contacts[].kind": ("contact_kinds",),
    "policy.intents": ("vocabulary_intents", "vocabulary_roles"),
    "policy.consumers.may": ("vocabulary_consumers",),
    "policy.consumers.may-not": ("vocabulary_consumers",),
    "dependencies[].track": ("vocabulary_track",),
}

DEFAULTS = {
    "schema": "exactly `1`",
    "module.name": "`module.id`",
    "module.moved-to.path": "`.`",
    "module.release.tags": "`false`",
    "module.exports[].path": "the module directory (`.`)",
    "agents[].scope": "`[\"**\"]`",
    "agents[].trust.signatures": "`false`",
    "agents[].trust.accepts-external": "unstated",
    "agents[].trust.jwks": "no pinned JWKS sources",
    "agents[].trust.keys": "no pinned key thumbprints",
    "agents[].trust.origins": "repository/card binding rules",
    "agents[].trust.jwks-max-age": "`24h`",
    "policy.intents": "unlisted intents route to `owner`",
    "policy.consumers.may": "`[read-surface, ask]`",
    "policy.consumers.may-not": "`[commit]`",
    "dependencies[].ref": "the owner's `release.channel`, otherwise remote HEAD",
    "dependencies[].path": "`.`",
    "dependencies[].track": "`locked`",
    "dependencies[].wire": "all matching detected ecosystems",
    "dependencies[].require.commits": "`any`",
    "dependencies[].require.cards": "`any`",
    "dependencies[].require.card-origin": "`false`",
    "settings.vendor-dir": "`deps`",
    "dependencies[].vendor.mode": "`submodule`",
    "dependencies[].vendor.path": "`<settings.vendor-dir>/<dependency-id>`",
    "dependencies[].vendor.recursive": "`false`",
}


def fail(message: str) -> None:
    raise SystemExit(f"manifest-reference: {message}")


def compiled_hint(target: str = "spec") -> str:
    command = ["hint", target]
    if os.name == "nt":
        # npm exposes global executables as .cmd shims on Windows. CreateProcess cannot resolve
        # that shim directly, so let the platform command processor perform PATHEXT lookup.
        command = ["cmd.exe", "/d", "/s", "/c", "hint", target]
    result = subprocess.run(command, cwd=ROOT, check=True, capture_output=True)
    output = result.stdout.decode("utf-8-sig")
    return output.replace("\r\r\n", "\n").replace("\r\n", "\n").replace("\r", "\n")


def attribute(opening: str, name: str) -> str:
    match = re.search(rf'\b{re.escape(name)}="([^"]*)"', opening)
    return html.unescape(match.group(1)) if match else ""


def entity_fields(compiled: str) -> dict[str, dict[str, tuple[str, str]]]:
    """Read HINT's XML-like stream without treating prose angle brackets as markup."""
    result: dict[str, dict[str, tuple[str, str]]] = {}
    entity_pattern = re.compile(r'(<data_structure\b[^>]*>)\n(.*?)\n</data_structure>', re.S)
    field_pattern = re.compile(r'(<field\b[^>]*>)\n(.*?)\n</field>', re.S)
    for entity_match in entity_pattern.finditer(compiled):
        entity_open, entity_body = entity_match.groups()
        entity_id = attribute(entity_open, "id")
        if not entity_id:
            continue
        fields: dict[str, tuple[str, str]] = {}
        for field_match in field_pattern.finditer(entity_body):
            field_open, raw_body = field_match.groups()
            name = attribute(field_open, "name")
            body = textwrap.dedent(html.unescape(raw_body)).strip()
            fields[name] = (body, attribute(field_open, "source") or "spec/_.hint")
        result[entity_id] = fields
    return result


def named_blocks(compiled: str, tag: str, key: str) -> dict[str, str]:
    """Extract complete compiler-owned blocks by id or name."""
    result: dict[str, str] = {}
    pattern = re.compile(rf'(<{tag}\b[^>]*>)\n(.*?)\n</{tag}>', re.S)
    for match in pattern.finditer(compiled):
        opening, raw_body = match.groups()
        identity = attribute(opening, key)
        if identity:
            result[identity] = textwrap.dedent(html.unescape(raw_body)).strip()
    return result


def inline_references(body: str, terms: dict[str, str], decisions: dict[str, str]) -> str:
    """Replace terse HINT references with the normative text agents need at the field."""
    def term(match: re.Match[str]) -> str:
        name = match.group(1)
        if name not in terms:
            fail(f"field references unknown term {name}")
        return terms[name]

    def decision(match: re.Match[str]) -> str:
        identity = match.group(1)
        if identity not in decisions:
            fail(f"field references unknown root decision {identity}")
        return decisions[identity]

    body = re.sub(r"See term `([^`]+)`\.?", term, body)
    return re.sub(r"See root decision `([^`]+)`\.?", decision, body)


def at_path(document: dict[str, object], path: tuple[str, ...]) -> dict[str, object]:
    value: object = document
    for part in path:
        if not isinstance(value, dict) or part not in value:
            fail(f"schema route is missing: {'/'.join(path)}")
        value = value[part]
    if not isinstance(value, dict):
        fail(f"schema route is not an object: {'/'.join(path)}")
    return value


def resolve(schema: dict[str, object], node: dict[str, object]) -> dict[str, object]:
    reference = node.get("$ref")
    if not isinstance(reference, str):
        return node
    if not reference.startswith("#/"):
        fail(f"external schema reference is unsupported: {reference}")
    return at_path(schema, tuple(reference[2:].split("/")))


def type_text(schema: dict[str, object], node: dict[str, object]) -> str:
    target = resolve(schema, node)
    if "const" in target:
        return type(target["const"]).__name__.replace("int", "integer")
    if "enum" in target:
        return "string"
    value = target.get("type", "object")
    if value == "array":
        item = target.get("items", {})
        return f"array of {type_text(schema, item if isinstance(item, dict) else {})}"
    return str(value)


def constraints(schema: dict[str, object], node: dict[str, object], body: str) -> str:
    target = resolve(schema, node)
    values: list[str] = []
    if "const" in target:
        values.append(f"exact value `{json.dumps(target['const'])}`")
    if isinstance(target.get("enum"), list):
        values.append("one of " + ", ".join(f"`{value}`" for value in target["enum"]))
    if "pattern" in target:
        values.append(f"pattern `{target['pattern']}`")
    if "minLength" in target:
        values.append(f"minimum length {target['minLength']}")
    if "minItems" in target:
        values.append(f"minimum {target['minItems']} item(s)")
    lowered = body.lower()
    if "open vocabulary" in lowered or "set is open" in lowered:
        values.append("open vocabulary; see the known-values table in this entry")
    return "; ".join(values) if values else "schema type plus the normative behavior in this entry"


def source_link(source: str) -> str:
    source = source.replace("\\", "/")
    path, separator, line = source.rpartition(":")
    if separator and line.isdigit():
        return f"[`{source}`](../{path}#L{line})"
    return f"[`{source}`](../spec/_.hint)"


def generate() -> str:
    schema = json.loads(SCHEMA_PATH.read_text(encoding="utf-8"))
    compiled = compiled_hint()
    hints = entity_fields(compiled)
    definitions = named_blocks(compiled, "data_definition", "id")
    terms = named_blocks(compiled, "defined_term", "name")
    decisions = named_blocks(compiled_hint("."), "settled_decision", "id")
    missing_entities = {entity_id for entity_id, _, _ in ROUTES} - hints.keys()
    if missing_entities:
        fail("compiled HINT lacks entities: " + ", ".join(sorted(missing_entities)))
    lines = [
        "<!-- Code generated by tools/gen-reference.py; DO NOT EDIT. -->",
        "# Manifest field reference",
        "",
        "This reference is generated from the compiled normative `spec/_.hint` and",
        "`spec/schema/a2amodule.schema.json`. Run `make docs-check` to detect drift.",
        "Paths use `[]` for one array entry. Unknown `x-*` extension keys are allowed at every",
        "declared object boundary and are preserved; other unknown keys are validation errors.",
        "",
    ]
    seen_paths: set[str] = set()
    for entity_id, prefix, schema_route in ROUTES:
        node = at_path(schema, schema_route)
        properties = node.get("properties")
        if not isinstance(properties, dict):
            fail(f"schema entity {entity_id} has no properties")
        required = set(node.get("required", []))
        entity_hints = hints[entity_id]
        schema_fields = set(properties)
        hint_fields = set(entity_hints) - {"x-*"}
        if schema_fields != hint_fields:
            missing = sorted(schema_fields - hint_fields)
            extra = sorted(hint_fields - schema_fields)
            fail(f"{entity_id} field parity failed; missing HINT={missing}, extra HINT={extra}")
        for name, raw_node in properties.items():
            if not isinstance(raw_node, dict):
                fail(f"schema field {entity_id}.{name} is not an object")
            path = prefix + name
            if path in seen_paths:
                fail(f"duplicate rendered path: {path}")
            seen_paths.add(path)
            body, source = entity_hints[name]
            body = inline_references(body, terms, decisions)
            body = re.sub(r"\]\(#[^)]+\)", "](../spec/_.hint)", body)
            required_text = "required" if name in required else "optional"
            default = DEFAULTS.get(path, "none declared")
            lines.extend([
                f"## `{path}`",
                "",
                f"- Type: {type_text(schema, raw_node)}; {required_text}.",
                f"- Default: {default}.",
                f"- Allowed values: {constraints(schema, raw_node, body)}.",
                f"- Normative source: {source_link(source)}.",
                "- Example: see the [public library manifest](https://github.com/neprel/git-a2a-demo-acme-lib/blob/main/a2amodule.yml) and [consumer manifest](https://github.com/neprel/git-a2a-demo-acme-app/blob/main/a2amodule.yml).",
                "",
                "Behavior and consequence:",
                "",
                body,
                "",
            ])
            vocabularies = VOCABULARIES.get(path, ())
            if vocabularies:
                lines.extend(["Known values:", ""])
                for vocabulary in vocabularies:
                    if vocabulary not in definitions:
                        fail(f"missing vocabulary definition {vocabulary} for {path}")
                    lines.extend([definitions[vocabulary], ""])
    lines.extend([
        "## Extension fields",
        "",
        "At every schema object that declares `patternProperties`, keys beginning with `x-` are",
        "extensions. The CLI preserves them; changing one changes only the extension consumer's",
        "behavior. A known object rejects every other unknown key.",
        "",
        "For an authoring sequence rather than field-by-field lookup, read the",
        "[owner's guide](authoring.md).",
        "",
    ])
    return "\n".join(lines)


def generate_contact_kinds(compiled: str) -> str:
    definitions = named_blocks(compiled, "data_definition", "id")
    table = definitions.get("contact_kinds")
    if not table:
        fail("compiled HINT lacks contact_kinds")
    return "\n".join([
        "<!-- Code generated by tools/gen-reference.py; DO NOT EDIT. -->",
        "# Contact-kind reference",
        "",
        "Contact routing starts with the universal `intents` list and optional `note`. The",
        "selected `kind` then permits only the kind-specific keys below. `git-a2a who` prints",
        "the declared destination in owner order; `git-a2a contact` either invokes the listed",
        "driver or prints the listed instruction. Every delivery emits one stdout record and",
        "stores no history.",
        "",
        table,
        "",
        "The vocabulary is open. Unknown kinds and `x-*` extension keys remain valid data, but",
        "the reference CLI does not invent a delivery mechanism for them. See the",
        "[consumer guide](consuming.md#ask-the-owner), [authoring guide](authoring.md), and",
        "[manifest fields](manifest-reference.md#agentscontactskind).",
        "",
    ])


def generate_llms_map() -> str:
    return """# git-a2a

> git-a2a is an open repository manifest standard and CLI for importing Git modules together with the agents that own them. A consumer resolves one commit, wires native ecosystems, and retains owner routing and policy.

Canonical behavior is defined by the repository specification and schemas; the website is a distribution and explanation surface.

## Documentation

- [Specification](https://github.com/neprel/git-a2a/blob/main/spec/README.md)
- [Manifest field reference](https://github.com/neprel/git-a2a/blob/main/docs/manifest-reference.md)
- [Authoring guide](https://github.com/neprel/git-a2a/blob/main/docs/authoring.md)
- [Consumer guide](https://github.com/neprel/git-a2a/blob/main/docs/consuming.md)
- [Contact kinds](https://github.com/neprel/git-a2a/blob/main/docs/contact-kinds.md)
- [FAQ](https://github.com/neprel/git-a2a/blob/main/docs/faq.md)
- [CLI reference](https://github.com/neprel/git-a2a/blob/main/docs/cli.md)
- [MCP server](https://github.com/neprel/git-a2a/blob/main/docs/mcp.md)
- [A2A module extension](https://git-a2a.com/ext/module/v1)
- [Manifest schema](https://git-a2a.com/schema/a2amodule.v1.json)
- [Lock schema](https://git-a2a.com/schema/a2amodule-lock.v1.json)
- [Full concatenated project context](https://git-a2a.com/llms-full.txt)

## Project

- [Canonical site](https://git-a2a.com/)
- [Source and issues](https://github.com/neprel/git-a2a)
- [Releases](https://github.com/neprel/git-a2a/releases)
- [Agent Skill index](https://git-a2a.com/.well-known/skills/index.json)
"""


def generate_llms_full(reference: str, contacts: str) -> str:
    inputs = [
        ("Project README", ROOT / "README.md", None),
        ("Specification overview", ROOT / "spec/README.md", None),
        ("Authoring guide", ROOT / "docs/authoring.md", None),
        ("Consumer guide", ROOT / "docs/consuming.md", None),
        ("Vendoring guide", ROOT / "docs/vendoring.md", None),
        ("Trust guide", ROOT / "docs/trust.md", None),
        ("Agent and operator guide", ROOT / "docs/agents.md", None),
        ("Public demo", ROOT / "docs/demo.md", None),
        ("Contact kinds", CONTACT_PATH, contacts),
        ("FAQ", ROOT / "docs/faq.md", None),
        ("CLI reference", ROOT / "docs/cli.md", None),
        ("MCP guide", ROOT / "docs/mcp.md", None),
        ("Manifest field reference", OUTPUT_PATH, reference),
    ]
    lines = [
        "# git-a2a: full generated project context",
        "",
        "Canonical site: https://git-a2a.com/",
        "Source: https://github.com/neprel/git-a2a",
        "Specification: https://github.com/neprel/git-a2a/blob/main/spec/README.md",
        "CLI reference: https://github.com/neprel/git-a2a/blob/main/docs/cli.md",
        "",
        "This file concatenates canonical repository documentation for retrieval. Section source",
        "labels are navigation aids; the specification and schemas remain authoritative.",
    ]
    for title, path, rendered in inputs:
        body = rendered if rendered is not None else path.read_text(encoding="utf-8")
        lines.extend(["", f"---\n\n## Source: {title}\n", body.rstrip()])
    return "\n".join(lines) + "\n"


def generate_works_with() -> str:
    lines = [
        "<!-- Code generated by tools/gen-reference.py; DO NOT EDIT. -->",
        "# Works with",
        "",
        "| Layer | Integrations | Wiring |",
        "|---|---|---|",
    ]
    lines.extend(f"| {layer} | {integrations} | {wiring} |" for layer, integrations, wiring in WORKS_WITH)
    return "\n".join(lines) + "\n"


def check_works_with_copy() -> None:
    readme = (ROOT / "README.md").read_text(encoding="utf-8")
    site = (ROOT / "sites/git-a2a.com/index.html").read_text(encoding="utf-8")
    prototype = (ROOT / "sites/design/git-a2a Landing.dc.html").read_text(encoding="utf-8")
    for layer, integrations, _ in WORKS_WITH:
        if layer not in readme or integrations not in readme:
            fail(f"README Works with copy is missing generated row {layer}")
        for token in [part.strip() for part in integrations.split(",")]:
            visible = token.replace("GitHub Copilot", "GitHub Copilot").replace("uv/PyPI", "PyPI")
            if visible not in site or visible not in prototype:
                fail(f"site/prototype Works with copy is missing {visible}")


def check_local_markdown_links() -> None:
    sources = [ROOT / "README.md", ROOT / "spec/README.md", *sorted((ROOT / "docs").glob("*.md"))]
    pattern = re.compile(r"\[[^\]]+\]\(([^)]+)\)")
    for source in sources:
        for raw in pattern.findall(source.read_text(encoding="utf-8")):
            target = raw.strip().split()[0].strip("<>")
            if target.startswith(("http://", "https://", "mailto:", "#")):
                continue
            path_text = target.split("#", 1)[0]
            if not path_text:
                continue
            resolved = (source.parent / path_text).resolve()
            try:
                resolved.relative_to(ROOT)
            except ValueError:
                fail(f"{source.relative_to(ROOT)} link escapes repository: {target}")
            if not resolved.exists():
                fail(f"{source.relative_to(ROOT)} has missing local link: {target}")


def check_markdown_table_code_spans() -> None:
    sources = [ROOT / "README.md", ROOT / "spec/README.md", *sorted((ROOT / "docs").glob("*.md"))]
    for source in sources:
        for line_number, line in enumerate(source.read_text(encoding="utf-8").splitlines(), 1):
            if not line.startswith("|"):
                continue
            for code in re.findall(r"`([^`]*)`", line):
                if re.search(r"(?<!\\)\|", code):
                    fail(
                        f"{source.relative_to(ROOT)}:{line_number} has an unescaped table pipe "
                        "inside an inline-code span"
                    )


def output(path: pathlib.Path, rendered: str, check: bool, label: str) -> None:
    if check:
        current = path.read_text(encoding="utf-8") if path.exists() else ""
        if current != rendered:
            difference = "".join(
                difflib.unified_diff(
                    current.splitlines(keepends=True),
                    rendered.splitlines(keepends=True),
                    fromfile=str(path.relative_to(ROOT)),
                    tofile=f"generated {label}",
                    n=2,
                )
            )
            if difference:
                print(difference[:4000], end="" if difference.endswith("\n") else "\n")
            fail(f"{path.relative_to(ROOT)} is stale; run tools/gen-reference.py")
        return
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(rendered.encode("utf-8"))


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    if source_link(r"spec\_.hint:110") != "[`spec/_.hint:110`](../spec/_.hint#L110)":
        fail("normative source paths are not platform-neutral")
    compiled = compiled_hint()
    rendered = generate()
    contacts = generate_contact_kinds(compiled)
    llms = generate_llms_map()
    llms_full = generate_llms_full(rendered, contacts)
    works_with = generate_works_with()
    for forbidden in ("See term `", "See root decision `", "documented below"):
        if forbidden in rendered:
            fail(f"generated reference contains unresolved wording: {forbidden}")
    if args.check:
        output(OUTPUT_PATH, rendered, True, "manifest reference")
        output(EMBED_PATH, rendered, True, "embedded reference")
        output(CONTACT_PATH, contacts, True, "contact kinds")
        output(LLMS_PATH, llms, True, "llms map")
        output(LLMS_FULL_PATH, llms_full, True, "llms full")
        output(WORKS_WITH_PATH, works_with, True, "works with")
        check_works_with_copy()
        check_local_markdown_links()
        check_markdown_table_code_spans()
        print("manifest-reference: schema fields, contact kinds, LLM guides, and local links are current")
        return
    output(OUTPUT_PATH, rendered, False, "manifest reference")
    output(EMBED_PATH, rendered, False, "embedded reference")
    output(CONTACT_PATH, contacts, False, "contact kinds")
    output(LLMS_PATH, llms, False, "llms map")
    output(LLMS_FULL_PATH, llms_full, False, "llms full")
    output(WORKS_WITH_PATH, works_with, False, "works with")
    check_works_with_copy()
    check_local_markdown_links()
    check_markdown_table_code_spans()
    print("manifest-reference: wrote field reference, contact kinds, and generated LLM guides")


if __name__ == "__main__":
    main()
