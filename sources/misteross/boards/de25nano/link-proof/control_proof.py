#!/usr/bin/env python3
"""Run native/AArch64 runtime control against the actual Verilated PR endpoint."""
from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
from pathlib import Path
import shutil
import subprocess
import tempfile
import time

BOARD = Path(__file__).resolve().parent
ROOT = BOARD.parents[4]
SCENARIOS = ("success", "program-failure", "wrong-persona", "ambiguous-write", "missing-response", "late-reset", "wrong-build")
EXPECTED_COUNTS = {"success": (14, 3), "program-failure": (2, 0), "wrong-persona": (4, 1),
                   "ambiguous-write": (2, 0), "missing-response": (4, 1), "late-reset": (0, 0), "wrong-build": (0, 0)}


def validate_result(value: dict, scenario: str) -> None:
    if (not isinstance(value, dict) or set(value) != {"scenario", "result", "writes", "logical_selections"}
            or value["scenario"] != scenario or value["result"] != "passed"
            or type(value["writes"]) is not int or type(value["logical_selections"]) is not int
            or (value["writes"], value["logical_selections"]) != EXPECTED_COUNTS[scenario]):
        raise ValueError("runtime/RTL result does not match the requested scenario and exact expected mutations")


def digest(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def run(argv: list[str], log: Path, cwd: Path | None = None) -> bytes:
    result = subprocess.run(argv, cwd=cwd, capture_output=True, timeout=240)
    log.write_bytes(result.stdout + result.stderr)
    if result.returncode:
        raise ValueError(f"command exited {result.returncode}; retained {log}")
    return result.stdout


def capture(output: Path, allow_working_copy: bool) -> tuple[Path, dict]:
    runtime = ROOT / "sources/libmister-runtime"
    files = [BOARD / name for name in ("control_proof.py", "cosim/client.cpp", "cosim/server.cpp",
             "fpga/shell.v", "fpga/personas.v", "fpga/sim_top.v", "fpga/persona_a.hex", "fpga/persona_b.hex")]
    files += list((BOARD / "generated").glob("*.vh"))
    files += list((runtime / "include").rglob("*.h"))
    files += list((runtime / "src").rglob("*.hpp"))
    files += [runtime / "src/native/de25_pr_control.cpp"]
    revision = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
    snapshot = output / "source"
    hashes = {}
    committed = True
    for path in sorted(set(files)):
        relative = path.relative_to(ROOT)
        data = path.read_bytes()
        result = subprocess.run(["git", "show", f"{revision}:{relative.as_posix()}"], cwd=ROOT,
                                capture_output=True)
        committed = committed and result.returncode == 0 and result.stdout == data
        target = snapshot / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(data)
        hashes[relative.as_posix()] = hashlib.sha256(data).hexdigest()
    receipt = {"revision": revision, "source_committed": committed, "sha256": hashes}
    (output / "inputs.json").write_text(json.dumps(receipt, sort_keys=True, indent=2) + "\n")
    if not committed and not allow_working_copy:
        raise ValueError("final co-simulation requires committed captured sources; development needs --allow-working-copy")
    return snapshot, receipt


def execute(args: argparse.Namespace) -> dict:
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=False)
    snapshot, inputs = capture(output, args.allow_working_copy)
    board = snapshot / BOARD.relative_to(ROOT)
    runtime = snapshot / "sources/libmister-runtime"
    logs = output / "logs"
    logs.mkdir()
    # Record the executables before they consume the frozen source. Buildroot's
    # SDK driver is a wrapper, so its .br_real implementation is recorded too.
    native_tools = {}
    for executable in (args.cxx, args.verilator):
        found = shutil.which(executable)
        if not found:
            raise ValueError("missing tool: " + executable)
        path = Path(found).resolve()
        native_tools[str(path)] = digest(path)
        companion = path.with_name("verilator_bin")
        if executable == args.verilator and companion.is_file():
            native_tools[str(companion)] = digest(companion)
    tool_info = {"native_tool_sha256": native_tools,
                 "verilator": run([args.verilator, "--version"], logs / "verilator-version.txt").decode().strip(),
                 "native_cxx": run([args.cxx, "--version"], logs / "native-cxx-version.txt").decode().strip()}
    obj = output / "obj"
    rtl = board / "fpga"
    command = [args.verilator, "--cc", "--exe", "--build", "-j", "2", "-O2", "--assert",
               "-Wall", "-Wno-DECLFILENAME", "-Wno-SYNCASYNCNET", "--top-module", "fes_link_sim",
               "-GTIMEOUT_CYCLES=1000000", "--Mdir", str(obj), "-I" + str(board),
               "-GBUILD_ID=128'h" + args.build_id,
               str(rtl / "shell.v"), str(rtl / "personas.v"), str(rtl / "sim_top.v"),
               str(board / "cosim/server.cpp")]
    run(command, logs / "server-build.log", rtl)
    native = output / "client-native"
    flags = ["-O2", "-std=c++14", "-Wall", "-Wextra", "-Werror", "-pthread"]
    sources = [board / "cosim/client.cpp", runtime / "src/native/de25_pr_control.cpp"]
    run([args.cxx, *flags, "-I" + str(runtime / "include"), "-I" + str(runtime / "src"),
         *map(str, sources), "-o", str(native)], logs / "native-build.log")
    clients = {"native": [str(native)]}
    if not args.native_only:
        if not all((args.builder_image, args.sdk_volume, args.sdk_cxx, args.qemu)):
            raise ValueError("AArch64 requires --builder-image, --sdk-volume, --sdk-cxx and --qemu")
        # Both source and SDK are read-only; only this proof's private output is writable.
        container = ["docker", "run", "--rm", "--network=none", "--user", f"{os.getuid()}:{os.getgid()}",
                     "--mount", f"type=bind,src={snapshot},dst=/src,readonly",
                     "--mount", f"type=bind,src={output},dst=/work",
                     "--mount", f"type=volume,src={args.sdk_volume},dst=/build,readonly", args.builder_image]
        container_board = Path("/src") / BOARD.relative_to(ROOT)
        container_runtime = Path("/src/sources/libmister-runtime")
        target = output / "client-aarch64"
        sdk_tools = [args.sdk_cxx, args.sdk_cxx + ".br_real"]
        sdk_hashes = run([*container, "sha256sum", *sdk_tools], logs / "sdk-tools-before.txt")
        run([*container, args.sdk_cxx, *flags, "-static", "-I" + str(container_runtime / "include"),
             "-I" + str(container_runtime / "src"), str(container_board / "cosim/client.cpp"),
             str(container_runtime / "src/native/de25_pr_control.cpp"), "-o", "/work/client-aarch64"],
            logs / "aarch64-build.log")
        data = target.read_bytes()
        if data[:6] != b"\x7fELF\x02\x01" or data[18:20] != b"\xb7\x00":
            raise ValueError("target is not little-endian ELF64 AArch64")
        qemu_source = Path(shutil.which(args.qemu) or args.qemu).resolve()
        qemu = output / "qemu-aarch64"
        shutil.copyfile(qemu_source, qemu)
        qemu.chmod(0o700)
        clients["aarch64-cortex-a76"] = [str(qemu), "-cpu", "cortex-a76", str(target)]
        tool_info.update({"builder_image": args.builder_image, "sdk_volume": args.sdk_volume,
                         "sdk_cxx": args.sdk_cxx, "qemu_sha256": digest(qemu),
                         "qemu_version": run([str(qemu), "--version"], logs / "qemu-version.txt").decode().strip(),
                         "aarch64_cxx_version": run([*container, args.sdk_cxx, "--version"],
                                                    logs / "aarch64-cxx-version.txt").decode().strip(),
                         "aarch64_tools_sha256": sdk_hashes.decode().splitlines()})
    results = []
    for architecture, client in clients.items():
        for scenario in SCENARIOS:
            with tempfile.TemporaryDirectory(prefix="fes-pr-control-") as directory:
                endpoint = Path(directory) / "endpoint"
                with (logs / f"{architecture}-{scenario}-server.log").open("wb") as log:
                    server = subprocess.Popen([str(obj / "Vfes_link_sim"), str(endpoint)], cwd=rtl,
                                              stdout=log, stderr=subprocess.STDOUT)
                    try:
                        limit = time.monotonic() + 15
                        while not endpoint.exists():
                            if server.poll() is not None or time.monotonic() >= limit:
                                raise ValueError("RTL server did not start; inspect retained server log")
                            time.sleep(0.01)
                        result = json.loads(run([*client, str(endpoint), scenario, args.build_id],
                                                logs / f"{architecture}-{scenario}-client.log"))
                        validate_result(result, scenario)
                        if server.wait(timeout=10) != 0:
                            raise ValueError("RTL server failed")
                        results.append({"architecture": architecture, **result})
                    finally:
                        if server.poll() is None:
                            server.terminate()
                            try:
                                server.wait(timeout=5)
                            except subprocess.TimeoutExpired:
                                server.kill()
                                server.wait()
    if (len(results) != len(clients) * len(SCENARIOS)
            or {(row["architecture"], row["scenario"]) for row in results}
            != {(architecture, scenario) for architecture in clients for scenario in SCENARIOS}):
        raise ValueError("missing or repeated runtime/RTL scenario")
    for scenario in SCENARIOS:
        values = [{k: v for k, v in row.items() if k != "architecture"}
                  for row in results if row["scenario"] == scenario]
        if any(value != values[0] for value in values):
            raise ValueError("native and AArch64 runtime/RTL outcomes differ")
    if any(digest(Path(path)) != expected for path, expected in native_tools.items()):
        raise ValueError("native build executable changed during proof")
    if not args.native_only and run([*container, "sha256sum", *sdk_tools], logs / "sdk-tools-after.txt") != sdk_hashes:
        raise ValueError("SDK compiler or implementation changed during proof")
    report = {"format": 1, "kind": "fes.de25-pr-control-cosim", "status": "passed",
              "physical_programming": "not-run", "configuration_model": "explicit-persona-mux-only",
              "inputs": inputs, "tools": tool_info, "results": results, "selected_build_id": args.build_id,
              "binaries": {p.name: digest(p) for p in (obj / "Vfes_link_sim", native)
                           if p.is_file()}}
    if not args.native_only:
        report["binaries"]["client-aarch64"] = digest(output / "client-aarch64")
    (output / "proof.json").write_text(json.dumps(report, sort_keys=True, indent=2) + "\n")
    return report


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--native-only", action="store_true")
    parser.add_argument("--allow-working-copy", action="store_true")
    parser.add_argument("--cxx", default="c++")
    parser.add_argument("--verilator", default="verilator")
    parser.add_argument("--builder-image")
    parser.add_argument("--sdk-volume")
    parser.add_argument("--sdk-cxx")
    parser.add_argument("--qemu")
    parser.add_argument("--build-id", default="112233445566778899aabbccddeeff00")
    args = parser.parse_args()
    if not re.fullmatch(r"[0-9a-f]{32}", args.build_id) or args.build_id == "0" * 32:
        parser.error("--build-id requires a nonzero 32-digit lowercase hexadecimal identity")
    try:
        report = execute(args)
    except (ValueError, OSError, subprocess.SubprocessError) as error:
        parser.exit(1, f"PR control co-simulation failed: {error}\n")
    print(json.dumps({"status": report["status"], "scenarios": len(report["results"]),
                      "source_committed": report["inputs"]["source_committed"]}))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
