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
import hashlib
import json
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
    "st_ym2149", "st_floppy", "st_floppy_writer", "st_native_low_video")]
RTL += ["cores/fes-zx81/expansions/zonx_ay.v"]
RTL += [CPU + name for name in ("fx68k.sv", "fx68kAlu.sv", "uaddrPla.sv")]
DATA = [CPU + "microrom.mem", CPU + "nanorom.mem", "cores/fes-common/generated/fes_video_part.vh"]
WRAPPER = PREFIX + CORE + "sim/st_boot_sim_top.sv"
HELPERS = ["scripts/sim_atari_st_demo.py", "scripts/atari_st_demo_sim.cpp"]


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


def run(args):
    rtl = list(RTL)
    wrapper = WRAPPER
    helpers = list(HELPERS)
    top = "st_boot_sim_top"
    if args.shared_memory:
        top = "st_boot_memory_sim_top"
        wrapper = PREFIX + CORE + "sim/" + top + ".sv"
        helpers[1] = PREFIX + CORE + "sim/boot_memory_tb.cpp"
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
    archive(output / "frozen-source.zip", source, identities)
    command = [verilator, "--cc", "--exe", "--build", "-O2", "-j", "2", "--top-module", top]
    command += [f"-GMFP_WAIT_STATES={args.mfp_wait_states}"]
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
                  str(args.seconds), str(output / "demo"), str(args.key_b_at or 0),
                  str(args.trace_start), str(args.trace_end), str(args.ram_extra_wait),
                  str(-1 if args.ram_fixed_wait is None else args.ram_fixed_wait)]
    if args.shared_memory:
        invocation = [str(executable), str(output / "etos192us.img"), str(args.seconds),
                      str(output / "demo"), str(output / "original.st"), str(args.trace_start), str(args.trace_end)]
        record["framebuffer_capture"] = "fixed 720p60 output after Direct part boundaries"
        record["native_rgb_capture"] = "production capture under shared SDRAM commands and independent 74.25 MHz pixel clock; no analog/electrical or original GLUE equivalence"
        record["media_fixture"] = "original disk preloaded into separate SDRAM disk buffer; production floppy/DMA/memory RTL; upload/mailbox not exercised"
    record["input_event"] = (None if args.key_b_at is None else
                             {"hid_usage": 5, "press_second": args.key_b_at,
                              "hold_milliseconds": 150})
    record["consecutive_native_frames"] = None if args.shared_memory else "first sixteen complete captures after 6.5 seconds; production capture with model RAM, no original GLUE/border oracle"
    record["raster_trace"] = {"start_second": args.trace_start, "end_second": args.trace_end,
                              "events": "IACK/peripheral request starts, sync changes and every committed palette write (including unchanged values)",
                              "pc": "diagnostic prefetch state, not instruction retirement"}
    if args.shared_memory:
        record["raster_trace"]["events"] = "IACK (including MFP vector), sync changes and every committed palette write (including unchanged values)"
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
    record["horizontal_phase_units"] = "68000 cycles within the native line"
    record["palette_trace"] = "changes after six seconds; first eight changed bundles per native frame, plus complete per-frame counts; model timing only"
    record["run_command"] = invocation
    process = subprocess.Popen(invocation, cwd=build, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
    timer = threading.Timer(1800, process.kill); timer.start()
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
                                   and record["rom_unchanged"] and record["original_disk_unchanged"]
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
    parser.add_argument("--seconds", type=int, default=15)
    parser.add_argument("--key-b-at", type=int,
                        help="press HID B at this simulated second for 150 ms (optional scroller selection)")
    parser.add_argument("--trace-start", type=int, help="inclusive simulated second for complete raster/logo tracing")
    parser.add_argument("--trace-end", type=int, help="exclusive simulated second for complete raster/logo tracing")
    parser.add_argument("--ram-extra-wait", type=int, default=0, help="add 0..64 system clocks to each model CPU RAM access; not physical arbitration")
    parser.add_argument("--ram-fixed-wait", type=int, help="replace variable CPU RAM callback waits with a fixed 0..64 system-clock delay; an ideal-storage probe, not physical SDRAM")
    parser.add_argument("--mfp-wait-states", type=int, default=0, help="optional 0..8 native CPU wait-state experiment; delays MFP access and data sampling")
    parser.add_argument("--shared-memory", action="store_true", help="use the existing shared SDRAM/independent pixel-clock boot fixture; disk preloaded before reset release")
    parser.add_argument("--verilator", default="verilator")
    args = parser.parse_args(argv)
    if len(args.rom_sha256) != 64 or any(c not in "0123456789abcdef" for c in args.rom_sha256):
        parser.error("--rom-sha256 must be a lowercase SHA-256 digest")
    if not 1 <= args.seconds <= 30:
        parser.error("--seconds must be 1..30")
    args.trace_start = min(6, args.seconds-1) if args.trace_start is None else args.trace_start
    args.trace_end = min(7, args.seconds) if args.trace_end is None else args.trace_end
    if not 0 <= args.trace_start < args.trace_end <= args.seconds:
        parser.error("trace window must satisfy 0 <= start < end <= seconds")
    if not 0 <= args.ram_extra_wait <= 64:
        parser.error("--ram-extra-wait must be 0..64")
    if args.ram_fixed_wait is not None and not 0 <= args.ram_fixed_wait <= 64:
        parser.error("--ram-fixed-wait must be 0..64")
    if not 0 <= args.mfp_wait_states <= 8:
        parser.error("--mfp-wait-states must be 0..8")
    if args.key_b_at is not None and not 1 <= args.key_b_at < args.seconds:
        parser.error("--key-b-at must be at least one and less than --seconds")
    if args.shared_memory and (args.seconds < 6 or args.key_b_at is not None or args.ram_extra_wait or args.ram_fixed_wait is not None):
        parser.error("--shared-memory requires at least 6 seconds, no key event and no callback RAM delay or override")
    try:
        return 0 if run(args)["capture_completed"] else 1
    except (OSError, ValueError, subprocess.SubprocessError) as error:
        parser.exit(1, str(error) + "\n")


if __name__ == "__main__":
    raise SystemExit(main())
