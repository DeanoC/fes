#!/usr/bin/env python3
"""Construct the diagnostic Nano's canonical record and format-2 package.

The board source comes from a second Git revision. Its entire captured snapshot
and vendor archive are named in the functional inputs; this is not a
production producer or evidence that the RBF is safe for HPS reconfiguration.
"""
from __future__ import annotations

import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import zipfile

HERE = Path(__file__).resolve().parent
MISTEROSS = HERE.parents[1]
ROOT = MISTEROSS.parents[1]
sys.path.insert(0, str(HERE))
import reader_memory
import computer_model
sys.path.insert(0, str(MISTEROSS))
from scripts.core_package import encode_manifest, package_identity, read_package
from scripts.export_core_package import build_identity, encode_build_record, export_package, source_input_closure
from scripts.source_repository import canonical_repository

RECIPE = "sources/misteross/prototypes/de25_board_control/build.py"
DEFINITION = "sources/misteross/cores/fes-common/generated/fes_embedded_control.vh"
ROOTS = sorted(("sources/misteross/prototypes/de25_board_control",
                "sources/misteross/scripts",
                "sources/misteross/cores/fes-common/rtl/fes_embedded_control.sv",
                "sources/misteross/prototypes/embedded_control/fes_embedded_window_reader.sv",
                "sources/misteross/prototypes/embedded_control/fes_embedded_window_control.sv",
                DEFINITION))
PLATFORM = "de25_nano"
DEVICE = "A5EB013BB23BE4SCS"
PROFILE = "de25-embedded-diagnostic-v1"
HPS_PROFILE = "de25-embedded-hps-first-diagnostic-v1"
DDR_PROFILE = "de25-embedded-ddr-diagnostic-v1"
HPS_DDR_PROFILE = "de25-embedded-hps-first-ddr-diagnostic-v1"
WINDOW_PROFILE = "de25-embedded-window-diagnostic-v1"
HPS_WINDOW_PROFILE = "de25-embedded-hps-first-window-diagnostic-v1"
ABI = "fes.de25-embedded-diagnostic"
SPL_MEMBER = "Demonstration/SoC_FPGA/GHRD/software/u-boot/spl/u-boot-spl-dtb.hex"


def boot_order(inputs):
    order = inputs.get("computer", {}).get("hps_initialization", "AFTER INIT_DONE")
    if order not in ("HPS FIRST", "AFTER INIT_DONE"):
        raise ValueError("unsupported HPS initialization order")
    return order


def profile_for(inputs):
    mode=reader_memory.mode(inputs['reader_memory'])
    profiles=(HPS_PROFILE,HPS_DDR_PROFILE,HPS_WINDOW_PROFILE) if boot_order(inputs)=='HPS FIRST' else (PROFILE,DDR_PROFILE,WINDOW_PROFILE)
    return profiles[mode]


def sha(data):
    return hashlib.sha256(data).hexdigest()


def snapshot_digest(sources):
    return sha((json.dumps(sources, sort_keys=True, separators=(",", ":")) + "\n").encode())


def tool_snapshot(tool, version):
    """Record selected executable identities; this is not toolchain authentication."""
    tool = Path(tool)
    names = ("quartus_sh", "quartus_ipgenerate", "quartus_syn", "quartus_fit", "quartus_sta", "quartus_asm", "quartus_pfg")
    tools = {"quartus-version": version.splitlines()[1].strip()}
    for name in names:
        wrapper = tool.parent / name
        binary = tool.parent.parent / "linux64" / name
        if not wrapper.is_file() or not binary.is_file():
            raise ValueError("missing selected Quartus executable: " + name)
        tools[name.replace("_", "-")] = "wrapper-sha256:" + sha(wrapper.read_bytes()) + "; binary-sha256:" + sha(binary.read_bytes())
    qsys = tool.parent.parent / "sopc_builder/bin/qsys-generate"
    if not qsys.is_file():
        raise ValueError("missing selected qsys-generate")
    tools["qsys-generate"] = "sha256:" + sha(qsys.read_bytes())
    for name, path in (("python", Path(sys.executable).resolve()), ("git", Path(shutil.which("git") or "").resolve())):
        if not path.is_file():
            raise ValueError("missing selected " + name)
        tools[name] = "sha256:" + sha(path.read_bytes())
    return tools


