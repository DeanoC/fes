#!/usr/bin/env python3
"""Generate or check the synthetic `fes.computer` 1.0 golden exchanges.

The `Endpoint` class below is the reference model of the mailbox described in
docs/computer-io.md. Every exchange in testdata/fes-computer-v1/exchanges.json
is produced by it; the Go test replays the file against an independent model,
and runtime/RTL consumers replay the same requests against their own code.
The fixtures describe synthetic endpoints and are never deployed.
"""

from __future__ import annotations

import argparse
import json
import sys
import zlib
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
OUTPUT = ROOT / "testdata" / "fes-computer-v1" / "exchanges.json"

SIGNATURE = 0xF5000000
ACK = 0x00800000
ERROR = 0x00400000
REQUEST = 0x80000000
BUILD_ID = bytes.fromhex("00112233445566778899aabbccddeeff")

CAP_VIDEO = 1 << 0
CAP_KEYBOARD = 1 << 1
CAP_PORTS = 1 << 2
CAP_AUDIO = 1 << 3
CAP_APPLE2_FLOPPY = 1 << 4
CAP_MOUSE = 1 << 8
MEDIA_CAPS = CAP_APPLE2_FLOPPY

OP_IDENTITY, OP_EXECUTION, OP_KEYBOARD, OP_CONTROLLER = 1, 2, 3, 4
OP_INFO, OP_BEGIN, OP_CHUNK, OP_DATA, OP_COMMIT, OP_EJECT = 5, 6, 7, 8, 9, 10
OP_MOUSE = 11
E_OPCODE, E_INDEX, E_ARGUMENT, E_STATE = 1, 2, 3, 4
ABSENT, EMPTY, LOADING, READY = 0, 1, 2, 3
UNITS = 8
CHUNK_MAX = 512


class Unit:
    def __init__(self, minimum: int, maximum: int) -> None:
        self.minimum = minimum
        self.maximum = maximum
        self.state = EMPTY
        self.data = bytearray()


