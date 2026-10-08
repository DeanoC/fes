#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Generate original stackless 192 KiB 68000 RAM/byte-lane diagnostic firmware.

Link this ROM as the ST shell's normal firmware input; it is not a raw RBF.
Reset SSP=$07fff0 and PC=$fc0100. The program never uses the stack, calls,
traps, expansion, interrupts or an operating-system ROM. It selects MMU $04
and tests physical 512 KiB RAM while code and immediates remain in ROM.

HDMI: blue background/white stage while running; green "00" is PASS;
red "01".."05" names the first failed stage. The original large seven-segment
numbers and top/bottom stripe pattern use full-word framebuffer stores only.
If full-word memory itself fails, the palette still goes red but framebuffer
text may be damaged. A blue screen is incomplete, never PASS.

01 distinct full-word patterns/address bits; 02 even/odd byte reads;
03 upper/even byte write preserves lower; 04 lower/odd preserves upper;
05 interleaved lanes preserve adjacent words across physical banks/columns.
RAM $0400 contains C0DE on pass or E001..E005 on failure; $0402 is the stage.
Those observation words are not used to control the program or its display.
This is a bounded address/lane diagnostic, not an exhaustive RAM March test.

The existing original Program emitter supplies MOVE/CMPI/Bcc absolute-long
encodings. The two additional loop encodings are MC68000 MOVE.W D0,(A0)+
and DBF D7,d16, verified against the Motorola programmer's reference manual:
https://www.nxp.com/docs/en/reference-manual/M68000PRM.pdf
"""
from __future__ import annotations

import argparse
import hashlib
import importlib.util
import json
from pathlib import Path
import struct
import subprocess

ROOT = Path(__file__).resolve().parents[1]
HELPER = ROOT / "sources/misteross/cores/fes-atari-st/diagnostic/firmware.py"
_spec = importlib.util.spec_from_file_location("fes_st_original_emitter", HELPER)
_emitter = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(_emitter)
Program = _emitter.Program
ROM_BASE, ROM_SIZE, ENTRY, STACK = 0xFC0000, 192 * 1024, 0xFC0100, 0x07FFF0
SCREEN, RESULT, STAGE = 0x060000, 0x0400, 0x0402
# Low four halfword-address bits cover both physical bank bits and low columns.
# The walking CPU byte-address bits cover every RAM address bit without vectors.
ADDRESSES = tuple(sorted({*(0x2000 + 2 * i for i in range(16)),
                         *(0x3000 ^ (1 << i) for i in range(1, 19)),
                         0x3000, 0x8000, 0x10000, 0x20000, 0x40000, 0x7F000, 0x7FFFC}))
PAIRS = tuple(0x2000 + 4 * i for i in range(8)) + (0x8000, 0x10000, 0x20000, 0x40000, 0x7F000, 0x7FFFC)
PATTERNS = (0x0000, 0xFFFF, 0x55AA, 0xAA55, 0x0F0F, 0xF0F0, 0x1234)
STAGES = {1: "full-word patterns and distinct addresses", 2: "even/high and odd/low byte reads",
          3: "upper/even byte write, lower preserved", 4: "lower/odd byte write, upper preserved",
          5: "interleaved lanes and adjacent-word preservation"}
# Original four-column seven-segment geometry; bitsets are a,b,c,d,e,f,g.
SEGMENTS = (0x3F, 0x06, 0x5B, 0x4F, 0x66, 0x6D, 0x7D, 0x07,
            0x7F, 0x6F, 0x77, 0x7C, 0x39, 0x5E, 0x79, 0x71)


def _glyph_rows(digit: int) -> tuple[int, ...]:
    bits = SEGMENTS[digit]
    rows = [6 if bits & 1 else 0,
            (8 if bits & 32 else 0) | (1 if bits & 2 else 0),
            (8 if bits & 32 else 0) | (1 if bits & 2 else 0),
            6 if bits & 64 else 0,
            (8 if bits & 16 else 0) | (1 if bits & 4 else 0),
            (8 if bits & 16 else 0) | (1 if bits & 4 else 0),
            6 if bits & 8 else 0]
    expanded = [sum(0xF << (12 - 4 * x) for x in range(4) if row & (8 >> x)) for row in rows]
    return tuple(row for row in expanded for _ in range(4))


def _draw_stage(p: Program, number: int) -> None:
    # Low resolution has four interleaved plane words for each sixteen pixels.
    # Each digit is 16x28 ST pixels (64x84 at the fixed 720p output).
    for digit_index, digit in enumerate((number >> 4, number & 15)):
        for y, word in enumerate(_glyph_rows(digit)):
            p.move_imm("w", word, SCREEN + (72 + y) * 160 + (7 + digit_index * 2) * 8)


def _palette(p: Program, background: int, foreground: int) -> None:
    p.move_imm("w", background, 0xFF8240)
    p.move_imm("w", foreground, 0xFF8242)


def firmware() -> tuple[bytes, dict[str, int]]:
    p = Program(ENTRY)
    p.sr(0x2700)
    p.move_imm("b", 4, 0xFF8001)  # 512 KiB bank0; bank1 physically empty
    p.move_imm("b", SCREEN >> 16, 0xFF8201)
    p.move_imm("b", (SCREEN >> 8) & 255, 0xFF8203)
    p.move_imm("b", 2, 0xFF820A)
    p.move_imm("b", 0, 0xFF8260)
    for index in range(16):
        p.move_imm("w", 0, 0xFF8240 + index * 2)
    _palette(p, 0x002, 0x777)
    # Full-word framebuffer clear, no stack or subroutine. All other planes
    # stay zero; display indices 0/1 permit palette-only pass/fail coloring.
    p.word(0x207C)  # MOVEA.L #SCREEN,A0
    p.long(SCREEN)
    p.word(0x303C)  # MOVE.W #0,D0
    p.word(0)
    p.word(0x3E3C)  # MOVE.W #15999,D7
    p.word(15999)
    p.label("framebuffer_clear")
    p.word(0x30C0)  # MOVE.W D0,(A0)+
    p.word(0x51CF)  # DBF D7,d16 (MC68000)
    p.fixups.append((len(p.code), "framebuffer_clear", True))
    p.word(0)
    # A striped bar at the top and bottom makes actual RAM scanout visible.
    for y in (*range(4, 8), *range(192, 196)):
        for x in range(20):
            p.move_imm("w", 0xF0F0, SCREEN + y * 160 + x * 8)

    def stage(number):
        p.move_imm("w", number, STAGE)
        _draw_stage(p, number)

    def check(size, expected, address, number):
        p.compare(size, expected, address)
        p.branch("ne", f"fail_{number}")

    stage(1)
    for pattern in PATTERNS:
        # Write every address before reading any: a decoder alias cannot hide
        # behind a successful immediate write/read of just one location.
        for index, address in enumerate(ADDRESSES):
            p.move_imm("w", pattern ^ (index * 0x101), address)
        for index, address in enumerate(ADDRESSES):
            check("w", pattern ^ (index * 0x101), address, 1)
    stage(2)
    for index, address in enumerate(ADDRESSES):
        value = 0x1234 ^ (index * 0x101)
        p.move_imm("w", value, address)
        check("b", value >> 8, address, 2)
        check("b", value & 255, address + 1, 2)
    stage(3)
    for index, address in enumerate(ADDRESSES):
        value, upper = 0x5AA5 ^ (index * 0x101), (0xC3 ^ (index * 7)) & 255
        p.move_imm("w", value, address)
        p.move_imm("b", upper, address)
        check("w", (upper << 8) | (value & 255), address, 3)
        check("b", value & 255, address + 1, 3)
    stage(4)
    for index, address in enumerate(ADDRESSES):
        value, lower = 0xA55A ^ (index * 0x101), (0x3C ^ (index * 11)) & 255
        p.move_imm("w", value, address)
        p.move_imm("b", lower, address + 1)
        check("w", (value & 0xFF00) | lower, address, 4)
        check("b", value >> 8, address, 4)
    stage(5)
    for address in PAIRS:
        p.move_imm("w", 0x1357, address)
        p.move_imm("w", 0x2468, address + 2)
        for target, byte, expected_a, expected_b in ((address, 0xA5, 0xA557, 0x2468),
                (address + 1, 0x5A, 0xA55A, 0x2468),
                (address + 3, 0x3C, 0xA55A, 0x243C),
                (address + 2, 0xC3, 0xA55A, 0xC33C)):
            p.move_imm("b", byte, target)
            check("w", expected_a, address, 5)
            check("w", expected_b, address + 2, 5)
    _draw_stage(p, 0)
    _palette(p, 0x020, 0x070)
    p.move_imm("w", 0, STAGE)
    p.move_imm("w", 0xC0DE, RESULT)
    p.label("passed")
    p.branch("always", "passed")
    for number in STAGES:
        p.label(f"fail_{number}")
        _palette(p, 0x200, 0x700)
        p.move_imm("w", number, STAGE)
        p.move_imm("w", 0xE000 | number, RESULT)
        p.label(f"failed_{number}")
        p.branch("always", f"failed_{number}")
    code = p.finish()
    image = bytearray(b"\xff" * ROM_SIZE)
    image[:8] = struct.pack(">II", STACK, ENTRY)
    image[ENTRY - ROM_BASE:ENTRY - ROM_BASE + len(code)] = code
    if len(image) != ROM_SIZE:
        raise ValueError("diagnostic escaped 192 KiB firmware envelope")
    return bytes(image), p.labels


def record(image: bytes, labels: dict[str, int]) -> dict:
    inputs = [Path(__file__).resolve(), HELPER, *[ROOT / path for path in (
        "sources/misteross/cores/fes-atari-st/rtl/st_system.sv",
        "sources/misteross/cores/fes-atari-st/rtl/st_cpu.sv",
        "sources/misteross/cores/fes-atari-st/rtl/st_machine.sv",
        "sources/misteross/cores/fes-atari-st/rtl/st_memory.sv",
        "sources/misteross/cores/fes-atari-st/rtl/st_video_adapter.sv",
        "sources/misteross/cores/fes-atari-st/rtl/st_native_low_video.sv",
        "sources/misteross/cores/fes-ramtest/rtl/sdram_addon_port.v")]]
    revision = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
    committed = True
    for path in inputs:
        original = subprocess.run(["git", "show", f"{revision}:{path.relative_to(ROOT)}"], cwd=ROOT, capture_output=True)
        committed &= original.returncode == 0 and original.stdout == path.read_bytes()
    return {"schema": 1, "kind": "fes.atari-st.memory-diagnostic/1", "source_commit": revision,
            "source_files_match_commit": committed,
            "source_files_scope": "generator/emitter and relevant memory wiring; not a native build closure",
            "rom_base": ROM_BASE, "rom_bytes": len(image), "rom_sha256": hashlib.sha256(image).hexdigest(),
            "entry": ENTRY, "reset_ssp": STACK, "screen_base": SCREEN, "result_address": RESULT,
            "stage_address": STAGE, "addresses": list(ADDRESSES), "neighbor_pairs": list(PAIRS),
            "stages": STAGES, "labels": labels, "pass_display": "green 00", "failure_display": "red 01..05",
            "incomplete_display": "blue current stage", "no_stack_instructions": True,
            "source_files": [{"path": str(path.relative_to(ROOT)), "sha256": hashlib.sha256(path.read_bytes()).hexdigest()} for path in inputs],
            "hardware_acceptance": False}


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--record", type=Path)
    args = parser.parse_args(argv)
    image, labels = firmware()
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_bytes(image)
    result = record(image, labels)
    if args.record:
        args.record.parent.mkdir(parents=True, exist_ok=True)
        args.record.write_text(json.dumps(result, indent=2, sort_keys=True) + "\n")
    print(json.dumps(result, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
