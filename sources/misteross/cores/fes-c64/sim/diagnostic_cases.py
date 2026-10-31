#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Positive and negative controls for the original native-drive diagnostic."""
import argparse
from pathlib import Path
import subprocess


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--model", type=Path, required=True)
    parser.add_argument("--diagnostic-dir", type=Path, required=True)
    parser.add_argument("--case", action="append", choices=("positive", "changed-payload", "no-media"),
                        help="run only these cases (default all)")
    args = parser.parse_args()
    directory = args.diagnostic_dir.resolve()
    model = str(args.model.resolve())
    disk = bytearray((directory / "disk.d64").read_bytes())
    assert disk[:8] == bytes((0, 7, 1, 8, 0x11, 0x22, 0x33, 0x44))
    disk[4] ^= 1
    changed = directory / "disk-changed.hex"
    changed.write_text("".join(f"{b:02x}\n" for b in disk))
    common = [model, f"+C64_FIRMWARE_HEX={directory / 'firmware.hex'}"]
    cases = (
        ("positive", "disk.hex", [], 0, "delayed=1"),
        ("changed-payload", "disk-changed.hex", [], 1, "load_status 0"),
        ("no-media", "disk.hex", ["+C64_DIAG_NO_MEDIA"], 1, "load_status 128"),
    )
    for name, image, extra, code, marker in cases:
        if args.case and name not in args.case:
            continue
        # A stalled model or broken harness is a failure, not an accepted
        # negative control. Both negatives must reach the firmware's stage 9.
        result = subprocess.run(common + [f"+C64_DISK_HEX={directory / image}"] + extra,
                                text=True, capture_output=True, timeout=180)
        log = result.stdout + result.stderr
        (directory / f"native-{name}.log").write_text(log)
        if result.returncode != code or marker not in log or (code and "failed stage 9" not in log):
            raise RuntimeError(f"{name}: exit {result.returncode}, expected {code} and {marker!r}\n{log}")
        if name == "positive" and "delayed=0" not in log:
            raise RuntimeError("positive case skipped immediate media/input coverage")
        print(f"native diagnostic {name}: passed")


if __name__ == "__main__":
    main()
