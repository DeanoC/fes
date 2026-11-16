#!/usr/bin/env python3
"""Fit fes_i386_cpu or pc386_system on one device, in a virtual-pin envelope.

Internal characterisation only: no board shell, no pins, no HPS, no PLL, no
reset-release IP, no programming artifact, no seal and no hardware acceptance.
It answers how much of a device the CPU and the machine take, and the clock
they close at, on the DE10-Nano's Cyclone V and on the DE25-Nano's Agilex 5.

Both devices read the same Yosys netlist, with two exceptions on the Agilex
side. scripts/yosys_netlist.py's SEAL recipe elaborates, flattens and writes
plain Verilog without any technology mapping, so the netlist is structurally
device-neutral and each locked Quartus does all of its own synthesis, mapping,
placement and routing. But the RTL asks for Cyclone V block RAM by name, and
Agilex 5 has no M10K, so the Agilex side renames that attribute to M20K; and
Quartus Pro 26.3 stops where Yosys writes a bare and a valued parallel_case on
one casez (DeanoC/yosys#78), so the Agilex side drops the repeated bare line.
Both rewrites are mechanical, verified by round trip, counted in each fit
record, and applied to the Agilex side only. The DE10 side reads the netlist
exactly as the pc386 seal lane does. So the two sides differ in device, in
fitter, and in those two attribute rewrites -- and in nothing else.

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
import threading
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
# A fixed path for the machine elaboration, so its netlist digest does not
# depend on which output directory a fit happens to use (DeanoC/yosys#80).
MACHINE_NETLIST_WORK = "build/diagnostic/i386-agilex5/machine-elaboration/pc386_system.v"
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
# Every port but the clocks is virtual and every virtual port path is cut, so
# no port needs a domain: which clock a cut port would have belonged to does
# not change the fit. pc386_system's video output is in pixel_clk
# (pc386_system.sv:66-67) and is cut like the rest; the pixel domain's own
# register-to-register paths are analysed as the system domain's are.
DESIGNS = {
    "cpu": {"top": "fes_i386_cpu", "clocks": ("clk",), "asynchronous": False,
            "asynchronous_resets": False},
    "machine": {"top": "pc386_system", "clocks": ("clk", "pixel_clk"), "asynchronous": True,
                "asynchronous_resets": True},
}
# The method's runtime caps, which a fit reported as "not measured within the
# cap" must actually have been given.
DEFAULT_TIMEOUT_MINUTES = {"cpu": 90, "machine": 240}
# Designs known to have asynchronous resets, where recovery and removal must
# have paths: finding none would mean those resets were swept or
# mis-elaborated, and the gate must say so instead of recording the checks as
# not applicable. fes_i386_cpu's resets are all synchronous.
ASYNCHRONOUS_RESETS = {"machine": True, "cpu": False}
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
def elaboration_lease():
    """The fixed machine-elaboration workspace, held across writing it and
    copying it out. Two concurrent machine fits from one worktree write the
    same netlist, script and log there, and without this one could copy a file
    the other is replacing, or unlink it before the other has copied it."""
    LOCK_FILE.parent.mkdir(parents=True, exist_ok=True)
    path = ROOT / MACHINE_NETLIST_WORK
    path.parent.mkdir(parents=True, exist_ok=True)
    guard = path.with_name("elaboration.lock")
    with guard.open("a+") as lease:
        notice = -math.inf
        while True:
            try:
                fcntl.flock(lease, fcntl.LOCK_EX | fcntl.LOCK_NB)
                break
            except BlockingIOError:
                if time.monotonic() - notice >= 30:
                    print(f"i386 fit heartbeat: waiting for {guard}", flush=True)
                    notice = time.monotonic()
                time.sleep(1)
        try:
            yield path
        finally:
            fcntl.flock(lease, fcntl.LOCK_UN)


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
    in_function = False
    for line in text[start.start():].splitlines()[1:]:
        if re.match(r"^\s*endmodule\b", line):
            break
        # Yosys writes the flattened module's $pmux helpers as Verilog
        # functions, and a function declares its own inputs. Those are not
        # ports: counting them put ~1,950 names that are not nodes into the
        # machine's virtual-pin list and made its port count meaningless.
        if re.match(r"^\s*function\b", line):
            in_function = True
            continue
        if re.match(r"^\s*endfunction\b", line):
            in_function = False
            continue
        if in_function:
            continue
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


def clock_constraints(design: dict, period_ns: float) -> str:
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

    The clocks themselves are real inputs and are not cut.
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
    held = " ".join(f"[get_ports {clock}]" for clock in clocks)
    collection = f"[add_to_collection {held}]" if len(clocks) > 1 else held
    lines.append(f"set virtual_inputs [remove_from_collection [all_inputs] {collection}]\n")
    lines.append("set_false_path -from $virtual_inputs\n")
    lines.append("set_false_path -to [all_outputs]\n")
    return "".join(lines)


def fes_revision() -> dict:
    """The revision the fit was prepared from, recorded in the fit rather than
    read when the record is collected: a checkout that moves between the two
    would otherwise attribute a stale fit to whatever is checked out later.

    A commit alone is not the source that ran. `git rev-parse HEAD` returns the
    clean commit even when tracked files are edited -- and the M+ fit is made
    by editing one, deliberately. So the tracked diff is recorded with it, and
    a fit from a dirty checkout says so and carries the diff that made it.
    """
    try:
        head = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=REPO, text=True).strip()
    except (OSError, subprocess.SubprocessError):
        return {"commit": "unknown", "clean": None}
    try:
        diff = subprocess.check_output(["git", "diff", "HEAD"], cwd=REPO, text=True)
    except (OSError, subprocess.SubprocessError):
        return {"commit": head, "clean": None}
    record = {"commit": head, "clean": not diff.strip()}
    if diff.strip():
        record["tracked_diff_sha256"] = sha(diff.encode())
        record["tracked_diff"] = diff
        record["note"] = ("prepared from a modified checkout; this commit alone does not describe "
                          "the source that ran, and the diff above does")
    return record


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
    (project / f"{top}.sdc").write_text(clock_constraints(design, period_ns))
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
        "asynchronous_resets": design["asynchronous_resets"],
        "host": socket.gethostname(), "fes_revision": fes_revision(), "quartus": metadata,
        "netlist_sha256": sha(netlist.read_bytes()),
        "project_sha256": {path.name: sha(path.read_bytes()) for path in sorted(project.iterdir())},
    }
    (output / "inputs.json").write_text(json.dumps(inputs, indent=2, sort_keys=True) + "\n")
    return project


class FitTerminated(BaseException):
    """A termination signal, turned into an exception so cleanup can unwind.

    Python's default action for SIGTERM and SIGHUP exits the interpreter
    without raising, so no `finally` and no `except` runs. A Quartus stage is
    started with start_new_session=True, which is what lets this runner kill
    the whole compiler process group deliberately -- and also means the child
    group never receives the signal the runner got. Without this, a job
    timeout or a disconnected session kills the runner, the builder lease dies
    with it, and Quartus keeps compiling: the next fit takes the lock and
    overlaps an orphaned compile, which surfaces as inexplicable figures
    rather than as an error. It derives from BaseException so that an
    `except Exception` anywhere in a stage cannot swallow a termination.
    """


@contextmanager
def terminating_signals():
    """SIGTERM and SIGHUP raise FitTerminated for the duration.

    One definition for the whole run rather than a handler per stage: the
    lease, the heartbeat threads and the compiler group all unwind through the
    same path a Ctrl-C already takes.
    """
    def relay(number, frame):
        raise FitTerminated(f"terminated by {signal.Signals(number).name}")

    previous = {}
    for number in TERMINATING_SIGNALS:
        try:
            previous[number] = signal.signal(number, relay)
        except (ValueError, OSError):
            # Not the main thread, or a platform without the signal: the
            # default action stands and this is no worse than before.
            pass
    try:
        yield
    finally:
        for number, handler in previous.items():
            try:
                signal.signal(number, handler)
            except (ValueError, OSError):
                pass


TERMINATING_SIGNALS = (signal.SIGTERM, signal.SIGHUP)


def start_stage(command: list[str], **popen):
    """Start a stage with the terminating signals blocked, and hand back the
    process together with the call that lets them through again.

    Blocking is what makes the launch window not exist: a signal raised while
    blocked stays pending rather than being delivered, so the caller releases
    it inside the region that owns the pid and can kill the process group. A
    narrower guard would still have a window, and this family of bug has been
    found twice in this repository by narrowing one.

    On a platform without `pthread_sigmask`, or off the main thread, the launch
    is unprotected as it was before: that is the previous behaviour and not a
    new failure, and the caller is told nothing it would act on differently.
    """
    try:
        blocked = signal.pthread_sigmask(signal.SIG_BLOCK, TERMINATING_SIGNALS)
    except (AttributeError, ValueError, OSError):
        return subprocess.Popen(command, **popen), lambda: None

    # A child inherits its launching thread's mask across fork and exec, so
    # blocking for the parent's sake would otherwise hand Quartus a mask in
    # which SIGTERM is blocked: the cleanup's polite SIGTERM would be ignored,
    # every termination would wait out the 15 s grace and end in SIGKILL mid
    # compile. The mask is therefore reset in the child as part of creating it
    # -- between fork and exec, where nothing has run yet -- rather than
    # afterwards, which is too late by definition.
    #
    # preexec_fn runs in the forked child, where only async-signal-safe work
    # is sound: this is one `pthread_sigmask` call and nothing else. It rests
    # on the runner being single-threaded at launch, which it is -- stages are
    # started from the main thread and the elaboration heartbeat thread is
    # stopped and joined before any compiler stage begins. That assumption is
    # stated in the log rather than assumed silently if it ever stops holding.
    if threading.active_count() > 1:
        print("i386 fit: launching a stage with "
              f"{threading.active_count()} threads alive; the child's signal-mask reset "
              "runs after fork and is only safe single-threaded", flush=True)

    def unblock_in_child():
        signal.pthread_sigmask(signal.SIG_UNBLOCK, TERMINATING_SIGNALS)

    popen = dict(popen, preexec_fn=unblock_in_child)
    released = False

    def deliver():
        # Idempotent: the caller releases on the happy path, and the finally
        # below releases if the launch itself failed.
        nonlocal released
        if not released:
            released = True
            signal.pthread_sigmask(signal.SIG_SETMASK, blocked)

    try:
        process = subprocess.Popen(command, **popen)
    except BaseException:
        deliver()
        raise
    return process, deliver


def run(command: list[str], project: Path, output: Path, label: str, deadline: float,
        env: dict[str, str] | None = None) -> None:
    if deadline - time.monotonic() <= 0:
        raise FitError("runtime cap expired")
    log = output / f"{label}.log"
    print(f"i386 fit: {label} started; log {log}", flush=True)
    with log.open("w") as stream:
        # A guard cannot be entered before the child exists, so there is no
        # ordering of `Popen` and `try` that has no window: the previous
        # version left the instructions between them, and a signal landing
        # there raised outside the cleanup and orphaned the group exactly as
        # before. The window is removed rather than shortened by making the
        # signals undeliverable across the launch -- blocked signals stay
        # pending -- and releasing them only once the cleanup owns the pid.
        # `deliver_blocked_signals` is therefore called INSIDE the try, and is
        # where a signal from the launch window is raised.
        process, deliver_blocked_signals = start_stage(
            command, cwd=project, stdout=stream, stderr=subprocess.STDOUT,
            start_new_session=True, env=dict(os.environ, **(env or {})))
        try:
            deliver_blocked_signals()
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
# Leading indent varies: the packing breakdown rows are indented five or more
# spaces inside the table, so the allowance is generous rather than fixed at
# four, which silently skipped them.
_ROW = re.compile(r"^;[ \t]*(\S[^;\n]*?)[ \t]*;[ \t]*([^;\n]*?)[ \t]*;", re.M)
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
    # The packing breakdown the ALMs-needed row is computed from (A - B + C).
    # The record's claims about how sparsely a design is spread rest on these,
    # so they belong in the record rather than only in the retained report.
    "alms_in_final_placement": ("[A] ALMs used in final placement [=a+b+c+d]",
                                "ALMs used in final placement [=a+b+c+d]",
                                "ALMs used in final placement"),
    "alms_recoverable_by_packing": ("[B] Estimate of ALMs recoverable by dense packing",
                                    "Estimate of ALMs recoverable by dense packing"),
    "alms_unavailable": ("[C] Estimate of ALMs unavailable [=a+b+c+d]",
                         "Estimate of ALMs unavailable [=a+b+c+d]",
                         "Estimate of ALMs unavailable"),
    # Not a number; reported as the fitter's own word.
    "packing_difficulty": ("Difficulty packing design",),
    "virtual_pins": ("Total virtual pins", "Virtual pins"),
    # The fitter's own count of how much of the ALM figure is envelope. It is
    # reported beside the ALMs and never subtracted from them.
    "alms_with_virtual_pins": ("* ALMs containing virtual pins", "ALMs containing virtual pins"),
    # Both fitters report this row, under the same label. It is NOT a capacity
    # figure: a LAB holds 10 ALMs on both devices and counts as used when one
    # ALM in it is used, so this tracks how sparsely the fitter spread the
    # design. The capacity figure is ALMs needed, above. #1889's refusal
    # reports LABs *required* before placement, which is a third quantity and
    # appears in no report here.
    "labs": ("Total LABs:  partially or completely used",
             "Total LABs: partially or completely used"),
    # The LAB row's own breakdown. The record compares these between fits --
    # two designs 4,132 ALMs apart reporting the same 4,580 logic and 10
    # memory LABs is a claim that needs its rows kept.
    "logic_labs": ("-- Logic LABs",),
    "memory_labs": ("-- Memory LABs (up to half of total LABs)",),
    "mlabs": ("MLAB blocks", "Total MLAB memory bits"),
}
_FRACTION = re.compile(r"^([\d,]+(?:\.\d+)?)\s*(?:/\s*([\d,]+(?:\.\d+)?))?\s*(?:\(\s*([<>]?\s*[\d.]+)\s*%\s*\))?")


def _number(text: str) -> float | None:
    try:
        return float(text.replace(",", ""))
    except ValueError:
        return None


def _entry(label: str, value: str) -> dict | None:
    """One figure: the count, and the device total and percentage where the
    report row states a denominator. Quartus gives no denominator for ALUTs or
    dedicated logic registers, so those are counts alone and no percentage is
    invented for them here."""
    match = _FRACTION.match(value)
    if match is None or _number(match.group(1)) is None:
        return None
    entry = {"report_label": label, "used": _number(match.group(1))}
    if match.group(2) is not None:
        entry["available"] = _number(match.group(2))
        if entry["available"]:
            entry["percent_of_device"] = round(100.0 * entry["used"] / entry["available"], 2)
    if match.group(3) is not None:
        entry["report_percent"] = match.group(3).replace(" ", "")
    return entry


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
            entry = _entry(label, rows[label])
            if entry is None:
                continue
            found[resource] = entry
            break
        else:
            missing.append(resource)
    if missing:
        raise FitError("fit report has no " + ", ".join(missing)
                       + "; its rows: " + ", ".join(sorted(rows)[:40]))
    for resource, labels in OPTIONAL_LABELS.items():
        for label in labels:
            if label not in rows:
                continue
            entry = _entry(label, rows[label])
            if entry is None:
                # A row whose value is a word rather than a count, such as the
                # fitter's packing difficulty. Keep it verbatim.
                entry = {"report_label": label, "value": rows[label]}
            found[resource] = entry
            break
    return found


def tool_names(metadata: dict) -> tuple[str, ...]:
    """Pro synthesises with quartus_syn; Quartus 17's standard flow uses quartus_map."""
    return ("quartus_syn",) if metadata["edition"] == "pro" else ("quartus_map",)


