#!/usr/bin/env python3
"""Build standalone DE25 idle/menu-pattern SOFs; never program hardware."""
from __future__ import annotations
import argparse
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import tomllib
import xml.etree.ElementTree as ET
import zipfile

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[4]
MISTEROSS = ROOT / "sources/misteross"
PROJECT = "de25nano_display"
DEVICE = "A5EB013BB23BE4SCS"
SPEC = importlib.util.spec_from_file_location("de25_display_tools", HERE.parent / "build.py")
TOOLS = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(TOOLS)
LOCAL = ("build.py", "top.v", "clocks.sdc", "vendor.lock.toml")
SOURCE_PATHS = {name: HERE / name for name in LOCAL}
SOURCE_PATHS.update({"board_build.py": HERE.parent / "build.py",
    "fes_de25_top_reset.v": HERE.parent / "hps/fes_de25_top_reset.v",
    "fes_splash_core.v": MISTEROSS / "cores/fes-splash/rtl/fes_splash_core.v",
    **{name + ".v": MISTEROSS / ("cores/fes-menu/rtl/" + name + ".v")
       for name in ("fes_menu_reader", "fes_menu_video", "fes_menu_pattern_memory")}})
VENDOR_FILES = ("golden_top.qsf", "sys_pll.ip", "agilex_reset_release.ip",
                "HDMI_I2C/I2C_HDMI_Config.v", "HDMI_I2C/I2C_Controller.v")
SCS_GOLDEN = "Demonstration/FPGA/Golden_top/golden_top.qsf"
PORTS = {"CLOCK0_50", "HDMI_TX_CLK", "HDMI_TX_DE", "HDMI_TX_HS", "HDMI_TX_VS",
         "HDMI_I2C_SCL", "HDMI_I2C_SDA", "HDMI_TX_INT", "HDMI_MCLK", "HDMI_SCLK",
         "HDMI_LRCLK", "HDMI_I2S", *[f"HDMI_TX_D[{n}]" for n in range(24)]}
NS = {"i": "http://www.accellera.org/XMLSchema/IPXACT/1685-2014",
      "a": "http://www.altera.com/XMLSchema/IPXact2014/extensions"}


def sha(data):
    return hashlib.sha256(data).hexdigest()


def source_identity():
    revision = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
    status = subprocess.check_output(["git", "status", "--porcelain", "--untracked-files=normal"], cwd=ROOT, text=True)
    return {"base_revision" if status else "fes_revision": revision,
            "source_classification": "working-copy-diagnostic" if status else "committed"}


def capture_sources(revision=None):
    return {name: path.read_bytes() if revision is None else subprocess.check_output(
        ["git", "show", revision + ":" + path.relative_to(ROOT).as_posix()], cwd=ROOT)
        for name, path in SOURCE_PATHS.items()}


def pin_assignments(source):
    lines, counts = [], {}
    for line in source.splitlines():
        match = re.fullmatch(r'(set_location_assignment\s+PIN_\w+|set_instance_assignment\s+-name IO_STANDARD\s+"[^"]+")\s+-to\s+(\S+)\s*', line)
        if match and match[2] in PORTS:
            lines.append(line)
            key = (match[2], match[1].split()[0])
            counts[key] = counts.get(key, 0) + 1
    expected = {(port, kind): 1 for port in PORTS for kind in ("set_location_assignment", "set_instance_assignment")}
    if counts != expected:
        raise ValueError("vendor display pin/IO assignment set changed")
    return "\n".join(lines) + "\n"


def pll_parameters(source):
    tree = ET.fromstring(source)
    return {item.get("parameterId"): item.findtext("i:value", namespaces=NS)
            for item in tree.findall(".//a:altera_module_parameters/i:parameters/i:parameter", NS)}


def retarget_pll(source):
    parameters = pll_parameters(source)
    expected = {"system_info_device_component": "A5EB013BB23BE4SR1",
                "gui_reference_clock_frequency": "50.0", "gui_output_clock_frequency0": "74.25",
                "gui_number_of_clocks": "1", "gui_use_locked": "true",
                "gui_phase_shift0": "0.0", "gui_duty_cycle0": "50.0"}
    if any(parameters.get(key) != value for key, value in expected.items()):
        raise ValueError("vendor 720p PLL contract changed")
    before = b"<ipxact:value>A5EB013BB23BE4SR1</ipxact:value>"
    # The wrapper carries the target in both module and system metadata.
    if source.count(before) != 2:
        raise ValueError("vendor PLL device metadata patch target changed")
    result = source.replace(before, ("<ipxact:value>" + DEVICE + "</ipxact:value>").encode())
    return result, {"parameters": ["system_info_device_component", "system/device"],
                    "before": expected["system_info_device_component"], "after": DEVICE}