def make_record(inputs, revision, repository, tools):
    if inputs.get("device") != DEVICE or not inputs.get("diagnostic_only"):
        raise ValueError("not the expected diagnostic Nano project")
    sources = inputs["functional_snapshot_sha256"]
    if not sources or not inputs["board_fes_revision"] or inputs["overlay_source_state"] != "committed":
        raise ValueError("sealed diagnostic requires committed, captured sources")
    processors = inputs.get('compile_processors')
    if type(processors) is not int or not 1 <= processors <= 16:
        raise ValueError('sealed diagnostic requires explicit compile processor count')
    closure = source_input_closure(ROOT, ROOTS)
    order = boot_order(inputs)
    fields = {
        "format": 2, "repository": canonical_repository(repository), "revision": revision, "source_path": ".",
        "recipe": RECIPE, "recipe_sha256": closure[RECIPE], "abi_definition": DEFINITION,
        "abi_definition_sha256": closure[DEFINITION], "source_roots": ROOTS, "source_inputs": closure,
        "dependencies": {}, "tools": tools,
        "parameters": {"classification": "de25-two-revision-diagnostic", "board_revision": inputs["board_fes_revision"],
                       "snapshot_sha256": snapshot_digest(sources), "vendor_sha256": inputs["vendor_archive_sha256"],
                       "device": DEVICE, "control_abi": reader_memory.control_abi(inputs['reader_memory']), "peripheral_offset": 0x10400,
                       **reader_memory.parameters(inputs['reader_memory']),
                       **computer_model.parameters(inputs['computer_model']),
                       "system_hz": 100000000, "pixel_hz": 74250000,
                       "spl_member": SPL_MEMBER, "hps_initialization": order,
                       "compile_flow": "quartus-pro-bounded-tasks-v1",
                       "parallel_processors": processors,
                       "conversion": "hps-first-split-rbf-v1" if order == "HPS FIRST" else "sof+hps-hex-to-sof;sof-to-rbf"},
    }
    return encode_build_record(fields)


