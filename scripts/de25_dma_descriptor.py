#!/usr/bin/env python3
"""Validate an offline DE25 DMA descriptor and print an unqualified write plan."""
from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path
import re


ROOT = Path(__file__).resolve().parents[1]
CONTRACT = ROOT / "sources/misteross/boards/de25nano/hps/generated/fes_de25_ddr_probe.hpp"
PREFIX = "FesDe25DdrProbe"
WRITE_FIELDS = (
    ("grant_dma_lo", "GrantLoOffset"), ("grant_dma_hi", "GrantHiOffset"),
    ("grant_span_bytes", "GrantSpanOffset"), ("read_offset_bytes", "ReadOffsetOffset"),
    ("read_bytes", "ReadBytesOffset"), ("generation_lo", "GenerationLoOffset"),
    ("generation_hi", "GenerationHiOffset"), ("sequence", "SequenceOffset"),
)
REQUIRED = {"Identity", "Version", "PeripheralOffset", "Span", "ArchitecturalLwBase", "ArchitecturalLwSpan", "DmaAddressBits",
            "BeatBytes", "GrantAlignment", "MaxBurstBeats", "BoundaryBytes",
            "CommandOffset", "CommandStart", "FrameWidth", "FrameHeight", "FrameStride",
            "FrameBytes", "SlotBytes", "SlotCount", "AllocationBytes",
            *(offset for _, offset in WRITE_FIELDS)}


def load_contract(path=CONTRACT):
    """Read integer constants from the checked-in generated C++ consumer."""
    data = Path(path).read_bytes()
    source = data.decode("ascii")
    if "packages/fabric/fes_fabric_de25_ddr_probe.yaml." not in source:
        raise ValueError("DE25 descriptor constants lack their canonical generated provenance")
    constants = {}
    for line in source.splitlines():
        if PREFIX not in line:
            continue
        match = re.fullmatch(r"constexpr std::uint(32|64)_t " + PREFIX +
                             r"([A-Za-z0-9]+) = (0x[0-9a-fA-F]+|[0-9]+)(?:u|ull);", line)
        if not match:
            raise ValueError("unsupported generated DE25 descriptor constant")
        bits, name, literal = match.groups()
        value = int(literal, 0)
        if name in constants or value >= 1 << int(bits):
            raise ValueError("duplicate or overflowing DE25 descriptor constant")
        constants[name] = value
    if not REQUIRED <= constants.keys():
        raise ValueError("missing generated DE25 descriptor constants")
    if (constants["DmaAddressBits"] != 32 or constants["BeatBytes"] != 16
            or constants["GrantAlignment"] != 32 or constants["SlotCount"] != 2
            or constants["FrameStride"] != constants["FrameWidth"] * 4
            or constants["FrameBytes"] != constants["FrameStride"] * constants["FrameHeight"]
            or not 0 < constants["FrameBytes"] <= constants["SlotBytes"]
            or constants["AllocationBytes"] != constants["SlotCount"] * constants["SlotBytes"]):
        raise ValueError("unsupported DE25 DMA/frame profile")
    offsets = [constants[name] for _, name in WRITE_FIELDS] + [constants["CommandOffset"]]
    if len(set(offsets)) != len(offsets) or any(offset % 4 or offset + 4 > constants["Span"] for offset in offsets):
        raise ValueError("invalid DE25 descriptor register layout")
    if constants["PeripheralOffset"] + constants["Span"] > constants["ArchitecturalLwSpan"]:
        raise ValueError("DE25 descriptor endpoint is outside the architectural lightweight aperture")
    return constants, hashlib.sha256(data).hexdigest()


def unsigned(name, value, bits, nonzero=False):
    if type(value) is not int or value < int(nonzero) or value >= 1 << bits:
        raise ValueError(f"{name} must be a {'nonzero ' if nonzero else ''}unsigned {bits}-bit integer")
    return value


def probe_plan(dma_base, grant_bytes, read_offset, read_bytes, generation, sequence):
    constants, contract_sha = load_contract()
    return _probe_plan(constants, contract_sha, dma_base, grant_bytes, read_offset, read_bytes,
                       generation, sequence)


