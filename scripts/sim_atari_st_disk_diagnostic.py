#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-or-later
"""Freeze and run original GEMDOS AUTO PRG on the real ST CPU/SDRAM simulator.

This host-only guest test preloads an immutable disk fixture into the physical
SDRAM model, then every CPU/DMA/video/media operation uses actual RTL and SDRAM
commands. It does not exercise host media upload, durable host publication,
native FPGA compilation or electrical hardware. Requires Verilator and C++17.
The ROM must be the independently verified stock EmuTOS 192US 1.4 image.

Example (no FPGA/compiler/GPU operation):
python3 scripts/sim_atari_st_disk_diagnostic.py --rom /absolute/etos192us.img \\
  --output out/validation/atari-st/disk-guest-REV --revision HEAD --seconds 15
"""
from __future__ import annotations

import argparse
import hashlib
import importlib.util
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
RTL = [CORE + "sim/st_memory_sim_top.sv"] + [CORE + "rtl/" + name + ".sv" for name in (
    "st_system", "st_cpu", "st_machine", "st_memory", "st_video_adapter", "st_video",
    "st_io", "st_mfp", "st_acia", "st_ikbd", "st_ym2149", "st_floppy", "st_floppy_writer")]
RTL += ["cores/fes-ramtest/rtl/sdram_addon_port.v", "cores/fes-common/rtl/fes_video_part_direct.v",
        "cores/fes-common/rtl/fes_video_part_scanlines.v", "cores/fes-zx81/expansions/zonx_ay.v"]
RTL += [CPU + name for name in ("fx68k.sv", "fx68kAlu.sv", "uaddrPla.sv")]
DATA = [CPU + "microrom.mem", CPU + "nanorom.mem", "cores/fes-common/generated/fes_video_part.vh"]
WRAPPER = PREFIX + CORE + "sim/st_boot_disk_sim_top.sv"
TEST = PREFIX + CORE + "sim/boot_disk_tb.cpp"
HELPERS = ["scripts/atari_st_disk_diagnostic.py", "scripts/sim_atari_st_disk_diagnostic.py"]


def sha(data):
    return hashlib.sha256(data).hexdigest()


def write_json(path, record):
    path.write_text(json.dumps(record, indent=2, sort_keys=True) + "\n")


def zip_sources(path, root, relatives):
    with zipfile.ZipFile(path, "w", zipfile.ZIP_DEFLATED) as archive:
        for relative in sorted(relatives):
            archive.write(root / relative, relative)


