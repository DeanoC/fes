#!/usr/bin/env python3
"""Check SMS RAM/media against unmodified, locally supplied Intel models."""
from __future__ import annotations

import argparse
import hashlib
import os
from pathlib import Path
import subprocess
import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from scripts.build_fes_sms import SYSTEMVERILOG_SOURCES, VERILOG_SOURCES


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--quartus-root", type=Path, default=os.environ.get("QUARTUS_ROOTDIR"))
    parser.add_argument("--iverilog", default=os.environ.get("IVERILOG", "iverilog"))
    parser.add_argument("--iverilog-base", default=os.environ.get("IVERILOG_BASE"))
    parser.add_argument("--vvp", default=os.environ.get("VVP", "vvp"))
    args = parser.parse_args()
    if args.quartus_root is None:
        parser.error("set QUARTUS_ROOTDIR to the Quartus 17.0.2 quartus directory")
    model = args.quartus_root.resolve() / "eda/sim_lib/altera_mf.v"
    if not model.is_file():
        parser.error(f"missing Intel simulation model: {model}")
    root = Path(__file__).resolve().parents[1]
    output = root / "build/sim/fes-sms-quartus"
    output.mkdir(parents=True, exist_ok=True)
    sources = [
        s
        for s in (*VERILOG_SOURCES, *SYSTEMVERILOG_SOURCES)
        if Path(s).name not in ("sys_pll.v", "pixel_pll.v", "top.v")
    ]
    identity = f"{model}: sha256 {hashlib.sha256(model.read_bytes()).hexdigest()}\n"
    print(identity, end="", flush=True)
    selected = [root / source for source in sources]
    for test in ("quartus_ram_tb", "quartus_media_tb"):
        binary = output / f"{test}.vvp"
        command = [args.iverilog]
        if args.iverilog_base:
            command += ["-B", args.iverilog_base]
        command += [
            "-g2012",
            "-DQUARTUS=1",
            "-I",
            str(root / "cores/fes-sms/generated"),
            "-s",
            test,
            "-o",
            str(binary),
            str(root / f"cores/fes-sms/sim/{test}.sv"),
            *map(str, selected),
            str(model),
        ]
        compile_result = subprocess.run(command, capture_output=True, text=True)
        (output / f"{test}.compile.log").write_text(compile_result.stdout + compile_result.stderr)
        if compile_result.returncode:
            raise RuntimeError(f"compile failed: {output / (test + '.compile.log')}")
        result = subprocess.run(
            [args.vvp, str(binary)],
            cwd=output,
            capture_output=True,
            text=True,
            timeout=120,
        )
        log = identity + result.stdout + result.stderr
        (output / f"{test}.log").write_text(log)
        print(f"{test}:\n{result.stdout}", end="", flush=True)
        if result.returncode != 0 or "passed" not in result.stdout or "ERROR:" in log:
            raise RuntimeError(f"vendor regression failed: {test}")


if __name__ == "__main__":
    main()