class Endpoint:
    """Reference state machine. Rejected requests never change state."""

    def __init__(self, capabilities: int, units: dict[int, tuple[int, int]]) -> None:
        self.capabilities = capabilities
        self.units = {u: Unit(lo, hi) for u, (lo, hi) in units.items()}
        self.held = True
        self.rows = [0] * 9
        self.ports = [0, 0]
        self.mouse_buttons = 0
        self.mouse_motion = [0, 0]
        self.begin_unit: int | None = None
        self.begin_words: list[int] = []
        self.active: int | None = None
        self.total = 0
        self.expected_crc = 0
        self.received = 0
        self.crc = 0xFFFFFFFF
        self.chunk_words: list[int] = []
        self.chunk_length = 0
        self.chunk_received = 0
        self.ordinal = 0

    def identity(self) -> list[int]:
        words = [0x4546, 0x3153, 1, 0, 4, 1, 0, self.capabilities]
        words += [BUILD_ID[i] | BUILD_ID[i + 1] << 8 for i in range(0, 16, 2)]
        return words

    def media(self) -> bool:
        return self.capabilities & MEDIA_CAPS != 0

    def _clear_transfer(self) -> None:
        self.begin_unit = None
        self.begin_words = []
        self.active = None
        self.chunk_words = []
        self.chunk_length = 0
        self.chunk_received = 0
        self.ordinal = 0

    def request(self, op: int, index: int, arg: int) -> tuple[int, int]:
        """Return (error, data)."""
        if op == OP_IDENTITY:
            if index >= 16:
                return E_INDEX, 0
            if arg:
                return E_ARGUMENT, 0
            return 0, self.identity()[index]
        if op == OP_EXECUTION:
            if index:
                return E_INDEX, 0
            if arg > 1:
                return E_ARGUMENT, 0
            self.held = arg == 0
            if self.held:
                self.rows = [0] * 9
                self.ports = [0, 0]
                self.mouse_buttons = 0
            return 0, 0
        if op == OP_MOUSE:
            if not self.capabilities & CAP_MOUSE:
                return E_OPCODE, 0
            if index > 3:
                return E_INDEX, 0
            if self.held:
                return E_STATE, 0
            dx, dy = arg & 255, arg >> 8
            self.mouse_motion[0] += dx if dx < 128 else dx - 256
            self.mouse_motion[1] += dy if dy < 128 else dy - 256
            self.mouse_buttons = index
            return 0, 0
        if op == OP_KEYBOARD:
            if not self.capabilities & CAP_KEYBOARD:
                return E_OPCODE, 0
            if index > 8:
                return E_INDEX, 0
            if (index == 0 and arg & 0x000F) or (index == 8 and arg > 0xFF):
                return E_ARGUMENT, 0
            self.rows[index] = arg
            return 0, 0
        if op == OP_CONTROLLER:
            if not self.capabilities & CAP_PORTS:
                return E_OPCODE, 0
            if index > 1:
                return E_INDEX, 0
            if arg > 0xFF:
                return E_ARGUMENT, 0
            self.ports[index] = arg
            return 0, 0
        if op in (OP_INFO, OP_BEGIN, OP_CHUNK, OP_DATA, OP_COMMIT, OP_EJECT) and not self.media():
            return E_OPCODE, 0
        if op == OP_INFO:
            unit, field = index >> 3, index & 7
            if unit >= UNITS or field > 5:
                return E_INDEX, 0
            if arg:
                return E_ARGUMENT, 0
            u = self.units.get(unit)
            if u is None:
                return 0, 0
            values = [u.minimum & 0xFFFF, u.minimum >> 16, u.maximum & 0xFFFF,
                      u.maximum >> 16, CHUNK_MAX, u.state]
            return 0, values[field]
        if op == OP_BEGIN:
            unit, word = index >> 2, index & 3
            if unit not in self.units:
                return E_INDEX, 0
            if self.active is not None:
                return E_STATE, 0
            if self.begin_unit is None:
                if word != 0:
                    return E_INDEX, 0
            elif unit != self.begin_unit or word != len(self.begin_words):
                return E_INDEX, 0
            if word == 3:
                total = self.begin_words[0] | self.begin_words[1] << 16
                u = self.units[unit]
                if not u.minimum <= total <= u.maximum:
                    return E_ARGUMENT, 0
                self.active = unit
                self.total = total
                self.expected_crc = self.begin_words[2] | arg << 16
                self.received = 0
                self.crc = 0xFFFFFFFF
                self.begin_unit = None
                self.begin_words = []
                u.data = bytearray()
                return 0, 0
            if word == 0:
                self.begin_unit = unit
                self.units[unit].state = LOADING
            self.begin_words.append(arg)
            return 0, 0
        if op == OP_CHUNK:
            unit, word = index >> 2, index & 3
            if self.active is None:
                return E_STATE, 0
            if unit != self.active or word != len(self.chunk_words) or word > 2:
                return E_INDEX, 0
            if self.chunk_length:
                return E_STATE, 0
            if word == 2:
                offset = self.chunk_words[0] | self.chunk_words[1] << 16
                if offset != self.received or not 1 <= arg <= CHUNK_MAX or arg > self.total - self.received:
                    return E_ARGUMENT, 0
                self.chunk_words = []
                self.chunk_length = arg
                self.chunk_received = 0
                self.ordinal = 0
                return 0, 0
            self.chunk_words.append(arg)
            return 0, 0
        if op == OP_DATA:
            if self.active is None or not self.chunk_length:
                return E_STATE, 0
            if index != self.ordinal:
                return E_INDEX, 0
            remaining = self.chunk_length - self.chunk_received
            if remaining == 1 and arg >> 8:
                return E_ARGUMENT, 0
            payload = bytes([arg & 0xFF]) if remaining == 1 else bytes([arg & 0xFF, arg >> 8])
            self.units[self.active].data.extend(payload)
            self.crc = zlib.crc32(payload, self.crc ^ 0xFFFFFFFF) ^ 0xFFFFFFFF
            self.received += len(payload)
            self.chunk_received += len(payload)
            self.ordinal += 1
            if self.chunk_received == self.chunk_length:
                self.chunk_length = 0
            return 0, 0
        if op == OP_COMMIT:
            if index >= UNITS or index not in self.units:
                return E_INDEX, 0
            if arg:
                return E_ARGUMENT, 0
            if self.active is None or self.begin_unit is not None:
                return E_STATE, 0
            if index != self.active:
                return E_INDEX, 0
            if self.chunk_length or self.received != self.total:
                return E_STATE, 0
            if (self.crc ^ 0xFFFFFFFF) != self.expected_crc:
                return E_ARGUMENT, 0
            self.units[index].state = READY
            self._clear_transfer()
            return 0, 0
        if op == OP_EJECT:
            if index >= UNITS or index not in self.units:
                return E_INDEX, 0
            if arg:
                return E_ARGUMENT, 0
            if self.active == index or self.begin_unit == index:
                self._clear_transfer()
            self.units[index].state = EMPTY
            return 0, 0
        return E_OPCODE, 0


