#!/usr/bin/env python3
"""Build a tiny exact-device PR artifact proof; never configure an FPGA."""
from __future__ import annotations

import argparse
from decimal import Decimal
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess

BOARD = Path(__file__).resolve().parent
DEVICE = "A5EB013BB23BE4SCS"
PROJECT = "fes_link_proof"
TOOL_VERSION = "26.3.0"
SOURCE_NAMES = ("build.py", "build_timing.tcl", "clocks.sdc", "fpga/top.v", "fpga/shell.v", "fpga/personas.v",
                "fpga/persona_a.hex", "fpga/persona_b.hex")
PARENT_NAMES = ("reset_release.ip", "pins.qsf")
INTERFACE = {"name": "fes.de25.registered-pr-proof", "version": 1, "clock_hz": 50000000,
             "request": {"valid": 1, "address": 8}, "response": {"valid": 1, "data": 32},
             "latency_cycles": 2, "reset": "active-high-synchronous", "backpressure": False}
CLOCK_RESET = {"clock": "board50", "period_ns": "20.000", "source": "CLOCK0_50",
               "freeze_control": "virtual-diagnostic-input-no-hps-backend",
               "sequence": ["stop-requests", "drain-responses", "freeze-ack",
                            "clamp-outputs-and-hold-local-reset", "load-persona",
                            "release-local-reset-and-resume"],
               "cold_reset": "SDM-ninit_done-two-register-release"}


class BuildError(ValueError):
    pass


def canonical(value: object) -> bytes:
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=True).encode("ascii")