def check_envelope(record: dict, fit_text: str, inputs: dict) -> None:
    """The envelope is only an envelope if the virtual pins took effect.

    Quartus 17.0.2 ignores a VIRTUAL_PIN whose node name is braced, and then
    every port is a real pin: the fit is a different experiment, and on a
    device with pins to spare it would fail silently. This runs before the
    timing gate and again on the failure path, so a fit whose timing missed --
    a ladder rung above the closure -- still has its envelope verified before
    its area is quoted beside the rungs that passed.
    """
    if "real_pins" in record:
        return
    pins = re.search(r"^;\s*Total pins\s*;\s*([\d,]+)\s*/", fit_text, re.M)
    if pins is None:
        raise FitError("fit report has no total pin count, so the virtual-pin envelope "
                       "cannot be confirmed")
    record["real_pins"] = int(pins.group(1).replace(",", ""))
    # Exactly the intended count, not at most: fewer real pins than real clocks
    # means a clock input went virtual or was removed, and the timing gate only
    # requires at least one clock -- so the surviving clock's clean coverage
    # rows would let a fit be recorded with the other clock never analysed.
    if record["real_pins"] != len(inputs["real_clocks"]):
        raise FitError(f"{record['real_pins']} real pins against "
                       f"{len(inputs['real_clocks'])} real clocks: the VIRTUAL_PIN assignments "
                       "did not take effect as intended, so this is not the intended envelope")


