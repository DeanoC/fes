#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
# Copyright (c) 2026 FES contributors
"""Small two-pass NMOS 6502 assembler for the open Apple II diagnostics.

Only Python's standard library is needed. The syntax is conventional:

    label:  LDA #$20        ; comment
            STA $0400,X
            BNE label
            .org $F800
            .byte $01, 2, "TEXT"
            .word reset
            .fill 16, $EA
    NAME = $C000

Operands accept decimal, ``$hex``, ``%binary``, ``'c'`` characters, labels,
``*`` (current address), ``+``/``-`` and the ``<``/``>`` low/high byte
prefixes. Zero-page forms are chosen only when the operand value is known on
the first pass and fits in one byte, so instruction sizes never change between
passes.
"""

from __future__ import annotations

import re

# mnemonic -> {mode: opcode}
_OPCODES: dict[str, dict[str, int]] = {}


def _define(mnemonic: str, **modes: int) -> None:
    _OPCODES[mnemonic] = modes


_define("ADC", imm=0x69, zp=0x65, zpx=0x75, abs=0x6D, absx=0x7D, absy=0x79, indx=0x61, indy=0x71)
_define("AND", imm=0x29, zp=0x25, zpx=0x35, abs=0x2D, absx=0x3D, absy=0x39, indx=0x21, indy=0x31)
_define("ASL", acc=0x0A, zp=0x06, zpx=0x16, abs=0x0E, absx=0x1E)
_define("BCC", rel=0x90)
_define("BCS", rel=0xB0)
_define("BEQ", rel=0xF0)
_define("BIT", zp=0x24, abs=0x2C)
_define("BMI", rel=0x30)
_define("BNE", rel=0xD0)
_define("BPL", rel=0x10)
_define("BRK", imp=0x00)
_define("BVC", rel=0x50)
_define("BVS", rel=0x70)
_define("CLC", imp=0x18)
_define("CLD", imp=0xD8)
_define("CLI", imp=0x58)
_define("CLV", imp=0xB8)
_define("CMP", imm=0xC9, zp=0xC5, zpx=0xD5, abs=0xCD, absx=0xDD, absy=0xD9, indx=0xC1, indy=0xD1)
_define("CPX", imm=0xE0, zp=0xE4, abs=0xEC)
_define("CPY", imm=0xC0, zp=0xC4, abs=0xCC)
_define("DEC", zp=0xC6, zpx=0xD6, abs=0xCE, absx=0xDE)
_define("DEX", imp=0xCA)
_define("DEY", imp=0x88)
_define("EOR", imm=0x49, zp=0x45, zpx=0x55, abs=0x4D, absx=0x5D, absy=0x59, indx=0x41, indy=0x51)
_define("INC", zp=0xE6, zpx=0xF6, abs=0xEE, absx=0xFE)
_define("INX", imp=0xE8)
_define("INY", imp=0xC8)
_define("JMP", abs=0x4C, ind=0x6C)
_define("JSR", abs=0x20)
_define("LDA", imm=0xA9, zp=0xA5, zpx=0xB5, abs=0xAD, absx=0xBD, absy=0xB9, indx=0xA1, indy=0xB1)
_define("LDX", imm=0xA2, zp=0xA6, zpy=0xB6, abs=0xAE, absy=0xBE)
_define("LDY", imm=0xA0, zp=0xA4, zpx=0xB4, abs=0xAC, absx=0xBC)
_define("LSR", acc=0x4A, zp=0x46, zpx=0x56, abs=0x4E, absx=0x5E)
_define("NOP", imp=0xEA)
_define("ORA", imm=0x09, zp=0x05, zpx=0x15, abs=0x0D, absx=0x1D, absy=0x19, indx=0x01, indy=0x11)
_define("PHA", imp=0x48)
_define("PHP", imp=0x08)
_define("PLA", imp=0x68)
_define("PLP", imp=0x28)
_define("ROL", acc=0x2A, zp=0x26, zpx=0x36, abs=0x2E, absx=0x3E)
_define("ROR", acc=0x6A, zp=0x66, zpx=0x76, abs=0x6E, absx=0x7E)
_define("RTI", imp=0x40)
_define("RTS", imp=0x60)
_define("SBC", imm=0xE9, zp=0xE5, zpx=0xF5, abs=0xED, absx=0xFD, absy=0xF9, indx=0xE1, indy=0xF1)
_define("SEC", imp=0x38)
_define("SED", imp=0xF8)
_define("SEI", imp=0x78)
_define("STA", zp=0x85, zpx=0x95, abs=0x8D, absx=0x9D, absy=0x99, indx=0x81, indy=0x91)
_define("STX", zp=0x86, zpy=0x96, abs=0x8E)
_define("STY", zp=0x84, zpx=0x94, abs=0x8C)
_define("TAX", imp=0xAA)
_define("TAY", imp=0xA8)
_define("TSX", imp=0xBA)
_define("TXA", imp=0x8A)
_define("TXS", imp=0x9A)
_define("TYA", imp=0x98)

