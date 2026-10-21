#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Regenerate compact old-6526 expectations using unchanged pinned VICE sources.

The normal Verilator regression uses the checked-in headers and needs no VICE
checkout. --vice-root points to an unpacked vice-3.10 directory containing src/.
See cia_timing.md for archive provenance, coverage and timing conventions.
"""
import argparse
import csv
import hashlib
import io
from pathlib import Path
import subprocess
import tempfile

SOURCE_HASHES = {
    "core/ciacore.c": "9bb1b7e7e0f2ae4c5665036b21382fa18bf70638a94f335a614ce2cd1b45ff0f",
    "core/ciatimer.c": "29a2b799442fed620f40f137b014907b921d22cc7cb9b93494e0b4aac5c67ce0",
    "core/ciatimer.h": "486e3e8bc08249b71b9277f72403b96eaf8f12ffdc9ab8f44e29f1f7848b32fd",
    "alarm.c": "e12ffbe409481d1fe92d980da2e18c4899c7664bf3ec83f126ae69f291706ea8",
    "cia.h": "df55c1030a85d69e1bfcb549def6a9174c6c7de489a093ea432f94284e9f4b1e",
    "alarm.h": "bfd69d427573607d5934678739de781c7f846636aa08a65c8b6a8d53451bdd3c",
}
CONFIG = """#define HAVE_INTTYPES_H 1
#define HAVE_STDINT_H 1
#define HAVE_OFF_T 1
#define HAVE_OFF_T_IN_SYS_TYPES 1
#define SIZEOF_UNSIGNED_LONG 8
#define SIZEOF_UNSIGNED_INT 4
#define SIZEOF_UNSIGNED_SHORT 2
#define SIZEOF_VOID_P 8
#define HAVE_STDBOOL_H 1
"""
FNV_OFFSET = 14695981039346656037
FNV_PRIME = 1099511628211


def state_hash(previous, fields):
    # Explicit little-endian uint32 encoding, independent of host byte order.
    for value in fields:
        for byte in value.to_bytes(4, "little"):
            previous = ((previous ^ byte) * FNV_PRIME) & ((1 << 64) - 1)
    return previous


def render(trace, program_hash):
    cases = {}
    for row in csv.reader(io.StringIO(trace)):
        kind, case_id = row[0], int(row[1])
        case = cases.setdefault(case_id, {"hash": FNV_OFFSET, "states": 0,
                                          "reads": [], "edges": []})
        values = [int(value) for value in row[2:]]
        if kind == "S":
            assert len(values) == 10
            assert values[0] == case["states"] + 1
            case["hash"] = state_hash(case["hash"], values)
            case["states"] += 1
        elif kind == "R":
            assert len(values) == 3
            case["reads"].append(values)
        elif kind == "E":
            assert len(values) == 2
            case["edges"].append(values)
        else:
            raise ValueError(f"Unknown oracle record: {row}")
    assert list(cases) == list(range(len(cases)))
    lines = [
        "// SPDX-License-Identifier: MIT",
        "// Generated from unchanged VICE 3.10 old-6526 sources; do not hand edit.",
        "// Regenerate with cia_timing_vectors.py; provenance in cia_timing.md.",
        f"// Program SHA256: {program_hash}",
        "// State hashes cover every cycle, TA/latch, TB/latch, CRA, CRB, ICR, mask, pin.",
        "#pragma once", "#include <stdint.h>", "",
        "struct CIATimingRead { uint32_t cycle, reg, value; };",
        "struct CIATimingEdge { uint32_t cycle, value; };",
        "struct CIATimingExpected {",
        "    uint64_t state_hash;",
        "    uint32_t states, first_read, read_count, first_edge, edge_count;",
        "};", "", "static const CIATimingRead cia_timing_reads[] = {",
    ]
    for case in cases.values():
        lines.extend("    {%d, %d, 0x%02x}," % tuple(row) for row in case["reads"])
    lines += ["};", "", "static const CIATimingEdge cia_timing_edges[] = {"]
    for case in cases.values():
        lines.extend("    {%d, %d}," % tuple(row) for row in case["edges"])
    lines += ["};", "", "static const CIATimingExpected cia_timing_expected[] = {"]
    first_read = first_edge = 0
    for case in cases.values():
        nr, ne = len(case["reads"]), len(case["edges"])
        lines.append(f'    {{UINT64_C(0x{case["hash"]:016x}), {case["states"]}, '
                     f'{first_read}, {nr}, {first_edge}, {ne}}},')
        first_read += nr
        first_edge += ne
    lines += ["};", ""]
    print(f"Generated {len(cases)} cases, {sum(c['states'] for c in cases.values())} "
          f"states, {first_read} reads and {first_edge} ordered IRQ edges")
    return "\n".join(lines)


def main():
    directory = Path(__file__).resolve().parent
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--vice-root", type=Path, required=True)
    parser.add_argument("--output", type=Path, default=directory / "cia_timing_expected.h")
    parser.add_argument("--trace", type=Path, help="optional full reference CSV for diagnosis")
    parser.add_argument("--cc", default="cc")
    args = parser.parse_args()
    source = args.vice_root.resolve() / "src"
    for relative, expected in SOURCE_HASHES.items():
        actual = hashlib.sha256((source / relative).read_bytes()).hexdigest()
        if actual != expected:
            raise SystemExit(f"Pinned VICE source hash mismatch: {relative}")
    with tempfile.TemporaryDirectory(prefix="cia-timing-") as temporary:
        build = Path(temporary)
        (build / "config.h").write_text(CONFIG)
        binary = build / "oracle"
        command = [args.cc, "-O2", "-ffunction-sections", "-fdata-sections",
                   "-I", str(build), "-I", str(source), "-I", str(source / "core"),
                   str(directory / "cia_timing_oracle.c"),
                   str(source / "core/ciatimer.c"), str(source / "alarm.c"),
                   "-Wl,--gc-sections", "-o", str(binary)]
        subprocess.run(command, check=True)
        trace = subprocess.check_output([str(binary)], text=True)
    program_hash = hashlib.sha256((directory / "cia_timing_program.h").read_bytes()).hexdigest()
    args.output.write_text(render(trace, program_hash))
    if args.trace:
        args.trace.write_text(trace)


if __name__ == "__main__":
    main()
