#!/usr/bin/env python3
"""Integrate the standard pc386 into the managed DE25 GHRD (see ../machine/board.py).

The machine is the DE10's pc386_system with its display plane derived out
(derive.py), compiled as the DE10 seal compiles it: the pc386 lock's Yosys
SEAL netlist, here at the DE25 machine clock's guest rates. Its memory is a
window in HPS DDR (fes.fabric.de25-hps-window): fes_de25_hps_window's guards,
rebase and bounds, then fes_de25_window_arbiter's one master. Two models
(Deano, 25 Nov 2026: "Both, per-model clocks"), each its own elaboration and
machine clock (model_clocks.py):
- 486sx: fes_i386_cpu as a 486SX (#1962: CPUID, no VME) with no 387: the
  elaboration sets pc386_system's I486 parameter to 1, which the DE10's 386
  seal leaves at 0, and pc386_system ties x87_present to 0, as on the DE10.
- 486dx: the DE25's i486DX (#2135; i486dx/derive.py): fes_i486dx_cpu, the
  integer core's DE25-only derivation with both L1s at 8 KiB 2-way and no
  load forwarding, with the 486 identity and the 387 on (X87_PRESENT = 1).
The parameters are Yosys top parameters and the DX's files are outside the
DE10's CPU closure, so no shared source changes and the DE10 netlist does not
move.
"""
from fractions import Fraction
from pathlib import Path
import importlib.util
import math
import os
import subprocess
import sys
import fcntl
import json
import shutil

HERE = Path(__file__).resolve().parent
BOARD = "sources/misteross/boards/de25nano/"
MISTEROSS = "sources/misteross/"
NAME = "pc386"
DIRECTORY = "pc386"
CORE_ID = "fes.pc386"
COMPONENT = "fes_de25_pc386_standard"
SDC = BOARD + "pc386/pc386-clocks.sdc"
DERIVE = BOARD + "pc386/derive.py"
MODELS = {"486sx": None, "486dx": None}
# pc386_system's CPU identity parameter (fes_i386_cpu I486): the 486, both models.
I486 = 1
# The 486DX model: pc386_system's CPU is i486dx/'s, with the 387 on.
DX_MODEL = "486dx"
# The board: its own machine PLL on CLOCK1_50. Each model has its own machine
# clock (pc386/model_clocks.py: C0 on the one VCO, the model's PLL IP and the
# reviewed Qsys parent digest its clock generates); model_clock() gives it to
# the board (machine/board.py reads a spec's model_clock for the model it
# builds). No board SDRAM; a window.
BOARD_SDRAM = False
WINDOW = True
# pc386_system's presenter and pixel-reset synchronizers (pc386_presenter.sv,
# pc386_machine.sv) carry only async_reg in the Yosys netlist. Name them
# synchronizers on the board, as the board's own chains name themselves, so
# Quartus Pro's design assistant sees each chain. Each stage 0 drives only
# stage 1; pc386-clocks.sdc names the launch into each stage 0.
ASSIGNMENTS = tuple(("SYNCHRONIZER_IDENTIFICATION", "FORCED_IF_ASYNCHRONOUS", "system|" + name) for name in (
    "machine_presenter_sequence_sync[*]", "machine_presenter_request_sync[*]",
    "machine_presenter_loaded_sync[*]", "machine_pixel_reset_sync[*]"))
# Qsys writes this peripheral subsystem for the component. Reviewed against the
# Spectrum's (a08edf0b...): no SDRAM conduit; the window's master, control
# slave and bridge (fes_win_cdc) added; one more reset controller, which
# synchronizes the system reset into the machine clock for the component's
# machine_reset and the bridge's machine side (rst_controller_001), so the
# others are renumbered by one; the window's machine-clock interconnect has its
# own (rst_controller_005). The shared FIFO reset derivation checks its bridge
# clock and reset wiring before rewriting it.
# The parent's reviewed digest is per model (model_clocks.py): at every clock
# the parent is this file with only that interconnect's module name changed.
LOCAL_FILES = ("build.py", "derive.py", "signoff_bases.py", "model_clocks.py", "pc386-clocks.sdc", "fes_de25_pc386_standard.v",
               "fes_de25_pc386_standard_hw.tcl", "fes_de25_hps_window.v", "fes_de25_hps_window_control.v",
               "fes_de25_window_arbiter.v", "fes_de25_window_unpack.v", "generated/fes_de25_hps_window.vh",
               "pc386_dma2_registers.sv")
