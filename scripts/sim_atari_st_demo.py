#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-or-later
"""Run an unchanged raw-ST demo disk on actual FX68K/pinned ROM/chipset RTL.

This is a bounded loader diagnostic with model ROM/RAM/disk storage, not a
physical SDRAM, mailbox, HDMI, or raster-effects test by default.
--shared-memory selects the existing digital SDRAM/independent pixel-clock
fixture with a preloaded disk; it does not establish electrical equivalence. No demo success oracle is
implied. Per-second RAM/PPM snapshots, CPU/FDC trace and disk accesses aid diagnosis.
Requires Verilator/C++17 and an independently supplied 192 KiB ROM. The default
ROM pin selects stock EmuTOS192US1.4; --rom-sha256 selects another exact image.

Default compilation requires source bytes to match --revision. --working-tree
explicitly captures current diagnostic edits and records every deviation from
that revision. All compiled sources and input identities are frozen before build.
"""
from __future__ import annotations
import argparse
from decimal import Decimal, InvalidOperation
from fractions import Fraction
import hashlib
import json
import re
from pathlib import Path
import shutil
import subprocess
import threading
import zipfile

ROOT = Path(__file__).resolve().parents[1]
PREFIX = "sources/misteross/"
CORE = "cores/fes-atari-st/"
CPU = "cores/fes-common/rtl/fx68k/"
ROM_SHA256 = "8fbbf8b44fc3e34281eaf8cda5265510e9af9ccda0e3e409111648060d244cfc"
FLAGS = ["-Wno-" + name for name in (
    "WIDTH", "TIMESCALEMOD", "MULTIDRIVEN", "INITIALDLY", "ALWCOMBORDER", "UNOPTFLAT",
    "BLKANDNBLK", "CASEINCOMPLETE", "CASEOVERLAP")]
RTL = [CORE + "rtl/" + name + ".sv" for name in (
    "st_system", "st_cpu", "st_machine", "st_io", "st_mfp", "st_acia", "st_ikbd",
    "st_ym2149", "st_ym_filter", "st_ym_mixer", "st_ym_mix_rom", "st_floppy", "st_floppy_writer", "st_native_border", "st_native_low_video")]
RTL += ["cores/fes-zx81/expansions/zonx_ay.v"]
RTL += [CPU + name for name in ("fx68k.sv", "fx68kAlu.sv", "uaddrPla.sv")]
DATA = [CPU + "microrom.mem", CPU + "nanorom.mem", "cores/fes-common/generated/fes_video_part.vh"]
WRAPPER = PREFIX + CORE + "sim/st_boot_sim_top.sv"
HELPERS = ["scripts/sim_atari_st_demo.py", "scripts/atari_st_demo_sim.cpp", PREFIX + CORE + "sim/st_audio_capture.hpp", PREFIX + CORE + "sim/st_keyboard_stimulus.hpp"]


def sha(data):
    return hashlib.sha256(data).hexdigest()


def write_json(path, record):
    path.write_text(json.dumps(record, indent=2, sort_keys=True) + "\n")


def archive(path, root, names):
    with zipfile.ZipFile(path, "w", zipfile.ZIP_DEFLATED) as output:
        for name in sorted(names):
            output.write(root / name, name)


def disk_geometry(data):
    matches = [(tracks, heads, sectors) for tracks in range(80, 83)
               for heads in (1, 2) for sectors in (9, 10)
               if len(data) == tracks * heads * sectors * 512]
    if len(matches) != 1:
        raise ValueError("disk must be raw-ST with 80..82 tracks, 1..2 sides, 9..10 sectors")
    tracks, heads, sectors = matches[0]
    return {"tracks": tracks, "heads": heads, "sectors_per_track": sectors,
            "sector_bytes": 512, "bytes": len(data)}


SYSTEM_HZ = 52224000
MAX_KEY_EVENTS = 256
RELEASE_GAP_CYCLES = SYSTEM_HZ * 2 // 1000


