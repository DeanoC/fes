#!/usr/bin/env python3
"""The fes.pc386 Quartus seal's shared definitions: the machine, its sources,
guest rates, firmware pins, Quartus project, timing gate and lockstep gate.

The registered producer, cores/fes-pc386/tools/build_pc386_product.py, seals
with these (docs/design/quartus-cores.md); this module writes no record and
builds no package. The simulators, the i386 fit tools and the x87 spike read
the same machine definition from here.

docs/pc386-machine-design.md, phase M5 (= CPU Phase 3): pc386_system, the
machine (AT core, VGA, capture, presenter, the display plane on DDR port 2,
OPL2, Sound Blaster DSP, mixer) with fes_i386_cpu, its L1s and bus interface
on DDR port 0, behind fes.computer 1.1: the session-display plane, HPS DDR,
48 kHz audio, the ATA disk the runtime writes into the DDR window, the
fes.pc386.machine record and the wall clock. The Bochs BIOS and the LGPL
VGABios are M10K contents of the RBF (pc386_rom.sv), from the images
scripts/build_pc386_firmware.py builds and firmware.lock pins. That recipe
writes the ROM hex as its own process before the seal; the seal never runs
its code. check_rom checks the file pc386_system.sv's ROM_HEX names against
the ROM digest firmware.lock pins, before and after Yosys reads it (#1984 S1).
The system clock is sealed at SYSTEM_CLOCK_HZ and every guest time base is a
rational enable of it, set here from that frequency.

The locked Quartus's SystemVerilog front end rejects some of the RTL (the OPL2
import's casts, the CPU's generate loops), so the pc386 lock's Yosys
elaborates everything below the board shell, pc386_system, with the guest
rates and the firmware. Memories with more than one write port and no
ramstyle become registers there: Yosys writes their ports as separate always
blocks, which Quartus rejects as multiple drivers, and Quartus infers no RAM
from them either. Yosys's flattened names (\\a.b[3]) become plain
identifiers in the same run (rename -unescape), since the locked Quartus's
synthesis stops with an internal error on them. scripts/yosys_netlist.py holds
that program (its SEAL recipe), and the i386 lockstep reads the CPU through
the same passes with the same Yosys. Quartus reads that netlist and the shell's own files, top.sv with
QUARTUS defined among them, and does all synthesis, placement and routing.
validate_build_evidence requires every timing analysis to pass on every clock
at the slow and fast corners. quartus.lock authenticates quartus_sh by digest
and its version line; the toolchain lock's Yosys elaborates the machine. The
toolchain lock's whole slot is authenticated, whose nextpnr is linked against
a GPU runtime: HIP or CUDA selects that slot. Nothing routes on a GPU here.

check_lockstep_report is the lockstep gate (#1581 S5): a report of
scripts/test_fes_i386_lockstep.py --netlist must have passed the default suite
over a full corpus and name this build's Yosys program, CPU sources and Yosys,
the i386 lock's Verilator and this tree's Bochs revision and patch, and the CPU
netlist this build's Yosys writes must have the report's digest. The oracle and
suite are hashed from the lists in cores/fes-common/sim/i386/oracle_sources.toml
without importing the oracle provisioner, scripts/fes_i386_oracle.py, so an edit
there cannot change what the compilers read (#1984 S1, case H1).
"""
from __future__ import annotations

from fractions import Fraction
import hashlib
import json
import math
import os
from pathlib import Path
import re
import shlex
import shutil
import tomllib

from scripts import fes_build_common as board
from scripts import lockfile
from scripts import yosys_netlist
from scripts.core_package import MAX_PAYLOAD_SIZE
from scripts.quartus_tools import QuartusLock, RebuildError, load_quartus_lock, locate_quartus

ROOT = Path(__file__).resolve().parents[1]
OUTPUT = Path("build/fes-pc386-quartus")
LOCK = "cores/fes-pc386/toolchain.lock"
# The lockstep's Verilator comes from the i386 lock (scripts/i386_verilator.py).
I386_LOCK = "cores/fes-common/rtl/i386/toolchain.lock"
LOCKSTEP_REPORT = "lockstep-report.json"
QUARTUS_LOCK = "cores/fes-pc386/quartus.lock"
QSF = "cores/fes-pc386/constraints-oss.qsf"
SDC = "cores/fes-pc386/clocks.sdc"
ABI_DEFINITION = "cores/fes-common/generated/fes_computer.vh"
FIRMWARE_LOCK = "cores/fes-pc386/firmware/firmware.lock"
FIRMWARE_RECIPE = "scripts/build_pc386_firmware.py"
FIRMWARE_IMAGES = ("bios", "vgabios")
# The lockstep oracle and suite, as data: the seal hashes these files and
# never imports their provisioner, scripts/fes_i386_oracle.py, which reads the
# same data file.
ORACLE_PROVISIONER = "scripts/fes_i386_oracle.py"
ORACLE_SOURCES_FILE = "cores/fes-common/sim/i386/oracle_sources.toml"
ORACLE_SIM = "cores/fes-common/sim/i386"


