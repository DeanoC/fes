#!/usr/bin/env python3
"""Integrate the standard pc386 into the managed DE25 GHRD (see ../machine/board.py).

The machine is the DE10's pc386_system with its display plane derived out
(derive.py), compiled as the DE10 seal compiles it: the pc386 lock's Yosys
SEAL netlist, here at the DE25 machine clock's guest rates. Its memory is a
window in HPS DDR (fes.fabric.de25-hps-window): fes_de25_hps_window's guards,
rebase and bounds, then fes_de25_window_arbiter's one master. The identity
is a 386 with no 387: pc386_system ties x87_present to 0, as on the DE10.
"""
from fractions import Fraction
from pathlib import Path
import importlib.util
import math
import os
import subprocess
import sys
import fcntl
import shutil

HERE = Path(__file__).resolve().parent
BOARD = "sources/misteross/boards/de25nano/"
MISTEROSS = "sources/misteross/"
NAME = "pc386"
DIRECTORY = "pc386"
CORE_ID = "fes.pc386"
COMPONENT = "fes_de25_pc386_standard"
SDC = BOARD + "pc386/pc386-clocks.sdc"
MODELS = {"386": None}
# The board: its own machine PLL on the shared VCO (50 x 188/3): /51 for the
# machine's 61.437908 MHz and /255 for the 12.287582 MHz audio master clock,
# both -34 ppm and exactly 5:1, as on the DE10. No board SDRAM; a window.
MACHINE_HZ = 61437908
CLOCK = Fraction(9_400_000_000, 153)
PLL_IP = BOARD + "pc386/machine_pll.ip"
PLL_OUTPUTS = ("61.437908", "12.287582")
BOARD_SDRAM = False
WINDOW = True
ASSIGNMENTS = ()
# Qsys writes this peripheral subsystem for the component. Reviewed against the
# Spectrum's (a08edf0b...): no SDRAM conduit; the window's master, control
# slave and bridge (fes_win_cdc) added; one more reset controller, which
# synchronizes the system reset into the machine clock for the component's
# machine_reset and the bridge's machine side (rst_controller_001), so the
# others are renumbered by one; the window's machine-clock interconnect has its
# own (rst_controller_005). The shared FIFO reset derivation checks its bridge
# clock and reset wiring before rewriting it.
PARENT_SHA256 = "10defc9a021d7119aac2bbc793ac0d6913e902b99f85cb29977c452996eeb249"
LOCAL_FILES = ("build.py", "derive.py", "machine_pll.ip", "pc386-clocks.sdc", "fes_de25_pc386_standard.v",
               "fes_de25_pc386_standard_hw.tcl", "fes_de25_hps_window.v", "fes_de25_hps_window_control.v",
               "fes_de25_window_arbiter.v", "fes_de25_window_unpack.v", "generated/fes_de25_hps_window.vh")
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

SOURCES = ({BOARD + "pc386/" + name for name in LOCAL_FILES} |
           {BOARD + "spectrum/derive.py", BOARD + "zx81-hps/fes_de25_computer.v",
            "sources/misteross/cores/fes-common/rtl/fes_hps_ddr_guard.v"} |
           {MISTEROSS + path for path in sealer.MACHINE_SOURCES + sealer.INCLUDES} |
           {MISTEROSS + path for path in (sealer.FIRMWARE_LOCK, sealer.LOCK, "scripts/yosys_netlist.py",
                                          "scripts/pc386_quartus.py", "scripts/fit_i386_device.py")})


def guest_rates():
    """pc386_system's N/D pairs at the DE25 machine clock, exact rationals of it.

    AUDIO is the one fixed ratio: the I2S runs at the audio clock / 256, which
    is the machine clock / 1280, so the mixer's frame request is 1/1280 of it
    rather than 48 kHz of the true clock, which would drift against the I2S.
    """
    parameters = {}
    for name, rate in sealer.GUEST_RATES.items():
        ratio = Fraction(1, 1280) if name == "AUDIO" else rate / CLOCK
        if ratio.numerator >= 2 ** 31 or ratio.denominator >= 2 ** 31:
            raise ValueError(f"{name} does not fit pc386_system's integer parameters")
        parameters[f"{name}_N"], parameters[f"{name}_D"] = ratio.numerator, ratio.denominator
    parameters["ATA_RESET_CLOCKS"] = math.ceil(sealer.ATA_RESET_SECONDS * CLOCK)
    return parameters


def machine_netlist(sources, derived_files):
    """The derived pc386_system as the pc386 lock's Yosys elaborates it (SEAL).

    It runs in the checkout, whose captured sources it requires to be exactly
    these bytes, with the ROM hex firmware.lock pins.
    """
    root = board.ROOT / MISTEROSS
    for path, data in sources.items():
        if path.startswith(MISTEROSS) and (board.ROOT / path).read_bytes() != data:
            raise ValueError("the checkout is not the captured source: " + path)
    sealer.check_rom(root)
    cache = os.environ.get("FES_TOOLCHAIN_CACHE_ROOT")
    if not cache:
        raise ValueError("FES_TOOLCHAIN_CACHE_ROOT must name the pc386 lock's toolchain cache")
    yosys = yosys_netlist.authenticate(root, cache_root=Path(cache), gpu_router=os.environ.get("FES_GPU_ROUTER", "HIP"),
                                       lock=sealer.LOCK)
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
        machine = tuple(swap.get(path, path) for path in sealer.MACHINE_SOURCES)
        netlist = work / "pc386_system.v"
        program = yosys_netlist.program(yosys_netlist.SEAL, top=sealer.MACHINE_TOP, sources=machine,
                                        includes=sealer.INCLUDE_DIRS, parameters=guest_rates(),
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


def component_files(sources):
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
    files["pc386_system.v"], prepared = machine_netlist(sources, {"pc386_machine.sv": machine, "pc386_system.sv": system})
    derived["pc386_system.v"] = {"front_end": "yosys-seal-netlist", "guest_rates": guest_rates(),
                                 "netlist_sha256": board.sha(files["pc386_system.v"]), **prepared,
                                 "x87": {"x87_present": "tied 0 in pc386_system"}}
    files["fes_de25_computer_transport.v"], derived["fes_de25_computer_transport.v"] = spectrum.transport(
        sources[BOARD + "zx81-hps/fes_de25_computer.v"])
    return files, derived


def project_files(sources):
    return {}


if __name__ == "__main__":
    board.main(sys.modules[__name__])
