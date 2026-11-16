#!/usr/bin/env python3
"""Fit the standalone x87 backend at 200 MHz; no programming artifact is made."""
from __future__ import annotations

import argparse
from contextlib import contextmanager
import fcntl
import hashlib
import importlib.util
import json
import math
import os
from pathlib import Path
import re
import shutil
import signal
import subprocess
import tempfile
import time

HERE = Path(__file__).resolve().parent
MISTEROSS = HERE.parents[2]
ROOT = MISTEROSS.parents[1]
PROJECT = "fes_de25_x87"
DEVICE = "A5EB013BB23BE4SCS"
PERIOD_NS = 5.0
TIMEOUT_SECONDS = 30 * 60
MIN_FREE_BYTES = 30 * 1024 ** 3
LOCK = Path("/tmp/lander-quartus-3090.lock")
RTL = ("fes_x87_pkg.sv", "fes_x87_fast.sv", "fes_x87_slow.sv",
       "fes_x87_round.sv", "fes_x87_pipe.sv")


class BuildError(RuntimeError):
    pass


def sha(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def board_tools():
    spec = importlib.util.spec_from_file_location("x87_de25_board_build", HERE.parent / "build.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def require_disk(path: Path) -> None:
    while not path.exists():
        path = path.parent
    free = shutil.disk_usage(path).free
    if free < MIN_FREE_BYTES:
        raise BuildError(f"refusing build with {free / 1024**3:.1f} GiB free; at least 30 GiB required")


@contextmanager
def compiler_lease():
    # The established builder-wide lock covers discovery, snapshot and every
    # Quartus process. Never interrupt a different owner's compiler job.
    with LOCK.open("a+") as lease:
        last_notice = -math.inf
        while True:
            try:
                fcntl.flock(lease, fcntl.LOCK_EX | fcntl.LOCK_NB)
                break
            except BlockingIOError:
                if time.monotonic() - last_notice >= 30:
                    print(f"x87 build heartbeat: waiting for {LOCK}", flush=True)
                    last_notice = time.monotonic()
                time.sleep(1)
        try:
            yield
        finally:
            fcntl.flock(lease, fcntl.LOCK_UN)


def prepare(output: Path | None = None) -> Path:
    board = board_tools()
    metadata = board.quartus_metadata()
    sources = {"rtl/" + name: (HERE / "rtl" / name).read_bytes() for name in RTL}
    sources.update({"build.py": (HERE / "build.py").read_bytes(),
                    "timing.tcl": (HERE / "timing.tcl").read_bytes(),
                    "board-build.py": (HERE.parent / "build.py").read_bytes(),
                    "quartus.lock": (HERE.parent / "quartus.lock").read_bytes(),
                    "quartus_tools.py": (MISTEROSS / "scripts/quartus_tools.py").read_bytes()})
    if output is None:
        parent = MISTEROSS / "build/diagnostic/de25nano-x87"
        parent.mkdir(parents=True, exist_ok=True)
        output = Path(tempfile.mkdtemp(prefix="run-", dir=parent))
    else:
        output = output.resolve()
        output.mkdir(parents=True, exist_ok=False)
    project = output / "project"
    project.mkdir()
    snapshot = output / "source"
    for name, data in sources.items():
        path = snapshot / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(data)
    for name in RTL:
        (project / name).write_bytes(sources["rtl/" + name])
    (project / "timing.tcl").write_bytes(sources["timing.tcl"])
    (project / "clocks.sdc").write_text(
        "# Internal virtual-pin fit only; no board I/O timing is established.\n"
        "create_clock -name x87_clock -period 5.000 [get_ports clk]\n"
        "derive_clock_uncertainty\n"
        "set data_inputs [remove_from_collection [all_inputs] [get_ports clk]]\n"
        "set_input_delay -clock x87_clock -max 0.000 $data_inputs\n"
        "set_input_delay -clock x87_clock -min 0.000 $data_inputs\n"
        "set_output_delay -clock x87_clock -max 0.000 [all_outputs]\n"
        "set_output_delay -clock x87_clock -min 0.000 [all_outputs]\n")
    (project / f"{PROJECT}.qpf").write_text(
        f'QUARTUS_VERSION = "{metadata["series"]}"\nPROJECT_REVISION = "{PROJECT}"\n')
    assignments = (("FAMILY", '"Agilex 5"'), ("DEVICE", DEVICE),
                   ("TOP_LEVEL_ENTITY", "fes_x87_pipe"),
                   ("PROJECT_OUTPUT_DIRECTORY", "output_files"),
                   ("LAST_QUARTUS_VERSION", f'"{metadata["qsf_version"]}"'),
                   ("NUM_PARALLEL_PROCESSORS", "2"), ("SEED", "1"),
                   ("SDC_FILE", "clocks.sdc"))
    # Retain a real primary clock input for clock-tree fitting. Every data and
    # control port is virtual; the resulting fit has no DE25 pinout contract.
    virtual_ports = ("reset", "flush", "req_valid", "req_ready", "req_op[*]",
                     "req_a[*]", "req_b[*]", "req_pc[*]", "req_rc[*]", "req_tag[*]",
                     "resp_valid", "resp_ready", "resp_value[*]", "resp_flags[*]",
                     "resp_cc[*]", "resp_incremented", "resp_tag[*]")
    qsf = "# Internal-only 200 MHz virtual-pin characterization; never assemble/program.\n"
    qsf += "".join(f"set_global_assignment -name {key} {value}\n" for key, value in assignments)
    qsf += "".join(f"set_global_assignment -name SYSTEMVERILOG_FILE {name}\n" for name in RTL)
    qsf += "".join(f"set_instance_assignment -name VIRTUAL_PIN ON -to {{{port}}}\n" for port in virtual_ports)
    qsf += 'set_instance_assignment -name IO_STANDARD "1.1-V" -to clk\n'
    (project / f"{PROJECT}.qsf").write_text(qsf)
    revision = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
    inputs = {"fes_base_revision": revision, "source_classification": "working-copy-diagnostic",
              "board": "de25_nano", "device": DEVICE, "project": PROJECT,
              "top": "fes_x87_pipe", "clock_hz": 200_000_000, "period_ns": PERIOD_NS,
              "quartus": metadata, "diagnostic_only": True, "virtual_data_ports": list(virtual_ports),
              "primary_clock": "clk (fitter-assigned location)",
              "evidence_scope": "internal virtual-pin fit; no board I/O or hardware acceptance",
              "source_sha256": {name: sha(data) for name, data in sources.items()},
              "project_sha256": {path.name: sha(path.read_bytes()) for path in project.iterdir()}}
    (output / "inputs.json").write_text(json.dumps(inputs, indent=2, sort_keys=True) + "\n")
    return output


def run(command: list[str], output: Path, label: str, deadline: float) -> None:
    remaining = deadline - time.monotonic()
    if remaining <= 0:
        raise BuildError("30 minute Quartus time cap expired")
    print(f"x87 build: {label} started; log {output / (label + '.log')}", flush=True)
    with (output / (label + ".log")).open("w") as log:
        process = subprocess.Popen(command, cwd=output / "project", stdout=log,
                                   stderr=subprocess.STDOUT, start_new_session=True)
        try:
            while True:
                try:
                    status = process.wait(timeout=min(30, max(0.01, deadline-time.monotonic())))
                    break
                except subprocess.TimeoutExpired:
                    if time.monotonic() >= deadline:
                        raise BuildError(f"30 minute Quartus time cap expired during {label}")
                    print(f"x87 build heartbeat: {label}; pid {process.pid}; "
                          f"{int(deadline-time.monotonic())} seconds remain", flush=True)
        except BaseException:
            # This is our own new process group, including Quartus children.
            os.killpg(process.pid, signal.SIGTERM)
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait()
            raise
    if status:
        raise BuildError(f"{label} failed ({status}); retained {output / (label + '.log')}")


def resources(text: str) -> dict:
    labels = {"alms": ("Logic utilization (ALMs needed / total ALMs on device)",
                       "Logic utilization (in ALMs)"),
              "registers": ("Dedicated logic registers", "Total registers", "Total dedicated logic registers"),
              "dsp_blocks": ("DSP Blocks Needed [=A+B+C-D]", "Total DSP Blocks", "DSP blocks"),
              "m20k_blocks": ("M20K blocks", "M20K Blocks", "Total M20K Blocks"),
              "block_memory_bits": ("Total block memory bits",)}
    result = {}
    for resource, names in labels.items():
        for name in names:
            match = re.search(rf"^;\s*{re.escape(name)}\s*;\s*([\d,]+(?:\.\d+)?)"
                              r"(?:\s*/\s*([\d,]+(?:\.\d+)?))?", text, re.M)
            if match:
                counts = [float(x.replace(",", "")) if x else None for x in match.groups()]
                result[resource] = {"used": counts[0]}
                if counts[1] is not None:
                    result[resource]["available"] = counts[1]
                break
        else:
            raise BuildError(f"fitter report has no {resource} count")
    return result


def compile_project(output: Path, tool: Path, version: str) -> dict:
    inputs = json.loads((output / "inputs.json").read_text())
    project = output / "project"
    for name, expected in inputs["project_sha256"].items():
        if sha((project / name).read_bytes()) != expected:
            raise BuildError(f"prepared project source changed: {name}")
    (output / "quartus-version.txt").write_text(version + "\n")
    tools = {name: tool.with_name(name) for name in ("quartus_syn", "quartus_fit", "quartus_sta")}
    for path in tools.values():
        if not path.is_file() or not os.access(path, os.X_OK):
            raise BuildError(f"missing executable {path}")
    record = {"quartus_version": version, "clock_hz": 200_000_000, "device": DEVICE,
              "evidence_scope": inputs["evidence_scope"], "hardware_acceptance": "not-run",
              "programming_artifact": None, "tool_sha256": {str(p): sha(p.read_bytes()) for p in tools.values()},
              "commands": []}
    deadline = time.monotonic() + TIMEOUT_SECONDS
    # The aggregate report is written after successful finalization. Its
    # final resource summary includes changes made by routing and retiming.
    fitted = project / "output_files" / f"{PROJECT}.fit.rpt"
    try:
        for label, command in (("synthesis", [str(tools["quartus_syn"]), PROJECT]),
                               ("fit", [str(tools["quartus_fit"]), PROJECT]),
                               ("sta", [str(tools["quartus_sta"]), "--multicorner", PROJECT]),
                               ("timing-gate", [str(tools["quartus_sta"]), "-t", "timing.tcl", str(output)])):
            require_disk(output)
            record["commands"].append(command)
            run(command, output, label, deadline)
        record["resources"] = resources(fitted.read_text(errors="replace"))
        record["timing"] = json.loads((output / "timing.json").read_text())
        if record["timing"].get("status") != "pass":
            raise BuildError("200 MHz all-corner timing gate did not pass")
        sta_text = (project / "output_files" / f"{PROJECT}.sta.rpt").read_text(errors="replace")
        unconstrained = re.findall(r"^;\s*(Illegal Clocks|Unconstrained Clocks)\s*;\s*(\d+)\s*;\s*(\d+)\s*;", sta_text, re.M)
        if {row[0] for row in unconstrained} != {"Illegal Clocks", "Unconstrained Clocks"}:
            raise BuildError("TimeQuest report lacks unconstrained/illegal clock coverage")
        if any(int(setup) or int(hold) for _, setup, hold in unconstrained):
            raise BuildError("TimeQuest reports unconstrained or illegal clocks")
    except BaseException as error:
        record.update(status="fail", error=str(error))
        if not fitted.is_file():
            for stage in ("route", "place"):
                candidate = project / "output_files" / f"{PROJECT}.fit.{stage}.rpt"
                if candidate.is_file():
                    fitted = candidate
                    record["resources_stage"] = stage
                    break
        if fitted.is_file():
            try:
                record["resources"] = resources(fitted.read_text(errors="replace"))
            except (BuildError, OSError) as parse_error:
                record["resource_parse_error"] = str(parse_error)
        if (output / "timing.json").is_file():
            try:
                record["timing"] = json.loads((output / "timing.json").read_text())
            except (ValueError, OSError) as parse_error:
                record["timing_parse_error"] = str(parse_error)
        (output / "build.json").write_text(json.dumps(record, indent=2, sort_keys=True) + "\n")
        raise
    record["status"] = "pass"
    (output / "build.json").write_text(json.dumps(record, indent=2, sort_keys=True) + "\n")
    return record


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("project", "build"))
    parser.add_argument("--output", type=Path, help="new output directory; existing directories are refused")
    parser.add_argument("--quartus-root", default=os.environ.get("QUARTUS_ROOTDIR", ""))
    args = parser.parse_args(argv)
    output = None
    try:
        if args.action == "project":
            output = prepare(args.output)
            print(f"x87 internal virtual-pin project: {output}", flush=True)
        else:
            with compiler_lease():
                require_disk(args.output or MISTEROSS)
                tool, version = board_tools().locate_quartus(args.quartus_root)
                output = prepare(args.output)
                print(f"x87 internal virtual-pin build: {output}", flush=True)
                record = compile_project(output, tool, version)
                print("x87 build FINISH: 200 MHz all-corner timing PASS; "
                      + json.dumps(record["resources"], sort_keys=True), flush=True)
        return 0
    except (OSError, RuntimeError, ValueError, subprocess.SubprocessError, KeyboardInterrupt) as error:
        print(f"x87 build FAIL: {error}; retained output {output}", flush=True)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
