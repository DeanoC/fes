#!/usr/bin/env python3
"""Build an HPS-controlled exact-device PR diagnostic; never configure an FPGA."""
from __future__ import annotations

import argparse
from decimal import Decimal
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import types
import xml.etree.ElementTree as ET

BOARD = Path(__file__).resolve().parent
DEVICE = "A5EB013BB23BE4SCS"
PROJECT = "fes_link_proof"
PROFILE = None
SOURCE_NAMES = ("build.py", "build_timing.tcl", "clocks.sdc", "fpga/shell.v", "fpga/personas.v",
                "fpga/fes_de25_pr_control_hw.tcl", "fpga/persona_a.hex", "fpga/persona_b.hex",
                "generated/fes_de25_pr_control.vh")
INTERFACE = {"name": "fes.de25.registered-pr-control", "version": 1, "clock_hz": 100000000,
             "request": {"valid": 1, "address": 8}, "response": {"valid": 1, "data": 32},
             "latency_cycles": 4, "id_probe_address": 0, "reset": "active-high-synchronous", "backpressure": False}
CLOCK_RESET = {"clock": "pll_inst|iopll_0_outclk0", "period_ns": "10.000", "source": "preserved-GHRD-PLL",
               "freeze_control": "HPS-lightweight-Avalon-peripheral-0x10700",
               "sequence": ["stop-requests", "drain-responses", "registered-reset-settle",
                            "freeze-ack", "load-persona", "release-reset", "identity-probe", "resume-ack"],
               "cold_reset": "preserved-SDM-and-GHRD-two-register-release",
               "late_peripheral_reset": "terminal-fault-no-new-epoch"}
EMIF_REFERENCE_CLOCK = "soc_inst|subsys_hps|emif_hps|emif_io96b_hps|emif_0_lpddr4|emif_0_lpddr4_0_ref_clock"



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


def verify_hps_inputs(project: Path) -> None:
    hps = load_hps(project.parent.parent / "functional-source/sources/misteross/boards/de25nano/hps/build.py")
    preserved = json.loads((project.parent / "inputs.json").read_bytes())
    for name, expected in preserved["vendor_hps_emif_parameters"].items():
        if hps.module_parameters(project / name) != expected:
            raise BuildError("changed preserved HPS/EMIF module parameters: " + name)
    vendor = json.loads((project.parent / "vendor-inputs.json").read_bytes())
    for name, expected in vendor["source_sha256"].items():
        if name.startswith("software/") and sha((project / name).read_bytes()) != expected:
            raise BuildError("changed preserved vendor boot data: " + name)


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
    # With no redundant REGION_NAME assignment, Quartus labels the physical
    # region by its hierarchy instance in both base and implementation reports.
    expected_name = region["instance"]
    def report_coordinates(value: str) -> str:
        match = re.fullmatch(r"X([0-9]+) Y([0-9]+) X([0-9]+) Y([0-9]+)", value)
        if match is None:
            raise BuildError("invalid captured region coordinates")
        x0, y0, x1, y1 = map(int, match.groups())
        return f"({x0}, {y0}) to ({x1}, {y1})"
    selected = [row for row in rows if row.get("Name") == expected_name]
    if (len(selected) != 1
            or selected[0].get("Place Region Constraint") != report_coordinates(region["placement"])
            or selected[0].get("Route Region Constraint") != report_coordinates(region["routing"])):
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
            "external_io": "preserved-vendor-HPS-and-memory-IO-not-physical-timing-qualification",
            "report_sha256": {f"{revision}.sta.summary": sha(summary.encode()),
                              f"{revision}.sta.rpt": sha(report.encode())}}


def pfg_report(log: Path, *, allow_missing_bootloader: bool = False) -> str:
    text = log.read_text(errors="replace")
    errors = [line.strip() for line in text.splitlines()
              if re.match(r"^\s*Error(?:$|:| \([0-9]+\):| info:| detail:)", line, re.I)]
    missing_bootloader = ["Error", "Error info: The SOF file is incomplete.",
                         "Error detail: HPS is present but bootloader information is missing"]
    if (allow_missing_bootloader and errors != missing_bootloader) or (not allow_missing_bootloader and errors):
        raise BuildError(f"{log.name}: PFG reported an error despite its exit status")
    if re.search(r"was unsuccessful", text, re.I):
        raise BuildError(f"{log.name}: PFG reported unsuccessful processing")
    return text


def vendor_pof(log: Path, *, allow_missing_bootloader: bool = False) -> dict[str, str]:
    text = pfg_report(log, allow_missing_bootloader=allow_missing_bootloader)
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


def vendor_integrity(log: Path) -> None:
    text = pfg_report(log)
    if re.findall(r"^\s*Integrity status:\s*(\S+)\s*$", text, re.M) != ["OK"]:
        raise BuildError(f"{log.name}: missing or failed PFG integrity status")


def blob(output: Path, path: Path) -> dict:
    if not path.is_file() or path.stat().st_size == 0:
        raise BuildError(f"missing nonempty artifact: {path}")
    return {"path": path.relative_to(output).as_posix(), "size": path.stat().st_size,
            "sha256": sha(path.read_bytes())}


