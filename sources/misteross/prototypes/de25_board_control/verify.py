#!/usr/bin/env python3
"""Verify a generated DE25 diagnostic through its actual Avalon RTL model."""
from __future__ import annotations
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import uuid

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[3]
TAMAGO = ROOT / "platform/embedded/tamago"
SPEC = importlib.util.spec_from_file_location("embedded_sim_producer", HERE.parent / "embedded_control/producer.py")
PRODUCER = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(PRODUCER)


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def run(label, args, cwd, env, output, expect=0, marker=None):
    result = subprocess.run([str(x) for x in args], cwd=cwd, env=env, stdout=subprocess.PIPE,
                            stderr=subprocess.STDOUT, timeout=180)
    (output / (label + ".log")).write_bytes(result.stdout)
    if (result.returncode == 0) != (expect == 0) or (marker and marker not in result.stdout.decode(errors="replace")):
        raise ValueError(f"{label} returned {result.returncode}; see {output / (label + '.log')}")
    return result.stdout.decode()


def inputs_for(project):
    board = project / "board"
    inputs_path = board / "hps/inputs.json"
    generation_path = board / "hps/generation.json"
    inputs = json.loads(inputs_path.read_text())
    generation = json.loads(generation_path.read_text())
    identity = inputs["embedded_control"]
    snapshot = board / "functional-source"
    sources = inputs["functional_snapshot_sha256"]
    if not sources or any(sha(snapshot / name) != expected for name, expected in sources.items()):
        raise ValueError("functional source snapshot differs")
    fingerprint = hashlib.sha256(json.dumps({"source_sha256": sources,
        "vendor_sha256": inputs["vendor_archive_sha256"], "system_hz": 100000000,
        "pixel_hz": 74250000}, sort_keys=True).encode()).hexdigest()[:32]
    if (fingerprint != identity["build_id"] or fingerprint != inputs["computer"]["build_id"] or
            not re.fullmatch(r"[0-9a-f]{32}", fingerprint) or fingerprint == "0" * 32):
        raise ValueError("board producer fingerprint does not bind both endpoints")
    local = board / "hps/project/fes_mailbox"
    mapped = {"fes_de25_embedded.sv": "sources/misteross/prototypes/de25_board_control/fes_de25_embedded.sv",
              "fes_de25_embedded_hw.tcl": "sources/misteross/prototypes/de25_board_control/fes_de25_embedded_hw.tcl",
              "fes_embedded_control.sv": "sources/misteross/cores/fes-common/rtl/fes_embedded_control.sv",
              "generated/fes_embedded_control.vh": "sources/misteross/cores/fes-common/generated/fes_embedded_control.vh"}
    for installed, source in mapped.items():
        if sha(local / installed) != sources[source]:
            raise ValueError("project endpoint differs from source snapshot: " + installed)
    script = board / "hps/project/integrate-mailbox.tcl"
    if sha(script) != inputs["computer"]["integration_sha256"]:
        raise ValueError("project integration script differs")
    qsf = (board / "hps/project/golden_top.qsf").read_text()
    if qsf.count(f"FES_DE25_COMPUTER_BUILD_ID=128'h{fingerprint}") != 1:
        raise ValueError("project build-ID macro differs")
    generated = generation["embedded_control"]
    if (generated["maps"]["lw"]["base"] != identity["peripheral_offset"] or
        generated["maps"]["lw"]["span"] != identity["span"] or
        generated["maps"]["jtag"]["base"] != identity["jtag_bridge_base"] + identity["peripheral_offset"] or
        generated["maps"]["jtag"]["span"] != identity["span"]):
        raise ValueError("generated control maps differ")
    changed = dict(sources)
    changed[mapped["fes_de25_embedded.sv"]] = "f" * 64
    modified = hashlib.sha256(json.dumps({"source_sha256": changed, "vendor_sha256": inputs["vendor_archive_sha256"],
        "system_hz": 100000000, "pixel_hz": 74250000}, sort_keys=True).encode()).hexdigest()[:32]
    if modified == fingerprint:
        raise ValueError("board fingerprint did not respond to endpoint source")
    return inputs, generation, local


