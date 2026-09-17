#!/usr/bin/env python3
"""Compute downgrade-safe release and recovery channel policy."""

from __future__ import annotations

import argparse
import json
import re
import sys
from dataclasses import dataclass


VERSION = re.compile(
    r"^v?(?P<major>0|[1-9][0-9]*)\."
    r"(?P<minor>0|[1-9][0-9]*)\."
    r"(?P<patch>0|[1-9][0-9]*)"
    r"(?:-(?P<prerelease>[0-9A-Za-z.-]+))?"
    r"(?:\+[0-9A-Za-z.-]+)?$"
)


@dataclass(frozen=True)
class SemVer:
    major: int
    minor: int
    patch: int
    prerelease: tuple[int | str, ...]


def parse_version(value: str) -> SemVer:
    match = VERSION.fullmatch(value.strip())
    if not match:
        raise ValueError(f"invalid semantic version: {value}")
    prerelease: tuple[int | str, ...] = ()
    if match.group("prerelease"):
        prerelease = tuple(
            int(part) if part.isdigit() else part
            for part in match.group("prerelease").split(".")
        )
    return SemVer(
        int(match.group("major")),
        int(match.group("minor")),
        int(match.group("patch")),
        prerelease,
    )


def compare(left: SemVer, right: SemVer) -> int:
    left_core = (left.major, left.minor, left.patch)
    right_core = (right.major, right.minor, right.patch)
    if left_core != right_core:
        return 1 if left_core > right_core else -1
    if not left.prerelease or not right.prerelease:
        if not left.prerelease and not right.prerelease:
            return 0
        return 1 if not left.prerelease else -1
    for left_part, right_part in zip(left.prerelease, right.prerelease):
        if left_part == right_part:
            continue
        if isinstance(left_part, int) and isinstance(right_part, str):
            return -1
        if isinstance(left_part, str) and isinstance(right_part, int):
            return 1
        return 1 if left_part > right_part else -1
    if len(left.prerelease) == len(right.prerelease):
        return 0
    return 1 if len(left.prerelease) > len(right.prerelease) else -1


def newest_release(releases: list[dict[str, object]], prerelease: bool) -> str:
    candidates: list[tuple[SemVer, str]] = []
    for release in releases:
        tag = str(release.get("tagName", ""))
        if release.get("isDraft") or bool(release.get("isPrerelease")) != prerelease:
            continue
        try:
            candidates.append((parse_version(tag), tag))
        except ValueError:
            continue
    if not candidates:
        return ""
    newest = candidates[0]
    for candidate in candidates[1:]:
        if compare(candidate[0], newest[0]) > 0:
            newest = candidate
    return newest[1]


def should_promote(candidate: str, current: str, prerelease: bool) -> bool:
    parsed_candidate = parse_version(candidate)
    if bool(parsed_candidate.prerelease) != prerelease:
        return False
    if not current:
        return True
    return compare(parsed_candidate, parse_version(current)) >= 0


def release_plan(candidate: str, stable: str, prerelease: str, exists: bool) -> dict[str, bool]:
    parsed = parse_version(candidate)
    is_prerelease = bool(parsed.prerelease)
    return {
        "release_exists": exists,
        "publish_release": not exists,
        "preserve_immutable": exists,
        "promote_stable": should_promote(candidate, stable, False),
        "promote_prerelease": should_promote(candidate, prerelease, True),
        "is_prerelease": is_prerelease,
    }


def main() -> None:
    parser = argparse.ArgumentParser()
    subparsers = parser.add_subparsers(dest="command", required=True)

    latest = subparsers.add_parser("latest")
    latest.add_argument("--kind", choices=("stable", "prerelease"), required=True)

    plan = subparsers.add_parser("plan")
    plan.add_argument("--candidate", required=True)
    plan.add_argument("--latest-stable", default="")
    plan.add_argument("--latest-prerelease", default="")
    plan.add_argument("--release-exists", choices=("true", "false"), required=True)
    plan.add_argument("--github-output")

    args = parser.parse_args()
    if args.command == "latest":
        releases = json.load(sys.stdin)
        print(newest_release(releases, args.kind == "prerelease"))
        return

    result = release_plan(
        args.candidate,
        args.latest_stable,
        args.latest_prerelease,
        args.release_exists == "true",
    )
    if args.github_output:
        with open(args.github_output, "a", encoding="utf-8") as output:
            for key, value in result.items():
                output.write(f"{key}={str(value).lower()}\n")
    print(json.dumps(result, sort_keys=True))


if __name__ == "__main__":
    main()
