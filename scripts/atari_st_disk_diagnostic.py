#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-or-later
"""Original GEMDOS disk diagnostic and deterministic 720 KiB FAT12 fixture.

Generate: python3 scripts/atari_st_disk_diagnostic.py --output out/st-disk/test.st --auto
Inspect a *captured* image: python3 scripts/atari_st_disk_diagnostic.py --inspect saved.st

DISKTEST.PRG creates A:\\FESDATA.TMP, writes an odd 1537-byte patterned payload,
closes/reopens/compares it including EOF, renames it, reopens/compares again,
then deletes it. Only after all checks does it write and close A:\\PASS.TXT.
Failure removes PASS.TXT where possible and attempts FAIL.TXT; the screen names
the failed stage. Use a freshly generated image for each acceptance run: a
marker from an earlier execution cannot establish a new execution's result.
--auto places the PRG under AUTO; otherwise double-click DISKTEST.PRG in GEM.
There is no executable boot sector, AES dependency, external program or ROM.

This script is the original, complete program source: the bounded emitter uses
only 68000 instructions and PC-relative data addresses (no relocation table).
GEMDOS trap #1 numbers/word-long argument ordering and the PRG header were
verified against EmuTOS 1.4 primary sources:
https://github.com/emutos/emutos/blob/VERSION_1_4/include/bdosbind.h
https://github.com/emutos/emutos/blob/VERSION_1_4/bdos/kpgmld.c
https://github.com/emutos/emutos/blob/VERSION_1_4/bdos/pghdr.h
https://github.com/emutos/emutos/blob/VERSION_1_4/bdos/proc.c
Host tests exercise the actual emitted instructions and trap arguments. They
are not a claim that this PRG has run under EmuTOS or on physical hardware.
"""

from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path
import struct

SECTOR = 512
SECTORS = 1440
DISK_BYTES = SECTOR * SECTORS
CLUSTER_BYTES = 1024
ROOT_SECTOR = 7
DATA_SECTOR = 14
PAYLOAD = bytes((i * 29 + (i >> 8) * 71 + 0x53) & 255 for i in range(1537))
PASS_TEXT = b"FES ST GEMDOS disk diagnostic v1\r\nPASS create/write/close/reopen/read/rename/delete 1537 bytes\r\n"
FAIL_TEXT = b"FAIL FES ST GEMDOS disk diagnostic v1; see screen for stage\r\n"
README = b"FES original GPL-2.0-or-later GEMDOS disk diagnostic v1\r\nRun DISKTEST.PRG (in AUTO if selected). Drive A must be writable.\r\nPASS.TXT means create/write/close/reopen/read/rename/delete passed.\r\nFAIL.TXT or no PASS.TXT means failure or incomplete execution.\r\nUse a freshly generated disk for each test; retain the captured image.\r\nThis tests guest file operations, not durable host storage by itself.\r\n"


class _M68k:
    """Small original two-pass 68000 emitter; every fixup is signed PC-relative."""

    def __init__(self):
        self.code = bytearray()
        self.labels: dict[str, int] = {}
        self.fixups: list[tuple[int, str]] = []

    def label(self, name):
        if name in self.labels:
            raise ValueError("duplicate label")
        self.labels[name] = len(self.code)

    def word(self, value):
        self.code.extend(struct.pack(">H", value & 0xffff))

    def long(self, value):
        self.code.extend(struct.pack(">I", value & 0xffffffff))

    def relative(self, opcode, label):
        self.word(opcode)
        self.fixups.append((len(self.code), label))
        self.word(0)

    def lea(self, label, register=0):
        self.relative(0x41fa + (register << 9), label)  # LEA d16(PC),An

    def branch(self, condition, label):
        self.relative(0x6000 + (condition << 8), label)

    def push_word(self, value):
        self.word(0x3f3c)  # MOVE.W #imm,-(SP)
        self.word(value)

    def push_long(self, value):
        self.word(0x2f3c)
        self.long(value)

    def push_pointer(self, label):
        self.lea(label)
        self.word(0x2f08)  # MOVE.L A0,-(SP)

    def call(self, function, argument_bytes):
        self.push_word(function)
        self.word(0x4e41)  # TRAP #1
        self.word(0x4fef)  # LEA argument_bytes+2(SP),SP; preserves D0/CCR
        self.word(argument_bytes + 2)

    def expect(self, value):
        self.word(0x0c80)  # CMPI.L #imm,D0
        self.long(value)
        self.branch(6, "failure")  # BNE

    def stage(self, label):
        self.lea(label, 5)  # A5 survives GEMDOS; identify failure on screen

    def finish(self):
        for offset, label in self.fixups:
            displacement = self.labels[label] - offset
            if not -32768 <= displacement <= 32767:
                raise ValueError("PC-relative program exceeded 68000 range")
            struct.pack_into(">h", self.code, offset, displacement)
        return bytes(self.code)


