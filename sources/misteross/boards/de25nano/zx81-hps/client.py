#!/usr/bin/env python3
"""DE25 ZX81 diagnostic protocol client; the supplied file backend is offline only.

RegisterIO is the boundary for a future admitted, ordered 32-bit device backend.
FileRegisters intentionally supports only ordinary register-fixture files. It
does not implement physical MMIO, bus access deadlines or runtime admission.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import math
import mmap
import os
from pathlib import Path
import re
import stat
import struct
import time
from typing import Protocol
import zlib

HERE = Path(__file__).resolve().parent


class RegisterIO(Protocol):
    def read32(self, offset: int) -> int: ...
    def write32(self, offset: int, value: int) -> None: ...


class DiagnosticError(Exception):
    """A diagnostic command could not be completed as requested."""


class TransportError(DiagnosticError):
    """The client must be discarded; an uncertain command is never replayed."""


class CommandRejected(DiagnosticError):
    def __init__(self, opcode: int, code: int):
        self.opcode, self.code = opcode, code
        super().__init__(f"mailbox opcode {opcode} rejected with error {code}")


class Definitions:
    def __init__(self, directory: Path = HERE / "generated"):
        self.values = {}
        for name in ("fes_de25_computer.vh", "fes_computer.vh"):
            for line in (directory / name).read_text().splitlines():
                match = re.fullmatch(r"`define ([A-Z0-9_]+) 32'h([0-9a-fA-F]{8})", line)
                if match:
                    if match[1] in self.values:
                        raise ValueError("duplicate generated constant: " + match[1])
                    self.values[match[1]] = int(match[2], 16)
        required_bridge = ("IDENTITY VERSION ID_OFFSET VERSION_OFFSET STATUS_OFFSET GPO_OFFSET "
                           "GPI_OFFSET FAULT_OFFSET SPAN ZX81_TAPE_MAX_BYTES "
                           "STATUS_READY STATUS_BUSY STATUS_FAULT").split()
        required_abi = ("SIGNATURE REQUEST_MASK ACK_MASK ERROR_MASK RESPONSE_MASK IDENTITY_WORD_COUNT "
                        "IDENTITY_MAGIC0 IDENTITY_MAGIC1 TRANSPORT_MAJOR TRANSPORT_MINOR ABI_TAG ABI_MAJOR ABI_MINOR "
                        "IDENTITY_CAPABILITIES_INDEX IDENTITY_BUILD_IDSTART_INDEX OPCODE_IDENTITY OPCODE_EXECUTION "
                        "OPCODE_KEYBOARD_HID OPCODE_MEDIA_INFO OPCODE_MEDIA_BEGIN OPCODE_MEDIA_CHUNK OPCODE_MEDIA_DATA "
                        "OPCODE_MEDIA_COMMIT OPCODE_MEDIA_EJECT EXECUTION_HOLD_RESET EXECUTION_RELEASE "
                        "ERROR_INVALID_OPCODE ERROR_INVALID_INDEX ERROR_INVALID_ARGUMENT ERROR_INVALID_STATE "
                        "KEYBOARD_ROW_COUNT KEYBOARD_MODIFIER_FIRST_USAGE KEYBOARD_MODIFIER_ROW KEYBOARD_USAGE_ROWS "
                        "MEDIA_INFO_STRIDE MEDIA_INFO_STATE_FIELD MEDIA_HEADER_STRIDE MEDIA_CHUNK_MAX_BYTES "
                        "MEDIA_STATE_EMPTY MEDIA_STATE_LOADING MEDIA_STATE_READY ZX81_TAPE_UNIT ZX81_TAPE_MIN_BYTES "
                        "ZX81_TAPE_MAX_BYTES INTERFACE_VIDEO_FIXED_720P60_CAPABILITY_MASK "
                        "INTERFACE_KEYBOARD_HID_CAPABILITY_MASK INTERFACE_MEDIA_ZX81_TAPE_CAPABILITY_MASK").split()
        for name in ["FES_DE25_COMPUTER_" + x for x in required_bridge] + ["FES_COMPUTER_" + x for x in required_abi]:
            if name not in self.values:
                raise ValueError("missing literal generated constant: " + name)
        if self.bridge("ZX81_TAPE_MAX_BYTES") > self.abi("ZX81_TAPE_MAX_BYTES"):
            raise ValueError("DE25 tape limit exceeds the ZX81 computer ABI limit")

    def bridge(self, name: str) -> int:
        return self.values["FES_DE25_COMPUTER_" + name]

    def abi(self, name: str) -> int:
        return self.values["FES_COMPUTER_" + name]


class FileRegisters:
    """Little-endian register storage for an offline regular-file fixture.

    Python mmap/struct does not promise the native full-word device stores or
    ordering this bridge requires. Device and virtual-resource paths are refused.
    """
    def __init__(self, path: Path, span: int, offset: int = 0):
        if type(offset) is not int or type(span) is not int or offset < 0 or offset % 4 or span < 4 or span % 4:
            raise ValueError("register file offset/span must be aligned nonnegative byte ranges")
        resolved = Path(path).resolve(strict=True)
        if any(resolved == p or p in resolved.parents for p in map(Path, ("/dev", "/proc", "/sys"))):
            raise ValueError("only offline regular register files are supported")
        self.fd = os.open(path, os.O_RDWR | os.O_CLOEXEC | os.O_NOFOLLOW)
        try:
            metadata = os.fstat(self.fd)
            if not stat.S_ISREG(metadata.st_mode) or metadata.st_size < offset + span:
                raise ValueError("register fixture must be a regular file covering the complete span")
            base = offset // mmap.PAGESIZE * mmap.PAGESIZE
            self.delta, self.span = offset - base, span
            self.mapping = mmap.mmap(self.fd, self.delta + span, offset=base)
        except BaseException:
            os.close(self.fd)
            raise

    def _position(self, offset: int) -> int:
        if type(offset) is not int or offset < 0 or offset % 4 or offset > self.span - 4:
            raise ValueError("register access is outside an aligned 32-bit fixture word")
        return self.delta + offset

    def read32(self, offset: int) -> int:
        return struct.unpack_from("<I", self.mapping, self._position(offset))[0]

    def write32(self, offset: int, value: int) -> None:
        if type(value) is not int or not 0 <= value <= 0xffffffff:
            raise ValueError("register value must be an unsigned 32-bit integer")
        struct.pack_into("<I", self.mapping, self._position(offset), value)

    def close(self):
        self.mapping.close()
        os.close(self.fd)

    def __enter__(self):
        return self

    def __exit__(self, *_):
        self.close()


class ComputerClient:
    def __init__(self, registers: RegisterIO, definitions: Definitions | None = None,
                 *, timeout: float = 1.0, poll_interval: float = 0.0005,
                 clock=time.monotonic, sleep=time.sleep):
        if not math.isfinite(timeout) or timeout <= 0 or not math.isfinite(poll_interval) or poll_interval <= 0:
            raise ValueError("timeouts and polling intervals must be finite and positive")
        self.io, self.c = registers, definitions or Definitions()
        self.timeout, self.poll_interval = timeout, poll_interval
        self.clock, self.sleep = clock, sleep
        self.failed = False
        self.probed = False
        self.rows = None

    def _abort(self, message: str):
        self.failed = True
        self.probed = False
        self.rows = None
        raise TransportError(message)

    def _read(self, name: str) -> int:
        value = self.io.read32(self.c.bridge(name + "_OFFSET"))
        if type(value) is not int or not 0 <= value <= 0xffffffff:
            self._abort("backend returned a value outside a 32-bit register")
        return value

    def _status(self) -> int:
        status, fault = self._read("STATUS"), self._read("FAULT")
        known = self.c.bridge("STATUS_READY") | self.c.bridge("STATUS_BUSY") | self.c.bridge("STATUS_FAULT")
        if status & self.c.bridge("STATUS_FAULT") or fault:
            self._abort(f"transport fault 0x{fault:08x}; FPGA reconfiguration is required")
        if status & ~known or not status & self.c.bridge("STATUS_READY"):
            self._abort("diagnostic transport is not ready")
        return status

    def _gpi(self) -> int:
        value = self._read("GPI")
        if value & 0xff000000 != self.c.abi("SIGNATURE"):
            self._abort("mailbox response signature changed")
        if value & ~(0xff000000 | self.c.abi("ACK_MASK") | self.c.abi("ERROR_MASK") | self.c.abi("RESPONSE_MASK")):
            self._abort("reserved mailbox response bits are set")
        return value

    def _exchange(self, opcode: int, index: int = 0, argument: int = 0) -> int:
        if any(type(v) is not int or not 0 <= v <= maximum for v, maximum in
               ((opcode, 127), (index, 255), (argument, 65535))):
            raise ValueError("opcode, index and argument must fit their mailbox fields")
        if self.failed:
            raise TransportError("discard the failed client; uncertain commands are never replayed")
        try:
            deadline = self.clock() + self.timeout
            if self._status() & self.c.bridge("STATUS_BUSY"):
                self._abort("another request is already pending")
            previous = self._gpi()
            old_toggle = bool(previous & self.c.abi("ACK_MASK"))
            if bool(self._read("GPO") & self.c.abi("REQUEST_MASK")) != old_toggle:
                self._abort("idle request/acknowledge state is inconsistent")
            toggle = not old_toggle
            word = opcode << 24 | index << 16 | argument
            if toggle:
                word |= self.c.abi("REQUEST_MASK")
            # This Avalon bridge stores the complete word atomically. Unlike a
            # raw GP port, it rejects an old-toggle preload write.
            self.io.write32(self.c.bridge("GPO_OFFSET"), word)
            self._status()
            if self._read("GPO") != word:
                self._abort("request payload was not retained by the bridge")
            while True:
                self._status()
                response = self._gpi()
                if bool(response & self.c.abi("ACK_MASK")) == toggle:
                    if self._status() & self.c.bridge("STATUS_BUSY"):
                        self._abort("acknowledged request remains busy")
                    if self._read("GPO") != word:
                        self._abort("request payload changed before completion")
                    self._status()  # Fault after ACK must not be reported as success.
                    data = response & self.c.abi("RESPONSE_MASK")
                    if response & self.c.abi("ERROR_MASK"):
                        if data not in [self.c.abi("ERROR_" + name) for name in
                                        ("INVALID_OPCODE", "INVALID_INDEX", "INVALID_ARGUMENT", "INVALID_STATE")]:
                            self._abort("mailbox returned an unknown rejection code")
                        raise CommandRejected(opcode, data)
                    return data
                remaining = deadline - self.clock()
                if remaining <= 0:
                    self._abort("mailbox acknowledgement deadline expired; completion is uncertain")
                self.sleep(min(self.poll_interval, remaining))
        except (OSError, ValueError) as error:
            self._abort("register backend failed; completion is uncertain: " + str(error))

    def command(self, opcode: int, index: int = 0, argument: int = 0) -> int:
        if not self.probed:
            raise TransportError("probe and validate the diagnostic identity before issuing commands")
        return self._exchange(opcode, index, argument)

    def probe(self, expected_build: str | None = None) -> dict:
        if expected_build is not None and not re.fullmatch(r"[0-9a-fA-F]{32}", expected_build):
            raise ValueError("expected build identity must contain exactly 32 hexadecimal digits")
        if self.failed:
            raise TransportError("discard the failed client before probing again")
        self.probed = False
        self.rows = None
        try:
            if self._read("ID") != self.c.bridge("IDENTITY") or self._read("VERSION") != self.c.bridge("VERSION"):
                self._abort("register resource is not the expected DE25 computer endpoint")
            words = [self._exchange(self.c.abi("OPCODE_IDENTITY"), index)
                     for index in range(self.c.abi("IDENTITY_WORD_COUNT"))]
            expected = [self.c.abi(name) for name in ("IDENTITY_MAGIC0", "IDENTITY_MAGIC1",
                "TRANSPORT_MAJOR", "TRANSPORT_MINOR", "ABI_TAG", "ABI_MAJOR", "ABI_MINOR")]
            capabilities = (self.c.abi("INTERFACE_VIDEO_FIXED_720P60_CAPABILITY_MASK") |
                            self.c.abi("INTERFACE_KEYBOARD_HID_CAPABILITY_MASK") |
                            self.c.abi("INTERFACE_MEDIA_ZX81_TAPE_CAPABILITY_MASK"))
            cap_index = self.c.abi("IDENTITY_CAPABILITIES_INDEX")
            if words[:cap_index] != expected or words[cap_index] != capabilities:
                self._abort("mailbox identity/capabilities do not match this diagnostic")
            build = b"".join(word.to_bytes(2, "little") for word in words[self.c.abi("IDENTITY_BUILD_IDSTART_INDEX"):]).hex()
            if expected_build is not None and build != expected_build.lower():
                self._abort("mailbox build identity differs from the selected artifact")
            media = self._media_info()
            if (media["minimum"], media["maximum"], media["chunk_maximum"]) != (
                    self.c.abi("ZX81_TAPE_MIN_BYTES"), self.c.bridge("ZX81_TAPE_MAX_BYTES"),
                    self.c.abi("MEDIA_CHUNK_MAX_BYTES")):
                self._abort("live tape limits differ from the ZX81 diagnostic contract")
            self.probed = True
            self.rows = None
            return {"build_id": build, "capabilities": capabilities, "media": media}
        except (OSError, ValueError) as error:
            self._abort("identity probe failed: " + str(error))
        except CommandRejected as error:
            self._abort("identity probe command was rejected: " + str(error))

    def _media_info(self) -> dict:
        unit = self.c.abi("ZX81_TAPE_UNIT")
        words = [self._exchange(self.c.abi("OPCODE_MEDIA_INFO"),
                               unit * self.c.abi("MEDIA_INFO_STRIDE") + field)
                 for field in range(self.c.abi("MEDIA_INFO_STATE_FIELD") + 1)]
        if words[5] not in [self.c.abi("MEDIA_STATE_" + name) for name in ("EMPTY", "LOADING", "READY")]:
            self._abort("ZX81 media unit reports an invalid or absent state")
        return {"minimum": words[0] | words[1] << 16, "maximum": words[2] | words[3] << 16,
                "chunk_maximum": words[4], "state": words[5]}

    def hold(self):
        self._mutate(self.c.abi("OPCODE_EXECUTION"), argument=self.c.abi("EXECUTION_HOLD_RESET"))
        self.rows = [0] * self.c.abi("KEYBOARD_ROW_COUNT")

    def run(self):
        self._mutate(self.c.abi("OPCODE_EXECUTION"), argument=self.c.abi("EXECUTION_RELEASE"))

    def _mutate(self, opcode: int, index: int = 0, argument: int = 0):
        if self.command(opcode, index, argument) != 0:
            self._abort("mutating command returned nonzero success data")

    def set_keys(self, usages):
        """Replace the complete owned HID state; never infer an existing row."""
        rows = [0] * self.c.abi("KEYBOARD_ROW_COUNT")
        first_modifier = self.c.abi("KEYBOARD_MODIFIER_FIRST_USAGE")
        for usage in usages:
            if type(usage) is not int:
                raise ValueError("HID usages must be integers")
            if first_modifier <= usage < first_modifier + 8:
                rows[self.c.abi("KEYBOARD_MODIFIER_ROW")] |= 1 << (usage - first_modifier)
            elif 4 <= usage < 16 * self.c.abi("KEYBOARD_USAGE_ROWS"):
                rows[usage // 16] |= 1 << (usage % 16)
            else:
                raise ValueError("usage is outside the non-error HID keyboard/modifier rows")
        try:
            for index, row in enumerate(rows):
                self._mutate(self.c.abi("OPCODE_KEYBOARD_HID"), index, row)
        except DiagnosticError:
            self.rows = None
            raise
        self.rows = rows
        self.sleep(0.002)  # Permit the nominal 1 ms hardware settling interval.

    def neutral(self):
        self.set_keys(())

    def upload(self, payload: bytes, *, chunk_bytes: int | None = None) -> dict:
        size = len(payload)
        tape_maximum = self.c.bridge("ZX81_TAPE_MAX_BYTES")
        if not self.c.abi("ZX81_TAPE_MIN_BYTES") <= size <= tape_maximum:
            raise ValueError(f"ZX81 tape payload must contain 1 through {tape_maximum} bytes")
        maximum = self.c.abi("MEDIA_CHUNK_MAX_BYTES")
        chunk_bytes = maximum if chunk_bytes is None else chunk_bytes
        if type(chunk_bytes) is not int or not 1 <= chunk_bytes <= maximum:
            raise ValueError("chunk size must contain 1 through 512 bytes")
        checksum = zlib.crc32(payload) & 0xffffffff
        base = self.c.abi("ZX81_TAPE_UNIT") * self.c.abi("MEDIA_HEADER_STRIDE")
        for word, value in enumerate((size & 0xffff, size >> 16, checksum & 0xffff, checksum >> 16)):
            self._mutate(self.c.abi("OPCODE_MEDIA_BEGIN"), base + word, value)
        for offset in range(0, size, chunk_bytes):
            chunk = payload[offset:offset + chunk_bytes]
            for word, value in enumerate((offset & 0xffff, offset >> 16, len(chunk))):
                self._mutate(self.c.abi("OPCODE_MEDIA_CHUNK"), base + word, value)
            for ordinal, start in enumerate(range(0, len(chunk), 2)):
                value = int.from_bytes(chunk[start:start + 2], "little")
                self._mutate(self.c.abi("OPCODE_MEDIA_DATA"), ordinal, value)
        self._mutate(self.c.abi("OPCODE_MEDIA_COMMIT"), self.c.abi("ZX81_TAPE_UNIT"))
        if self._media_info()["state"] != self.c.abi("MEDIA_STATE_READY"):
            self._abort("acknowledged tape commit did not make the media unit ready")
        return {"committed_bytes": size, "crc32": f"{checksum:08x}",
                "sha256": hashlib.sha256(payload).hexdigest()}

    def eject(self):
        self._mutate(self.c.abi("OPCODE_MEDIA_EJECT"), self.c.abi("ZX81_TAPE_UNIT"))


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--register-file", type=Path, required=True, help="explicit offline regular register fixture")
    parser.add_argument("--offset", type=lambda x: int(x, 0), default=0, help="byte offset inside that file")
    parser.add_argument("--timeout", type=float, default=1.0, help="software ACK polling deadline, not a bus deadline")
    parser.add_argument("--expect-build", help="selected artifact's 32-digit build identity")
    sub = parser.add_subparsers(dest="operation", required=True)
    for name in ("info", "hold", "run", "neutral", "eject"):
        sub.add_parser(name)
    key = sub.add_parser("key", help="replace complete HID state; release neutralizes every key")
    key.add_argument("usages", type=lambda x: int(x, 0), nargs="*")
    action = key.add_mutually_exclusive_group(required=True)
    action.add_argument("--press", action="store_true")
    action.add_argument("--release", action="store_true")
    upload = sub.add_parser("upload", help="commit .p bytes only; does not run or type LOAD")
    upload.add_argument("payload", type=Path)
    args = parser.parse_args(argv)
    try:
        constants = Definitions()
        payload = None
        if args.operation == "upload":
            tape_maximum = constants.bridge("ZX81_TAPE_MAX_BYTES")
            if not args.payload.is_file() or not 1 <= args.payload.stat().st_size <= tape_maximum:
                raise ValueError(f"tape input must be a regular file containing 1 through {tape_maximum} bytes")
            with args.payload.open("rb") as source:
                payload = source.read(tape_maximum + 1)
            if not 1 <= len(payload) <= tape_maximum:
                raise ValueError("tape input changed outside the permitted size")
        if args.operation == "key" and args.press and not args.usages:
            raise ValueError("key --press requires at least one HID usage")
        with FileRegisters(args.register_file, constants.bridge("SPAN"), args.offset) as registers:
            client = ComputerClient(registers, constants, timeout=args.timeout)
            identity = client.probe(args.expect_build)
            result = identity if args.operation == "info" else {
                "operation": args.operation, "acknowledged": True, "build_id": identity["build_id"]}
            if args.operation == "key":
                client.set_keys(args.usages if args.press else ())
            elif args.operation == "upload":
                result.update(client.upload(payload))
            elif args.operation != "info":
                getattr(client, args.operation)()
            print(json.dumps(result, indent=2, sort_keys=True))
    except (DiagnosticError, OSError, ValueError, KeyError) as error:
        parser.exit(1, "de25-zx81-client: " + str(error) + "\n")


if __name__ == "__main__":
    main()
