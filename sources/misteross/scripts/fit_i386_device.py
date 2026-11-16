#!/usr/bin/env python3
"""Fit fes_i386_cpu or pc386_system on one device, in a virtual-pin envelope.

Internal characterisation only: no board shell, no pins, no HPS, no PLL, no
reset-release IP, no programming artifact, no seal and no hardware acceptance.
It answers how much of a device the CPU and the machine take, and the clock
they close at, on the DE10-Nano's Cyclone V and on the DE25-Nano's Agilex 5.

Both devices read the same Yosys netlist. scripts/yosys_netlist.py's SEAL
recipe elaborates, flattens and writes plain Verilog without any technology
mapping, so the netlist bytes are device-neutral and each locked Quartus does
all of its own synthesis, mapping, placement and routing. Only the device and
the fitter differ between the two sides.

Cyclone V ALMs and Agilex 5 ALMs are not the same unit. This script reports
raw resources with each device's totals and never compares the two devices.

  QUARTUS_ROOTDIR=/abs/<install>/quartus python3 scripts/fit_i386_device.py \
      --design cpu --device de25 --period-ns 16.276 --seed 1 \
      --cache-root /abs/out/cache/misteross-toolchains --output /abs/new/dir

docs/validation/2026-11-16-i386-agilex5-fit/method.md is the committed method.
"""
from __future__ import annotations

import argparse
from contextlib import contextmanager
import dataclasses
import fcntl
import hashlib
import json
import math
import os
from pathlib import Path
import re
import shutil
import signal
import socket
import subprocess
import sys
import time

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from contextlib import nullcontext
import importlib.util

from scripts import yosys_netlist
from scripts.quartus_tools import load_quartus_lock, locate_quartus

ROOT = Path(__file__).resolve().parents[1]
REPO = ROOT.parents[1]
LOCK_FILE = Path("/tmp/lander-quartus-3090.lock")
MIN_FREE_BYTES = 30 * 1024 ** 3


class FitError(RuntimeError):
    pass