def _oracle_sources(root: Path) -> dict:
    try:
        data = tomllib.loads(board._regular_input(root, ORACLE_SOURCES_FILE).read_text(encoding="utf-8"))
    except (OSError, ValueError) as exc:
        raise board.BuildError(f"cannot read {ORACLE_SOURCES_FILE}: {exc}") from exc
    lists = ("oracle_sources", "suite_sources", "linked_names")
    if (not isinstance(data, dict) or set(data) != {"bochs_revision", "bochs_patch", *lists}
            or not isinstance(data["bochs_revision"], str) or re.fullmatch(r"[0-9a-f]{40}", data["bochs_revision"]) is None
            or not isinstance(data["bochs_patch"], str)
            or any(not isinstance(data[key], list) or not data[key] or len(set(data[key])) != len(data[key])
                   or not all(isinstance(name, str) and name for name in data[key]) for key in lists)):
        raise board.BuildError(f"{ORACLE_SOURCES_FILE} is not a valid oracle source list")
    return data


_ORACLE = _oracle_sources(ROOT)
BOCHS_REVISION = _ORACLE["bochs_revision"]
BOCHS_PATCH = _ORACLE["bochs_patch"]
# Relative to ORACLE_SIM, as the oracle's adapter digest names them.
ORACLE_SOURCES = tuple(_ORACLE["oracle_sources"])
SUITE_SOURCES = tuple(_ORACLE["suite_sources"])
LINKED_NAMES = tuple(_ORACLE["linked_names"])
SYSTEM_CLOCK_HZ = 61_440_000
SYSTEM_CLOCK = "61.44 MHz"
PIXEL_CLOCK_MHZ = 74.25
AUDIO_CLOCK_MHZ = 12.288
I2C_SITE = "HPSINTERFACEPERIPHERALI2C_X52_Y60_N111"
INCLUDES = (
    "cores/fes-common/generated/fes_application.vh",
    "cores/fes-common/generated/fes_computer.vh",
    "cores/fes-common/generated/fes_display_plane.vh",
    "cores/fes-common/vendor/kf8253/KF8253_Definitions.svh",
    "cores/fes-common/vendor/kf8259/KF8259_Common_Package.svh",
)
INCLUDE_DIRS = yosys_netlist.PC386_INCLUDES
OPL2 = "cores/fes-pc386/vendor/opl2_fpga"
# Quartus reads these directly: the shell, its PLLs and the hard-block users.
SHELL_SOURCES = (
    "cores/fes-pc386/rtl/pc386_system_pll.v",
    "cores/fes-common/rtl/pixel_pll.v",
    "cores/fes-common/rtl/fes_audio_output.v",
    "cores/fes-common/rtl/fes_audio_i2s.v",
    "cores/fes-common/rtl/fes_hps_ddr.v",
    "cores/fes-common/rtl/fes_hps_ddr_guard.v",
    "cores/fes-pc386/rtl/top.sv",
)
# Yosys elaborates these into one netlist below the shell.
MACHINE_SOURCES = (
    *yosys_netlist.CPU_SOURCES,
    *(f"cores/fes-common/vendor/kf8259/{name}.sv" for name in (
        "KF8259", "KF8259_Bus_Control_Logic", "KF8259_Control_Logic", "KF8259_In_Service",
        "KF8259_Interrupt_Request", "KF8259_Priority_Resolver")),
    *(f"cores/fes-common/vendor/kf8253/{name}.sv" for name in ("KF8253", "KF8253_Control_Logic", "KF8253_Counter")),
    "cores/fes-common/rtl/fes_dma8237.sv",
    *(f"cores/fes-pc386/rtl/{name}.sv" for name in (
        "pc386_rate", "pc386_io", "pc386_pic", "pc386_sysctl", "pc386_hid_keyboard", "pc386_kbc", "pc386_rtc",
        "pc386_ata", "pc386_fdc", "pc386_at_core", "pc386_ram", "pc386_vga", "pc386_vga_scanout", "pc386_presenter",
        "pc386_plane_bridge", "pc386_ems", "pc386_port2", "pc386_machine")),
    "cores/fes-common/rtl/fes_display_plane.v",
    "cores/fes-common/rtl/fes_display_cdc.v",
    "cores/fes-menu/rtl/fes_menu_control.v",
    "cores/fes-menu/rtl/fes_menu_video.v",
    "cores/fes-menu/rtl/fes_menu_reader.v",
    "cores/fes-common/rtl/fes_computer_mailbox.v",
    "cores/fes-common/rtl/fes_computer_storage.v",
    f"{OPL2}/opl2_pkg.sv",
    *(f"{OPL2}/{name}.sv" for name in (
        "pipeline_sr", "mem_simple_dual_port", "mem_simple_dual_port_async_read", "mem_single_bank",
        "mem_single_bank_reset", "opl2_log_sine_lut", "opl2_exp_lut", "ksl_add_rom", "tremolo", "vibrato",
        "calc_phase_inc", "calc_envelope_shift", "calc_rhythm_phase", "envelope_generator", "phase_generator",
        "operator", "control_operators", "dac_prep", "channels")),
    *(f"cores/fes-pc386/rtl/{name}.sv" for name in (
        "pc386_opl2", "pc386_sb", "pc386_mixer", "pc386_audio", "pc386_rom", "pc386_disk", "pc386_snapshot",
        "pc386_status", "pc386_system")),
)
MACHINE_TOP = "pc386_system"
MACHINE_NETLIST = "pc386_system.v"
# pc386_system.sv's ROM_HEX default, which it passes to pc386_rom; Yosys reads
# it from the repository root. scripts/build_pc386_firmware.py writes it.
ROM_HEX = "build/firmware/pc386/pc386-rom.hex"
ROM_HEX_READERS = ("cores/fes-pc386/rtl/pc386_system.sv", "cores/fes-pc386/rtl/pc386_rom.sv")
PINNED_INPUTS = (
    "scripts/pc386_quartus.py", FIRMWARE_RECIPE, FIRMWARE_LOCK, "scripts/quartus_tools.py", "scripts/fes_build_common.py",
    "scripts/source_provenance.py", "scripts/yosys_netlist.py", "scripts/lockfile.py", ABI_DEFINITION, LOCK,
    I386_LOCK, QUARTUS_LOCK, QSF, SDC, ORACLE_PROVISIONER, ORACLE_SOURCES_FILE, BOCHS_PATCH,
    *(f"{ORACLE_SIM}/{name}" for name in ORACLE_SOURCES),
    *(name for name in SUITE_SOURCES if name != "scripts/yosys_netlist.py"),
    *INCLUDES, *SHELL_SOURCES,
    *MACHINE_SOURCES,
)
BUILD_OUTPUTS = (
    "core.rbf", "top.sta.rpt", "top.fit.rpt", "machine.ys", "yosys.log", "quartus.log", "quartus-version.log",
    "build-inputs.json", "build-summary.json", "manifest.toml", LOCKSTEP_REPORT,
)
TIMING_CATEGORIES = ("Setup", "Hold", "Recovery", "Removal", "Minimum Pulse Width")
WORST_SLACK_RE = re.compile(r"; Worst-case Slack\s*((?:;\s*-?[0-9.]+\s*){5});")
DESIGN_TNS_RE = re.compile(r"; Design-wide TNS\s*((?:;\s*-?[0-9.]+\s*){5});")
# Registers on a clock TimeQuest cannot analyse are in no slack row.
UNANALYSED_CLOCKS = ("Illegal Clocks", "Unconstrained Clocks")
UNANALYSED_ROW_RE = re.compile(r"^; (Illegal Clocks|Unconstrained Clocks)\s*; (\d+)\s*; (\d+)\s*;", re.M)
# TimeQuest's clock table: name, type, period, frequency, ...
CLOCK_ROW_RE = re.compile(r"^; (\S+)\s*; (Base|Generated)\s*; ([0-9.]+)\s*; ([0-9.]+) MHz\s*;", re.M)
# The PLLs' fractional synthesis lands the pixel clock at 74.27 MHz.
COUNTER = ".gpll~PLL_OUTPUT_COUNTER|divclk"
CLOCKS = (
    ("system", f"system_clock|pll|general[0]{COUNTER}", SYSTEM_CLOCK_HZ / 1e6, 0.01),
    ("audio", f"system_clock|pll|general[1]{COUNTER}", AUDIO_CLOCK_MHZ, 0.01),
    ("pixel", f"video_clock|pll|general[0]{COUNTER}", PIXEL_CLOCK_MHZ, 0.03),
)
# The Fitter Summary's resource rows: used and available.
RESOURCES = {
    "alms": "Logic utilization (in ALMs)", "registers": "Total registers", "ram_blocks": "Total RAM Blocks",
    "block_memory_bits": "Total block memory bits", "dsp_blocks": "Total DSP Blocks",
}
SEED_LIMIT = 0x7fffffff
CORE = {"id": "fes.pc386", "name": "FES pc386", "version": "0.1.0",
        "description": "A 386 AT: fes_i386_cpu, the Bochs BIOS and LGPL VGABios, VGA at 720p, OPL2 and Sound "
                       "Blaster 2.0, a PS/2 keyboard and mouse, and a hard disk of up to 128 MiB with its saves. "
                       "Development only."}
