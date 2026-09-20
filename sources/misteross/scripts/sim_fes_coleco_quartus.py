#!/usr/bin/env python3
"""Check Coleco RAM/media against unmodified, locally supplied Intel models."""
from __future__ import annotations

import argparse
import hashlib
import os
from pathlib import Path
import shutil
import subprocess
import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from scripts.build_fes_coleco import RESET_ROM_MIF, SYSTEMVERILOG_SOURCES, VERILOG_SOURCES


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--quartus-root", type=Path, default=os.environ.get("QUARTUS_ROOTDIR"))
    parser.add_argument("--iverilog", default=os.environ.get("IVERILOG", "iverilog"))
    parser.add_argument("--iverilog-base", default=os.environ.get("IVERILOG_BASE"))
    parser.add_argument("--vvp", default=os.environ.get("VVP", "vvp"))
    parser.add_argument("--baseline", help="also require both probes to fail against this pre-fix revision")
    args = parser.parse_args()
    if args.quartus_root is None:
        parser.error("set QUARTUS_ROOTDIR to the Quartus 17.0.2 quartus directory")
    model = args.quartus_root.resolve() / "eda/sim_lib/altera_mf.v"
    if not model.is_file():
        parser.error(f"missing Intel simulation model: {model}")
    root = Path(__file__).resolve().parents[1]
    output = root / "build/sim/fes-coleco-quartus"
    sources = [s for s in (*VERILOG_SOURCES, *SYSTEMVERILOG_SOURCES)
               if Path(s).name not in ("sys_pll.v", "coleco_system_pll.v", "pixel_pll.v", "top.v")]
    identity = f"{model}: sha256 {hashlib.sha256(model.read_bytes()).hexdigest()}\n"
    print(identity, end="", flush=True)
    for label, revision in (("before", args.baseline), ("after", None)):
        if label == "before" and not revision:
            continue
        work = output / label
        (work / RESET_ROM_MIF).parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(root / RESET_ROM_MIF, work / RESET_ROM_MIF)
        selected = [root / source for source in sources]
        if revision:
            # Generated snapshots only; never edit a production checkout/model.
            for name in ("coleco_machine.sv", "coleco_video_dpram.v"):
                relative = f"cores/fes-coleco/rtl/{name}"
                data = subprocess.check_output(["git", "show", f"{revision}:{relative}"], cwd=root)
                snapshot = work / name
                snapshot.write_bytes(data)
                current_relative = ("cores/fes-common/rtl/coleco_video_dpram.v"
                                    if name == "coleco_video_dpram.v" else relative)
                selected[selected.index(root / current_relative)] = snapshot
        for test in ("quartus_ram_tb", "quartus_media_tb"):
            binary = work / f"{test}.vvp"
            command = [args.iverilog]
            if args.iverilog_base:
                command += ["-B", args.iverilog_base]
            command += ["-g2012", "-DQUARTUS=1", "-I", str(root / "cores/fes-common/generated"),
                        "-s", test, "-o", str(binary),
                        str(root / f"cores/fes-coleco/sim/{test}.sv"),
                        *map(str, selected), str(model)]
            compile_result = subprocess.run(command, capture_output=True, text=True)
            (work / f"{test}.compile.log").write_text(compile_result.stdout + compile_result.stderr)
            if compile_result.returncode:
                raise RuntimeError(f"compile failed: {work / (test + '.compile.log')}")
            result = subprocess.run([args.vvp, str(binary)], cwd=work,
                                    capture_output=True, text=True, timeout=120)
            log = identity + result.stdout + result.stderr
            (work / f"{test}.log").write_text(log)
            print(f"{label} {test}:\n{result.stdout}", end="", flush=True)
            if revision:
                expected = ("unexpected read cycle" if test == "quartus_ram_tb"
                            else "Quartus cartridge[1]=40 expected=41 size=3")
                if result.returncode == 0 or expected not in result.stdout:
                    raise RuntimeError(f"baseline did not reproduce expected failure: {test}")
            elif result.returncode != 0 or "passed" not in result.stdout or "ERROR:" in log:
                raise RuntimeError(f"vendor regression failed: {test}")


if __name__ == "__main__":
    main()