def sha(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def verify_files(directory: Path, expected: dict[str, str]) -> None:
    for name, wanted in expected.items():
        if sha((directory / name).read_bytes()) != wanted:
            raise BuildError(f"selected compiler input changed: {name}")


def verify_qpf(directory: Path) -> dict:
    # Quartus writes its own date/comment header and normalizes 26.3.0 to 26.3.
    # It also moves the current revision first. Commands select -c explicitly;
    # membership remains strict while this tool-owned order is recorded.
    source = (directory / f"{PROJECT}.qpf").read_text()
    records = []
    for line in source.splitlines():
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        match = re.fullmatch(r'([A-Z_]+)\s*=\s*"([^"\r\n]*)"\s*', line)
        if match is None or match[1] not in ("QUARTUS_VERSION", "DATE", "PROJECT_REVISION"):
            raise BuildError("unexpected project-file assignment")
        records.append((match[1], match[2]))
    versions = [value for key, value in records if key == "QUARTUS_VERSION"]
    revisions = [value for key, value in records if key == "PROJECT_REVISION"]
    if versions not in (["26.3"], ["26.3.0"]) or sorted(revisions) != ["base", "persona_a", "persona_b"]:
        raise BuildError("Quartus project revision/version selection changed")
    return {"sha256": sha(source.encode()), "revisions": revisions, "version": versions[0]}


def verify_project(directory: Path, inputs: dict) -> None:
    verify_files(directory, inputs["project_sha256"])
    verify_qpf(directory)


def report_table(text: str, title: str) -> list[dict[str, str]]:
    match = re.search(r"^;\s*" + re.escape(title) + r"\s*;\s*$", text, re.MULTILINE)
    if match is None:
        raise BuildError(f"missing Quartus report table: {title}")
    block = text[match.end():].split("\n\n", 1)[0]
    rows = [[cell.strip() for cell in line.strip().strip(";").split(";")]
            for line in block.splitlines() if line.strip().startswith(";")]
    if len(rows) < 2 or any(len(row) != len(rows[0]) for row in rows):
        raise BuildError(f"malformed Quartus report table: {title}")
    return [dict(zip(rows[0], row)) for row in rows[1:]]


def verify_regions(rows: list[dict[str, str]], region: dict, revision: str) -> None:
    # Imported root QDBs retain the coordinates but Quartus labels the region
    # with the hierarchy instance in implementation reports, not its base name.
    expected_name = region["id"] if revision == "base" else region["instance"]
    def report_coordinates(value: str) -> str:
        match = re.fullmatch(r"X([0-9]+) Y([0-9]+) X([0-9]+) Y([0-9]+)", value)
        if match is None:
            raise BuildError("invalid captured region coordinates")
        x0, y0, x1, y1 = map(int, match.groups())
        return f"({x0}, {y0}) to ({x1}, {y1})"
    if (len(rows) != 1 or rows[0].get("Name") != expected_name
            or rows[0].get("Place Region Constraint") != report_coordinates(region["placement"])
            or rows[0].get("Route Region Constraint") != report_coordinates(region["routing"])):
        raise BuildError(f"{revision}: fitted PR region differs from captured name or coordinates")


def timing_summary(output: Path, revision: str) -> dict:
    directory = output / "output_files"
    summary = (directory / f"{revision}.sta.summary").read_text()
    report = (directory / f"{revision}.sta.rpt").read_text()
    matches = re.findall(r"Type\s*:\s*(.+)\nSlack\s*:\s*([^\n]+)\nTNS\s*:\s*([^\n]+)\nCorner:\s*([^\n]+)", summary)
    if not matches:
        raise BuildError(f"{revision}: missing constrained timing rows")
    rows = [{"type": kind, "slack_ns": slack.strip(), "tns_ns": tns.strip(), "corner": corner.strip()}
            for kind, slack, tns, corner in matches]
    for row in rows:
        if Decimal(row["slack_ns"]) < 0 or Decimal(row["tns_ns"]) != 0:
            raise BuildError(f"{revision}: constrained timing failed: {row}")
    categories = ("Setup", "Hold", "Recovery", "Removal", "Minimum Pulse Width")
    if any(not any(row["type"].startswith(kind + " '") for row in rows) for kind in categories):
        raise BuildError(f"{revision}: incomplete five-category timing coverage")
    coverage = report_table(report, "Unconstrained Paths Summary")
    clocks = report_table(report, "Clock Status Summary")
    for property_name in ("Illegal Clocks", "Unconstrained Clocks"):
        found = [row for row in coverage if row.get("Property") == property_name]
        if len(found) != 1 or found[0].get("Setup") != "0" or found[0].get("Hold") != "0":
            raise BuildError(f"{revision}: illegal or unconstrained clocks")
    if any(row.get("Status") != "Constrained" for row in clocks):
        raise BuildError(f"{revision}: clock coverage is incomplete")
    return {"status": "passed", "scope": "constrained-internal-timing-only",
            "rows": rows, "coverage": coverage, "clocks": clocks,
            "closure": report_table(report, "Design Closure Summary"),
            "external_io": "human-visible-LEDs-and-virtual-freeze-control-not-physical-timing-qualification",
            "report_sha256": {f"{revision}.sta.summary": sha(summary.encode()),
                              f"{revision}.sta.rpt": sha(report.encode())}}


def vendor_pof(log: Path) -> dict[str, str]:
    text = log.read_text(errors="replace")
    def field(name: str, pattern: str) -> str:
        values = re.findall(r"^" + re.escape(name) + r": (" + pattern + r")$", text, re.MULTILINE)
        if len(values) != 1:
            raise BuildError(f"{log.name}: missing or ambiguous vendor {name}")
        return values[0]
    if field("Device name", r"\S+") != DEVICE:
        raise BuildError("vendor-decoded device does not match exact proof target")
    return {"region_id": field("POF0 region ID", r"0x[0-9A-Fa-f]{8}").lower(),
            "hash": field("POF0 hash", r"[0-9A-Fa-f]{64}").lower(),
            "info_sha256": sha(log.read_bytes())}


def blob(output: Path, path: Path) -> dict:
    if not path.is_file() or path.stat().st_size == 0:
        raise BuildError(f"missing nonempty artifact: {path}")
    return {"path": path.relative_to(output).as_posix(), "size": path.stat().st_size,
            "sha256": sha(path.read_bytes())}


def collect(output: Path, tools: dict[str, Path], version: str) -> Path:
    inputs = json.loads((output / "inputs.json").read_text())
    if "project_sha256" in inputs:
        verify_project(output, inputs)
    selected_tools = tool_hashes(tools)
    if (output / "tools.json").exists():
        if json.loads((output / "tools.json").read_text())["sha256"] != selected_tools:
            raise BuildError("Quartus executable closure changed after compilation")
    elif inputs["source_committed"]:
        raise BuildError("missing frozen compiler tool identity")
    directory = output / "artifacts"
    directory.mkdir(exist_ok=False)
    summaries = {}
    decoded = {}
    raw_hashes = {}
    root_qdb = blob(output, output / "root_partition.qdb")
    run([str(tools["quartus_pfg"]), "-c", str(output / "output_files/base.sof"), str(directory / "base.rbf")],
        output, "base-convert.log")
    run([str(tools["quartus_pfg"]), "-i", str(directory / "base.rbf")], output, "base-info.log")
    base_pof = vendor_pof(output / "base-info.log")
    run([str(tools["quartus_pfg"]), "--check_integrity", str(directory / "base.rbf")], output, "base-integrity.log")
    for revision in ("base", "persona_a", "persona_b"):
        if "Critical Warning (" in (output / f"{revision}-compile.log").read_text():
            raise BuildError(f"{revision}: unresolved critical compiler warning")
        summaries[revision] = timing_summary(output, revision)
        fit = (output / "output_files" / f"{revision}.fit.rpt").read_text()
        memories = report_table(fit, "Fitter RAM Summary")
        if (len(memories) != 1 or memories[0].get("Type") != "M20K"
                or memories[0].get("Logical Size") != "8192" or memories[0].get("M20K blocks") != "1.000"
                or memories[0].get("MLABs") != "0.000"):
            raise BuildError(f"{revision}: expected exactly one 256x32 M20K and no extra RAM")
        regions = report_table(fit, "Logic Lock Region Constraints")
        verify_regions(regions, inputs["region"], revision)
        summaries[revision]["memory"] = memories
        summaries[revision]["regions"] = regions
        summaries[revision]["partition_reuse"] = report_table(fit, "Partial Reconfiguration and Periphery Reuse Statistics")
        drc = (output / "output_files" / f"{revision}.tq.drc.signoff.rpt").read_text()
        failures = []
        for rule, severity, violations, waived in re.findall(
                r"^; ([A-Z]+-[0-9]+[^;]*?)\s*;\s*(Critical|High|Medium|Low)\s*;\s*([0-9]+)\s*;\s*([0-9]+)\s*;", drc, re.MULTILINE):
            if int(waived):
                raise BuildError("proof must not contain waived Design Assistant findings")
            if int(violations):
                failures.append({"rule": rule.strip(), "severity": severity, "violations": int(violations)})
                if severity in ("Critical", "High") and not rule.startswith("TMC-20012 "):
                    raise BuildError(f"{revision}: unqualified internal Design Assistant failure {rule}")
        summaries[revision]["unwaived_design_assistant_findings"] = failures
        sta_report = (output / "output_files" / f"{revision}.sta.rpt").read_text()
        summaries[revision]["metastability"] = {
            "status": next(row["Result Flag"] for row in summaries[revision]["closure"]
                           if row["Panel Name"] == "Metastability Summary"),
            "found_counts": sorted(set(int(v) for v in re.findall(r"Number of Synchronizer Chains Found: (\d+)", sta_report))),
            "excluded_counts": sorted(set(int(v) for v in re.findall(r"Number of Chains Excluded from MTBF Analysis: (\d+)", sta_report)))}
        for path in sorted((output / "output_files").glob(f"{revision}.*")):
            if path.suffix in (".rpt", ".summary", ".sof", ".rbf", ".pmsf", ".pin"):
                raw_hashes[path.relative_to(output).as_posix()] = sha(path.read_bytes())
        before_audit = dict(raw_hashes)
        before_audit["root_partition.qdb"] = root_qdb["sha256"]
        run([str(tools["quartus_sta"]), "-t", str(output / "build_timing.tcl"), str(output), revision,
             str(output / f"timing-{revision}")], output, f"{revision}-timing-audit.log")
        verify_files(output, before_audit)
        summaries[revision]["synchronizer_audit"] = {
            "log_sha256": sha((output / f"{revision}-timing-audit.log").read_bytes()),
            "paths_sha256": sha((output / f"timing-{revision}/synchronizer-paths.tsv").read_bytes())}
        if revision == "base":
            continue
        log = (output / f"{revision}-compile.log").read_text()
        if 'Loading "final" snapshot for partition "root_partition"' not in log:
            raise BuildError(f"{revision}: no actual final-root import evidence")
        source = output / "output_files" / f"{revision}.slot0.rbf"
        target = directory / f"counter_{revision[-1]}.rbf"
        shutil.copyfile(source, target)
        run([str(tools["quartus_pfg"]), "-i", str(target)], output, f"{revision}-info.log")
        run([str(tools["quartus_pfg"]), "--check_integrity", str(target)], output, f"{revision}-integrity.log")
        pof = vendor_pof(output / f"{revision}-info.log")
        if any(pof[key] != base_pof[key] for key in ("region_id", "hash")):
            raise BuildError(f"{revision}: vendor-decoded POF0 does not match base")
        decoded[revision] = pof
        # This vendor comparator currently reports the absent owner key for
        # unsigned files. Preserve its output; POF matching above is separate.
        run([str(tools["quartus_pfg"]), "--compare", str(directory / "base.rbf"),
             str(output / "output_files" / f"{revision}.slot0.pmsf")], output, f"{revision}-compare.log")
    if tool_hashes(tools) != selected_tools:
        raise BuildError("Quartus executable closure changed during verification")
    if "project_sha256" in inputs:
        verify_project(output, inputs)
    interface_hash = sha(canonical(inputs["interface"]))
    reset_hash = sha(canonical(inputs["clock_reset"]))
    descriptor = {"format": 1, "kind": "fes.de25-pr-proof-artifacts", "diagnostic_only": True,
        "device": DEVICE,
        "tool": {"version": "26.3.0 Build114", "edition": "SC Pro Edition",
                 "quartus_sh_sha256": selected_tools["quartus_sh"],
                 "quartus_asm_sha256": selected_tools["quartus_asm"],
                 "quartus_pfg_sha256": selected_tools["quartus_pfg"]},
        "shell": {"source_revision": inputs["source_revision"],
                  "source_closure_sha256": inputs["source_closure_sha256"],
                  "root_qdb_sha256": root_qdb["sha256"], "interface_sha256": interface_hash,
                  "clock_reset_sha256": reset_hash, "base": blob(output, directory / "base.rbf"),
                  "vendor_pof": base_pof},
        "region": inputs["region"], "personas": [],
        "acceptance": {"physical": "not-run", "timing": "passed",
                       "base_persona_compatibility": "pfg-info-pof0-match"}}
    descriptor["shell"]["id"] = sha(b"fes-de25-pr-shell-v1\0" + canonical(
        {key: descriptor[key] for key in ("device", "tool", "shell", "region")}))
    for suffix in ("a", "b"):
        persona = {"id": f"counter_{suffix}", "entity": f"fes_link_persona_{suffix}",
                   "revision": f"persona_{suffix}", **blob(output, directory / f"counter_{suffix}.rbf"),
                   "root_qdb_sha256": root_qdb["sha256"], "interface_sha256": interface_hash,
                   "clock_reset_sha256": reset_hash, "region_id": "slot0",
                   "vendor_pof": decoded[f"persona_{suffix}"]}
        descriptor["personas"].append(persona)
    manifest = output / "artifacts.json"
    manifest.write_bytes(canonical(descriptor) + b"\n")
    verifier = capture()
    build = {"format": 1, "kind": "fes.de25-quartus-pr-build", "diagnostic_only": True,
             "source_revision": inputs["source_revision"], "source_committed": inputs["source_committed"],
             "source_closure_sha256": inputs["source_closure_sha256"], "device": DEVICE,
             "quartus_version": version, "tool_sha256": selected_tools,
             "qpf_initial": inputs.get("qpf"), "qpf_final": verify_qpf(output),
             "compiler_status": "passed", "timing_status": "passed",
             "compatibility_status": "pfg-info-pof0-match", "hardware_acceptance": "not-run",
             "artifact_manifest": blob(output, manifest), "root_qdb": root_qdb,
             "timing": summaries, "compiled_report_sha256": raw_hashes,
             "compatibility_scope": "matching vendor-decoded POF0 region/hash and exact imported root QDB; no SDM execution",
             "pfg_compare_scope": "retained vendor output; unsigned owner-key field absent, not a complete pairing check"}
    build["verification_recipe"] = {"source_revision": verifier["revision"],
        "source_committed": verifier["committed"],
        "source_sha256": {name: sha(data) for name, data in verifier["files"].items()}}
    (output / "build.json").write_bytes(canonical(build) + b"\n")
    return manifest


def run(command: list[str], cwd: Path, log: str) -> None:
    environment = dict(os.environ)
    environment.pop("QUARTUS_ROOTDIR_OVERRIDE", None)
    with (cwd / "commands.jsonl").open("ab") as commands:
        commands.write(canonical({"argv": command, "cwd": str(cwd), "log": log}) + b"\n")
    with (cwd / log).open("w") as handle:
        result = subprocess.run(command, cwd=cwd, env=environment, stdout=handle, stderr=subprocess.STDOUT, check=False)
    if result.returncode:
        raise BuildError(f"command failed ({result.returncode}); retained {cwd / log}")


def toolchain(root: Path) -> tuple[dict[str, Path], str]:
    candidates = (root / "quartus/bin", root / "bin")
    directory = next((p for p in candidates if (p / "quartus_sh").is_file()), None)
    if directory is None:
        raise BuildError("explicit Quartus root must contain quartus/bin/quartus_sh or bin/quartus_sh")
    tools = {name: directory / name for name in
             ("quartus_sh", "quartus_syn", "quartus_fit", "quartus_ipgenerate", "quartus_cdb", "quartus_pfg", "quartus_asm", "quartus_sta")}
    if any(not p.is_file() or not os.access(p, os.X_OK) for p in tools.values()):
        raise BuildError("Quartus tool closure is incomplete")
    environment = dict(os.environ)
    environment.pop("QUARTUS_ROOTDIR_OVERRIDE", None)
    result = subprocess.run([str(tools["quartus_sh"]), "--version"], text=True, env=environment,
                            stdout=subprocess.PIPE, stderr=subprocess.STDOUT, check=False)
    version = result.stdout.strip()
    if result.returncode or not re.search(r"\bVersion 26\.3\.0 Build 114\b", version) or "SC Pro Edition" not in version:
        raise BuildError(f"requires Quartus 26.3.0 Build114 SC Pro Edition; got {version}")
    return tools, version


def tool_hashes(tools: dict[str, Path]) -> dict[str, str]:
    result = {}
    for name, launcher in tools.items():
        binary = launcher.parent.parent / "linux64" / name
        if not binary.is_file():
            raise BuildError(f"missing selected Linux tool executable: {binary}")
        result[name] = sha(binary.read_bytes())
        result[name + "_launcher"] = sha(launcher.read_bytes())
    return result


def capture() -> dict:
    repository = Path(subprocess.check_output(["git", "rev-parse", "--show-toplevel"], cwd=BOARD, text=True).strip())
    revision = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=BOARD, text=True).strip()
    paths = [BOARD / name for name in SOURCE_NAMES] + [BOARD.parent / name for name in PARENT_NAMES]
    files = {p.relative_to(repository).as_posix(): p.read_bytes() for p in paths}
    committed = True
    for name, data in files.items():
        result = subprocess.run(["git", "show", f"{revision}:{name}"], cwd=repository,
                                stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, check=False)
        committed = committed and result.returncode == 0 and result.stdout == data
    return {"repository": repository, "revision": revision, "files": files, "committed": committed}