def retarget_reset(source):
    before = b"<ipxact:value>A5EB013BB23BE4SR1</ipxact:value>"
    if source.count(before) != 1 or pll_parameters(source).get("DEVICE_FAMILY") != "Agilex 5":
        raise ValueError("vendor reset-release device/family contract changed")
    return source.replace(before, ("<ipxact:value>" + DEVICE + "</ipxact:value>").encode()), {
        "parameter": "system/device", "before": "A5EB013BB23BE4SR1", "after": DEVICE}


def prepare(archive, mode, output=None, revision=None):
    sources = capture_sources(revision)
    lock = tomllib.loads(sources["vendor.lock.toml"].decode())
    if lock["device"] != DEVICE or lock["format"] != 1:
        raise ValueError("invalid DE25 display lock")
    archive = Path(archive)
    if not archive.is_file() or archive.is_symlink():
        raise ValueError("vendor ZIP must be a regular file matching vendor.lock.toml")
    archive_bytes = archive.read_bytes()
    if sha(archive_bytes) != lock["archive"]["sha256"]:
        raise ValueError("vendor ZIP must be a regular file matching vendor.lock.toml")
    with zipfile.ZipFile(io.BytesIO(archive_bytes)) as package:
        prefix = lock["archive"]["prefix"]
        wanted = {prefix + name for name in VENDOR_FILES}
        wanted.add(SCS_GOLDEN)
        if any(package.namelist().count(name) != 1 for name in wanted):
            raise ValueError("missing/duplicate vendor display source")
        vendor = {name: package.read(prefix + name) for name in VENDOR_FILES}
        baseline = package.read(SCS_GOLDEN).decode()
    pins = pin_assignments(vendor["golden_top.qsf"].decode())
    if pin_assignments(baseline) != pins or not re.search(r"-name DEVICE " + DEVICE + r"\s", baseline):
        raise ValueError("HDMI reference pins differ from the SCS golden board")
    pll, repair = retarget_pll(vendor["sys_pll.ip"])
    reset, reset_repair = retarget_reset(vendor["agilex_reset_release.ip"])
    if output is None:
        directory = MISTEROSS / "build/diagnostic/de25nano-display"
        directory.mkdir(parents=True, exist_ok=True)
        output = Path(tempfile.mkdtemp(prefix=mode + "-", dir=directory))
    else:
        output = Path(output).resolve()
        output.mkdir(parents=True, exist_ok=False)
    project = output / "project"
    project.mkdir()
    for name, data in vendor.items():
        path = output / "vendor" / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(data)
    (output / "vendor/scs-golden.qsf").write_text(baseline)
    for name, data in sources.items():
        (project / name).write_bytes(data)
    for name in VENDOR_FILES[1:]:
        (project / Path(name).name).write_bytes(pll if name == "sys_pll.ip" else
            reset if name == "agilex_reset_release.ip" else vendor[name])
    (project / "pins.qsf").write_text(pins)
    (project / (PROJECT + ".qpf")).write_text(f'QUARTUS_VERSION = "26.3.0"\nPROJECT_REVISION = "{PROJECT}"\n')
    assignments = '\n'.join('set_global_assignment -name ' + name + ' ' + value for name, value in (
        ("FAMILY", '"Agilex 5"'), ("DEVICE", DEVICE), ("TOP_LEVEL_ENTITY", PROJECT),
        ("PROJECT_OUTPUT_DIRECTORY", "output_files"), ("NUM_PARALLEL_PROCESSORS", "2"), ("SEED", "1"),
        ("STRATIXV_CONFIGURATION_SCHEME", '"ACTIVE SERIAL X4"'), ("ACTIVE_SERIAL_CLOCK", "AS_FREQ_125MHZ"),
        ("DEVICE_INITIALIZATION_CLOCK", "OSC_CLK_1_125MHZ"), ("RESERVE_ALL_UNUSED_PINS", '"AS INPUT TRI-STATED"'),
        ("IP_FILE", "sys_pll.ip"), ("IP_FILE", "agilex_reset_release.ip"), ("SDC_FILE", "clocks.sdc")))
    rtl = ["top.v", "fes_de25_top_reset.v", "I2C_HDMI_Config.v", "I2C_Controller.v"]
    rtl += ["fes_splash_core.v"] if mode == "idle" else ["fes_menu_reader.v", "fes_menu_video.v", "fes_menu_pattern_memory.v"]
    (project / (PROJECT + ".qsf")).write_text(assignments + '\n' +
        '\n'.join('set_global_assignment -name VERILOG_FILE ' + name for name in rtl) +
        f'\nset_parameter -name MENU_PATTERN {int(mode == "menu-pattern")}\nsource pins.qsf\n')
    identity = {"fes_revision": revision, "source_classification": "committed"} if revision else source_identity()
    if not revision:
        identity = {"base_revision": identity.get("fes_revision", identity.get("base_revision")), "source_classification": "working-copy-diagnostic"}
    inputs = {**identity, "board": "de25_nano", "device": DEVICE, "mode": mode, "diagnostic_only": True,
        "source_sha256": {name: sha(data) for name, data in sources.items()},
        "vendor_archive": lock["archive"], "vendor_source_sha256": {name: sha(data) for name, data in vendor.items()},
        "reviewed_pll_device_repair": repair, "reviewed_reset_release_device_repair": reset_repair,
        "video": {"pixel_clock_hz": 74250000, "h_total": 1650, "v_total": 750},
        "hps": False, "ddr": False, "gp": False, "runtime_menu": False,
        "boot_firmware_compatibility": "not-qualified", "hardware_acceptance": "not-run"}
    inputs["prepared_project_sha256"] = {str(path.relative_to(project)): sha(path.read_bytes()) for path in project.iterdir() if path.is_file()}
    (output / "inputs.json").write_text(json.dumps(inputs, indent=2, sort_keys=True) + "\n")
    return output


