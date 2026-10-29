#!/usr/bin/env python3
"""Independently inspect fitted memory and emitted firmware initialization."""
import argparse
import hashlib
import importlib.util
import json
from pathlib import Path
import re
HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location("de25_ram_memory_audit", HERE.parent / "build.py")
build = importlib.util.module_from_spec(spec)
spec.loader.exec_module(build)


def mif_bytes(source):
    text = re.sub(r"--[^\n]*", "", source)
    text = re.sub(r"%.*?%", "", text, flags=re.S)
    if not re.search(r"WIDTH\s*=\s*8\s*;", text, re.I):
        raise ValueError("firmware MIF width is not 8")
    radices = {"HEX": 16, "UNS": 10, "DEC": 10, "BIN": 2}
    address_radix = radices[re.search(r"ADDRESS_RADIX\s*=\s*(\w+)\s*;", text, re.I)[1].upper()]
    data_radix = radices[re.search(r"DATA_RADIX\s*=\s*(\w+)\s*;", text, re.I)[1].upper()]
    content = re.search(r"CONTENT\s+BEGIN(.*?)END\s*;", text, re.S | re.I)
    if content is None:
        raise ValueError("firmware MIF content is missing")
    words = {}
    for record in content[1].split(';'):
        if not record.strip(): continue
        target, data = record.split(':')
        values = [int(word, data_radix) for word in data.split()]
        interval = re.fullmatch(r"\s*\[([0-9a-fA-F]+)\.\.([0-9a-fA-F]+)\]\s*", target)
        if interval:
            if len(values) != 1: raise ValueError("unsupported MIF range")
            addresses = range(int(interval[1], address_radix), int(interval[2], address_radix) + 1)
            values = values * len(addresses)
        else:
            start = int(target, address_radix)
            addresses = range(start, start + len(values))
        for address, value in zip(addresses, values):
            if address in words or not 0 <= value <= 255: raise ValueError("duplicate or invalid firmware byte")
            words[address] = value
    return bytes(words[address] for address in range(8192))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--project", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    project = args.project.resolve()
    inputs = json.loads((project / "inputs.json").read_bytes())
    source = project / "fes_link/zx8x.hex"
    expected = bytes(int(x, 16) for x in source.read_text().split())[:8192]
    if len(expected) != 8192: raise ValueError("expected exact 8 KiB ZX81 firmware window")
    records = {}
    for revision in ("base", "persona_a", "persona_b"):
        fit = (project / "output_files" / (revision + ".fit.rpt")).read_text()
        rows = build.report_table(fit, "Fitter RAM Summary")
        rom = [r for r in rows if r["Name"].startswith("zx81_static|") and "|rom|" in r["Name"]]
        if len(rom) != 1: raise ValueError("expected one static firmware instance")
        path = project / rom[0]["MIF"]
        actual = mif_bytes(path.read_text())
        if actual != expected: raise ValueError("fitted firmware differs from captured ZX81 bytes")
        records[revision] = {"memories": rows, "firmware": {"bytes_checked": 8192,
            "sha256": hashlib.sha256(actual).hexdigest(), "mif_path": str(path.relative_to(project)),
            "mif_sha256": hashlib.sha256(path.read_bytes()).hexdigest()},
            "region_usage": build.report_table(fit, "Logic Lock Region Usage Summary")}
    root = hashlib.sha256((project / "root_partition.qdb").read_bytes()).hexdigest()
    baseline = json.loads((project / "root-qdb.json").read_bytes())["sha256"]
    if root != baseline: raise ValueError("root partition changed")
    receipt = {"status": "passed", "source_revision": inputs["source_revision"], "root_qdb_sha256": root,
        "audit_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(), "revisions": records,
        "hardware_acceptance": "not-run"}
    args.output.write_bytes(build.canonical(receipt) + b'\n')
    print(args.output)
if __name__ == "__main__": main()