def hps_boot_packaging(output: Path) -> dict:
    vendor = json.loads((output.parent / "vendor-inputs.json").read_bytes())["source_sha256"]
    names = ("software/u-boot/spl/u-boot-spl-dtb.hex", "sof_with_hps.bat")
    for name in names:
        path = output / name
        if path.is_symlink() or name not in vendor or sha(path.read_bytes()) != vendor[name]:
            raise BuildError("changed or missing vendor HPS packaging input: " + name)
    lines = [line.strip() for line in (output / names[1]).read_text().splitlines() if line.strip()]
    expected = r"%QUARTUS_ROOTDIR%\bin64\quartus_pfg -c output_files\golden_top.sof output_files\golden_top_hps.sof -o hps_path=software\u-boot\spl\u-boot-spl-dtb.hex"
    if lines != [expected, "pause"]:
        raise BuildError("preserved vendor HPS SOF-enrichment recipe differs")
    return {"strategy": "vendor-sof-enrichment-then-full-rbf", "bootloader": blob(output, output / names[0]),
            "vendor_recipe": blob(output, output / names[1])}


def collect(output: Path, tools: dict[str, Path], version: str) -> Path:
    verifier = capture()
    if not verifier["committed"]:
        raise BuildError("verification requires committed recipe source")
    inputs = json.loads((output / "inputs.json").read_text())
    selected_profile = PROFILE.identity if PROFILE is not None else None
    if inputs.get("profile") != selected_profile:
        raise BuildError("selected verifier/compiler profile differs from captured inputs")
    verify_project(output, inputs)
    generated = json.loads((output / "generated-inputs.json").read_text())
    verify_files(output, generated)
    verify_hps_inputs(output)
    root_qdb = blob(output, output / "root_partition.qdb")
    baseline_path = output / "root-qdb.json"
    baseline = json.loads(baseline_path.read_text())
    if (set(baseline) != {"sha256", "phase"} or baseline["phase"] != "after-base-before-personas"
            or re.fullmatch(r"[0-9a-f]{64}", baseline["sha256"]) is None):
        raise BuildError("invalid pre-persona root QDB baseline record")
    expected_root = baseline["sha256"]
    if expected_root != root_qdb["sha256"]:
        raise BuildError("root QDB differs from the retained pre-persona baseline")
    selected_tools = tool_hashes(tools)
    if json.loads((output / "tools.json").read_text())["sha256"] != selected_tools:
        raise BuildError("Quartus executable closure changed after compilation")
    packaging = hps_boot_packaging(output)
    packaging["compiled_sof"] = blob(output, output / "output_files/base.sof")
    packaging["quartus_pfg_sha256"] = selected_tools["quartus_pfg"]
    verification = output / "verification-recipe"
    verification.mkdir(exist_ok=False)
    for name, data in verifier["files"].items():
        target = verification / name
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(data)
    timing_script = verification / (PROFILE.timing_path if PROFILE is not None else
        "sources/misteross/boards/de25nano/link-proof/build_timing.tcl")
    directory = output / "artifacts"
    directory.mkdir(exist_ok=False)
    summaries = {}
    decoded = {}
    raw_hashes = {}
    # The compiled SOF intentionally lacks boot software. Its vendor decoder
    # reports that incompleteness even with exit 0; use only its POF metadata.
    run([str(tools["quartus_pfg"]), "-i", str(output / "output_files/base.sof")], output, "base-compiled-info.log")
    packaging["compiled_sof_inspection"] = {"status": "known-incomplete-metadata-only",
        "vendor_pof": vendor_pof(output / "base-compiled-info.log", allow_missing_bootloader=True)}
    enriched = directory / "base_hps.sof"
    run([str(tools["quartus_pfg"]), "-c", str(output / "output_files/base.sof"), str(enriched),
         "-o", "hps_path=" + str(output / packaging["bootloader"]["path"])], output, "base-enrich.log")
    packaging["enriched_sof"] = blob(output, enriched)
    run([str(tools["quartus_pfg"]), "-i", str(enriched)], output, "base-enriched-info.log")
    packaging["enriched_sof_pof"] = vendor_pof(output / "base-enriched-info.log")
    run([str(tools["quartus_pfg"]), "-c", str(enriched), str(directory / "base.rbf")],
        output, "base-convert.log")
    run([str(tools["quartus_pfg"]), "-i", str(directory / "base.rbf")], output, "base-info.log")
    base_pof = vendor_pof(output / "base-info.log")
    for previous in (packaging["compiled_sof_inspection"]["vendor_pof"], packaging["enriched_sof_pof"]):
        if any(previous[key] != base_pof[key] for key in ("region_id", "hash")):
            raise BuildError("HPS bootloader enrichment changed the compiled base POF0 identity")
    run([str(tools["quartus_pfg"]), "--check_integrity", str(directory / "base.rbf")], output, "base-integrity.log")
    vendor_integrity(output / "base-integrity.log")
    for revision in ("base", "persona_a", "persona_b"):
        if "Critical Warning (" in (output / f"{revision}-compile.log").read_text():
            raise BuildError(f"{revision}: unresolved critical compiler warning")
        summaries[revision] = timing_summary(output, revision)
        fit = (output / "output_files" / f"{revision}.fit.rpt").read_text()
        all_memories = report_table(fit, "Fitter RAM Summary")
        memories = [row for row in all_memories if row.get("Name", "").startswith("slot_inst|")]
        if PROFILE is not None:
            PROFILE.verify_memories(revision, all_memories)
        elif (len(memories) != 1 or memories[0].get("Type") != "M20K"
                or memories[0].get("Logical Size") != "8192" or memories[0].get("M20K blocks") != "1.000"
                or memories[0].get("MLABs") != "0.000"):
            raise BuildError(f"{revision}: expected exactly one 256x32 M20K inside the PR slot")
        regions = report_table(fit, "Logic Lock Region Constraints")
        verify_regions(regions, inputs["region"], revision)
        summaries[revision]["memory"] = memories
        summaries[revision]["static_memory"] = [row for row in all_memories if row not in memories]
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
                if rule.startswith("BBD-60000 "):
                    raise BuildError(f"{revision}: PR boundary is not directly registered")
                if severity in ("Critical", "High") and not rule.startswith(("TMC-20011 ", "TMC-20012 ")):
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
        run([str(tools["quartus_sta"]), "-t", str(timing_script), str(output), revision,
             str(output / f"timing-{revision}")], output, f"{revision}-timing-audit.log")
        verify_files(output, before_audit)
        summaries[revision]["synchronizer_audit"] = {
            "log_sha256": sha((output / f"{revision}-timing-audit.log").read_bytes()),
            "paths_sha256": sha((output / f"timing-{revision}/boundary-paths.tsv").read_bytes())}
        if revision == "base":
            continue
        log = (output / f"{revision}-compile.log").read_text()
        if 'Loading "final" snapshot for partition "root_partition"' not in log:
            raise BuildError(f"{revision}: no actual final-root import evidence")
        source = output / "output_files" / f"{revision}.slot0.rbf"
        target = directory / ((PROFILE.persona_names[revision[-1]] if PROFILE is not None else f"counter_{revision[-1]}") + ".rbf")
        shutil.copyfile(source, target)
        run([str(tools["quartus_pfg"]), "-i", str(target)], output, f"{revision}-info.log")
        run([str(tools["quartus_pfg"]), "--check_integrity", str(target)], output, f"{revision}-integrity.log")
        vendor_integrity(output / f"{revision}-integrity.log")
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
    verify_project(output, inputs)
    verify_files(output, generated)
    verify_hps_inputs(output)
    verify_files(verification, {name: sha(data) for name, data in verifier["files"].items()})
    for key in ("compiled_sof", "bootloader", "vendor_recipe"):
        verify_files(output, {packaging[key]["path"]: packaging[key]["sha256"]})
    if sha((output / "root_partition.qdb").read_bytes()) != expected_root:
        raise BuildError("root QDB changed during verification")
    interface_hash = sha(canonical(inputs["interface"]))
    reset_hash = sha(canonical(inputs["clock_reset"]))
    descriptor = {"format": 2, "kind": "fes.de25-pr-proof-artifacts", "diagnostic_only": True,
        "device": DEVICE,
        "tool": {"version": "26.3.0 Build114", "edition": "SC Pro Edition",
                 "quartus_sh_sha256": selected_tools["quartus_sh"],
                 "quartus_asm_sha256": selected_tools["quartus_asm"],
                 "quartus_pfg_sha256": selected_tools["quartus_pfg"]},
        "shell": {"source_revision": inputs["source_revision"],
                  "source_closure_sha256": inputs["source_closure_sha256"],
                  "root_qdb_sha256": root_qdb["sha256"], "interface_sha256": interface_hash,
                  "clock_reset_sha256": reset_hash, "base": blob(output, directory / "base.rbf"),
                  "vendor_pof": base_pof, "control_build_id": inputs["control_build_id"]},
        "region": inputs["region"], "personas": [],
        "acceptance": {"physical": "not-run", "timing": "passed",
                       "base_persona_compatibility": "pfg-info-pof0-match"}}
    descriptor["shell"]["id"] = sha(b"fes-de25-pr-shell-v2\0" + canonical(
        {key: descriptor[key] for key in ("device", "tool", "shell", "region")}))
    for suffix in ("a", "b"):
        name = PROFILE.persona_names[suffix] if PROFILE is not None else f"counter_{suffix}"
        persona = {"id": name, "entity": f"fes_link_persona_{suffix}",
                   "revision": f"persona_{suffix}", "hardware_id": 0xc001a001 if suffix == "a" else 0xc002b002, **blob(output, directory / (name + ".rbf")),
                   "root_qdb_sha256": root_qdb["sha256"], "interface_sha256": interface_hash,
                   "clock_reset_sha256": reset_hash, "region_id": "slot0",
                   "vendor_pof": decoded[f"persona_{suffix}"]}
        descriptor["personas"].append(persona)
    manifest = output / "artifacts.json"
    manifest.write_bytes(canonical(descriptor) + b"\n")
    build = {"format": 1, "kind": "fes.de25-quartus-pr-build", "diagnostic_only": True,
             "source_revision": inputs["source_revision"], "source_committed": inputs["source_committed"],
             "source_closure_sha256": inputs["source_closure_sha256"], "device": DEVICE,
             "quartus_version": version, "tool_sha256": selected_tools,
             "control_build_id": inputs["control_build_id"],
             "qpf_initial": inputs.get("qpf"), "qpf_final": verify_qpf(output),
             "compiler_status": "passed", "timing_status": "passed",
             "compatibility_status": "pfg-info-pof0-match", "hardware_acceptance": "not-run",
             "artifact_manifest": blob(output, manifest), "root_qdb": root_qdb,
             "root_qdb_baseline": blob(output, baseline_path),
             "timing": summaries, "compiled_report_sha256": raw_hashes,
             "compatibility_scope": "matching vendor-decoded POF0 region/hash and exact imported root QDB; no SDM execution",
             "pfg_compare_scope": "retained vendor output; unsigned owner-key field absent, not a complete pairing check"}
    build["verification_recipe"] = {"source_revision": verifier["revision"],
        "source_committed": verifier["committed"],
        "source_sha256": {name: sha(data) for name, data in verifier["files"].items()},
        "snapshot": str(verification.relative_to(output)), "timing_script": blob(output, timing_script)}
    packaging["logs_sha256"] = {name: sha((output / name).read_bytes()) for name in
        ("base-compiled-info.log", "base-enrich.log", "base-enriched-info.log", "base-convert.log", "base-info.log", "base-integrity.log")}
    build["packaging"] = packaging
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


