#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
# Copyright (c) 2026 FES contributors
"""Original 68000 firmware for the FES Atari 520ST bring-up machine.

No Atari ROM bytes or external assembler are needed. The exact 192 KiB image
contains supervisor reset vectors followed by an original CPU program. It
checks byte lanes, big-endian words/longs, the 512 KiB RAM boundary, video
registers, a synthetic expansion, bus-error exceptions and an IRQ autovector.

RAM words: $0400 result (C0DE pass, E000 failure), $0402 stage, $0404 firmware
variant, $0406 bus-error count, $0408 interrupt count, $040C recovery PC.
The synthetic expansion supplies 5205/6800 at FA0000/FA0002 and writable words
at FF9000, FF9008 and FF900A. FF9002 deliberately never acknowledges; FF9004
returns BERR. The simulation drives level 3 during stage 13.

Bus-error recovery deliberately discards the exception frame and jumps to an
explicit continuation. This tests vector delivery, not bus-frame RTE recovery.
The interrupt handler uses the normal RTE path.
"""

from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path

ROM_BASE = 0xFC0000
ROM_SIZE = 192 * 1024
ENTRY = ROM_BASE + 0x100
STACK = 0x07FFF0
RESULT, STAGE, VARIANT, ERRORS, IRQS, RESUME = 0x400, 0x402, 0x404, 0x406, 0x408, 0x40C
BUS_ERROR_HANDLER = ROM_BASE + 0x2000
IRQ_HANDLER = ROM_BASE + 0x2100
FAILURE_HANDLER = ROM_BASE + 0x2200
ROM_SENTINEL = ROM_BASE + 0x3000
PASS = 0xC0DE
FAIL = 0xE000


class Program:
    """The few documented MC68000 encodings used by this diagnostic.

    Every memory operand uses absolute-long addressing, avoiding implicit
    sign extension of absolute-word addresses. Branches always use a signed
    16-bit displacement measured from the extension word's address.
    Reference: NXP M68000PRM, integer instruction encodings.
    https://www.nxp.com/docs/en/reference-manual/M68000PRM.pdf
    """

    def __init__(self, address: int) -> None:
        self.origin = address
        self.code = bytearray()
        self.labels: dict[str, int] = {}
        self.fixups: list[tuple[int, str, bool]] = []

    @property
    def pc(self) -> int:
        return self.origin + len(self.code)

    def word(self, value: int) -> None:
        self.code.extend((value & 0xFFFF).to_bytes(2, "big"))

    def long(self, value: int | str) -> None:
        if isinstance(value, str):
            self.fixups.append((len(self.code), value, False))
            value = 0
        self.word(value >> 16)
        self.word(value)

    def label(self, name: str) -> None:
        if name in self.labels:
            raise ValueError(f"duplicate label {name}")
        self.labels[name] = self.pc

    def move_imm(self, size: str, value: int | str, address: int) -> None:
        self.word({"b": 0x13FC, "w": 0x33FC, "l": 0x23FC}[size])
        if size == "l":
            self.long(value)
        else:
            if not isinstance(value, int):
                raise TypeError("only long immediates may refer to labels")
            self.word(value)
        self.long(address)

    def read(self, size: str, address: int) -> None:
        self.word({"b": 0x1039, "w": 0x3039, "l": 0x2039}[size])  # MOVE ...,D0
        self.long(address)

    def tas(self, address: int) -> None:
        self.word(0x4AF9)  # TAS.B (abs.l), read/modify/write under one AS pulse
        self.long(address)

    def compare(self, size: str, value: int, address: int) -> None:
        self.word({"b": 0x0C39, "w": 0x0C79, "l": 0x0CB9}[size])
        if size == "l":
            self.long(value)
        else:
            self.word(value)
        self.long(address)

    def branch(self, condition: str, label: str) -> None:
        self.word({"always": 0x6000, "ne": 0x6600, "eq": 0x6700}[condition])
        self.fixups.append((len(self.code), label, True))
        self.word(0)

    def jump(self, address: int) -> None:
        self.word(0x4EF9)
        self.long(address)

    def sr(self, value: int) -> None:
        self.word(0x46FC)  # MOVE #imm,SR
        self.word(value)

    def reset_stack(self) -> None:
        self.word(0x2E7C)  # MOVEA.L #imm,A7
        self.long(STACK)

    def check(self, size: str, value: int, address: int) -> None:
        self.compare(size, value, address)
        self.branch("ne", "fail")

    def stage(self, number: int) -> None:
        self.move_imm("w", number, STAGE)

    def fault(self, number: int, instruction, user: bool = False) -> None:
        """Continue only through the bus-error vector; fall-through fails."""
        continuation = f"fault_{number}_resume"
        self.move_imm("l", continuation, RESUME)
        if user:
            self.sr(0)  # user mode, instructions remain in the firmware window
        instruction()
        self.jump(FAILURE_HANDLER)
        self.label(continuation)
        self.reset_stack()
        self.check("w", number, ERRORS)

    def finish(self) -> bytes:
        for offset, label, relative in self.fixups:
            if label not in self.labels:
                raise ValueError(f"undefined label {label}")
            value = self.labels[label]
            if relative:
                value -= self.origin + offset
                if not -0x8000 <= value <= 0x7FFF:
                    raise ValueError(f"branch out of range: {label}")
                self.code[offset:offset + 2] = (value & 0xFFFF).to_bytes(2, "big")
            else:
                self.code[offset:offset + 4] = value.to_bytes(4, "big")
        return bytes(self.code)