def check_clock_coverage(record: dict, project: Path, top: str) -> None:
    """Every clock constrained and none illegal, from TimeQuest's own report.

    This runs on the failure path too. A missed ladder rung is what bounds the
    closure from above, so "this rung misses" has to mean the analysis covered
    every clock -- otherwise the bound could come from a clock TimeQuest never
    looked at.
    """
    if "unconstrained_clock_rows" in record:
        return
    report = project / "output_files" / f"{top}.sta.rpt"
    if not report.is_file():
        raise FitError("no TimeQuest report, so clock coverage cannot be confirmed")
    sta = report.read_text(errors="replace")
    clocks = re.findall(r"^;\s*(Illegal Clocks|Unconstrained Clocks)\s*;\s*(\d+)\s*;\s*(\d+)\s*;",
                        sta, re.M)
    record["unconstrained_clock_rows"] = clocks
    if {row[0] for row in clocks} != {"Illegal Clocks", "Unconstrained Clocks"}:
        raise FitError("TimeQuest report lacks unconstrained/illegal clock coverage")
    if any(int(setup) or int(hold) for _, setup, hold in clocks):
        raise FitError("TimeQuest reports unconstrained or illegal clocks")


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
            # The gate checks the clock count against the SDC's own clocks.
            run(command, project, output, label, deadline,
                env={"FES_FIT_CLOCKS": " ".join(inputs["real_clocks"]),
                     "FES_FIT_ASYNC_RESET": "1" if inputs["asynchronous_resets"] else "0"})
        fit_text = fit_report.read_text(errors="replace")
        record["resources"] = resources(fit_text)
        check_envelope(record, fit_text, inputs)
        record["timing"] = json.loads((output / "timing.json").read_text())
        check_clock_coverage(record, project, top)
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
                failed_text = fit_report.read_text(errors="replace")
                record["resources"] = resources(failed_text)
                # A missed rung's area is quoted beside the rungs that passed,
                # so its envelope is verified here too.
                check_envelope(record, failed_text, inputs)
            except (FitError, OSError) as parse_error:
                record["resource_parse_error"] = str(parse_error)
        if (output / "timing.json").is_file():
            try:
                record["timing"] = json.loads((output / "timing.json").read_text())
                # A missed rung bounds the closure from above, so its analysis
                # must have covered every clock for the bound to mean anything.
                check_clock_coverage(record, project, top)
            except (FitError, ValueError, OSError) as parse_error:
                record["timing_parse_error"] = str(parse_error)
        (output / "fit.json").write_text(json.dumps(record, indent=2, sort_keys=True) + "\n")
        raise
    record["status"] = "pass"
    (output / "fit.json").write_text(json.dumps(record, indent=2, sort_keys=True) + "\n")
    return record