REVIEW_FILES = ("retimed_tension.py", "retimed_tension.tcl", "retimed_structure.tcl")
SHARED_FILES = ("fes_computer_mailbox.v",)


def load(path, name):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


class RegisteredBoard:
    """Add pc386's model-specific check to the registered compile stage."""

    def __init__(self, delegate):
        self.delegate = delegate

    def __getattr__(self, name):
        return getattr(self.delegate, name)

    def compile_board(self, spec, output, scanout, hps, display, tool):
        from boards.de25nano.scanout import machine_findings

        directory = output / "combined/hps"
        record = output.parent / "build-inputs.json"
        try:
            model = json.loads(record.read_bytes())["parameters"]["model"]
            prepared_model = json.loads((directory / "inputs.json").read_bytes())[self.delegate.RECORD]["model"]
        except (OSError, ValueError, KeyError, TypeError) as error:
            raise machine_findings.BuildError("DE25 pc386 registered fit lacks a recorded or prepared model") from error
        if not isinstance(model, str) or model not in MODELS:
            raise machine_findings.BuildError(f"DE25 pc386 registered fit has unknown model {model!r}")
        if model != prepared_model:
            raise machine_findings.BuildError(
                f"DE25 pc386 recorded model {model!r} differs from prepared model {prepared_model!r}")

        with quartus_lease("pro", label="DE25 pc386 registered fit"):
            artifact = self.delegate.compile_board(spec, output, scanout, hps, display, tool)
            basis = load(HERE / "signoff_bases.py", "de25_pc386_signoff_bases")
            drc = (directory / "project/output_files/golden_top.tq.drc.signoff.rpt").read_text()
            if model == DX_MODEL:
                proof = load(HERE / "retimed_tension.py", "de25_pc386_retimed_tension")
                subjects = proof.reviewed_targets(drc, basis.CPU_CONTROL_TENSION, basis.RETIMED_CONTROL_TENSION)
                if subjects:
                    proof.collect(output, tool.with_name("quartus_sta"), subjects)
            else:
                machine_findings.review(drc, "", (basis.CPU_CONTROL_TENSION,))
            return artifact


sys.path.insert(0, str(HERE.parents[2]))
from scripts import pc386_quartus as sealer  # noqa: E402
from scripts import yosys_netlist  # noqa: E402
from scripts import fit_i386_device as fit  # noqa: E402
from scripts.quartus_tools import quartus_lease  # noqa: E402

board = RegisteredBoard(load(HERE.parent / "machine/board.py", "de25_pc386_machine_board"))
dx = load(HERE / "i486dx/derive.py", "de25_pc386_i486dx_derive")
CLOCKS = load(HERE / "model_clocks.py", "de25_pc386_model_clocks")
VCO = Fraction(CLOCKS.VCO_NUMERATOR, CLOCKS.VCO_DENOMINATOR)


def model_clock(model=None):
    """One model's machine clock, as machine/board.py's clocks() and the parent check take it: its
    rate (hz, and the exact clock), PLL IP and parameters, SDRAM clocks (none), the board top's PLL
    comment and the reviewed parent digest; and the guest-rate AUDIO ratio its clock gives."""
    model = model or next(iter(MODELS))
    if model not in MODELS or model not in CLOCKS.MODEL_CLOCKS:
        raise ValueError("pc386 has no model " + str(model))
    entry = CLOCKS.MODEL_CLOCKS[model]
    c0 = entry["c0"]
    clock = VCO / c0
    mhz = f"{float(clock / 1_000_000):.6f}"
    return {"model": model, "c0": c0, "clock": clock, "hz": round(clock), "mhz": mhz,
            "pll_ip": BOARD + "pc386/" + entry["pll_ip"],
            "pll_parameters": {"gui_number_of_clocks": "2", "gui_output_clock_frequency0": mhz,
                               "gui_output_clock_frequency1": f"{float(VCO / CLOCKS.AUDIO_C / 1_000_000):.6f}"},
            "sdram_clocks": (),
            "pll_comment": ("// One PLL on CLOCK1_50 in place of the stock audio PLL, whose pin can feed no\n"
                            f"// second one: x 188/3, then /{c0} for the {model}'s {mhz} MHz machine clock and\n"
                            f"// /{CLOCKS.AUDIO_C} for the 12.287582 MHz audio master clock (-34 ppm).\n"),
            "parent_sha256": entry["parent_sha256"],
            # The mixer's frame request against the I2S's rate: the audio master / 256 over the
            # machine clock, (VCO / 255 / 256) / (VCO / C0).
            "audio": Fraction(c0, CLOCKS.AUDIO_C * 256)}

