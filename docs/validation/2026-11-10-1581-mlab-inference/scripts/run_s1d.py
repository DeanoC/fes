#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Spike S1d (#1581): build each return-stack variant in two Quartus 17.0.2 flows.

usage: run_s1d.py REPO_ROOT WORK_DIR [BUILD ...]

  flow "yosys":  the pc386 lock's Yosys writes a netlist with the pc386
                 producer's own script (build_fes_pc386_quartus.machine_program
                 and plain_identifiers; only the top and its parameters differ),
                 and Quartus reads that netlist.
  flow "direct": Quartus reads the SystemVerilog.

Environment: CACHE_ROOT (the authenticated toolchain cache) and
QUARTUS_ROOTDIR (the 17.0.2 quartus directory). Builds run one at a time with
two Quartus processors. Each tool is time-capped. The trimmed reports go to
../reports/<build>/ and the summary to ../reports/summary.json.
"""
from __future__ import annotations

import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import time

HERE = Path(__file__).resolve().parent.parent
VARIANTS = ("v1_current", "v2_async", "v3_regread", "v3b_regread_bypass")
FLOWS = ("yosys", "direct")
TOP = "s1d_harness"
SEED = 1
PROCESSORS = 2
TOOL_TIMEOUT_S = 900
MESSAGE_RE = re.compile(r"^\s*(?:Info|Warning|Critical Warning) \((276\d{3}|19000|276014|276013)\):.*$", re.M)


def section(report: str, title: str) -> str:
    """One "; Title ;" table of a Quartus report, with its frame."""
    match = re.search(r"^\+-+\+\n; " + re.escape(title) + r"\s*;\n(?:[+;].*\n)+", report, re.M)
    return match.group(0) if match else ""


def table_rows(text: str) -> list[list[str]]:
    return [[cell.strip() for cell in line.strip().strip(";").split(";")]
            for line in text.splitlines() if line.startswith(";")]


def ram_type(ram_summary: str, usage: str) -> str:
    """Where the stack went: an MLAB, an M10K (a RAM the fitter placed as
    AUTO), or no RAM at all (registers)."""
    if "MLAB" in ram_summary:
        return "MLAB"
    m10k = next((row[1] for row in table_rows(usage) if row[0] == "M10K blocks"), "0 /")
    return "M10K" if ram_summary and not m10k.startswith("0 ") else "none (FFs)"


def run(command: list[str], cwd: Path, log: Path, env: dict[str, str]) -> None:
    started = time.time()
    with log.open("ab") as handle:
        handle.write(("$ " + " ".join(command) + "\n").encode())
        handle.flush()
        result = subprocess.run(command, cwd=cwd, env=env, stdout=handle, stderr=subprocess.STDOUT,
                                timeout=TOOL_TIMEOUT_S, check=False)
    print(f"  {Path(command[0]).name}: exit {result.returncode}, {time.time() - started:.0f} s", flush=True)
    if result.returncode != 0:
        raise SystemExit(f"{command[0]} failed (exit {result.returncode}); log: {log}")


def qsf(device: str, sources: list[tuple[str, str]]) -> str:
    # The pc386 producer's project settings (project_qsf) that apply to a
    # block with no shell: family, device, seed, language version. Two
    # processors, not ALL, because the host is shared.
    lines = [
        'set_global_assignment -name FAMILY "Cyclone V"',
        f"set_global_assignment -name DEVICE {device}",
        f"set_global_assignment -name TOP_LEVEL_ENTITY {TOP}",
        'set_global_assignment -name PROJECT_OUTPUT_DIRECTORY "output_files"',
        f"set_global_assignment -name NUM_PARALLEL_PROCESSORS {PROCESSORS}",
        f"set_global_assignment -name SEED {SEED}",
        "set_global_assignment -name VERILOG_INPUT_VERSION SYSTEMVERILOG_2005",
        'set_global_assignment -name LAST_QUARTUS_VERSION "17.0.2 Lite Edition"',
        "set_global_assignment -name SDC_FILE s1d.sdc",
    ]
    lines += [f"set_global_assignment -name {kind} {name}" for kind, name in sources]
    return "\n".join(lines) + "\n"


def results(build: str, project: Path, out: Path) -> dict:
    reports = project / "output_files"
    map_rpt = (reports / f"{TOP}.map.rpt").read_text(errors="replace")
    fit_rpt = (reports / f"{TOP}.fit.rpt").read_text(errors="replace")
    sta_rpt = (reports / f"{TOP}.sta.rpt").read_text(errors="replace")

    messages = sorted(set(match.group(0).strip() for match in MESSAGE_RE.finditer(map_rpt)))
    ram_summary = section(map_rpt, "Analysis & Synthesis RAM Summary")
    fit_ram = section(fit_rpt, "Fitter RAM Summary")
    usage = section(fit_rpt, "Fitter Resource Usage Summary")
    by_entity = section(fit_rpt, "Fitter Resource Utilization by Entity")
    packed = section(map_rpt, "Registers Packed Into Inferred Megafunctions")
    # The usage table without its rows of unused hard blocks.
    keep, usage_lines = True, []
    for line in usage.splitlines():
        if line.startswith("; Virtual pins") or line.startswith("; Total DSP Blocks"):
            keep = False
        elif line.startswith("; M10K blocks") or line.startswith("+-"):
            keep = True
        if keep:
            usage_lines.append(line)
    usage_trimmed = "\n".join(usage_lines) + "\n"

    def usage_value(label: str) -> str:
        return next((row[1] for row in table_rows(usage) if row[0].lstrip("- ") == label), "")

    fit_summary = (reports / f"{TOP}.fit.summary").read_text()

    def summary_value(label: str) -> str:
        match = re.search(r"^" + re.escape(label) + r" : (.*)$", fit_summary, re.M)
        return match.group(1).strip() if match else ""

    fmax = {}
    for corner in ("Slow 1100mV 100C Model", "Slow 1100mV -40C Model"):
        rows = [row for row in table_rows(section(sta_rpt, f"{corner} Fmax Summary")) if row and row[0].endswith("MHz")]
        if len(rows) != 1:
            raise SystemExit(f"{build}: expected one Fmax row at {corner}")
        fmax[corner] = {"fmax_mhz": float(rows[0][0].split()[0]), "restricted_fmax_mhz": float(rows[0][1].split()[0])}
    slack = {}
    for line in (reports / f"{TOP}.sta.summary").read_text().splitlines():
        if line.startswith("Type  : "):
            kind = line[8:]
        elif line.startswith("Slack : "):
            slack[kind] = float(line[8:])
    unconstrained = section(sta_rpt, "Unconstrained Paths Summary")

    mlab_cells = [row for row in table_rows(by_entity)[1:] if row]
    record = {
        "build": build,
        "mlab_inferred": "MLAB" in ram_summary,
        "ram_type": ram_type(ram_summary, usage),
        "ram_messages": messages,
        "alms": summary_value("Logic utilization (in ALMs)"),
        "alms_needed": int(summary_value("Logic utilization (in ALMs)").split("/")[0].replace(",", "")),
        "registers": int(summary_value("Total registers").replace(",", "")),
        "block_memory_bits": summary_value("Total block memory bits"),
        "mlab_memory_bits": usage_value("Total MLAB memory bits"),
        "memory_aluts": usage_value("Memory ALUT usage"),
        "memory_alms": usage_value("[d] ALMs used for memory (up to half of total ALMs)"),
        "fmax": fmax,
        "worst_slack_ns": slack,
        "timing_met": all(value >= 0 for value in slack.values()),
        "entities": [[row[0], row[1], row[2]] for row in mlab_cells[:4]],
    }

    out.mkdir(parents=True, exist_ok=True)
    (out / "map-ram.txt").write_text(
        "RAM inference messages (Analysis & Synthesis):\n" + ("\n".join(messages) or "(none)") + "\n\n"
        + (ram_summary or "No Analysis & Synthesis RAM Summary: no RAM was inferred.\n") + "\n" + (packed or "No registers were packed into an inferred megafunction.\n"))
    (out / "fit-extract.txt").write_text(fit_summary + "\n" + usage_trimmed + "\n" + by_entity + "\n" + fit_ram)
    fmax_tables = "".join(section(sta_rpt, f"{corner} Fmax Summary") + "\n" for corner in fmax)
    (out / "sta-extract.txt").write_text((reports / f"{TOP}.sta.summary").read_text() + "\n" + fmax_tables + unconstrained)
    shutil.copy(project / f"{TOP}.qsf", out / f"{TOP}.qsf")
    for name in ("s1d_harness.yosys.v", "yosys.log", "yosys-program.txt"):
        if (project / name).exists():
            shutil.copy(project / name, out / name)
    (out / "result.json").write_text(json.dumps(record, indent=2) + "\n")
    return record


def main() -> None:
    root, work = Path(sys.argv[1]).resolve(), Path(sys.argv[2]).resolve()
    wanted = sys.argv[3:] or [f"{flow}-{variant}" for flow in FLOWS for variant in VARIANTS]
    misteross = root / "sources/misteross"
    sys.path.insert(0, str(misteross))
    from scripts import build_fes_pc386_quartus as producer
    from scripts import fes_build_common as board

    tools = producer._authenticate_tools(misteross, cache_root=Path(os.environ["CACHE_ROOT"]))
    quartus_bin = Path(tools["quartus_sh"]).parent
    env = os.environ.copy()
    env["QUARTUS_ROOTDIR"] = str(tools["quartus_root"])
    versions = {
        "quartus": tools["quartus_version"],
        "quartus_sh": tools["identities"]["quartus_sh"],
        "yosys": tools["identities"]["yosys"],
        "yosys_version": subprocess.run([str(tools["yosys"].path), "--version"], capture_output=True, text=True).stdout.strip(),
        "device": board.TARGET,
        "seed": SEED,
        "processors": PROCESSORS,
    }
    print(json.dumps(versions, indent=2), flush=True)

    summary = {"versions": versions, "builds": []}
    for build in wanted:
        flow, variant = build.split("-", 1)
        print(f"{build}: {time.strftime('%H:%M:%S')}", flush=True)
        project = work / build
        if project.exists():
            shutil.rmtree(project)
        project.mkdir(parents=True)
        log = project / "build.log"
        shutil.copy(HERE / "rtl/s1d.sdc", project / "s1d.sdc")
        rtl = [HERE / "rtl/s1d_harness.sv", HERE / f"rtl/ras_{variant}.sv"]
        if flow == "yosys":
            raw = project / "raw.v"
            program = producer.machine_program(raw, " ".join(str(path) for path in rtl))
            # The producer's script with the spike's top in place of the
            # machine's top and guest-rate parameters; every pass is as sealed.
            head, tail = program.split("hierarchy -top ", 1)
            program = head + f"hierarchy -top {TOP}; " + tail.split("; ", 1)[1]
            assert "memory_map t:$mem_v2 r:WR_PORTS>1 %i a:ramstyle %d" in program and "flatten" in program
            (project / "yosys-program.txt").write_text(program + "\n")
            run([str(tools["yosys"].path), "-q", "-l", str(project / "yosys.log"), "-p", program], misteross, log, env)
            (project / "s1d_harness.yosys.v").write_text(producer.plain_identifiers(raw.read_text()))
            raw.unlink()
            sources = [("VERILOG_FILE", "s1d_harness.yosys.v")]
        else:
            for path in rtl:
                shutil.copy(path, project / path.name)
            sources = [("SYSTEMVERILOG_FILE", path.name) for path in rtl]
        (project / f"{TOP}.qsf").write_text(qsf(board.TARGET, sources))
        for tool in ("quartus_map", "quartus_fit", "quartus_sta"):
            run([str(quartus_bin / tool), TOP], project, log, env)
        record = results(build, project, HERE / "reports" / build)
        summary["builds"].append(record)
        print(f"  MLAB {record['mlab_inferred']}, ALMs {record['alms']}, registers {record['registers']}, "
              f"Fmax {record['fmax']}", flush=True)
    if not sys.argv[3:]:
        (HERE / "reports/summary.json").write_text(json.dumps(summary, indent=2) + "\n")


if __name__ == "__main__":
    main()
