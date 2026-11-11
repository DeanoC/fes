#!/usr/bin/env python3
"""Simulate shared application endpoints, controller ports and I2S audio."""
from pathlib import Path
import argparse
import json
import subprocess

ROOT = Path(__file__).resolve().parents[1]

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--verilator", default="verilator")
    args = parser.parse_args()
    for module, sources, bench in (
        ("fes_audio_i2s", ["cores/fes-common/rtl/fes_audio_i2s.v"], "audio_tb.cpp"),
    ):
        output = ROOT / f"build/sim/{module}"
        output.mkdir(parents=True, exist_ok=True)
        subprocess.run([args.verilator, "--cc", "--exe", "--build", "--top-module", module,
                        "-Wall", "-Wno-SYNCASYNCNET", "-Wno-UNUSEDSIGNAL", "--Mdir", str(output), *sources,
                        str(ROOT / "cores/fes-common/sim" / bench)], cwd=ROOT, check=True)
        subprocess.run([str(output / f"V{module}")], cwd=ROOT, check=True)
    scenarios = json.loads((ROOT / "cores/fes-common/generated/exchanges.json").read_text())["scenarios"]
    for gamepad, media, audio, ports, keypad in ((0, 0, 0, 0, 0), (1, 0, 0, 0, 0), (0, 1, 0, 0, 0), (1, 1, 0, 0, 0), (1, 0, 1, 0, 0), (0, 0, 0, 1, 0), (0, 1, 0, 1, 1), (0, 0, 0, 1, 1)):
        output = ROOT / f"build/sim/fes-application-gp-{gamepad}-{media}-{audio}-{ports}-{keypad}"
        output.mkdir(parents=True, exist_ok=True)
        subprocess.run([
            args.verilator, "--cc", "--exe", "--build", "--top-module", "fes_application_gp",
            "-Wall", "-Icores/fes-common/generated", "--Mdir", str(output),
            f"-GENABLE_GAMEPAD=1'b{gamepad}", f"-GENABLE_MEDIA=1'b{media}",
            f"-GENABLE_AUDIO=1'b{audio}",
            f"-GENABLE_CONTROLLER_PORTS=1'b{ports}", f"-GENABLE_KEYPAD_PORTS=1'b{keypad}",
            "-CFLAGS", f"-DGAMEPAD={gamepad} -DMEDIA={media} -DAUDIO={audio} -DPORTS={ports} -DKEYPAD={keypad}",
            "cores/fes-common/rtl/fes_application_gp.v", str(ROOT / "cores/fes-common/sim/gp_tb.cpp"),
        ], cwd=ROOT, check=True)
        fixture_args = []
        for scenario in scenarios:
            if scenario["capabilities"] == 2 | gamepad | (media << 2) | (audio << 4) | (ports << 5) | (keypad << 6):
                fixture = output / "exchanges.txt"
                fixture.write_text("".join(
                    f"{row['gpo'][0]} {row['gpo'][1]} {row['gpi']}\n"
                    for row in scenario["exchanges"]))
                fixture_args = [str(fixture)]
        if ports and keypad and not media:
            controller_fixture = json.loads((ROOT / "cores/fes-common/generated/controller-exchanges.json").read_text())
            fixture = output / "controllers.txt"
            fixture.write_text("".join(
                f"{r['gpo'][0]} {r['gpo'][1]} {r['gpi']} {r['buttons'][0]} {r['buttons'][1]} {r['keypad'][0]} {r['keypad'][1]}\n"
                for r in controller_fixture["exchanges"]))
            fixture_args = [str(fixture), "states"]
        subprocess.run([str(output / "Vfes_application_gp"), *fixture_args], cwd=ROOT, check=True)

if __name__ == "__main__":
    main()
