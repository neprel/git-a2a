#!/usr/bin/env python3
"""Generate docs/manifest-reference.md from compiled HINT and the public schema."""

from __future__ import annotations

import argparse
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
    ("dependency", "dependencies[].", ("$defs", "dependency")),
]

DEFAULTS = {
    "schema": "exactly `1`",
    "module.name": "`module.id`",
    "module.moved-to.path": "`.`",
    "module.release.tags": "`false`",
    "module.exports[].path": "the module directory (`.`)",
    "agents[].scope": "`[\"**\"]`",
    "agents[].trust.signatures": "`false`",
    "agents[].trust.accepts-external": "unstated",
    "policy.intents": "unlisted intents route to `owner`",
    "policy.consumers.may": "`[read-surface, ask]`",
    "policy.consumers.may-not": "`[commit]`",
    "dependencies[].ref": "the owner's `release.channel`, otherwise remote HEAD",
    "dependencies[].path": "`.`",
    "dependencies[].track": "`locked`",
    "dependencies[].wire": "all matching detected ecosystems",
}


def fail(message: str) -> None:
    raise SystemExit(f"manifest-reference: {message}")


def compiled_hint() -> str:
    command = ["hint", "spec"]
    if os.name == "nt":
        # npm exposes global executables as .cmd shims on Windows. CreateProcess cannot resolve
        # that shim directly, so let the platform command processor perform PATHEXT lookup.
        command = ["cmd.exe", "/d", "/s", "/c", "hint", "spec"]
    result = subprocess.run(command, cwd=ROOT, check=True, capture_output=True, text=True)
    return result.stdout


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
        values.append("open vocabulary; known values are documented below")
    return "; ".join(values) if values else "schema type only; semantic constraints are documented below"


def source_link(source: str) -> str:
    path, separator, line = source.rpartition(":")
    if separator and line.isdigit():
        return f"[`{source}`](../{path}#L{line})"
    return f"[`{source}`](../spec/_.hint)"


def generate() -> str:
    schema = json.loads(SCHEMA_PATH.read_text())
    hints = entity_fields(compiled_hint())
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


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    rendered = generate()
    if args.check:
        if not OUTPUT_PATH.exists() or OUTPUT_PATH.read_text() != rendered:
            fail("docs/manifest-reference.md is stale; run tools/gen-reference.py")
        print("manifest-reference: generated documentation is current and every schema field has HINT")
        return
    OUTPUT_PATH.write_text(rendered)
    print(f"manifest-reference: wrote {OUTPUT_PATH.relative_to(ROOT)}")


if __name__ == "__main__":
    main()