def prepare(output: Path, placement: str, routing: str) -> Path:
    for region in (placement, routing):
        if re.fullmatch(r"X[0-9]+ Y[0-9]+ X[0-9]+ Y[0-9]+", region) is None:
            raise BuildError("region must contain exactly four canonical device coordinates")
    frozen = capture()
    output = output.resolve()
    output.mkdir(parents=True, exist_ok=False)
    for name, data in frozen["files"].items():
        (output / Path(name).name).write_bytes(data)
    common = (
        'set_global_assignment -name FAMILY "Agilex 5"\n'
        f'set_global_assignment -name DEVICE {DEVICE}\n'
        f'set_global_assignment -name TOP_LEVEL_ENTITY {PROJECT}\n'
        'set_global_assignment -name PROJECT_OUTPUT_DIRECTORY output_files\n'
        'set_global_assignment -name NUM_PARALLEL_PROCESSORS 2\n'
        'set_global_assignment -name SEED 1\n'
        'set_global_assignment -name ENABLE_PR_POF_ID ON\n'
        'set_global_assignment -name STRATIXV_CONFIGURATION_SCHEME "ACTIVE SERIAL X4"\n'
        'set_global_assignment -name ACTIVE_SERIAL_CLOCK AS_FREQ_125MHZ\n'
        'set_global_assignment -name DEVICE_INITIALIZATION_CLOCK OSC_CLK_1_125MHZ\n'
        'set_global_assignment -name RESERVE_ALL_UNUSED_PINS "AS INPUT TRI-STATED"\n'
        'set_global_assignment -name RESERVE_ALL_UNUSED_PINS_NO_OUTPUT_GND "AS INPUT TRI-STATED"\n'
        'set_global_assignment -name PWRMGT_VOLTAGE_OUTPUT_FORMAT "LINEAR FORMAT"\n'
        'set_global_assignment -name PWRMGT_LINEAR_FORMAT_N "-12"\n'
        'set_global_assignment -name LAST_QUARTUS_VERSION "26.3.0 Pro Edition"\n'
        'set_global_assignment -name IP_FILE reset_release.ip\n'
        'set_global_assignment -name VERILOG_FILE top.v\n'
        'set_global_assignment -name VERILOG_FILE shell.v\n'
        'set_global_assignment -name VERILOG_FILE personas.v\n'
        'set_instance_assignment -name VIRTUAL_PIN ON -to freeze_req\n'
        'source pins.qsf\n'
    )
    partition = (
        'set_instance_assignment -name PARTITION slot0 -to slot_inst\n'
        'set_instance_assignment -name PARTIAL_RECONFIGURATION_PARTITION ON -to slot_inst\n'
        f'set_instance_assignment -name PLACE_REGION "{placement}" -to slot_inst\n'
        f'set_instance_assignment -name ROUTE_REGION "{routing}" -to slot_inst\n'
        'set_instance_assignment -name RESERVE_PLACE_REGION ON -to slot_inst\n'
        'set_instance_assignment -name RESERVE_ROUTE_REGION ON -to slot_inst\n'
        'set_instance_assignment -name CORE_ONLY_PLACE_REGION ON -to slot_inst\n'
    )
    (output / f"{PROJECT}.qpf").write_text('QUARTUS_VERSION = "26.3"\nPROJECT_REVISION = "base"\n'
                                           'PROJECT_REVISION = "persona_a"\nPROJECT_REVISION = "persona_b"\n')
    (output / "base.qsf").write_text(common + 'set_global_assignment -name REVISION_TYPE PR_BASE\n' + partition +
        'set_instance_assignment -name EXPORT_PARTITION_SNAPSHOT_FINAL root_partition.qdb -to |\n'
        'set_global_assignment -name SDC_FILE clocks.sdc\n')
    for suffix in ("a", "b"):
        (output / f"persona_{suffix}.qsf").write_text(common +
            'set_global_assignment -name REVISION_TYPE PR_IMPL\n' +
            'set_instance_assignment -name QDB_FILE_PARTITION root_partition.qdb -to |\n' +
            'set_instance_assignment -name PARTITION slot0 -to slot_inst\n' +
            'set_instance_assignment -name PARTIAL_RECONFIGURATION_PARTITION ON -to slot_inst\n' +
            f'set_instance_assignment -name ENTITY_REBINDING fes_link_persona_{suffix} -to slot_inst\n' +
            'set_global_assignment -name SDC_FILE clocks.sdc\n')
    inputs = {"diagnostic_only": True, "source_revision": frozen["revision"],
              "source_committed": frozen["committed"], "device": DEVICE,
              "source_sha256": {name: sha(data) for name, data in frozen["files"].items()},
              "interface": INTERFACE, "clock_reset": CLOCK_RESET,
              "region": {"id": "slot0", "instance": "slot_inst", "placement": placement, "routing": routing}}
    inputs["project_sha256"] = {path.name: sha(path.read_bytes()) for path in output.iterdir()
                                if path.is_file() and path.suffix != ".qpf"}
    inputs["qpf"] = verify_qpf(output)
    inputs["source_closure_sha256"] = sha(canonical(inputs))
    (output / "inputs.json").write_bytes(canonical(inputs) + b"\n")
    return output


