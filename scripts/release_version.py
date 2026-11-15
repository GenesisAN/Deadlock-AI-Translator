"""Validate a release tag and expose its version to GitHub Actions."""

import argparse
import re


VERSION_PATTERN = re.compile(r"(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)")


def parse_version(value):
    match = VERSION_PATTERN.fullmatch(value)
    return tuple(map(int, match.groups())) if match else None


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("tag")
    tag = parser.parse_args().tag
    if not tag.startswith("v") or parse_version(tag[1:]) is None:
        parser.error("Release tag must use vMAJOR.MINOR.PATCH without leading zeroes")
    print(f"version={tag[1:]}")


if __name__ == "__main__":
    main()