DX_FILES = ("derive.py", "fes_i486dx_l1i.sv", "fes_i486dx_l1d.sv")
SOURCES = ({BOARD + "pc386/" + name for name in (*LOCAL_FILES, *REVIEW_FILES)} |
           {BOARD + "pc386/i486dx/" + name for name in DX_FILES} |
           {BOARD + "spectrum/derive.py", BOARD + "zx81-hps/fes_de25_computer.v",
            "sources/misteross/cores/fes-common/rtl/fes_hps_ddr_guard.v"} |
           {MISTEROSS + path for path in sealer.MACHINE_SOURCES + sealer.INCLUDES} |
           {MISTEROSS + path for path in (sealer.FIRMWARE_LOCK, sealer.LOCK, "scripts/yosys_netlist.py",
                                          "scripts/pc386_quartus.py", "scripts/fit_i386_device.py")})


def guest_rates(model=None):
    """pc386_system's N/D pairs at the DE25 machine clock, exact rationals of it.

    AUDIO is the one fixed ratio: the I2S runs at the audio clock / 256, so
    the mixer's frame request is that rate over the machine clock (C0 / 65280,
    1/1280 at C0 = 51) rather than 48 kHz of the true clock, which would drift
    against the I2S.
    """
    clock = model_clock(model)
    parameters = {}
    for name, rate in sealer.GUEST_RATES.items():
        ratio = clock["audio"] if name == "AUDIO" else rate / clock["clock"]
        if ratio.numerator >= 2 ** 31 or ratio.denominator >= 2 ** 31:
            raise ValueError(f"{name} does not fit pc386_system's integer parameters")
        parameters[f"{name}_N"], parameters[f"{name}_D"] = ratio.numerator, ratio.denominator
    parameters["ATA_RESET_CLOCKS"] = math.ceil(sealer.ATA_RESET_SECONDS * clock["clock"])
    return parameters


def selected(model=None):
    """The model a call builds: the first (486sx) when none is named. An unknown model is refused, so a
    model is never built as another."""
    model = model or next(iter(MODELS))
    if model not in MODELS:
        raise ValueError("pc386 has no model " + str(model))
    return model


def machine_parameters(model=None):
    """pc386_system's top parameters: the guest rates, the 486 identity, and for the 486DX the 387."""
    model = selected(model)
    return {**guest_rates(model), "I486": I486, **({"X87_PRESENT": 1} if model == DX_MODEL else {})}


# The pc386 lock's Yosys, once authenticated. A producer authenticates it
# before its controlled environment (which carries no toolchain cache) and the
# elaboration uses exactly that tool; board.main authenticates it from the
# caller's environment.
FRONT_END = None


def authenticate_front_end(*, cache_root=None, gpu_router=None):
    """Authenticate the pc386 lock's Yosys from the slot gpu_router selects; nothing routes on a GPU."""
    global FRONT_END
    cache = cache_root or os.environ.get("FES_TOOLCHAIN_CACHE_ROOT")
    if not cache:
        raise ValueError("the pc386 lock's toolchain cache is required (--cache-root or FES_TOOLCHAIN_CACHE_ROOT)")
    router = gpu_router or os.environ.get("FES_GPU_ROUTER", "HIP")
    tool = yosys_netlist.authenticate(board.ROOT / MISTEROSS, cache_root=Path(cache), gpu_router=router,
                                      lock=sealer.LOCK)
    if FRONT_END is not None and FRONT_END.identity != tool.identity:
        raise ValueError("the pc386 lock's Yosys changed during the build")
    FRONT_END = tool
    return tool