def compile_project(output: Path, tools: dict[str, Path], version: str, only: str | None) -> None:
    inputs = json.loads((output / "inputs.json").read_text())
    verify_project(output, inputs)
    selected_tools = tool_hashes(tools)
    (output / "tools.json").write_bytes(canonical({"version": version, "sha256": selected_tools}) + b"\n")
    (output / "quartus-version.txt").write_text(version + "\n")
    run([str(tools["quartus_ipgenerate"]), "--generate_project_ip_files", "--synthesis=verilog", PROJECT,
         "-c", "base"], output, "ip-generation.log")
    generated = {path.relative_to(output).as_posix(): sha(path.read_bytes())
                 for path in (output / "reset_release").rglob("*")
                 if path.is_file() and path.suffix in (".v", ".sv", ".sdc", ".qip")}
    if not generated:
        raise BuildError("Reset Release generation produced no selected HDL/SDC/QIP closure")
    (output / "generated-inputs.json").write_bytes(canonical(generated) + b"\n")
    root_qdb_hash = None
    for revision in ("base", "persona_a", "persona_b"):
        verify_project(output, inputs)
        verify_files(output, generated)
        if tool_hashes(tools) != selected_tools:
            raise BuildError("selected Quartus executable changed")
        run([str(tools["quartus_sh"]), "--flow", "compile", PROJECT, "-c", revision],
            output, f"{revision}-compile.log")
        verify_project(output, inputs)
        verify_files(output, generated)
        actual_root_hash = sha((output / "root_partition.qdb").read_bytes())
        if root_qdb_hash is None:
            root_qdb_hash = actual_root_hash
        elif root_qdb_hash != actual_root_hash:
            raise BuildError("implementation changed the exported base root QDB")
        if revision == "base" and only == "base":
            return
    # Keep vendor metadata/integrity checks and opaque payloads together. A
    # successful compile alone never emits a portable-composition descriptor.
    (output / "compile-complete.json").write_bytes(canonical({"version": version, "inputs": inputs,
        "status": "compiled-awaiting-artifact-and-timing-verification"}) + b"\n")
    collect(output, tools, version)


