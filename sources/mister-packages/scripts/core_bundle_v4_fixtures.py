#!/usr/bin/env python3
"""Generate synthetic two-ROM format-4 package fixtures; never deploy them."""

import argparse
import hashlib
import json
from pathlib import Path
import struct

import core_bundle_fixtures as v2


ROOT = Path(__file__).resolve().parents[1]
FIXTURE_ROOT = ROOT / "testdata/core-bundle-v4"


def identity(manifest, payload, mapping):
    digest = hashlib.sha256(b"FES-CORE-PACKAGE-4\n")
    for data in (manifest, payload, mapping):
        digest.update(struct.pack("<Q", len(data)))
        digest.update(data)
    return digest.hexdigest()


def rom_map():
    blocks = []
    for index in range(2):
        blocks.append(dict(
            bel=f"M10K.005.{73 + index:03}", source_offset=index * 1024,
            word_bits=[[32 * 7605 + index * 10240 + word * 40 + bit
                        for bit in range(40)] for word in range(256)],
        ))
    return dict(format=1, device="5CSEBA6U23I7", encoding="m10k-1024x10-v1",
                base_sha256=v2.PAYLOAD_SHA256, source_size=2048, blocks=blocks)


def encode_map(value):
    return (json.dumps(value, separators=(",", ":")) + "\n").encode()


def manifest(mapping):
    base = v2.canonical_manifest().replace(b"format = 2", b"format = 4", 1)
    return base + f'''
[[roms]]
id = "coleco-bios"
role = "firmware"
source_size = 1024
source_offset = 0

[[roms]]
id = "coleco-cart"
role = "cartridge"
source_size = 1024
source_offset = 1024

[rom_map]
file = "rom-map.json"
size = {len(mapping)}
sha256 = "{hashlib.sha256(mapping).hexdigest()}"
'''.encode()


def generated_files():
    mapping = encode_map(rom_map())
    base = manifest(mapping)
    files = {"payloads/fes-fixture.rbf": v2.PAYLOAD,
             "maps/valid-basic.json": mapping}
    cases = []

    def add(name, man=base, data=mapping, payload=v2.PAYLOAD, reason=None, members=None):
        mp = f"manifests/{name}.toml"
        rp = "maps/valid-basic.json" if data == mapping else f"maps/{name}.json"
        pp = "payloads/fes-fixture.rbf" if payload == v2.PAYLOAD else f"payloads/{name}.rbf"
        files[mp], files[rp], files[pp] = man, data, payload
        case = dict(name=name, manifest=mp, payload=pp, rom_map=rp, valid=reason is None)
        if members:
            case["archive_members"] = members
        case["package_id" if reason is None else "reason"] = (
            identity(man, payload, data) if reason is None else reason)
        cases.append(case)

    add("valid-basic")
    add("invalid-missing-roms", base.split(b"\n[[roms]]")[0] + base[base.index(b"\n[rom_map]"):], reason="missing ROM sources")
    add("invalid-missing-cartridge", base.replace(b"\n[[roms]]\nid = \"coleco-cart\"\nrole = \"cartridge\"\nsource_size = 1024\nsource_offset = 1024\n", b""), reason="missing cartridge")
    add("invalid-duplicate-role", base.replace(b'role = "cartridge"', b'role = "firmware"'), reason="duplicate role")
    add("invalid-duplicate-id", base.replace(b'id = "coleco-cart"', b'id = "coleco-bios"'), reason="duplicate ID")
    add("invalid-reversed-offset", base.replace(b"source_offset = 0", b"source_offset = 1024", 1).replace(b"source_offset = 1024\n\n[[roms]]", b"source_offset = 1024\n\n[[roms]]", 1).replace(b"source_offset = 1024\n\n[rom_map]", b"source_offset = 0\n\n[rom_map]"), reason="reversed offsets")
    add("invalid-gapped-offset", base.replace(b"source_offset = 1024\n\n[rom_map]", b"source_offset = 2048\n\n[rom_map]"), reason="gap")
    add("invalid-total-over-bound", base.replace(b"source_size = 1024\nsource_offset = 1024", b"source_size = 262144\nsource_offset = 1024"), reason="total over 256 KiB")
    add("invalid-map-source", base, encode_map({**rom_map(), "source_size": 1024}), reason="map source mismatch")
    add("invalid-payload-digest", base, payload=v2.PAYLOAD + b"x", reason="payload digest mismatch")
    add("invalid-map-digest", base, data=mapping.replace(b'"format":1', b'"format":2', 1), reason="map digest mismatch")
    add("invalid-extra-member", base, reason="extra archive member", members=["manifest.toml", "core.rbf", "rom-map.json", "extra.bin"])
    add("invalid-v3-with-roms", base.replace(b"format = 4", b"format = 3", 1), reason="format 3 rejects ROMs")
    files["cases.json"] = (json.dumps(cases, indent=2) + "\n").encode()
    return files


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    v2.FIXTURE_ROOT = FIXTURE_ROOT
    files = generated_files()
    return v2.check(files) if args.check else v2.write(files)


if __name__ == "__main__":
    raise SystemExit(main())