def build_prg() -> bytes:
    a = _M68k()
    a.word(0x7cff)  # MOVEQ #-1,D6: no open handle
    # GEMDOS places the process basepage at 4(SP). Move off the original stack
    # before shrinking the TPA to our text (including a private 2 KiB stack).
    a.word(0x286f)  # MOVEA.L 4(SP),A4
    a.word(4)
    a.lea("stack_end", 7)
    a.stage("stage_memory")
    a.word(0x2f3c)
    retained_size_offset = len(a.code)
    a.long(0)  # linked below: entire retained TPA, including 256-byte basepage
    a.word(0x2f0c)  # MOVE.L A4,-(SP)
    a.push_word(0)
    a.call(0x4a, 10)  # Mshrink(0,basepage,size)
    a.expect(0)
    for label in ("pass_path", "fail_path", "tmp_path", "new_path"):
        a.stage("stage_cleanup")
        a.push_pointer(label)
        a.call(0x41, 4)  # Fdelete; existing files or EFILNF are acceptable
        a.word(0x4a80)  # TST.L D0
        a.branch(7, "deleted_" + label)  # BEQ
        a.expect(-33)
        a.label("deleted_" + label)

    def open_file(path, create, stage):
        a.stage(stage)
        a.push_word(0)  # attr=0 or mode=read-only
        a.push_pointer(path)
        a.call(0x3c if create else 0x3d, 6)
        a.word(0x4a80)
        a.branch(11, "failure")  # BMI: GEMDOS error
        a.word(0x2c00)  # MOVE.L D0,D6

    def io(function, buffer, count, stage, checked=True):
        a.stage(stage)
        a.push_pointer(buffer)
        a.push_long(count)
        a.word(0x3f06)  # MOVE.W D6,-(SP)
        a.call(function, 10)
        if checked:
            a.expect(count)

    def close_file(stage, checked=True):
        a.stage(stage)
        a.word(0x3f06)
        a.call(0x3e, 2)
        if checked:
            a.expect(0)
        a.word(0x7cff)

    def read_compare(path, suffix):
        open_file(path, False, "stage_open")
        io(0x3f, "read_buffer", len(PAYLOAD), "stage_read")
        a.stage("stage_compare")
        a.lea("pattern")
        a.lea("read_buffer", 1)
        a.word(0x3e3c)  # MOVE.W #len-1,D7
        a.word(len(PAYLOAD) - 1)
        a.label("compare_" + suffix)
        a.word(0xb308)  # CMPM.B (A0)+,(A1)+
        a.branch(6, "failure")
        a.relative(0x51cf, "compare_" + suffix)  # DBRA D7
        io(0x3f, "read_buffer", 1, "stage_eof", checked=False)
        a.expect(0)  # exact file length, not a matching prefix
        close_file("stage_close")

    open_file("tmp_path", True, "stage_create")
    io(0x40, "pattern", len(PAYLOAD), "stage_write")
    close_file("stage_close")
    read_compare("tmp_path", "before_rename")
    a.stage("stage_rename")
    a.push_pointer("new_path")
    a.push_pointer("tmp_path")
    a.push_word(0)
    a.call(0x56, 10)  # Frename(0,old,new)
    a.expect(0)
    read_compare("new_path", "after_rename")
    a.stage("stage_delete")
    a.push_pointer("new_path")
    a.call(0x41, 4)
    a.expect(0)
    open_file("pass_path", True, "stage_marker")
    io(0x40, "pass_text", len(PASS_TEXT), "stage_marker")
    close_file("stage_marker")
    a.push_pointer("pass_text")
    a.call(0x09, 4)  # Cconws
    a.push_word(0)
    a.push_word(0x4c)  # Pterm(0)
    a.word(0x4e41)

    a.label("failure")
    a.word(0x4a86)  # TST.L D6
    a.branch(11, "failure_no_handle")
    a.word(0x3f06)
    a.call(0x3e, 2)  # best effort close before deleting a partial PASS
    a.label("failure_no_handle")
    a.push_pointer("pass_path")
    a.call(0x41, 4)
    a.push_word(0)
    a.push_pointer("fail_path")
    a.call(0x3c, 6)
    a.word(0x4a80)
    a.branch(11, "failure_console")
    a.word(0x2c00)
    a.push_pointer("fail_text")
    a.push_long(len(FAIL_TEXT))
    a.word(0x3f06)
    a.call(0x40, 10)
    a.word(0x3f06)
    a.call(0x3e, 2)
    a.label("failure_console")
    a.word(0x2f0d)  # MOVE.L A5,-(SP): original failing stage
    a.call(0x09, 4)
    a.push_word(1)
    a.push_word(0x4c)
    a.word(0x4e41)

    for name, data in (("tmp_path", b"A:\\FESDATA.TMP"), ("new_path", b"A:\\FESDATA.NEW"),
                       ("pass_path", b"A:\\PASS.TXT"), ("fail_path", b"A:\\FAIL.TXT"),
                       ("pass_text", PASS_TEXT), ("fail_text", FAIL_TEXT)):
        a.label(name)
        a.code.extend(data + b"\0")
    for stage in ("memory", "cleanup", "create", "write", "close", "open", "read", "compare", "eof", "rename", "delete", "marker"):
        a.label("stage_" + stage)
        a.code.extend(("FAIL GEMDOS " + stage + "\r\n\0").encode())
    a.label("pattern")
    a.code.extend(PAYLOAD)
    a.label("read_buffer")
    a.code.extend(bytes(len(PAYLOAD)))
    if len(a.code) & 1:
        a.code.append(0)
    a.code.extend(bytes(2048))
    a.label("stack_end")
    struct.pack_into(">I", a.code, retained_size_offset, 256 + len(a.code))
    text = a.finish()
    # PGMHDR01: text, data, bss, symbols, reserved, flags, absolute/no-fixups.
    return struct.pack(">H6IH", 0x601a, len(text), 0, 0, 0, 0, 0, 1) + text