class Session:
    def __init__(self, name: str, endpoint: Endpoint, description: str) -> None:
        self.name = name
        self.endpoint = endpoint
        self.description = description
        self.toggle = False
        self.exchanges: list[dict] = []

    def send(self, name: str, op: int, index: int, arg: int) -> tuple[int, int]:
        fields = op << 24 | index << 16 | arg
        old = fields | (REQUEST if self.toggle else 0)
        self.toggle = not self.toggle
        new = fields | (REQUEST if self.toggle else 0)
        error, data = self.endpoint.request(op, index, arg)
        response = error if error else data
        gpi = SIGNATURE | (ACK if self.toggle else 0) | (ERROR if error else 0) | response
        self.exchanges.append({"name": name, "opcode": op, "index": index, "argument": arg,
                               "error": error, "data": response, "gpo": [old, new], "gpi": gpi})
        return error, data

    def identify(self) -> None:
        for word in range(16):
            self.send(f"identity-{word:02d}", OP_IDENTITY, word, 0)

    def document(self) -> dict:
        e = self.endpoint
        return {
            "name": self.name,
            "description": self.description,
            "capabilities": e.capabilities,
            "build_id": BUILD_ID.hex(),
            "units": [{"unit": u, "min": v.minimum, "max": v.maximum} for u, v in sorted(e.units.items())],
            "initial_request_toggle": False,
            "exchanges": self.exchanges,
            "final": {
                "held": e.held,
                "keyboard_rows": e.rows,
                "controller_ports": e.ports,
                "mouse_buttons": e.mouse_buttons,
                "mouse_motion": e.mouse_motion,
                "unit_states": {str(u): v.state for u, v in sorted(e.units.items())},
                "unit_sha_crc32": {str(u): zlib.crc32(bytes(v.data)) for u, v in sorted(e.units.items())},
            },
        }


def payload(size: int) -> bytes:
    return bytes((7 * i + (i >> 8)) & 0xFF for i in range(size))