INTERFACES = ("fes.video.fixed-720p60", "fes.keyboard.hid", "fes.audio.pcm-s16-stereo-48k", "fes.mouse.relative",
              "fes.memory.hps-ddr", "fes.video.session-display", "fes.fabric.video.display-plane",
              "fes.media.ata-disk", "fes.persistence.words", "fes.rtc.wall-clock", "fes.pc386.machine")
# Every guest time base as a rational enable of the system clock
# (docs/pc386-machine-design.md, "Clocks and timing"): N/D = rate / clock.
GUEST_RATES = {
    "ISA": Fraction(25_000_000, 3),           # 8.33 MHz ISA clock
    "PIT": 2 * Fraction(105_000_000, 88),     # both edges of the 1,193,181.8 Hz PIT clock
    "RTC": Fraction(32_768),
    "US": Fraction(1_000_000),                # 8042 pacing, DSP time base
    "DMA": Fraction(25_000_000, 6),           # 4.17 MHz DMA clock
    "DOT0": Fraction(25_175_000),
    "DOT1": Fraction(28_322_000),
    "OPL2": Fraction(315_000_000, 88 * 72),   # 49,715.9 Hz OPL2 samples
    "AUDIO": Fraction(48_000),                # the mixer's frame request
    "GOV": Fraction(100_000),                 # the governor's frequency unit (fes_i386_governor)
}
ATA_RESET_SECONDS = Fraction(1, 100_000)      # 10 us


