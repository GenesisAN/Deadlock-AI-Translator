"""Repackage the verified BabelTower Community v1.0.1 release with a local bridge."""

import argparse
import hashlib
import struct
import zlib
from collections import defaultdict
from pathlib import Path

SIGNATURE = 0x55AA1234
ENTRY = struct.Struct("<IHHIIH")
EXPECTED_SHA = "5df514abd1a66edc1aaaa41a841b8b7b4b219cd349b44c648961caa539a2779d"
PRIOR_AI_SHA = "8e23728cf20861e85da93ba93dcb5ce8e7541c42effa4e79c328f11f3ae7aefd"
PROTOCOL = "panorama/scripts/lc_protocol.vjs_c"
CHAT = "panorama/scripts/lc_chat.vjs_c"
SCRIPT_DIR = Path(__file__).resolve().parents[1] / "panorama/scripts"


def read_vpk(data):
    magic, version, tree_size = struct.unpack_from("<III", data)
    if magic != SIGNATURE or version != 1:
        raise ValueError("Expected a standalone VPK v1")
    end = 12 + tree_size
    tree = data[12:end]
    cursor = 0
    entries = {}

    def read_name():
        nonlocal cursor
        stop = tree.index(0, cursor)
        name = tree[cursor:stop].decode("ascii")
        cursor = stop + 1
        return name

    while ext := read_name():
        while folder := read_name():
            while name := read_name():
                crc, preload, archive, offset, size, marker = ENTRY.unpack_from(tree, cursor)
                cursor += ENTRY.size
                if archive != 0x7FFF or marker != 0xFFFF or end + offset + size > len(data):
                    raise ValueError("Unexpected VPK archive entry")
                blob = tree[cursor:cursor + preload] + data[end + offset:end + offset + size]
                cursor += preload
                if zlib.crc32(blob) != crc:
                    raise ValueError(f"CRC mismatch: {name}")
                path = f"{folder}/{name}.{ext}"
                if path in entries:
                    raise ValueError(f"Duplicate VPK entry: {path}")
                entries[path] = blob
    if cursor != tree_size:
        raise ValueError("Unexpected VPK tree length")
    return entries


def write_vpk(entries):
    groups = defaultdict(lambda: defaultdict(list))
    for path, blob in sorted(entries.items()):
        folder, filename = path.rsplit("/", 1)
        name, ext = filename.rsplit(".", 1)
        groups[ext][folder].append((name, blob))
    tree, payload = bytearray(), bytearray()
    for ext, folders in groups.items():
        tree.extend(ext.encode() + b"\0")
        for folder, files in folders.items():
            tree.extend(folder.encode() + b"\0")
            for name, blob in files:
                tree.extend(name.encode() + b"\0")
                tree.extend(ENTRY.pack(zlib.crc32(blob), 0, 0x7FFF, len(payload), len(blob), 0xFFFF))
                payload.extend(blob)
            tree.append(0)
        tree.append(0)
    tree.append(0)
    return struct.pack("<III", SIGNATURE, 1, len(tree)) + tree + payload


def data_block(resource):
    size, version, _, relative_offset, count = struct.unpack_from("<IHHII", resource)
    if size != len(resource) or version != 12:
        raise ValueError("Unexpected Panorama resource header")
    directory = 8 + relative_offset
    for index in range(count):
        block = directory + index * 12
        if resource[block:block + 4] == b"DATA":
            offset, length = struct.unpack_from("<II", resource, block + 4)
            start = block + 4 + offset
            if start + length != size:
                raise ValueError("Expected raw script as the last resource block")
            return block, start
    raise ValueError("Resource has no script block")


def replace_script(resource, text):
    block, start = data_block(resource)
    output = bytearray(resource[:start]) + text
    struct.pack_into("<I", output, 0, len(output))
    struct.pack_into("<I", output, block + 8, len(text))
    return bytes(output)


def patch(entries):
    entries = entries.copy()
    for resource in (PROTOCOL, CHAT):
        source = SCRIPT_DIR / (Path(resource).stem + ".js")
        text = source.read_text(encoding="utf-8").encode("utf-8")
        entries[resource] = replace_script(entries[resource], text)
    return entries


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("upstream", type=Path)
    parser.add_argument("output", type=Path)
    args = parser.parse_args()
    upstream = args.upstream.read_bytes()
    if hashlib.sha256(upstream).hexdigest() not in {EXPECTED_SHA, PRIOR_AI_SHA}:
        parser.error("Input does not match BabelTower Community v1.0.1 or the previous AI release")
    if args.upstream.resolve() == args.output.resolve():
        parser.error("Choose a different output file")
    original = read_vpk(upstream)
    updated = patch(original)
    result = write_vpk(updated)
    if read_vpk(result) != updated:
        raise ValueError("Output verification failed")
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_bytes(result)
    print(f"{args.output} ({len(updated)} resources, SHA256 {hashlib.sha256(result).hexdigest()})")


if __name__ == "__main__":
    main()
