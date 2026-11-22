#!/usr/bin/env python3
"""Test the board RAM against its portable source or prepare a tiny Quartus project."""
import argparse
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import tempfile

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[4]
PORTABLE = ROOT / "sources/misteross/cores/fes-zx81/rtl/zx81_tape_store.v"


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def output_directory(selected):
    if selected:
        selected = selected.resolve(); selected.mkdir(parents=True, exist_ok=False)
        return selected
    parent = ROOT / "out/de25-tape-store"
    parent.mkdir(parents=True, exist_ok=True)
    return Path(tempfile.mkdtemp(prefix="run-", dir=parent))


def selected_sources(output):
    replacement = (HERE / "tape_store.v").read_bytes()
    original = PORTABLE.read_bytes()
    if replacement.split(b"`else\n", 1)[1].rsplit(b"`endif\n", 1)[0] != original:
        raise ValueError("board behavioral branch must contain the entire portable source verbatim")
    (output / "tape_store.v").write_bytes(replacement)
    return replacement, original


def project(output):
    selected_sources(output)
    (output / "tape_store.qpf").write_text('QUARTUS_VERSION = "26.3.0"\nPROJECT_REVISION = "tape_store"\n')
    (output / "tape_store.qsf").write_text(
        'set_global_assignment -name FAMILY "Agilex 5"\n'
        'set_global_assignment -name DEVICE A5EB013BB23BE4SCS\n'
        'set_global_assignment -name TOP_LEVEL_ENTITY zx81_tape_store\n'
        'set_global_assignment -name VERILOG_FILE tape_store.v\n'
        'set_global_assignment -name VERILOG_MACRO QUARTUS=1\n'
        'set_global_assignment -name PROJECT_OUTPUT_DIRECTORY output_files\n'
        'set_global_assignment -name NUM_PARALLEL_PROCESSORS 2\n'
        'set_global_assignment -name SDC_FILE tape_store.sdc\n')
    (output / "tape_store.sdc").write_text('create_clock -name tape -period 10 [get_ports clk]\n')
    (output / "README.txt").write_text(
        'Prepared only; no Quartus command was run.\n'
        'From this private directory, run Quartus Pro26.3 synthesis only:\n'
        '/data/altera_pro/26.3/quartus/bin/quartus_syn tape_store --rev=tape_store\n'
        'Inspect output_files/tape_store.syn.rpt and .syn.summary for two8192x8\n'
        'simple-dual-port OLD_DATA banks in M20K, total131072 RAM bits;\n'
        'do not accept a131072-register/logic implementation.\n')


def simulate(output, args):
    replacement, original = selected_sources(output)
    (output / "primitive.v").write_bytes(b'`define QUARTUS\n' + replacement + b'\n`undef QUARTUS\n')
    (output / "behavior.v").write_bytes(replacement.replace(
        b"module zx81_tape_store (", b"module zx81_tape_store_behavior ("))
    (output / "reference.v").write_bytes(original.replace(
        b"module zx81_tape_store (", b"module zx81_tape_store_reference ("))
    shutil.copyfile(HERE / "tape_store_tb.sv", output / "tape_store_tb.sv")
    model = args.vendor_model.resolve()
    command = [args.iverilog]
    if args.iverilog_base:
        command += ["-B" + str(args.iverilog_base.resolve())]
    command += ["-g2012", "-s", "tape_store_tb", "-o", str(output / "tape_store.vvp"),
                str(model), *(str(output / name) for name in
                ("primitive.v", "behavior.v", "reference.v", "tape_store_tb.sv"))]
    result = subprocess.run(command, cwd=output, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
    (output / "compile.log").write_text(result.stdout)
    result.check_returncode()
    run = [args.vvp, "-n", str(output / "tape_store.vvp")]
    result = subprocess.run(run, cwd=output, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
    (output / "simulation.log").write_text(result.stdout)
    print(result.stdout, end="")
    result.check_returncode()
    if "PASS tape-store differential:" not in result.stdout or "Error:" in result.stdout:
        raise ValueError("vendor-model simulation did not complete the differential bench")
    record = {"evidence_class": "host vendor-model functional differential; no physical mapping claim",
        "vendor_model": str(model), "vendor_model_sha256": digest(model),
        "vendor_model_notices": [line for line in result.stdout.splitlines() if line.startswith("Warning:")],
        "power_up_boundary": "unwritten portable array bytes are undefined and excluded from equality checks",
        "portable_source_sha256": digest(PORTABLE), "board_source_sha256": digest(HERE / "tape_store.v"),
        "bench_sha256": digest(HERE / "tape_store_tb.sv"), "compile_command": command,
        "simulation_command": run, "simulation_log_sha256": digest(output / "simulation.log")}
    (output / "receipt.json").write_text(json.dumps(record, indent=2, sort_keys=True) + "\n")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("simulate", "project"))
    parser.add_argument("--output", type=Path)
    parser.add_argument("--vendor-model", type=Path)
    parser.add_argument("--iverilog", default="iverilog")
    parser.add_argument("--iverilog-base", type=Path)
    parser.add_argument("--vvp", default="vvp")
    args = parser.parse_args()
    if args.action == "simulate" and args.vendor_model is None:
        parser.error("simulate requires --vendor-model pointing to the actual supplied altera_mf.v")
    output = output_directory(args.output)
    print("Tape store " + args.action + ": " + str(output), flush=True)
    if args.action == "project": project(output)
    else: simulate(output, args)


if __name__ == "__main__":
    main()
