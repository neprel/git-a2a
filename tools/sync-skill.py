#!/usr/bin/env python3
"""Synchronize generated/copied git-a2a skill distribution files."""

from __future__ import annotations

import argparse
import json
import pathlib
import re
import shutil
import tempfile

ROOT = pathlib.Path(__file__).resolve().parents[1]
SKILL = ROOT / "skills/git-a2a"
SITE_SKILL = ROOT / "sites/git-a2a.com/.well-known/skills/git-a2a"
EMBEDDED_SKILL = ROOT / "internal/setupskill/files"
INDEX = ROOT / "sites/git-a2a.com/.well-known/skills/index.json"
REFERENCES = {
    "cli.md": ROOT / "docs/cli.md",
    "manifest-reference.md": ROOT / "docs/manifest-reference.md",
    "authoring.md": ROOT / "docs/authoring.md",
}


def fail(message: str) -> None:
    raise SystemExit(f"skill-sync: {message}")


def metadata(skill_md: str) -> tuple[str, str, str]:
    match = re.match(r"---\n(.*?)\n---\n", skill_md, re.S)
    if not match:
        fail("SKILL.md has no frontmatter")
    frontmatter = match.group(1)
    values: dict[str, str] = {}
    for key in ("name", "description", "compatibility"):
        value = re.search(rf"^{key}:\s*(.+)$", frontmatter, re.M)
        if not value:
            fail(f"SKILL.md lacks {key}")
        values[key] = value.group(1).strip().strip('"')
    version = re.search(r'^\s+version:\s*"([^\"]+)"$', frontmatter, re.M)
    if not version:
        fail("SKILL.md lacks metadata.version")
    return values["name"], values["description"], version.group(1)


def build(destination: pathlib.Path) -> None:
    skill_target = destination / "skill"
    shutil.copytree(SKILL, skill_target)
    references = skill_target / "references"
    if references.exists():
        shutil.rmtree(references)
    references.mkdir()
    for name, source in REFERENCES.items():
        shutil.copy2(source, references / name)

    skill_md = (skill_target / "SKILL.md").read_text()
    name, description, version = metadata(skill_md)
    expected_version = (ROOT / "internal/version/VERSION").read_text().strip()
    if version != expected_version:
        fail(f"metadata.version {version} differs from tool version {expected_version}")
    index = {
        "skills": [{
            "name": name,
            "description": description,
            "files": [
                "SKILL.md",
                "references/authoring.md",
                "references/cli.md",
                "references/manifest-reference.md",
            ],
        }],
    }
    # Generated artifacts are compared byte-for-byte in CI. Writing bytes keeps LF stable on
    # Windows instead of letting text mode translate the temporary file to CRLF.
    (destination / "index.json").write_bytes((json.dumps(index, indent=2) + "\n").encode())


def same_tree(left: pathlib.Path, right: pathlib.Path) -> bool:
    left_files = {path.relative_to(left) for path in left.rglob("*") if path.is_file()}
    right_files = {path.relative_to(right) for path in right.rglob("*") if path.is_file()}
    return left_files == right_files and all((left / path).read_bytes() == (right / path).read_bytes() for path in left_files)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    with tempfile.TemporaryDirectory() as temporary:
        rendered = pathlib.Path(temporary)
        build(rendered)
        if args.check:
            if not SITE_SKILL.exists() or not same_tree(rendered / "skill", SITE_SKILL):
                fail("site skill copy is stale; run tools/sync-skill.py")
            if not INDEX.exists() or INDEX.read_bytes() != (rendered / "index.json").read_bytes():
                fail("site skill index is stale; run tools/sync-skill.py")
            references = SKILL / "references"
            if not references.exists() or not same_tree(rendered / "skill" / "references", references):
                fail("skill references are stale; run tools/sync-skill.py")
            if not EMBEDDED_SKILL.exists() or not same_tree(rendered / "skill", EMBEDDED_SKILL):
                fail("embedded setup skill is stale; run tools/sync-skill.py")
            print("skill-sync: references, site copy, embedded setup copy, index, and tool version are current")
            return
        references = SKILL / "references"
        if references.exists():
            shutil.rmtree(references)
        shutil.copytree(rendered / "skill" / "references", references)
        if SITE_SKILL.exists():
            shutil.rmtree(SITE_SKILL)
        SITE_SKILL.parent.mkdir(parents=True, exist_ok=True)
        shutil.copytree(rendered / "skill", SITE_SKILL)
        if EMBEDDED_SKILL.exists():
            shutil.rmtree(EMBEDDED_SKILL)
        EMBEDDED_SKILL.parent.mkdir(parents=True, exist_ok=True)
        shutil.copytree(rendered / "skill", EMBEDDED_SKILL)
        INDEX.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(rendered / "index.json", INDEX)
        print("skill-sync: wrote references, site and embedded setup copies, and discovery index")


if __name__ == "__main__":
    main()
