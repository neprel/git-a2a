#!/usr/bin/env python3
"""Consolidate native summaries in report order without discarding history."""

from __future__ import annotations

import argparse
import datetime as dt
import json
from pathlib import Path


def key(item: dict[str, str]) -> tuple[str, str]:
    return item["family"], item["variant"]


LIFECYCLE_COVERAGE = (
    "add/use, fresh-clone restore, same-commit repair, revision advance, targeted and "
    "idempotent pull, remove, and unrelated-content preservation as applicable"
)


def flatten_attempts(item: dict[str, object], target: str, summary: str) -> list[dict[str, object]]:
    """Return executed leaf attempts, preserving history from consolidated input."""
    nested = item.get("attempts")
    if isinstance(nested, list) and nested:
        history: list[dict[str, object]] = []
        for attempt in nested:
            if not isinstance(attempt, dict):
                raise ValueError("native summary attempts must be objects")
            history.extend(flatten_attempts(attempt, target, summary))
        return history
    if item["status"] == "NOT RUN":
        return []
    attempt = {name: value for name, value in item.items() if name not in {"attempts", "coverage"}}
    attempt.setdefault("target", target)
    attempt.setdefault("summary", summary)
    return [attempt]


def consolidate_results(
    documents: list[dict[str, object]], source_names: list[str]
) -> list[dict[str, object]]:
    if not documents or len(documents) != len(source_names):
        raise ValueError("native summaries and source names must be non-empty and aligned")
    first_results = documents[0]["results"]
    if not isinstance(first_results, list):
        raise ValueError("native summary results must be a list")
    ordered_keys = [key(item) for item in first_results]
    attempts: dict[tuple[str, str], list[dict[str, object]]] = {
        item_key: [] for item_key in ordered_keys
    }
    templates: dict[tuple[str, str], dict[str, object]] = {}
    for item in first_results:
        template = {name: value for name, value in item.items() if name not in {"attempts", "coverage"}}
        template.setdefault("target", documents[0].get("target", "unknown"))
        templates[key(item)] = template

    for source_name, document in zip(source_names, documents):
        document_results = document["results"]
        if not isinstance(document_results, list):
            raise ValueError("native summary results must be a list")
        target = str(document.get("target", "unknown"))
        for item in document_results:
            item_key = key(item)
            attempts.setdefault(item_key, []).extend(flatten_attempts(item, target, source_name))

    results: list[dict[str, object]] = []
    for item_key in ordered_keys:
        history = attempts[item_key]
        selected = dict(history[-1] if history else templates[item_key])
        selected["coverage"] = LIFECYCLE_COVERAGE
        selected["attempts"] = history
        results.append(selected)
    return results


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base", required=True, type=Path)
    parser.add_argument("--overlay", action="append", default=[], type=Path)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()

    sources = [args.base, *args.overlay]
    documents = [json.loads(path.read_text(encoding="utf-8")) for path in sources]
    source_names = [str(path.resolve()) for path in sources]
    results = consolidate_results(documents, source_names)

    args.output.mkdir(parents=True, exist_ok=True)
    payload = {
        "schema": 1,
        "generated_at": dt.datetime.now(dt.timezone.utc).isoformat(),
        "target": documents[0].get("target", "unknown"),
        "coverage": LIFECYCLE_COVERAGE,
        "sources": source_names,
        "results": results,
    }
    (args.output / "summary.json").write_text(json.dumps(payload, indent=2) + "\n", encoding="utf-8")

    lines = [
        "# Consolidated native lifecycle evidence",
        "",
        "The latest executed attempt is selected in input-report order; NOT RUN rows do not replace an earlier result.",
        "The history column retains every executed attempt, including history from consolidated inputs.",
        f"Each selected public-CLI lifecycle test covers {LIFECYCLE_COVERAGE}.",
        "",
        "| Family | Variant | Image | Target | Version | Public CLI test command | Status | Selected log | Attempt history |",
        "| --- | --- | --- | --- | --- | --- | --- | --- | --- |",
    ]
    for item in results:
        history = "; ".join(
            f"{attempt['status']} [`log`]({attempt['log']})"
            for attempt in item["attempts"]
        ) or "NOT RUN"
        selected_log = f"[`log`]({item['log']})" if item.get("log") else ""
        lines.append(
            f"| {item['family']} | {item['variant']} | `{item['image']}` | `{item['target']}` | "
            f"{item['observed_version']} | `{item['command']}` | {item['status']} | {selected_log} | {history} |"
        )
    (args.output / "summary.md").write_text("\n".join(lines) + "\n", encoding="utf-8")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
