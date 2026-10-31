#!/usr/bin/env python3
"""Execute the native/AArch64 De25Computer against actual ZX81 endpoint RTL."""
from __future__ import annotations

import argparse
import concurrent.futures
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import shutil
import struct
import subprocess
import sys
import tempfile
import time

BOARD = Path(__file__).resolve().parent
ROOT = BOARD.parents[4]
SCENARIOS = ("session", "tape-bounds", "wrong-build", "bad-signature", "missing-ack",
             "late-ack", "post-write-reset", "ambiguous-write", "held-word-change", "late-reset")


def sha(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def run(command: list[str], log: Path, **kwargs) -> bytes:
    result = subprocess.run(command, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                            timeout=kwargs.pop("timeout", 180), **kwargs)
    log.write_bytes(result.stdout + result.stderr)
    if result.returncode:
        raise ValueError(f"command exited {result.returncode}; see {log}")
    return result.stdout


def load(path: Path, name: str):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def capture() -> tuple[dict, dict[str, bytes]]:
    recipe = load(BOARD / "build.py", "de25_client_proof_build")
    files = recipe.capture()
    paths = [BOARD / name for name in ("sim.py", "client_proof.py", "computer_sim.hpp", "computer_tb.cpp")]
    paths += list((BOARD / "cosim").glob("*"))
    runtime = ROOT / "sources/libmister-runtime"
    paths += list((runtime / "src/native").rglob("*.hpp"))
    paths += [runtime / "src/native/de25_computer.cpp", runtime / "include/libmister-runtime/runtime.h"]
    for path in paths:
        if path.is_symlink() or not path.is_file(): raise ValueError(f"unsafe source {path}")
        files[path.relative_to(ROOT).as_posix()] = path.read_bytes()
    revision = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
    committed = True
    for name, data in files.items():
        result = subprocess.run(["git", "show", revision + ":" + name], cwd=ROOT, capture_output=True)
        committed &= result.returncode == 0 and result.stdout == data
    return {"revision": revision, "source_committed": committed,
            "sha256": {name: sha(data) for name, data in sorted(files.items())}}, files


def arm_elf(path: Path) -> None:
    data = path.read_bytes()
    if data[:6] != b"\x7fELF\x02\x01" or struct.unpack_from("<H", data, 18)[0] != 183:
        raise ValueError("expected little-endian ELF64 AArch64 client")
    offset = struct.unpack_from("<Q", data, 32)[0]
    size, count = struct.unpack_from("<HH", data, 54)
    if size < 56 or offset + size * count > len(data): raise ValueError("invalid ELF program headers")
    if any(struct.unpack_from("<I", data, offset + n * size)[0] == 3 for n in range(count)):
        raise ValueError("AArch64 client must have no dynamic interpreter")


def upload_writes(length: int, chunk: int = 127) -> int:
    return 4 + sum(3 + (min(chunk, length - n) + 1) // 2 for n in range(0, length, chunk)) + 1 + 6


def tape_maximum() -> int:
    text = (BOARD / "generated/fes_de25_computer.vh").read_text()
    values = re.findall(r"^`define FES_DE25_COMPUTER_ZX81_TAPE_MAX_BYTES 32'h([0-9a-fA-F]{8})$", text, re.M)
    if len(values) != 1: raise ValueError("missing or duplicate generated DE25 tape limit")
    return int(values[0], 16)


def check_roundtrip(result: dict, image_bytes: int) -> None:
    expected={'saved_bytes':image_bytes,'loads':2,'runs':2,'editor_returns':24,
              'saved_cdflag':0x40,'printed_value':7,'reload_removed_edit':True}
    if result!=expected or any(type(result[k]) is not type(v) for k,v in expected.items()):
        raise ValueError('incomplete BASIC SAVE/load/run/reload/editor evidence')


def validate_result(result: dict, scenario: str) -> None:
    expected_fields={"scenario", "result", "writes", "image_bytes", "loaded_bytes", "complete_frames"}
    if scenario=="session":expected_fields.add("roundtrip")
    if set(result) != expected_fields:
        raise ValueError("incomplete or unknown result fields")
    if result["scenario"] != scenario or result["result"] != "passed": raise ValueError("wrong scenario result")
    for name in ("writes", "image_bytes", "loaded_bytes", "complete_frames"):
        if type(result[name]) is not int or result[name] < 0: raise ValueError("invalid result count")
    if scenario == "session":
        if not 1 <= result["image_bytes"] <= tape_maximum() or result["loaded_bytes"] != result["image_bytes"] or result["complete_frames"] < 2:
            raise ValueError("session lacks complete real-machine evidence")
        check_roundtrip(result['roundtrip'],result['image_bytes'])
        # 17 typed tokens have down/up states, seven ENTER/release pairs,
        # nine full HID rows per state, plus identity/execution/eject controls.
        expected = 475 + 2*upload_writes(result["image_bytes"])
    else:
        if any(result[name] for name in ("image_bytes", "loaded_bytes", "complete_frames")):
            raise ValueError("non-session result claims machine evidence")
        expected = {"wrong-build": 16, "late-reset": 22,
                    "tape-bounds": 22 + upload_writes(517) + 7 + upload_writes(tape_maximum()) + 7 + 16}.get(scenario, 23)
    if result["writes"] != expected: raise ValueError(f"unexpected command count for {scenario}: {result['writes']} != {expected}")


def execute(args) -> dict:
    if not args.native_only and (not all((args.builder_image, args.sdk_volume, args.sdk_cxx, args.qemu))
            or not re.fullmatch(r"sha256:[0-9a-f]{64}", args.builder_image)):
        raise ValueError("AArch64 requires a pinned builder image, SDK volume/compiler and QEMU")
    inputs, files = capture()
    if not inputs["source_committed"] and not args.allow_working_copy:
        raise ValueError("commit selected sources before producing evidence")
    selected = tuple(args.scenario or SCENARIOS)
    if len(set(selected)) != len(selected): raise ValueError("duplicate scenario")
    output = args.output.resolve(); output.mkdir(parents=True, mode=0o700, exist_ok=False)
    logs = output / "logs"; logs.mkdir()
    snapshot = output / "source"
    for name, data in files.items():
        path = snapshot / name; path.parent.mkdir(parents=True, exist_ok=True); path.write_bytes(data)
    (output / "inputs.json").write_text(json.dumps(inputs, indent=2, sort_keys=True) + "\n")
    board = snapshot / BOARD.relative_to(ROOT)
    sys.path.insert(0, str(board))
    old_build = sys.modules.pop("build", None)
    try:
        sim = load(board / "sim.py", "de25_frozen_client_sim")
        rtl, sources = sim.prepare("client", output / "rtl")
    finally:
        sys.path.pop(0); sys.modules.pop("build", None)
        if old_build is not None: sys.modules["build"] = old_build
    runtime = snapshot / "sources/libmister-runtime"
    cxx = Path(shutil.which(args.cxx) or args.cxx).resolve()
    verilator = Path(shutil.which(args.verilator) or args.verilator).resolve()
    companion = verilator.with_name("verilator_bin")
    tools = {str(path): sha(path.read_bytes()) for path in (cxx, verilator, companion)}
    tool_info = {"native_sha256": tools,
                 "cxx": run([str(cxx), "--version"], logs / "cxx-version.txt").decode().strip(),
                 "verilator": run([str(verilator), "--version"], logs / "verilator-version.txt").decode().strip()}
    obj = output / "obj"
    run([str(verilator), "--cc", "--exe", "--build", "-j", "2", "--public-flat-rw", "--x-initial", "unique",
         "-MAKEFLAGS", "CXX=" + str(cxx),
         "-O3", "--top-module", "fes_de25_computer", "-Wno-UNOPTFLAT", "-Wno-CASEINCOMPLETE",
         "-Wno-WIDTHTRUNC", "-Wno-WIDTHEXPAND", "-Wno-PINCONNECTEMPTY", "-Wno-IMPLICITSTATIC", "-Wno-CASEX",
         "-I" + str(board), "-I" + str(board / "generated"),
         "-I" + str(snapshot / "sources/misteross/cores/fes-common/generated"),
         "-I" + str(snapshot / "sources/misteross/cores/fes-zx81/rtl"),
         "-I" + str(snapshot / "sources/misteross/cores/fes-common/rtl/z80"),
         "-GBUILD_ID=128'h" + args.build_id,
         "-CFLAGS", f"-O2 -I{rtl} -I{board} -I{board / 'generated'}",
         "--Mdir", str(obj), *map(str, sources), str(board / "cosim/server.cpp")],
        logs / "rtl-build.log", cwd=snapshot, timeout=300)
    flags = ["-std=c++14", "-O2", "-Wall", "-Wextra", "-Werror", "-pthread"]
    native = output / "client-native"
    run([str(cxx), *flags, "-I" + str(runtime / "include"), "-I" + str(runtime / "src"),
         str(board / "cosim/client.cpp"), str(runtime / "src/native/de25_computer.cpp"), "-o", str(native)],
        logs / "native-build.log")
    clients = {"native": [str(native)]}
    binaries = {"client-native": sha(native.read_bytes()), "rtl-server": sha((obj / "Vfes_de25_computer").read_bytes())}
    if not args.native_only:
        container = ["docker", "run", "--rm", "--network=none", "--user", f"{os.getuid()}:{os.getgid()}",
                     "--mount", f"type=bind,src={snapshot},dst=/src,readonly",
                     "--mount", f"type=bind,src={output},dst=/work",
                     "--mount", f"type=volume,src={args.sdk_volume},dst=/build,readonly", args.builder_image]
        container_board = Path("/src") / BOARD.relative_to(ROOT)
        container_runtime = Path("/src/sources/libmister-runtime")
        sdk_tools = [args.sdk_cxx, args.sdk_cxx + ".br_real"]
        sdk_hashes = run([*container, "sha256sum", *sdk_tools], logs / "sdk-before.txt")
        run([*container, args.sdk_cxx, *flags, "-static", "-I" + str(container_runtime / "include"),
             "-I" + str(container_runtime / "src"), str(container_board / "cosim/client.cpp"),
             str(container_runtime / "src/native/de25_computer.cpp"), "-o", "/work/client-aarch64"], logs / "aarch64-build.log")
        arm_elf(output / "client-aarch64")
        qemu_source = Path(shutil.which(args.qemu) or args.qemu).resolve()
        qemu = output / "qemu-aarch64"; shutil.copyfile(qemu_source, qemu); qemu.chmod(0o700)
        clients["aarch64-cortex-a76"] = [str(qemu), "-cpu", "cortex-a76", str(output / "client-aarch64")]
        binaries["client-aarch64"] = sha((output / "client-aarch64").read_bytes())
        tool_info.update({"builder_image": args.builder_image, "sdk_volume": args.sdk_volume, "sdk_cxx": args.sdk_cxx,
                         "sdk_sha256": sdk_hashes.decode().splitlines(), "qemu_sha256": sha(qemu.read_bytes()),
                         "qemu": run([str(qemu), "--version"], logs / "qemu-version.txt").decode().strip()})
    def suite(architecture, command):
        rows = []
        for scenario in selected:
            with tempfile.TemporaryDirectory(prefix="fes-zx81-client-") as directory:
                endpoint = Path(directory) / "endpoint"
                with (logs / f"{architecture}-{scenario}-server.log").open("wb") as stream:
                    server = subprocess.Popen([str(obj / "Vfes_de25_computer"), str(endpoint),
                                               "+verilator+rand+reset+2", "+verilator+seed+41"],
                                              stdout=stream, stderr=subprocess.STDOUT, cwd=rtl)
                    try:
                        limit = time.monotonic() + 15
                        while not endpoint.exists():
                            if server.poll() is not None or time.monotonic() >= limit: raise ValueError("RTL server did not start")
                            time.sleep(0.01)
                        result = json.loads(run([*command, str(endpoint), scenario, args.build_id, str(logs/f"{architecture}-saved.p")],
                                                logs / f"{architecture}-{scenario}-client.log",
                                                # Two LOAD/RUN cycles plus SAVE/edit add work;
                                                # guest operation bounds remain unchanged.
                                                timeout=1800 if scenario == "session" else 900))
                        validate_result(result, scenario)
                        if scenario=='session':
                            data=(logs/f"{architecture}-saved.p").read_bytes()
                            if len(data)!=result['image_bytes'] or data[0x403b-0x4009]!=0x40:raise ValueError('retained SAVE image differs')
                            result['saved_image_sha256']=sha(data)
                        if server.wait(timeout=10): raise ValueError("RTL server failed")
                        rows.append({"architecture": architecture, **result})
                    finally:
                        if server.poll() is None:
                            server.terminate()
                            try: server.wait(timeout=5)
                            except subprocess.TimeoutExpired: server.kill(); server.wait()
        return rows
    with concurrent.futures.ThreadPoolExecutor(max_workers=len(clients)) as pool:
        futures = [pool.submit(suite, arch, command) for arch, command in clients.items()]
        results = [row for future in futures for row in future.result()]
    expected = {(arch, scenario) for arch in clients for scenario in selected}
    if {(r["architecture"], r["scenario"]) for r in results} != expected or len(results) != len(expected):
        raise ValueError("incomplete or duplicated scenario matrix")
    for scenario in selected:
        values = [{k: v for k, v in row.items() if k != "architecture"} for row in results if row["scenario"] == scenario]
        if any(value != values[0] for value in values): raise ValueError("native/AArch64 outcomes differ")
    for name, data in files.items():
        if (ROOT / name).read_bytes() != data: raise ValueError("source changed during proof: " + name)
    if any(sha(Path(name).read_bytes()) != value for name, value in tools.items()): raise ValueError("native tool changed")
    if not args.native_only and run([*container, "sha256sum", *sdk_tools], logs / "sdk-after.txt") != sdk_hashes:
        raise ValueError("SDK tools changed")
    if not args.native_only and sha(qemu.read_bytes()) != tool_info["qemu_sha256"]:
        raise ValueError("QEMU executable changed")
    report = {"format": 1, "kind": "fes.de25-zx81-client-cosim", "status": "passed", "inputs": inputs,
              "selected_build_id": args.build_id, "full_scenario_matrix": selected == SCENARIOS,
              "tools": tool_info, "binaries": binaries, "results": results,
              "clock_model": "explicit 100 MHz system / 74.25 MHz pixel; observation advances are outside client calls",
              "memory_model": "existing portable behavioral RAM", "physical_mmio": "not-run"}
    (output / "proof.json").write_text(json.dumps(report, indent=2, sort_keys=True) + "\n")
    return report


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--native-only", action="store_true")
    parser.add_argument("--allow-working-copy", action="store_true")
    parser.add_argument("--scenario", action="append", choices=SCENARIOS)
    parser.add_argument("--build-id", default="00112233445566778899aabbccddeeff")
    parser.add_argument("--cxx", default="c++")
    parser.add_argument("--verilator", default="verilator")
    parser.add_argument("--builder-image")
    parser.add_argument("--sdk-volume")
    parser.add_argument("--sdk-cxx")
    parser.add_argument("--qemu")
    args = parser.parse_args()
    if not re.fullmatch(r"[0-9a-f]{32}", args.build_id) or args.build_id == "0" * 32:
        parser.error("build ID must be a nonzero 32-digit lowercase hexadecimal value")
    try:
        report = execute(args)
    except (ValueError, OSError, subprocess.SubprocessError) as error:
        parser.exit(1, f"DE25 ZX81 client proof failed: {error}\n")
    print(json.dumps({"status": report["status"], "cases": len(report["results"]),
                      "source_committed": report["inputs"]["source_committed"]}))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
