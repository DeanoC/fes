"""Build native package-inspection inputs from the existing shared corpus."""
import hashlib
import importlib.util
import json
from pathlib import Path
import sys


def prepare(root, output):
    producer = root / "sources/misteross/scripts/core_package.py"
    spec = importlib.util.spec_from_file_location("fes_package_fixture_headers", producer)
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    sources = {producer, root / "sources/misteross/scripts/rfc3986_validator.py"}
    directory = output / "package-fixtures"
    directory.mkdir()
    cases = []
    for version in (2, 3, 4):
        base = root / f"sources/FogCast/corepackage/testdata/core-bundle-v{version}"
        index = base / "cases.json"
        sources.add(index)
        for case in json.loads(index.read_text()):
            members = {}
            for key, name in (("manifest", "manifest.toml"), ("payload", "core.rbf"), ("rom_map", "rom-map.json")):
                if case.get(key):
                    path = base / case[key]
                    sources.add(path)
                    members[name] = path.read_bytes()
            order = case.get("archive_members", list(members))
            archive = bytearray()
            payload_offset = None
            for name in order:
                value = members.get(name, b"unexpected member")
                archive.extend(module._ustar_header(name, len(value)))
                if name == "core.rbf":
                    payload_offset = len(archive)
                archive.extend(value)
                archive.extend(b"\0" * (-len(value) % 512))
            archive.extend(b"\0" * 1024)
            filename = f"CASE{len(cases):03d}.FCP"
            path = directory / filename
            path.write_bytes(archive)
            cases.append({"name": f"v{version}/{case['name']}", "path": f"FES/{filename}",
                          "valid": case["valid"], "package_id": case.get("package_id", ""),
                          "payload_sha256": hashlib.sha256(members["core.rbf"]).hexdigest(),
                          "payload_offset": payload_offset, "format": version})
    # Container failures complement manifest/map cases using the same valid
    # base bytes. These test the archive boundary, not another validator.
    base = (directory / "CASE000.FCP").read_bytes()
    header = bytearray(base)
    header[0] ^= 1
    padding = bytearray(base)
    start = cases[0]["payload_offset"]
    payload_size = int(base[start - 512 + 124:start - 512 + 135], 8)
    padding[start + payload_size] = 1
    for name, body in (("header", header), ("padding", padding),
                       ("trailing", base + b"x"), ("truncated", base[:511])):
        filename = f"CASE{len(cases):03d}.FCP"
        (directory / filename).write_bytes(body)
        cases.append({"name": f"container/{name}", "path": f"FES/{filename}",
                      "valid": False, "package_id": "", "format": 0})
    index = directory / "CASES.JSON"
    index.write_text(json.dumps(cases, indent=2) + "\n")
    return directory, cases, sources