def de25_board():
    """boards/de25nano/build.py, which owns the Agilex 5 lock's parsing."""
    spec = importlib.util.spec_from_file_location(
        "fit_i386_de25_board", ROOT / "boards/de25nano/build.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def lock_context(device: "Device"):
    """Quartus Pro 26.3 reports the SC product marker, which the shared lock
    parser does not know; the DE25 board module widens it for the duration.
    Reused rather than copied, so one place knows that fact."""
    return de25_board().de25_quartus_tools() if device.name == "de25" else nullcontext()


@dataclasses.dataclass(frozen=True)
class Device:
    name: str
    board: str
    family: str
    part: str
    quartus_lock: str
    alms: int
    ram_block: str


DEVICES = {
    # ALM and RAM-block totals are read back from each fit report, not trusted
    # from here; these are the documented device sizes for the record's text.
    "de10": Device("de10", "DE10-Nano", "Cyclone V", "5CSEBA6U23I7",
                   "cores/fes-pc386/quartus.lock", 41_910, "M10K"),
    "de25": Device("de25", "DE25-Nano", "Agilex 5", "A5EB013BB23BE4SCS",
                   "boards/de25nano/quartus.lock", 46_800, "M20K"),
}

# Each design's top and its real clock inputs. Every other port of the top is
# a virtual pin. pc386_system's two clocks come from two separate PLLs in the
# board shell and cores/fes-pc386/clocks.sdc does not time paths between
# them, so they are declared asynchronous here too.
# port_clock names the clock each non-clock port is constrained to. The two
# machine clocks are asynchronous, so a pixel-domain port constrained to the
# system clock would have its paths cut rather than analysed, and the fit could
# report closure without ever looking at the video output.
# pc386_system.sv:66-67 puts video in pixel_clk, and pixel_locked is that
# domain's own reset input.
DESIGNS = {
    "cpu": {"top": "fes_i386_cpu", "clocks": ("clk",), "asynchronous": False,
            "port_clock": {}},
    "machine": {"top": "pc386_system", "clocks": ("clk", "pixel_clk"), "asynchronous": True,
                "port_clock": {"video[*]": "pixel_clk", "pixel_locked": "pixel_clk"}},
}
# pc386_system's pixel clock is 74.25 MHz in the shell, fixed; only the system
# clock moves with --period-ns.
PIXEL_PERIOD_NS = 1000.0 / 74.25


def sha(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def require_disk(path: Path) -> None:
    while not path.exists():
        path = path.parent
    free = shutil.disk_usage(path).free
    if free < MIN_FREE_BYTES:
        raise FitError(f"refusing to fit with {free / 1024 ** 3:.1f} GiB free; at least 30 GiB required")


@contextmanager
def compiler_lease():
    """The builder-wide Quartus lease. Never interrupt another owner's job."""
    with LOCK_FILE.open("a+") as lease:
        notice = -math.inf
        while True:
            try:
                fcntl.flock(lease, fcntl.LOCK_EX | fcntl.LOCK_NB)
                break
            except BlockingIOError:
                if time.monotonic() - notice >= 30:
                    print(f"i386 fit heartbeat: waiting for {LOCK_FILE}", flush=True)
                    notice = time.monotonic()
                time.sleep(1)
        try:
            yield
        finally:
            fcntl.flock(lease, fcntl.LOCK_UN)


_PORT = re.compile(r"^\s*(input|output|inout)\b(.*?);\s*$")
_RANGE = re.compile(r"\[\s*(\d+)\s*:\s*(\d+)\s*\]")
_DECL_KEYWORDS = frozenset({"wire", "reg", "logic", "signed", "unsigned"})


def parse_ports(text: str, top: str) -> list[tuple[str, str]]:
    """The top module's ports, each with its direction, as Quartus names them:
    a vector becomes a wildcard. Read from the netlist, so no RTL port list is
    duplicated here."""
    start = re.search(rf"^\s*module\s+{re.escape(top)}\s*\(", text, re.M)
    if start is None:
        raise FitError(f"netlist has no module {top}")
    ports: list[tuple[str, str]] = []
    for line in text[start.start():].splitlines()[1:]:
        if re.match(r"^\s*endmodule\b", line):
            break
        match = _PORT.match(line)
        if match is None:
            continue
        direction, body = match.group(1), match.group(2)
        vector = bool(_RANGE.search(body))
        # Tokenise rather than substitute: a port named arch_x87_regs contains
        # the keyword "reg", and replacing substrings would corrupt its name.
        for token in re.split(r"[,\s]+", _RANGE.sub(" ", body)):
            if not token or token in _DECL_KEYWORDS:
                continue
            if not re.fullmatch(r"[A-Za-z_][A-Za-z0-9_$]*", token):
                raise FitError(f"unreadable port declaration in {top}: {line.strip()!r}")
            ports.append((f"{token}[*]" if vector else token, direction))
    if not ports:
        raise FitError(f"{top} declares no ports in the netlist body; this reads the non-ANSI form "
                       "Yosys's write_verilog produces, where the header names the ports and the "
                       "body declares them")
    return ports


def netlist_ports(text: str, top: str) -> list[str]:
    """The top module's port names, in declaration order."""
    return [name for name, _direction in parse_ports(text, top)]


# The i386 and pc386 RTL asks for Cyclone V block RAM by name, and Agilex 5
# has no M10K: its altera_syncram refuses RAM_BLOCK_TYPE "M10K" outright, so
# synthesis stops before elaboration. The Agilex side renames exactly this
# token to the equivalent block, and nothing else.
M10K_ATTRIBUTE = 'ramstyle = "M10K"'
M20K_ATTRIBUTE = 'ramstyle = "M20K"'


def retarget_ram_attribute(text: str) -> tuple[str, int]:
    """Rename the M10K block request to M20K for an Agilex 5 fit.

    The rename is verified by round trip: turning M20K back into M10K must
    reproduce the original bytes exactly, which proves both that nothing else
    in the netlist changed and that the netlist held no M20K request already.
    MLAB requests are legal on both devices and are left alone."""
    if M20K_ATTRIBUTE in text:
        raise FitError("netlist already asks for M20K; the round-trip check could not tell "
                       "a rewritten request from an original one")
    rewritten = text.replace(M10K_ATTRIBUTE, M20K_ATTRIBUTE)
    count = text.count(M10K_ATTRIBUTE)
    if rewritten.replace(M20K_ATTRIBUTE, M10K_ATTRIBUTE) != text:
        raise FitError("the M10K to M20K rename did not round trip; it changed more than that token")
    return rewritten, count


_ATTRIBUTE = re.compile(r"^\s*\(\*\s*([A-Za-z_][A-Za-z0-9_$]*)\s*(=[^*]*)?\*\)\s*$")


def drop_duplicate_attributes(text: str) -> tuple[str, int]:
    """Remove a bare (* name *) that repeats a valued (* name = ... *) beside it.

    Yosys writes both forms on the same casez. A bare Verilog attribute means
    the value 1, so the pair says one thing twice; Quartus 17.0.2 only warns,
    but Quartus Pro 26.3 stops with Error (22074). Fork issue DeanoC/yosys#78.
    Only a line that is nothing
    but a duplicate attribute is removed, and putting the removed lines back at
    their recorded positions must reproduce the original text exactly.
    """
    lines = text.splitlines(keepends=True)
    kept: list[str] = []
    removed: list[tuple[int, str]] = []
    index = 0
    while index < len(lines):
        run = index
        names: dict[str, bool] = {}
        while run < len(lines):
            match = _ATTRIBUTE.match(lines[run])
            if match is None:
                break
            # True once this name has been seen with an explicit value.
            names[match.group(1)] = names.get(match.group(1), False) or match.group(2) is not None
            run += 1
        for position in range(index, run):
            match = _ATTRIBUTE.match(lines[position])
            if match.group(2) is None and names.get(match.group(1)):
                removed.append((position, lines[position]))
                continue
            kept.append(lines[position])
        if run == index:
            kept.append(lines[index])
            run += 1
        index = run
    rebuilt = list(kept)
    for position, line in removed:
        rebuilt.insert(position, line)
    if "".join(rebuilt) != text:
        raise FitError("dropping duplicate attributes did not round trip; it changed more than those lines")
    return "".join(kept), len(removed)


def prepare_netlist(netlist: Path, device: "Device", work: Path) -> tuple[Path, dict]:
    """The netlist this device's fitter reads, and what was done to it."""
    original = netlist.read_bytes()
    record = {"source": str(netlist), "source_sha256": sha(original)}
    if device.ram_block == "M10K":
        record["ram_attribute"] = "unchanged: the RTL already names this device's block"
        record["fitted_sha256"] = record["source_sha256"]
        return netlist, record
    rewritten, count = retarget_ram_attribute(original.decode())
    rewritten, duplicates = drop_duplicate_attributes(rewritten)
    work.mkdir(parents=True, exist_ok=True)
    retargeted = work / netlist.name
    retargeted.write_text(rewritten)
    record["ram_attribute"] = (f'renamed {count} occurrences of {M10K_ATTRIBUTE!r} to '
                               f'{M20K_ATTRIBUTE!r}; verified by round trip; nothing else changed')
    record["ram_attribute_count"] = count
    record["duplicate_attributes"] = (
        f"removed {duplicates} bare attribute lines that repeat a valued attribute beside them; "
        "Quartus Pro 26.3 stops with Error (22074) on the pair, Quartus 17.0.2 only warns; "
        "a bare Verilog attribute means the value 1, so the pair says one thing twice")
    record["duplicate_attribute_count"] = duplicates
    record["fitted_sha256"] = sha(retargeted.read_bytes())
    return retargeted, record


def clock_constraints(design: dict, period_ns: float,
                      ports: list[tuple[str, str]] | None = None) -> str:
    """The SDC: the clocks, and every virtual port path cut.

    A virtual pin is not a board pin. It has no package, no board trace and no
    external device, so there is no delay to constrain it against and no
    meaningful arrival or required time for it. Giving one a zero input and
    output delay does not make its path meaningful; it invents a hold
    requirement on a path to a pin that does not exist, and the fitter then
    pads routing delay to meet it. So every path from a virtual input and
    every path to a virtual output is cut explicitly, and what the fit
    analyses is the design's own register-to-register timing, which is what
    this measurement is about. No board I/O contract is established either
    way; the earlier zero-delay form established none either.

    The clocks themselves are real inputs and are not cut. Where a design has
    two, port_clock names which ports belong to the second one, so the record
    can say which domain each cut port came from.
    """
    clocks = design["clocks"]
    periods = {clocks[0]: period_ns}
    for clock in clocks[1:]:
        periods[clock] = PIXEL_PERIOD_NS
    lines = ["# Internal virtual-pin fit: the design's own register-to-register timing.\n",
             "# Virtual ports are cut, not constrained. No board I/O timing is established.\n"]
    for clock in clocks:
        lines.append(f"create_clock -name {clock}_clock -period {periods[clock]:.4f} [get_ports {clock}]\n")
    lines.append("derive_clock_uncertainty\n")
    if design["asynchronous"]:
        groups = " \\\n".join(f"    -group [get_clocks {{{clock}_clock}}]" for clock in clocks)
        lines.append("set_clock_groups -asynchronous \\\n" + groups + "\n")
    domains = design["port_clock"]
    if ports is not None:
        unknown = set(domains) - {name for name, _direction in ports}
        if unknown:
            raise FitError("port_clock names ports the netlist does not have: "
                           + ", ".join(sorted(unknown)))
    held = " ".join(f"[get_ports {clock}]" for clock in clocks)
    collection = f"[add_to_collection {held}]" if len(clocks) > 1 else held
    lines.append(f"set virtual_inputs [remove_from_collection [all_inputs] {collection}]\n")
    lines.append("set_false_path -from $virtual_inputs\n")
    lines.append("set_false_path -to [all_outputs]\n")
    return "".join(lines)


def write_project(output: Path, device: Device, design: dict, netlist: Path,
                  period_ns: float, seed: int, metadata: dict) -> Path:
    project = output / "project"
    project.mkdir()
    top = design["top"]
    text = netlist.read_text(errors="replace")
    shutil.copyfile(netlist, project / f"{top}.v")
    shutil.copyfile(Path(__file__), project / "fit_i386_device.py")
    shutil.copyfile(Path(__file__).with_name("fit_timing.tcl"), project / "timing.tcl")
    (project / f"{top}.qpf").write_text(
        f'QUARTUS_VERSION = "{metadata["series"]}"\nPROJECT_REVISION = "{top}"\n')
    ports = parse_ports(text, top)
    (project / f"{top}.sdc").write_text(clock_constraints(design, period_ns, ports))
    virtual = [port for port, _direction in ports if port not in design["clocks"]]
    assignments = [("FAMILY", f'"{device.family}"'), ("DEVICE", device.part),
                   ("TOP_LEVEL_ENTITY", top), ("PROJECT_OUTPUT_DIRECTORY", "output_files"),
                   ("LAST_QUARTUS_VERSION", f'"{metadata["qsf_version"]}"'),
                   ("NUM_PARALLEL_PROCESSORS", "4"), ("SEED", str(seed)),
                   ("VERILOG_FILE", f"{top}.v"), ("SDC_FILE", f"{top}.sdc")]
    qsf = ("# Internal virtual-pin characterisation of " + top + " on " + device.part
           + ". Never assembled or programmed; no board pinout is implied.\n")
    qsf += "".join(f"set_global_assignment -name {key} {value}\n" for key, value in assignments)
    # Unbraced: Quartus 17.0.2 rejects a braced node name outright (Critical
    # Warning 136021) and silently leaves the port as a real pin.
    qsf += "".join(f"set_instance_assignment -name VIRTUAL_PIN ON -to {port}\n" for port in virtual)
    (project / f"{top}.qsf").write_text(qsf)
    inputs = {
        "classification": "internal virtual-pin characterisation fit; no seal, no board I/O, "
                          "no hardware acceptance",
        "board": device.board, "device": device.part, "family": device.family,
        "documented_device_alms": device.alms, "ram_block": device.ram_block,
        "top": top, "seed": seed, "period_ns": period_ns,
        "clock_hz": round(1e9 / period_ns), "real_clocks": list(design["clocks"]),
        "pixel_period_ns": PIXEL_PERIOD_NS if len(design["clocks"]) > 1 else None,
        "virtual_ports": virtual, "virtual_port_count": len(virtual),
        "host": socket.gethostname(), "quartus": metadata,
        "netlist_sha256": sha(netlist.read_bytes()),
        "project_sha256": {path.name: sha(path.read_bytes()) for path in sorted(project.iterdir())},
    }
    (output / "inputs.json").write_text(json.dumps(inputs, indent=2, sort_keys=True) + "\n")
    return project


def run(command: list[str], project: Path, output: Path, label: str, deadline: float) -> None:
    if deadline - time.monotonic() <= 0:
        raise FitError("runtime cap expired")
    log = output / f"{label}.log"
    print(f"i386 fit: {label} started; log {log}", flush=True)
    with log.open("w") as stream:
        process = subprocess.Popen(command, cwd=project, stdout=stream, stderr=subprocess.STDOUT,
                                   start_new_session=True)
        try:
            while True:
                try:
                    status = process.wait(timeout=min(60, max(0.01, deadline - time.monotonic())))
                    break
                except subprocess.TimeoutExpired:
                    if time.monotonic() >= deadline:
                        raise FitError(f"runtime cap expired during {label}")
                    print(f"i386 fit heartbeat: {label}; pid {process.pid}; "
                          f"{int(deadline - time.monotonic())} s of the cap remain", flush=True)
        except BaseException:
            os.killpg(process.pid, signal.SIGTERM)  # our own process group, Quartus children included
            try:
                process.wait(timeout=15)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait()
            raise
    if status:
        raise FitError(f"{label} failed ({status}); retained {log}")


# One report row, within one line: a value may not run past the newline, or a
# section header would take the next row's value as its own.
_ROW = re.compile(r"^;[ \t]{0,4}(\S[^;\n]*?)[ \t]*;[ \t]*([^;\n]*?)[ \t]*;", re.M)
# The fitter's own label for each figure, in the order they are looked for.
# The record names which label a figure came from, so a renamed report row
# fails loudly instead of quietly reporting something else.
RESOURCE_LABELS = {
    "alms": ("Logic utilization (ALMs needed / total ALMs on device)",
             "Logic utilization (in ALMs)"),
    "aluts": ("Combinational ALUT usage for logic", "Total combinational ALUT usage",
              "Combinational ALUTs", "ALUTs used"),
    "registers": ("Dedicated logic registers", "Total registers",
                  "Total dedicated logic registers"),
    "dsp_blocks": ("DSP Blocks Needed [=A+B+C-D]", "Total DSP Blocks", "DSP blocks"),
    "ram_blocks": ("M20K blocks", "M20K Blocks", "Total M20K Blocks",
                   "M10K blocks", "Total RAM Blocks", "Total block memory blocks"),
    "block_memory_bits": ("Total block memory bits", "Total block memory implementation bits"),
}
# Reported when the fitter gives them, and never required. Virtual pins are not
# free: the fitter builds each one in the fabric, so part of a virtual-pin fit's
# ALMs is envelope rather than design. The count is reported beside the ALMs so
# a reader can judge that, and it is never subtracted from them.
OPTIONAL_LABELS = {
    "virtual_pins": ("Virtual pins", "Total virtual pins"),
    "mlabs": ("MLAB blocks", "Total MLAB memory bits"),
}
_FRACTION = re.compile(r"^([\d,]+(?:\.\d+)?)\s*(?:/\s*([\d,]+(?:\.\d+)?))?\s*(?:\(\s*([<>]?\s*[\d.]+)\s*%\s*\))?")


def _number(text: str) -> float | None:
    try:
        return float(text.replace(",", ""))
    except ValueError:
        return None


def resources(text: str) -> dict:
    """Every figure the fit report gives, with the label it was read from and
    the device total where the report states one."""
    rows: dict[str, str] = {}
    for label, value in _ROW.findall(text):
        rows.setdefault(label, value)
    found: dict[str, dict] = {}
    missing = []
    for resource, labels in RESOURCE_LABELS.items():
        for label in labels:
            if label not in rows:
                continue
            match = _FRACTION.match(rows[label])
            if match is None or _number(match.group(1)) is None:
                continue
            entry = {"report_label": label, "used": _number(match.group(1))}
            if match.group(2) is not None:
                entry["available"] = _number(match.group(2))
                if entry["available"]:
                    entry["percent_of_device"] = round(100.0 * entry["used"] / entry["available"], 2)
            if match.group(3) is not None:
                entry["report_percent"] = match.group(3).replace(" ", "")
            found[resource] = entry
            break
        else:
            missing.append(resource)
    if missing:
        raise FitError("fit report has no " + ", ".join(missing)
                       + "; its rows: " + ", ".join(sorted(rows)[:40]))
    for resource, labels in OPTIONAL_LABELS.items():
        for label in labels:
            match = _FRACTION.match(rows.get(label, ""))
            if match is not None and _number(match.group(1)) is not None:
                found[resource] = {"report_label": label, "used": _number(match.group(1))}
                break
    return found


def tool_names(metadata: dict) -> tuple[str, ...]:
    """Pro synthesises with quartus_syn; Quartus 17's standard flow uses quartus_map."""
    return ("quartus_syn",) if metadata["edition"] == "pro" else ("quartus_map",)


def compile_project(output: Path, project: Path, top: str, quartus_sh: Path,
                    metadata: dict, timeout_seconds: int) -> dict:
    inputs = json.loads((output / "inputs.json").read_text())
    for name, expected in inputs["project_sha256"].items():
        if sha((project / name).read_bytes()) != expected:
            raise FitError(f"prepared project source changed before the fit: {name}")
    synth = tool_names(metadata)[0]
    tools = {name: quartus_sh.with_name(name) for name in (synth, "quartus_fit", "quartus_sta")}
    for path in tools.values():
        if not path.is_file() or not os.access(path, os.X_OK):
            raise FitError(f"missing executable {path}")
    record = {"status": "fail", "classification": inputs["classification"],
              "host": socket.gethostname(),
              "device": inputs["device"], "family": inputs["family"], "top": top,
              "seed": inputs["seed"], "period_ns": inputs["period_ns"],
              "quartus": metadata, "hardware_acceptance": "not-run",
              "programming_artifact": None, "synthesis_tool": synth,
              "tool_sha256": {str(path): sha(path.read_bytes()) for path in tools.values()},
              "commands": []}
    deadline = time.monotonic() + timeout_seconds
    fit_report = project / "output_files" / f"{top}.fit.rpt"
    try:
        for label, command in (("synthesis", [str(tools[synth]), top]),
                               ("fit", [str(tools["quartus_fit"]), top]),
                               ("sta", [str(tools["quartus_sta"]), "--multicorner", top]),
                               ("timing", [str(tools["quartus_sta"]), "-t", "timing.tcl",
                                           str(output), top])):
            require_disk(output)
            record["commands"].append(command)
            run(command, project, output, label, deadline)
        fit_text = fit_report.read_text(errors="replace")
        record["resources"] = resources(fit_text)
        # The envelope is only an envelope if the virtual pins took effect.
        # Quartus 17.0.2 ignores a VIRTUAL_PIN whose node name is braced, and
        # then every port is a real pin: the fit is a different experiment, and
        # on a device with pins to spare it would fail silently.
        pins = re.search(r"^;\s*Total pins\s*;\s*([\d,]+)\s*/", fit_text, re.M)
        if pins is None:
            raise FitError("fit report has no total pin count, so the virtual-pin envelope "
                           "cannot be confirmed")
        record["real_pins"] = int(pins.group(1).replace(",", ""))
        if record["real_pins"] > len(inputs["real_clocks"]):
            raise FitError(f"{record['real_pins']} real pins against "
                           f"{len(inputs['real_clocks'])} real clocks: the VIRTUAL_PIN assignments "
                           "did not take effect, so this is not the intended envelope")
        record["timing"] = json.loads((output / "timing.json").read_text())
        sta = (project / "output_files" / f"{top}.sta.rpt").read_text(errors="replace")
        clocks = re.findall(r"^;\s*(Illegal Clocks|Unconstrained Clocks)\s*;\s*(\d+)\s*;\s*(\d+)\s*;",
                            sta, re.M)
        record["unconstrained_clock_rows"] = clocks
        if {row[0] for row in clocks} != {"Illegal Clocks", "Unconstrained Clocks"}:
            raise FitError("TimeQuest report lacks unconstrained/illegal clock coverage")
        if any(int(setup) or int(hold) for _, setup, hold in clocks):
            raise FitError("TimeQuest reports unconstrained or illegal clocks")
        if record["timing"].get("status") != "pass":
            raise FitError(f"timing did not pass at {inputs['period_ns']} ns on every corner")
    except BaseException as error:
        record["error"] = str(error)
        if not fit_report.is_file():
            for stage in ("route", "place"):
                candidate = project / "output_files" / f"{top}.fit.{stage}.rpt"
                if candidate.is_file():
                    fit_report, record["resources_stage"] = candidate, stage
                    break
        if fit_report.is_file():
            try:
                record["resources"] = resources(fit_report.read_text(errors="replace"))
            except (FitError, OSError) as parse_error:
                record["resource_parse_error"] = str(parse_error)
        if (output / "timing.json").is_file():
            try:
                record["timing"] = json.loads((output / "timing.json").read_text())
            except (ValueError, OSError) as parse_error:
                record["timing_parse_error"] = str(parse_error)
        (output / "fit.json").write_text(json.dumps(record, indent=2, sort_keys=True) + "\n")
        raise
    record["status"] = "pass"
    (output / "fit.json").write_text(json.dumps(record, indent=2, sort_keys=True) + "\n")
    return record


def cpu_netlist(output: Path, cache_root: Path, gpu_router: str) -> tuple[Path, dict]:
    yosys = yosys_netlist.authenticate(ROOT, cache_root=cache_root, gpu_router=gpu_router)
    netlist = yosys_netlist.seal_netlist(yosys.path, output / "netlist", ROOT)
    return netlist, yosys_netlist.report_record(ROOT, yosys, gpu_router, netlist)


def machine_netlist(output: Path, cache_root: Path, gpu_router: str) -> tuple[Path, dict]:
    from scripts import build_fes_pc386_quartus as sealer
    rom = REPO / "sources/misteross" / sealer.ROM_HEX
    if not rom.is_file():
        raise FitError(f"build the pinned firmware first: {rom} is missing, and a fit without the ROM "
                       "contents would let the fitter sweep the ROM and everything downstream of it "
                       "(python3 scripts/build_pc386_firmware.py)")
    yosys = yosys_netlist.authenticate(ROOT, cache_root=cache_root, gpu_router=gpu_router,
                                       lock=sealer.LOCK)
    work = output / "netlist"
    work.mkdir(parents=True, exist_ok=True)
    netlist = work / sealer.MACHINE_NETLIST
    # pc386_rom reads ROM_HEX relative to the misteross root as the seal does.
    yosys_netlist.run(yosys.path, sealer.machine_program(netlist), cwd=ROOT,
                      program_file=work / "machine.ys", log=work / "yosys.log")
    return netlist, {"program_sha256": yosys_netlist.program_sha256(yosys_netlist.SEAL),
                     "netlist_sha256": sha(netlist.read_bytes()),
                     "guest_rates": sealer.guest_rates(),
                     "rom_hex_sha256": sha(rom.read_bytes()),
                     "yosys": yosys_netlist.lock_record(ROOT, yosys, gpu_router, sealer.LOCK)}


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--design", choices=tuple(DESIGNS), required=True)
    parser.add_argument("--device", choices=tuple(DEVICES), required=True)
    parser.add_argument("--period-ns", type=float, default=1000.0 / 61.44,
                        help="the system clock's period; the default is the pc386 seal's 61.44 MHz")
    parser.add_argument("--seed", type=int, default=1)
    parser.add_argument("--cache-root", type=Path,
                        default=os.environ.get("FES_TOOLCHAIN_CACHE_ROOT") or None)
    parser.add_argument("--gpu-router", choices=yosys_netlist.GPU_ROUTERS, default="HIP",
                        help="the toolchain slot whose Yosys writes the netlist; nothing runs on a GPU")
    parser.add_argument("--output", type=Path, required=True, help="a new directory")
    parser.add_argument("--netlist", type=Path, help="reuse this netlist instead of writing one")
    parser.add_argument("--timeout-minutes", type=int, default=90)
    parser.add_argument("--project-only", action="store_true", help="prepare, do not compile")
    args = parser.parse_args(argv)
    output = args.output.resolve()
    try:
        if args.cache_root is None and args.netlist is None:
            raise FitError("--cache-root must be absolute (or set FES_TOOLCHAIN_CACHE_ROOT)")
        if args.cache_root is not None and not args.cache_root.is_absolute():
            raise FitError("--cache-root must be absolute")
        if not 1 <= args.seed <= 2 ** 31 - 1:
            raise FitError("seed must be a positive integer")
        device, design = DEVICES[args.device], DESIGNS[args.design]
        with lock_context(device):
            lock = load_quartus_lock(ROOT, device.quartus_lock)
            metadata = {"series": lock.series, "qsf_version": lock.qsf_version, "release": lock.release,
                        "edition": lock.edition_name.lower(), "lock": device.quartus_lock}
        output.mkdir(parents=True, exist_ok=False)
        if args.netlist is not None:
            netlist, provenance = args.netlist.resolve(), {"reused": str(args.netlist)}
        else:
            writer = cpu_netlist if args.design == "cpu" else machine_netlist
            netlist, provenance = writer(output, args.cache_root, args.gpu_router)
        netlist, retarget = prepare_netlist(netlist, device, output / "fitted-netlist")
        provenance["fitted_netlist"] = retarget
        (output / "netlist.json").write_text(json.dumps(provenance, indent=2, sort_keys=True) + "\n")
        project = write_project(output, device, design, netlist, args.period_ns, args.seed, metadata)
        print(f"i386 fit project: {project}", flush=True)
        if args.project_only:
            return 0
        with compiler_lease():
            require_disk(output)
            with lock_context(device):
                _root, quartus_sh, version = locate_quartus(
                    os.environ.get("QUARTUS_ROOTDIR", ""), ROOT, lock)
            metadata["version_line"] = version
            record = compile_project(output, project, design["top"], quartus_sh, metadata,
                                     args.timeout_minutes * 60)
        print(f"i386 fit FINISH {design['top']} on {device.part}: "
              + json.dumps(record["resources"], sort_keys=True), flush=True)
        print("fmax: " + json.dumps(record["timing"].get("fmax", {}), sort_keys=True), flush=True)
        return 0
    except (OSError, RuntimeError, ValueError, subprocess.SubprocessError, KeyboardInterrupt) as error:
        print(f"i386 fit FAIL: {error}; retained output {output}", flush=True)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
