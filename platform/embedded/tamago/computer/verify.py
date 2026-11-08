#!/usr/bin/env python3
"""Check native computer control against an explicit captured DE25 project.

Compiles the actual Avalon/mailbox/Z80/ROM closure without changing its RTL.
Produces host simulation evidence only; never accesses or programs a board.
"""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import shutil
import signal
import subprocess
import sys
import time
import uuid

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[3]
BOARD = ROOT / "sources/misteross/prototypes/de25_board_control"
FIRMWARE = ROOT / "platform/embedded/firmware"
SOURCES = ("fes_de25_computer.v", "fes_computer_mailbox.v", "zx81_machine.sv",
           "zx81_machine_clock.v", "zx81_keyboard.v", "zx81_dpram_de25.v",
           "zx81_tape_store_de25.v", "fes_z80_nmos.sv", "fes_z80_engine.sv",
           "fes_z80_bus.sv", "fes_z80_alu.sv", "video.v")
DATA = ("zx8x.hex", "zx81_model.vh", "zx81_bus_pack.vh",
        "generated/fes_de25_computer.vh", "generated/fes_computer.vh")


def sha(path):
    if path.is_symlink() or not path.is_file():
        raise ValueError("expected regular input: " + str(path))
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--project", type=Path, required=True)
    parser.add_argument("--output", type=Path, default=ROOT / "out" / ("computer-check-" + uuid.uuid4().hex))
    args = parser.parse_args()
    project, output = args.project.absolute(), args.output.absolute()
    for path in (project, output):
        if ".." in path.parts or not path.is_relative_to(ROOT / "out") or any(p.is_symlink() for p in (path, *path.parents)):
            parser.error("project and output must be private paths beneath this worktree's out/")
    if output.exists() or not project.is_dir():
        parser.error("project must exist and output must be fresh")
    if shutil.disk_usage(ROOT).free < 30 * (1 << 30):
        parser.error("less than 30 GiB free")
    output.mkdir(parents=True)
    sys.path.insert(0, str(BOARD))
    spec = importlib.util.spec_from_file_location("computer_project_check", BOARD / "verify.py")
    board = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(board)
    import computer_model
    inputs, _, local = board.inputs_for(project)
    computer = inputs["computer"]
    if computer.get("new_dma_master") is not False:
        raise ValueError("this test supports the internal-RAM computer only")
    probe = HERE / "testdata/probe.cpp"
    files = list(HERE.glob("*.go")) + list((HERE / "generated").glob("*.go")) + [probe, HERE / "verify.py"]
    files += [FIRMWARE / name for name in ('nano_tape.c', 'nano_tape.h',
              'generated/fes_computer.h', 'generated/fes_de25_computer.h')]
    files += list((HERE.parent / "nativefabric").glob("*.go"))
    files += list((HERE.parent / "nativefabric/testdata").glob("hps*.json"))
    files += list((HERE.parent / "embeddedcontrol").rglob("*.go"))
    files += list((HERE.parent / "fabricmodel").glob("*.go"))
    files += [ROOT / "sources/mister-packages/packages/abi/fes_computer.yaml",
              ROOT / "sources/mister-packages/packages/fabric/fes_fabric_de25_computer.yaml",
              HERE.parent / "go.mod", HERE.parent / "go.sum", BOARD / "verify.py", BOARD / "computer_model.py"]
    hashes = {str(p.relative_to(ROOT)): sha(p) for p in files}
    sim = output / "simulation-source"
    sim.mkdir(); (sim / "generated").mkdir()
    rtl_hashes = {}
    for name in (*SOURCES, *DATA):
        selected = local / name
        actual = sha(selected)
        if computer["source_sha256"].get(name) != actual:
            raise ValueError("captured computer source differs: " + name)
        (sim / name).write_bytes(selected.read_bytes())
        rtl_hashes[name] = actual
    tools, paths = board.PRODUCER.tools_snapshot()
    env = dict(os.environ, **board.PRODUCER.build_environment(paths))
    for key in ("GOOS", "GOARCH", "GOARM", "GOARM64", "GOFLAGS", "GOTOOLCHAIN", "MAKEFLAGS", "MFLAGS", "MAKELEVEL", "GNUMAKEFLAGS"):
        env.pop(key, None)
    env["GOWORK"] = "off"
    commands = []

    def run(label, argv, cwd, cap):
        argv = [str(a) for a in argv]
        start = time.monotonic()
        with (output / (label + ".log")).open("xb") as log:
            child = subprocess.Popen(argv, cwd=cwd, env=env, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
            beat = start + 12
            try:
                while child.poll() is None:
                    now = time.monotonic()
                    if now-start >= cap:
                        raise TimeoutError(label + " exceeded bound")
                    if now >= beat:
                        print(label + ": running " + str(int(now-start)) + "s", flush=True)
                        beat = now+12
                    time.sleep(.2)
            finally:
                if child.poll() is None:
                    os.killpg(child.pid, signal.SIGTERM)
                    try:
                        child.wait(timeout=5)
                    except subprocess.TimeoutExpired:
                        os.killpg(child.pid, signal.SIGKILL)
                        child.wait()
        commands.append({"label": label, "argv": argv, "cwd": str(cwd), "returncode": child.returncode,
                         "cap_seconds": cap, "elapsed_seconds": time.monotonic()-start,
                         "log_sha256": sha(output / (label + ".log"))})
        if child.returncode:
            raise ValueError(label + " failed; see " + str(output / (label + ".log")))
        print(label + ": PASS", flush=True)

    obj = output / "obj"
    secure_object = output / 'nano_tape.o'
    run('secure-engine', ['cc', '-std=c11', '-Os', '-Wall', '-Wextra', '-Werror',
                         '-fsanitize=undefined', '-fno-sanitize-recover=all',
                         '-I' + str(FIRMWARE), '-c', FIRMWARE / 'nano_tape.c', '-o', secure_object], ROOT, 30)
    run("build", [paths["verilator"], "--cc", "--exe", "--build", "--public-flat-rw",
                  "--top-module", "fes_de25_computer", "-Wno-fatal", "--Mdir", obj,
                  "-I" + str(sim), "-I" + str(sim / "generated"),
                  "-DFES_DE25_COMPUTER_BUILD_ID=128'h" + computer["build_id"],
                  *('-D' + macro for macro in computer_model.macros(inputs.get('computer_model', 'zx81'))),
                  "-CFLAGS", "-std=c++17 -O2 -I" + str(FIRMWARE),
                  "-LDFLAGS", str(secure_object) + " -fsanitize=undefined -fno-sanitize-recover=all",
                  *(sim / name for name in SOURCES), probe], sim, 120)
    executable = obj / "Vfes_de25_computer"
    env.update(FES_COMPUTER_MODEL=str(executable), FES_COMPUTER_MODEL_DIRECTORY=str(sim),
               FES_COMPUTER_BUILD_ID=computer["build_id"])
    run("native-control", ["go", "test", "-count=1", "-v", "./computer/...", "./nativefabric"], HERE.parent, 300)
    if any(sha(ROOT / name) != expected for name, expected in hashes.items()):
        raise ValueError("native inputs changed during check")
    if any(sha(local / name) != expected or sha(sim / name) != expected for name, expected in rtl_hashes.items()):
        raise ValueError("captured RTL changed during check")
    result = {"status": "passed", "classification": "captured-rtl-host-simulation", "project": str(project),
              "package_id": json.loads((project / "board/hps/seal-result.json").read_text())["package_id"],
              "build_id": computer["build_id"], "source_sha256": hashes, "rtl_sha256": rtl_hashes,
              "artifact_sha256": sha(executable), "tools": tools, "commands": commands}
    (output / "result.json").write_text(json.dumps(result, indent=2, sort_keys=True) + "\n")
    print(json.dumps({"status": "passed", "output": str(output)}))


if __name__ == "__main__":
    main()
