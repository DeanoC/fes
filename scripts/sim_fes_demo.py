#!/usr/bin/env python3
"""Simulate the real application endpoint in all four capability combinations."""
from pathlib import Path
import argparse
import json
import subprocess

ROOT = Path(__file__).resolve().parents[1]

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--verilator", default="verilator")
    args = parser.parse_args()
    scenarios = json.loads((ROOT / "cores/fes-common/generated/exchanges.json").read_text())["scenarios"]
    for gamepad, media in ((0, 0), (1, 0), (0, 1), (1, 1)):
        output = ROOT / f"build/sim/fes-demo-gp-{gamepad}-{media}"
        output.mkdir(parents=True, exist_ok=True)
        subprocess.run([
            args.verilator, "--cc", "--exe", "--build", "--top-module", "fes_application_gp",
            "-Wall", "-Icores/fes-common/generated", "--Mdir", str(output),
            f"-GENABLE_GAMEPAD=1'b{gamepad}", f"-GENABLE_MEDIA=1'b{media}",
            "-CFLAGS", f"-DGAMEPAD={gamepad} -DMEDIA={media}",
            "cores/fes-common/rtl/fes_application_gp.v", str(ROOT / "cores/fes-demo/sim/gp_tb.cpp"),
        ], cwd=ROOT, check=True)
        fixture_args = []
        for scenario in scenarios:
            if scenario["capabilities"] == 2 | gamepad | (media << 2):
                fixture = output / "exchanges.txt"
                fixture.write_text("".join(
                    f"{row['gpo'][0]} {row['gpo'][1]} {row['gpi']}\n"
                    for row in scenario["exchanges"]))
                fixture_args = [str(fixture)]
        subprocess.run([str(output / "Vfes_application_gp"), *fixture_args], cwd=ROOT, check=True)
    output = ROOT / "build/sim/fes-demo-video"
    output.mkdir(parents=True, exist_ok=True)
    subprocess.run([
        args.verilator, "--cc", "--exe", "--build", "--top-module", "fes_demo_core",
        "-Wall", "-Wno-PINCONNECTEMPTY", "-Wno-UNUSEDSIGNAL", "--Mdir", str(output),
        "cores/fes-demo/rtl/fes_demo_core.v", "cores/fes-common/rtl/fes_video_720p.v",
        str(ROOT / "cores/fes-demo/sim/video_tb.cpp"),
    ], cwd=ROOT, check=True)
    subprocess.run([str(output / "Vfes_demo_core")], cwd=ROOT, check=True)
    for media in (0, 1):
        output = ROOT / f"build/sim/fes-demo-board-{media}"
        output.mkdir(parents=True, exist_ok=True)
        subprocess.run([
            args.verilator, "--cc", "--exe", "--build", "--top-module", "top",
            "-Wall", "--public-flat-rw", "-Wno-PINCONNECTEMPTY", "-Wno-UNUSEDSIGNAL",
            "-Icores/fes-common/generated", "--Mdir", str(output),
            f"-GENABLE_GAMEPAD=1'b{media}", f"-GENABLE_MEDIA=1'b{media}",
            "-CFLAGS", f"-DMEDIA={media}",
            "cores/fes-pong/sim/board_models.v", "cores/fes-demo/rtl/top.v",
            "cores/fes-demo/rtl/fes_demo_core.v", "cores/fes-common/rtl/fes_application_gp.v",
            "cores/fes-common/rtl/fes_video_720p.v", str(ROOT / "cores/fes-demo/sim/board_tb.cpp"),
        ], cwd=ROOT, check=True)
        subprocess.run([str(output / "Vtop")], cwd=ROOT, check=True)

if __name__ == "__main__":
    main()