def keyboard_schedule(specifications, seconds):
    """Normalize exact decimal times to the first system clock at/after each edge."""
    if len(specifications) > MAX_KEY_EVENTS:
        raise ValueError("at most 256 keyboard events are allowed")
    schedule = []
    for specification in specifications:
        if len(specification) > 256:
            raise ValueError("key event specification too long")
        fields = specification.split(":")
        if len(fields) not in (2, 3):
            raise ValueError("key event must be SECOND:HID[:HOLD_MS]")
        try:
            start = Decimal(fields[0])
            usage = int(fields[1])
            hold = Decimal(fields[2]) if len(fields) == 3 else Decimal(150)
        except (InvalidOperation, ValueError):
            raise ValueError("key event has invalid time, HID or hold") from None
        if not start.is_finite() or not hold.is_finite():
            raise ValueError("key event times must be finite")
        if not 0 <= start < seconds or not 0 < hold <= seconds * 1000 or not 4 <= usage <= 127:
            raise ValueError("key event outside time, hold or HID bounds")
        if start.as_tuple().exponent < -64 or hold.as_tuple().exponent < -64:
            raise ValueError("key event times allow at most 64 fractional places")
        start_exact = Fraction(start)
        end_exact = start_exact + Fraction(hold) / 1000
        def clock(time):
            scaled = time * SYSTEM_HZ
            return (scaled.numerator + scaled.denominator - 1) // scaled.denominator
        press, release = clock(start_exact), clock(end_exact)
        if release <= press or release >= seconds * SYSTEM_HZ:
            raise ValueError("key event must press and release before the run ends")
        if schedule and press - schedule[-1]["release_cycle"] < RELEASE_GAP_CYCLES:
            raise ValueError("ordered key events need at least a 2 ms release gap")
        schedule.append({"press_second": str(start), "hold_milliseconds": str(hold),
                         "hid_usage": usage, "press_cycle": press, "release_cycle": release})
    return schedule


def schedule_bytes(schedule):
    return "".join(f"{e['press_cycle']}\t{e['release_cycle']}\t{e['hid_usage']}\n"
                   for e in schedule).encode("ascii")


def ram_write_ranges(values):
    if len(values) > 16:
        raise ValueError("at most 16 RAM write ranges")
    ranges = []
    for value in values:
        fields = value.split(":")
        if len(fields) != 2 or len(value) > 64:
            raise ValueError("RAM write range must be START:END byte addresses")
        try:
            start, end = (int(v, 16 if v.lower().startswith("0x") else 10) for v in fields)
        except ValueError as error:
            raise ValueError("RAM write addresses must be decimal or hexadecimal") from error
        if not 0 <= start < end <= 524288 or (start | end) & 1:
            raise ValueError("RAM write ranges must be word-aligned within 512 KiB")
        ranges.append((start, end))
    ranges.sort()
    if any(b[0] < a[1] for a, b in zip(ranges, ranges[1:])):
        raise ValueError("RAM write ranges must not overlap")
    return ranges


