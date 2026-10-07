#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-or-later
"""Freeze and run an original IKBD/YM AUTO PRG on the real ST CPU/chipset.

Stock EmuTOS loads the original guest through actual floppy/DMA RTL, then FX68K
executes its ACIA polling, exact keyboard/joystick checks and YM register writes.
RAM/disk replies use bounded model storage; this does not exercise physical
SDRAM, mailbox upload, durable publication, video/HDMI transport or hardware.
Requires Verilator and C++17 and the independently verified EmuTOS192US1.4 ROM.

The default semantic run shortens only the six audio hold immediates to20 timer
ticks(0.1s), recording its different PRG/disk identity explicitly. Use
--audio-phase-ticks 600 for the exact default3-second hardware guest. Both run
unmodified production CPU/chipset RTL and retain actual PCM and fetch traces.

python3 scripts/sim_atari_st_io_diagnostic.py --rom /absolute/etos192us.img \\
  --output out/validation/atari-st/io-guest-REV --revision HEAD --seconds 10
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
import sys
from array import array
import math
import statistics
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
    "st_system", "st_cpu", "st_machine", "st_io", "st_mfp", "st_acia", "st_ikbd", "st_ym2149", "st_floppy", "st_floppy_writer")]
RTL += ["cores/fes-zx81/expansions/zonx_ay.v"]
RTL += [CPU + name for name in ("fx68k.sv", "fx68kAlu.sv", "uaddrPla.sv")]

DATA = [CPU + "microrom.mem", CPU + "nanorom.mem", "cores/fes-common/generated/fes_video_part.vh"]
WRAPPER = PREFIX + CORE + "tests/io_guest_sim_top.sv"
TEST = PREFIX + CORE + "tests/io_guest_tb.cpp"
HELPERS = ["scripts/atari_st_io_diagnostic.py", "scripts/sim_atari_st_io_diagnostic.py", "scripts/atari_st_disk_diagnostic.py"]


def sha(data):
    return hashlib.sha256(data).hexdigest()


def write_json(path, record):
    path.write_text(json.dumps(record, indent=2, sort_keys=True) + "\n")


def zip_sources(path, root, relatives):
    with zipfile.ZipFile(path, "w", zipfile.ZIP_DEFLATED) as archive:
        for relative in sorted(relatives):
            archive.write(root / relative, relative)


def inspect_pcm(output):
    """Independent rising-edge Yamaha equation check on actual guest PCM.

    Console/marker work follows the YM write; discard21ms at both phase edges
    to avoid mixing successive tones. The shortened envelope phase covers only
    its initial low-volume steps; full32-step semantics have focused RTL tests.
    """
    markers=[json.loads(line) for line in (output / "guest-markers.jsonl").read_text().splitlines()]
    starts={}
    for marker in markers:
        phase=marker["marker"]-0x41550000
        if 0 <= phase < 6 and phase not in starts:
            starts[phase]=marker["pcm_sample"]
    pcm=array("h")
    pcm.frombytes((output / "guest-pcm-s16le.raw").read_bytes())
    if sys.byteorder != "little": pcm.byteswap()
    result={"schema":1,"sample_rate_hz":48000,"trim_each_edge_samples":1000,"phases":[]}
    for phase in range(6):
        data=pcm[starts[phase]+1000:starts.get(phase+1,len(pcm))-1000]
        if not data: raise ValueError("audio phase too short for settled PCM observation")
        dc=statistics.mean(data)
        rises=[index for index in range(1,len(data)) if data[index-1] <= dc and data[index] > dc]
        frequency=48000*(len(rises)-1)/(rises[-1]-rises[0]) if len(rises)>1 else None
        rms=math.sqrt(statistics.mean((value-dc)**2 for value in data))
        record={"phase":phase,"samples":len(data),"minimum":min(data),"maximum":max(data),
                "unique_levels":len(set(data)),"ac_rms":rms,"rising_edge_hz":frequency}
        if phase < 3:
            wanted=2000000/(16*[284,189,142][phase])
            record.update(expected_hz=wanted, passed=frequency is not None and abs(frequency-wanted)<2)
        elif phase == 3: record["passed"]=rms>100 and len(rises)>100
        elif phase == 4: record["passed"]=max(data)>0 and rms>0
        else: record["passed"]=all(value==0 for value in data)
        result["phases"].append(record)
    result["pass"]=all(phase["passed"] for phase in result["phases"])
    return result


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
    sys.path.insert(0, str(source / "scripts"))
    spec = importlib.util.spec_from_file_location("frozen_st_disk_diagnostic", source / HELPERS[0])
    diagnostic = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(diagnostic)
    original = diagnostic.build_disk(phase_ticks=args.audio_phase_ticks)
    (output / "original-auto.st").write_bytes(original)
    (output / "IOTEST.PRG").write_bytes(diagnostic.build_prg(phase_ticks=args.audio_phase_ticks))
    (output / "etos192us.img").write_bytes(rom)
    prg, symbols = diagnostic.build_prg_with_symbols(phase_ticks=args.audio_phase_ticks)
    events = []
    for group,(_, sequence,_) in enumerate(diagnostic.groups()):
        for event in sequence:
            usage = event["Code"] - 0x1000 if event["Device"] == 0 else event["Code"] - 100
            if usage >= 0xe0 and event["Device"] == 0: usage = 128 + usage - 0xe0
            events.append((group,-1 if event["Device"] == 0 else event["Player"],usage,event["Action"]))
    header = "struct Input { unsigned group; int player; unsigned code; bool down; };\n"
    header += "constexpr unsigned MarkerOffset=" + str(symbols["marker"]) + ";\n"
    header += "constexpr Input Inputs[]={" + ",".join("{"+",".join(map(str,event))+"}" for event in events) + "};\n"
    header += "constexpr unsigned InputCount=sizeof(Inputs)/sizeof(Inputs[0]);\n"
    (build / "io_events.h").write_text(header)
    write_json(output / "schedule.json", diagnostic.schedule() | {"symbols": symbols})
    for name in ("microrom.mem", "nanorom.mem"):
        shutil.copyfile(source / PREFIX / CPU / name, build / name)
    zip_sources(output / "frozen-source.zip", source, identities)
    verilator = shutil.which(args.verilator)
    if verilator is None:
        raise ValueError("Verilator is unavailable")
    command = [verilator, "--cc", "--exe", "--build", "-O2", "-j", "2", "--top-module", "st_io_guest_sim_top"]
    command += FLAGS + ["-I" + str(source / PREFIX / "cores/fes-common/generated"), "--Mdir", str(build)]
    command += [str(source / WRAPPER)] + [str(source / PREFIX / name) for name in RTL]
    command += [str(source / TEST), "-CFLAGS", "-O3 -std=c++17"]
    record = {"schema": 1, "source_revision": revision, "git_compiled_inputs": selected,
              "private_guest_harness_inputs": private, "rom_sha256": sha(rom),
              "original_disk_sha256": sha(original), "prg_sha256": sha(diagnostic.build_prg(phase_ticks=args.audio_phase_ticks)),
              "source_archive_sha256": sha((output / "frozen-source.zip").read_bytes()),
              "build_command": command,
              "verilator_version": subprocess.check_output([verilator, "--version"], text=True).strip(),
              "hardware_execution": False, "native_fpga_build": False,
              "media_fixture": "bounded RAM/disk callbacks; real FX68K/EmuTOS/IKBD/YM/floppy/DMA RTL",
              "audio_phase_ticks": args.audio_phase_ticks, "hardware_audio_phase_ticks": diagnostic.PHASE_TICKS,
              "guest_duration_variant": "same emitted instructions, audio wait immediates shortened" if args.audio_phase_ticks != diagnostic.PHASE_TICKS else "full hardware fixture", "pass": False}
    write_json(output / "source-identity.json", record)
    print(f"Compiling frozen guest simulator at {revision}; {len(selected)} selected inputs", flush=True)
    with (output / "build.log").open("w") as log:
        subprocess.run(command, cwd=source / PREFIX, stdout=log, stderr=subprocess.STDOUT, check=True, timeout=300)
    executable = build / "Vst_io_guest_sim_top"
    record["executable_sha256_before"] = sha(executable.read_bytes())
    model_files = [str(path.relative_to(build)) for path in build.iterdir() if path.suffix in (".cpp", ".h")]
    zip_sources(output / "generated-model.zip", build, model_files)
    record["generated_model_sha256"] = sha((output / "generated-model.zip").read_bytes())
    record["generated_model_inputs"] = {name: sha((build / name).read_bytes()) for name in sorted(model_files)}
    prefix = output / "guest"
    invocation = [str(executable), str(output / "etos192us.img"), str(output / "original-auto.st"),
                  str(output / "IOTEST.PRG"), str(args.seconds), str(prefix)]
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
        record["disk_unchanged_by_guest"] = capture.read_bytes() == original
        record["guest_metrics"] = json.loads((output / "guest-guest.json").read_bytes())
        record["pcm_oracle"] = inspect_pcm(output)
        write_json(output / "pcm-oracle.json", record["pcm_oracle"])
        record["artifacts"] = {path.name: sha(path.read_bytes()) for path in output.glob("guest*") if path.is_file()}
        record["pass"] = (record["pcm_oracle"]["pass"] and record["disk_unchanged_by_guest"] and record["guest_metrics"]["input_events"] == len(events) and record["guest_metrics"]["audio_phase_mask"] == 63 and record["frozen_inputs_unchanged"] and
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
    parser.add_argument("--seconds", type=int, default=10)
    parser.add_argument("--audio-phase-ticks", type=int, default=20, help="20 for bounded semantic proof; 600 for exact default hardware PRG")
    parser.add_argument("--verilator", default="verilator")
    args = parser.parse_args(argv)
    if not 8 <= args.seconds <= 30:
        parser.error("--seconds must be 8..30")
    if not 20 <= args.audio_phase_ticks <= 600:
        parser.error("--audio-phase-ticks must be 20..600")
    return 0 if run(args)["pass"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