def guest_rates(clock_hz: int = SYSTEM_CLOCK_HZ) -> dict[str, int]:
    """pc386_system's N/D parameters and ATA_RESET_CLOCKS for a system clock."""
    if clock_hz < 2 * GUEST_RATES["DOT1"]:
        raise board.BuildError("the system clock must be at least twice the 28.322 MHz dot clock")
    match = re.fullmatch(r"(\d+(?:\.\d+)?) MHz", SYSTEM_CLOCK)
    if match is None or round(float(match.group(1)) * 1e6) != SYSTEM_CLOCK_HZ:
        raise board.BuildError("SYSTEM_CLOCK and SYSTEM_CLOCK_HZ disagree")
    parameters = {}
    for name, rate in GUEST_RATES.items():
        ratio = rate / clock_hz
        parameters[f"{name}_N"] = ratio.numerator
        parameters[f"{name}_D"] = ratio.denominator
    parameters["ATA_RESET_CLOCKS"] = math.ceil(ATA_RESET_SECONDS * clock_hz)
    return parameters


def quartus_lock(root: Path) -> QuartusLock:
    """cores/fes-pc386/quartus.lock, the Quartus every version string names."""
    try:
        return load_quartus_lock(root, QUARTUS_LOCK)
    except RebuildError as exc:
        raise board.BuildError(str(exc)) from exc


def gpu_choice(gpu_router: str) -> tuple[str, str, str]:
    """The backend, architectures and nextpnr configuration of the slot gpu_router selects."""
    backend = board.gpu_backend_for_router(gpu_router)
    if gpu_router == "HIP":
        return backend, board.FES_GPU_ARCHITECTURES, board.FES_TOOLCHAIN_CONFIGURATION
    return backend, "unused", board.cuda_toolchain_configuration()


