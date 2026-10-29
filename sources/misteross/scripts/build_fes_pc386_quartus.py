#!/usr/bin/env python3
"""Build and seal fes.pc386 with Quartus 17.0.2, the pc386 bring-up lane.

docs/pc386-machine-design.md, phase M5 (= CPU Phase 3): pc386_system, the
machine (AT core, VGA, capture, presenter, the display plane on DDR port 2,
OPL2, Sound Blaster DSP, mixer) with fes_i386_cpu, its L1s and bus interface
on DDR port 0, behind fes.computer 1.0 with the session-display plane, HPS
DDR and 48 kHz audio. The Bochs BIOS and the LGPL VGABios are M10K contents
of the RBF (pc386_rom.sv), from the images scripts/build_pc386_firmware.py
builds and firmware.lock pins. Until the M0 contracts, the HPS stages the
disk and the machine's configuration in the DDR window (pc386_host_link.sv).
The system clock is sealed at SYSTEM_CLOCK_HZ and every guest time base is a
rational enable of it, set here from that frequency. It is a development
package, labelled a Quartus build (docs/design/quartus-cores.md) and not
registered in the FES recipe table; this recipe never programs hardware.

Quartus 17.0.2's SystemVerilog front end rejects some of the RTL (the OPL2
import's casts, the CPU's generate loops), so the pc386 lock's Yosys
elaborates everything below the board shell, pc386_system, with the guest
rates and the firmware. Memories with more than one write port and no
ramstyle become registers there: Yosys writes their ports as separate always
blocks, which Quartus rejects as multiple drivers, and Quartus infers no RAM
from them either. Yosys's flattened names (\\a.b[3]) become plain
identifiers, since Quartus 17.0.2's synthesis stops with an internal error on
them. Quartus reads that netlist and the shell's own files, top.sv with
QUARTUS defined among them, and does all synthesis, placement and routing.
Every timing analysis must pass on every clock at the slow and fast corners,
or the build publishes nothing.

  QUARTUS_ROOTDIR=/abs/intelFPGA_lite/17.0/quartus \\
    python3 scripts/build_fes_pc386_quartus.py --cache-root /abs/out/cache/misteross-toolchains
"""
from __future__ import annotations

import argparse
from fractions import Fraction
import hashlib
import json
import math
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts import build_pc386_firmware as firmware
from scripts import fes_build_common as board
from scripts import source_provenance
from scripts.core_package import MAX_PAYLOAD_SIZE, encode_manifest
from scripts.export_core_package import build_identity, encode_build_record, export_package
from scripts.quartus_tools import RebuildError, locate_quartus, quartus_version_line