_SIZES = {"imp": 1, "acc": 1, "imm": 2, "zp": 2, "zpx": 2, "zpy": 2, "rel": 2,
          "indx": 2, "indy": 2, "abs": 3, "absx": 3, "absy": 3, "ind": 3}
_WIDE = {"zp": "abs", "zpx": "absx", "zpy": "absy"}


class AsmError(ValueError):
    pass


class _Unknown(Exception):
    pass


def _split_operands(text: str) -> list[str]:
    parts: list[str] = []
    current = ""
    quote = False
    for char in text:
        if char == '"':
            quote = not quote
        if char == "," and not quote:
            parts.append(current.strip())
            current = ""
        else:
            current += char
    if current.strip():
        parts.append(current.strip())
    return parts


class Assembler:
    """Assemble one source string into an image covering [base, base+size)."""

    def __init__(self, base: int, size: int, fill: int = 0xFF) -> None:
        self.base = base
        self.size = size
        self.fill = fill
        self.symbols: dict[str, int] = {}

    # Expression evaluation -------------------------------------------------
    def _value(self, text: str, pc: int, final: bool) -> int:
        text = text.strip()
        if not text:
            raise AsmError("empty expression")
        if text[0] == "<":
            return self._value(text[1:], pc, final) & 0xFF
        if text[0] == ">":
            return (self._value(text[1:], pc, final) >> 8) & 0xFF
        tokens = re.findall(r"\$[0-9A-Fa-f]+|%[01]+|'.'|[A-Za-z_][A-Za-z0-9_.]*|\d+|\*|[+\-]", text)
        if "".join(tokens) != text.replace(" ", ""):
            raise AsmError(f"cannot parse expression {text!r}")
        total = 0
        sign = 1
        expect_term = True
        for token in tokens:
            if token in "+-" and not expect_term:
                sign = 1 if token == "+" else -1
                expect_term = True
                continue
            if token == "-" and expect_term:
                sign = -sign
                continue
            if token.startswith("$"):
                term = int(token[1:], 16)
            elif token.startswith("%"):
                term = int(token[1:], 2)
            elif token.startswith("'"):
                term = ord(token[1])
            elif token == "*":
                term = pc
            elif token.isdigit():
                term = int(token)
            else:
                if token not in self.symbols:
                    if final:
                        raise AsmError(f"undefined symbol {token}")
                    raise _Unknown(token)
                term = self.symbols[token]
            total += sign * term
            sign = 1
            expect_term = False
        return total

    def _try_value(self, text: str, pc: int) -> int | None:
        try:
            return self._value(text, pc, final=False)
        except _Unknown:
            return None

    # Operand classification -----------------------------------------------
    @staticmethod
    def _mode(mnemonic: str, operand: str) -> tuple[str, str]:
        modes = _OPCODES[mnemonic]
        op = operand.strip()
        upper = op.upper()
        if not op:
            return ("acc" if "acc" in modes and "imp" not in modes else "imp"), ""
        if upper == "A" and "acc" in modes:
            return "acc", ""
        if "rel" in modes:
            return "rel", op
        if op.startswith("#"):
            return "imm", op[1:]
        match = re.fullmatch(r"\((.*),\s*[Xx]\s*\)", op)
        if match:
            return "indx", match.group(1)
        match = re.fullmatch(r"\((.*)\)\s*,\s*[Yy]", op)
        if match:
            return "indy", match.group(1)
        match = re.fullmatch(r"\((.*)\)", op)
        if match:
            return "ind", match.group(1)
        match = re.fullmatch(r"(.*),\s*[Xx]", op)
        if match:
            return "zpx", match.group(1)
        match = re.fullmatch(r"(.*),\s*[Yy]", op)
        if match:
            return "zpy", match.group(1)
        return "zp", op

    # Assembly ---------------------------------------------------------------
    def assemble(self, source: str) -> bytes:
        lines = source.splitlines()
        sizes: dict[int, tuple[str, int]] = {}
        self._pass(lines, sizes, final=False)
        image = self._pass(lines, sizes, final=True)
        return bytes(image)

    def _pass(self, lines: list[str], sizes: dict[int, tuple[str, int]], final: bool) -> bytearray:
        image = bytearray([self.fill] * self.size)
        pc = self.base

        def put(value: int) -> None:
            nonlocal pc
            offset = pc - self.base
            if not 0 <= offset < self.size:
                raise AsmError(f"address ${pc:04X} outside image")
            image[offset] = value & 0xFF
            pc += 1

        for number, raw in enumerate(lines):
            line = raw.split(";", 1)[0].rstrip()
            if not line.strip():
                continue
            try:
                match = re.match(r"\s*([A-Za-z_][A-Za-z0-9_.]*)\s*=\s*(.+)$", line)
                if match:
                    value = self._value(match.group(2), pc, final) if final else self._try_value(match.group(2), pc)
                    if value is not None:
                        self.symbols[match.group(1)] = value
                    continue
                match = re.match(r"\s*([A-Za-z_][A-Za-z0-9_.]*):(.*)$", line)
                if match:
                    name = match.group(1)
                    if not final and name in self.symbols and self.symbols[name] != pc:
                        raise AsmError(f"duplicate label {name}")
                    self.symbols[name] = pc
                    line = match.group(2)
                    if not line.strip():
                        continue
                parts = line.strip().split(None, 1)
                word = parts[0].upper()
                rest = parts[1].strip() if len(parts) > 1 else ""
                if word == ".ORG":
                    pc = self._value(rest, pc, True)
                    continue
                if word in (".BYTE", ".DB"):
                    for item in _split_operands(rest):
                        if item.startswith('"'):
                            for char in item.strip('"'):
                                put(ord(char))
                        else:
                            value = self._value(item, pc, final) if final else (self._try_value(item, pc) or 0)
                            put(value)
                    continue
                if word in (".WORD", ".DW"):
                    for item in _split_operands(rest):
                        value = self._value(item, pc, final) if final else (self._try_value(item, pc) or 0)
                        put(value)
                        put(value >> 8)
                    continue
                if word == ".FILL":
                    items = _split_operands(rest)
                    count = self._value(items[0], pc, True)
                    value = self._value(items[1], pc, True) if len(items) > 1 else 0
                    for _ in range(count):
                        put(value)
                    continue
                if word not in _OPCODES:
                    raise AsmError(f"unknown mnemonic {word}")
                mode, operand = self._mode(word, rest)
                key = number
                if not final:
                    if mode in _WIDE:
                        known = self._try_value(operand, pc)
                        if known is None or known > 0xFF or mode not in _OPCODES[word]:
                            mode = _WIDE[mode]
                    if mode not in _OPCODES[word]:
                        raise AsmError(f"{word} does not support {mode}")
                    sizes[key] = (mode, _SIZES[mode])
                else:
                    mode = sizes[key][0]
                opcode = _OPCODES[word][mode]
                put(opcode)
                if mode in ("imp", "acc"):
                    continue
                value = self._value(operand, pc - 1, final) if final else (self._try_value(operand, pc - 1) or 0)
                if mode == "rel":
                    offset = value - (pc + 1)
                    if final and not -128 <= offset <= 127:
                        raise AsmError(f"branch out of range ({offset})")
                    put(offset)
                elif _SIZES[mode] == 2:
                    if final and not 0 <= value <= 0xFF and mode != "imm":
                        raise AsmError(f"operand ${value:X} is not zero page")
                    put(value)
                else:
                    put(value)
                    put(value >> 8)
            except AsmError as error:
                raise AsmError(f"line {number + 1}: {raw.strip()}: {error}") from None
        return image


def assemble(source: str, base: int, size: int, fill: int = 0xFF) -> bytes:
    return Assembler(base, size, fill).assemble(source)
