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
import shutil
import subprocess
import sys
import zipfile

HERE = Path(__file__).resolve().parent
MISTEROSS = HERE.parents[1]
ROOT = MISTEROSS.parents[1]
sys.path.insert(0, str(MISTEROSS))
from scripts.core_package import encode_manifest, read_package
from scripts.export_core_package import build_identity, encode_build_record, export_package, source_input_closure
from scripts.source_repository import canonical_repository

RECIPE = "sources/misteross/prototypes/de25_board_control/build.py"
DEFINITION = "sources/misteross/cores/fes-common/generated/fes_embedded_control.vh"
ROOTS = sorted(("sources/misteross/prototypes/de25_board_control",
                "sources/misteross/scripts",
                "sources/misteross/cores/fes-common/rtl/fes_embedded_control.sv",
                DEFINITION))
PLATFORM = "de25_nano"
DEVICE = "A5EB013BB23BE4SCS"
PROFILE = "de25-embedded-diagnostic-v1"
ABI = "fes.de25-embedded-diagnostic"
CONTROL_ABI = 0x10203040
SPL_MEMBER = "Demonstration/SoC_FPGA/GHRD/software/u-boot/spl/u-boot-spl-dtb.hex"


def sha(data):
    return hashlib.sha256(data).hexdigest()


def snapshot_digest(sources):
    return sha((json.dumps(sources, sort_keys=True, separators=(",", ":")) + "\n").encode())


def tool_snapshot(tool, version):
    """Record selected executable identities; this is not toolchain authentication."""
    tool = Path(tool)
    names = ("quartus_sh", "quartus_ipgenerate", "quartus_map", "quartus_fit", "quartus_sta", "quartus_asm", "quartus_pfg")
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
    closure = source_input_closure(ROOT, ROOTS)
    fields = {
        "format": 2, "repository": canonical_repository(repository), "revision": revision, "source_path": ".",
        "recipe": RECIPE, "recipe_sha256": closure[RECIPE], "abi_definition": DEFINITION,
        "abi_definition_sha256": closure[DEFINITION], "source_roots": ROOTS, "source_inputs": closure,
        "dependencies": {}, "tools": tools,
        "parameters": {"classification": "de25-two-revision-diagnostic", "board_revision": inputs["board_fes_revision"],
                       "snapshot_sha256": snapshot_digest(sources), "vendor_sha256": inputs["vendor_archive_sha256"],
                       "device": DEVICE, "control_abi": CONTROL_ABI, "peripheral_offset": 0x10400,
                       "system_hz": 100000000, "pixel_hz": 74250000,
                       "spl_member": SPL_MEMBER, "conversion": "sof+hps-hex-to-sof;sof-to-rbf"},
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
    commands = [[str(pfg), "-c", str(artifact), str(combined), "-o", "hps_path=" + str(spl)],
                [str(pfg), "-c", str(combined), str(payload)]]
    for i, command in enumerate(commands):
        result = subprocess.run(command, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, check=False)
        (output / f"pfg-{i}.log").write_bytes(result.stdout)
        if result.returncode:
            raise ValueError("diagnostic RBF conversion failed; see pfg log")
    data = payload.read_bytes()
    if not data:
        raise ValueError("empty diagnostic RBF")
    (output / "build-inputs.json").write_bytes(record)
    manifest = encode_manifest({
        "format": 2,
        "core": {"id": "fes.de25-embedded-diagnostic", "name": "DE25 embedded control diagnostic",
                 "description": "Exploratory diagnostic; not qualified for physical reconfiguration.", "version": "0.1.0"},
        "target": {"platform": PLATFORM, "device": DEVICE, "programming_profile": PROFILE},
        "payload": {"file": "core.rbf", "size": len(data), "sha256": sha(data)},
        "abi": {"id": ABI, "major": 1, "minor": 0}, "interfaces": [],
        "build": {"id": build_identity(record), "repository": fields["repository"], "revision": fields["revision"],
                  "recipe_sha256": fields["recipe_sha256"], "toolchain": expected_tools["quartus-version"]},
    })
    directory = export_package(manifest, payload, output / "packages")
    package = read_package(directory)
    result = {"classification": "sealed diagnostic only; physical HPS reconfiguration not qualified",
              "build_id": build_identity(record), "build_inputs_sha256": sha(record),
              "package_id": package.package_id, "rbf_sha256": sha(data), "rbf_size": len(data),
              "raw_sof_sha256": sha(artifact.read_bytes()), "combined_sof_sha256": sha(combined.read_bytes()),
              "vendor_spl_sha256": sha(spl.read_bytes()), "commands": commands}
    (output / "seal-result.json").write_text(json.dumps(result, indent=2, sort_keys=True) + "\n")
    return result