ROOT = Path(__file__).resolve().parents[1]
RECIPE = "scripts/build_fes_pc386_quartus.py"
OUTPUT = Path("build/fes-pc386-quartus")
LOCK = "cores/fes-pc386/toolchain.lock"
TOOLCHAIN_ROOT = "build/toolchain/fes-pc386"
QSF = "cores/fes-pc386/constraints-oss.qsf"
SDC = "cores/fes-pc386/clocks.sdc"
ABI_DEFINITION = "cores/fes-common/generated/fes_computer.vh"
FIRMWARE_LOCK = "cores/fes-pc386/firmware/firmware.lock"
FIRMWARE_RECIPE = "scripts/build_pc386_firmware.py"
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
INCLUDE_DIRS = ("cores/fes-common/generated", "cores/fes-common/vendor/kf8259", "cores/fes-common/vendor/kf8253")
OPL2 = "cores/fes-pc386/vendor/opl2_fpga"
I386 = "cores/fes-common/rtl/i386"
# Quartus reads these directly: the shell, its PLLs and the hard-block users.
SHELL_SOURCES = (
    "cores/fes-pc386/rtl/pc386_system_pll.v",
    "cores/fes-common/rtl/pixel_pll.v",
    "cores/fes-common/rtl/fes_computer_mailbox.v",
    "cores/fes-common/rtl/fes_audio_output.v",
    "cores/fes-common/rtl/fes_audio_i2s.v",
    "cores/fes-common/rtl/fes_hps_ddr.v",
    "cores/fes-common/rtl/fes_hps_ddr_guard.v",
    "cores/fes-pc386/rtl/top.sv",
)
# Yosys elaborates these into one netlist below the shell.
MACHINE_SOURCES = (
    *(f"{I386}/{name}.sv" for name in (
        "fes_i386_pkg", "fes_i386_oplen", "fes_i386_lendec", "fes_i386_lstage", "fes_i386_fetchbuf",
        "fes_i386_qalign", "fes_i386_frontend", "fes_i386_amode", "fes_i386_dfwd", "fes_i386_agu",
        "fes_i386_ucode_pkg", "fes_i386_ucode_rom", "fes_i386_decode", "fes_i386_seq", "fes_i386_dops",
        "fes_i386_shift", "fes_i386_shflags", "fes_i386_exctl", "fes_i386_muldiv", "fes_i386_ex",
        "fes_i386_l1i", "fes_i386_l1d", "fes_i386_biu", "fes_i386_cpu")),
    *(f"cores/fes-common/vendor/kf8259/{name}.sv" for name in (
        "KF8259", "KF8259_Bus_Control_Logic", "KF8259_Control_Logic", "KF8259_In_Service",
        "KF8259_Interrupt_Request", "KF8259_Priority_Resolver")),
    *(f"cores/fes-common/vendor/kf8253/{name}.sv" for name in ("KF8253", "KF8253_Control_Logic", "KF8253_Counter")),
    "cores/fes-common/rtl/fes_dma8237.sv",
    *(f"cores/fes-pc386/rtl/{name}.sv" for name in (
        "pc386_rate", "pc386_io", "pc386_pic", "pc386_sysctl", "pc386_kbc", "pc386_rtc", "pc386_ata",
        "pc386_at_core", "pc386_ram", "pc386_vga", "pc386_vga_scanout", "pc386_presenter", "pc386_plane_bridge",
        "pc386_port2", "pc386_machine")),
    "cores/fes-common/rtl/fes_display_plane.v",
    "cores/fes-common/rtl/fes_display_cdc.v",
    "cores/fes-menu/rtl/fes_menu_control.v",
    "cores/fes-menu/rtl/fes_menu_video.v",
    "cores/fes-menu/rtl/fes_menu_reader.v",
    f"{OPL2}/opl2_pkg.sv",
    *(f"{OPL2}/{name}.sv" for name in (
        "pipeline_sr", "mem_simple_dual_port", "mem_simple_dual_port_async_read", "mem_single_bank",
        "mem_single_bank_reset", "opl2_log_sine_lut", "opl2_exp_lut", "ksl_add_rom", "tremolo", "vibrato",
        "calc_phase_inc", "calc_envelope_shift", "calc_rhythm_phase", "envelope_generator", "phase_generator",
        "operator", "control_operators", "dac_prep", "channels")),
    *(f"cores/fes-pc386/rtl/{name}.sv" for name in (
        "pc386_opl2", "pc386_sb", "pc386_mixer", "pc386_audio", "pc386_rom", "pc386_host_link", "pc386_system")),
)
MACHINE_TOP = "pc386_system"
MACHINE_NETLIST = "pc386_system.v"
# pc386_rom.sv's ROM_HEX, read by Yosys from the repository root.
ROM_HEX = firmware.ROM_HEX.relative_to(ROOT).as_posix()
PINNED_INPUTS = (
    RECIPE, FIRMWARE_RECIPE, FIRMWARE_LOCK, "scripts/quartus_tools.py", "scripts/fes_build_common.py",
    "scripts/source_provenance.py", ABI_DEFINITION, LOCK, QSF, SDC, *INCLUDES, *SHELL_SOURCES, *MACHINE_SOURCES,
)
BUILD_OUTPUTS = (
    "core.rbf", "top.sta.rpt", "top.fit.rpt", "yosys.log", "quartus.log", "quartus-version.log",
    "build-inputs.json", "build-summary.json", "manifest.toml",
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
# Keywords a rewritten name could spell, since only "_" replaces characters.
UNDERSCORE_KEYWORDS = frozenset({
    "always_comb", "always_ff", "always_latch", "accept_on", "reject_on", "s_always", "s_eventually",
    "s_nexttime", "s_until", "s_until_with", "sync_accept_on", "sync_reject_on", "until_with", "wait_order",
    "pulsestyle_ondetect", "pulsestyle_onevent", "first_match", "ignore_bins", "illegal_bins", "join_any",
    "join_none",
})
CORE = {"id": "fes.pc386", "name": "FES pc386", "version": "0.1.0",
        "description": "A 386 AT: fes_i386_cpu, the Bochs BIOS and LGPL VGABios, VGA at 720p, OPL2 and Sound "
                       "Blaster 2.0. Development only: the HPS stages the disk and the machine's configuration "
                       "in the DDR window, and there is no keyboard or mouse yet."}
INTERFACES = ("fes.video.fixed-720p60", "fes.audio.pcm-s16-stereo-48k", "fes.memory.hps-ddr",
              "fes.video.session-display", "fes.fabric.video.display-plane")
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


def _authenticate_tools(root: Path, *, cache_root: Path | None = None) -> dict[str, str]:
    """The lock's Yosys and the Quartus install, each by digest."""
    yosys = board._authenticate_tools(root, lock_path=root / LOCK, toolchain_root=root / TOOLCHAIN_ROOT,
                                      cache_root=cache_root)["yosys"]
    try:
        quartus_root, quartus_sh = locate_quartus(os.environ.get("QUARTUS_ROOTDIR", ""), root)
        version = quartus_version_line(quartus_sh)
    except RebuildError as exc:
        raise board.BuildError(str(exc)) from exc
    return {
        "yosys": yosys,
        "quartus_root": quartus_root,
        "quartus_sh": quartus_sh,
        "quartus_version": version,
        "identities": {
            "yosys": yosys.identity,
            "quartus_sh": f"version=17.0.2; sha256={board._sha256(quartus_sh)}",
        },
    }


def require_clean_source(root: Path) -> tuple[str, str]:
    return board._require_clean_source(root, pinned_inputs=PINNED_INPUTS)


def _check_seed(seed: int) -> int:
    if type(seed) is not int or not 1 <= seed <= SEED_LIMIT:
        raise board.BuildError(f"Quartus seed must be an integer from 1 to {SEED_LIMIT}")
    return seed


def machine_program(netlist: Path, sources: str | None = None) -> str:
    """The Yosys script that elaborates the machine at the sealed guest rates. pc386_rom
    reads ROM_HEX while it is read in. sources stands in for MACHINE_SOURCES."""
    rates = " ".join(f"-chparam {name} {value}" for name, value in guest_rates().items())
    includes = " ".join(f"-I {path}" for path in INCLUDE_DIRS)
    return (
        f"read_verilog -sv {includes} {' '.join(MACHINE_SOURCES) if sources is None else sources}; "
        f"hierarchy -top {MACHINE_TOP} {rates}; proc -norom; flatten; opt -purge; memory_collect; "
        "memory_map t:$mem_v2 r:WR_PORTS>1 %i a:ramstyle %d; opt -purge; "
        "attrmap -remove src; attrmap -remove hdlname; "
        f"write_verilog {netlist.as_posix()}"
    )


def plain_identifiers(verilog: str) -> str:
    """Rewrite escaped identifiers that hold hierarchy or index characters as
    plain ones, unique within the netlist. A name escaped only because it is a
    keyword (\\always_ff) and string literals (attribute values) stay."""
    taken = (set(re.findall(r"(?<![\\\w$])[A-Za-z_][A-Za-z0-9_$]*", verilog))
             | set(re.findall(r"\\([A-Za-z_][A-Za-z0-9_$]*)(?=\s)", verilog)) | UNDERSCORE_KEYWORDS)
    names: dict[str, str] = {}

    def plain(match: re.Match) -> str:
        name = match.group(1)
        if name is None or re.fullmatch(r"[A-Za-z_][A-Za-z0-9_$]*", name):
            return match.group(0)
        if name not in names:
            base = re.sub(r"[^A-Za-z0-9_]", "_", name)
            if not re.match(r"[A-Za-z_]", base):
                base = "_" + base
            candidate, suffix = base, 0
            while candidate in taken:
                suffix += 1
                candidate = f"{base}_{suffix}"
            taken.add(candidate)
            names[name] = candidate
        return names[name]

    return re.sub(r'"(?:[^"\\\n]|\\.)*"|\\(\S+)', plain, verilog)


def firmware_digests(root: Path) -> dict[str, str]:
    """The BIOS and VGA BIOS digests firmware.lock pins: the ROM's contents."""
    lock = json.loads(board._regular_input(root, FIRMWARE_LOCK).read_text(encoding="utf-8"))
    return {name: lock["images"][name]["sha256"] for name in firmware.IMAGES}


def record_fields(root: Path, repository: str, revision: str, identities: dict[str, str], seed: int) -> dict:
    # A format-1 record names its files from the repository root.
    qualify = source_provenance.context(root).qualify
    return {
        "format": 1, "repository": repository, "revision": revision,
        "recipe": qualify(RECIPE), "recipe_sha256": board._sha256(board._regular_input(root, RECIPE)),
        "abi_definition": qualify(ABI_DEFINITION),
        "abi_definition_sha256": board._sha256(board._regular_input(root, ABI_DEFINITION)),
        "dependencies": {}, "tools": dict(identities),
        "parameters": {
            "compiler": "quartus-17.0.2", "device": board.TARGET, "top": board.TOP, "seed": _check_seed(seed),
            "toolchain_lock": LOCK, "toolchain_lock_sha256": board._sha256(board._regular_input(root, LOCK)),
            "system_clock_hz": SYSTEM_CLOCK_HZ, "pixel_clock_hz": 74_250_000, "audio_clock_hz": 12_288_000,
            "reference_clock_hz": 50_000_000, "audio_sample_hz": 48_000,
            "guest_rates": json.dumps(guest_rates(), sort_keys=True, separators=(",", ":")),
            "cpu": "fes_i386_cpu, its L1I, L1D and bus interface on DDR port 0 at window offset 0x04000000",
            "firmware": json.dumps(firmware_digests(root), sort_keys=True, separators=(",", ":")),
            "host_link": "pc386_host_link configuration, status and debug blocks at window offset 0x01000000",
            # The script with its source list named, which is too long for a parameter.
            "machine_netlist": machine_program(Path(MACHINE_NETLIST), "$MACHINE_SOURCES"),
            "machine_sources_sha256": hashlib.sha256("\n".join(MACHINE_SOURCES).encode()).hexdigest(),
        },
    }


def create_build_record(root: Path, repository: str, revision: str, identities: dict[str, str], seed: int) -> bytes:
    return encode_build_record(record_fields(root, repository, revision, identities, seed))


def project_qsf(root: Path, project: Path, build_id: str, seed: int) -> str:
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
        "set_global_assignment -name NUM_PARALLEL_PROCESSORS ALL",
        f"set_global_assignment -name SEED {_check_seed(seed)}",
        "set_global_assignment -name VERILOG_INPUT_VERSION SYSTEMVERILOG_2005",
        'set_global_assignment -name LAST_QUARTUS_VERSION "17.0.2 Lite Edition"',
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


def write_project(root: Path, output: Path, build_id: str, seed: int) -> Path:
    project = output / "project"
    if project.is_symlink():
        raise board.BuildError(f"Quartus project must not be a symlink: {project}")
    if project.exists():
        shutil.rmtree(project)
    project.mkdir()
    (project / "top.qpf").write_text('QUARTUS_VERSION = "17.0";\nPROJECT_REVISION = "top";\n', encoding="utf-8")
    (project / "top.qsf").write_text(project_qsf(root, project, build_id, seed), encoding="utf-8")
    return project


def firmware_rom(root: Path) -> Path:
    """The locked images as pc386_rom's contents, where Yosys reads them."""
    try:
        paths = firmware.images()
        if any(board._sha256(path) != digest for path, digest in
               ((paths[name], firmware_digests(root)[name]) for name in firmware.IMAGES)):
            raise board.BuildError("firmware images do not match firmware.lock")
        rom = firmware.write_rom_hex(paths)
    except firmware.FirmwareError as exc:
        raise board.BuildError(f"firmware: {exc}") from exc
    if rom.resolve() != (root / ROM_HEX).resolve():
        raise board.BuildError(f"the ROM contents must be at {ROM_HEX}")
    return rom


def elaborate_machine(root: Path, output: Path, project: Path, yosys: Path) -> None:
    raw = output / "machine.yosys.v"
    result = subprocess.run(
        [str(yosys), "-q", "-l", str(output / "yosys.log"), "-p", machine_program(raw)],
        cwd=root, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, check=False,
    )
    if result.returncode != 0:
        raise board.BuildError(f"Yosys could not elaborate {MACHINE_TOP} (exit {result.returncode}); "
                               f"log: {output / 'yosys.log'}")
    netlist = plain_identifiers(raw.read_text(encoding="utf-8"))
    if f"module {MACHINE_TOP}(" not in netlist:
        raise board.BuildError(f"elaborated netlist does not define {MACHINE_TOP}")
    (project / MACHINE_NETLIST).write_text(netlist, encoding="utf-8")
    raw.unlink()


def run_quartus(quartus_sh: Path, quartus_root: Path, project: Path, log: Path) -> None:
    env = os.environ.copy()
    env["QUARTUS_ROOTDIR"] = str(quartus_root)
    result = subprocess.run(
        [str(ROOT / "scripts" / "run_logged.sh"), str(log), str(quartus_sh), "--flow", "compile", board.TOP],
        cwd=project, env=env, check=False,
    )
    if result.returncode != 0:
        raise board.BuildError(f"Quartus compile failed (exit {result.returncode}); log: {log}")


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


def toolchain_label(identities: dict[str, str]) -> str:
    return (f"Quartus Prime Lite 17.0.2 ({identities['quartus_sh']}); "
            f"machine elaborated by Yosys ({identities['yosys']})")


def manifest(record: bytes, evidence: dict, repository: str, revision: str, identities: dict[str, str]) -> bytes:
    core = dict(CORE)
    core["name"] = f"{CORE['name']} (Quartus)"
    core["description"] = f"Quartus bring-up build. {CORE['description']}"
    return encode_manifest({
        "format": 2,
        "core": core,
        "target": {"platform": "de10_nano", "device": board.TARGET, "programming_profile": "fes-gp-v1"},
        "payload": {"file": "core.rbf", **evidence["rbf"]},
        "abi": {"id": "fes.computer", "major": 1, "minor": 0},
        "interfaces": [{"id": name, "major": 1, "minor": 0, "required": True} for name in INTERFACES],
        "build": {"id": build_identity(record), "repository": repository, "revision": revision,
                  "recipe_sha256": json.loads(record)["recipe_sha256"], "toolchain": toolchain_label(identities)},
    })


def build(root: Path = ROOT, package_store=None, *, cache_root: Path | None = None, seed: int = 1) -> Path:
    _check_seed(seed)
    root = Path(root).resolve()
    package_store = root / "build/packages" if package_store is None else Path(package_store).resolve()
    if package_store != root / "build/packages":
        raise board.BuildError("fes.pc386 package store must be build/packages")
    repository, revision = require_clean_source(root)
    tools = _authenticate_tools(root, cache_root=cache_root)
    identities = tools["identities"]
    output = None
    try:
        record = create_build_record(root, repository, revision, identities, seed)
        build_id = build_identity(record)
        output = board._prepare_output(root, relative=OUTPUT, build_outputs=BUILD_OUTPUTS)
        board._write_atomic(output / "build-inputs.json", record)
        project = write_project(root, output, build_id, seed)
        rom = firmware_rom(root)
        elaborate_machine(root, output, project, tools["yosys"].path)
        run_quartus(tools["quartus_sh"], tools["quartus_root"], project, output / "quartus.log")
        board._write_atomic(output / "quartus-version.log", (tools["quartus_version"] + "\n").encode())
        evidence = validate_build_evidence(output, project)
        evidence.update({"build_id": build_id, "device": board.TARGET, "top": board.TOP, "seed": seed,
                         "firmware": {**firmware_digests(root), "rom_hex": board._sha256(rom)},
                         "inputs": {p: board._sha256(root / p) for p in sorted(PINNED_INPUTS)},
                         "tools": identities, "toolchain": toolchain_label(identities)})
        board._write_atomic(output / "build-summary.json",
                            (json.dumps(evidence, indent=2, sort_keys=True) + "\n").encode())
        encoded = manifest(record, evidence, repository, revision, identities)
        board._write_atomic(output / "manifest.toml", encoded)
        if _authenticate_tools(root, cache_root=cache_root)["identities"] != identities:
            raise board.BuildError("authenticated tool identity changed during build")
        if require_clean_source(root) != (repository, revision):
            raise board.BuildError("source identity changed during build")
        if create_build_record(root, repository, revision, identities, seed) != record:
            raise board.BuildError("source inputs changed during build")
        return export_package(encoded, output / "core.rbf", package_store)
    except Exception:
        if output is not None:
            board._invalidate_failed_artifact(output)
        raise


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--root", type=Path, default=ROOT)
    parser.add_argument("--cache-root", type=Path)
    parser.add_argument("--package-output", type=Path)
    parser.add_argument("--seed", type=int, default=1, help="Quartus fitter seed; a timing miss fails")
    args = parser.parse_args()
    try:
        print(build(args.root, args.package_output, cache_root=args.cache_root, seed=args.seed))
    except (board.BuildError, OSError, ValueError) as exc:
        print(f"FES pc386 (Quartus): {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