def transfer(s: Session, unit: int, data: bytes, crc: int | None = None, label: str = "") -> None:
    total = len(data)
    crc = zlib.crc32(data) if crc is None else crc
    for word, value in enumerate((total & 0xFFFF, total >> 16, crc & 0xFFFF, crc >> 16)):
        s.send(f"{label}begin-{word}", OP_BEGIN, unit * 4 + word, value)
    offset = 0
    chunk = 0
    while offset < total:
        length = min(CHUNK_MAX, total - offset)
        for word, value in enumerate((offset & 0xFFFF, offset >> 16, length)):
            s.send(f"{label}chunk{chunk}-{word}", OP_CHUNK, unit * 4 + word, value)
        for ordinal in range((length + 1) // 2):
            lo = data[offset + 2 * ordinal]
            hi = data[offset + 2 * ordinal + 1] if 2 * ordinal + 1 < length else 0
            s.send(f"{label}chunk{chunk}-data-{ordinal:03d}", OP_DATA, ordinal, lo | hi << 8)
        offset += length
        chunk += 1


def input_session() -> Session:
    s = Session("keyboard-and-controllers",
                Endpoint(CAP_VIDEO | CAP_KEYBOARD | CAP_PORTS | CAP_AUDIO, {}),
                "HID key rows and controller ports, held and released; media opcodes absent.")
    s.identify()
    s.send("keyboard-reserved-usage", OP_KEYBOARD, 0, 0x0001)
    s.send("keyboard-row-out-of-range", OP_KEYBOARD, 9, 0)
    s.send("keyboard-modifier-high-byte", OP_KEYBOARD, 8, 0x0100)
    s.send("keyboard-a-held", OP_KEYBOARD, 0, 0x0010)          # usage 0x04 'A'
    s.send("keyboard-left-shift", OP_KEYBOARD, 8, 0x0002)
    s.send("controller-port0", OP_CONTROLLER, 0, 0x11)
    s.send("controller-port1", OP_CONTROLLER, 1, 0x28)
    s.send("controller-port-out-of-range", OP_CONTROLLER, 2, 0)
    s.send("controller-mask-out-of-range", OP_CONTROLLER, 0, 0x100)
    s.send("execution-invalid-argument", OP_EXECUTION, 0, 2)
    s.send("execution-invalid-index", OP_EXECUTION, 1, 1)
    s.send("release", OP_EXECUTION, 0, 1)
    s.send("keyboard-return-while-running", OP_KEYBOARD, 2, 0x0100)  # usage 0x28 Return
    s.send("media-info-absent", OP_INFO, 0, 0)
    s.send("media-eject-absent", OP_EJECT, 0, 0)
    s.send("hold-neutralises-input", OP_EXECUTION, 0, 0)
    s.send("release-again", OP_EXECUTION, 0, 1)
    s.send("keyboard-escape", OP_KEYBOARD, 2, 0x0200)                # usage 0x29 Escape
    return s


def media_session() -> Session:
    s = Session("live-media-unit-0",
                Endpoint(CAP_VIDEO | CAP_APPLE2_FLOPPY, {0: (1, 1030)}),
                "Synthetic endpoint: unit 0 advertises 1..1030 bytes. Transfers and eject "
                "while execution runs; every rejected request leaves state unchanged.")
    s.identify()
    s.send("release-without-media", OP_EXECUTION, 0, 1)
    for field in range(6):
        s.send(f"info-unit0-field{field}", OP_INFO, field, 0)
    s.send("info-unit1-absent-min", OP_INFO, 8, 0)
    s.send("info-invalid-field", OP_INFO, 6, 0)
    s.send("info-invalid-unit", OP_INFO, 64, 0)
    s.send("info-nonzero-argument", OP_INFO, 0, 1)
    s.send("begin-unimplemented-unit", OP_BEGIN, 4, 0)
    s.send("begin-out-of-order", OP_BEGIN, 1, 0)
    s.send("commit-without-transfer", OP_COMMIT, 0, 0)
    s.send("chunk-without-transfer", OP_CHUNK, 0, 0)
    s.send("data-without-chunk", OP_DATA, 0, 0)
    image = payload(1030)
    # First image: header, three chunks with errors interleaved, commit.
    total, crc = len(image), zlib.crc32(image)
    for word, value in enumerate((total & 0xFFFF, total >> 16, crc & 0xFFFF)):
        s.send(f"begin-{word}", OP_BEGIN, word, value)
    s.send("info-state-loading", OP_INFO, 5, 0)
    s.send("begin-header-restart-rejected", OP_BEGIN, 0, total)
    s.send("begin-3", OP_BEGIN, 3, crc >> 16)
    s.send("begin-while-active", OP_BEGIN, 0, 1)
    s.send("chunk-wrong-unit", OP_CHUNK, 4, 0)
    s.send("chunk0-0", OP_CHUNK, 0, 0)
    s.send("chunk0-1", OP_CHUNK, 1, 0)
    s.send("chunk-too-long", OP_CHUNK, 2, 513)
    s.send("chunk-zero-length", OP_CHUNK, 2, 0)
    s.send("chunk0-2", OP_CHUNK, 2, 512)
    s.send("chunk-while-armed", OP_CHUNK, 0, 0)
    s.send("data-out-of-order", OP_DATA, 1, 0)
    for ordinal in range(256):
        s.send(f"chunk0-data-{ordinal:03d}", OP_DATA, ordinal, image[2 * ordinal] | image[2 * ordinal + 1] << 8)
    s.send("data-after-chunk-closed", OP_DATA, 0, 0)
    s.send("hold-preserves-transfer", OP_EXECUTION, 0, 0)
    s.send("release-during-transfer", OP_EXECUTION, 0, 1)
    for word, value in enumerate((512, 0, 512)):
        s.send(f"chunk1-{word}", OP_CHUNK, word, value)
    for ordinal in range(256):
        s.send(f"chunk1-data-{ordinal:03d}", OP_DATA, ordinal,
               image[512 + 2 * ordinal] | image[513 + 2 * ordinal] << 8)
    s.send("commit-incomplete", OP_COMMIT, 0, 0)
    for word, value in enumerate((1024, 0, 6)):
        s.send(f"chunk2-{word}", OP_CHUNK, word, value)
    s.send("chunk2-data-000", OP_DATA, 0, image[1024] | image[1025] << 8)
    s.send("chunk2-data-001", OP_DATA, 1, image[1026] | image[1027] << 8)
    s.send("commit-open-chunk", OP_COMMIT, 0, 0)
    s.send("chunk2-data-002", OP_DATA, 2, image[1028] | image[1029] << 8)
    s.send("commit-wrong-unit", OP_COMMIT, 1, 0)
    s.send("commit-nonzero-argument", OP_COMMIT, 0, 1)
    s.send("commit", OP_COMMIT, 0, 0)
    s.send("info-state-ready", OP_INFO, 5, 0)
    s.send("eject", OP_EJECT, 0, 0)
    s.send("info-state-empty", OP_INFO, 5, 0)
    # Rejected totals do not arm the transfer; eject cancels partial staging.
    for word, value in enumerate((0, 0, 0)):
        s.send(f"zero-begin-{word}", OP_BEGIN, word, value)
    s.send("zero-begin-3-rejected", OP_BEGIN, 3, 0)
    s.send("zero-eject-cancels-header", OP_EJECT, 0, 0)
    for word, value in enumerate((2000, 0, 0)):
        s.send(f"oversize-begin-{word}", OP_BEGIN, word, value)
    s.send("oversize-begin-3-rejected", OP_BEGIN, 3, 0)
    s.send("oversize-eject-cancels-header", OP_EJECT, 0, 0)
    s.send("oversize-state-empty", OP_INFO, 5, 0)
    # A chunk offset other than the received count is rejected; eject cancels.
    short = payload(5)
    for word, value in enumerate((5, 0, zlib.crc32(short) & 0xFFFF, zlib.crc32(short) >> 16)):
        s.send(f"offset-begin-{word}", OP_BEGIN, word, value)
    s.send("offset-chunk-0", OP_CHUNK, 0, 1)
    s.send("offset-chunk-1", OP_CHUNK, 1, 0)
    s.send("offset-chunk-2-rejected", OP_CHUNK, 2, 4)
    s.send("offset-eject-cancels-transfer", OP_EJECT, 0, 0)
    s.send("offset-state-empty", OP_INFO, 5, 0)
    # A CRC mismatch keeps the unit loading until eject.
    bad = payload(5)
    transfer(s, 0, bad, crc=zlib.crc32(bad) ^ 1, label="badcrc-")
    s.send("badcrc-commit", OP_COMMIT, 0, 0)
    s.send("badcrc-state-loading", OP_INFO, 5, 0)
    s.send("badcrc-eject", OP_EJECT, 0, 0)
    # Odd-length image with padding rules, then commit while running.
    odd = payload(3)
    for word, value in enumerate((3, 0, zlib.crc32(odd) & 0xFFFF, zlib.crc32(odd) >> 16)):
        s.send(f"odd-begin-{word}", OP_BEGIN, word, value)
    for word, value in enumerate((0, 0, 3)):
        s.send(f"odd-chunk-{word}", OP_CHUNK, word, value)
    s.send("odd-data-000", OP_DATA, 0, odd[0] | odd[1] << 8)
    s.send("odd-padding-rejected", OP_DATA, 1, odd[2] | 0x0100)
    s.send("odd-data-001", OP_DATA, 1, odd[2])
    s.send("odd-commit", OP_COMMIT, 0, 0)
    s.send("odd-state-ready", OP_INFO, 5, 0)
    return s


def mouse_session() -> Session:
    s = Session("relative-mouse", Endpoint(CAP_VIDEO | CAP_MOUSE, {}),
                "Atomic signed relative motion and left/right state; held input rejected.")
    s.identify()
    s.send("held-motion-rejected", OP_MOUSE, 1, 0x017f)
    s.send("reserved-buttons-rejected", OP_MOUSE, 4, 0)
    s.send("release", OP_EXECUTION, 0, 1)
    s.send("signed-extrema", OP_MOUSE, 3, 0x807f)
    s.send("opposite-extrema", OP_MOUSE, 1, 0x7f80)
    s.send("release-buttons", OP_MOUSE, 0, 0)
    s.send("reserved-bit-rejected", OP_MOUSE, 128, 0xffff)
    s.send("hold-neutralises-buttons", OP_EXECUTION, 0, 0)
    s.send("held-button-rejected", OP_MOUSE, 2, 0)
    return s


def document() -> dict:
    return {
        "description": "Synthetic fes.computer 1.0 wire fixtures; never deploy.",
        "abi": {"id": "fes.computer", "tag": 4, "major": 1, "minor": 0},
        "crc_vectors": [{"hex": "010203", "crc32": zlib.crc32(bytes([1, 2, 3]))},
                        {"hex": "313233343536373839", "crc32": zlib.crc32(b"123456789")}],
        "scenarios": [input_session().document(), media_session().document(), mouse_session().document()],
    }


def render() -> str:
    return json.dumps(document(), indent=1, sort_keys=True) + "\n"


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    text = render()
    if args.check:
        if not OUTPUT.exists() or OUTPUT.read_text() != text:
            print(f"{OUTPUT.relative_to(ROOT)} is stale; run scripts/fes_computer_fixtures.py", file=sys.stderr)
            return 1
        return 0
    OUTPUT.parent.mkdir(parents=True, exist_ok=True)
    OUTPUT.write_text(text)
    return 0


if __name__ == "__main__":
    sys.exit(main())