def cpu_sources() -> list[Path]:
    """fes_i386_cpu's SystemVerilog, in the read order the seal uses."""
    return [ROOT / path for path in yosys_netlist.CPU_SOURCES]


def write_sv_project(output: Path, device: "Device", design: dict, period_ns: float,
                     seed: int, metadata: dict) -> Path:
    """The secondary front end: Quartus Pro reads the SystemVerilog itself.

    This exists as a cross-check on the primary netlist flow, not as a second
    area result. The method requires its register count to be compared against
    the primary on the same device, and a large gap withdraws it rather than
    reporting it, because a gap means swept or mis-elaborated logic. The
    locked Quartus 17.0.2 front end rejects this RTL outright, so this flow
    can only ever exist on the Pro side.
    """
    if design["top"] != "fes_i386_cpu":
        raise FitError("the direct SystemVerilog flow is defined for fes_i386_cpu only")
    project = output / "project"
    project.mkdir()
    top = design["top"]
    sources = cpu_sources()
    # The same M10K-to-M20K rename the primary flow applies to the netlist,
    # applied here to the RTL instead, and verified the same way. Without it
    # Quartus Pro refuses outright: fes_i386_l1i, fes_i386_l1d and
    # fes_i386_btb carry 30 Cyclone V block-RAM requests between them, and
    # Agilex 5 has no M10K. Doing it keeps the secondary a cross-check on the
    # front end rather than a second experiment.
    renamed = 0
    for path in sources:
        text = path.read_text()
        if device.ram_block != "M10K":
            text, count = retarget_ram_attribute(text)
            renamed += count
        (project / path.name).write_text(text)
    shutil.copyfile(Path(__file__).with_name("fit_timing.tcl"), project / "timing.tcl")
    (project / f"{top}.sdc").write_text(clock_constraints(design, period_ns))
    (project / f"{top}.qpf").write_text(
        f'QUARTUS_VERSION = "{metadata["series"]}"\nPROJECT_REVISION = "{top}"\n')
    # The RTL declares its ports in the module header, with multi-dimensional
    # packed types the netlist flow has already flattened away, so the
    # netlist's port reader does not apply and the exact pin names Quartus
    # will invent for them are not known here. Every pin is made virtual with
    # a wildcard and the clock turned back, which needs no port list at all;
    # the fit's pin gate then proves it worked, since a design whose
    # VIRTUAL_PIN assignments did not take effect reports more real pins than
    # real clocks and fails.
    virtual = ["* (wildcard; the clock is assigned back)"]
    assignments = [("FAMILY", f'"{device.family}"'), ("DEVICE", device.part),
                   ("TOP_LEVEL_ENTITY", top), ("PROJECT_OUTPUT_DIRECTORY", "output_files"),
                   ("LAST_QUARTUS_VERSION", f'"{metadata["qsf_version"]}"'),
                   ("NUM_PARALLEL_PROCESSORS", "4"), ("SEED", str(seed)),
                   ("SDC_FILE", f"{top}.sdc")]
    qsf = ("# Secondary front end: Quartus reads the SystemVerilog directly. A\n"
           "# cross-check on the netlist flow, not a second area result.\n")
    qsf += "".join(f"set_global_assignment -name {key} {value}\n" for key, value in assignments)
    qsf += "".join(f"set_global_assignment -name SYSTEMVERILOG_FILE {path.name}\n"
                   for path in sources)
    qsf += "set_instance_assignment -name VIRTUAL_PIN ON -to *\n"
    for clock in design["clocks"]:
        qsf += f"set_instance_assignment -name VIRTUAL_PIN OFF -to {clock}\n"
    (project / f"{top}.qsf").write_text(qsf)
    inputs = {
        "classification": "internal virtual-pin characterisation fit, SECONDARY direct-SystemVerilog "
                          "front end; a cross-check on the netlist flow, not a second area result; "
                          "no seal, no board I/O, no hardware acceptance",
        "front_end": "quartus-systemverilog",
        "board": device.board, "device": device.part, "family": device.family,
        "documented_device_alms": device.alms, "ram_block": device.ram_block,
        "top": top, "seed": seed, "period_ns": period_ns,
        "clock_hz": round(1e9 / period_ns), "real_clocks": list(design["clocks"]),
        "pixel_period_ns": None,
        "virtual_ports": virtual, "virtual_port_count": len(virtual),
        "asynchronous_resets": design["asynchronous_resets"],
        "host": socket.gethostname(), "fes_revision": fes_revision(), "quartus": metadata,
        "netlist_sha256": None,
        "ram_attribute": (f'renamed {renamed} occurrences of {M10K_ATTRIBUTE!r} to '
                          f'{M20K_ATTRIBUTE!r} across the RTL sources, as the primary flow does to '
                          'the netlist; verified by round trip; nothing else changed'),
        "ram_attribute_count": renamed,
        "source_sha256": {path.name: sha(path.read_bytes()) for path in sources},
        "fitted_source_sha256": {path.name: sha((project / path.name).read_bytes())
                                 for path in sources},
        "project_sha256": {path.name: sha(path.read_bytes()) for path in sorted(project.iterdir())},
    }
    (output / "inputs.json").write_text(json.dumps(inputs, indent=2, sort_keys=True) + "\n")
    return project


