#!/usr/bin/env python3
"""Generate or check the synthetic `fes.computer` golden exchanges.

The `Endpoint` class below is the reference model of the 1.0 mailbox described
in docs/computer-io.md, and `Endpoint11` adds ABI 1.1: identity word 16, the
ATA disk, persistence words and the wall clock. Every exchange in
testdata/fes-computer-v1/exchanges.json (1.0 endpoints) and
exchanges-1.1.json is produced by them; the Go tests replay both files against
independent models, and runtime/RTL consumers replay the same requests
against their own code. The fixtures describe synthetic endpoints and are
never deployed.
"""

from __future__ import annotations

import argparse
from datetime import date
import json
import sys
import zlib
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
OUTPUT = ROOT / "testdata" / "fes-computer-v1" / "exchanges.json"
OUTPUT_11 = ROOT / "testdata" / "fes-computer-v1" / "exchanges-1.1.json"

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
CAP_HPS_DDR = 1 << 12
CAP_SESSION_DISPLAY = 1 << 13
CAP_ATA_DISK = 1 << 16
CAP_PERSISTENCE = 1 << 17
CAP_WALL_CLOCK = 1 << 18
CAP_PC386_MACHINE = 1 << 19
MEDIA_CAPS = CAP_APPLE2_FLOPPY

OP_IDENTITY, OP_EXECUTION, OP_KEYBOARD, OP_CONTROLLER = 1, 2, 3, 4
OP_INFO, OP_BEGIN, OP_CHUNK, OP_DATA, OP_COMMIT, OP_EJECT = 5, 6, 7, 8, 9, 10
OP_MOUSE = 11
OP_DISK_INFO, OP_DISK_CONTROL, OP_DISK_COMMIT, OP_DISK_DIRTY = 16, 17, 22, 23
OP_DATA_CONTROL, OP_DATA_READ, OP_DATA_WRITE, OP_DATA_INFO = 24, 25, 26, 27
OP_WALL_CLOCK = 28
E_OPCODE, E_INDEX, E_ARGUMENT, E_STATE = 1, 2, 3, 4
ABSENT, EMPTY, LOADING, READY = 0, 1, 2, 3
UNITS = 8
CHUNK_MAX = 512
VERIFYING = 4

# fes.media.ata-disk 1.0
SECTOR = 512
DISK_MIN_SECTORS, DISK_MAX_SECTORS = 2048, 262144
CHUNK_SECTORS = 128
DIRTY_WORDS = 128
MAX_CYLINDERS, MAX_HEADS, MAX_SPT = 16383, 16, 63
FLAG_DIRTY, FLAG_FROZEN, FLAG_BUSY, FLAG_ERROR = 1, 2, 4, 8
PREPARE, COMMIT, EJECT, FREEZE, RESUME, SAVED = range(6)

# fes.persistence.words 1.0 with layout fes.pc386.machine 1.0
DATA_FREEZE, DATA_BEGIN, DATA_COMMIT, DATA_RESUME = range(4)
PC386_TAG, PC386_WORDS = 3, 72
PC386_DEFAULTS = [0, 0, 0, 0, 0x220, 7, 1, 0] + [0] * 64

# fes.rtc.wall-clock 1.0
YEAR_MIN, YEAR_MAX = 1980, 2099


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


def pc386_machine_valid(words: list[int]) -> bool:
    """Commit-time check of fes.pc386.machine 1.0 (docs/computer-io.md)."""
    preset, frequency, profile, turbo, base, irq, dma, reserved = words[:8]
    if preset >= 8 or profile > 1 or turbo > 1 or reserved:
        return False
    if frequency and (preset == 0 or not 10 <= frequency <= 1000):
        return False
    return base in (0x220, 0x240) and irq in (2, 3, 5, 7) and dma == 1


def month_days(year: int, month: int) -> int:
    if month == 2:
        return 29 if year % 4 == 0 and (year % 100 or year % 400 == 0) else 28
    return 30 if month in (4, 6, 9, 11) else 31


def window_pattern(size: int) -> bytes:
    """Synthetic contents of the disk region of the DDR window."""
    return bytes((offset * 7 + (offset >> 11)) & 0xFF for offset in range(size))