def load_hps(path: Path):
    spec = importlib.util.spec_from_file_location("de25_pr_frozen_hps", path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def control_constant(definition: bytes, name: str) -> int:
    matches = re.findall(rb"^`define FES_DE25_PR_CONTROL_" + name.encode() + rb" 32'h([0-9a-f]{8})$", definition, re.M)
    if len(matches) != 1:
        raise BuildError("missing or duplicate generated PR control constant: " + name)
    return int(matches[0], 16)


def remove_duplicate_emif_clock(project: Path) -> dict:
    # The HPS helper has already verified the preserved EMIF reference input
    # and 166.6666 MHz IP setting. Generated EMIF SDC owns this clock; Quartus
    # ignores the later manual declaration on the same port (TMC-20025).
    path = project / "ghrd_timing.sdc"
    source = path.read_text()
    duplicate = 'create_clock -name EMIF_REF_CLOCK -period "166.6666MHz" [get_ports LPDDR4A_REFCLK_p]'
    if source.splitlines().count(duplicate) != 1 or source.count("EMIF_REF_CLOCK") != 1:
        raise BuildError("preserved manual EMIF reference declaration differs")
    path.write_text(source.replace(duplicate,
        "# Generated EMIF SDC owns the 6.000 ns LPDDR4A reference clock."))
    return {"removed_duplicate": duplicate, "owner": EMIF_REFERENCE_CLOCK, "period_ns": "6.000",
            "port": "LPDDR4A_REFCLK_p", "verified_by": ["fes_link/clocks.sdc", "fes_link/build_timing.tcl"]}


def load_profile(path: Path, source: bytes | None = None):
    source = path.read_bytes() if source is None else source
    module = types.ModuleType("de25_pr_selected_profile")
    module.__file__ = str(path)
    exec(compile(source, str(path), "exec"), module.__dict__)
    module.selected_source_bytes = source
    return module


def capture() -> dict:
    global PROFILE
    repository = Path(subprocess.check_output(["git", "rev-parse", "--show-toplevel"], cwd=BOARD, text=True).strip())
    revision = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=BOARD, text=True).strip()
    hps = load_hps(BOARD.parent / "hps/build.py")
    paths = [BOARD / name for name in SOURCE_NAMES]
    paths += [BOARD.parent / "hps" / name for name in hps.SOURCES]
    paths += [BOARD.parent / "build.py"]
    if PROFILE is not None:
        paths += PROFILE.source_paths()
    files = {p.relative_to(repository).as_posix(): p.read_bytes() for p in paths}
    if PROFILE is not None:
        name = (BOARD / "zx81-ram/profile.py").relative_to(repository).as_posix()
        if files[name] != PROFILE.selected_source_bytes:
            raise BuildError("profile source changed after selection")
        # Execute the captured bytes; callbacks and recorded source are identical.
        PROFILE = load_profile(BOARD / "zx81-ram/profile.py", files[name])
    committed = True
    for name, data in files.items():
        result = subprocess.run(["git", "show", f"{revision}:{name}"], cwd=repository,
                                stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, check=False)
        committed = committed and result.returncode == 0 and result.stdout == data
    return {"repository": repository, "revision": revision, "files": files, "committed": committed}


