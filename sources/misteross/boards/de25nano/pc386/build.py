#!/usr/bin/env python3
"""Integrate the standard pc386 into the managed DE25 GHRD (see ../machine/board.py).

The machine is the DE10's pc386_system with its display plane derived out
(derive.py), compiled as the DE10 seal compiles it: the pc386 lock's Yosys
SEAL netlist, here at the DE25 machine clock's guest rates. Its memory is a
window in HPS DDR (fes.fabric.de25-hps-window): fes_de25_hps_window's guards,
rebase and bounds, then fes_de25_window_arbiter's one master. The identity
is a 486SX (#1962: CPUID, no VME) with no 387: the elaboration sets
pc386_system's I486 parameter to 1, which the DE10's 386 seal leaves at 0, and
pc386_system ties x87_present to 0, as on the DE10. The parameter is a Yosys
top parameter, so no shared source changes and the DE10 netlist does not move.
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
MODELS = {"486sx": None}
# pc386_system's CPU identity parameter (fes_i386_cpu I486): the 486SX.
I486 = 1
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
SHARED_FILES = ("fes_computer_mailbox.v",)


def load(path, name):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


board = load(HERE.parent / "machine/board.py", "de25_pc386_machine_board")
sys.path.insert(0, str(board.ROOT / MISTEROSS))
from scripts import pc386_quartus as sealer  # noqa: E402
from scripts import yosys_netlist  # noqa: E402
from scripts import fit_i386_device as fit  # noqa: E402

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

SOURCES = ({BOARD + "pc386/" + name for name in LOCAL_FILES} |
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


def machine_parameters(model=None):
    """pc386_system's top parameters: the guest rates and the 486SX identity."""
    return {**guest_rates(model), "I486": I486}


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
    linked firmware and the CPU and x87 identity."""
    root = Path(root)
    return {"front_end": "yosys-seal-netlist", "guest_rates": json.dumps(guest_rates(model), sort_keys=True),
            "i486": I486,
            "firmware": json.dumps({**sealer.firmware_digests(root), "rom_hex": sealer.pinned_rom_digest(root)},
                                   sort_keys=True),
            "x87_present": False, "signoff_findings_review_policy": "de25-pc386-reviewed-signoff-findings-v1"}


def machine_netlist(sources, derived_files, model=None):
    """The derived pc386_system as the pc386 lock's Yosys elaborates it (SEAL).

    It runs in the checkout, whose captured sources it requires to be exactly
    these bytes, with the ROM hex firmware.lock pins.
    """
    root = board.ROOT / MISTEROSS
    for path, data in sources.items():
        if path.startswith(MISTEROSS) and (board.ROOT / path).read_bytes() != data:
            raise ValueError("the checkout is not the captured source: " + path)
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


def component_files(sources, model=None):
    """The machine's own component files; the board adds the shared shell."""
    derive = board.module(sources, BOARD + "pc386/derive.py", "pc386_selected_derive")
    spectrum = board.module(sources, BOARD + "spectrum/derive.py", "pc386_selected_transport")
    files, derived = {}, {}
    for name in LOCAL_FILES:
        if name.endswith((".v", ".tcl", ".vh")):
            files[name] = sources[BOARD + "pc386/" + name]
    files["fes_hps_ddr_guard.v"] = sources["sources/misteross/cores/fes-common/rtl/fes_hps_ddr_guard.v"]
    machine, derived["pc386_machine.sv"] = derive.retarget_machine(sources[MISTEROSS + "cores/fes-pc386/rtl/pc386_machine.sv"])
    system, derived["pc386_system.sv"] = derive.retarget_system(sources[MISTEROSS + "cores/fes-pc386/rtl/pc386_system.sv"])
    at_core, derived["pc386_at_core.sv"] = derive.retarget_at_core(sources[MISTEROSS + "cores/fes-pc386/rtl/pc386_at_core.sv"])
    files["pc386_system.v"], prepared = machine_netlist(sources, {
        "pc386_machine.sv": machine, "pc386_system.sv": system, "pc386_at_core.sv": at_core,
        "pc386_dma2_registers.sv": sources[BOARD + "pc386/pc386_dma2_registers.sv"]}, model)
    derived["pc386_system.v"] = {"front_end": "yosys-seal-netlist", "guest_rates": guest_rates(model),
                                 "netlist_sha256": board.sha(files["pc386_system.v"]), "I486": I486, **prepared,
                                 "x87": {"x87_present": "tied 0 in pc386_system"}}
    files["fes_de25_computer_transport.v"], derived["fes_de25_computer_transport.v"] = spectrum.transport(
        sources[BOARD + "zx81-hps/fes_de25_computer.v"])
    return files, derived


def project_files(sources):
    return {}


if __name__ == "__main__":
    board.main(sys.modules[__name__])