@contextmanager
def elaboration_heartbeat(label: str):
    """Print while Yosys runs. yosys_netlist.run() blocks with no output of its
    own and the Quartus deadline does not exist yet, so a slow or hung
    elaboration would otherwise be indistinguishable from a wedged runner."""
    stop = threading.Event()
    started = time.monotonic()

    def beat() -> None:
        while not stop.wait(30):
            print(f"i386 fit heartbeat: {label}; {int(time.monotonic() - started)} s elapsed",
                  flush=True)

    thread = threading.Thread(target=beat, daemon=True)
    thread.start()
    try:
        yield
    finally:
        stop.set()
        thread.join(timeout=5)


def cpu_netlist(output: Path, cache_root: Path, gpu_router: str) -> tuple[Path, dict]:
    yosys = yosys_netlist.authenticate(ROOT, cache_root=cache_root, gpu_router=gpu_router)
    with elaboration_heartbeat("elaborating fes_i386_cpu"):
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
    # The machine's Yosys program carries its output path, and Yosys's
    # auto-generated _NNNNN_ names change with it: four elaborations of
    # byte-identical sources at four output paths gave four digests, all with
    # the same size, statement counts and autoname-reference counts and
    # differing only in which numbers those names carry. So the netlist is
    # written at a FIXED path and copied to the fit's own directory, which
    # makes its digest reproducible. Fork issue: DeanoC/yosys#80.
    # pc386_rom reads ROM_HEX relative to the misteross root as the seal does,
    # so Yosys still runs there.
    netlist = work / sealer.MACHINE_NETLIST
    # Every path Yosys is given is fixed, not just the netlist's: the program
    # file and the log carry the output directory too, and the auto-names
    # follow any of them. The workspace is held while it is written and copied.
    with elaboration_lease() as stable, elaboration_heartbeat("elaborating pc386_system"):
        yosys_netlist.run(yosys.path, sealer.machine_program(stable), cwd=ROOT,
                          program_file=stable.with_name("machine.ys"),
                          log=stable.with_name("yosys.log"))
        for name in (sealer.MACHINE_NETLIST, "machine.ys", "yosys.log"):
            source = stable.with_name(name)
            if source.is_file():
                shutil.copyfile(source, work / name)
                source.unlink()
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
    parser.add_argument("--netlist-source", metavar="REVISION",
                        help="the revision the reused netlist was elaborated from, when that is "
                             "not this checkout. A reused netlist's digest identifies its bytes "
                             "and says nothing about their source, and the fitting checkout's own "
                             "HEAD is not it -- so without this the record cannot attribute the "
                             "fit to any source and says so rather than guessing. Pass 'self' "
                             "when the netlist was elaborated from this checkout.")
    parser.add_argument("--netlist-source-note", metavar="TEXT",
                        help="anything a reader needs about that source: a pre-merge head, a "
                             "scratch edit, a different branch")
    parser.add_argument("--timeout-minutes", type=int,
                        help="runtime cap; the method's default per design, 90 minutes for the CPU "
                             "and 240 for the machine. A fit that hits its cap is reported as not "
                             "measured within the cap, so the cap must be the promised one.")
    parser.add_argument("--project-only", action="store_true", help="prepare, do not compile")
    parser.add_argument("--front-end", choices=("netlist", "systemverilog"), default="netlist",
                        help="netlist is the primary flow (the Yosys SEAL netlist, the same bytes "
                             "on both devices bar two recorded attribute rewrites); systemverilog "
                             "is the labelled secondary cross-check, Quartus reading the RTL "
                             "itself, and is defined for fes_i386_cpu on the Pro side only")
    args = parser.parse_args(argv)
    output = args.output.resolve()
    try:
        # The direct-SystemVerilog flow needs no Yosys, so it needs no cache.
        if args.cache_root is None and args.netlist is None and args.front_end != "systemverilog":
            raise FitError("--cache-root must be absolute (or set FES_TOOLCHAIN_CACHE_ROOT)")
        if args.cache_root is not None and not args.cache_root.is_absolute():
            raise FitError("--cache-root must be absolute")
        if not 1 <= args.seed <= 2 ** 31 - 1:
            raise FitError("seed must be a positive integer")
        device, design = DEVICES[args.device], DESIGNS[args.design]
        timeout_minutes = args.timeout_minutes or DEFAULT_TIMEOUT_MINUTES[args.design]
        with lock_context(device):
            lock = load_quartus_lock(ROOT, device.quartus_lock)
            metadata = {"series": lock.series, "qsf_version": lock.qsf_version, "release": lock.release,
                        "edition": lock.edition_name.lower(), "lock": device.quartus_lock}
        output.mkdir(parents=True, exist_ok=False)
        if args.front_end == "systemverilog":
            if metadata["edition"] != "pro":
                raise FitError("the locked Quartus 17.0.2 front end rejects this RTL, so the "
                               "direct SystemVerilog flow exists on the Pro side only")
            project = write_sv_project(output, device, design, args.period_ns, args.seed, metadata)
            # The rewrite provenance belongs where the collector reads it, not
            # only in inputs.json: it is the evidence for what was changed.
            sv_inputs = json.loads((output / "inputs.json").read_text())
            (output / "netlist.json").write_text(json.dumps(
                {"front_end": "quartus-systemverilog",
                 "note": "no Yosys netlist; Quartus read the RTL itself",
                 "ram_attribute": sv_inputs["ram_attribute"],
                 "ram_attribute_count": sv_inputs["ram_attribute_count"],
                 "source_sha256": sv_inputs["source_sha256"],
                 "fitted_source_sha256": sv_inputs["fitted_source_sha256"]},
                indent=2, sort_keys=True) + "\n")
            print(f"i386 fit project (direct SystemVerilog): {project}", flush=True)
            if args.project_only:
                return 0
            with compiler_lease():
                require_disk(output)
                with lock_context(device):
                    _root, quartus_sh, version = locate_quartus(
                        os.environ.get("QUARTUS_ROOTDIR", ""), ROOT, lock)
                metadata["version_line"] = version
                record = compile_project(output, project, design["top"], quartus_sh, metadata,
                                         timeout_minutes * 60)
            print(f"i386 fit FINISH (direct SystemVerilog) {design['top']} on {device.part}: "
                  + json.dumps(record["resources"], sort_keys=True), flush=True)
            return 0
        if args.netlist is not None:
            netlist = args.netlist.resolve()
            provenance = {"reused": str(args.netlist)}
            given = args.netlist_source
            if given == "self":
                revision = fes_revision()
                provenance["source_revision"] = revision
                # "this checkout" names a source only when the checkout can
                # name itself. On a tree copied without a resolvable .git the
                # commit is "unknown", and the unqualified sentence would read
                # as provenance where there is none; the collector refuses such
                # a fit, and the note must not contradict it.
                stated = ("declared as elaborated from this checkout"
                          if revision.get("commit") != "unknown" else
                          "declared as elaborated from this checkout, but the checkout could "
                          "not name its own revision (`git rev-parse` failed, as a copied tree "
                          "with no resolvable .git gives), so no source revision is stated")
                provenance["source_revision_note"] = stated + (
                        f"; {args.netlist_source_note}" if args.netlist_source_note else "")
            elif given:
                provenance["source_revision"] = given
                provenance["source_revision_note"] = (
                    args.netlist_source_note
                    or "declared by --netlist-source; not this checkout's HEAD")
            else:
                provenance["source_revision"] = None
                provenance["source_revision_note"] = (
                    "NOT STATED. The caller passed --netlist without --netlist-source, so this "
                    "fit's netlist is identified only by its digest and its source revision is "
                    "unknown. It must not be attributed to the checkout that ran the fitter, "
                    "which is recorded separately as fes_revision.")
        else:
            # Before the elaboration, not only before the compile: a netlist of
            # the machine is tens of megabytes and the Yosys job should be
            # refused rather than left to fail part-written.
            require_disk(output)
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
                                     timeout_minutes * 60)
        print(f"i386 fit FINISH {design['top']} on {device.part}: "
              + json.dumps(record["resources"], sort_keys=True), flush=True)
        print("fmax: " + json.dumps(record["timing"].get("fmax", {}), sort_keys=True), flush=True)
        return 0
    except (OSError, RuntimeError, ValueError, subprocess.SubprocessError,
            KeyboardInterrupt, FitTerminated) as error:
        print(f"i386 fit FAIL: {error}; retained output {output}", flush=True)
        return 1


if __name__ == "__main__":
    # The handlers are installed around main rather than inside it so that a
    # signal arriving before the first stage, while the lease is being taken,
    # unwinds the same way as one arriving during a compile.
    with terminating_signals():
        raise SystemExit(main())