def publish(output, artifact, archive, tool, record, expected_tools, version):
    output, artifact, archive, tool = map(Path, (output, artifact, archive, tool))
    inputs = json.loads((output / "inputs.json").read_text())
    fields = json.loads(record)
    if make_record(inputs, fields["revision"], fields["repository"], tool_snapshot(tool, version)) != record or expected_tools != tool_snapshot(tool, version):
        raise ValueError("functional inputs or selected tools changed during build")
    snapshot = output.parent / "functional-source"
    for name, expected in inputs["functional_snapshot_sha256"].items():
        if sha((snapshot / name).read_bytes()) != expected:
            raise ValueError("captured board source changed: " + name)
    if sha(archive.read_bytes()) != inputs["vendor_archive_sha256"]:
        raise ValueError("vendor archive changed during build")
    spl = output / "project/software/u-boot/spl/u-boot-spl-dtb.hex"
    with zipfile.ZipFile(archive) as vendor:
        if spl.read_bytes() != vendor.read(SPL_MEMBER):
            raise ValueError("project SPL differs from captured vendor archive")
    pfg = tool.with_name("quartus_pfg")
    combined, payload = output / "diagnostic-with-vendor-spl.sof", output / "core.rbf"
    order = boot_order(inputs)
    qsf = (output / "project/golden_top.qsf").read_text()
    assigned = re.findall(r'(?m)^\s*set_global_assignment\s+-name\s+HPS_INITIALIZATION\s+"([^"\n]+)"\s*$', qsf)
    if (order == "HPS FIRST" and assigned != [order]) or (order != "HPS FIRST" and assigned not in ([], [order])):
        raise ValueError("project HPS initialization differs from captured build inputs")
    commands = [[str(pfg), "-c", str(artifact), str(combined), "-o", "hps_path=" + str(spl)],
                ([str(pfg), "-c", str(artifact), str(output / "hps-first.rbf"), "-o", "hps_path=" + str(spl), "-o", "hps=on"]
                 if order == "HPS FIRST" else [str(pfg), "-c", str(combined), str(payload)])]
    for i, command in enumerate(commands):
        result = subprocess.run(command, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, check=False, env=dict(os.environ, LC_ALL="C"))
        (output / f"pfg-{i}.log").write_bytes(result.stdout)
        if result.returncode:
            raise ValueError("diagnostic RBF conversion failed; see pfg log")
    phase1 = output / "hps-first.hps.rbf"
    core = output / "hps-first.core.rbf"
    io_hash = None
    if order == "HPS FIRST":
        for part in (phase1, core):
            if part.is_symlink() or not part.is_file() or part.stat().st_size == 0:
                raise ValueError("missing or invalid HPS-first split artifact")
        hashes = []
        for label, path in (("hps", phase1), ("core", core)):
            command = [str(pfg), "-i", str(path)]
            commands.append(command)
            result = subprocess.run(command, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, check=False, env=dict(os.environ, LC_ALL="C"))
            (output / f"pfg-info-{label}.log").write_bytes(result.stdout)
            if result.returncode:
                raise ValueError("cannot inspect HPS-first artifact")
            hashes.append(parse_pfg_info(result.stdout))
        assembly = parse_assembler_hash((output / "project/output_files/golden_top.asm.rpt").read_text())
        if hashes[0] != hashes[1] or hashes[0] != assembly:
            raise ValueError("HPS-first split and assembler HPS IO hashes differ")
        io_hash = hashes[0]
        payload.write_bytes(core.read_bytes())
    data = payload.read_bytes()
    if not data:
        raise ValueError("empty diagnostic RBF")
    (output / "build-inputs.json").write_bytes(record)
    manifest = encode_manifest({
        "format": 2,
        "core": {"id": "fes.de25-embedded-diagnostic", "name": "DE25 embedded control diagnostic",
                 "description": "Exploratory diagnostic; not qualified for physical reconfiguration.", "version": "0.1.0"},
        "target": {"platform": PLATFORM, "device": DEVICE, "programming_profile": profile_for(inputs)},
        "payload": {"file": "core.rbf", "size": len(data), "sha256": sha(data)},
        "abi": {"id": ABI, "major": 1, "minor": 0}, "interfaces": [],
        "build": {"id": build_identity(record), "repository": fields["repository"], "revision": fields["revision"],
                  "recipe_sha256": fields["recipe_sha256"], "toolchain": expected_tools["quartus-version"]},
    })
    pair = None
    if order == "HPS FIRST":
        pair = {"format": 1, "classification": "unqualified-hps-first-diagnostic",
                "package_id": package_identity(manifest, data), "build_id": build_identity(record),
                "build_inputs_sha256": sha(record), "hps_io_hash": io_hash,
                "pfg_identity": expected_tools["quartus-pfg"], "quartus_version": expected_tools["quartus-version"],
                "phase1_sha256": sha(phase1.read_bytes()), "phase1_size": phase1.stat().st_size,
                "core_sha256": sha(data), "core_size": len(data), "vendor_spl_sha256": sha(spl.read_bytes())}
        pair_bytes = (json.dumps(pair, sort_keys=True, separators=(",", ":")) + "\n").encode()
        pair_path = output / "hps-first-pair.json"
        with pair_path.open("xb") as stream:
            stream.write(pair_bytes)
        pair_path.chmod(0o444)
    directory = export_package(manifest, payload, output / "packages")
    package = read_package(directory)
    if pair is not None and pair["package_id"] != package.package_id:
        raise ValueError("HPS-first pair package identity differs")
    result = {"classification": "sealed diagnostic only; physical HPS reconfiguration not qualified",
              "build_id": build_identity(record), "build_inputs_sha256": sha(record),
              "package_id": package.package_id, "rbf_sha256": sha(data), "rbf_size": len(data),
              "raw_sof_sha256": sha(artifact.read_bytes()), "combined_sof_sha256": sha(combined.read_bytes()),
              "vendor_spl_sha256": sha(spl.read_bytes()), "commands": commands}
    if pair is not None:
        result["hps_first_pair_sha256"] = sha(pair_bytes)
    (output / "seal-result.json").write_text(json.dumps(result, indent=2, sort_keys=True) + "\n")
    return result


def parse_pfg_info(data):
    # PFG emits a single-byte degree symbol even under LC_ALL=C. Decode
    # losslessly while continuing to match the ASCII identity fields exactly.
    text = data.decode("latin-1")
    hashes = re.findall(r"(?m)^HPS IO hash:\s*([0-9A-Fa-f]{64})\s*$", text)
    devices = re.findall(r"(?m)^Device name:\s*(\S+)\s*$", text)
    if len(hashes) != 1 or devices != [DEVICE]:
        raise ValueError("PFG did not report one exact device and HPS IO hash")
    return hashes[0].lower()


def parse_assembler_hash(text):
    hashes = re.findall(r"(?mi)^\s*;\s*HPS IO hash\s*;\s*([0-9a-f]{64})\s*;", text)
    if len(hashes) != 1:
        raise ValueError("assembler did not report one HPS IO hash")
    return hashes[0].lower()