def run(command, project, log):
    print("+ " + " ".join(map(str, command)), flush=True)
    with log.open("w") as stream:
        result = subprocess.run(list(map(str, command)), cwd=project, stdout=stream, stderr=subprocess.STDOUT)
    if result.returncode:
        raise ValueError(f"command failed ({result.returncode}); retained {log}")


def compile_project(output, tool, version):
    project = output / "project"
    (output / "quartus-version.txt").write_text(version + "\n")
    generate = [tool.with_name("quartus_ipgenerate"), "--generate_project_ip_files", "--synthesis=verilog", PROJECT]
    expected = {name: pll_parameters((project / name).read_bytes()) for name in ("sys_pll.ip", "agilex_reset_release.ip")}
    run(generate, project, output / "ip-generation.log")
    # Only the intended device selection may change the vendor PLL parameters.
    changes = {}
    for name, parameters in expected.items():
        actual = pll_parameters((project / name).read_bytes())
        delta = {key: {"before": parameters.get(key), "after": actual.get(key)}
                 for key in parameters.keys() | actual.keys() if actual.get(key) != parameters.get(key)}
        if delta:
            changes[name] = delta
    (output / "ip-parameter-review.json").write_text(json.dumps(changes, indent=2, sort_keys=True) + "\n")
    if changes:
        raise ValueError("IP generation changed original PLL/reset parameters; retained review")
    command = [tool, "--flow", "compile", PROJECT]
    run(command, project, output / "compile.log")
    if any(pll_parameters((project / name).read_bytes()) != parameters for name, parameters in expected.items()):
        raise ValueError("compile changed PLL/reset parameters")
    artifact = project / "output_files" / (PROJECT + ".sof")
    if not artifact.is_file() or not artifact.stat().st_size:
        raise ValueError("Quartus produced no nonempty display SOF")
    receipt = {"fes_revision": json.loads((output / "inputs.json").read_text())["fes_revision"],
        "artifact": str(artifact.relative_to(output)), "sha256": sha(artifact.read_bytes()),
        "inputs_sha256": sha((output / "inputs.json").read_bytes()), "quartus_version": version,
        "command": list(map(str, command)), "ip_generation_command": list(map(str, generate)),
        "reports_sha256": {str(path.relative_to(project)): sha(path.read_bytes()) for path in (project / "output_files").iterdir()
                            if path.suffix in (".rpt", ".summary", ".pin")},
        "generated_sdc_sha256": {str(path.relative_to(project)): sha(path.read_bytes()) for path in project.rglob("*.sdc")},
        "diagnostic_only": True, "timing_acceptance": "not-qualified", "hardware_acceptance": "not-run",
        "boot_firmware_compatibility": "not-qualified"}
    (output / "build.json").write_text(json.dumps(receipt, indent=2, sort_keys=True) + "\n")
    return artifact


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("project", "build"))
    parser.add_argument("--mode", choices=("idle", "menu-pattern"), required=True)
    parser.add_argument("--vendor-archive", type=Path, required=True)
    parser.add_argument("--output", type=Path)
    parser.add_argument("--quartus-root", default=os.environ.get("QUARTUS_ROOTDIR", ""))
    args = parser.parse_args(argv)
    try:
        identity = source_identity()
        revision = identity.get("fes_revision") if args.action == "build" else None
        if args.action == "build" and revision is None:
            raise ValueError("display build requires clean committed source")
        tool, version = TOOLS.locate_quartus(args.quartus_root) if revision else (None, None)
        output = prepare(args.vendor_archive, args.mode, args.output, revision)
        print("Diagnostic project: " + str(output), flush=True)
        if tool:
            if source_identity() != identity:
                raise ValueError("source identity changed before IP generation")
            print("Diagnostic SOF: " + str(compile_project(output, tool, version)), flush=True)
    except (OSError, ValueError, KeyError, zipfile.BadZipFile, subprocess.CalledProcessError) as error:
        parser.exit(1, "de25-display: " + str(error) + "\n")


if __name__ == "__main__":
    main()
