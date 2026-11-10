#!/usr/bin/env python3
"""S1b: Quartus Pro synthesis (quartus_syn) of the probe cases for Agilex 5.

  python3 run_pro.py --quartus-root /data/altera_pro/26.3/quartus \\
      --source <sources/misteross> --netlists <netlists.py output> --work ~/fes-scratch/s1b/run

Each case gets its own project directory, a time cap and a summary: exit status, elapsed
time, every Error / Critical Warning / Warning class (message ID, count, first text) and
the synthesis resource rows. Nothing is fitted, assembled or programmed.
"""
import argparse
import json
from pathlib import Path
import re
import shutil
import subprocess
import time

DEVICE = "A5EB013BB23BE4SCS"  # DE25-Nano, sources/misteross/boards/de25nano/build.py
OPL2 = "cores/fes-pc386/vendor/opl2_fpga"
OPL2_TREE = (
    f"{OPL2}/opl2_pkg.sv",
    *(f"{OPL2}/{name}.sv" for name in (
        "pipeline_sr", "mem_simple_dual_port", "mem_simple_dual_port_async_read", "mem_single_bank",
        "mem_single_bank_reset", "opl2_log_sine_lut", "opl2_exp_lut", "ksl_add_rom", "tremolo", "vibrato",
        "calc_phase_inc", "calc_envelope_shift", "calc_rhythm_phase", "envelope_generator", "phase_generator",
        "operator", "control_operators", "dac_prep", "channels")),
)
OPL2_MACHINE = (*OPL2_TREE, "cores/fes-pc386/rtl/pc386_rate.sv", "cores/fes-pc386/rtl/pc386_opl2.sv")
# name: (top, "netlist" or "native", files, parameters)
CASES = {
    "yosys-opl2": ("pc386_opl2", "netlist", ("pc386_opl2.v",), {}),
    "yosys-opl2-escaped": ("pc386_opl2", "netlist", ("pc386_opl2.raw.v",), {}),
    "yosys-opl2-fixed": ("pc386_opl2", "netlist", ("pc386_opl2.fixed.v",), {}),
    "yosys-opl2-fixed-escaped": ("pc386_opl2", "netlist", ("pc386_opl2.fixed.raw.v",), {}),
    "yosys-menu-m20k": ("fes_menu_reader", "netlist", ("fes_menu_reader.v",), {}),
    "yosys-menu-m10k": ("fes_menu_reader", "netlist", ("fes_menu_reader.m10k.v",), {}),
    "native-opl2": ("pc386_opl2", "native", OPL2_MACHINE, {}),
    "native-operator": ("operator", "native", OPL2_TREE, {}),
    "native-menu-m10k": ("fes_menu_reader", "native", ("cores/fes-menu/rtl/fes_menu_reader.v",),
                         {"WINDOW_BASE": "32'h30000000"}),
}
MESSAGE = re.compile(r"^\s*(Error|Critical Warning|Warning) ?(?:\((\d+)\))?: (.*)$")
RESOURCE = re.compile(r"^; (Total ALMs[^;]*|Total block memory bits|Total DSP Blocks|Total registers|"
                      r"Combinational ALUT usage for logic|Dedicated logic registers|Total MLAB memory bits|"
                      r"Total RAM Blocks|Estimate of Logic utilization[^;]*|Family|Device|Top-level Entity Name) *; *([^;]*?) *;")


def summarise(log_text, reports):
    classes = {}
    for line in log_text.splitlines():
        match = MESSAGE.match(line)
        if match:
            key = f"{match.group(1)} ({match.group(2) or 'no id'})"
            entry = classes.setdefault(key, {"count": 0, "first": match.group(3)[:300]})
            entry["count"] += 1
    resources = {}
    for report in reports:
        for line in report.read_text(errors="replace").splitlines():
            match = RESOURCE.match(line)
            if match:
                resources.setdefault(match.group(1).strip(), match.group(2))
    return classes, resources


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--quartus-root", required=True, type=Path)
    parser.add_argument("--source", required=True, type=Path)
    parser.add_argument("--netlists", required=True, type=Path)
    parser.add_argument("--work", required=True, type=Path)
    parser.add_argument("--timeout", type=int, default=1800, help="seconds per case")
    parser.add_argument("--case", action="append", choices=sorted(CASES))
    args = parser.parse_args()
    quartus_syn = args.quartus_root / "bin/quartus_syn"
    args.work.mkdir(parents=True, exist_ok=True)
    free = shutil.disk_usage(args.work).free / 2**30
    if free < 30:
        raise SystemExit(f"{free:.0f}G free under {args.work}; Quartus jobs need 30G")
    version = subprocess.check_output([str(quartus_syn), "--version"], text=True).strip()
    m20k = (args.netlists / "fes_menu_reader.v").read_text()
    if m20k.count('ramstyle = "M20K"') != 1:
        raise SystemExit("the menu reader netlist has no M20K hint")
    (args.netlists / "fes_menu_reader.m10k.v").write_text(m20k.replace('"M20K"', '"M10K"'))
    results = {"quartus_syn": version, "device": DEVICE, "cases": {}}
    for name in args.case or CASES:
        top, kind, files, parameters = CASES[name]
        project = args.work / name
        if project.exists():
            shutil.rmtree(project)
        project.mkdir()
        lines = [
            'set_global_assignment -name FAMILY "Agilex 5"',
            f"set_global_assignment -name DEVICE {DEVICE}",
            f"set_global_assignment -name TOP_LEVEL_ENTITY {top}",
            "set_global_assignment -name PROJECT_OUTPUT_DIRECTORY output_files",
            "set_global_assignment -name NUM_PARALLEL_PROCESSORS 4",
        ]
        for parameter, value in parameters.items():
            lines.append(f'set_parameter -name {parameter} "{value}"')
        for file in files:
            source = (args.netlists if kind == "netlist" else args.source) / file
            shutil.copyfile(source, project / source.name)
            assignment = "SYSTEMVERILOG_FILE" if source.suffix == ".sv" else "VERILOG_FILE"
            lines.append(f"set_global_assignment -name {assignment} {source.name}")
        (project / f"{top}.qpf").write_text(f'PROJECT_REVISION = "{top}"\n')
        (project / f"{top}.qsf").write_text("\n".join(lines) + "\n")
        start = time.monotonic()
        print(f"[{time.strftime('%H:%M:%S')}] {name}: quartus_syn {top}", flush=True)
        with (project / "quartus_syn.log").open("w") as log:
            try:
                status = subprocess.run([str(quartus_syn), top], cwd=project, stdout=log,
                                        stderr=subprocess.STDOUT, timeout=args.timeout).returncode
            except subprocess.TimeoutExpired:
                status = "timeout"
        elapsed = round(time.monotonic() - start)
        classes, resources = summarise((project / "quartus_syn.log").read_text(errors="replace"),
                                       sorted(project.glob("output_files/*.syn.rpt"))
                                       + sorted(project.glob("output_files/*.syn.summary")))
        results["cases"][name] = {"top": top, "front_end": kind, "files": [Path(f).name for f in files],
                                  "parameters": parameters, "exit": status, "seconds": elapsed,
                                  "messages": classes, "resources": resources}
        print(f"[{time.strftime('%H:%M:%S')}] {name}: exit {status} in {elapsed}s; "
              f"{sum(v['count'] for k, v in classes.items() if k.startswith('Error'))} errors", flush=True)
        (args.work / "results.json").write_text(json.dumps(results, indent=2) + "\n")
    print("done", flush=True)


if __name__ == "__main__":
    main()