def simulate(output: Path) -> None:
    output.mkdir(parents=True, exist_ok=False)
    for name in ("shell.v", "personas.v", "sim_top.v", "tb.cpp", "persona_a.hex", "persona_b.hex"):
        shutil.copyfile(BOARD / "fpga" / name, output / name)
    # The released reset intentionally clears the async-ingress synchronizer
    # and synchronously resets functional state; this is not two reset sources.
    run(["verilator", "--cc", "--exe", "--build", "-j", "2", "-Wall", "-Wno-DECLFILENAME", "-Wno-SYNCASYNCNET",
         "--top-module", "fes_link_sim", "shell.v", "personas.v", "sim_top.v", "tb.cpp"], output, "build.log")
    run([str((output / "obj_dir/Vfes_link_sim").resolve())], output, "simulation.log")
    files = ("shell.v", "personas.v", "sim_top.v", "tb.cpp", "persona_a.hex", "persona_b.hex")
    (output / "simulation.json").write_bytes(canonical({
        "kind": "actual-RTL-with-persona-selection-mux",
        "status": "passed", "physical_pr": "not-modeled", "primitive_memory": "not-modeled",
        "source_sha256": {name: sha((output / name).read_bytes()) for name in files},
        "log_sha256": sha((output / "simulation.log").read_bytes()),
        "verilator_version": subprocess.check_output(["verilator", "--version"], text=True).strip(),
    }) + b"\n")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("project", "probe", "build", "verify", "sim"))
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--quartus-root", type=Path, default=Path("/data/altera_pro/26.3"))
    parser.add_argument("--placement")
    parser.add_argument("--routing")
    parser.add_argument("--only", choices=("base",))
    args = parser.parse_args()
    try:
        if args.action == "sim":
            simulate(args.output.resolve())
        elif args.action == "verify":
            tools, version = toolchain(args.quartus_root)
            collect(args.output.resolve(), tools, version)
        else:
            if not args.placement or not args.routing:
                raise BuildError("explicit tool-verified --placement and --routing regions are required")
            output = prepare(args.output, args.placement, args.routing)
            if args.action in ("probe", "build"):
                if args.action == "build" and not json.loads((output / "inputs.json").read_text())["source_committed"]:
                    raise BuildError("final build requires every captured source byte at the recorded Git commit; use probe for working-copy diagnostics")
                tools, version = toolchain(args.quartus_root)
                compile_project(output, tools, version, args.only)
        print(args.output.resolve())
        return 0
    except (OSError, BuildError) as error:
        parser.exit(1, f"de25-link-proof: {error}\n")


if __name__ == "__main__":
    raise SystemExit(main())
