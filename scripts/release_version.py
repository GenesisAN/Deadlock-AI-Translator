"""Choose the release version for the checked-out commit."""

import json
import re
import subprocess
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
VERSION_PATTERN = re.compile(r"(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)")


def parse_version(value):
    match = VERSION_PATTERN.fullmatch(value)
    return tuple(map(int, match.groups())) if match else None


def plan_release(minimum, head, tags, head_tags, releases):
    versions = {tag: parse_version(tag[1:]) for tag in tags if tag.startswith("v")}
    latest = max((version for version in versions.values() if version is not None), default=(0, 0, -1))
    draft = None
    for release in releases:
        tag = release["tag_name"]
        version = parse_version(tag[1:]) if tag.startswith("v") else None
        if version is None:
            continue
        latest = max(latest, version)
        if release["prerelease"]:
            continue
        if tag in head_tags and not release["draft"]:
            return None, False
        if release["draft"] and (tag in head_tags or (tag not in versions and release["target_commitish"] == head)):
            draft = max(draft or version, version)
    if draft is not None and draft == latest and draft >= minimum:
        return draft, True
    return max(minimum, (latest[0], latest[1], latest[2] + 1)), False


def git(*args):
    return subprocess.check_output(["git", *args], cwd=ROOT, encoding="utf-8").splitlines()


def main():
    minimum = parse_version((ROOT / "internal/assets/VERSION").read_text(encoding="utf-8").strip())
    if minimum is None:
        raise SystemExit("internal/assets/VERSION must contain a stable major.minor.patch version")
    releases = [release for page in json.load(sys.stdin) for release in page]
    version, draft = plan_release(minimum, git("rev-parse", "HEAD")[0], git("tag", "--list"),
                                  set(git("tag", "--points-at", "HEAD")), releases)
    if version is not None:
        print("version=" + ".".join(map(str, version)))
        print(f"draft={str(draft).lower()}")


if __name__ == "__main__":
    main()