def write_revisions(project: Path, region: dict, build_id: str) -> None:
    common = (project / "golden_top.qsf").read_text()
    # Emit the selected tool defaults once; Quartus must not mutate functional
    # assignments while compiling one of the explicit revisions.
    overrides = {"NUM_PARALLEL_PROCESSORS", "SEED", "IP_SEARCH_PATHS", "LAST_QUARTUS_VERSION",
                 "PWRMGT_VOLTAGE_OUTPUT_FORMAT", "PWRMGT_LINEAR_FORMAT_N",
                 "RESERVE_ALL_UNUSED_PINS_NO_OUTPUT_GND", "ENABLE_PR_POF_ID"}
    common = "\n".join(line for line in common.splitlines() if not any(
        re.match(r"^set_global_assignment -name " + name + r"(?:\s|$)", line) for name in overrides)) + "\n"
    common += ('set_global_assignment -name NUM_PARALLEL_PROCESSORS 2\n'
        'set_global_assignment -name SEED 1\n'
        'set_global_assignment -name LAST_QUARTUS_VERSION "26.3.0 Pro Edition"\n'
        'set_global_assignment -name PWRMGT_VOLTAGE_OUTPUT_FORMAT "LINEAR FORMAT"\n'
        'set_global_assignment -name PWRMGT_LINEAR_FORMAT_N "-12"\n'
        'set_global_assignment -name RESERVE_ALL_UNUSED_PINS_NO_OUTPUT_GND "AS INPUT TRI-STATED"\n'
        'set_global_assignment -name ENABLE_PR_POF_ID ON\n'
        'set_global_assignment -name IP_SEARCH_PATHS "custom_ip/**/*;fes_mailbox;fes_link"\n'
        'set_global_assignment -name VERILOG_FILE fes_link/personas.v\n'
        'set_global_assignment -name SDC_FILE fes_link/clocks.sdc\n'
        'set_instance_assignment -name NOT_GATE_PUSH_BACK OFF -to "soc_inst|subsys_periph|fes_pr_control|fes_pr_control|slot_reset"\n'
        f'set_global_assignment -name VERILOG_MACRO "FES_DE25_PR_BUILD_ID=128\'h{build_id}"\n')
    if PROFILE is not None:
        common += PROFILE.qsf()
    lines = common.splitlines()
    constraints = "\n".join(line for line in lines if line.startswith("set_global_assignment -name SDC_FILE ")) + "\n"
    common = "\n".join(line for line in lines if not line.startswith("set_global_assignment -name SDC_FILE ")) + "\n"
    partition = ('set_instance_assignment -name PARTITION slot0 -to slot_inst\n'
        'set_instance_assignment -name PARTIAL_RECONFIGURATION_PARTITION ON -to slot_inst\n')
    reserved = (f'set_instance_assignment -name PLACE_REGION "{region["placement"]}" -to slot_inst\n'
        f'set_instance_assignment -name ROUTE_REGION "{region["routing"]}" -to slot_inst\n'
        'set_instance_assignment -name RESERVE_PLACE_REGION ON -to slot_inst\n'
        'set_instance_assignment -name RESERVE_ROUTE_REGION ON -to slot_inst\n'
        'set_instance_assignment -name CORE_ONLY_PLACE_REGION ON -to slot_inst\n')
    (project / f"{PROJECT}.qpf").write_text('QUARTUS_VERSION = "26.3"\nPROJECT_REVISION = "base"\n'
                                         'PROJECT_REVISION = "persona_a"\nPROJECT_REVISION = "persona_b"\n')
    (project / "base.qsf").write_text(common + 'set_global_assignment -name REVISION_TYPE PR_BASE\n' +
        partition + reserved + 'set_instance_assignment -name EXPORT_PARTITION_SNAPSHOT_FINAL root_partition.qdb -to |\n' + constraints)
    for suffix in ("a", "b"):
        (project / f"persona_{suffix}.qsf").write_text(common +
            'set_global_assignment -name REVISION_TYPE PR_IMPL\n' +
            'set_instance_assignment -name QDB_FILE_PARTITION root_partition.qdb -to |\n' + partition +
            f'set_instance_assignment -name ENTITY_REBINDING fes_link_persona_{suffix} -to slot_inst\n' + constraints)


