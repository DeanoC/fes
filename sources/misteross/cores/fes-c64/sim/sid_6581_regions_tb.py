#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Replay the existing 6502 SID store trace on both production 6581 boards."""
import csv
import hashlib
import subprocess

from checkpoint_tb import MODULE, SYS_CYCLES, check_sid_stores, write_sid_firmware


# Accepted register stores and signed 48 kHz mono PCM from the default SVF on main at 2de4feabc.
CASES = (
    ("pal", "fes-c64-boot", 985248,
     "cbf1eb0dba53d35d07ab614ff14a8f22f33cde0711f0d9ce88a88c71d47325ad",
     "500ae9c8d878d06cd752df7fa2867c2c91a0429d4bc4bbd654d6e9fa47c9e415"),
    ("ntsc", "fes-c64-boot-ntsc", 1022730,
     "406a3e6487749a945cf61eebb008afb7adb97180bd35449727ce5d0773945b08",
     "2d64b54e0791de1cd526cbe678e0680270d8bec7789b2f780cd2a3ce72e3ed68"),
)


def main():
    build = MODULE / "build/sim"
    fixture = build / "fes-c64-sid-6581-regions"
    fixture.mkdir(parents=True, exist_ok=True)
    write_sid_firmware(fixture)
    disk = fixture / "disk.hex"
    disk.write_text("00\n" * 174848)
    for region, simulator, phi_hz, expected_trace, expected_pcm in CASES:
        run = fixture / region
        run.mkdir(exist_ok=True)
        trace = run / "sid-writes.csv"
        audio = run / "audio.pcm"
        command = [
            build / simulator / "Vc64_boot_sim_top",
            f"+C64_EXPECT_CPU_HZ={phi_hz}", "+C64_EXPECT_SID=6581",
            f"+C64_FIRMWARE_HEX={fixture / 'firmware.hex'}", f"+C64_DISK_HEX={disk}",
            f"+C64_SYS_CYCLES={SYS_CYCLES}", f"+C64_SID_TRACE={trace}",
            f"+C64_AUDIO_PCM={audio}",
        ]
        with (run / "run.log").open("w") as log:
            subprocess.run(command, cwd=run, stdout=log, stderr=subprocess.STDOUT, check=True)
        with trace.open() as source:
            stores = [{k: int(v) for k, v in row.items()} for row in csv.DictReader(source)]
        check_sid_stores(stores, phi_hz)
        trace_digest = hashlib.sha256(trace.read_bytes()).hexdigest()
        assert trace_digest == expected_trace, f"{region} 6581 register trace: {trace_digest}"
        pcm = audio.read_bytes()
        digest = hashlib.sha256(pcm).hexdigest()
        assert len(pcm) == (SYS_CYCLES // 1088) * 2
        assert digest == expected_pcm, f"{region} 6581 PCM: {digest}, expected {expected_pcm}"
        print(f"{region} 6581 PASS: {len(stores)} timed stores, {len(pcm) // 2} PCM samples")


if __name__ == "__main__":
    main()