def record_parameters(root, model=None):
    """A producer record's machine parameters, from the source module at root: guest rates, the
    linked firmware and the CPU and x87 identity. The 486DX also records its presenter re-arm interval."""
    root, model = Path(root), selected(model)
    parameters = {"front_end": "yosys-seal-netlist", "guest_rates": json.dumps(guest_rates(model), sort_keys=True),
                  "i486": I486,
                  "firmware": json.dumps({**sealer.firmware_digests(root), "rom_hex": sealer.pinned_rom_digest(root)},
                                         sort_keys=True),
                  "x87_present": False, "signoff_findings_review_policy": "de25-pc386-reviewed-signoff-findings-v1"}
    if model == DX_MODEL:
        derive = load(root / DERIVE.removeprefix(MISTEROSS), "de25_pc386_record_derive")
        parameters.update(cpu=dx.TOP, x87_present=True, loaded_rearm_cycles=derive.LOADED_REARM_CYCLES)
    return parameters


def machine_netlist(sources, derived_files, model=None, cpu=None):
    """The derived pc386_system as the pc386 lock's Yosys elaborates it (SEAL).

    It runs in the checkout, whose captured sources it requires to be exactly
    these bytes, with the ROM hex firmware.lock pins.
    """
    root = board.ROOT / MISTEROSS
    for path, data in sources.items():
        if path.startswith(MISTEROSS) and (board.ROOT / path).read_bytes() != data:
            raise ValueError("the checkout is not the captured source: " + path)
    if (cpu is not None) != (selected(model) == DX_MODEL):
        raise ValueError("the 486DX, and only it, elaborates the i486DX's CPU")
    sealer.check_rom(root)
    yosys = FRONT_END or authenticate_front_end()
    # Yosys's auto-generated names follow every path it is given (the derived
    # sources, the program, the log, the netlist; DeanoC/yosys#80), so it runs
    # at one fixed path, held under a lock: a revision always elaborates to the
    # same bytes, which an RBF reproduction and the envelope fits need.
    work = root / "build/de25-pc386/elaboration"
    work.parent.mkdir(parents=True, exist_ok=True)
    with open(work.parent / "elaboration.lock", "w") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        shutil.rmtree(work, ignore_errors=True)
        work.mkdir()
        swap = {}
        for name, data in derived_files.items():
            (work / name).write_bytes(data)
            swap["cores/fes-pc386/rtl/" + name] = str(work / name)
        # A derived file that is not a DE10 source (pc386_dma2_registers.sv) is added to them.
        added = tuple(str(work / name) for name in derived_files
                      if "cores/fes-pc386/rtl/" + name not in sealer.MACHINE_SOURCES)
        if cpu is not None:
            # The 486DX: its derived CPU (written under fes_i386_cpu.sv's name) and the forks in place of
            # the integer core's files.
            (work / Path(dx.CPU_UPSTREAM).name).write_bytes(cpu)
            replaced = dx.cpu_sources(yosys_netlist.CPU_SOURCES, str(work / Path(dx.CPU_UPSTREAM).name))
            swap.update({old: new for old, new in zip(yosys_netlist.CPU_SOURCES, replaced) if old != new})
        machine = tuple(swap.get(path, path) for path in sealer.MACHINE_SOURCES) + added
        netlist = work / "pc386_system.v"
        program = yosys_netlist.program(yosys_netlist.SEAL, top=sealer.MACHINE_TOP, sources=machine,
                                        includes=sealer.INCLUDE_DIRS, parameters=machine_parameters(model),
                                        netlist=netlist.as_posix())
        yosys_netlist.run(yosys.path, program, cwd=root, program_file=work / "machine.ys", log=work / "yosys.log")
        log = (work / "yosys.log").read_text(errors="replace")
        if "implicitly declared" in log:
            raise ValueError("the seal's Yosys reported implicit declarations (DeanoC/yosys#79)")
        data = netlist.read_bytes()
    # As every Agilex 5 fit of this netlist prepares it (fit_i386_device): M20K
    # where the RTL names Cyclone V's M10K, and no bare attribute repeated beside
    # its valued form, which Quartus Pro 26.3 refuses (Error 22074).
    text, renamed = fit.retarget_ram_attribute(data.decode())
    text, duplicates = fit.drop_duplicate_attributes(text)
    return text.encode(), {"m10k_to_m20k": renamed, "duplicate_attributes_dropped": duplicates}