def prepare(output: Path, placement: str, routing: str, archive: Path | None = None) -> Path:
    for region in (placement, routing):
        if re.fullmatch(r"X[0-9]+ Y[0-9]+ X[0-9]+ Y[0-9]+", region) is None:
            raise BuildError("region must contain exactly four canonical device coordinates")
    if archive is None or not archive.is_file() or archive.is_symlink():
        raise BuildError("an explicit regular-file --vendor-archive is required")
    frozen = capture()
    definition = frozen["files"]["sources/misteross/boards/de25nano/link-proof/generated/fes_de25_pr_control.vh"]
    control_offset = control_constant(definition, "PERIPHERAL_OFFSET")
    if control_constant(definition, "CLOCK_HZ") != 100000000 or control_constant(definition, "SPAN") != 64:
        raise BuildError("PR control clock/span contract changed")
    output = output.resolve()
    output.mkdir(parents=True, exist_ok=False)
    snapshot = output / "functional-source"
    for name, data in frozen["files"].items():
        path = snapshot / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(data)
    hps = load_hps(snapshot / "sources/misteross/boards/de25nano/hps/build.py")
    retained = output / "vendor.zip"
    retained.write_bytes(archive.read_bytes())
    hps.source_identity = lambda: {"fes_revision": frozen["revision"],
        "source_classification": "committed" if frozen["committed"] else "working-copy-diagnostic"}
    hps_output = hps.prepare_project(retained, output / "hps")
    project = hps_output / "project"
    emif_reference_clock = remove_duplicate_emif_clock(project)
    local = project / "fes_link"
    local.mkdir()
    for name in SOURCE_NAMES:
        if name == "build.py":
            target = local / "build.py"
        elif name.startswith("fpga/"):
            target = local / name.removeprefix("fpga/")
        else:
            target = local / name
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(frozen["files"]["sources/misteross/boards/de25nano/link-proof/" + name])
    if PROFILE is not None:
        PROFILE.prepare(local, project, frozen)
    for suffix in ("a", "b"):
        shutil.copyfile(local / f"persona_{suffix}.hex", project / f"persona_{suffix}.hex")
    top = (project / "golden_top.v").read_text()
    anchor = "qsys_top soc_inst ("
    if top.count(anchor) != 1 or top.count("endmodule") != 1:
        raise BuildError("preserved GHRD top boundary differs")
    declarations = """wire pr_req, pr_reset, pr_rsp_valid, pr_frozen, pr_running;
wire [7:0] pr_address;
wire [31:0] pr_rsp_data, pr_isolated_data, pr_static_count, pr_retired, pr_last_data;
qsys_top soc_inst (
.fes_pr_slot_req(pr_req), .fes_pr_slot_address(pr_address), .fes_pr_slot_reset(pr_reset),
.fes_pr_slot_rsp_valid(pr_rsp_valid), .fes_pr_slot_rsp_data(pr_rsp_data),
.fes_pr_diagnostic_frozen(pr_frozen), .fes_pr_diagnostic_running(pr_running),
.fes_pr_diagnostic_isolated_data(pr_isolated_data), .fes_pr_diagnostic_static_count(pr_static_count),
.fes_pr_diagnostic_retired(pr_retired), .fes_pr_diagnostic_last_data(pr_last_data),
"""
    top = top.replace(anchor, declarations)
    top = top.replace("endmodule", """fes_link_persona_a slot_inst (
.clk(system_clk_100_internal), .reset(pr_reset), .req(pr_req), .address(pr_address),
.rsp_valid(pr_rsp_valid), .rsp_data(pr_rsp_data));
endmodule
""")
    if PROFILE is not None:
        top = PROFILE.top(top)
    (project / "golden_top.v").write_text(top)
    script = (project / "integrate-mailbox.tcl").read_text()
    insert = """add_instance fes_pr_control fes_de25_pr_control 1.0
add_connection periph_clk.out_clk fes_pr_control.clk
add_connection periph_rst_in.out_reset fes_pr_control.reset
add_connection pb_cpu_0.m0 fes_pr_control.avs
set_connection_parameter_value pb_cpu_0.m0/fes_pr_control.avs baseAddress @PR_BASE@
add_interface fes_pr_slot conduit end
set_interface_property fes_pr_slot EXPORT_OF fes_pr_control.slot
add_interface fes_pr_diagnostic conduit end
set_interface_property fes_pr_diagnostic EXPORT_OF fes_pr_control.diagnostic
"""
    anchor = "save_system $peripheral_system\n\n# Refresh"
    if script.count(anchor) != 1:
        raise BuildError("preserved GHRD integration boundary differs")
    script = script.replace(anchor, insert.replace("@PR_BASE@", hex(control_offset)) + "\n" + anchor)
    anchor = "save_system " + hps.tcl_word(project / "qsys_top.qsys") + "\n"
    if script.count(anchor) != 1:
        raise BuildError("preserved GHRD top save boundary differs")
    exports = """add_interface fes_pr_slot conduit end
set_interface_property fes_pr_slot EXPORT_OF subsys_periph.fes_pr_slot
add_interface fes_pr_diagnostic conduit end
set_interface_property fes_pr_diagnostic EXPORT_OF subsys_periph.fes_pr_diagnostic
"""
    script = script.replace(anchor, exports + anchor)
    (project / "integrate-mailbox.tcl").write_text(script)
    # The generator's project gets the same component search paths as PR revisions.
    with (project / "golden_top.qsf").open("a") as qsf:
        qsf.write('\nset_global_assignment -name IP_SEARCH_PATHS "custom_ip/**/*;fes_mailbox;fes_link"\n')
    region = {"id": "slot0", "instance": "slot_inst", "placement": placement, "routing": routing}
    inputs = {"diagnostic_only": True, "source_revision": frozen["revision"],
              "source_committed": frozen["committed"], "device": DEVICE,
              "source_sha256": {name: sha(data) for name, data in frozen["files"].items()},
              "source_snapshot": str(snapshot.relative_to(project.parent.parent)),
              "vendor_archive_sha256": sha(retained.read_bytes()),
              "emif_reference_clock": emif_reference_clock,
              "control_peripheral_offset": control_offset,
              "interface": INTERFACE, "clock_reset": CLOCK_RESET, "region": region}
    if PROFILE is not None:
        inputs["profile"] = PROFILE.identity
        inputs["interface"] = PROFILE.interface
    inputs["control_build_id"] = sha(canonical(inputs))[:32]
    write_revisions(project, region, inputs["control_build_id"])
    inputs["project_sha256"] = {path.relative_to(project).as_posix(): sha(path.read_bytes())
        for path in local.rglob("*") if path.is_file()}
    for name in ("base.qsf", "persona_a.qsf", "persona_b.qsf", "golden_top.v", "ghrd_timing.sdc", "persona_a.hex", "persona_b.hex"):
        inputs["project_sha256"][name] = sha((project / name).read_bytes())
    if PROFILE is not None:
        for name in ("zx8x.hex", "zx8x.mif"):
            inputs["project_sha256"][name] = sha((project / name).read_bytes())
    inputs["qpf"] = verify_qpf(project)
    inputs["source_closure_sha256"] = sha(canonical(inputs))
    (project / "inputs.json").write_bytes(canonical(inputs) + b"\n")
    return project


