#!/usr/bin/env python3
"""Simulate the splash HDMI mark, motion, and ADV7513 I2C board path."""
from __future__ import annotations

import argparse
from pathlib import Path
import struct
import subprocess
import zlib

ROOT = Path(__file__).resolve().parents[1]


def write_png(ppm_path: Path, png_path: Path) -> None:
    data = ppm_path.read_bytes()
    header, _, body = data.partition(b"\n1280 720\n255\n")
    if not header.startswith(b"P6") or len(body) != 1280 * 720 * 3:
        raise SystemExit(f"unexpected PPM: {ppm_path}")
    raw = b"".join(b"\x00" + body[row * 3840:(row + 1) * 3840] for row in range(720))

    def chunk(tag: bytes, payload: bytes) -> bytes:
        return struct.pack(">I", len(payload)) + tag + payload + struct.pack(">I", zlib.crc32(tag + payload) & 0xFFFFFFFF)

    png_path.write_bytes(
        b"\x89PNG\r\n\x1a\n"
        + chunk(b"IHDR", struct.pack(">IIBBBBB", 1280, 720, 8, 2, 0, 0, 0))
        + chunk(b"IDAT", zlib.compress(raw, 9))
        + chunk(b"IEND", b"")
    )


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--verilator", default="verilator")
    args = parser.parse_args()
    frames = ROOT / "build/sim/fes-splash-frames"
    frames.mkdir(parents=True, exist_ok=True)
    video = ROOT / "build/sim/fes-splash-video"
    video.mkdir(parents=True, exist_ok=True)
    frame0 = frames / "frame0.ppm"
    frame1 = frames / "frame1.ppm"
    subprocess.run([
        args.verilator, "--cc", "--exe", "--build", "--top-module", "fes_splash_core",
        "-Wall", "-Wno-UNUSEDSIGNAL", "--Mdir", str(video),
        "cores/fes-splash/rtl/fes_splash_core.v",
        str(ROOT / "cores/fes-splash/sim/video_tb.cpp"),
    ], cwd=ROOT, check=True)
    subprocess.run([str(video / "Vfes_splash_core"), str(frame0), str(frame1)], cwd=ROOT, check=True)
    write_png(frame0, frames / "frame0.png")
    write_png(frame1, frames / "frame1.png")
    board = ROOT / "build/sim/fes-splash-board"
    board.mkdir(parents=True, exist_ok=True)
    subprocess.run([
        args.verilator, "--cc", "--exe", "--build", "--top-module", "top",
        "-Wall", "--public-flat-rw", "-Wno-PINCONNECTEMPTY", "-Wno-UNUSEDSIGNAL",
        "--Mdir", str(board),
        "cores/fes-splash/sim/board_models.v",
        "cores/fes-splash/rtl/top.v",
        "cores/fes-splash/rtl/fes_splash_core.v",
        str(ROOT / "cores/fes-splash/sim/board_tb.cpp"),
    ], cwd=ROOT, check=True)
    subprocess.run([str(board / "Vtop")], cwd=ROOT, check=True)


if __name__ == "__main__":
    main()