def main():
    p = argparse.ArgumentParser(description=__doc__)
    mode = p.add_mutually_exclusive_group(required=True)
    mode.add_argument("--project", type=Path, help="output of build.py generate/build")
    mode.add_argument("--standalone", action="store_true", help="test the Avalon adapter without vendor tools")
    p.add_argument("--output", type=Path, default=ROOT / "out" / ("de25-board-control-check-" + uuid.uuid4().hex))
    args = p.parse_args()
    try:
        project = args.project.absolute() if args.project else None
        output = args.output.absolute()
        for path in (output, *([project] if project else [])):
            if ".." in path.parts or any(x.is_symlink() for x in (path, *path.parents)) or not path.is_relative_to(ROOT / "out"):
                raise ValueError("project and output must be private paths under this worktree's out/")
        if (project is not None and not project.is_dir()) or output.exists():
            raise ValueError("project must exist and output must be fresh")
        inputs, generation, local = inputs_for(project) if project else (None, None, None)
        output.mkdir(parents=True)
        source = (HERE / "probe.cpp").read_bytes()
        (output / "probe.cpp").write_bytes(source)
        tcl_sources = {name: sha(HERE / name) for name in ("hardware.tcl", "sim_jtag.tcl")}
        tcl = shutil.which("tclsh")
        if tcl is None:
            raise ValueError("tclsh is required for the diagnostic-script check")
        if project:
            endpoint = local / "fes_embedded_control.sv"
            wrapper = local / "fes_de25_embedded.sv"
            includes = local / "generated"
            build_id, abi = inputs["embedded_control"]["build_id"], inputs["embedded_control"]["control_abi"]
        else:
            local = output / "simulation-source"
            includes = local / "generated"
            includes.mkdir(parents=True)
            endpoint = local / "fes_embedded_control.sv"
            wrapper = local / "fes_de25_embedded.sv"
            endpoint.write_bytes((ROOT / "sources/misteross/cores/fes-common/rtl/fes_embedded_control.sv").read_bytes())
            wrapper.write_bytes((HERE / "fes_de25_embedded.sv").read_bytes())
            (includes / "fes_embedded_control.vh").write_bytes((ROOT / "sources/misteross/cores/fes-common/generated/fes_embedded_control.vh").read_bytes())
            build_id, abi = "00112233445566778899aabbccddeeff", 0x10203040
        source_sha256 = {"endpoint": sha(endpoint), "wrapper": sha(wrapper), "definition": sha(includes / "fes_embedded_control.vh")}
        tools, paths = PRODUCER.tools_snapshot()
        obj = output / "obj"
        command = [paths["verilator"], "--cc", "--exe", "--build", "--top-module", "fes_de25_embedded",
                   "-Wall", "-Wno-UNUSEDSIGNAL", "--Mdir", obj, "-I" + str(includes),
                   "-DFES_DE25_COMPUTER_BUILD_ID=128'h" + build_id, "-CFLAGS", "-std=c++17 -O2",
                   endpoint, wrapper, output / "probe.cpp"]
        env = PRODUCER.build_environment(paths)
        run("build", command, ROOT, env, output)
        executable = obj / "Vfes_de25_embedded"
        observed = json.loads(run("model", [executable, build_id, str(abi)], ROOT, env, output))
        if observed != {"status": "passed", "build_id": build_id, "control_abi": abi,
                        "held_address": True, "partial_drain": True, "fault": True}:
            raise ValueError("model returned unexpected observations")
        wrong = ("0" if build_id[0] != "0" else "1") + build_id[1:]
        run("wrong-id", [executable, wrong, str(abi)], ROOT, env, output, expect=1, marker="live build identity mismatch")
        run("wrong-abi", [executable, build_id, str(abi + 1)], ROOT, env, output, expect=1, marker="control ABI mismatch")
        tcl_env = dict(os.environ, FES_MODEL=str(executable), FES_DIAGNOSTIC_SCRIPT=str(HERE / "hardware.tcl"),
                       FES_DE25_SOF="simulation.sof", FES_DE25_DEVICE="simulated-device",
                       FES_DE25_MASTER="simulated-master", FES_DE25_BASE="0x110400",
                       FES_DE25_BUILD_ID=wrong, FES_DE25_ABI=str(abi))
        run("jtag-wrong-id", [tcl, HERE / "sim_jtag.tcl"], ROOT, tcl_env, output, expect=1,
            marker="FES_IDENTITY_REJECTED writes=0")
        tcl_env["FES_DE25_BUILD_ID"] = build_id
        result = run("jtag-model", [tcl, HERE / "sim_jtag.tcl"], ROOT, tcl_env, output,
                     marker="FES_DIAGNOSTIC_PASS")
        if "FES_DRAIN_PASS" not in result or "FES_FAULT_PASS" not in result or "FES_MASTER_CLOSED" not in result:
            raise ValueError("JTAG diagnostic did not report all cases and closure")
        native_env = dict(os.environ)
        for name in ("GOOS", "GOARCH", "GOARM", "GOAMD64", "GOARM64", "GOOSPKG", "GOFLAGS", "GOTOOLCHAIN",
                     "MAKEFLAGS", "MFLAGS", "MAKELEVEL", "GNUMAKEFLAGS", "PYTHON"):
            native_env.pop(name, None)
        native_env["GOWORK"] = "off"
        go_root = subprocess.check_output(["go", "env", "GOROOT"], cwd=TAMAGO, env=native_env, text=True).strip()
        native_env.update({"GOTOOLCHAIN": "local", "GOPROXY": "off", "GOSUMDB": "off",
                           "FES_DE25_BOARD_CONTROL_PROBE": str(executable), "FES_DE25_BOARD_CONTROL_ID": build_id,
                           "FES_DE25_BOARD_CONTROL_ABI": str(abi)})
        go = Path(go_root) / "bin/go"
        run("native", [go, "test", "-race", "-count=1", "-timeout", "45s", "-v", "-run",
                       "^TestBoardAvalonIdentityBindsNativeControl$", "./nativefabric"], TAMAGO, native_env, output)
        if (HERE / "probe.cpp").read_bytes() != source or tcl_sources != {name: sha(HERE / name) for name in tcl_sources}:
            raise ValueError("diagnostic test source changed during verification")
        if project:
            inputs_for(project)
        elif source_sha256 != {"endpoint": sha(ROOT / "sources/misteross/cores/fes-common/rtl/fes_embedded_control.sv"),
                               "wrapper": sha(HERE / "fes_de25_embedded.sv"),
                               "definition": sha(ROOT / "sources/misteross/cores/fes-common/generated/fes_embedded_control.vh")}:
            raise ValueError("standalone sources changed during verification")
        report = {"status": "passed", "classification": "generated DE25 diagnostic Avalon RTL and native adapter in host simulation" if project else "standalone Avalon RTL and native adapter in host simulation",
                  "board_revision": inputs["embedded_control"]["board_fes_revision"] if project else None,
                  "overlay_revision": inputs["embedded_control"]["overlay_fes_revision"] if project else None,
                  "overlay_source_state": inputs["embedded_control"]["overlay_source_state"] if project else "working-tree",
                  "build_id": build_id, "control_abi": abi, "observed": observed,
                  "generated_routes": generation["embedded_control"] if project else None, "tools": tools,
                  "go_sha256": sha(go.resolve()), "tcl_sha256": sha(Path(tcl).resolve()),
                  "probe_source_sha256": hashlib.sha256(source).hexdigest(), "tcl_source_sha256": tcl_sources,
                  "source_sha256": source_sha256, "model_sha256": sha(executable),
                  "project_inputs_sha256": sha(project / "board/hps/inputs.json") if project else None,
                  "generation_sha256": sha(project / "board/hps/generation.json") if project else None,
                  "command": [str(x) for x in command],
                  "logs_sha256": {name: sha(output / (name + ".log")) for name in ("build", "model", "wrong-id", "wrong-abi", "jtag-wrong-id", "jtag-model", "native")},
                  "limitations": ["host simulation only; no physical CPU MMIO or DDR path",
                                  "board source/vendor fingerprint is not a sealed FES package",
                                  "on-chip responder tests one reader; other FPGA masters are not contained"]}
        (output / "result.json").write_text(json.dumps(report, indent=2, sort_keys=True) + "\n")
        print("PASS:", output)
        return 0
    except (ValueError, OSError, KeyError, subprocess.SubprocessError, json.JSONDecodeError) as error:
        print("de25 board control verification:", error, file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