def generate_hps(project: Path, tools: dict[str, Path], version: str) -> None:
    snapshot = project.parent.parent / "functional-source"
    hps = load_hps(snapshot / "sources/misteross/boards/de25nano/hps/build.py")
    install = tools["quartus_sh"].parents[2]
    qsys = install / "qsys/bin/qsys-script"
    run([str(qsys), "--quartus-project=golden_top",
         "--search-path=custom_ip/**/*,fes_mailbox,fes_link,$", "--script=integrate-mailbox.tcl"],
        project, "hps-integration.log")
    run([str(tools["quartus_ipgenerate"]), "--clear_ip_generation_dirs", "golden_top"], project, "hps-clear.log")
    run([str(tools["quartus_ipgenerate"]), "--generate_project_ip_files", "--synthesis=verilog", "golden_top"],
        project, "hps-generation.log")
    preserved = json.loads((project.parent / "inputs.json").read_bytes())
    for name, expected in preserved["vendor_hps_emif_parameters"].items():
        if hps.module_parameters(project / name) != expected:
            raise BuildError("generation changed preserved HPS/EMIF module parameters: " + name)
    vendor = json.loads((project.parent / "vendor-inputs.json").read_bytes())
    for name, expected in vendor["source_sha256"].items():
        if name.startswith("software/") and sha((project / name).read_bytes()) != expected:
            raise BuildError("generation changed preserved vendor boot data: " + name)
    handoff = hps.verify_mailbox_handoff(project)
    handoff["ddr_probe"] = hps.verify_probe_handoff(project)
    inputs = json.loads((project / "inputs.json").read_bytes())
    tree = ET.parse(project / "qsys_top/qsys_top.sopcinfo")
    modules = [item for item in tree.findall("module") if item.get("path") == "subsys_periph.fes_pr_control"]
    if len(modules) != 1 or modules[0].get("kind") != "fes_de25_pr_control" or modules[0].get("version") != "1.0":
        raise BuildError("actual generated system lacks PR control endpoint")
    connections = {(item.get("kind"), item.get("start"), item.get("end")): item for item in tree.findall("connection")}
    control = connections.get(("avalon", "subsys_periph_pb_cpu_0.m0", "subsys_periph_fes_pr_control.avs"))
    clock = connections.get(("clock", "subsys_periph_periph_clk.out_clk", "subsys_periph_fes_pr_control.clk"))
    reset = connections.get(("reset", "subsys_periph_periph_rst_in.out_reset", "subsys_periph_fes_pr_control.reset"))
    if control is None or clock is None or reset is None or int(control.findtext("parameter[@name='baseAddress']/value"), 0) != inputs["control_peripheral_offset"] or clock.findtext("parameter[@name='clockRateSysInfo']/value") != "100000000":
        raise BuildError("actual PR control offset/clock differs")
    expected_ports = {"address": ("Input", 6), "read": ("Input", 1), "write": ("Input", 1),
                      "byteenable": ("Input", 4), "writedata": ("Input", 32), "readdata": ("Output", 32),
                      "readdatavalid": ("Output", 1), "waitrequest": ("Output", 1),
                      "writeresponsevalid": ("Output", 1), "response": ("Output", 2)}
    if hps.component_ports(modules[0], "avs") != expected_ports:
        raise BuildError("generated PR control byte/address/response interface differs")
    regions = [r for r in handoff["ddr_probe"]["peripheral_slave_map"] if r["name"] == "subsys_periph.fes_pr_control.avs"]
    if len(regions) != 1 or regions[0]["base"] != inputs["control_peripheral_offset"] or regions[0]["span"] != 64:
        raise BuildError("actual HPS bridge PR control aperture differs")
    handoff["pr_control"] = {"module": modules[0].get("path"), "peripheral_offset": inputs["control_peripheral_offset"],
                             "clock_hz": 100000000, "physical_mmio": "not-established"}
    (project / "hps-handoff.json").write_bytes(canonical(handoff) + b"\n")
    write_revisions(project, inputs["region"], inputs["control_build_id"])
    for name in ("base.qsf", "persona_a.qsf", "persona_b.qsf"):
        inputs["project_sha256"][name] = sha((project / name).read_bytes())
    inputs["qpf"] = verify_qpf(project)
    # Generation parameters are verified by the HPS helper; record the selected
    # generated HDL/SDC and system topology before starting any compilation.
    generated = {path.relative_to(project).as_posix(): sha(path.read_bytes())
        for path in project.rglob("*") if path.is_file() and path.suffix in (".v", ".sv", ".sdc", ".qip", ".qsys", ".ip", ".mif", ".hex")
        and "output_files" not in path.parts and not path.is_relative_to(project / "fes_link")}
    if not generated:
        raise BuildError("HPS generation produced no selected source closure")
    (project / "generated-inputs.json").write_bytes(canonical(generated) + b"\n")
    (project / "inputs.json").write_bytes(canonical(inputs) + b"\n")