def _fat_set(fat: bytearray, cluster: int, value: int):
    offset = cluster * 3 // 2
    pair = int.from_bytes(fat[offset:offset + 2], "little")
    pair = (pair & 0x000f) | (value << 4) if cluster & 1 else (pair & 0xf000) | value
    fat[offset:offset + 2] = pair.to_bytes(2, "little")


def _entry(name: bytes, cluster: int, size: int, attribute=0x20) -> bytes:
    if len(name) != 11:
        raise ValueError("FAT name must be an exact 8.3 field")
    result = bytearray(32)
    result[:11] = name
    result[11] = attribute
    struct.pack_into("<HHHI", result, 22, 0, 0x21, cluster, size)  # 1980-01-01
    return bytes(result)


def build_disk(*, auto=False) -> bytes:
    image = bytearray(DISK_BYTES)
    image[:11] = b"\x60\x1cFESST \x12\x34\x56"  # ST branch/OEM/24-bit serial
    struct.pack_into("<HBHBHHBHHHH", image, 11, 512, 2, 1, 2, 112, 1440, 0xf9, 3, 9, 2, 0)
    # Deliberately non-executable ST boot checksum (sum of BE words != 0x1234).
    fat = bytearray(3 * SECTOR)
    fat[:3] = b"\xf9\xff\xff"
    next_cluster = 2

    def allocate(data):
        nonlocal next_cluster
        count = (len(data) + CLUSTER_BYTES - 1) // CLUSTER_BYTES
        first = next_cluster
        for i in range(count):
            cluster = next_cluster
            next_cluster += 1
            _fat_set(fat, cluster, 0xfff if i == count - 1 else next_cluster)
            offset = DATA_SECTOR * SECTOR + (cluster - 2) * CLUSTER_BYTES
            block = data[i * CLUSTER_BYTES:(i + 1) * CLUSTER_BYTES]
            image[offset:offset + len(block)] = block
        return first

    root = bytearray(7 * SECTOR)
    program = build_prg()
    if auto:
        directory_cluster = allocate(bytes(CLUSTER_BYTES))
        program_cluster = allocate(program)
        directory = (_entry(b".          ", directory_cluster, 0, 0x10) +
                     _entry(b"..         ", 0, 0, 0x10) +
                     _entry(b"DISKTESTPRG", program_cluster, len(program)))
        offset = DATA_SECTOR * SECTOR + (directory_cluster - 2) * CLUSTER_BYTES
        image[offset:offset + len(directory)] = directory
        root[:32] = _entry(b"AUTO       ", directory_cluster, 0, 0x10)
    else:
        root[:32] = _entry(b"DISKTESTPRG", allocate(program), len(program))
    root[32:64] = _entry(b"README  TXT", allocate(README), len(README))
    image[ROOT_SECTOR * SECTOR:DATA_SECTOR * SECTOR] = root
    image[SECTOR:4 * SECTOR] = fat
    image[4 * SECTOR:7 * SECTOR] = fat
    return bytes(image)