def elaboration(sources, model=None):
    """A model's derived machine files (keyed by name, as machine_netlist takes them), the 486DX's derived
    CPU (None for the 486SX) and their records, without running Yosys."""
    model = selected(model)
    derive = board.module(sources, DERIVE, "pc386_selected_derive")
    derived = {}
    machine, derived["pc386_machine.sv"] = derive.retarget_machine(sources[MISTEROSS + "cores/fes-pc386/rtl/pc386_machine.sv"])
    system, derived["pc386_system.sv"] = derive.retarget_system(sources[MISTEROSS + "cores/fes-pc386/rtl/pc386_system.sv"])
    at_core, derived["pc386_at_core.sv"] = derive.retarget_at_core(sources[MISTEROSS + "cores/fes-pc386/rtl/pc386_at_core.sv"])
    files = {"pc386_machine.sv": machine, "pc386_system.sv": system, "pc386_at_core.sv": at_core,
             "pc386_dma2_registers.sv": sources[BOARD + "pc386/pc386_dma2_registers.sv"]}
    cpu = None
    if model == DX_MODEL:
        i486dx = board.module(sources, BOARD + "pc386/i486dx/derive.py", "pc386_selected_i486dx")
        for upstream, (fork, pinned) in i486dx.FORKS.items():
            if board.sha(sources[MISTEROSS + upstream]) != pinned:
                raise ValueError("an i486DX fork is behind its upstream: " + upstream)
            if MISTEROSS + fork not in sources:
                raise ValueError("an i486DX fork is not a captured source: " + fork)
        files["pc386_system.sv"], derived["pc386_system_486dx_cpu"] = derive.retarget_system_cpu(system)
        cpu, derived[i486dx.CPU_FILE] = i486dx.derive_cpu(sources[MISTEROSS + i486dx.CPU_UPSTREAM])
        presenter = Path(derive.PRESENTER_UPSTREAM).name
        files[presenter], derived[presenter] = derive.retarget_presenter_loaded(
            sources[MISTEROSS + derive.PRESENTER_UPSTREAM])
    return files, cpu, derived


def component_files(sources, model=None):
    """The machine's own component files for a model; the board adds the shared shell."""
    model = selected(model)
    spectrum = board.module(sources, BOARD + "spectrum/derive.py", "pc386_selected_transport")
    files = {}
    for name in LOCAL_FILES:
        if name.endswith((".v", ".tcl", ".vh")):
            files[name] = sources[BOARD + "pc386/" + name]
    machine_files, cpu, derived = elaboration(sources, model)
    derive = board.module(sources, BOARD + "pc386/derive.py", "pc386_selected_derive")
    guard = "fes_hps_ddr_guard.v"
    files[guard], derived[guard] = derive.retarget_window_guard(
        sources[MISTEROSS + "cores/fes-common/rtl/" + guard])
    files["pc386_system.v"], prepared = machine_netlist(sources, machine_files, model, cpu)
    derived["pc386_system.v"] = {"front_end": "yosys-seal-netlist", "guest_rates": guest_rates(model),
                                 "netlist_sha256": board.sha(files["pc386_system.v"]), "I486": I486, **prepared,
                                 "x87": {"x87_present": "tied 0 in pc386_system"}}
    if model == DX_MODEL:
        derived["pc386_system.v"].update(model=model, X87_PRESENT=1, cpu=dx.TOP, x87={"x87_present": "X87_PRESENT = 1"},
                                         forks={upstream: fork for upstream, (fork, _) in dx.FORKS.items()})
    files["fes_de25_computer_transport.v"], derived["fes_de25_computer_transport.v"] = spectrum.transport(
        sources[BOARD + "zx81-hps/fes_de25_computer.v"])
    return files, derived


def project_files(sources):
    return {}


if __name__ == "__main__":
    board.delegate.main(sys.modules[__name__])