def _probe_plan(constants, contract_sha, dma_base, grant_bytes, read_offset, read_bytes, generation, sequence):
    unsigned("DMA base", dma_base, 64)
    unsigned("grant bytes", grant_bytes, 32, True)
    unsigned("read offset", read_offset, 32)
    unsigned("read bytes", read_bytes, 32, True)
    unsigned("generation", generation, 64, True)
    unsigned("sequence", sequence, 32, True)
    if dma_base % constants["GrantAlignment"]:
        raise ValueError("DMA base must meet the 32-byte grant alignment")
    if any(value % constants["BeatBytes"] for value in (grant_bytes, read_offset, read_bytes)):
        raise ValueError("grant span, read offset and read length must be multiples of the 16-byte beat")
    if read_offset > grant_bytes or read_bytes > grant_bytes - read_offset:
        raise ValueError("read is outside its DMA grant")
    grant_end = dma_base + grant_bytes
    if grant_end > 1 << constants["DmaAddressBits"]:
        raise ValueError("DMA grant exceeds the selected 32-bit bus address space")
    values = (dma_base & 0xffffffff, dma_base >> 32, grant_bytes, read_offset, read_bytes,
              generation & 0xffffffff, generation >> 32, sequence)
    writes = [{"register": field, "offset": constants[offset], "value": value}
              for (field, offset), value in zip(WRITE_FIELDS, values)]
    writes.append({"register": "command_start", "offset": constants["CommandOffset"],
                   "value": constants["CommandStart"]})
    return {
        "format": 1, "classification": "offline-descriptor-plan", "admission": "not-qualified",
        "hardware_access": "not-performed", "address_space": "dma-bus-or-iova",
        "allocation_ownership": "prospective-kernel-dma-api; not verified by this tool",
        "translation": "not-qualified", "firewall": "not-qualified", "boot_pairing": "not-qualified",
        "contract_sha256": contract_sha,
        "endpoint": {"identity": constants["Identity"], "version": constants["Version"],
                     "lightweight_peripheral_offset": constants["PeripheralOffset"]},
        "candidate_cpu_control_window": {
            "architectural_lw_base": constants["ArchitecturalLwBase"],
            "derived_endpoint_address": constants["ArchitecturalLwBase"] + constants["PeripheralOffset"],
            "admission": "not-qualified"},
        "descriptor": {"grant_dma_address": dma_base, "grant_span_bytes": grant_bytes,
                       "read_offset_bytes": read_offset, "read_bytes": read_bytes,
                       "generation": generation, "sequence": sequence},
        "bounds": {"grant_start": dma_base, "grant_end_exclusive": grant_end,
                   "read_start": dma_base + read_offset,
                   "read_end_exclusive": dma_base + read_offset + read_bytes},
        "burst_rules": {"beat_bytes": constants["BeatBytes"], "max_beats": constants["MaxBurstBeats"],
                        "boundary_bytes": constants["BoundaryBytes"], "implementation": "RTL; not executed here"},
        "register_offset_space": "endpoint-relative; 32-bit writes", "register_writes": writes,
    }


def frame_plan(dma_base, allocation_bytes, slot, generation, sequence):
    constants, contract_sha = load_contract()
    if type(allocation_bytes) is not int or allocation_bytes != constants["AllocationBytes"]:
        raise ValueError("frame allocation must be exactly the shared two-slot allocation size")
    if type(slot) is not int or not 0 <= slot < constants["SlotCount"]:
        raise ValueError("frame slot must be 0 or 1")
    result = _probe_plan(constants, contract_sha, dma_base, allocation_bytes, slot * constants["SlotBytes"],
                         constants["FrameBytes"], generation, sequence)
    result["mode"] = "frame"
    result["frame"] = {"width": constants["FrameWidth"], "height": constants["FrameHeight"],
                       "stride": constants["FrameStride"], "frame_bytes": constants["FrameBytes"],
                       "slot_bytes": constants["SlotBytes"], "slots": constants["SlotCount"],
                       "pixel_format": "bgrx8888", "slot": slot,
                       "slot_dma_addresses": [dma_base + index * constants["SlotBytes"]
                                              for index in range(constants["SlotCount"])],
                       "purpose": "read-probe plan for a prospective framebuffer; not a display submission"}
    return result


def integer(text):
    try:
        return int(text, 0)
    except ValueError as error:
        raise argparse.ArgumentTypeError("expected a decimal or 0x-prefixed integer") from error


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    modes = parser.add_subparsers(dest="mode", required=True)
    for mode in ("probe", "frame"):
        command = modes.add_parser(mode)
        command.add_argument("--dma-base", type=integer, required=True, help="device DMA bus address or IOVA, not a CPU pointer")
        command.add_argument("--generation", type=integer, required=True)
        command.add_argument("--sequence", type=integer, required=True)
        if mode == "probe":
            command.add_argument("--grant-bytes", type=integer, required=True)
            command.add_argument("--offset", type=integer, default=0)
            command.add_argument("--length", type=integer, required=True)
        else:
            command.add_argument("--allocation-bytes", type=integer, required=True)
            command.add_argument("--slot", type=integer, required=True)
    args = parser.parse_args(argv)
    try:
        if args.mode == "probe":
            result = probe_plan(args.dma_base, args.grant_bytes, args.offset, args.length,
                                args.generation, args.sequence)
            result["mode"] = "probe"
        else:
            result = frame_plan(args.dma_base, args.allocation_bytes, args.slot, args.generation, args.sequence)
        serialized = json.dumps(result, indent=2, sort_keys=True) + "\n"
    except (OSError, ValueError, UnicodeError) as error:
        parser.exit(1, "de25-dma-descriptor: " + str(error) + "\n")
    print(serialized, end="")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