def firmware(variant: int = 1) -> tuple[bytes, dict[str, int]]:
    if not 1 <= variant <= 0xFFFF:
        raise ValueError("variant must be 1..65535")
    p = Program(ENTRY)
    p.sr(0x2700)
    for address in (RESULT, STAGE, VARIANT, ERRORS, IRQS):
        p.move_imm("w", 0, address)
    p.move_imm("l", BUS_ERROR_HANDLER, 8)
    p.move_imm("l", IRQ_HANDLER, 27 * 4)
    p.move_imm("l", FAILURE_HANDLER, RESUME)

    p.stage(1)
    p.check("l", STACK, 0)  # the supervisor vector alias remains readable
    p.check("l", ENTRY, 4)
    p.check("l", 0x5AA56996, ROM_BASE + ROM_SIZE - 4)
    p.check("w", 0x68A5, ROM_SENTINEL)
    p.move_imm("b", 4, 0xFF8001)
    p.check("b", 4, 0xFF8001)

    p.stage(2)
    p.move_imm("w", 0x1234, 0x800)
    p.check("b", 0x12, 0x800)
    p.check("b", 0x34, 0x801)
    p.move_imm("b", 0xA5, 0x800)
    p.move_imm("b", 0x5A, 0x801)
    p.check("w", 0xA55A, 0x800)
    p.move_imm("l", 0x12345678, 0x804)
    p.check("w", 0x1234, 0x804)
    p.check("w", 0x5678, 0x806)
    p.check("l", 0x12345678, 0x804)
    p.move_imm("w", 0x1234, 0x820)
    p.tas(0x820)
    p.tas(0x821)
    p.check("w", 0x92B4, 0x820)

    p.stage(3)
    for bank, marker in enumerate((0x1101, 0x2202, 0x3303, 0x4404)):
        p.move_imm("w", marker, bank * 0x20000 + 0x880)
    for bank, marker in enumerate((0x1101, 0x2202, 0x3303, 0x4404)):
        p.check("w", marker, bank * 0x20000 + 0x880)
    p.move_imm("l", 0xFEA51234, 0x07FFFC)
    p.check("l", 0xFEA51234, 0x07FFFC)

    p.stage(4)
    for index, plane in enumerate((0xAAAA, 0xCCCC, 0xF0F0, 0xFF00)):
        p.move_imm("w", plane, 0x010000 + 2 * index)
        p.check("w", plane, 0x010000 + 2 * index)
    p.move_imm("b", 1, 0xFF8201)
    p.move_imm("b", 0, 0xFF8203)
    p.move_imm("b", 2, 0xFF820A)
    for mode in (2, 1, 0):
        p.move_imm("b", mode, 0xFF8260)
        p.check("b", mode, 0xFF8260)
    p.check("b", 1, 0xFF8201)
    p.check("b", 0, 0xFF8203)
    p.check("b", 2, 0xFF820A)
    for index in range(16):
        color = ((index & 7) << 8) | (((index + 2) & 7) << 4) | ((index + 4) & 7)
        p.move_imm("w", color | 0xF888, 0xFF8240 + 2 * index)
        p.check("w", color, 0xFF8240 + 2 * index)

    p.stage(5)
    p.check("l", 0x52056800, 0xFA0000)
    p.move_imm("w", 0x1234, 0xFF9000)
    p.move_imm("b", 0x56, 0xFF9000)
    p.move_imm("b", 0x78, 0xFF9001)
    p.check("w", 0x5678, 0xFF9000)
    p.tas(0xFF9000)
    p.tas(0xFF9001)
    p.check("w", 0xD6F8, 0xFF9000)
    p.move_imm("l", 0xABCDEF01, 0xFF9008)
    p.check("l", 0xABCDEF01, 0xFF9008)

    p.stage(6)
    p.fault(1, lambda: p.move_imm("w", 0x0BAD, 0xFA0000))
    p.stage(7)
    p.fault(2, lambda: p.move_imm("w", 0x0BAD, ROM_SENTINEL))
    p.check("w", 0x68A5, ROM_SENTINEL)
    p.stage(8)
    p.fault(3, lambda: p.read("w", 0x080000))
    p.stage(9)
    p.fault(4, lambda: p.read("w", 0xFF9002))
    p.stage(10)
    p.fault(5, lambda: p.read("w", 0xFF9004))
    p.stage(11)
    p.fault(6, lambda: p.move_imm("l", 0x0BADBEEF, 0))
    p.check("l", STACK, 0)
    p.stage(12)
    p.word(0x207C)  # MOVEA.L #$078000,A0
    p.long(0x078000)
    p.word(0x4E60)  # MOVE A0,USP
    p.fault(7, lambda: p.read("w", RESULT), user=True)
    p.fault(8, lambda: p.read("w", 0xFF9000), user=True)

    p.stage(13)
    p.sr(0x2000)  # enable the synthetic expansion's level-3 autovector
    p.label("irq_wait")
    p.compare("w", 1, IRQS)
    p.branch("ne", "irq_wait")
    p.sr(0x2700)
    p.move_imm("w", variant, VARIANT)
    p.move_imm("w", 0, STAGE)
    p.move_imm("w", PASS, RESULT)
    p.label("passed")
    p.branch("always", "passed")
    p.label("fail")
    p.jump(FAILURE_HANDLER)
    code = p.finish()
    if ENTRY + len(code) > BUS_ERROR_HANDLER:
        raise ValueError("diagnostic overlaps exception handlers")

    error = Program(BUS_ERROR_HANDLER)
    error.word(0x5279)  # ADDQ.W #1,$0406
    error.long(ERRORS)
    error.word(0x2079)  # MOVEA.L $040C,A0
    error.long(RESUME)
    error.word(0x4ED0)  # JMP (A0)

    irq = Program(IRQ_HANDLER)
    irq.word(0x5279)  # ADDQ.W #1,$0408
    irq.long(IRQS)
    irq.word(0x4E73)  # RTE

    failure = Program(FAILURE_HANDLER)
    failure.move_imm("w", FAIL, RESULT)
    failure.label("failed")
    failure.branch("always", "failed")

    image = bytearray(b"\xFF" * ROM_SIZE)

    def put(address: int, data: bytes) -> None:
        offset = address - ROM_BASE
        if offset < 0 or offset + len(data) > ROM_SIZE:
            raise ValueError("firmware write outside ROM")
        image[offset:offset + len(data)] = data

    put(ROM_BASE, STACK.to_bytes(4, "big") + ENTRY.to_bytes(4, "big"))
    put(ENTRY, code)
    put(BUS_ERROR_HANDLER, error.finish())
    put(IRQ_HANDLER, irq.finish())
    put(FAILURE_HANDLER, failure.finish())
    put(ROM_SENTINEL, b"\x68\xA5")
    put(ROM_BASE + ROM_SIZE - 4, b"\x5A\xA5\x69\x96")
    return bytes(image), p.labels


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out", type=Path, default=Path("build/diagnostics/fes-atari-st"))
    args = parser.parse_args()
    args.out.mkdir(parents=True, exist_ok=True)
    records = []
    for variant, name in ((1, "firmware"), (2, "firmware-variant")):
        image, labels = firmware(variant)
        (args.out / f"{name}.rom").write_bytes(image)
        (args.out / f"{name}.hex").write_text(
            "".join(f"{int.from_bytes(image[i:i + 2], 'big'):04x}\n"
                    for i in range(0, len(image), 2)), encoding="ascii")
        records.append({"name": name, "variant": variant, "size": len(image),
                        "sha256": hashlib.sha256(image).hexdigest(), "labels": labels})
    (args.out / "firmware.json").write_text(json.dumps(records, indent=2) + "\n", encoding="utf-8")
    print(f"FES Atari ST: wrote two original {ROM_SIZE}-byte firmware images to {args.out}")


if __name__ == "__main__":
    main()