def compile_project(output: Path, tools: dict[str, Path], version: str, only: str | None) -> None:
    inputs = json.loads((output / "inputs.json").read_text())
    selected_profile = PROFILE.identity if PROFILE is not None else None
    if inputs.get("profile") != selected_profile:
        raise BuildError("selected verifier/compiler profile differs from captured inputs")
    verify_project(output, inputs)
    selected_tools = tool_hashes(tools)
    (output / "tools.json").write_bytes(canonical({"version": version, "sha256": selected_tools}) + b"\n")
    (output / "quartus-version.txt").write_text(version + "\n")
    generate_hps(output, tools, version)
    inputs = json.loads((output / "inputs.json").read_bytes())
    generated = json.loads((output / "generated-inputs.json").read_bytes())
    root_qdb_hash = None
    for revision in ("base", "persona_a", "persona_b"):
        verify_project(output, inputs)
        verify_files(output, generated)
        if tool_hashes(tools) != selected_tools:
            raise BuildError("selected Quartus executable changed")
        if PROFILE is not None:
            run([str(tools["quartus_syn"]), "--read_settings_files=on", "--write_settings_files=off",
                 PROJECT, "-c", revision], output, f"{revision}-synthesis-preflight.log")
            preflight = PROFILE.verify_synthesis(output / "output_files" / f"{revision}.syn.rpt")
            (output / f"{revision}-synthesis-preflight.json").write_bytes(canonical(preflight) + b"\n")
            verify_project(output, inputs)
            verify_files(output, generated)
        run([str(tools["quartus_sh"]), "--flow", "compile", PROJECT, "-c", revision],
            output, f"{revision}-compile.log")
        verify_project(output, inputs)
        verify_files(output, generated)
        actual_root_hash = sha((output / "root_partition.qdb").read_bytes())
        if root_qdb_hash is None:
            root_qdb_hash = actual_root_hash
            (output / "root-qdb.json").write_bytes(canonical({"sha256": root_qdb_hash,
                "phase": "after-base-before-personas"}) + b"\n")
        elif root_qdb_hash != actual_root_hash:
            raise BuildError("implementation changed the exported base root QDB")
        if revision == "base" and PROFILE is not None:
            script = output.parent.parent / "functional-source" / PROFILE.timing_path
            report_hashes = {path.relative_to(output).as_posix(): sha(path.read_bytes())
                for path in (output / "output_files").glob("base.*") if path.is_file()}
            report_hashes["root_partition.qdb"] = root_qdb_hash
            run([str(tools["quartus_sta"]), "-t", str(script), str(output), revision,
                 str(output / "timing-base-preflight")], output, "base-boundary-preflight.log")
            verify_files(output, report_hashes)
            verify_project(output, inputs)
            (output / "base-boundary-preflight.json").write_bytes(canonical({
                "status": "passed", "timing_script_sha256": sha(script.read_bytes()),
                "paths_sha256": sha((output / "timing-base-preflight/boundary-paths.tsv").read_bytes()),
                "quartus_sta_sha256": selected_tools["quartus_sta"], "root_qdb_sha256": root_qdb_hash}) + b"\n")
        if revision == "base" and only == "base":
            return
    # Keep vendor metadata/integrity checks and opaque payloads together. A
    # successful compile alone never emits a portable-composition descriptor.
    (output / "compile-complete.json").write_bytes(canonical({"version": version, "inputs": inputs,
        "status": "compiled-awaiting-artifact-and-timing-verification"}) + b"\n")
    collect(output, tools, version)