def _authenticate_tools(root: Path, *, cache_root: Path | None = None, gpu_router: str = "HIP") -> dict[str, str]:
    """The toolchain lock's Yosys and the quartus.lock install, each by digest. The
    lock's slot for gpu_router is authenticated whole; the build runs only its Yosys."""
    gpu_choice(gpu_router)
    try:
        yosys = yosys_netlist.authenticate(root, cache_root=cache_root, gpu_router=gpu_router, lock=LOCK)
    except yosys_netlist.NetlistError as exc:
        message = str(exc)
        if "missing path component:" in message or "ready manifest is missing:" in message:
            command = ["make", "toolchain-fes-pc386"]
            if cache_root is not None:
                command.append(f"CACHE_ROOT={cache_root}")
            command.append(f"GPU_ROUTER={gpu_router}")
            message += f"; provision the locked toolchain with `{shlex.join(command)}`"
        raise board.BuildError(message) from exc
    lock = quartus_lock(root)
    try:
        quartus_root, quartus_sh, version = locate_quartus(os.environ.get("QUARTUS_ROOTDIR", ""), root, lock)
    except RebuildError as exc:
        raise board.BuildError(str(exc)) from exc
    return {
        "yosys": yosys,
        "quartus_root": quartus_root,
        "quartus_sh": quartus_sh,
        "quartus_version": version,
        "identities": {
            "yosys": yosys.identity,
            "quartus_sh": f"version={lock.release}; sha256={board._sha256(quartus_sh)}",
        },
    }


def _check_seed(seed: int) -> int:
    if type(seed) is not int or not 1 <= seed <= SEED_LIMIT:
        raise board.BuildError(f"Quartus seed must be an integer from 1 to {SEED_LIMIT}")
    return seed


def machine_program(netlist: Path, sources: str | None = None) -> str:
    """The Yosys program that elaborates the machine at the sealed guest rates. pc386_rom
    reads ROM_HEX while it is read in. sources stands in for MACHINE_SOURCES."""
    return yosys_netlist.program(yosys_netlist.SEAL, top=MACHINE_TOP,
                                 sources=MACHINE_SOURCES if sources is None else (sources,),
                                 includes=INCLUDE_DIRS, parameters=guest_rates(), netlist=netlist.as_posix())


def _firmware_lock(root: Path) -> dict:
    try:
        lock = json.loads(board._regular_input(root, FIRMWARE_LOCK).read_text(encoding="utf-8"))
    except (OSError, ValueError) as exc:
        raise board.BuildError(f"cannot read {FIRMWARE_LOCK}: {exc}") from exc
    if not isinstance(lock, dict):
        raise board.BuildError(f"{FIRMWARE_LOCK} is not a JSON object")
    return lock


def _pinned_sha256(lock: dict, *keys: str) -> str:
    value = lock
    for key in keys:
        value = value.get(key) if isinstance(value, dict) else None
    if not isinstance(value, str) or board.HEX64_RE.fullmatch(value) is None:
        raise board.BuildError(f"{FIRMWARE_LOCK} does not pin {'.'.join(keys)}")
    return value


def firmware_digests(root: Path) -> dict[str, str]:
    """The BIOS and VGA BIOS digests firmware.lock pins: the ROM's contents."""
    lock = _firmware_lock(root)
    return {name: _pinned_sha256(lock, "images", name, "sha256") for name in FIRMWARE_IMAGES}


def pinned_rom_digest(root: Path) -> str:
    """The digest firmware.lock pins for the ROM hex Yosys reads."""
    return _pinned_sha256(_firmware_lock(root), "rom_hex", "sha256")


def project_qsf(root: Path, project: Path, build_id: str, seed: int, *, threads: int | str) -> str:
    if board.HEX32_RE.fullmatch(build_id) is None:
        raise board.BuildError("build ID must be 32 lowercase hexadecimal characters")
    rel = Path(os.path.relpath(root, project)).as_posix()
    lines = [
        'set_global_assignment -name FAMILY "Cyclone V"',
        f"set_global_assignment -name DEVICE {board.TARGET}",
        f"set_global_assignment -name TOP_LEVEL_ENTITY {board.TOP}",
        'set_global_assignment -name PROJECT_OUTPUT_DIRECTORY "output_files"',
        "set_global_assignment -name GENERATE_RBF_FILE ON",
        "set_global_assignment -name IGNORE_PARTITIONS ON",
        f"set_global_assignment -name NUM_PARALLEL_PROCESSORS {threads}",
        f"set_global_assignment -name SEED {_check_seed(seed)}",
        "set_global_assignment -name VERILOG_INPUT_VERSION SYSTEMVERILOG_2005",
        f'set_global_assignment -name LAST_QUARTUS_VERSION "{quartus_lock(root).qsf_version}"',
        f'set_global_assignment -name SEARCH_PATH "{rel}/cores/fes-common/generated"',
        'set_global_assignment -name VERILOG_MACRO "QUARTUS=1"',
        f"set_global_assignment -name VERILOG_MACRO \"FES_PC386_BUILD_ID=128'h{build_id}\"",
        f'set_global_assignment -name SDC_FILE "{rel}/{SDC}"',
        f"set_instance_assignment -name HPS_LOCATION {I2C_SITE} -entity {board.TOP} -to hdmi_i2c",
        f"set_global_assignment -name VERILOG_FILE {MACHINE_NETLIST}",
    ]
    for source in SHELL_SOURCES:
        kind = "SYSTEMVERILOG_FILE" if source.endswith(".sv") else "VERILOG_FILE"
        lines.append(f'set_global_assignment -name {kind} "{rel}/{source}"')
    pins = board._regular_input(root, QSF).read_text(encoding="utf-8")
    return "\n".join(lines) + "\n\n" + pins