class Fat12Disk:
    """Bounded capture reader; rejects mismatched FATs and malformed chains."""

    def __init__(self, image: bytes):
        self.image = image
        if len(image) != DISK_BYTES or struct.unpack_from("<HBHBHHBHHHH", image, 11) != (512, 2, 1, 2, 112, 1440, 0xf9, 3, 9, 2, 0):
            raise ValueError("expected an exact 720 KiB ST FAT12 geometry")
        self.fat = image[SECTOR:4 * SECTOR]
        if self.fat != image[4 * SECTOR:7 * SECTOR] or self.fat[:3] != b"\xf9\xff\xff":
            raise ValueError("FAT copies or reserved entries disagree")
        self.root = image[ROOT_SECTOR * SECTOR:DATA_SECTOR * SECTOR]

    def chain(self, cluster: int) -> bytes:
        data, visited = bytearray(), set()
        while True:
            if cluster < 2 or cluster >= 715 or cluster in visited:
                raise ValueError("invalid or cyclic FAT12 chain")
            visited.add(cluster)
            offset = DATA_SECTOR * SECTOR + (cluster - 2) * CLUSTER_BYTES
            data.extend(self.image[offset:offset + CLUSTER_BYTES])
            pair = int.from_bytes(self.fat[cluster * 3 // 2:cluster * 3 // 2 + 2], "little")
            cluster = (pair >> 4) & 0xfff if cluster & 1 else pair & 0xfff
            if cluster >= 0xff8:
                return bytes(data)

    def read(self, name: bytes, directory: bytes | None = None) -> bytes | None:
        directory = self.root if directory is None else directory
        for offset in range(0, len(directory), 32):
            entry = directory[offset:offset + 32]
            if entry[0] == 0:
                break
            if entry[0] == 0xe5 or entry[11] & 0x08:
                continue
            if entry[:11] == name:
                cluster, size = struct.unpack_from("<HI", entry, 26)
                if entry[11] & 0x10:
                    return self.chain(cluster)
                if size == 0:
                    return b""
                data = self.chain(cluster)
                if not len(data) - CLUSTER_BYTES < size <= len(data):
                    raise ValueError("file size disagrees with FAT12 chain")
                return data[:size]
        return None


def inspect_capture(image: bytes) -> dict:
    disk = Fat12Disk(image)
    program = disk.read(b"DISKTESTPRG")
    if program is None:
        directory = disk.read(b"AUTO       ")
        program = disk.read(b"DISKTESTPRG", directory) if directory is not None else None
    if program != build_prg():
        raise ValueError("capture does not contain this exact diagnostic PRG")
    passed = (disk.read(b"PASS    TXT") == PASS_TEXT and disk.read(b"FAIL    TXT") is None and
              disk.read(b"FESDATA TMP") is None and disk.read(b"FESDATA NEW") is None)
    return {"schema": 1, "marker_pass": passed, "image_sha256": hashlib.sha256(image).hexdigest(),
            "prg_sha256": hashlib.sha256(program).hexdigest(), "image_bytes": len(image),
            "hardware_acceptance": False}


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    choice = parser.add_mutually_exclusive_group(required=True)
    choice.add_argument("--output", type=Path, help="generate a fresh diagnostic .st image")
    choice.add_argument("--inspect", type=Path, help="inspect a captured post-execution .st image")
    parser.add_argument("--auto", action="store_true", help="place DISKTEST.PRG in AUTO")
    parser.add_argument("--prg", type=Path, help="also save standalone DISKTEST.PRG")
    args = parser.parse_args(argv)
    if args.inspect:
        if args.auto or args.prg:
            parser.error("--auto/--prg apply only to generation")
        record = inspect_capture(args.inspect.read_bytes())
    else:
        image = build_disk(auto=args.auto)
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_bytes(image)
        if args.prg:
            args.prg.parent.mkdir(parents=True, exist_ok=True)
            args.prg.write_bytes(build_prg())
        record = inspect_capture(image) | {"autorun": args.auto, "generated": True}
    print(json.dumps(record, sort_keys=True))
    return 0 if args.output or record["marker_pass"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
