#!/usr/bin/env python3
"""Replay the locked Terasic SDM completion handler off-board; never load firmware."""

from pathlib import Path
import argparse
import datetime
import hashlib
import json
import struct
import sys
import time
if sys.flags.optimize:
    raise SystemExit("DE25 proof verifier refuses optimized Python")

def require(condition, message):
    if not condition:
        raise ValueError(message)

from unicorn import Uc, UC_ARCH_ARM64, UC_MODE_ARM, UC_HOOK_CODE
from unicorn.arm64_const import (
    UC_ARM64_REG_X0,
    UC_ARM64_REG_X2,
    UC_ARM64_REG_X6,
    UC_ARM64_REG_X30,
    UC_ARM64_REG_PC,
    UC_ARM64_REG_SP,
)

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--bl31", required=True, type=Path)
parser.add_argument("--output", required=True, type=Path)
args = parser.parse_args()
b = args.bl31.read_bytes()
sha = lambda x: hashlib.sha256(x).hexdigest()
if sha(b) != "0d29d7f1ff43afa267f63c1599657eea6d7fcdc8fd53addf8beb2d4f7089fb3c":
    raise ValueError("BL31 differs from locked stock DE25 firmware")
J = args.output
J.mkdir(parents=True, exist_ok=False)
BASE = 0x80000000
STACK = 0x90010000
CTX = 0x90020000
END = 0x90030000
ENTRY = BASE + 0x8A7C
READ = BASE + 0x6EB0
WRITE_ALL = BASE + 0x8630
CLEAR = BASE + 0x7024
for off, word in {
    0x8A7C: 0xD10483FF,
    0x8F2C: 0x97FFF7E1,
    0x9018: 0xB940CBE0,
    0x901C: 0x34FFF900,
}.items():
    require(struct.unpack_from("<I", b, off)[0] == word,
            f"stock BL31 instruction differs at {off:#x}")
require(BASE + 0x8B64 + 4 * struct.unpack_from("<h", b, 0xC62C)[0] == BASE + 0x8F00,
        "stock BL31 branch target differs")


def run(name, replies, expect, *, pending=True, mutation=False):
    u = Uc(UC_ARCH_ARM64, UC_MODE_ARM)
    u.mem_map(BASE, 0x20000)
    u.mem_write(BASE, b)
    # Negative control: remove only the binary branch that masks errors with
    # zero response length. This mutated buffer is never exported as firmware.
    if mutation:
        u.mem_write(BASE + 0x901C, struct.pack("<I", 0xD503201F))
    u.mem_map(0x90000000, 0x40000)
    u.reg_write(UC_ARM64_REG_SP, STACK)
    u.reg_write(UC_ARM64_REG_X0, 0xC2000003)
    u.reg_write(UC_ARM64_REG_X6, CTX)
    u.reg_write(UC_ARM64_REG_X30, END)

    def put(addr, value):
        u.mem_write(addr, struct.pack("<I", value & 0xFFFFFFFF))

    for i in range(4):
        # Actual stock struct layout: addr,size,written,requested,subblocks,block.
        u.mem_write(
            BASE + 0x19478 + i * 24,
            struct.pack(
                "<IiiIii", 0x1FF80000 - i * 0x80000, 524288, 524288, int(pending), 1, i
            ),
        )
    put(BASE + 0x19470, 0)
    put(BASE + 0x194DC, 0)
    put(BASE + 0x194E4, 1)
    trace = []
    n = 0
    instructions = 0
    start = time.monotonic()

    def hook(uc, address, size, unused):
        nonlocal n, instructions
        instructions += 1
        if time.monotonic() - start > 3:
            raise RuntimeError("emulation wall deadline exceeded")
        if address == END:
            uc.emu_stop()
            return
        if address == READ:
            status, length = replies[min(n, len(replies) - 1)]
            n += 1
            require(uc.reg_read(UC_ARM64_REG_X0) == BASE + 0x194E0,
                    "stock SDM handler passed an unexpected response buffer")
            length_ptr = uc.reg_read(UC_ARM64_REG_X2)
            require(STACK - 0x120 <= length_ptr < STACK,
                    "stock SDM handler passed an out-of-frame response length pointer")
            put(length_ptr, length)
            uc.reg_write(UC_ARM64_REG_X0, status & 0xFFFFFFFF)
            trace.append(
                dict(
                    call="mailbox_read_response",
                    injected_status=status,
                    injected_length=length,
                )
            )
        elif address in [WRITE_ALL, CLEAR]:
            trace.append(
                dict(
                    call="write_all" if address == WRITE_ALL else "clear_response",
                    injected="no external I/O",
                )
            )
            uc.reg_write(UC_ARM64_REG_X0, 0)
        else:
            return
        uc.reg_write(UC_ARM64_REG_PC, uc.reg_read(UC_ARM64_REG_X30))

    u.hook_add(UC_HOOK_CODE, hook)
    u.emu_start(ENTRY, END + 4, timeout=4000000, count=100000)
    require(u.reg_read(UC_ARM64_REG_PC) == END, f"{name}: emulation did not reach its return address")
    regs = struct.unpack("<QQQQ", u.mem_read(CTX, 32))
    require(regs == expect, f"{name}: returned SMC registers {regs}, expected {expect}")
    return dict(
        name=name,
        returned_smc_registers=[hex(x) for x in regs],
        instructions=instructions,
        trace=trace,
        binary_mutated=mutation,
    )