def write_project(root: Path, output: Path, build_id: str, seed: int, *, threads: int | str) -> Path:
    project = output / "project"
    if project.is_symlink():
        raise board.BuildError(f"Quartus project must not be a symlink: {project}")
    if project.exists():
        shutil.rmtree(project)
    project.mkdir()
    series = quartus_lock(root).series
    (project / "top.qpf").write_text(f'QUARTUS_VERSION = "{series}";\nPROJECT_REVISION = "top";\n', encoding="utf-8")
    (project / "top.qsf").write_text(project_qsf(root, project, build_id, seed, threads=threads), encoding="utf-8")
    return project


ROM_HEX_PARAMETER_RE = re.compile(r'\bparameter\s+ROM_HEX\s*=\s*"([^"]*)"')


def check_rom(root: Path) -> Path:
    """The ROM hex at the path the RTL reads, checked against firmware.lock's
    pinned digest. scripts/build_pc386_firmware.py writes it; the seal only
    reads it, so a recipe edit that writes other bytes or another path is
    refused here rather than elaborated (#1984 S1, cases R3, R4 and H2)."""
    for reader in ROM_HEX_READERS:
        defaults = ROM_HEX_PARAMETER_RE.findall(board._regular_input(root, reader).read_text(encoding="utf-8"))
        if defaults != [ROM_HEX]:
            raise board.BuildError(f"{reader} reads the ROM from {defaults}, but the seal checks {ROM_HEX}")
    expected = pinned_rom_digest(root)
    rom = root / ROM_HEX
    if board._contains_symlink(root, ROM_HEX) or not rom.is_file():
        raise board.BuildError(f"{ROM_HEX} is missing; run scripts/build_pc386_firmware.py before the seal")
    actual = board._sha256(rom)
    if actual != expected:
        raise board.BuildError(f"{ROM_HEX} is {actual}, not the {expected} firmware.lock pins; "
                               "run scripts/build_pc386_firmware.py")
    return rom


def elaborate_machine(root: Path, output: Path, project: Path, yosys: Path) -> None:
    """Yosys writes project/pc386_system.v, run from the repository root with
    paths relative to it, so the program and argv name no checkout."""
    netlist = project / MACHINE_NETLIST
    try:
        yosys_netlist.run(yosys, machine_program(netlist.relative_to(root)), cwd=root,
                          program_file=output / "machine.ys", log=output / "yosys.log")
    except yosys_netlist.NetlistError as exc:
        raise board.BuildError(f"Yosys could not elaborate {MACHINE_TOP}: {exc}") from exc
    if f"module {MACHINE_TOP}(" not in netlist.read_text(encoding="utf-8"):
        raise board.BuildError(f"elaborated netlist does not define {MACHINE_TOP}")