def simulate(output: Path) -> None:
    output.mkdir(parents=True, exist_ok=False)
    (output / "generated").mkdir()
    shutil.copyfile(BOARD / "generated/fes_de25_pr_control.vh", output / "generated/fes_de25_pr_control.vh")
    for name in ("shell.v", "personas.v", "sim_top.v", "tb.cpp", "persona_a.hex", "persona_b.hex"):
        shutil.copyfile(BOARD / "fpga" / name, output / name)
    # The released reset intentionally clears the async-ingress synchronizer
    # and synchronously resets functional state; this is not two reset sources.
    run(["verilator", "--cc", "--exe", "--build", "-j", "2", "--x-initial", "unique", "-Wall", "-Wno-DECLFILENAME", "-Wno-SYNCASYNCNET",
         "--top-module", "fes_link_sim", "shell.v", "personas.v", "sim_top.v", "tb.cpp"], output, "build.log")
    binary = str((output / "obj_dir/Vfes_link_sim").resolve())
    for seed in (17, 29, 41):
        run([binary, "+verilator+rand+reset+2", f"+verilator+seed+{seed}"], output,
            "simulation.log" if seed == 17 else f"simulation-seed-{seed}.log")
    files = ("shell.v", "personas.v", "sim_top.v", "tb.cpp", "persona_a.hex", "persona_b.hex",
             "generated/fes_de25_pr_control.vh")
    (output / "simulation.json").write_bytes(canonical({
        "kind": "actual-RTL-with-persona-selection-mux",
        "status": "passed", "physical_pr": "not-modeled", "primitive_memory": "not-modeled",
        "source_sha256": {name: sha((output / name).read_bytes()) for name in files},
        "log_sha256": sha((output / "simulation.log").read_bytes()),
        "unspecified_register_initialization": {"mode": "random", "seeds": [17, 29, 41]},
        "verilator_version": subprocess.check_output(["verilator", "--version"], text=True).strip(),
    }) + b"\n")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("project", "probe", "build", "verify", "sim"))
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--quartus-root", type=Path, default=Path("/data/altera_pro/26.3"))
    parser.add_argument("--vendor-archive", type=Path)
    parser.add_argument("--placement")
    parser.add_argument("--routing")
    parser.add_argument("--only", choices=("base",))
    parser.add_argument("--profile", choices=("zx81-ram",))
    args = parser.parse_args()
    if args.profile:
        global PROFILE
        PROFILE = load_profile(BOARD / args.profile / "profile.py")
    try:
        if args.action == "sim":
            if PROFILE is not None:
                raise BuildError("run zx81-ram/sim.py for the actual ZX81 profile simulation")
            simulate(args.output.resolve())
        elif args.action == "verify":
            tools, version = toolchain(args.quartus_root)
            collect(args.output.resolve(), tools, version)
        else:
            if not args.placement or not args.routing:
                raise BuildError("explicit tool-verified --placement and --routing regions are required")
            output = prepare(args.output, args.placement, args.routing, args.vendor_archive)
            if args.action in ("probe", "build"):
                if args.action == "build" and not json.loads((output / "inputs.json").read_text())["source_committed"]:
                    raise BuildError("final build requires every captured source byte at the recorded Git commit; use probe for working-copy diagnostics")
                tools, version = toolchain(args.quartus_root)
                compile_project(output, tools, version, args.only)
        print(args.output.resolve() if args.action in ("sim", "verify") else output)
        return 0
    except (OSError, BuildError) as error:
        parser.exit(1, f"de25-link-proof: {error}\n")


if __name__ == "__main__":
    raise SystemExit(main())
