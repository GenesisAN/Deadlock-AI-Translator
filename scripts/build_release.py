"""Build the Windows and Linux x86-64 bridges and package the release."""

import argparse
import hashlib
import os
import re
import shutil
import subprocess
import sys
from pathlib import Path
from tempfile import NamedTemporaryFile
from zipfile import ZIP_DEFLATED, ZipFile, ZipInfo

from release_version import parse_version


ROOT = Path(__file__).resolve().parents[1]
TARGETS = (
    ("windows", "amd64"),
    ("linux", "amd64"),
)
LICENSES = ("LICENSE", "NOTICE.md", "panorama/LICENSE")


def target_environment(system, arch):
    return dict(os.environ, GOOS=system, GOARCH=arch, CGO_ENABLED="0")


def build_targets(output, github_repository, version, commit):
    go = shutil.which("go")
    if go is None:
        raise SystemExit("Go 1.25.12 or newer must be on PATH")
    ldflags = "-s -w"
    if github_repository:
        ldflags += f" -X main.githubRepository={github_repository}"
    if version:
        ldflags += f" -X main.buildVersion={version}"
    if commit:
        ldflags += f" -X main.buildCommit={commit}"
    binaries = []
    for system, arch in TARGETS:
        name = "deadlock-translator.exe" if system == "windows" else "deadlock-translator"
        target = output / f"{system}-{arch}" / name
        target.parent.mkdir(parents=True, exist_ok=True)
        subprocess.run(
            [go, "build", "-mod=readonly", "-trimpath", "-buildvcs=false", f"-ldflags={ldflags}",
             "-o", str(target), "./cmd/deadlock-translator"],
            cwd=ROOT, env=target_environment(system, arch), check=True,
        )
        binaries.append((target, system != "windows"))
    return binaries


def dependency_notices():
    modules = set()
    template = '{{with .Module}}{{if not .Main}}{{.Path}}{{"\t"}}{{.Dir}}{{end}}{{end}}'
    for system, arch in TARGETS:
        dependencies = subprocess.check_output(
            ["go", "list", "-mod=readonly", "-deps", "-f", template, "./cmd/deadlock-translator"],
            cwd=ROOT, env=target_environment(system, arch), encoding="utf-8",
        )
        modules.update(tuple(line.split("\t", 1)) for line in dependencies.splitlines() if line)
    notices = []
    for name, directory in sorted(modules):
        for path in sorted(Path(directory).iterdir()):
            if path.is_file() and path.stem.upper() in {"LICENSE", "LICENCE", "COPYING", "NOTICE", "PATENTS"}:
                notices.append((path, f"licenses/{name}/{path.name}"))
    return notices


def package(archive, files):
    with NamedTemporaryFile(dir=archive.parent, suffix=".zip.tmp", delete=False) as temp:
        pending = Path(temp.name)
    try:
        with ZipFile(pending, "w", ZIP_DEFLATED) as release:
            for path, name, unix in files:
                if not unix:
                    release.write(path, name)
                    continue
                # Preserve executable permissions when the zip is built on Windows.
                info = ZipInfo.from_file(path, name)
                info.create_system = 3
                info.external_attr = 0o100755 << 16
                info.compress_type = ZIP_DEFLATED
                with path.open("rb") as src, release.open(info, "w") as dst:
                    shutil.copyfileobj(src, dst)
        pending.replace(archive)
    finally:
        pending.unlink(missing_ok=True)
    return archive


def write_checksums(output, name, paths):
    contents = "".join(f"{hashlib.sha256(path.read_bytes()).hexdigest()}  {path.relative_to(output).as_posix()}\n"
                       for path in paths)
    checksums = output / name
    checksums.write_bytes(contents.encode("ascii"))
    print(checksums)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, default=ROOT / "dist")
    parser.add_argument("--base-vpk", type=Path, default=ROOT / "local/vpk/community-v1.0.1-local.vpk",
                        help="Path to the BabelTower Community v1.0.1 VPK or the previous AI release VPK")
    parser.add_argument("--github-repository", metavar="OWNER/REPO", default="",
                        help="Build GitHub update support and package release assets")
    parser.add_argument("--version", default="", help="Override the embedded version for this release")
    parser.add_argument("--commit", default="", help="Embed the source commit hash")
    args = parser.parse_args()
    output = args.output.resolve()
    repository = args.github_repository
    if repository and not re.fullmatch(r"[A-Za-z0-9-]+/[A-Za-z0-9_][A-Za-z0-9_.-]*", repository):
        parser.error("--github-repository must be GitHub OWNER/REPO")
    if args.version and parse_version(args.version) is None:
        parser.error("--version must be a stable major.minor.patch version")
    if args.commit and not re.fullmatch(r"(?:[0-9a-f]{40}|[0-9a-f]{64})", args.commit):
        parser.error("--commit must be a full lowercase Git commit hash")
    if not args.base_vpk.is_file():
        parser.error("Pass --base-vpk with a local VPK. See docs/vpk.md for download instructions.")

    vpk = output / "pak23_dir.vpk"
    subprocess.run(
        [sys.executable, str(ROOT / "scripts/build_vpk.py"),
         str(args.base_vpk), str(vpk)],
        check=True,
    )
    binaries = build_targets(output, repository, args.version, args.commit)
    notices = [(ROOT / name, name, False) for name in LICENSES]
    notices.extend((path, name, False) for path, name in dependency_notices())
    files = [(ROOT / "docs/QUICKSTART.zh-CN.md", "QUICKSTART.zh-CN.md", False), (vpk, vpk.name, False)]
    files.extend((binary, binary.relative_to(output).as_posix() if unix else binary.name, unix)
                 for binary, unix in binaries)
    archives = [package(output / "Deadlock-AI-Translator-Go.zip", files + notices)]
    if repository:
        for binary, unix in binaries:
            archive = output / f"deadlock-translator-{binary.parent.name}.zip"
            archives.append(package(archive, [(binary, binary.name, unix)] + notices))
    write_checksums(output, "SHA256SUMS.txt", archives)
    write_checksums(output, "EXECUTABLE-SHA256SUMS.txt", (binary for binary, _ in binaries))
    for archive in archives:
        print(archive)


if __name__ == "__main__":
    main()