def run(args):
    rtl = list(RTL)
    wrapper = WRAPPER
    helpers = list(HELPERS)
    top = "st_boot_sim_top"
    if args.shared_memory:
        top = "st_boot_memory_sim_top"
        wrapper = PREFIX + CORE + "sim/" + top + ".sv"
        helpers[1] = PREFIX + CORE + "sim/boot_memory_tb.cpp"
        helpers.append(PREFIX + CORE + "sim/st_rendered_capture.hpp")
        helpers.append(PREFIX + CORE + "sim/st_ram_write_trace.hpp")
        rtl += [CORE + "sim/st_memory_sim_top.sv", CORE + "rtl/st_memory.sv",
                CORE + "rtl/st_video.sv", CORE + "rtl/st_video_adapter.sv",
                "cores/fes-ramtest/rtl/sdram_addon_port.v", "cores/fes-common/rtl/fes_video_part_direct.v"]
    revision = subprocess.check_output(["git", "rev-parse", args.revision + "^{commit}"], cwd=ROOT, text=True).strip()
    rom = args.rom.read_bytes()
    if len(rom) != 196608 or sha(rom) != args.rom_sha256:
        raise ValueError("ROM must be 192 KiB and match --rom-sha256")
    with args.disk.open("rb") as stream:
        disk = stream.read(839681)
    geometry = disk_geometry(disk)
    verilator = shutil.which(args.verilator)
    if verilator is None:
        raise ValueError("Verilator unavailable")
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=False)
    source, build = output / "source", output / "build"
    source.mkdir(); build.mkdir()
    identities, selected, differences = {}, {}, {}
    for relative in [PREFIX + name for name in rtl + DATA] + [wrapper] + helpers:
        result = subprocess.run(["git", "show", revision + ":" + relative], cwd=ROOT, capture_output=True)
        if result.returncode and not args.working_tree:
            raise ValueError(f"selected revision lacks compiled input: {relative}")
        committed = None if result.returncode else result.stdout
        working = (ROOT / relative).read_bytes()
        selected[relative] = None if committed is None else sha(committed)
        if working != committed:
            if not args.working_tree:
                raise ValueError(f"working compiled input differs from selected revision: {relative}")
            differences[relative] = {"revision_sha256": selected[relative], "captured_sha256": sha(working)}
        data = working if args.working_tree else committed
        identities[relative] = sha(data)
        destination = source / relative
        destination.parent.mkdir(parents=True, exist_ok=True)
        destination.write_bytes(data)
    (output / "etos192us.img").write_bytes(rom)
    (output / "original.st").write_bytes(disk)
    for name in ("microrom.mem", "nanorom.mem"):
        shutil.copyfile(source / PREFIX / CPU / name, build / name)
    schedule_path = output / "keyboard-events.tsv"
    schedule_data = schedule_bytes(args.input_schedule)
    schedule_path.write_bytes(schedule_data)
    archive(output / "frozen-source.zip", source, identities)
    command = [verilator, "--cc", "--exe", "--build", "-O2", "-j", "2", "--top-module", top]
    command += [f"-GRAW_MFP_IACK={int(args.mfp_iack_raw_pin)}", f"-GMFP_IACK_PHASE={args.mfp_iack_phase}", f"-GMFP_WAIT_STATES={args.mfp_wait_states}", f"-GPSG_WAIT_STATES={args.psg_wait_states}"]
    command += FLAGS + ["-I" + str(source / PREFIX / "cores/fes-common/generated"), "--Mdir", str(build)]
    command += [str(source / wrapper)] + [str(source / PREFIX / name) for name in rtl]
    command += [str(source / helpers[1]), "-CFLAGS", "-O3 -std=c++17"]
    record = {"schema": 1, "source_revision": revision, "source_mode": "working-tree diagnostic" if args.working_tree else "selected revision",
              "selected_revision_inputs": selected, "captured_inputs": identities, "working_tree_differences": differences,
              "rom_sha256": sha(rom), "expected_rom_sha256": args.rom_sha256, "original_disk_sha256": sha(disk), "disk_geometry": geometry,
              "source_archive_sha256": sha((output / "frozen-source.zip").read_bytes()),
              "build_command": command, "verilator_version": subprocess.check_output([verilator, "--version"], text=True).strip(),
              "seconds_requested": args.seconds, "hardware_execution": False, "native_fpga_build": False,
              "demo_compatibility_asserted": False, "framebuffer_capture": "static RAM/palette reconstruction; no raster or border proof",
              "native_rgb_capture": "production low-resolution capture observed with model RAM/system-clock consumer; no physical SDRAM, HDMI or original GLUE/border oracle",
              "media_fixture": "read-only bounded disk callbacks; real FX68K/pinned ROM/floppy/DMA RTL",
              "capture_completed": False}
    write_json(output / "source-identity.json", record)
    print(f"Compiling frozen loader diagnostic at {revision}; dirty inputs={len(differences)}", flush=True)
    with (output / "build.log").open("w") as log:
        subprocess.run(command, cwd=source / PREFIX, stdout=log, stderr=subprocess.STDOUT, check=True, timeout=300)
    executable = build / ("V" + top)
    record["executable_sha256_before"] = sha(executable.read_bytes())
    microcode_hashes = {name: sha((build / name).read_bytes()) for name in ("microrom.mem", "nanorom.mem")}
    record["runtime_microcode_inputs"] = microcode_hashes
    models = [str(path.relative_to(build)) for path in build.iterdir() if path.suffix in (".cpp", ".h")]
    archive(output / "generated-model.zip", build, models)
    record["generated_model_sha256"] = sha((output / "generated-model.zip").read_bytes())
    model_hashes = {name: sha((build / name).read_bytes()) for name in models}
    record["generated_model_inputs"] = model_hashes
    invocation = [str(executable), str(output / "etos192us.img"), str(output / "original.st"),
                  str(args.seconds), str(output / "demo"), str(args.key_at or 0),
                  str(args.trace_start), str(args.trace_end), str(args.ram_extra_wait),
                  str(-1 if args.ram_fixed_wait is None else args.ram_fixed_wait), str(args.key_usage)]
    if args.shared_memory:
        invocation = [str(executable), str(output / "etos192us.img"), str(args.seconds),
                      str(output / "demo"), str(output / "original.st"), str(args.trace_start), str(args.trace_end),
                      str(args.key_at or 0), str(args.key_usage)]
        record["framebuffer_capture"] = "fixed 720p60 output after Direct part boundaries"
        record["native_rgb_capture"] = "production capture under shared SDRAM commands and independent 74.25 MHz pixel clock; no analog/electrical or original GLUE equivalence"
        record["media_fixture"] = "original disk preloaded into separate SDRAM disk buffer; production floppy/DMA/memory RTL; upload/mailbox not exercised"
    if args.key_event or args.ram_write_ranges:
        invocation.append(str(schedule_path))
    ram_ranges_path = output / "ram-write-ranges.tsv"
    ram_ranges_data = "".join(f"{a}\t{b}\n" for a,b in args.ram_write_ranges).encode()
    if args.ram_write_ranges:
        ram_ranges_path.write_bytes(ram_ranges_data)
        invocation.append(str(ram_ranges_path))
        record["ram_write_trace"] = {"file": "demo-ram-writes.jsonl",
            "ranges": args.ram_write_ranges, "ranges_file": ram_ranges_path.name,
            "ranges_sha256": sha(ram_ranges_data),
            "window_seconds": [args.trace_start,args.trace_end],
            "origin": "reset release after SDRAM initialization",
            "phase": "physical WRITE command before system rising edge",
            "scope": "all physical writes in selected byte ranges, including CPU and DMA; repeated values retained",
            "byte_enable": "bit1 high byte, bit0 low byte",
            "pc": "current diagnostic prefetch state; not retirement or writer identity"}
    record["keyboard_schedule"] = {"events": args.input_schedule, "system_clock_hz": SYSTEM_HZ,
        "file": schedule_path.name, "sha256": sha(schedule_data),
        "origin": "reset release after SDRAM initialization" if args.shared_memory else "callback model cycle zero (including 64 reset clocks)",
        "edges": "demo-input.jsonl; actual keyboard row transitions, not guest acknowledgement"}
    record["input_event"] = (None if args.key_at is None else
                             {"hid_usage": args.key_usage, "press_second": args.key_at,
                              "hold_milliseconds": 150})
    record["consecutive_native_frames"] = None if args.shared_memory else "first sixteen complete captures after 6.5 seconds; production capture with model RAM, no original GLUE/border oracle"
    record["raster_trace"] = {"start_second": args.trace_start, "end_second": args.trace_end,
                              "events": "IACK/peripheral request starts, sync changes and every committed palette write (including unchanged values)",
                              "pc": "diagnostic prefetch state, not instruction retirement"}
    if args.shared_memory:
        record["raster_trace"]["events"] = "IACK (including MFP vector), sync changes and every committed palette write (including unchanged values)"
    if args.shared_memory:
        record["ram_snapshots"] = {"trace": "demo-ram-snapshots.jsonl",
            "files": "demo-ram-cycle-{system_cycle}.bin", "bytes_per_snapshot": 524288,
            "scope": "read-only physical command-model RAM at per-second status events throughout the bounded guest run",
            "system_cycle": "elapsed system clocks since reset release; no guest clocks advance during the copy"}
        record["rendered_capture"] = {"trace": "demo-rendered.jsonl",
            "source": "actual complete HDMI replay frames; pixel-domain immutable front_sequence",
            "saved": "first eight distinct valid displayed native sequences whose HDMI SOF is in trace window",
            "canvas": "PAL xywh 224,84,832,552; NTSC 224,105,832,510; reduce only if all 2x2 RGB samples agree",
            "identity": "native_frame is publication epoch; hdmi_frame is consumer frame; no original GLUE equivalence claim"}
    record["distinct_logo_captures"] = "save first eight distinct crop hashes as complete native PPMs in trace window"
    record["logo_trace"] = "every completed native capture in trace window; FNV-1a-64 RGB crop xywh 65,0,180,64; equality diagnostic, not cryptographic identity"
    record["storage_model"] = {"cpu_ram_wait": "8+(byte_address%7)+ram_extra_wait system clocks",
                               "ram_extra_wait": args.ram_extra_wait, "capture_wait": "12+(byte_address%7)",
                               "shared_arbitration": False}
    if args.ram_fixed_wait is not None:
        record["storage_model"]["cpu_ram_wait"] = f"{args.ram_fixed_wait}+ram_extra_wait system clocks"
    record["storage_model"]["ram_fixed_wait"] = args.ram_fixed_wait
    if args.shared_memory:
        record["storage_model"] = {"shared_arbitration": True, "cpu_video_dma_media": "production st_memory.sv and addon controller",
                                   "sdram": "digital command/timing model with CAS2, DDIO capture and byte masks; no electrical equivalence"}
    record["storage_model"]["mfp_wait_states"] = args.mfp_wait_states
    record["storage_model"]["experimental_psg_wait_states"] = args.psg_wait_states
    record["storage_model"]["experimental_mfp_iack_phase"] = args.mfp_iack_phase
    record["storage_model"]["experimental_mfp_iack_raw_pin"] = args.mfp_iack_raw_pin
    record["horizontal_phase_units"] = "68000 cycles within the native line"
    record["palette_trace"] = "changes after six seconds; first eight changed bundles per native frame, plus complete per-frame counts; model timing only"
    record["run_command"] = invocation
    record["timeout_seconds"] = args.timeout_seconds
    record["audio_capture"] = "system-domain signed mono 48 kHz production YM PCM, with nonlinear mixing/STF C10 low-pass/rational integration/DC removal; no audio-fidelity oracle"
    # Read the frozen selection, including historical revisions whose system
    # used the machine's nominal default. Never label a new capture as 2 MHz
    # after changing the selected board's clock policy.
    system_rtl = (source / (PREFIX + CORE + "rtl/st_system.sv")).read_text()
    clock_match = re.search(r"parameter integer CPU_CLOCK_HZ\s*=\s*([0-9_]+)\s*,", system_rtl)
    if "parameter integer CPU_CLOCK_HZ" in system_rtl and clock_match is None:
        raise ValueError("unsupported selected CPU clock policy")
    cpu_clock_hz = int(clock_match.group(1).replace("_", "")) if clock_match else 8000000
    record["machine_clocks"] = {"cpu_hz": cpu_clock_hz,
                                 "psg_hz": cpu_clock_hz // 4,
                                 "acia_hz": cpu_clock_hz // 16,
                                 "mfp_crystal_hz": 2457600}
    if args.shared_memory and b'debug_ym_write' in (source / wrapper).read_bytes():
        record["ym_trace"] = {"file": "demo-ym.jsonl", "system_clock_hz": 52224000,
                              "chip_clock_hz": record["machine_clocks"]["psg_hz"],
                              "scope": "all valid-register PSG data acceptance edges during guest run; raw values before register masking",
                              "fidelity_asserted": False}
    process = subprocess.Popen(invocation, cwd=build, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
    timer = threading.Timer(args.timeout_seconds, process.kill); timer.start()
    try:
        with (output / "demo.log").open("w") as log:
            for line in process.stdout:
                log.write(line); log.flush(); print(line.rstrip(), flush=True)
        record["exit_code"] = process.wait()
    finally:
        timer.cancel()
        if process.poll() is None:
            process.kill(); process.wait()
    record["frozen_inputs_unchanged"] = all(sha((source / name).read_bytes()) == digest for name, digest in identities.items())
    record["working_inputs_unchanged"] = all(sha((ROOT / name).read_bytes()) == digest for name, digest in identities.items())
    record["keyboard_schedule_unchanged"] = schedule_path.read_bytes() == schedule_data
    record["ram_write_ranges_unchanged"] = not args.ram_write_ranges or ram_ranges_path.read_bytes() == ram_ranges_data
    record["rom_unchanged"] = sha((output / "etos192us.img").read_bytes()) == sha(rom)
    record["original_disk_unchanged"] = (output / "original.st").read_bytes() == disk
    record["runtime_microcode_unchanged"] = all(sha((build / name).read_bytes()) == digest for name, digest in microcode_hashes.items())
    record["generated_model_unchanged"] = all(sha((build / name).read_bytes()) == digest for name, digest in model_hashes.items())
    record["executable_sha256_after"] = sha(executable.read_bytes())
    metrics = output / "demo-metrics.json"
    if metrics.exists():
        record["metrics"] = json.loads(metrics.read_text())
    record["artifacts"] = {path.name: sha(path.read_bytes()) for path in output.glob("demo*") if path.is_file()}
    record["capture_completed"] = (record["exit_code"] == 0 and metrics.exists() and record["frozen_inputs_unchanged"]
                                   and record["rom_unchanged"] and record["original_disk_unchanged"] and record["keyboard_schedule_unchanged"] and record["ram_write_ranges_unchanged"]
                                   and record["generated_model_unchanged"] and record["runtime_microcode_unchanged"] and
                                   record["executable_sha256_before"] == record["executable_sha256_after"])
    write_json(output / "demo-proof.json", record)
    print(json.dumps({"capture_completed": record["capture_completed"], "demo_compatibility_asserted": False,
                      "proof": str(output / "demo-proof.json")}), flush=True)
    return record


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--rom", type=Path, required=True)
    parser.add_argument("--rom-sha256", default=ROM_SHA256,
                        help="independently pinned 192 KiB ROM digest; defaults to stock EmuTOS 192US 1.4")
    parser.add_argument("--disk", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True, help="new ignored output directory")
    parser.add_argument("--revision", default="HEAD")
    parser.add_argument("--working-tree", action="store_true", help="explicitly freeze current diagnostic edits")
    parser.add_argument("--seconds", type=int, default=15,
                        help="1..120 guest seconds; longer runs cover multi-track playback and disk reload")
    parser.add_argument("--key-at", "--key-b-at", dest="key_at", type=int,
                        help="press selected HID usage for 150 ms at this simulated second; B by default")
    parser.add_argument("--key-event", action="append", default=[], metavar="SECOND:HID[:HOLD_MS]",
                        help="repeat up to 256 ordered presses; default hold 150 ms; 2 ms release gap; time zero allowed")
    parser.add_argument("--key-usage", type=int, help="HID key usage 4..127; B=5, 1/2/3=30/31/32, Escape=41")
    parser.add_argument("--timeout-seconds", type=int, default=3600, help="host run deadline; does not change guest timing")
    parser.add_argument("--trace-start", type=int, help="inclusive simulated second for complete raster/logo tracing")
    parser.add_argument("--trace-end", type=int, help="exclusive simulated second for complete raster/logo tracing")
    parser.add_argument("--ram-extra-wait", type=int, default=0, help="add 0..64 system clocks to each model CPU RAM access; not physical arbitration")
    parser.add_argument("--ram-fixed-wait", type=int, help="replace variable CPU RAM callback waits with a fixed 0..64 system-clock delay; an ideal-storage probe, not physical SDRAM")
    parser.add_argument("--mfp-wait-states", type=int, default=4, help="MFP native CPU wait states (default: 4, matching the standard core); use 0..8 for diagnostics")
    parser.add_argument("--mfp-iack-raw-pin", action=argparse.BooleanOptionalAction, default=True, help="start MFP clock timing at AS/FC pins (default); disable for registered-admission diagnostics")
    parser.add_argument("--mfp-iack-phase", type=int, default=1, choices=(-1,0,1,2,3), help="MFP bus-clock phase (default: 1); -1 bypasses timing for diagnostics")
    parser.add_argument("--psg-wait-states", type=int, default=4, help="GLUE sound-bus waits per FF88 burst (default: 4); 0..8 native CPU states")
    parser.add_argument("--ram-write-range", action="append", default=[], metavar="START:END",
                        help="shared-memory only: trace accepted physical writes in up to 16 word-aligned byte ranges during the trace window; decimal or hex addresses")
    parser.add_argument("--shared-memory", action="store_true", help="use the existing shared SDRAM/independent pixel-clock boot fixture; disk preloaded before reset release")
    parser.add_argument("--verilator", default="verilator")
    args = parser.parse_args(argv)
    key_usage_explicit = args.key_usage is not None
    args.key_usage = 5 if args.key_usage is None else args.key_usage
    if len(args.rom_sha256) != 64 or any(c not in "0123456789abcdef" for c in args.rom_sha256):
        parser.error("--rom-sha256 must be a lowercase SHA-256 digest")
    if not 1 <= args.seconds <= 120:
        parser.error("--seconds must be 1..120")
    args.trace_start = min(6, args.seconds-1) if args.trace_start is None else args.trace_start
    args.trace_end = min(7, args.seconds) if args.trace_end is None else args.trace_end
    if not 0 <= args.trace_start < args.trace_end <= args.seconds:
        parser.error("trace window must satisfy 0 <= start < end <= seconds")
    if not 0 <= args.ram_extra_wait <= 64:
        parser.error("--ram-extra-wait must be 0..64")
    if args.ram_fixed_wait is not None and not 0 <= args.ram_fixed_wait <= 64:
        parser.error("--ram-fixed-wait must be 0..64")
    if args.mfp_iack_raw_pin and args.mfp_iack_phase < 0:
        parser.error("--mfp-iack-raw-pin requires --mfp-iack-phase 0..3")
    if not 0 <= args.psg_wait_states <= 8:
        parser.error("--psg-wait-states must be 0..8")
    if not 0 <= args.mfp_wait_states <= 8:
        parser.error("--mfp-wait-states must be 0..8")
    if args.key_at is not None and not 1 <= args.key_at < args.seconds:
        parser.error("--key-at must be at least one and less than --seconds")
    if not 4 <= args.key_usage <= 127 or (args.key_at is None and args.key_usage != 5):
        parser.error("key usage must be 4..127 and a nondefault usage requires --key-at")
    if not 1 <= args.timeout_seconds <= 86400:
        parser.error("timeout must be 1..86400 host seconds")
    if args.shared_memory and (args.seconds < 6 or args.ram_extra_wait or args.ram_fixed_wait is not None):
        parser.error("--shared-memory requires at least 6 seconds and no callback RAM delay or override")
    if args.ram_write_range and not args.shared_memory:
        parser.error("--ram-write-range requires --shared-memory")
    if args.key_event and (args.key_at is not None or key_usage_explicit):
        parser.error("--key-event cannot be combined with --key-at or --key-usage")
    try:
        args.ram_write_ranges = ram_write_ranges(args.ram_write_range)
        args.input_schedule = keyboard_schedule(args.key_event or
            ([] if args.key_at is None else [f"{args.key_at}:{args.key_usage}:150"]), args.seconds)
    except ValueError as error:
        parser.error(str(error))
    try:
        return 0 if run(args)["capture_completed"] else 1
    except (OSError, ValueError, subprocess.SubprocessError) as error:
        parser.exit(1, str(error) + "\n")


if __name__ == "__main__":
    raise SystemExit(main())