def validate_timing_report(sta_text: str) -> dict:
    """Every analysis's worst slack and TNS across corners, and each clock's
    frequency, from TimeQuest's report."""
    slack, tns = WORST_SLACK_RE.search(sta_text), DESIGN_TNS_RE.search(sta_text)
    if slack is None or tns is None:
        raise board.BuildError("timing report lacks the multicorner worst-case slack and TNS rows")
    analyses = {}
    for name, slack_text, tns_text in zip(TIMING_CATEGORIES, re.findall(r"-?[0-9.]+", slack.group(1)),
                                          re.findall(r"-?[0-9.]+", tns.group(1))):
        worst, total = float(slack_text), float(tns_text)
        if not (math.isfinite(worst) and math.isfinite(total)):
            raise board.BuildError(f"{name} timing is not finite")
        # A miss under half a picosecond prints as -0.000.
        if slack_text.startswith("-") or tns_text.startswith("-"):
            raise board.BuildError(f"{name} timing fails: worst slack {worst} ns, TNS {total} ns")
        analyses[name.lower().replace(" ", "_")] = {"worst_slack_ns": worst, "tns_ns": total}
    unanalysed = UNANALYSED_ROW_RE.findall(sta_text)
    if sorted(row[0] for row in unanalysed) != sorted(UNANALYSED_CLOCKS):
        raise board.BuildError("timing report lacks the unconstrained paths summary")
    for row, setup, hold in unanalysed:
        if int(setup) or int(hold):
            raise board.BuildError(f"timing report has {row.lower()}: {setup} setup, {hold} hold")
    rows = {(name, float(frequency)) for name, _, _, frequency in CLOCK_ROW_RE.findall(sta_text)}
    clocks = {}
    for label, clock, mhz, tolerance in CLOCKS:
        frequencies = [frequency for name, frequency in rows if name == clock]
        if len(frequencies) != 1:
            raise board.BuildError(f"timing report must list the {label} clock {clock} once")
        if abs(frequencies[0] - mhz) > tolerance:
            raise board.BuildError(f"{label} clock is {frequencies[0]} MHz, not {mhz:g} MHz")
        clocks[label] = {"clock": clock, "mhz": frequencies[0]}
    return {"status": "pass", "analyses": analyses, "clocks": clocks}


def fit_resources(fit_text: str) -> dict:
    """The Fitter Summary's resource use: each row's count, and the device's where it gives one."""
    resources = {}
    for key, label in RESOURCES.items():
        match = re.search(rf"^; {re.escape(label)}\s*; ([0-9,]+)(?: / ([0-9,]+))?", fit_text, re.M)
        if match is None:
            raise board.BuildError(f"fit report lacks the {label} row")
        used, available = (int(value.replace(",", "")) if value else None for value in match.groups())
        resources[key] = {"used": used} if available is None else {"used": used, "available": available}
    return resources


def validate_build_evidence(output: Path, project: Path) -> dict:
    built = project / "output_files"
    for name in ("top.rbf", "top.sta.rpt", "top.fit.rpt"):
        path = built / name
        if path.is_symlink() or not path.is_file():
            raise board.BuildError(f"Quartus did not write {path}")
    if I2C_SITE not in (project / "top.qsf").read_text(encoding="utf-8"):
        raise board.BuildError("Quartus project does not place the HDMI I2C controller")
    timing = validate_timing_report((built / "top.sta.rpt").read_text(encoding="utf-8", errors="replace"))
    resources = fit_resources((built / "top.fit.rpt").read_text(encoding="utf-8", errors="replace"))
    rbf = built / "top.rbf"
    if not 1 <= rbf.stat().st_size <= MAX_PAYLOAD_SIZE:
        raise board.BuildError(f"RBF must be a nonempty bounded file: {rbf}")
    for name in ("top.sta.rpt", "top.fit.rpt"):
        shutil.copyfile(built / name, output / name)
    shutil.copyfile(rbf, output / "core.rbf")
    payload = output / "core.rbf"
    return {
        "status": "pass",
        "timing": timing,
        "resources": resources,
        "guest_rates": guest_rates(),
        "rbf": {"sha256": board._sha256(payload), "size": payload.stat().st_size},
    }


def named_sources_sha256(named: list[tuple[str, bytes]]) -> str:
    """Named source bytes in order with length framing: the digest
    scripts/fes_i386_oracle.py's named_sources_sha256 gives the lockstep report."""
    digest = hashlib.sha256()
    for name, data in named:
        digest.update(name.encode() + b"\0")
        digest.update(len(data).to_bytes(8, "big"))
        digest.update(data)
    return digest.hexdigest()


def oracle_identity(root: Path) -> dict[str, str]:
    """The source identity of this tree's declared lockstep oracle and suite,
    hashed as data. The lists are this checkout's, and root's provisioner must
    be this checkout's, as a report from another provisioner cannot be judged."""
    try:
        target = board._regular_input(root, ORACLE_PROVISIONER).read_bytes()
        if target != board._regular_input(ROOT, ORACLE_PROVISIONER).read_bytes():
            raise board.BuildError("target oracle provisioning script differs from the running one")
        patch = board._regular_input(root, BOCHS_PATCH)
        adapter = [(name, board._regular_input(root, f"{ORACLE_SIM}/{name}").read_bytes()) for name in ORACLE_SOURCES]
        suite = [(name, board._regular_input(root, name).read_bytes()) for name in SUITE_SOURCES]
        return {"revision": BOCHS_REVISION, "patch_sha256": board._sha256(patch),
                "provisioner_sha256": hashlib.sha256(target).hexdigest(),
                "adapter_sha256": named_sources_sha256(adapter), "suite_sha256": named_sources_sha256(suite)}
    except OSError as exc:
        raise board.BuildError(f"cannot read the Bochs oracle or suite source: {exc}") from exc