class Endpoint11(Endpoint):
    """ABI 1.1 reference state machine; legacy opcodes are the 1.0 model's.

    The core's CRC pass is asynchronous on hardware. Here it resolves after
    `verify_reads` DiskInfo state-field reads have reported verifying, so a
    replay sees the runtime poll. Machine events (guest sector writes and
    guest CMOS writes) are scripted by the session between exchanges.
    """

    def __init__(self, capabilities: int, abi_minor: int, window: bytes = b"", verify_reads: int = 1) -> None:
        super().__init__(capabilities, {})
        self.abi_minor = abi_minor
        self.window = window
        self.verify_reads = verify_reads
        # ATA disk
        self.disk_state = EMPTY
        self.frozen = False
        self.saved = False
        self.writer_busy = False
        self.pending_sector: int | None = None
        self.error = False
        self.epoch = 0
        self.dirty: set[int] = set()
        self.staged: list[int] = []
        self.geometry: list[int] = []
        self.verify_left = 0
        self.verify_result = EMPTY
        # Persistence
        self.words = list(PC386_DEFAULTS)
        self.snapshot: list[int] | None = None
        self.staging: dict[int, int] | None = None
        # Wall clock
        self.clock_staged: list[int] = []
        self.clock: list[int] = []

    def identity(self) -> list[int]:
        words = super().identity()
        words[6] = self.abi_minor
        words[7] = self.capabilities & 0xFFFF
        if self.abi_minor:
            words.append(self.capabilities >> 16)
        return words

    # Machine side ---------------------------------------------------------

    def guest_sector_write(self, sector: int) -> None:
        self.guest_sector_write_begin(sector)
        self.guest_sector_write_complete()

    def guest_sector_write_begin(self, sector: int) -> None:
        assert self.disk_state == READY and not self.held and not self.frozen
        assert not self.writer_busy and sector < self.geometry[0]
        self.writer_busy = True
        self.pending_sector = sector

    def guest_sector_write_complete(self) -> None:
        assert self.writer_busy and self.pending_sector is not None
        sector = self.pending_sector
        self.dirty.add(sector // CHUNK_SECTORS)
        self.epoch = (self.epoch + 1) & 0xFFFF
        self.writer_busy = False
        self.pending_sector = None

    def guest_cmos_write(self, byte: int, value: int) -> None:
        assert not self.held and 0 <= byte < 128 and 0 <= value < 256
        word = 8 + byte // 2
        shift = 8 * (byte & 1)
        self.words[word] = self.words[word] & ~(0xFF << shift) | value << shift

    # Mailbox --------------------------------------------------------------

    def _flags(self) -> int:
        return ((FLAG_DIRTY if self.dirty else 0) | (FLAG_FROZEN if self.frozen else 0)
                | (FLAG_BUSY if self.writer_busy else 0) | (FLAG_ERROR if self.error else 0))

    def _clear_disk(self, state: int) -> None:
        self.disk_state = state
        self.frozen = self.saved = self.error = False
        self.writer_busy = False
        self.pending_sector = None
        self.dirty = set()
        self.staged = []
        self.geometry = []
        self.verify_left = 0

    def request(self, op: int, index: int, arg: int) -> tuple[int, int]:
        if op == OP_IDENTITY:
            if index >= (17 if self.abi_minor else 16):
                return E_INDEX, 0
            if arg:
                return E_ARGUMENT, 0
            return 0, self.identity()[index]
        if op == OP_EXECUTION:
            error, data = super().request(op, index, arg)
            if not error:
                # Hold abandons a persistence snapshot; any Execution closes
                # restore staging and drops a partly staged wall clock.
                if self.held:
                    self.snapshot = None
                self.staging = None
                self.clock_staged = []
            return error, data
        if op in (OP_DISK_INFO, OP_DISK_CONTROL, OP_DISK_COMMIT, OP_DISK_DIRTY):
            if not self.capabilities & CAP_ATA_DISK:
                return E_OPCODE, 0
            return self._disk(op, index, arg)
        if op in (OP_DATA_CONTROL, OP_DATA_READ, OP_DATA_WRITE, OP_DATA_INFO):
            persistence_caps = CAP_PERSISTENCE | CAP_PC386_MACHINE
            if self.capabilities & persistence_caps != persistence_caps:
                return E_OPCODE, 0
            return self._data(op, index, arg)
        if op == OP_WALL_CLOCK:
            if not self.capabilities & CAP_WALL_CLOCK:
                return E_OPCODE, 0
            return self._wall_clock(index, arg)
        return super().request(op, index, arg)

    def _disk(self, op: int, index: int, arg: int) -> tuple[int, int]:
        if op == OP_DISK_INFO:
            unit, field = index >> 3, index & 7
            if unit >= UNITS:
                return E_INDEX, 0
            if arg:
                return E_ARGUMENT, 0
            if unit:
                return 0, 0
            state = self.disk_state
            if field == 5 and state == VERIFYING:
                self.verify_left -= 1
                if not self.verify_left:
                    self.disk_state = self.verify_result
                    self.error = self.verify_result != READY
            values = [(DISK_MIN_SECTORS * SECTOR) & 0xFFFF, (DISK_MIN_SECTORS * SECTOR) >> 16,
                      (DISK_MAX_SECTORS * SECTOR) & 0xFFFF, (DISK_MAX_SECTORS * SECTOR) >> 16,
                      CHUNK_SECTORS, state, self._flags(), self.epoch]
            return 0, values[field]
        if op == OP_DISK_CONTROL:
            if index:
                return E_INDEX, 0
            if arg > SAVED:
                return E_ARGUMENT, 0
            if arg in (PREPARE, EJECT):
                if not self.held or (self.frozen and not self.saved):
                    return E_STATE, 0
                self._clear_disk(LOADING if arg == PREPARE else EMPTY)
                return 0, 0
            if arg == COMMIT:
                if not self.held or self.disk_state != LOADING or len(self.staged) != 8:
                    return E_STATE, 0
                size, cylinders, heads, spt = self.staged[0] | self.staged[1] << 16, *self.staged[2:5]
                crc = self.staged[5] | self.staged[6] << 16
                match = zlib.crc32(self.window[:size * SECTOR]) == crc
                self.staged = []
                self.error = False
                self.disk_state = VERIFYING
                self.verify_left = self.verify_reads
                self.verify_result = READY if match else LOADING
                self.geometry = [size, cylinders, heads, spt] if match else []
                return 0, 0
            if arg == FREEZE:
                if self.disk_state != READY:
                    return E_STATE, 0
                if not self.frozen:
                    self.frozen, self.saved = True, False
                if self.writer_busy:
                    self.guest_sector_write_complete()
                return 0, 0
            if arg == RESUME:
                self.frozen = self.saved = False
                return 0, 0
            if not self.frozen or self.writer_busy:  # SAVED
                return E_STATE, 0
            self.dirty = set()
            self.saved = True
            return 0, 0
        if op == OP_DISK_COMMIT:
            if index >= 8:
                return E_INDEX, 0
            if not self.held or self.disk_state != LOADING:
                return E_STATE, 0
            if index != len(self.staged):
                return E_INDEX, 0
            staged = self.staged
            if index == 1 and not DISK_MIN_SECTORS <= (staged[0] | arg << 16) <= DISK_MAX_SECTORS:
                return E_ARGUMENT, 0
            if index == 2 and not 1 <= arg <= MAX_CYLINDERS:
                return E_ARGUMENT, 0
            if index == 3 and not 1 <= arg <= MAX_HEADS:
                return E_ARGUMENT, 0
            if index == 4 and (not 1 <= arg <= MAX_SPT
                               or staged[2] * staged[3] * arg > (staged[0] | staged[1] << 16)):
                return E_ARGUMENT, 0
            if index == 7 and arg:
                return E_ARGUMENT, 0
            staged.append(arg)
            return 0, 0
        # OP_DISK_DIRTY
        if index >= DIRTY_WORDS:
            return E_INDEX, 0
        if arg:
            return E_ARGUMENT, 0
        if not self.frozen:
            return E_STATE, 0
        return 0, sum(1 << bit for bit in range(16) if 16 * index + bit in self.dirty)

    def _data(self, op: int, index: int, arg: int) -> tuple[int, int]:
        if op == OP_DATA_CONTROL:
            if index:
                return E_INDEX, 0
            if arg == DATA_FREEZE:
                if self.held:
                    return E_STATE, 0
                if self.snapshot is None:
                    self.snapshot = list(self.words)
                return 0, 0
            if arg == DATA_BEGIN:
                if not self.held:
                    return E_STATE, 0
                self.staging = {}
                return 0, 0
            if arg == DATA_COMMIT:
                if not self.held or self.staging is None or len(self.staging) != PC386_WORDS:
                    return E_STATE, 0
                words = [self.staging[i] for i in range(PC386_WORDS)]
                if not pc386_machine_valid(words):
                    return E_ARGUMENT, 0
                self.words = words
                self.staging = None
                return 0, 0
            if arg == DATA_RESUME:
                if self.snapshot is None:
                    return E_STATE, 0
                self.snapshot = None
                return 0, 0
            return E_ARGUMENT, 0
        if op == OP_DATA_READ:
            if index >= PC386_WORDS:
                return E_INDEX, 0
            if arg:
                return E_ARGUMENT, 0
            if self.snapshot is None:
                return E_STATE, 0
            return 0, self.snapshot[index]
        if op == OP_DATA_WRITE:
            if index >= PC386_WORDS:
                return E_INDEX, 0
            if not self.held or self.staging is None:
                return E_STATE, 0
            self.staging[index] = arg
            return 0, 0
        # OP_DATA_INFO
        if index > 3:
            return E_INDEX, 0
        if arg:
            return E_ARGUMENT, 0
        return 0, [PC386_WORDS, PC386_TAG, 1, 0][index]

    def _wall_clock(self, index: int, arg: int) -> tuple[int, int]:
        if index > 3:
            return E_INDEX, 0
        if not self.held:
            return E_STATE, 0
        if index and index != len(self.clock_staged):
            return E_INDEX, 0
        low, high = arg & 0xFF, arg >> 8
        if index == 0 and not YEAR_MIN <= arg <= YEAR_MAX:
            return E_ARGUMENT, 0
        if index == 1 and not (1 <= low <= 12 and 1 <= high <= month_days(self.clock_staged[0], low)):
            return E_ARGUMENT, 0
        if index == 2 and not (low <= 23 and high <= 59):
            return E_ARGUMENT, 0
        if index == 3:
            if not (low <= 59 and 1 <= high <= 7):
                return E_ARGUMENT, 0
            year = self.clock_staged[0]
            month_day = self.clock_staged[1]
            expected_weekday = (date(year, month_day & 0xFF, month_day >> 8).weekday() + 1) % 7 + 1
            if high != expected_weekday:
                return E_ARGUMENT, 0
        if index == 0:
            self.clock_staged = []
        self.clock_staged.append(arg)
        if index == 3:
            year, month_day, hour_minute, second_weekday = self.clock_staged
            self.clock = [year, month_day & 0xFF, month_day >> 8, hour_minute & 0xFF,
                          hour_minute >> 8, second_weekday & 0xFF, second_weekday >> 8]
            self.clock_staged = []
        return 0, 0


class Session11(Session):
    def __init__(self, name: str, endpoint: Endpoint11, description: str) -> None:
        super().__init__(name, endpoint, description)
        self.events: list[dict] = []

    def identify(self) -> None:
        count = 17 if self.endpoint.abi_minor else 16
        for word in range(count):
            self.send(f"identity-{word:02d}", OP_IDENTITY, word, 0)

    def sector_write(self, sector: int) -> None:
        self.endpoint.guest_sector_write(sector)
        self.events.append({"before": len(self.exchanges), "kind": "sector-write", "sector": sector})

    def sector_write_begin(self, sector: int) -> None:
        self.endpoint.guest_sector_write_begin(sector)
        self.events.append({"before": len(self.exchanges), "kind": "sector-write-begin", "sector": sector})

    def sector_write_complete(self) -> None:
        self.endpoint.guest_sector_write_complete()
        self.events.append({"before": len(self.exchanges), "kind": "sector-write-complete"})

    def cmos_write(self, byte: int, value: int) -> None:
        self.endpoint.guest_cmos_write(byte, value)
        self.events.append({"before": len(self.exchanges), "kind": "cmos-write", "byte": byte, "value": value})

    def document(self) -> dict:
        e = self.endpoint
        doc = super().document()
        doc["abi_minor"] = e.abi_minor
        doc["identity_word_count"] = 17 if e.abi_minor else 16
        doc["verify_reads"] = e.verify_reads
        doc["window"] = {"bytes": len(e.window), "pattern": "(offset * 7 + (offset >> 11)) & 255"}
        doc["machine_events"] = self.events
        doc["final"].update({
            "disk_state": e.disk_state,
            "disk_flags": e._flags(),
            "disk_epoch": e.epoch,
            "disk_dirty_chunks": sorted(e.dirty),
            "disk_geometry": e.geometry,
            "persistent_words": e.words,
            "wall_clock": e.clock,
        })
        return doc


PC386_CAPS = (CAP_VIDEO | CAP_KEYBOARD | CAP_AUDIO | CAP_MOUSE | CAP_HPS_DDR | CAP_SESSION_DISPLAY
              | CAP_ATA_DISK | CAP_PERSISTENCE | CAP_WALL_CLOCK | CAP_PC386_MACHINE)
DISK_SECTORS = 4096  # a 2 MiB synthetic image


def persistence_capability_session(name: str, capabilities: int) -> Session11:
    s = Session11(name, Endpoint11(capabilities, 1),
                  "Persistence and its pc386 layout capability must be advertised together.")
    s.identify()
    s.send("data-info-with-unpaired-capability", OP_DATA_INFO, 0, 0)
    s.send("data-begin-with-unpaired-capability", OP_DATA_CONTROL, 0, DATA_BEGIN)
    return s


def minor0_session() -> Session11:
    s = Session11("minor-0-endpoint", Endpoint11(CAP_VIDEO | CAP_KEYBOARD | CAP_APPLE2_FLOPPY, 0),
                  "A 1.0 endpoint under a 1.1 runtime: identity stops at word 15 and the 1.1 opcodes "
                  "are absent.")
    s.identify()
    s.send("identity-16-rejected", OP_IDENTITY, 16, 0)
    s.send("disk-info-absent", OP_DISK_INFO, 5, 0)
    s.send("disk-control-absent", OP_DISK_CONTROL, 0, PREPARE)
    s.send("disk-commit-absent", OP_DISK_COMMIT, 0, 0)
    s.send("disk-dirty-absent", OP_DISK_DIRTY, 0, 0)
    s.send("data-info-absent", OP_DATA_INFO, 0, 0)
    s.send("data-control-absent", OP_DATA_CONTROL, 0, DATA_BEGIN)
    s.send("wall-clock-absent", OP_WALL_CLOCK, 0, 2026)
    s.send("media-info-present", OP_INFO, 5, 0)
    return s


def machine_record() -> list[int]:
    """A restored pc386 record: 386DX-25, queue16, SB at 240h IRQ 5."""
    cmos = [(2 * i * 37 + 11 & 0xFF) | ((2 * i + 1) * 37 + 11 & 0xFF) << 8 for i in range(64)]
    return [3, 0, 1, 0, 0x240, 5, 1, 0] + cmos


def persistence_session() -> Session11:
    s = Session11("pc386-persistence-and-wall-clock", Endpoint11(PC386_CAPS, 1),
                  "Restore of the fes.pc386.machine record and the wall-clock seed while held, then "
                  "the record snapshot at Stop with a guest CMOS change.")
    s.identify()
    s.send("identity-17-rejected", OP_IDENTITY, 17, 0)
    s.send("legacy-media-absent", OP_INFO, 5, 0)
    s.send("legacy-snapshot-absent", 12, 0, 0)  # MediaSnapshotInfo
    for index in range(4):
        s.send(f"data-info-{index}", OP_DATA_INFO, index, 0)
    s.send("data-info-index-rejected", OP_DATA_INFO, 4, 0)
    s.send("data-info-argument-rejected", OP_DATA_INFO, 0, 1)
    s.send("data-read-before-freeze", OP_DATA_READ, 0, 0)
    s.send("data-write-before-begin", OP_DATA_WRITE, 0, 3)
    s.send("data-freeze-while-held", OP_DATA_CONTROL, 0, DATA_FREEZE)
    s.send("data-control-index-rejected", OP_DATA_CONTROL, 1, DATA_BEGIN)
    s.send("data-control-argument-rejected", OP_DATA_CONTROL, 0, 4)
    s.send("data-begin", OP_DATA_CONTROL, 0, DATA_BEGIN)
    s.send("data-write-index-rejected", OP_DATA_WRITE, PC386_WORDS, 0)
    record = machine_record()
    for index in range(PC386_WORDS - 1):
        s.send(f"data-write-{index:02d}", OP_DATA_WRITE, index, record[index])
    s.send("data-commit-incomplete", OP_DATA_CONTROL, 0, DATA_COMMIT)
    s.send(f"data-write-{PC386_WORDS - 1}", OP_DATA_WRITE, PC386_WORDS - 1, record[-1])
    # Words are opaque to the transfer; the layout is checked at commit.
    for name, index, bad in (("sb-base", 4, 0x230), ("sb-irq", 5, 9), ("sb-dma", 6, 3),
                             ("preset", 0, 8), ("profile", 2, 2), ("turbo", 3, 2), ("reserved", 7, 1),
                             ("frequency-low", 1, 9), ("frequency-high", 1, 1001)):
        s.send(f"stage-invalid-{name}", OP_DATA_WRITE, index, bad)
        s.send(f"commit-invalid-{name}", OP_DATA_CONTROL, 0, DATA_COMMIT)
        s.send(f"restore-{name}", OP_DATA_WRITE, index, record[index])
    s.send("stage-preset-max", OP_DATA_WRITE, 0, 0)
    s.send("stage-frequency-override", OP_DATA_WRITE, 1, 250)
    s.send("commit-override-under-max", OP_DATA_CONTROL, 0, DATA_COMMIT)
    s.send("restore-preset", OP_DATA_WRITE, 0, record[0])
    s.send("data-commit", OP_DATA_CONTROL, 0, DATA_COMMIT)
    s.send("data-write-after-commit", OP_DATA_WRITE, 0, 0)
    s.send("data-commit-again", OP_DATA_CONTROL, 0, DATA_COMMIT)
    # Wall clock: 2026-10-28 14:05:37, a Wednesday (weekday 4).
    s.send("clock-word-1-first", OP_WALL_CLOCK, 1, 10 | 28 << 8)
    s.send("clock-index-rejected", OP_WALL_CLOCK, 4, 0)
    s.send("clock-year-low", OP_WALL_CLOCK, 0, 1979)
    s.send("clock-year-high", OP_WALL_CLOCK, 0, 2100)
    s.send("clock-year-first", OP_WALL_CLOCK, 0, 2025)
    s.send("clock-year-restart", OP_WALL_CLOCK, 0, 2026)
    s.send("clock-february-30", OP_WALL_CLOCK, 1, 2 | 30 << 8)
    s.send("clock-month-13", OP_WALL_CLOCK, 1, 13 | 1 << 8)
    s.send("clock-day-0", OP_WALL_CLOCK, 1, 10 | 0 << 8)
    s.send("clock-month-day", OP_WALL_CLOCK, 1, 10 | 28 << 8)
    s.send("clock-word-out-of-order", OP_WALL_CLOCK, 3, 37 | 4 << 8)
    s.send("clock-hour-24", OP_WALL_CLOCK, 2, 24 | 5 << 8)
    s.send("clock-minute-60", OP_WALL_CLOCK, 2, 14 | 60 << 8)
    s.send("clock-hour-minute", OP_WALL_CLOCK, 2, 14 | 5 << 8)
    s.send("clock-second-60", OP_WALL_CLOCK, 3, 60 | 4 << 8)
    s.send("clock-weekday-0", OP_WALL_CLOCK, 3, 37 | 0 << 8)
    s.send("clock-weekday-8", OP_WALL_CLOCK, 3, 37 | 8 << 8)
    s.send("clock-weekday-mismatch", OP_WALL_CLOCK, 3, 37 | 3 << 8)
    s.send("clock-second-weekday", OP_WALL_CLOCK, 3, 37 | 4 << 8)
    s.send("clock-word-1-after-apply", OP_WALL_CLOCK, 1, 10 | 28 << 8)
    # A leap day is a valid date; an Execution request drops the staged words.
    s.send("leap-year", OP_WALL_CLOCK, 0, 2028)
    s.send("leap-day", OP_WALL_CLOCK, 1, 2 | 29 << 8)
    s.send("release", OP_EXECUTION, 0, 1)
    s.send("clock-while-running", OP_WALL_CLOCK, 0, 2026)
    s.send("data-begin-while-running", OP_DATA_CONTROL, 0, DATA_BEGIN)
    s.cmos_write(0x40, 0x5A)
    s.cmos_write(0x00, 0x59)
    s.send("data-freeze", OP_DATA_CONTROL, 0, DATA_FREEZE)
    s.cmos_write(0x41, 0xA5)  # after the snapshot: not in it
    s.send("data-freeze-again", OP_DATA_CONTROL, 0, DATA_FREEZE)
    s.send("data-read-index-rejected", OP_DATA_READ, PC386_WORDS, 0)
    s.send("data-read-argument-rejected", OP_DATA_READ, 0, 1)
    for index in range(PC386_WORDS):
        s.send(f"data-read-{index:02d}", OP_DATA_READ, index, 0)
    s.send("data-resume", OP_DATA_CONTROL, 0, DATA_RESUME)
    s.send("data-read-after-resume", OP_DATA_READ, 0, 0)
    s.send("data-resume-again", OP_DATA_CONTROL, 0, DATA_RESUME)
    s.send("data-freeze-for-stop", OP_DATA_CONTROL, 0, DATA_FREEZE)
    s.send("hold", OP_EXECUTION, 0, 0)
    s.send("data-read-after-hold", OP_DATA_READ, 0, 0)
    return s


def disk_commit(s: Session11, words: list[int], label: str) -> None:
    for index, value in enumerate(words):
        s.send(f"{label}commit-word-{index}", OP_DISK_COMMIT, index, value)


def disk_session() -> Session11:
    window = window_pattern(DISK_SECTORS * SECTOR)
    crc = zlib.crc32(window)
    s = Session11("pc386-ata-disk", Endpoint11(PC386_CAPS, 1, window, verify_reads=1),
                  "A 2 MiB image in the synthetic window: prepare, geometry and CRC, a failed and a "
                  "matching commit with polling, guest writes, then freeze, dirty map and Saved.")
    s.identify()
    for field in range(8):
        s.send(f"disk-info-field{field}", OP_DISK_INFO, field, 0)
    s.send("disk-info-unit1-absent", OP_DISK_INFO, 8 + 5, 0)
    s.send("disk-info-unit-out-of-range", OP_DISK_INFO, 64, 0)
    s.send("disk-info-argument-rejected", OP_DISK_INFO, 5, 1)
    s.send("disk-commit-word-before-prepare", OP_DISK_COMMIT, 0, DISK_SECTORS)
    s.send("disk-control-unit-rejected", OP_DISK_CONTROL, 1, PREPARE)
    s.send("disk-control-argument-rejected", OP_DISK_CONTROL, 0, 6)
    s.send("disk-freeze-empty", OP_DISK_CONTROL, 0, FREEZE)
    s.send("disk-saved-unfrozen", OP_DISK_CONTROL, 0, SAVED)
    s.send("disk-dirty-unfrozen", OP_DISK_DIRTY, 0, 0)
    s.send("disk-prepare", OP_DISK_CONTROL, 0, PREPARE)
    s.send("disk-state-loading", OP_DISK_INFO, 5, 0)
    s.send("disk-commit-without-words", OP_DISK_CONTROL, 0, COMMIT)
    s.send("disk-commit-word-index-rejected", OP_DISK_COMMIT, 8, 0)
    s.send("disk-commit-word-1-first", OP_DISK_COMMIT, 1, 0)
    s.send("size-lo", OP_DISK_COMMIT, 0, DISK_SECTORS)
    s.send("size-oversize", OP_DISK_COMMIT, 1, 4)
    s.send("size-hi", OP_DISK_COMMIT, 1, 0)
    s.send("cylinders-zero", OP_DISK_COMMIT, 2, 0)
    s.send("cylinders-over-16383", OP_DISK_COMMIT, 2, 16384)
    s.send("cylinders-five", OP_DISK_COMMIT, 2, 5)
    s.send("heads-17", OP_DISK_COMMIT, 3, 17)
    s.send("heads", OP_DISK_COMMIT, 3, 16)
    s.send("chs-over-size", OP_DISK_COMMIT, 4, 63)  # 5 x 16 x 63 = 5040 > 4096
    # A rejected word is not consumed; prepare restarts the staging.
    s.send("re-prepare", OP_DISK_CONTROL, 0, PREPARE)
    disk_commit(s, [DISK_SECTORS, 0, 4, 16], "")
    s.send("spt-zero", OP_DISK_COMMIT, 4, 0)
    s.send("spt-64", OP_DISK_COMMIT, 4, 64)
    s.send("spt", OP_DISK_COMMIT, 4, 63)
    s.send("crc-lo-wrong", OP_DISK_COMMIT, 5, (crc ^ 1) & 0xFFFF)
    s.send("crc-hi", OP_DISK_COMMIT, 6, crc >> 16)
    s.send("reserved-nonzero", OP_DISK_COMMIT, 7, 1)
    s.send("reserved", OP_DISK_COMMIT, 7, 0)
    s.send("commit-word-after-complete", OP_DISK_COMMIT, 0, DISK_SECTORS)
    s.send("disk-commit-mismatch", OP_DISK_CONTROL, 0, COMMIT)
    s.send("flags-verifying-no-error", OP_DISK_INFO, 6, 0)
    s.send("poll-verifying", OP_DISK_INFO, 5, 0)
    s.send("poll-loading", OP_DISK_INFO, 5, 0)
    s.send("flags-error", OP_DISK_INFO, 6, 0)
    s.send("freeze-after-mismatch", OP_DISK_CONTROL, 0, FREEZE)
    disk_commit(s, [DISK_SECTORS, 0, 4, 16, 63, crc & 0xFFFF, crc >> 16, 0], "retry-")
    s.send("disk-commit", OP_DISK_CONTROL, 0, COMMIT)
    s.send("poll-verifying-again", OP_DISK_INFO, 5, 0)
    s.send("poll-ready", OP_DISK_INFO, 5, 0)
    s.send("flags-clear", OP_DISK_INFO, 6, 0)
    s.send("commit-word-while-ready", OP_DISK_COMMIT, 0, DISK_SECTORS)
    s.send("release", OP_EXECUTION, 0, 1)
    s.send("prepare-while-running", OP_DISK_CONTROL, 0, PREPARE)
    s.send("eject-while-running", OP_DISK_CONTROL, 0, EJECT)
    for sector in (0, 300, 4095, 301):
        s.sector_write(sector)
    s.send("flags-dirty", OP_DISK_INFO, 6, 0)
    s.send("epoch", OP_DISK_INFO, 7, 0)
    s.sector_write_begin(512)
    s.send("flags-writer-busy", OP_DISK_INFO, 6, 0)
    s.send("disk-saved-busy-writer", OP_DISK_CONTROL, 0, SAVED)
    s.send("epoch-before-freeze-drain", OP_DISK_INFO, 7, 0)
    s.send("disk-freeze-drain", OP_DISK_CONTROL, 0, FREEZE)
    s.send("flags-drained-frozen", OP_DISK_INFO, 6, 0)
    s.send("epoch-after-freeze-drain", OP_DISK_INFO, 7, 0)
    s.send("dirty-after-freeze-drain", OP_DISK_DIRTY, 0, 0)
    s.send("disk-freeze-again", OP_DISK_CONTROL, 0, FREEZE)
    s.send("flags-dirty-frozen", OP_DISK_INFO, 6, 0)
    s.send("dirty-index-rejected", OP_DISK_DIRTY, DIRTY_WORDS, 0)
    s.send("dirty-argument-rejected", OP_DISK_DIRTY, 0, 1)
    for word in range(3):
        s.send(f"dirty-word-{word}", OP_DISK_DIRTY, word, 0)
    s.send("dirty-word-127", OP_DISK_DIRTY, 127, 0)
    s.send("hold", OP_EXECUTION, 0, 0)
    s.send("eject-unsaved", OP_DISK_CONTROL, 0, EJECT)
    s.send("prepare-unsaved", OP_DISK_CONTROL, 0, PREPARE)
    s.send("disk-saved", OP_DISK_CONTROL, 0, SAVED)
    s.send("dirty-after-saved", OP_DISK_DIRTY, 0, 0)
    s.send("flags-frozen", OP_DISK_INFO, 6, 0)
    s.send("disk-resume", OP_DISK_CONTROL, 0, RESUME)
    s.send("disk-resume-again", OP_DISK_CONTROL, 0, RESUME)
    s.send("release-again", OP_EXECUTION, 0, 1)
    s.sector_write(1000)
    s.send("disk-freeze-for-stop", OP_DISK_CONTROL, 0, FREEZE)
    s.send("dirty-word-0-for-stop", OP_DISK_DIRTY, 0, 0)
    s.send("disk-saved-for-stop", OP_DISK_CONTROL, 0, SAVED)
    s.send("hold-for-stop", OP_EXECUTION, 0, 0)
    s.send("eject-after-saved", OP_DISK_CONTROL, 0, EJECT)
    s.send("state-empty", OP_DISK_INFO, 5, 0)
    return s


def document_11() -> dict:
    return {
        "description": "Synthetic fes.computer 1.1 wire fixtures; never deploy.",
        "abi": {"id": "fes.computer", "tag": 4, "major": 1, "minor": 1},
        "scenarios": [minor0_session().document(), persistence_session().document(),
                      disk_session().document(),
                      persistence_capability_session("persistence-without-layout",
                                                     PC386_CAPS & ~CAP_PC386_MACHINE).document(),
                      persistence_capability_session("layout-without-persistence",
                                                     PC386_CAPS & ~CAP_PERSISTENCE).document()],
    }


def document() -> dict:
    return {
        "description": "Synthetic fes.computer 1.0 wire fixtures; never deploy.",
        "abi": {"id": "fes.computer", "tag": 4, "major": 1, "minor": 0},
        "crc_vectors": [{"hex": "010203", "crc32": zlib.crc32(bytes([1, 2, 3]))},
                        {"hex": "313233343536373839", "crc32": zlib.crc32(b"123456789")}],
        "scenarios": [input_session().document(), media_session().document(), mouse_session().document()],
    }


def render(value: dict) -> str:
    return json.dumps(value, indent=1, sort_keys=True) + "\n"


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    for path, value in ((OUTPUT, document()), (OUTPUT_11, document_11())):
        text = render(value)
        if args.check:
            if not path.exists() or path.read_text() != text:
                print(f"{path.relative_to(ROOT)} is stale; run scripts/fes_computer_fixtures.py", file=sys.stderr)
                return 1
            continue
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text)
    return 0


if __name__ == "__main__":
    sys.exit(main())