cases = [
    run("pending buffers and no response produce BUSY", [(-2, 0)], (1, 0, 0, 0)),
    run(
        "hard mailbox error with empty payload is masked as BUSY",
        [(-33, 0)],
        (1, 0, 0, 0),
    ),
    run(
        "same hard error with nonempty payload produces ERROR", [(-33, 1)], (4, 0, 0, 0)
    ),
    run(
        "one returned buffer hides a later hard error in the same call",
        [(0, 0), (-33, 0)],
        (0, 0x1FF80000, 0, 0),
    ),
    run(
        "two returned buffers preserve both IOVAs",
        [(0, 0), (0, 0), (-2, 0)],
        (0, 0x1FF80000, 0x1FF00000, 0),
    ),
    run(
        "three returned buffers preserve all IOVAs",
        [(0, 0), (0, 0), (0, 0)],
        (0, 0x1FF80000, 0x1FF00000, 0x1FE80000),
    ),
    run(
        "no outstanding buffers and no response report completion",
        [(-2, 0)],
        (0, 0, 0, 0),
        pending=False,
    ),
    run(
        "mutation removing zero-length guard exposes hard error",
        [(-33, 0)],
        (4, 0, 0, 0),
        mutation=True,
    ),
]
record = dict(
    passed=True,
    utc=datetime.datetime.now(datetime.timezone.utc).isoformat(),
    classification="Actual stock BL31 AArch64 instructions in Unicorn; injected mailbox replies, four buffer records and write-all/clear-response external calls. No EL3 entry, SDM, MMIO, DMA or board execution.",
    stock_bl31_sha256=sha(b),
    stock_bl31_bytes=len(b),
    stock_bl31_version="v2.12.0(release):de25_nano_revA_v1.0",
    entry=hex(ENTRY),
    smc=hex(0xC2000003),
    cases=cases,
    hardware_access=False,
    firmware_deployed=False,
    root_cause_proven=False,
    interpretation="Repeated native BUSY alone cannot distinguish absent completions from some hard mailbox errors. The injected error is not evidence that the board returned that error. This does not explain or fix the first persona stall.",
    unicorn_version=__import__("unicorn").__version__,
    script_sha256=sha(Path(__file__).read_bytes()),
)
(J / "verified.json").write_text(json.dumps(record, indent=2) + "\n")
for c in cases:
    print(c["name"] + ": " + ",".join(c["returned_smc_registers"]))