def require_same_oracle(root: Path, expected: dict) -> None:
    actual = oracle_identity(root)
    if any(expected.get(key) != value for key, value in actual.items()):
        raise board.BuildError("Bochs oracle or lockstep suite source changed during build")


def valid_sha256(value) -> bool:
    return isinstance(value, str) and board.HEX64_RE.fullmatch(value) is not None


def check_lockstep_report(root: Path, data: bytes, yosys: board.AuthenticatedTool, gpu_router: str,
                          output: Path) -> dict:
    """The lockstep gate (#1581 S5). data is the report's bytes. Returns the
    seal summary's "lockstep" record, or raises if the report does not bind
    to this build."""
    try:
        report = json.loads(data)
    except ValueError as exc:
        raise board.BuildError(f"lockstep report is not JSON: {exc}") from exc
    run = report.get("run") if isinstance(report, dict) else None
    if not isinstance(run, dict) or run.get("passed") is not True:
        raise board.BuildError("lockstep report did not pass")
    if run.get("complete_corpus") is not True:
        raise board.BuildError("lockstep report does not prove a complete corpus; use the current lockstep runner")
    if run.get("default_suite") is not True:
        raise board.BuildError(f"lockstep report is not the default suite: {run.get('arguments')}")
    netlist = report.get("netlist")
    if not isinstance(netlist, dict):
        raise board.BuildError("lockstep report is not a --netlist run")
    expected = {"program_sha256": yosys_netlist.program_sha256(yosys_netlist.SEAL),
                "cpu_source_sha256": yosys_netlist.cpu_source_sha256(root),
                "yosys": yosys_netlist.lock_record(root, yosys, gpu_router, LOCK)}
    for key, value in expected.items():
        if netlist.get(key) != value:
            raise board.BuildError(f"lockstep report's netlist {key} is {netlist.get(key)!r}, this build's is {value!r}")
    verilator = report.get("verilator")
    try:
        pinned = lockfile.load_lock(root / I386_LOCK)["verilator"].commit
    except (OSError, lockfile.LockfileError, KeyError, ValueError) as exc:
        raise board.BuildError(f"cannot read {I386_LOCK}: {exc}") from exc
    if not isinstance(verilator, dict) or verilator.get("lock") != I386_LOCK or verilator.get("commit") != pinned:
        raise board.BuildError(f"lockstep report's Verilator is not {I386_LOCK}'s {pinned}: {verilator!r}")
    expected_oracle = oracle_identity(root)
    measured_oracle = report.get("oracle")
    if (not isinstance(measured_oracle, dict)
            or set(measured_oracle) != set(expected_oracle) | {"linked_sha256", "testbench_sha256"}
            or any(measured_oracle.get(key) != value for key, value in expected_oracle.items())):
        raise board.BuildError(f"lockstep report's oracle identity {measured_oracle!r} is not {expected_oracle!r}")
    linked = measured_oracle["linked_sha256"]
    if (not isinstance(linked, dict) or set(linked) != set(LINKED_NAMES)
            or not all(valid_sha256(value) for value in linked.values())
            or not valid_sha256(measured_oracle["testbench_sha256"])):
        raise board.BuildError("lockstep report's oracle binary digests are missing or invalid")
    try:
        regenerated = yosys_netlist.seal_netlist(yosys.path, output / "lockstep", root)
    except yosys_netlist.NetlistError as exc:
        raise board.BuildError(f"cannot regenerate the CPU netlist: {exc}") from exc
    digest = board._sha256(regenerated)
    if digest != netlist.get("netlist_sha256"):
        raise board.BuildError(f"lockstep report's CPU netlist {netlist.get('netlist_sha256')!r} is not the one "
                               f"this build's Yosys writes ({digest})")
    return {"report_sha256": hashlib.sha256(data).hexdigest(), "netlist_sha256": digest,
            "netlist_program_sha256": expected["program_sha256"], "yosys_tool": yosys.identity,
            "cpu_source_closure_sha256": expected["cpu_source_sha256"], "simulator": verilator,
            "oracle": measured_oracle}
