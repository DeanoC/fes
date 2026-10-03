#!/usr/bin/env python3
"""Diagnostic native consumer synthesis; no placement, sealing or hardware."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess

from build_fes_coleco_socket_v2 import authenticate_tools

ROOT = Path(__file__).resolve().parents[1]
SOURCES = (
    "cores/fes-common/generated/fes_native_video.vh",
    "cores/fes-common/rtl/coleco_video_dpram.v",
    "cores/fes-common/rtl/fes_native_video.v",
)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--cache-root", type=Path, required=True)
    args = parser.parse_args()
    tools = authenticate_tools(ROOT, args.cache_root)
    sources = {name: hashlib.sha256((ROOT / name).read_bytes()).hexdigest() for name in SOURCES}
    for variant, scanlines in (("direct", 0), ("scanlines", 1)):
        output = ROOT / "build/synth/fes-native-video" / variant
        output.mkdir(parents=True, exist_ok=True)
        program = (
            "read_verilog -sv -I cores/fes-common/generated " + " ".join(SOURCES[1:]) + "; "
            f"chparam -set SCANLINES {scanlines} fes_native_video; "
            "synth_intel_alm -nolutram -nodsp -top fes_native_video; stat; "
            "write_json synth.json"
        )
        # Read paths are anchored at ROOT; output path is supplied separately
        # as a Yosys command with quoting for arbitrary checkout paths.
        program = program.replace("write_json synth.json", "write_json " + json.dumps(str(output / "synth.json")))
        with (output / "yosys.log").open("w") as log:
            subprocess.run([str(tools["yosys"].path), "-p", program], cwd=ROOT,
                           stdout=log, stderr=subprocess.STDOUT, check=True)
        data = json.loads((output / "synth.json").read_text())["modules"]["fes_native_video"]
        counts = {}
        clocks = set()
        for cell in data["cells"].values():
            kind = cell["type"]
            counts[kind] = counts.get(kind, 0) + 1
            if kind == "MISTRAL_M10K":
                for pin in ("CLK1", "CLK2"):
                    clocks.add(tuple(cell["connections"][pin]))
        if counts.get("MISTRAL_M10K") != 48 or len(clocks) != 1:
            raise ValueError(f"native RAM mapping/clock differs: {counts}, {clocks}")
        if sources != {name: hashlib.sha256((ROOT / name).read_bytes()).hexdigest() for name in SOURCES}:
            raise ValueError("native sources changed during diagnostic synthesis")
        receipt = {"format": 1, "classification": "diagnostic-synthesis-only",
                   "variant": variant, "yosys": tools["yosys"].identity,
                   "sources": sources, "cells": counts,
                   "limits": ["no placement or timing", "no sealed part", "no hardware acceptance"]}
        (output / "diagnostic.json").write_text(json.dumps(receipt, sort_keys=True, indent=2) + "\n")
        print(f"{variant}: {counts['MISTRAL_M10K']} M10Ks, one RAM clock; diagnostic only")


if __name__ == "__main__":
    main()