def run(args) -> dict:
    revision = subprocess.check_output(["git", "rev-parse", args.revision + "^{commit}"], cwd=ROOT, text=True).strip()
    rom = args.rom.read_bytes()
    if len(rom) != 196608 or sha(rom) != ROM_SHA256:
        raise ValueError("ROM is not the verified stock EmuTOS 192US 1.4 image")
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=False)
    source, build = output / "source", output / "build"
    source.mkdir()
    build.mkdir()
    selected, private = {}, {}
    for relative in [PREFIX + name for name in RTL + DATA]:
        data = subprocess.check_output(["git", "show", revision + ":" + relative], cwd=ROOT)
        if (ROOT / relative).read_bytes() != data:
            raise ValueError(f"working compiled input differs from selected commit: {relative}")
        selected[relative] = sha(data)
        destination = source / relative
        destination.parent.mkdir(parents=True, exist_ok=True)
        destination.write_bytes(data)
    for relative in [WRAPPER, TEST] + HELPERS:
        data = (ROOT / relative).read_bytes()
        private[relative] = sha(data)
        destination = source / relative
        destination.parent.mkdir(parents=True, exist_ok=True)
        destination.write_bytes(data)
    identities = selected | private
    spec = importlib.util.spec_from_file_location("frozen_st_disk_diagnostic", source / HELPERS[0])
    diagnostic = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(diagnostic)
    original = diagnostic.build_disk(auto=True)
    (output / "original-auto.st").write_bytes(original)
    (output / "DISKTEST.PRG").write_bytes(diagnostic.build_prg())
    (output / "etos192us.img").write_bytes(rom)
    for name in ("microrom.mem", "nanorom.mem"):
        shutil.copyfile(source / PREFIX / CPU / name, build / name)
    zip_sources(output / "frozen-source.zip", source, identities)
    verilator = shutil.which(args.verilator)
    if verilator is None:
        raise ValueError("Verilator is unavailable")
    command = [verilator, "--cc", "--exe", "--build", "-O2", "-j", "2", "--top-module", "st_boot_disk_sim_top"]
    command += FLAGS + ["-I" + str(source / PREFIX / "cores/fes-common/generated"), "--Mdir", str(build)]
    command += [str(source / WRAPPER)] + [str(source / PREFIX / name) for name in RTL]
    command += [str(source / TEST), "-CFLAGS", "-O3 -std=c++17"]
    record = {"schema": 1, "source_revision": revision, "git_compiled_inputs": selected,
              "private_guest_harness_inputs": private, "rom_sha256": sha(rom),
              "original_disk_sha256": sha(original), "prg_sha256": sha(diagnostic.build_prg()),
              "source_archive_sha256": sha((output / "frozen-source.zip").read_bytes()),
              "build_command": command,
              "verilator_version": subprocess.check_output([verilator, "--version"], text=True).strip(),
              "hardware_execution": False, "native_fpga_build": False,
              "media_fixture": "preloaded physical SDRAM; all guest accesses use RTL/controller",
              "pass": False}
    write_json(output / "source-identity.json", record)
    print(f"Compiling frozen guest simulator at {revision}; {len(selected)} selected inputs", flush=True)
    with (output / "build.log").open("w") as log:
        subprocess.run(command, cwd=source / PREFIX, stdout=log, stderr=subprocess.STDOUT, check=True, timeout=300)
    executable = build / "Vst_boot_disk_sim_top"
    record["executable_sha256_before"] = sha(executable.read_bytes())
    model_files = [str(path.relative_to(build)) for path in build.iterdir() if path.suffix in (".cpp", ".h")]
    zip_sources(output / "generated-model.zip", build, model_files)
    record["generated_model_sha256"] = sha((output / "generated-model.zip").read_bytes())
    record["generated_model_inputs"] = {name: sha((build / name).read_bytes()) for name in sorted(model_files)}
    prefix = output / "guest"
    invocation = [str(executable), str(output / "etos192us.img"), str(output / "original-auto.st"),
                  str(output / "DISKTEST.PRG"), str(args.seconds), str(prefix)]
    record["run_command"] = invocation
    print("Running bounded stock-ROM guest test; progress each emulated second", flush=True)
    process = subprocess.Popen(invocation, cwd=build, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
    timer = threading.Timer(1800, process.kill)
    timer.start()
    try:
        with (output / "guest.log").open("w") as log:
            for line in process.stdout:
                log.write(line)
                log.flush()
                print(line.rstrip(), flush=True)
        record["guest_exit_code"] = process.wait()
    finally:
        timer.cancel()
        if process.poll() is None:
            process.kill()
            process.wait()
    after = {name: sha((source / name).read_bytes()) for name in identities}
    record["frozen_inputs_unchanged"] = after == identities
    record["working_inputs_unchanged"] = all(sha((ROOT / name).read_bytes()) == digest for name, digest in identities.items())
    record["executable_sha256_after"] = sha(executable.read_bytes())
    record["rom_unchanged"] = sha((output / "etos192us.img").read_bytes()) == record["rom_sha256"]
    record["original_disk_unchanged"] = (output / "original-auto.st").read_bytes() == original
    record["generated_model_unchanged"] = all(sha((build / name).read_bytes()) == digest for name, digest in record["generated_model_inputs"].items())
    capture = output / "guest-disk.st"
    if record["guest_exit_code"] == 0:
        record["capture"] = diagnostic.inspect_capture(capture.read_bytes())
        record["guest_metrics"] = json.loads((output / "guest-guest.json").read_bytes())
        record["artifacts"] = {path.name: sha(path.read_bytes()) for path in output.glob("guest*") if path.is_file()}
        record["pass"] = (record["capture"]["marker_pass"] and record["frozen_inputs_unchanged"] and
                          record["working_inputs_unchanged"] and record["rom_unchanged"] and
                          record["original_disk_unchanged"] and record["generated_model_unchanged"] and
                          record["executable_sha256_before"] == record["executable_sha256_after"])
    write_json(output / "guest-proof.json", record)
    print(json.dumps({"pass": record["pass"], "proof": str(output / "guest-proof.json")}, sort_keys=True), flush=True)
    return record


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--rom", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True, help="new ignored directory; must not exist")
    parser.add_argument("--revision", default="HEAD")
    parser.add_argument("--seconds", type=int, default=15)
    parser.add_argument("--verilator", default="verilator")
    args = parser.parse_args(argv)
    if not 8 <= args.seconds <= 30:
        parser.error("--seconds must be 8..30")
    return 0 if run(args)["pass"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
